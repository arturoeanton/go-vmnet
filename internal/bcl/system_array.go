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
	register("System.Array::Resize", false, arrayResize)
	register("System.Array::IndexOf", true, arrayIndexOf)
	register("System.Array::Copy", false, arrayCopy)
}

// arrayResize backs the generic Array.Resize<T>(ref T[] array, int
// newSize) — array arrives as a managed pointer (a `ref` parameter),
// same mechanism as any other by-ref argument since Fase 3.5.
func arrayResize(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 || args[0].Kind != runtime.KindRef || args[0].Ref == nil || args[1].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: Array.Resize expects (ref T[], int)")
	}
	newSize := int(args[1].I4)
	if newSize < 0 {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "newSize must be non-negative"}
	}
	newArr := runtime.NewArray(newSize)
	if old := args[0].Ref; old.Kind == runtime.KindArray && old.Arr != nil {
		copy(newArr.Elems, old.Arr.Elems)
	}
	*args[0].Ref = runtime.ArrRef(newArr)
	return runtime.Value{}, nil
}

// arrayIndexOf backs the generic Array.IndexOf<T>(T[] array, T value)
// static helper — a linear scan using the same value-equality vmnet's
// other Contains/IndexOf natives already share (system_object.go).
func arrayIndexOf(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 || args[0].Kind != runtime.KindArray || args[0].Arr == nil {
		return runtime.Value{}, fmt.Errorf("bcl: Array.IndexOf expects (T[], T)")
	}
	for i, item := range args[0].Arr.Elems {
		if valuesEqual(item, args[1]) {
			return runtime.Int32(int32(i)), nil
		}
	}
	return runtime.Int32(-1), nil
}

// arrayCopy backs Array.Copy(Array source, int sourceIndex, Array
// destination, int destinationIndex, int length) — the 5-arg overload,
// the shape every real caller found so far actually uses (e.g.
// StringDictionarySlim`1.Resize copying the old _entries into a larger
// array). Go's copy() already handles the source/destination overlap
// case correctly (memmove semantics), matching real Array.Copy.
func arrayCopy(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 5 ||
		args[0].Kind != runtime.KindArray || args[0].Arr == nil ||
		args[1].Kind != runtime.KindI4 ||
		args[2].Kind != runtime.KindArray || args[2].Arr == nil ||
		args[3].Kind != runtime.KindI4 ||
		args[4].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: Array.Copy expects (Array, int, Array, int, int)")
	}
	srcIdx, dstIdx, length := int(args[1].I4), int(args[3].I4), int(args[4].I4)
	src, dst := args[0].Arr.Elems, args[2].Arr.Elems
	if srcIdx < 0 || dstIdx < 0 || length < 0 || srcIdx+length > len(src) || dstIdx+length > len(dst) {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "Array.Copy: index or length out of range"}
	}
	copy(dst[dstIdx:dstIdx+length], src[srcIdx:srcIdx+length])
	return runtime.Value{}, nil
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
