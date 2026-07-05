package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

func init() {
	machineRegistry["System.Runtime.CompilerServices.AsyncTaskMethodBuilder::Start"] = asyncBuilderStart
	machineRegistry["System.Runtime.CompilerServices.AsyncTaskMethodBuilder`1::Start"] = asyncBuilderStart
	machineRegistry["System.Runtime.CompilerServices.AsyncTaskMethodBuilder::AwaitUnsafeOnCompleted"] = asyncAwaitUnsafeOnCompleted
	machineRegistry["System.Runtime.CompilerServices.AsyncTaskMethodBuilder`1::AwaitUnsafeOnCompleted"] = asyncAwaitUnsafeOnCompleted
	machineRegistry["System.Threading.Tasks.Task::Run"] = taskRun
	machineRegistry["System.Threading.Tasks.Task::get_Factory"] = taskGetFactory
	machineRegistry["System.Threading.Tasks.TaskFactory::StartNew"] = taskFactoryStartNew
	machineRegistry["System.Threading.Tasks.TaskScheduler::get_Default"] = taskSchedulerGetDefault
}

// taskSchedulerGetDefault backs the static TaskScheduler.Default property
// — a plain marker value, same posture as taskGetFactory above:
// taskFactoryStartNew ignores whichever TaskScheduler it's handed
// entirely (everything runs synchronously right now, no thread pool to
// route through), so this just needs to exist as A value.
func taskSchedulerGetDefault(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	return runtime.ObjRef(&runtime.Object{Native: &nativeTaskScheduler{}}), nil
}

type nativeTaskScheduler struct{}

// taskGetFactory backs the static Task.Factory property — real .NET
// hands back a real TaskFactory instance carrying default scheduling
// options, but every option TaskFactory.StartNew's own real overloads
// accept (CancellationToken/TaskCreationOptions/TaskScheduler) is
// ignored by taskFactoryStartNew below anyway (same "runs synchronously
// right now, no thread pool" posture Task.Run already established), so
// this just needs to be A value, not a meaningfully-configured one.
// Found via AutoMapper's own MapperConfiguration constructor, which
// fires a background license-validation check via
// `Task.Factory.StartNew(ValidateLicense, ..., TaskScheduler.Default)`
// and discards the resulting Task (`pop`) — real .NET never observes
// that Task's own outcome either, so this doesn't need to either.
func taskGetFactory(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	return runtime.ObjRef(&runtime.Object{Native: &nativeTaskFactory{}}), nil
}

// nativeTaskFactory is a stateless marker — every real TaskFactory.
// StartNew overload's own scheduling-related arguments are accepted and
// ignored (see taskFactoryStartNew's own doc comment), so there's
// nothing to actually carry here.
type nativeTaskFactory struct{}

// taskFactoryStartNew covers every real TaskFactory.StartNew(Action/
// Func<T>, ...) overload sharing the "first real argument after the
// receiver is the delegate to run, everything else is
// CancellationToken/TaskCreationOptions/TaskScheduler/state" shape —
// same "runs synchronously right now, wraps the outcome as an
// already-completed (or faulted) Task" semantics Task.Run's own
// taskRun already established, reused here rather than duplicated: a
// thrown exception becomes a faulted Task (matching real .NET's own
// "the caller never awaits or observes this Task" posture for a
// fire-and-forget StartNew whose result is immediately discarded, the
// one real call site found so far), not a propagated Go error.
func taskFactoryStartNew(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 2 || args[1].Kind != runtime.KindFunc || args[1].Func == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: TaskFactory.StartNew expects a delegate argument")
	}
	v, hasReturn, err := m.invokeFunc(args[1].Func, nil, depth, instrCount)
	if err != nil {
		if faulted, ok := taskFaultOrPropagate(err); ok {
			return bcl.NewFaultedTask(faulted), nil
		}
		return runtime.Value{}, err
	}
	return bcl.NewCompletedTask(v, hasReturn), nil
}

