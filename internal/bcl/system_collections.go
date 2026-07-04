package bcl

import (
	"fmt"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// nativeList backs List<T> for any T: vmnet's runtime.Value is already a
// uniform tagged union, so there's no need to specialize on the generic
// argument (spec §17.1's minimal generics scope). It also backs the
// legacy System.Collections.ArrayList verbatim (same reasoning). typeName
// records which of the two a given instance really is ("Namespace.Type",
// NativeTypeName's shape) — needed since Fase 3.39: NativeTypeName used
// to answer purely from the Go type (*nativeList always => "List`1"),
// which silently misreported every ArrayList as a List`1 to
// receiverTypeName's virtual-dispatch chain walk. See nativeDict's
// identical typeName field for the real bug this caused.
type nativeList struct {
	items    []runtime.Value
	typeName string
}

// nativeDict backs Dictionary<TKey,TValue>. Keys are any of
// string/int32/int64 (Fase 3.38 widened this from Fase 2's
// string-only scope — found via a real, load-bearing case: NPOI's own
// FormulaError..cctor builds a Dictionary keyed by an int error-code
// enum, needed just to construct an HSSFWorkbook at all, not something
// specific to reading formulas). dictEntry keeps the real, original key
// Value alongside the map's own canonical string encoding
// (encodeDictKey) so enumeration (get_Keys/GetEnumerator) can hand back
// the real key — an int-keyed Dictionary's .Keys must yield ints, not
// vmnet's internal string encoding of them.
//
// typeName records which real BCL type this instance is ("Namespace.Type",
// NativeTypeName's shape) — Dictionary`2 or the legacy Hashtable, both
// backed by this same struct. Needed for the identical reason nativeList
// documents: NativeTypeName used to report every nativeDict as
// "Dictionary`2" regardless, which misidentified a Hashtable receiver to
// receiverTypeName's virtual-dispatch chain walk — found via a real,
// load-bearing bug opening an actual .xls file through NPOI: BitField
// Factory.GetInstance's `(BitField)instances[mask]` (instances declared
// as Hashtable) got silently redirected to Dictionary`2::get_Item, which
// throws on a miss instead of Hashtable's own real "return null" miss
// behavior — corrupting the very first lookup into an always-empty cache.
//
// order tracks encoded keys in insertion order (Fase 3.39). Real
// Dictionary<TKey,TValue> doesn't formally guarantee enumeration order,
// but CoreCLR's implementation stably yields insertion order as long as
// no key has ever been removed — and real C# code sometimes silently
// depends on that observable behavior. Found the hard way: NPOI's own
// FileMagicContainer.ValueOf builds a static Dictionary<FileMagic,
// FileMagicContainer> literal (OLE2 first, ..., UNKNOWN last, whose
// "magic" pattern is a zero-length byte array that trivially matches
// ANY input) and relies on foreach checking OLE2 well before reaching
// UNKNOWN's unconditional match. Backing the map with a plain Go map
// (whose `range` order is intentionally randomized per iteration, not
// just per process) made opening a real .xls file succeed or throw
// NotOLE2FileException nondeterministically from one run to the next —
// order restores the real, depended-upon behavior generally, for every
// Dictionary in every package, not just this one call site.
type nativeDict struct {
	m        map[string]dictEntry
	order    []string
	typeName string
	// sorted is true only for SortedDictionary<K,V> (see that type's own
	// registration below) — makes put insert a brand new key at its
	// sorted position (compareValuesNatural over the real, decoded key)
	// instead of appending to order's end, so get_Keys/get_Values/
	// GetEnumerator always yield ascending key order —
	// SortedDictionary<K,V>'s one defining, observable difference from
	// Dictionary<K,V>. Neither constructor's IComparer<K> overload is
	// wired up (silently ignored, same documented gap as SortedSet<T>'s
	// own comparer argument, system_hashset.go) — this package has no
	// Machine access to dispatch a custom one.
	sorted bool
}

// dictPut inserts or overwrites key (already encoded) in d, appending to
// order (or, for a sorted dictionary, inserting at its sorted position)
// only the first time a key is seen — every write path (Add, indexer
// setter, NewDictValue) must go through this so order never drifts out
// of sync with m.
func (d *nativeDict) put(key string, entry dictEntry) {
	if _, exists := d.m[key]; !exists {
		if d.sorted {
			i := 0
			for ; i < len(d.order); i++ {
				if compareValuesNatural(entry.key, d.m[d.order[i]].key) < 0 {
					break
				}
			}
			d.order = append(d.order, "")
			copy(d.order[i+1:], d.order[i:])
			d.order[i] = key
		} else {
			d.order = append(d.order, key)
		}
	}
	d.m[key] = entry
}

// dictDelete removes key from both m and order — used by Remove/Clear so
// a later enumeration never yields a stale or double-counted key.
func (d *nativeDict) delete(key string) bool {
	if _, exists := d.m[key]; !exists {
		return false
	}
	delete(d.m, key)
	for i, k := range d.order {
		if k == key {
			d.order = append(d.order[:i], d.order[i+1:]...)
			break
		}
	}
	return true
}

type dictEntry struct {
	key   runtime.Value
	value runtime.Value
}

// keyValuePairType backs System.Collections.Generic.KeyValuePair`2 — what
// a Dictionary<K,V> enumerator's Current yields per real BCL shape.
var keyValuePairType = runtime.NewValueType(
	"System.Collections.Generic", "KeyValuePair`2",
	[]string{"key", "value"},
	[]runtime.Value{runtime.Null(), runtime.Null()},
)

// listEnumeratorType/dictEnumeratorType back List`1.Enumerator/
// Dictionary`2.Enumerator: real value types (structs), matching the
// compiler-generated `ldloca`+`call` shape `foreach` actually compiles to
// — confirmed against real IL, not assumed (Fase 3.11). index starts at
// -1: MoveNext increments before checking, so the first MoveNext() call
// advances to element 0, matching real enumerator semantics (Current is
// undefined before the first MoveNext).
var listEnumeratorType = runtime.NewValueType(
	"System.Collections.Generic", "List`1+Enumerator",
	[]string{"list", "index"},
	[]runtime.Value{runtime.Null(), runtime.Int32(-1)},
)

// dictEnumeratorType snapshots keys at GetEnumerator time into "keys" (a
// KindArray of strings) rather than iterating nativeDict.m live: Go map
// iteration order is randomized per-run, which would make MoveNext
// non-deterministic *within* a single enumeration, not just across runs
// (real Dictionary doesn't guarantee order either, but does keep it
// stable for the lifetime of one enumerator).
var dictEnumeratorType = runtime.NewValueType(
	"System.Collections.Generic", "Dictionary`2+Enumerator",
	[]string{"dict", "keys", "index"},
	[]runtime.Value{runtime.Null(), runtime.Null(), runtime.Int32(-1)},
)

// sortedDictEnumeratorType is SortedDictionary<K,V>'s own real enumerator
// struct name (distinct from Dictionary`2+Enumerator above) — same
// "exact concrete struct name matters for a direct, non-virtual foreach"
// reasoning as queueEnumeratorType (system_queue.go's own doc comment).
var sortedDictEnumeratorType = runtime.NewValueType(
	"System.Collections.Generic", "SortedDictionary`2+Enumerator",
	[]string{"dict", "keys", "index"},
	[]runtime.Value{runtime.Null(), runtime.Null(), runtime.Int32(-1)},
)

func init() {
	registerCtor("System.Collections.Generic.List`1", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeList{typeName: "System.Collections.Generic.List`1"}}, nil
	})
	register("System.Collections.Generic.List`1::Add", false, listAdd)
	register("System.Collections.Generic.List`1::get_Count", true, listCount)
	register("System.Collections.Generic.List`1::get_Item", true, listGetItem)
	register("System.Collections.Generic.List`1::set_Item", false, listSetItem)
	register("System.Collections.Generic.List`1::ToArray", true, listToArray)
	register("System.Collections.Generic.List`1::AddRange", false, listAddRange)
	register("System.Collections.Generic.List`1::Contains", true, listContains)
	register("System.Collections.Generic.List`1::RemoveAt", false, listRemoveAt)
	register("System.Collections.Generic.List`1::Insert", false, listInsert)
	register("System.Collections.Generic.List`1::Clear", false, listClear)
	register("System.Collections.Generic.List`1::Remove", true, listRemove)
	register("System.Collections.Generic.List`1::GetEnumerator", true, listGetEnumerator)
	register("System.Collections.Generic.List`1+Enumerator::MoveNext", true, listEnumeratorMoveNext)
	register("System.Collections.Generic.List`1+Enumerator::get_Current", true, listEnumeratorGetCurrent)

	registerCtor("System.Collections.Generic.Dictionary`2", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeDict{m: map[string]dictEntry{}, typeName: "System.Collections.Generic.Dictionary`2"}}, nil
	})
	// A plugin/BCL-package class subclassing Dictionary<TKey,TValue>
	// directly (`class Foo : Dictionary<string,string> { public Foo() :
	// base(...) {} }`, a real, if less common, pattern than subclassing
	// Exception) chains to its base via a plain, non-virtual `call
	// Dictionary\`2::.ctor(this, ...)` — not `newobj` (only the exact
	// leaf type gets newobj'd, allocating a plain runtime.Object with no
	// Native at all; the base call runs on that already-allocated
	// object). Without this, every native Dictionary method reached via
	// the ancestor chain walk (Add, the indexer, ...) panics/errors on a
	// nil Native. Same established pattern as system_exception.go's
	// baseExceptionCtorInPlace (Fase 3.13) — found via a real, load-
	// bearing case: DocumentFormat.OpenXml.Packaging's own internal
	// PartExtensionProvider : Dictionary<string, string>.
	register("System.Collections.Generic.Dictionary`2::.ctor", false, dictCtorInPlace)
	register("System.Collections.Generic.Dictionary`2::Add", false, dictAdd)
	register("System.Collections.Generic.Dictionary`2::get_Item", true, dictGetItem)
	register("System.Collections.Generic.Dictionary`2::set_Item", false, dictSetItem)
	register("System.Collections.Generic.Dictionary`2::ContainsKey", true, dictContainsKey)
	register("System.Collections.Generic.Dictionary`2::TryGetValue", true, dictTryGetValue)
	register("System.Collections.Generic.Dictionary`2::get_Count", true, dictCount)
	register("System.Collections.Generic.Dictionary`2::Clear", false, dictClear)
	register("System.Collections.Generic.Dictionary`2::Remove", true, dictRemove)
	registerValueTypeCtor("System.Collections.Generic.KeyValuePair`2", keyValuePairCtor)
	register("System.Collections.Generic.Dictionary`2::GetEnumerator", true, dictGetEnumerator)
	register("System.Collections.Generic.Dictionary`2+Enumerator::MoveNext", true, dictEnumeratorMoveNext)
	register("System.Collections.Generic.Dictionary`2+Enumerator::get_Current", true, dictEnumeratorGetCurrent)
	// ValueCollection/KeyCollection (Dictionary.Values/.Keys) are backed
	// by a plain snapshot nativeList (Fase 3.32) — foreach over either
	// then reuses List<T>'s own enumerator natives verbatim rather than
	// duplicating them under a new struct type: nothing downstream
	// inspects the enumerator's own reported type name, only its
	// MoveNext/get_Current behavior.
	register("System.Collections.Generic.Dictionary`2::get_Values", true, dictGetValues)
	register("System.Collections.Generic.Dictionary`2::get_Keys", true, dictGetKeys)
	register("System.Collections.Generic.Dictionary`2+ValueCollection::GetEnumerator", true, listGetEnumerator)
	register("System.Collections.Generic.Dictionary`2+ValueCollection+Enumerator::MoveNext", true, listEnumeratorMoveNext)
	register("System.Collections.Generic.Dictionary`2+ValueCollection+Enumerator::get_Current", true, listEnumeratorGetCurrent)
	register("System.Collections.Generic.Dictionary`2+KeyCollection::GetEnumerator", true, listGetEnumerator)
	register("System.Collections.Generic.Dictionary`2+KeyCollection+Enumerator::MoveNext", true, listEnumeratorMoveNext)
	register("System.Collections.Generic.Dictionary`2+KeyCollection+Enumerator::get_Current", true, listEnumeratorGetCurrent)

	// SortedDictionary<K,V> (Fase 3.44) — missing entirely before this
	// hardening pass (grepping the whole repo for "SortedDictionary" found
	// nothing at all). Reuses every one of Dictionary<K,V>'s own method
	// implementations verbatim (none of them care about ordering except
	// nativeDict.put, which branches on d.sorted) rather than duplicating
	// them under new names; only GetEnumerator needs its own registration,
	// to tag the returned struct under SortedDictionary`2's own real
	// enumerator type name (dictGetEnumerator already branches on
	// d.sorted for this).
	registerCtor("System.Collections.Generic.SortedDictionary`2", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeDict{m: map[string]dictEntry{}, typeName: "System.Collections.Generic.SortedDictionary`2", sorted: true}}, nil
	})
	register("System.Collections.Generic.SortedDictionary`2::Add", false, dictAdd)
	register("System.Collections.Generic.SortedDictionary`2::get_Item", true, dictGetItem)
	register("System.Collections.Generic.SortedDictionary`2::set_Item", false, dictSetItem)
	register("System.Collections.Generic.SortedDictionary`2::ContainsKey", true, dictContainsKey)
	register("System.Collections.Generic.SortedDictionary`2::TryGetValue", true, dictTryGetValue)
	register("System.Collections.Generic.SortedDictionary`2::get_Count", true, dictCount)
	register("System.Collections.Generic.SortedDictionary`2::Clear", false, dictClear)
	register("System.Collections.Generic.SortedDictionary`2::Remove", true, dictRemove)
	register("System.Collections.Generic.SortedDictionary`2::GetEnumerator", true, dictGetEnumerator)
	register("System.Collections.Generic.SortedDictionary`2+Enumerator::MoveNext", true, dictEnumeratorMoveNext)
	register("System.Collections.Generic.SortedDictionary`2+Enumerator::get_Current", true, dictEnumeratorGetCurrent)
	register("System.Collections.Generic.SortedDictionary`2::get_Values", true, dictGetValues)
	register("System.Collections.Generic.SortedDictionary`2::get_Keys", true, dictGetKeys)

	// System.Collections.ArrayList (Fase 3.36) is the legacy,
	// non-generic predecessor of List<T> — vmnet's runtime.Value is
	// already a uniform tagged union regardless of a real generic type
	// argument, so nativeList (and every one of its existing methods)
	// backs ArrayList verbatim with zero new code. GetEnumerator reuses
	// listGetEnumerator, which always tags its result struct
	// "List`1+Enumerator" regardless of the declared receiver type —
	// Machine.call's virtual dispatch (Fase 3.27) tries the receiver's
	// actual concrete struct type first, so MoveNext/get_Current resolve
	// correctly without a separate "ArrayList+Enumerator" registration.
	registerCtor("System.Collections.ArrayList", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeList{typeName: "System.Collections.ArrayList"}}, nil
	})
	register("System.Collections.ArrayList::Add", false, listAdd)
	register("System.Collections.ArrayList::get_Count", true, listCount)
	register("System.Collections.ArrayList::get_Item", true, listGetItem)
	register("System.Collections.ArrayList::set_Item", false, listSetItem)
	register("System.Collections.ArrayList::ToArray", true, listToArray)
	register("System.Collections.ArrayList::Contains", true, listContains)
	register("System.Collections.ArrayList::RemoveAt", false, listRemoveAt)
	register("System.Collections.ArrayList::Insert", false, listInsert)
	register("System.Collections.ArrayList::Clear", false, listClear)
	register("System.Collections.ArrayList::Remove", true, listRemove)
	register("System.Collections.ArrayList::GetEnumerator", true, listGetEnumerator)

	// System.Collections.Hashtable is the legacy, non-generic
	// predecessor of Dictionary<K,V> — nativeDict backs it the same way,
	// with the same string-keys-only scope nativeDict already documents.
	// Contains(key) is a real alias for ContainsKey on Hashtable (unlike
	// Dictionary<K,V>, which has no such alias). GetEnumerator/foreach
	// (yielding DictionaryEntry, not KeyValuePair`2) is deliberately not
	// wired up: no real IL in this loop's target packages was found
	// enumerating a Hashtable, only indexer-style access.
	registerCtor("System.Collections.Hashtable", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeDict{m: map[string]dictEntry{}, typeName: "System.Collections.Hashtable"}}, nil
	})
	register("System.Collections.Hashtable::Add", false, dictAdd)
	register("System.Collections.Hashtable::get_Item", true, hashtableGetItem)
	register("System.Collections.Hashtable::set_Item", false, dictSetItem)
	register("System.Collections.Hashtable::ContainsKey", true, dictContainsKey)
	register("System.Collections.Hashtable::Contains", true, dictContainsKey)
	register("System.Collections.Hashtable::get_Count", true, dictCount)
	register("System.Collections.Hashtable::Clear", false, dictClear)
	register("System.Collections.Hashtable::Remove", false, dictRemove)

	register("System.Collections.Generic.KeyValuePair`2::get_Key", true, keyValuePairGetKey)
	register("System.Collections.Generic.KeyValuePair`2::get_Value", true, keyValuePairGetValue)
	// `var kv = new KeyValuePair<K,V>(k, v);` assigned straight to a
	// local compiles as `ldloca`+`call .ctor`, not `newobj` — the same
	// compiler optimization DateTime/Nullable`1/TimeSpan already needed
	// their own entry point for.
	register("System.Collections.Generic.KeyValuePair`2::.ctor", false, keyValuePairCtorInPlace)

	// foreach's implicit Dispose() on its enumerator (compiled into a
	// finally block regardless of whether the enumerator type actually
	// needs disposing) — a no-op covers both List/Dictionary's struct
	// enumerators above and the overwhelming majority of real IDisposable
	// usage in practice (nothing to release in a pure-Go interpreter with
	// no unmanaged handles).
	register("System.IDisposable::Dispose", false, disposeNoop)
}

