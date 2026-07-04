package bcl

import "github.com/arturoeanton/go-vmnet/internal/runtime"

// System.Threading.Volatile.Read/Write<T>(ref T location) are real JIT
// memory-fence intrinsics — meaningless for a single-goroutine-per-call
// interpreter with Value-based storage instead of raw memory (same
// reasoning volatile./readonly. IL prefixes already document, ir/
// builder.go), so both are a plain read/write through the ref — needed
// since Fase 3.40: System.Text.Json's own JsonDocument.Parse path uses
// Volatile.Read for lock-free lazy-initialization of shared metadata.
func init() {
	register("System.Threading.Volatile::Read", true, volatileRead)
	register("System.Threading.Volatile::Write", false, volatileWrite)
}

func volatileRead(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindRef || args[0].Ref == nil {
		return runtime.Null(), nil
	}
	return *args[0].Ref, nil
}

func volatileWrite(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[0].Kind != runtime.KindRef || args[0].Ref == nil {
		return runtime.Value{}, nil
	}
	*args[0].Ref = args[1]
	return runtime.Value{}, nil
}
