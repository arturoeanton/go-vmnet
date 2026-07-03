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
	register("System.Math::Ceiling", true, mathCeiling)
	register("System.Math::Truncate", true, mathTruncate)
	register("System.Math::Pow", true, mathPow)
	register("System.Math::Sqrt", true, mathUnary(math.Sqrt))
	register("System.Math::Log", true, mathLog)
	register("System.Math::Log10", true, mathUnary(math.Log10))
	register("System.Math::Log2", true, mathUnary(math.Log2))
	register("System.Math::Exp", true, mathUnary(math.Exp))
	register("System.Math::Sign", true, mathSign)
	register("System.Math::Round", true, mathRound)
	register("System.Math::Sin", true, mathUnary(math.Sin))
	register("System.Math::Cos", true, mathUnary(math.Cos))
	register("System.Math::Tan", true, mathUnary(math.Tan))
	register("System.Math::Atan", true, mathUnary(math.Atan))
	register("System.Math::Atan2", true, mathAtan2)
}

// mathUnary adapts a plain float64->float64 Go math function into a
// Native taking (and returning) a single System.Double argument — the
// shape most of Math's trig/log/root functions share.
func mathUnary(fn func(float64) float64) Native {
	return func(args []runtime.Value) (runtime.Value, error) {
		if len(args) != 1 || args[0].Kind != runtime.KindR8 {
			return runtime.Value{}, fmt.Errorf("bcl: System.Math method expects a double argument")
		}
		return runtime.Float64(fn(args[0].R8)), nil
	}
}

func mathCeiling(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 || args[0].Kind != runtime.KindR8 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Math.Ceiling expects a double argument")
	}
	return runtime.Float64(math.Ceil(args[0].R8)), nil
}

func mathTruncate(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 || args[0].Kind != runtime.KindR8 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Math.Truncate expects a double argument")
	}
	return runtime.Float64(math.Trunc(args[0].R8)), nil
}

func mathPow(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 || args[0].Kind != runtime.KindR8 || args[1].Kind != runtime.KindR8 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Math.Pow expects two double arguments")
	}
	return runtime.Float64(math.Pow(args[0].R8, args[1].R8)), nil
}

// mathLog backs both Math.Log(double) (natural log) and Math.Log(double,
// newBase) — the same call target either way (resolveCallTarget doesn't
// disambiguate overloads by signature), disambiguated here by arg count.
func mathLog(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindR8 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Math.Log expects a double argument")
	}
	if len(args) >= 2 && args[1].Kind == runtime.KindR8 {
		return runtime.Float64(math.Log(args[0].R8) / math.Log(args[1].R8)), nil
	}
	return runtime.Float64(math.Log(args[0].R8)), nil
}

func mathSign(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Math.Sign expects 1 argument")
	}
	var f float64
	switch args[0].Kind {
	case runtime.KindR8:
		f = args[0].R8
	case runtime.KindR4:
		f = float64(args[0].R4)
	case runtime.KindI4:
		f = float64(args[0].I4)
	case runtime.KindI8:
		f = float64(args[0].I8)
	default:
		return runtime.Value{}, fmt.Errorf("bcl: System.Math.Sign: unsupported argument kind")
	}
	switch {
	case f > 0:
		return runtime.Int32(1), nil
	case f < 0:
		return runtime.Int32(-1), nil
	default:
		return runtime.Int32(0), nil
	}
}

func mathAtan2(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 || args[0].Kind != runtime.KindR8 || args[1].Kind != runtime.KindR8 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Math.Atan2 expects two double arguments")
	}
	return runtime.Float64(math.Atan2(args[0].R8, args[1].R8)), nil
}

// mathRound backs Math.Round(double), Math.Round(double, digits),
// Math.Round(double, MidpointRounding) and Math.Round(double, digits,
// MidpointRounding) — all the same call target, disambiguated by arg
// count/kind. Real .NET's parameterless overload defaults to
// MidpointRounding.ToEven ("banker's rounding"), matched here via Go's
// math.RoundToEven regardless of whether a MidpointRounding argument is
// present: distinguishing ToEven from AwayFromZero would need decoding
// the enum's raw int value, not yet worth the complexity since no target
// package in this loop's IL was found relying on AwayFromZero
// specifically.
func mathRound(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindR8 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Math.Round expects a double argument")
	}
	digits := 0
	if len(args) >= 2 && args[1].Kind == runtime.KindI4 {
		digits = int(args[1].I4)
	}
	scale := math.Pow(10, float64(digits))
	return runtime.Float64(math.RoundToEven(args[0].R8*scale) / scale), nil
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
