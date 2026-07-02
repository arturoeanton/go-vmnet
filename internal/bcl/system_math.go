package bcl

import (
	"fmt"
	"math"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

func init() {
	register("System.Math::Abs", true, mathAbs)
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
