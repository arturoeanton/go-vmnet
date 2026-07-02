package interpreter

import "errors"

// Limits bounds one Invoke call for sandboxed execution (spec §13.3, §26.1).
type Limits struct {
	MaxCallDepth    int
	MaxInstructions int64
}

// DefaultLimits returns generous but non-infinite bounds, suitable when the
// caller hasn't configured its own (spec §13.4: execution should never be
// truly unbounded by default).
func DefaultLimits() Limits {
	return Limits{MaxCallDepth: 256, MaxInstructions: 10_000_000}
}

var (
	ErrStackOverflow            = errors.New("interpreter: stack overflow")
	ErrCallDepthExceeded        = errors.New("interpreter: call depth exceeded")
	ErrInstructionLimitExceeded = errors.New("interpreter: instruction limit exceeded")
)
