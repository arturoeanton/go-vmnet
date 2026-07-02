// Command rules is the Fase 2 demo: a Go host calling a C# business-rules
// method that uses objects, property accessors, a List<T> and a
// Dictionary<string,int>, through the JSON bridge — plus a managed
// exception and a runaway plugin caught by the instruction sandbox. See
// docs/ROADMAP.md, "Demo de cierre de Fase 2".
package main

import (
	"errors"
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

	result, err := asm.CallJSON("Vmnet.Fixtures.Rules", "Eval", "checkout request")
	if err != nil {
		log.Fatalf("CallJSON(Rules.Eval): %v", err)
	}
	fmt.Println("Rules.Eval(\"checkout request\") =", result)

	_, err = asm.CallBytes("Vmnet.Fixtures.Rules", "Eval", []byte(""))
	var managed *vmnet.ManagedException
	if errors.As(err, &managed) {
		fmt.Printf("Rules.Eval(\"\") raised a managed exception: %s\n", managed)
	} else {
		log.Fatalf("expected a managed exception for empty input, got: %v", err)
	}

	fmt.Println("Loading a buggy plugin with an infinite loop...")
	_, err = asm.Call("Vmnet.Fixtures.Loops", "Runaway")
	fmt.Println("Runaway plugin stopped by the sandbox:", err)
}
