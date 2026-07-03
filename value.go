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

// byteArrayValue wraps a real CIL byte[] (a KindArray of KindI4
// elements, each 0-255) — what `newarr`/interpreted array construction
// actually produces, and what a native BCL method taking a byte[]
// parameter (e.g. System.IO.MemoryStream's ctor) expects. This is
// distinct from CallBytes/CallJSON's KindBytes, a separate bridge-only
// representation only understood by a `byte[] X(byte[])` static method
// invoked through CallBytes itself — passing a byteArrayValue anywhere
// CallBytes expects KindBytes, or vice versa, would not resolve.
type byteArrayValue []byte

// ByteArray wraps data as a real CIL byte[] argument/return value — for
// constructing an object whose real constructor or method takes a
// byte[] (e.g. `New("System.IO.MemoryStream", vmnet.ByteArray(data))`),
// not for CallBytes/CallJSON (which have their own byte[]-shaped bridge
// and need a plain Go []byte instead).
func ByteArray(data []byte) Value { return byteArrayValue(data) }

func (v byteArrayValue) Native() any { return []byte(v) }
func (v byteArrayValue) toRuntime() runtime.Value {
	elems := make([]runtime.Value, len(v))
	for i, b := range v {
		elems[i] = runtime.Int32(int32(b))
	}
	return runtime.ArrRef(&runtime.Array{Elems: elems})
}

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
	case runtime.KindArray:
		// Represented as ByteArray regardless of the real element type
		// (each element truncated to a byte) — the common real case a
		// Call/Instance.Call result needs (Stream.ToArray()-style byte[]
		// results); a genuine int[]/object[] result loses fidelity here,
		// same posture as every other "vmnet doesn't preserve full type
		// info across the Go boundary" simplification in this API.
		if v.Arr == nil {
			return nil
		}
		data := make([]byte, len(v.Arr.Elems))
		for i, e := range v.Arr.Elems {
			if e.Kind == runtime.KindI4 {
				data[i] = byte(e.I4)
			}
		}
		return byteArrayValue(data)
	default:
		return nil
	}
}
