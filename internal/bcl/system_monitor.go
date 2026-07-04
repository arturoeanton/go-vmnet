package bcl

import "github.com/arturoeanton/go-vmnet/internal/runtime"

// System.Threading.Monitor backs the `lock (obj) { ... }` statement
// (Monitor.Enter/Exit, or the modern Enter(object, ref bool lockTaken)
// overload the compiler emits when the locked block might throw before
// entering). vmnet never runs two goroutines inside the same call chain
// concurrently (a Machine invocation is always sequential C# execution,
// same rationale System.Threading.Interlocked already documents), so
// there's really nothing to lock against — every Enter trivially
// "succeeds" and Exit is a no-op. Found via a real case: NPOI's own I/O
// helper classes guard a lock object around buffer access.
func init() {
	register("System.Threading.Monitor::Enter", false, monitorEnter)
	register("System.Threading.Monitor::Exit", false, monitorNoop)
	register("System.Threading.Monitor::TryEnter", true, monitorTryEnter)
}

func monitorEnter(args []runtime.Value) (runtime.Value, error) {
	// Enter(object) is 1 real arg (2 with receiver-less static dispatch
	// counted, but Monitor's own methods are static so args has no
	// receiver); Enter(object, ref bool lockTaken) additionally sets the
	// out parameter to true, matching real semantics once the lock is
	// (trivially) acquired.
	if len(args) >= 2 && args[1].Kind == runtime.KindRef && args[1].Ref != nil {
		*args[1].Ref = runtime.Bool(true)
	}
	return runtime.Value{}, nil
}

func monitorTryEnter(args []runtime.Value) (runtime.Value, error) {
	return runtime.Bool(true), nil
}

func monitorNoop(args []runtime.Value) (runtime.Value, error) {
	return runtime.Value{}, nil
}
