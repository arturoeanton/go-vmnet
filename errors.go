package vmnet

import (
	"errors"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/interpreter"
	"github.com/arturoeanton/go-vmnet/internal/ir"
	"github.com/arturoeanton/go-vmnet/internal/metadata"
	"github.com/arturoeanton/go-vmnet/internal/pe"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// ManagedException is a thrown-and-unhandled CIL exception, surfaced as a
// wrapped Go error from Call/CallBytes/CallJSON — use errors.As to inspect
// it. Real .NET exceptions caught by the interpreted code's own try/catch
// never reach here at all; only an exception that escapes every catch
// clause in the call chain (Fase 3.10) does.
type ManagedException = runtime.ManagedException

// Code identifies which broad class of failure an Error represents (spec
// §30.2). Stable across releases — new codes may be added, but an
// existing one's meaning never changes, so a caller can safely switch on
// Code today and still compile against a newer vmnet later.
type Code string

// The 14 codes from spec §30.2, plus CodeInternal — a deliberate addition
// beyond the spec's own minimum list, so classify (below) never has to
// leave Code empty for a real failure it can't otherwise place. Every
// code below has at least one real, reproducible trigger exercised by
// TestErrorClassification (vmnet_test.go) — none of these are aspirational.
const (
	CodeInvalidPE            Code = "VMNET_INVALID_PE"
	CodeMissingCLIHeader     Code = "VMNET_MISSING_CLI_HEADER"
	CodeInvalidMetadata      Code = "VMNET_INVALID_METADATA"
	CodeUnsupportedOpcode    Code = "VMNET_UNSUPPORTED_OPCODE"
	CodeUnsupportedBCLMethod Code = "VMNET_UNSUPPORTED_BCL_METHOD"
	CodeTypeNotFound         Code = "VMNET_TYPE_NOT_FOUND"
	CodeMethodNotFound       Code = "VMNET_METHOD_NOT_FOUND"
	CodeFieldNotFound        Code = "VMNET_FIELD_NOT_FOUND"
	CodeStackOverflow        Code = "VMNET_STACK_OVERFLOW"
	CodeCallDepthExceeded    Code = "VMNET_CALL_DEPTH_EXCEEDED"
	CodeManagedException     Code = "VMNET_MANAGED_EXCEPTION"
	CodeNuGetResolveFailed   Code = "VMNET_NUGET_RESOLVE_FAILED"
	CodeUnsupportedPackage   Code = "VMNET_UNSUPPORTED_PACKAGE"
	CodePermissionDenied     Code = "VMNET_PERMISSION_DENIED"

	// CodeInternal is not part of spec §30.2's own 14-code list — a
	// catch-all for a real failure classify couldn't place under any of
	// the other 14 (a new, not-yet-classified error shape, or a plain
	// bug). Treat it as "something went wrong that this version of
	// vmnet doesn't have a specific code for yet", not as its own stable
	// contract the way the other 14 are: a future release may reclassify
	// a specific CodeInternal-today failure under a more precise code.
	CodeInternal Code = "VMNET_INTERNAL_ERROR"
)

// Error is vmnet's own structured error (spec §30.1) — every error
// Call/CallBytes/CallJSON/New/LoadFile/LoadBytes/LoadPackage/NuGet()
// returns is either this type directly or (rarely — a bug, not a
// documented contract) a plain Go error that slipped past classify.
// Always check Code with a plain switch/if, and use errors.As/errors.Is
// against Cause (or the well-known sentinels below) for anything more
// specific — Message/Details are for a human, not for program logic.
type Error struct {
	// Code is this failure's stable classification (see the Code* consts
	// above) — the one field safe to switch/if on across releases.
	Code Code
	// Message is a short, one-line, human-readable summary — safe to
	// log or show a user directly.
	Message string
	// Details is optional extra context (spec §30.3's own "Method:"/
	// "Required by:"/"Suggestion:" style block) — a *ManagedException's
	// own full spec §18.3 stack trace lands here (via
	// ManagedException.String()), not in Message, so Message stays a
	// single line.
	Details string
	// Cause is the real underlying error — unwrap with errors.Unwrap/
	// errors.As/errors.Is to reach a specific sentinel (pe.ErrInvalidPE,
	// metadata.ErrOutOfRange, a *ManagedException, ...) this package
	// doesn't re-export itself.
	Cause error
}

func (e *Error) Error() string {
	if e.Message == "" {
		return string(e.Code)
	}
	return string(e.Code) + ": " + e.Message
}

func (e *Error) Unwrap() error { return e.Cause }

// classify wraps err (never nil) as an *Error with its best-effort Code —
// called once, at the outermost public API boundary (Call/CallBytes/
// CallJSON/New/LoadFile/LoadBytes/LoadPackage/NuGetManager.Add/Restore),
// never internally, so every internal fmt.Errorf call site keeps its own
// unstructured message untouched. Classification is layered: an exact Go
// error TYPE/sentinel match first (reliable, never wrong), a message-
// content match only for the handful of real, well-established phrasings
// that have no dedicated sentinel today (internal/metadata's own
// TypeDef/MethodDef "not found" messages, internal/interpreter's own
// field-not-found messages, internal/nuget's own plain-string errors) —
// honest best-effort, not a guaranteed-correct parse of arbitrary error
// text. Falls back to CodeInternal, never leaves Code empty.
func classify(err error) *Error {
	if err == nil {
		return nil
	}
	if e, ok := err.(*Error); ok {
		return e // already classified (e.g. re-wrapped by a caller) — don't double-wrap
	}

	var mex *ManagedException
	if errors.As(err, &mex) {
		code := CodeManagedException
		// System.UnauthorizedAccessException is the one real .NET
		// exception type the Permissions gate (internal/runtime/
		// permissions.go) always raises for a denied capability — see
		// docs/en/security.md. Every other ManagedException is a real
		// thrown-and-uncaught .NET exception on its own terms.
		if mex.TypeName == "System.UnauthorizedAccessException" {
			code = CodePermissionDenied
		}
		return &Error{Code: code, Message: mex.Error(), Details: mex.String(), Cause: err}
	}

	var opErr *ir.UnsupportedOpcodeError
	if errors.As(err, &opErr) {
		return &Error{Code: CodeUnsupportedOpcode, Message: err.Error(), Cause: err}
	}

	var bclErr *interpreter.UnsupportedBCLMethodError
	if errors.As(err, &bclErr) {
		return &Error{Code: CodeUnsupportedBCLMethod, Message: err.Error(), Details: "Method: " + bclErr.Method, Cause: err}
	}

	switch {
	case errors.Is(err, interpreter.ErrStackOverflow):
		return &Error{Code: CodeStackOverflow, Message: err.Error(), Cause: err}
	case errors.Is(err, interpreter.ErrCallDepthExceeded),
		errors.Is(err, interpreter.ErrInstructionLimitExceeded),
		errors.Is(err, interpreter.ErrArrayTooLarge),
		errors.Is(err, interpreter.ErrStringTooLarge):
		// All four are "a configured execution-resource limit was
		// exceeded" (spec §26.1/§13.3) — spec §30.2 only lists one code
		// for this whole family, not one per specific limit.
		return &Error{Code: CodeCallDepthExceeded, Message: err.Error(), Cause: err}
	case errors.Is(err, pe.ErrMissingCLIHeader):
		return &Error{Code: CodeMissingCLIHeader, Message: err.Error(), Cause: err}
	case errors.Is(err, pe.ErrInvalidPE), errors.Is(err, pe.ErrInvalidRVA):
		return &Error{Code: CodeInvalidPE, Message: err.Error(), Cause: err}
	case errors.Is(err, pe.ErrInvalidMetadataRoot),
		errors.Is(err, metadata.ErrInvalidMetadataRoot),
		errors.Is(err, metadata.ErrMissingStream),
		errors.Is(err, metadata.ErrUnsupportedTable):
		return &Error{Code: CodeInvalidMetadata, Message: err.Error(), Cause: err}
	}

	msg := err.Error()

	// assembly.go's own resolveMethod (the Call/CallBytes boundary's
	// "type or method not found by that name" path) wraps EITHER a
	// missing-TypeDef or a no-matching-overload failure under the same
	// runtime.ErrMethodNotFound sentinel (interpolated with %v, not %w —
	// so metadata.ErrOutOfRange itself, checked above, is NOT reachable
	// via errors.Is through this specific wrap even when that's the real
	// underlying cause) — distinguished here by the message's own
	// well-established "type X.Y not found" phrasing (assembly.go's
	// notFoundErr always includes the real cause's own text).
	if errors.Is(err, runtime.ErrMethodNotFound) {
		if strings.Contains(msg, "type ") && strings.Contains(msg, "not found") {
			return &Error{Code: CodeTypeNotFound, Message: msg, Cause: err}
		}
		return &Error{Code: CodeMethodNotFound, Message: msg, Cause: err}
	}

	// internal/metadata's own OTHER TypeDef/MethodDef lookup failures
	// (Fase 1, resolver.go, reached through a path that DOES preserve
	// %w — e.g. field/property reflection) wrap the shared ErrOutOfRange
	// sentinel with a message naming which kind of lookup failed — no
	// dedicated per-kind sentinel exists (ErrOutOfRange itself covers
	// many unrelated bounds-check failures too), so this distinguishes
	// by the message's own phrasing the same way.
	if errors.Is(err, metadata.ErrOutOfRange) {
		switch {
		case strings.Contains(msg, "type ") && strings.Contains(msg, "not found"):
			return &Error{Code: CodeTypeNotFound, Message: msg, Cause: err}
		case strings.Contains(msg, "method ") && strings.Contains(msg, "not found"):
			return &Error{Code: CodeMethodNotFound, Message: msg, Cause: err}
		}
		return &Error{Code: CodeInvalidMetadata, Message: msg, Cause: err}
	}

	// internal/interpreter's own field-access failures (eval.go's
	// fieldSlot and friends) — "TypeName has no field "FieldName"" /
	// ""...has no static field..." — likewise no dedicated sentinel.
	if strings.Contains(msg, "has no field ") || strings.Contains(msg, "has no static field ") {
		return &Error{Code: CodeFieldNotFound, Message: msg, Cause: err}
	}

	// internal/nuget's own errors are all plain "nuget: ..." strings
	// (Fase 3.29+) with no sentinels at all — LoadPackage's own "package
	// %q has no usable assembly" (no compatible TFM/asset selected) is
	// the one real "unsupported package" shape found so far; every
	// other nuget failure (network, parse, dependency resolution) is a
	// resolve failure.
	if strings.Contains(msg, "nuget:") || strings.Contains(msg, "has no usable assembly") {
		if strings.Contains(msg, "has no usable assembly") {
			return &Error{Code: CodeUnsupportedPackage, Message: msg, Cause: err}
		}
		return &Error{Code: CodeNuGetResolveFailed, Message: msg, Cause: err}
	}

	return &Error{Code: CodeInternal, Message: msg, Cause: err}
}
