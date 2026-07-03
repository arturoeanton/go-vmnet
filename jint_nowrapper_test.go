package vmnet

import (
	"os"
	"testing"
)

// TestJintNoWrapperE2E is the Fase 3.28 counterpart to TestJintDemoE2E:
// the same real Jint 3.1.3 engine, driven with zero compiled C# glue —
// Assembly.New/Instance.Call construct a real Jint.Engine and call its
// instance methods directly from Go. See examples/jint-nowrapper for the
// standalone runnable version (including why Evaluate needs an explicit
// second argument and why AsNumber isn't reachable this way — both real
// C#-only language conveniences, not vmnet bugs).
//
// Needs network access to nuget.org (to restore Jint) — no dotnet SDK
// required at all, unlike TestJintDemoE2E.
func TestJintNoWrapperE2E(t *testing.T) {
	if os.Getenv("VMNET_NETWORK_TESTS") == "" {
		t.Skip("set VMNET_NETWORK_TESTS=1 to run (downloads Jint from nuget.org)")
	}

	dir := t.TempDir()
	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	os.Chdir(dir)

	vm := New()
	if err := vm.NuGet().Add("Jint", "3.1.3"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := vm.NuGet().Restore(); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	jintAsm, err := vm.LoadPackage("Jint")
	if err != nil {
		t.Fatalf("LoadPackage(Jint): %v", err)
	}

	t.Run("Evaluate literal expression", func(t *testing.T) {
		engine, err := jintAsm.New("Jint.Engine")
		if err != nil {
			t.Fatalf("New(Jint.Engine): %v", err)
		}
		if got := engine.TypeName(); got != "Jint.Engine" {
			t.Errorf("TypeName() = %q, want %q", got, "Jint.Engine")
		}

		result, err := engine.Call("Evaluate", String("1 + 2"), String(""))
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		inst, ok := result.(*Instance)
		if !ok {
			t.Fatalf("Evaluate result is not *Instance: %#v", result)
		}
		str, err := inst.Call("ToString")
		if err != nil {
			t.Fatalf("ToString: %v", err)
		}
		if got := str.Native().(string); got != "3" {
			t.Errorf(`Evaluate("1 + 2").ToString() = %q, want "3"`, got)
		}
	})

	t.Run("SetValue + variable expression", func(t *testing.T) {
		engine, err := jintAsm.New("Jint.Engine")
		if err != nil {
			t.Fatalf("New(Jint.Engine): %v", err)
		}
		if _, err := engine.Call("SetValue", String("a"), Float64(3)); err != nil {
			t.Fatalf("SetValue(a, 3): %v", err)
		}
		if _, err := engine.Call("SetValue", String("b"), Float64(4)); err != nil {
			t.Fatalf("SetValue(b, 4): %v", err)
		}
		sum, err := engine.Call("Evaluate", String("a + b"), String(""))
		if err != nil {
			t.Fatalf("Evaluate(a + b): %v", err)
		}
		str, err := sum.(*Instance).Call("ToString")
		if err != nil {
			t.Fatalf("ToString: %v", err)
		}
		if got := str.Native().(string); got != "7" {
			t.Errorf(`(a + b).ToString() = %q, want "7"`, got)
		}
	})
}
