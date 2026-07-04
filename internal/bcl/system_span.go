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
//
// Field names deliberately match System.Memory's own real, NuGet-cached
// ReadOnlySpan<T>/Span<T> shim (fields _pinnable/_byteOffset/_length,
// confirmed against its real decompiled IL) rather than something
// vmnet-idiomatic like "backing"/"start"/"length": newobj always goes
// through our own native ctor below (bcl.LookupValueTypeCtor intercepts
// every constructor overload for this type name, real ctor IL never
// runs), but plenty of the *other* real members this package's own
// ReadOnlySpan.cs defines (IsEmpty, ...) are NOT natively registered here
// and so still run as real interpreted IL — including a bare `ldfld
// _length` — Fase 3.40, found running real DocumentFormat.OpenXml code
// depending transitively on System.Memory. Matching the real field names
// makes any such not-yet-intercepted simple field read work against our
// synthetic struct without needing a bespoke native registration for
// every single member the real type happens to expose.
var (
	spanType           = runtime.NewValueType("System", "Span`1", []string{"_pinnable", "_byteOffset", "_length"}, spanDefaults())
	readOnlySpanType   = runtime.NewValueType("System", "ReadOnlySpan`1", []string{"_pinnable", "_byteOffset", "_length"}, spanDefaults())
	memoryType         = runtime.NewValueType("System", "Memory`1", []string{"_object", "_index", "_length"}, spanDefaults())
	readOnlyMemoryType = runtime.NewValueType("System", "ReadOnlyMemory`1", []string{"_object", "_index", "_length"}, spanDefaults())
)

func spanDefaults() []runtime.Value {
	return []runtime.Value{runtime.Null(), runtime.Int32(0), runtime.Int32(0)}
}

// SpanBacking exposes a Span<T>/ReadOnlySpan<T>/Memory<T>/ReadOnlyMemory<T>
// value's own (backing, start, length) triple to internal/interpreter
// (Fase 3.41, MemoryMarshal.Read<T>/Write<T> — see that package's own
// memorymarshal.go) without needing its own copy of this package's
// private field-index convention.
func SpanBacking(v runtime.Value) (backing runtime.Value, start, length int, ok bool) {
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	if v.Kind != runtime.KindStruct || v.Struct == nil {
		return runtime.Value{}, 0, 0, false
	}
	switch v.Struct.Type {
	case spanType, readOnlySpanType, memoryType, readOnlyMemoryType:
	default:
		return runtime.Value{}, 0, 0, false
	}
	return v.Struct.Fields[0], int(v.Struct.Fields[1].I4), int(v.Struct.Fields[2].I4), true
}

