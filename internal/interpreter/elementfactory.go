package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// DocumentFormat.OpenXml.Framework.Metadata.ElementMetadata.Builder.
// AddChild<T>() (Fase 3.41, the third real call site found reading a real
// .xlsx through ClosedXML 0.105.0's `new XLWorkbook(stream)` — after
// AttributeMetadata.Builder<TSimpleType> (attribute_metadata.go) and
// OpenXmlPart.LoadDomTree<T> (loaddomtree.go)) hits the same
// unresolvable-generic-parameter shape, but TWO chained levels deep
// rather than one.
//
// Real decompiled source (DocumentFormat.OpenXml.Framework 9.0.0,
// /tmp/openxmlfw_ns20/DocumentFormat.OpenXml.Framework.Metadata/
// ElementMetadata.cs:15-20,81-88 and .../ElementFactory.cs:22-31):
//
//	private class KnownChild<T> : IMetadataBuilder<ElementFactory> where T : OpenXmlElement, new()
//	{
//	    public ElementFactory Build() => ElementFactory.Create<T>();
//	}
//	public void AddChild<T>() where T : OpenXmlElement, new()
//	{
//	    _children ??= new HashSet<IMetadataBuilder<ElementFactory>>();
//	    _children.Add(new KnownChild<T>());
//	}
//	// ElementFactory.cs:
//	public static ElementFactory Create<T>() where T : OpenXmlElement, new()
//	{
//	    T val = new T();
//	    return new ElementFactory(typeof(T), val.Metadata.QName, () => new T());
//	}
//
// AddChild<T>'s OWN call site is always genuinely concrete (confirmed via
// temporary tracing: every real call comes from some concrete element's
// own `ConfigureMetadata` override, e.g.
// `DocumentFormat.OpenXml.Spreadsheet.SharedStringTable::ConfigureMetadata`
// calling `builder.AddChild<SharedStringItem>()` — never itself generic),
// so ir.Call.MethodGenericArgs resolves T correctly right here. But
// AddChild<T>'s body only ever constructs `new KnownChild<T>()` — a
// generic CLASS instantiation vmnet's runtime.Type model has no notion of
// "closed identity" for (see attribute_metadata.go's doc comment) — so by
// the time `KnownChild<T>.Build()` and `ElementFactory.Create<T>()` run
// later (from ElementMetadata.Builder.Build()'s own
// `_children.Select(c => c.Build())`, sharing ONE IR body across every T),
// the concrete T resolved here has nowhere left to be read from: both
// hit the exact same "T could not be resolved" wall activator.go
// documents.
//
// Unlike AttributeMetadata.Builder<TSimpleType>'s _defaultValidator
// (validation-only, safe to seed with a fixed placeholder), this IS the
// real document-read data path: Children feeds ElementMetadata.Children,
// consulted by OpenXmlCompositeElement.Populate/OpenXmlElement.
// CreateElement (confirmed via the same tracing: `DocumentFormat.OpenXml.
// OpenXmlCompositeElement::Populate` and `...OpenXmlElement::CreateElement`
// both call `ElementFactory` right after) to construct the ACTUAL typed
// child elements (Cell, Row, SharedStringItem, ...) while parsing real
// XML — silently seeding a wrong-but-harmless placeholder here would
// silently corrupt the parsed document tree instead.
//
// So the fix bypasses the whole KnownChild<T>/Build()/ElementFactory.
// Create<T>() chain at its one genuinely non-chained entry point instead:
// AddChild<T> is intercepted here (genericMachineRegistry, same mechanism
// LoadDomTree uses), and — since T is real and concrete right here —
// stashes it on a small vmnet-only native placeholder object (never a
// real KnownChild<T>, which vmnet couldn't tag with a closed T anyway)
// added to the real `_children` HashSet in its place. That placeholder's
// own "Build" (dispatched by receiverTypeName + calls.go's ancestor walk,
// the exact same interface-dispatch path IEnumerable`1::GetEnumerator
// already goes through for a HashSet<T> receiver — see the NativeTypeName
// entry added for *bcl.nativeHashSet in system_object.go, Fase 3.41) and
// the delegate the reconstructed ElementFactory itself carries both read
// back that stashed concrete T to do the REAL equivalent of Create<T>():
// build a genuine `new T()`, its real Metadata.QName, and a real working
// factory delegate — nothing about the actual parsed document differs
// from what interpreting the original generic chain correctly would have
// produced, only how vmnet gets there.
func init() {
	genericMachineRegistry["DocumentFormat.OpenXml.Framework.Metadata.ElementMetadata+Builder::AddChild"] = elementMetadataBuilderAddChild
	machineRegistry["VmnetInternal.KnownChildBuilder::Build"] = knownChildBuilderBuild
	machineRegistry["VmnetInternal.ElementFactoryThunk::Invoke"] = elementFactoryThunkInvoke
}

