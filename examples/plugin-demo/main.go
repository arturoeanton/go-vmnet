// Command plugin-demo loads a real vmnet plugin — a plain, standalone
// .NET class library, not a fixture or NuGet package — the way an
// application actually would: LoadFile a compiled .dll, then CallBytes/
// CallJSON its one required Entry.Invoke([]byte) []byte entry point.
//
// BillingRules/ was scaffolded with:
//
//	dotnet new install ./templates/vmnet-plugin
//	dotnet new vmnet-plugin -n BillingRules
//
// and then had its generated Entry.Invoke body replaced with a small real
// business rule (an 8% flat tax line) — see BillingRules/Entry.cs and
// docs/en/plugin-sdk.md for the full guide.
package main

import (
	"fmt"
	"log"

	vmnet "github.com/arturoeanton/go-vmnet"
)

func main() {
	vm := vmnet.New()
	plugin, err := vm.LoadFile("BillingRules/bin/Release/netstandard2.0/BillingRules.dll")
	if err != nil {
		log.Fatalf("LoadFile: %v (run `dotnet build BillingRules/BillingRules.csproj -c Release` first)", err)
	}

	// CallBytes: raw JSON bytes in, raw JSON bytes out — vmnet never
	// looks inside either payload, Entry.Invoke owns the whole contract.
	out, err := plugin.CallBytes("BillingRules.Entry", "Invoke", []byte(`{"customer":"Ada","amount":100}`))
	if err != nil {
		log.Fatalf("CallBytes: %v", err)
	}
	fmt.Println("CallBytes:", string(out))

	// CallJSON: vmnet marshals the Go value to JSON on the way in, and
	// unmarshals the plugin's JSON response back into a Go value on the
	// way out.
	result, err := plugin.CallJSON("BillingRules.Entry", "Invoke", map[string]any{"customer": "Grace", "amount": 250})
	if err != nil {
		log.Fatalf("CallJSON: %v", err)
	}
	fmt.Printf("CallJSON: %v\n", result)
}
