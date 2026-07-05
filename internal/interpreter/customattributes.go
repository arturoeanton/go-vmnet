package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// Real System.Reflection.CustomAttributeData/System.Attribute.
// GetCustomAttribute/System.Reflection.CustomAttributeExtensions support
// (Fase 3.63) — deliberately deferred until now: see docs/en/ROADMAP.md's
// own long-standing "genuinely new subsystem" note. Needs Machine access
// throughout (Machine.ResolveCustomAttributes to find what's actually
// applied, Machine.New/newObj to construct a real attribute instance for
// every shape but the raw CustomAttributeData one), so none of this can
// live in internal/bcl as a plain bcl.Native — see calls.go's own doc
// comment on the plain-native vs Machine-aware split.
//
// Real support is scoped to Type and PropertyInfo receivers so far —
// exactly matching assembly.go's own resolveCustomAttributes, which only
// resolves "type" and "property" member kinds today (a real, confirmed
// corpus need: Microsoft.Extensions.Configuration.Binder's own
// GetPropertyName reads a property's [ConfigurationKeyName], Markdig's
// own Markdown.Version reads an assembly-level attribute — the latter
// isn't covered either, since assembly-level custom attributes are a
// separate Parent shape (the Assembly/Module table, not a TypeDef) not
// yet wired into resolveCustomAttributes). Every OTHER receiver kind
// (ParameterInfo, MethodInfo, ConstructorInfo, MethodBase, FieldInfo, the
// generic MemberInfo base) degrades to "no attributes found" — the same
// honest, documented gap Fase 3.60's own stub already was, just now
// centralized in one place instead of a separate always-empty bcl.Native
// per receiver.
func init() {
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
		machineRegistry[recv+"::GetCustomAttributesData"] = customAttributesGetData
		machineRegistry[recv+"::GetCustomAttributes"] = customAttributesGetAll
		machineRegistry[recv+"::IsDefined"] = customAttributesIsDefined
	}
	machineRegistry["System.Attribute::GetCustomAttribute"] = attributeGetCustomAttribute
	// Attribute.IsDefined(MemberInfo, Type) — the static-method spelling
	// of the same real API MemberInfo.IsDefined already provides as an
	// instance method above; customAttributesIsDefined's own (receiver,
	// attributeType) argument shape is identical either way, so no new
	// function is needed. Found via CsvHelper's own real usage.
	machineRegistry["System.Attribute::IsDefined"] = customAttributesIsDefined
	// CustomAttributeExtensions.GetCustomAttribute<T>(this MemberInfo) —
	// the generic extension-method spelling of the same real API (found
	// via Markdig's own Markdown.Version reading its containing
	// assembly's AssemblyFileVersionAttribute — not itself covered yet,
	// see this file's own doc comment on assembly-level attributes, but
	// the mechanism is real for a Type/PropertyInfo receiver now).
	genericMachineRegistry["System.Reflection.CustomAttributeExtensions::GetCustomAttribute"] = customAttributeExtensionsGetCustomAttribute
	machineRegistry["System.Reflection.CustomAttributeExtensions::GetCustomAttributes"] = customAttributesGetAll
	machineRegistry["System.Reflection.CustomAttributeExtensions::IsDefined"] = customAttributesIsDefined
}

// memberIdentity resolves a receiver to the (typeFullName, memberKind,
// memberName) triple Machine.ResolveCustomAttributes needs — see this
// file's own doc comment for which receiver kinds are actually supported.
func memberIdentity(v runtime.Value) (typeFullName, memberKind, memberName string, ok bool) {
	if tfn, ok := bcl.TypeFullNameOf(v); ok {
		return tfn, "type", "", true
	}
	if tfn, pn, _, _, ok := bcl.PropertyInfoParts(v); ok {
		return tfn, "property", pn, true
	}
	return "", "", "", false
}

// resolveAttributesFor returns every real attribute applied to receiver,
// or nil for an unsupported receiver kind/no resolver/no TypeDef — every
// caller here treats nil identically to "genuinely zero attributes",
// matching real reflection's own answer for an unannotated member.
func resolveAttributesFor(m *Machine, receiver runtime.Value) []runtime.ResolvedAttribute {
	typeFullName, memberKind, memberName, ok := memberIdentity(receiver)
	if !ok || m.ResolveCustomAttributes == nil {
		return nil
	}
	attrs, ok := m.ResolveCustomAttributes(typeFullName, memberKind, memberName)
	if !ok {
		return nil
	}
	return attrs
}

