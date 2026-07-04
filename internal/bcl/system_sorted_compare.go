package bcl

import (
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// compareValuesNatural is SortedSet<T>/SortedDictionary<K,V>'s default
// (no explicit IComparer<T> — neither type's constructor overload taking
// one is wired up yet, a documented gap noted where each is registered)
// key ordering. This package has no Machine access (unlike
// interpreter/comparer.go's own compareNatural, which can fall back to
// dispatching a real IComparable<T>/IComparable override), so a
// KindObject/KindStruct key — a plugin class or ValueTuple used as a
// sorted-collection key, an uncommon but real pattern — falls back to
// "equal" (0) rather than guessing an order: consistently wrong-but-
// stable (every such key sorts as a tie, so insertion order among them
// is preserved by nativeDict/nativeHashSet's own stable-scan insertion)
// beats a nonsensical or panicking comparison. The common real-world
// case (int/string keys) is handled exactly like every other natural-
// ordering helper in this codebase.
func compareValuesNatural(a, b runtime.Value) int {
	switch a.Kind {
	case runtime.KindI4:
		switch {
		case a.I4 < b.I4:
			return -1
		case a.I4 > b.I4:
			return 1
		default:
			return 0
		}
	case runtime.KindI8:
		switch {
		case a.I8 < b.I8:
			return -1
		case a.I8 > b.I8:
			return 1
		default:
			return 0
		}
	case runtime.KindR4:
		switch {
		case a.R4 < b.R4:
			return -1
		case a.R4 > b.R4:
			return 1
		default:
			return 0
		}
	case runtime.KindR8:
		switch {
		case a.R8 < b.R8:
			return -1
		case a.R8 > b.R8:
			return 1
		default:
			return 0
		}
	case runtime.KindString:
		return strings.Compare(a.Str, b.Str)
	default:
		return 0
	}
}
