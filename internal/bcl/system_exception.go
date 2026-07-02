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
		// A plugin's own exception subclass (`class MyException :
		// Exception { public MyException(string m) : base(m) {} }`)
		// chains to its base via a plain, non-virtual `call
		// System.Exception::.ctor(this, message)` — not `newobj` (only
		// the *exact* type gets newobj'd; the base call runs on the
		// already-allocated derived object). registerCtor above only
		// ever fires from newObj's LookupCtor branch, which handles
		// constructing the exact BCL type directly — this second
		// registration is what makes base-chaining from a subclass
		// resolve at all (Fase 3.13).
		register(name+"::.ctor", false, baseExceptionCtorInPlace(name))
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

// baseExceptionCtorInPlace backs "<name>::.ctor" as a plain (non-newobj)
// call — the shape a derived plugin class's constructor uses to chain to
// its base via `: base(message)`. Unlike newExceptionCtor's newobj-only
// path (which allocates a fresh Object), the receiver here already
// exists: newObj already allocated it as the *derived* plugin type
// (Obj.Type is the derived TypeDef, Fields holds the derived class's own
// fields), so this only adds Obj.Native alongside it — a deliberate,
// narrow exception to Object's normal "Type xor Native" rule (see
// runtime.Object's doc comment): ir.Throw requires Obj.Native to be a
// *runtime.ManagedException on ANY thrown object, plugin-declared or
// not, and vmnet has no real field layout for System.Exception to fold
// Message into Fields instead.
//
// TypeName is set to the receiver's actual *runtime.Type (the derived
// plugin class, e.g. "Vmnet.Fixtures.MyException"), not the fallback
// typeName this native is registered under ("System.Exception" etc.) —
// catch-matching (exceptionMatchesCatch, internal/interpreter/
// exceptions.go) needs the real most-derived name so `catch
// (MyException e)` matches; `catch (Exception e)` still works too, via
// nativeMatches walking the real TypeDef base chain once it can't find a
// hand-mapped BCL exception name (Fase 3.13).
func baseExceptionCtorInPlace(typeName string) Native {
	return func(args []runtime.Value) (runtime.Value, error) {
		if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
			return runtime.Value{}, fmt.Errorf("bcl: %s constructor called without a receiver", typeName)
		}
		msg := ""
		if len(args) > 1 && args[1].Kind == runtime.KindString {
			msg = args[1].Str
		}
		name := typeName
		if t := args[0].Obj.Type; t != nil {
			if t.Namespace != "" {
				name = t.Namespace + "." + t.Name
			} else {
				name = t.Name
			}
		}
		args[0].Obj.Native = &runtime.ManagedException{TypeName: name, Message: msg}
		return runtime.Value{}, nil
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
