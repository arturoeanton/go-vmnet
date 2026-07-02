package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/ir"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// Machine executes runtime.Method IR. Resolve supplies methods and
// ResolveType supplies field layouts for anything that isn't a BCL native
// (bcl.Lookup / bcl.LookupCtor).
type Machine struct {
	Resolve     Resolver
	ResolveType TypeResolver
	Limits      Limits

	// cctorsRunning tracks static constructors currently executing on
	// this Machine's own call chain (a Machine is never shared across
	// goroutines — see call.go's asm.machine()), so a .cctor that reads
	// or writes its own type's static fields (the overwhelmingly common
	// case) re-enters staticType without deadlocking on the Type's
	// EnsureCctor latch. See internal/interpreter/statics.go.
	cctorsRunning map[*runtime.Type]bool
}

func New(resolve Resolver, resolveType TypeResolver, limits Limits) *Machine {
	return &Machine{Resolve: resolve, ResolveType: resolveType, Limits: limits}
}

// Invoke runs method with args and returns its result (the zero Value if
// method is void).
//
// A vmnet plugin must never be able to crash its host: Invoke recovers any
// panic from anywhere in the call tree below it (a bounds check we missed,
// a bad type assertion, malformed IR) and turns it into a plain error
// instead of unwinding into the caller's goroutine.
func (m *Machine) Invoke(method *runtime.Method, args []runtime.Value) (result runtime.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("interpreter: internal error (recovered panic): %v", r)
		}
	}()
	instrCount := new(int64)
	return m.invoke(method, args, 0, instrCount)
}

