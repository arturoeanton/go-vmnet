package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// DocumentFormat.OpenXml.OpenXmlCompositeElement.GetElement<TElement>()
// (Fase 3.42, found running a real .xlsx through ClosedXML 0.105.0's own
// `new XLWorkbook(stream)` — one more real call site hitting the same
// "generic method forwards its own still-open T into a shared inner
// generic method body" shape features.go's FeatureCollectionBase.
// GetRequired<TFeature>/Get<TFeature> already documents) backs every
// real strongly-typed single-child accessor across the whole SDK, e.g.
// `Workbook.Sheets => GetElement<Sheets>();`.
//
// Real decompiled source (DocumentFormat.OpenXml.Framework 3.1.1,
// DocumentFormat.OpenXml/OpenXmlCompositeElement.cs:611-614 and
// DocumentFormat.OpenXml.Framework/ParticleExtensions.cs:17-34):
//
//	private protected TElement? GetElement<TElement>() where TElement : OpenXmlElement
//	{
//	    return base.Metadata.Particle.Get<TElement>(this);
//	}
//
//	public static TElement? Get<TElement>(this CompiledParticle? compiled, OpenXmlCompositeElement element) where TElement : OpenXmlElement
//	{
//	    if (compiled == null) return null;
//	    OpenXmlElement openXmlElement = element.FirstChild;
//	    if (openXmlElement == null) return null;
//	    do {
//	        if (openXmlElement.GetType() == typeof(TElement)) return (TElement)openXmlElement;
//	        openXmlElement = openXmlElement.Next;
//	    } while (openXmlElement != null && openXmlElement != element.FirstChild);
//	    return null;
//	}
//
// GetElement<TElement>()'s OWN call site (e.g. Workbook::get_Sheets
// calling `GetElement<Sheets>()`) is genuinely closed — a real MethodSpec
// resolves TElement correctly there — but its body immediately forwards
// that same T into `ParticleExtensions.Get<TElement>(...)`, a SEPARATE
// generic method whose own shared IR body sees only GetElement's own
// still-open method parameter (methodGenericArgs resolves to "" there,
// same as GetRequired<TFeature>'s forwarding call to Get<TFeature> —
// see features.go's own doc comment for the general shape). Worse than
// the feature-lookup case: real Get<TElement> uses `typeof(TElement)`
// for an exact type-identity comparison against each real child
// element — with T unresolvable, that comparison can never succeed, so
// EVERY real `SomeElement.SomeChild` single-child accessor across the
// whole SDK would silently return null even when the real child exists
// in the parsed document (confirmed via tracing: `workbookPart.Workbook.
// Sheets` returned null against a real workbook.xml whose <x:sheets>
// child is right there, then `((IEnumerable)sheets).OfType<Sheet>()`
// threw a real ArgumentNullException("source") over that null — not a
// vmnet crash, but a real, load-bearing case of "the read path
// depends on this resolving correctly").
//
// Fixed the same way GetRequired<TFeature> was: intercept GetElement<T>
// itself (genuinely closed T) and reimplement its whole effect —
// including inlining ParticleExtensions.Get<T>'s own child-walk — using
// only real interpreted sub-calls (FirstChild/Next, both plain, already-
// working properties untouched by any generic-parameter problem) plus
// receiverTypeName (typecheck.go), the same real concrete-type-name
// lookup isinst/castclass and the interface-dispatch ancestor walk
// already use, in place of the one operation (`typeof(TElement)`) that
// can't run as ordinary interpreted IL here.
func init() {
	genericMachineRegistry["DocumentFormat.OpenXml.OpenXmlCompositeElement::GetElement"] = openXmlCompositeElementGetElement
}

func openXmlCompositeElementGetElement(m *Machine, args []runtime.Value, methodGenericArgs []string, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: OpenXmlCompositeElement.GetElement<T> called without a receiver")
	}
	if len(methodGenericArgs) < 1 || methodGenericArgs[0] == "" {
		// T genuinely unresolvable (some other, still-open call shape
		// this fix doesn't cover) — null is what every real "no known
		// child of this type" case already returns, so this degrades
		// permissively rather than erroring the whole accessor out.
		return runtime.Null(), nil
	}
	target := methodGenericArgs[0]
	recv := args[0]

	first, _, err := m.call("DocumentFormat.OpenXml.OpenXmlElement::get_FirstChild", []runtime.Value{recv}, true, depth, instrCount, nil, nil)
	if err != nil {
		return runtime.Value{}, err
	}
	if first.Kind == runtime.KindNull || first.Obj == nil {
		return runtime.Null(), nil
	}

	cur := first
	for {
		if name, ok := receiverTypeName(cur); ok && name == target {
			return cur, nil
		}
		next, _, err := m.call("DocumentFormat.OpenXml.OpenXmlElement::get_Next", []runtime.Value{cur}, true, depth, instrCount, nil, nil)
		if err != nil {
			return runtime.Value{}, err
		}
		if next.Kind == runtime.KindNull || next.Obj == nil || next.Obj == first.Obj {
			return runtime.Null(), nil
		}
		cur = next
	}
}
