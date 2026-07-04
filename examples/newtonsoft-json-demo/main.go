// Command newtonsoft-json-demo parses and edits a real JSON document
// through the real, unmodified Newtonsoft.Json 13.0.3 NuGet package — no
// dotnet SDK installed, no compiled C# wrapper. It drives Newtonsoft's
// own "LINQ to JSON" DOM (JObject/JValue, the real dynamic-style JSON
// tree API — no custom POCO class definition needed, so this works
// entirely through Assembly.Call/Assembly.New/Instance.Call, the same
// no-wrapper pattern examples/jint-nowrapper and examples/system-text-
// json-demo use) rather than JsonConvert.DeserializeObject<T>, which
// would need a compiled C# type to deserialize into.
package main

import (
	"fmt"
	"log"

	vmnet "github.com/arturoeanton/go-vmnet"
)

func main() {
	vm := vmnet.New()

	if err := vm.NuGet().Add("Newtonsoft.Json", "13.0.3"); err != nil {
		log.Fatalf("NuGet().Add: %v", err)
	}
	if err := vm.NuGet().Restore(); err != nil {
		log.Fatalf("NuGet().Restore: %v (needs network access to nuget.org)", err)
	}

	jsonAsm, err := vm.LoadPackage("Newtonsoft.Json")
	if err != nil {
		log.Fatalf("LoadPackage(Newtonsoft.Json): %v", err)
	}

	const input = `{"name":"vmnet","stars":42,"active":true}`

	docVal, err := jsonAsm.Call("Newtonsoft.Json.Linq.JObject", "Parse", vmnet.String(input))
	if err != nil {
		log.Fatalf("JObject.Parse: %v", err)
	}
	doc := docVal.(*vmnet.Instance)

	nameVal, err := doc.Call("get_Item", vmnet.String("name"))
	if err != nil {
		log.Fatalf("get_Item(name): %v", err)
	}
	if nameVal == nil {
		log.Fatalf("get_Item(name): returned nil Value")
	}
	nameStr, err := nameVal.(*vmnet.Instance).Call("ToString")
	if err != nil {
		log.Fatalf("name.ToString: %v", err)
	}

	starsVal, err := doc.Call("get_Item", vmnet.String("stars"))
	if err != nil {
		log.Fatalf("get_Item(stars): %v", err)
	}
	starsStr, err := starsVal.(*vmnet.Instance).Call("ToString")
	if err != nil {
		log.Fatalf("stars.ToString: %v", err)
	}

	fmt.Printf("%v:%v\n", nameStr.Native(), starsStr.Native())
}
