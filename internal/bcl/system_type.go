package bcl

import (
	"fmt"
	"strings"
	"sync"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// nativeTypeInfo backs a System.Type instance: just the full name it
// represents.
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
	// covers the call site shape directly. Registered ONLY here (Fase
	// 3.41 bug: system_linq_expressions.go used to register this same
	// "System.Reflection.MemberInfo::get_Name" key a second time for its
	// own *nativeMemberInfo receiver shape — since register() always
	// overwrites, whichever init() ran last silently won, and Go's
	// alphabetical-by-filename init order made system_type.go's entry
	// win, breaking every real MemberExpression.Member.Name lookup
	// ConfigureMetadata/AddAttribute<T> depends on with "System.Type
	// method receiver is not a Type". typeGetName below now handles both
	// receiver shapes directly instead of two competing registrations.
	register("System.Reflection.MemberInfo::get_Name", true, typeGetName)

	// Generics reflection (Fase 3.25) — all pure string manipulation over
	// FullName's "Open`N[[Arg1],[Arg2]]" closed-generic encoding (see
	// internal/ir/builder.go's sigTypeFullName, which is what produces
	// that encoding for ldtoken/typeof(T) since this fase). None of these
	// need Machine access: get_IsValueType/IsEnum/IsInterface/get_BaseType/
	// GetInterfaces do (internal/interpreter/reflection.go) since a
	// plugin-defined generic type needs its real TypeDef flags, not just
	// name parsing.
	register("System.Type::get_IsGenericType", true, typeGetIsGenericType)
	register("System.Type::GetGenericTypeDefinition", true, typeGetGenericTypeDefinition)
	register("System.Type::GetGenericArguments", true, typeGetGenericArguments)
	// Type.IsArray (Fase 3.52, found via Dapper's own SqlMapper column-
	// type coercion) — pure string manipulation over FullName's own
	// "Elem[]" array encoding (ir/builder.go's SigTypeFullName), same
	// posture as the generics accessors just above: no Machine access
	// needed at all, unlike get_IsValueType/IsEnum/IsInterface (which
	// need a plugin type's real TypeDef flags).
	register("System.Type::get_IsArray", true, typeGetIsArray)
	register("System.Type::GetElementType", true, typeGetElementType)
	register("System.Type::MakeGenericType", true, typeMakeGenericType)
	register("System.Nullable::GetUnderlyingType", true, nullableGetUnderlyingType)
	// IsGenericTypeDefinition/GenericTypeArguments/ContainsGenericParameters/
	// IsGenericParameter (Fase 3.53, found via a corpus-wide compatibility
	// pass: 5-7 of 19 real NuGet packages each) — same pure string-shape
	// posture as IsGenericType/GetGenericArguments above, no Machine access
	// needed: every answer comes straight out of FullName's own
	// "Open`N[[Arg1],[Arg2]]" encoding, never a plugin TypeDef's real flags.
	register("System.Type::get_IsGenericTypeDefinition", true, typeGetIsGenericTypeDefinition)
	register("System.Type::get_GenericTypeArguments", true, typeGetGenericTypeArguments)
	register("System.Type::get_ContainsGenericParameters", true, typeGetContainsGenericParameters)
	register("System.Type::get_IsGenericParameter", true, typeGetIsGenericParameter)

	registerCtor("System.Reflection.Assembly", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeAssembly{}}, nil
	})
	register("System.Type::get_Assembly", true, typeGetAssembly)
	register("System.Reflection.Assembly::ToString", true, assemblyToString)
	register("System.Reflection.Assembly::get_FullName", true, assemblyToString)
	register("System.Reflection.Assembly::GetExecutingAssembly", true, assemblyGetExecuting)
	register("System.Reflection.Assembly::GetCallingAssembly", true, assemblyGetExecuting)
	register("System.Reflection.Assembly::GetEntryAssembly", true, assemblyGetExecuting)
	// IntrospectionExtensions.GetTypeInfo(this Type) is a netstandard1.x
	// compatibility shim: TypeInfo IS Type on every modern runtime (a
	// TypeInfo-returning API exists only for source compat with old
	// portable-class-library code) — an identity function, since vmnet's
	// own System.Type reflection-lite model has no separate TypeInfo
	// shape at all. Found via a real, load-bearing case: System.Span's
	// own internal SpanHelpers+PerTypeValues`1..cctor, reached just from
	// ClosedXML's own real ReadOnlySpan<byte> use of embedded font data.
	register("System.Reflection.IntrospectionExtensions::GetTypeInfo", true, typeInfoIdentity)
	register("System.Type::GetTypeCode", true, typeGetTypeCode)
}

