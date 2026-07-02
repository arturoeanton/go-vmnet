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

func init() {
	registerCtor("System.Collections.Generic.List`1", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeList{}}, nil
	})
	register("System.Collections.Generic.List`1::Add", false, listAdd)
	register("System.Collections.Generic.List`1::get_Count", true, listCount)
	register("System.Collections.Generic.List`1::get_Item", true, listGetItem)
	register("System.Collections.Generic.List`1::GetEnumerator", true, listGetEnumerator)
	register("System.Collections.Generic.List`1+Enumerator::MoveNext", true, listEnumeratorMoveNext)
	register("System.Collections.Generic.List`1+Enumerator::get_Current", true, listEnumeratorGetCurrent)

	registerCtor("System.Collections.Generic.Dictionary`2", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeDict{m: map[string]runtime.Value{}}}, nil
	})
	register("System.Collections.Generic.Dictionary`2::Add", false, dictAdd)
	register("System.Collections.Generic.Dictionary`2::get_Item", true, dictGetItem)
	register("System.Collections.Generic.Dictionary`2::set_Item", false, dictSetItem)
	register("System.Collections.Generic.Dictionary`2::ContainsKey", true, dictContainsKey)
	register("System.Collections.Generic.Dictionary`2::get_Count", true, dictCount)
	register("System.Collections.Generic.Dictionary`2::GetEnumerator", true, dictGetEnumerator)
	register("System.Collections.Generic.Dictionary`2+Enumerator::MoveNext", true, dictEnumeratorMoveNext)
	register("System.Collections.Generic.Dictionary`2+Enumerator::get_Current", true, dictEnumeratorGetCurrent)

	register("System.Collections.Generic.KeyValuePair`2::get_Key", true, keyValuePairGetKey)
	register("System.Collections.Generic.KeyValuePair`2::get_Value", true, keyValuePairGetValue)

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
	keys := make([]runtime.Value, 0, len(d.m))
	for k := range d.m {
		keys = append(keys, runtime.String(k))
	}
	s := runtime.NewStruct(dictEnumeratorType)
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
		return runtime.Value{}, fmt.Errorf("bcl: Dictionary.Enumerator.Current: index %d out of range", idx)
	}
	key := keys[idx].Str
	kv := runtime.NewStruct(keyValuePairType)
	kv.Fields[0] = runtime.String(key)
	kv.Fields[1] = d.m[key]
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
