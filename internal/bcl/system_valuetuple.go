package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// valueTuple2Type models System.ValueTuple`2 (the `(T1, T2)` tuple
// literal syntax) as a real synthetic struct with public fields Item1/
// Item2 — unlike every other value type in this package, ValueTuple's
// members really are plain public fields in the BCL (not properties), so
// `t.Item1` compiles to a direct ldfld/stfld, not a callvirt to a
// get_Item1 native. Registering it as a value type (internal/
// interpreter/eval.go's fieldSlot already resolves ldfld/stfld against
// any registered struct's Type.FieldIndex generically) is all that's
// needed — no native getter/setter code at all.
var valueTuple2Type = runtime.NewValueType(
	"System", "ValueTuple`2",
	[]string{"Item1", "Item2"},
	[]runtime.Value{runtime.Null(), runtime.Null()},
)

func init() {
	registerValueType(valueTuple2Type)
	registerValueTypeCtor("System.ValueTuple`2", valueTuple2Ctor)
	register("System.ValueTuple`2::.ctor", false, valueTuple2CtorInPlace)
}

func valueTuple2Ctor(args []runtime.Value) (*runtime.Struct, error) {
	s := runtime.NewStruct(valueTuple2Type)
	if len(args) > 0 {
		s.Fields[0] = args[0]
	}
	if len(args) > 1 {
		s.Fields[1] = args[1]
	}
	return s, nil
}

func valueTuple2CtorInPlace(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindRef || args[0].Ref == nil {
		return runtime.Value{}, fmt.Errorf("bcl: ValueTuple constructor called without a receiver")
	}
	s, err := valueTuple2Ctor(args[1:])
	if err != nil {
		return runtime.Value{}, err
	}
	*args[0].Ref = runtime.StructVal(s)
	return runtime.Value{}, nil
}
