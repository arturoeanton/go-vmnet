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
// tell two overloads apart), not to invoke anything here. paramTypeNames
// is the call site's own compile-time-resolved parameter type names
// (Fase 3.40, ir.Call.ParamTypeNames) — nil for a caller with no Call-
// level IR context at all (Machine.New/CallInstance, the public host
// API; a .cctor lookup). Used only as an optional, early exact-match
// preference before falling back to args' own Kind-based scoring — see
// pickMethodOverload. genericArgCount is the call site's own generic-
// method instantiation arity (Fase 3.41, len(ir.Call.MethodGenericArgs))
// — 0 for a plain, non-generic call — needed to hard-disambiguate a
// same-named, same-real-arity plain/generic method pair (e.g.
// DocumentFormat.OpenXml.OpenXmlElement's own Descendants()/
// Descendants<T>()), which a real arity/shape score alone can never
// tell apart (T contributes zero real parameters either way).
type Resolver func(fullName string, args []runtime.Value, paramTypeNames []string, genericArgCount int) (*runtime.Method, error)

// TypeResolver looks up a type's field layout by its "Namespace.Type" full
// name, for newobj/ldfld/stfld.
type TypeResolver func(fullName string) (*runtime.Type, error)

// ExplicitImplResolver finds the real, fully-qualified method name a
// concrete type (or one of its ancestors, Fase 3.40) uses to explicitly
// implement an interface method (Fase 3.13) — e.g. a `yield return`
// iterator's compiler-generated class implements IEnumerable`1::
// GetEnumerator not as a plain "GetEnumerator" method but as
// "System.Collections.Generic.IEnumerable<System.Int32>.GetEnumerator"
// (the mangled name explicit interface implementation requires), which a
// plain concreteType+"::"+method lookup can never find. implMethodName is
// already "<declaringType>::<mangledMethod>", ready to call directly —
// declaringType is whichever ancestor of concreteTypeFullName actually
// declares the MethodImpl, which need not be concreteTypeFullName itself.
// Returns ok=false when the type implements the interface method under
// its plain name (or doesn't implement it at all) — the ordinary
// receiverTypeName fallback in Machine.call already covers that case.
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

// MemberResolver finds a real method or constructor by exact name and
// declared parameter type names (Fase 3.39, System.Reflection —
// Type.GetConstructor/GetMethod) and returns its full callable name
// ("Namespace.Type::Member"), if one exists — exact declared-type-name
// matching per real reflection semantics (no argument-Kind coercion,
// unlike pickMethodOverload's runtime-argument-based scoring, since
// there are no actual arguments yet at this point, only the caller's own
// declared Type[] signature). memberName is ".ctor" for
// Type.GetConstructor.
type MemberResolver func(typeFullName, memberName string, paramTypeFullNames []string) (fullName string, ok bool)

// ManifestResourceResolver returns an embedded manifest resource's raw
// bytes by name (Fase 3.40, Assembly.GetManifestResourceStream) — see
// runtime.Resolvers.ResolveManifestResource.
type ManifestResourceResolver func(name string) ([]byte, bool)

// PropertyResolver reads a plugin type's own declared properties (Fase
// 3.50, Type.GetProperties/GetProperty; propTypes added Fase 3.52 for
// PropertyInfo.PropertyType) — see runtime.Resolvers.ResolveProperties.
type PropertyResolver func(typeFullName string) (names []string, canRead []bool, canWrite []bool, propTypes []string, ok bool)

// MemberParamsResolver reads every real overload of a member's own
// declared parameter list (Fase 3.52, Type.GetConstructors/MethodBase.
// GetParameters) — see runtime.Resolvers.ResolveMemberParams.
type MemberParamsResolver func(typeFullName, memberName string) (paramTypes [][]string, paramNames [][]string, ok bool)

// FieldsResolver reads every field typeFullName's own TypeDef declares
// (Fase 3.53, Type.GetFields plus FieldInfo.FieldType) — parallel slices
// (names[i]/fieldTypes[i]/isStatic[i] all describe the same i'th field,
// same convention PropertyResolver's own names/canRead/canWrite/propTypes
// slices already use) — see runtime.Resolvers.ResolveFields.
type FieldsResolver func(typeFullName string) (names []string, fieldTypes []string, isStatic []bool, ok bool)

