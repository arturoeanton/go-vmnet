package interpreter

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

func init() {
	machineRegistry["System.Type::IsAssignableFrom"] = typeIsAssignableFrom
	machineRegistry["System.Type::IsInstanceOfType"] = typeIsInstanceOfType
	machineRegistry["System.Type::IsSubclassOf"] = typeIsSubclassOf
	machineRegistry["System.Type::get_IsValueType"] = typeGetIsValueType
	machineRegistry["System.Type::get_IsEnum"] = typeGetIsEnum
	machineRegistry["System.Type::get_IsInterface"] = typeGetIsInterface
	machineRegistry["System.Type::get_IsAbstract"] = typeGetIsAbstract
	machineRegistry["System.Type::get_IsPrimitive"] = typeGetIsPrimitive
	machineRegistry["System.Type::get_BaseType"] = typeGetBaseType
	machineRegistry["System.Type::GetInterfaces"] = typeGetInterfaces
	machineRegistry["System.Type::GetType"] = typeStaticGetType
	machineRegistry["System.Enum::GetValues"] = enumGetValues
	machineRegistry["System.Enum::GetNames"] = enumGetNames
	machineRegistry["System.Enum::IsDefined"] = enumIsDefined
	machineRegistry["System.Enum::ToObject"] = enumToObject
	// Enum.Parse(Type, string[, bool ignoreCase]) is a plain, non-generic
	// static method (the Type argument carries the target enum, so no
	// method-generic-argument machinery is needed at all) — unlike
	// Enum.TryParse<TEnum>, which is itself a generic method and needs
	// genericMachineRegistry instead (see enumTryParseGeneric's own doc
	// comment).
	machineRegistry["System.Enum::Parse"] = enumParse
	genericMachineRegistry["System.Enum::TryParse"] = enumTryParseGeneric
	// Real reflection (Fase 3.39) — Type.GetConstructor/GetMethod/GetField
	// plus ConstructorInfo/MethodInfo/FieldInfo's own Invoke/GetValue.
	// Found via a real, common pattern: a reflection-based type registry
	// (NPOI's own RecordFactory, mapping ~200 discovered record types to
	// their real constructors) — not Reflection.Emit, no new code is ever
	// generated, every target here is a real MethodDef/Field this loop's
	// existing machinery (Machine.New/Machine.call/Type.FieldIndex)
	// already knows how to run.
	machineRegistry["System.Type::GetConstructor"] = typeGetConstructor
	machineRegistry["System.Type::GetMethod"] = typeGetMethod
	machineRegistry["System.Type::GetField"] = typeGetField
	machineRegistry["System.Reflection.ConstructorInfo::Invoke"] = constructorInfoInvoke
	machineRegistry["System.Reflection.MethodInfo::Invoke"] = methodInfoInvoke
	machineRegistry["System.Reflection.FieldInfo::GetValue"] = fieldInfoGetValue
	machineRegistry["System.Reflection.ConstructorInfo::op_Inequality"] = memberInfoOpInequality
	machineRegistry["System.Reflection.ConstructorInfo::op_Equality"] = memberInfoOpEquality
	machineRegistry["System.Reflection.MethodInfo::op_Inequality"] = memberInfoOpInequality
	machineRegistry["System.Reflection.MethodInfo::op_Equality"] = memberInfoOpEquality
	// The base MemberInfo name itself (Fase 3.64) — reached when the
	// compared receivers' declared static type is the MemberInfo base,
	// not a concrete subtype directly; found via FluentValidation's own
	// PropertyRule construction comparing two real MemberInfo values.
	machineRegistry["System.Reflection.MemberInfo::op_Inequality"] = memberInfoOpInequality
	machineRegistry["System.Reflection.MemberInfo::op_Equality"] = memberInfoOpEquality
	// Type.GetProperties/GetProperty plus PropertyInfo.GetValue/SetValue
	// (Fase 3.51) — same real-reflection posture as GetConstructor/
	// GetMethod/GetField above: every PropertyInfo here names a real
	// get_Xxx/set_Xxx MethodDef pair (assembly.go's resolveProperties),
	// invoked through the exact same Machine.call a normal property
	// access already goes through.
	machineRegistry["System.Type::GetProperties"] = typeGetProperties
	machineRegistry["System.Type::GetProperty"] = typeGetProperty
	machineRegistry["System.Reflection.PropertyInfo::GetValue"] = propertyInfoGetValue
	machineRegistry["System.Reflection.PropertyInfo::SetValue"] = propertyInfoSetValue
	machineRegistry["System.Reflection.PropertyInfo::op_Inequality"] = memberInfoOpInequality
	machineRegistry["System.Reflection.PropertyInfo::op_Equality"] = memberInfoOpEquality
	// Assembly.GetManifestResourceStream (Fase 3.40) — needs Machine
	// access to reach whichever assembly's ResolveManifestResource is
	// currently active (Machine.invoke swaps it per Fase 3.27's pattern),
	// unlike the plain bcl.Native stubs system_type.go registers for
	// GetExecutingAssembly/ToString/FullName.
	machineRegistry["System.Reflection.Assembly::GetManifestResourceStream"] = assemblyGetManifestResourceStream
	// Type.GetConstructors() (plural) plus MethodBase.GetParameters/
	// ParameterInfo (Fase 3.52) — Dapper's own constructor-based
	// row-to-object mapper enumerates every real .ctor overload this way
	// to find the one whose parameters best match a query's column set.
	// GetParameters is registered under all three of MethodBase's own
	// concrete subclasses reachable here: whichever one
	// receiverTypeName/bcl.NativeTypeName reports for the actual wrapper
	// (ConstructorInfo or MethodInfo) is what Machine.call's virtual-
	// dispatch ancestor walk tries first, regardless of which one a call
	// site's own declared static type names.
	machineRegistry["System.Type::GetConstructors"] = typeGetConstructors
	machineRegistry["System.Reflection.ConstructorInfo::GetParameters"] = methodBaseGetParameters
	machineRegistry["System.Reflection.MethodInfo::GetParameters"] = methodBaseGetParameters
	machineRegistry["System.Reflection.MethodBase::GetParameters"] = methodBaseGetParameters
	// MethodBase's own accessibility/modifier getters (Fase 3.60) — found
	// via a real, load-bearing case: Microsoft.Extensions.
	// DependencyInjection's own ConstructorMatcher (real .NET reflection-
	// based service activation) reads IsPublic while picking which of a
	// service implementation's constructors to invoke; ComponentModel.
	// Annotations/Configuration.Binder read a few of the others while
	// walking a validated/bound object's own members. Registered under
	// all three MethodBase subclasses reachable here, same rationale as
	// GetParameters above.
	for _, recv := range []string{"System.Reflection.ConstructorInfo", "System.Reflection.MethodInfo", "System.Reflection.MethodBase"} {
		machineRegistry[recv+"::get_IsPublic"] = methodBaseGetIsPublic
		machineRegistry[recv+"::get_IsPrivate"] = methodBaseGetIsPrivate
		machineRegistry[recv+"::get_IsFamily"] = methodBaseGetIsFamily
		machineRegistry[recv+"::get_IsAssembly"] = methodBaseGetIsAssembly
		machineRegistry[recv+"::get_IsStatic"] = methodBaseGetIsStatic
		machineRegistry[recv+"::get_IsVirtual"] = methodBaseGetIsVirtual
		machineRegistry[recv+"::get_IsAbstract"] = methodBaseGetIsAbstract
		machineRegistry[recv+"::get_IsFinal"] = methodBaseGetIsFinal
	}
	// Type.GetFields()/GetMethods() (Fase 3.53, plural, no-args overloads)
	// — found via a corpus-wide compatibility pass: 9 of 19 real NuGet
	// packages call GetFields()/GetMethods() (distinct from the
	// already-real singular GetField(name)/GetMethod(name) above) to
	// enumerate every declared field/method rather than look one up by
	// name, the same real, common "reflection-based registry" pattern
	// GetConstructor/GetProperties already target. Each backed by its own
	// new Machine.ResolveFields/ResolveMethods callback (assembly.go's
	// resolveFields/resolveMethods) walking TypeDefFieldRange/
	// TypeDefMethodRange directly, mirroring ResolveProperties' own
	// TypeDefPropertyRange walk.
	machineRegistry["System.Type::GetFields"] = typeGetFields
	machineRegistry["System.Type::GetMethods"] = typeGetMethods
}

