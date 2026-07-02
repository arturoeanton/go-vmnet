package bcl

import (
	"fmt"
	"hash/fnv"
	"math"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

func init() {
	// Every class the C# compiler emits chains up to Object::.ctor(), even
	// when the source has no explicit base-class call.
	register("System.Object::.ctor", false, objectCtorNoop)
	register("System.Object::ToString", true, objectToString)
	register("System.Object::Equals", true, objectEquals)
	register("System.Object::GetHashCode", true, objectGetHashCode)
	// Attribute::.ctor: modern C# compilers emit attribute classes of
	// their own (e.g. EmbeddedAttribute, RefSafetyRulesAttribute) into
	// every assembly for certain language features, regardless of
	// whether the source uses them — their .ctor chains here.
	register("System.Attribute::.ctor", false, objectCtorNoop)
}

func objectCtorNoop(args []runtime.Value) (runtime.Value, error) {
	return runtime.Value{}, nil
}

// objectToString backs a boxed value's virtual ToString() call: since
// box is a no-op in vmnet's Value model (see internal/ir/builder.go), the
// callvirt still carries the real Kind, so this can dispatch on it exactly
// like the CLR would dispatch on the boxed value's runtime type.
//
// It's also, today, the ONLY place a ToString() override on a native BCL
// type (StringBuilder, ...) gets a chance to run at all: vmnet resolves
// callvirt targets statically from the MemberRef's declared class, not by
// real virtual dispatch on the receiver's runtime type (no vtable yet —
// Fase 3.8), and the C# compiler is free to emit `.ToString()` call sites
// against the base System.Object::ToString MemberRef relying on the CLR's
// virtual dispatch to reach the override — which is exactly what happens
// for `StringBuilder.ToString()`. See nativeToString below.
func objectToString(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 {
		return runtime.String("null"), nil
	}
	return runtime.String(displayString(args[0])), nil
}

func displayString(v runtime.Value) string {
	// A value type's `this` (e.g. `item.ToString()` inside a generic
	// method over T, compiled as `constrained. !!0` + `callvirt
	// Object::ToString`) always arrives as a managed pointer, never the
	// struct value directly — same reasoning as derefReceiver below. This
	// is also why ReadOnlySpan<char>.ToString() needs the same treatment
	// as StringBuilder below: `constrained.`+`callvirt Object::ToString`
	// again, confirmed against real IL (Fase 3.12).
	v = derefReceiver(v)
	switch v.Kind {
	case runtime.KindObject:
		if v.Obj != nil {
			if s, ok := nativeToString(v.Obj.Native); ok {
				return s
			}
		}
	case runtime.KindStruct:
		if v.Struct != nil {
			if s, ok := spanToStringValue(v.Struct); ok {
				return s
			}
		}
	}
	return v.String()
}

// nativeToString special-cases the native-backed BCL types whose ToString()
// override needs to run even when the call site resolved to the base
// System.Object::ToString (see objectToString's doc comment). Types with
// no meaningful ToString (List/Dictionary, which use the CLR's unhelpful
// default of the type name — not useful to reproduce here) fall through
// to false, matching the pre-existing behavior for anything not listed.
func nativeToString(native any) (string, bool) {
	switch n := native.(type) {
	case *nativeStringBuilder:
		return n.buf.String(), true
	default:
		return "", false
	}
}

// NativeTypeName returns the BCL full type name of a native-backed
// Object (List<T>, Dictionary<K,V>, StringBuilder, ...) — vmnet gives
// these no *runtime.Type (they're backed by a plain Go struct in Native,
// not fields), so unlike a plugin object or a synthetic value type there
// is normally nothing to ask "what is your real type" at runtime. This
// exists for exactly one caller: the interpreter's interface-call
// fallback (Fase 3.13), which redirects a call site declared against an
// interface (e.g. IEnumerable`1::GetEnumerator) to the receiver's actual
// concrete type when the interface name itself has no native registered
// — the names returned here must match the strings register() calls use
// in system_collections.go/system_stringbuilder.go exactly.
func NativeTypeName(native any) (string, bool) {
	switch native.(type) {
	case *nativeList:
		return "System.Collections.Generic.List`1", true
	case *nativeDict:
		return "System.Collections.Generic.Dictionary`2", true
	case *nativeStringBuilder:
		return "System.Text.StringBuilder", true
	default:
		return "", false
	}
}

// objectEquals/objectGetHashCode back Object::Equals/GetHashCode — the
// other pair of virtual methods (besides ToString) the `constrained.`
// prefix commonly precedes on a generic type parameter or value type
// (EqualityComparer<T>-style comparison code). A struct instance-method
// receiver arrives as a managed pointer (see fieldSlot in
// internal/interpreter/eval.go), hence the deref.
func objectEquals(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Object.Equals expects 2 arguments")
	}
	return runtime.Bool(valuesEqual(derefReceiver(args[0]), derefReceiver(args[1]))), nil
}

