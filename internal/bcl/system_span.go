package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// spanType/readOnlySpanType/memoryType/readOnlyMemoryType all share the
// same 3-field shape — (backing, start, length) — a defensive view over
// an existing runtime.Array or a string's characters, since vmnet has no
// raw pointers to model the real ref-struct/byref semantics with. Kept as
// 4 separate registered Types (rather than one shared var) only so error
// messages and Struct.Type identity stay meaningful — nothing depends on
// them being distinct beyond that.
var (
	spanType           = runtime.NewValueType("System", "Span`1", []string{"backing", "start", "length"}, spanDefaults())
	readOnlySpanType   = runtime.NewValueType("System", "ReadOnlySpan`1", []string{"backing", "start", "length"}, spanDefaults())
	memoryType         = runtime.NewValueType("System", "Memory`1", []string{"backing", "start", "length"}, spanDefaults())
	readOnlyMemoryType = runtime.NewValueType("System", "ReadOnlyMemory`1", []string{"backing", "start", "length"}, spanDefaults())
)

func spanDefaults() []runtime.Value {
	return []runtime.Value{runtime.Null(), runtime.Int32(0), runtime.Int32(0)}
}

func init() {
	for _, t := range []*runtime.Type{spanType, readOnlySpanType, memoryType, readOnlyMemoryType} {
		registerValueType(t)
	}

	register("System.MemoryExtensions::AsSpan", true, memoryExtensionsAsSpan)
	register("System.MemoryExtensions::AsMemory", true, memoryExtensionsAsMemory)
	// ReadOnlySpan<T>(void* pointer, int length): the "unsafe" ctor real
	// code uses to wrap a compiler-embedded RVA data blob with zero
	// copying (found running real Jint/Esprima: Character.s_characterData,
	// a 32KB Unicode table literal) — the *only* ReadOnlySpan<T> ctor
	// shape vmnet has ever needed against real IL so far, so this is
	// deliberately narrow rather than a general pointer-arithmetic
	// implementation vmnet has no way to support anyway (no raw memory
	// model). "pointer" arrives as a managed pointer (KindRef) to
	// whatever Value the RVA-backed static field itself was built as
	// (Fase 3.27, assembly.go's rvaFieldBytes/bytesToInt32Array) — always
	// a real KindArray in every case seen so far.
	registerValueTypeCtor("System.ReadOnlySpan`1", readOnlySpanFromPointerCtor)

	for _, prefix := range []string{"System.Span`1", "System.ReadOnlySpan`1"} {
		register(prefix+"::get_Length", true, spanLength)
		register(prefix+"::get_Item", true, spanGetItem)
		register(prefix+"::Slice", true, spanSlice)
		register(prefix+"::ToString", true, spanToString)
		register(prefix+"::ToArray", true, spanToArray)
	}

	for _, prefix := range []string{"System.Memory`1", "System.ReadOnlyMemory`1"} {
		register(prefix+"::get_Length", true, spanLength)
		register(prefix+"::get_Span", true, memoryGetSpan)
	}
}

func readOnlySpanFromPointerCtor(args []runtime.Value) (*runtime.Struct, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("bcl: ReadOnlySpan<T>(void*, int) expects 2 arguments")
	}
	ptr := args[0]
	if ptr.Kind != runtime.KindRef || ptr.Ref == nil {
		return nil, fmt.Errorf("bcl: ReadOnlySpan<T>(void*, int): first argument is not a managed pointer")
	}
	backing := *ptr.Ref
	if backing.Kind != runtime.KindArray {
		return nil, fmt.Errorf("bcl: ReadOnlySpan<T>(void*, int): unsupported backing shape (vmnet has no raw memory model beyond an RVA-backed static array)")
	}
	length := args[1]
	if length.Kind != runtime.KindI4 {
		return nil, fmt.Errorf("bcl: ReadOnlySpan<T>(void*, int): second argument is not an int32 length")
	}
	s := runtime.NewStruct(readOnlySpanType)
	s.Fields[0] = backing
	s.Fields[1] = runtime.Int32(0)
	s.Fields[2] = length
	return s, nil
}

