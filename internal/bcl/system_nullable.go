package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// nullableType is Nullable`1's synthetic Type descriptor: no TypeDef
// exists for it in a loaded assembly (it's a system-assembly value type),
// so unlike a plugin's own structs it's registered directly instead of
// resolved through Assembly.resolveTypeByFullName. Field 1 ("value")
// defaults to Int32(0) rather than Null() — GetValueOrDefault() on an
// empty Nullable should still return a usable numeric zero for the
// dominant real case (int?/double?/bool?), not an untyped null.
var nullableType = runtime.NewValueType(
	"System", "Nullable`1",
	[]string{"hasValue", "value"},
	[]runtime.Value{runtime.Bool(false), runtime.Int32(0)},
)

func init() {
	registerValueType(nullableType)
	registerValueTypeCtor("System.Nullable`1", nullableCtor)
	// `int? x = 5;` compiles as `ldloca`+`call .ctor` directly on the
	// local's storage, not `newobj` (confirmed against real IL, Fase
	// 3.13) — the same compiler optimization already needing its own
	// entry point for System.DateTime (Fase 3.12) and plugin structs
	// (Fase 3.7).
	register("System.Nullable`1::.ctor", false, nullableCtorInPlace)
	register("System.Nullable`1::get_HasValue", true, nullableGetHasValue)
	register("System.Nullable`1::get_Value", true, nullableGetValue)
	register("System.Nullable`1::GetValueOrDefault", true, nullableGetValueOrDefault)
}

func nullableCtor(args []runtime.Value) (*runtime.Struct, error) {
	s := runtime.NewStruct(nullableType)
	if len(args) > 0 {
		s.Fields[0] = runtime.Bool(true)
		s.Fields[1] = args[0]
	}
	return s, nil
}

func nullableCtorInPlace(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindRef || args[0].Ref == nil {
		return runtime.Value{}, fmt.Errorf("bcl: Nullable constructor called without a receiver")
	}
	s, err := nullableCtor(args[1:])
	if err != nil {
		return runtime.Value{}, err
	}
	*args[0].Ref = runtime.StructVal(s)
	return runtime.Value{}, nil
}

// asNullable unwraps a Nullable<T> receiver via derefStructReceiver
// (system_collections.go): instance methods on a value type receive
// `this` as a managed pointer (see fieldSlot in
// internal/interpreter/eval.go), so args[0] is normally KindRef, not
// KindStruct directly.
func asNullable(args []runtime.Value) (*runtime.Struct, error) {
	return derefStructReceiver(args, "Nullable", "Nullable method")
}

func nullableGetHasValue(args []runtime.Value) (runtime.Value, error) {
	s, err := asNullable(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return s.Fields[0], nil
}

func nullableGetValue(args []runtime.Value) (runtime.Value, error) {
	s, err := asNullable(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if !s.Fields[0].Truthy() {
		return runtime.Value{}, &runtime.ManagedException{
			TypeName: "System.InvalidOperationException",
			Message:  "Nullable object must have a value.",
		}
	}
	return s.Fields[1], nil
}

func nullableGetValueOrDefault(args []runtime.Value) (runtime.Value, error) {
	s, err := asNullable(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return s.Fields[1], nil
}

// UnwrapNullable collapses a Nullable<T> struct Value (system_nullable.go)
// into either its underlying T (HasValue == true) or a plain KindNull
// (HasValue == false) — v unchanged for every other Kind. A LINQ key/
// aggregation callback typed to return `int?`/`double?`/etc. (e.g.
// `xs.OrderBy(x => x.NullableAge)`, `xs.Sum(x => x.NullableScore)`) hands
// back exactly this struct shape verbatim; the CLR only ever unboxes it
// to a bare value or a real null reference at a `box`/pattern-match site,
// neither of which a plain delegate return passes through. Comparison
// (interpreter/comparer.go) and the numeric LINQ aggregates (Sum/
// Average/Min/Max, interpreter/linq.go) both need the T underneath (or
// "no value, sorts/counts as null") rather than an opaque two-field
// struct they have no other reason to know about.
func UnwrapNullable(v runtime.Value) runtime.Value {
	if v.Kind != runtime.KindStruct || v.Struct == nil || v.Struct.Type == nil {
		return v
	}
	t := v.Struct.Type
	if t.Namespace != "System" || t.Name != "Nullable`1" {
		return v
	}
	if !v.Struct.Fields[0].Truthy() {
		return runtime.Null()
	}
	return v.Struct.Fields[1]
}
