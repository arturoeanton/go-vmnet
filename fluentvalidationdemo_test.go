package vmnet

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFluentValidationDemoE2E is the Fase 3.64 demo: real, unmodified
// FluentValidation 11.9.2 running a real validator end to end, both
// rejecting an invalid object with the right error message and accepting
// a valid one. See examples/fluentvalidation-demo/ for the standalone
// runnable version and docs/en/ROADMAP.md, Fase 3.64, for the
// interpreter work this needed: walking AND compiling (not just
// inspecting) a real System.Linq.Expressions property-access tree.
//
// Needs network access to nuget.org (to restore the package) and the
// wrapper DLL built ahead of time:
//
//	dotnet build examples/fluentvalidation-demo/FvDemoWrapper.csproj -c Release
func TestFluentValidationDemoE2E(t *testing.T) {
	if os.Getenv("VMNET_NETWORK_TESTS") == "" {
		t.Skip("set VMNET_NETWORK_TESTS=1 to run (downloads FluentValidation from nuget.org)")
	}

	wrapperRelPath := "examples/fluentvalidation-demo/bin/Release/netstandard2.0/FvDemoWrapper.dll"
	wrapperData, err := os.ReadFile(filepath.FromSlash(wrapperRelPath))
	if err != nil {
		t.Skipf("FvDemoWrapper.dll not built: %v (run `dotnet build examples/fluentvalidation-demo/FvDemoWrapper.csproj -c Release`)", err)
	}

	dir := t.TempDir()
	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	os.Chdir(dir)

	vm := New()
	if err := vm.NuGet().Add("FluentValidation", "11.9.2"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := vm.NuGet().Restore(); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	fvAsm, err := vm.LoadPackage("FluentValidation")
	if err != nil {
		t.Fatalf("LoadPackage(FluentValidation): %v", err)
	}

	wrapperAsm, err := vm.LoadBytes("FvDemoWrapper.dll", wrapperData)
	if err != nil {
		t.Fatalf("LoadBytes(wrapper): %v", err)
	}
	wrapperAsm.WithDependencies(fvAsm)

	good, err := wrapperAsm.Call("VmnetFvDemo.Program", "Validate", String("Ada"))
	if err != nil {
		t.Fatalf("Validate(\"Ada\") error: %v", err)
	}
	if got := good.Native().(string); got != "valid" {
		t.Errorf("Validate(\"Ada\") = %q, want %q", got, "valid")
	}

	bad, err := wrapperAsm.Call("VmnetFvDemo.Program", "Validate", String(""))
	if err != nil {
		t.Fatalf("Validate(\"\") error: %v", err)
	}
	const wantBad = "invalid: Name is required"
	if got := bad.Native().(string); got != wantBad {
		t.Errorf("Validate(\"\") = %q, want %q", got, wantBad)
	}
}
