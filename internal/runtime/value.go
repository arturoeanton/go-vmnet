// Package runtime holds vmnet's managed object model: types, methods,
// fields, the heap, arrays, strings, delegates, exceptions and generic
// instantiations that back values produced by internal/interpreter. See
// docs/en/ROADMAP.md, Fase 1 y Fase 2, module "/runtime".
package runtime

import "fmt"

// Kind is the tag of a Value. The CIL evaluation stack only ever holds a
// handful of shapes (ECMA-335 §III.1.1: int32, int64, native int, F,
// managed pointer, object reference) — booleans/chars/bytes are all int32
// on the stack. Fase 1 adds a String kind as a pragmatic stand-in for a
// real System.String heap object, which arrives with the object model in
// Fase 2.
type Kind byte

const (
	KindNull Kind = iota
	KindI4
	KindI8
	KindR4
	KindR8
	KindString
	KindBytes  // a CLI byte[], used at the JSON/bytes bridge boundary (Fase 2)
	KindObject // a heap object reference: an instance of a Type, or a BCL-native-backed object
	KindArray  // a CLI SZARRAY (Fase 3.5)
	KindRef    // a managed pointer (Fase 3.5) — see ref.go
	KindStruct // a value type instance (Fase 3.7) — see struct.go
	KindFunc   // a delegate (Fase 3.9) — see func.go
)

// Value is one CIL evaluation-stack slot.
type Value struct {
	Kind   Kind
	I4     int32
	I8     int64
	R4     float32
	R8     float64
	Str    string
	Bytes  []byte
	Obj    *Object
	Arr    *Array
	Ref    *Value
	Struct *Struct
	Func   *Func
}

func Null() Value             { return Value{Kind: KindNull} }
func Int32(v int32) Value     { return Value{Kind: KindI4, I4: v} }
func Int64(v int64) Value     { return Value{Kind: KindI8, I8: v} }
func Float32(v float32) Value { return Value{Kind: KindR4, R4: v} }
func Float64(v float64) Value { return Value{Kind: KindR8, R8: v} }
func String(v string) Value   { return Value{Kind: KindString, Str: v} }
func Bytes(v []byte) Value    { return Value{Kind: KindBytes, Bytes: v} }
func ObjRef(o *Object) Value  { return Value{Kind: KindObject, Obj: o} }
func ArrRef(a *Array) Value   { return Value{Kind: KindArray, Arr: a} }

// RefTo wraps a pointer to another Value slot (a local, an argument, an
// array element, ...) as a managed-pointer Value. See ref.go.
func RefTo(v *Value) Value { return Value{Kind: KindRef, Ref: v} }

// StructVal wraps a value-type instance as a Value. See struct.go.
func StructVal(s *Struct) Value { return Value{Kind: KindStruct, Struct: s} }

// FuncVal wraps a delegate as a Value. See func.go.
func FuncVal(f *Func) Value { return Value{Kind: KindFunc, Func: f} }

// Bool encodes a CIL boolean as the int32 0/1 it actually is on the stack.
func Bool(v bool) Value {
	if v {
		return Int32(1)
	}
	return Int32(0)
}

// Truthy implements brtrue/brfalse's notion of truth: zero (of whichever
// numeric kind) or a nil reference is false, everything else is true.
func (v Value) Truthy() bool {
	switch v.Kind {
	case KindI4:
		return v.I4 != 0
	case KindI8:
		return v.I8 != 0
	case KindR4:
		return v.R4 != 0
	case KindR8:
		return v.R8 != 0
	case KindString:
		return true
	case KindBytes:
		return v.Bytes != nil
	case KindObject:
		return v.Obj != nil
	case KindArray:
		return v.Arr != nil
	case KindRef:
		return v.Ref != nil
	case KindStruct:
		// A value type instance is never null (spec: value types have no
		// null state), same as CLR struct semantics.
		return true
	case KindFunc:
		return v.Func != nil
	case KindNull:
		return false
	}
	return false
}

func (v Value) String() string {
	switch v.Kind {
	case KindNull:
		return "null"
	case KindI4:
		return fmt.Sprintf("%d", v.I4)
	case KindI8:
		return fmt.Sprintf("%d", v.I8)
	case KindR4:
		return fmt.Sprintf("%g", v.R4)
	case KindR8:
		return fmt.Sprintf("%g", v.R8)
	case KindString:
		return v.Str
	case KindBytes:
		return fmt.Sprintf("<%d bytes>", len(v.Bytes))
	case KindObject:
		if v.Obj == nil {
			return "null"
		}
		if v.Obj.Type != nil {
			return fmt.Sprintf("<%s>", v.Obj.Type.Name)
		}
		return "<object>"
	case KindArray:
		if v.Arr == nil {
			return "null"
		}
		return fmt.Sprintf("<array[%d]>", len(v.Arr.Elems))
	case KindRef:
		if v.Ref == nil {
			return "null"
		}
		return "<ref>"
	case KindStruct:
		if v.Struct == nil || v.Struct.Type == nil {
			return "<struct>"
		}
		return fmt.Sprintf("<%s>", v.Struct.Type.Name)
	case KindFunc:
		if v.Func == nil {
			return "null"
		}
		return fmt.Sprintf("<delegate:%s>", v.Func.FullName)
	}
	return "<invalid value>"
}
