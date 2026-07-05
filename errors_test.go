package vmnet

import (
	"errors"
	"testing"

	"github.com/arturoeanton/go-vmnet/internal/interpreter"
	"github.com/arturoeanton/go-vmnet/internal/ir"
	"github.com/arturoeanton/go-vmnet/internal/metadata"
	"github.com/arturoeanton/go-vmnet/internal/pe"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// TestErrorClassification verifies spec §30.2's own error-code catalog:
// every public entry point (Call/CallBytes/New/LoadBytes/NuGet) that can
// fail returns a *vmnet.Error carrying one of the Code* constants, not
// just a bare Go error a caller has to string-match. Real, reproducible
// end-to-end triggers where a cheap one already exists (the shared C#
// test fixture, or a pure-Go failure needing no fixture at all); a
// direct classify() unit check for the handful of codes that would
// otherwise need a brand new C# fixture, real network access, or an
// artificially corrupted PE just to reach.
func TestErrorClassification(t *testing.T) {
	t.Run("CodeInvalidPE: garbage bytes", func(t *testing.T) {
		vm := New()
		_, err := vm.LoadBytes("garbage.dll", []byte("not a PE file at all"))
		assertCode(t, err, CodeInvalidPE)
	})

	t.Run("CodeMissingCLIHeader", func(t *testing.T) {
		// classify() itself, not LoadBytes: constructing a real PE with a
		// COFF/optional header but no CLI directory (a native, non-.NET
		// DLL) needs a real native PE fixture this project has no reason
		// to carry — pe.ErrMissingCLIHeader's own real trigger is already
		// covered by internal/pe's own tests (pe_test.go).
		assertCode(t, classify(pe.ErrMissingCLIHeader), CodeMissingCLIHeader)
	})

	t.Run("CodeInvalidMetadata", func(t *testing.T) {
		assertCode(t, classify(metadata.ErrInvalidMetadataRoot), CodeInvalidMetadata)
	})

	t.Run("CodeTypeNotFound: unknown type", func(t *testing.T) {
		asm := loadFixture(t)
		_, err := asm.Call("Vmnet.Fixtures.DoesNotExist", "Whatever")
		assertCode(t, err, CodeTypeNotFound)
	})

	t.Run("CodeMethodNotFound: unknown method on a real type", func(t *testing.T) {
		asm := loadFixture(t)
		_, err := asm.Call("Vmnet.Fixtures.SimpleMath", "DoesNotExist")
		assertCode(t, err, CodeMethodNotFound)
	})

	t.Run("CodeUnsupportedBCLMethod", func(t *testing.T) {
		assertCode(t, classify(&interpreter.UnsupportedBCLMethodError{Method: "System.Not.A.Real::Method"}), CodeUnsupportedBCLMethod)
	})

	t.Run("CodeUnsupportedOpcode", func(t *testing.T) {
		assertCode(t, classify(&ir.UnsupportedOpcodeError{OpCode: "calli", Offset: 0}), CodeUnsupportedOpcode)
	})

	t.Run("CodeManagedException: real thrown-and-uncaught exception", func(t *testing.T) {
		asm := loadFixture(t)
		_, err := asm.CallBytes("Vmnet.Fixtures.Rules", "Eval", []byte(""))
		assertCode(t, err, CodeManagedException)
		var vErr *Error
		if !errors.As(err, &vErr) {
			t.Fatalf("error = %v, want *Error", err)
		}
		if vErr.Details == "" {
			t.Error("Details is empty, want a real spec §18.3 stack trace")
		}
	})

	t.Run("CodePermissionDenied: real denied file write", func(t *testing.T) {
		asm := loadFixtureWithPermissions(t, nil)
		_, err := asm.Call("Vmnet.Fixtures.FileIO", "WriteThenReadText", String(t.TempDir()+"/x.txt"), String("hi"))
		assertCode(t, err, CodePermissionDenied)
	})

	t.Run("CodeCallDepthExceeded: real runaway plugin killed by the sandbox", func(t *testing.T) {
		asm := loadFixture(t)
		_, err := asm.Call("Vmnet.Fixtures.Loops", "Runaway")
		assertCode(t, err, CodeCallDepthExceeded)
	})

	t.Run("CodeStackOverflow", func(t *testing.T) {
		assertCode(t, classify(interpreter.ErrStackOverflow), CodeStackOverflow)
	})

	t.Run("CodeNuGetResolveFailed", func(t *testing.T) {
		assertCode(t, classify(errors.New("nuget: resolving Foo@1.0 (via root): boom")), CodeNuGetResolveFailed)
	})

	t.Run("CodeUnsupportedPackage", func(t *testing.T) {
		assertCode(t, classify(errors.New(`package "Foo" has no usable assembly (check NuGet().Packages() for the reason)`)), CodeUnsupportedPackage)
	})

	t.Run("CodeFieldNotFound", func(t *testing.T) {
		assertCode(t, classify(errors.New(`interpreter: Vmnet.Fixtures.Customer has no field "Bogus"`)), CodeFieldNotFound)
	})

	t.Run("CodeInternal: catch-all for an unrecognized failure", func(t *testing.T) {
		assertCode(t, classify(errors.New("something this classifier has never seen before")), CodeInternal)
	})

	t.Run("already-classified error passes through unchanged", func(t *testing.T) {
		original := &Error{Code: CodeTypeNotFound, Message: "already done"}
		if got := classify(original); got != original {
			t.Errorf("classify(already-*Error) = %#v, want the same pointer back unchanged", got)
		}
	})

	t.Run("classify(nil) is nil", func(t *testing.T) {
		if got := classify(nil); got != nil {
			t.Errorf("classify(nil) = %#v, want nil", got)
		}
	})
}

func assertCode(t *testing.T, err error, want Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want Code %s", want)
	}
	var vErr *Error
	if !errors.As(err, &vErr) {
		t.Fatalf("error = %v (%T), want a *vmnet.Error wrapping it", err, err)
	}
	if vErr.Code != want {
		t.Errorf("Code = %s, want %s (full error: %v)", vErr.Code, want, err)
	}
	if vErr.Cause == nil {
		t.Error("Cause is nil, want the original underlying error preserved")
	}
}

// A quick sanity check that runtime.ManagedException.String() actually
// includes recorded stack frames, matching spec §18.3's own "at
// Type.Method()" line format — TestErrorClassification's own "Details is
// empty" check above already exercises this end to end; this pins the
// exact format down more narrowly.
func TestManagedExceptionStackTraceFormat(t *testing.T) {
	ex := &runtime.ManagedException{TypeName: "System.InvalidOperationException", Message: "boom"}
	ex.PushFrame("Vmnet.Fixtures.Rules::Eval")
	ex.PushFrame("Vmnet.Fixtures.Program::Main")
	got := ex.String()
	want := "System.InvalidOperationException: boom\n   at Vmnet.Fixtures.Rules::Eval()\n   at Vmnet.Fixtures.Program::Main()"
	if got != want {
		t.Errorf("String() =\n%s\nwant:\n%s", got, want)
	}
}
