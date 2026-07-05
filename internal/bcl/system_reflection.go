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

// nativeConstructorInfo's overloadIndex (Fase 3.52) picks out WHICH real
// .ctor overload this particular wrapper names, among however many
// resolveMemberParams(typeFullName, ".ctor") finds — needed once
// Type.GetConstructors() (plural) can hand out more than one distinct
// ConstructorInfo for the same type: without it, every element of that
// array would look identical and GetParameters() on any of them would
// answer with the same (first) overload's parameters regardless of
// which one the caller actually meant. 0 for a ConstructorInfo obtained
// via the singular Type.GetConstructor(Type[]) overload — "the one
// overload matching this exact signature", which existence was already
// confirmed against via Machine.ResolveMember at that call site,
// defaults to "first/only" the same way typeGetMethod's own 2-arg
// (name-only) shape already does.
type nativeConstructorInfo struct {
	typeFullName  string
	overloadIndex int
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

// nativeFieldInfo backs System.Reflection.FieldInfo (Fase 3.39, Type.
// GetField(s)). fieldTypeFullName (Fase 3.53) backs FieldType — unlike
// PropertyInfo.PropertyType (nativePropertyInfo.propertyTypeFullName),
// which has to read a getter/setter MethodDef's own return/parameter type
// since a property is just a pair of accessor methods with no signature
// of its own, a field's declared signature IS its type: assembly.go's
// resolveFields reads it straight off the Field table (metadata.
// ParseFieldSig + ir.SigTypeFullName) with no accessor indirection at
// all. "" (same "somehow unresolvable" fallback propertyTypeFullName
// uses) only for a FieldInfo obtained before ResolveFields existed, or a
// BCL type vmnet has no TypeDef for.
type nativeFieldInfo struct {
	typeFullName      string
	fieldName         string
	fieldTypeFullName string
}

// nativeParameterInfo backs System.Reflection.ParameterInfo (Fase 3.52,
// MethodBase.GetParameters) — plain data captured once, at GetParameters
// time, off the real Param/MethodSig metadata (assembly.go's
// resolveMemberParams); every ParameterInfo member here is a pure
// read of one of these three fields, needing no further Machine access.
type nativeParameterInfo struct {
	paramTypeFullName string
	name              string
	position          int
}

// nativePropertyInfo backs System.Reflection.PropertyInfo (Fase 3.51,
// Type.GetProperties/GetProperty) — canRead/canWrite come from the real
// get_Xxx/set_Xxx MethodDef linkage (assembly.go's resolveProperties),
// not guessed from the name, so a get-only or set-only property answers
// CanRead/CanWrite correctly and GetValue/SetValue can reject the
// unsupported direction with a real error instead of an opaque "method
// not found" from whatever m.call attempt would otherwise fail deep
// inside. propertyTypeFullName (Fase 3.52) backs PropertyType —
// Dapper's own reflection-based row-to-object mapper (SqlMapper's
// CreateParamInfoGenerator/GetSettableProps) reads it to pick a coercion
// path per column before ever calling GetValue/SetValue.
type nativePropertyInfo struct {
	typeFullName         string
	propertyName         string
	canRead, canWrite    bool
	propertyTypeFullName string
	// indexParamTypes is non-empty only for the narrow well-known-BCL-
	// indexer fallback (Fase 3.52, internal/interpreter/reflection.go's
	// wellKnownBclProperties — e.g. DbDataReader's real `this[int]`) —
	// every ordinary PropertyInfo from Type.GetProperties/GetProperty
	// against a real plugin TypeDef leaves this nil (vmnet's own
	// reflection here never models a plugin's own indexer specifically,
	// see propertyInfoGetIndexParameters' own doc comment).
	indexParamTypes []string
}

func init() {
	register("System.Reflection.PropertyInfo::get_Name", true, propertyInfoGetName)
	register("System.Reflection.PropertyInfo::get_CanRead", true, propertyInfoGetCanRead)
	register("System.Reflection.PropertyInfo::get_CanWrite", true, propertyInfoGetCanWrite)
	register("System.Reflection.PropertyInfo::get_PropertyType", true, propertyInfoGetPropertyType)
	// GetGetMethod/GetSetMethod (Fase 3.52) accept both the parameterless
	// overload and the (bool nonPublic) one — vmnet has no visibility
	// model for reflection at all (see PropertyInfoParts' own doc
	// comment), so both shapes answer identically: null when that
	// accessor doesn't exist, the accessor's own MethodInfo otherwise,
	// regardless of whether nonPublic was requested.
	register("System.Reflection.PropertyInfo::GetGetMethod", true, propertyInfoGetGetMethod)
	register("System.Reflection.PropertyInfo::GetSetMethod", true, propertyInfoGetSetMethod)
	// GetIndexParameters: real reflection returns the indexer's own index
	// parameters (`this[int i]`), empty for an ordinary property — every
	// PropertyInfo backed by a real plugin TypeDef (Type.GetProperties/
	// GetProperty, assembly.go's resolveProperties) comes from a plain
	// Property row with a get_Xxx/set_Xxx taking no extra index
	// parameters (an indexer would need one), so those always answer
	// empty here — real code checking `GetIndexParameters().Length == 0`
	// to skip indexers (a common pattern in exactly the ORM/serializer
	// code this whole reflection subsystem targets) sees the correct
	// answer. The one exception is the narrow well-known-BCL-property
	// fallback (wellKnownBclProperties, internal/interpreter/
	// reflection.go) for a real framework indexer like DbDataReader's
	// `this[int]` — those carry real indexParamTypes.
	register("System.Reflection.PropertyInfo::GetIndexParameters", true, propertyInfoGetIndexParameters)
	// MakeGenericMethod(Type[] typeArguments) needs no Machine access at
	// all — unlike Invoke (which actually has to run the target method),
	// this just stamps the real closed type-argument names onto a NEW
	// MethodInfo wrapper (real MakeGenericMethod never mutates the
	// receiver), so it's a plain bcl.Native rather than a machineRegistry
	// entry like every other MethodInfo/PropertyInfo member here.
	register("System.Reflection.MethodInfo::MakeGenericMethod", true, methodInfoMakeGenericMethod)
	// ParameterInfo (Fase 3.52, MethodBase.GetParameters) — plain reads
	// off nativeParameterInfo's own captured fields, no Machine access
	// needed (the resolving work already happened in
	// internal/interpreter/reflection.go's methodBaseGetParameters).
	register("System.Reflection.ParameterInfo::get_ParameterType", true, parameterInfoGetParameterType)
	register("System.Reflection.ParameterInfo::get_Name", true, parameterInfoGetName)
	register("System.Reflection.ParameterInfo::get_Position", true, parameterInfoGetPosition)
	// FieldInfo.FieldType (Fase 3.53) — a plain read off the wrapper's own
	// stored fieldTypeFullName (see nativeFieldInfo's own doc comment for
	// why, unlike PropertyType, this needs no accessor-method indirection
	// at all), so it stays a plain bcl.Native like ParameterType above
	// rather than needing Machine access.
	register("System.Reflection.FieldInfo::get_FieldType", true, fieldInfoGetFieldType)
	// MemberInfo.DeclaringType (Fase 3.53) — real reflection's
	// ConstructorInfo/MethodInfo/FieldInfo/PropertyInfo all inherit this
	// from MemberInfo WITHOUT overriding/re-declaring it themselves (only
	// RuntimeFieldInfo etc., the private concrete CLR types, actually
	// override it) — so a real compiler-emitted call site against any of
	// them (e.g. `fi.DeclaringType` where fi is statically typed
	// FieldInfo) callvirts "System.Reflection.MemberInfo::get_
	// DeclaringType" directly (confirmed via ilspycmd against a real
	// compiled test DLL), never "FieldInfo::get_DeclaringType" — the exact
	// same real-IL precedent get_Name's own registration comment above
	// documents (and originally got wrong the same way, Fase 3.41's own
	// bug). One shared registration handles every one of vmnet's four
	// wrapper types, each already storing its own owning typeFullName
	// (that's how GetValue/Invoke/GetParameters find their real target in
	// the first place) — no Machine access needed, unlike Invoke/GetValue
	// which actually have to run something.
	register("System.Reflection.MemberInfo::get_DeclaringType", true, memberInfoGetDeclaringType)
	// GetCustomAttributes/IsDefined (Fase 3.60) — a real, known limitation,
	// not a full implementation: vmnet has no CustomAttributeData/attribute-
	// blob-decoding subsystem yet (ECMA-335 §II.23.3 — a genuinely new,
	// sizable piece of work, deliberately deferred), so every receiver here
	// always answers "no custom attributes at all", regardless of what a
	// real assembly's metadata actually declares. Correct for the
	// overwhelming common case a defensive attribute check hits (there
	// really is no such attribute on this specific member/parameter) —
	// found via a real, load-bearing case: Microsoft.Extensions.
	// DependencyInjection's own real constructor-injection call-site
	// builder (CallSiteFactory.CreateArgumentCallSites) calls
	// ParameterInfo.GetCustomAttributes() on every constructor parameter
	// while resolving a service's real dependencies, and gracefully
	// proceeds when none are found — exactly like real code would for a
	// plain, unannotated constructor parameter. Would give a wrong answer
	// only for a caller that specifically depends on reading a real
	// attribute's data (e.g. FluentValidation's [Flags] checks, CsvHelper's
	// [Name]) — those need the full subsystem this isn't, and remain a
	// documented gap (docs/en/ROADMAP.md).
	for _, recv := range []string{
		"System.Reflection.ParameterInfo",
		"System.Reflection.MemberInfo",
		"System.Reflection.MethodInfo",
		"System.Reflection.ConstructorInfo",
		"System.Reflection.MethodBase",
		"System.Reflection.PropertyInfo",
		"System.Reflection.FieldInfo",
		"System.Type",
	} {
		register(recv+"::GetCustomAttributes", true, reflectionEmptyObjectArray)
		register(recv+"::IsDefined", true, reflectionFalse)
	}
	// Attribute.GetCustomAttribute(MemberInfo/ParameterInfo, Type[, bool])
	// — same "no attribute ever found" posture as GetCustomAttributes
	// above, just the singular real static-method shape.
	register("System.Attribute::GetCustomAttribute", true, reflectionNullValue)
	// CustomAttributeExtensions.GetCustomAttribute<T>(this MemberInfo) —
	// the generic extension-method spelling of the same real API
	// (found via Markdig's own Markdown.Version property reading its
	// containing assembly's AssemblyFileVersionAttribute). Same "no
	// attribute ever found" posture as System.Attribute::GetCustomAttribute
	// above — a plain bcl.Native despite being a generic method call site,
	// since vmnet's type-erased Value model means the answer (always
	// null) doesn't depend on what T actually closes over, so no
	// genericMachineRegistry entry is needed at all.
	register("System.Reflection.CustomAttributeExtensions::GetCustomAttribute", true, reflectionNullValue)
	register("System.Reflection.CustomAttributeExtensions::GetCustomAttributes", true, reflectionEmptyObjectArray)
	register("System.Reflection.CustomAttributeExtensions::IsDefined", true, reflectionFalse)
}

func reflectionEmptyObjectArray(args []runtime.Value) (runtime.Value, error) {
	return runtime.ArrRef(runtime.NewArray(0)), nil
}

func reflectionFalse(args []runtime.Value) (runtime.Value, error) {
	return runtime.Bool(false), nil
}

func reflectionNullValue(args []runtime.Value) (runtime.Value, error) {
	return runtime.Null(), nil
}

func fieldInfoGetFieldType(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("bcl: FieldInfo.FieldType expects a receiver")
	}
	fi, ok := nativeOf[*nativeFieldInfo](args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: FieldInfo.FieldType receiver is not a FieldInfo")
	}
	if fi.fieldTypeFullName == "" {
		return runtime.Null(), nil
	}
	return NewTypeValue(fi.fieldTypeFullName), nil
}

