package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// interlockedCompareExchange covers the int32/int64/object overloads:
// the location argument (`ref` in C#) always arrives as a managed
// pointer (KindRef), same mechanism as any other `ref`/`out` parameter
// since Fase 3.5. vmnet has no real multi-core memory model to make this
// atomic against (a Machine never runs two goroutines inside the same
// call chain concurrently), but the compare-and-swap semantics — the
// part real code actually depends on for correctness, not just raw
// atomicity — are implemented exactly, not stubbed.
func init() {
	register("System.Threading.Interlocked::CompareExchange", true, interlockedCompareExchange)
}

func interlockedCompareExchange(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 3 || args[0].Kind != runtime.KindRef || args[0].Ref == nil {
		return runtime.Value{}, fmt.Errorf("bcl: Interlocked.CompareExchange expects a ref location")
	}
	loc := args[0].Ref
	original := *loc
	if valuesEqual(*loc, args[2]) {
		*loc = args[1]
	}
	return original, nil
}
