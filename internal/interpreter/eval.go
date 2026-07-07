package interpreter

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/ir"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// valueAsThrowable converts a real exception object Value into the Go
// error `throw`/`ir.Throw` propagates — nil if v isn't a recognized
// exception instance at all. Factored out of ir.Throw's own handling
// (Fase 3.66) so Expression.Throw's evaluator (internal/interpreter/
// exprcompile.go) can reuse the identical real-exception-object logic
// rather than duplicating it.
func valueAsThrowable(v runtime.Value) error {
	if v.Kind != runtime.KindObject || v.Obj == nil {
		return nil
	}
	ex, ok := v.Obj.Native.(*runtime.ManagedException)
	if !ok {
		return nil
	}
	// Record the real thrown Object (see ManagedException.Object's own
	// doc comment) — a plugin exception subclass's extra fields (or a
	// base Exception's otherwise-untracked Data dictionary,
	// system_exception.go's exceptionGetData) are only reachable through
	// it, never through ManagedException's own flat TypeName/Message/
	// Inner fields. `throw e;` re-throwing an already-caught local is the
	// same v.Obj a previous catch already set this to, so this is
	// idempotent — not just a first-throw special case.
	ex.Object = v.Obj
	return ex
}

// Machine executes runtime.Method IR. Resolve supplies methods and
// ResolveType supplies field layouts for anything that isn't a BCL native
// (bcl.Lookup / bcl.LookupCtor).
type Machine struct {
	Resolve                 Resolver
	ResolveType             TypeResolver
	ResolveExplicitImpl     ExplicitImplResolver
	ResolveEnum             EnumResolver
	ResolveFieldBytes       FieldBytesResolver
	ResolveMember           MemberResolver
	ResolveManifestResource ManifestResourceResolver
	ResolveProperties       PropertyResolver
	ResolveMemberParams     MemberParamsResolver
	ResolveFields           FieldsResolver
	ResolveMethods          MethodsResolver
	ResolveMemberFlags      MemberFlagsResolver
	ResolveCustomAttributes CustomAttributesResolver
	Limits                  Limits

	// Permissions is the deny-by-default capability gate (Fase 3.59,
	// internal/runtime/permissions.go) checked by permissionGatedBCLNatives
	// (calls.go) before any permission-gated plain bcl.Native runs — real
	// filesystem access, today. nil (a Machine built without
	// WithPermissions, e.g. most existing tests/fixtures) is treated
	// exactly like a non-nil zero value: every gated capability denied,
	// never "ungated" — a missing Permissions must never silently behave
	// as "allow everything".
	Permissions *runtime.Permissions

	// cctorsRunning tracks static constructors currently executing on
	// this Machine's own call chain (a Machine is never shared across
	// goroutines — see call.go's asm.machine()), so a .cctor that reads
	// or writes its own type's static fields (the overwhelmingly common
	// case) re-enters staticType without deadlocking on the Type's
	// EnsureCctor latch. See internal/interpreter/statics.go.
	cctorsRunning map[*runtime.Type]bool
}

func New(resolve Resolver, resolveType TypeResolver, limits Limits) *Machine {
	return &Machine{Resolve: resolve, ResolveType: resolveType, Limits: limits}
}

// WithExplicitImplResolver attaches an ExplicitImplResolver (Fase 3.13) to
// an already-constructed Machine — a separate setter rather than a New
// parameter so every existing caller (tests especially, see
// internal/interpreter/*_test.go) keeps compiling unchanged; the explicit
// interface impl fallback is a pure improvement that degrades to "no
// match" (not an error) when unset.
func (m *Machine) WithExplicitImplResolver(r ExplicitImplResolver) *Machine {
	m.ResolveExplicitImpl = r
	return m
}

// WithEnumResolver attaches an EnumResolver (Fase 3.26) — same rationale
// as WithExplicitImplResolver: a separate setter so existing callers keep
// compiling unchanged, degrading to "no plugin enum data available"
// rather than an error when unset.
func (m *Machine) WithEnumResolver(r EnumResolver) *Machine {
	m.ResolveEnum = r
	return m
}

// WithFieldBytesResolver attaches a FieldBytesResolver (Fase 3.27,
// RuntimeHelpers.InitializeArray) — same rationale as
// WithExplicitImplResolver: a separate setter so existing callers keep
// compiling unchanged. Redundant for any method actually invoked through
// Invoke (Machine.invoke swaps in method.Resolvers.ResolveFieldBytes
// before running its body regardless), but leaving this field nil until
// that first swap is needless asymmetry with the other four resolvers,
// which every constructor call site already sets up front.
func (m *Machine) WithFieldBytesResolver(r FieldBytesResolver) *Machine {
	m.ResolveFieldBytes = r
	return m
}

// WithMemberResolver attaches a MemberResolver (Fase 3.39,
// System.Reflection.ConstructorInfo/MethodInfo) — same rationale as
// WithFieldBytesResolver.
func (m *Machine) WithMemberResolver(r MemberResolver) *Machine {
	m.ResolveMember = r
	return m
}

// WithManifestResourceResolver attaches a ManifestResourceResolver (Fase
// 3.40, Assembly.GetManifestResourceStream) — same rationale as
// WithFieldBytesResolver/WithMemberResolver.
func (m *Machine) WithManifestResourceResolver(r ManifestResourceResolver) *Machine {
	m.ResolveManifestResource = r
	return m
}

// WithPropertyResolver attaches a PropertyResolver (Fase 3.51, Type.
// GetProperties/GetProperty) — same rationale as WithFieldBytesResolver/
// WithMemberResolver.
func (m *Machine) WithPropertyResolver(r PropertyResolver) *Machine {
	m.ResolveProperties = r
	return m
}

// WithMemberParamsResolver attaches a MemberParamsResolver (Fase 3.52,
// Type.GetConstructors/MethodBase.GetParameters) — same rationale as
// WithFieldBytesResolver/WithMemberResolver.
func (m *Machine) WithMemberParamsResolver(r MemberParamsResolver) *Machine {
	m.ResolveMemberParams = r
	return m
}

// WithFieldsResolver attaches a FieldsResolver (Fase 3.53, Type.GetFields
// plus FieldInfo.FieldType) — same rationale as WithFieldBytesResolver/
// WithMemberResolver.
func (m *Machine) WithFieldsResolver(r FieldsResolver) *Machine {
	m.ResolveFields = r
	return m
}

// WithMethodsResolver attaches a MethodsResolver (Fase 3.53, Type.
// GetMethods) — same rationale as WithFieldBytesResolver/WithMemberResolver.
func (m *Machine) WithMethodsResolver(r MethodsResolver) *Machine {
	m.ResolveMethods = r
	return m
}

// WithMemberFlagsResolver attaches a MemberFlagsResolver (Fase 3.60,
// MethodBase.IsPublic/IsPrivate/IsStatic/IsVirtual/IsAbstract/IsFinal/
// IsFamily/IsAssembly) — same rationale as WithFieldBytesResolver/
// WithMemberResolver.
func (m *Machine) WithMemberFlagsResolver(r MemberFlagsResolver) *Machine {
	m.ResolveMemberFlags = r
	return m
}

