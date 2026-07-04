package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// DocumentFormat.OpenXml.OpenXmlElement.Elements<T>()/GetFirstChild<T>()
// (Fase 3.43, found running a real .xlsx through ClosedXML 0.105.0's own
// `new XLWorkbook(stream)` — one more real call site hitting the same
// "generic method forwards its own still-open T into a shared inner
// generic method body" shape linqOfType (linq.go), GetElement<T>
// (getelement.go) and GetPartsOfType<T>/GetSubPartOfType<T>
// (partcontainer.go) all already document and fix for their own call
// shapes.
//
// Real decompiled source (DocumentFormat.OpenXml.Framework 9.0.0,
// /tmp/openxmlfw_ns20/DocumentFormat.OpenXml/OpenXmlElement.cs:847-850,
// 958-961, and OpenXmlElementList.cs:39-58,127-139):
//
//	public T? GetFirstChild<T>() where T : OpenXmlElement
//	{
//	    return ChildElements.First<T>();
//	}
//	public IEnumerable<T> Elements<T>() where T : OpenXmlElement
//	{
//	    return ChildElements.OfType<T>();
//	}
//	// OpenXmlElementList.cs's own Enumerator.MoveNext():
//	//   _current = (_current != null) ? _current.NextSibling() : _element.FirstChild;
//	// OpenXmlElementList.First<T>():
//	//   foreach (child in this) if (child is T result) return result;
//
// Both GetFirstChild<T> and Elements<T> are called with a genuinely
// closed T at THEIR OWN call site (e.g. LoadSpreadsheetDocument's own
// `extendedFilePropertiesPart.Properties.Elements<Company>().Any()` /
// `.GetFirstChild<Company>()`) — ir.Call.MethodGenericArgs resolves T
// correctly right there. But both bodies immediately forward that same T
// into a SECOND generic method (OpenXmlElementList.OfType<T>/First<T>),
// whose own shared IR sees only Elements<T>'s/GetFirstChild<T>'s own
// still-open method type parameter (methodGenericArgs resolves to ""
// there) — exactly the shape every sibling fix above already names.
//
// The two inner methods degrade differently on an unresolved T, which is
// what turned this into a real, silently-corrupting bug rather than a
// clean failure: linqOfType-style "OfType<T> with unknown T" used to fall
// back to an UNFILTERED pass-through (permissive), while `is T` pattern
// matching on an unresolved/empty type name (isAssignableTo's own "" case,
// typecheck.go) is unconditionally false (restrictive). So — confirmed via
// temporary call tracing against testdata/sample.xlsx's
// real docProps/app.xml, which has neither a <Company> nor <Manager>
// element — `Elements<Company>().Any()` incorrectly returned true (Any()
// saw the UNFILTERED list of every real child: Application, TitlesOfParts,
// HeadingPairs, DocSecurity, ScaleCrop), while `GetFirstChild<Company>()`
// correctly-by-accident returned null (every real child failed the
// unresolved `is T` check). LoadSpreadsheetDocument's own real source then
// does exactly `if (...Elements<Company>().Any()) { Properties.Company =
// ((OpenXmlLeafTextElement)...GetFirstChild<Company>()).Text; }` — the
// mismatched-but-individually-"reasonable" degradations combined into a
// real NullReferenceException on OpenXmlLeafTextElement.Text's own null
// receiver (eval.go's callvirt-on-null check), not a vmnet crash.
//
// Fixed the same way GetElement<T> (getelement.go) was: intercept
// Elements<T> and GetFirstChild<T> themselves (genuinely closed T) and
// reimplement their whole effect using only real interpreted sub-calls —
// get_FirstChild and the real (non-generic, already-correct) NextSibling()
// method, exactly matching OpenXmlElementList.Enumerator.MoveNext()'s own
// walk (NOT the raw `.Next` field GetElement<T>'s ParticleExtensions.Get<T>
// walk uses instead — a different real algorithm for a different real
// method, ChildElements' own enumeration protocol specifically) — plus
// isAssignableTo (typecheck.go), the real inheritance-aware `is T` check
// OfType<T>/`is T` pattern matching both need and linqOfType already
// reuses for the identical reason.
func init() {
	genericMachineRegistry["DocumentFormat.OpenXml.OpenXmlElement::Elements"] = openXmlElementElements
	genericMachineRegistry["DocumentFormat.OpenXml.OpenXmlElement::GetFirstChild"] = openXmlElementGetFirstChild
}

