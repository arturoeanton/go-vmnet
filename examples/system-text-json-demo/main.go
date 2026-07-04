// Command system-text-json-demo parses a real JSON document through the
// real, unmodified System.Text.Json NuGet package — no dotnet SDK
// installed, no compiled C# wrapper. It drives JsonDocument's real static
// Parse factory plus JsonElement's instance API directly from Go via
// Assembly.Call/Instance.Call (Fase 3.28), the same no-wrapper pattern
// examples/jint-nowrapper uses.
package main

import (
	"fmt"
	"log"

	vmnet "github.com/arturoeanton/go-vmnet"
)

func main() {
	vm := vmnet.New()

	if err := vm.NuGet().Add("System.Text.Json", "8.0.5"); err != nil {
		log.Fatalf("NuGet().Add: %v", err)
	}
	if err := vm.NuGet().Restore(); err != nil {
		log.Fatalf("NuGet().Restore: %v (needs network access to nuget.org)", err)
	}

	jsonAsm, err := vm.LoadPackage("System.Text.Json")
	if err != nil {
		log.Fatalf("LoadPackage(System.Text.Json): %v", err)
	}

	const input = `{"name":"vmnet","ok":true}`

	// JsonDocument.Parse(string, JsonDocumentOptions options = default) —
	// the "= default" is compile-time C# sugar the compiler fills in at
	// every real call site; the method itself still takes 2 arguments,
	// so the default JsonDocumentOptions has to be passed explicitly here
	// (same real-world case examples/jint-nowrapper documents for
	// Jint.Engine.Evaluate's own optional `source` parameter).
	options, err := jsonAsm.New("System.Text.Json.JsonDocumentOptions")
	if err != nil {
		log.Fatalf("new JsonDocumentOptions: %v", err)
	}

	docVal, err := jsonAsm.Call("System.Text.Json.JsonDocument", "Parse", vmnet.String(input), options)
	if err != nil {
		log.Fatalf("JsonDocument.Parse: %v", err)
	}
	doc := docVal.(*vmnet.Instance)

	rootVal, err := doc.Call("get_RootElement")
	if err != nil {
		log.Fatalf("get_RootElement: %v", err)
	}
	root := rootVal.(*vmnet.Instance)

	nameProp, err := root.Call("GetProperty", vmnet.String("name"))
	if err != nil {
		log.Fatalf("GetProperty(name): %v", err)
	}
	nameVal, err := nameProp.(*vmnet.Instance).Call("GetString")
	if err != nil {
		log.Fatalf("GetString: %v", err)
	}

	okProp, err := root.Call("GetProperty", vmnet.String("ok"))
	if err != nil {
		log.Fatalf("GetProperty(ok): %v", err)
	}
	okVal, err := okProp.(*vmnet.Instance).Call("GetBoolean")
	if err != nil {
		log.Fatalf("GetBoolean: %v", err)
	}

	// vmnet's Value model has no distinct Kind for bool (every CIL i4-
	// shaped value — int, bool, char, an enum's underlying storage — is
	// the same KindI4, spec §17.1): GetBoolean()'s real result comes back
	// as a plain int32, 0/1, not a Go bool.
	fmt.Printf("%v:%v\n", nameVal.Native(), okVal.Native().(int32) != 0)
}
