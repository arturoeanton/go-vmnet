package interpreter

import (
	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// System.SpanHelpers (the real, interpreted internal TypeDef shipped
// inside the System.Memory netstandard2.0 shim package System.Text.Json
// depends on for down-level TFMs) memoizes IsReferenceOrContainsReferences
// <T>() per closed instantiation via a nested generic class's own static
// field: `PerTypeValues<T>.IsReferenceOrContainsReferences = ...Core(typeof
// (T))`. vmnet has no notion of a generic CLASS instantiation as a distinct
// runtime.Type (ir/builder.go's resolveTypeSpecName deliberately discards
// TypeSpec generic args for a static-field owner — true for native-backed
// generics like List<T>, false here), so every T sharing that one TypeDef
// gets the SAME *runtime.Type and therefore the same cached bool: whichever
// T calls this first "poisons" the answer for every other T for the rest of
// the process. Found via a real crash: System.Text.Json's own low-level
// buffer code called this first for some reference-containing internal
// struct, then later called it again for T=byte and got the stale `true`
// back, throwing a real ArgumentException
// (Argument_InvalidTypeWithPointersNotSupported) on perfectly valid byte
// data.
//
// Rather than teach the whole interpreter about per-instantiation static
// storage for generic classes (the real, general fix — out of scope here),
// this intercepts the public generic METHOD itself: same mechanism
// ir.Call.MethodGenericArgs/genericMachineRegistry already provide for
// DocumentFormat.OpenXml.Packaging.FeatureCollectionBase::Get<TFeature>
// (features.go), computing the answer directly from the call site's own
// resolved T instead of ever touching the shared, poisoned static field.
func init() {
	genericMachineRegistry["System.SpanHelpers::IsReferenceOrContainsReferences"] = spanHelpersIsReferenceOrContainsReferences
}

func spanHelpersIsReferenceOrContainsReferences(m *Machine, args []runtime.Value, methodGenericArgs []string, depth int, instrCount *int64) (runtime.Value, error) {
	if len(methodGenericArgs) < 1 || methodGenericArgs[0] == "" {
		return runtime.Bool(false), nil
	}
	return runtime.Bool(typeContainsReferences(m, methodGenericArgs[0])), nil
}

// typeContainsReferences answers the same question real
// IsReferenceOrContainsReferencesCore does, minus its field-by-field
// struct recursion (runtime.Type tracks instance field NAMES only, not
// field types — see runtime/class.go's Fields doc comment; no real caller
// found in this loop's target packages instantiates this over a
// reference-holding struct, only ever over primitives/enums/blittable
// value types like byte, char, and internal buffer structs).
func typeContainsReferences(m *Machine, typeFullName string) bool {
	open := bcl.GenericOpenName(typeFullName)
	if bclPrimitiveTypes[open] {
		return false
	}
	isValueType, isEnum, _, _ := classifyTypeByName(m, typeFullName)
	if isEnum {
		return false
	}
	return !isValueType
}
