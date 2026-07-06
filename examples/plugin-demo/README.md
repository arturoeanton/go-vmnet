# plugin-demo

Loads a real vmnet plugin the way an application actually would: a plain,
standalone .NET class library — not this project's own shared fixture
assembly, not a NuGet package — built once with the real .NET SDK, then
loaded and called from Go with no .NET runtime anywhere the Go program
itself runs.

`BillingRules/` was scaffolded from this project's own template:

```bash
dotnet new install ./templates/vmnet-plugin
dotnet new vmnet-plugin -n BillingRules
```

That produces `BillingRules.csproj` and an `Entry.cs` with a single
required entry point:

```csharp
namespace BillingRules
{
    public static class Entry
    {
        public static byte[] Invoke(byte[] input) { /* JSON in, JSON out */ }
    }
}
```

`BillingRules/Entry.cs` in this example has its generated starter body
replaced with a small real business rule (an 8% flat tax line on
`{"customer":"...", "amount":...}`) — exactly the intended path: generate,
then fill in your own logic.

```bash
cd BillingRules && dotnet build -c Release && cd ..
go run .
```

Expected output:

```txt
CallBytes: {"customer":"Ada","amount":100,"tax":8,"total":108}
CallJSON: map[amount:250 customer:Grace tax:20 total:270]
```

## Why this is the whole plugin contract

`vm.LoadFile` loads any compiled `.dll` — a plugin needs nothing
vmnet-specific beyond that one `byte[] Invoke(byte[])` static method:

- **`plugin.CallBytes(typeName, methodName, input)`** passes raw bytes in
  and gets raw bytes back — vmnet never looks inside either payload; the
  plugin owns the whole encoding (JSON here, but it could be anything).
- **`plugin.CallJSON(typeName, methodName, input)`** does the same call,
  but marshals a Go value to JSON on the way in and unmarshals the
  plugin's JSON response back into a Go value on the way out.

Both already exist on vmnet's frozen public API (`docs/en/api-stability.md`)
— the plugin *template* is new (`templates/vmnet-plugin`,
`docs/en/plugin-sdk.md`), the Go-side contract it targets is not.

See `examples/rules` for the same `CallJSON`/`CallBytes` pattern against a
plugin with real objects/collections/exceptions, and `docs/en/security.md`
for `VM.Permissions()` — a plugin gets the same deny-by-default sandbox
every other assembly vmnet loads does.
