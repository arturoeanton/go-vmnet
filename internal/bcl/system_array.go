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
	register("System.Array::CopyTo", false, arrayCopyTo)
	register("System.Array::Clone", true, arrayClone)
	register("System.Array::get_Length", true, arrayGetLength)
	register("System.Array::GetLength", true, arrayGetLengthDim)
	// A real CIL array implicitly implements ICollection<T>/IList<T>
	// (SZArray covariance, ECMA-335 §II.9.9 — same rationale
	// receiverTypeName's own "System.Array" fallback, internal/
	// interpreter/typecheck.go, already documents for foreach/LINQ) —
	// get_Count is that interface pair's own real member name for what
	// Array itself calls get_Length; a real array's Count always equals
	// its Length. Found via a real, load-bearing case (Fase 3.63):
	// MemberInfo.GetCustomAttributesData() declares IList<CustomAttributeData>,
	// not a bare array, so a caller checking `.Count` reaches this
	// callvirt, not get_Length.
	register("System.Array::get_Count", true, arrayGetLength)
	// IList<T>'s own indexer (get_Item(int)) — same real-array-implicitly-
	// implements-the-interface reasoning as get_Count above; a real
	// array's IList<T>[i] is exactly Array.GetValue(i), just reached
	// through a different declared name at the call site.
	register("System.Array::get_Item", true, arrayGetValue)
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
	register("System.Array::Clear", false, arrayClear)
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

// arrayCopy backs both real Array.Copy overloads: the 5-arg
// (Array source, int sourceIndex, Array destination, int
// destinationIndex, int length) shape most real callers use (e.g.
// StringDictionarySlim`1.Resize copying the old _entries into a larger
// array), and the 3-arg (Array sourceArray, Array destinationArray, int
// length) shorthand — implicitly sourceIndex=destinationIndex=0 — found
// running real Jint's own internal array/property-storage growth logic.
// Go's copy() already handles the source/destination overlap case
// correctly (memmove semantics), matching real Array.Copy.
func arrayCopy(args []runtime.Value) (runtime.Value, error) {
	var srcIdx, dstIdx, length int
	var src, dst []runtime.Value
	switch len(args) {
	case 3:
		if args[0].Kind != runtime.KindArray || args[0].Arr == nil ||
			args[1].Kind != runtime.KindArray || args[1].Arr == nil ||
			args[2].Kind != runtime.KindI4 {
			return runtime.Value{}, fmt.Errorf("bcl: Array.Copy expects (Array, Array, int)")
		}
		src, dst = args[0].Arr.Elems, args[1].Arr.Elems
		length = int(args[2].I4)
	case 5:
		if args[0].Kind != runtime.KindArray || args[0].Arr == nil ||
			args[1].Kind != runtime.KindI4 ||
			args[2].Kind != runtime.KindArray || args[2].Arr == nil ||
			args[3].Kind != runtime.KindI4 ||
			args[4].Kind != runtime.KindI4 {
			return runtime.Value{}, fmt.Errorf("bcl: Array.Copy expects (Array, int, Array, int, int)")
		}
		srcIdx, dstIdx, length = int(args[1].I4), int(args[3].I4), int(args[4].I4)
		src, dst = args[0].Arr.Elems, args[2].Arr.Elems
	default:
		return runtime.Value{}, fmt.Errorf("bcl: Array.Copy expects (Array, int, Array, int, int) or (Array, Array, int)")
	}
	if srcIdx < 0 || dstIdx < 0 || length < 0 || srcIdx+length > len(src) || dstIdx+length > len(dst) {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "Array.Copy: index or length out of range"}
	}
	copy(dst[dstIdx:dstIdx+length], src[srcIdx:srcIdx+length])
	return runtime.Value{}, nil
}

// arrayClear backs both real Array.Clear overloads — Clear(Array) (the
// whole array) and Clear(Array, int index, int length) (a range), the
// shape real code overwhelmingly uses (e.g. Esprima.ArrayList`1.Clear(),
// found running real Jint/Esprima: `Array.Clear(_items, 0, _count);`
// drops references to whatever AST nodes the scratch buffer held before
// being reused for the next declaration list). Each cleared slot becomes
// this element's OWN Kind's zero value (arrayClearZero) — vmnet's arrays
// aren't declared with a static element type the way a real
// `T[]`'s own metadata is, so there's no better source of truth than
// what a slot already holds; every real caller found so far clears a
// range that's either already at its default or holds same-shaped
// elements throughout.
func arrayClear(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 1 {
		if args[0].Kind != runtime.KindArray || args[0].Arr == nil {
			return runtime.Value{}, fmt.Errorf("bcl: Array.Clear expects an Array")
		}
		elems := args[0].Arr.Elems
		for i := range elems {
			elems[i] = arrayClearZero(elems[i])
		}
		return runtime.Value{}, nil
	}
	if len(args) == 3 &&
		args[0].Kind == runtime.KindArray && args[0].Arr != nil &&
		args[1].Kind == runtime.KindI4 && args[2].Kind == runtime.KindI4 {
		idx, length := int(args[1].I4), int(args[2].I4)
		elems := args[0].Arr.Elems
		if idx < 0 || length < 0 || idx+length > len(elems) {
			return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "Array.Clear: index or length out of range"}
		}
		for i := idx; i < idx+length; i++ {
			elems[i] = arrayClearZero(elems[i])
		}
		return runtime.Value{}, nil
	}
	return runtime.Value{}, fmt.Errorf("bcl: Array.Clear expects (Array) or (Array, int, int)")
}

// arrayClearZero is default(T) inferred from v's own current Kind — a
// numeric Kind zeros to the same typed zero newarr's own value-type
// element seeding uses, a KindStruct re-zeros via a fresh NewStruct of
// the SAME Type (not a shared instance — Value.Clone's own doc comment
// explains why sharing one *Struct across slots is wrong), and anything
// reference-shaped (object/array/string/func) becomes Null(), matching
// real default(T) for a reference type.
func arrayClearZero(v runtime.Value) runtime.Value {
	switch v.Kind {
	case runtime.KindI4:
		return runtime.Int32(0)
	case runtime.KindI8:
		return runtime.Int64(0)
	case runtime.KindR4:
		return runtime.Float32(0)
	case runtime.KindR8:
		return runtime.Float64(0)
	case runtime.KindStruct:
		if v.Struct != nil {
			return runtime.StructVal(runtime.NewStruct(v.Struct.Type))
		}
		return v
	default:
		return runtime.Null()
	}
}

// arrayCopyTo backs the instance CopyTo(Array destinationArray, int
// destinationIndex) (Fase 3.74, found via System.Text.Json's own
// PooledByteBufferWriter): copies the WHOLE receiver array starting at
// destinationIndex — real .NET's own CopyTo(Array, long) overload
// collapses to the same shape here (vmnet has no real 64-bit array
// index distinct from int32).
func arrayCopyTo(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 3 ||
		args[0].Kind != runtime.KindArray || args[0].Arr == nil ||
		args[1].Kind != runtime.KindArray || args[1].Arr == nil ||
		args[2].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: Array.CopyTo expects (Array destinationArray, int destinationIndex)")
	}
	src, dst := args[0].Arr.Elems, args[1].Arr.Elems
	dstIdx := int(args[2].I4)
	if dstIdx < 0 || dstIdx+len(src) > len(dst) {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "Array.CopyTo: destination array too small"}
	}
	copy(dst[dstIdx:dstIdx+len(src)], src)
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
