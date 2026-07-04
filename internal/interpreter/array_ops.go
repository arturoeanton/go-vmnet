package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// Common System.Array static members beyond Sort/BinarySearch (Fase
// 3.42, general IL/BCL hardening pass — not tied to any one target
// package, just broad real-world .NET Core surface coverage). Each of
// Find/FindIndex/FindAll/Exists/ForEach/TrueForAll/ConvertAll takes a
// Predicate<T>/Action<T>/Converter<T,TOutput> delegate argument, needing
// Machine access the same way Array.Sort's own Comparison<T> overload
// does (array_sort.go) — registered here, a separate file, purely to
// keep that one focused on ordering/searching.
func init() {
	machineRegistry["System.Array::Reverse"] = arrayReverse
	machineRegistry["System.Array::Fill"] = arrayFill
	machineRegistry["System.Array::Find"] = arrayFind
	machineRegistry["System.Array::FindLast"] = arrayFindLast
	machineRegistry["System.Array::FindIndex"] = arrayFindIndex
	machineRegistry["System.Array::FindAll"] = arrayFindAll
	machineRegistry["System.Array::Exists"] = arrayExists
	machineRegistry["System.Array::ForEach"] = arrayForEach
	machineRegistry["System.Array::TrueForAll"] = arrayTrueForAll
	machineRegistry["System.Array::ConvertAll"] = arrayConvertAll
	machineRegistry["System.Array::LastIndexOf"] = arrayLastIndexOf
	// List<T>.RemoveAll(Predicate<T>) needs the exact same Machine-aware
	// delegate invocation as Array.Find/FindAll above — found via a real,
	// common in-memory-collection-maintenance pattern (examples/
	// dapper-demo's own fake in-memory table prunes rows this way).
	machineRegistry["System.Collections.Generic.List`1::RemoveAll"] = listRemoveAll
}

// listRemoveAll backs List<T>.RemoveAll(Predicate<T> match) — removes
// every element the predicate answers true for, returning the real
// count removed (matching List<T>.RemoveAll's own return value).
// Rebuilds the list's backing slice via bcl.SetNativeListItems rather
// than mutating in place, the same approach listSort takes for its own
// Machine-aware List<T> member.
func listRemoveAll(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: List.RemoveAll expects (this, predicate)")
	}
	recv := args[0]
	if recv.Kind == runtime.KindRef && recv.Ref != nil {
		recv = *recv.Ref
	}
	if recv.Kind != runtime.KindObject || recv.Obj == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: List.RemoveAll called without a receiver")
	}
	items, ok := bcl.NativeListItems(recv.Obj.Native)
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: List.RemoveAll: receiver is not a List<T>")
	}
	kept := make([]runtime.Value, 0, len(items))
	removed := 0
	for _, e := range items {
		match, err := m.arrayPredicate(args[1], e, depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		if match {
			removed++
			continue
		}
		kept = append(kept, e)
	}
	bcl.SetNativeListItems(recv.Obj.Native, kept)
	return runtime.Int32(int32(removed)), nil
}

// arrayReverse covers both Reverse(array) (the whole array) and
// Reverse(array, index, length) (a sub-range) — real .NET semantics.
func arrayReverse(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindArray || args[0].Arr == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: Array.Reverse expects an array argument")
	}
	elems := args[0].Arr.Elems
	start, end := 0, len(elems)
	if len(args) >= 3 && args[1].Kind == runtime.KindI4 && args[2].Kind == runtime.KindI4 {
		start = int(args[1].I4)
		end = start + int(args[2].I4)
		if start < 0 || end > len(elems) || end < start {
			return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "index or length"}
		}
	}
	for i, j := start, end-1; i < j; i, j = i+1, j-1 {
		elems[i], elems[j] = elems[j], elems[i]
	}
	return runtime.Value{}, nil
}

// arrayFill covers Fill(array, value) and Fill(array, value, start,
// count).
func arrayFill(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 2 || args[0].Kind != runtime.KindArray || args[0].Arr == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: Array.Fill expects (array, value[, start, count])")
	}
	elems := args[0].Arr.Elems
	value := args[1]
	start, end := 0, len(elems)
	if len(args) >= 4 && args[2].Kind == runtime.KindI4 && args[3].Kind == runtime.KindI4 {
		start = int(args[2].I4)
		end = start + int(args[3].I4)
		if start < 0 || end > len(elems) || end < start {
			return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "startIndex or count"}
		}
	}
	for i := start; i < end; i++ {
		elems[i] = value
	}
	return runtime.Value{}, nil
}

// arrayPredicate invokes a Predicate<T> delegate argument, returning its
// bool result — the shape Find/FindAll/Exists/TrueForAll all share.
func (m *Machine) arrayPredicate(fn runtime.Value, elem runtime.Value, depth int, instrCount *int64) (bool, error) {
	if fn.Kind != runtime.KindFunc || fn.Func == nil {
		return false, fmt.Errorf("interpreter: Array predicate/action argument is not a delegate")
	}
	v, _, err := m.invokeFunc(fn.Func, []runtime.Value{elem}, depth, instrCount)
	if err != nil {
		return false, err
	}
	return v.Truthy(), nil
}

