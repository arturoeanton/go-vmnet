package vmnet

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDiDemoE2E is the Fase 3.60 demo: real, unmodified Microsoft.
// Extensions.DependencyInjection 8.0.0 (Microsoft's own official
// dependency-injection container) running inside vmnet, resolving a
// service through real constructor injection — not a trivial
// parameterless-type special case. See examples/di-demo/ for the
// standalone runnable version and docs/en/ROADMAP.md, Fase 3.60, for the
// three interpreter fixes this needed: a method-overload-resolution
// tie-break that mis-picked ServiceDescriptor's own private constructor
// (an infinite self-recursion), typeof(T) never resolving on a generic
// method's own still-open type parameter, and reflection over a service
// implementation type declared in the WRAPPER assembly reached from code
// running inside the framework assembly with no declared dependency edge
// back to it at all.
//
// Needs network access to nuget.org (to restore the package) and the
// wrapper DLL built ahead of time:
//
//	dotnet build examples/di-demo/DiDemoWrapper.csproj -c Release
func TestDiDemoE2E(t *testing.T) {
	if os.Getenv("VMNET_NETWORK_TESTS") == "" {
		t.Skip("set VMNET_NETWORK_TESTS=1 to run (downloads Microsoft.Extensions.DependencyInjection from nuget.org)")
	}

	wrapperRelPath := "examples/di-demo/bin/Release/netstandard2.0/DiDemoWrapper.dll"
	wrapperData, err := os.ReadFile(filepath.FromSlash(wrapperRelPath))
	if err != nil {
		t.Skipf("DiDemoWrapper.dll not built: %v (run `dotnet build examples/di-demo/DiDemoWrapper.csproj -c Release`)", err)
	}

	dir := t.TempDir()
	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	os.Chdir(dir)

	vm := New()
	if err := vm.NuGet().Add("Microsoft.Extensions.DependencyInjection", "8.0.0"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := vm.NuGet().Restore(); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	diAsm, err := vm.LoadPackage("Microsoft.Extensions.DependencyInjection")
	if err != nil {
		t.Fatalf("LoadPackage(Microsoft.Extensions.DependencyInjection): %v", err)
	}

	wrapperAsm, err := vm.LoadBytes("DiDemoWrapper.dll", wrapperData)
	if err != nil {
		t.Fatalf("LoadBytes(wrapper): %v", err)
	}
	wrapperAsm.WithDependencies(diAsm)

	out, err := wrapperAsm.Call("VmnetDiDemo.Program", "Run", String("vmnet"))
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	const want = "Hello, vmnet! (at 2026-01-01T00:00:00Z)"
	if got := out.Native().(string); got != want {
		t.Errorf("Run(\"vmnet\") = %q, want %q", got, want)
	}
	t.Logf("Run(\"vmnet\") = %v", out.Native())
}
