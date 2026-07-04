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
	register("System.Array::Clone", true, arrayClone)
	register("System.Array::get_Length", true, arrayGetLength)
	register("System.Array::GetLength", true, arrayGetLengthDim)
	// Non-generic Array reflection (Fase 3.52) — SetValue/GetValue/
	// CreateInstance are the shape a reflection-driven caller (working
	// against a bare System.Array, no compile-time element type at all)
	// uses instead of ordinary ldelem/stelem/newarr; found via Dapper's
	// own SqlMapper array-parameter expansion for a SQL `IN (...)`
	// clause. GetValue/SetValue only cover the single-index overload (no
	// real caller found here needing a multi-dimensional Array's
	// int[]-indices form).
	register("System.Array::CreateInstance", true, arrayCreateInstance)
	register("System.Array::GetValue", true, arrayGetValue)
	register("System.Array::SetValue", true, arraySetValue)
}

// arrayCreateInstance backs Array.CreateInstance(Type elementType, int
// length) — every element defaults to Null() regardless of elementType:
// vmnet's Value model has no generic "zero value for this arbitrary
// Type" constructor outside a real plugin TypeDef (assembly.go's own
// newObj already needs a resolvable Type for that), and every real
// caller found here only ever populates the array immediately after
// creating it (via SetValue), never reads an un-set slot back first.
func arrayCreateInstance(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 || args[1].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: Array.CreateInstance expects (Type, int length)")
	}
	n := int(args[1].I4)
	if n < 0 {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "length"}
	}
	elems := make([]runtime.Value, n)
	for i := range elems {
		elems[i] = runtime.Null()
	}
	return runtime.ArrRef(&runtime.Array{Elems: elems}), nil
}

// arrayGetValue/arraySetValue back the single-index Array.GetValue(int)/
// SetValue(object, int) reflection overloads.
func arrayGetValue(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 || args[0].Kind != runtime.KindArray || args[0].Arr == nil || args[1].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: Array.GetValue expects (array, int index)")
	}
	idx := int(args[1].I4)
	if idx < 0 || idx >= len(args[0].Arr.Elems) {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.IndexOutOfRangeException", Message: "Index was outside the bounds of the array."}
	}
	return args[0].Arr.Elems[idx], nil
}

func arraySetValue(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 3 || args[0].Kind != runtime.KindArray || args[0].Arr == nil || args[2].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: Array.SetValue expects (array, value, int index)")
	}
	idx := int(args[2].I4)
	if idx < 0 || idx >= len(args[0].Arr.Elems) {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.IndexOutOfRangeException", Message: "Index was outside the bounds of the array."}
	}
	args[0].Arr.Elems[idx] = args[1]
	return runtime.Value{}, nil
}

// arrayClone backs Array.Clone() — a shallow copy: each element Value is
// copied as-is (a reference-shaped element, e.g. another array or
// object, still aliases the same backing storage the real CLR's shallow
// clone would also share).
func arrayClone(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 || args[0].Kind != runtime.KindArray || args[0].Arr == nil {
		return runtime.Value{}, fmt.Errorf("bcl: Array.Clone expects an array receiver")
	}
	elems := make([]runtime.Value, len(args[0].Arr.Elems))
	copy(elems, args[0].Arr.Elems)
	return runtime.ArrRef(&runtime.Array{Elems: elems}), nil
}

// arrayGetLength backs Array.Length accessed through a call site typed
// against the System.Array base (real C# code holding an array in an
// Array-typed local/parameter, or via reflection) — the far more common
// case, a real array-typed local, compiles Length as the `ldlen` opcode
// directly and never reaches this native at all.
func arrayGetLength(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 || args[0].Kind != runtime.KindArray || args[0].Arr == nil {
		return runtime.Value{}, fmt.Errorf("bcl: Array.get_Length expects an array receiver")
	}
	return runtime.Int32(int32(len(args[0].Arr.Elems))), nil
}

// arrayGetLengthDim backs GetLength(int dimension) — vmnet only ever
// models a single-dimension SZArray (Fase 3.5), so dimension is always 0
// for any real caller and this is just get_Length again.
func arrayGetLengthDim(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindArray || args[0].Arr == nil {
		return runtime.Value{}, fmt.Errorf("bcl: Array.GetLength expects an array receiver")
	}
	return runtime.Int32(int32(len(args[0].Arr.Elems))), nil
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

// arrayIndexOf backs every real Array.IndexOf<T> overload — (array,
// value), (array, value, startIndex), and (array, value, startIndex,
// count) (Fase 3.44, found via a real, load-bearing case: Newtonsoft.
// Json's own KeyedCollection<TKey,TItem> base implementation uses the
// 3-arg form when re-locating an item during a key change) — a linear
// scan using the same value-equality vmnet's other Contains/IndexOf
// natives already share (system_object.go).
func arrayIndexOf(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[0].Kind != runtime.KindArray || args[0].Arr == nil {
		return runtime.Value{}, fmt.Errorf("bcl: Array.IndexOf expects (T[], T[, startIndex[, count]])")
	}
	elems := args[0].Arr.Elems
	start, end := 0, len(elems)
	if len(args) >= 3 && args[2].Kind == runtime.KindI4 {
		start = int(args[2].I4)
	}
	if len(args) >= 4 && args[3].Kind == runtime.KindI4 {
		end = start + int(args[3].I4)
	}
	if start < 0 || end > len(elems) || end < start {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "startIndex or count"}
	}
	for i := start; i < end; i++ {
		if valuesEqual(elems[i], args[1]) {
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
