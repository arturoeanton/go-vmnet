package interpreter

import (
	"fmt"
	"sort"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// Array.Sort, Array.BinarySearch and List<T>.Sort all need Machine access
// for their IComparer<T>/Comparison<T> overloads (comparer.go's
// compareFunc, which may need to invoke a delegate or dispatch a real
// Compare implementation) — none can be a plain bcl.Native. List<T>.Sort
// was found missing entirely (this hardening pass's own probe: hand-
// written fixtures for `arr.Sort()`/`Array.Sort(arr, cmp)`/
// `list.Sort(comparer)`, only the last of which had no native registered
// anywhere).
func init() {
	machineRegistry["System.Array::Sort"] = arraySort
	// Array.BinarySearch needs the exact same Machine-aware comparer
	// dispatch as Sort (Fase 3.41, found via a real, load-bearing case:
	// DocumentFormat.OpenXml.Framework.Metadata.ElementFactoryCollection.
	// Create(in qname) — the real per-element lookup ElementFactory
	// child-element resolution (elementfactory.go) runs through on every
	// real element parsed from a real .xlsx's XML — does
	// `Array.BinarySearch(_data, new ElementFactory(...), ElementChildNameComparer.Instance)`
	// against an array already sorted by that same real comparer in its
	// own ctor, via Array.Sort just above).
	machineRegistry["System.Array::BinarySearch"] = arrayBinarySearch
	machineRegistry["System.Collections.Generic.List`1::Sort"] = listSort
}

// arrayBinarySearch backs both Array.BinarySearch(array, value) and
// Array.BinarySearch(array, value, comparer) — real .NET semantics: the
// array must already be sorted ascending by the same ordering, and the
// result is either the found index or the two's-complement (^index) of
// where value would need to be inserted to keep it sorted.
func arrayBinarySearch(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 2 || args[0].Kind != runtime.KindArray || args[0].Arr == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: Array.BinarySearch expects (array, value[, comparer])")
	}
	elems := args[0].Arr.Elems
	value := args[1]
	var comparerArg *runtime.Value
	if len(args) >= 3 && args[2].Kind != runtime.KindNull {
		comparerArg = &args[2]
	}
	cmp, err := m.compareFunc(comparerArg, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	lo, hi := 0, len(elems)-1
	for lo <= hi {
		mid := int(uint(lo+hi) >> 1)
		c, err := cmp(elems[mid], value)
		if err != nil {
			return runtime.Value{}, err
		}
		switch {
		case c == 0:
			return runtime.Int32(int32(mid)), nil
		case c < 0:
			lo = mid + 1
		default:
			hi = mid - 1
		}
	}
	return runtime.Int32(int32(^lo)), nil
}

// arraySort backs Array.Sort(array), Array.Sort(array, comparer) and
// Array.Sort(array, comparison) — real .NET's Array.Sort is an unstable
// introspective sort (a quicksort/heapsort/insertion-sort hybrid); this
// uses sort.SliceStable instead, a documented simplification also taken
// by List<T>.Sort (listSort, just below): for a comparer that never
// returns 0 between two distinct elements (the overwhelmingly common
// real case — sorting by a genuinely unique key) the two are
// indistinguishable; only a comparer with real ties could observe the
// difference, and even then only as "which of several correctly-sorted
// orderings" rather than a wrong one.
func arraySort(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 || len(args) > 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: Array.Sort expects (array[, comparerOrComparison])")
	}
	arr := args[0]
	if arr.Kind == runtime.KindRef && arr.Ref != nil {
		arr = *arr.Ref
	}
	if arr.Kind != runtime.KindArray {
		return runtime.Value{}, fmt.Errorf("interpreter: Array.Sort expects an array receiver")
	}
	if arr.Arr == nil || len(arr.Arr.Elems) < 2 {
		return runtime.Value{}, nil
	}
	var comparerArg *runtime.Value
	if len(args) == 2 {
		comparerArg = &args[1]
	}
	less, err := m.compareFunc(comparerArg, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	elems := arr.Arr.Elems
	var sortErr error
	sort.SliceStable(elems, func(i, j int) bool {
		c, err := less(elems[i], elems[j])
		if err != nil {
			sortErr = err
			return false
		}
		return c < 0
	})
	if sortErr != nil {
		return runtime.Value{}, sortErr
	}
	return runtime.Value{}, nil
}

// listSort backs List<T>.Sort()/Sort(IComparer<T>)/Sort(Comparison<T>) —
// same three overload shapes and the same SliceStable-instead-of-real-
// unstable-sort simplification as arraySort just above. Sorts a copy and
// writes it back via bcl.SetNativeListItems rather than sorting
// bcl.NativeListItems' returned slice in place: NativeListItems is
// documented as handing back the list's real backing slice (not a copy)
// for LINQ's own fast-path reads, so mutating it through a *different*
// alias here would still happen to work today, but relying on that
// aliasing accidentally would silently break the moment NativeListItems'
// own contract changes to return a defensive copy instead.
func listSort(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 || len(args) > 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: List.Sort expects (this[, comparerOrComparison])")
	}
	recv := args[0]
	if recv.Kind == runtime.KindRef && recv.Ref != nil {
		recv = *recv.Ref
	}
	if recv.Kind != runtime.KindObject || recv.Obj == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: List.Sort called without a receiver")
	}
	items, ok := bcl.NativeListItems(recv.Obj.Native)
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: List.Sort: receiver is not a List<T>")
	}
	var comparerArg *runtime.Value
	if len(args) == 2 {
		comparerArg = &args[1]
	}
	less, err := m.compareFunc(comparerArg, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	sorted := make([]runtime.Value, len(items))
	copy(sorted, items)
	var sortErr error
	sort.SliceStable(sorted, func(i, j int) bool {
		c, err := less(sorted[i], sorted[j])
		if err != nil {
			sortErr = err
			return false
		}
		return c < 0
	})
	if sortErr != nil {
		return runtime.Value{}, sortErr
	}
	bcl.SetNativeListItems(recv.Obj.Native, sorted)
	return runtime.Value{}, nil
}
