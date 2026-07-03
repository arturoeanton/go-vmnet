package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

func init() {
	machineRegistry["System.Type::IsAssignableFrom"] = typeIsAssignableFrom
}

// typeIsAssignableFrom implements Type.IsAssignableFrom(Type) — deferred
// out of Fase 3.14 as needing Machine access (a plain bcl.Native can't
// walk the real class/interface hierarchy, since that needs
// Machine.ResolveType), now mechanically simple once the Machine-aware
// native registry existed for LINQ (Fase 3.15). Both operands are
// System.Type values carrying only a FullName string (bcl.NewTypeValue),
// so this re-derives isAssignableTo's logic (Fase 3.8) starting from a
// type NAME rather than an already-known runtime.Value/Kind: an exact
// name match (or either side being System.Object) short-circuits, then
// the candidate's real TypeDef is resolved and walked via typeMatches —
// the same hierarchy walk isinst/castclass and exception catch-matching
// (Fase 3.13) already use.
func typeIsAssignableFrom(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.IsAssignableFrom expects (this, other)")
	}
	if args[1].Kind == runtime.KindNull {
		return runtime.Bool(false), nil
	}
	target, ok := bcl.TypeFullNameOf(args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.IsAssignableFrom receiver is not a Type")
	}
	candidate, ok := bcl.TypeFullNameOf(args[1])
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: Type.IsAssignableFrom argument is not a Type")
	}
	if target == candidate || target == "System.Object" {
		return runtime.Bool(true), nil
	}
	if m.ResolveType == nil {
		return runtime.Bool(false), nil
	}
	t, err := m.ResolveType(candidate)
	if err != nil {
		return runtime.Bool(false), nil
	}
	return runtime.Bool(m.typeMatches(t, target)), nil
}
