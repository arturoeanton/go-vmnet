package bcl

import "github.com/arturoeanton/go-vmnet/internal/runtime"

// System.Diagnostics.Debugger — vmnet has no debugger attach concept at
// all, so IsAttached is always false and Break/Log are no-ops, matching
// how real code guards debugger-only diagnostics (`if (Debugger.
// IsAttached) Debugger.Break();`) in a normal, undebugged run.
func init() {
	register("System.Diagnostics.Debugger::get_IsAttached", true, debuggerFalse)
	register("System.Diagnostics.Debugger::Break", false, debuggerNoop)
	register("System.Diagnostics.Debugger::Log", false, debuggerNoop)
}

func debuggerFalse(args []runtime.Value) (runtime.Value, error) {
	return runtime.Bool(false), nil
}

func debuggerNoop(args []runtime.Value) (runtime.Value, error) {
	return runtime.Value{}, nil
}
