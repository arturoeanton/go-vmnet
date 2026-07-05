package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// System.Runtime.ExceptionServices.ExceptionDispatchInfo — found via a
// corpus-wide static-checker scan across the 19 tracked NuGet packages:
// 6 packages, 28 Capture + 28 Throw real call sites, all in the exact
// same real shape (confirmed by disassembling Polly.dll's own real
// Polly.Utilities.ExceptionExtensions.RethrowWithOriginalStackTraceIfDiffersFrom):
//
//	catch (Exception ex) {
//	    ...
//	    ExceptionDispatchInfo.Capture(ex).Throw();
//	}
//
// — the standard "rethrow without losing the original exception's own
// stack trace" idiom a plain `throw ex;` (as opposed to bare `throw;`)
// would otherwise defeat, used pervasively by exactly the kind of
// wrapper/pipeline/retry code this loop's target packages (Polly,
// MediatR's generated async state machines, CsvHelper's async path) are
// built from.
//
// vmnet has no separate stack-trace representation to preserve at all
// (docs/en/spec.md — no CLR call-stack frames are modeled), so the real,
// hard problem ExceptionDispatchInfo solves doesn't exist here in the
// first place; what DOES matter for observable behavior is that
// Throw() re-raises the exact same exception object Capture wrapped —
// not a fresh copy — so an outer `catch (MyException e) { ... e.Code
// ...}` still sees the original object's own fields intact. That's
// exactly ManagedException.Object's own existing contract (internal/
// runtime/exception.go's doc comment, and internal/interpreter/
// exceptions.go's exceptionValue, which already prefers ex.Object over a
// fresh wrapper for precisely this reason) — Capture only needs to hold
// onto the SAME *runtime.ManagedException a real `catch` clause handed
// it, and Throw only needs to hand that same pointer back out as the Go
// error every other native fault path in this package already uses to
// signal a managed exception (see e.g. sqliteManagedException's own doc
// comment, system_data_sqlite.go) — internal/interpreter/eval.go's own
// call-boundary handling (the `ex, ok := err.(*runtime.ManagedException)`
// check backing every method return, Machine.call) is what actually
// propagates it onward to whichever real `catch`/`try`/`finally` region
// is waiting, identically to a real `throw` opcode.
type nativeExceptionDispatchInfo struct {
	ex *runtime.ManagedException
}

func init() {
	// Capture(Exception source) is a static factory method (`call`, no
	// receiver) — not a constructor (no real `new ExceptionDispatchInfo
	// (...)` exists at all in the BCL; Capture is the only way to get one),
	// so this is registered as an ordinary Native under the type's own
	// name rather than through registerCtor/newobj.
	register("System.Runtime.ExceptionServices.ExceptionDispatchInfo::Capture", true, exceptionDispatchInfoCapture)
	register("System.Runtime.ExceptionServices.ExceptionDispatchInfo::Throw", false, exceptionDispatchInfoThrow)
	// SourceException: real code occasionally inspects the captured
	// exception without rethrowing it (e.g. logging it first) — cheap to
	// support alongside Capture/Throw since exceptionValue below already
	// does all the real work Throw needs too.
	register("System.Runtime.ExceptionServices.ExceptionDispatchInfo::get_SourceException", true, exceptionDispatchInfoGetSourceException)
}

func exceptionDispatchInfoCapture(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return runtime.Value{}, fmt.Errorf("bcl: ExceptionDispatchInfo.Capture expects an Exception argument")
	}
	ex, ok := args[0].Obj.Native.(*runtime.ManagedException)
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: ExceptionDispatchInfo.Capture argument is not an Exception")
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeExceptionDispatchInfo{ex: ex}}), nil
}

// exceptionDispatchInfoValue mirrors internal/interpreter/exceptions.go's
// own exceptionValue exactly (duplicated, not imported: bcl is imported
// BY interpreter, never the other way around, so reusing that unexported
// helper directly isn't possible) — the real thrown *Object if one was
// ever recorded (ManagedException.Object's own doc comment), preserving
// a plugin exception subclass's extra fields, or a fresh bare wrapper for
// an exception that originated as a Go error deep inside some other
// native and was never a real thrown Object at all.
func exceptionDispatchInfoValue(ex *runtime.ManagedException) runtime.Value {
	if ex.Object != nil {
		return runtime.ObjRef(ex.Object)
	}
	return runtime.ObjRef(&runtime.Object{Native: ex})
}

func exceptionDispatchInfoGetSourceException(args []runtime.Value) (runtime.Value, error) {
	edi, ok := nativeOf[*nativeExceptionDispatchInfo](firstArg(args))
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: ExceptionDispatchInfo.SourceException called on a non-ExceptionDispatchInfo receiver")
	}
	return exceptionDispatchInfoValue(edi.ex), nil
}

// exceptionDispatchInfoThrow re-raises the SAME *runtime.ManagedException
// Capture wrapped, by returning it as this native's own error — exactly
// how every other native in this package already signals a managed fault
// (e.g. sqliteManagedException, system_data_sqlite.go), and exactly what
// ir.Throw itself returns for a real `throw` statement (internal/
// interpreter/eval.go). Machine.call's own per-frame dispatch loop
// (internal/interpreter/exceptions.go, dispatchException) then finds the
// nearest matching `catch` for it with no special-casing needed here at
// all — from the interpreter's point of view this is indistinguishable
// from the original exception propagating on its own.
func exceptionDispatchInfoThrow(args []runtime.Value) (runtime.Value, error) {
	edi, ok := nativeOf[*nativeExceptionDispatchInfo](firstArg(args))
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: ExceptionDispatchInfo.Throw called on a non-ExceptionDispatchInfo receiver")
	}
	return runtime.Value{}, edi.ex
}
