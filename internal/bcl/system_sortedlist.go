package bcl

import (
	"fmt"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// nativeSortedList backs the legacy System.Collections.SortedList — an
// IDictionary that keeps entries ordered by key at all times (Fase
// 3.39), unlike Dictionary/Hashtable's unordered (nativeDict) storage.
// Found via a real, load-bearing case: NPOI's own RowRecordsAggregate
// keys its rows by row number in a SortedList specifically so its own
// enumeration (.Values) streams rows back in ascending order — an
// ordinary Hashtable/Dictionary here would silently shuffle row order.
// keys/values are parallel slices kept sorted by keys via binary-search
// insertion; real SortedList is backed by two parallel arrays the exact
// same way internally.
type nativeSortedList struct {
	keys     []runtime.Value
	values   []runtime.Value
	typeName string
}

func init() {
	// Registered under both the legacy non-generic SortedList and the
	// generic SortedList`2 (e.g. ClosedXML's style caches use
	// SortedList<TKey,TValue> directly) — same Go struct backs both,
	// matching the nativeDict/nativeStack precedent (Fase 3.39/3.40).
	for _, typeName := range []string{"System.Collections.SortedList", "System.Collections.Generic.SortedList`2"} {
		tn := typeName
		registerCtor(tn, func([]runtime.Value) (*runtime.Object, error) {
			return &runtime.Object{Native: &nativeSortedList{typeName: tn}}, nil
		})
		register(tn+"::get_Item", true, sortedListGetItem)
		register(tn+"::set_Item", false, sortedListSetItem)
		register(tn+"::Add", false, sortedListAdd)
		register(tn+"::get_Count", true, sortedListGetCount)
		// Remove is void on SortedList (unlike Dictionary`2.Remove's bool) —
		// a missing key is silently a no-op, matching real semantics.
		register(tn+"::Remove", false, sortedListRemove)
		register(tn+"::get_Values", true, sortedListGetValues)
		register(tn+"::get_Keys", true, sortedListGetKeys)
		register(tn+"::ContainsKey", true, sortedListContainsKey)
		register(tn+"::Contains", true, sortedListContainsKey)
		register(tn+"::TryGetValue", true, sortedListTryGetValue)
		register(tn+"::Clear", false, sortedListClear)
		register(tn+"::IndexOfKey", true, sortedListIndexOfKey)
	}
	// List<T>/ArrayList.CopyTo(array, index) — needed by SortedList.
	// Values.CopyTo, but registered for both real collections too since
	// it's the same real ICollection member either exposes.
	register("System.Collections.Generic.List`1::CopyTo", false, listCopyTo)
	register("System.Collections.ArrayList::CopyTo", false, listCopyTo)
}

func asSortedList(args []runtime.Value) (*nativeSortedList, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return nil, fmt.Errorf("bcl: SortedList method called without a receiver")
	}
	sl, ok := args[0].Obj.Native.(*nativeSortedList)
	if !ok {
		return nil, fmt.Errorf("bcl: receiver is not a SortedList")
	}
	return sl, nil
}

