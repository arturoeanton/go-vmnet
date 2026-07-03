package bcl

import (
	"fmt"
	"strconv"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// Small, self-contained natives that don't warrant their own file: a
// culture stub (vmnet has no locale-aware formatting, so InvariantCulture
// is just a sentinel object other natives ignore) and a thread-id stub
// (vmnet runs interpreted code on whatever Go goroutine called it — there
// is no managed thread pool to report a real ID from).
func init() {
	register("System.Globalization.CultureInfo::get_InvariantCulture", true, cultureInfoInvariant)
	register("System.Environment::get_CurrentManagedThreadId", true, environmentThreadID)
	// "\n", not "\r\n" — vmnet has no concept of a host OS to match
	// Environment.NewLine's real platform-dependent value against; "\n"
	// is the more common expectation for embedded/server code (and what
	// every other vmnet-produced string, e.g. StringBuilder.AppendLine,
	// already uses).
	register("System.Environment::get_NewLine", true, environmentNewLine)
	register("System.Convert::ToInt32", true, convertToInt32)
	register("System.Double::ToString", true, doubleToString)
}

func cultureInfoInvariant(args []runtime.Value) (runtime.Value, error) {
	return runtime.ObjRef(&runtime.Object{}), nil
}

func environmentThreadID(args []runtime.Value) (runtime.Value, error) {
	return runtime.Int32(1), nil
}

func environmentNewLine(args []runtime.Value) (runtime.Value, error) {
	return runtime.String("\n"), nil
}

// convertToInt32 covers the string/double/int64/bool/object-typed
// overloads by inspecting the actual argument Kind, same approach as
// every other multi-overload native in this package — a IFormatProvider
// trailing argument (culture) is accepted and ignored, same limitation
// documented elsewhere (no culture support anywhere in vmnet).
func convertToInt32(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Convert.ToInt32 expects an argument")
	}
	switch v := args[0]; v.Kind {
	case runtime.KindI4:
		return v, nil
	case runtime.KindI8:
		return runtime.Int32(int32(v.I8)), nil
	case runtime.KindR4:
		return runtime.Int32(int32(v.R4 + sign(v.R4)*0.5)), nil
	case runtime.KindR8:
		return runtime.Int32(int32(v.R8 + sign(v.R8)*0.5)), nil
	case runtime.KindString:
		n, err := strconv.ParseInt(v.Str, 10, 32)
		if err != nil {
			return runtime.Value{}, &runtime.ManagedException{TypeName: "System.FormatException", Message: fmt.Sprintf("Input string %q was not in a correct format.", v.Str)}
		}
		return runtime.Int32(int32(n)), nil
	case runtime.KindNull:
		return runtime.Int32(0), nil
	default:
		return runtime.Value{}, fmt.Errorf("bcl: System.Convert.ToInt32: unsupported argument kind")
	}
}

// sign implements .NET's ToInt32(double)/(float) round-half-away-from-zero.
func sign[T float32 | float64](f T) T {
	if f < 0 {
		return -1
	}
	return 1
}

func doubleToString(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Double.ToString expects a receiver")
	}
	v := args[0]
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	if v.Kind != runtime.KindR8 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Double.ToString expects a double receiver")
	}
	return runtime.String(strconv.FormatFloat(v.R8, 'G', -1, 64)), nil
}
