package vmnet

import (
	"path/filepath"
	"testing"
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

func TestCall_UnsupportedCallvirt(t *testing.T) {
	// ExceptionTest.Fail throws — `throw` isn't lowered by the Fase 1 IR
	// builder, so this must fail with a clear unsupported-opcode error,
	// not a crash or a wrong result.
	asm := loadFixture(t)
	_, err := asm.Call("Vmnet.Fixtures.ExceptionTest", "Fail")
	if err == nil {
		t.Fatal("Call(ExceptionTest.Fail): error = nil, want an unsupported-opcode error (Fase 2)")
	}
}
