package bcl

import (
	"fmt"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// nativeTypeInfo backs a System.Type instance: just the full name it
// represents. vmnet doesn't give System.Type objects real reference
// identity (typeof(X) called twice produces two distinct
// *nativeTypeInfo, unlike the CLR's single canonical Type per real
// type) — every comparison/lookup below works on the FullName string,
// never Go pointer identity, so this doesn't matter for any supported
// operation (Fase 3.14).
type nativeTypeInfo struct {
	FullName string
}

func init() {
	register("System.Object::GetType", true, objectGetType)
	register("System.Type::GetTypeFromHandle", true, typeGetTypeFromHandle)
	register("System.Type::get_Name", true, typeGetName)
	register("System.Type::get_FullName", true, typeGetFullName)
	register("System.Type::ToString", true, typeGetFullName)
	register("System.Type::op_Equality", true, typeEquals)
	register("System.Type::op_Inequality", true, typeNotEquals)
	register("System.Type::Equals", true, typeEquals)
	// System.Type IS a System.Reflection.MemberInfo in the real BCL — the
	// same simple-name property, just reached through a MemberInfo-typed
	// expression (`someMember.Name` where someMember happens to hold a
	// Type). vmnet doesn't model MemberInfo as a distinct hierarchy; this
	// covers the call site shape directly.
	register("System.Reflection.MemberInfo::get_Name", true, typeGetName)
}

// NewTypeValue builds a System.Type value for fullName — the runtime
// counterpart of ir.LoadTypeToken (typeof(T)), called directly from
// internal/interpreter/eval.go rather than through the normal
// bcl.Lookup/native-call path, since ldtoken isn't a call at all.
func NewTypeValue(fullName string) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeTypeInfo{FullName: fullName}})
}

// typeGetTypeFromHandle is the identity function: LoadTypeToken already
// produced the real System.Type value ldtoken+GetTypeFromHandle
// represents together (see LoadTypeToken's doc comment), so by the time
// this native would run, there's nothing left to convert.
func typeGetTypeFromHandle(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Type.GetTypeFromHandle expects 1 argument")
	}
	return args[0], nil
}

// objectGetType inspects the receiver's actual runtime shape to produce
// its real type's full name — the same information isAssignableTo (Fase
// 3.8) already extracts for isinst/castclass, reused here instead of
// duplicating a second type-identity mechanism. A boxed primitive's
// exact type is inherently ambiguous from Kind alone (KindI4 covers
// int32/bool/char/short/byte — same documented limitation
// isAssignableTo's KindI4 branch already has); the common case (int32)
// wins.
func objectGetType(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Object.GetType expects a receiver")
	}
	v := args[0]
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	switch v.Kind {
	case runtime.KindNull:
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.NullReferenceException", Message: "Object reference not set to an instance of an object (calling GetType)"}
	case runtime.KindI4:
		return NewTypeValue("System.Int32"), nil
	case runtime.KindI8:
		return NewTypeValue("System.Int64"), nil
	case runtime.KindR4:
		return NewTypeValue("System.Single"), nil
	case runtime.KindR8:
		return NewTypeValue("System.Double"), nil
	case runtime.KindString:
		return NewTypeValue("System.String"), nil
	case runtime.KindArray, runtime.KindBytes:
		return NewTypeValue("System.Array"), nil
	case runtime.KindStruct:
		if v.Struct == nil || v.Struct.Type == nil {
			return runtime.Value{}, fmt.Errorf("bcl: System.Object.GetType: unresolved struct receiver")
		}
		return NewTypeValue(typeFullName(v.Struct.Type)), nil
	case runtime.KindObject:
		if v.Obj == nil {
			return runtime.Value{}, &runtime.ManagedException{TypeName: "System.NullReferenceException", Message: "Object reference not set to an instance of an object (calling GetType)"}
		}
		if v.Obj.Type != nil {
			return NewTypeValue(typeFullName(v.Obj.Type)), nil
		}
		if ex, ok := v.Obj.Native.(*runtime.ManagedException); ok {
			return NewTypeValue(ex.TypeName), nil
		}
		if name, ok := NativeTypeName(v.Obj.Native); ok {
			return NewTypeValue(name), nil
		}
		return runtime.Value{}, fmt.Errorf("bcl: System.Object.GetType: unrecognized native receiver")
	default:
		return runtime.Value{}, fmt.Errorf("bcl: System.Object.GetType: unsupported receiver kind")
	}
}

func typeFullName(t *runtime.Type) string {
	if t.Namespace == "" {
		return t.Name
	}
	return t.Namespace + "." + t.Name
}

// TypeFullNameOf returns a System.Type value's FullName — used by
// internal/interpreter/reflection.go (Fase 3.16), which needs it outside
// this package to implement Type::IsAssignableFrom (a Machine-aware
// native: walking the real type hierarchy needs Machine.ResolveType,
// unavailable to a plain bcl.Native).
func TypeFullNameOf(v runtime.Value) (string, bool) {
	ti, err := asTypeInfo(v)
	if err != nil {
		return "", false
	}
	return ti.FullName, true
}

func asTypeInfo(v runtime.Value) (*nativeTypeInfo, error) {
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	if v.Kind != runtime.KindObject || v.Obj == nil {
		return nil, fmt.Errorf("bcl: System.Type method called on a non-Type value")
	}
	ti, ok := v.Obj.Native.(*nativeTypeInfo)
	if !ok {
		return nil, fmt.Errorf("bcl: System.Type method receiver is not a Type")
	}
	return ti, nil
}

func typeGetFullName(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Type.FullName expects a receiver")
	}
	ti, err := asTypeInfo(args[0])
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.String(ti.FullName), nil
}

// typeGetName returns the simple name: the last "+"-nested or
// "."-namespaced segment, matching Type.Name's real "no namespace, no
// enclosing type" contract.
func typeGetName(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Type.Name expects a receiver")
	}
	ti, err := asTypeInfo(args[0])
	if err != nil {
		return runtime.Value{}, err
	}
	name := ti.FullName
	if idx := strings.LastIndexAny(name, ".+"); idx >= 0 {
		name = name[idx+1:]
	}
	return runtime.String(name), nil
}

// typeEquals/typeNotEquals compare by FullName, not Go pointer identity —
// see nativeTypeInfo's doc comment for why that's the only option here,
// and the only thing observable through Type's public API anyway
// (real code never compares Type identity any other way).
func typeEquals(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Type.Equals expects 2 arguments")
	}
	a, b := args[0], args[1]
	if a.Kind == runtime.KindNull || b.Kind == runtime.KindNull {
		return runtime.Bool(a.Kind == b.Kind), nil
	}
	ta, err := asTypeInfo(a)
	if err != nil {
		return runtime.Value{}, err
	}
	tb, err := asTypeInfo(b)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(ta.FullName == tb.FullName), nil
}

func typeNotEquals(args []runtime.Value) (runtime.Value, error) {
	v, err := typeEquals(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(!v.Truthy()), nil
}
