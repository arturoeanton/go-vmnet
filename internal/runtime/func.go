package runtime

// Func is a delegate value (Fase 3.9): a resolved target method's full
// name ("Namespace.Type::Method") plus its captured receiver, if any.
//
// This deliberately doesn't try to model System.Delegate/
// MulticastDelegate as real BCL types — every delegate type (Action,
// Func`2, a user's own `delegate` declaration) compiles its construction
// down to the exact same low-level shape regardless of name: `ldftn`
// pushes an unbound Func, and `newobj SomeDelegateType::.ctor(object,
// native int)` combines it with a receiver (null for a static method
// target) into one of these. Detecting that shape structurally
// (internal/interpreter/calls.go's newObj) means Action<T>/Func<T,TResult>
// / any custom delegate all work without per-type registration — see
// docs/en/ROADMAP.md Fase 3.9.
//
// A closure (a lambda capturing outer locals) needs no special handling
// beyond this: the C# compiler itself lowers the capture into a
// compiler-generated class holding the captured variables as fields and
// the lambda body as an instance method on it, so Receiver ends up being
// a perfectly ordinary runtime.Object — vmnet's existing object/field
// machinery already does the rest.
type Func struct {
	FullName string
	Receiver *Value // nil for a static-method target
	// Chain holds additional targets appended by Delegate.Combine (Fase
	// 3.24): a multicast delegate invokes itself, then each entry in
	// Chain, in order — matching real MulticastDelegate.Invoke, which
	// runs every combined target and (for a non-void delegate) discards
	// every result but the last.
	Chain []*Func
	// DelegateTypeName is the real delegate type this Func was bound as
	// (Fase 3.40) — "" if unset. Every delegate collapses to this same
	// Func shape regardless of its declared type (Action`2, a custom
	// `delegate`, ...), which is exactly what makes ldftn/newobj-based
	// construction type-agnostic (see this type's own top doc comment) —
	// but it also meant overload resolution had no way to tell two
	// different delegate-typed parameters apart (pickMethodOverload's
	// exact-type-name scoring only ever looked at KindObject/KindStruct
	// values). Found via a real, load-bearing case: DocumentFormat.
	// OpenXml's own OpenXmlPackageBuilderExtensions declares three
	// same-arity "Use" overloads differing only in their delegate
	// parameter's real type (PackageInitializerDelegate`1 vs
	// System.Action`2 vs a Func`2) — without this, one overload's own
	// body calling `builder.Use(newDelegate)` to reach a DIFFERENT
	// overload instead resolved back onto itself, recursing forever.
	DelegateTypeName string
	// Virtual records whether the FullName this Func wraps came from
	// ldvirtftn rather than plain ldftn (Fase 3.40) — a method-group
	// conversion of a virtual/abstract instance method
	// (`new Func<T>(builder.Create)` where Create is `abstract`, e.g.
	// DocumentFormat.OpenXml.Builder's own OpenXmlPackageBuilder<T>)
	// needs the SAME virtual-dispatch chain-walk a normal callvirt site
	// gets (Machine.call's own concrete-type-first search), or invoking
	// it just calls straight into the abstract declaration itself, which
	// has no body at all. A plain ldftn target, by contrast, really is
	// always the exact bound method regardless of the receiver's runtime
	// type — see ir.LoadFtn's own doc comment for why the receiver
	// itself is otherwise irrelevant to which FullName this wraps.
	Virtual bool
}

// BindDelegate combines an unbound Func (from ldftn/ldvirtftn) with the
// receiver popped just before it in a delegate constructor call
// (KindNull for a static method target), tagged with the constructing
// newobj's own delegate type name — see calls.go's newObj.
func BindDelegate(receiver Value, fn Func, delegateTypeName string) Value {
	if receiver.Kind != KindNull {
		r := receiver.Clone()
		fn.Receiver = &r
	}
	fn.DelegateTypeName = delegateTypeName
	return FuncVal(&fn)
}
