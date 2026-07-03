package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

func init() {
	machineRegistry["System.Type::IsAssignableFrom"] = typeIsAssignableFrom
	machineRegistry["System.Type::get_IsValueType"] = typeGetIsValueType
	machineRegistry["System.Type::get_IsEnum"] = typeGetIsEnum
	machineRegistry["System.Type::get_IsInterface"] = typeGetIsInterface
	machineRegistry["System.Type::get_BaseType"] = typeGetBaseType
	machineRegistry["System.Type::GetInterfaces"] = typeGetInterfaces
	machineRegistry["System.Type::GetType"] = typeStaticGetType
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
func classifyTypeByName(m *Machine, fullName string) (isValueType, isEnum, isInterface bool) {
	open := bcl.GenericOpenName(fullName)
	if bclPrimitiveValueTypes[open] {
		return true, false, false
	}
	if bclKnownInterfaces[open] {
		return false, false, true
	}
	if _, ok := bcl.LookupValueType(open); ok {
		return true, false, false
	}
	if m.ResolveType == nil {
		return false, false, false
	}
	t, err := m.ResolveType(open)
	if err != nil {
		return false, false, false
	}
	return t.IsValueType, t.IsEnum, t.IsInterface
}

func typeGetIsValueType(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	fullName, ok := bcl.TypeFullNameOf(argsSelf(args))
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.IsValueType receiver is not a Type")
	}
	isValueType, _, _ := classifyTypeByName(m, fullName)
	return runtime.Bool(isValueType), nil
}

func typeGetIsEnum(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	fullName, ok := bcl.TypeFullNameOf(argsSelf(args))
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.IsEnum receiver is not a Type")
	}
	_, isEnum, _ := classifyTypeByName(m, fullName)
	return runtime.Bool(isEnum), nil
}

func typeGetIsInterface(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	fullName, ok := bcl.TypeFullNameOf(argsSelf(args))
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.IsInterface receiver is not a Type")
	}
	_, _, isInterface := classifyTypeByName(m, fullName)
	return runtime.Bool(isInterface), nil
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
	isValueType, isEnum, isInterface := classifyTypeByName(m, fullName)
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
