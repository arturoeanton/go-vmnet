package interpreter

import "errors"

// Limits bounds one Invoke call for sandboxed execution (spec §13.3, §26.1).
type Limits struct {
	MaxCallDepth    int
	MaxInstructions int64
	MaxStackDepth   int // evaluation-stack depth, not call depth
}

// DefaultLimits returns generous but non-infinite bounds, suitable when the
// caller hasn't configured its own (spec §13.4: execution should never be
// truly unbounded by default). MaxStackDepth exists mainly to bound memory:
// without it, IR that pushes without popping (buggy or adversarial) grows
// the stack until MaxInstructions trips, by which point it could be a
// large amount of memory.
func DefaultLimits() Limits {
	return Limits{MaxCallDepth: 256, MaxInstructions: 10_000_000, MaxStackDepth: 10_000}
}

var (
	ErrStackOverflow            = errors.New("interpreter: stack overflow")
	ErrCallDepthExceeded        = errors.New("interpreter: call depth exceeded")
	ErrInstructionLimitExceeded = errors.New("interpreter: instruction limit exceeded")
)