func typeInfoIdentity(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("bcl: IntrospectionExtensions.GetTypeInfo expects 1 argument")
	}
	return args[0], nil
}

// typeCodes maps a primitive/well-known BCL type's FullName to its real
// Type.GetTypeCode(Type) answer (the System.TypeCode enum's own fixed
// ordinals, stable since .NET 1.0) — found via Dapper's own SqlMapper
// column-type coercion, which switches on GetTypeCode rather than
// comparing Type instances directly for exactly this common primitive
// set. Any type not listed here (every plugin class, and every BCL type
// this package doesn't special-case) answers TypeCode.Object (1),
// matching real GetTypeCode's own default for a reference type with no
// IConvertible-backed mapping.
var typeCodes = map[string]int32{
	"System.Boolean":  3,
	"System.Char":     4,
	"System.SByte":    5,
	"System.Byte":     6,
	"System.Int16":    7,
	"System.UInt16":   8,
	"System.Int32":    9,
	"System.UInt32":   10,
	"System.Int64":    11,
	"System.UInt64":   12,
	"System.Single":   13,
	"System.Double":   14,
	"System.Decimal":  15,
	"System.DateTime": 16,
	"System.String":   18,
}

// typeGetTypeCode backs the static Type.GetTypeCode(Type type) —
// TypeCode.Empty (0) for a null Type argument, matching real semantics.
func typeGetTypeCode(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("bcl: Type.GetTypeCode expects 1 argument")
	}
	if args[0].Kind == runtime.KindNull {
		return runtime.Int32(0), nil
	}
	fullName, ok := TypeFullNameOf(args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: Type.GetTypeCode expects a Type argument")
	}
	if code, ok := typeCodes[GenericOpenName(fullName)]; ok {
		return runtime.Int32(code), nil
	}
	return runtime.Int32(1), nil
}

// nativeAssembly is a stub System.Reflection.Assembly — vmnet has no real
// multi-assembly model (a plugin is always one flat set of resolvable
// types plus the BCL surface this package implements), so every Assembly
// value is interchangeable; only .ToString()/.FullName are given a
// plausible constant (Fase 3.25), matching the CultureInfo stub precedent
// (Fase 3.23).
type nativeAssembly struct{}

func typeGetAssembly(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Type.Assembly expects a receiver")
	}
	if _, err := asTypeInfo(args[0]); err != nil {
		return runtime.Value{}, err
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeAssembly{}}), nil
}

func assemblyToString(args []runtime.Value) (runtime.Value, error) {
	return runtime.String("vmnet, Version=0.0.0.0"), nil
}

// assemblyGetExecuting backs the static Assembly.GetExecutingAssembly/
// GetCallingAssembly/GetEntryAssembly — all the same nativeAssembly stub
// (see its own doc comment: vmnet has no real multi-assembly identity
// model, every Assembly value is interchangeable).
func assemblyGetExecuting(args []runtime.Value) (runtime.Value, error) {
	return runtime.ObjRef(&runtime.Object{Native: &nativeAssembly{}}), nil
}

// genericOpenName strips a closed generic instantiation's "[[Arg1],
// [Arg2]]" suffix, if present, leaving the open generic type's own name
// (e.g. "System.Collections.Generic.List`1[[System.Int32]]" ->
// "System.Collections.Generic.List`1"). Exported for
// internal/interpreter/reflection.go, which needs it to classify a
// closed generic instantiation's IsValueType/IsEnum/IsInterface/BaseType
// against the SAME open name a plugin's TypeDef or a hardcoded BCL entry
// is registered under.
func GenericOpenName(fullName string) string {
	if idx := strings.Index(fullName, "[["); idx >= 0 {
		return fullName[:idx]
	}
	return fullName
}

func isGenericTypeName(fullName string) bool {
	return strings.Contains(GenericOpenName(fullName), "`")
}

