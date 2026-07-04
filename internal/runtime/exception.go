package runtime

import "fmt"

// ManagedException is a thrown CIL exception object, surfaced to Go as a
// plain error (spec §18.4 ManagedExceptionError, simplified: Fase 2 only
// supports unhandled throw — see docs/en/ROADMAP.md, try/catch/finally are
// deferred).
type ManagedException struct {
	TypeName string
	Message  string
	// Inner is the real Exception object passed to a
	// `new SomeException(message, innerException)`-shaped constructor
	// (real .NET's Exception.InnerException) — nil when none was given.
	// Kept as the original *Object (not just its ManagedException) so
	// `.InnerException` returns something every other Exception member
	// (Message, GetType, ToString, ...) still works against transparently.
	Inner *Object
}

func (e *ManagedException) Error() string {
	msg := e.TypeName
	if e.Message != "" {
		msg = fmt.Sprintf("%s: %s", e.TypeName, e.Message)
	}
	if e.Inner != nil {
		if inner, ok := e.Inner.Native.(*ManagedException); ok {
			msg += " ---> " + inner.Error()
		}
	}
	return msg
}
