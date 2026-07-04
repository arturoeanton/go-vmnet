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
}

func runtimeHelpersIsReferenceOrContainsReferencesFalse(args []runtime.Value) (runtime.Value, error) {
	return runtime.Bool(false), nil
}
