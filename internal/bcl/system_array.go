package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

func init() {
	register("System.Array::Empty", true, arrayEmpty)
	register("System.Array::GetEnumerator", true, arrayGetEnumerator)
	register("System.Array+ArrayEnumerator::MoveNext", true, arrayEnumeratorMoveNext)
	register("System.Array+ArrayEnumerator::get_Current", true, arrayEnumeratorGetCurrent)
}

// arrayEmpty backs the generic Array.Empty<T>() helper: always a
// zero-length SZARRAY regardless of T, since runtime.Array doesn't carry
// an element type (see internal/runtime/array.go).
func arrayEmpty(args []runtime.Value) (runtime.Value, error) {
	return runtime.ArrRef(runtime.NewArray(0)), nil
}

// nativeArrayEnumerator backs the enumerator System.Array.GetEnumerator()
// returns. Unlike List<T>.Enumerator (a struct inlined directly at the
// foreach call site, Fase 3.11), a plain array enumerated through the
// non-generic System.Collections.IEnumerable protocol gets a real
// reference-type enumerator (System.Array+SZArrayEnumerator in the real
// BCL) — confirmed against real IL (Fase 3.24): `foreach` over an
// Array/IEnumerable-typed source compiles to `callvirt
// System.Array::GetEnumerator` directly, then drives the *result* through
// the IEnumerator interface (`callvirt IEnumerator::MoveNext`/
// `get_Current`), which is why this needs a real NativeTypeName entry
// below — the interface-dispatch fallback (Fase 3.13) is what redirects
// those interface-typed calls to the concrete natives registered here.
// index starts at -1, same reasoning as listEnumeratorType
// (system_collections.go): MoveNext increments before checking.
type nativeArrayEnumerator struct {
	arr   *runtime.Array
	index int
}

func arrayGetEnumerator(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindArray || args[0].Arr == nil {
		return runtime.Value{}, fmt.Errorf("bcl: Array.GetEnumerator called on a non-array receiver")
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeArrayEnumerator{arr: args[0].Arr, index: -1}}), nil
}

func asArrayEnumerator(args []runtime.Value) (*nativeArrayEnumerator, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return nil, fmt.Errorf("bcl: Array.Enumerator method called without a receiver")
	}
	e, ok := args[0].Obj.Native.(*nativeArrayEnumerator)
	if !ok {
		return nil, fmt.Errorf("bcl: receiver is not an Array enumerator")
	}
	return e, nil
}

func arrayEnumeratorMoveNext(args []runtime.Value) (runtime.Value, error) {
	e, err := asArrayEnumerator(args)
	if err != nil {
		return runtime.Value{}, err
	}
	e.index++
	return runtime.Bool(e.index < len(e.arr.Elems)), nil
}

func arrayEnumeratorGetCurrent(args []runtime.Value) (runtime.Value, error) {
	e, err := asArrayEnumerator(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if e.index < 0 || e.index >= len(e.arr.Elems) {
		return runtime.Value{}, fmt.Errorf("bcl: Array.Enumerator.Current: index %d out of range", e.index)
	}
	return e.arr.Elems[e.index], nil
}