// splitGenericArgs parses a closed generic instantiation's bracketed
// argument list — e.g. "[[System.String],[System.Int32]]" ->
// ["System.String", "System.Int32"] — tracking bracket depth so a nested
// generic argument (itself closed, e.g. "[[System.Collections.Generic.
// List`1[[System.String]]]]") splits correctly instead of breaking on its
// own internal commas/brackets.
func splitGenericArgs(bracketed string) []string {
	if len(bracketed) < 2 || bracketed[0] != '[' || bracketed[len(bracketed)-1] != ']' {
		return nil
	}
	inner := bracketed[1 : len(bracketed)-1]
	var args []string
	depth := 0
	start := -1
	for i := 0; i < len(inner); i++ {
		switch inner[i] {
		case '[':
			if depth == 0 {
				start = i + 1
			}
			depth++
		case ']':
			depth--
			if depth == 0 {
				args = append(args, inner[start:i])
			}
		}
	}
	return args
}

func typeGetIsGenericType(args []runtime.Value) (runtime.Value, error) {
	ti, err := asTypeInfo(args[0])
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(isGenericTypeName(ti.FullName)), nil
}

// typeGetIsArray/typeGetElementType back Type.IsArray/GetElementType —
// an array type's FullName is always "Elem[]" (SigTypeFullName's own
// array encoding), so both are plain suffix manipulation, no Machine
// access needed.
func typeGetIsArray(args []runtime.Value) (runtime.Value, error) {
	ti, err := asTypeInfo(args[0])
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(strings.HasSuffix(ti.FullName, "[]")), nil
}

func typeGetElementType(args []runtime.Value) (runtime.Value, error) {
	ti, err := asTypeInfo(args[0])
	if err != nil {
		return runtime.Value{}, err
	}
	if !strings.HasSuffix(ti.FullName, "[]") {
		return runtime.Null(), nil
	}
	return NewTypeValue(strings.TrimSuffix(ti.FullName, "[]")), nil
}

func typeGetGenericTypeDefinition(args []runtime.Value) (runtime.Value, error) {
	ti, err := asTypeInfo(args[0])
	if err != nil {
		return runtime.Value{}, err
	}
	open := GenericOpenName(ti.FullName)
	if !strings.Contains(open, "`") {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.InvalidOperationException", Message: fmt.Sprintf("Type '%s' is not a generic type.", ti.FullName)}
	}
	return NewTypeValue(open), nil
}

// typeGetGenericArguments returns [] for an unbound open generic type
// (typeof(List<>)) — real .NET returns the generic parameter placeholders
// (T) there, which vmnet has no way to name (Fase 3.25, documented
// limitation): every concrete closed instantiation this project actually
// constructs (typeof(List<int>), MakeGenericType's own result) carries
// real argument names and works fully.
func typeGetGenericArguments(args []runtime.Value) (runtime.Value, error) {
	ti, err := asTypeInfo(args[0])
	if err != nil {
		return runtime.Value{}, err
	}
	idx := strings.Index(ti.FullName, "[[")
	if idx < 0 {
		return runtime.ArrRef(runtime.NewArray(0)), nil
	}
	argNames := splitGenericArgs(ti.FullName[idx:])
	elems := make([]runtime.Value, len(argNames))
	for i, n := range argNames {
		elems[i] = NewTypeValue(n)
	}
	return runtime.ArrRef(&runtime.Array{Elems: elems}), nil
}

// typeGetIsGenericTypeDefinition backs Type.IsGenericTypeDefinition — true
// only for the UNBOUND generic type itself (typeof(List<>), or
// GetGenericTypeDefinition()'s own result): FullName carries a backtick
// arity marker ("`1") but no closed "[[...]]" argument list at all. A
// closed instantiation (typeof(List<int>), FullName "...`1[[System.
// Int32]]") answers false here even though IsGenericType is true for it
// too — real reflection's own documented distinction between "is this
// type generic at all" (IsGenericType) and "is this SPECIFICALLY the
// open definition" (IsGenericTypeDefinition).
func typeGetIsGenericTypeDefinition(args []runtime.Value) (runtime.Value, error) {
	ti, err := asTypeInfo(args[0])
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(strings.Contains(ti.FullName, "`") && !strings.Contains(ti.FullName, "[[")), nil
}

// typeGetGenericTypeArguments backs the GenericTypeArguments property —
// the same closed-generic-argument extraction GetGenericArguments()
// performs (both read the same "[[Arg1],[Arg2]]" encoded suffix), but
// real GenericTypeArguments only ever answers for a closed constructed
// generic type: an open, unbound generic type or a non-generic type both
// answer empty, matching real semantics. Unlike GetGenericArguments()
// (which also returns the open definition's own unbound parameter
// placeholders "T" — something vmnet has no way to name at all, see that
// function's own doc comment), GenericTypeArguments never did return
// those in the first place, so there's no divergent case to handle: this
// safely reuses the exact same code path.
func typeGetGenericTypeArguments(args []runtime.Value) (runtime.Value, error) {
	return typeGetGenericArguments(args)
}