func disposeNoop(args []runtime.Value) (runtime.Value, error) {
	return runtime.Value{}, nil
}

// NewListValue wraps items as a real List<T>-shaped value — the same
// native backing `new List<T>()` produces, so the result is a valid
// source for another foreach/LINQ call/List<T> method. Used by LINQ
// (internal/interpreter/linq.go, Fase 3.14) to materialize eager results
// (Select/Where/ToList/...) as something the rest of the program can keep
// treating as a normal collection.
func NewListValue(items []runtime.Value) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeList{items: items, typeName: "System.Collections.Generic.List`1"}})
}

// NativeListItems returns a native-backed List<T>'s items, if native is
// one — used by LINQ's enumerateAll (internal/interpreter/linq.go) as a
// direct fast path (skip driving a real GetEnumerator/MoveNext/
// get_Current loop when the elements are already a Go slice), and by
// every other plain bcl.Native that special-cases "an IEnumerable
// argument might really already be a Go slice" the same way (String.Join,
// List<T>.AddRange/Contains, ...). A *NativeOrdered (a pending LINQ
// OrderBy/ThenBy chain, system_linq_native.go) answers here too, via its
// own already-sorted Items, and a *NativeGrouping (one GroupBy result
// group) recurses into its own already-List-shaped Items — found via a
// real, hand-written probe: `string.Join("/", someGroup)` (iterating an
// IGrouping<K,V> directly, a very ordinary GroupBy consumption pattern)
// printed the group's own placeholder ToString() instead of its
// elements before this case existed, the exact same class of bug
// NativeOrdered's own case just below already fixed for OrderBy/ThenBy.
// Both types' own doc comments explain why this is load-bearing, not
// just a convenience: those plain natives have no Machine access to
// fall back to a real GetEnumerator/MoveNext loop if this returns false.
func NativeListItems(native any) ([]runtime.Value, bool) {
	switch n := native.(type) {
	case *nativeList:
		return n.items, true
	case *NativeOrdered:
		return n.Items, true
	case *NativeGrouping:
		if n.Items.Kind == runtime.KindObject && n.Items.Obj != nil {
			return NativeListItems(n.Items.Obj.Native)
		}
		return nil, false
	default:
		return nil, false
	}
}

