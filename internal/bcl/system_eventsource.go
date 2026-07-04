package bcl

import "github.com/arturoeanton/go-vmnet/internal/runtime"

// System.Diagnostics.Tracing.EventSource is the real ETW tracing base
// class many BCL libraries subclass for diagnostics (e.g. System.Text.
// Json's own ArrayPoolEventSource, Fase 3.40) — purely observational in
// real .NET too (a disabled/unlistened EventSource has zero effect on
// program behavior), so every member here is a no-op/false, mirroring
// how a real EventSource behaves when nothing is listening.
func init() {
	registerCtor("System.Diagnostics.Tracing.EventSource", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeEventSource{}}, nil
	})
	register("System.Diagnostics.Tracing.EventSource::.ctor", false, eventSourceCtorInPlace)
	register("System.Diagnostics.Tracing.EventSource::IsEnabled", true, eventSourceFalse)
	register("System.Diagnostics.Tracing.EventSource::WriteEvent", false, eventSourceNoop)
	register("System.Diagnostics.Tracing.EventSource::Write", false, eventSourceNoop)
	register("System.Diagnostics.Tracing.EventSource::Dispose", false, eventSourceNoop)
}

type nativeEventSource struct{}

func eventSourceCtorInPlace(args []runtime.Value) (runtime.Value, error) {
	return runtime.Value{}, nil
}

func eventSourceFalse(args []runtime.Value) (runtime.Value, error) {
	return runtime.Bool(false), nil
}

func eventSourceNoop(args []runtime.Value) (runtime.Value, error) {
	return runtime.Value{}, nil
}
