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
	_, _, err := m.call(concrete+"::MoveNext", []runtime.Value{stateMachine}, false, depth, instrCount)
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
		if mex, ok := err.(*runtime.ManagedException); ok {
			return bcl.NewFaultedTask(mex), nil
		}
		return runtime.Value{}, err
	}
	return bcl.NewCompletedTask(v, hasReturn), nil
}
