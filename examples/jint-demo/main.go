// Command jint-demo runs real JavaScript inside a Go process with no CGo
// and no external process: it loads the real, unmodified Jint 3.1.3
// NuGet package (a full C# JS engine) plus its whole transitive
// dependency chain (Esprima, System.Memory, ...) via vm.LoadPackage, then
// calls into a small compiled C# wrapper (JintWrapper.dll, built from
// JintWrapper.cs in this directory) that drives Jint's own
// Engine.Evaluate API. See docs/ROADMAP.md, Fase 3.27, for the
// architecture work (multi-assembly resolution, real virtual dispatch,
// generic-method overload disambiguation) this needed.
package main

import (
	"fmt"
	"log"
	"os"

	vmnet "github.com/arturoeanton/go-vmnet"
)

func main() {
	vm := vmnet.New()

	if err := vm.NuGet().Add("Jint", "3.1.3"); err != nil {
		log.Fatalf("NuGet().Add: %v", err)
	}
	if err := vm.NuGet().Restore(); err != nil {
		log.Fatalf("NuGet().Restore: %v (needs network access to nuget.org)", err)
	}

	jintAsm, err := vm.LoadPackage("Jint")
	if err != nil {
		log.Fatalf("LoadPackage(Jint): %v", err)
	}

	wrapperData, err := os.ReadFile("bin/Release/netstandard2.0/JintWrapper.dll")
	if err != nil {
		log.Fatalf("read JintWrapper.dll: %v (run `dotnet build -c Release` in this directory first)", err)
	}
	wrapperAsm, err := vm.LoadBytes("JintWrapper.dll", wrapperData)
	if err != nil {
		log.Fatalf("LoadBytes(JintWrapper.dll): %v", err)
	}
	wrapperAsm.WithDependencies(jintAsm)

	out, err := wrapperAsm.Call("VmnetJintDemo.JintWrapper", "RunJs", vmnet.String("1 + 2"))
	if err != nil {
		log.Fatalf("RunJs: %v", err)
	}
	fmt.Printf("RunJs(\"1 + 2\") = %v\n", out.Native())

	out2, err := wrapperAsm.Call("VmnetJintDemo.JintWrapper", "AddNumbers", vmnet.Float64(3), vmnet.Float64(4))
	if err != nil {
		log.Fatalf("AddNumbers: %v", err)
	}
	fmt.Printf("AddNumbers(3, 4) = %v\n", out2.Native())
}
