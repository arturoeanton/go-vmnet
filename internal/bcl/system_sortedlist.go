package bcl

import (
	"fmt"

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
	keys   []runtime.Value
	values []runtime.Value
}

func init() {
	registerCtor("System.Collections.SortedList", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeSortedList{}}, nil
	})
	register("System.Collections.SortedList::get_Item", true, sortedListGetItem)
	register("System.Collections.SortedList::set_Item", false, sortedListSetItem)
	register("System.Collections.SortedList::get_Count", true, sortedListGetCount)
	// Remove is void on SortedList (unlike Dictionary`2.Remove's bool) —
	// a missing key is silently a no-op, matching real semantics.
	register("System.Collections.SortedList::Remove", false, sortedListRemove)
	register("System.Collections.SortedList::get_Values", true, sortedListGetValues)
	register("System.Collections.SortedList::get_Keys", true, sortedListGetKeys)
	register("System.Collections.SortedList::ContainsKey", true, sortedListContainsKey)
	register("System.Collections.SortedList::Contains", true, sortedListContainsKey)
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

// compareSortedListKeys orders two SortedList keys — string/int32/int64
// only, the same real-world scope encodeDictKey documents for
// Dictionary/Hashtable (Fase 3.38/3.39): every real key found in this
// loop's target packages is one of these three.
func compareSortedListKeys(a, b runtime.Value) (int, error) {
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
