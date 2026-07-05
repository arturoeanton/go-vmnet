package interpreter

import "errors"

// Limits bounds one Invoke call for sandboxed execution (spec §13.3, §26.1).
type Limits struct {
	MaxCallDepth    int
	MaxInstructions int64
	MaxStackDepth   int // evaluation-stack depth, not call depth
	MaxArrayLength  int // spec §26.1: newarr with an adversarial length must not OOM the host
	MaxStringBytes  int // spec §26.1's own string analogue — see the doc comment on the check site (calls.go's tryCall)
}

// DefaultLimits returns generous but non-infinite bounds, suitable when the
// caller hasn't configured its own (spec §13.4: execution should never be
// truly unbounded by default). MaxStackDepth exists mainly to bound memory:
// without it, IR that pushes without popping (buggy or adversarial) grows
// the stack until MaxInstructions trips, by which point it could be a
// large amount of memory. MaxArrayLength is the same idea for a single
// newarr allocation (Fase 3.5); MaxStringBytes is the same idea again for a
// single string-producing native call (Fase 3.72) — `new string('x',
// int.MaxValue)`/`"x".PadLeft(int.MaxValue)` are single, real, adversarial
// call sites that would otherwise attempt a multi-gigabyte allocation
// before MaxInstructions ever gets a chance to trip, exactly the same
// "one call, not a loop MaxInstructions would catch" shape MaxArrayLength
// already closed for arrays.
func DefaultLimits() Limits {
	return Limits{MaxCallDepth: 256, MaxInstructions: 10_000_000, MaxStackDepth: 10_000, MaxArrayLength: 16 << 20, MaxStringBytes: 64 << 20}
}

var (
	ErrStackOverflow            = errors.New("interpreter: stack overflow")
	ErrCallDepthExceeded        = errors.New("interpreter: call depth exceeded")
	ErrInstructionLimitExceeded = errors.New("interpreter: instruction limit exceeded")
	ErrArrayTooLarge            = errors.New("interpreter: array length exceeds MaxArrayLength")
	ErrStringTooLarge           = errors.New("interpreter: string length exceeds MaxStringBytes")

	errEndfinallyOutsideHandler = errors.New("interpreter: endfinally outside a finally/fault handler")
	errRethrowOutsideCatch      = errors.New("interpreter: rethrow outside a catch handler")
	errEndfilterOutsideFilter   = errors.New("interpreter: endfilter outside a filter handler")
)