func objectGetHashCode(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Object.GetHashCode expects a receiver")
	}
	return runtime.Int32(valueHash(derefReceiver(args[0]))), nil
}

func derefReceiver(v runtime.Value) runtime.Value {
	if v.Kind == runtime.KindRef && v.Ref != nil {
		return *v.Ref
	}
	return v
}

// valuesEqual implements Object.Equals' default value-equality semantics:
// same bits for primitives, same content for strings, field-wise
// (recursive) equality for structs, reference identity for
// classes/arrays — matching how the CLR's default Equals behaves absent a
// type-specific override, which is the common case for generic comparison
// code (EqualityComparer<T>.Default and friends, Fase 3.8).
func valuesEqual(a, b runtime.Value) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case runtime.KindNull:
		return true
	case runtime.KindI4:
		return a.I4 == b.I4
	case runtime.KindI8:
		return a.I8 == b.I8
	case runtime.KindR4:
		return a.R4 == b.R4
	case runtime.KindR8:
		return a.R8 == b.R8
	case runtime.KindString:
		return a.Str == b.Str
	case runtime.KindObject:
		return a.Obj == b.Obj
	case runtime.KindArray:
		return a.Arr == b.Arr
	case runtime.KindStruct:
		if a.Struct == nil || b.Struct == nil {
			return a.Struct == b.Struct
		}
		if a.Struct.Type != b.Struct.Type || len(a.Struct.Fields) != len(b.Struct.Fields) {
			return false
		}
		for i := range a.Struct.Fields {
			if !valuesEqual(a.Struct.Fields[i], b.Struct.Fields[i]) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func valueHash(v runtime.Value) int32 {
	switch v.Kind {
	case runtime.KindNull:
		return 0
	case runtime.KindI4:
		return v.I4
	case runtime.KindI8:
		return int32(v.I8 ^ (v.I8 >> 32))
	case runtime.KindR4:
		return int32(math.Float32bits(v.R4))
	case runtime.KindR8:
		bits := math.Float64bits(v.R8)
		return int32(bits ^ (bits >> 32))
	case runtime.KindString:
		h := fnv.New32a()
		h.Write([]byte(v.Str))
		return int32(h.Sum32())
	case runtime.KindObject:
		return hashPointer(v.Obj)
	case runtime.KindArray:
		return hashPointer(v.Arr)
	case runtime.KindStruct:
		if v.Struct == nil {
			return 0
		}
		h := int32(17)
		for _, f := range v.Struct.Fields {
			h = h*31 + valueHash(f)
		}
		return h
	default:
		return 0
	}
}

// hashPointer gives a stable, if not identity-strong, hash for a
// reference-typed Value backed by a Go pointer, without resorting to the
// unsafe package (out of step with vmnet's pure-Go, no-tricks philosophy —
// see docs/adr/0001-pure-go-core.md) to read the pointer bits directly.
func hashPointer(p any) int32 {
	h := fnv.New32a()
	fmt.Fprintf(h, "%p", p)
	return int32(h.Sum32())
}
