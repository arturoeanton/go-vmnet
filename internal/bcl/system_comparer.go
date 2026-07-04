package bcl

import (
	"fmt"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// System.Collections.Generic.Comparer`1 is the abstract IComparer<T> base
// class real code subclasses to implement a custom ordering (Fase 3.39,
// e.g. NPOI's own SharedValueManager.SharedFormulaGroupComparator :
// Comparer<SharedFormulaGroup>) — a real interpreted TypeDef's `: base()`
// constructor chain call still needs somewhere to land, since Comparer`1
// itself has no TypeDef (a BCL type). Its own Compare(T,T) is abstract
// (never called directly, always through the concrete override an
// interpreted method already provides) — only the constructor needs a
// native stub at all.
//
// Comparer<T>.Default additionally needs a real natural-ordering
// Compare(x,y) (Fase 3.40, found via System.IO.Packaging's own
// PackUriHelper sorting part URIs) — same posture as
// EqualityComparer<T>.Default (system_equalitycomparer.go): a stateless
// sentinel object, comparing by Kind since vmnet's Value has no generic
// IComparable dispatch of its own.
// Comparer<T>::Compare itself is NOT registered here: Comparer<T>.Create
// wraps a real Comparison<T> delegate that needs Machine access to invoke
// (internal/interpreter/comparer.go, machineRegistry) — since a plain
// bcl.Native has none, and vmnet's dispatch tries bcl.Lookup before
// machineRegistry, registering a plain native under the same name here
// would make the delegate-invoking machineRegistry entry unreachable.
// CompareOrdinaryValues is exported so that machineRegistry entry can
// still fall back to natural ordering for the plain Comparer<T>.Default
// sentinel case.
func init() {
	register("System.Collections.Generic.Comparer`1::.ctor", false, comparerCtorNoop)
	register("System.Collections.Generic.Comparer`1::get_Default", true, comparerDefault)
}

func comparerCtorNoop(args []runtime.Value) (runtime.Value, error) {
	return runtime.Value{}, nil
}

func comparerDefault(args []runtime.Value) (runtime.Value, error) {
	return runtime.ObjRef(&runtime.Object{}), nil
}

// CompareOrdinaryValues implements Comparer<T>.Default's natural ordering
// for the primitive/string kinds real callers in this loop's target
// packages actually sort by — vmnet's Value has no generic IComparable
// dispatch, so this switches on Kind directly (same posture linqCompare,
// internal/interpreter/linq.go, takes for LINQ's own OrderBy — kept as a
// separate copy here rather than shared, since interpreter already
// imports bcl and a reverse import would cycle).
func CompareOrdinaryValues(a, b runtime.Value) (int, error) {
	if a.Kind == runtime.KindRef && a.Ref != nil {
		a = *a.Ref
	}
	if b.Kind == runtime.KindRef && b.Ref != nil {
		b = *b.Ref
	}
	if a.Kind == runtime.KindNull || b.Kind == runtime.KindNull {
		switch {
		case a.Kind == runtime.KindNull && b.Kind == runtime.KindNull:
			return 0, nil
		case a.Kind == runtime.KindNull:
			return -1, nil
		default:
			return 1, nil
		}
	}
	switch a.Kind {
	case runtime.KindI4:
		switch {
		case a.I4 < b.I4:
			return -1, nil
		case a.I4 > b.I4:
			return 1, nil
		default:
			return 0, nil
		}
	case runtime.KindI8:
		switch {
		case a.I8 < b.I8:
			return -1, nil
		case a.I8 > b.I8:
			return 1, nil
		default:
			return 0, nil
		}
	case runtime.KindR4:
		switch {
		case a.R4 < b.R4:
			return -1, nil
		case a.R4 > b.R4:
			return 1, nil
		default:
			return 0, nil
		}
	case runtime.KindR8:
		switch {
		case a.R8 < b.R8:
			return -1, nil
		case a.R8 > b.R8:
			return 1, nil
		default:
			return 0, nil
		}
	case runtime.KindString:
		return strings.Compare(a.Str, b.Str), nil
	default:
		return 0, fmt.Errorf("bcl: Comparer<T>.Default: unsupported value kind %v for comparison", a.Kind)
	}
}
