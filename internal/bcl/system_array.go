package bcl

import "github.com/arturoeanton/go-vmnet/internal/runtime"

func init() {
	register("System.Array::Empty", true, arrayEmpty)
}

// arrayEmpty backs the generic Array.Empty<T>() helper: always a
// zero-length SZARRAY regardless of T, since runtime.Array doesn't carry
// an element type (see internal/runtime/array.go).
func arrayEmpty(args []runtime.Value) (runtime.Value, error) {
	return runtime.ArrRef(runtime.NewArray(0)), nil
}