// taskFaultOrPropagate is Task.Run/TaskFactory.StartNew's shared
// "what does the delegate's own failure become" decision (Fase 3.66,
// found via AutoMapper's own fire-and-forget license-validation
// StartNew call: its real body hit a genuine, unrelated vmnet gap deep
// inside — a type this loop's own metadata has no TypeDef for at all —
// and that error used to propagate all the way to the CALLING thread,
// crashing a real, unmodified AutoMapper's own constructor even though
// nothing ever awaits or observes the StartNew'd Task, exactly like a
// real .NET exception thrown inside an unobserved background Task
// wouldn't either). Any error becomes the returned Task's own Faulted
// exception — including a plain interpreter limitation, synthesized as
// a generic System.Exception, not just a real *runtime.ManagedException
// — EXCEPT vmnet's own resource-safety sentinels (instruction-count/
// stack-depth limits), which must still abort the whole run rather than
// be silently absorbed by a background task that happened to be running
// when the limit tripped.
func taskFaultOrPropagate(err error) (*runtime.ManagedException, bool) {
	if err == ErrInstructionLimitExceeded || err == ErrStackOverflow {
		return nil, false
	}
	if mex, ok := err.(*runtime.ManagedException); ok {
		return mex, true
	}
	return &runtime.ManagedException{TypeName: "System.Exception", Message: err.Error()}, true
}

// asyncBuilderStart runs a compiler-generated async state machine to
// completion in one synchronous call — vmnet models every awaitable as
// already-completed (internal/bcl/system_task.go), so the generated
// MoveNext() body never actually needs to suspend: every `await`'s
// IsCompleted check takes the "continue synchronously" branch on its
// first (only) evaluation, meaning a single MoveNext() call always runs
// the whole async method through to its `return`/exception — which the
// generated code itself already routes into SetResult/SetException on
// this same builder before MoveNext returns. No interpreter changes
// were needed for MoveNext's own body: it's ordinary IL (fields,
// branches, a real try/catch/finally region for exception routing),
// already fully handled since Fase 1/3.10.
func asyncBuilderStart(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: AsyncTaskMethodBuilder.Start expects (this, ref stateMachine)")
	}
	return runtime.Value{}, runStateMachine(m, args[1], depth, instrCount)
}

// asyncAwaitUnsafeOnCompleted should never actually run in vmnet's
// synchronous model — every awaiter's IsCompleted is always true, so
// generated code never takes the branch that calls this. Kept as a
// defensive fallback (continue the state machine immediately, as if a
// continuation ran instantly) rather than an error, in case some future
// awaitable genuinely isn't complete by the time this fires.
func asyncAwaitUnsafeOnCompleted(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 3 {
		return runtime.Value{}, fmt.Errorf("interpreter: AsyncTaskMethodBuilder.AwaitUnsafeOnCompleted expects (this, ref awaiter, ref stateMachine)")
	}
	return runtime.Value{}, runStateMachine(m, args[2], depth, instrCount)
}

func runStateMachine(m *Machine, stateMachine runtime.Value, depth int, instrCount *int64) error {
	concrete, ok := receiverTypeName(stateMachine)
	if !ok {
		return fmt.Errorf("interpreter: async state machine: cannot determine its concrete type")
	}
	_, _, err := m.call(concrete+"::MoveNext", []runtime.Value{stateMachine}, false, depth, instrCount, nil, nil)
	return err
}

// taskRun invokes its delegate argument synchronously right now (no
// thread pool — vmnet has nothing to hand the work off to) and wraps
// whatever it returns as an already-completed Task. A delegate that
// itself returns a Task (Func<Task>/Func<Task<T>>) is not unwrapped —
// the result is a Task-of-a-Task rather than real Task.Run's flattened
// one, a documented simplification not measured as necessary by the
// probe this fase was built against.
func taskRun(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindFunc || args[0].Func == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: Task.Run expects a delegate argument")
	}
	v, hasReturn, err := m.invokeFunc(args[0].Func, nil, depth, instrCount)
	if err != nil {
		if faulted, ok := taskFaultOrPropagate(err); ok {
			return bcl.NewFaultedTask(faulted), nil
		}
		return runtime.Value{}, err
	}
	return bcl.NewCompletedTask(v, hasReturn), nil
}
