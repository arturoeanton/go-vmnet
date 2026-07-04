package bcl

import "github.com/arturoeanton/go-vmnet/internal/runtime"

// System.IntPtr has no distinct representation in vmnet — it's always
// just a plain Int64 Value (see Unsafe.ByteOffset's own doc comment:
// there's no real memory model to give IntPtr actual pointer semantics
// against). op_Explicit backs both real conversion directions
// (`(IntPtr)intValue` and `(int)intPtrValue`) under the same
// fully-qualified name (real .NET declares both on IntPtr itself) —
// dispatched by the argument's own Kind, the same convention every other
// multi-shape native in this package follows, since vmnet's native
// registry has no arity/type-based overload resolution.
// intPtrStaticsType backs IntPtr.Zero (`ldsfld System.IntPtr::Zero`) — a
// real static readonly field, not a property, so it needs the same
// static-field-host registration Path's DirectorySeparatorChar uses
// (system_io_path.go), not a method registration.
var intPtrStaticsType = runtime.NewType("System", "IntPtr", nil, []string{"Zero"}, nil, []runtime.Value{runtime.Int64(0)})

func init() {
	registerStaticFieldHost(intPtrStaticsType)
	register("System.IntPtr::op_Explicit", true, intPtrOpExplicit)
	register("System.IntPtr::op_Implicit", true, intPtrOpExplicit)
	register("System.IntPtr::ToInt32", true, intPtrToInt32)
	register("System.IntPtr::ToInt64", true, intPtrToInt64)
	register("System.IntPtr::op_Addition", true, intPtrOpAddition)
	register("System.IntPtr::op_Subtraction", true, intPtrOpSubtraction)
	register("System.IntPtr::op_Equality", true, intPtrOpEquality)
	register("System.IntPtr::op_Inequality", true, intPtrOpInequality)
	register("System.IntPtr::Equals", true, intPtrOpEquality)
}

func intPtrOpAddition(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, nil
	}
	a, _ := valueAsInt64(args[0])
	b, _ := valueAsInt64(args[1])
	return runtime.Int64(a + b), nil
}

func intPtrOpSubtraction(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, nil
	}
	a, _ := valueAsInt64(args[0])
	b, _ := valueAsInt64(args[1])
	return runtime.Int64(a - b), nil
}

func intPtrOpEquality(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Bool(false), nil
	}
	a, _ := valueAsInt64(args[0])
	b, _ := valueAsInt64(args[1])
	return runtime.Bool(a == b), nil
}

func intPtrOpInequality(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Bool(true), nil
	}
	a, _ := valueAsInt64(args[0])
	b, _ := valueAsInt64(args[1])
	return runtime.Bool(a != b), nil
}

func intPtrOpExplicit(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 {
		return runtime.Int64(0), nil
	}
	switch args[0].Kind {
	case runtime.KindI4:
		return runtime.Int64(int64(args[0].I4)), nil
	case runtime.KindI8:
		return runtime.Int32(int32(args[0].I8)), nil
	default:
		return args[0], nil
	}
}

func intPtrToInt32(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 {
		return runtime.Int32(0), nil
	}
	v, _ := valueAsInt64(args[0])
	return runtime.Int32(int32(v)), nil
}

func intPtrToInt64(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 {
		return runtime.Int64(0), nil
	}
	v, _ := valueAsInt64(args[0])
	return runtime.Int64(v), nil
}
