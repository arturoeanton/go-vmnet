package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// Resolver looks up another method in the same assembly by its
// "Namespace.Type::Method" full name, for calls that aren't BCL natives.
type Resolver func(fullName string) (*runtime.Method, error)

// TypeResolver looks up a type's field layout by its "Namespace.Type" full
// name, for newobj/ldfld/stfld.
type TypeResolver func(fullName string) (*runtime.Type, error)

// ExplicitImplResolver finds the real method name a concrete type uses to
// explicitly implement an interface method (Fase 3.13) — e.g. a `yield
// return` iterator's compiler-generated class implements
// IEnumerable`1::GetEnumerator not as a plain "GetEnumerator" method but
// as "System.Collections.Generic.IEnumerable<System.Int32>.GetEnumerator"
// (the mangled name explicit interface implementation requires), which a
// plain concreteType+"::"+method lookup can never find. Returns ok=false
// when the type implements the interface method under its plain name (or
// doesn't implement it at all) — the ordinary receiverTypeName fallback
// in Machine.call already covers that case.
type ExplicitImplResolver func(concreteTypeFullName, interfaceFullName, methodName string) (implMethodName string, ok bool)

// EnumResolver reads a plugin-declared enum's members (name, real
// constant value) in declaration order — e.g. ["Red","Yellow","Green"],
// [0,1,2] for `enum TrafficLight` (Fase 3.26, System.Enum.GetValues/
// GetNames/IsDefined/ToObject). ok=false when fullName doesn't name a
// resolvable plugin enum (a BCL-only enum like System.DayOfWeek, or any
// other unresolvable name) — vmnet has no metadata at all for BCL enums
// it doesn't declare itself.
type EnumResolver func(fullName string) (names []string, values []int64, ok bool)

// call dispatches fullName as either a BCL native or an interpreted
// method. virtual must be true only for an actual `callvirt` site — the
// interface/virtual-dispatch fallback below must never apply to a plain
// `call` (a base-class constructor chaining via `base(...)`, a
// non-virtual/sealed/private method, `newobj`'s own constructor
// invocation): those name an exact target on purpose, and redirecting by
// the receiver's concrete type would, for a constructor specifically,
// just re-invoke the very constructor currently running — an infinite
// recursion caught by a real example (a plugin exception subclass
// chaining `: base(message)`) while building this fallback, not a
// hypothetical edge case.
func (m *Machine) call(fullName string, args []runtime.Value, virtual bool, depth int, instrCount *int64) (runtime.Value, bool, error) {
	if v, hasReturn, err, found := m.tryCall(fullName, args, depth, instrCount); found {
		return v, hasReturn, err
	}

	// Interface-typed call site fallback (Fase 3.13): fullName is baked in
	// at compile time from the *declared* type of the call site (see
	// resolveMemberRefClassName in internal/ir/builder.go) — for
	// `IEnumerable<T> xs = list; foreach (var x in xs)` that's literally
	// "System.Collections.Generic.IEnumerable`1::GetEnumerator", never the
	// receiver's actual concrete type. vmnet has no vtable to dispatch a
	// virtual/interface call through, so when the declared name resolves
	// to nothing, retry once against the receiver's real concrete type
	// instead. This covers both BCL collections accessed through an
	// interface-typed local (List<T> called as IEnumerable<T>) and plugin
	// classes implementing an interface explicitly (a hand-written
	// IEnumerator, a custom IEquatable<T>, ...) uniformly, since both
	// paths go through the same two lookups in tryCall.
	if virtual && len(args) > 0 {
		if concrete, ok := receiverTypeName(args[0]); ok {
			if class, method, ok := splitCallName(fullName); ok {
				if retryName := concrete + "::" + method; retryName != fullName {
					if v, hasReturn, err, found := m.tryCall(retryName, args, depth, instrCount); found {
						return v, hasReturn, err
					}
				}
				// The plain name didn't resolve either — the concrete type
				// may still implement the interface method, just under the
				// mangled name explicit interface implementation requires
				// (a `yield return` iterator's GetEnumerator/Current, most
				// commonly). See ExplicitImplResolver's doc comment.
				if m.ResolveExplicitImpl != nil {
					if implMethod, ok := m.ResolveExplicitImpl(concrete, class, method); ok {
						if v, hasReturn, err, found := m.tryCall(concrete+"::"+implMethod, args, depth, instrCount); found {
							return v, hasReturn, err
						}
					}
				}
			}
		}
	}

	return runtime.Value{}, false, fmt.Errorf("interpreter: unsupported BCL method %q (no native registered)", fullName)
}

// tryCall attempts fullName as either a BCL native or an interpreted
// method, reporting via found whether the name resolved at all — as
// opposed to resolving but then failing at runtime (err), which the
// caller must propagate rather than silently swallow into a fallback
// retry.
func (m *Machine) tryCall(fullName string, args []runtime.Value, depth int, instrCount *int64) (v runtime.Value, hasReturn bool, err error, found bool) {
	if native, hr, ok := bcl.Lookup(fullName); ok {
		v, err = native(args)
		return v, hr, err, true
	}
	// Machine-aware natives (LINQ, Fase 3.15; Type::IsAssignableFrom,
	// Fase 3.16): need Machine access (invoking a delegate argument,
	// driving an arbitrary source's real iteration protocol, walking the
	// real type hierarchy), none of which a plain bcl.Native has — see
	// linq.go/reflection.go.
	if native, ok := machineRegistry[fullName]; ok {
		v, err = native(m, args, depth, instrCount)
		return v, true, err, true
	}
	if m.Resolve == nil {
		return runtime.Value{}, false, nil, false
	}
	method, rerr := m.Resolve(fullName)
	if rerr != nil {
		return runtime.Value{}, false, nil, false
	}
	v, err = m.invoke(method, args, depth+1, instrCount)
	return v, method.HasReturn, err, true
}