// nativeKnownChildBuilder replaces a real `new KnownChild<T>()` instance
// (see this file's own top doc comment for why vmnet can't construct a
// real one with T still attached) — elementTypeName is the one piece of
// per-instance state a real KnownChild<T> would have carried in its own
// closed generic identity instead.
type nativeKnownChildBuilder struct {
	elementTypeName string
}

// nativeElementFactoryThunk replaces the real `() => new T()` closure
// ElementFactory.Create<T>() builds — same reasoning as
// nativeKnownChildBuilder, for the delegate ElementFactory.Create()
// (ElementFactory.cs:22-25) invokes on every later real element
// construction.
type nativeElementFactoryThunk struct {
	elementTypeName string
}

func elementMetadataBuilderAddChild(m *Machine, args []runtime.Value, methodGenericArgs []string, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindObject || args[0].Obj == nil || args[0].Obj.Type == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: ElementMetadata.Builder.AddChild<T> called without a receiver")
	}
	if len(methodGenericArgs) < 1 || methodGenericArgs[0] == "" {
		return runtime.Value{}, fmt.Errorf("interpreter: ElementMetadata.Builder.AddChild<T>: T could not be resolved (generic method chaining through its own open type parameter)")
	}
	recv := args[0]
	idx := recv.Obj.Type.FieldIndex("_children")
	if idx < 0 {
		// The real TypeDef's shape changed (a different OpenXml SDK
		// version) — nothing safe to do but report it rather than
		// silently dropping a real child registration.
		return runtime.Value{}, fmt.Errorf("interpreter: ElementMetadata.Builder has no _children field (SDK shape changed)")
	}
	if recv.Obj.Fields[idx].Kind == runtime.KindNull {
		hs, err := m.newObj(newObjArgs{
			TypeFullName: "System.Collections.Generic.HashSet`1",
			CtorFullName: "System.Collections.Generic.HashSet`1::.ctor",
		}, depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		recv.Obj.Fields[idx] = hs
	}
	child := runtime.ObjRef(&runtime.Object{Native: &nativeKnownChildBuilder{elementTypeName: methodGenericArgs[0]}})
	if _, _, err := m.call("System.Collections.Generic.HashSet`1::Add", []runtime.Value{recv.Obj.Fields[idx], child}, true, depth, instrCount, nil, nil); err != nil {
		return runtime.Value{}, err
	}
	return runtime.Value{}, nil
}

func knownChildBuilderBuild(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: KnownChildBuilder.Build called without a receiver")
	}
	kc, ok := args[0].Obj.Native.(*nativeKnownChildBuilder)
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: KnownChildBuilder.Build: receiver is not a KnownChildBuilder")
	}
	return buildElementFactory(m, kc.elementTypeName, depth, instrCount)
}

