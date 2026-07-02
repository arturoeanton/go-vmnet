package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/ir"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

func evalBinOp(in ir.BinOp, a, b runtime.Value) (runtime.Value, error) {
	if a.Kind != b.Kind {
		return runtime.Value{}, fmt.Errorf("interpreter: binary op on mismatched value kinds (%d, %d)", a.Kind, b.Kind)
	}
	switch a.Kind {
	case runtime.KindI4:
		if in.Unsigned {
			return evalBinOpUint(in.Op, uint32(a.I4), uint32(b.I4), func(v uint32) runtime.Value { return runtime.Int32(int32(v)) })
		}
		return evalBinOpInt(in.Op, a.I4, b.I4, runtime.Int32)
	case runtime.KindI8:
		if in.Unsigned {
			return evalBinOpUint(in.Op, uint64(a.I8), uint64(b.I8), func(v uint64) runtime.Value { return runtime.Int64(int64(v)) })
		}
		return evalBinOpInt(in.Op, a.I8, b.I8, runtime.Int64)
	case runtime.KindR4:
		return evalBinOpFloat(in.Op, a.R4, b.R4, runtime.Float32)
	case runtime.KindR8:
		return evalBinOpFloat(in.Op, a.R8, b.R8, runtime.Float64)
	default:
		return runtime.Value{}, fmt.Errorf("interpreter: binary op on unsupported value kind %d", a.Kind)
	}
}

func evalBinOpInt[T int32 | int64](op ir.BinOpKind, a, b T, wrap func(T) runtime.Value) (runtime.Value, error) {
	switch op {
	case ir.OpAdd:
		return wrap(a + b), nil
	case ir.OpSub:
		return wrap(a - b), nil
	case ir.OpMul:
		return wrap(a * b), nil
	case ir.OpDiv:
		if b == 0 {
			return runtime.Value{}, fmt.Errorf("interpreter: integer divide by zero")
		}
		return wrap(a / b), nil
	case ir.OpRem:
		if b == 0 {
			return runtime.Value{}, fmt.Errorf("interpreter: integer divide by zero")
		}
		return wrap(a % b), nil
	case ir.OpAnd:
		return wrap(a & b), nil
	case ir.OpOr:
		return wrap(a | b), nil
	case ir.OpXor:
		return wrap(a ^ b), nil
	case ir.OpShl:
		return wrap(a << uint(b)), nil
	case ir.OpShr:
		return wrap(a >> uint(b)), nil // arithmetic (sign-extending) shift, correct for signed T
	case ir.OpCeq:
		return runtime.Bool(a == b), nil
	case ir.OpCgt:
		return runtime.Bool(a > b), nil
	case ir.OpClt:
		return runtime.Bool(a < b), nil
	default:
		return runtime.Value{}, fmt.Errorf("interpreter: unsupported integer binary op %d", op)
	}
}

// evalBinOpUint backs the .un opcodes (div.un/rem.un/shr.un/cgt.un/clt.un):
// the same bit pattern as evalBinOpInt's T, but compared/divided/shifted
// as unsigned. This matters for real code, not just edge cases — e.g. the
// extremely common range-check idiom `(uint)(c - low) <= high` relies on
// unsigned wraparound turning "c < low" into a huge value that fails the
// `<=`, and gets a silently wrong answer under signed comparison.
func evalBinOpUint[T uint32 | uint64](op ir.BinOpKind, a, b T, wrap func(T) runtime.Value) (runtime.Value, error) {
	switch op {
	case ir.OpDiv:
		if b == 0 {
			return runtime.Value{}, fmt.Errorf("interpreter: integer divide by zero")
		}
		return wrap(a / b), nil
	case ir.OpRem:
		if b == 0 {
			return runtime.Value{}, fmt.Errorf("interpreter: integer divide by zero")
		}
		return wrap(a % b), nil
	case ir.OpShr:
		return wrap(a >> uint(b)), nil // logical (zero-fill) shift
	case ir.OpCgt:
		return runtime.Bool(a > b), nil
	case ir.OpClt:
		return runtime.Bool(a < b), nil
	default:
		return runtime.Value{}, fmt.Errorf("interpreter: unsupported unsigned integer binary op %d", op)
	}
}

func evalBinOpFloat[T float32 | float64](op ir.BinOpKind, a, b T, wrap func(T) runtime.Value) (runtime.Value, error) {
	switch op {
	case ir.OpAdd:
		return wrap(a + b), nil
	case ir.OpSub:
		return wrap(a - b), nil
	case ir.OpMul:
		return wrap(a * b), nil
	case ir.OpDiv:
		return wrap(a / b), nil
	case ir.OpCeq:
		return runtime.Bool(a == b), nil
	case ir.OpCgt:
		return runtime.Bool(a > b), nil
	case ir.OpClt:
		return runtime.Bool(a < b), nil
	default:
		return runtime.Value{}, fmt.Errorf("interpreter: unsupported float binary op %d", op)
	}
}

