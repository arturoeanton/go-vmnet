package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// nativeHashSet backs both HashSet<T> and SortedSet<T> (Fase-3.39-style
// typeName field, same reasoning as nativeList/nativeDict/nativeStack's
// own typeName: NativeTypeName must be able to tell the two real BCL
// types apart, or an interface-declared call site — LINQ's own
// enumerateAll fallback included — redirects a SortedSet<T> receiver to
// "HashSet`1::GetEnumerator" instead of its own real
// "SortedSet`1::GetEnumerator", and vice versa). Deduplication/Contains
// use a linear scan with valuesEqual (system_object.go), not a real Go
// map keyed by value — vmnet's runtime.Value isn't inherently hashable/
// comparable in Go's map-key sense (a KindStruct/KindObject would need
// canonicalizing first). O(n) instead of O(1) for large sets, same
// pragmatic simplification already accepted for List<T>.Contains;
// correctness (no duplicates observed, SortedSet's own iteration order)
// holds regardless of the data structure behind it.
//
// sorted (true only for SortedSet<T>) makes Add insert at its sorted
// position (compareValuesNatural) instead of appending at the end, so
// items is always kept in ascending order — SortedSet<T>'s single
// defining, observable difference from HashSet<T>. Neither constructor's
// IComparer<T> overload is wired up (a custom order/equality argument is
// silently ignored, same documented gap as HashSet<T>'s own comparer
// constructor argument) — this package has no Machine access to dispatch
// one, and no real target package's usage of it was found in this pass.
type nativeHashSet struct {
	items    []runtime.Value
	typeName string
	sorted   bool
}

// hashSetEnumeratorType/sortedSetEnumeratorType mirror queueEnumeratorType
// exactly (system_queue.go's own doc comment explains why the exact real
// struct name matters: a direct `foreach (var x in hashSet)` calls it
// non-virtually by name) — one per real BCL type nativeHashSet backs.
var hashSetEnumeratorType = runtime.NewValueType(
	"System.Collections.Generic", "HashSet`1+Enumerator",
	[]string{"set", "index"},
	[]runtime.Value{runtime.Null(), runtime.Int32(-1)},
)

var sortedSetEnumeratorType = runtime.NewValueType(
	"System.Collections.Generic", "SortedSet`1+Enumerator",
	[]string{"set", "index"},
	[]runtime.Value{runtime.Null(), runtime.Int32(-1)},
)

