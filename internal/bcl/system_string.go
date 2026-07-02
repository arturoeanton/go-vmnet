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

func stringConcat(args []runtime.Value) (runtime.Value, error) {
	var sb strings.Builder
	for i, a := range args {
		if a.Kind != runtime.KindString {
			return runtime.Value{}, fmt.Errorf("bcl: System.String.Concat: argument %d is not a string", i)
		}
		sb.WriteString(a.Str)
	}
	return runtime.String(sb.String()), nil
}

func stringLength(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.get_Length expects a string receiver")
	}
	return runtime.Int32(int32(len([]rune(args[0].Str)))), nil
}
