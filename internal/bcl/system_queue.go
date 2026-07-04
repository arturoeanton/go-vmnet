package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// nativeQueue backs Queue<T> — items[0] is the front, matching Enqueue/
// Dequeue/Peek's FIFO order directly off a Go slice append/reslice.
type nativeQueue struct {
	items []runtime.Value
}

// queueEnumeratorType mirrors listEnumeratorType/hashSetEnumeratorType
// exactly, under real Queue<T>.Enumerator's own real struct name —
// getting this name wrong is a real, probed bug, not a style choice: a
// direct `foreach (var x in queue)` compiles to a non-virtual `call` on
// this EXACT concrete struct type's own MoveNext/get_Current (the C#
// compiler statically knows Queue<T>.GetEnumerator() returns this real
// struct, so it skips virtual/interface dispatch entirely) — an earlier
// version of this fix returned listEnumeratorType's own struct instead
// (delegating to List<T>'s existing enumerator machinery on a snapshot
// copy), which happened to satisfy LINQ's own enumerateAll (drives
// MoveNext/get_Current through the interface names, virtual=true, so it
// never noticed the type mismatch) but broke plain `foreach` outright:
// the compiled call site names "Queue`1+Enumerator::MoveNext" literally,
// and vmnet has no vtable to redirect a non-virtual `call` by the
// receiver's actual runtime shape the way callvirt's own fallback does.
var queueEnumeratorType = runtime.NewValueType(
	"System.Collections.Generic", "Queue`1+Enumerator",
	[]string{"queue", "index"},
	[]runtime.Value{runtime.Null(), runtime.Int32(-1)},
)

func init() {
	registerCtor("System.Collections.Generic.Queue`1", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeQueue{}}, nil
	})
	register("System.Collections.Generic.Queue`1::Enqueue", false, queueEnqueue)
	register("System.Collections.Generic.Queue`1::Dequeue", true, queueDequeue)
	register("System.Collections.Generic.Queue`1::Peek", true, queuePeek)
	register("System.Collections.Generic.Queue`1::get_Count", true, queueCount)
	register("System.Collections.Generic.Queue`1::TryDequeue", true, queueTryDequeue)
	register("System.Collections.Generic.Queue`1::TryPeek", true, queueTryPeek)
	register("System.Collections.Generic.Queue`1::GetEnumerator", true, queueGetEnumerator)
	register("System.Collections.Generic.Queue`1+Enumerator::MoveNext", true, queueEnumeratorMoveNext)
	register("System.Collections.Generic.Queue`1+Enumerator::get_Current", true, queueEnumeratorGetCurrent)
}

func asQueue(args []runtime.Value) (*nativeQueue, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return nil, fmt.Errorf("bcl: Queue method called without a receiver")
	}
	q, ok := args[0].Obj.Native.(*nativeQueue)
	if !ok {
		return nil, fmt.Errorf("bcl: receiver is not a Queue")
	}
	return q, nil
}

func queueEnqueue(args []runtime.Value) (runtime.Value, error) {
	q, err := asQueue(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: Queue.Enqueue expects 1 argument")
	}
	q.items = append(q.items, args[1])
	return runtime.Value{}, nil
}

func queueDequeue(args []runtime.Value) (runtime.Value, error) {
	q, err := asQueue(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(q.items) == 0 {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.InvalidOperationException", Message: "Queue empty."}
	}
	front := q.items[0]
	q.items = q.items[1:]
	return front, nil
}

func queuePeek(args []runtime.Value) (runtime.Value, error) {
	q, err := asQueue(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(q.items) == 0 {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.InvalidOperationException", Message: "Queue empty."}
	}
	return q.items[0], nil
}

func queueCount(args []runtime.Value) (runtime.Value, error) {
	q, err := asQueue(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(int32(len(q.items))), nil
}

// queueTryDequeue backs Queue<T>.TryDequeue(out T result) — same `out`-
// by-managed-pointer mechanism as Int32.TryParse/Dictionary.TryGetValue.
func queueTryDequeue(args []runtime.Value) (runtime.Value, error) {
	q, err := asQueue(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: Queue.TryDequeue expects an out param")
	}
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

// queueTryPeek is queueTryDequeue's non-removing counterpart — same
// `out`-by-managed-pointer mechanism, but leaves items untouched.
func queueTryPeek(args []runtime.Value) (runtime.Value, error) {
	q, err := asQueue(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: Queue.TryPeek expects an out param")
	}
	if len(q.items) == 0 {
		return runtime.Bool(false), nil
	}
	if args[1].Kind == runtime.KindRef && args[1].Ref != nil {
		*args[1].Ref = q.items[0]
	}
	return runtime.Bool(true), nil
}

// queueGetEnumerator backs both a direct `foreach (var x in queue)` and
// LINQ over a Queue<T> reached through IEnumerable`1 (enumerateAll's
// fallback, internal/interpreter/linq.go — Queue<T> isn't a nativeList,
// so it doesn't take that function's fast path). Missing entirely until
// probed against a hand-written fixture. Mirrors listGetEnumerator
// exactly (front-to-back, live off the receiver — not a snapshot, same
// posture as List<T>/HashSet<T>'s own enumerators), just under Queue<T>.
// Enumerator's own real struct name — see queueEnumeratorType's doc
// comment for why that name specifically matters here.
func queueGetEnumerator(args []runtime.Value) (runtime.Value, error) {
	if _, err := asQueue(args); err != nil {
		return runtime.Value{}, err
	}
	s := runtime.NewStruct(queueEnumeratorType)
	s.Fields[0] = args[0]
	return runtime.StructVal(s), nil
}

func queueEnumeratorMoveNext(args []runtime.Value) (runtime.Value, error) {
	s, err := derefStructReceiver(args, "Queue.Enumerator", "Queue.Enumerator.MoveNext")
	if err != nil {
		return runtime.Value{}, err
	}
	q, err := asQueue([]runtime.Value{s.Fields[0]})
	if err != nil {
		return runtime.Value{}, err
	}
	next := s.Fields[1].I4 + 1
	s.Fields[1] = runtime.Int32(next)
	return runtime.Bool(int(next) < len(q.items)), nil
}

func queueEnumeratorGetCurrent(args []runtime.Value) (runtime.Value, error) {
	s, err := derefStructReceiver(args, "Queue.Enumerator", "Queue.Enumerator.Current")
	if err != nil {
		return runtime.Value{}, err
	}
	q, err := asQueue([]runtime.Value{s.Fields[0]})
	if err != nil {
		return runtime.Value{}, err
	}
	idx := int(s.Fields[1].I4)
	if idx < 0 || idx >= len(q.items) {
		return runtime.Value{}, fmt.Errorf("bcl: Queue.Enumerator.Current: index %d out of range", idx)
	}
	return q.items[idx], nil
}