// assemblyGetManifestResourceStream backs Assembly.GetManifestResource
// Stream(string name) — a real .NET assembly can embed arbitrary named
// byte blobs (icons, templates, fonts) directly in its own PE image;
// found via a real, load-bearing case: ClosedXML's own font-metrics
// engine loads four bundled .ttf files this way just from constructing
// an XLWorkbook, not from anything a caller does with fonts directly.
// Returns Null() for an unresolvable name, matching real semantics
// (GetManifestResourceStream never throws for a missing resource).
func assemblyGetManifestResourceStream(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 || args[1].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("interpreter: Assembly.GetManifestResourceStream expects a string name")
	}
	if m.ResolveManifestResource == nil {
		return runtime.Null(), nil
	}
	data, ok := m.ResolveManifestResource(args[1].Str)
	if !ok {
		return runtime.Null(), nil
	}
	return bcl.NewMemoryStreamValue(data), nil
}

// enumTypeMembers resolves a System.Type argument's real enum members via
// Machine.ResolveEnum (Fase 3.26) — only ever succeeds for a plugin-
// declared enum (a real TypeDef in the loaded assembly's own metadata);
// a BCL-only enum like System.DayOfWeek fails here, since vmnet has no
// BCL enum member database at all (documented limitation, same reasoning
// as every other "no real BCL metadata" gap in this project).
func enumTypeMembers(m *Machine, typeArg runtime.Value) (names []string, values []int64, err error) {
	fullName, ok := bcl.TypeFullNameOf(typeArg)
	if !ok {
		return nil, nil, fmt.Errorf("interpreter: Enum method expects a Type argument")
	}
	if m.ResolveEnum == nil {
		return nil, nil, fmt.Errorf("interpreter: no enum member data available for %s", fullName)
	}
	names, values, ok = m.ResolveEnum(bcl.GenericOpenName(fullName))
	if !ok {
		return nil, nil, fmt.Errorf("interpreter: %s is not a resolvable plugin enum (vmnet has no BCL enum member database)", fullName)
	}
	return names, values, nil
}

func enumGetValues(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enum.GetValues expects 1 argument")
	}
	_, values, err := enumTypeMembers(m, args[0])
	if err != nil {
		return runtime.Value{}, err
	}
	elems := make([]runtime.Value, len(values))
	for i, v := range values {
		elems[i] = runtime.Int32(int32(v))
	}
	return runtime.ArrRef(&runtime.Array{Elems: elems}), nil
}

func enumGetNames(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enum.GetNames expects 1 argument")
	}
	names, _, err := enumTypeMembers(m, args[0])
	if err != nil {
		return runtime.Value{}, err
	}
	elems := make([]runtime.Value, len(names))
	for i, n := range names {
		elems[i] = runtime.String(n)
	}
	return runtime.ArrRef(&runtime.Array{Elems: elems}), nil
}

// enumIsDefined accepts either the underlying integer value or the
// member's name (both real overload shapes of Enum.IsDefined(Type,
// object) — vmnet's call dispatch doesn't distinguish overloads, so the
// argument's own Kind picks the comparison, same approach every other
// multi-overload native in this project uses).
func enumIsDefined(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enum.IsDefined expects 2 arguments")
	}
	names, values, err := enumTypeMembers(m, args[0])
	if err != nil {
		return runtime.Value{}, err
	}
	switch args[1].Kind {
	case runtime.KindString:
		for _, n := range names {
			if n == args[1].Str {
				return runtime.Bool(true), nil
			}
		}
	default:
		target, err := toInt64(args[1])
		if err != nil {
			return runtime.Value{}, fmt.Errorf("interpreter: Enum.IsDefined: %w", err)
		}
		for _, v := range values {
			if v == target {
				return runtime.Bool(true), nil
			}
		}
	}
	return runtime.Bool(false), nil
}

// enumToObject constructs a boxed enum instance from its underlying
// value — a no-op in vmnet's Value model, where an enum instance is
// already just its underlying integer Kind (boxing never changes
// representation here, see objectToString's doc comment,
// internal/bcl/system_object.go). Doesn't validate the value is actually
// a defined member — real Enum.ToObject doesn't either.
func enumToObject(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enum.ToObject expects 2 arguments")
	}
	switch args[1].Kind {
	case runtime.KindI4:
		return args[1], nil
	case runtime.KindI8:
		return runtime.Int32(int32(args[1].I8)), nil
	default:
		return runtime.Value{}, fmt.Errorf("interpreter: Enum.ToObject: unsupported value kind %v", args[1].Kind)
	}
}

// enumParse backs the non-generic Enum.Parse(Type enumType, string value
// [, bool ignoreCase]) — a real, common pattern (config/deserialization
// code turning a stored string back into an enum member) distinct from
// the generic Enum.TryParse<TEnum> below only in how the target enum
// type reaches this native (a real System.Type argument here, a method
// generic argument there) — both ultimately just search the same
// name/value pairs enumTypeMembers already extracts for GetValues/
// GetNames. Also accepts the underlying integer as a string (e.g. "2"),
// matching real Enum.Parse's own documented behavior of treating a
// purely-numeric string as the raw underlying value regardless of
// whether it's a defined member.
func enumParse(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enum.Parse expects (Type, string[, bool])")
	}
	if args[1].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("interpreter: Enum.Parse expects a string value")
	}
	names, values, err := enumTypeMembers(m, args[0])
	if err != nil {
		return runtime.Value{}, err
	}
	text := args[1].Str
	ignoreCase := len(args) >= 3 && args[2].Truthy()
	if v, ok := matchEnumMember(names, values, text, ignoreCase); ok {
		return runtime.Int32(int32(v)), nil
	}
	if n, convErr := strconv.ParseInt(strings.TrimSpace(text), 10, 64); convErr == nil {
		return runtime.Int32(int32(n)), nil
	}
	return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentException", Message: fmt.Sprintf("Requested value '%s' was not found.", text)}
}

// matchEnumMember finds text among names (case-sensitively, or via
// strings.EqualFold when ignoreCase), returning its underlying value —
// shared by enumParse and enumTryParseGeneric so both agree on exactly
// the same matching rule.
func matchEnumMember(names []string, values []int64, text string, ignoreCase bool) (int64, bool) {
	for i, n := range names {
		if n == text || (ignoreCase && strings.EqualFold(n, text)) {
			return values[i], true
		}
	}
	return 0, false
}

