package interpreter

import (
	"sort"

	"github.com/arturoeanton/go-vmnet/internal/ir"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// unwind tracks an in-flight control transfer that must run one or more
// intervening finally/fault handlers before it can complete — either a
// `leave` (target is the eventual jump destination) or an exception
// propagating outward looking for a catch (exception is set instead).
// pending holds the remaining handler candidates to try, innermost first;
// each `endfinally` advances past the one that just ran.
type unwind struct {
	target    int
	exception *runtime.ManagedException
	pending   []ir.Handler
}

// handlersContaining returns method's handlers whose try region contains
// ip, sorted innermost-first (narrowest TryEnd-TryStart range) — properly
// nested try blocks always have this property, and sibling catch clauses
// for the very same try region keep their relative (table) order, which
// is exactly the order they must be tried in (spec §III.3.16).
func handlersContaining(method *runtime.Method, ip int) []ir.Handler {
	var out []ir.Handler
	for _, h := range method.Handlers {
		if h.TryStart <= ip && ip < h.TryEnd {
			out = append(out, h)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return (out[i].TryEnd - out[i].TryStart) < (out[j].TryEnd - out[j].TryStart)
	})
	return out
}

// handlersLeaving returns method's finally handlers a `leave` from ip to
// target must run: those whose try region contains ip but does NOT
// contain target (a leave that stays within the same try, jumping between
// two points inside it, doesn't need to unwind out of it at all).
//
// Fault handlers are deliberately excluded here (Fase 3.42, found via a
// real, minimal repro: a `yield return` method whose body does `foreach`
// over another collection — the exact shape the C# compiler generates a
// `fault` clause for around the inner foreach's own MoveNext/Dispose, to
// guarantee cleanup only on an abnormal exit). Per ECMA-335 III.3.65
// (leave) and I.12.4.2.5, a `fault` handler is a "finally that only runs
// when an exception is propagating" — ordinary `leave` is BY DEFINITION
// the non-exceptional path, so a fault handler must never run for it,
// only for an exception actually unwinding through (handlersContaining,
// consulted from dispatchException, correctly has no such Catch-only
// filter to begin with). Before this fix, a real `MoveNext()`'s own
// ordinary `leave` (taken every time a `yield return` successfully
// returns true) incorrectly ran the surrounding fault handler as if
// leaving via an exception — calling the state machine's own Dispose()/
// `<>m__FinallyN()` immediately after its FIRST successful yield, which
// disposes the hoisted inner enumerator and resets `<>1__state` to its
// terminal value. The first yielded item still came back correctly
// (already captured before the errant dispose ran), but every
// subsequent MoveNext() call then saw an already-disposed, terminal
// state and returned false — silently truncating every such iterator to
// exactly one element, regardless of the real collection's length
// (confirmed both via a standalone repro and via the real bug this was
// chasing: DocumentFormat.OpenXml's own PackageFeatureBase.
// RelationshipCollection.GetEnumerator(), `foreach (var v in
// Relationships.Values) { yield return v; }`, only ever yielding the
// first real package relationship out of a real .rels file's several).
func handlersLeaving(method *runtime.Method, ip, target int) []ir.Handler {
	var out []ir.Handler
	for _, h := range method.Handlers {
		if h.Kind != ir.HandlerFinally {
			continue
		}
		inTry := h.TryStart <= ip && ip < h.TryEnd
		targetAlsoInTry := h.TryStart <= target && target < h.TryEnd
		if inTry && !targetAlsoInTry {
			out = append(out, h)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return (out[i].TryEnd - out[i].TryStart) < (out[j].TryEnd - out[j].TryStart)
	})
	return out
}

// dispatchException searches candidates (from handlersContaining, or a
// suffix of a previous search resumed after a finally/fault ran) for a
// matching catch, running any finally/fault found along the way first.
// Returns whether a handler was entered — on true, frame.IP/Stack (and
// frame.unwind, if a finally/fault still needs to run before a catch)
// are already set up to resume execution; on false, the exception
// propagates out of this method entirely.
func (m *Machine) dispatchException(frame *Frame, ex *runtime.ManagedException, candidates []ir.Handler) bool {
	for i, h := range candidates {
		switch h.Kind {
		case ir.HandlerCatch:
			if !m.exceptionMatchesCatch(ex, h.CatchTypeFullName) {
				continue
			}
			frame.Stack = frame.Stack[:0]
			frame.push(runtime.ObjRef(&runtime.Object{Native: ex}))
			frame.currentException = ex
			frame.IP = h.HandlerStart
			return true
		case ir.HandlerFinally, ir.HandlerFault:
			frame.Stack = frame.Stack[:0]
			frame.unwind = &unwind{exception: ex, pending: candidates[i+1:]}
			frame.IP = h.HandlerStart
			return true
		}
	}
	return false
}

// exceptionMatchesCatch reuses isAssignableTo's real class-hierarchy walk
// (Fase 3.8) plus typecheck.go's hand-maintained exception hierarchy —
// the same mechanism a real `isinst`/`is` check against the exception
// would use, so "which catch clause matches" agrees with "what `ex is
// SomeType` would say" elsewhere in the same program.
func (m *Machine) exceptionMatchesCatch(ex *runtime.ManagedException, catchTypeFullName string) bool {
	return m.isAssignableTo(runtime.ObjRef(&runtime.Object{Native: ex}), catchTypeFullName)
}

// resumeAfterFinally implements endfinally/endfault: continue whichever
// control transfer (leave or exception) brought execution into the
// finally/fault block that just ended. Returns the IR index to resume at,
// or a non-nil exception if the transfer was an exception unwind that
// found no further handler and must propagate out of the method.
func (m *Machine) resumeAfterFinally(frame *Frame) (next int, propagate *runtime.ManagedException, err error) {
	u := frame.unwind
	if u == nil {
		return 0, nil, errEndfinallyOutsideHandler
	}
	frame.unwind = nil

	if u.exception == nil {
		// leave-chaining: pending is only ever finally/fault handlers,
		// run in order; once none remain, jump to the leave's real target.
		if len(u.pending) == 0 {
			return u.target, nil, nil
		}
		frame.unwind = &unwind{target: u.target, pending: u.pending[1:]}
		return u.pending[0].HandlerStart, nil, nil
	}

	if m.dispatchException(frame, u.exception, u.pending) {
		return frame.IP, nil, nil
	}
	return 0, u.exception, nil
}
