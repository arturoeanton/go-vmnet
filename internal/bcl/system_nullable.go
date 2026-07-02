package bcl

import "github.com/arturoeanton/go-vmnet/internal/runtime"

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
