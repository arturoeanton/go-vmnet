package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// nativeStack backs Stack<T> — items[len-1] is the top, matching Push/
// Pop/Peek's LIFO order directly off a Go slice append/truncate. Also
// backs the legacy, non-generic System.Collections.Stack (Fase 3.39,
// same reasoning as nativeList/nativeDict's own typeName field: found
// via a real, load-bearing case, NPOI's own formula rendering needing a
// real Stack once the AreaPtg overload-resolution bug — see
// valueIsAssignableToTypeName's doc comment — stopped masking it).
type nativeStack struct {
	items    []runtime.Value
	typeName string
}

func init() {
	registerCtor("System.Collections.Generic.Stack`1", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeStack{typeName: "System.Collections.Generic.Stack`1"}}, nil
	})
	register("System.Collections.Generic.Stack`1::Push", false, stackPush)
	register("System.Collections.Generic.Stack`1::Pop", true, stackPop)
	register("System.Collections.Generic.Stack`1::Peek", true, stackPeek)
	register("System.Collections.Generic.Stack`1::get_Count", true, stackCount)
	register("System.Collections.Generic.Stack`1::Clear", false, stackClear)
	register("System.Collections.Generic.Stack`1::Contains", true, stackContains)

	registerCtor("System.Collections.Stack", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeStack{typeName: "System.Collections.Stack"}}, nil
	})
	register("System.Collections.Stack::Push", false, stackPush)
	register("System.Collections.Stack::Pop", true, stackPop)
	register("System.Collections.Stack::Peek", true, stackPeek)
	register("System.Collections.Stack::get_Count", true, stackCount)
	register("System.Collections.Stack::Clear", false, stackClear)
	register("System.Collections.Stack::Contains", true, stackContains)
	register("System.Collections.Stack::ToArray", true, stackToArray)
}

// stackToArray returns items top-to-bottom, matching real Stack.ToArray
// semantics (index 0 is the top, unlike the internal items slice which
// keeps the top at the end for O(1) push/pop).
func stackToArray(args []runtime.Value) (runtime.Value, error) {
	s, err := asStack(args)
	if err != nil {
		return runtime.Value{}, err
	}
	n := len(s.items)
	out := make([]runtime.Value, n)
	for i, v := range s.items {
		out[n-1-i] = v
	}
	return runtime.ArrRef(&runtime.Array{Elems: out}), nil
}

func stackClear(args []runtime.Value) (runtime.Value, error) {
	s, err := asStack(args)
	if err != nil {
		return runtime.Value{}, err
	}
	s.items = nil
	return runtime.Value{}, nil
}

func stackContains(args []runtime.Value) (runtime.Value, error) {
	s, err := asStack(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: Stack.Contains expects 1 argument")
	}
	for _, item := range s.items {
		if valuesEqual(item, args[1]) {
			return runtime.Bool(true), nil
		}
	}
	return runtime.Bool(false), nil
}

func asStack(args []runtime.Value) (*nativeStack, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return nil, fmt.Errorf("bcl: Stack method called without a receiver")
	}
	s, ok := args[0].Obj.Native.(*nativeStack)
	if !ok {
		return nil, fmt.Errorf("bcl: receiver is not a Stack")
	}
	return s, nil
}

func stackPush(args []runtime.Value) (runtime.Value, error) {
	s, err := asStack(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: Stack.Push expects 1 argument")
	}
	s.items = append(s.items, args[1])
	return runtime.Value{}, nil
}

func stackPop(args []runtime.Value) (runtime.Value, error) {
	s, err := asStack(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(s.items) == 0 {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.InvalidOperationException", Message: "Stack empty."}
	}
	top := s.items[len(s.items)-1]
	s.items = s.items[:len(s.items)-1]
	return top, nil
}

func stackPeek(args []runtime.Value) (runtime.Value, error) {
	s, err := asStack(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(s.items) == 0 {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.InvalidOperationException", Message: "Stack empty."}
	}
	return s.items[len(s.items)-1], nil
}

func stackCount(args []runtime.Value) (runtime.Value, error) {
	s, err := asStack(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(int32(len(s.items))), nil
}