// enumTryParseGeneric backs Enum.TryParse<TEnum>(string value[, bool
// ignoreCase], out TEnum result) — unlike Enum.Parse(Type, string)
// above, TryParse<TEnum> is itself a generic METHOD (TEnum is a method
// type parameter, not a regular argument), the same
// ir.Call.MethodGenericArgs shape Activator.CreateInstance<T> uses (see
// its own doc comment) — hence genericMachineRegistry rather than
// machineRegistry. false + result=default(TEnum) (zero) for an
// unresolvable name, matching real TryParse's own "never throws" no-match
// contract (unlike Parse, which throws ArgumentException).
func enumTryParseGeneric(m *Machine, args []runtime.Value, methodGenericArgs []string, depth int, instrCount *int64) (runtime.Value, error) {
	if len(methodGenericArgs) < 1 || methodGenericArgs[0] == "" {
		return runtime.Value{}, fmt.Errorf("interpreter: Enum.TryParse<TEnum>: TEnum could not be resolved (generic method chaining through its own open type parameter)")
	}
	if m.ResolveEnum == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: no enum member data available for %s", methodGenericArgs[0])
	}
	names, values, ok := m.ResolveEnum(bcl.GenericOpenName(methodGenericArgs[0]))
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: %s is not a resolvable plugin enum (vmnet has no BCL enum member database)", methodGenericArgs[0])
	}
	if len(args) < 2 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("interpreter: Enum.TryParse expects a string value")
	}
	text := args[0].Str
	var outRef *runtime.Value
	for i := len(args) - 1; i >= 0; i-- {
		if args[i].Kind == runtime.KindRef && args[i].Ref != nil {
			outRef = args[i].Ref
			break
		}
	}
	if outRef == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: Enum.TryParse expects an out parameter")
	}
	ignoreCase := false
	for _, a := range args[1 : len(args)-1] {
		if a.Kind == runtime.KindI4 {
			ignoreCase = a.Truthy()
		}
	}
	if v, ok := matchEnumMember(names, values, text, ignoreCase); ok {
		*outRef = runtime.Int32(int32(v))
		return runtime.Bool(true), nil
	}
	*outRef = runtime.Int32(0)
	return runtime.Bool(false), nil
}

// bclPrimitiveValueTypes/bclKnownInterfaces are hand-maintained, mirroring
// exceptionBaseType/interfaceDispatchTargets (typecheck.go/calls.go):
// vmnet has no TypeDef at all for these — no field layout, no metadata —
// so their reflection classification (Fase 3.25) can only come from a
// fixed list of well-known BCL names, same reasoning as every other
// "vmnet doesn't have real BCL metadata" fallback in this project.
var bclPrimitiveValueTypes = map[string]bool{
	"System.Boolean": true, "System.Char": true, "System.SByte": true, "System.Byte": true,
	"System.Int16": true, "System.UInt16": true, "System.Int32": true, "System.UInt32": true,
	"System.Int64": true, "System.UInt64": true, "System.Single": true, "System.Double": true,
	"System.IntPtr": true, "System.UIntPtr": true, "System.Decimal": true,
}

var bclKnownInterfaces = map[string]bool{
	"System.IDisposable":                               true,
	"System.IComparable":                               true,
	"System.IComparable`1":                             true,
	"System.IEquatable`1":                              true,
	"System.ICloneable":                                true,
	"System.Collections.IEnumerable":                   true,
	"System.Collections.Generic.IEnumerable`1":         true,
	"System.Collections.IEnumerator":                   true,
	"System.Collections.Generic.IEnumerator`1":         true,
	"System.Collections.ICollection":                   true,
	"System.Collections.Generic.ICollection`1":         true,
	"System.Collections.IList":                         true,
	"System.Collections.Generic.IList`1":               true,
	"System.Collections.Generic.IDictionary`2":         true,
	"System.Collections.Generic.IReadOnlyList`1":       true,
	"System.Collections.Generic.IReadOnlyCollection`1": true,
	"System.Collections.Generic.IEqualityComparer`1":   true,
}

// classifyTypeByName answers IsValueType/IsEnum/IsInterface for a type
// FullName the same way regardless of caller: hardcoded BCL knowledge
// first (primitives, well-known interfaces, vmnet's own synthetic BCL
// value types via bcl.LookupValueType), then a real plugin TypeDef via
// Machine.ResolveType, whose flags (Fase 3.25: runtime.Type.IsValueType/
// IsEnum/IsInterface) are now real. An unresolvable name (a BCL type
// vmnet doesn't model at all) defaults to "ordinary reference type" —
// false/false/false — the least surprising guess for arbitrary code that
// doesn't gate behavior on knowing this precisely.
func classifyTypeByName(m *Machine, fullName string) (isValueType, isEnum, isInterface, isAbstract bool) {
	open := bcl.GenericOpenName(fullName)
	if bclPrimitiveValueTypes[open] {
		return true, false, false, false
	}
	if bclKnownInterfaces[open] {
		return false, false, true, true
	}
	if _, ok := bcl.LookupValueType(open); ok {
		return true, false, false, false
	}
	if m.ResolveType == nil {
		return false, false, false, false
	}
	t, err := m.ResolveType(open)
	if err != nil {
		return false, false, false, false
	}
	return t.IsValueType, t.IsEnum, t.IsInterface, t.IsAbstract
}

func typeGetIsValueType(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	fullName, ok := bcl.TypeFullNameOf(argsSelf(args))
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.IsValueType receiver is not a Type")
	}
	isValueType, _, _, _ := classifyTypeByName(m, fullName)
	return runtime.Bool(isValueType), nil
}

func typeGetIsEnum(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	fullName, ok := bcl.TypeFullNameOf(argsSelf(args))
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.IsEnum receiver is not a Type")
	}
	_, isEnum, _, _ := classifyTypeByName(m, fullName)
	return runtime.Bool(isEnum), nil
}

func typeGetIsInterface(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	fullName, ok := bcl.TypeFullNameOf(argsSelf(args))
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.IsInterface receiver is not a Type")
	}
	_, _, isInterface, _ := classifyTypeByName(m, fullName)
	return runtime.Bool(isInterface), nil
}

func typeGetIsAbstract(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	fullName, ok := bcl.TypeFullNameOf(argsSelf(args))
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.IsAbstract receiver is not a Type")
	}
	_, _, _, isAbstract := classifyTypeByName(m, fullName)
	return runtime.Bool(isAbstract), nil
}

// bclPrimitiveTypes answers Type.IsPrimitive — a DIFFERENT, narrower set
// than bclPrimitiveValueTypes' "IsValueType" list: real .NET's IsPrimitive
// specifically excludes Decimal (a value type, but not one of the CLR's
// own hardware-primitive kinds) even though it's every bit as much a
// value type as Int32. Found via a real, load-bearing case: System.Span's
// own internal SpanHelpers+PerTypeValues`1..cctor checks typeof(T).
// IsPrimitive, reached just from ClosedXML's own ReadOnlySpan<byte> use
// of embedded font data.
var bclPrimitiveTypes = map[string]bool{
	"System.Boolean": true, "System.Char": true, "System.SByte": true, "System.Byte": true,
	"System.Int16": true, "System.UInt16": true, "System.Int32": true, "System.UInt32": true,
	"System.Int64": true, "System.UInt64": true, "System.Single": true, "System.Double": true,
	"System.IntPtr": true, "System.UIntPtr": true,
}

