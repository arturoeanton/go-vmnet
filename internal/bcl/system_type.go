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
	register("System.Type::MakeGenericType", true, typeMakeGenericType)
	register("System.Nullable::GetUnderlyingType", true, nullableGetUnderlyingType)

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
}

func typeInfoIdentity(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("bcl: IntrospectionExtensions.GetTypeInfo expects 1 argument")
	}
	return args[0], nil
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
