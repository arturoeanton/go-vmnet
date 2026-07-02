package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// Resolver looks up another method in the same assembly by its
// "Namespace.Type::Method" full name, for calls that aren't BCL natives.
type Resolver func(fullName string) (*runtime.Method, error)

// TypeResolver looks up a type's field layout by its "Namespace.Type" full
// name, for newobj/ldfld/stfld.
type TypeResolver func(fullName string) (*runtime.Type, error)

func (m *Machine) call(fullName string, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, bool, error) {
	if native, hasReturn, ok := bcl.Lookup(fullName); ok {
		v, err := native(args)
		return v, hasReturn, err
	}
	if m.Resolve == nil {
		return runtime.Value{}, false, fmt.Errorf("interpreter: unsupported BCL method %q (no native registered)", fullName)
	}
	method, err := m.Resolve(fullName)
	if err != nil {
		return runtime.Value{}, false, fmt.Errorf("interpreter: unsupported BCL method %q: %w", fullName, err)
	}
	v, err := m.invoke(method, args, depth+1, instrCount)
	if err != nil {
		return runtime.Value{}, false, err
	}
	return v, method.HasReturn, nil
}

// newObj implements the ir.NewObj instruction: allocate (native or plain)
// and, for plain objects, run the constructor.
func (m *Machine) newObj(in newObjArgs, depth int, instrCount *int64) (runtime.Value, error) {
	if ctor, ok := bcl.LookupCtor(in.TypeFullName); ok {
		obj, err := ctor(in.Args)
		if err != nil {
			return runtime.Value{}, err
		}
		return runtime.ObjRef(obj), nil
	}

	if m.ResolveType == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: unsupported type %q (no native constructor and no type resolver)", in.TypeFullName)
	}
	typ, err := m.ResolveType(in.TypeFullName)
	if err != nil {
		return runtime.Value{}, err
	}
	fields := make([]runtime.Value, len(typ.Fields))
	copy(fields, typ.FieldDefaults)
	obj := &runtime.Object{Type: typ, Fields: fields}
	objVal := runtime.ObjRef(obj)

	ctorArgs := make([]runtime.Value, 0, len(in.Args)+1)
	ctorArgs = append(ctorArgs, objVal)
	ctorArgs = append(ctorArgs, in.Args...)
	if _, _, err := m.call(in.CtorFullName, ctorArgs, depth, instrCount); err != nil {
		return runtime.Value{}, err
	}
	return objVal, nil
}

type newObjArgs struct {
	TypeFullName string
	CtorFullName string
	Args         []runtime.Value
}
