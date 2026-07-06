package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// arraySegmentType models System.ArraySegment<T> as a real (_array,
// _offset, _count) triple, field names matching the real BCL source
// (Fase 3.74, same "match real field names so a not-yet-intercepted bare
// ldfld still works" reasoning system_span.go's own Span<T>/
// ReadOnlySpan<T> doc comment already documents) — found via
// System.Text.Json's own JsonReaderHelper/PooledByteBufferWriter code,
// which wraps a rented byte[] buffer this way to pass around a
// (backing array, valid length) pair without a fresh copy.
var arraySegmentType = runtime.NewValueType("System", "ArraySegment`1", []string{"_array", "_offset", "_count"}, arraySegmentDefaults())

func arraySegmentDefaults() []runtime.Value {
	return []runtime.Value{runtime.Null(), runtime.Int32(0), runtime.Int32(0)}
}

func init() {
	registerValueType(arraySegmentType)
	registerValueTypeCtor("System.ArraySegment`1", arraySegmentCtor)
	// A value type constructed directly into an already-addressed local
	// (`var seg = new ArraySegment<int>(arr);`) compiles to `ldloca` +
	// `call instance void ArraySegment\`1::.ctor(...)` — a plain
	// instance-method call on the local's own address, NOT a `newobj` —
	// unlike a value constructed as a standalone expression (passed
	// straight into another call, or returned), which does use `newobj`
	// and so only ever reaches registerValueTypeCtor above. Machine.call
	// never consults LookupValueTypeCtor at all (only Machine.newObj
	// does), so this second, plain bcl.Lookup registration is what makes
	// the ldloca+call.ctor shape work — found via this exact real shape
	// (Fase 3.74).
	register("System.ArraySegment`1::.ctor", false, arraySegmentCtorInPlace)
	register("System.ArraySegment`1::get_Array", true, arraySegmentGetArray)
	register("System.ArraySegment`1::get_Offset", true, arraySegmentGetOffset)
	register("System.ArraySegment`1::get_Count", true, arraySegmentGetCount)
}

// arraySegmentFieldsFor builds the real (_array, _offset, _count) triple
// both ArraySegment<T> constructor entry points share — covers both real
// overloads: (T[] array) — the whole array, offset 0 — and (T[] array,
// int offset, int count).
func arraySegmentFieldsFor(args []runtime.Value) ([]runtime.Value, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindArray {
		return nil, fmt.Errorf("bcl: ArraySegment<T> constructor expects an array argument")
	}
	arr := args[0]
	offset := int32(0)
	count := int32(0)
	if arr.Arr != nil {
		count = int32(len(arr.Arr.Elems))
	}
	if len(args) >= 3 && args[1].Kind == runtime.KindI4 && args[2].Kind == runtime.KindI4 {
		offset = args[1].I4
		count = args[2].I4
	}
	return []runtime.Value{arr, runtime.Int32(offset), runtime.Int32(count)}, nil
}

// arraySegmentCtor backs the newobj-reached construction path (a value
// used as a standalone expression).
func arraySegmentCtor(args []runtime.Value) (*runtime.Struct, error) {
	fields, err := arraySegmentFieldsFor(args)
	if err != nil {
		return nil, err
	}
	return &runtime.Struct{Type: arraySegmentType, Fields: fields}, nil
}

// arraySegmentCtorInPlace backs the ldloca+call.ctor construction path —
// args[0] is a KindRef to the already-allocated (zero-valued) struct
// slot; the real fields get written through it in place, exactly
// mirroring what newobj's own arraySegmentCtor above builds fresh.
func arraySegmentCtorInPlace(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindRef || args[0].Ref == nil {
		return runtime.Value{}, fmt.Errorf("bcl: ArraySegment<T> constructor called without a receiver")
	}
	fields, err := arraySegmentFieldsFor(args[1:])
	if err != nil {
		return runtime.Value{}, err
	}
	*args[0].Ref = runtime.StructVal(&runtime.Struct{Type: arraySegmentType, Fields: fields})
	return runtime.Value{}, nil
}

func arraySegmentFields(args []runtime.Value) (*runtime.Struct, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("bcl: ArraySegment<T> member expects a receiver")
	}
	v := args[0]
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	if v.Kind != runtime.KindStruct || v.Struct == nil {
		return nil, fmt.Errorf("bcl: ArraySegment<T> member called on a non-struct receiver")
	}
	return v.Struct, nil
}

func arraySegmentGetArray(args []runtime.Value) (runtime.Value, error) {
	s, err := arraySegmentFields(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return s.Fields[0], nil
}

func arraySegmentGetOffset(args []runtime.Value) (runtime.Value, error) {
	s, err := arraySegmentFields(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return s.Fields[1], nil
}

func arraySegmentGetCount(args []runtime.Value) (runtime.Value, error) {
	s, err := arraySegmentFields(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return s.Fields[2], nil
}