func init() {
	registerCtor("System.Collections.Generic.HashSet`1", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeHashSet{typeName: "System.Collections.Generic.HashSet`1"}}, nil
	})
	// A plugin/BCL-package class subclassing HashSet<T> directly chains to
	// its base via a plain, non-virtual `call HashSet\`1::.ctor(this[,
	// args])` — not `newobj` (Fase 3.83, found via ClosedXML/OpenXml's own
	// real internals: this native was simply missing entirely, unlike
	// List`1/Dictionary`2's own long-registered in-place ctors for this
	// exact pattern — surfaced as a hard "unsupported BCL method" crash
	// the moment Fase 3.83's own List<T>/ArrayList real-enumeration fix
	// started actually running real downstream code paths that used to
	// silently no-op against an always-empty list). Any real constructor
	// argument (capacity, an IEnumerable<T>/IComparer<T> to seed from) is
	// ignored, same documented scope every other in-place ctor native in
	// this codebase already accepts (listCtorInPlace, dictCtorInPlace) —
	// turns a hard crash back into the same "silently empty" gap
	// List<T>'s own newobj path had before its own Fase 3.83 fix, not a
	// wrong-data regression this specific case has been found to need yet.
	register("System.Collections.Generic.HashSet`1::.ctor", false, hashSetCtorInPlace)
	register("System.Collections.Generic.HashSet`1::Add", true, hashSetAdd)
	register("System.Collections.Generic.HashSet`1::Contains", true, hashSetContains)
	register("System.Collections.Generic.HashSet`1::Remove", true, hashSetRemove)
	register("System.Collections.Generic.HashSet`1::Clear", false, hashSetClear)
	register("System.Collections.Generic.HashSet`1::get_Count", true, hashSetCount)
	register("System.Collections.Generic.HashSet`1::get_IsReadOnly", true, alwaysFalseBool)
	register("System.Collections.Generic.HashSet`1::GetEnumerator", true, hashSetGetEnumerator)
	register("System.Collections.Generic.HashSet`1+Enumerator::MoveNext", true, hashSetEnumeratorMoveNext)
	register("System.Collections.Generic.HashSet`1+Enumerator::get_Current", true, hashSetEnumeratorGetCurrent)

	// SortedSet<T> — missing entirely before this hardening pass (grepping
	// the whole repo for "SortedSet" found nothing at all). Reuses every
	// one of HashSet<T>'s own method implementations verbatim (none of
	// them care about ordering except Add, which branches on hs.sorted)
	// rather than duplicating them under new names.
	registerCtor("System.Collections.Generic.SortedSet`1", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeHashSet{typeName: "System.Collections.Generic.SortedSet`1", sorted: true}}, nil
	})
	// Same in-place base-chaining ctor HashSet`1 needed above, mirrored
	// for its sorted sibling.
	register("System.Collections.Generic.SortedSet`1::.ctor", false, sortedSetCtorInPlace)
	register("System.Collections.Generic.SortedSet`1::Add", true, hashSetAdd)
	register("System.Collections.Generic.SortedSet`1::Contains", true, hashSetContains)
	register("System.Collections.Generic.SortedSet`1::Remove", true, hashSetRemove)
	register("System.Collections.Generic.SortedSet`1::Clear", false, hashSetClear)
	register("System.Collections.Generic.SortedSet`1::get_Count", true, hashSetCount)
	register("System.Collections.Generic.SortedSet`1::get_Min", true, sortedSetGetMin)
	register("System.Collections.Generic.SortedSet`1::get_Max", true, sortedSetGetMax)
	register("System.Collections.Generic.SortedSet`1::GetEnumerator", true, hashSetGetEnumerator)
	register("System.Collections.Generic.SortedSet`1+Enumerator::MoveNext", true, hashSetEnumeratorMoveNext)
	register("System.Collections.Generic.SortedSet`1+Enumerator::get_Current", true, hashSetEnumeratorGetCurrent)
}

// NewHashSetValue wraps items (already deduplicated by the caller — LINQ's
// own ToHashSet, internal/interpreter/linq.go) as a real HashSet<T>-shaped
// value, the same way NewListValue backs every other LINQ terminal method.
func NewHashSetValue(items []runtime.Value) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeHashSet{items: items, typeName: "System.Collections.Generic.HashSet`1"}})
}

func asHashSet(args []runtime.Value) (*nativeHashSet, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return nil, fmt.Errorf("bcl: HashSet method called without a receiver")
	}
	hs, ok := args[0].Obj.Native.(*nativeHashSet)
	if !ok {
		return nil, fmt.Errorf("bcl: receiver is not a HashSet")
	}
	return hs, nil
}

// hashSetCtorInPlace/sortedSetCtorInPlace back HashSet`1/SortedSet`1's own
// in-place ".ctor" (Fase 3.83) — see their own register() call's doc
// comment above for why this exists at all. Mirrors listCtorInPlace/
// dictCtorInPlace's exact "ignore every argument, just wire up the
// receiver's own Native backing" shape.
func hashSetCtorInPlace(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return runtime.Value{}, fmt.Errorf("bcl: HashSet`1 constructor called without a receiver")
	}
	args[0].Obj.Native = &nativeHashSet{typeName: "System.Collections.Generic.HashSet`1"}
	return runtime.Value{}, nil
}

func sortedSetCtorInPlace(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return runtime.Value{}, fmt.Errorf("bcl: SortedSet`1 constructor called without a receiver")
	}
	args[0].Obj.Native = &nativeHashSet{typeName: "System.Collections.Generic.SortedSet`1", sorted: true}
	return runtime.Value{}, nil
}

// hashSetAdd returns whether the item was newly added (real HashSet<T>.Add
// return value — SortedSet<T>.Add shares this same return shape), false
// if it was already present. A SortedSet<T> receiver (hs.sorted) inserts
// at its natural-order position instead of appending at the end, keeping
// items always in ascending order — SortedSet<T>'s one defining
// behavioral difference from HashSet<T>.
func hashSetAdd(args []runtime.Value) (runtime.Value, error) {
	hs, err := asHashSet(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: HashSet.Add expects 1 argument")
	}
	for _, item := range hs.items {
		if valuesEqual(item, args[1]) {
			return runtime.Bool(false), nil
		}
	}
	if hs.sorted {
		i := 0
		for ; i < len(hs.items); i++ {
			if compareValuesNatural(args[1], hs.items[i]) < 0 {
				break
			}
		}
		hs.items = append(hs.items, runtime.Value{})
		copy(hs.items[i+1:], hs.items[i:])
		hs.items[i] = args[1]
		return runtime.Bool(true), nil
	}
	hs.items = append(hs.items, args[1])
	return runtime.Bool(true), nil
}

