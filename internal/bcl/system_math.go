package bcl

import (
	"fmt"
	"math"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

func init() {
	register("System.Math::Abs", true, mathAbs)
	register("System.Math::Min", true, mathMin)
	register("System.Math::Max", true, mathMax)
	register("System.Double::IsNaN", true, doubleIsNaN)
	register("System.Double::IsInfinity", true, doubleInfinityPredicate(func(f float64) bool { return math.IsInf(f, 0) }))
	register("System.Double::IsPositiveInfinity", true, doubleInfinityPredicate(func(f float64) bool { return math.IsInf(f, 1) }))
	register("System.Double::IsNegativeInfinity", true, doubleInfinityPredicate(func(f float64) bool { return math.IsInf(f, -1) }))
	register("System.Math::Floor", true, mathFloor)
}

func doubleInfinityPredicate(pred func(float64) bool) Native {
	return func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 {
			return runtime.Value{}, fmt.Errorf("bcl: System.Double infinity check expects 1 argument")
		}
		switch v := args[0]; v.Kind {
		case runtime.KindR8:
			return runtime.Bool(pred(v.R8)), nil
		case runtime.KindR4:
			return runtime.Bool(pred(float64(v.R4))), nil
		default:
			return runtime.Value{}, fmt.Errorf("bcl: System.Double infinity check: unsupported argument kind")
		}
	}
}

func mathFloor(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 || args[0].Kind != runtime.KindR8 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Math.Floor expects a double argument")
	}
	return runtime.Float64(math.Floor(args[0].R8)), nil
}

func mathMin(args []runtime.Value) (runtime.Value, error) { return mathMinMax(args, false) }
func mathMax(args []runtime.Value) (runtime.Value, error) { return mathMinMax(args, true) }

func mathMinMax(args []runtime.Value, wantMax bool) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Math.Min/Max expects 2 arguments, got %d", len(args))
	}
	a, b := args[0], args[1]
	if a.Kind != b.Kind {
		return runtime.Value{}, fmt.Errorf("bcl: System.Math.Min/Max: mismatched argument kinds")
	}
	switch a.Kind {
	case runtime.KindI4:
		if (a.I4 > b.I4) == wantMax {
			return a, nil
		}
		return b, nil
	case runtime.KindI8:
		if (a.I8 > b.I8) == wantMax {
			return a, nil
		}
		return b, nil
	case runtime.KindR4:
		if (a.R4 > b.R4) == wantMax {
			return a, nil
		}
		return b, nil
	case runtime.KindR8:
		if (a.R8 > b.R8) == wantMax {
			return a, nil
		}
		return b, nil
	default:
		return runtime.Value{}, fmt.Errorf("bcl: System.Math.Min/Max: unsupported argument kind")
	}
}

func doubleIsNaN(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Double.IsNaN expects 1 argument, got %d", len(args))
	}
	switch v := args[0]; v.Kind {
	case runtime.KindR8:
		return runtime.Bool(math.IsNaN(v.R8)), nil
	case runtime.KindR4:
		return runtime.Bool(math.IsNaN(float64(v.R4))), nil
	default:
		return runtime.Value{}, fmt.Errorf("bcl: System.Double.IsNaN: unsupported argument kind")
	}
}

func mathAbs(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Math.Abs expects 1 argument, got %d", len(args))
	}
	switch v := args[0]; v.Kind {
	case runtime.KindI4:
		return runtime.Int32(int32(math.Abs(float64(v.I4)))), nil
	case runtime.KindI8:
		return runtime.Int64(int64(math.Abs(float64(v.I8)))), nil
	case runtime.KindR4:
		return runtime.Float32(float32(math.Abs(float64(v.R4)))), nil
	case runtime.KindR8:
		return runtime.Float64(math.Abs(v.R8)), nil
	default:
		return runtime.Value{}, fmt.Errorf("bcl: System.Math.Abs: unsupported argument kind")
	}
}
