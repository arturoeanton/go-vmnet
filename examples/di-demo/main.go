// Command di-demo runs the real, unmodified Microsoft.Extensions.
// DependencyInjection 8.0.0 NuGet package — Microsoft's own official
// dependency-injection container, the same one every ASP.NET Core and
// worker-service Program.cs builds on — inside vmnet, with no .NET
// runtime installed. A small compiled C# wrapper (DiDemoWrapper.dll,
// built from DiDemoWrapper.cs in this directory) registers two real
// services and resolves one through the other via real constructor
// injection: Greeter's own constructor takes an IClock, which the
// container discovers and supplies on its own — not a trivial
// parameterless-type special case.
//
// Getting this far required three real interpreter fixes (Fase 3.60, see
// docs/en/ROADMAP.md): a method-overload-resolution tie-break that
// mis-picked ServiceDescriptor's own private constructor (an infinite
// self-recursion), typeof(T) never resolving on a generic method's own
// still-open type parameter (AddSingleton<TService,TImplementation>'s
// real body does exactly this), and reflection over a service
// implementation type declared in the WRAPPER assembly, reached from
// CODE RUNNING INSIDE the framework assembly with no declared dependency
// edge back to it at all.
package main

import (
	"fmt"
	"log"
	"os"

	vmnet "github.com/arturoeanton/go-vmnet"
)

func main() {
	vm := vmnet.New()

	if err := vm.NuGet().Add("Microsoft.Extensions.DependencyInjection", "8.0.0"); err != nil {
		log.Fatalf("NuGet().Add: %v", err)
	}
	if err := vm.NuGet().Restore(); err != nil {
		log.Fatalf("NuGet().Restore: %v (needs network access to nuget.org)", err)
	}
	diAsm, err := vm.LoadPackage("Microsoft.Extensions.DependencyInjection")
	if err != nil {
		log.Fatalf("LoadPackage(Microsoft.Extensions.DependencyInjection): %v", err)
	}

	wrapperData, err := os.ReadFile("bin/Release/netstandard2.0/DiDemoWrapper.dll")
	if err != nil {
		log.Fatalf("read DiDemoWrapper.dll: %v (run `dotnet build DiDemoWrapper.csproj -c Release` in this directory first)", err)
	}
	wrapperAsm, err := vm.LoadBytes("DiDemoWrapper.dll", wrapperData)
	if err != nil {
		log.Fatalf("LoadBytes(DiDemoWrapper.dll): %v", err)
	}
	wrapperAsm.WithDependencies(diAsm)

	out, err := wrapperAsm.Call("VmnetDiDemo.Program", "Run", vmnet.String("vmnet"))
	if err != nil {
		log.Fatalf("Run: %v", err)
	}
	fmt.Println("Program.Run(\"vmnet\") =", out.Native())
}
