package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// System.Activator.CreateInstance covers two entirely different real call
// shapes under one CIL method name: the GENERIC CreateInstance<T>() (the
// compiler-emitted form of a `where T : new()` constraint's `new T()`,
// found pervasively in DocumentFormat.OpenXml.Framework's real IL — `call
// !!0 System.Activator::CreateInstance<!!T>()`) and the ordinary
// non-generic CreateInstance(Type[, object[] args]) reflection overload
// (Fase 3.51, e.g. a plugin discovery registry that only has a
// System.Type value in hand, not a static T to constrain against) —
// genericMachineRegistry only tells them apart by whether
// methodGenericArgs is non-empty, since both compile to the exact same
// "System.Activator::CreateInstance" full name. Activator itself has no
// real interpreted body to run at all (it's a CoreLib intrinsic), so
// either shape constructs the target directly via the same newObj/
// Machine.New machinery a real `newobj T::.ctor(...)` would use.
func init() {
	genericMachineRegistry["System.Activator::CreateInstance"] = activatorCreateInstance
}

func activatorCreateInstance(m *Machine, args []runtime.Value, methodGenericArgs []string, depth int, instrCount *int64) (runtime.Value, error) {
	if len(methodGenericArgs) == 0 {
		return activatorCreateInstanceFromType(m, args, depth, instrCount)
	}
	if methodGenericArgs[0] == "" {
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
		// methodGenericArgs[0] is already a real, fully closed type name
		// (Activator.CreateInstance<T>() only ever reaches this branch
		// once T itself resolved to something concrete, see the ""
		// check above) — parsed directly off its own "[[...]]" suffix,
		// same posture Machine.New's own doc comment documents (Fase
		// 3.66).
		ClassGenericArgs: bcl.ClosedGenericArgs(methodGenericArgs[0]),
	}, depth, instrCount)
}

// activatorCreateInstanceFromType backs the non-generic
// Activator.CreateInstance(Type type[, object[] args]) reflection
// overload (methodGenericArgs is empty for this shape — it's an ordinary,
// non-generic static call). Constructs through Machine.New, the exact
// same overload-resolution-by-real-argument-Kind path ConstructorInfo.
// Invoke already uses, so this and `new T(...)` agree on which
// constructor overload actually runs. A trailing non-array argument (the
// (Type, bool nonPublic) or (Type, BindingFlags, ...) overloads) is
// accepted and ignored — every real caller found using those still just
// wants the default constructor, which an empty ctorArgs already gives.
func activatorCreateInstanceFromType(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: Activator.CreateInstance expects a Type argument")
	}
	typeFullName, ok := bcl.TypeFullNameOf(args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: Activator.CreateInstance: argument is not a Type")
	}
	var ctorArgs []runtime.Value
	if len(args) >= 2 && args[1].Kind == runtime.KindArray {
		var err error
		ctorArgs, err = bcl.ObjectArrayToValues(args[1])
		if err != nil {
			return runtime.Value{}, err
		}
	}
	return m.New(typeFullName, ctorArgs)
}