func typeGetIsPrimitive(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	fullName, ok := bcl.TypeFullNameOf(argsSelf(args))
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.IsPrimitive receiver is not a Type")
	}
	return runtime.Bool(bclPrimitiveTypes[bcl.GenericOpenName(fullName)]), nil
}

// argsSelf reads the receiver out of a 1-arg call (every get_Xxx property
// native here takes just the Type receiver) — a tiny helper so each
// wrapper above stays one line instead of repeating the len(args)!=1
// check three times identically.
func argsSelf(args []runtime.Value) runtime.Value {
	if len(args) == 0 {
		return runtime.Null()
	}
	return args[0]
}

// typeFullNameOfOpen is bcl.TypeFullNameOf, normalized through
// bcl.GenericOpenName — every real reflection lookup below
// (GetConstructor(s)/GetMethod/GetField/GetProperty(ies)) ultimately
// resolves against a TypeDef via Machine.ResolveMember/ResolveProperties/
// ResolveMemberParams, none of which can ever find a CLOSED generic
// instantiation's own encoded name (e.g.
// "Outer+Inner`1[[System.Data.DataTable]]", ir/builder.go's
// sigTypeFullName encoding for typeof(T)/MakeGenericType) — there's only
// ever one TypeDef per open/unbound generic type in real metadata
// (ECMA-335), never a separate one per closed instantiation. Every
// plain (non-generic) name passes through unchanged, so this is safe to
// use unconditionally wherever bcl.TypeFullNameOf was used directly
// before. Found via a real, load-bearing case (Fase 3.52): Dapper's own
// SqlMapper static constructor reflects over
// TypeHandlerCache<DataTable>/<XmlDocument>/<XDocument>/<XElement> (a
// closed generic obtained via Type.MakeGenericType) to cache each one's
// own SetHandler method — GetMethod silently returning null there (this
// bug, unfixed) crashes the cctor the instant Dapper.SqlMapper is
// touched at all, before a single real query runs.
func typeFullNameOfOpen(v runtime.Value) (string, bool) {
	name, ok := bcl.TypeFullNameOf(v)
	if !ok {
		return "", false
	}
	return bcl.GenericOpenName(name), true
}

// typeGetBaseType matches real Type.BaseType semantics for the three
// shapes vmnet can classify: an interface or System.Object itself has no
// base (null); a struct/enum's implicit base is System.ValueType/
// System.Enum (never tracked in BaseTypeFullName — Fase 3.7 explicitly
// skips base-field-inheritance for value types, since C# structs can't
// have a user-defined base at all); an ordinary class uses its real
// BaseTypeFullName if resolvable, defaulting to System.Object.
func typeGetBaseType(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	fullName, ok := bcl.TypeFullNameOf(argsSelf(args))
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.BaseType receiver is not a Type")
	}
	if fullName == "System.Object" {
		return runtime.Null(), nil
	}
	open := bcl.GenericOpenName(fullName)
	isValueType, isEnum, isInterface, _ := classifyTypeByName(m, fullName)
	if isInterface {
		return runtime.Null(), nil
	}
	if isEnum {
		return bcl.NewTypeValue("System.Enum"), nil
	}
	if isValueType {
		return bcl.NewTypeValue("System.ValueType"), nil
	}
	if m.ResolveType != nil {
		if t, err := m.ResolveType(open); err == nil && t.BaseTypeFullName != "" {
			return bcl.NewTypeValue(t.BaseTypeFullName), nil
		}
	}
	return bcl.NewTypeValue("System.Object"), nil
}

// typeGetInterfaces returns a plugin type's directly-implemented
// interfaces (runtime.Type.Interfaces, Fase 3.8) — not transitively
// expanded, and empty for any BCL type vmnet doesn't have a TypeDef for
// (documented simplification: no BCL interface-implementation database
// exists here, only what a real TypeDef records).
func typeGetInterfaces(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	fullName, ok := bcl.TypeFullNameOf(argsSelf(args))
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.GetInterfaces receiver is not a Type")
	}
	if m.ResolveType == nil {
		return runtime.ArrRef(runtime.NewArray(0)), nil
	}
	t, err := m.ResolveType(bcl.GenericOpenName(fullName))
	if err != nil {
		return runtime.ArrRef(runtime.NewArray(0)), nil
	}
	elems := make([]runtime.Value, len(t.Interfaces))
	for i, name := range t.Interfaces {
		elems[i] = bcl.NewTypeValue(name)
	}
	return runtime.ArrRef(&runtime.Array{Elems: elems}), nil
}

// typeStaticGetType backs Type.GetType(string) — resolves a plugin type
// by name via Machine.ResolveType, or a well-known vmnet-native BCL value
// type via bcl.LookupValueType; anything else (a real cross-assembly
// lookup, which needs an assembly-qualified name and a loader vmnet
// doesn't have) returns null, matching real Type.GetType's own contract
// for a name it can't resolve (rather than throwing).
func typeStaticGetType(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.GetType expects a string argument")
	}
	name := args[0].Str
	if _, ok := bcl.LookupValueType(bcl.GenericOpenName(name)); ok {
		return bcl.NewTypeValue(name), nil
	}
	if m.ResolveType != nil {
		if _, err := m.ResolveType(bcl.GenericOpenName(name)); err == nil {
			return bcl.NewTypeValue(name), nil
		}
	}
	return runtime.Null(), nil
}

// typeIsAssignableFrom implements Type.IsAssignableFrom(Type) — deferred
// out of Fase 3.14 as needing Machine access (a plain bcl.Native can't
// walk the real class/interface hierarchy, since that needs
// Machine.ResolveType), now mechanically simple once the Machine-aware
// native registry existed for LINQ (Fase 3.15). Both operands are
// System.Type values carrying only a FullName string (bcl.NewTypeValue),
// so this re-derives isAssignableTo's logic (Fase 3.8) starting from a
// type NAME rather than an already-known runtime.Value/Kind: an exact
// name match (or either side being System.Object) short-circuits, then
// the candidate's real TypeDef is resolved and walked via typeMatches —
// the same hierarchy walk isinst/castclass and exception catch-matching
// (Fase 3.13) already use.
func typeIsAssignableFrom(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.IsAssignableFrom expects (this, other)")
	}
	if args[1].Kind == runtime.KindNull {
		return runtime.Bool(false), nil
	}
	target, ok := bcl.TypeFullNameOf(args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.IsAssignableFrom receiver is not a Type")
	}
	candidate, ok := bcl.TypeFullNameOf(args[1])
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.IsAssignableFrom argument is not a Type")
	}
	if target == candidate || target == "System.Object" {
		return runtime.Bool(true), nil
	}
	if m.ResolveType == nil {
		return runtime.Bool(false), nil
	}
	t, err := m.ResolveType(candidate)
	if err != nil {
		return runtime.Bool(false), nil
	}
	return runtime.Bool(m.typeMatches(t, target)), nil
}