// memoryExtensionsAsSpan backs every AsSpan overload — extension methods
// compile to a plain static call with the extended value as args[0], not
// a "this" receiver, so this reads args positionally: (source[, start[,
// length]]).
func memoryExtensionsAsSpan(args []runtime.Value) (runtime.Value, error) {
	return newSpanFrom(args, spanType, readOnlySpanType)
}

func memoryExtensionsAsMemory(args []runtime.Value) (runtime.Value, error) {
	return newSpanFrom(args, memoryType, readOnlyMemoryType)
}

func newSpanFrom(args []runtime.Value, arrayType, stringType *runtime.Type) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: AsSpan/AsMemory expects a source argument")
	}
	backing := args[0]
	var full int
	var target *runtime.Type
	switch backing.Kind {
	case runtime.KindString:
		full = len([]rune(backing.Str))
		target = stringType
	case runtime.KindArray:
		if backing.Arr == nil {
			return runtime.Value{}, &runtime.ManagedException{TypeName: "System.NullReferenceException", Message: "array reference is null (AsSpan)"}
		}
		full = len(backing.Arr.Elems)
		target = arrayType
	default:
		return runtime.Value{}, fmt.Errorf("bcl: AsSpan/AsMemory: unsupported source kind")
	}

	start, length := 0, full
	if len(args) >= 2 && args[1].Kind == runtime.KindI4 {
		start = int(args[1].I4)
		length = full - start
	}
	if len(args) >= 3 && args[2].Kind == runtime.KindI4 {
		length = int(args[2].I4)
	}
	if start < 0 || length < 0 || start+length > full {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "AsSpan/AsMemory: start/length out of range"}
	}

	s := runtime.NewStruct(target)
	s.Fields[0] = backing
	s.Fields[1] = runtime.Int32(int32(start))
	s.Fields[2] = runtime.Int32(int32(length))
	return runtime.StructVal(s), nil
}

func spanLength(args []runtime.Value) (runtime.Value, error) {
	s, err := derefStructReceiver(args, "Span", "Span.Length")
	if err != nil {
		return runtime.Value{}, err
	}
	return s.Fields[2], nil
}

// spanGetItem backs Span<T>/ReadOnlySpan<T>'s indexer — which, unlike a
// normal property, is declared `ref T this[int]` (`ref readonly` for
// ReadOnlySpan<T>): the real signature returns a managed pointer to the
// element, not the value. Confirmed against real compiled IL before
// fixing this (Fase 3.12): `span[i]` is `call get_Item` + `ldind.i4`, and
// `span[i] = v` is the *same* `call get_Item` + `stind.i4` — there is no
// separate set_Item in the metadata for a ref-returning indexer at all.
// Returning the value directly here would push a non-KindRef where
// ldind/stind expect one, failing immediately with "dereferencing a null
// managed pointer".
func spanGetItem(args []runtime.Value) (runtime.Value, error) {
	s, err := derefStructReceiver(args, "Span", "Span indexer")
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 || args[1].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: Span indexer expects an int32 index")
	}
	idx, start, length := int(args[1].I4), int(s.Fields[1].I4), int(s.Fields[2].I4)
	if idx < 0 || idx >= length {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.IndexOutOfRangeException", Message: "Index was outside the bounds of the span."}
	}
	switch backing := s.Fields[0]; backing.Kind {
	case runtime.KindString:
		// No addressable storage backs a Go string's runes — box the
		// character into a fresh Value and return a pointer to that. Only
		// ever used transiently (immediately deref'd by the following
		// ldind, per the real ReadOnlySpan<char> call shape above), so a
		// short-lived heap Value is fine — nothing else can observe it.
		v := runtime.Int32([]rune(backing.Str)[start+idx])
		return runtime.RefTo(&v), nil
	case runtime.KindArray:
		return runtime.RefTo(&backing.Arr.Elems[start+idx]), nil
	default:
		return runtime.Value{}, fmt.Errorf("bcl: Span: unsupported backing kind")
	}
}

