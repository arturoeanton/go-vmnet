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

func init() {
	registerCtor("System.Collections.Generic.Queue`1", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeQueue{}}, nil
	})
	register("System.Collections.Generic.Queue`1::Enqueue", false, queueEnqueue)
	register("System.Collections.Generic.Queue`1::Dequeue", true, queueDequeue)
	register("System.Collections.Generic.Queue`1::Peek", true, queuePeek)
	register("System.Collections.Generic.Queue`1::get_Count", true, queueCount)
	register("System.Collections.Generic.Queue`1::TryDequeue", true, queueTryDequeue)
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