// typeIsSubclassOf implements Type.IsSubclassOf(Type c) — unlike
// IsAssignableFrom/IsInstanceOfType above, this walks ONLY the real class
// (BaseTypeFullName) chain, never interfaces (a real, documented
// difference: IsSubclassOf(typeof(ISomeInterface)) is always false even
// for an implementing class, matching real .NET), and requires a STRICT
// ancestor — the receiver is never its own subclass, unlike
// IsAssignableFrom's own reflexive true. Found via Markdig's own
// MarkdownObjectExtensions.Descendants<T>, filtering a tree by real type.
func typeIsSubclassOf(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.IsSubclassOf expects (this, c)")
	}
	receiver, ok := bcl.TypeFullNameOf(args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.IsSubclassOf receiver is not a Type")
	}
	ancestor, ok := bcl.TypeFullNameOf(args[1])
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.IsSubclassOf argument is not a Type")
	}
	if m.ResolveType == nil {
		return runtime.Bool(false), nil
	}
	t, err := m.ResolveType(receiver)
	if err != nil {
		return runtime.Bool(false), nil
	}
	for t.BaseTypeFullName != "" {
		if t.BaseTypeFullName == ancestor {
			return runtime.Bool(true), nil
		}
		next, err := m.ResolveType(t.BaseTypeFullName)
		if err != nil {
			// A BCL base name past this point (e.g. "System.Object") has no
			// real TypeDef for m.ResolveType to walk further, but real
			// IsSubclassOf(typeof(object)) is true for absolutely any
			// class — the walk reaching here at all (past the receiver's
			// own real TypeDef chain) already confirms this IS a class, so
			// "object" specifically still matches even though the walk
			// itself can't continue past it.
			return runtime.Bool(ancestor == "System.Object"), nil
		}
		t = next
	}
	return runtime.Bool(false), nil
}

// typeIsInstanceOfType implements Type.IsInstanceOfType(object) — the
// mirror image of typeIsAssignableFrom above: instead of comparing two
// Type values by name, the second argument here is an actual runtime
// value (any Kind at all, not necessarily a Type), so this reuses
// isAssignableTo (typecheck.go) directly — the exact same real
// inheritance-aware check isinst/castclass and exception catch-matching
// already use — rather than re-deriving it from a resolved Type name a
// second time. Found via a real, load-bearing case: Microsoft.Extensions.
// DependencyInjection's own ServiceProvider validates a resolved
// implementation instance against its registered service Type this way
// before caching it.
func typeIsInstanceOfType(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.IsInstanceOfType expects (this, obj)")
	}
	// A null object is never an instance of anything, unlike
	// IsAssignableFrom(null Type) above (a different, Type-vs-Type
	// question) — matches real IsInstanceOfType's own documented
	// behavior exactly.
	if args[1].Kind == runtime.KindNull {
		return runtime.Bool(false), nil
	}
	target, ok := bcl.TypeFullNameOf(args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.IsInstanceOfType receiver is not a Type")
	}
	return runtime.Bool(m.isAssignableTo(args[1], target)), nil
}

// typeGetConstructor backs Type.GetConstructor(Type[] parameterTypes) —
// real reflection (Fase 3.39), not Reflection.Emit: finds a real .ctor
// matching the declared parameter types via Machine.ResolveMember, and
// wraps it as a ConstructorInfo if found (null otherwise, matching real
// semantics for "no matching constructor").
func typeGetConstructor(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.GetConstructor expects (this, Type[])")
	}
	typeFullName, ok := typeFullNameOfOpen(args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.GetConstructor receiver is not a Type")
	}
	paramNames, err := bcl.TypeArrayToFullNames(args[1])
	if err != nil {
		return runtime.Value{}, err
	}
	if m.ResolveMember == nil {
		return runtime.Null(), nil
	}
	if _, ok := m.ResolveMember(typeFullName, ".ctor", paramNames); !ok {
		return runtime.Null(), nil
	}
	return bcl.NewConstructorInfoValue(typeFullName), nil
}

// typeGetConstructors backs Type.GetConstructors() (Fase 3.52, plural,
// no signature argument) — every real .ctor overload typeFullName
// declares, each wrapped as its own ConstructorInfo. Empty (not an
// error) for a BCL type vmnet has no TypeDef for, or a type with only
// the compiler-synthesized default constructor (no real MethodDef row
// to enumerate) — matching GetInterfaces/GetProperties' own "no
// metadata, no results" convention.
func typeGetConstructors(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.GetConstructors expects a receiver")
	}
	typeFullName, ok := typeFullNameOfOpen(args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.GetConstructors receiver is not a Type")
	}
	if m.ResolveMemberParams == nil {
		return runtime.ArrRef(runtime.NewArray(0)), nil
	}
	paramTypes, _, ok := m.ResolveMemberParams(typeFullName, ".ctor")
	if !ok {
		return runtime.ArrRef(runtime.NewArray(0)), nil
	}
	// Each ConstructorInfo remembers WHICH overload (its own index into
	// this same ResolveMemberParams(typeFullName, ".ctor") result) it
	// names — methodBaseGetParameters re-resolves against that exact
	// index rather than anything captured here, so a later mutation of
	// the underlying metadata view (there isn't one in practice, but the
	// two call sites deliberately stay independent) can't disagree.
	elems := make([]runtime.Value, len(paramTypes))
	for i := range paramTypes {
		elems[i] = bcl.NewConstructorInfoValueAt(typeFullName, i)
	}
	return runtime.ArrRef(&runtime.Array{Elems: elems}), nil
}

// methodBaseGetParameters backs MethodBase.GetParameters() for both
// ConstructorInfo and MethodInfo receivers (Fase 3.52) — re-resolves the
// receiver's own real parameter list via Machine.ResolveMemberParams
// rather than something captured at GetConstructor(s)/GetMethod time, so
// no extra state needs to ride along on the wrapper beyond a plain
// overload index. A ConstructorInfo from Type.GetConstructors() carries
// its own real overloadIndex (bcl.ConstructorInfoParts), so each element
// of that array answers with ITS OWN parameters, not just the first
// one's. A MethodInfo has no such index (Type.GetMethod only ever
// resolves a single method by name, never enumerates same-named
// overloads — Type.GetMethods() plural isn't implemented at all, see
// docs/en/ROADMAP.md), so it always answers with the first overload
// found — the same "first match, no signature to disambiguate against"
// posture typeGetMethod's own 2-arg (name-only) shape already documents
// accepting.
func methodBaseGetParameters(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: MethodBase.GetParameters expects a receiver")
	}
	var typeFullName, memberName string
	overloadIndex := 0
	if tfn, idx, ok := bcl.ConstructorInfoParts(args[0]); ok {
		typeFullName, memberName, overloadIndex = tfn, ".ctor", idx
	} else if tfn, mn, ok := bcl.MethodInfoParts(args[0]); ok {
		typeFullName, memberName = tfn, mn
	} else {
		return runtime.Value{}, fmt.Errorf("interpreter: MethodBase.GetParameters receiver is not a ConstructorInfo or MethodInfo")
	}
	if m.ResolveMemberParams == nil {
		return runtime.ArrRef(runtime.NewArray(0)), nil
	}
	paramTypes, paramNames, ok := m.ResolveMemberParams(typeFullName, memberName)
	if !ok || overloadIndex >= len(paramTypes) {
		return runtime.ArrRef(runtime.NewArray(0)), nil
	}
	types, names := paramTypes[overloadIndex], paramNames[overloadIndex]
	elems := make([]runtime.Value, len(types))
	for i := range types {
		elems[i] = bcl.NewParameterInfoValue(types[i], names[i], i)
	}
	return runtime.ArrRef(&runtime.Array{Elems: elems}), nil
}