// invokeFunc calls a delegate: fn's captured receiver (nil for a static
// target), if any, is prepended to args, then dispatched exactly like any
// other call (BCL native or interpreted local method) — see
// runtime.Func's doc comment for why closures need nothing beyond this.
func (m *Machine) invokeFunc(fn *runtime.Func, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, bool, error) {
	if fn == nil {
		return runtime.Value{}, false, &runtime.ManagedException{TypeName: "System.NullReferenceException", Message: "delegate is null"}
	}
	result, hasReturn, err := m.invokeFuncTarget(fn, args, depth, instrCount)
	if err != nil {
		return runtime.Value{}, false, err
	}
	// A multicast delegate (built by Delegate.Combine, Fase 3.24) runs
	// every remaining target too, keeping only the last one's result —
	// matching real MulticastDelegate.Invoke.
	for _, next := range fn.Chain {
		result, hasReturn, err = m.invokeFuncTarget(next, args, depth, instrCount)
		if err != nil {
			return runtime.Value{}, false, err
		}
	}
	return result, hasReturn, nil
}

func (m *Machine) invokeFuncTarget(fn *runtime.Func, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, bool, error) {
	callArgs := args
	if fn.Receiver != nil {
		callArgs = append([]runtime.Value{*fn.Receiver}, args...)
	}
	// fn.FullName is already the exact bound target (resolved at ldftn
	// time, Fase 3.9) — never a declared-interface name needing
	// redirection, so this is never a candidate for the virtual-dispatch
	// fallback either.
	return m.call(fn.FullName, callArgs, false, depth, instrCount)
}

// newObj implements the ir.NewObj instruction: allocate (native value
// type, native reference type, or plain assembly type) and, for
// non-fully-native cases, run the constructor.
func (m *Machine) newObj(in newObjArgs, depth int, instrCount *int64) (runtime.Value, error) {
	// A delegate constructor (any delegate type at all — see
	// runtime.Func's doc comment) always has exactly this shape:
	// (receiver-or-null, unbound-function-from-ldftn). Detected
	// structurally rather than by TypeFullName, which is unbounded (a
	// custom `delegate` declaration, or a foreign BCL one like Action<T>
	// with no TypeDef in the loaded assembly — Fase 3.9).
	if len(in.Args) == 2 && in.Args[1].Kind == runtime.KindFunc {
		return runtime.BindDelegate(in.Args[0], *in.Args[1].Func), nil
	}

	// System.String is a reference type in real .NET, but vmnet
	// represents every string as a plain KindString value, not a
	// KindObject (every other native ctor below returns via
	// runtime.ObjRef, which would be wrong here — nothing downstream
	// treats a string as an Object). `new string(...)` needs its own
	// path for exactly that reason.
	if in.TypeFullName == "System.String" {
		return bcl.NewStringFromCtor(in.Args)
	}

	if vtCtor, ok := bcl.LookupValueTypeCtor(in.TypeFullName); ok {
		s, err := vtCtor(in.Args)
		if err != nil {
			return runtime.Value{}, err
		}
		return runtime.StructVal(s), nil
	}

	if ctor, ok := bcl.LookupCtor(in.TypeFullName); ok {
		obj, err := ctor(in.Args)
		if err != nil {
			return runtime.Value{}, err
		}
		return runtime.ObjRef(obj), nil
	}

	if m.ResolveType == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: unsupported type %q (no native constructor and no type resolver)", in.TypeFullName)
	}
	typ, err := m.ResolveType(in.TypeFullName)
	if err != nil {
		return runtime.Value{}, err
	}

	// A value type's `newobj` allocates temp storage, calls its .ctor with
	// `this` as a managed pointer to that storage (like any struct instance
	// method — see fieldSlot in eval.go), then pushes the value itself
	// rather than a heap reference (spec §III.4.21).
	if typ.IsValueType {
		objVal := runtime.StructVal(runtime.NewStruct(typ))
		ctorArgs := make([]runtime.Value, 0, len(in.Args)+1)
		ctorArgs = append(ctorArgs, runtime.RefTo(&objVal))
		ctorArgs = append(ctorArgs, in.Args...)
		if _, _, err := m.call(in.CtorFullName, ctorArgs, false, depth, instrCount); err != nil {
			return runtime.Value{}, err
		}
		return objVal, nil
	}

	fields := make([]runtime.Value, len(typ.Fields))
	copy(fields, typ.FieldDefaults)
	obj := &runtime.Object{Type: typ, Fields: fields}
	objVal := runtime.ObjRef(obj)

	ctorArgs := make([]runtime.Value, 0, len(in.Args)+1)
	ctorArgs = append(ctorArgs, objVal)
	ctorArgs = append(ctorArgs, in.Args...)
	if _, _, err := m.call(in.CtorFullName, ctorArgs, false, depth, instrCount); err != nil {
		return runtime.Value{}, err
	}
	return objVal, nil
}

type newObjArgs struct {
	TypeFullName string
	CtorFullName string
	Args         []runtime.Value
}
