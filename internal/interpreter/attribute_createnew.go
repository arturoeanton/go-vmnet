package interpreter

import (
	"fmt"
	"sync"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// DocumentFormat.OpenXml.Framework.Metadata.AttributeMetadata.Builder<
// TSimpleType>'s nested AttributeInfo.CreateNew() (Fase 3.42, found
// running a real .xlsx through ClosedXML 0.105.0/DocumentFormat.OpenXml
// 3.1.1's own `new XLWorkbook(stream)` — one level deeper into the same
// investigation that found OpenXmlPartContainer.GetPartsOfType<T>
// (partcontainer.go) and OpenXmlPart.LoadDomTree<T> (loaddomtree.go)):
//
//	public override OpenXmlSimpleType CreateNew() => new TSimpleType();
//
// (real decompiled source: DocumentFormat.OpenXml.Framework.Metadata/
// AttributeMetadata.cs:30-33). Unlike LoadDomTree<T> and GetPartsOfType
// <T>, TSimpleType here is a generic CLASS parameter (Builder<TSimpleType>
// itself, not CreateNew() — a plain, non-generic override), the exact
// "generic class instantiation identity" gap attributeMetadataBuilderCctor
// (attribute_metadata.go, this same Fase 3.41 investigation) already
// documents for `Builder<TSimpleType>`'s own static _defaultValidator:
// vmnet's runtime.Type model has ONE shared Type for "AttributeMetadata+
// Builder`1+AttributeInfo" no matter which TSimpleType closes it, so
// CreateNew()'s own receiver carries no usable T at all — there is no
// ir.Call.MethodGenericArgs here to read (CreateNew() itself takes no
// generic parameters; T belongs to the enclosing class).
//
// Unlike _defaultValidator (validation-only, confirmed via grep that
// nothing outside DocumentFormat.OpenXml.Validation.Schema/
// SchemaTypeValidator.cs ever reads AttributeMetadata.Validators — safe
// to seed with one arbitrary-but-honest default), CreateNew() sits
// squarely on the real attribute-parsing read path: OpenXmlElement.
// TrySetFixedAttribute (DocumentFormat.OpenXml/OpenXmlElement.cs:1452)
// does `attributeEntry.Value = attributeEntry.Property.CreateNew();
// attributeEntry.Value.InnerText = value;` while parsing every real XML
// attribute (confirmed via temporary tracing: reached while parsing
// xl/sharedStrings.xml's own real "count"/"uniqueCount" attributes on
// <sst>, both real UInt32Value-typed) — returning the wrong concrete
// OpenXmlSimpleType subtype here would silently corrupt whatever typed
// accessor (.Value as uint, as bool, ...) the caller uses next, so
// there's no safe "any answer will do" shortcut like the validator case.
//
// Fixed by relaying TSimpleType across the one call site that DOES still
// have it — ElementMetadata.Builder<TElement>.AddAttribute<TSimpleType>
// (ElementMetadata.cs:129-139), a genuine, closed generic METHOD
// instantiation whose own ir.Call.MethodGenericArgs resolves correctly,
// exactly like AddChild<T>'s own outer call site (elementfactory.go).
// AddAttribute<TSimpleType> itself is left to run entirely unmodified —
// it already builds a real, correct AttributeInfo today (its own QName/
// PropertyName wiring is proven working: AttributeCollection.GetIndex,
// confirmed via tracing, matches real parsed attributes against it
// without issue; only CreateNew() was ever broken) — so re-implementing
// any of that would only risk a regression for zero benefit. Instead,
// this records a small, honest (Namespace, LocalName, PropertyName) ->
// TSimpleType map at AddAttribute<TSimpleType>'s own real call site
// (computed via the exact same real sub-calls the interpreted body
// itself already uses: Builder.CreateQName, LambdaExpression.Body,
// MemberExpression.Member, MemberInfo.Name — nothing new), then consults
// it from CreateNew() by reading the SAME AttributeInfo instance's own
// already-working QName/PropertyName getters. A genuine miss (some
// AddAttribute<T> shape this doesn't cover) falls through to the real
// interpreted CreateNew() body, surfacing the exact same honest
// "T could not be resolved" error as before rather than fabricating a
// wrong type — no worse than the pre-fix behavior for anything this
// doesn't handle.
const addAttributeFullName = "DocumentFormat.OpenXml.Framework.Metadata.ElementMetadata+Builder`1::AddAttribute"

type attrKey struct {
	ns, name, property string
}

var (
	attrSimpleTypeMu       sync.RWMutex
	attrSimpleTypeRegistry = map[attrKey]string{}
)

func init() {
	genericMachineRegistry[addAttributeFullName] = elementMetadataAddAttribute
	machineRegistry["DocumentFormat.OpenXml.Framework.Metadata.AttributeMetadata+Builder`1+AttributeInfo::CreateNew"] = attributeInfoCreateNew
}

// elementMetadataAddAttribute records the (qname, propertyName) ->
// TSimpleType association this specific call site knows (best-effort:
// any failure just skips recording, never blocks the real call below),
// then runs the exact same real interpreted AddAttribute<TSimpleType>
// body untouched via m.Resolve+m.invoke — bypassing only this one
// genericMachineRegistry entry itself, not any other dispatch step.
func elementMetadataAddAttribute(m *Machine, args []runtime.Value, methodGenericArgs []string, depth int, instrCount *int64) (runtime.Value, error) {
	if len(methodGenericArgs) > 0 && methodGenericArgs[0] != "" && len(args) >= 3 {
		recordAttributeSimpleType(m, args, methodGenericArgs[0], depth, instrCount)
	}
	if m.Resolve == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: ElementMetadata.Builder<TElement>.AddAttribute<T>: no Resolver attached")
	}
	method, err := m.Resolve(addAttributeFullName, args, nil, len(methodGenericArgs))
	if err != nil {
		return runtime.Value{}, err
	}
	return m.invoke(method, args, depth+1, instrCount)
}