// MethodsResolver reads every method name typeFullName's own TypeDef
// declares (Fase 3.53, Type.GetMethods) — see runtime.Resolvers.
// ResolveMethods.
type MethodsResolver func(typeFullName string) (names []string, ok bool)

// MemberFlagsResolver reads every real overload of a member's own raw
// ECMA-335 MethodAttributes bitmask (Fase 3.60, MethodBase.IsPublic/
// IsPrivate/IsStatic/IsVirtual/IsAbstract/IsFinal/IsFamily/IsAssembly) —
// flags[i] is memberName's i'th overload's Flags, in exactly the same
// order MemberParamsResolver's own paramTypes[i]/paramNames[i] already
// enumerate that member's overloads, so a ConstructorInfo/MethodInfo
// wrapper's existing (typeFullName, memberName, overloadIndex) triple
// re-resolves flags the same way methodBaseGetParameters already
// re-resolves parameters, rather than needing its own new field.
type MemberFlagsResolver func(typeFullName, memberName string) (flags []uint16, ok bool)

// CustomAttributesResolver reads every real custom attribute applied to a
// member (Fase 3.63, System.Reflection.CustomAttributeData/
// CustomAttributeExtensions.GetCustomAttribute<T>) — see
// runtime.Resolvers.ResolveCustomAttributes.
type CustomAttributesResolver func(typeFullName, memberKind, memberName string) (attrs []runtime.ResolvedAttribute, ok bool)

// genericMachineNative is a Machine-aware native (like machineNative,
// linq.go) that additionally needs the call site's own resolved generic
// method type arguments (Fase 3.40, ir.Call.MethodGenericArgs) — e.g.
// DocumentFormat.OpenXml.Packaging.FeatureCollectionBase::Get<TFeature>,
// whose real body does `this[typeof(TFeature)]` and has no other way to
// learn what TFeature actually is (see ir.Call.MethodGenericArgs's own
// doc comment for why this is a separate, narrower registry rather than
// widening machineNative itself).
type genericMachineNative func(m *Machine, args []runtime.Value, methodGenericArgs []string, depth int, instrCount *int64) (runtime.Value, error)

