// Command bind-demo shows `vmnet bind`'s own generated Go wrapper code
// in real use: examples/bind-demo/generated/fixtures.go was produced by
//
//	vmnet bind tests/fixtures/csharp/bin/Release/netstandard2.0/Vmnet.Fixtures.dll \
//	  --out=examples/bind-demo/generated --package=fixtures
//
// against this project's own shared golden fixture assembly — no
// hand-written glue, no manually-typed "Namespace.Type"/"Method" string
// literals at the call site below, just real, typed Go functions/methods
// generated straight from the assembly's own metadata.
package main

import (
	"fmt"
	"log"
	"os"

	vmnet "github.com/arturoeanton/go-vmnet"
	"github.com/arturoeanton/go-vmnet/examples/bind-demo/generated"
)

func main() {
	vm := vmnet.New()
	data, err := os.ReadFile("../../tests/fixtures/csharp/bin/Release/netstandard2.0/Vmnet.Fixtures.dll")
	if err != nil {
		log.Fatalf("read Vmnet.Fixtures.dll: %v (run `dotnet build tests/fixtures/csharp/Fixtures.csproj -c Release` from the repo root first)", err)
	}
	asm, err := vm.LoadBytes("Vmnet.Fixtures.dll", data)
	if err != nil {
		log.Fatalf("LoadBytes: %v", err)
	}

	// A precisely-typed, non-overloaded static method (SimpleMath.Add(int,
	// int) int) generates a real, idiomatic Go function — no vmnet.Value
	// wrapping/unwrapping or raw method-name strings at the call site.
	sum, err := fixtures.SimpleMathStatic_Add(asm, 3, 4)
	if err != nil {
		log.Fatalf("SimpleMathStatic_Add: %v", err)
	}
	fmt.Println("SimpleMath.Add(3, 4) =", sum)

	// A real object: NewCustomer constructs one, and its generated
	// property accessors read/write it like any other typed Go method.
	customer, err := fixtures.NewCustomer(asm)
	if err != nil {
		log.Fatalf("NewCustomer: %v", err)
	}
	if err := customer.SetName("Ada"); err != nil {
		log.Fatalf("SetName: %v", err)
	}
	name, err := customer.GetName()
	if err != nil {
		log.Fatalf("GetName: %v", err)
	}
	fmt.Println("Customer.Name =", name)
}
