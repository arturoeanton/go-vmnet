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

// TestInvoke_MaxStringBytes_PadLeft proves the Fase 3.72 pre-call size
// check: `"x".PadLeft(a huge width)` must be rejected BEFORE
// strings.Repeat ever runs, not after — the whole point of checking
// PadLeft's own width argument ahead of the call (calls.go's
// checkStringSizeRequest), rather than relying on checkStringLimit's
// after-the-fact check, which would already be too late for a real,
// multi-gigabyte allocation attempt.
func TestInvoke_MaxStringBytes_PadLeft(t *testing.T) {
	method := &runtime.Method{
		FullName:  "Broken::HugePad",
		HasReturn: true,
		MaxStack:  2,
		IR: []ir.Instr{
			ir.LoadString{Value: "x"},
			ir.LoadConstI4{Value: 999_999_999},
			ir.Call{FullName: "System.String::PadLeft", ArgCount: 2, HasReturn: true},
			ir.Return{HasValue: true},
		},
	}

	limits := DefaultLimits()
	limits.MaxStringBytes = 1024

	m := New(nil, nil, limits)
	_, err := m.Invoke(method, nil)
	if !errors.Is(err, ErrStringTooLarge) {
		t.Fatalf("Invoke() error = %v, want ErrStringTooLarge", err)
	}
}

// TestInvoke_MaxStringBytes_NewStringCtor mirrors the PadLeft case above
// for `new string(char, int)` — the other bare-int-argument string
// constructor with no preceding array allocation to have already been
// bounded by MaxArrayLength.
func TestInvoke_MaxStringBytes_NewStringCtor(t *testing.T) {
	method := &runtime.Method{
		FullName:  "Broken::HugeNewString",
		HasReturn: true,
		MaxStack:  2,
		IR: []ir.Instr{
			ir.LoadConstI4{Value: int32('x')},
			ir.LoadConstI4{Value: 999_999_999},
			ir.NewObj{TypeFullName: "System.String", CtorFullName: "System.String::.ctor", ArgCount: 2},
			ir.Return{HasValue: true},
		},
	}

	limits := DefaultLimits()
	limits.MaxStringBytes = 1024

	m := New(nil, nil, limits)
	_, err := m.Invoke(method, nil)
	if !errors.Is(err, ErrStringTooLarge) {
		t.Fatalf("Invoke() error = %v, want ErrStringTooLarge", err)
	}
}

// TestInvoke_MaxStringBytes_AllowsSmallResult proves MaxStringBytes only
// rejects a genuinely oversized result — a legitimate, small PadLeft call
// under the limit must keep working normally.
func TestInvoke_MaxStringBytes_AllowsSmallResult(t *testing.T) {
	method := &runtime.Method{
		FullName:  "Broken::SmallPad",
		HasReturn: true,
		MaxStack:  2,
		IR: []ir.Instr{
			ir.LoadString{Value: "x"},
			ir.LoadConstI4{Value: 5},
			ir.Call{FullName: "System.String::PadLeft", ArgCount: 2, HasReturn: true},
			ir.Return{HasValue: true},
		},
	}

	limits := DefaultLimits()
	limits.MaxStringBytes = 1024

	m := New(nil, nil, limits)
	v, err := m.Invoke(method, nil)
	if err != nil {
		t.Fatalf("Invoke() error = %v, want nil", err)
	}
	if v.Kind != runtime.KindString || v.Str != "    x" {
		t.Errorf("Invoke() = %+v, want the string %q", v, "    x")
	}
}

// TestInvoke_MaxStringBytes_CatchesResultAfterTheFact proves
// checkStringLimit's own general, post-call safety net: a native not in
// stringSizeGatedBCLNatives (Concat, here) that still manages to produce
// an oversized result gets caught after the call returns.
func TestInvoke_MaxStringBytes_CatchesResultAfterTheFact(t *testing.T) {
	big := strings.Repeat("x", 2000)
	method := &runtime.Method{
		FullName:  "Broken::HugeConcat",
		HasReturn: true,
		MaxStack:  2,
		IR: []ir.Instr{
			ir.LoadString{Value: big},
			ir.LoadString{Value: big},
			ir.Call{FullName: "System.String::Concat", ArgCount: 2, HasReturn: true},
			ir.Return{HasValue: true},
		},
	}

	limits := DefaultLimits()
	limits.MaxStringBytes = 1024

	m := New(nil, nil, limits)
	_, err := m.Invoke(method, nil)
	if !errors.Is(err, ErrStringTooLarge) {
		t.Fatalf("Invoke() error = %v, want ErrStringTooLarge", err)
	}
}