func customAttributesGetData(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: GetCustomAttributesData expects a receiver")
	}
	attrs := resolveAttributesFor(m, args[0])
	elems := make([]runtime.Value, len(attrs))
	for i, a := range attrs {
		elems[i] = bcl.NewCustomAttributeDataValue(a.TypeFullName, a.CtorArgs)
	}
	return runtime.ArrRef(&runtime.Array{Elems: elems}), nil
}

// constructAttribute builds a real instance of a resolved attribute via
// the exact same newObj path an ordinary `new SomeAttribute(args)` call
// site already uses — attributes are real, constructible types like any
// other, once their constructor arguments are known (assembly.go's own
// decodeCustomAttribute already did the hard part: finding and decoding
// the real blob).
func constructAttribute(m *Machine, a runtime.ResolvedAttribute, depth int, instrCount *int64) (runtime.Value, error) {
	return m.newObj(newObjArgs{
		TypeFullName: a.TypeFullName,
		CtorFullName: a.TypeFullName + "::.ctor",
		Args:         a.CtorArgs,
	}, depth, instrCount)
}

// customAttributesGetAll backs the plural GetCustomAttributes() (no Type
// filter) — every real, constructible attribute applied to the receiver.
// An attribute this subsystem can't construct (an unsupported argument
// shape decodeCustomAttribute already skipped, or whose own constructor
// somehow fails to resolve) is skipped rather than failing every other,
// constructible one on the same member.
func customAttributesGetAll(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: GetCustomAttributes expects a receiver")
	}
	attrs := resolveAttributesFor(m, args[0])
	var elems []runtime.Value
	for _, a := range attrs {
		v, err := constructAttribute(m, a, depth, instrCount)
		if err != nil {
			continue
		}
		elems = append(elems, v)
	}
	return runtime.ArrRef(&runtime.Array{Elems: elems}), nil
}

func customAttributesIsDefined(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: IsDefined expects (receiver, attributeType)")
	}
	targetName, ok := bcl.TypeFullNameOf(args[1])
	if !ok {
		return runtime.Bool(false), nil
	}
	for _, a := range resolveAttributesFor(m, args[0]) {
		if a.TypeFullName == targetName {
			return runtime.Bool(true), nil
		}
	}
	return runtime.Bool(false), nil
}

// attributeGetCustomAttribute backs System.Attribute.GetCustomAttribute
// (MemberInfo/ParameterInfo element, Type attributeType[, bool inherit]) —
// the non-generic, real-Type-argument spelling of the same API
// CustomAttributeExtensions.GetCustomAttribute<T> below provides
// generically.
func attributeGetCustomAttribute(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: Attribute.GetCustomAttribute expects (element, attributeType)")
	}
	targetName, ok := bcl.TypeFullNameOf(args[1])
	if !ok {
		return runtime.Null(), nil
	}
	for _, a := range resolveAttributesFor(m, args[0]) {
		if a.TypeFullName == targetName {
			return constructAttribute(m, a, depth, instrCount)
		}
	}
	return runtime.Null(), nil
}

// customAttributeExtensionsGetCustomAttribute backs CustomAttributeExtensions.
// GetCustomAttribute<T>(this MemberInfo) — T is a generic method type
// argument (methodGenericArgs[0], already resolved to a real closed type
// name by the time this runs — see Frame.MethodGenericArgs's own doc
// comment, Fase 3.60), not a runtime Type value the way
// Attribute.GetCustomAttribute's own 2nd argument above is.
func customAttributeExtensionsGetCustomAttribute(m *Machine, args []runtime.Value, methodGenericArgs []string, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: CustomAttributeExtensions.GetCustomAttribute<T> expects a receiver")
	}
	if len(methodGenericArgs) < 1 || methodGenericArgs[0] == "" {
		return runtime.Null(), nil
	}
	targetName := methodGenericArgs[0]
	for _, a := range resolveAttributesFor(m, args[0]) {
		if a.TypeFullName == targetName {
			return constructAttribute(m, a, depth, instrCount)
		}
	}
	return runtime.Null(), nil
}