// Raw ECMA-335 MethodAttributes bit values (§II.23.1.10) — MemberAccessMask
// is the low 3 bits; Static/Virtual/Abstract/Final are single bits above it.
// Mirrors assembly.go's own fieldAttrStatic/fieldAttrLiteral constants for
// FieldAttributes, one layer up in the package graph (these are needed
// here, in internal/interpreter, not assembly.go's root package).
const (
	methodAttrMemberAccessMask = 0x0007
	methodAttrPrivate          = 0x0001
	methodAttrFamANDAssem      = 0x0002
	methodAttrAssembly         = 0x0003
	methodAttrFamily           = 0x0004
	methodAttrFamORAssem       = 0x0005
	methodAttrPublic           = 0x0006
	methodAttrStatic           = 0x0010
	methodAttrFinal            = 0x0020
	methodAttrVirtual          = 0x0040
	methodAttrAbstract         = 0x0400
)

// methodBaseFlags re-resolves a ConstructorInfo/MethodInfo receiver's own
// raw MethodAttributes bitmask via Machine.ResolveMemberFlags (Fase
// 3.60) — same dual-branch receiver handling and "first overload, no
// signature to disambiguate against" MethodInfo posture as
// methodBaseGetParameters above, since a MethodBase wrapper's identity is
// exactly the same (typeFullName, memberName[, overloadIndex]) triple
// either helper needs.
func methodBaseFlags(m *Machine, args []runtime.Value) (uint16, error) {
	if len(args) != 1 {
		return 0, fmt.Errorf("interpreter: MethodBase accessibility property expects a receiver")
	}
	var typeFullName, memberName string
	overloadIndex := 0
	if tfn, idx, ok := bcl.ConstructorInfoParts(args[0]); ok {
		typeFullName, memberName, overloadIndex = tfn, ".ctor", idx
	} else if tfn, mn, ok := bcl.MethodInfoParts(args[0]); ok {
		typeFullName, memberName = tfn, mn
	} else {
		return 0, fmt.Errorf("interpreter: MethodBase accessibility property receiver is not a ConstructorInfo or MethodInfo")
	}
	if m.ResolveMemberFlags == nil {
		return 0, nil
	}
	flags, ok := m.ResolveMemberFlags(typeFullName, memberName)
	if !ok || overloadIndex >= len(flags) {
		return 0, nil
	}
	return flags[overloadIndex], nil
}

