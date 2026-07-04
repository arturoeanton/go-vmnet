package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// DocumentFormat.OpenXml.Packaging.OpenXmlPartContainer.GetPartsOfType<T>/
// GetSubPartOfType<T> (Fase 3.42, found reading a real .xlsx through
// ClosedXML 0.105.0's own `new XLWorkbook(stream)` — the SAME investigation
// that found LoadDomTree<T>'s "new T() inside its own shared IR body"
// shape, loaddomtree.go) hit the identical unresolvable-generic-parameter
// wall one level further down the call chain, through LINQ instead of
// Activator.CreateInstance.
//
// Real decompiled source (DocumentFormat.OpenXml.Framework 3.1.1,
// DocumentFormat.OpenXml.Packaging/OpenXmlPartContainer.cs:830-837,
// 1266-1277):
//
//	public IEnumerable<T> GetPartsOfType<T>() where T : OpenXmlPart
//	{
//	    ThrowIfObjectDisposed();
//	    return ChildrenRelationshipParts.Parts.OfType<T>();
//	}
//
//	internal T? GetSubPartOfType<T>() where T : OpenXmlPart
//	{
//	    ThrowIfObjectDisposed();
//	    using (IEnumerator<T> enumerator = GetPartsOfType<T>().GetEnumerator())
//	    {
//	        if (enumerator.MoveNext()) return enumerator.Current;
//	    }
//	    return null;
//	}
//
// Every real "SomePart? Foo => ((OpenXmlPartContainer)this).
// GetSubPartOfType<Foo>()" accessor across the whole OpenXml SDK (dozens:
// WorkbookPart.SharedStringTablePart, SpreadsheetDocument.
// CustomFilePropertiesPart, ...) is built on this pair. GetSubPartOfType<T>
// itself IS reached from a genuinely closed call site (e.g. `Spreadsheet
// Document::get_CustomFilePropertiesPart` calling
// `GetSubPartOfType<CustomFilePropertiesPart>()` via a real, closed
// MethodSpec — confirmed via temporary tracing), so ir.Call.MethodGenericArgs
// resolves T correctly right there. But GetSubPartOfType<T>'s own shared IR
// body then calls `GetPartsOfType<T>()` using ITS OWN T — a still-open
// method generic parameter from vmnet's point of view (same "one shared IR
// body per generic method definition" limitation LoadDomTree<T>'s inner
// `new T()` hits, see loaddomtree.go's own doc comment) — and THAT call's
// own inner `.OfType<T>()` is a second, even deeper instance of the exact
// same problem. Both would resolve methodGenericArgs to "" if left to run
// as ordinary interpreted IL, and linqOfType (linq.go) deliberately
// degrades an empty T to the old unfiltered pass-through rather than
// filtering everything out — which is exactly how this bug first
// surfaced: GetSubPartOfType<CustomFilePropertiesPart>() on a real .xlsx
// with no docProps/custom.xml at all (so genuinely zero CustomFileProperties
// Parts) silently returned the package's first unrelated child part
// (WorkbookPart) instead of null, corrupting a document read that never
// should have touched CustomFilePropertiesPart::get_Properties at all.
//
// Fixed the same way LoadDomTree<T> was: intercept both methods at their
// own genuinely-closed outer call sites (genericMachineRegistry) and
// reimplement their bodies natively, calling back into the real
// interpreted sub-operations that aren't themselves part of the
// unresolvable-generic shape (ChildrenRelationshipParts, Parts, and
// isAssignableTo — the same real is-a check Fase 3.8's isinst/castclass
// already implements, reused rather than duplicated) instead of ever
// re-entering GetPartsOfType<T>'s or Enumerable.OfType<T>'s own shared,
// generic-parameter-still-open IR bodies.
func init() {
	genericMachineRegistry["DocumentFormat.OpenXml.Packaging.OpenXmlPartContainer::GetPartsOfType"] = openXmlPartContainerGetPartsOfType
	genericMachineRegistry["DocumentFormat.OpenXml.Packaging.OpenXmlPartContainer::GetSubPartOfType"] = openXmlPartContainerGetSubPartOfType
}

// childPartsOfType is the real, shared body both GetPartsOfType<T> and
// GetSubPartOfType<T> boil down to once T is known: read the container's
// own ChildrenRelationshipParts.Parts (a real IEnumerable<OpenXmlPart>,
// unaffected by the generic-parameter problem this file exists to work
// around) and keep only the elements whose real runtime type is T or
// derives from it.
func childPartsOfType(m *Machine, recv runtime.Value, target string, depth int, instrCount *int64) ([]runtime.Value, error) {
	features, _, err := m.call("DocumentFormat.OpenXml.Packaging.OpenXmlPartContainer::get_ChildrenRelationshipParts", []runtime.Value{recv}, true, depth, instrCount, nil, nil)
	if err != nil {
		return nil, err
	}
	parts, _, err := m.call("DocumentFormat.OpenXml.Features.IPartRelationshipsFeature::get_Parts", []runtime.Value{features}, true, depth, instrCount, nil, nil)
	if err != nil {
		return nil, err
	}
	elems, err := m.enumerateAll(parts, depth, instrCount)
	if err != nil {
		return nil, err
	}
	if target == "" {
		// T genuinely unresolvable (e.g. reached some other, still-open
		// call shape this fix doesn't cover) — degrade to "every child
		// part", the same permissive fallback linqOfType takes, rather
		// than silently filtering everything out.
		return elems, nil
	}
	out := make([]runtime.Value, 0, len(elems))
	for _, e := range elems {
		if e.Kind != runtime.KindNull && m.isAssignableTo(e, target) {
			out = append(out, e)
		}
	}
	return out, nil
}

func openXmlPartContainerGetPartsOfType(m *Machine, args []runtime.Value, methodGenericArgs []string, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: OpenXmlPartContainer.GetPartsOfType<T> called without a receiver")
	}
	target := ""
	if len(methodGenericArgs) > 0 {
		target = methodGenericArgs[0]
	}
	out, err := childPartsOfType(m, args[0], target, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	return bcl.NewListValue(out), nil
}

func openXmlPartContainerGetSubPartOfType(m *Machine, args []runtime.Value, methodGenericArgs []string, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: OpenXmlPartContainer.GetSubPartOfType<T> called without a receiver")
	}
	target := ""
	if len(methodGenericArgs) > 0 {
		target = methodGenericArgs[0]
	}
	out, err := childPartsOfType(m, args[0], target, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(out) == 0 {
		return runtime.Null(), nil
	}
	return out[0], nil
}
