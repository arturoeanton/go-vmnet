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
	// genericArgs holds MakeGenericMethod's own real Type[] argument
	// (closed generic method type arguments, e.g. ["System.Int32"] for
	// `Identity<int>`) — nil for an ordinary non-generic MethodInfo, or
	// one obtained via GetMethod that hasn't had MakeGenericMethod called
	// on it. internal/interpreter/reflection.go's methodInfoInvoke reads
	// this to pass the real methodGenericArgs through to Machine.call,
	// the same argument an ordinary compiled `callvirt SomeMethod<T>()`
	// site's own ir.Call.MethodGenericArgs already carries.
	genericArgs []string
}

type nativeFieldInfo struct {
	typeFullName string
	fieldName    string
}

// nativePropertyInfo backs System.Reflection.PropertyInfo (Fase 3.51,
// Type.GetProperties/GetProperty) — canRead/canWrite come from the real
// get_Xxx/set_Xxx MethodDef linkage (assembly.go's resolveProperties),
// not guessed from the name, so a get-only or set-only property answers
// CanRead/CanWrite correctly and GetValue/SetValue can reject the
// unsupported direction with a real error instead of an opaque "method
// not found" from whatever m.call attempt would otherwise fail deep
// inside.
type nativePropertyInfo struct {
	typeFullName      string
	propertyName      string
	canRead, canWrite bool
}

func init() {
	register("System.Reflection.PropertyInfo::get_Name", true, propertyInfoGetName)
	register("System.Reflection.PropertyInfo::get_CanRead", true, propertyInfoGetCanRead)
	register("System.Reflection.PropertyInfo::get_CanWrite", true, propertyInfoGetCanWrite)
	// MakeGenericMethod(Type[] typeArguments) needs no Machine access at
	// all — unlike Invoke (which actually has to run the target method),
	// this just stamps the real closed type-argument names onto a NEW
	// MethodInfo wrapper (real MakeGenericMethod never mutates the
	// receiver), so it's a plain bcl.Native rather than a machineRegistry
	// entry like every other MethodInfo/PropertyInfo member here.
	register("System.Reflection.MethodInfo::MakeGenericMethod", true, methodInfoMakeGenericMethod)
}

func methodInfoMakeGenericMethod(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: MethodInfo.MakeGenericMethod expects (this, Type[])")
	}
	mi, ok := nativeOf[*nativeMethodInfo](args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: MethodInfo.MakeGenericMethod receiver is not a MethodInfo")
	}
	typeArgs, err := TypeArrayToFullNames(args[1])
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeMethodInfo{
		typeFullName: mi.typeFullName,
		methodName:   mi.methodName,
		genericArgs:  typeArgs,
	}}), nil
}

// MethodInfoGenericArgs returns a MethodInfo wrapper's own MakeGenericMethod
// type arguments, if any were ever attached — exported so internal/
// interpreter/reflection.go's methodInfoInvoke (which needs Machine
// access to actually call the target, unlike MakeGenericMethod itself)
// can pass them through to Machine.call as its real methodGenericArgs.
func MethodInfoGenericArgs(v runtime.Value) []string {
	mi, ok := nativeOf[*nativeMethodInfo](v)
	if !ok {
		return nil
	}
	return mi.genericArgs
}

func propertyInfoGetName(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("bcl: PropertyInfo.Name expects a receiver")
	}
	name, ok := propertyInfoName(args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: PropertyInfo.Name receiver is not a PropertyInfo")
	}
	return runtime.String(name), nil
}

func propertyInfoGetCanRead(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("bcl: PropertyInfo.CanRead expects a receiver")
	}
	pi, ok := nativeOf[*nativePropertyInfo](args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: PropertyInfo.CanRead receiver is not a PropertyInfo")
	}
	return runtime.Bool(pi.canRead), nil
}

func propertyInfoGetCanWrite(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("bcl: PropertyInfo.CanWrite expects a receiver")
	}
	pi, ok := nativeOf[*nativePropertyInfo](args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: PropertyInfo.CanWrite receiver is not a PropertyInfo")
	}
	return runtime.Bool(pi.canWrite), nil
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

// NewPropertyInfoValue builds a System.Reflection.PropertyInfo wrapper —
// called from internal/interpreter/reflection.go's Machine-aware
// GetProperties/GetProperty natives.
func NewPropertyInfoValue(typeFullName, propertyName string, canRead, canWrite bool) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativePropertyInfo{
		typeFullName: typeFullName,
		propertyName: propertyName,
		canRead:      canRead,
		canWrite:     canWrite,
	}})
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

// PropertyInfoParts unwraps a PropertyInfo wrapper value back to its
// plain data — same rationale as ConstructorInfoTypeFullName/
// MethodInfoParts/FieldInfoParts above.
func PropertyInfoParts(v runtime.Value) (typeFullName, propertyName string, canRead, canWrite bool, ok bool) {
	pi, ok := nativeOf[*nativePropertyInfo](v)
	if !ok {
		return "", "", false, false, false
	}
	return pi.typeFullName, pi.propertyName, pi.canRead, pi.canWrite, true
}

func propertyInfoName(v runtime.Value) (string, bool) {
	pi, ok := nativeOf[*nativePropertyInfo](v)
	if !ok {
		return "", false
	}
	return pi.propertyName, true
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
