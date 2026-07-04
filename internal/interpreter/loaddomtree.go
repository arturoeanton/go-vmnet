package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// DocumentFormat.OpenXml.Packaging.OpenXmlPart.LoadDomTree<T>() (Fase
// 3.41, found reading a real .xlsx through ClosedXML 0.105.0's own
// `new XLWorkbook(stream)`) hits the exact same unresolvable-generic-
// parameter shape activator.go documents, but through a different route
// than a class-level generic (AttributeMetadata.Builder<TSimpleType>):
// here T genuinely IS a generic METHOD parameter (`where T :
// OpenXmlPartRootElement, new()`), the shape ir.Call.MethodGenericArgs
// exists for — but LoadDomTree<T>'s own body does `new T()` INSIDE
// itself (see real decompiled source below), and vmnet builds one
// shared IR body per generic method definition rather than specializing
// it per closed instantiation (see activator.go's own doc comment). So
// while the OUTER call site that invokes LoadDomTree<T> (e.g.
// SharedStringTablePart.SharedStringTable's own `LoadDomTree
// <SharedStringTable>()`, confirmed via temporary tracing:
// `DEBUG call: target=...OpenXmlPart::LoadDomTree
// caller=...SharedStringTablePart::get_SharedStringTable
// methodGenericArgs=[...SharedStringTable]`) resolves T correctly, the
// INNER `new T()` call inside LoadDomTree<T>'s own shared body sees only
// the method's own still-open MVAR (methodGenericArgs=[] — same trace,
// next line: `target=System.Activator::CreateInstance
// caller=...OpenXmlPart::LoadDomTree methodGenericArgs=[]`).
//
// Real decompiled source (DocumentFormat.OpenXml.Framework 9.0.0,
// /tmp/openxmlfw_ns20/DocumentFormat.OpenXml.Packaging/OpenXmlPart.cs:
// 464-501):
//
//	internal void LoadDomTree<T>() where T : OpenXmlPartRootElement, new()
//	{
//	    if (_isLoading) throw new InvalidOperationException(...);
//	    _isLoading = true;
//	    try {
//	        IPartRootEventsFeature partRootEventsFeature = Features.Get<IPartRootEventsFeature>();
//	        partRootEventsFeature?.OnChange(EventType.Creating, this);
//	        using Stream stream = GetStream(FileMode.OpenOrCreate, FileAccess.Read);
//	        if (stream.Length < 4) return;
//	        try {
//	            T val = new T { OpenXmlPart = this };
//	            if (val.LoadFromPart(this, stream)) {
//	                InternalRootElement = val;
//	                partRootEventsFeature?.OnChange(EventType.Created, this);
//	            }
//	        } catch (InvalidDataException innerException) {
//	            throw new InvalidDataException(ExceptionMessages.CannotLoadRootElement, innerException);
//	        }
//	    } finally { _isLoading = false; }
//	}
//
// Registered here (genericMachineRegistry, the same mechanism
// features.go's FeatureCollectionBase.Get<TFeature>/GetRequired<TFeature>
// use) rather than as a nativeCctorOverrides seed, because this is a real
// method call with a real, load-bearing return value (the loaded DOM
// tree itself) on the actual document-READ data path — unlike
// AttributeMetadata.Builder<TSimpleType>'s _defaultValidator (attribute_
// metadata.go), there is no safe "doesn't matter which answer" shortcut
// here: this IS the part-loading logic ClosedXML depends on to read real
// cell/shared-string/style data. So the whole method is reimplemented
// natively instead, calling back into the real interpreted bodies for
// every sub-operation that isn't itself part of the unresolvable-generic
// shape (GetStream, LoadFromPart, the InternalRootElement/OpenXmlPart
// property setters — all plain or virtually-dispatched calls vmnet
// already runs correctly), and using methodGenericArgs[0] — resolved
// correctly THIS time, because genericMachineRegistry's lookup happens at
// LoadDomTree's own OUTER call site, exactly where T is still a concrete
// closed type — only for the one `new T()` construction the shared-IR
// limitation actually blocks.
//
// Two real behaviors are deliberately NOT reproduced, both confirmed
// safe by grepping the full decompiled DocumentFormat.OpenXml.Framework
// surface:
//
//   - _isLoading reentrancy guard: never fires in vmnet's own call
//     shape (LoadDomTree is only ever reached once per part, from this
//     one native override, never recursively), so the guard has nothing
//     to guard against here; not worth adding instance-field bookkeeping
//     for a codepath that can't occur.
//   - `partRootEventsFeature?.OnChange(...)`: IPartRootEventsFeature is
//     only ever populated by PartRootEventExtensions.AddPartRootEvents
//     Feature (DocumentFormat.OpenXml.Features/PartRootEventExtensions.cs)
//     — grepping both the OpenXml.Framework and the main OpenXml SDK's
//     entire decompiled source for any call to AddPartRootEventsFeature
//     turns up none at all, so Features.Get<IPartRootEventsFeature>()
//     always returns null on every real call path here; the null-
//     conditional call is therefore a guaranteed no-op to skip, not a
//     silently-dropped side effect.
func init() {
	genericMachineRegistry["DocumentFormat.OpenXml.Packaging.OpenXmlPart::LoadDomTree"] = openXmlPartLoadDomTree
}