// WithCustomAttributesResolver attaches a CustomAttributesResolver (Fase
// 3.63, System.Reflection.CustomAttributeData/CustomAttributeExtensions.
// GetCustomAttribute<T>) — same rationale as WithFieldBytesResolver/
// WithMemberResolver.
func (m *Machine) WithCustomAttributesResolver(r CustomAttributesResolver) *Machine {
	m.ResolveCustomAttributes = r
	return m
}

// WithPermissions attaches the deny-by-default capability gate (Fase
// 3.59) — same rationale as WithFieldBytesResolver/WithMemberResolver: a
// separate setter so every existing caller (tests especially) keeps
// compiling unchanged. Unset (nil) behaves identically to an explicit
// &runtime.Permissions{} — every gated capability denied.
func (m *Machine) WithPermissions(p *runtime.Permissions) *Machine {
	m.Permissions = p
	return m
}

// Invoke runs method with args and returns its result (the zero Value if
// method is void).
//
// A vmnet plugin must never be able to crash its host: Invoke recovers any
// panic from anywhere in the call tree below it (a bounds check we missed,
// a bad type assertion, malformed IR) and turns it into a plain error
// instead of unwinding into the caller's goroutine.
func (m *Machine) Invoke(method *runtime.Method, args []runtime.Value) (result runtime.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("interpreter: internal error (recovered panic): %v", r)
		}
	}()
	instrCount := new(int64)
	return m.invoke(method, args, 0, instrCount, nil)
}

// New constructs an instance of typeFullName via a real newobj + its
// resolved .ctor overload — the exact same machinery ir.NewObj drives
// when a newobj instruction executes inside another method's IR (see
// Machine.newObj), just entered fresh from the host instead. Exported
// for vmnet's public Assembly.New API (Fase 3.28): the piece that lets
// host code construct an instance of a plugin/dependency type (e.g.
// Jint's `new Engine()`) without a compiled glue assembly. The .ctor
// overload is picked by arity/Kind against args exactly like any other
// call — see assembly.go's pickMethodOverload.
func (m *Machine) New(typeFullName string, args []runtime.Value) (result runtime.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("interpreter: internal error (recovered panic): %v", r)
		}
	}()
	instrCount := new(int64)
	// Unlike an ordinary `newobj` IL instruction (whose own ClassGenericArgs
	// may need forwarding-resolution against the CALLING frame's own
	// MethodGenericArgs, see ir.NewObj.ClassGenericArgs's own doc
	// comment), typeFullName here is ALREADY fully closed — this is the
	// entry point real reflection-based construction goes through
	// (Activator.CreateInstance(Type)/(Type,object[]), Assembly.New's own
	// public API), where the caller already resolved every generic
	// argument at runtime before ever reaching here. Parsed directly off
	// the name's own "[[...]]" suffix, same as bcl's own
	// typeGetGenericArguments (Fase 3.66, found via CsvHelper's own
	// AutoMap machinery constructing its internal ClassMap via
	// reflection rather than a literal `newobj Type\`N<Args>` site).
	//
	// TypeFullName/CtorFullName below carry the OPEN name, not typeFullName
	// itself (Fase 3.81) — newObj's own m.ResolveType/m.call lookups
	// resolve a TypeDef/MethodDef by its real metadata name, which for a
	// generic class is always the open one (ECMA-335: one TypeDef per
	// open generic, never one per closed instantiation — see
	// typeFullNameOfOpen's own doc comment, reflection.go). The closed
	// instantiation itself is carried separately via ClassGenericArgs,
	// exactly like any real compiled `newobj Type\`N<Args>::.ctor()` site
	// already does. Passing the closed name straight through here (as
	// this did before Fase 3.81) made m.ResolveType look up a TypeDef by
	// a name like "DefaultClassMap\`1[[Person]]", which no TypeDef row
	// ever has — "type ... not found" for every reflection-constructed
	// closed generic instance, the exact shape CsvHelper's own AutoMap()
	// needs (`(ClassMap)ObjectResolver.Current.Resolve(typeof(
	// DefaultClassMap<>).MakeGenericType(recordType))`).
	openName := bcl.GenericOpenName(typeFullName)
	return m.newObj(newObjArgs{TypeFullName: openName, CtorFullName: openName + "::.ctor", Args: args, ClassGenericArgs: bcl.ClosedGenericArgs(typeFullName)}, 0, instrCount)
}

// CallInstance invokes fullName ("Namespace.Type::Method") as an
// instance method, with args[0] as the receiver (this) and args[1:] the
// real call arguments — exported for vmnet's public Instance.Call API
// (Fase 3.28). Always dispatches as a virtual call (Machine.call's
// concrete-type-first, full-inheritance-chain-walk behavior, Fase
// 3.27): safe even for a genuinely non-virtual method, since the
// receiver's own concrete type is tried first regardless and a
// non-virtual method is always declared directly on it.
func (m *Machine) CallInstance(fullName string, args []runtime.Value) (result runtime.Value, hasReturn bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("interpreter: internal error (recovered panic): %v", r)
		}
	}()
	instrCount := new(int64)
	result, hasReturn, err = m.call(fullName, args, true, 0, instrCount, nil, nil)
	return
}

