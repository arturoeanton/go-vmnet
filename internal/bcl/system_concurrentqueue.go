package bcl

import (
	"fmt"
	"sync"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// nativeConcurrentQueue backs System.Collections.Concurrent.ConcurrentQueue
// <T> (Fase 3.61, found via Markdig's own ConcurrentQueueExtensions.Clear
// call) — mirrors nativeQueue (system_queue.go) exactly, plus a mutex: a
// real ConcurrentQueue<T> is most often reached through a static field
// (a shared object-pool pattern), and Assembly.Call is documented safe
// for concurrent goroutines (unlike Queue<T>, which real code never
// shares across threads without its own external locking, so
// nativeQueue itself stays lock-free).
type nativeConcurrentQueue struct {
	mu    sync.Mutex
	items []runtime.Value
}

var concurrentQueueEnumeratorType = runtime.NewValueType(
	"System.Collections.Concurrent", "ConcurrentQueue`1+Enumerator",
	[]string{"queue", "index"},
	[]runtime.Value{runtime.Null(), runtime.Int32(-1)},
)

func init() {
	registerCtor("System.Collections.Concurrent.ConcurrentQueue`1", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeConcurrentQueue{}}, nil
	})
	register("System.Collections.Concurrent.ConcurrentQueue`1::Enqueue", false, concurrentQueueEnqueue)
	register("System.Collections.Concurrent.ConcurrentQueue`1::get_Count", true, concurrentQueueCount)
	register("System.Collections.Concurrent.ConcurrentQueue`1::get_IsEmpty", true, concurrentQueueIsEmpty)
	register("System.Collections.Concurrent.ConcurrentQueue`1::TryDequeue", true, concurrentQueueTryDequeue)
	register("System.Collections.Concurrent.ConcurrentQueue`1::TryPeek", true, concurrentQueueTryPeek)
	register("System.Collections.Concurrent.ConcurrentQueue`1::Clear", false, concurrentQueueClear)
	register("System.Collections.Concurrent.ConcurrentQueue`1::GetEnumerator", true, concurrentQueueGetEnumerator)
	register("System.Collections.Concurrent.ConcurrentQueue`1+Enumerator::MoveNext", true, concurrentQueueEnumeratorMoveNext)
	register("System.Collections.Concurrent.ConcurrentQueue`1+Enumerator::get_Current", true, concurrentQueueEnumeratorGetCurrent)
	// ConcurrentQueueExtensions.Clear(this ConcurrentQueue<T>) — a real
	// static extension method .NET added alongside the instance Clear()
	// above for netstandard2.0 targets that predate it; same effect,
	// registered separately since it's a distinct real MethodDef.
	register("System.Collections.Concurrent.ConcurrentQueueExtensions::Clear", false, concurrentQueueClear)
}

func asConcurrentQueue(args []runtime.Value) (*nativeConcurrentQueue, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return nil, fmt.Errorf("bcl: ConcurrentQueue method called without a receiver")
	}
	q, ok := args[0].Obj.Native.(*nativeConcurrentQueue)
	if !ok {
		return nil, fmt.Errorf("bcl: receiver is not a ConcurrentQueue")
	}
	return q, nil
}

func concurrentQueueEnqueue(args []runtime.Value) (runtime.Value, error) {
	q, err := asConcurrentQueue(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: ConcurrentQueue.Enqueue expects 1 argument")
	}
	q.mu.Lock()
	q.items = append(q.items, args[1])
	q.mu.Unlock()
	return runtime.Value{}, nil
}

func concurrentQueueCount(args []runtime.Value) (runtime.Value, error) {
	q, err := asConcurrentQueue(args)
	if err != nil {
		return runtime.Value{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return runtime.Int32(int32(len(q.items))), nil
}

func concurrentQueueIsEmpty(args []runtime.Value) (runtime.Value, error) {
	q, err := asConcurrentQueue(args)
	if err != nil {
		return runtime.Value{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return runtime.Bool(len(q.items) == 0), nil
}

func concurrentQueueTryDequeue(args []runtime.Value) (runtime.Value, error) {
	q, err := asConcurrentQueue(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: ConcurrentQueue.TryDequeue expects an out param")
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return runtime.Bool(false), nil
	}
	front := q.items[0]
	q.items = q.items[1:]
	if args[1].Kind == runtime.KindRef && args[1].Ref != nil {
		*args[1].Ref = front
	}
	return runtime.Bool(true), nil
}

func concurrentQueueTryPeek(args []runtime.Value) (runtime.Value, error) {
	q, err := asConcurrentQueue(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: ConcurrentQueue.TryPeek expects an out param")
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return runtime.Bool(false), nil
	}
	if args[1].Kind == runtime.KindRef && args[1].Ref != nil {
		*args[1].Ref = q.items[0]
	}
	return runtime.Bool(true), nil
}

func concurrentQueueClear(args []runtime.Value) (runtime.Value, error) {
	q, err := asConcurrentQueue(args)
	if err != nil {
		return runtime.Value{}, err
	}
	q.mu.Lock()
	q.items = nil
	q.mu.Unlock()
	return runtime.Value{}, nil
}

// concurrentQueueGetEnumerator takes a live snapshot (copies items under
// the lock, then iterates the copy) — the closest honest match to real
// ConcurrentQueue<T>.GetEnumerator's own documented "weakly consistent"
// snapshot semantics (never throws on concurrent modification, unlike
// Queue<T>'s own live, non-snapshot enumerator, system_queue.go), without
// needing to hold the lock for the whole enumeration.
func concurrentQueueGetEnumerator(args []runtime.Value) (runtime.Value, error) {
	q, err := asConcurrentQueue(args)
	if err != nil {
		return runtime.Value{}, err
	}
	q.mu.Lock()
	snapshot := append([]runtime.Value(nil), q.items...)
	q.mu.Unlock()
	s := runtime.NewStruct(concurrentQueueEnumeratorType)
	s.Fields[0] = runtime.ObjRef(&runtime.Object{Native: &nativeConcurrentQueue{items: snapshot}})
	return runtime.StructVal(s), nil
}

func concurrentQueueEnumeratorMoveNext(args []runtime.Value) (runtime.Value, error) {
	s, err := derefStructReceiver(args, "ConcurrentQueue.Enumerator", "ConcurrentQueue.Enumerator.MoveNext")
	if err != nil {
		return runtime.Value{}, err
	}
	snap, err := asConcurrentQueue([]runtime.Value{s.Fields[0]})
	if err != nil {
		return runtime.Value{}, err
	}
	next := s.Fields[1].I4 + 1
	s.Fields[1] = runtime.Int32(next)
	return runtime.Bool(int(next) < len(snap.items)), nil
}

func concurrentQueueEnumeratorGetCurrent(args []runtime.Value) (runtime.Value, error) {
	s, err := derefStructReceiver(args, "ConcurrentQueue.Enumerator", "ConcurrentQueue.Enumerator.Current")
	if err != nil {
		return runtime.Value{}, err
	}
	snap, err := asConcurrentQueue([]runtime.Value{s.Fields[0]})
	if err != nil {
		return runtime.Value{}, err
	}
	idx := int(s.Fields[1].I4)
	if idx < 0 || idx >= len(snap.items) {
		return runtime.Value{}, fmt.Errorf("bcl: ConcurrentQueue.Enumerator.Current: index %d out of range", idx)
	}
	return snap.items[idx], nil
}
