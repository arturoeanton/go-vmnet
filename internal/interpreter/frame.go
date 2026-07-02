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
}

func (f *Frame) push(v runtime.Value) { f.Stack = append(f.Stack, v) }

func (f *Frame) pop() runtime.Value {
	v := f.Stack[len(f.Stack)-1]
	f.Stack = f.Stack[:len(f.Stack)-1]
	return v
}