func (m *Machine) invoke(method *runtime.Method, args []runtime.Value, depth int, instrCount *int64, methodGenericArgs []string) (runtime.Value, error) {
	if m.Limits.MaxCallDepth > 0 && depth > m.Limits.MaxCallDepth {
		return runtime.Value{}, ErrCallDepthExceeded
	}

	// Assembly-scoped resolution (Fase 3.27): method's own IR references
	// names ("Namespace.Type::Method", "Namespace.Type") meaningful only
	// against the specific Assembly whose metadata produced it — not
	// necessarily the entry-point assembly Call() was originally invoked
	// on. A multi-assembly dependency chain can have the SAME type name
	// in more than one assembly (found running real Jint: both Jint.dll
	// and Esprima.dll have their own compiler-generated
	// `<PrivateImplementationDetails>`) — resolving against the wrong one
	// silently returns the wrong type's static fields instead of erroring,
	// which is worse than a crash. Swapping Machine's active resolvers to
	// match whichever method is currently executing, for the duration of
	// that call, keeps every nested resolution (a call target, a field's
	// owning type, ...) scoped to the right assembly automatically — a
	// Machine is never shared across goroutines (see call.go's
	// asm.machine()), so this mutate-then-restore is safe with no locking.
	if method.Resolvers != nil {
		prevResolve, prevResolveType := m.Resolve, m.ResolveType
		prevResolveExplicitImpl, prevResolveEnum := m.ResolveExplicitImpl, m.ResolveEnum
		prevResolveFieldBytes := m.ResolveFieldBytes
		prevResolveMember := m.ResolveMember
		prevResolveManifestResource := m.ResolveManifestResource
		prevResolveProperties := m.ResolveProperties
		prevResolveMemberParams := m.ResolveMemberParams
		prevResolveFields := m.ResolveFields
		prevResolveMethods := m.ResolveMethods
		prevResolveMemberFlags := m.ResolveMemberFlags
		prevResolveCustomAttributes := m.ResolveCustomAttributes
		if method.Resolvers.Resolve != nil {
			m.Resolve = method.Resolvers.Resolve
		}
		if method.Resolvers.ResolveType != nil {
			m.ResolveType = method.Resolvers.ResolveType
		}
		if method.Resolvers.ResolveExplicitImpl != nil {
			m.ResolveExplicitImpl = method.Resolvers.ResolveExplicitImpl
		}
		if method.Resolvers.ResolveEnum != nil {
			m.ResolveEnum = method.Resolvers.ResolveEnum
		}
		if method.Resolvers.ResolveFieldBytes != nil {
			m.ResolveFieldBytes = method.Resolvers.ResolveFieldBytes
		}
		if method.Resolvers.ResolveMember != nil {
			m.ResolveMember = method.Resolvers.ResolveMember
		}
		if method.Resolvers.ResolveManifestResource != nil {
			m.ResolveManifestResource = method.Resolvers.ResolveManifestResource
		}
		if method.Resolvers.ResolveProperties != nil {
			m.ResolveProperties = method.Resolvers.ResolveProperties
		}
		if method.Resolvers.ResolveMemberParams != nil {
			m.ResolveMemberParams = method.Resolvers.ResolveMemberParams
		}
		if method.Resolvers.ResolveFields != nil {
			m.ResolveFields = method.Resolvers.ResolveFields
		}
		if method.Resolvers.ResolveMethods != nil {
			m.ResolveMethods = method.Resolvers.ResolveMethods
		}
		if method.Resolvers.ResolveMemberFlags != nil {
			m.ResolveMemberFlags = method.Resolvers.ResolveMemberFlags
		}
		if method.Resolvers.ResolveCustomAttributes != nil {
			m.ResolveCustomAttributes = method.Resolvers.ResolveCustomAttributes
		}
		defer func() {
			m.Resolve, m.ResolveType = prevResolve, prevResolveType
			m.ResolveExplicitImpl, m.ResolveEnum = prevResolveExplicitImpl, prevResolveEnum
			m.ResolveFieldBytes = prevResolveFieldBytes
			m.ResolveMember = prevResolveMember
			m.ResolveManifestResource = prevResolveManifestResource
			m.ResolveProperties = prevResolveProperties
			m.ResolveMemberParams = prevResolveMemberParams
			m.ResolveFields = prevResolveFields
			m.ResolveMethods = prevResolveMethods
			m.ResolveMemberFlags = prevResolveMemberFlags
			m.ResolveCustomAttributes = prevResolveCustomAttributes
		}()
	}

	// Each call needs its own independent locals, not shared aliases into
	// method.LocalDefaults (cached once on the Method) — Clone() is a
	// no-op for every kind except KindStruct, where it allocates a fresh
	// Fields backing slice per call (see runtime.Value.Clone's doc
	// comment).
	locals := make([]runtime.Value, method.LocalCount)
	for i, def := range method.LocalDefaults {
		locals[i] = def.Clone()
	}

	frame := &Frame{
		Args:              args,
		Locals:            locals,
		Stack:             make([]runtime.Value, 0, method.MaxStack+8),
		MethodGenericArgs: methodGenericArgs,
	}

	// A managed exception surfacing from runFrame (from `throw`/`rethrow`
	// directly in this method, or propagated up from any call it made —
	// runFrame returning means frame.IP is exactly the instruction that
	// was executing, whatever the actual source) gets one dispatch
	// attempt against this method's own try/catch/finally (Fase 3.10)
	// before it's allowed to keep propagating to our own caller.
	for {
		result, err := m.runFrame(frame, method, depth, instrCount)
		if err == nil {
			return result, nil
		}
		ex, ok := err.(*runtime.ManagedException)
		if !ok {
			return runtime.Value{}, err
		}
		if !m.dispatchException(frame, ex, handlersContaining(method, frame.IP)) {
			// Genuinely leaving this frame unhandled (spec §18.3's own
			// stack trace) — recorded here, not at the `throw` site
			// itself, so a `catch`-and-rethrow (a real, common pattern)
			// still gets ITS OWN frame's entry once IT, in turn, fails
			// to handle whatever it rethrows, rather than only ever
			// recording the ORIGINAL throw site.
			ex.PushFrame(method.FullName)
			return runtime.Value{}, ex
		}
		// Handled: dispatchException already pointed frame.IP at the
		// matching catch (or an intervening finally/fault it must run
		// first) — resume execution there.
	}
}

