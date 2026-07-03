package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// nativeTask backs System.Threading.Tasks.Task/Task<T> — modeled as
// always synchronously resolved: vmnet has no real scheduler/thread
// pool, so every Task any native here produces is already completed by
// the time anything can observe it (see internal/interpreter/async.go's
// doc comment on asyncBuilderStart for why this makes a real compiler-
// generated async state machine's MoveNext() run start-to-finish in a
// single call, needing no changes to the interpreter itself — the
// existing try/catch/finally machinery, Fase 3.10, already handles the
// state machine's own generated exception-routing region).
type nativeTask struct {
	completed bool
	hasValue  bool
	value     runtime.Value
	err       *runtime.ManagedException
}

// asyncBuilderType/asyncBuilderGenericType model
// System.Runtime.CompilerServices.AsyncTaskMethodBuilder/`1 — real
// value types (compiler-generated state machines embed one as a plain
// struct field), holding just a reference to the nativeTask they're
// building so SetResult/SetException/get_Task all observe the same
// object regardless of how many times the containing struct gets
// copied.
var (
	asyncBuilderType = runtime.NewValueType(
		"System.Runtime.CompilerServices", "AsyncTaskMethodBuilder",
		[]string{"task"},
		[]runtime.Value{runtime.Null()},
	)
	asyncBuilderGenericType = runtime.NewValueType(
		"System.Runtime.CompilerServices", "AsyncTaskMethodBuilder`1",
		[]string{"task"},
		[]runtime.Value{runtime.Null()},
	)
)

func init() {
	registerValueType(asyncBuilderType)
	registerValueType(asyncBuilderGenericType)
	register("System.Runtime.CompilerServices.AsyncTaskMethodBuilder::Create", true, asyncBuilderCreate(asyncBuilderType))
	register("System.Runtime.CompilerServices.AsyncTaskMethodBuilder`1::Create", true, asyncBuilderCreate(asyncBuilderGenericType))
	// SetStateMachine only matters when a struct-shaped state machine
	// needs to be boxed onto the heap to survive a real suspension —
	// since every await in vmnet's model resolves synchronously (never
	// actually suspends), there's nothing for it to do.
	register("System.Runtime.CompilerServices.AsyncTaskMethodBuilder::SetStateMachine", false, asyncBuilderNoop)
	register("System.Runtime.CompilerServices.AsyncTaskMethodBuilder`1::SetStateMachine", false, asyncBuilderNoop)
	register("System.Runtime.CompilerServices.AsyncTaskMethodBuilder::SetResult", false, asyncBuilderSetResultVoid)
	register("System.Runtime.CompilerServices.AsyncTaskMethodBuilder`1::SetResult", false, asyncBuilderSetResultValue)
	register("System.Runtime.CompilerServices.AsyncTaskMethodBuilder::SetException", false, asyncBuilderSetException)
	register("System.Runtime.CompilerServices.AsyncTaskMethodBuilder`1::SetException", false, asyncBuilderSetException)
	register("System.Runtime.CompilerServices.AsyncTaskMethodBuilder::get_Task", true, asyncBuilderGetTask)
	register("System.Runtime.CompilerServices.AsyncTaskMethodBuilder`1::get_Task", true, asyncBuilderGetTask)

	// The Task itself doubles as its own awaiter/ConfiguredTaskAwaitable
	// in this model — TaskAwaiter/ConfiguredTaskAwaitable(+Awaiter) have
	// no members beyond get_IsCompleted/GetResult/OnCompleted that this
	// package's callers ever reach, so there is no observable difference
	// from allocating a distinct wrapper each time.
	register("System.Threading.Tasks.Task::GetAwaiter", true, taskGetAwaiter)
	register("System.Threading.Tasks.Task`1::GetAwaiter", true, taskGetAwaiter)
	register("System.Threading.Tasks.Task::ConfigureAwait", true, taskGetAwaiter)
	register("System.Threading.Tasks.Task`1::ConfigureAwait", true, taskGetAwaiter)
	register("System.Runtime.CompilerServices.ConfiguredTaskAwaitable::GetAwaiter", true, taskGetAwaiter)
	register("System.Runtime.CompilerServices.ConfiguredTaskAwaitable`1::GetAwaiter", true, taskGetAwaiter)

	register("System.Threading.Tasks.Task::get_IsCompleted", true, taskIsCompleted)
	register("System.Threading.Tasks.Task`1::get_IsCompleted", true, taskIsCompleted)
	register("System.Runtime.CompilerServices.TaskAwaiter::get_IsCompleted", true, taskIsCompleted)
	register("System.Runtime.CompilerServices.TaskAwaiter`1::get_IsCompleted", true, taskIsCompleted)
	register("System.Runtime.CompilerServices.ConfiguredTaskAwaitable+ConfiguredTaskAwaiter::get_IsCompleted", true, taskIsCompleted)
	register("System.Runtime.CompilerServices.ConfiguredTaskAwaitable`1+ConfiguredTaskAwaiter::get_IsCompleted", true, taskIsCompleted)

	// GetResult on the non-generic awaiter shapes is void — hasReturn
	// must be false there specifically, even though it's the exact same
	// underlying Go function as the generic (`Task<T>`) shapes.
	register("System.Runtime.CompilerServices.TaskAwaiter::GetResult", false, taskGetResult)
	register("System.Runtime.CompilerServices.TaskAwaiter`1::GetResult", true, taskGetResult)
	register("System.Runtime.CompilerServices.ConfiguredTaskAwaitable+ConfiguredTaskAwaiter::GetResult", false, taskGetResult)
	register("System.Runtime.CompilerServices.ConfiguredTaskAwaitable`1+ConfiguredTaskAwaiter::GetResult", true, taskGetResult)

	register("System.Threading.Tasks.Task::FromResult", true, taskFromResultNative)
	register("System.Threading.Tasks.Task::get_CompletedTask", true, taskCompletedTaskNative)
	register("System.Threading.Tasks.Task::Delay", true, taskCompletedTaskNative)
}

