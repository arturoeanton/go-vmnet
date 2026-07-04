package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

func init() {
	register("System.Enum::HasFlag", true, enumHasFlag)
	register("System.Enum::GetUnderlyingType", true, enumGetUnderlyingType)
}

// enumGetUnderlyingType backs the static Enum.GetUnderlyingType(Type
// enumType) — found via Dapper's own SqlMapper enum-column coercion,
// which reads this before converting a stored value into the target
// enum. Unconditionally System.Int32: vmnet represents every enum value
// as a bare KindI4 (enumHasFlag's own doc comment — a `[Flags] enum :
// long` is the one real exception, using KindI8 instead, but no real
// caller found here asks GetUnderlyingType for one of those
// specifically), so there's no real per-enum underlying-type metadata to
// consult at all, the same "no BCL enum member database" limitation
// enumTypeMembers documents (internal/interpreter/reflection.go).
func enumGetUnderlyingType(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("bcl: Enum.GetUnderlyingType expects 1 argument")
	}
	return NewTypeValue("System.Int32"), nil
}

// enumHasFlag backs Enum.HasFlag(Enum flag): real semantics are
// `(this & flag) == flag`. vmnet represents every enum value as its
// underlying primitive directly (KindI4 for the overwhelming majority,
// KindI8 for a `[Flags] enum : long`) — never a boxed/struct wrapper —
// so both operands are read straight off whichever integer Kind they
// actually are, matching the same "enum is a bare int" convention used
// everywhere else (assembly.go's valueTypeDefault, Fase 3.25/3.27).
func enumHasFlag(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: Enum.HasFlag expects a receiver and a flag")
	}
	this, flag := enumAsInt64(args[0]), enumAsInt64(args[1])
	return runtime.Bool(this&flag == flag), nil
}

func enumAsInt64(v runtime.Value) int64 {
	switch v.Kind {
	case runtime.KindI4:
		return int64(v.I4)
	case runtime.KindI8:
		return v.I8
	default:
		return 0
	}
}