func init() {
	// Buffer.BlockCopy(Array src, int srcOffset, Array dst, int dstOffset,
	// int count) (Fase 3.41) — real semantics are byte-granular (works
	// across any array element type by raw byte size), but every real
	// caller vmnet has needed to support so far (JsonDocument.Parse's own
	// ArrayPool<byte> rented-buffer bookkeeping, both TFM builds) only
	// ever copies byte[] arrays, where vmnet's own uniform "1 array
	// element == 1 KindI4" representation already makes a byte offset/
	// count exactly an element offset/count — see bufferBlockCopy's own
	// doc comment.
	register("System.Buffer::BlockCopy", false, bufferBlockCopy)

	for _, t := range []*runtime.Type{spanType, readOnlySpanType, memoryType, readOnlyMemoryType} {
		registerValueType(t)
	}

	register("System.MemoryExtensions::AsSpan", true, memoryExtensionsAsSpan)
	register("System.MemoryExtensions::AsMemory", true, memoryExtensionsAsMemory)
	register("System.MemoryExtensions::IndexOf", true, memoryExtensionsIndexOf)
	register("System.MemoryExtensions::TrimEnd", true, memoryExtensionsTrimEnd)
	register("System.MemoryExtensions::TrimStart", true, memoryExtensionsTrimStart)
	register("System.MemoryExtensions::Trim", true, memoryExtensionsTrim)
	register("System.MemoryExtensions::SequenceEqual", true, memoryExtensionsSequenceEqual)
	// The real implicit `T[] -> ReadOnlySpan<T>`/`T[] -> Span<T>`
	// conversion operator (`char[] arr; ReadOnlySpan<char> s = arr;`) —
	// same shape as the (array) newobj overload, just reached via
	// op_Implicit instead of a constructor call (Fase 3.40, found via
	// System.IO.Packaging.ContentType.ValidateCarriageReturns/ParseTypeAndSubType
	// passing a static char[] straight into a ReadOnlySpan<char>
	// parameter).
	register("System.ReadOnlySpan`1::op_Implicit", true, readOnlySpanOpImplicit)
	register("System.Span`1::op_Implicit", true, spanOpImplicit)
	// ReadOnlySpan<T> has several real constructor overloads, all
	// collapsing to this one native (Fase 3.12's overload-name-collapse
	// convention, same as StringBuilder.Append): (T[] array) — 1 arg —
	// and (T[] array, int start, int length) — 3 args — are the safe,
	// managed-code shapes (Fase 3.40, found via a real, load-bearing
	// case: SixLabors.Fonts, a real font-parsing library ClosedXML's own
	// font-metrics engine depends on, wraps a real byte[] this way while
	// parsing embedded .ttf data). (void* pointer, int length) — 2 args
	// — is the "unsafe" ctor real code uses to wrap a compiler-embedded
	// RVA data blob with zero copying (found running real Jint/Esprima:
	// Character.s_characterData, a 32KB Unicode table literal) — kept
	// deliberately narrow (an RVA-backed static array only) rather than a
	// general pointer-arithmetic implementation vmnet has no way to
	// support anyway (no raw memory model).
	registerValueTypeCtor("System.ReadOnlySpan`1", readOnlySpanCtor)
	// Span<T> has the exact same real constructor overloads as
	// ReadOnlySpan<T> (Fase 3.41, found via a real, load-bearing case:
	// System.Text.Json's own JsonReaderHelper stackalloc's a scratch
	// Span<byte> buffer, constructed via the (void*, int) overload right
	// after `localloc` — see ir.LocalAlloc's own doc comment). Only
	// ReadOnlySpan`1 had a registered ctor before this; a writable
	// Span<T> silently fell through to a zero-valued default instance
	// with no backing array at all, breaking every native (Encoding.
	// GetBytes, ...) that writes through it.
	registerValueTypeCtor("System.Span`1", spanCtor)

	for _, prefix := range []string{"System.Span`1", "System.ReadOnlySpan`1"} {
		register(prefix+"::get_Length", true, spanLength)
		register(prefix+"::get_IsEmpty", true, spanIsEmpty)
		register(prefix+"::get_Item", true, spanGetItem)
		register(prefix+"::Slice", true, spanSlice)
		register(prefix+"::ToString", true, spanToString)
		register(prefix+"::ToArray", true, spanToArray)
		// GetPinnableReference()/the internal DangerousGetPinnableReference()
		// (Fase 3.41) — see spanGetPinnableReference's own doc comment for
		// why the real interpreted body (System.Memory's own shim IL,
		// still used for every OTHER member not natively registered here)
		// can't be trusted for these two specifically.
		register(prefix+"::GetPinnableReference", true, spanGetPinnableReference)
		register(prefix+"::DangerousGetPinnableReference", true, spanGetPinnableReference)
	}
	// Span<T>.Clear() (Fase 3.41) — ReadOnlySpan<T> has no such member at
	// all (real API, read-only) — real callers only ever reach this as
	// cleanup after a rented ArrayPool buffer transcoding attempt fails
	// (e.g. JsonDocument.Parse's own catch block:
	// `array.AsSpan(0, utf8ByteCount).Clear()` before returning it to the
	// pool), zeroing every element so a pooled buffer never leaks stale
	// content to Rent's next caller.
	register("System.Span`1::Clear", false, spanClear)

	for _, prefix := range []string{"System.Memory`1", "System.ReadOnlyMemory`1"} {
		register(prefix+"::get_Length", true, spanLength)
		register(prefix+"::get_IsEmpty", true, spanIsEmpty)
		register(prefix+"::get_Span", true, memoryGetSpan)
	}
}

func readOnlySpanCtor(args []runtime.Value) (*runtime.Struct, error) {
	return spanCtorAs(readOnlySpanType, "ReadOnlySpan<T>", args)
}

func spanCtor(args []runtime.Value) (*runtime.Struct, error) {
	return spanCtorAs(spanType, "Span<T>", args)
}