var genericMachineRegistry = map[string]genericMachineNative{}

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
func (m *Machine) call(fullName string, args []runtime.Value, virtual bool, depth int, instrCount *int64, paramTypeNames []string, methodGenericArgs []string) (runtime.Value, bool, error) {
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
	// class between them and Node does. So — after the explicit-impl
	// check below, which must run first — this walks the full chain
	// from the concrete type all the way up (INCLUDING the declared
	// type itself, Fase 3.48 — see the loop's own doc comment for why
	// skipping it used to let a worse-matching ancestor win the race),
	// trying each ancestor's own plain-named override before giving up
	// on this receiver hierarchy entirely.
	if virtual && len(args) > 0 {
		if concrete, ok := receiverTypeName(args[0]); ok {
			if class, method, ok := splitCallName(fullName); ok {
				// Explicit interface implementations (a real MethodImpl row
				// pairing "class::method" to a mangled body on some
				// ancestor) must be tried BEFORE the plain-name ancestor
				// walk below, not just as a fallback once it comes up
				// empty (Fase 3.40, found via a real bug: DocumentFormat.
				// OpenXml.Features.PackageFeatureBase declares BOTH a
				// plain `protected abstract Package Package { get; }` AND
				// an unrelated explicit `IPackage
				// DocumentFormat.OpenXml.Features.IPackageFeature.
				// get_Package()` — both bare-named "get_Package" on the
				// exact same ancestor. The old order let the plain-name
				// walk claim victory first purely because a same-named
				// method happened to exist, silently returning the
				// receiver's real System.IO.Packaging.Package/ZipPackage
				// instead of the wrapper `this` the interface method
				// actually returns — corrupting every later IPackage-typed
				// call on it. Real C#/CLR semantics never leave this
				// ambiguous: an explicit interface implementation always
				// wins over an unrelated same-named member, so it must be
				// checked first here too.
				if m.ResolveExplicitImpl != nil {
					// implMethod is already fully qualified as
					// "<declaringType>::<mangledMethod>" — the declaring
					// type can be any ancestor of concrete, not
					// necessarily concrete itself (Fase 3.40, see
					// resolveExplicitImpl's own doc comment).
					if implMethod, ok := m.ResolveExplicitImpl(concrete, class, method); ok {
						v, hasReturn, err, found, rerr := m.tryCall(implMethod, args, depth, instrCount, paramTypeNames, methodGenericArgs)
						if found {
							return v, hasReturn, err
						}
						if rerr != nil {
							lastResolveErr = rerr
						}
					}
				}
				seen := map[string]bool{}
				for t, ok := concrete, true; ok && !seen[t]; t, ok = m.baseTypeOf(t) {
					seen[t] = true
					// t==class (the declared/host-call target itself) used
					// to be skipped here on the theory that the final
					// plain-fullName fallback below already covers it —
					// true in isolation, but wrong in a walk that returns
					// on the FIRST found match: for a host-driven
					// Instance.Call (Fase 3.28), fullName always names the
					// receiver's own exact concrete type
					// (in.typeName+"::"+methodName), so concrete==class on
					// the very first iteration — skipping it here meant
					// the walk could return an ANCESTOR's worse-matching
					// overload (found first, since class was never even
					// tried until every ancestor was exhausted) instead of
					// the leaf type's own better-matching one. Found via a
					// real, load-bearing case (Fase 3.48): Newtonsoft.
					// Json's own JObject.get_Item(string) — declared
					// directly on JObject — losing to JContainer/JToken's
					// unrelated get_Item(object) inherited overload, which
					// this same ancestor walk incorrectly tried first.
					// Trying t==class here (identical to retryName==
					// fullName) is redundant-safe with the final fallback,
					// not a correctness risk — pickMethodOverload's own
					// candidate scoring at THIS exact name is unaffected
					// by when it runs, only by whether an ancestor's
					// unrelated match wrongly wins the race first.
					retryName := t + "::" + method
					v, hasReturn, err, found, rerr := m.tryCall(retryName, args, depth, instrCount, paramTypeNames, methodGenericArgs)
					if found {
						return v, hasReturn, err
					}
					if rerr != nil {
						lastResolveErr = rerr
					}
				}
				// Last resort: System.Object's own Equals/GetHashCode/
				// ToString/GetType (Fase 3.40, GetType added hardening
				// this same gap further). The ancestor chain walk above
				// never reaches "System.Object" itself — buildType
				// deliberately leaves BaseTypeFullName empty for a type
				// whose immediate base IS Object (assembly.go's own
				// "resolved != System.Object" guard) — so a real,
				// unoverridden inherited Equals called through a generic
				// interface constraint (`IEquatable<T>::Equals`, found
				// via System.IO.Packaging's own ValidatedPartUri going
				// through EqualityComparer<T>-style dispatch) fell all
				// the way through to the literal "IEquatable`1::Equals"
				// fallback below, which can never resolve (no real
				// TypeDef for a BCL-only generic interface). Only tried
				// for the 4 names that actually have a native here, AND
				// only when the argument count actually matches Object's
				// own arity (Equals: receiver+1, GetHashCode/ToString/
				// GetType: receiver only) — a same-named but differently-
				// shaped interface method (IEqualityComparer<T>.Equals(T,
				// T), receiver+2) must NOT be redirected here: found via a
				// real bug where a comparer's own unresolvable
				// Equals(x,y) hit this fallback and objectEquals rejected
				// it outright ("expects 2 arguments"), instead of falling
				// through to the ordinary, more informative error below.
				//
				// GetType specifically: found via a real, common pattern
				// this ancestor walk alone can't reach at all — a plugin
				// exception subclass (`class MyException : Exception`)
				// calling `e.GetType()` inside a catch block walks
				// MyException -> System.Exception (its real
				// BaseTypeFullName) -> stops (System.Exception has no
				// TypeDef to resolve further, so baseTypeOf's ok is
				// false), never trying System.Object at all — regardless
				// of catch (Exception e).ToString()/Equals hitting the
				// exact same dead end, which this fallback already
				// covered for those two.
				if (method == "Equals" && len(args) == 2) || ((method == "GetHashCode" || method == "ToString" || method == "GetType") && len(args) == 1) {
					v, hasReturn, err, found, rerr := m.tryCall("System.Object::"+method, args, depth, instrCount, paramTypeNames, methodGenericArgs)
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

	// The receiver's concrete type has no override at all — use the
	// declared/base name directly (the overwhelmingly common case: most
	// calls aren't virtual, and most virtual calls have no override
	// anywhere in between the declared type and the receiver's own).
	v, hasReturn, err, found, resolveErr := m.tryCall(fullName, args, depth, instrCount, paramTypeNames, methodGenericArgs)
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

// charSensitiveNatives lists the handful of natives whose real behavior
// genuinely differs between a `char` and a plain `int` argument that
// vmnet's uniform KindI4 (spec §17.1: no distinct char Kind, documented
// since the NPOI phase) can't tell apart on its own — StringBuilder's own
// doc comment explains the general limitation. Rather than widen every
// native's signature to take paramTypeNames (a large, invasive change
// for a narrow problem), this converts a KindI4 argument the call site's
// own resolved signature says is System.Char into a single-rune string
// right before dispatch, for just these known-affected natives (Fase
// 3.40, found via a real, load-bearing case: System.IO.Packaging's own
// ContentType.ToString() does `stringBuilder.Append('/')`, and without
// this the resulting content-type string reads "application47vnd..."
// instead of "application/vnd...", corrupting every OPC content-type
// ClosedXML/OpenXml round-trips through it).
var charSensitiveNatives = map[string]bool{
	"System.Text.StringBuilder::Append":     true,
	"System.Text.StringBuilder::AppendLine": true,
	"System.Text.StringBuilder::Insert":     true,
	// TextWriter.Write(char)/WriteLine(char) (internal/bcl/system_io_
	// stringwriter.go, Fase 3.53) has the identical problem AppendLine
	// above already documents: without this, a `Write('/')`-shaped real
	// call site would append the numeric code point ("47") instead of the
	// character itself.
	"System.IO.StringWriter::Write":     true,
	"System.IO.StringWriter::WriteLine": true,
	"System.IO.TextWriter::Write":       true,
	"System.IO.TextWriter::WriteLine":   true,
}

// boolSensitiveNatives is charSensitiveNatives' own Boolean counterpart,
// scoped narrower (TextWriter.Write/WriteLine only, not StringBuilder.
// Append/Insert — no real call site found needing it there, and widening
// an already-shipped native's behavior without one risks an unrelated
// regression for no benefit): real TextWriter.Write(bool)/WriteLine(bool)
// prints "True"/"False", not vmnet's uniform KindI4 0/1 that a plain
// `Write(int)` overload also produces from the exact same Value shape —
// confirmed against real `dotnet run` output (StringWriter.Write(true)
// prints "True") while chasing this file's own textWriterWrite bug.
var boolSensitiveNatives = map[string]bool{
	"System.IO.StringWriter::Write":     true,
	"System.IO.StringWriter::WriteLine": true,
	"System.IO.TextWriter::Write":       true,
	"System.IO.TextWriter::WriteLine":   true,
}

// convertCharArgsForNative converts args in place (on a copy, made lazily
// only if a real conversion is needed) for a charSensitiveNatives/
// boolSensitiveNatives entry. paramTypeNames indexes the call's own
// declared (non-receiver) parameters 1:1 (ir.Call.ParamTypeNames' own
// convention) — args[0] is the receiver for these instance methods, so
// argument i+1 is paramTypeNames[i]'s value.
func convertCharArgsForNative(fullName string, args []runtime.Value, paramTypeNames []string) []runtime.Value {
	charSensitive := charSensitiveNatives[fullName]
	boolSensitive := boolSensitiveNatives[fullName]
	if (!charSensitive && !boolSensitive) || len(paramTypeNames) == 0 {
		return args
	}
	out := args
	copied := false
	ensureCopy := func() {
		if !copied {
			out = append([]runtime.Value(nil), args...)
			copied = true
		}
	}
	for i, name := range paramTypeNames {
		argIdx := i + 1
		if argIdx >= len(out) {
			break
		}
		if charSensitive && name == "System.Char" && out[argIdx].Kind == runtime.KindI4 {
			ensureCopy()
			out[argIdx] = runtime.String(string(rune(out[argIdx].I4)))
		}
		if boolSensitive && name == "System.Boolean" && out[argIdx].Kind == runtime.KindI4 {
			ensureCopy()
			if out[argIdx].I4 != 0 {
				out[argIdx] = runtime.String("True")
			} else {
				out[argIdx] = runtime.String("False")
			}
		}
	}
	return out
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
func (m *Machine) tryCall(fullName string, args []runtime.Value, depth int, instrCount *int64, paramTypeNames []string, methodGenericArgs []string) (v runtime.Value, hasReturn bool, err error, found bool, resolveErr error) {
	if native, hr, ok := bcl.Lookup(fullName); ok {
		if gate, gated := permissionGatedBCLNatives[fullName]; gated {
			if denyErr := gate(m.Permissions, args); denyErr != nil {
				return runtime.Value{}, hr, denyErr, true, nil
			}
		}
		v, err = native(convertCharArgsForNative(fullName, args, paramTypeNames))
		return v, hr, err, true, nil
	}
	// genericMachineRegistry (Fase 3.40) is checked before the ordinary
	// machineRegistry: a handful of real methods (DocumentFormat.OpenXml.
	// Packaging.FeatureCollectionBase::Get<TFeature>, most notably) need
	// the call site's own resolved generic method type arguments, which
	// only reach this far and no further (see ir.Call.MethodGenericArgs's
	// own doc comment for why this stops here rather than reaching
	// ordinary interpreted method bodies too).
	if native, ok := genericMachineRegistry[fullName]; ok {
		v, err = native(m, args, methodGenericArgs, depth, instrCount)
		return v, true, err, true, nil
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
	method, rerr := m.Resolve(fullName, args, paramTypeNames, len(methodGenericArgs))
	if rerr != nil {
		return runtime.Value{}, false, nil, false, rerr
	}
	v, err = m.invoke(method, args, depth+1, instrCount, methodGenericArgs)
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
	// fn.FullName is the exact bound target for a plain ldftn (Fase
	// 3.9) — but for ldvirtftn (fn.Virtual, Fase 3.40: a method-group
	// conversion of a virtual/abstract method, e.g. DocumentFormat.
	// OpenXml.Builder's own `new Func<T>(builder.Create)`), FullName is
	// only the declared method; the real override lives on the bound
	// receiver's own concrete type, which Machine.call's ordinary
	// virtual-dispatch chain-walk (the same one callvirt itself uses)
	// resolves correctly once told to.
	return m.call(fn.FullName, callArgs, fn.Virtual, depth, instrCount, nil, nil)
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
	//
	// Args[1].Func.Receiver == nil is required, not just Kind == KindFunc:
	// a real 2-argument constructor whose own 2nd parameter merely happens
	// to be delegate-typed (e.g. DocumentFormat.OpenXml.Builder's own
	// internal `Factory(Func<TPackage> package, PackageInitializerDelegate
	// <TPackage> pipeline)`) also has args[1].Kind == KindFunc, and without
	// this guard was wrongly treated as a delegate ctor call too — found
	// the hard way (Fase 3.40): the "pipeline" argument there is an
	// already-bound, previously-constructed delegate (Receiver != nil),
	// whereas a genuine delegate-ctor's 2nd argument is always the raw,
	// still-unbound Func straight off `ldftn` (Receiver == nil at that
	// point, always — see ir.LoadFtn's own handling). Rebinding that
	// already-bound delegate value under BindDelegate's rules silently
	// discarded the real Factory object (2 real fields) in favor of
	// treating args[0] (Factory's own first, unrelated argument) as if it
	// were a delegate's receiver.
	if len(in.Args) == 2 && in.Args[1].Kind == runtime.KindFunc && in.Args[1].Func != nil && in.Args[1].Func.Receiver == nil {
		return runtime.BindDelegate(in.Args[0], *in.Args[1].Func, in.TypeFullName), nil
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

	// System.IntPtr is a real value type in .NET, but vmnet represents
	// it as a bare Int64 Value with no struct wrapper at all (see
	// system_intptr.go's own doc comment) — every other native ctor
	// below returns either a *runtime.Struct (LookupValueTypeCtor) or a
	// *runtime.Object (LookupCtor), neither of which fits "just a plain
	// scalar," so `new IntPtr(value)` needs its own path for the same
	// reason System.String does (Fase 3.41, found via a real, load-
	// bearing case: System.Text.Json's own JsonReaderHelper stackalloc-
	// scratch-buffer path constructs an IntPtr directly from an int).
	if in.TypeFullName == "System.IntPtr" || in.TypeFullName == "System.UIntPtr" {
		if len(in.Args) == 0 {
			return runtime.Int64(0), nil
		}
		switch in.Args[0].Kind {
		case runtime.KindI8:
			return runtime.Int64(in.Args[0].I8), nil
		default:
			return runtime.Int64(int64(in.Args[0].I4)), nil
		}
	}

	if vtCtor, ok := bcl.LookupValueTypeCtor(in.TypeFullName); ok {
		s, err := vtCtor(in.Args)
		if err != nil {
			return runtime.Value{}, err
		}
		return runtime.StructVal(s), nil
	}

	if ctor, ok := bcl.LookupCtor(in.TypeFullName); ok {
		if gate, gated := permissionGatedBCLCtors[in.TypeFullName]; gated {
			if denyErr := gate(m.Permissions, in.Args); denyErr != nil {
				return runtime.Value{}, denyErr
			}
		}
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
		if _, _, err := m.call(in.CtorFullName, ctorArgs, false, depth, instrCount, in.ParamTypeNames, nil); err != nil {
			// `new T()` with no arguments at all (Fase 3.40) — a struct
			// with no explicitly-declared constructor has no real .ctor
			// method in metadata whatsoever: `new T()` is pure C# syntax
			// sugar the compiler lowers straight to `initobj`, identical
			// to default(T) — found via a real, load-bearing case:
			// System.Text.Json's own JsonDocumentOptions (a plain options
			// struct, no declared ctor) needs to be passed explicitly to
			// JsonDocument.Parse's optional-parameter-turned-required
			// 2nd argument. Only safe to fall back silently when there
			// are zero arguments — anything else genuinely needed a real
			// constructor to run and this must still surface that error.
			if len(in.Args) == 0 {
				return objVal, nil
			}
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
	obj := &runtime.Object{Type: typ, Fields: fields, ClassGenericArgs: in.ClassGenericArgs}
	objVal := runtime.ObjRef(obj)

	ctorArgs := make([]runtime.Value, 0, len(in.Args)+1)
	ctorArgs = append(ctorArgs, objVal)
	ctorArgs = append(ctorArgs, in.Args...)
	if _, _, err := m.call(in.CtorFullName, ctorArgs, false, depth, instrCount, in.ParamTypeNames, nil); err != nil {
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

	// ParamTypeNames is the newobj site's own declared .ctor overload
	// signature (ir.NewObj.ParamTypeNames, Fase 3.43 — see its doc comment
	// for the real XLFill case that made ctor overload resolution need
	// this), passed through to Machine.call/pickMethodOverload exactly
	// like an ordinary ir.Call's ParamTypeNames. nil for native callers
	// (loaddomtree.go/elementfactory.go's parameterless `new T()`s) with
	// no IR-level ctor token to read a signature from.
	ParamTypeNames []string

	// ClassGenericArgs is the constructed generic CLASS's own real,
	// already-forwarding-resolved closed type argument names (Fase
	// 3.66, ir.NewObj.ClassGenericArgs — resolved against the calling
	// frame's own MethodGenericArgs by eval.go's own ir.NewObj case
	// before reaching here). Stored on the resulting runtime.Object so
	// its own methods' `typeof(T)` on a CLASS-level generic parameter
	// (ir.LoadTypeToken.IsClassGenericParam) can answer correctly for
	// as long as the object exists — nil for a non-generic class or a
	// native constructor call with no IR-level newobj site.
	ClassGenericArgs []string
}
