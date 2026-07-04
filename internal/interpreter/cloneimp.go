package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// DocumentFormat.OpenXml.OpenXmlElement.CloneImp<T>(bool deep) (Fase
// 3.43, found reading a real .xlsx through ClosedXML 0.105.0's `new
// XLWorkbook(stream)` — reached from real element cloning during
// worksheet loading) is one more instance of the exact
// unresolvable-generic-parameter shape loaddomtree.go documents in full:
// every real call site is a concrete element's own CloneNode override
// (e.g. `return CloneImp<AlternateContent>(deep);`, decompiled
// DocumentFormat.OpenXml/AlternateContent.cs:102 — hundreds of identical
// per-element overrides across the SDK), so ir.Call.MethodGenericArgs
// resolves T correctly at the CloneImp call site itself; but CloneImp<T>'s
// own shared body does `new T()` (compiled as `call !!0
// System.Activator::CreateInstance<!!T>()`), where T is its own still-open
// method parameter — "" by the time activator.go sees it.
//
// Real decompiled source (DocumentFormat.OpenXml.Framework 9.0.0,
// /tmp/openxmlfw_ns20/DocumentFormat.OpenXml/OpenXmlElement.cs:1700-1709):
//
//	internal virtual T CloneImp<T>(bool deep) where T : OpenXmlElement, new()
//	{
//	    T val = new T();
//	    val.CopyAttributes(this);
//	    if (deep)
//	    {
//	        val.CopyChildren(this, deep);
//	    }
//	    return val;
//	}
//
// Same treatment as LoadDomTree<T>: intercepted at its one genuinely
// non-chained entry point, `new T()` done natively from the call site's
// resolved methodGenericArgs[0], every other sub-operation (CopyAttributes,
// CopyChildren — both plain interpreted methods with no generic parameters
// of their own) called back into the real interpreted bodies.
func init() {
	genericMachineRegistry["DocumentFormat.OpenXml.OpenXmlElement::CloneImp"] = openXmlElementCloneImp
}

func openXmlElementCloneImp(m *Machine, args []runtime.Value, methodGenericArgs []string, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: OpenXmlElement.CloneImp<T> expects (receiver, deep)")
	}
	if len(methodGenericArgs) < 1 || methodGenericArgs[0] == "" {
		return runtime.Value{}, fmt.Errorf("interpreter: OpenXmlElement.CloneImp<T>: T could not be resolved (generic method chaining through its own open type parameter)")
	}
	recv, deep := args[0], args[1]

	val, err := m.newObj(newObjArgs{
		TypeFullName: methodGenericArgs[0],
		CtorFullName: methodGenericArgs[0] + "::.ctor",
	}, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}

	if _, _, err := m.call("DocumentFormat.OpenXml.OpenXmlElement::CopyAttributes", []runtime.Value{val, recv}, true, depth, instrCount, nil, nil); err != nil {
		return runtime.Value{}, err
	}
	if deep.Kind == runtime.KindI4 && deep.I4 != 0 {
		if _, _, err := m.call("DocumentFormat.OpenXml.OpenXmlElement::CopyChildren", []runtime.Value{val, recv, deep}, true, depth, instrCount, nil, nil); err != nil {
			return runtime.Value{}, err
		}
	}
	return val, nil
}
