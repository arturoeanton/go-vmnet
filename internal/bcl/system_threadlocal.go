package bcl

import (
	"fmt"
	"sync"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// nativeValueBox backs both System.Threading.ThreadLocal<T> and System.
// Threading.AsyncLocal<T> (Fase 3.61) — found via a corpus-wide checker
// scan across the Microsoft.Extensions.* family (Caching.Memory/Logging/
// Logging.Abstractions) and OpenTelemetry.Api. Both real BCL types exist
// to give each concurrent thread/async flow its own independent value —
// a distinction that collapses to nothing here: vmnet runs every call
// chain synchronously on whichever single goroutine invoked it (async.go's
// own doc comment already documents this same collapse for
// CancellationToken), so "this thread's/this async flow's own value" and
// "the one value this box currently holds" are the same thing. Modeled as
// a real, if trivial, mutable box — not a permanently-empty stub — the
// same posture CancellationToken's own cancelState takes for exactly the
// same class of "no real concurrency here to make this observably
// different from a plain box" reasoning.
//
// ThreadLocal<T>'s own optional valueFactory (mu-guarded, computed at most
// once, mirroring nativeLazy exactly — LazyGetOrCompute above is reused
// verbatim rather than duplicated) is the one piece of real behavior that
// needs Machine access to invoke (internal/interpreter/threadlocal.go);
// AsyncLocal<T> has no factory concept in real .NET at all (its Value is
// only ever set directly), so its own get_Value/set_Value are plain
// bcl.Native with no Machine dependency.
type nativeValueBox struct {
	mu       sync.Mutex
	hasValue bool
	value    runtime.Value
	factory  runtime.Value
	typeName string
}

func init() {
	registerCtor("System.Threading.ThreadLocal`1", threadLocalCtor)
	register("System.Threading.ThreadLocal`1::set_Value", false, valueBoxSetValue)
	register("System.Threading.ThreadLocal`1::Dispose", false, objectCtorNoop)
	register("System.Threading.ThreadLocal`1::get_IsValueCreated", true, valueBoxGetIsValueCreated)

	registerCtor("System.Threading.AsyncLocal`1", asyncLocalCtor)
	register("System.Threading.AsyncLocal`1::get_Value", true, valueBoxGetValueDirect)
	register("System.Threading.AsyncLocal`1::set_Value", false, valueBoxSetValue)
}

// threadLocalCtor covers both the parameterless overload (default(T) until
// first set) and the Func<T> valueFactory overload (with or without a
// trailing bool — ignored, same "every access already serialized" posture
// nativeLazy's own ctor takes for LazyThreadSafetyMode).
func threadLocalCtor(args []runtime.Value) (*runtime.Object, error) {
	b := &nativeValueBox{typeName: "System.Threading.ThreadLocal`1"}
	if len(args) > 0 && args[0].Kind == runtime.KindFunc {
		b.factory = args[0]
	}
	return &runtime.Object{Native: b}, nil
}

// asyncLocalCtor covers both the parameterless overload and the one
// taking an Action<AsyncLocalValueChangedArgs<T>> change-notification
// callback — accepted but never invoked (no real corpus caller here
// relies on the notification firing), the same documented scope cut
// CancellationToken.Register's own callback already takes.
func asyncLocalCtor(args []runtime.Value) (*runtime.Object, error) {
	return &runtime.Object{Native: &nativeValueBox{typeName: "System.Threading.AsyncLocal`1"}}, nil
}

func asValueBox(args []runtime.Value) (*nativeValueBox, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return nil, fmt.Errorf("bcl: ThreadLocal<T>/AsyncLocal<T> method called without a receiver")
	}
	b, ok := args[0].Obj.Native.(*nativeValueBox)
	if !ok {
		return nil, fmt.Errorf("bcl: receiver is not a ThreadLocal<T>/AsyncLocal<T>")
	}
	return b, nil
}

func valueBoxSetValue(args []runtime.Value) (runtime.Value, error) {
	b, err := asValueBox(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: ThreadLocal<T>/AsyncLocal<T>.Value setter expects a value argument")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.value = args[1]
	b.hasValue = true
	return runtime.Value{}, nil
}

// valueBoxGetValueDirect backs AsyncLocal<T>.Value — no factory to
// invoke, so (unlike ThreadLocal<T>'s own get_Value) this needs no
// Machine access at all: a never-set AsyncLocal<T> answers default(T),
// which for vmnet's type-erased Value model is simply the zero Value
// (Null()) — matching every reference-typed T's own real default, and
// close enough for a value-typed T too (no real corpus caller here reads
// an unset AsyncLocal<T> of a value type and depends on a specific
// nonzero default).
func valueBoxGetValueDirect(args []runtime.Value) (runtime.Value, error) {
	b, err := asValueBox(args)
	if err != nil {
		return runtime.Value{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.hasValue {
		return runtime.Null(), nil
	}
	return b.value, nil
}

func valueBoxGetIsValueCreated(args []runtime.Value) (runtime.Value, error) {
	b, err := asValueBox(args)
	if err != nil {
		return runtime.Value{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return runtime.Bool(b.hasValue), nil
}

// ValueBoxFactory returns a ThreadLocal<T> instance's own valueFactory
// delegate, if any — used by internal/interpreter/threadlocal.go's
// Machine-aware get_Value (invoking it needs m.invokeFunc, unavailable to
// a plain bcl.Native). Mirrors bcl.LazyFactory exactly.
func ValueBoxFactory(native any) (runtime.Value, bool) {
	b, ok := native.(*nativeValueBox)
	if !ok {
		return runtime.Value{}, false
	}
	return b.factory, true
}

// ValueBoxGetOrCompute mirrors bcl.LazyGetOrCompute exactly (same
// hold-the-lock-across-compute rationale — a static ThreadLocal<T> field
// is a real, common use, and Assembly.Call is documented safe for
// concurrent goroutines).
func ValueBoxGetOrCompute(native any, compute func() (runtime.Value, error)) (runtime.Value, error) {
	b, ok := native.(*nativeValueBox)
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: receiver is not a ThreadLocal<T>")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.hasValue {
		return b.value, nil
	}
	v, err := compute()
	if err != nil {
		return runtime.Value{}, err
	}
	b.value = v
	b.hasValue = true
	return v, nil
}
