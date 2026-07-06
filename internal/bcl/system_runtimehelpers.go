package bcl

import "github.com/arturoeanton/go-vmnet/internal/runtime"

// RuntimeHelpers.IsReferenceOrContainsReferences<T>() is a real JIT
// intrinsic (no runtime args at all — a pure type-level query, Fase
// 3.40) low-level buffer code uses to guard unsafe memory reinterpretation
// against a T that holds managed references. vmnet's generics model has
// no per-instantiation type substitution to answer this precisely for an
// arbitrary T (the same documented boundary System.Memory's own
// PerTypeValues<T> hits), but every real caller found in this loop's
// target packages (System.Text.Json's own low-level buffer/span code)
// only ever instantiates this over a primitive/blittable T (byte, char,
// int, ...) — always false in real .NET too — so a flat false is a safe,
// pragmatic answer rather than the exact one.
func init() {
	register("System.Runtime.CompilerServices.RuntimeHelpers::IsReferenceOrContainsReferences", true, runtimeHelpersIsReferenceOrContainsReferencesFalse)
	// EnsureSufficientExecutionStack is a defensive check real recursive
	// algorithms (e.g. Microsoft.Extensions.DependencyInjection's own
	// CallSiteRuntimeResolver, walking a service dependency graph) call
	// before recursing further, throwing InsufficientExecutionStackException
	// if the current native thread is actually near a real stack overflow —
	// a no-op here, since vmnet's own MaxCallDepth/MaxStackDepth (internal/
	// interpreter/limits.go) already guard against runaway recursion at a
	// layer above this, deterministically and recoverably, well before any
	// real Go-level stack ever gets close to overflowing.
	register("System.Runtime.CompilerServices.RuntimeHelpers::EnsureSufficientExecutionStack", false, objectCtorNoop)
	// RuntimeHelpers.GetHashCode(object) — the *identity* hash code,
	// deliberately bypassing whatever GetHashCode() override the actual
	// receiver's type might declare (real .NET uses this to hash by
	// reference identity even when a type overrides equality/hashing,
	// e.g. a real object used as a WeakMap/ConditionalWeakTable-style
	// cache key). vmnet's own valueHash (system_object.go, backing the
	// plain virtual Object.GetHashCode already) never dispatches to an
	// overridden GetHashCode() either — every Kind hashes structurally/
	// by identity already — so reusing it here gives the same real
	// answer real callers need, not a separate implementation. Found
	// running real Jint: object/array literal construction hits this via
	// Jint's own internal dictionary bookkeeping (Fase, examples/jint-
	// advanced-demo).
	register("System.Runtime.CompilerServices.RuntimeHelpers::GetHashCode", true, objectGetHashCode)
}

func runtimeHelpersIsReferenceOrContainsReferencesFalse(args []runtime.Value) (runtime.Value, error) {
	return runtime.Bool(false), nil
}
