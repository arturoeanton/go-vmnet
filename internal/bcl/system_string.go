package bcl

import (
	"fmt"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

func init() {
	// All String.Concat overloads (2/3/4-arg, params object[]) collapse to
	// this one native: it just concatenates whatever string args arrive.
	register("System.String::Concat", true, stringConcat)
	register("System.String::get_Length", true, stringLength)
}

// stringConcat backs every String.Concat overload, including the
// object-typed ones the compiler picks for `"literal" + nonStringExpr`
// (values arrive boxed — a no-op in vmnet, see internal/ir/builder.go —
// so non-string args are formatted the same way Object.ToString() would).
func stringConcat(args []runtime.Value) (runtime.Value, error) {
	var sb strings.Builder
	for _, a := range args {
		if a.Kind == runtime.KindString {
			sb.WriteString(a.Str)
		} else {
			sb.WriteString(displayString(a))
		}
	}
	return runtime.String(sb.String()), nil
}

func stringLength(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.get_Length expects a string receiver")
	}
	return runtime.Int32(int32(len([]rune(args[0].Str)))), nil
}