func asyncBuilderCreate(vt *runtime.Type) Native {
	return func(args []runtime.Value) (runtime.Value, error) {
		s := runtime.NewStruct(vt)
		s.Fields[0] = runtime.ObjRef(&runtime.Object{Native: &nativeTask{}})
		return runtime.StructVal(s), nil
	}
}

func asyncBuilderNoop(args []runtime.Value) (runtime.Value, error) {
	return runtime.Value{}, nil
}

// asyncBuilderTaskValue returns both the Task Value stored on the
// builder (what get_Task must return unchanged) and the *nativeTask
// behind it (what SetResult/SetException mutate).
func asyncBuilderTaskValue(args []runtime.Value) (runtime.Value, *nativeTask, error) {
	s, err := derefStructReceiver(args, "AsyncTaskMethodBuilder", "AsyncTaskMethodBuilder method")
	if err != nil {
		return runtime.Value{}, nil, err
	}
	v := s.Fields[0]
	if v.Kind != runtime.KindObject || v.Obj == nil {
		return runtime.Value{}, nil, fmt.Errorf("bcl: AsyncTaskMethodBuilder has no Task (Create() was never called)")
	}
	t, ok := v.Obj.Native.(*nativeTask)
	if !ok {
		return runtime.Value{}, nil, fmt.Errorf("bcl: AsyncTaskMethodBuilder.task is not a Task")
	}
	return v, t, nil
}

func asyncBuilderSetResultVoid(args []runtime.Value) (runtime.Value, error) {
	_, t, err := asyncBuilderTaskValue(args)
	if err != nil {
		return runtime.Value{}, err
	}
	t.completed = true
	return runtime.Value{}, nil
}

func asyncBuilderSetResultValue(args []runtime.Value) (runtime.Value, error) {
	_, t, err := asyncBuilderTaskValue(args)
	if err != nil {
		return runtime.Value{}, err
	}
	t.completed = true
	if len(args) > 1 {
		t.hasValue = true
		t.value = args[1]
	}
	return runtime.Value{}, nil
}

func asyncBuilderSetException(args []runtime.Value) (runtime.Value, error) {
	_, t, err := asyncBuilderTaskValue(args)
	if err != nil {
		return runtime.Value{}, err
	}
	t.completed = true
	t.err = managedExceptionOf(args)
	return runtime.Value{}, nil
}

// managedExceptionOf extracts the *runtime.ManagedException an
// exception-shaped argument carries, falling back to a generic wrapper
// when it isn't one (a plugin's own exception subclass without a
// Native, or anything unexpected) — mirrors how ir.Throw itself expects
// exceptions to be shaped (Obj.Native, Fase 3.13).
func managedExceptionOf(args []runtime.Value) *runtime.ManagedException {
	if len(args) > 1 && args[1].Kind == runtime.KindObject && args[1].Obj != nil {
		if ex, ok := args[1].Obj.Native.(*runtime.ManagedException); ok {
			return ex
		}
	}
	return &runtime.ManagedException{TypeName: "System.Exception", Message: "async method faulted"}
}

func asyncBuilderGetTask(args []runtime.Value) (runtime.Value, error) {
	v, _, err := asyncBuilderTaskValue(args)
	return v, err
}

func asTask(args []runtime.Value) (*nativeTask, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("bcl: Task method called without a receiver")
	}
	v := args[0]
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	if v.Kind != runtime.KindObject || v.Obj == nil {
		return nil, fmt.Errorf("bcl: receiver is not a Task")
	}
	t, ok := v.Obj.Native.(*nativeTask)
	if !ok {
		return nil, fmt.Errorf("bcl: receiver is not a Task")
	}
	return t, nil
}

func taskGetAwaiter(args []runtime.Value) (runtime.Value, error) {
	if _, err := asTask(args); err != nil {
		return runtime.Value{}, err
	}
	return args[0], nil
}

func taskIsCompleted(args []runtime.Value) (runtime.Value, error) {
	t, err := asTask(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(t.completed), nil
}

func taskGetResult(args []runtime.Value) (runtime.Value, error) {
	t, err := asTask(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if t.err != nil {
		return runtime.Value{}, t.err
	}
	if t.hasValue {
		return t.value, nil
	}
	return runtime.Value{}, nil
}

// NewCompletedTask/NewFaultedTask are exported for
// internal/interpreter/async.go's Task.Run (needs Machine access to
// invoke the delegate argument, unavailable to a plain bcl.Native).
func NewCompletedTask(value runtime.Value, hasValue bool) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeTask{completed: true, hasValue: hasValue, value: value}})
}

func NewFaultedTask(err *runtime.ManagedException) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeTask{completed: true, err: err}})
}

func taskFromResultNative(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return NewCompletedTask(runtime.Value{}, false), nil
	}
	return NewCompletedTask(args[0], true), nil
}

func taskCompletedTaskNative(args []runtime.Value) (runtime.Value, error) {
	return NewCompletedTask(runtime.Value{}, false), nil
}
