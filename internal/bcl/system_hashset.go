package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// nativeHashSet backs HashSet<T>. Deduplication/Contains use a linear
// scan with valuesEqual (system_object.go), not a real Go map keyed by
// value — vmnet's runtime.Value isn't inherently hashable/comparable in
// Go's map-key sense (a KindStruct/KindObject would need canonicalizing
// first). O(n) instead of O(1) for large sets, same pragmatic
// simplification already accepted for List<T>.Contains; correctness (no
// duplicates observed) holds regardless of the data structure behind it.
type nativeHashSet struct {
	items []runtime.Value
}

// hashSetEnumeratorType mirrors listEnumeratorType (Fase 3.11) exactly —
// HashSet<T>.GetEnumerator() also returns a value-type struct enumerator,
// confirmed against real IL before assuming so.
var hashSetEnumeratorType = runtime.NewValueType(
	"System.Collections.Generic", "HashSet`1+Enumerator",
	[]string{"set", "index"},
	[]runtime.Value{runtime.Null(), runtime.Int32(-1)},
)

func init() {
	registerCtor("System.Collections.Generic.HashSet`1", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeHashSet{}}, nil
	})
	register("System.Collections.Generic.HashSet`1::Add", true, hashSetAdd)
	register("System.Collections.Generic.HashSet`1::Contains", true, hashSetContains)
	register("System.Collections.Generic.HashSet`1::Remove", true, hashSetRemove)
	register("System.Collections.Generic.HashSet`1::Clear", false, hashSetClear)
	register("System.Collections.Generic.HashSet`1::get_Count", true, hashSetCount)
	register("System.Collections.Generic.HashSet`1::GetEnumerator", true, hashSetGetEnumerator)
	register("System.Collections.Generic.HashSet`1+Enumerator::MoveNext", true, hashSetEnumeratorMoveNext)
	register("System.Collections.Generic.HashSet`1+Enumerator::get_Current", true, hashSetEnumeratorGetCurrent)
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

// hashSetAdd returns whether the item was newly added (real HashSet<T>.Add
// return value), false if it was already present.
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

func hashSetGetEnumerator(args []runtime.Value) (runtime.Value, error) {
	if _, err := asHashSet(args); err != nil {
		return runtime.Value{}, err
	}
	s := runtime.NewStruct(hashSetEnumeratorType)
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
