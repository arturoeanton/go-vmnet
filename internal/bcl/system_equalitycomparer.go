package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// EqualityComparer<T>.Default reuses the exact same value/reference
// equality (valuesEqual) and hash (valueHash) system_object.go's
// Object::Equals/GetHashCode already implement (Fase 3.7) — it's the
// same default comparison logic the CLR falls back to absent a
// type-specific IEquatable<T> override, which is the common case for
// generic collection/comparison code (HashSet<T>, Dictionary<K,V>'s own
// key comparisons, LINQ's Distinct/GroupBy, ...).
func init() {
	register("System.Collections.Generic.EqualityComparer`1::get_Default", true, equalityComparerDefault)
	register("System.Collections.Generic.EqualityComparer`1::Equals", true, equalityComparerEquals)
	register("System.Collections.Generic.EqualityComparer`1::GetHashCode", true, equalityComparerGetHashCode)
}

// equalityComparerDefault returns a stateless sentinel — same pattern as
// Encoding.UTF8 (system_text.go): every EqualityComparer<T>.Default
// instance behaves identically regardless of which object represents it.
func equalityComparerDefault(args []runtime.Value) (runtime.Value, error) {
	return runtime.ObjRef(&runtime.Object{}), nil
}

func equalityComparerEquals(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 3 {
		return runtime.Value{}, fmt.Errorf("bcl: EqualityComparer.Equals expects (this, x, y)")
	}
	return runtime.Bool(valuesEqual(args[1], args[2])), nil
}

func equalityComparerGetHashCode(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: EqualityComparer.GetHashCode expects (this, obj)")
	}
	return runtime.Int32(valueHash(args[1])), nil
}
