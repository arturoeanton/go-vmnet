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
// docs/ROADMAP.md Fase 3.9.
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
}

// BindDelegate combines an unbound Func (from ldftn/ldvirtftn) with the
// receiver popped just before it in a delegate constructor call
// (KindNull for a static method target) — see calls.go's newObj.
func BindDelegate(receiver Value, fn Func) Value {
	if receiver.Kind != KindNull {
		r := receiver.Clone()
		fn.Receiver = &r
	}
	return FuncVal(&fn)
}
