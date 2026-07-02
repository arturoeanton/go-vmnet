package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/ir"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// Machine executes runtime.Method IR. Resolve supplies methods for calls
// that aren't BCL natives (bcl.Lookup) — Fase 1 only exercises static
// calls, but the recursion/resolver plumbing is already general.
type Machine struct {
	Resolve Resolver
	Limits  Limits
}

func New(resolve Resolver, limits Limits) *Machine {
	return &Machine{Resolve: resolve, Limits: limits}
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

			result, hasReturn, err := m.call(in.FullName, callArgs, depth, instrCount)
			if err != nil {
				return runtime.Value{}, err
			}
			if hasReturn {
				frame.push(result)
			}

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
