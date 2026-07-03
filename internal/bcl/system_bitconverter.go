package bcl

import (
	"fmt"
	"math"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

func init() {
	register("System.BitConverter::DoubleToInt64Bits", true, bitConverterDoubleToInt64Bits)
	register("System.BitConverter::Int64BitsToDouble", true, bitConverterInt64BitsToDouble)
	register("System.BitConverter::SingleToInt32Bits", true, bitConverterSingleToInt32Bits)
	register("System.BitConverter::Int32BitsToSingle", true, bitConverterInt32BitsToSingle)
}

func bitConverterDoubleToInt64Bits(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 || args[0].Kind != runtime.KindR8 {
		return runtime.Value{}, fmt.Errorf("bcl: BitConverter.DoubleToInt64Bits expects a double argument")
	}
	return runtime.Int64(int64(math.Float64bits(args[0].R8))), nil
}

func bitConverterInt64BitsToDouble(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 || args[0].Kind != runtime.KindI8 {
		return runtime.Value{}, fmt.Errorf("bcl: BitConverter.Int64BitsToDouble expects a long argument")
	}
	return runtime.Float64(math.Float64frombits(uint64(args[0].I8))), nil
}

func bitConverterSingleToInt32Bits(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 || args[0].Kind != runtime.KindR4 {
		return runtime.Value{}, fmt.Errorf("bcl: BitConverter.SingleToInt32Bits expects a float argument")
	}
	return runtime.Int32(int32(math.Float32bits(args[0].R4))), nil
}

func bitConverterInt32BitsToSingle(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 || args[0].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: BitConverter.Int32BitsToSingle expects an int argument")
	}
	return runtime.Float32(math.Float32frombits(uint32(args[0].I4))), nil
}