// FileMode.OpenOrCreate and FileAccess.Read's real System.IO enum
// values (standard BCL constants; vmnet has no TypeDef for BCL-only
// System.IO enums to resolve a symbolic name against — same posture
// examples/npoi-demo and this demo's own main.go already take for
// ClosedXML.Excel.XLDataType).
const (
	fileModeOpenOrCreate = 4
	fileAccessRead       = 1
)

func openXmlPartLoadDomTree(m *Machine, args []runtime.Value, methodGenericArgs []string, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: OpenXmlPart.LoadDomTree<T> called without a receiver")
	}
	if len(methodGenericArgs) < 1 || methodGenericArgs[0] == "" {
		return runtime.Value{}, fmt.Errorf("interpreter: OpenXmlPart.LoadDomTree<T>: T could not be resolved (generic method chaining through its own open type parameter)")
	}
	recv := args[0]

	stream, _, err := m.call("DocumentFormat.OpenXml.Packaging.OpenXmlPart::GetStream", []runtime.Value{recv, runtime.Int32(fileModeOpenOrCreate), runtime.Int32(fileAccessRead)}, true, depth, instrCount, nil, nil)
	if err != nil {
		return runtime.Value{}, err
	}

	length, _, err := m.call("System.IO.Stream::get_Length", []runtime.Value{stream}, true, depth, instrCount, nil, nil)
	if err != nil {
		return runtime.Value{}, err
	}
	if length.Kind == runtime.KindI8 && length.I8 < 4 {
		return runtime.Value{}, nil
	}

	val, err := m.newObj(newObjArgs{
		TypeFullName: methodGenericArgs[0],
		CtorFullName: methodGenericArgs[0] + "::.ctor",
	}, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}

	if _, _, err := m.call("DocumentFormat.OpenXml.OpenXmlPartRootElement::set_OpenXmlPart", []runtime.Value{val, recv}, true, depth, instrCount, nil, nil); err != nil {
		return runtime.Value{}, err
	}

	loaded, _, err := m.call("DocumentFormat.OpenXml.OpenXmlPartRootElement::LoadFromPart", []runtime.Value{val, recv, stream}, true, depth, instrCount, nil, nil)
	if err != nil {
		return runtime.Value{}, err
	}
	if loaded.Kind == runtime.KindI4 && loaded.I4 != 0 {
		if _, _, err := m.call("DocumentFormat.OpenXml.Packaging.OpenXmlPart::set_InternalRootElement", []runtime.Value{recv, val}, true, depth, instrCount, nil, nil); err != nil {
			return runtime.Value{}, err
		}
	}
	return runtime.Value{}, nil
}
