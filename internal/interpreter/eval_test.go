package interpreter

import (
	"errors"
	"strings"
	"testing"

	"github.com/arturoeanton/go-vmnet/internal/ir"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// TestInvoke_RecoversPanic proves the "a plugin can never crash its host"
// guarantee: malformed IR that would panic (here, popping an empty stack)
// must come back as an error, not unwind into the caller.
func TestInvoke_RecoversPanic(t *testing.T) {
	method := &runtime.Method{
		FullName:  "Broken::Method",
		HasReturn: true,
		MaxStack:  1,
		IR: []ir.Instr{
			ir.Return{HasValue: true}, // stack is empty: frame.pop() panics
		},
	}

	m := New(nil, nil, DefaultLimits())
	_, err := m.Invoke(method, nil)
	if err == nil {
		t.Fatal("Invoke() with panicking IR: error = nil, want a recovered-panic error")
	}
	if !strings.Contains(err.Error(), "recovered panic") {
		t.Errorf("Invoke() error = %q, want it to mention a recovered panic", err.Error())
	}
}

// TestInvoke_MaxStackDepth proves a plugin that pushes without ever
// popping (buggy or adversarial IR) is stopped by MaxStackDepth well
// before it can grow the stack without bound.
func TestInvoke_MaxStackDepth(t *testing.T) {
	// IR: an infinite loop that pushes one value per iteration and never
	// pops — Branch{0} jumps back to LoadConstI4 forever.
	method := &runtime.Method{
		FullName:  "Broken::Runaway",
		HasReturn: false,
		MaxStack:  1,
		IR: []ir.Instr{
			ir.LoadConstI4{Value: 1},
			ir.Branch{Target: 0},
		},
	}

	limits := DefaultLimits()
	limits.MaxStackDepth = 100
	limits.MaxInstructions = 1_000_000 // must not be what trips first

	m := New(nil, nil, limits)
	_, err := m.Invoke(method, nil)
	if !errors.Is(err, ErrStackOverflow) {
		t.Fatalf("Invoke() error = %v, want ErrStackOverflow", err)
	}
}

func TestInvoke_CallDepthExceeded(t *testing.T) {
	// A method that calls itself recursively forever.
	resolve := func(fullName string, args []runtime.Value, paramTypeNames []string, genericArgCount int) (*runtime.Method, error) {
		return &runtime.Method{
			FullName:  fullName,
			HasReturn: false,
			IR: []ir.Instr{
				ir.Call{FullName: fullName, ArgCount: 0},
				ir.Return{HasValue: false},
			},
		}, nil
	}

	method, err := resolve("Broken::Recurse", nil, nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	limits := DefaultLimits()
	limits.MaxCallDepth = 50

	m := New(resolve, nil, limits)
	_, err = m.Invoke(method, nil)
	if !errors.Is(err, ErrCallDepthExceeded) {
		t.Fatalf("Invoke() error = %v, want ErrCallDepthExceeded", err)
	}
}
