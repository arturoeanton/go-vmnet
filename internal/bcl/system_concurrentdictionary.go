package bcl

import (
	"fmt"
	"sync"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// nativeConcurrentDict backs System.Collections.Concurrent.ConcurrentDictionary
// `2. Keys are encoded via nativeDict's own general encodeDictKey (Fase
// 3.40: string/int32/int64/float/object/struct, not just string) — plus
// a real mutex, since a host application can legitimately share one
// ConcurrentDictionary across multiple goroutines even though a single
// vmnet Machine only ever runs on one. Stores dictEntry (key AND value,
// Fase 3.52 — originally just the value, since Keys/GetEnumerator/Clear
// weren't implemented yet) so GetEnumerator/get_Keys can hand back the
// real original key, not just its encoded string form.
type nativeConcurrentDict struct {
	mu sync.Mutex
	m  map[string]dictEntry
}

func init() {
	registerCtor("System.Collections.Concurrent.ConcurrentDictionary`2", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeConcurrentDict{m: map[string]dictEntry{}}}, nil
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
	// Clear/get_Keys/GetEnumerator (Fase 3.52) — found via Dapper's own
	// query-plan cache maintenance (SqlMapper.PurgeQueryCache/
	// PurgeQueryCacheByType/CollectCacheGarbage/GetHashCollissions).
	register("System.Collections.Concurrent.ConcurrentDictionary`2::Clear", false, concurrentDictClear)
	register("System.Collections.Concurrent.ConcurrentDictionary`2::get_Keys", true, concurrentDictGetKeys)
	// GetEnumerator reuses Array.GetEnumerator's own nativeArrayEnumerator
	// (system_array.go) over a snapshot KeyValuePair`2 array, rather than
	// a bespoke ConcurrentDictionary-specific enumerator struct — a real
	// ConcurrentDictionary enumerator is already documented as a
	// point-in-time snapshot (real .NET: "does not represent a moving
	// snapshot"), so a plain copied array is a faithful, simpler match.
	register("System.Collections.Concurrent.ConcurrentDictionary`2::GetEnumerator", true, concurrentDictGetEnumerator)
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

// concurrentDictKey reuses nativeDict's own general key encoder
// (Fase 3.40) — string/int32/int64/float/object/struct keys, not just
// string.
func concurrentDictKey(v runtime.Value) (string, error) {
	return encodeDictKey(v)
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
	if e, ok := d.m[k]; ok {
		return e.value, nil
	}
	v, err := compute()
	if err != nil {
		return runtime.Value{}, err
	}
	d.m[k] = dictEntry{key: key, value: v}
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
	d.m[key] = dictEntry{key: args[1], value: args[2]}
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
	e, ok := d.m[key]
	d.mu.Unlock()
	if ok && args[2].Kind == runtime.KindRef && args[2].Ref != nil {
		*args[2].Ref = e.value
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
	e, ok := d.m[key]
	if ok {
		delete(d.m, key)
	}
	d.mu.Unlock()
	if ok && args[2].Kind == runtime.KindRef && args[2].Ref != nil {
		*args[2].Ref = e.value
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
	e, ok := d.m[key]
	d.mu.Unlock()
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: ConcurrentDictionary has no key %q", key)
	}
	return e.value, nil
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
	d.m[key] = dictEntry{key: args[1], value: args[2]}
	d.mu.Unlock()
	return runtime.Value{}, nil
}

// concurrentDictClear backs ConcurrentDictionary.Clear().
func concurrentDictClear(args []runtime.Value) (runtime.Value, error) {
	d, err := asConcurrentDict(args)
	if err != nil {
		return runtime.Value{}, err
	}
	d.mu.Lock()
	d.m = map[string]dictEntry{}
	d.mu.Unlock()
	return runtime.Value{}, nil
}

// concurrentDictGetKeys backs ConcurrentDictionary.Keys — a real
// ICollection<TKey> snapshot (a plain List<T>-shaped value here, same
// simplification dictGetKeys already documents for the ordinary
// Dictionary`2 case).
func concurrentDictGetKeys(args []runtime.Value) (runtime.Value, error) {
	d, err := asConcurrentDict(args)
	if err != nil {
		return runtime.Value{}, err
	}
	d.mu.Lock()
	keys := make([]runtime.Value, 0, len(d.m))
	for _, e := range d.m {
		keys = append(keys, e.key)
	}
	d.mu.Unlock()
	return NewListValue(keys), nil
}

// concurrentDictGetEnumerator backs ConcurrentDictionary.GetEnumerator()
// — snapshots every entry into a real KeyValuePair`2 array, then hands
// that off to Array.GetEnumerator's own nativeArrayEnumerator
// (system_array.go) rather than a bespoke enumerator type; see this
// method's own registration comment (init, above) for why a plain
// snapshot array is a faithful match for real ConcurrentDictionary
// enumeration semantics.
func concurrentDictGetEnumerator(args []runtime.Value) (runtime.Value, error) {
	d, err := asConcurrentDict(args)
	if err != nil {
		return runtime.Value{}, err
	}
	d.mu.Lock()
	elems := make([]runtime.Value, 0, len(d.m))
	for _, e := range d.m {
		pair := runtime.NewStruct(keyValuePairType)
		pair.Fields[0] = e.key
		pair.Fields[1] = e.value
		elems = append(elems, runtime.StructVal(pair))
	}
	d.mu.Unlock()
	return arrayGetEnumerator([]runtime.Value{runtime.ArrRef(&runtime.Array{Elems: elems})})
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