// compareSortedListKeys orders two SortedList keys — string/int32/int64,
// the same real-world scope encodeDictKey documents for
// Dictionary/Hashtable (Fase 3.38/3.39), plus a Uri-backed object key
// (Fase 3.40, found via System.IO.Packaging.PackUriHelper's own
// SortedList<PackUriHelper.ValidatedPartUri, ...>): compareSortedListKeys
// has no Machine access to dispatch a real IComparable<T>.CompareTo
// override generically (unlike Array.Sort/Comparer<T>.Default,
// internal/interpreter/comparer.go), but ValidatedPartUri's own ordering
// is really just its underlying Uri string compared ordinally, and that
// underlying nativeUri is reachable directly off Obj.Native (set
// alongside Obj.Type by uriCtorInPlace's "Type xor Native" exception,
// system_uri.go) without needing to invoke anything.
func compareSortedListKeys(a, b runtime.Value) (int, error) {
	if a.Kind == runtime.KindObject && b.Kind == runtime.KindObject {
		au, aerr := asUriValue(a)
		bu, berr := asUriValue(b)
		if aerr == nil && berr == nil {
			return strings.Compare(au.u.String(), bu.u.String()), nil
		}
		return 0, fmt.Errorf("bcl: SortedList key kind %v is not supported", a.Kind)
	}
	switch a.Kind {
	case runtime.KindI4:
		if b.Kind != runtime.KindI4 {
			return 0, fmt.Errorf("bcl: SortedList key kind mismatch")
		}
		switch {
		case a.I4 < b.I4:
			return -1, nil
		case a.I4 > b.I4:
			return 1, nil
		default:
			return 0, nil
		}
	case runtime.KindI8:
		if b.Kind != runtime.KindI8 {
			return 0, fmt.Errorf("bcl: SortedList key kind mismatch")
		}
		switch {
		case a.I8 < b.I8:
			return -1, nil
		case a.I8 > b.I8:
			return 1, nil
		default:
			return 0, nil
		}
	case runtime.KindString:
		if b.Kind != runtime.KindString {
			return 0, fmt.Errorf("bcl: SortedList key kind mismatch")
		}
		switch {
		case a.Str < b.Str:
			return -1, nil
		case a.Str > b.Str:
			return 1, nil
		default:
			return 0, nil
		}
	default:
		return 0, fmt.Errorf("bcl: SortedList key kind %v is not supported", a.Kind)
	}
}

// find returns key's slot: found=true and its exact index, or found=false
// and the index a new entry belonging at key would need inserting at
// (binary search over the already-sorted keys slice).
func (sl *nativeSortedList) find(key runtime.Value) (idx int, found bool, err error) {
	lo, hi := 0, len(sl.keys)
	for lo < hi {
		mid := (lo + hi) / 2
		c, cerr := compareSortedListKeys(sl.keys[mid], key)
		if cerr != nil {
			return 0, false, cerr
		}
		switch {
		case c == 0:
			return mid, true, nil
		case c < 0:
			lo = mid + 1
		default:
			hi = mid
		}
	}
	return lo, false, nil
}

func sortedListGetItem(args []runtime.Value) (runtime.Value, error) {
	sl, err := asSortedList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	key, err := dictKeyValue(args, 1)
	if err != nil {
		return runtime.Value{}, err
	}
	idx, found, err := sl.find(key)
	if err != nil {
		return runtime.Value{}, err
	}
	if !found {
		return runtime.Null(), nil
	}
	return sl.values[idx], nil
}

func sortedListSetItem(args []runtime.Value) (runtime.Value, error) {
	sl, err := asSortedList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	key, err := dictKeyValue(args, 1)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 3 {
		return runtime.Value{}, fmt.Errorf("bcl: SortedList indexer setter expects a value")
	}
	idx, found, err := sl.find(key)
	if err != nil {
		return runtime.Value{}, err
	}
	if found {
		sl.values[idx] = args[2]
		return runtime.Value{}, nil
	}
	sl.keys = append(sl.keys, runtime.Value{})
	copy(sl.keys[idx+1:], sl.keys[idx:])
	sl.keys[idx] = key
	sl.values = append(sl.values, runtime.Value{})
	copy(sl.values[idx+1:], sl.values[idx:])
	sl.values[idx] = args[2]
	return runtime.Value{}, nil
}

// sortedListAdd backs Add(key, value) — unlike the indexer setter,
// real SortedList/SortedList<K,V>.Add throws ArgumentException on a
// duplicate key instead of silently overwriting it.
func sortedListAdd(args []runtime.Value) (runtime.Value, error) {
	sl, err := asSortedList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	key, err := dictKeyValue(args, 1)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 3 {
		return runtime.Value{}, fmt.Errorf("bcl: SortedList.Add expects a value")
	}
	idx, found, err := sl.find(key)
	if err != nil {
		return runtime.Value{}, err
	}
	if found {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentException", Message: "An item with the same key has already been added."}
	}
	sl.keys = append(sl.keys, runtime.Value{})
	copy(sl.keys[idx+1:], sl.keys[idx:])
	sl.keys[idx] = key
	sl.values = append(sl.values, runtime.Value{})
	copy(sl.values[idx+1:], sl.values[idx:])
	sl.values[idx] = args[2]
	return runtime.Value{}, nil
}

