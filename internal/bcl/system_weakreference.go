package bcl

import "github.com/arturoeanton/go-vmnet/internal/runtime"

// System.WeakReference is backed by a plain strong reference (Fase
// 3.40) — vmnet has no way to hook into Go's own GC to observe when an
// object becomes otherwise-unreachable, and no caller in this loop's
// target packages depends on the target ever actually being collected
// (found via a real, load-bearing case: ClosedXML's own style-value
// repositories cache interned values in a
// ConcurrentDictionary<TKey,WeakReference> purely as a memory
// optimization — ClosedXML's own logic already re-checks IsAlive/Target
// before trusting a cached entry, so "never collected" is a safe, if
// less memory-efficient, over-approximation, same conservative choice
// simple GC-less interpreters commonly make for WeakReference).
type nativeWeakReference struct {
	target runtime.Value
}

func init() {
	registerCtor("System.WeakReference", func(args []runtime.Value) (*runtime.Object, error) {
		wr := &nativeWeakReference{}
		if len(args) > 0 {
			wr.target = args[0]
		}
		return &runtime.Object{Native: wr}, nil
	})
	register("System.WeakReference::get_Target", true, weakReferenceGetTarget)
	register("System.WeakReference::set_Target", false, weakReferenceSetTarget)
	register("System.WeakReference::get_IsAlive", true, weakReferenceGetIsAlive)
	register("System.WeakReference::TryGetTarget", true, weakReferenceTryGetTarget)
	register("System.WeakReference::SetTarget", false, weakReferenceSetTargetGeneric)

	// WeakReference`1 (WeakReference<T>) is a distinct, real BCL type
	// from the non-generic WeakReference above, but shares the same
	// always-alive backing and native struct.
	registerCtor("System.WeakReference`1", func(args []runtime.Value) (*runtime.Object, error) {
		wr := &nativeWeakReference{}
		if len(args) > 0 {
			wr.target = args[0]
		}
		return &runtime.Object{Native: wr}, nil
	})
	register("System.WeakReference`1::TryGetTarget", true, weakReferenceTryGetTarget)
	register("System.WeakReference`1::SetTarget", false, weakReferenceSetTargetGeneric)
}

func asWeakReference(args []runtime.Value) (*nativeWeakReference, bool) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return nil, false
	}
	wr, ok := args[0].Obj.Native.(*nativeWeakReference)
	return wr, ok
}

func weakReferenceGetTarget(args []runtime.Value) (runtime.Value, error) {
	wr, ok := asWeakReference(args)
	if !ok {
		return runtime.Null(), nil
	}
	return wr.target, nil
}

func weakReferenceSetTarget(args []runtime.Value) (runtime.Value, error) {
	wr, ok := asWeakReference(args)
	if !ok || len(args) < 2 {
		return runtime.Value{}, nil
	}
	wr.target = args[1]
	return runtime.Value{}, nil
}

func weakReferenceGetIsAlive(args []runtime.Value) (runtime.Value, error) {
	wr, ok := asWeakReference(args)
	if !ok {
		return runtime.Bool(false), nil
	}
	return runtime.Bool(wr.target.Kind != runtime.KindNull), nil
}

// weakReferenceTryGetTarget backs WeakReference<T>.TryGetTarget(out T) —
// the generic sibling of the non-generic WeakReference above, same
// always-alive backing.
func weakReferenceTryGetTarget(args []runtime.Value) (runtime.Value, error) {
	wr, ok := asWeakReference(args)
	if !ok || len(args) < 2 || args[1].Kind != runtime.KindRef || args[1].Ref == nil {
		return runtime.Bool(false), nil
	}
	*args[1].Ref = wr.target
	return runtime.Bool(wr.target.Kind != runtime.KindNull), nil
}

func weakReferenceSetTargetGeneric(args []runtime.Value) (runtime.Value, error) {
	return weakReferenceSetTarget(args)
}
