package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// System.Linq.Enumerable.Range (Fase 3.43, found reading a real .xlsx
// through ClosedXML 0.105.0's `new XLWorkbook(stream)` — reached from
// ClosedXML's own worksheet-loading code once real cell data started
// parsing). Registered machineRegistry-style alongside every other
// linq.go native purely for consistency of dispatch; the body itself
// needs no Machine at all. Real Enumerable.Range semantics: count < 0 or
// start+count-1 overflowing int32 throws ArgumentOutOfRangeException.
func init() {
	machineRegistry["System.Linq.Enumerable::Range"] = linqRange
}

func linqRange(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 || args[0].Kind != runtime.KindI4 || args[1].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.Range expects (int start, int count)")
	}
	start, count := int64(args[0].I4), int64(args[1].I4)
	if count < 0 || start+count-1 > 2147483647 {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "count"}
	}
	out := make([]runtime.Value, count)
	for i := range out {
		out[i] = runtime.Int32(int32(start + int64(i)))
	}
	return bcl.NewListValue(out), nil
}
