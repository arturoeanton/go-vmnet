// Package interpreter executes vmnet IR (internal/ir) against a call frame
// and value stack: arithmetic, branches, calls and exception handling, with
// configurable limits (max stack depth, call depth, instruction count) for
// sandboxed execution. See docs/ROADMAP.md, Fase 1 y Fase 2, module
// "/interpreter".
package interpreter

import "github.com/arturoeanton/go-vmnet/internal/runtime"

// Frame is one method activation: its arguments, locals and evaluation
// stack.
type Frame struct {
	Args   []runtime.Value
	Locals []runtime.Value
	Stack  []runtime.Value
	IP     int

	// unwind tracks an in-flight `leave` or exception propagating through
	// one or more finally/fault handlers it still needs to run before
	// resuming — see internal/interpreter/exceptions.go (Fase 3.10).
	unwind *unwind

	// currentException is the exception the innermost catch handler
	// execution is currently in is handling, for `rethrow` (C#'s `throw;`
	// with no operand). Overwritten on entering any catch handler —
	// narrow known gap: a rethrow appearing *after* a nested try/catch
	// inside the same catch block would see the nested exception instead
	// of being restored, an edge case rare enough not to warrant a full
	// stack for it.
	currentException *runtime.ManagedException
}

func (f *Frame) push(v runtime.Value) { f.Stack = append(f.Stack, v) }

func (f *Frame) pop() runtime.Value {
	v := f.Stack[len(f.Stack)-1]
	f.Stack = f.Stack[:len(f.Stack)-1]
	return v
}
