package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

func init() {
	machineRegistry["System.Lazy`1::get_Value"] = lazyGetValue
}

// lazyGetValue implements Lazy<T>.Value: invoking the factory delegate
// needs Machine access (m.invokeFunc), unavailable to a plain bcl.Native
// — same reason LINQ (Fase 3.15) and Type::IsAssignableFrom (Fase 3.16)
// needed the Machine-aware registry. The actual once-only/concurrency
// guarantee lives in bcl.LazyGetOrCompute (holds the instance's own lock
// across this whole call), not here.
func lazyGetValue(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 1 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: Lazy<T>.Value called without a receiver")
	}
	factory, ok := bcl.LazyFactory(args[0].Obj.Native)
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: receiver is not a Lazy<T>")
	}
	return bcl.LazyGetOrCompute(args[0].Obj.Native, func() (runtime.Value, error) {
		if factory.Kind != runtime.KindFunc || factory.Func == nil {
			return runtime.Value{}, fmt.Errorf("interpreter: Lazy<T> has no factory delegate (unsupported constructor overload)")
		}
		v, _, err := m.invokeFunc(factory.Func, nil, depth, instrCount)
		return v, err
	})
}
