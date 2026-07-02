package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// nativeList backs List<T> for any T: vmnet's runtime.Value is already a
// uniform tagged union, so there's no need to specialize on the generic
// argument (spec §17.1's minimal generics scope).
type nativeList struct {
	items []runtime.Value
}

// nativeDict backs Dictionary<TKey,TValue>. Fase 2 only supports string
// keys (spec §17.1 lists Dictionary<string,string>/<string,object> as the
// initial cases) — a documented, not accidental, limitation.
type nativeDict struct {
	m map[string]runtime.Value
}

func init() {
	registerCtor("System.Collections.Generic.List`1", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeList{}}, nil
	})
	register("System.Collections.Generic.List`1::Add", false, listAdd)
	register("System.Collections.Generic.List`1::get_Count", true, listCount)
	register("System.Collections.Generic.List`1::get_Item", true, listGetItem)

	registerCtor("System.Collections.Generic.Dictionary`2", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeDict{m: map[string]runtime.Value{}}}, nil
	})
	register("System.Collections.Generic.Dictionary`2::Add", false, dictAdd)
	register("System.Collections.Generic.Dictionary`2::get_Item", true, dictGetItem)
	register("System.Collections.Generic.Dictionary`2::set_Item", false, dictSetItem)
	register("System.Collections.Generic.Dictionary`2::ContainsKey", true, dictContainsKey)
	register("System.Collections.Generic.Dictionary`2::get_Count", true, dictCount)
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

func dictKey(args []runtime.Value, i int) (string, error) {
	if len(args) <= i || args[i].Kind != runtime.KindString {
		return "", fmt.Errorf("bcl: Dictionary key must be a string (Fase 2 limitation)")
	}
	return args[i].Str, nil
}

func dictAdd(args []runtime.Value) (runtime.Value, error) {
	d, err := asDict(args)
	if err != nil {
		return runtime.Value{}, err
	}
	key, err := dictKey(args, 1)
	if err != nil {
		return runtime.Value{}, err
	}
	if _, exists := d.m[key]; exists {
		return runtime.Value{}, fmt.Errorf("bcl: Dictionary already contains key %q", key)
	}
	d.m[key] = args[2]
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
	v, ok := d.m[key]
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: Dictionary has no key %q", key)
	}
	return v, nil
}

func dictSetItem(args []runtime.Value) (runtime.Value, error) {
	d, err := asDict(args)
	if err != nil {
		return runtime.Value{}, err
	}
	key, err := dictKey(args, 1)
	if err != nil {
		return runtime.Value{}, err
	}
	d.m[key] = args[2]
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

func dictCount(args []runtime.Value) (runtime.Value, error) {
	d, err := asDict(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(int32(len(d.m))), nil
}
