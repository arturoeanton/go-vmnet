package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// System.Activator.CreateInstance<T>() is the real, compiler-emitted
// shape of a `where T : new()` generic constraint's `new T()` — found
// pervasively in DocumentFormat.OpenXml.Framework's real IL (`call !!0
// System.Activator::CreateInstance<!!T>()`, e.g. building a fresh
// element/part instance generically). T is a generic METHOD parameter
// at these call sites, exactly the shape ir.Call.MethodGenericArgs
// exists for (see features.go's identical reasoning for
// FeatureCollectionBase::Get<TFeature>) — Activator itself has no real
// interpreted body to run at all (it's a CoreLib intrinsic), so this
// constructs T directly via the same newObj machinery a real `newobj
// T::.ctor()` would use.
func init() {
	genericMachineRegistry["System.Activator::CreateInstance"] = activatorCreateInstance
}

func activatorCreateInstance(m *Machine, args []runtime.Value, methodGenericArgs []string, depth int, instrCount *int64) (runtime.Value, error) {
	if len(methodGenericArgs) < 1 || methodGenericArgs[0] == "" {
		// The enclosing generic method's own still-open type parameter
		// (an MVAR referencing the CALLER's own T, e.g. a generic method
		// calling Activator.CreateInstance<T>() with its own unresolved
		// T) — ir.Call.MethodGenericArgs only resolves a call site's
		// argument when it's already a concrete closed type at IR-build
		// time (see that field's own doc comment); a chained still-open
		// reference resolves to "" here, the same documented limitation
		// FeatureExtensions.GetRequired<TFeature>() hits calling its own
		// Get<TFeature>() forward.
		return runtime.Value{}, fmt.Errorf("interpreter: Activator.CreateInstance<T>: T could not be resolved (generic method chaining through its own open type parameter)")
	}
	return m.newObj(newObjArgs{
		TypeFullName: methodGenericArgs[0],
		CtorFullName: methodGenericArgs[0] + "::.ctor",
		Args:         nil,
	}, depth, instrCount)
}