// memberInfoGetDeclaringType backs MemberInfo.DeclaringType for every one
// of the four concrete wrapper types registered above — each one already
// carries its own owning typeFullName (nativeConstructorInfo/
// nativeMethodInfo/nativeFieldInfo/nativePropertyInfo), so this just
// tries each shape in turn, the same "check every possible native shape"
// posture typeGetName (system_type.go) already uses to answer
// MemberInfo.Name for both a Type and a *nativeMemberInfo receiver.
func memberInfoGetDeclaringType(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("bcl: MemberInfo.DeclaringType expects a receiver")
	}
	if ci, ok := nativeOf[*nativeConstructorInfo](args[0]); ok {
		return NewTypeValue(ci.typeFullName), nil
	}
	if mi, ok := nativeOf[*nativeMethodInfo](args[0]); ok {
		return NewTypeValue(mi.typeFullName), nil
	}
	if fi, ok := nativeOf[*nativeFieldInfo](args[0]); ok {
		return NewTypeValue(fi.typeFullName), nil
	}
	if pi, ok := nativeOf[*nativePropertyInfo](args[0]); ok {
		return NewTypeValue(pi.typeFullName), nil
	}
	return runtime.Value{}, fmt.Errorf("bcl: MemberInfo.DeclaringType receiver is not a ConstructorInfo, MethodInfo, FieldInfo, or PropertyInfo")
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

