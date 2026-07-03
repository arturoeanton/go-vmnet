// Command hello is the smallest possible vmnet example: load a compiled
// C# assembly and call two of its static methods from Go. It's the Go
// side of the Fase 1 demo (see docs/en/ROADMAP.md) and reuses the fixture DLL
// from tests/fixtures/csharp, since that's the only assembly vmnet has
// today — real examples arrive with more of the profile in later fases.
package main

import (
	"fmt"
	"log"

	vmnet "github.com/arturoeanton/go-vmnet"
)

func main() {
	vm := vmnet.New()

	asm, err := vm.LoadFile("../../tests/fixtures/csharp/bin/Release/netstandard2.0/Vmnet.Fixtures.dll")
	if err != nil {
		log.Fatalf("LoadFile: %v (run `dotnet build tests/fixtures/csharp/Fixtures.csproj -c Release` first)", err)
	}

	sum, err := asm.Call("Vmnet.Fixtures.SimpleMath", "Add", vmnet.Int32(3), vmnet.Int32(4))
	if err != nil {
		log.Fatalf("Call(SimpleMath.Add): %v", err)
	}
	fmt.Println("SimpleMath.Add(3, 4) =", sum.Native())

	greeting, err := asm.Call("Vmnet.Fixtures.Strings", "Hello", vmnet.String("vmnet"))
	if err != nil {
		log.Fatalf("Call(Strings.Hello): %v", err)
	}
	fmt.Println("Strings.Hello(\"vmnet\") =", greeting.Native())
}
