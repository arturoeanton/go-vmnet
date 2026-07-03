package vmnet

import (
	"os"
	"path/filepath"
	"testing"
)

// TestJintDemoE2E is the Fase 3.27 demo: real, unmodified JavaScript
// execution inside vmnet via the real Jint 3.1.3 NuGet package and its
// real transitive dependency chain (Esprima, System.Memory,
// System.Buffers, System.Numerics.Vectors,
// System.Runtime.CompilerServices.Unsafe) — not a vmnet-specific fixture.
// See examples/jint-demo/ for the standalone runnable version and
// docs/ROADMAP.md, Fase 3.27, for the architecture work this needed
// (multi-assembly resolution, real virtual dispatch across inheritance
// chains, generic-method and struct-vs-class overload disambiguation).
//
// Needs network access to nuget.org (to restore Jint) and the wrapper
// DLL built ahead of time:
//
//	dotnet build examples/jint-demo/JintWrapper.csproj -c Release
func TestJintDemoE2E(t *testing.T) {
	if os.Getenv("VMNET_NETWORK_TESTS") == "" {
		t.Skip("set VMNET_NETWORK_TESTS=1 to run (downloads Jint from nuget.org)")
	}

	wrapperRelPath := "examples/jint-demo/bin/Release/netstandard2.0/JintWrapper.dll"
	wrapperData, err := os.ReadFile(filepath.FromSlash(wrapperRelPath))
	if err != nil {
		t.Skipf("JintWrapper.dll not built: %v (run `dotnet build examples/jint-demo/JintWrapper.csproj -c Release`)", err)
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
	pkgs, err := vm.NuGet().Packages()
	if err != nil {
		t.Fatalf("Packages: %v", err)
	}
	for _, p := range pkgs {
		t.Logf("%s@%s -> %s deps=%v unselectable=%q", p.ID, p.Version, p.SelectedAsset, p.Dependencies, p.Unselectable)
	}

	jintAsm, err := vm.LoadPackage("Jint")
	if err != nil {
		t.Fatalf("LoadPackage(Jint): %v", err)
	}

	wrapperAsm, err := vm.LoadBytes("JintWrapper.dll", wrapperData)
	if err != nil {
		t.Fatalf("LoadBytes(wrapper): %v", err)
	}
	wrapperAsm.WithDependencies(jintAsm)

	out, err := wrapperAsm.Call("VmnetJintDemo.JintWrapper", "RunJs", String("1 + 2"))
	if err != nil {
		t.Fatalf("RunJs error: %v", err)
	}
	if got := out.Native().(string); got != "3" {
		t.Errorf(`RunJs("1 + 2") = %q, want "3"`, got)
	}
	t.Logf("RunJs(\"1 + 2\") = %v", out.Native())

	out2, err := wrapperAsm.Call("VmnetJintDemo.JintWrapper", "AddNumbers", Float64(3), Float64(4))
	if err != nil {
		t.Fatalf("AddNumbers error: %v", err)
	}
	if got := out2.Native().(float64); got != 7 {
		t.Errorf("AddNumbers(3, 4) = %v, want 7", got)
	}
	t.Logf("AddNumbers(3, 4) = %v", out2.Native())
}