func methodBaseGetIsPublic(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	f, err := methodBaseFlags(m, args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(f&methodAttrMemberAccessMask == methodAttrPublic), nil
}

func methodBaseGetIsPrivate(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	f, err := methodBaseFlags(m, args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(f&methodAttrMemberAccessMask == methodAttrPrivate), nil
}

// methodBaseGetIsFamily backs MethodBase.IsFamily — real C# `protected`.
func methodBaseGetIsFamily(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	f, err := methodBaseFlags(m, args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(f&methodAttrMemberAccessMask == methodAttrFamily), nil
}

// methodBaseGetIsAssembly backs MethodBase.IsAssembly — real C# `internal`.
func methodBaseGetIsAssembly(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	f, err := methodBaseFlags(m, args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(f&methodAttrMemberAccessMask == methodAttrAssembly), nil
}

func methodBaseGetIsStatic(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	f, err := methodBaseFlags(m, args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(f&methodAttrStatic != 0), nil
}

func methodBaseGetIsVirtual(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	f, err := methodBaseFlags(m, args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(f&methodAttrVirtual != 0), nil
}

func methodBaseGetIsAbstract(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	f, err := methodBaseFlags(m, args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(f&methodAttrAbstract != 0), nil
}

func methodBaseGetIsFinal(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	f, err := methodBaseFlags(m, args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(f&methodAttrFinal != 0), nil
}

// typeGetMethod backs every real Type.GetMethod overload sharing the
// (string name, ...) shape: the simple GetMethod(name) (found via a real,
// common pattern: a generic method like `T Identity<T>(T)`, whose
// MakeGenericMethod call site has no way to spell out a still-open T in
// a Type[] up front, so real code always looks it up by bare name
// first), GetMethod(name, Type[]), GetMethod(name, BindingFlags), and
// GetMethod(name, BindingFlags, Binder, Type[], ParameterModifier[])
// (Fase 3.52, found via Dapper's own SqlMapper static ctor, which uses
// exactly this 5-argument overload through its own GetPublicInstanceMethod
// helper). Real .NET overloads this on ARITY, but every one of them past
// the bare name takes its own mix of BindingFlags/Binder/
// ParameterModifier[] arguments that are never a Type[] — rather than
// hardcoding which positional argument index holds the signature for
// each shape, this scans every argument after the name for the first
// real Type[] (bcl.TypeArrayToFullNames succeeds only for an actual
// Type[], returning an error for anything else, which this treats as
// "not this argument" rather than a hard failure). paramNames stays nil
// when no Type[] argument is found at all (matching by name only) — a
// real Go nil, not just an empty slice — which resolveMember
// (assembly.go) treats as "match by name only, any signature" rather
// than "match a zero-parameter method": see its own doc comment for why
// that distinction matters here specifically.
func typeGetMethod(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.GetMethod expects (this, name, ...)")
	}
	typeFullName, ok := typeFullNameOfOpen(args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.GetMethod receiver is not a Type")
	}
	if args[1].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.GetMethod expects a string name")
	}
	methodName := args[1].Str
	var paramNames []string
	for _, a := range args[2:] {
		if a.Kind != runtime.KindArray {
			continue
		}
		if names, err := bcl.TypeArrayToFullNames(a); err == nil {
			paramNames = names
			break
		}
	}
	if m.ResolveMember == nil {
		return runtime.Null(), nil
	}
	if _, ok := m.ResolveMember(typeFullName, methodName, paramNames); !ok {
		return runtime.Null(), nil
	}
	return bcl.NewMethodInfoValue(typeFullName, methodName), nil
}

// typeGetField backs Type.GetField(string name) — existence checked via
// the same Type.FieldIndex/StaticFieldIndex lookup ldfld/ldsfld already
// use, not a separate metadata scan. The field's own declared TYPE (Fase
// 3.53, FieldInfo.FieldType) is a separate, best-effort lookup via
// fieldTypeFullNameOf below — Machine.ResolveFields being nil/not finding
// it just means "" (a null FieldType later), never a hard failure, since
// existence itself already came from t.FieldIndex/StaticFieldIndex above.
func typeGetField(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.GetField expects (this, name)")
	}
	typeFullName, ok := typeFullNameOfOpen(args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.GetField receiver is not a Type")
	}
	if args[1].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.GetField expects a string name")
	}
	fieldName := args[1].Str
	if m.ResolveType == nil {
		return runtime.Null(), nil
	}
	t, err := m.ResolveType(typeFullName)
	if err != nil {
		return runtime.Null(), nil
	}
	if t.FieldIndex(fieldName) < 0 && t.StaticFieldIndex(fieldName) < 0 {
		return runtime.Null(), nil
	}
	return bcl.NewFieldInfoValue(typeFullName, fieldName, fieldTypeFullNameOf(m, typeFullName, fieldName)), nil
}

// fieldTypeFullNameOf looks up fieldName's own declared type off
// Machine.ResolveFields' parallel names/fieldTypes slices (assembly.go's
// resolveFields) — shared by typeGetField (singular) and typeGetFields
// (plural) below so both agree on exactly the same source. "" for an
// unset resolver or an unresolvable type/field name, which the caller
// then stores as FieldInfo.FieldType's own "no answer" sentinel (see
// nativeFieldInfo's own doc comment) rather than treating as an error.
func fieldTypeFullNameOf(m *Machine, typeFullName, fieldName string) string {
	if m.ResolveFields == nil {
		return ""
	}
	names, fieldTypes, _, ok := m.ResolveFields(typeFullName)
	if !ok {
		return ""
	}
	for i, n := range names {
		if n == fieldName {
			return fieldTypes[i]
		}
	}
	return ""
}

// typeGetFields backs Type.GetFields() (Fase 3.53, plural, no-args
// overload) — every field typeFullName's own TypeDef declares
// (Machine.ResolveFields, backed by the real Field table, not a name
// guess), each wrapped as its own FieldInfo carrying its real declared
// type. Empty (not an error) for a BCL type vmnet has no TypeDef for at
// all, matching GetProperties/GetInterfaces' own "no metadata, no
// results" convention.
func typeGetFields(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.GetFields expects a receiver")
	}
	typeFullName, ok := typeFullNameOfOpen(args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.GetFields receiver is not a Type")
	}
	if m.ResolveFields == nil {
		return runtime.ArrRef(runtime.NewArray(0)), nil
	}
	names, fieldTypes, _, ok := m.ResolveFields(typeFullName)
	if !ok {
		return runtime.ArrRef(runtime.NewArray(0)), nil
	}
	elems := make([]runtime.Value, len(names))
	for i, name := range names {
		elems[i] = bcl.NewFieldInfoValue(typeFullName, name, fieldTypes[i])
	}
	return runtime.ArrRef(&runtime.Array{Elems: elems}), nil
}

// typeGetMethods backs Type.GetMethods() (Fase 3.53, plural, no-args
// overload) — every method typeFullName's own TypeDef declares
// (Machine.ResolveMethods), each wrapped as its own MethodInfo. Same "one
// MethodInfo per declared name, no per-overload signature tracking"
// simplification typeGetMethod's own doc comment already documents
// accepting for a single-name lookup — a real overload set collapses to
// duplicate, functionally interchangeable MethodInfo entries here
// (Invoke always dispatches through the same first-match m.call), rather
// than real reflection's one-distinct-MethodInfo-per-overload. Empty
// (not an error) for a BCL type vmnet has no TypeDef for at all, matching
// GetFields/GetProperties/GetInterfaces' own "no metadata, no results"
// convention.
func typeGetMethods(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.GetMethods expects a receiver")
	}
	typeFullName, ok := typeFullNameOfOpen(args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.GetMethods receiver is not a Type")
	}
	if m.ResolveMethods == nil {
		return runtime.ArrRef(runtime.NewArray(0)), nil
	}
	names, ok := m.ResolveMethods(typeFullName)
	if !ok {
		return runtime.ArrRef(runtime.NewArray(0)), nil
	}
	elems := make([]runtime.Value, len(names))
	for i, name := range names {
		elems[i] = bcl.NewMethodInfoValue(typeFullName, name)
	}
	return runtime.ArrRef(&runtime.Array{Elems: elems}), nil
}

// constructorInfoInvoke backs ConstructorInfo.Invoke(object[] parameters)
// — constructs a real instance via Machine.New, the exact same path a
// real `newobj` already goes through (including its own overload
// resolution by the real argument Kinds now available, not just the
// declared Type[] signature GetConstructor matched against).
func constructorInfoInvoke(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: ConstructorInfo.Invoke expects (this, object[])")
	}
	typeFullName, ok := bcl.ConstructorInfoTypeFullName(args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: ConstructorInfo.Invoke receiver is not a ConstructorInfo")
	}
	ctorArgs, err := bcl.ObjectArrayToValues(args[1])
	if err != nil {
		return runtime.Value{}, err
	}
	return m.New(typeFullName, ctorArgs)
}

// methodInfoInvoke backs MethodInfo.Invoke(object obj, object[]
// parameters) — obj is null for a static method (real semantics: ignored
// either way, since a static target never needs a receiver); otherwise
// dispatches through Machine.call with virtual=true, exactly like a real
// callvirt would (the receiver's actual concrete type is tried first).
func methodInfoInvoke(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 3 {
		return runtime.Value{}, fmt.Errorf("interpreter: MethodInfo.Invoke expects (this, obj, object[])")
	}
	typeFullName, methodName, ok := bcl.MethodInfoParts(args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: MethodInfo.Invoke receiver is not a MethodInfo")
	}
	methodArgs, err := bcl.ObjectArrayToValues(args[2])
	if err != nil {
		return runtime.Value{}, err
	}
	fullName := typeFullName + "::" + methodName
	callArgs := methodArgs
	if args[1].Kind != runtime.KindNull {
		callArgs = append([]runtime.Value{args[1]}, methodArgs...)
	}
	// genericArgs is non-nil only when this MethodInfo came from
	// MakeGenericMethod (bcl.methodInfoMakeGenericMethod) — passed
	// through as Machine.call's own methodGenericArgs, the same argument
	// an ordinary compiled `callvirt SomeMethod<T>()` site's own
	// ir.Call.MethodGenericArgs already carries, so a generic method
	// invoked via reflection resolves identically to one called directly.
	genericArgs := bcl.MethodInfoGenericArgs(args[0])
	v, _, err := m.call(fullName, callArgs, true, depth, instrCount, nil, genericArgs)
	return v, err
}

// fieldInfoGetValue backs FieldInfo.GetValue(object obj) — tries the
// field as static first (Machine.staticType, the same lazy-.cctor-
// triggering lookup ldsfld itself uses), falling back to obj's own
// instance field slot via the receiver's real Type.FieldIndex.
func fieldInfoGetValue(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: FieldInfo.GetValue expects (this, obj)")
	}
	typeFullName, fieldName, ok := bcl.FieldInfoParts(args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: FieldInfo.GetValue receiver is not a FieldInfo")
	}
	if t, err := m.staticType(typeFullName, depth, instrCount); err == nil {
		if idx := t.StaticFieldIndex(fieldName); idx >= 0 {
			return t.StaticField(idx), nil
		}
	}
	obj := args[1]
	if obj.Kind == runtime.KindRef && obj.Ref != nil {
		obj = *obj.Ref
	}
	if obj.Kind != runtime.KindObject || obj.Obj == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: FieldInfo.GetValue: %s.%s is an instance field but obj is null", typeFullName, fieldName)
	}
	if obj.Obj.Type == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: FieldInfo.GetValue: receiver has no field layout")
	}
	idx := obj.Obj.Type.FieldIndex(fieldName)
	if idx < 0 {
		return runtime.Value{}, fmt.Errorf("interpreter: FieldInfo.GetValue: %s has no field %q", typeFullName, fieldName)
	}
	return obj.Obj.Fields[idx], nil
}

