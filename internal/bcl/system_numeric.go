package bcl

import (
	"fmt"
	"strconv"

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
// mechanism as any other `ref`/`out` primitive since Fase 3.5.
func int32TryParse(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[0].Kind != runtime.KindString || args[1].Kind != runtime.KindRef || args[1].Ref == nil {
		return runtime.Value{}, fmt.Errorf("bcl: System.Int32.TryParse expects (string, out int)")
	}
	n, err := strconv.ParseInt(args[0].Str, 10, 32)
	if err != nil {
		*args[1].Ref = runtime.Int32(0)
		return runtime.Bool(false), nil
	}
	*args[1].Ref = runtime.Int32(int32(n))
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
