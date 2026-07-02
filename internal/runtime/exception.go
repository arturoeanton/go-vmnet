package runtime

import "fmt"

// ManagedException is a thrown CIL exception object, surfaced to Go as a
// plain error (spec §18.4 ManagedExceptionError, simplified: Fase 2 only
// supports unhandled throw — see docs/ROADMAP.md, try/catch/finally are
// deferred).
type ManagedException struct {
	TypeName string
	Message  string
}

func (e *ManagedException) Error() string {
	if e.Message == "" {
		return e.TypeName
	}
	return fmt.Sprintf("%s: %s", e.TypeName, e.Message)
}
