package bcl

import "github.com/arturoeanton/go-vmnet/internal/runtime"

// Common exception types fixtures throw. Fase 2 only supports unhandled
// throw (docs/ROADMAP.md) — construction just needs to capture a type
// name and message for the resulting Go error, not a full object.
func init() {
	for _, name := range []string{
		"System.Exception",
		"System.InvalidOperationException",
		"System.ArgumentException",
		"System.ArgumentNullException",
		"System.ArgumentOutOfRangeException",
		"System.IndexOutOfRangeException",
		"System.NotSupportedException",
		"System.InvalidCastException",
	} {
		registerCtor(name, newExceptionCtor(name))
	}
}

func newExceptionCtor(typeName string) NativeCtor {
	return func(args []runtime.Value) (*runtime.Object, error) {
		msg := ""
		if len(args) > 0 && args[0].Kind == runtime.KindString {
			msg = args[0].Str
		}
		return &runtime.Object{Native: &runtime.ManagedException{TypeName: typeName, Message: msg}}, nil
	}
}