func spanSlice(args []runtime.Value) (runtime.Value, error) {
	s, err := derefStructReceiver(args, "Span", "Span.Slice")
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 || args[1].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: Span.Slice expects an int32 start")
	}
	curStart, curLength := int(s.Fields[1].I4), int(s.Fields[2].I4)
	start := int(args[1].I4)
	length := curLength - start
	if len(args) >= 3 && args[2].Kind == runtime.KindI4 {
		length = int(args[2].I4)
	}
	if start < 0 || length < 0 || start+length > curLength {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "Span.Slice: start/length out of range"}
	}
	out := runtime.NewStruct(s.Type)
	out.Fields[0] = s.Fields[0]
	out.Fields[1] = runtime.Int32(int32(curStart + start))
	out.Fields[2] = runtime.Int32(int32(length))
	return runtime.StructVal(out), nil
}

func spanToString(args []runtime.Value) (runtime.Value, error) {
	s, err := derefStructReceiver(args, "Span", "Span.ToString")
	if err != nil {
		return runtime.Value{}, err
	}
	if str, ok := spanToStringValue(s); ok {
		return runtime.String(str), nil
	}
	// A non-char Span's real ToString() is "System.Span<T>" (the type
	// name, not its contents) — matches the same "unhelpful CLR default"
	// choice already made for List/Dictionary (system_object.go).
	return runtime.String(displayString(s.Fields[0])), nil
}

// spanToStringValue is the shared core for both the direct
// System.Span`1::ToString() native above and displayString's
// System.Object::ToString dispatch (system_object.go) — real compiled IL
// for ReadOnlySpan<char>.ToString() goes through the latter path
// (confirmed against real IL, Fase 3.12), same as StringBuilder before
// it. Only char-backed (string-backed) spans get a meaningful ToString;
// anything else returns false so the caller falls back to a generic
// representation.
func spanToStringValue(s *runtime.Struct) (string, bool) {
	if s.Type != spanType && s.Type != readOnlySpanType {
		return "", false
	}
	backing := s.Fields[0]
	if backing.Kind != runtime.KindString {
		return "", false
	}
	start, length := int(s.Fields[1].I4), int(s.Fields[2].I4)
	runes := []rune(backing.Str)
	if start < 0 || length < 0 || start+length > len(runes) {
		return "", false
	}
	return string(runes[start : start+length]), true
}

func spanToArray(args []runtime.Value) (runtime.Value, error) {
	s, err := derefStructReceiver(args, "Span", "Span.ToArray")
	if err != nil {
		return runtime.Value{}, err
	}
	start, length := int(s.Fields[1].I4), int(s.Fields[2].I4)
	out := runtime.NewArray(length)
	switch backing := s.Fields[0]; backing.Kind {
	case runtime.KindString:
		runes := []rune(backing.Str)
		for i := 0; i < length; i++ {
			out.Elems[i] = runtime.Int32(runes[start+i])
		}
	case runtime.KindArray:
		copy(out.Elems, backing.Arr.Elems[start:start+length])
	default:
		return runtime.Value{}, fmt.Errorf("bcl: Span.ToArray: unsupported backing kind")
	}
	return runtime.ArrRef(out), nil
}

func memoryGetSpan(args []runtime.Value) (runtime.Value, error) {
	s, err := derefStructReceiver(args, "Memory", "Memory.Span")
	if err != nil {
		return runtime.Value{}, err
	}
	target := spanType
	if s.Type == readOnlyMemoryType {
		target = readOnlySpanType
	}
	out := runtime.NewStruct(target)
	out.Fields[0], out.Fields[1], out.Fields[2] = s.Fields[0], s.Fields[1], s.Fields[2]
	return runtime.StructVal(out), nil
}