// spanCtorAs backs both Span<T> and ReadOnlySpan<T>'s real constructor
// overloads — identical shapes, differing only in which registered Type
// the resulting struct carries (see spanCtor/readOnlySpanCtor's own doc
// comments, Fase 3.41).
func spanCtorAs(target *runtime.Type, name string, args []runtime.Value) (*runtime.Struct, error) {
	switch len(args) {
	case 1:
		// (T[] array) — the whole array, start 0.
		if args[0].Kind != runtime.KindArray {
			return nil, fmt.Errorf("bcl: %s(array) expects a real array argument", name)
		}
		s := runtime.NewStruct(target)
		s.Fields[0] = args[0]
		s.Fields[1] = runtime.Int32(0)
		s.Fields[2] = runtime.Int32(int32(len(args[0].Arr.Elems)))
		return s, nil
	case 2:
		return spanFromPointerCtor(target, name, args)
	case 3:
		// (T[] array, int start, int length).
		if args[0].Kind != runtime.KindArray {
			return nil, fmt.Errorf("bcl: %s(array, start, length) expects a real array argument", name)
		}
		if args[1].Kind != runtime.KindI4 || args[2].Kind != runtime.KindI4 {
			return nil, fmt.Errorf("bcl: %s(array, start, length) expects int32 start/length", name)
		}
		s := runtime.NewStruct(target)
		s.Fields[0] = args[0]
		s.Fields[1] = args[1]
		s.Fields[2] = args[2]
		return s, nil
	default:
		return nil, fmt.Errorf("bcl: %s constructor: unsupported argument count %d", name, len(args))
	}
}