// recordAttributeSimpleType computes the real (namespace, localName)
// OpenXmlQualifiedName and property name this AddAttribute<TSimpleType>
// call site wires up — the exact same two real sub-operations the real
// interpreted body itself performs (Builder.CreateQName; expression.Body
// as MemberExpression -> .Member.Name) — and stores the association.
// Never returns an error to its caller: a lookup/shape miss here just
// means CreateNew() won't have an entry later (falls through to the real
// body's own honest error), not a reason to fail the real AddAttribute
// call this wraps.
func recordAttributeSimpleType(m *Machine, args []runtime.Value, simpleType string, depth int, instrCount *int64) {
	recv := args[0]
	if recv.Kind != runtime.KindObject || recv.Obj == nil || recv.Obj.Type == nil {
		return
	}
	idx := recv.Obj.Type.FieldIndex("_builder")
	if idx < 0 || idx >= len(recv.Obj.Fields) {
		return
	}
	outerBuilder := recv.Obj.Fields[idx]
	qname, _, err := m.call("DocumentFormat.OpenXml.Framework.Metadata.ElementMetadata+Builder::CreateQName", []runtime.Value{outerBuilder, args[1]}, true, depth, instrCount, nil, nil)
	if err != nil {
		return
	}
	ns, name, ok := qualifiedNameParts(qname)
	if !ok {
		return
	}
	propertyName := ""
	if len(args) >= 3 {
		if body, _, err := m.call("System.Linq.Expressions.LambdaExpression::get_Body", []runtime.Value{args[2]}, true, depth, instrCount, nil, nil); err == nil {
			if member, _, err := m.call("System.Linq.Expressions.MemberExpression::get_Member", []runtime.Value{body}, true, depth, instrCount, nil, nil); err == nil {
				if nameVal, _, err := m.call("System.Reflection.MemberInfo::get_Name", []runtime.Value{member}, true, depth, instrCount, nil, nil); err == nil && nameVal.Kind == runtime.KindString {
					propertyName = nameVal.Str
				}
			}
		}
	}
	key := attrKey{ns: ns, name: name, property: propertyName}
	attrSimpleTypeMu.Lock()
	attrSimpleTypeRegistry[key] = simpleType
	attrSimpleTypeMu.Unlock()
}

// qualifiedNameParts reads a real OpenXmlQualifiedName struct value's own
// Namespace.Uri/Name fields directly (no interpreted call needed: it's a
// plain readonly struct, see internal/bcl's own OpenXmlQualifiedName
// doc comments for the equivalent System.Xml.XmlQualifiedName case) —
// falls back to a real getter call for a KindObject/KindRef shape this
// doesn't recognize directly, rather than assuming the struct layout.
func qualifiedNameParts(v runtime.Value) (ns, name string, ok bool) {
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	if v.Kind != runtime.KindStruct || v.Struct == nil || v.Struct.Type == nil {
		return "", "", false
	}
	nsIdx := v.Struct.Type.FieldIndex("<Namespace>k__BackingField")
	nameIdx := v.Struct.Type.FieldIndex("<Name>k__BackingField")
	if nsIdx < 0 || nameIdx < 0 || nsIdx >= len(v.Struct.Fields) || nameIdx >= len(v.Struct.Fields) {
		return "", "", false
	}
	nsVal := v.Struct.Fields[nsIdx]
	if nsVal.Kind == runtime.KindStruct && nsVal.Struct != nil {
		uriIdx := nsVal.Struct.Type.FieldIndex("_uri")
		if uriIdx >= 0 && uriIdx < len(nsVal.Struct.Fields) && nsVal.Struct.Fields[uriIdx].Kind == runtime.KindString {
			ns = nsVal.Struct.Fields[uriIdx].Str
		}
	}
	if v.Struct.Fields[nameIdx].Kind == runtime.KindString {
		name = v.Struct.Fields[nameIdx].Str
	}
	return ns, name, true
}

