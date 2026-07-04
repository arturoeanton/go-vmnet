package bcl

import "github.com/arturoeanton/go-vmnet/internal/runtime"

// System.Collections.Generic.Comparer`1 is the abstract IComparer<T> base
// class real code subclasses to implement a custom ordering (Fase 3.39,
// e.g. NPOI's own SharedValueManager.SharedFormulaGroupComparator :
// Comparer<SharedFormulaGroup>) — a real interpreted TypeDef's `: base()`
// constructor chain call still needs somewhere to land, since Comparer`1
// itself has no TypeDef (a BCL type). Its own Compare(T,T) is abstract
// (never called directly, always through the concrete override an
// interpreted method already provides) — only the constructor needs a
// native stub at all.
func init() {
	register("System.Collections.Generic.Comparer`1::.ctor", false, comparerCtorNoop)
}

func comparerCtorNoop(args []runtime.Value) (runtime.Value, error) {
	return runtime.Value{}, nil
}
