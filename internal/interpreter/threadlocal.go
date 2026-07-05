package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

func init() {
	machineRegistry["System.Threading.ThreadLocal`1::get_Value"] = threadLocalGetValue
}

// threadLocalGetValue implements ThreadLocal<T>.Value — mirrors
// lazyGetValue (lazy.go) exactly: invoking the optional valueFactory
// delegate needs Machine access (m.invokeFunc), unavailable to a plain
// bcl.Native. A ThreadLocal<T> constructed via the parameterless overload
// has no factory at all (bcl.ValueBoxFactory returns a zero, non-KindFunc
// Value) — answers default(T) (Null()) the first time, exactly like
// AsyncLocal<T>'s own plain get_Value already does, rather than erroring.
func threadLocalGetValue(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 1 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: ThreadLocal<T>.Value called without a receiver")
	}
	factory, ok := bcl.ValueBoxFactory(args[0].Obj.Native)
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: receiver is not a ThreadLocal<T>")
	}
	return bcl.ValueBoxGetOrCompute(args[0].Obj.Native, func() (runtime.Value, error) {
		if factory.Kind != runtime.KindFunc || factory.Func == nil {
			return runtime.Null(), nil
		}
		v, _, err := m.invokeFunc(factory.Func, nil, depth, instrCount)
		return v, err
	})
}
