package bcl

import (
	"fmt"
	"sync"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// nativeLazy backs System.Lazy<T>: factory is set once at construction
// (immutable afterward, safe to read without locking); hasValue/value
// are guarded by mu so concurrent Value accesses on the same instance —
// a real scenario, since Lazy<T> is overwhelmingly used for shared
// static fields, and an Assembly's methods may run on multiple
// goroutines at once (see Assembly's own doc comment) — serialize
// through the factory exactly once, matching Lazy<T>'s default
// (ExecutionAndPublication) thread-safety mode, not a documented
// approximation of it. See LazyGetOrCompute (internal/interpreter/
// lazy.go, Fase 3.17) for why computing the value itself needs Machine
// access and can't happen in this package.
type nativeLazy struct {
	mu       sync.Mutex
	hasValue bool
	value    runtime.Value
	factory  runtime.Value
}

func init() {
	registerCtor("System.Lazy`1", lazyCtor)
	register("System.Lazy`1::get_IsValueCreated", true, lazyGetIsValueCreated)
}

// lazyCtor covers the Func<T>-factory overloads (with or without a
// trailing bool/LazyThreadSafetyMode argument, ignored — every access is
// already serialized through nativeLazy.mu regardless of the requested
// mode). The parameterless/bool-only overloads (which construct T via
// Activator.CreateInstance<T>()) aren't covered — vmnet has no generic
// type-argument info at this call site to know what T even is; get_Value
// on such an instance errors rather than guessing, same "no native
// implementation" outcome as before this fase for that specific overload.
func lazyCtor(args []runtime.Value) (*runtime.Object, error) {
	l := &nativeLazy{}
	if len(args) > 0 && args[0].Kind == runtime.KindFunc {
		l.factory = args[0]
	}
	return &runtime.Object{Native: l}, nil
}

func asLazy(args []runtime.Value) (*nativeLazy, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return nil, fmt.Errorf("bcl: Lazy<T> method called without a receiver")
	}
	l, ok := args[0].Obj.Native.(*nativeLazy)
	if !ok {
		return nil, fmt.Errorf("bcl: receiver is not a Lazy<T>")
	}
	return l, nil
}

func lazyGetIsValueCreated(args []runtime.Value) (runtime.Value, error) {
	l, err := asLazy(args)
	if err != nil {
		return runtime.Value{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return runtime.Bool(l.hasValue), nil
}

// LazyFactory returns a Lazy<T> instance's factory delegate — used by
// internal/interpreter/lazy.go's Machine-aware get_Value (invoking the
// factory needs m.invokeFunc, unavailable to a plain bcl.Native). Safe to
// read without locking: factory is set once at construction and never
// mutated afterward.
func LazyFactory(native any) (runtime.Value, bool) {
	l, ok := native.(*nativeLazy)
	if !ok {
		return runtime.Value{}, false
	}
	return l.factory, true
}

// LazyGetOrCompute returns a Lazy<T> instance's cached value, or calls
// compute (with the instance's own lock held for the whole call, not
// just around the check) to produce and cache it. Holding the lock
// across compute — rather than releasing it, computing, then
// re-acquiring to store — is what makes two goroutines racing to read
// the same Lazy<T>.Value for the first time serialize into "one computes,
// the other blocks and then observes the same cached result" instead of
// "both compute, one cached value silently wins" (a real bug class, not
// hypothetical: static Lazy<T> fields are Lazy<T>'s primary real-world
// use, and Assembly.Call is documented safe for concurrent goroutines).
func LazyGetOrCompute(native any, compute func() (runtime.Value, error)) (runtime.Value, error) {
	l, ok := native.(*nativeLazy)
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: receiver is not a Lazy<T>")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hasValue {
		return l.value, nil
	}
	v, err := compute()
	if err != nil {
		return runtime.Value{}, err
	}
	l.value = v
	l.hasValue = true
	return v, nil
}