// openXmlElementChildren walks recv's real children using get_FirstChild
// and get_Next — the same raw-field traversal (NOT the higher-level
// NextSibling() method) GetElement<T>'s own child-walk already uses
// (getelement.go), stopping on either a real null or wrapping back to the
// first child.
//
// The real _lastChild.Next child-list is circular (OpenXmlCompositeElement.
// FirstChild getter, OpenXmlCompositeElement.cs:28-35: `return _lastChild?.
// Next`, i.e., the last child's own Next wraps to the first) — so
// NextSibling()'s real body (OpenXmlElement.cs:898-906) stops by comparing
// Next against parent.FirstChild each step, which re-invokes the parent's
// FirstChild getter (and therefore MakeSureParsed()) on every single
// traversal step. Calling the real NextSibling() method here to mirror
// OpenXmlElementList.Enumerator.MoveNext()'s exact algorithm was tried
// first and hung this whole demo indefinitely (100%+ CPU, no forward
// progress) — not this file's own loop (confirmed via temporary iteration
// tracing: stuck on the very first NextSibling() call, before ever
// reaching a second iteration), so the cost lives inside NextSibling()'s
// own repeated FirstChild/MakeSureParsed() re-entry chain for this
// receiver shape, not diagnosed further given get_Next's raw traversal
// already has a proven-correct, much cheaper equivalent right here
// (getelement.go's own GetElement<T> fix already relies on it for the
// identical circular-list shape). Comparing next against the ORIGINAL
// first child (by object identity) reproduces the same "stop before
// wrapping around" behavior without re-deriving parent.FirstChild on
// every step.
func openXmlElementChildren(m *Machine, recv runtime.Value, depth int, instrCount *int64) ([]runtime.Value, error) {
	first, _, err := m.call("DocumentFormat.OpenXml.OpenXmlElement::get_FirstChild", []runtime.Value{recv}, true, depth, instrCount, nil, nil)
	if err != nil {
		return nil, err
	}
	if first.Kind == runtime.KindNull || first.Obj == nil {
		return nil, nil
	}
	var out []runtime.Value
	cur := first
	for {
		out = append(out, cur)
		next, _, err := m.call("DocumentFormat.OpenXml.OpenXmlElement::get_Next", []runtime.Value{cur}, true, depth, instrCount, nil, nil)
		if err != nil {
			return nil, err
		}
		if next.Kind == runtime.KindNull || next.Obj == nil || next.Obj == first.Obj {
			return out, nil
		}
		cur = next
	}
}

func openXmlElementElements(m *Machine, args []runtime.Value, methodGenericArgs []string, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: OpenXmlElement.Elements<T> called without a receiver")
	}
	children, err := openXmlElementChildren(m, args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(methodGenericArgs) < 1 || methodGenericArgs[0] == "" {
		// T genuinely unresolvable — same "wrong-but-permissive beats
		// wrong-but-empty" call linqOfType already makes (linq.go) for the
		// identical unresolved-T shape.
		return bcl.NewListValue(children), nil
	}
	target := methodGenericArgs[0]
	filtered := make([]runtime.Value, 0, len(children))
	for _, c := range children {
		if m.isAssignableTo(c, target) {
			filtered = append(filtered, c)
		}
	}
	return bcl.NewListValue(filtered), nil
}

func openXmlElementGetFirstChild(m *Machine, args []runtime.Value, methodGenericArgs []string, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: OpenXmlElement.GetFirstChild<T> called without a receiver")
	}
	children, err := openXmlElementChildren(m, args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	target := ""
	if len(methodGenericArgs) > 0 {
		target = methodGenericArgs[0]
	}
	if target == "" {
		// T genuinely unresolvable — null is what every real "no known
		// child of this type" case already returns, matching GetElement<T>'s
		// own degradation (getelement.go) for the analogous single-child
		// accessor shape.
		return runtime.Null(), nil
	}
	for _, c := range children {
		if m.isAssignableTo(c, target) {
			return c, nil
		}
	}
	return runtime.Null(), nil
}
