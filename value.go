package vmnet

import "github.com/arturoeanton/go-vmnet/internal/runtime"

// Value is an argument or return value for Assembly.Call (spec §6.1). A
// nil Value means "void" (a method call with no return value).
type Value interface {
	Native() any
	toRuntime() runtime.Value
}

type int32Value int32

func Int32(v int32) Value                     { return int32Value(v) }
func (v int32Value) Native() any              { return int32(v) }
func (v int32Value) toRuntime() runtime.Value { return runtime.Int32(int32(v)) }

type int64Value int64

func Int64(v int64) Value                     { return int64Value(v) }
func (v int64Value) Native() any              { return int64(v) }
func (v int64Value) toRuntime() runtime.Value { return runtime.Int64(int64(v)) }

type float32Value float32

func Float32(v float32) Value                   { return float32Value(v) }
func (v float32Value) Native() any              { return float32(v) }
func (v float32Value) toRuntime() runtime.Value { return runtime.Float32(float32(v)) }

type float64Value float64

func Float64(v float64) Value                   { return float64Value(v) }
func (v float64Value) Native() any              { return float64(v) }
func (v float64Value) toRuntime() runtime.Value { return runtime.Float64(float64(v)) }

type stringValue string

func String(v string) Value                    { return stringValue(v) }
func (v stringValue) Native() any              { return string(v) }
func (v stringValue) toRuntime() runtime.Value { return runtime.String(string(v)) }

func fromRuntime(v runtime.Value) Value {
	switch v.Kind {
	case runtime.KindI4:
		return int32Value(v.I4)
	case runtime.KindI8:
		return int64Value(v.I8)
	case runtime.KindR4:
		return float32Value(v.R4)
	case runtime.KindR8:
		return float64Value(v.R8)
	case runtime.KindString:
		return stringValue(v.Str)
	default:
		return nil
	}
}