// SetNativeListItems overwrites a native-backed List<T>'s items in place,
// reporting whether native really was one — used by List<T>.Sort
// (internal/interpreter/array_sort.go's machineRegistry entry, which
// needs Machine access to invoke a Comparison<T>/IComparer<T> argument,
// unlike every other plain List method in this file): real List<T>.Sort
// mutates the same list instance every other outstanding reference sees,
// not a copy, so this must write back through the existing *nativeList
// rather than have the caller build and return a brand new one.
func SetNativeListItems(native any, items []runtime.Value) bool {
	l, ok := native.(*nativeList)
	if !ok {
		return false
	}
	l.items = items
	return true
}

// NewDictValue wraps pairs (string keys, LINQ's ToDictionary own scope)
// as a real Dictionary<string,V>-shaped value — used by LINQ's
// ToDictionary (internal/interpreter/linq.go, Fase 3.32), which needs to
// build a real Dictionary instance without importing bcl's own
// unexported nativeDict/dictEntry types.
func NewDictValue(pairs map[string]runtime.Value) runtime.Value {
	d := &nativeDict{m: make(map[string]dictEntry, len(pairs)), typeName: "System.Collections.Generic.Dictionary`2"}
	for k, v := range pairs {
		encoded, _ := encodeDictKey(runtime.String(k)) // a string key always encodes
		d.put(encoded, dictEntry{key: runtime.String(k), value: v})
	}
	return runtime.ObjRef(&runtime.Object{Native: d})
}

