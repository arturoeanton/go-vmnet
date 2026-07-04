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
	register("System.Threading.Interlocked::Exchange", true, interlockedExchange)
	register("System.Threading.Interlocked::Increment", true, interlockedIncrement)
	register("System.Threading.Interlocked::Decrement", true, interlockedDecrement)
	register("System.Threading.Interlocked::Add", true, interlockedAdd)
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

// interlockedExchange backs Exchange<T>(ref T location, T value) — sets
// *location = value and returns the ORIGINAL value (Fase 3.40, found via
// System.Text.Json's own lock-free lazy-initialization paths).
func interlockedExchange(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 || args[0].Kind != runtime.KindRef || args[0].Ref == nil {
		return runtime.Value{}, fmt.Errorf("bcl: Interlocked.Exchange expects a ref location")
	}
	loc := args[0].Ref
	original := *loc
	*loc = args[1]
	return original, nil
}

func interlockedIncrement(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 || args[0].Kind != runtime.KindRef || args[0].Ref == nil {
		return runtime.Value{}, fmt.Errorf("bcl: Interlocked.Increment expects a ref location")
	}
	loc := args[0].Ref
	if loc.Kind == runtime.KindI8 {
		*loc = runtime.Int64(loc.I8 + 1)
	} else {
		*loc = runtime.Int32(loc.I4 + 1)
	}
	return *loc, nil
}

func interlockedDecrement(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 || args[0].Kind != runtime.KindRef || args[0].Ref == nil {
		return runtime.Value{}, fmt.Errorf("bcl: Interlocked.Decrement expects a ref location")
	}
	loc := args[0].Ref
	if loc.Kind == runtime.KindI8 {
		*loc = runtime.Int64(loc.I8 - 1)
	} else {
		*loc = runtime.Int32(loc.I4 - 1)
	}
	return *loc, nil
}

func interlockedAdd(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 || args[0].Kind != runtime.KindRef || args[0].Ref == nil {
		return runtime.Value{}, fmt.Errorf("bcl: Interlocked.Add expects a ref location")
	}
	loc := args[0].Ref
	if loc.Kind == runtime.KindI8 {
		*loc = runtime.Int64(loc.I8 + args[1].I8)
	} else {
		*loc = runtime.Int32(loc.I4 + args[1].I4)
	}
	return *loc, nil
}
