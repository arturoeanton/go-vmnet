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
}

func New(resolve Resolver, resolveType TypeResolver, limits Limits) *Machine {
	return &Machine{Resolve: resolve, ResolveType: resolveType, Limits: limits}
}

// Invoke runs method with args and returns its result (the zero Value if
// method is void).
func (m *Machine) Invoke(method *runtime.Method, args []runtime.Value) (runtime.Value, error) {
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
			v, err := evalBinOp(in.Op, a, b)
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
			take, err := evalCompare(in.Op, a, b)
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

		case ir.Throw:
			v := frame.pop()
			if v.Kind == runtime.KindObject && v.Obj != nil {
				if ex, ok := v.Obj.Native.(*runtime.ManagedException); ok {
					return runtime.Value{}, ex
				}
			}
			return runtime.Value{}, fmt.Errorf("interpreter: thrown object is not a recognized exception type")

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
