package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// pointType models System.Drawing.Point as a plain (x, y) int32 pair —
// the CLR's own representation exactly. Found via a real, load-bearing
// case: ClosedXML's own picture/drawing-anchor code (XLMarker) builds one
// from EMU-converted pixel offsets, reached just from opening a real
// .xlsx workbook, not from anything the caller's own code does with
// pictures directly.
var pointType = runtime.NewValueType(
	"System.Drawing", "Point",
	[]string{"x", "y"},
	[]runtime.Value{runtime.Int32(0), runtime.Int32(0)},
)

func init() {
	registerValueType(pointType)
	registerValueTypeCtor("System.Drawing.Point", pointCtor)
	// `var p = new Point(x, y);` assigned straight to a local compiles as
	// `ldloca`+`call .ctor`, not `newobj` — the same compiler optimization
	// DateTime/TimeSpan/Nullable`1 already needed their own entry point for.
	register("System.Drawing.Point::.ctor", false, pointCtorInPlace)
	register("System.Drawing.Point::get_X", true, pointGetX)
	register("System.Drawing.Point::get_Y", true, pointGetY)
	register("System.Drawing.Point::set_X", false, pointSetX)
	register("System.Drawing.Point::set_Y", false, pointSetY)
}

func pointCtor(args []runtime.Value) (*runtime.Struct, error) {
	s := runtime.NewStruct(pointType)
	if len(args) != 2 || args[0].Kind != runtime.KindI4 || args[1].Kind != runtime.KindI4 {
		return nil, fmt.Errorf("bcl: Point(x, y) expects two int32 arguments")
	}
	s.Fields[0] = args[0]
	s.Fields[1] = args[1]
	return s, nil
}

func pointCtorInPlace(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindRef || args[0].Ref == nil {
		return runtime.Value{}, fmt.Errorf("bcl: Point constructor called without a receiver")
	}
	s, err := pointCtor(args[1:])
	if err != nil {
		return runtime.Value{}, err
	}
	*args[0].Ref = runtime.StructVal(s)
	return runtime.Value{}, nil
}

func pointGetX(args []runtime.Value) (runtime.Value, error) {
	s, err := derefStructReceiver(args, "Point", "Point.X")
	if err != nil {
		return runtime.Value{}, err
	}
	return s.Fields[0], nil
}

func pointGetY(args []runtime.Value) (runtime.Value, error) {
	s, err := derefStructReceiver(args, "Point", "Point.Y")
	if err != nil {
		return runtime.Value{}, err
	}
	return s.Fields[1], nil
}

func pointSetX(args []runtime.Value) (runtime.Value, error) {
	s, err := derefStructReceiver(args, "Point", "Point.X")
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: Point.X setter expects a value")
	}
	s.Fields[0] = args[1]
	return runtime.Value{}, nil
}

func pointSetY(args []runtime.Value) (runtime.Value, error) {
	s, err := derefStructReceiver(args, "Point", "Point.Y")
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: Point.Y setter expects a value")
	}
	s.Fields[1] = args[1]
	return runtime.Value{}, nil
}
