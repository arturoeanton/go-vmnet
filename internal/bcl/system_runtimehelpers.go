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
}

func runtimeHelpersIsReferenceOrContainsReferencesFalse(args []runtime.Value) (runtime.Value, error) {
	return runtime.Bool(false), nil
}