// derefStructReceiver unwraps a struct instance method's receiver: it
// arrives as a managed pointer (KindRef) from `ldloca`+`call`, same
// reasoning as struct receivers throughout Fase 3.7-3.9.
func derefStructReceiver(args []runtime.Value, kind, methodDesc string) (*runtime.Struct, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("bcl: %s called without a receiver", methodDesc)
	}
	recv := args[0]
	if recv.Kind == runtime.KindRef {
		if recv.Ref == nil {
			return nil, fmt.Errorf("bcl: %s called through a null pointer", methodDesc)
		}
		recv = *recv.Ref
	}
	if recv.Kind != runtime.KindStruct || recv.Struct == nil {
		return nil, fmt.Errorf("bcl: %s receiver is not a %s", methodDesc, kind)
	}
	return recv.Struct, nil
}

func listGetEnumerator(args []runtime.Value) (runtime.Value, error) {
	if _, err := asList(args); err != nil {
		return runtime.Value{}, err
	}
	s := runtime.NewStruct(listEnumeratorType)
	s.Fields[0] = args[0]
	return runtime.StructVal(s), nil
}

func listEnumeratorMoveNext(args []runtime.Value) (runtime.Value, error) {
	s, err := derefStructReceiver(args, "List.Enumerator", "List.Enumerator.MoveNext")
	if err != nil {
		return runtime.Value{}, err
	}
	l, err := asList([]runtime.Value{s.Fields[0]})
	if err != nil {
		return runtime.Value{}, err
	}
	next := s.Fields[1].I4 + 1
	s.Fields[1] = runtime.Int32(next)
	return runtime.Bool(int(next) < len(l.items)), nil
}

