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
	register("System.Convert::ToInt64", true, convertToInt64)
	register("System.Double::ToString", true, doubleToString)
	register("System.Double::TryParse", true, doubleTryParse)
	register("System.Double::Equals", true, doubleEquals)
	register("System.Globalization.CultureInfo::get_CurrentCulture", true, cultureInfoInvariant)
	register("System.Globalization.CultureInfo::get_Name", true, cultureInfoName)
	// TimeZoneInfo.Local/Utc: vmnet has no real timezone database (same
	// "no locale/culture data anywhere" limitation as CultureInfo above)
	// — both return the same stub object, offset-less, matching the
	// existing UTC-identity treatment DateTime.ToUniversalTime/
	// ToLocalTime already has (Fase 3.23).
	register("System.TimeZoneInfo::get_Local", true, cultureInfoInvariant)
	register("System.TimeZoneInfo::get_Utc", true, cultureInfoInvariant)
	register("System.Double::CompareTo", true, doubleCompareTo)
	register("System.Double::Parse", true, doubleParse)
	register("System.Boolean::ToString", true, boolToString)
	register("System.Boolean::CompareTo", true, boolCompareTo)
	register("System.Boolean::GetHashCode", true, boolGetHashCode)
	register("System.Convert::ToString", true, convertToString)
	register("System.Char::ConvertFromUtf32", true, charConvertFromUtf32)
}

func doubleCompareTo(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Double.CompareTo expects a receiver and an argument")
	}
	a, b := args[0], args[1]
	if a.Kind == runtime.KindRef && a.Ref != nil {
		a = *a.Ref
	}
	if a.Kind != runtime.KindR8 || b.Kind != runtime.KindR8 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Double.CompareTo expects double operands")
	}
	switch {
	case a.R8 < b.R8:
		return runtime.Int32(-1), nil
	case a.R8 > b.R8:
		return runtime.Int32(1), nil
	default:
		return runtime.Int32(0), nil
	}
}

func doubleParse(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.Double.Parse expects a string argument")
	}
	f, err := strconv.ParseFloat(args[0].Str, 64)
	if err != nil {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.FormatException", Message: fmt.Sprintf("Input string %q was not in a correct format.", args[0].Str)}
	}
	return runtime.Float64(f), nil
}

// boolToString capitalizes "True"/"False" — real Boolean.ToString's
// exact casing, unlike Go's lowercase strconv.FormatBool.
func boolToString(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Boolean.ToString expects a receiver")
	}
	v := args[0]
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	if v.I4 != 0 {
		return runtime.String("True"), nil
	}
	return runtime.String("False"), nil
}

func boolCompareTo(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Boolean.CompareTo expects a receiver and an argument")
	}
	a, b := args[0], args[1]
	if a.Kind == runtime.KindRef && a.Ref != nil {
		a = *a.Ref
	}
	switch {
	case a.I4 == b.I4:
		return runtime.Int32(0), nil
	case a.I4 == 0:
		return runtime.Int32(-1), nil
	default:
		return runtime.Int32(1), nil
	}
}

func boolGetHashCode(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Boolean.GetHashCode expects a receiver")
	}
	v := args[0]
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	if v.I4 != 0 {
		return runtime.Int32(1), nil
	}
	return runtime.Int32(0), nil
}

// convertToString covers Convert.ToString(object)/(int)/(double)/... by
// reusing displayString, the same formatting every other implicit
// ToString/boxed-argument path in this package already shares — a
// trailing IFormatProvider argument (culture) is accepted and ignored.
func convertToString(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.String(""), nil
	}
	if args[0].Kind == runtime.KindNull {
		return runtime.String(""), nil
	}
	return runtime.String(displayString(args[0])), nil
}

// charConvertFromUtf32 converts a Unicode code point to its string
// representation — Go's string(rune) already produces the correct UTF-8
// encoding for the full code point range, including values beyond
// U+FFFF that real .NET represents as a UTF-16 surrogate pair
// internally (an implementation detail invisible at this API boundary).
func charConvertFromUtf32(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Char.ConvertFromUtf32 expects an int argument")
	}
	return runtime.String(string(rune(args[0].I4))), nil
}

func cultureInfoName(args []runtime.Value) (runtime.Value, error) {
	return runtime.String(""), nil
}

func convertToInt64(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Convert.ToInt64 expects an argument")
	}
	switch v := args[0]; v.Kind {
	case runtime.KindI4:
		return runtime.Int64(int64(v.I4)), nil
	case runtime.KindI8:
		return v, nil
	case runtime.KindR4:
		return runtime.Int64(int64(v.R4 + sign(v.R4)*0.5)), nil
	case runtime.KindR8:
		return runtime.Int64(int64(v.R8 + sign(v.R8)*0.5)), nil
	case runtime.KindString:
		n, err := strconv.ParseInt(v.Str, 10, 64)
		if err != nil {
			return runtime.Value{}, &runtime.ManagedException{TypeName: "System.FormatException", Message: fmt.Sprintf("Input string %q was not in a correct format.", v.Str)}
		}
		return runtime.Int64(n), nil
	case runtime.KindNull:
		return runtime.Int64(0), nil
	default:
		return runtime.Value{}, fmt.Errorf("bcl: System.Convert.ToInt64: unsupported argument kind")
	}
}

// doubleTryParse's `out double result` uses the same managed-pointer
// mechanism as any other `ref`/`out` primitive since Fase 3.5.
func doubleTryParse(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[0].Kind != runtime.KindString || args[1].Kind != runtime.KindRef || args[1].Ref == nil {
		return runtime.Value{}, fmt.Errorf("bcl: System.Double.TryParse expects (string, out double)")
	}
	f, err := strconv.ParseFloat(args[0].Str, 64)
	if err != nil {
		*args[1].Ref = runtime.Float64(0)
		return runtime.Bool(false), nil
	}
	*args[1].Ref = runtime.Float64(f)
	return runtime.Bool(true), nil
}

func doubleEquals(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Double.Equals expects a receiver and an argument")
	}
	a, b := args[0], args[1]
	if a.Kind == runtime.KindRef && a.Ref != nil {
		a = *a.Ref
	}
	if a.Kind != runtime.KindR8 || b.Kind != runtime.KindR8 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Double.Equals expects double operands")
	}
	return runtime.Bool(a.R8 == b.R8), nil
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
