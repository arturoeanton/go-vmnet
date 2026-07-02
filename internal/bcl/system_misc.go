package bcl

import "github.com/arturoeanton/go-vmnet/internal/runtime"

// Small, self-contained natives that don't warrant their own file: a
// culture stub (vmnet has no locale-aware formatting, so InvariantCulture
// is just a sentinel object other natives ignore) and a thread-id stub
// (vmnet runs interpreted code on whatever Go goroutine called it — there
// is no managed thread pool to report a real ID from).
func init() {
	register("System.Globalization.CultureInfo::get_InvariantCulture", true, cultureInfoInvariant)
	register("System.Environment::get_CurrentManagedThreadId", true, environmentThreadID)
}

func cultureInfoInvariant(args []runtime.Value) (runtime.Value, error) {
	return runtime.ObjRef(&runtime.Object{}), nil
}

func environmentThreadID(args []runtime.Value) (runtime.Value, error) {
	return runtime.Int32(1), nil
}