func listEnumeratorGetCurrent(args []runtime.Value) (runtime.Value, error) {
	s, err := derefStructReceiver(args, "List.Enumerator", "List.Enumerator.Current")
	if err != nil {
		return runtime.Value{}, err
	}
	l, err := asList([]runtime.Value{s.Fields[0]})
	if err != nil {
		return runtime.Value{}, err
	}
	idx := int(s.Fields[1].I4)
	if idx < 0 || idx >= len(l.items) {
		return runtime.Value{}, fmt.Errorf("bcl: List.Enumerator.Current: index %d out of range", idx)
	}
	return l.items[idx], nil
}

func dictGetEnumerator(args []runtime.Value) (runtime.Value, error) {
	d, err := asDict(args)
	if err != nil {
		return runtime.Value{}, err
	}
	keys := make([]runtime.Value, len(d.order))
	for i, k := range d.order {
		keys[i] = runtime.String(k)
	}
	t := dictEnumeratorType
	if d.sorted {
		t = sortedDictEnumeratorType
	}
	s := runtime.NewStruct(t)
	s.Fields[0] = args[0]
	s.Fields[1] = runtime.ArrRef(&runtime.Array{Elems: keys})
	return runtime.StructVal(s), nil
}

func dictEnumeratorMoveNext(args []runtime.Value) (runtime.Value, error) {
	s, err := derefStructReceiver(args, "Dictionary.Enumerator", "Dictionary.Enumerator.MoveNext")
	if err != nil {
		return runtime.Value{}, err
	}
	next := s.Fields[2].I4 + 1
	s.Fields[2] = runtime.Int32(next)
	return runtime.Bool(int(next) < len(s.Fields[1].Arr.Elems)), nil
}

func dictEnumeratorGetCurrent(args []runtime.Value) (runtime.Value, error) {
	s, err := derefStructReceiver(args, "Dictionary.Enumerator", "Dictionary.Enumerator.Current")
	if err != nil {
		return runtime.Value{}, err
	}
	d, err := asDict([]runtime.Value{s.Fields[0]})
	if err != nil {
		return runtime.Value{}, err
	}
	keys := s.Fields[1].Arr.Elems
	idx := int(s.Fields[2].I4)
	if idx < 0 || idx >= len(keys) {
		// Real Dictionary<TKey,TValue>.Enumerator.Current doesn't throw
		// when accessed before the first MoveNext() or after it's
		// returned false — it quietly answers default(KeyValuePair
		// <TKey,TValue>) (a zeroed struct), the same as every other
		// built-in BCL enumerator's Current in that state (Fase 3.40,
		// found via a real, load-bearing case: some real call site reads
		// Current speculatively around its own MoveNext check).
		return runtime.StructVal(runtime.NewStruct(keyValuePairType)), nil
	}
	entry, ok := d.m[keys[idx].Str]
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: Dictionary.Enumerator.Current: key no longer present (mutated during enumeration?)")
	}
	kv := runtime.NewStruct(keyValuePairType)
	kv.Fields[0] = entry.key
	kv.Fields[1] = entry.value
	return runtime.StructVal(kv), nil
}

func keyValuePairGetKey(args []runtime.Value) (runtime.Value, error) {
	s, err := derefStructReceiver(args, "KeyValuePair", "KeyValuePair.Key")
	if err != nil {
		return runtime.Value{}, err
	}
	return s.Fields[0], nil
}

func keyValuePairGetValue(args []runtime.Value) (runtime.Value, error) {
	s, err := derefStructReceiver(args, "KeyValuePair", "KeyValuePair.Value")
	if err != nil {
		return runtime.Value{}, err
	}
	return s.Fields[1], nil
}

func keyValuePairCtor(args []runtime.Value) (*runtime.Struct, error) {
	kv := runtime.NewStruct(keyValuePairType)
	if len(args) > 0 {
		kv.Fields[0] = args[0]
	}
	if len(args) > 1 {
		kv.Fields[1] = args[1]
	}
	return kv, nil
}

func keyValuePairCtorInPlace(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindRef || args[0].Ref == nil {
		return runtime.Value{}, fmt.Errorf("bcl: KeyValuePair constructor called without a receiver")
	}
	s, err := keyValuePairCtor(args[1:])
	if err != nil {
		return runtime.Value{}, err
	}
	*args[0].Ref = runtime.StructVal(s)
	return runtime.Value{}, nil
}

func asList(args []runtime.Value) (*nativeList, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return nil, fmt.Errorf("bcl: List method called without a receiver")
	}
	l, ok := args[0].Obj.Native.(*nativeList)
	if !ok {
		return nil, fmt.Errorf("bcl: receiver is not a List")
	}
	return l, nil
}

