package vmnet

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/arturoeanton/go-vmnet/internal/interpreter"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

const fixtureRelPath = "tests/fixtures/csharp/bin/Release/netstandard2.0/Vmnet.Fixtures.dll"

func loadFixture(t *testing.T) *Assembly {
	t.Helper()
	vm := New()
	asm, err := vm.LoadFile(filepath.FromSlash(fixtureRelPath))
	if err != nil {
		t.Skipf("fixture assembly not built: %v (run `dotnet build tests/fixtures/csharp/Fixtures.csproj -c Release`)", err)
	}
	return asm
}

// TestFase1Demo exercises exactly the three methods docs/ROADMAP.md's
// Fase 1 demo script calls out: SimpleMath.Add, Strings.Hello, Loops.Sum.
func TestFase1Demo(t *testing.T) {
	asm := loadFixture(t)

	t.Run("SimpleMath.Add", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.SimpleMath", "Add", Int32(3), Int32(4))
		if err != nil {
			t.Fatalf("Call() error = %v", err)
		}
		if got := out.Native().(int32); got != 7 {
			t.Errorf("Add(3, 4) = %d, want 7", got)
		}
	})

	t.Run("Strings.Hello", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.Strings", "Hello", String("vmnet"))
		if err != nil {
			t.Fatalf("Call() error = %v", err)
		}
		if got := out.Native().(string); got != "Hello vmnet" {
			t.Errorf("Hello(%q) = %q, want %q", "vmnet", got, "Hello vmnet")
		}
	})

	t.Run("Loops.Sum", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.Loops", "Sum", Int32(10))
		if err != nil {
			t.Fatalf("Call() error = %v", err)
		}
		if got := out.Native().(int32); got != 55 {
			t.Errorf("Sum(10) = %d, want 55", got)
		}
	})
}

func TestCall_ArgumentCountMismatch(t *testing.T) {
	asm := loadFixture(t)
	_, err := asm.Call("Vmnet.Fixtures.SimpleMath", "Add", Int32(1))
	if err == nil {
		t.Fatal("Call() with wrong argument count: error = nil, want an error")
	}
}

func TestCall_UnknownMethod(t *testing.T) {
	asm := loadFixture(t)
	_, err := asm.Call("Vmnet.Fixtures.SimpleMath", "DoesNotExist")
	if err == nil {
		t.Fatal("Call() for an unknown method: error = nil, want an error")
	}
}

// TestFase2Demo exercises exactly what docs/ROADMAP.md's Fase 2 demo
// script calls out: a realistic Rules.Eval (objects, callvirt property
// accessors, List<T>, Dictionary<string,V>) driven through the JSON
// bridge, a managed exception surfacing as a clean Go error, and a
// runaway plugin killed by the instruction sandbox.
func TestFase2Demo(t *testing.T) {
	asm := loadFixture(t)

	t.Run("Rules.Eval via CallBytes", func(t *testing.T) {
		out, err := asm.CallBytes("Vmnet.Fixtures.Rules", "Eval", []byte("go request"))
		if err != nil {
			t.Fatalf("CallBytes() error = %v", err)
		}
		const want = `{"ok":true,"customer":"acme corp"}`
		if string(out) != want {
			t.Errorf("CallBytes() = %s, want %s", out, want)
		}
	})

	t.Run("Rules.Eval via CallJSON", func(t *testing.T) {
		result, err := asm.CallJSON("Vmnet.Fixtures.Rules", "Eval", "go request")
		if err != nil {
			t.Fatalf("CallJSON() error = %v", err)
		}
		m, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("CallJSON() result = %#v, want map[string]any", result)
		}
		if m["ok"] != true || m["customer"] != "acme corp" {
			t.Errorf("CallJSON() result = %#v, want ok=true customer=\"acme corp\"", m)
		}
	})

	t.Run("managed exception on empty input", func(t *testing.T) {
		_, err := asm.CallBytes("Vmnet.Fixtures.Rules", "Eval", []byte(""))
		if err == nil {
			t.Fatal("CallBytes(empty input): error = nil, want a managed exception")
		}
		var ex *runtime.ManagedException
		if !errors.As(err, &ex) {
			t.Fatalf("CallBytes(empty input) error = %v, want it to wrap *runtime.ManagedException", err)
		}
		if ex.TypeName != "System.InvalidOperationException" || ex.Message != "empty input" {
			t.Errorf("exception = %+v, want {System.InvalidOperationException, empty input}", ex)
		}
	})

	t.Run("sandbox kills a runaway plugin", func(t *testing.T) {
		start := time.Now()
		_, err := asm.Call("Vmnet.Fixtures.Loops", "Runaway")
		elapsed := time.Since(start)

		if !errors.Is(err, interpreter.ErrInstructionLimitExceeded) {
			t.Fatalf("Runaway() error = %v, want ErrInstructionLimitExceeded", err)
		}
		if elapsed > 5*time.Second {
			t.Errorf("Runaway() took %v to hit the instruction limit, want well under 5s", elapsed)
		}
	})
}

// TestObjectsAndCollections exercises newobj/callvirt/ldfld/stfld
// (Customer's auto-property accessors) and List<T> independently of the
// Rules.Eval demo scenario.
func TestObjectsAndCollections(t *testing.T) {
	asm := loadFixture(t)

	out, err := asm.Call("Vmnet.Fixtures.CollectionsTest", "Count")
	if err != nil {
		t.Fatalf("CollectionsTest.Count() error = %v", err)
	}
	if got := out.Native().(int32); got != 2 {
		t.Errorf("CollectionsTest.Count() = %d, want 2", got)
	}
}

func TestCall_InstanceMethodRejected(t *testing.T) {
	// Customer.get_Name is an instance method; Call only invokes statics
	// (use CallBytes/CallJSON, or construct via a static factory method).
	asm := loadFixture(t)
	_, err := asm.Call("Vmnet.Fixtures.Customer", "get_Name")
	if err == nil {
		t.Fatal("Call(Customer.get_Name): error = nil, want an instance-method error")
	}
}

// TestConcurrentCalls proves a single *Assembly is safe to share across
// goroutines — e.g. concurrent requests in a Go server. Run with -race:
// before the cacheMu fix this triggered a concurrent map read/write panic
// almost immediately (asm.methods/asm.types are populated lazily on first
// use, from whichever goroutine gets there first).
func TestConcurrentCalls(t *testing.T) {
	asm := loadFixture(t)

	const goroutines = 32
	const perGoroutine = 50

	errCh := make(chan error, goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			for i := 0; i < perGoroutine; i++ {
				sum, err := asm.Call("Vmnet.Fixtures.SimpleMath", "Add", Int32(3), Int32(4))
				if err != nil {
					errCh <- err
					return
				}
				if got := sum.Native().(int32); got != 7 {
					errCh <- fmt.Errorf("Add(3,4) = %d, want 7", got)
					return
				}

				if _, err := asm.CallBytes("Vmnet.Fixtures.Rules", "Eval", []byte("concurrent")); err != nil {
					errCh <- err
					return
				}
			}
			errCh <- nil
		}()
	}

	for g := 0; g < goroutines; g++ {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}
}