func spanFromPointerCtor(target *runtime.Type, name string, args []runtime.Value) (*runtime.Struct, error) {
	ptr := args[0]
	if ptr.Kind != runtime.KindRef || ptr.Ref == nil {
		return nil, fmt.Errorf("bcl: %s(void*, int): first argument is not a managed pointer", name)
	}
	backing := *ptr.Ref
	if backing.Kind != runtime.KindArray {
		return nil, fmt.Errorf("bcl: %s(void*, int): unsupported backing shape (vmnet has no raw memory model beyond an RVA-backed static array or a localloc'd one — see ir.LocalAlloc)", name)
	}
	length := args[1]
	if length.Kind != runtime.KindI4 {
		return nil, fmt.Errorf("bcl: %s(void*, int): second argument is not an int32 length", name)
	}
	s := runtime.NewStruct(target)
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

func spanIsEmpty(args []runtime.Value) (runtime.Value, error) {
	s, err := derefStructReceiver(args, "Span", "Span.IsEmpty")
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(s.Fields[2].I4 == 0), nil
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

// spanGetPinnableReference backs Span<T>/ReadOnlySpan<T>.
// GetPinnableReference()/the internal DangerousGetPinnableReference()
// (Fase 3.41) — every real caller vmnet has needed to support only ever
// uses these as the source of a `fixed (T* p = span)` pointer pin
// (ECMA-335's own C# `fixed` lowering), immediately handing the
// resulting "pointer" to ANOTHER method taking a (T*, length) shape
// (most notably Encoding.GetByteCount/GetBytes's pointer-taking
// overloads — System.Text.Json's netstandard2.0 build, the TFM vmnet
// actually selects, uses exactly this to transcode JsonDocument.Parse's
// UTF-16 input to UTF-8: JsonReaderHelper.GetUtf8ByteCount/
// GetUtf8FromText) rather than ever performing real pointer arithmetic
// on it directly. vmnet has no raw address space to hand back a real
// pointer for, so this returns a ref to a fresh copy of the SAME
// (backing, start, length) span struct it was called on instead — any
// natively-intercepted callee that receives this "pointer" can
// dereference it straight back to the full remaining span content
// (spanCharArg, system_text.go), exactly like a real pinned pointer
// still describes the whole span, not just its first element.
//
// This is NOT left to fall through to the real interpreted body (like
// get_Length/get_IsEmpty/et al. above already do, Fase 3.12) because
// that real body (System.Memory's own shim: `Unsafe.AddByteOffset(ref
// _pinnable.Data, _byteOffset)`) assumes System.Memory's own real
// Pinnable<T>/IntPtr field semantics — but vmnet's synthetic struct only
// ever reuses those real field NAMES for simple field reads (a bare
// `ldfld _length`), never populated to match their real SHAPE (Fase
// 3.7's original span design), so the real body's own field accesses
// silently read garbage instead.
func spanGetPinnableReference(args []runtime.Value) (runtime.Value, error) {
	s, err := derefStructReceiver(args, "Span", "Span.GetPinnableReference")
	if err != nil {
		return runtime.Value{}, err
	}
	cp := runtime.NewStruct(s.Type)
	cp.Fields[0] = s.Fields[0]
	cp.Fields[1] = s.Fields[1]
	cp.Fields[2] = s.Fields[2]
	v := runtime.StructVal(cp)
	return runtime.RefTo(&v), nil
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
	// A default/empty Span<T> (e.g. `default(ReadOnlySpan<byte>)`,
	// `Span<T>.Empty`) has no backing store at all — Fields[0] is
	// KindNull, not a zero-length array — real ToArray() on one of these
	// just returns an empty array regardless, so length==0 short-circuits
	// before the backing-kind switch below (Fase 3.40, found via System.
	// Text.Json's own empty-buffer initialization paths).
	if length == 0 {
		return runtime.ArrRef(out), nil
	}
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

// spanClear backs Span<T>.Clear() (Fase 3.41) — zeroes every element the
// span covers in its backing array. A default/empty Span<T> has no
// backing store at all (Fields[0] is KindNull, same convention
// spanToArray above already documents), so length==0 short-circuits
// before touching it.
func spanClear(args []runtime.Value) (runtime.Value, error) {
	s, err := derefStructReceiver(args, "Span", "Span.Clear")
	if err != nil {
		return runtime.Value{}, err
	}
	length := int(s.Fields[2].I4)
	if length == 0 {
		return runtime.Value{}, nil
	}
	backing := s.Fields[0]
	if backing.Kind != runtime.KindArray || backing.Arr == nil {
		return runtime.Value{}, fmt.Errorf("bcl: Span.Clear: unsupported backing kind")
	}
	start := int(s.Fields[1].I4)
	// Every element vmnet's own span/array natives store is a KindI4
	// (byte/char/int alike all collapse to that one Kind, spec §17.1) —
	// same convention byteArrayArgToBytes (system_text.go) and every
	// other array-backed span native here already assumes.
	for i := start; i < start+length; i++ {
		backing.Arr.Elems[i] = runtime.Int32(0)
	}
	return runtime.Value{}, nil
}

// bufferBlockCopy backs the static Buffer.BlockCopy(Array src, int
// srcOffset, Array dst, int dstOffset, int count) — see this native's
// own registration comment for why treating count/offsets as element
// (not true byte) counts is safe for every real caller reached so far.
func bufferBlockCopy(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 5 {
		return runtime.Value{}, fmt.Errorf("bcl: Buffer.BlockCopy expects (src, srcOffset, dst, dstOffset, count)")
	}
	src, srcOffset, dst, dstOffset, count := args[0], args[1], args[2], args[3], args[4]
	if src.Kind != runtime.KindArray || src.Arr == nil || dst.Kind != runtime.KindArray || dst.Arr == nil {
		return runtime.Value{}, fmt.Errorf("bcl: Buffer.BlockCopy expects real array arguments")
	}
	if srcOffset.Kind != runtime.KindI4 || dstOffset.Kind != runtime.KindI4 || count.Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: Buffer.BlockCopy expects int32 offset/count arguments")
	}
	so, do, n := int(srcOffset.I4), int(dstOffset.I4), int(count.I4)
	if n == 0 {
		return runtime.Value{}, nil
	}
	if so < 0 || do < 0 || n < 0 || so+n > len(src.Arr.Elems) || do+n > len(dst.Arr.Elems) {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "Offset and length were out of bounds for the array (Buffer.BlockCopy)."}
	}
	copy(dst.Arr.Elems[do:do+n], src.Arr.Elems[so:so+n])
	return runtime.Value{}, nil
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

// spanElemAt reads element idx (0-based, within [0,length)) of a
// Span/ReadOnlySpan's logical content, regardless of whether it's backed
// by a real array or a Go string's runes — shared by IndexOf/TrimEnd/Trim
// below, which all need to compare individual elements without caring
// which backing shape they came from.
func spanElemAt(s *runtime.Struct, idx int) (int32, bool) {
	start, length := int(s.Fields[1].I4), int(s.Fields[2].I4)
	if idx < 0 || idx >= length {
		return 0, false
	}
	switch backing := s.Fields[0]; backing.Kind {
	case runtime.KindString:
		runes := []rune(backing.Str)
		return runes[start+idx], true
	case runtime.KindArray:
		return backing.Arr.Elems[start+idx].I4, true
	default:
		return 0, false
	}
}

// spanContainsElem reports whether value matches any element of a
// (typically short) trim-character-set span/array argument — used by
// TrimEnd/TrimStart/Trim's ReadOnlySpan<char>-of-trim-chars overload.
func spanContainsElem(set *runtime.Struct, value int32) bool {
	length := int(set.Fields[2].I4)
	for i := 0; i < length; i++ {
		v, ok := spanElemAt(set, i)
		if ok && v == value {
			return true
		}
	}
	return false
}

// memoryExtensionsIndexOf backs MemoryExtensions.IndexOf<T>(span, value)
// and MemoryExtensions.IndexOf<T>(span, valueSpan) alike — extension
// methods compile with the extended span as args[0], not a `this`
// receiver (Fase 3.40, found via System.IO.Packaging.ContentType's real
// `typeAndSubType.IndexOf('/')` call parsing an OPC part's ContentType
// string).
func memoryExtensionsIndexOf(args []runtime.Value) (runtime.Value, error) {
	s, err := derefStructReceiver(args, "Span", "MemoryExtensions.IndexOf")
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: MemoryExtensions.IndexOf expects a value or span argument")
	}
	length := int(s.Fields[2].I4)
	if args[1].Kind == runtime.KindStruct || args[1].Kind == runtime.KindRef {
		needle, nerr := derefStructReceiver(args[1:], "Span", "MemoryExtensions.IndexOf")
		if nerr != nil {
			return runtime.Value{}, nerr
		}
		needleLen := int(needle.Fields[2].I4)
		if needleLen == 0 {
			return runtime.Int32(0), nil
		}
		for i := 0; i+needleLen <= length; i++ {
			match := true
			for j := 0; j < needleLen; j++ {
				a, _ := spanElemAt(s, i+j)
				b, _ := spanElemAt(needle, j)
				if a != b {
					match = false
					break
				}
			}
			if match {
				return runtime.Int32(int32(i)), nil
			}
		}
		return runtime.Int32(-1), nil
	}
	target := args[1].I4
	for i := 0; i < length; i++ {
		v, _ := spanElemAt(s, i)
		if v == target {
			return runtime.Int32(int32(i)), nil
		}
	}
	return runtime.Int32(-1), nil
}

func memoryExtensionsTrimEnd(args []runtime.Value) (runtime.Value, error) {
	return spanTrim(args, false, true)
}

func memoryExtensionsTrimStart(args []runtime.Value) (runtime.Value, error) {
	return spanTrim(args, true, false)
}

func memoryExtensionsTrim(args []runtime.Value) (runtime.Value, error) {
	return spanTrim(args, true, true)
}

func spanTrim(args []runtime.Value, trimStart, trimEnd bool) (runtime.Value, error) {
	s, err := derefStructReceiver(args, "Span", "MemoryExtensions.Trim")
	if err != nil {
		return runtime.Value{}, err
	}
	matches := func(v int32) bool {
		if len(args) < 2 {
			// No trim-set argument: the default overload trims Unicode
			// whitespace — every real caller in this loop's target
			// packages only ever trims ASCII space/CR/LF/TAB, so that
			// narrower set is enough here.
			return v == ' ' || v == '\t' || v == '\r' || v == '\n'
		}
		switch args[1].Kind {
		case runtime.KindI4:
			return v == args[1].I4
		case runtime.KindStruct, runtime.KindRef, runtime.KindArray:
			set, serr := derefStructReceiver(args[1:], "Span", "MemoryExtensions.Trim")
			if serr == nil {
				return spanContainsElem(set, v)
			}
			if args[1].Kind == runtime.KindArray && args[1].Arr != nil {
				for _, e := range args[1].Arr.Elems {
					if e.I4 == v {
						return true
					}
				}
			}
			return false
		}
		return false
	}

	start, length := int(s.Fields[1].I4), int(s.Fields[2].I4)
	lo, hi := 0, length
	if trimStart {
		for lo < hi {
			v, _ := spanElemAt(s, lo)
			if !matches(v) {
				break
			}
			lo++
		}
	}
	if trimEnd {
		for hi > lo {
			v, _ := spanElemAt(s, hi-1)
			if !matches(v) {
				break
			}
			hi--
		}
	}
	out := runtime.NewStruct(s.Type)
	out.Fields[0] = s.Fields[0]
	out.Fields[1] = runtime.Int32(int32(start + lo))
	out.Fields[2] = runtime.Int32(int32(hi - lo))
	return runtime.StructVal(out), nil
}

// spanOpImplicit/readOnlySpanOpImplicit back the real implicit
// `T[] -> Span<T>`/`T[] -> ReadOnlySpan<T>` conversion operator — the
// same shape as the array-argument newobj overload, just reached via a
// compiler-inserted op_Implicit call instead of `new Span<T>(array)`
// (Fase 3.40, found via System.IO.Packaging.ContentType passing its own
// static char[] trim tables straight into a ReadOnlySpan<char> parameter).
func readOnlySpanOpImplicit(args []runtime.Value) (runtime.Value, error) {
	s, err := readOnlySpanCtor(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.StructVal(s), nil
}

// spanOpImplicit backs BOTH real implicit conversion operators the BCL
// declares under the one "op_Implicit" name on Span<T> (Fase 3.41,
// collapsed together per this file's established "same name, one
// native" convention, same as readOnlySpanOpImplicit's own T[]/
// ReadOnlySpan<T> pair): `T[] -> Span<T>` (existing, KindArray argument)
// and `Span<T> -> ReadOnlySpan<T>` (found running real System.Text.Json
// 8.0.5's netstandard2.0 JsonReaderHelper.GetUtf8FromText, whose own
// `return dest.Length - bytes.Length;`-adjacent code passes an already-
// built Span<byte> where a ReadOnlySpan<byte>-shaped call expects one —
// a KindStruct argument, since by this point it's already a real span,
// not a raw array). Same (backing, start, length) fields either way —
// the conversion is a pure read-only reinterpretation, never a copy.
func spanOpImplicit(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("bcl: Span<T> implicit conversion expects exactly 1 argument")
	}
	if s, ok := spanStructArg(args[0]); ok {
		out := runtime.NewStruct(readOnlySpanType)
		out.Fields[0] = s.Fields[0]
		out.Fields[1] = s.Fields[1]
		out.Fields[2] = s.Fields[2]
		return runtime.StructVal(out), nil
	}
	if args[0].Kind != runtime.KindArray {
		return runtime.Value{}, fmt.Errorf("bcl: Span<T> implicit conversion expects an array or Span<T> argument")
	}
	s := runtime.NewStruct(spanType)
	s.Fields[0] = args[0]
	s.Fields[1] = runtime.Int32(0)
	if args[0].Arr != nil {
		s.Fields[2] = runtime.Int32(int32(len(args[0].Arr.Elems)))
	}
	return runtime.StructVal(s), nil
}

// memoryExtensionsSequenceEqual backs MemoryExtensions.SequenceEqual —
// an extension method, so args are positional (span, otherSpan), same
// convention every other MemoryExtensions native here follows (Fase
// 3.40, found via System.IO.Packaging's own PartBasedPackageProperties.
// ValidateXsiType: `name.AsSpan().SequenceEqual(attribute.AsSpan(...))`).
func memoryExtensionsSequenceEqual(args []runtime.Value) (runtime.Value, error) {
	a, err := derefStructReceiver(args, "Span", "MemoryExtensions.SequenceEqual")
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: MemoryExtensions.SequenceEqual expects a second span")
	}
	b, err := derefStructReceiver(args[1:], "Span", "MemoryExtensions.SequenceEqual")
	if err != nil {
		return runtime.Value{}, err
	}
	lenA, lenB := int(a.Fields[2].I4), int(b.Fields[2].I4)
	if lenA != lenB {
		return runtime.Bool(false), nil
	}
	for i := 0; i < lenA; i++ {
		va, ok1 := spanElemAt(a, i)
		vb, ok2 := spanElemAt(b, i)
		if !ok1 || !ok2 || va != vb {
			return runtime.Bool(false), nil
		}
	}
	return runtime.Bool(true), nil
}
