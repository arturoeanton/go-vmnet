package bcl

import "github.com/arturoeanton/go-vmnet/internal/runtime"

func init() {
	// Every class the C# compiler emits chains up to Object::.ctor(), even
	// when the source has no explicit base-class call.
	register("System.Object::.ctor", false, objectCtorNoop)
	register("System.Object::ToString", true, objectToString)
}

func objectCtorNoop(args []runtime.Value) (runtime.Value, error) {
	return runtime.Value{}, nil
}

// objectToString backs a boxed value's virtual ToString() call: since
// box is a no-op in vmnet's Value model (see internal/ir/builder.go), the
// callvirt still carries the real Kind, so this can dispatch on it exactly
// like the CLR would dispatch on the boxed value's runtime type.
func objectToString(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 {
		return runtime.String("null"), nil
	}
	return runtime.String(displayString(args[0])), nil
}

func displayString(v runtime.Value) string {
	return v.String()
}