func listAdd(args []runtime.Value) (runtime.Value, error) {
	l, err := asList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: List.Add expects 1 argument, got %d", len(args)-1)
	}
	l.items = append(l.items, args[1])
	return runtime.Value{}, nil
}

func listCount(args []runtime.Value) (runtime.Value, error) {
	l, err := asList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(int32(len(l.items))), nil
}

func listGetItem(args []runtime.Value) (runtime.Value, error) {
	l, err := asList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 2 || args[1].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: List indexer expects an int32 index")
	}
	idx := int(args[1].I4)
	if idx < 0 || idx >= len(l.items) {
		return runtime.Value{}, fmt.Errorf("bcl: List index %d out of range (length %d)", idx, len(l.items))
	}
	return l.items[idx], nil
}

func listSetItem(args []runtime.Value) (runtime.Value, error) {
	l, err := asList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 3 || args[1].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: List indexer setter expects an int32 index")
	}
	idx := int(args[1].I4)
	if idx < 0 || idx >= len(l.items) {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "Index was out of range."}
	}
	l.items[idx] = args[2]
	return runtime.Value{}, nil
}

func listToArray(args []runtime.Value) (runtime.Value, error) {
	l, err := asList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	out := make([]runtime.Value, len(l.items))
	copy(out, l.items)
	return runtime.ArrRef(&runtime.Array{Elems: out}), nil
}

// listAddRange accepts either another List<T> (the common case) or a
// plain array — mirroring stringJoin's same two-shape unwrapping for an
// IEnumerable<T> argument.
func listAddRange(args []runtime.Value) (runtime.Value, error) {
	l, err := asList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: List.AddRange expects 1 argument")
	}
	switch other := args[1]; other.Kind {
	case runtime.KindArray:
		if other.Arr != nil {
			l.items = append(l.items, other.Arr.Elems...)
		}
	case runtime.KindObject:
		if other.Obj != nil {
			// NativeListItems, not a direct *nativeList assertion — same
			// widening as stringJoin's, and for the same reason: AddRange's
			// source is just as often a LINQ result (a NativeOrdered
			// OrderBy/ThenBy chain included) as a real List<T>.
			if items, ok := NativeListItems(other.Obj.Native); ok {
				l.items = append(l.items, items...)
			}
		}
	}
	return runtime.Value{}, nil
}

func listContains(args []runtime.Value) (runtime.Value, error) {
	l, err := asList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: List.Contains expects 1 argument")
	}
	for _, item := range l.items {
		if valuesEqual(item, args[1]) {
			return runtime.Bool(true), nil
		}
	}
	return runtime.Bool(false), nil
}

func listRemoveAt(args []runtime.Value) (runtime.Value, error) {
	l, err := asList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 2 || args[1].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: List.RemoveAt expects an int32 index")
	}
	idx := int(args[1].I4)
	if idx < 0 || idx >= len(l.items) {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "Index was out of range."}
	}
	l.items = append(l.items[:idx], l.items[idx+1:]...)
	return runtime.Value{}, nil
}

func listInsert(args []runtime.Value) (runtime.Value, error) {
	l, err := asList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 3 || args[1].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: List.Insert expects an int32 index")
	}
	idx := int(args[1].I4)
	if idx < 0 || idx > len(l.items) {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "Index was out of range."}
	}
	l.items = append(l.items, runtime.Value{})
	copy(l.items[idx+1:], l.items[idx:])
	l.items[idx] = args[2]
	return runtime.Value{}, nil
}

func listClear(args []runtime.Value) (runtime.Value, error) {
	l, err := asList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	l.items = l.items[:0]
	return runtime.Value{}, nil
}

// listRemove removes the first element equal to args[1] (reference
// identity for object/array/struct-shaped values, value equality for
// primitives/strings — same notion of equality valuesEqual already uses
// for Object.Equals/List.Contains), returning whether anything was
// removed, matching real List<T>.Remove's bool result.
func listRemove(args []runtime.Value) (runtime.Value, error) {
	l, err := asList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: List.Remove expects a value argument")
	}
	for i, item := range l.items {
		if valuesEqual(item, args[1]) {
			l.items = append(l.items[:i], l.items[i+1:]...)
			return runtime.Bool(true), nil
		}
	}
	return runtime.Bool(false), nil
}

func asDict(args []runtime.Value) (*nativeDict, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return nil, fmt.Errorf("bcl: Dictionary method called without a receiver")
	}
	d, ok := args[0].Obj.Native.(*nativeDict)
	if !ok {
		return nil, fmt.Errorf("bcl: receiver is not a Dictionary")
	}
	return d, nil
}

// dictKeyValue reads args[i] as a Dictionary key argument, dereferencing
// a managed pointer if needed (a struct-shaped key, e.g. a value-type
// enum, could in principle arrive that way — no real case found in this
// loop, but cheap to handle uniformly with every other by-ref-tolerant
// native).
func dictKeyValue(args []runtime.Value, i int) (runtime.Value, error) {
	if len(args) <= i {
		return runtime.Value{}, fmt.Errorf("bcl: Dictionary key argument missing")
	}
	v := args[i]
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	return v, nil
}