func hashSetContains(args []runtime.Value) (runtime.Value, error) {
	hs, err := asHashSet(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: HashSet.Contains expects 1 argument")
	}
	for _, item := range hs.items {
		if valuesEqual(item, args[1]) {
			return runtime.Bool(true), nil
		}
	}
	return runtime.Bool(false), nil
}

func hashSetRemove(args []runtime.Value) (runtime.Value, error) {
	hs, err := asHashSet(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: HashSet.Remove expects 1 argument")
	}
	for i, item := range hs.items {
		if valuesEqual(item, args[1]) {
			hs.items = append(hs.items[:i], hs.items[i+1:]...)
			return runtime.Bool(true), nil
		}
	}
	return runtime.Bool(false), nil
}

func hashSetClear(args []runtime.Value) (runtime.Value, error) {
	hs, err := asHashSet(args)
	if err != nil {
		return runtime.Value{}, err
	}
	hs.items = nil
	return runtime.Value{}, nil
}

func hashSetCount(args []runtime.Value) (runtime.Value, error) {
	hs, err := asHashSet(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(int32(len(hs.items))), nil
}

// sortedSetGetMin/sortedSetGetMax are SortedSet<T>-only properties (no
// HashSet<T> equivalent) — items is already kept sorted ascending by
// hashSetAdd, so the first/last element IS the min/max; real SortedSet<T>
// returns default(T) on an empty set rather than throwing.
func sortedSetGetMin(args []runtime.Value) (runtime.Value, error) {
	hs, err := asHashSet(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(hs.items) == 0 {
		return runtime.Null(), nil
	}
	return hs.items[0], nil
}

func sortedSetGetMax(args []runtime.Value) (runtime.Value, error) {
	hs, err := asHashSet(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(hs.items) == 0 {
		return runtime.Null(), nil
	}
	return hs.items[len(hs.items)-1], nil
}

// hashSetGetEnumerator backs both HashSet<T> and SortedSet<T> — the
// returned struct's own type (hashSetEnumeratorType vs
// sortedSetEnumeratorType) is picked by typeName so a direct `foreach`'s
// non-virtual MoveNext/get_Current call (see queueEnumeratorType's doc
// comment) lands on the receiver's real concrete enumerator type either
// way.
func hashSetGetEnumerator(args []runtime.Value) (runtime.Value, error) {
	hs, err := asHashSet(args)
	if err != nil {
		return runtime.Value{}, err
	}
	t := hashSetEnumeratorType
	if hs.sorted {
		t = sortedSetEnumeratorType
	}
	s := runtime.NewStruct(t)
	s.Fields[0] = args[0]
	return runtime.StructVal(s), nil
}

func hashSetEnumeratorMoveNext(args []runtime.Value) (runtime.Value, error) {
	s, err := derefStructReceiver(args, "HashSet.Enumerator", "HashSet.Enumerator.MoveNext")
	if err != nil {
		return runtime.Value{}, err
	}
	hs, err := asHashSet([]runtime.Value{s.Fields[0]})
	if err != nil {
		return runtime.Value{}, err
	}
	next := s.Fields[1].I4 + 1
	s.Fields[1] = runtime.Int32(next)
	return runtime.Bool(int(next) < len(hs.items)), nil
}

func hashSetEnumeratorGetCurrent(args []runtime.Value) (runtime.Value, error) {
	s, err := derefStructReceiver(args, "HashSet.Enumerator", "HashSet.Enumerator.Current")
	if err != nil {
		return runtime.Value{}, err
	}
	hs, err := asHashSet([]runtime.Value{s.Fields[0]})
	if err != nil {
		return runtime.Value{}, err
	}
	idx := int(s.Fields[1].I4)
	if idx < 0 || idx >= len(hs.items) {
		return runtime.Value{}, fmt.Errorf("bcl: HashSet.Enumerator.Current: index %d out of range", idx)
	}
	return hs.items[idx], nil
}