// typeGetProperties backs Type.GetProperties() — every declared property
// on typeFullName's own TypeDef (Machine.ResolveProperties, backed by the
// real Property/PropertyMap/MethodSemantics tables, not a name guess).
// Empty (not an error) for a BCL type vmnet has no TypeDef for at all,
// matching GetInterfaces' own "no metadata, no results" convention.
func typeGetProperties(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.GetProperties expects a receiver")
	}
	typeFullName, ok := typeFullNameOfOpen(args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.GetProperties receiver is not a Type")
	}
	var names []string
	var canRead, canWrite []bool
	var propTypes []string
	if m.ResolveProperties != nil {
		names, canRead, canWrite, propTypes, _ = m.ResolveProperties(typeFullName)
	}
	if len(names) == 0 {
		return runtime.ArrRef(&runtime.Array{Elems: wellKnownBclPropertyInfos(typeFullName)}), nil
	}
	elems := make([]runtime.Value, len(names))
	for i, name := range names {
		elems[i] = bcl.NewPropertyInfoValue(typeFullName, name, canRead[i], canWrite[i], propTypes[i])
	}
	return runtime.ArrRef(&runtime.Array{Elems: elems}), nil
}

// wellKnownBclProperties is a narrow, hand-maintained fallback for the
// handful of real BCL/framework properties found via a real, load-
// bearing case: Dapper's own SqlMapper static constructor (which runs
// unconditionally the instant ANY SqlMapper method is first touched —
// C#'s own guarantee for a type's .cctor, not something a caller can
// route around) reflects over CultureInfo.InvariantCulture and
// DbDataReader's real `this[int]` indexer purely to cache their
// MethodInfo for later use. vmnet has no BCL metadata database at all —
// same limitation bclKnownInterfaces/typeCodes above already document —
// so Type.GetProperty(ies) can never find these through the normal
// TypeDef path the way it does for a plugin's own declared properties,
// and unlike most such gaps (which just mean a missing feature), THIS
// one is fatal: `.GetGetMethod()` called on the real .NET behavior's
// non-null PropertyInfo, but vmnet's Null(), throws a
// NullReferenceException the instant Dapper.SqlMapper is loaded at all
// — before a single real query ever runs. Not a general BCL reflection
// database (deliberately narrow, same posture as every other "hardcoded
// knowledge for a specific well-known case" fallback in this codebase) —
// just enough for this one specific, common failure mode.
var wellKnownBclProperties = map[string][]struct {
	name              string
	canRead, canWrite bool
	propertyType      string
	indexParamType    string // "" for an ordinary (non-indexer) property
}{
	"System.Globalization.CultureInfo": {
		{name: "InvariantCulture", canRead: true, propertyType: "System.Globalization.CultureInfo"},
	},
	"System.Data.Common.DbDataReader": {
		{name: "Item", canRead: true, propertyType: "System.Object", indexParamType: "System.Int32"},
	},
}

func wellKnownBclPropertyInfos(typeFullName string) []runtime.Value {
	entries := wellKnownBclProperties[bcl.GenericOpenName(typeFullName)]
	elems := make([]runtime.Value, len(entries))
	for i, e := range entries {
		if e.indexParamType != "" {
			elems[i] = bcl.NewIndexerPropertyInfoValue(typeFullName, e.name, e.canRead, e.canWrite, e.propertyType, []string{e.indexParamType})
		} else {
			elems[i] = bcl.NewPropertyInfoValue(typeFullName, e.name, e.canRead, e.canWrite, e.propertyType)
		}
	}
	return elems
}

// typeGetProperty backs Type.GetProperty(string name) plus the
// BindingFlags-taking overloads sharing the same (name, ...) shape
// (Fase 3.52, found via Dapper's own SqlMapper static ctor:
// typeof(CultureInfo).GetProperty("InvariantCulture", BindingFlags.
// Static | BindingFlags.Public)) — any trailing arguments (BindingFlags,
// a Binder, a Type[] for an indexer signature, ...) are accepted and
// ignored, matched by name only, the same posture typeGetMethod's own
// no-Type[]-found fallback already takes for the equivalent GetMethod
// overloads. Null (not an error) for an unresolvable type OR a real
// property name it doesn't declare, matching real Type.GetProperty's own
// "no match" contract.
func typeGetProperty(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.GetProperty expects (this, name, ...)")
	}
	typeFullName, ok := typeFullNameOfOpen(args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.GetProperty receiver is not a Type")
	}
	if args[1].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.GetProperty expects a string name")
	}
	var names []string
	var canRead, canWrite []bool
	var propTypes []string
	if m.ResolveProperties != nil {
		names, canRead, canWrite, propTypes, _ = m.ResolveProperties(typeFullName)
	}
	for i, name := range names {
		if name == args[1].Str {
			return bcl.NewPropertyInfoValue(typeFullName, name, canRead[i], canWrite[i], propTypes[i]), nil
		}
	}
	for _, v := range wellKnownBclPropertyInfos(typeFullName) {
		if tfn, pn, _, _, ok := bcl.PropertyInfoParts(v); ok && tfn == typeFullName && pn == args[1].Str {
			return v, nil
		}
	}
	return runtime.Null(), nil
}

// propertyInfoGetValue backs PropertyInfo.GetValue(object obj) — obj is
// the instance to read from (never null here: unlike FieldInfo, no
// static-property fast path is needed yet — every real caller found so
// far reads an instance property). CanRead false (a set-only property,
// vanishingly rare but real) is a genuine ArgumentException in real
// .NET, not a silent null.
func propertyInfoGetValue(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: PropertyInfo.GetValue expects (this, obj)")
	}
	typeFullName, propertyName, canRead, _, ok := bcl.PropertyInfoParts(args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: PropertyInfo.GetValue receiver is not a PropertyInfo")
	}
	if !canRead {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentException", Message: "Property get method was not found."}
	}
	v, _, err := m.call(typeFullName+"::get_"+propertyName, []runtime.Value{args[1]}, true, depth, instrCount, nil, nil)
	return v, err
}

// propertyInfoSetValue backs PropertyInfo.SetValue(object obj, object
// value) — same CanWrite-checked shape as propertyInfoGetValue's own
// CanRead check.
func propertyInfoSetValue(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 3 {
		return runtime.Value{}, fmt.Errorf("interpreter: PropertyInfo.SetValue expects (this, obj, value)")
	}
	typeFullName, propertyName, _, canWrite, ok := bcl.PropertyInfoParts(args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: PropertyInfo.SetValue receiver is not a PropertyInfo")
	}
	if !canWrite {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentException", Message: "Property set method was not found."}
	}
	_, _, err := m.call(typeFullName+"::set_"+propertyName, []runtime.Value{args[1], args[2]}, true, depth, instrCount, nil, nil)
	return runtime.Value{}, err
}

// memberInfoOpEquality/memberInfoOpInequality back ConstructorInfo/
// MethodInfo's operator==/!= (real reflection member-info types
// overload these) — reference equality (Go pointer identity on the
// underlying wrapper), matching the one real pattern found needing this
// (`ctor != null` after GetConstructor): every fresh Get* call allocates
// its own wrapper, so two independently-obtained infos for even the
// exact same real member are never equal by reference — real CLR
// reflection actually caches and interns these, but no real call site in
// this loop compares two non-null MemberInfo values against each other,
// only against null.
func memberInfoOpEquality(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: MemberInfo.op_Equality expects 2 arguments")
	}
	return runtime.Bool(refEqual(args[0], args[1])), nil
}

func memberInfoOpInequality(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: MemberInfo.op_Inequality expects 2 arguments")
	}
	return runtime.Bool(!refEqual(args[0], args[1])), nil
}