func evalNeg(v runtime.Value) (runtime.Value, error) {
	switch v.Kind {
	case runtime.KindI4:
		return runtime.Int32(-v.I4), nil
	case runtime.KindI8:
		return runtime.Int64(-v.I8), nil
	case runtime.KindR4:
		return runtime.Float32(-v.R4), nil
	case runtime.KindR8:
		return runtime.Float64(-v.R8), nil
	default:
		return runtime.Value{}, fmt.Errorf("interpreter: neg on unsupported value kind %d", v.Kind)
	}
}

func evalNot(v runtime.Value) (runtime.Value, error) {
	switch v.Kind {
	case runtime.KindI4:
		return runtime.Int32(^v.I4), nil
	case runtime.KindI8:
		return runtime.Int64(^v.I8), nil
	default:
		return runtime.Value{}, fmt.Errorf("interpreter: not on unsupported value kind %d", v.Kind)
	}
}

func evalConv(kind ir.ConvKind, v runtime.Value) (runtime.Value, error) {
	switch kind {
	case ir.ConvR4, ir.ConvR8:
		f, err := toFloat64(v)
		if err != nil {
			return runtime.Value{}, err
		}
		if kind == ir.ConvR4 {
			return runtime.Float32(float32(f)), nil
		}
		return runtime.Float64(f), nil
	}

	i64, err := toInt64(v)
	if err != nil {
		return runtime.Value{}, err
	}
	switch kind {
	case ir.ConvI1:
		return runtime.Int32(int32(int8(i64))), nil
	case ir.ConvU1:
		return runtime.Int32(int32(uint8(i64))), nil
	case ir.ConvI2:
		return runtime.Int32(int32(int16(i64))), nil
	case ir.ConvU2:
		return runtime.Int32(int32(uint16(i64))), nil
	case ir.ConvI4, ir.ConvU4:
		return runtime.Int32(int32(i64)), nil
	case ir.ConvI8, ir.ConvU8:
		return runtime.Int64(i64), nil
	default:
		return runtime.Value{}, fmt.Errorf("interpreter: unsupported conv kind %d", kind)
	}
}

func evalCompare(in ir.BranchCompare, a, b runtime.Value) (bool, error) {
	if a.Kind != b.Kind {
		return false, fmt.Errorf("interpreter: compare on mismatched value kinds (%d, %d)", a.Kind, b.Kind)
	}
	switch a.Kind {
	case runtime.KindI4:
		if in.Unsigned {
			return compareOrdered(in.Op, uint32(a.I4), uint32(b.I4)), nil
		}
		return compareOrdered(in.Op, a.I4, b.I4), nil
	case runtime.KindI8:
		if in.Unsigned {
			return compareOrdered(in.Op, uint64(a.I8), uint64(b.I8)), nil
		}
		return compareOrdered(in.Op, a.I8, b.I8), nil
	case runtime.KindR4:
		return compareOrdered(in.Op, a.R4, b.R4), nil
	case runtime.KindR8:
		return compareOrdered(in.Op, a.R8, b.R8), nil
	default:
		return false, fmt.Errorf("interpreter: compare on unsupported value kind %d", a.Kind)
	}
}

func compareOrdered[T int32 | int64 | uint32 | uint64 | float32 | float64](op ir.CompareOp, a, b T) bool {
	switch op {
	case ir.CmpEq:
		return a == b
	case ir.CmpNe:
		return a != b
	case ir.CmpGe:
		return a >= b
	case ir.CmpGt:
		return a > b
	case ir.CmpLe:
		return a <= b
	case ir.CmpLt:
		return a < b
	default:
		return false
	}
}

func toInt64(v runtime.Value) (int64, error) {
	switch v.Kind {
	case runtime.KindI4:
		return int64(v.I4), nil
	case runtime.KindI8:
		return v.I8, nil
	case runtime.KindR4:
		return int64(v.R4), nil
	case runtime.KindR8:
		return int64(v.R8), nil
	default:
		return 0, fmt.Errorf("interpreter: cannot convert value kind %d to integer", v.Kind)
	}
}

func toFloat64(v runtime.Value) (float64, error) {
	switch v.Kind {
	case runtime.KindI4:
		return float64(v.I4), nil
	case runtime.KindI8:
		return float64(v.I8), nil
	case runtime.KindR4:
		return float64(v.R4), nil
	case runtime.KindR8:
		return v.R8, nil
	default:
		return 0, fmt.Errorf("interpreter: cannot convert value kind %d to float", v.Kind)
	}
}
