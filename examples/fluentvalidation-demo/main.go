// Command fluentvalidation-demo runs the real, unmodified FluentValidation
// 11.9.2 NuGet package end to end: a real validator, built with the
// exact same fluent RuleFor/NotEmpty/WithMessage API any real .NET app
// uses, both rejecting an invalid object with the right error message
// and accepting a valid one — genuine PASS and FAIL outcomes, not just
// "it didn't crash."
//
// RuleFor(c => c.Name) compiles to a real System.Linq.Expressions tree
// that FluentValidation itself compiles AND invokes (Expression<Func<T,
// TProperty>>.Compile()) to read the actual property value being
// validated — this is a genuinely different, deeper use of Expression
// trees than examples/openxml-demo's own DocumentFormat.OpenXml
// dependency, which only ever inspects a tree's shape, never compiles or
// runs it. See docs/en/ROADMAP.md, Fase 3.64, for the interpreter work
// this needed.
package main

import (
	"fmt"
	"log"
	"os"

	vmnet "github.com/arturoeanton/go-vmnet"
)

func main() {
	vm := vmnet.New()

	if err := vm.NuGet().Add("FluentValidation", "11.9.2"); err != nil {
		log.Fatalf("NuGet().Add: %v", err)
	}
	if err := vm.NuGet().Restore(); err != nil {
		log.Fatalf("NuGet().Restore: %v (needs network access to nuget.org)", err)
	}
	fvAsm, err := vm.LoadPackage("FluentValidation")
	if err != nil {
		log.Fatalf("LoadPackage(FluentValidation): %v", err)
	}

	wrapperData, err := os.ReadFile("bin/Release/netstandard2.0/FvDemoWrapper.dll")
	if err != nil {
		log.Fatalf("read FvDemoWrapper.dll: %v (run `dotnet build FvDemoWrapper.csproj -c Release` in this directory first)", err)
	}
	wrapperAsm, err := vm.LoadBytes("FvDemoWrapper.dll", wrapperData)
	if err != nil {
		log.Fatalf("LoadBytes(FvDemoWrapper.dll): %v", err)
	}
	wrapperAsm.WithDependencies(fvAsm)

	good, err := wrapperAsm.Call("VmnetFvDemo.Program", "Validate", vmnet.String("Ada"))
	if err != nil {
		log.Fatalf("Validate(\"Ada\"): %v", err)
	}
	fmt.Println("Validate(\"Ada\") =", good.Native())

	bad, err := wrapperAsm.Call("VmnetFvDemo.Program", "Validate", vmnet.String(""))
	if err != nil {
		log.Fatalf("Validate(\"\"): %v", err)
	}
	fmt.Println("Validate(\"\") =", bad.Native())
}
