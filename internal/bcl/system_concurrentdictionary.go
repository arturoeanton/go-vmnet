package bcl

import (
	"fmt"
	"sync"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// nativeConcurrentDict backs System.Collections.Concurrent.ConcurrentDictionary
// `2. Same string-key-only limitation as nativeDict (system_collections.go,
// Fase 2) — plus a real mutex, since a host application can legitimately
// share one ConcurrentDictionary across multiple goroutines even though a
// single vmnet Machine only ever runs on one.
type nativeConcurrentDict struct {
	mu sync.Mutex
	m  map[string]runtime.Value
}

func init() {
	registerCtor("System.Collections.Concurrent.ConcurrentDictionary`2", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeConcurrentDict{m: map[string]runtime.Value{}}}, nil
	})
	// GetOrAdd is NOT registered here: its factory-delegate overload needs
	// to invoke a Func argument, unavailable to a plain bcl.Native — it's
	// resolved through the Machine-aware registry instead (Fase 3.24,
	// internal/interpreter/concurrentdict.go), mirroring System.Lazy`1's
	// get_Value (internal/interpreter/lazy.go, Fase 3.17).
	register("System.Collections.Concurrent.ConcurrentDictionary`2::TryAdd", true, concurrentDictTryAdd)
	register("System.Collections.Concurrent.ConcurrentDictionary`2::TryGetValue", true, concurrentDictTryGetValue)
	register("System.Collections.Concurrent.ConcurrentDictionary`2::TryRemove", true, concurrentDictTryRemove)
	register("System.Collections.Concurrent.ConcurrentDictionary`2::ContainsKey", true, concurrentDictContainsKey)
	register("System.Collections.Concurrent.ConcurrentDictionary`2::get_Item", true, concurrentDictGetItem)
	register("System.Collections.Concurrent.ConcurrentDictionary`2::set_Item", false, concurrentDictSetItem)
	register("System.Collections.Concurrent.ConcurrentDictionary`2::get_Count", true, concurrentDictCount)
}

func asConcurrentDict(args []runtime.Value) (*nativeConcurrentDict, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return nil, fmt.Errorf("bcl: ConcurrentDictionary method called without a receiver")
	}
	d, ok := args[0].Obj.Native.(*nativeConcurrentDict)
	if !ok {
		return nil, fmt.Errorf("bcl: receiver is not a ConcurrentDictionary")
	}
	return d, nil
}

func concurrentDictKey(v runtime.Value) (string, error) {
	if v.Kind != runtime.KindString {
		return "", fmt.Errorf("bcl: ConcurrentDictionary only supports string keys")
	}
	return v.Str, nil
}

// ConcurrentDictGetOrAdd looks up key, computing and storing it via compute
// while holding the dictionary's own lock for the whole operation if it's
// missing — mirrors LazyGetOrCompute's single-lock-for-the-whole-compute
// approach (system_lazy.go, Fase 3.17) so two concurrent misses on the same
// key can't both run the factory. Exported for the Machine-aware GetOrAdd
// native (internal/interpreter/concurrentdict.go), which supplies compute
// as either "return the literal value" or "invoke the factory delegate"
// depending on the real overload called.
func ConcurrentDictGetOrAdd(recv runtime.Value, key runtime.Value, compute func() (runtime.Value, error)) (runtime.Value, error) {
	d, err := asConcurrentDict([]runtime.Value{recv})
	if err != nil {
		return runtime.Value{}, err
	}
	k, err := concurrentDictKey(key)
	if err != nil {
		return runtime.Value{}, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if v, ok := d.m[k]; ok {
		return v, nil
	}
	v, err := compute()
	if err != nil {
		return runtime.Value{}, err
	}
	d.m[k] = v
	return v, nil
}

func concurrentDictTryAdd(args []runtime.Value) (runtime.Value, error) {
	d, err := asConcurrentDict(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 3 {
		return runtime.Value{}, fmt.Errorf("bcl: ConcurrentDictionary.TryAdd expects a key and value")
	}
	key, err := concurrentDictKey(args[1])
	if err != nil {
		return runtime.Value{}, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.m[key]; exists {
		return runtime.Bool(false), nil
	}
	d.m[key] = args[2]
	return runtime.Bool(true), nil
}

func concurrentDictTryGetValue(args []runtime.Value) (runtime.Value, error) {
	d, err := asConcurrentDict(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 3 {
		return runtime.Value{}, fmt.Errorf("bcl: ConcurrentDictionary.TryGetValue expects a key and an out param")
	}
	key, err := concurrentDictKey(args[1])
	if err != nil {
		return runtime.Value{}, err
	}
	d.mu.Lock()
	v, ok := d.m[key]
	d.mu.Unlock()
	if ok && args[2].Kind == runtime.KindRef && args[2].Ref != nil {
		*args[2].Ref = v
	}
	return runtime.Bool(ok), nil
}

func concurrentDictTryRemove(args []runtime.Value) (runtime.Value, error) {
	d, err := asConcurrentDict(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 3 {
		return runtime.Value{}, fmt.Errorf("bcl: ConcurrentDictionary.TryRemove expects a key and an out param")
	}
	key, err := concurrentDictKey(args[1])
	if err != nil {
		return runtime.Value{}, err
	}
	d.mu.Lock()
	v, ok := d.m[key]
	if ok {
		delete(d.m, key)
	}
	d.mu.Unlock()
	if ok && args[2].Kind == runtime.KindRef && args[2].Ref != nil {
		*args[2].Ref = v
	}
	return runtime.Bool(ok), nil
}

func concurrentDictContainsKey(args []runtime.Value) (runtime.Value, error) {
	d, err := asConcurrentDict(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: ConcurrentDictionary.ContainsKey expects a key")
	}
	key, err := concurrentDictKey(args[1])
	if err != nil {
		return runtime.Value{}, err
	}
	d.mu.Lock()
	_, ok := d.m[key]
	d.mu.Unlock()
	return runtime.Bool(ok), nil
}

func concurrentDictGetItem(args []runtime.Value) (runtime.Value, error) {
	d, err := asConcurrentDict(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: ConcurrentDictionary indexer expects a key")
	}
	key, err := concurrentDictKey(args[1])
	if err != nil {
		return runtime.Value{}, err
	}
	d.mu.Lock()
	v, ok := d.m[key]
	d.mu.Unlock()
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: ConcurrentDictionary has no key %q", key)
	}
	return v, nil
}

func concurrentDictSetItem(args []runtime.Value) (runtime.Value, error) {
	d, err := asConcurrentDict(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 3 {
		return runtime.Value{}, fmt.Errorf("bcl: ConcurrentDictionary indexer set expects a key and value")
	}
	key, err := concurrentDictKey(args[1])
	if err != nil {
		return runtime.Value{}, err
	}
	d.mu.Lock()
	d.m[key] = args[2]
	d.mu.Unlock()
	return runtime.Value{}, nil
}

func concurrentDictCount(args []runtime.Value) (runtime.Value, error) {
	d, err := asConcurrentDict(args)
	if err != nil {
		return runtime.Value{}, err
	}
	d.mu.Lock()
	n := len(d.m)
	d.mu.Unlock()
	return runtime.Int32(int32(n)), nil
}