// typeGetContainsGenericParameters backs Type.ContainsGenericParameters —
// true for any type that still has an unbound generic parameter
// somewhere in its shape. vmnet's reflection-lite Type model only ever
// represents two generic shapes: fully open (typeof(List<>), no
// "[[...]]" suffix at all) and fully closed (every argument concrete,
// typeof(List<int>)) — there's no partially-open shape to detect here
// (e.g. an open type nested inside an otherwise-closed generic method's
// own type parameter), so this collapses to exactly the same test as
// IsGenericTypeDefinition: the type IS the open definition itself.
func typeGetContainsGenericParameters(args []runtime.Value) (runtime.Value, error) {
	return typeGetIsGenericTypeDefinition(args)
}

// typeGetIsGenericParameter backs Type.IsGenericParameter — true only for
// a Type instance that IS itself a generic parameter placeholder (the
// `T` in `class Foo<T>`, as obtained from real Type.GetGenericArguments()
// called on the open generic type definition). vmnet never actually
// produces such a value: ldtoken/typeof(T) inside a generic body is
// always resolved to its real closed type argument by the time a Type
// value exists at all (Fase 3.25's documented limitation — there's no
// unbound-parameter Type shape modeled here), so this always answers
// false for every Type value vmnet can construct.
func typeGetIsGenericParameter(args []runtime.Value) (runtime.Value, error) {
	if _, err := asTypeInfo(args[0]); err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(false), nil
}

// typeMakeGenericType receives its type arguments as a real System.Type[]
// (the C# compiler always lowers `params Type[]` to an actual array at
// the call site) — unlike typeof(T), which loses its argument names for
// an OPEN generic (typeof(List<>) has no way to carry "T"), this always
// has real names to build a proper closed instantiation from.
func typeMakeGenericType(args []runtime.Value) (runtime.Value, error) {
	ti, err := asTypeInfo(args[0])
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 || args[1].Kind != runtime.KindArray || args[1].Arr == nil {
		return runtime.Value{}, fmt.Errorf("bcl: System.Type.MakeGenericType expects a Type[] argument")
	}
	open := GenericOpenName(ti.FullName)
	argNames := make([]string, len(args[1].Arr.Elems))
	for i, v := range args[1].Arr.Elems {
		argTi, err := asTypeInfo(v)
		if err != nil {
			return runtime.Value{}, fmt.Errorf("bcl: System.Type.MakeGenericType: argument %d is not a Type", i)
		}
		argNames[i] = argTi.FullName
	}
	return NewTypeValue(open + "[[" + strings.Join(argNames, "],[") + "]]"), nil
}

// nullableGetUnderlyingType backs the static System.Nullable.
// GetUnderlyingType(Type) helper (note: System.Nullable, not
// System.Nullable`1 — this one real method lives on the non-generic
// helper class real .NET uses for exactly this kind of type-erased
// utility). Returns real null (not a Type) for any non-Nullable`1 input,
// matching actual semantics.
func nullableGetUnderlyingType(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("bcl: Nullable.GetUnderlyingType expects 1 argument")
	}
	if args[0].Kind == runtime.KindNull {
		return runtime.Value{}, fmt.Errorf("bcl: Nullable.GetUnderlyingType: type argument is null")
	}
	ti, err := asTypeInfo(args[0])
	if err != nil {
		return runtime.Value{}, err
	}
	if GenericOpenName(ti.FullName) != "System.Nullable`1" {
		return runtime.Null(), nil
	}
	idx := strings.Index(ti.FullName, "[[")
	if idx < 0 {
		return runtime.Null(), nil
	}
	argNames := splitGenericArgs(ti.FullName[idx:])
	if len(argNames) != 1 {
		return runtime.Null(), nil
	}
	return NewTypeValue(argNames[0]), nil
}

