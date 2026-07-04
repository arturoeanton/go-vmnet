package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// DocumentFormat.OpenXml.OpenXmlElement.GetAttribute<TSimpleType>() (Fase
// 3.42, the fourth real call site found running a real .xlsx through
// ClosedXML 0.105.0's own `new XLWorkbook(stream)` in this same
// investigation — after LoadDomTree<T> (loaddomtree.go), GetPartsOfType
// <T>/GetSubPartOfType<T> (partcontainer.go), and AttributeInfo.CreateNew
// (attribute_createnew.go)) backs literally every strongly-typed
// attribute-accessor property in the whole SDK — `Sheet.SheetId`,
// `Cell.CellReference`, every single `OpenXmlSimpleType?`-returning
// property — so its own real, load-bearing role is enormous even though
// its own real body is tiny.
//
// Real decompiled source (DocumentFormat.OpenXml.Framework 3.1.1,
// DocumentFormat.OpenXml/OpenXmlElement.cs:564-567):
//
//	private protected TSimpleType? GetAttribute<TSimpleType>([CallerMemberName] string propertyName = null) where TSimpleType : OpenXmlSimpleType
//	{
//	    return ParsedState.Attributes.GetProperty(propertyName).Value as TSimpleType;
//	}
//
// GetAttribute<TSimpleType>'s OWN call site (e.g. `Sheet.SheetId`'s real
// getter, `GetAttribute<UInt32Value>("SheetId")`) is genuinely closed —
// ir.Call.MethodGenericArgs resolves TSimpleType correctly right there —
// but its real body's `as TSimpleType` compiles to an `isinst`
// instruction whose own type token is TSimpleType, a generic METHOD
// parameter. Unlike a chained call's MethodGenericArgs (LoadDomTree<T>'s
// inner `new T()`, GetElement<T>'s inner Particle.Get<T>(), ...), an
// isinst/castclass instruction's target type is baked into ir.IsInst.
// TypeFullName at IR-BUILD time (internal/ir/builder.go), from whichever
// name the type token resolves to THEN — for an MVAR referencing the
// enclosing generic method's own still-open parameter, that's always ""
// (see ir.IsInst's own doc comment / eval.go's LoadTypeToken case for the
// identical "resolves once, shared by every instantiation" limitation),
// and isAssignableTo(v, "") is unconditionally false (typecheck.go) — so
// the real `as TSimpleType` cast here always failed, silently returning
// null for EVERY typed attribute read in the entire SDK regardless of
// the real parsed value underneath (confirmed via tracing: `Sheet.
// SheetId` returned null against a real `sheetId="1"` attribute
// DocumentFormat.OpenXml.Framework.Metadata.AttributeCollection.
// AttributeEntry.Value had already correctly parsed and stored, itself
// thanks to attribute_createnew.go's own fix earlier in this same
// investigation).
//
// Fixed by intercepting GetAttribute<TSimpleType> itself (genuinely
// closed T at every real call site) and reimplementing its tiny body
// with real sub-calls for every step that doesn't need T (ParsedState,
// Attributes, GetProperty, Value — all plain, already-working struct
// property/method calls) and m.isAssignableTo (typecheck.go's own real
// isinst/castclass check, reused rather than duplicated) for the one
// step that does.
func init() {
	genericMachineRegistry["DocumentFormat.OpenXml.OpenXmlElement::GetAttribute"] = openXmlElementGetAttribute
}

func openXmlElementGetAttribute(m *Machine, args []runtime.Value, methodGenericArgs []string, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: OpenXmlElement.GetAttribute<T> expects (this, propertyName)")
	}
	recv := args[0]
	propertyName := args[1]

	parsedState, _, err := m.call("DocumentFormat.OpenXml.OpenXmlElement::get_ParsedState", []runtime.Value{recv}, true, depth, instrCount, nil, nil)
	if err != nil {
		return runtime.Value{}, err
	}
	attributes, _, err := m.call("DocumentFormat.OpenXml.Framework.Metadata.ElementState::get_Attributes", []runtime.Value{runtime.RefTo(&parsedState)}, true, depth, instrCount, nil, nil)
	if err != nil {
		return runtime.Value{}, err
	}
	entry, _, err := m.call("DocumentFormat.OpenXml.Framework.Metadata.AttributeCollection::GetProperty", []runtime.Value{runtime.RefTo(&attributes), propertyName}, true, depth, instrCount, nil, nil)
	if err != nil {
		return runtime.Value{}, err
	}
	value, _, err := m.call("DocumentFormat.OpenXml.Framework.Metadata.AttributeCollection+AttributeEntry::get_Value", []runtime.Value{runtime.RefTo(&entry)}, true, depth, instrCount, nil, nil)
	if err != nil {
		return runtime.Value{}, err
	}
	// AttributeEntry's real Value getter returns `ref OpenXmlSimpleType?`
	// (a managed pointer into the AttributeCollection's own backing
	// array, not the value itself — real source: `public ref
	// OpenXmlSimpleType? Value => ref _collection._data[_index];`), so
	// m.call's result here is a KindRef needing one more dereference
	// before it's the actual stored value/null to inspect.
	if value.Kind == runtime.KindRef && value.Ref != nil {
		value = *value.Ref
	}
	if value.Kind == runtime.KindNull {
		return runtime.Null(), nil
	}
	if len(methodGenericArgs) < 1 || methodGenericArgs[0] == "" {
		// T genuinely unresolvable here — the real `as T` cast can't be
		// checked either way; returning the real underlying value
		// (rather than always-null) is the more useful degradation for
		// any caller that just re-reads it structurally.
		return value, nil
	}
	// methodGenericArgs[0] may be a closed generic instantiation (e.g.
	// "DocumentFormat.OpenXml.EnumValue`1[[...SystemColorValues]]") —
	// isAssignableTo/typeMatches compares against a real Object's own
	// Type, which for a generic class is always the bare OPEN name
	// (vmnet has no closed-generic-instantiation identity at all, see
	// attribute_createnew.go's own doc comment); the open form is what
	// every other real caller already strips to before calling
	// isAssignableTo with a possibly-closed methodGenericArgs value
	// (features.go's featureCollectionGetImpl, reflection.go, ...).
	if m.isAssignableTo(value, bcl.GenericOpenName(methodGenericArgs[0])) {
		return value, nil
	}
	return runtime.Null(), nil
}
