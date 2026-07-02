package bcl

import "github.com/arturoeanton/go-vmnet/internal/runtime"

func init() {
	// Every class the C# compiler emits chains up to Object::.ctor(), even
	// when the source has no explicit base-class call.
	register("System.Object::.ctor", false, objectCtorNoop)
	register("System.Object::ToString", true, objectToString)
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
	if v.Kind == runtime.KindObject && v.Obj != nil {
		if s, ok := nativeToString(v.Obj.Native); ok {
			return s
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