func (m *Machine) runFrame(frame *Frame, method *runtime.Method, depth int, instrCount *int64) (runtime.Value, error) {
	for frame.IP < len(method.IR) {
		*instrCount++
		if m.Limits.MaxInstructions > 0 && *instrCount > m.Limits.MaxInstructions {
			return runtime.Value{}, ErrInstructionLimitExceeded
		}
		if m.Limits.MaxStackDepth > 0 && len(frame.Stack) > m.Limits.MaxStackDepth {
			return runtime.Value{}, ErrStackOverflow
		}

		next := frame.IP + 1

		switch in := method.IR[frame.IP].(type) {
		case ir.Nop:
			// no-op

		case ir.Dup:
			v := frame.pop()
			frame.push(v)
			frame.push(v)

		case ir.Pop:
			frame.pop()

		case ir.LocalAlloc:
			// See ir.LocalAlloc's own doc comment: a real runtime.Array
			// of zeroed "bytes" (KindI4 0..255 per element, matching the
			// byte[] convention internal/bcl/system_unsafe.go and
			// memorymarshal.go both already rely on), addressed the same
			// way an RVA-backed static array's managed pointer already is.
			size := frame.pop()
			n := int(size.I4)
			if n < 0 {
				n = 0
			}
			arr := runtime.NewArray(n)
			for i := range arr.Elems {
				arr.Elems[i] = runtime.Int32(0)
			}
			boxed := runtime.ArrRef(arr)
			frame.push(runtime.RefTo(&boxed))

		case ir.LoadArg:
			if in.Index < 0 || in.Index >= len(frame.Args) {
				return runtime.Value{}, fmt.Errorf("interpreter: ldarg index %d out of range", in.Index)
			}
			frame.push(frame.Args[in.Index])

		case ir.StoreArg:
			if in.Index < 0 || in.Index >= len(frame.Args) {
				return runtime.Value{}, fmt.Errorf("interpreter: starg index %d out of range", in.Index)
			}
			frame.Args[in.Index] = frame.pop().Clone()

		case ir.LoadArgAddr:
			if in.Index < 0 || in.Index >= len(frame.Args) {
				return runtime.Value{}, fmt.Errorf("interpreter: ldarga index %d out of range", in.Index)
			}
			frame.push(runtime.RefTo(&frame.Args[in.Index]))

		case ir.LoadLocal:
			if in.Index < 0 || in.Index >= len(frame.Locals) {
				return runtime.Value{}, fmt.Errorf("interpreter: ldloc index %d out of range", in.Index)
			}
			frame.push(frame.Locals[in.Index])

		case ir.StoreLocal:
			if in.Index < 0 || in.Index >= len(frame.Locals) {
				return runtime.Value{}, fmt.Errorf("interpreter: stloc index %d out of range", in.Index)
			}
			frame.Locals[in.Index] = frame.pop().Clone()

		case ir.LoadLocalAddr:
			if in.Index < 0 || in.Index >= len(frame.Locals) {
				return runtime.Value{}, fmt.Errorf("interpreter: ldloca index %d out of range", in.Index)
			}
			frame.push(runtime.RefTo(&frame.Locals[in.Index]))

		case ir.LoadConstI4:
			frame.push(runtime.Int32(in.Value))
		case ir.LoadConstI8:
			frame.push(runtime.Int64(in.Value))
		case ir.LoadConstR4:
			frame.push(runtime.Float32(in.Value))
		case ir.LoadConstR8:
			frame.push(runtime.Float64(in.Value))
		case ir.LoadString:
			frame.push(runtime.String(in.Value))
		case ir.LoadNull:
			frame.push(runtime.Null())

		case ir.BinOp:
			b := frame.pop()
			a := frame.pop()
			v, err := evalBinOp(in, a, b)
			if err != nil {
				return runtime.Value{}, err
			}
			frame.push(v)

		case ir.Neg:
			v, err := evalNeg(frame.pop())
			if err != nil {
				return runtime.Value{}, err
			}
			frame.push(v)

		case ir.Not:
			v, err := evalNot(frame.pop())
			if err != nil {
				return runtime.Value{}, err
			}
			frame.push(v)

		case ir.Conv:
			v, err := evalConv(in.Kind, frame.pop())
			if err != nil {
				return runtime.Value{}, err
			}
			frame.push(v)

		case ir.Branch:
			next = in.Target

		case ir.BranchIfTrue:
			if frame.pop().Truthy() {
				next = in.Target
			}

		case ir.BranchIfFalse:
			if !frame.pop().Truthy() {
				next = in.Target
			}

		case ir.Switch:
			idx := frame.pop()
			if idx.Kind != runtime.KindI4 {
				return runtime.Value{}, fmt.Errorf("interpreter: switch on non-int32 value kind %d", idx.Kind)
			}
			if idx.I4 >= 0 && int(idx.I4) < len(in.Targets) {
				next = in.Targets[idx.I4]
			}
			// out of range: falls through to the next instruction (spec §III.3.68)

		case ir.BranchCompare:
			b := frame.pop()
			a := frame.pop()
			take, err := evalCompare(in, a, b)
			if err != nil {
				return runtime.Value{}, err
			}
			if take {
				next = in.Target
			}

		case ir.Call:
			total := in.ArgCount
			if in.HasThis {
				total++
			}
			if len(frame.Stack) < total {
				return runtime.Value{}, fmt.Errorf("interpreter: call to %s: stack underflow", in.FullName)
			}
			callArgs := append([]runtime.Value(nil), frame.Stack[len(frame.Stack)-total:]...)
			frame.Stack = frame.Stack[:len(frame.Stack)-total]

			// A `constrained. !!T` prefix (spec §III.2.1) ahead of a
			// callvirt on an open generic method's own `T`-typed
			// parameter always loads that parameter's ADDRESS (ldarga),
			// not its value — the one shape that works uniformly whether
			// T turns out to be a value type (needing a box) or a
			// reference type (needing a plain dereference) at any given
			// closed instantiation. vmnet drops the `constrained.`
			// prefix entirely at IR-build time (ir/builder.go's own
			// comment: harmless when the receiver's real Kind already
			// carries enough information for dispatch) — true for a
			// value-typed receiver (already a KindRef to a KindStruct,
			// which the interface/virtual dispatch below already
			// handles directly), but not for THIS case: when T closes
			// over a REFERENCE type, the receiver arrives here as a
			// KindRef to a KindObject/KindArray/KindString/... slot,
			// never dereferenced, and every method this call resolves to
			// expects a plain receiver value, not a pointer to one.
			// Found running real Jint/Esprima's own ES6 class support:
			// Jint.AstExtensions.GetKey<T>(this T property, Engine
			// engine) where T : IProperty does `((IProperty)property).
			// Key` — a constrained interface callvirt on a still-open
			// generic parameter — corrupting every class body with at
			// least one member into a `NullReferenceException:
			// dereferencing a null managed pointer
			// (Esprima.Ast.ClassProperty.<Key>k__BackingField)` the
			// moment get_Key's own body tried to read its "this" as a
			// plain object instead of the address the (discarded)
			// constrained. prefix should have already resolved.
			// Auto-dereferencing HasThis's own receiver here — not just
			// for a `constrained.`-prefixed call, which vmnet's IR
			// doesn't track at all post-Nop, but for ANY call whose
			// receiver happens to be a KindRef to a non-struct — is safe
			// generally: a real reference-typed receiver is NEVER
			// legitimately passed as a raw KindRef by any other call
			// shape (a struct instance method's own byref `this` is the
			// only real producer of a KindRef receiver, and that case is
			// excluded below since its target IS a KindStruct).
			//
			// KindNull must ALSO stay excluded, alongside KindStruct: a
			// `ldloca`/`ldarga` + `call instance .ctor` in-place
			// construction (e.g. `new KeyValuePair<T,U>(k, v)` assigned
			// straight to a local, internal/bcl's own registerValueTypeCtor
			// convention) targets a local/arg slot whose CURRENT value is
			// its type's zero default — which is Null(), not yet a real
			// KindStruct, whenever that default couldn't be resolved at
			// all (an unresolvable still-open generic value-type default,
			// assembly.go's fieldOrLocalDefault — the same limitation
			// ClosedXML's own font-metrics engine already hits). Treating
			// that slot's ref as "obviously a reference type, dereference
			// it" would silently replace the pointer the in-place
			// constructor needs with the very Null() it's about to
			// overwrite, breaking every such constructor call outright
			// (found immediately via this project's own regression suite,
			// TestCheapWins3/KeyValuePairCtorTest, the moment this fix was
			// first tried without this exclusion).
			if in.HasThis && len(callArgs) > 0 && callArgs[0].Kind == runtime.KindRef && callArgs[0].Ref != nil &&
				callArgs[0].Ref.Kind != runtime.KindStruct && callArgs[0].Ref.Kind != runtime.KindNull {
				callArgs[0] = *callArgs[0].Ref
			}

			if in.Virtual && callArgs[0].Kind == runtime.KindNull {
				return runtime.Value{}, &runtime.ManagedException{
					TypeName: "System.NullReferenceException",
					Message:  fmt.Sprintf("Object reference not set to an instance of an object (calling %s)", in.FullName),
				}
			}

			// A delegate's Invoke (any delegate type — Action, Func`2, a
			// user's own `delegate` declaration — they all compile to the
			// exact same shape) is intercepted by receiver Kind, not by
			// FullName: vmnet never registers "SomeDelegateType::Invoke"
			// anywhere, since the delegate type name is unbounded (Fase
			// 3.9). See runtime.Func's doc comment.
			var result runtime.Value
			var hasReturn bool
			var err error
			if in.HasThis && callArgs[0].Kind == runtime.KindFunc {
				result, hasReturn, err = m.invokeFunc(callArgs[0].Func, callArgs[1:], depth, instrCount)
			} else {
				methodGenericArgs := resolveForwardedGenericArgs(in.MethodGenericArgs, frame.MethodGenericArgs, frameClassGenericArgs(frame))
				result, hasReturn, err = m.call(in.FullName, callArgs, in.Virtual, depth, instrCount, in.ParamTypeNames, methodGenericArgs)
			}
			if err != nil {
				return runtime.Value{}, err
			}
			// The call site's own declared signature (in.HasReturn, known
			// from the MethodRef at IR-build time) is authoritative for
			// the stack effect — not hasReturn, whatever the resolved
			// callee itself reports. These normally agree, but the Fase
			// 3.13 interface-dispatch fallback can redirect a call to a
			// concrete method whose real signature genuinely differs from
			// the interface's declared one (found via a real example:
			// non-generic System.Collections.IList::Add returns int, but
			// it redirects to List`1::Add, which is void — pushing
			// nothing there left the stack one short of what the
			// following instruction expected, an index-out-of-range panic
			// popping a value that was never pushed). Pushing Null() as a
			// placeholder keeps the stack balanced for the overwhelmingly
			// common case where that return value is immediately
			// discarded (e.g. `pop` right after) — a real numeric result
			// (the inserted index) is only lost if a caller actually
			// captures IList.Add's return value, a rare pattern in
			// practice.
			if in.HasReturn {
				if hasReturn {
					frame.push(result)
				} else {
					frame.push(runtime.Null())
				}
			}

		case ir.NewObj:
			if len(frame.Stack) < in.ArgCount {
				return runtime.Value{}, fmt.Errorf("interpreter: newobj %s: stack underflow", in.TypeFullName)
			}
			ctorArgs := append([]runtime.Value(nil), frame.Stack[len(frame.Stack)-in.ArgCount:]...)
			frame.Stack = frame.Stack[:len(frame.Stack)-in.ArgCount]

			// A generic class's own closed type args may themselves be
			// the ENCLOSING generic method's own still-open type
			// parameter being forwarded (the "!!N" sentinel, Fase 3.66 —
			// same resolveForwardedGenericArgs call ir.Call's own case
			// above already makes for MethodGenericArgs), e.g.
			// AutoMapper's own CreateMapCore<TSource,TDestination>
			// forwarding into `newobj MappingExpression`2<!!TSource,
			// !!TDestination>::.ctor(...)`.
			classGenericArgs := resolveForwardedGenericArgs(in.ClassGenericArgs, frame.MethodGenericArgs, frameClassGenericArgs(frame))
			v, err := m.newObj(newObjArgs{TypeFullName: in.TypeFullName, CtorFullName: in.CtorFullName, Args: ctorArgs, ParamTypeNames: in.ParamTypeNames, ClassGenericArgs: classGenericArgs}, depth, instrCount)
			if err != nil {
				return runtime.Value{}, err
			}
			frame.push(v)

		case ir.LoadField:
			obj := frame.pop()
			slot, err := m.fieldSlot(obj, in.TypeFullName, in.FieldName)
			if err != nil {
				return runtime.Value{}, err
			}
			frame.push(*slot)

		case ir.StoreField:
			val := frame.pop()
			obj := frame.pop()
			slot, err := m.fieldSlot(obj, in.TypeFullName, in.FieldName)
			if err != nil {
				return runtime.Value{}, err
			}
			*slot = val.Clone()

		case ir.LoadFieldAddr:
			obj := frame.pop()
			slot, err := m.fieldSlot(obj, in.TypeFullName, in.FieldName)
			if err != nil {
				return runtime.Value{}, err
			}
			frame.push(runtime.RefTo(slot))

		case ir.LoadStaticField:
			t, err := m.staticType(in.TypeFullName, depth, instrCount)
			if err != nil {
				return runtime.Value{}, err
			}
			idx := t.StaticFieldIndex(in.FieldName)
			if idx < 0 {
				return runtime.Value{}, fmt.Errorf("interpreter: %s has no static field %q", in.TypeFullName, in.FieldName)
			}
			frame.push(t.StaticField(idx))

		case ir.LoadStaticFieldAddr:
			t, err := m.staticType(in.TypeFullName, depth, instrCount)
			if err != nil {
				return runtime.Value{}, err
			}
			idx := t.StaticFieldIndex(in.FieldName)
			if idx < 0 {
				return runtime.Value{}, fmt.Errorf("interpreter: %s has no static field %q", in.TypeFullName, in.FieldName)
			}
			frame.push(runtime.RefTo(t.StaticFieldAddr(idx)))

		case ir.StoreStaticField:
			val := frame.pop()
			t, err := m.staticType(in.TypeFullName, depth, instrCount)
			if err != nil {
				return runtime.Value{}, err
			}
			idx := t.StaticFieldIndex(in.FieldName)
			if idx < 0 {
				return runtime.Value{}, fmt.Errorf("interpreter: %s has no static field %q", in.TypeFullName, in.FieldName)
			}
			t.SetStaticField(idx, val.Clone())

		case ir.Throw:
			if err := valueAsThrowable(frame.pop()); err != nil {
				return runtime.Value{}, err
			}
			return runtime.Value{}, fmt.Errorf("interpreter: thrown object is not a recognized exception type")

		case ir.NewArr:
			lenVal := frame.pop()
			if lenVal.Kind != runtime.KindI4 {
				return runtime.Value{}, fmt.Errorf("interpreter: newarr length must be int32")
			}
			if lenVal.I4 < 0 {
				return runtime.Value{}, &runtime.ManagedException{TypeName: "System.OverflowException", Message: "array length must be non-negative"}
			}
			if m.Limits.MaxArrayLength > 0 && int(lenVal.I4) > m.Limits.MaxArrayLength {
				return runtime.Value{}, ErrArrayTooLarge
			}
			arr := runtime.NewArray(int(lenVal.I4))
			// A value-type array element is never actually null in real
			// CLR semantics — e.g. Jint's StringDictionarySlim<T> indexes
			// straight into a `new Entry[capacity]` and reads .key off an
			// element with no null check, because that's guaranteed safe
			// for a real struct array. runtime.NewArray seeds every slot
			// with a blanket Null() regardless of element type, so a
			// struct/enum element type needs its slots reseeded with a
			// real zero-valued default (Fase 3.27).
			if in.TypeFullName != "" {
				if def := m.defaultValueFor(in.TypeFullName); def.Kind != runtime.KindNull {
					for i := range arr.Elems {
						// Each element needs its own *Struct — def.Clone()
						// deep-copies (Int32(0) clones to itself, harmless).
						arr.Elems[i] = def.Clone()
					}
				}
			}
			frame.push(runtime.ArrRef(arr))

		case ir.LoadLen:
			v := frame.pop()
			if v.Kind != runtime.KindArray || v.Arr == nil {
				return runtime.Value{}, &runtime.ManagedException{TypeName: "System.NullReferenceException", Message: "array reference is null (ldlen)"}
			}
			frame.push(runtime.Int32(int32(len(v.Arr.Elems))))

		case ir.LoadElem:
			idxVal := frame.pop()
			arrVal := frame.pop()
			idx, err := arrayIndex(arrVal, idxVal, "ldelem")
			if err != nil {
				return runtime.Value{}, err
			}
			frame.push(arrVal.Arr.Elems[idx])

		case ir.StoreElem:
			val := frame.pop()
			idxVal := frame.pop()
			arrVal := frame.pop()
			idx, err := arrayIndex(arrVal, idxVal, "stelem")
			if err != nil {
				return runtime.Value{}, err
			}
			arrVal.Arr.Elems[idx] = val.Clone()

		case ir.LoadElemAddr:
			idxVal := frame.pop()
			arrVal := frame.pop()
			idx, err := arrayIndex(arrVal, idxVal, "ldelema")
			if err != nil {
				return runtime.Value{}, err
			}
			frame.push(runtime.RefTo(&arrVal.Arr.Elems[idx]))

		case ir.LoadIndirect:
			ref := frame.pop()
			if ref.Kind != runtime.KindRef || ref.Ref == nil {
				return runtime.Value{}, &runtime.ManagedException{TypeName: "System.NullReferenceException", Message: "dereferencing a null managed pointer (ldind)"}
			}
			frame.push(*ref.Ref)

		case ir.StoreIndirect:
			val := frame.pop()
			ref := frame.pop()
			if ref.Kind != runtime.KindRef || ref.Ref == nil {
				return runtime.Value{}, &runtime.ManagedException{TypeName: "System.NullReferenceException", Message: "dereferencing a null managed pointer (stind)"}
			}
			*ref.Ref = val.Clone()

		case ir.InitObj:
			addr := frame.pop()
			if addr.Kind != runtime.KindRef || addr.Ref == nil {
				return runtime.Value{}, &runtime.ManagedException{TypeName: "System.NullReferenceException", Message: "dereferencing a null managed pointer (initobj)"}
			}
			*addr.Ref = m.defaultValueFor(in.TypeFullName)

		case ir.IsInst:
			v := frame.pop()
			if v.Kind != runtime.KindNull && m.isAssignableTo(v, in.TypeFullName) {
				frame.push(v)
			} else {
				frame.push(runtime.Null())
			}

		case ir.Unbox:
			// See ir.Unbox's own doc comment: vmnet's boxing is already a
			// representation no-op (the value on the stack is already the
			// real KindStruct), so all this needs is an addressable copy
			// for the following ldfld/ldflda/instance call to dereference
			// — same "box a transient value, return a ref to it" pattern
			// spanGetItem (internal/bcl) already uses for a similar need.
			v := frame.pop()
			tmp := v
			frame.push(runtime.RefTo(&tmp))

		case ir.CastClass:
			v := frame.pop()
			if v.Kind == runtime.KindNull || m.isAssignableTo(v, in.TypeFullName) {
				frame.push(v)
			} else {
				return runtime.Value{}, &runtime.ManagedException{
					TypeName: "System.InvalidCastException",
					Message:  fmt.Sprintf("Unable to cast object to type '%s'.", in.TypeFullName),
				}
			}

		case ir.LoadFtn:
			if in.Virtual {
				frame.pop() // ldvirtftn's receiver — see ir.LoadFtn's doc comment
			}
			frame.push(runtime.FuncVal(&runtime.Func{FullName: in.FullName, Virtual: in.Virtual}))

		case ir.LoadTypeToken:
			// IsMethodGenericParam (Fase 3.40, resolved for real as of
			// Fase 3.60): typeof(T) on the enclosing method's own generic
			// parameter — the same IR runs for every different call
			// site's instantiation, so TypeFullName (baked in at IR-build
			// time) is meaningless here; frame.MethodGenericArgs carries
			// THIS specific call's own resolved type argument names
			// instead (populated by tryCall's fallback into an ordinary
			// interpreted method body — see Frame.MethodGenericArgs's own
			// doc comment for the real, load-bearing case this fixed:
			// Microsoft.Extensions.DependencyInjection's own
			// ServiceDescriptor.Singleton<TService,TImplementation>()).
			// Falls back to the old "" degenerate behavior only if this
			// call path genuinely has no generic args available (e.g.
			// reached through New/Invoke/a .cctor run, none of which
			// thread them today) — an out-of-range index defensively
			// does the same, rather than panicking on malformed IR.
			typeName := in.TypeFullName
			if in.IsMethodGenericParam {
				if in.MethodGenericParamIndex >= 0 && in.MethodGenericParamIndex < len(frame.MethodGenericArgs) {
					typeName = frame.MethodGenericArgs[in.MethodGenericParamIndex]
				} else {
					typeName = ""
				}
			}
			if in.IsClassGenericParam {
				// typeof(T) on the ENCLOSING CLASS's own generic parameter
				// (Fase 3.66) — resolved from the CURRENT method's own
				// receiver object (frame.Args[0], always true for an
				// instance method/constructor — the only real shape a
				// class-level generic parameter reference can appear in),
				// whose own ClassGenericArgs were populated at its
				// `newobj` site (see runtime.Object.ClassGenericArgs's own
				// doc comment). Degrades to "" the same way the method-
				// level case above does when unavailable — a static
				// method can't reach here at all (a class-level generic
				// parameter is only meaningful relative to `this`), and
				// neither this project's own IR builder nor real C# ever
				// emits one outside an instance context.
				typeName = ""
				if len(frame.Args) > 0 && frame.Args[0].Kind == runtime.KindObject && frame.Args[0].Obj != nil {
					args := frame.Args[0].Obj.ClassGenericArgs
					if in.ClassGenericParamIndex >= 0 && in.ClassGenericParamIndex < len(args) {
						typeName = args[in.ClassGenericParamIndex]
					}
				}
			}
			frame.push(bcl.NewTypeValue(typeName))

		case ir.LoadFieldToken:
			// No real Value of its own (see LoadFieldToken's doc comment)
			// — a plain string encoding is all RuntimeHelpers.
			// InitializeArray (the only real consumer) needs to look the
			// field back up via Machine.ResolveFieldBytes.
			frame.push(runtime.String(in.TypeFullName + "::" + in.FieldName))

		case ir.LoadMethodToken:
			// Same identity shortcut LoadTypeToken takes for typeof(T)/
			// Type.GetTypeFromHandle: push the real System.Reflection.
			// MethodInfo value directly rather than modeling a
			// RuntimeMethodHandle as its own distinct thing —
			// MethodBase.GetMethodFromHandle (the only real consumer,
			// see LoadMethodToken's own doc comment) is just an identity
			// passthrough over it.
			frame.push(bcl.NewMethodInfoValue(in.TypeFullName, in.MethodName))

		case ir.Leave:
			if finallys := handlersLeaving(method, frame.IP, in.Target); len(finallys) > 0 {
				frame.unwind = &unwind{target: in.Target, pending: finallys[1:]}
				next = finallys[0].HandlerStart
			} else {
				next = in.Target
			}

		case ir.EndFinally:
			resumeIP, propagate, err := m.resumeAfterFinally(frame)
			if err != nil {
				return runtime.Value{}, err
			}
			if propagate != nil {
				return runtime.Value{}, propagate
			}
			next = resumeIP

		case ir.EndFilter:
			verdict := frame.pop()
			resumeIP, propagate, err := m.resumeAfterFilter(frame, verdict)
			if err != nil {
				return runtime.Value{}, err
			}
			if propagate != nil {
				return runtime.Value{}, propagate
			}
			next = resumeIP

		case ir.Rethrow:
			if frame.currentException == nil {
				return runtime.Value{}, errRethrowOutsideCatch
			}
			return runtime.Value{}, frame.currentException

		case ir.Return:
			if in.HasValue {
				return frame.pop(), nil
			}
			return runtime.Value{}, nil

		default:
			return runtime.Value{}, fmt.Errorf("interpreter: unhandled IR instruction %T", method.IR[frame.IP])
		}

		frame.IP = next
	}

	return runtime.Value{}, fmt.Errorf("interpreter: method fell off the end without a ret")
}

