package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// System.Reflection.ConstructorInfo/MethodInfo/FieldInfo (Fase 3.39) —
// real reflection, not Reflection.Emit: given a Type and a declared
// signature, find the matching real method/ctor/field and let the host
// invoke/read it through the exact same machinery a normal call/newobj/
// ldfld already uses (Machine.New/Machine.call/Type.FieldIndex). Found
// via a real, common pattern (not a one-off NPOI workaround): a
// reflection-based registry mapping discovered types to their real
// constructors/factory methods — the same shape a serializer, ORM, or DI
// container's own plugin discovery typically takes. The natives
// themselves live in internal/interpreter/reflection.go (they need
// Machine access to actually resolve/invoke); this file only holds the
// plain data each wrapper carries.

type nativeConstructorInfo struct {
	typeFullName string
}

type nativeMethodInfo struct {
	typeFullName string
	methodName   string
}

type nativeFieldInfo struct {
	typeFullName string
	fieldName    string
}

// NewConstructorInfoValue/NewMethodInfoValue/NewFieldInfoValue build the
// respective System.Reflection wrapper values — called from
// internal/interpreter/reflection.go's Machine-aware
// GetConstructor/GetMethod/GetField natives.
func NewConstructorInfoValue(typeFullName string) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeConstructorInfo{typeFullName: typeFullName}})
}

func NewMethodInfoValue(typeFullName, methodName string) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeMethodInfo{typeFullName: typeFullName, methodName: methodName}})
}

func NewFieldInfoValue(typeFullName, fieldName string) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeFieldInfo{typeFullName: typeFullName, fieldName: fieldName}})
}

// ConstructorInfoTypeFullName/MethodInfoParts/FieldInfoParts unwrap the
// wrapper values back to their plain data — exported so
// internal/interpreter/reflection.go's Invoke/GetValue natives (which
// need Machine access the bcl package itself doesn't have) can read
// them without reaching into bcl's own unexported types.
func ConstructorInfoTypeFullName(v runtime.Value) (string, bool) {
	ci, ok := nativeOf[*nativeConstructorInfo](v)
	if !ok {
		return "", false
	}
	return ci.typeFullName, true
}

func MethodInfoParts(v runtime.Value) (typeFullName, methodName string, ok bool) {
	mi, ok := nativeOf[*nativeMethodInfo](v)
	if !ok {
		return "", "", false
	}
	return mi.typeFullName, mi.methodName, true
}

func FieldInfoParts(v runtime.Value) (typeFullName, fieldName string, ok bool) {
	fi, ok := nativeOf[*nativeFieldInfo](v)
	if !ok {
		return "", "", false
	}
	return fi.typeFullName, fi.fieldName, true
}

func nativeOf[T any](v runtime.Value) (T, bool) {
	var zero T
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	if v.Kind != runtime.KindObject || v.Obj == nil {
		return zero, false
	}
	t, ok := v.Obj.Native.(T)
	return t, ok
}

// ObjectArrayToValues unwraps an object[] argument (a real KindArray —
// every element already a plain runtime.Value regardless of its
// original declared/boxed type, vmnet's Value model doesn't box
// separately) into a plain slice — used by ConstructorInfo.Invoke/
// MethodInfo.Invoke's own args parameter.
func ObjectArrayToValues(v runtime.Value) ([]runtime.Value, error) {
	if v.Kind == runtime.KindNull {
		return nil, nil
	}
	if v.Kind != runtime.KindArray || v.Arr == nil {
		return nil, fmt.Errorf("bcl: expected an object[] argument")
	}
	out := make([]runtime.Value, len(v.Arr.Elems))
	copy(out, v.Arr.Elems)
	return out, nil
}

// TypeArrayToFullNames unwraps a Type[] argument (Type.GetConstructor/
// GetMethod's own parameterTypes argument) into full type name strings.
func TypeArrayToFullNames(v runtime.Value) ([]string, error) {
	if v.Kind == runtime.KindNull {
		return nil, nil
	}
	if v.Kind != runtime.KindArray || v.Arr == nil {
		return nil, fmt.Errorf("bcl: expected a Type[] argument")
	}
	out := make([]string, len(v.Arr.Elems))
	for i, e := range v.Arr.Elems {
		name, ok := TypeFullNameOf(e)
		if !ok {
			return nil, fmt.Errorf("bcl: Type[] element %d is not a Type", i)
		}
		out[i] = name
	}
	return out, nil
}
