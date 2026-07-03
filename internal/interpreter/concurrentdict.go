package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

func init() {
	machineRegistry["System.Collections.Concurrent.ConcurrentDictionary`2::GetOrAdd"] = concurrentDictGetOrAdd
}

// concurrentDictGetOrAdd implements both real GetOrAdd overloads —
// (key, TValue value) and (key, Func<TKey,TValue> factory) — behind one
// call target: vmnet's call dispatch doesn't distinguish overloads by
// signature, so the third argument's own Kind tells them apart the same
// way every other multi-overload native in this project does (e.g.
// resolveRegexAndInput, internal/bcl/system_regex.go). Invoking the
// factory needs Machine access, unavailable to a plain bcl.Native — same
// reason Lazy<T>.Value (lazy.go, Fase 3.17) needed this registry.
func concurrentDictGetOrAdd(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 3 {
		return runtime.Value{}, fmt.Errorf("interpreter: ConcurrentDictionary.GetOrAdd expects a key and a value/factory")
	}
	recv, key, third := args[0], args[1], args[2]
	if third.Kind == runtime.KindFunc {
		return bcl.ConcurrentDictGetOrAdd(recv, key, func() (runtime.Value, error) {
			v, _, err := m.invokeFunc(third.Func, []runtime.Value{key}, depth, instrCount)
			return v, err
		})
	}
	return bcl.ConcurrentDictGetOrAdd(recv, key, func() (runtime.Value, error) {
		return third, nil
	})
}