func fieldIndex(obj *runtime.Object, name string) int {
	if obj.Type == nil {
		return -1
	}
	return obj.Type.FieldIndex(name)
}

// fieldSlot resolves ldfld/stfld/ldflda's receiver to the addressable
// Value slot backing fieldName: a class instance (receiver is
// KindObject), a value type reached through a managed pointer (receiver
// is KindRef to a KindStruct — this is how a struct's own instance
// methods receive `this`, and how ldflda's own result chains into a
// nested struct field access), or — found via a real example
// (ValueTuple`2's second field read in `t.Item1 + t.Item2`, Fase 3.23) —
// a struct value handed over directly with no managed pointer at all: a
// plain `ldloc`+`ldfld` (not `ldloca`+`ldfld`) is legal per spec
// §III.4.10 and real compiler output does emit it, at least once the
// local's address has already been taken earlier in the same
// expression. Spec §III.4.10/4.28: ldfld/stfld accept all three shapes
// uniformly.
func (m *Machine) fieldSlot(receiver runtime.Value, typeFullName, fieldName string) (*runtime.Value, error) {
	switch receiver.Kind {
	case runtime.KindArray:
		// The classic "array reinterpreted as Pinnable<T>" idiom
		// (`Unsafe.As<Pinnable<T>>(array).Data`, System.Memory's own
		// SpanHelpers.PerTypeValues<T>.MeasureArrayAdjustment) — a real,
		// deliberate unsafe memory-layout trick treating an array's own
		// object reference as if it pointed at a `class Pinnable<T> {
		// public T Data; }`, exploiting the CLR's real object layout to
		// get a byref to element 0 without `fixed`. vmnet's Unsafe.As
		// native (internal/bcl) is an identity passthrough — the array
		// value itself never actually changes shape — so by the time
		// `.Data` reaches here the receiver is still a genuine KindArray.
		// There's no real memory model to reinterpret through in
		// general, but this ONE specific, extremely common idiom maps
		// exactly onto "the array's own first element": Data always
		// means Elems[0] for a Pinnable<T> reinterpretation, by
		// construction of the trick itself.
		if receiver.Arr == nil {
			return nil, &runtime.ManagedException{
				TypeName: "System.NullReferenceException",
				Message:  fmt.Sprintf("Object reference not set to an instance of an object (%s.%s)", typeFullName, fieldName),
			}
		}
		if bcl.GenericOpenName(typeFullName) != "System.Pinnable`1" || fieldName != "Data" {
			return nil, fmt.Errorf("interpreter: %s has no field %q (array received where an object was expected)", typeFullName, fieldName)
		}
		if len(receiver.Arr.Elems) == 0 {
			// A real empty (non-null) array still needs a valid, addressable
			// slot for this trick's "byref to element 0" — real code reaching
			// here (MemoryMarshal.GetReference, SpanHelpers's own
			// MeasureArrayAdjustment probe array, ...) always treats the
			// result as a base pointer for arithmetic a 0-length caller never
			// actually dereferences, never a real read. A throwaway zero
			// value is the same convention spanGetItem already uses for a Go
			// string's own non-addressable runes.
			placeholder := runtime.Value{}
			return &placeholder, nil
		}
		return &receiver.Arr.Elems[0], nil
	case runtime.KindString:
		// The same Pinnable<T>.Data trick (see the KindArray case's doc
		// comment) applied to a string-backed ReadOnlySpan<char> — real
		// CLR strings are just as "pinnable" as an array, and
		// MemoryMarshal.GetReference on a string-backed span goes through
		// this exact idiom. A fresh boxed rune is fine here for the same
		// reason spanGetItem's own KindString branch already boxes one:
		// this result is a transient byref immediately deref'd or used as
		// a bounded-arithmetic base, never retained.
		if bcl.GenericOpenName(typeFullName) != "System.Pinnable`1" || fieldName != "Data" {
			return nil, fmt.Errorf("interpreter: %s has no field %q (string received where an object was expected)", typeFullName, fieldName)
		}
		runes := []rune(receiver.Str)
		var v runtime.Value
		if len(runes) > 0 {
			v = runtime.Int32(runes[0])
		}
		return &v, nil
	case runtime.KindObject:
		if receiver.Obj == nil {
			return nil, &runtime.ManagedException{
				TypeName: "System.NullReferenceException",
				Message:  fmt.Sprintf("Object reference not set to an instance of an object (%s.%s)", typeFullName, fieldName),
			}
		}
		idx := fieldIndex(receiver.Obj, fieldName)
		if idx < 0 {
			return nil, fmt.Errorf("interpreter: %s has no field %q", typeFullName, fieldName)
		}
		return &receiver.Obj.Fields[idx], nil
	case runtime.KindRef:
		if receiver.Ref != nil && receiver.Ref.Kind == runtime.KindNull {
			// A managed pointer to a Null slot that this field access
			// declares to be a value type (Fase 3.43): the one real way
			// this arises is a `static T field;`/`T local;` whose declared
			// type is a still-open generic parameter at IR-build time, so
			// its default(T) seeded Null() instead of a zeroed struct
			// (assembly.go's fieldOrLocalDefault has no T to resolve — the
			// same shared-statics-per-generic-class limitation
			// attribute_metadata.go documents). Found via a real,
			// load-bearing case reading a real .xlsx through ClosedXML
			// 0.105.0: Slice<TElement>.Lut<T>'s own `private static
			// readonly T DefaultValue;` (decompiled ClosedXML.Excel/
			// Slice.cs:356) is handed out BY REFERENCE (`return ref
			// DefaultValue`, Slice.cs:378-390) for every unused cell slot,
			// and ValueSlice.GetCellValue immediately reads `.Type`/`.Value`
			// through that ref (ValueSlice.cs:111-113). The declared
			// typeFullName at THIS access is the real closed value type
			// (XLValueSliceContent), so a zeroed struct of it — exactly the
			// default(T) the real CLR would have materialized eagerly — is
			// substituted as the read target. A fresh TRANSIENT struct,
			// deliberately NOT written back through the ref: the slot it
			// points at is the one static shared across every closed
			// instantiation of the generic class (the same limitation
			// above), and a different instantiation may legitimately need
			// that same slot to stay a null CLASS reference (Slice's own
			// outer Lut<Lut<TElement>> reads DefaultValue as a nullable
			// Lut<TElement> and null-checks it, Slice.cs:539) — writing a
			// struct into it corrupts those readers, found the hard way on
			// the first version of this fix. Reads through the transient
			// see all-zero fields (the correct answer); real code never
			// writes through this `ref readonly` handout. Non-value-type
			// declared names stay on the NRE path below (defaultValueFor
			// returns Null() for them).
			if def := m.defaultValueFor(typeFullName); def.Kind == runtime.KindStruct {
				receiver = def
				idx := receiver.Struct.Type.FieldIndex(fieldName)
				if idx < 0 {
					return nil, fmt.Errorf("interpreter: %s has no field %q", typeFullName, fieldName)
				}
				return &receiver.Struct.Fields[idx], nil
			}
		}
		if receiver.Ref == nil || receiver.Ref.Kind != runtime.KindStruct || receiver.Ref.Struct == nil {
			return nil, &runtime.ManagedException{
				TypeName: "System.NullReferenceException",
				Message:  fmt.Sprintf("dereferencing a null managed pointer (%s.%s)", typeFullName, fieldName),
			}
		}
		s := receiver.Ref.Struct
		idx := s.Type.FieldIndex(fieldName)
		if idx < 0 {
			return nil, fmt.Errorf("interpreter: %s has no field %q", typeFullName, fieldName)
		}
		return &s.Fields[idx], nil
	case runtime.KindStruct:
		if receiver.Struct == nil {
			return nil, &runtime.ManagedException{
				TypeName: "System.NullReferenceException",
				Message:  fmt.Sprintf("Object reference not set to an instance of an object (%s.%s)", typeFullName, fieldName),
			}
		}
		idx := receiver.Struct.Type.FieldIndex(fieldName)
		if idx < 0 {
			return nil, fmt.Errorf("interpreter: %s has no field %q", typeFullName, fieldName)
		}
		return &receiver.Struct.Fields[idx], nil
	default:
		return nil, &runtime.ManagedException{
			TypeName: "System.NullReferenceException",
			Message:  fmt.Sprintf("Object reference not set to an instance of an object (%s.%s)", typeFullName, fieldName),
		}
	}
}