func (m *Machine) invoke(method *runtime.Method, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if m.Limits.MaxCallDepth > 0 && depth > m.Limits.MaxCallDepth {
		return runtime.Value{}, ErrCallDepthExceeded
	}

	frame := &Frame{
		Args:   args,
		Locals: make([]runtime.Value, method.LocalCount),
		Stack:  make([]runtime.Value, 0, method.MaxStack+8),
	}

	for frame.IP < len(method.IR) {
		*instrCount++
		if m.Limits.MaxInstructions > 0 && *instrCount > m.Limits.MaxInstructions {
			return runtime.Value{}, ErrInstructionLimitExceeded
		}
		if m.Limits.MaxStackDepth > 0 && len(frame.Stack) > m.Limits.MaxStackDepth {
			return runtime.Value{}, ErrStackOverflow
		}

		next := frame.IP + 1

		switch in := method.IR[frame.IP].(type) {
		case ir.Nop:
			// no-op

		case ir.Dup:
			v := frame.pop()
			frame.push(v)
			frame.push(v)

		case ir.Pop:
			frame.pop()

		case ir.LoadArg:
			if in.Index < 0 || in.Index >= len(frame.Args) {
				return runtime.Value{}, fmt.Errorf("interpreter: ldarg index %d out of range", in.Index)
			}
			frame.push(frame.Args[in.Index])

		case ir.StoreArg:
			if in.Index < 0 || in.Index >= len(frame.Args) {
				return runtime.Value{}, fmt.Errorf("interpreter: starg index %d out of range", in.Index)
			}
			frame.Args[in.Index] = frame.pop()

		case ir.LoadArgAddr:
			if in.Index < 0 || in.Index >= len(frame.Args) {
				return runtime.Value{}, fmt.Errorf("interpreter: ldarga index %d out of range", in.Index)
			}
			frame.push(runtime.RefTo(&frame.Args[in.Index]))

		case ir.LoadLocal:
			if in.Index < 0 || in.Index >= len(frame.Locals) {
				return runtime.Value{}, fmt.Errorf("interpreter: ldloc index %d out of range", in.Index)
			}
			frame.push(frame.Locals[in.Index])

		case ir.StoreLocal:
			if in.Index < 0 || in.Index >= len(frame.Locals) {
				return runtime.Value{}, fmt.Errorf("interpreter: stloc index %d out of range", in.Index)
			}
			frame.Locals[in.Index] = frame.pop()

		case ir.LoadLocalAddr:
			if in.Index < 0 || in.Index >= len(frame.Locals) {
				return runtime.Value{}, fmt.Errorf("interpreter: ldloca index %d out of range", in.Index)
			}
			frame.push(runtime.RefTo(&frame.Locals[in.Index]))

		case ir.LoadConstI4:
			frame.push(runtime.Int32(in.Value))
		case ir.LoadConstI8:
			frame.push(runtime.Int64(in.Value))
		case ir.LoadConstR4:
			frame.push(runtime.Float32(in.Value))
		case ir.LoadConstR8:
			frame.push(runtime.Float64(in.Value))
		case ir.LoadString:
			frame.push(runtime.String(in.Value))
		case ir.LoadNull:
			frame.push(runtime.Null())

		case ir.BinOp:
			b := frame.pop()
			a := frame.pop()
			v, err := evalBinOp(in, a, b)
			if err != nil {
				return runtime.Value{}, err
			}
			frame.push(v)

		case ir.Neg:
			v, err := evalNeg(frame.pop())
			if err != nil {
				return runtime.Value{}, err
			}
			frame.push(v)

		case ir.Not:
			v, err := evalNot(frame.pop())
			if err != nil {
				return runtime.Value{}, err
			}
			frame.push(v)

		case ir.Conv:
			v, err := evalConv(in.Kind, frame.pop())
			if err != nil {
				return runtime.Value{}, err
			}
			frame.push(v)

		case ir.Branch:
			next = in.Target

		case ir.BranchIfTrue:
			if frame.pop().Truthy() {
				next = in.Target
			}

		case ir.BranchIfFalse:
			if !frame.pop().Truthy() {
				next = in.Target
			}

		case ir.BranchCompare:
			b := frame.pop()
			a := frame.pop()
			take, err := evalCompare(in, a, b)
			if err != nil {
				return runtime.Value{}, err
			}
			if take {
				next = in.Target
			}

		case ir.Call:
			total := in.ArgCount
			if in.HasThis {
				total++
			}
			if len(frame.Stack) < total {
				return runtime.Value{}, fmt.Errorf("interpreter: call to %s: stack underflow", in.FullName)
			}
			callArgs := append([]runtime.Value(nil), frame.Stack[len(frame.Stack)-total:]...)
			frame.Stack = frame.Stack[:len(frame.Stack)-total]

			if in.Virtual && callArgs[0].Kind == runtime.KindNull {
				return runtime.Value{}, &runtime.ManagedException{
					TypeName: "System.NullReferenceException",
					Message:  fmt.Sprintf("Object reference not set to an instance of an object (calling %s)", in.FullName),
				}
			}

			result, hasReturn, err := m.call(in.FullName, callArgs, depth, instrCount)
			if err != nil {
				return runtime.Value{}, err
			}
			if hasReturn {
				frame.push(result)
			}

		case ir.NewObj:
			if len(frame.Stack) < in.ArgCount {
				return runtime.Value{}, fmt.Errorf("interpreter: newobj %s: stack underflow", in.TypeFullName)
			}
			ctorArgs := append([]runtime.Value(nil), frame.Stack[len(frame.Stack)-in.ArgCount:]...)
			frame.Stack = frame.Stack[:len(frame.Stack)-in.ArgCount]

			v, err := m.newObj(newObjArgs{TypeFullName: in.TypeFullName, CtorFullName: in.CtorFullName, Args: ctorArgs}, depth, instrCount)
			if err != nil {
				return runtime.Value{}, err
			}
			frame.push(v)

		case ir.LoadField:
			obj := frame.pop()
			if obj.Kind != runtime.KindObject || obj.Obj == nil {
				return runtime.Value{}, &runtime.ManagedException{
					TypeName: "System.NullReferenceException",
					Message:  fmt.Sprintf("Object reference not set to an instance of an object (reading %s.%s)", in.TypeFullName, in.FieldName),
				}
			}
			idx := fieldIndex(obj.Obj, in.FieldName)
			if idx < 0 {
				return runtime.Value{}, fmt.Errorf("interpreter: %s has no field %q", in.TypeFullName, in.FieldName)
			}
			frame.push(obj.Obj.Fields[idx])

		case ir.StoreField:
			val := frame.pop()
			obj := frame.pop()
			if obj.Kind != runtime.KindObject || obj.Obj == nil {
				return runtime.Value{}, &runtime.ManagedException{
					TypeName: "System.NullReferenceException",
					Message:  fmt.Sprintf("Object reference not set to an instance of an object (writing %s.%s)", in.TypeFullName, in.FieldName),
				}
			}
			idx := fieldIndex(obj.Obj, in.FieldName)
			if idx < 0 {
				return runtime.Value{}, fmt.Errorf("interpreter: %s has no field %q", in.TypeFullName, in.FieldName)
			}
			obj.Obj.Fields[idx] = val

		case ir.LoadFieldAddr:
			obj := frame.pop()
			if obj.Kind != runtime.KindObject || obj.Obj == nil {
				return runtime.Value{}, &runtime.ManagedException{
					TypeName: "System.NullReferenceException",
					Message:  fmt.Sprintf("Object reference not set to an instance of an object (reading &%s.%s)", in.TypeFullName, in.FieldName),
				}
			}
			idx := fieldIndex(obj.Obj, in.FieldName)
			if idx < 0 {
				return runtime.Value{}, fmt.Errorf("interpreter: %s has no field %q", in.TypeFullName, in.FieldName)
			}
			frame.push(runtime.RefTo(&obj.Obj.Fields[idx]))

		case ir.LoadStaticField:
			t, err := m.staticType(in.TypeFullName, depth, instrCount)
			if err != nil {
				return runtime.Value{}, err
			}
			idx := t.StaticFieldIndex(in.FieldName)
			if idx < 0 {
				return runtime.Value{}, fmt.Errorf("interpreter: %s has no static field %q", in.TypeFullName, in.FieldName)
			}
			frame.push(t.StaticField(idx))

		case ir.StoreStaticField:
			val := frame.pop()
			t, err := m.staticType(in.TypeFullName, depth, instrCount)
			if err != nil {
				return runtime.Value{}, err
			}
			idx := t.StaticFieldIndex(in.FieldName)
			if idx < 0 {
				return runtime.Value{}, fmt.Errorf("interpreter: %s has no static field %q", in.TypeFullName, in.FieldName)
			}
			t.SetStaticField(idx, val)

		case ir.Throw:
			v := frame.pop()
			if v.Kind == runtime.KindObject && v.Obj != nil {
				if ex, ok := v.Obj.Native.(*runtime.ManagedException); ok {
					return runtime.Value{}, ex
				}
			}
			return runtime.Value{}, fmt.Errorf("interpreter: thrown object is not a recognized exception type")

		case ir.NewArr:
			lenVal := frame.pop()
			if lenVal.Kind != runtime.KindI4 {
				return runtime.Value{}, fmt.Errorf("interpreter: newarr length must be int32")
			}
			if lenVal.I4 < 0 {
				return runtime.Value{}, &runtime.ManagedException{TypeName: "System.OverflowException", Message: "array length must be non-negative"}
			}
			if m.Limits.MaxArrayLength > 0 && int(lenVal.I4) > m.Limits.MaxArrayLength {
				return runtime.Value{}, ErrArrayTooLarge
			}
			frame.push(runtime.ArrRef(runtime.NewArray(int(lenVal.I4))))

		case ir.LoadLen:
			v := frame.pop()
			if v.Kind != runtime.KindArray || v.Arr == nil {
				return runtime.Value{}, &runtime.ManagedException{TypeName: "System.NullReferenceException", Message: "array reference is null (ldlen)"}
			}
			frame.push(runtime.Int32(int32(len(v.Arr.Elems))))

		case ir.LoadElem:
			idxVal := frame.pop()
			arrVal := frame.pop()
			idx, err := arrayIndex(arrVal, idxVal, "ldelem")
			if err != nil {
				return runtime.Value{}, err
			}
			frame.push(arrVal.Arr.Elems[idx])

		case ir.StoreElem:
			val := frame.pop()
			idxVal := frame.pop()
			arrVal := frame.pop()
			idx, err := arrayIndex(arrVal, idxVal, "stelem")
			if err != nil {
				return runtime.Value{}, err
			}
			arrVal.Arr.Elems[idx] = val

		case ir.LoadElemAddr:
			idxVal := frame.pop()
			arrVal := frame.pop()
			idx, err := arrayIndex(arrVal, idxVal, "ldelema")
			if err != nil {
				return runtime.Value{}, err
			}
			frame.push(runtime.RefTo(&arrVal.Arr.Elems[idx]))

		case ir.LoadIndirect:
			ref := frame.pop()
			if ref.Kind != runtime.KindRef || ref.Ref == nil {
				return runtime.Value{}, &runtime.ManagedException{TypeName: "System.NullReferenceException", Message: "dereferencing a null managed pointer (ldind)"}
			}
			frame.push(*ref.Ref)

		case ir.StoreIndirect:
			val := frame.pop()
			ref := frame.pop()
			if ref.Kind != runtime.KindRef || ref.Ref == nil {
				return runtime.Value{}, &runtime.ManagedException{TypeName: "System.NullReferenceException", Message: "dereferencing a null managed pointer (stind)"}
			}
			*ref.Ref = val

		case ir.Return:
			if in.HasValue {
				return frame.pop(), nil
			}
			return runtime.Value{}, nil

		default:
			return runtime.Value{}, fmt.Errorf("interpreter: unhandled IR instruction %T", method.IR[frame.IP])
		}

		frame.IP = next
	}

	return runtime.Value{}, fmt.Errorf("interpreter: method fell off the end without a ret")
}

func fieldIndex(obj *runtime.Object, name string) int {
	if obj.Type == nil {
		return -1
	}
	return obj.Type.FieldIndex(name)
}

// arrayIndex validates an ldelem/stelem array+index pair, returning a
// managed IndexOutOfRangeException/NullReferenceException — matching real
// CIL semantics — instead of a Go panic on out-of-bounds access.
func arrayIndex(arrVal, idxVal runtime.Value, op string) (int, error) {
	if arrVal.Kind != runtime.KindArray || arrVal.Arr == nil {
		return 0, &runtime.ManagedException{TypeName: "System.NullReferenceException", Message: fmt.Sprintf("array reference is null (%s)", op)}
	}
	if idxVal.Kind != runtime.KindI4 {
		return 0, fmt.Errorf("interpreter: %s index must be int32", op)
	}
	idx := int(idxVal.I4)
	if idx < 0 || idx >= len(arrVal.Arr.Elems) {
		return 0, &runtime.ManagedException{
			TypeName: "System.IndexOutOfRangeException",
			Message:  fmt.Sprintf("index %d is out of range (length %d)", idx, len(arrVal.Arr.Elems)),
		}
	}
	return idx, nil
}
