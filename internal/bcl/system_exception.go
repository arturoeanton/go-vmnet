package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// Common exception types fixtures throw and catch (Fase 3.10 adds real
// try/catch/finally — see internal/interpreter/exceptions.go; construction
// just needs to capture a type name and message, not a full object).
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
	register("System.Exception::get_Message", true, exceptionGetMessage)
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

func exceptionGetMessage(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return runtime.Value{}, fmt.Errorf("bcl: System.Exception.get_Message expects a receiver")
	}
	ex, ok := args[0].Obj.Native.(*runtime.ManagedException)
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: receiver is not an Exception")
	}
	return runtime.String(ex.Message), nil
}
