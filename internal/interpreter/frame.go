// Package interpreter executes vmnet IR (internal/ir) against a call frame
// and value stack: arithmetic, branches, calls and exception handling, with
// configurable limits (max stack depth, call depth, instruction count) for
// sandboxed execution. See docs/en/ROADMAP.md, Fase 1 y Fase 2, module
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

	// MethodGenericArgs holds THIS specific call's own resolved generic
	// method type arguments (Fase 3.60) — e.g. ["Vmnet.Fixtures.Greeter"]
	// for a call into `T Identity<T>()` closed over Greeter — nil for a
	// non-generic method, or any call reached through a path that doesn't
	// carry them (New/Invoke/CallInstance's own public entry points,
	// a .cctor run, an attribute's CreateNew/Deserialize helper, ...).
	// Populated only by tryCall's own fallback into an ordinary
	// interpreted method body, from the ORIGINAL call site's own
	// ir.Call.MethodGenericArgs (previously this information stopped at
	// genericMachineRegistry and never reached an interpreted method's
	// own IR at all — see ir.LoadTypeToken's own doc comment on
	// IsMethodGenericParam, which is the one real consumer: `typeof(T)`
	// on the enclosing generic method's own still-open type parameter,
	// found via a real, load-bearing case — Microsoft.Extensions.
	// DependencyInjection's own ServiceDescriptor.Singleton<TService,
	// TImplementation>() calling typeof(TImplementation) on the
	// generic parameter directly, not through any native intercept).
	MethodGenericArgs []string
}

func (f *Frame) push(v runtime.Value) { f.Stack = append(f.Stack, v) }

func (f *Frame) pop() runtime.Value {
	v := f.Stack[len(f.Stack)-1]
	f.Stack = f.Stack[:len(f.Stack)-1]
	return v
}