func propertyInfoGetPropertyType(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("bcl: PropertyInfo.PropertyType expects a receiver")
	}
	pi, ok := nativeOf[*nativePropertyInfo](args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: PropertyInfo.PropertyType receiver is not a PropertyInfo")
	}
	if pi.propertyTypeFullName == "" {
		return runtime.Null(), nil
	}
	return NewTypeValue(pi.propertyTypeFullName), nil
}

// propertyInfoGetGetMethod/propertyInfoGetSetMethod build the accessor's
// own MethodInfo directly from the stored typeFullName/propertyName —
// no Machine access needed (unlike methodInfoInvoke, nothing is actually
// called here), so these stay plain bcl.Native like every other
// PropertyInfo member above.
func propertyInfoGetGetMethod(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: PropertyInfo.GetGetMethod expects a receiver")
	}
	pi, ok := nativeOf[*nativePropertyInfo](args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: PropertyInfo.GetGetMethod receiver is not a PropertyInfo")
	}
	if !pi.canRead {
		return runtime.Null(), nil
	}
	return NewMethodInfoValue(pi.typeFullName, "get_"+pi.propertyName), nil
}

func propertyInfoGetSetMethod(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: PropertyInfo.GetSetMethod expects a receiver")
	}
	pi, ok := nativeOf[*nativePropertyInfo](args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: PropertyInfo.GetSetMethod receiver is not a PropertyInfo")
	}
	if !pi.canWrite {
		return runtime.Null(), nil
	}
	return NewMethodInfoValue(pi.typeFullName, "set_"+pi.propertyName), nil
}