// attributeInfoCreateNew looks up the real TSimpleType recorded for this
// AttributeInfo instance's own (already-correct) QName/PropertyName —
// both real, working getter calls on the shared "AttributeMetadata+
// Builder`1+AttributeInfo" type, untouched by this fix — and constructs
// a genuine `new TSimpleType()`. A registry miss falls through to the
// real interpreted CreateNew() body (m.Resolve+m.invoke, same bypass-
// only-this-one-entry pattern as elementMetadataAddAttribute), so an
// uncovered shape fails exactly as honestly as it did before this fix.
func attributeInfoCreateNew(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) >= 1 {
		if qname, _, err := m.call("DocumentFormat.OpenXml.Framework.Metadata.AttributeMetadata::get_QName", []runtime.Value{args[0]}, true, depth, instrCount, nil, nil); err == nil {
			if ns, name, ok := qualifiedNameParts(qname); ok {
				propertyName := ""
				if pn, _, err := m.call("DocumentFormat.OpenXml.Framework.Metadata.AttributeMetadata::get_PropertyName", []runtime.Value{args[0]}, true, depth, instrCount, nil, nil); err == nil && pn.Kind == runtime.KindString {
					propertyName = pn.Str
				}
				attrSimpleTypeMu.RLock()
				simpleType, found := attrSimpleTypeRegistry[attrKey{ns: ns, name: name, property: propertyName}]
				attrSimpleTypeMu.RUnlock()
				if found {
					// simpleType may be a CLOSED generic instantiation (e.g.
					// "DocumentFormat.OpenXml.EnumValue`1[[...SystemColor
					// Values]]", straight from AddAttribute<TSimpleType>'s
					// own MethodSpec, see SigTypeFullName's doc comment) —
					// vmnet has exactly one shared Type per OPEN generic
					// class definition (no closed-instantiation identity,
					// same limitation attribute_metadata.go/elementfactory.go
					// both already document), so newObj must construct the
					// bare open name instead; every other real generic
					// construction in this codebase already goes through
					// the open form the same way (e.g. AttributeMetadata+
					// Builder`1::.ctor, never a bracketed closed name).
					openName := bcl.GenericOpenName(simpleType)
					obj, err := m.newObj(newObjArgs{TypeFullName: openName, CtorFullName: openName + "::.ctor"}, depth, instrCount)
					// EnumValue`1 specifically (Fase 3.44, enumvalue_tryparse.go)
					// needs its own closed T back later, when TryParse has to
					// build a real default(T) to call IEnumValueFactory<T>::
					// Create through — stash it on the fresh object's Native
					// side channel (nil for a plain interpreted object like
					// this one otherwise) rather than threading it through
					// every intermediate call. Every other TSimpleType
					// (StringValue, UInt32Value, ...) hardcodes its own
					// non-generic T and never needs this.
					if err == nil && openName == "DocumentFormat.OpenXml.EnumValue`1" && obj.Kind == runtime.KindObject && obj.Obj != nil {
						if t, ok := firstClosedGenericArg(simpleType); ok {
							obj.Obj.Native = &enumValueGenericArg{typeFullName: t}
						}
					}
					return obj, err
				}
			}
		}
	}
	// No recorded association for this instance (an AddAttribute<T> shape
	// this fix doesn't cover, or a genuine miss) — fall through to the
	// real interpreted CreateNew() body, bypassing only this one
	// machineRegistry entry, so an uncovered case fails exactly as
	// honestly as it did before this fix rather than fabricating a type.
	const fullName = "DocumentFormat.OpenXml.Framework.Metadata.AttributeMetadata+Builder`1+AttributeInfo::CreateNew"
	if m.Resolve == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: AttributeInfo.CreateNew: no Resolver attached")
	}
	method, err := m.Resolve(fullName, args, nil, 0)
	if err != nil {
		return runtime.Value{}, err
	}
	return m.invoke(method, args, depth+1, instrCount)
}