// typeValueCache interns System.Type values by full name (Fase 3.40):
// real .NET Type objects are canonical per-AppDomain (`typeof(X) ==
// typeof(X)` is always true, by reference), but constructing a fresh
// *runtime.Object on every NewTypeValue call broke that — found via a
// real, load-bearing case, DocumentFormat.OpenXml.Packaging's own
// FeatureCollectionBase (a real Dictionary<Type,object>-backed "feature
// bag"): every lookup's key is a FRESH Type value from its own
// `typeof(TFeature)`, encoded by identity (encodeDictKey's KindObject
// case uses the Go pointer itself, %p, since most native objects
// genuinely don't have value semantics) — two separate typeof(X) calls
// for the very same X produced two different pointers, so every single
// feature lookup silently missed regardless of whether the feature had
// actually been registered.
var (
	typeValueCacheMu sync.Mutex
	typeValueCache   = map[string]*runtime.Object{}
)

// NewTypeValue builds a System.Type value for fullName — the runtime
// counterpart of ir.LoadTypeToken (typeof(T)), called directly from
// internal/interpreter/eval.go rather than through the normal
// bcl.Lookup/native-call path, since ldtoken isn't a call at all. Always
// returns the same *runtime.Object for the same fullName (see
// typeValueCache's own doc comment).
func NewTypeValue(fullName string) runtime.Value {
	typeValueCacheMu.Lock()
	defer typeValueCacheMu.Unlock()
	obj, ok := typeValueCache[fullName]
	if !ok {
		obj = &runtime.Object{Native: &nativeTypeInfo{FullName: fullName}}
		typeValueCache[fullName] = obj
	}
	return runtime.ObjRef(obj)
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

// typeFullName mirrors internal/interpreter/typecheck.go's own
// fullTypeName exactly (kept as a separate copy to avoid a bcl<->
// interpreter import cycle): QualifiedName must be checked first for a
// nested type ("Outer+Inner") — found via a real, load-bearing case
// (Fase 3.40): Object.GetType() on a real nested private class
// (DocumentFormat.OpenXml.Packaging.SpreadsheetDocument's own nested
// SpreadsheetDocumentFeatures) fell back to the bare "SpreadsheetDocument
// Features" with neither its enclosing type nor namespace, since a
// nested TypeDef's own Namespace column is always empty by definition —
// every reflection/interface-matching check downstream (Type.
// IsAssignableFrom's own real BCL-metadata-backed base/interface walk,
// most critically) failed to resolve that bare, unqualified name at all.
func typeFullName(t *runtime.Type) string {
	if t.QualifiedName != "" {
		return t.QualifiedName
	}
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
	// This same native also backs "System.Reflection.MemberInfo::get_Name"
	// (see its registration comment above) — a MemberExpression.Member
	// (Fase 3.41, system_linq_expressions.go's Expression.Property
	// support) is backed by *nativeMemberInfo, not *nativeTypeInfo; check
	// that shape first rather than routing it through asTypeInfo, which
	// only ever understands a real Type receiver.
	if mi, ok := nativeOf[*nativeMemberInfo](args[0]); ok {
		return runtime.String(mi.name), nil
	}
	// FieldInfo/MethodInfo/ConstructorInfo/PropertyInfo.Name (Fase 3.53) —
	// same real-IL precedent as MemberInfo.DeclaringType (see that
	// registration's own comment, internal/bcl/system_reflection.go): none
	// of these four concrete reflection wrapper types redeclare Name
	// themselves either (it's only ever declared once, on MemberInfo
	// itself), so a real compiled call site's `xxxInfo.Name` always
	// callvirts THIS exact "MemberInfo::get_Name" token regardless of
	// which concrete wrapper it's actually holding — confirmed via
	// ilspycmd against a real compiled test DLL enumerating Type.
	// GetFields()'s own FieldInfo[] result. A ConstructorInfo's Name is
	// always ".ctor" here (vmnet's reflection model never distinguishes a
	// static .cctor as its own ConstructorInfo — see nativeConstructorInfo's
	// own doc comment, which is only ever about instance constructors).
	if _, ok := nativeOf[*nativeConstructorInfo](args[0]); ok {
		return runtime.String(".ctor"), nil
	}
	if methodInfo, ok := nativeOf[*nativeMethodInfo](args[0]); ok {
		return runtime.String(methodInfo.methodName), nil
	}
	if fi, ok := nativeOf[*nativeFieldInfo](args[0]); ok {
		return runtime.String(fi.fieldName), nil
	}
	if pi, ok := nativeOf[*nativePropertyInfo](args[0]); ok {
		return runtime.String(pi.propertyName), nil
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