// arrayFind returns the first matching element, or Null() (a real
// default(T) approximation, same documented posture as LINQ's own
// FirstOrDefault — see linq.go's doc comment) if nothing matches.
func arrayFind(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 || args[0].Kind != runtime.KindArray || args[0].Arr == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: Array.Find expects (array, predicate)")
	}
	for _, e := range args[0].Arr.Elems {
		ok, err := m.arrayPredicate(args[1], e, depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		if ok {
			return e, nil
		}
	}
	return runtime.Null(), nil
}

func arrayFindLast(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 || args[0].Kind != runtime.KindArray || args[0].Arr == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: Array.FindLast expects (array, predicate)")
	}
	elems := args[0].Arr.Elems
	for i := len(elems) - 1; i >= 0; i-- {
		ok, err := m.arrayPredicate(args[1], elems[i], depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		if ok {
			return elems[i], nil
		}
	}
	return runtime.Null(), nil
}

func arrayFindIndex(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 || args[0].Kind != runtime.KindArray || args[0].Arr == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: Array.FindIndex expects (array, predicate)")
	}
	for i, e := range args[0].Arr.Elems {
		ok, err := m.arrayPredicate(args[1], e, depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		if ok {
			return runtime.Int32(int32(i)), nil
		}
	}
	return runtime.Int32(-1), nil
}

func arrayFindAll(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 || args[0].Kind != runtime.KindArray || args[0].Arr == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: Array.FindAll expects (array, predicate)")
	}
	var out []runtime.Value
	for _, e := range args[0].Arr.Elems {
		ok, err := m.arrayPredicate(args[1], e, depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		if ok {
			out = append(out, e)
		}
	}
	return runtime.ArrRef(&runtime.Array{Elems: out}), nil
}

func arrayExists(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 || args[0].Kind != runtime.KindArray || args[0].Arr == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: Array.Exists expects (array, predicate)")
	}
	for _, e := range args[0].Arr.Elems {
		ok, err := m.arrayPredicate(args[1], e, depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		if ok {
			return runtime.Bool(true), nil
		}
	}
	return runtime.Bool(false), nil
}

func arrayTrueForAll(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 || args[0].Kind != runtime.KindArray || args[0].Arr == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: Array.TrueForAll expects (array, predicate)")
	}
	for _, e := range args[0].Arr.Elems {
		ok, err := m.arrayPredicate(args[1], e, depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		if !ok {
			return runtime.Bool(false), nil
		}
	}
	return runtime.Bool(true), nil
}

func arrayForEach(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 || args[0].Kind != runtime.KindArray || args[0].Arr == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: Array.ForEach expects (array, action)")
	}
	if args[1].Kind != runtime.KindFunc || args[1].Func == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: Array.ForEach action argument is not a delegate")
	}
	for _, e := range args[0].Arr.Elems {
		if _, _, err := m.invokeFunc(args[1].Func, []runtime.Value{e}, depth, instrCount); err != nil {
			return runtime.Value{}, err
		}
	}
	return runtime.Value{}, nil
}

// arrayConvertAll backs Array.ConvertAll<TInput,TOutput> — vmnet's
// runtime.Value is already a uniform tagged union across every real
// generic instantiation, so the converter's own return Kind becomes each
// output element's Kind directly, with no separate typed-array identity
// to preserve (same documented approximation as LINQ's Select).
func arrayConvertAll(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 || args[0].Kind != runtime.KindArray || args[0].Arr == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: Array.ConvertAll expects (array, converter)")
	}
	if args[1].Kind != runtime.KindFunc || args[1].Func == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: Array.ConvertAll converter argument is not a delegate")
	}
	elems := args[0].Arr.Elems
	out := make([]runtime.Value, len(elems))
	for i, e := range elems {
		v, _, err := m.invokeFunc(args[1].Func, []runtime.Value{e}, depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		out[i] = v
	}
	return runtime.ArrRef(&runtime.Array{Elems: out}), nil
}

// arrayLastIndexOf mirrors Array.IndexOf (already registered elsewhere)
// but scans from the end — real overloads with startIndex/count aren't
// covered here (no real caller found needing them yet), matching this
// codebase's established "cover what's actually used, extend later"
// posture.
func arrayLastIndexOf(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 2 || args[0].Kind != runtime.KindArray || args[0].Arr == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: Array.LastIndexOf expects (array, value)")
	}
	elems := args[0].Arr.Elems
	for i := len(elems) - 1; i >= 0; i-- {
		if valuesDeepEqual(elems[i], args[1]) {
			return runtime.Int32(int32(i)), nil
		}
	}
	return runtime.Int32(-1), nil
}