// resolveForwardedGenericArgs substitutes any "!!N" or "!N" sentinel
// appearing ANYWHERE within each of callArgs (ir.methodSpecGenericArgNames/
// ir.sigTypeFullNameGenericArg's own encoding for "this forwards the
// enclosing context's own Nth generic type parameter, unresolvable at
// IR-build time" — Fase 3.60, extended in Fase 3.81) with that
// parameter's real, closed name — resolved fresh at every single
// execution of this call site, since the same static IR runs for every
// different calling instantiation. "!!N" (a method-level/MVAR forward)
// resolves against methodArgs — the CURRENTLY EXECUTING frame's own
// MethodGenericArgs. "!N" (a class-level/VAR forward) resolves against
// classArgs — the CURRENTLY EXECUTING frame's own receiver object's
// ClassGenericArgs (see frameClassGenericArgs) — the shape a compiler-
// generated iterator/async state machine produces: its MoveNext() is not
// itself generic, but is declared on a generic class that closes over
// the original method's own type parameter as a CLASS-level one, so a
// call from inside MoveNext() back out to another generic method using
// that same type parameter forwards it as "!N", not "!!N".
//
// A SUBSTRING scan, not a whole-string match, because the sentinel can
// appear NESTED inside a larger closed generic name — e.g. CsvHelper's
// own CsvContext.AutoMap<T>() forwarding T into `ObjectResolver.Current.
// Resolve<DefaultClassMap<T>>()`, a MethodSpec instantiated with
// `DefaultClassMap`1[[!!0]]`, not a bare `!!0` on its own (see
// ir.sigTypeFullNameGenericArg's own doc comment for why the sentinel
// survives that nesting in the first place). A sentinel with no
// corresponding entry (index out of range, or the args slice nil — this
// call wasn't itself reached with any generic args to forward) degrades
// to substituting "" for just that sentinel, rather than panicking,
// matching every other unresolvable-generic-argument case elsewhere in
// this project. Returns callArgs unchanged (no allocation) when none of
// them contain a "!" at all — the overwhelming majority of calls,
// generic or not.
func resolveForwardedGenericArgs(callArgs, methodArgs, classArgs []string) []string {
	var out []string
	for i, a := range callArgs {
		if !strings.Contains(a, "!") {
			continue
		}
		replaced := replaceGenericParamSentinels(a, methodArgs, classArgs)
		if replaced == a {
			continue
		}
		if out == nil {
			out = append([]string(nil), callArgs...)
		}
		out[i] = replaced
	}
	if out == nil {
		return callArgs
	}
	return out
}

