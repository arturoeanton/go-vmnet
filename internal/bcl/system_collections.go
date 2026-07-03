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
		return &runtime.Object{Native: &nativeDict{m: map[string]runtime.Value{}}}, nil
	})
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
	return runtime.ObjRef(&runtime.Object{Native: &nativeList{items: items}})
}

// NativeListItems returns a native-backed List<T>'s items, if native is
// one — used by LINQ's enumerateAll (internal/interpreter/linq.go) as a
// direct fast path (skip driving a real GetEnumerator/MoveNext/
// get_Current loop when the elements are already a Go slice).
func NativeListItems(native any) ([]runtime.Value, bool) {
	l, ok := native.(*nativeList)
	if !ok {
		return nil, false
	}
	return l.items, true
}

// NewDictValue wraps pairs as a real Dictionary<string,V>-shaped value
// (string keys only — see nativeDict's doc comment) — used by LINQ's
// ToDictionary (internal/interpreter/linq.go, Fase 3.32), which needs to
// build a real Dictionary instance without importing bcl's own
// unexported nativeDict type.
func NewDictValue(pairs map[string]runtime.Value) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeDict{m: pairs}})
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
			if ol, ok := other.Obj.Native.(*nativeList); ok {
				l.items = append(l.items, ol.items...)
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
	v, ok := d.m[key]
	if !ok {
		v = runtime.Null()
	}
	*args[2].Ref = v
	return runtime.Bool(ok), nil
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
	_, existed := d.m[key]
	delete(d.m, key)
	return runtime.Bool(existed), nil
}

func dictGetValues(args []runtime.Value) (runtime.Value, error) {
	d, err := asDict(args)
	if err != nil {
		return runtime.Value{}, err
	}
	values := make([]runtime.Value, 0, len(d.m))
	for _, v := range d.m {
		values = append(values, v)
	}
	return NewListValue(values), nil
}

func dictGetKeys(args []runtime.Value) (runtime.Value, error) {
	d, err := asDict(args)
	if err != nil {
		return runtime.Value{}, err
	}
	keys := make([]runtime.Value, 0, len(d.m))
	for k := range d.m {
		keys = append(keys, runtime.String(k))
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
	return runtime.Value{}, nil
}