func propertyInfoGetIndexParameters(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("bcl: PropertyInfo.GetIndexParameters expects a receiver")
	}
	pi, ok := nativeOf[*nativePropertyInfo](args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: PropertyInfo.GetIndexParameters receiver is not a PropertyInfo")
	}
	elems := make([]runtime.Value, len(pi.indexParamTypes))
	for i, t := range pi.indexParamTypes {
		elems[i] = NewParameterInfoValue(t, fmt.Sprintf("index%d", i), i)
	}
	return runtime.ArrRef(&runtime.Array{Elems: elems}), nil
}

// NewParameterInfoValue builds a System.Reflection.ParameterInfo wrapper
// — called from internal/interpreter/reflection.go's Machine-aware
// methodBaseGetParameters.
func NewParameterInfoValue(paramTypeFullName, name string, position int) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeParameterInfo{
		paramTypeFullName: paramTypeFullName,
		name:              name,
		position:          position,
	}})
}

func parameterInfoGetParameterType(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("bcl: ParameterInfo.ParameterType expects a receiver")
	}
	pi, ok := nativeOf[*nativeParameterInfo](args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: ParameterInfo.ParameterType receiver is not a ParameterInfo")
	}
	return NewTypeValue(pi.paramTypeFullName), nil
}

func parameterInfoGetName(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("bcl: ParameterInfo.Name expects a receiver")
	}
	pi, ok := nativeOf[*nativeParameterInfo](args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: ParameterInfo.Name receiver is not a ParameterInfo")
	}
	return runtime.String(pi.name), nil
}

