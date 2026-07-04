package bcl

import "github.com/arturoeanton/go-vmnet/internal/runtime"

// System.GC is meaningless in vmnet — there's no finalizer queue and no
// generational heap to manage — but real code (any IDisposable.Dispose()
// implementation following the standard dispose pattern, e.g.
// System.IO.Packaging.StreamPackageFeature.Dispose) calls
// GC.SuppressFinalize(this) unconditionally, so it needs a native no-op
// rather than an "unsupported BCL method" error.
func init() {
	register("System.GC::SuppressFinalize", false, gcNoop)
	register("System.GC::Collect", false, gcNoop)
	register("System.GC::WaitForPendingFinalizers", false, gcNoop)
	register("System.GC::ReRegisterForFinalize", false, gcNoop)
}

func gcNoop(args []runtime.Value) (runtime.Value, error) {
	return runtime.Value{}, nil
}
