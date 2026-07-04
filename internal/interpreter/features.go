package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// DocumentFormat.OpenXml.Packaging's own "typed feature bag" pattern
// (FeatureCollectionBase.Get<TFeature>/FeatureExtensions.GetRequired
// <TFeature>) is exactly the case ir.Call.MethodGenericArgs exists for
// (Fase 3.40): both real methods do `this[typeof(TFeature)]`, and
// TFeature is a generic METHOD parameter — the same compiled body runs
// for every different call site, so it can't be resolved once at
// IR-build time the way a generic CLASS parameter can. Both are
// registered here directly (rather than relying on GetRequired's own
// interpreted body calling Get<TFeature>() internally) because
// GetRequired<TFeature>()'s own call to Get<TFeature>() passes ITS OWN
// still-open method parameter as the instantiation argument (an MVAR,
// resolving to "" — see resolveCallTarget's TableMethodSpec case) rather
// than a concrete closed type; only the ORIGINAL outer call site
// (`features.GetRequired<IDisposableFeature>()`) actually knows the real
// answer, so GetRequired needs its own direct implementation rather than
// forwarding through another generic call vmnet can't propagate the
// argument through.
func init() {
	genericMachineRegistry["DocumentFormat.OpenXml.Packaging.FeatureCollectionBase::Get"] = featureCollectionGet
	genericMachineRegistry["DocumentFormat.OpenXml.Features.FeatureExtensions::GetRequired"] = featureCollectionGetRequired
	genericMachineRegistry["DocumentFormat.OpenXml.Packaging.FeatureCollectionBase::Set"] = featureCollectionSet
}

// featureCollectionSet mirrors featureCollectionGet for the write side —
// `public void Set<TFeature>(TFeature? instance) { this[typeof(TFeature)]
// = instance; }` has the exact same typeof(TFeature)-on-a-method-generic-
// parameter shape (Fase 3.40). Without this, every real
// `features.Set<IPackageFeature>(this)`-style registration (Package
// FeatureBase.Register, the actual place IPackageFeature/
// IRelationshipFilterFeature/IPackageStreamFeature/... get wired into a
// package's own feature collection at all) stored under an empty-name
// Type key instead, so every later Get<TFeature>() for that exact
// feature silently found nothing, regardless of this fix.
func featureCollectionSet(m *Machine, args []runtime.Value, methodGenericArgs []string, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 2 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: FeatureCollectionBase.Set<T> called without a receiver")
	}
	if len(methodGenericArgs) < 1 || methodGenericArgs[0] == "" {
		return runtime.Value{}, nil
	}
	typeVal := bcl.NewTypeValue(methodGenericArgs[0])
	_, _, err := m.call("DocumentFormat.OpenXml.Packaging.FeatureCollectionBase::set_Item", []runtime.Value{args[0], typeVal, args[1]}, true, depth, instrCount, nil, nil)
	return runtime.Value{}, err
}

func featureCollectionGetImpl(m *Machine, args []runtime.Value, methodGenericArgs []string, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return runtime.Null(), nil
	}
	if len(methodGenericArgs) < 1 || methodGenericArgs[0] == "" {
		return runtime.Null(), nil
	}
	typeVal := bcl.NewTypeValue(methodGenericArgs[0])
	v, _, err := m.call("DocumentFormat.OpenXml.Packaging.FeatureCollectionBase::get_Item", []runtime.Value{args[0], typeVal}, true, depth, instrCount, nil, nil)
	if err != nil || v.Kind != runtime.KindNull {
		return v, err
	}
	// Real FeatureCollectionBase's own indexer (see the real decompiled
	// source: `this[Type key]`) falls through the dictionary lookup to
	// "if key.IsAssignableFrom(GetType()) return this" — a feature
	// collection commonly implements a feature interface directly on
	// ITSELF rather than registering a separate instance via Set<T> (Fase
	// 3.40, found via a real, load-bearing case:
	// TypedPackageFeatureCollection<TDocumentType,TMainPart> implements
	// IDocumentTypeFeature<TDocumentType> as an explicit interface member
	// with no Set<T> call anywhere — WordprocessingDocument.DocumentType
	// depends on this fallback for every document it creates or opens).
	// vmnet's generics are type-argument-blind (erasure — see this
	// package's other Fase 3.40 comments), so the comparison uses the
	// OPEN generic name on both sides, same posture as everywhere else
	// generic interfaces are matched structurally instead of by exact
	// closed instantiation.
	if m.isAssignableTo(args[0], bcl.GenericOpenName(methodGenericArgs[0])) {
		return args[0], nil
	}
	return v, nil
}

func featureCollectionGet(m *Machine, args []runtime.Value, methodGenericArgs []string, depth int, instrCount *int64) (runtime.Value, error) {
	return featureCollectionGetImpl(m, args, methodGenericArgs, depth, instrCount)
}

func featureCollectionGetRequired(m *Machine, args []runtime.Value, methodGenericArgs []string, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind == runtime.KindNull {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentNullException", Message: "features"}
	}
	v, err := featureCollectionGetImpl(m, args, methodGenericArgs, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	if v.Kind != runtime.KindNull {
		return v, nil
	}
	featureName := "requested feature"
	if len(methodGenericArgs) > 0 && methodGenericArgs[0] != "" {
		featureName = methodGenericArgs[0]
	}
	return runtime.Value{}, &runtime.ManagedException{
		TypeName: "System.NotSupportedException",
		Message:  fmt.Sprintf("Feature %s is not available in this collection.", featureName),
	}
}