func sortedListGetCount(args []runtime.Value) (runtime.Value, error) {
	sl, err := asSortedList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(int32(len(sl.keys))), nil
}

func sortedListRemove(args []runtime.Value) (runtime.Value, error) {
	sl, err := asSortedList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	key, err := dictKeyValue(args, 1)
	if err != nil {
		return runtime.Value{}, err
	}
	idx, found, err := sl.find(key)
	if err != nil {
		return runtime.Value{}, err
	}
	if found {
		sl.keys = append(sl.keys[:idx], sl.keys[idx+1:]...)
		sl.values = append(sl.values[:idx], sl.values[idx+1:]...)
	}
	return runtime.Value{}, nil
}

func sortedListGetValues(args []runtime.Value) (runtime.Value, error) {
	sl, err := asSortedList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	values := make([]runtime.Value, len(sl.values))
	copy(values, sl.values)
	return NewListValue(values), nil
}

func sortedListGetKeys(args []runtime.Value) (runtime.Value, error) {
	sl, err := asSortedList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	keys := make([]runtime.Value, len(sl.keys))
	copy(keys, sl.keys)
	return NewListValue(keys), nil
}

func sortedListContainsKey(args []runtime.Value) (runtime.Value, error) {
	sl, err := asSortedList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	key, err := dictKeyValue(args, 1)
	if err != nil {
		return runtime.Value{}, err
	}
	_, found, err := sl.find(key)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(found), nil
}

// sortedListIndexOfKey returns key's position in sorted order, or -1 if
// absent — real SortedList<K,V>.IndexOfKey, found via a real, load-
// bearing case: DocumentFormat.OpenXml.Packaging's own PartUriHelper
// caches ValidatedPartUri lookups in a SortedList and uses IndexOfKey to
// check membership without a second key comparison.
func sortedListIndexOfKey(args []runtime.Value) (runtime.Value, error) {
	sl, err := asSortedList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	key, err := dictKeyValue(args, 1)
	if err != nil {
		return runtime.Value{}, err
	}
	idx, found, err := sl.find(key)
	if err != nil {
		return runtime.Value{}, err
	}
	if !found {
		return runtime.Int32(-1), nil
	}
	return runtime.Int32(int32(idx)), nil
}

func sortedListClear(args []runtime.Value) (runtime.Value, error) {
	sl, err := asSortedList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	sl.keys = sl.keys[:0]
	sl.values = sl.values[:0]
	return runtime.Value{}, nil
}

func sortedListTryGetValue(args []runtime.Value) (runtime.Value, error) {
	sl, err := asSortedList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	key, err := dictKeyValue(args, 1)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 3 || args[2].Kind != runtime.KindRef || args[2].Ref == nil {
		return runtime.Value{}, fmt.Errorf("bcl: SortedList.TryGetValue expects an out parameter")
	}
	idx, found, err := sl.find(key)
	if err != nil {
		return runtime.Value{}, err
	}
	if found {
		*args[2].Ref = sl.values[idx]
	} else {
		*args[2].Ref = runtime.Null()
	}
	return runtime.Bool(found), nil
}

// listCopyTo backs ICollection.CopyTo(Array array, int index) for
// List`1/ArrayList (and, transitively, SortedList.Values/.Keys, both
// snapshotted as a plain List — Fase 3.39). Silently stops at the
// destination array's own bounds rather than erroring: real CopyTo
// throws ArgumentException for a too-small destination, a case no real
// caller in this loop's target packages has been found to hit (they
// always pre-size the destination from the same Count being copied).
func listCopyTo(args []runtime.Value) (runtime.Value, error) {
	l, err := asList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 3 || args[1].Kind != runtime.KindArray || args[1].Arr == nil || args[2].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: List.CopyTo expects (array, index)")
	}
	start := int(args[2].I4)
	dst := args[1].Arr.Elems
	for i, v := range l.items {
		if start+i >= len(dst) {
			break
		}
		dst[start+i] = v
	}
	return runtime.Value{}, nil
}
