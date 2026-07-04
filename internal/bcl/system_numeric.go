package bcl

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// int32ToString ignores a format-string/IFormatProvider argument
// (culture-invariant decimal only — no culture support anywhere else
// either, see CultureInfo's stub since Fase 3.6): real Int32.ToString(D)/
// ToString("X") formatting would need the same specifier parser
// System.String.Format already has, not duplicated here for a single
// BCL type until real usage demands it.
func init() {
	register("System.Int32::ToString", true, int32ToString)
	register("System.Int32::Parse", true, int32Parse)
	register("System.Int32::TryParse", true, int32TryParse)
	register("System.Int32::CompareTo", true, int32CompareTo)
	register("System.Int64::ToString", true, int64ToString)
	register("System.Int64::Parse", true, int64Parse)
	register("System.Int64::TryParse", true, int64TryParse)
	register("System.Int32::GetHashCode", true, int32GetHashCode)
	// Int16/Byte are stored the same way Int32 is (a plain KindI4 on the
	// CIL stack — every integral type narrower than int32 widens to it,
	// spec §III.1.1), so ToString/GetHashCode reuse Int32's natives
	// directly rather than duplicating them.
	register("System.Int16::ToString", true, int32ToString)
	register("System.Int16::GetHashCode", true, int32GetHashCode)
	register("System.Byte::ToString", true, int32ToString)
	register("System.Byte::GetHashCode", true, int32GetHashCode)
}

// int32GetHashCode returns the value itself, matching real
// Int32.GetHashCode's documented identity behavior.
func int32GetHashCode(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: Int32.GetHashCode expects a receiver")
	}
	v := args[0]
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	if v.Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: Int32.GetHashCode expects an int32 receiver")
	}
	return runtime.Int32(v.I4), nil
}

func int64Parse(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.Int64.Parse expects a string argument")
	}
	n, err := strconv.ParseInt(args[0].Str, 10, 64)
	if err != nil {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.FormatException", Message: fmt.Sprintf("Input string %q was not in a correct format.", args[0].Str)}
	}
	return runtime.Int64(n), nil
}

// int64TryParse's `out long result` arrives as a managed pointer, same
// mechanism as Int32.TryParse — but unlike Int32.TryParse, real code
// calling Int64.TryParse commonly uses the ReadOnlySpan<char> overload
// (`TryParse(ReadOnlySpan<char>, out long)`, found running real Jint/
// Esprima number-parsing code) or the NumberStyles/IFormatProvider
// overload, not just the plain (string, out long) shape — parseTextArg/
// lastOutRef handle any of these uniformly rather than hard-requiring
// exactly 2 arguments.
func int64TryParse(args []runtime.Value) (runtime.Value, error) {
	text, ok := parseTextArg(args)
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: System.Int64.TryParse: unsupported argument shape")
	}
	outRef := lastOutRef(args)
	if outRef == nil {
		return runtime.Value{}, fmt.Errorf("bcl: System.Int64.TryParse: no out parameter found")
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		*outRef = runtime.Int64(0)
		return runtime.Bool(false), nil
	}
	*outRef = runtime.Int64(n)
	return runtime.Bool(true), nil
}

// parseTextArg reads the "what to parse" argument any Xxx.Parse/TryParse
// native receives as its first real argument — either a plain string or
// a ReadOnlySpan<char> (real modern code commonly prefers the latter to
// avoid allocating a substring first).
func parseTextArg(args []runtime.Value) (string, bool) {
	if len(args) == 0 {
		return "", false
	}
	if args[0].Kind == runtime.KindString {
		return args[0].Str, true
	}
	if args[0].Kind == runtime.KindStruct && args[0].Struct != nil {
		return spanToStringValue(args[0].Struct)
	}
	return "", false
}

// lastOutRef finds a TryParse-shaped native's `out` parameter: real
// TryParse overloads always declare it last, regardless of how many
// NumberStyles/IFormatProvider arguments come before it.
func lastOutRef(args []runtime.Value) *runtime.Value {
	for i := len(args) - 1; i >= 0; i-- {
		if args[i].Kind == runtime.KindRef && args[i].Ref != nil {
			return args[i].Ref
		}
	}
	return nil
}

func int64ToString(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Int64.ToString expects an int64 receiver")
	}
	v := args[0]
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	if v.Kind != runtime.KindI8 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Int64.ToString expects an int64 receiver")
	}
	return runtime.String(strconv.FormatInt(v.I8, 10)), nil
}

func int32Parse(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.Int32.Parse expects a string argument")
	}
	n, err := strconv.ParseInt(args[0].Str, 10, 32)
	if err != nil {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.FormatException", Message: fmt.Sprintf("Input string %q was not in a correct format.", args[0].Str)}
	}
	return runtime.Int32(int32(n)), nil
}

// int32TryParse's `out int result` arrives as a managed pointer, same
// mechanism as any other `ref`/`out` primitive since Fase 3.5. Both real
// overload shapes are covered (Fase 3.43, the second found reading a real
// .xlsx through ClosedXML 0.105.0's own worksheet reader): the plain
// `TryParse(string, out int)` and the culture-explicit `TryParse(string,
// NumberStyles, IFormatProvider, out int)` — the out parameter is always
// the LAST argument, the string always the first, and the styles/provider
// pair contributes nothing for this loop's target packages' real inputs
// (always NumberStyles.Integer-shaped decimal digits with
// CultureInfo.InvariantCulture, which is exactly what base-10 ParseInt
// already is; a styles flag requesting hex/thousands-separators would be
// a real semantic difference, but no caller here passes one).
func int32TryParse(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.Int32.TryParse expects (string, out int)")
	}
	out := args[len(args)-1]
	if out.Kind != runtime.KindRef || out.Ref == nil {
		return runtime.Value{}, fmt.Errorf("bcl: System.Int32.TryParse expects an out parameter")
	}
	n, err := strconv.ParseInt(strings.TrimSpace(args[0].Str), 10, 32)
	if err != nil {
		*out.Ref = runtime.Int32(0)
		return runtime.Bool(false), nil
	}
	*out.Ref = runtime.Int32(int32(n))
	return runtime.Bool(true), nil
}

func int32CompareTo(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Int32.CompareTo expects a receiver and an argument")
	}
	a, b := args[0], args[1]
	if a.Kind == runtime.KindRef && a.Ref != nil {
		a = *a.Ref
	}
	if a.Kind != runtime.KindI4 || b.Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Int32.CompareTo expects int32 operands")
	}
	switch {
	case a.I4 < b.I4:
		return runtime.Int32(-1), nil
	case a.I4 > b.I4:
		return runtime.Int32(1), nil
	default:
		return runtime.Int32(0), nil
	}
}

// int32ToString's receiver may arrive as a managed pointer (KindRef)
// rather than the raw int32: `n.ToString()` on a boxed/generic-context
// value compiles through `constrained.`+callvirt, the same by-pointer
// receiver shape every other value type's instance methods use (Fase
// 3.7 onward).
func int32ToString(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Int32.ToString expects an int32 receiver")
	}
	v := args[0]
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	if v.Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Int32.ToString expects an int32 receiver")
	}
	return runtime.String(strconv.FormatInt(int64(v.I4), 10)), nil
}