// encodeDictKey turns a real key Value into nativeDict.m's internal map
// key — string/int32/int64/object (Fase 3.38; widened from Fase 2's
// string-only scope by two real, load-bearing cases found opening a
// real NPOI workbook: an int-keyed and an object-keyed Dictionary, both
// in static field initializers that run just from touching the formula
// registry, not from anything the caller's own code does). Prefixed by
// kind so a string key can never collide with a numeric one that
// happens to format the same way.
//
// KindObject keys use Go pointer identity on the underlying
// *runtime.Object, not a called Equals()/GetHashCode() override — real
// Dictionary<TKey,TValue> would use EqualityComparer<TKey>.Default,
// which for a reference type with no override IS reference equality
// anyway, and the one real case found (NPOI.SS.Formula.Eval.ErrorEval
// keying by NPOI.SS.UserModel.FormulaError's static singleton
// instances — a common C# "smart enum" pattern) inserts and looks up
// using the exact same cached object reference every time, so pointer
// identity is the correct semantics here, not an approximation of it.
func encodeDictKey(v runtime.Value) (string, error) {
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	switch v.Kind {
	case runtime.KindString:
		return "s:" + v.Str, nil
	case runtime.KindI4:
		return fmt.Sprintf("i:%d", v.I4), nil
	case runtime.KindI8:
		return fmt.Sprintf("l:%d", v.I8), nil
	case runtime.KindR4:
		return fmt.Sprintf("f:%v", v.R4), nil
	case runtime.KindR8:
		return fmt.Sprintf("d:%v", v.R8), nil
	case runtime.KindObject:
		// A *nativeUri-backed key (System.Uri itself, or a real subclass
		// like System.IO.Packaging.PackUriHelper's internal
		// ValidatedPartUri, which chains `: base(...)` into the same
		// Native — see uriCtorInPlace/system_uri.go) needs VALUE equality,
		// not pointer identity: real System.Uri overrides both
		// Equals(object) and GetHashCode() to compare by normalized URI
		// string, and ValidatedPartUri never re-overrides GetHashCode (its
		// own IEquatable<ValidatedPartUri>.Equals is explicit-interface
		// only), so a real Dictionary<Uri,...>/Dictionary<ValidatedPartUri,
		// ...> looks up by that same inherited string-based hash — not by
		// reference. Found via a real, load-bearing bug reading
		// ClosedXML's real .xlsx through DocumentFormat.OpenXml: ZipPackage
		// .ContentTypeHelper keys its override dictionary by
		// ValidatedPartUri, populating it from one instance (parsing
		// [Content_Types].xml) and looking it up from a completely
		// different instance later (validating the part actually being
		// opened) — pointer-identity keying (this function's prior,
		// blanket assumption for every KindObject key) made every such
		// lookup miss, silently falling through to the extension-based
		// Default content type instead of the correct Override one, which
		// surfaced as a real, wrong InvalidPartContentType exception
		// thrown by real, unmodified OpenXmlPart.Load (Fase 3.40).
		if u, ok := v.Obj.Native.(*nativeUri); ok {
			return "u:" + u.u.String(), nil
		}
		return fmt.Sprintf("o:%p", v.Obj), nil
	case runtime.KindNull:
		return "n:", nil
	case runtime.KindStruct:
		// A real value-type key (e.g. a "record struct"-style key type
		// used purely for value-based interning/deduplication, Fase
		// 3.40 — found via a real, load-bearing case: ClosedXML's own
		// style repositories key a ConcurrentDictionary by structs like
		// XLAlignmentKey) — encoded by recursively encoding every field
		// in declaration order. This assumes the struct's real semantics
		// are plain field-wise value equality (the default a struct gets
		// unless it overrides Equals/GetHashCode with something more
		// exotic) — true for every real key type found in this loop's
		// target packages so far, all plain data-holder structs.
		if v.Struct == nil {
			return "", fmt.Errorf("bcl: Dictionary key: null struct value")
		}
		var sb strings.Builder
		sb.WriteString("t:")
		for i, f := range v.Struct.Fields {
			if i > 0 {
				sb.WriteByte(',')
			}
			enc, err := encodeDictKey(f)
			if err != nil {
				return "", fmt.Errorf("bcl: Dictionary key: struct field %d: %w", i, err)
			}
			sb.WriteString(enc)
		}
		return sb.String(), nil
	default:
		return "", fmt.Errorf("bcl: Dictionary key kind %v is not supported", v.Kind)
	}
}

// dictKey reads and encodes args[i] as a Dictionary key in one step —
// what every read/write accessor below actually needs.
func dictKey(args []runtime.Value, i int) (string, error) {
	v, err := dictKeyValue(args, i)
	if err != nil {
		return "", err
	}
	return encodeDictKey(v)
}

// dictCtorInPlace backs a base-chaining call to Dictionary`2::.ctor from
// a plugin/BCL-package subclass (see the register() call site's own doc
// comment) — mutates the already-allocated derived receiver's Native
// field rather than allocating a fresh Object (which registerCtor's
// newobj-only path already does for constructing Dictionary directly).
// Every real overload (capacity, IEqualityComparer, an IDictionary to
// copy from) is ignored beyond the receiver itself: nativeDict has no
// notion of a custom comparer (Fase 3.32's own string-keys-only scope),
// and no real caller found in this loop's target packages seeds a
// subclassed Dictionary from an existing one at construction time.
func dictCtorInPlace(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return runtime.Value{}, fmt.Errorf("bcl: Dictionary`2 constructor called without a receiver")
	}
	typeName := "System.Collections.Generic.Dictionary`2"
	if t := args[0].Obj.Type; t != nil {
		if t.Namespace != "" {
			typeName = t.Namespace + "." + t.Name
		} else {
			typeName = t.Name
		}
	}
	args[0].Obj.Native = &nativeDict{m: map[string]dictEntry{}, typeName: typeName}
	return runtime.Value{}, nil
}

