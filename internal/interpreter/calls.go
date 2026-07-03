package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// Resolver looks up another method in the same assembly by its
// "Namespace.Type::Method" full name, for calls that aren't BCL natives.
// args are the actual call-site arguments (receiver included, for an
// instance call) — needed since Fase 3.27 to disambiguate a real
// overload set (same name, different signature; FullName alone can't
// tell two overloads apart), not to invoke anything here.
type Resolver func(fullName string, args []runtime.Value) (*runtime.Method, error)

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

// FieldBytesResolver returns a field's compiler-embedded initial-value
// blob, if it has one (Fase 3.27) — see runtime.Resolvers.ResolveFieldBytes.
type FieldBytesResolver func(typeFullName, fieldName string) ([]byte, bool)

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
	var lastResolveErr error

	// Real virtual dispatch (Fase 3.27): a `callvirt` site's fullName is
	// baked in at compile time from the call's *declared* type (see
	// resolveMemberRefClassName, internal/ir/builder.go) — e.g. `Node n =
	// someLiteral; n.GetChildNodes();` compiles as `callvirt Esprima.Ast.
	// Node::GetChildNodes()`, naming the BASE class, even when the
	// receiver's real type (Literal) has its own override. vmnet has no
	// real vtable, so a virtual call always tries the receiver's actual
	// concrete type FIRST — not just as a "declared name resolved to
	// nothing" fallback (Fase 3.13's original, narrower scope): a base
	// class's own method can easily exist AND resolve successfully (it's
	// a real, callable MethodDef) while still being the WRONG one to run,
	// when a more-derived override is what real semantics require. Found
	// the hard way running real Jint/Esprima: Node's own GetChildNodes()
	// deliberately throws NotImplementedException for exactly this
	// reason (it's meant to be a "you forgot to override me" guard, only
	// ever safe to reach when nothing more derived exists) — resolving
	// it directly by the declared name, before ever considering the
	// receiver's concrete type, hit that guard on every call.
	//
	// The concrete leaf type itself may not be the one that overrides —
	// e.g. Esprima's concrete node classes (Literal, Identifier, ...)
	// don't override GetChildNodes themselves; an INTERMEDIATE base
	// class between them and Node does. So this walks the full chain
	// from the concrete type up to (but not including — that's the
	// final fallback below) the declared type, trying each ancestor's
	// own plain-named override before giving up on this receiver
	// hierarchy entirely.
	if virtual && len(args) > 0 {
		if concrete, ok := receiverTypeName(args[0]); ok {
			if class, method, ok := splitCallName(fullName); ok {
				seen := map[string]bool{}
				for t, ok := concrete, true; ok && t != class && !seen[t]; t, ok = m.baseTypeOf(t) {
					seen[t] = true
					retryName := t + "::" + method
					v, hasReturn, err, found, rerr := m.tryCall(retryName, args, depth, instrCount)
					if found {
						return v, hasReturn, err
					}
					if rerr != nil {
						lastResolveErr = rerr
					}
				}
				// No override anywhere in the chain under its plain name —
				// it may still implement the interface/base method, just
				// under the mangled name explicit interface implementation
				// requires (a `yield return` iterator's GetEnumerator/
				// Current, most commonly). See ExplicitImplResolver's doc
				// comment. Only checked against the concrete leaf type,
				// matching real C# — interface dispatch resolves against
				// the receiver's most-derived type, never an intermediate
				// base.
				if m.ResolveExplicitImpl != nil {
					if implMethod, ok := m.ResolveExplicitImpl(concrete, class, method); ok {
						v, hasReturn, err, found, rerr := m.tryCall(concrete+"::"+implMethod, args, depth, instrCount)
						if found {
							return v, hasReturn, err
						}
						if rerr != nil {
							lastResolveErr = rerr
						}
					}
				}
			}
		}
	}

	// The receiver's concrete type has no override at all — use the
	// declared/base name directly (the overwhelmingly common case: most
	// calls aren't virtual, and most virtual calls have no override
	// anywhere in between the declared type and the receiver's own).
	v, hasReturn, err, found, resolveErr := m.tryCall(fullName, args, depth, instrCount)
	if found {
		return v, hasReturn, err
	}
	if resolveErr != nil {
		lastResolveErr = resolveErr
	}

	if lastResolveErr != nil {
		return runtime.Value{}, false, fmt.Errorf("interpreter: unsupported BCL method %q: %w", fullName, lastResolveErr)
	}
	return runtime.Value{}, false, fmt.Errorf("interpreter: unsupported BCL method %q (no native registered)", fullName)
}

// tryCall attempts fullName as either a BCL native or an interpreted
// method, reporting via found whether the name resolved at all — as
// opposed to resolving but then failing at runtime (err), which the
// caller must propagate rather than silently swallow into a fallback
// retry. resolveErr carries the Resolver's own error when found is
// false (Fase 3.27) — e.g. a multi-assembly dependency's method that
// genuinely exists but failed to build (a real bug worth surfacing),
// not just "no such name anywhere." Machine.call folds this into its
// final error message instead of the generic "no native registered"
// text once every fallback (interface dispatch, explicit impl, ...) is
// exhausted, so the actual root cause survives instead of being masked
// by the outermost call target's name.
func (m *Machine) tryCall(fullName string, args []runtime.Value, depth int, instrCount *int64) (v runtime.Value, hasReturn bool, err error, found bool, resolveErr error) {
	if native, hr, ok := bcl.Lookup(fullName); ok {
		v, err = native(args)
		return v, hr, err, true, nil
	}
	// Machine-aware natives (LINQ, Fase 3.15; Type::IsAssignableFrom,
	// Fase 3.16): need Machine access (invoking a delegate argument,
	// driving an arbitrary source's real iteration protocol, walking the
	// real type hierarchy), none of which a plain bcl.Native has — see
	// linq.go/reflection.go.
	if native, ok := machineRegistry[fullName]; ok {
		v, err = native(m, args, depth, instrCount)
		return v, true, err, true, nil
	}
	if m.Resolve == nil {
		return runtime.Value{}, false, nil, false, nil
	}
	method, rerr := m.Resolve(fullName, args)
	if rerr != nil {
		return runtime.Value{}, false, nil, false, rerr
	}
	v, err = m.invoke(method, args, depth+1, instrCount)
	return v, method.HasReturn, err, true, nil
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

	// Each default is Clone()'d, not just copied — see runtime.NewStruct's
	// doc comment for why a plain copy() lets every instance of a type
	// share one nested struct-typed field's storage (Fase 3.27).
	fields := make([]runtime.Value, len(typ.Fields))
	for i, def := range typ.FieldDefaults {
		fields[i] = def.Clone()
	}
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

// baseTypeOf returns typeFullName's immediate base class, if any — used by
// Machine.call's virtual-dispatch chain walk (Fase 3.27) to try every
// ancestor between the receiver's concrete type and a callvirt's declared
// type, not just the concrete leaf. Only plugin/assembly TypeDefs are
// walkable this way (m.ResolveType); a BCL-native-backed receiver
// (bcl.NativeTypeName) has no base chain to walk, which is fine — those
// are always leaves from vmnet's perspective.
func (m *Machine) baseTypeOf(typeFullName string) (string, bool) {
	if m.ResolveType == nil {
		return "", false
	}
	t, err := m.ResolveType(typeFullName)
	if err != nil || t.BaseTypeFullName == "" {
		return "", false
	}
	return t.BaseTypeFullName, true
}

type newObjArgs struct {
	TypeFullName string
	CtorFullName string
	Args         []runtime.Value
}