// buildElementFactory replicates ElementFactory.Create<T>()'s real body
// (ElementFactory.cs:22-31) for a concrete, already-resolved
// elementTypeName: construct a real `new T()` to read its real
// Metadata.QName, then a real ElementFactory instance wrapping both plus
// a working factory delegate (nativeElementFactoryThunk) that itself
// constructs a fresh `new T()` on every later invocation, exactly like
// the real closure does.
func buildElementFactory(m *Machine, elementTypeName string, depth int, instrCount *int64) (runtime.Value, error) {
	val, err := m.newObj(newObjArgs{TypeFullName: elementTypeName, CtorFullName: elementTypeName + "::.ctor"}, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	metadata, _, err := m.call("DocumentFormat.OpenXml.OpenXmlElement::get_Metadata", []runtime.Value{val}, true, depth, instrCount, nil, nil)
	if err != nil {
		return runtime.Value{}, err
	}
	qname, _, err := m.call("DocumentFormat.OpenXml.Framework.Metadata.IElementMetadata::get_QName", []runtime.Value{metadata}, true, depth, instrCount, nil, nil)
	if err != nil {
		return runtime.Value{}, err
	}
	typeVal := bcl.NewTypeValue(elementTypeName)
	thunk := runtime.BindDelegate(
		runtime.ObjRef(&runtime.Object{Native: &nativeElementFactoryThunk{elementTypeName: elementTypeName}}),
		runtime.Func{FullName: "VmnetInternal.ElementFactoryThunk::Invoke"},
		"System.Func`1",
	)
	// ElementFactory's real ctor takes qname as `in OpenXmlQualifiedName`
	// (ElementFactory.cs:15) — a readonly-ref struct parameter, so its
	// real IL (`ldind`-style read from the incoming address) expects a
	// managed pointer to a live OpenXmlQualifiedName, not the struct
	// value directly (same convention newObj's own value-type `this`
	// already follows just above in calls.go). Passing qname bare here
	// crashed with "dereferencing a null managed pointer (ldind)" inside
	// ElementFactory::.ctor — found running this fix for the first time.
	return m.newObj(newObjArgs{
		TypeFullName: "DocumentFormat.OpenXml.Framework.Metadata.ElementFactory",
		CtorFullName: "DocumentFormat.OpenXml.Framework.Metadata.ElementFactory::.ctor",
		Args:         []runtime.Value{typeVal, runtime.RefTo(&qname), thunk},
	}, depth, instrCount)
}

// interpreterNativeTypeName is typecheck.go's receiverTypeName's own
// extension point for native-backed types declared in THIS package
// (rather than bcl's) — needed so calls.go's virtual-dispatch ancestor
// walk can find "VmnetInternal.KnownChildBuilder::Build" for a
// nativeKnownChildBuilder receiver reached through the real
// `IMetadataBuilder<ElementFactory>::Build()` interface-declared call
// site (the same "receiver's real type wasn't reachable" gap
// *bcl.nativeHashSet hit for IEnumerable`1::GetEnumerator, fixed in
// system_object.go).
func interpreterNativeTypeName(native any) (string, bool) {
	switch native.(type) {
	case *nativeKnownChildBuilder:
		return "VmnetInternal.KnownChildBuilder", true
	case *nativeElementFactoryThunk:
		return "VmnetInternal.ElementFactoryThunk", true
	case *funcComparer:
		// Comparer<T>.Create(Comparison<T>)'s own wrapper (comparer.go) —
		// needed so compareFunc/equalsFunc's generic "dispatch any
		// KindObject comparer argument through receiverTypeName" path
		// (Fase 3.44) reaches comparerCompareMachine (registered against
		// this exact name) instead of silently falling back to natural
		// ordering, ignoring a caller's real Comparison<T> the moment
		// Array.Sort/List<T>.Sort/OrderBy stopped special-casing
		// *funcComparer inline the way the pre-Fase-3.44 arraySort used to.
		return "System.Collections.Generic.Comparer`1", true
	default:
		return "", false
	}
}

func elementFactoryThunkInvoke(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: ElementFactoryThunk.Invoke called without a receiver")
	}
	th, ok := args[0].Obj.Native.(*nativeElementFactoryThunk)
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: ElementFactoryThunk.Invoke: receiver is not an ElementFactoryThunk")
	}
	return m.newObj(newObjArgs{TypeFullName: th.elementTypeName, CtorFullName: th.elementTypeName + "::.ctor"}, depth, instrCount)
}