func parameterInfoGetPosition(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("bcl: ParameterInfo.Position expects a receiver")
	}
	pi, ok := nativeOf[*nativeParameterInfo](args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: ParameterInfo.Position receiver is not a ParameterInfo")
	}
	return runtime.Int32(int32(pi.position)), nil
}

// NewConstructorInfoValue/NewMethodInfoValue/NewFieldInfoValue build the
// respective System.Reflection wrapper values — called from
// internal/interpreter/reflection.go's Machine-aware
// GetConstructor/GetMethod/GetField natives.
func NewConstructorInfoValue(typeFullName string) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeConstructorInfo{typeFullName: typeFullName}})
}

// NewConstructorInfoValueAt builds a ConstructorInfo tagged with WHICH
// real .ctor overload it names (Fase 3.52, Type.GetConstructors — see
// nativeConstructorInfo.overloadIndex's own doc comment).
func NewConstructorInfoValueAt(typeFullName string, overloadIndex int) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeConstructorInfo{typeFullName: typeFullName, overloadIndex: overloadIndex}})
}

func NewMethodInfoValue(typeFullName, methodName string) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeMethodInfo{typeFullName: typeFullName, methodName: methodName}})
}

// NewFieldInfoValue builds a FieldInfo wrapper carrying its own real
// declared type (Fase 3.53, FieldInfo.FieldType) — fieldTypeFullName is
// "" for the handful of call sites that don't have a real resolved type
// name to hand (a BCL type vmnet has no TypeDef/FieldsResolver data for
// at all), which fieldInfoGetFieldType above then answers with null,
// matching PropertyInfo.PropertyType's own "" -> null convention.
func NewFieldInfoValue(typeFullName, fieldName, fieldTypeFullName string) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeFieldInfo{typeFullName: typeFullName, fieldName: fieldName, fieldTypeFullName: fieldTypeFullName}})
}

// NewPropertyInfoValue builds a System.Reflection.PropertyInfo wrapper —
// called from internal/interpreter/reflection.go's Machine-aware
// GetProperties/GetProperty natives.
func NewPropertyInfoValue(typeFullName, propertyName string, canRead, canWrite bool, propertyTypeFullName string) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativePropertyInfo{
		typeFullName:         typeFullName,
		propertyName:         propertyName,
		canRead:              canRead,
		canWrite:             canWrite,
		propertyTypeFullName: propertyTypeFullName,
	}})
}

// NewIndexerPropertyInfoValue is NewPropertyInfoValue plus indexParamTypes
// — called only from the narrow well-known-BCL-property fallback
// (internal/interpreter/reflection.go's wellKnownBclProperties) for a
// real framework indexer (e.g. DbDataReader's `this[int]`) vmnet has no
// TypeDef to read a real Property row's accessor signature from.
func NewIndexerPropertyInfoValue(typeFullName, propertyName string, canRead, canWrite bool, propertyTypeFullName string, indexParamTypes []string) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativePropertyInfo{
		typeFullName:         typeFullName,
		propertyName:         propertyName,
		canRead:              canRead,
		canWrite:             canWrite,
		propertyTypeFullName: propertyTypeFullName,
		indexParamTypes:      indexParamTypes,
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

// ConstructorInfoParts is ConstructorInfoTypeFullName plus overloadIndex
// (Fase 3.52) — used by internal/interpreter/reflection.go's
// methodBaseGetParameters to find the exact overload this wrapper names
// among Type.GetConstructors()'s possibly-several real .ctor overloads.
func ConstructorInfoParts(v runtime.Value) (typeFullName string, overloadIndex int, ok bool) {
	ci, ok := nativeOf[*nativeConstructorInfo](v)
	if !ok {
		return "", 0, false
	}
	return ci.typeFullName, ci.overloadIndex, true
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
