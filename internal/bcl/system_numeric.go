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