func dictAdd(args []runtime.Value) (runtime.Value, error) {
	d, err := asDict(args)
	if err != nil {
		return runtime.Value{}, err
	}
	kv, err := dictKeyValue(args, 1)
	if err != nil {
		return runtime.Value{}, err
	}
	key, err := encodeDictKey(kv)
	if err != nil {
		return runtime.Value{}, err
	}
	if _, exists := d.m[key]; exists {
		return runtime.Value{}, fmt.Errorf("bcl: Dictionary already contains key %q", key)
	}
	d.put(key, dictEntry{key: kv, value: args[2]})
	return runtime.Value{}, nil
}

func dictGetItem(args []runtime.Value) (runtime.Value, error) {
	d, err := asDict(args)
	if err != nil {
		return runtime.Value{}, err
	}
	key, err := dictKey(args, 1)
	if err != nil {
		return runtime.Value{}, err
	}
	e, ok := d.m[key]
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: Dictionary has no key %q", key)
	}
	return e.value, nil
}

// hashtableGetItem backs Hashtable's indexer, unlike Dictionary<K,V>'s:
// a missing key returns null rather than throwing KeyNotFoundException,
// matching real System.Collections.Hashtable semantics.
func hashtableGetItem(args []runtime.Value) (runtime.Value, error) {
	d, err := asDict(args)
	if err != nil {
		return runtime.Value{}, err
	}
	key, err := dictKey(args, 1)
	if err != nil {
		return runtime.Value{}, err
	}
	e, ok := d.m[key]
	if !ok {
		return runtime.Null(), nil
	}
	return e.value, nil
}

func dictSetItem(args []runtime.Value) (runtime.Value, error) {
	d, err := asDict(args)
	if err != nil {
		return runtime.Value{}, err
	}
	kv, err := dictKeyValue(args, 1)
	if err != nil {
		return runtime.Value{}, err
	}
	key, err := encodeDictKey(kv)
	if err != nil {
		return runtime.Value{}, err
	}
	d.put(key, dictEntry{key: kv, value: args[2]})
	return runtime.Value{}, nil
}

func dictContainsKey(args []runtime.Value) (runtime.Value, error) {
	d, err := asDict(args)
	if err != nil {
		return runtime.Value{}, err
	}
	key, err := dictKey(args, 1)
	if err != nil {
		return runtime.Value{}, err
	}
	_, ok := d.m[key]
	return runtime.Bool(ok), nil
}

// dictTryGetValue's out parameter arrives as a managed pointer (KindRef),
// the same mechanism any `out`/`ref` primitive parameter already uses
// (Fase 3.5's ByRef.cs). On a miss it writes Null() rather than a real
// default(TValue) — vmnet has no generic type-argument info at this call
// site to produce a typed zero value instead, a documented approximation.
func dictTryGetValue(args []runtime.Value) (runtime.Value, error) {
	d, err := asDict(args)
	if err != nil {
		return runtime.Value{}, err
	}
	key, err := dictKey(args, 1)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 3 || args[2].Kind != runtime.KindRef || args[2].Ref == nil {
		return runtime.Value{}, fmt.Errorf("bcl: Dictionary.TryGetValue expects an out parameter")
	}
	e, ok := d.m[key]
	if ok {
		*args[2].Ref = e.value
		return runtime.Bool(true), nil
	}
	// A miss must set the out param to default(TValue), same as real
	// TryGetValue — but this call site has no TValue to build a real
	// typed zero from (same documented gap as FirstOrDefault's own
	// untyped-miss case). Unconditionally overwriting with an untyped
	// runtime.Null() used to actively destroy a perfectly good typed
	// zero that was ALREADY sitting there: an `out int v` argument's
	// storage is already zero-initialized to a real Int32(0) by the
	// method's own locals-init step (method.LocalDefaults,
	// interpreter/eval.go) before TryGetValue is ever called — probed
	// via a real fixture (`d.TryGetValue("missing", out int v); return
	// v.ToString();`): stomping that Int32(0) with KindNull made the
	// very next `v.ToString()` throw "expects an int32 receiver" instead
	// of printing "0" like real .NET does (Fase 3.44). Leaving the slot
	// untouched on a miss preserves that pre-existing typed zero for the
	// overwhelmingly common `out var`/freshly-declared-`out` case; the
	// one case this still gets wrong (the out variable already held some
	// OTHER non-default value before a miss) is a strictly smaller gap
	// than unconditionally corrupting every miss the way this did before.
	return runtime.Bool(false), nil
}

func dictCount(args []runtime.Value) (runtime.Value, error) {
	d, err := asDict(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(int32(len(d.m))), nil
}

func dictRemove(args []runtime.Value) (runtime.Value, error) {
	d, err := asDict(args)
	if err != nil {
		return runtime.Value{}, err
	}
	key, err := dictKey(args, 1)
	if err != nil {
		return runtime.Value{}, err
	}
	existed := d.delete(key)
	return runtime.Bool(existed), nil
}

func dictGetValues(args []runtime.Value) (runtime.Value, error) {
	d, err := asDict(args)
	if err != nil {
		return runtime.Value{}, err
	}
	values := make([]runtime.Value, len(d.order))
	for i, k := range d.order {
		values[i] = d.m[k].value
	}
	return NewListValue(values), nil
}

func dictGetKeys(args []runtime.Value) (runtime.Value, error) {
	d, err := asDict(args)
	if err != nil {
		return runtime.Value{}, err
	}
	keys := make([]runtime.Value, len(d.order))
	for i, k := range d.order {
		keys[i] = d.m[k].key
	}
	return NewListValue(keys), nil
}

func dictClear(args []runtime.Value) (runtime.Value, error) {
	d, err := asDict(args)
	if err != nil {
		return runtime.Value{}, err
	}
	for k := range d.m {
		delete(d.m, k)
	}
	d.order = nil
	return runtime.Value{}, nil
}