// replaceGenericParamSentinels scans s left to right, substituting every
// "!!N" (method generic param, resolved against methodArgs) and "!N"
// (class generic param, resolved against classArgs) occurrence with its
// real value — see resolveForwardedGenericArgs's own doc comment for why
// this needs to be a substring scan rather than a whole-string match.
// "!!" is tried before "!" at each position so a method-level sentinel is
// never misparsed as a class-level one followed by a stray "!". Any
// other "!"-led run that isn't immediately followed by digits (never
// produced by this project's own IR builder, but defensively handled
// rather than panicking on malformed input) is copied through verbatim.
func replaceGenericParamSentinels(s string, methodArgs, classArgs []string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] != '!' {
			b.WriteByte(s[i])
			i++
			continue
		}
		if rest := s[i:]; strings.HasPrefix(rest, "!!") {
			j := i + 2
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			if j > i+2 {
				if idx, err := strconv.Atoi(s[i+2 : j]); err == nil && idx >= 0 && idx < len(methodArgs) {
					b.WriteString(methodArgs[idx])
				}
				i = j
				continue
			}
		}
		j := i + 1
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j > i+1 {
			if idx, err := strconv.Atoi(s[i+1 : j]); err == nil && idx >= 0 && idx < len(classArgs) {
				b.WriteString(classArgs[idx])
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// frameClassGenericArgs returns the currently executing frame's own
// receiver object's ClassGenericArgs (nil if the receiver isn't a real
// object — a static method, or unavailable for any other reason) — see
// resolveForwardedGenericArgs's own doc comment for why a `!N` class-
// level generic parameter forward needs this instead of
// frame.MethodGenericArgs.
func frameClassGenericArgs(frame *Frame) []string {
	if len(frame.Args) > 0 && frame.Args[0].Kind == runtime.KindObject && frame.Args[0].Obj != nil {
		return frame.Args[0].Obj.ClassGenericArgs
	}
	return nil
}

// arrayIndex validates an ldelem/stelem array+index pair, returning a
// managed IndexOutOfRangeException/NullReferenceException — matching real
// CIL semantics — instead of a Go panic on out-of-bounds access.
func arrayIndex(arrVal, idxVal runtime.Value, op string) (int, error) {
	if arrVal.Kind != runtime.KindArray || arrVal.Arr == nil {
		return 0, &runtime.ManagedException{TypeName: "System.NullReferenceException", Message: fmt.Sprintf("array reference is null (%s)", op)}
	}
	if idxVal.Kind != runtime.KindI4 {
		return 0, fmt.Errorf("interpreter: %s index must be int32", op)
	}
	idx := int(idxVal.I4)
	if idx < 0 || idx >= len(arrVal.Arr.Elems) {
		return 0, &runtime.ManagedException{
			TypeName: "System.IndexOutOfRangeException",
			Message:  fmt.Sprintf("index %d is out of range (length %d)", idx, len(arrVal.Arr.Elems)),
		}
	}
	return idx, nil
}
