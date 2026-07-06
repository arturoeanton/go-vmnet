# The plugin SDK: `dotnet new vmnet-plugin`

`Assembly.CallBytes`/`Assembly.CallJSON` (`docs/en/api-stability.md`'s frozen
public API, spec §25.3-25.4) already let a Go program call any static
`byte[] X(byte[])` method on any loaded assembly. `templates/vmnet-plugin` is
a real `dotnet new` template that scaffolds a project shaped exactly for
that contract — the point isn't a new runtime capability, it's turning "the
Go side already knows how to call this shape" into "here's a one-command way
to start writing that shape."

## Install and generate

```bash
dotnet new install ./templates/vmnet-plugin
dotnet new vmnet-plugin -n BillingRules
```

This produces:

```
BillingRules/
  BillingRules.csproj   # netstandard2.0, no dependencies
  Entry.cs              # public static class Entry { public static byte[] Invoke(byte[] input) { ... } }
  README.md             # build + Go call-site instructions, name-substituted
```

`-n BillingRules` isn't just a directory name — `dotnet new`'s own
`sourceName` substitution (`.template.config/template.json`) rewrites the
namespace, the `.csproj` file name, `RootNamespace`/`AssemblyName`, and every
reference to `PluginName` inside `Entry.cs`/`README.md` to `BillingRules`,
the same way any real Microsoft template (`dotnet new classlib -n Foo`)
does.

## Build it, then call it from Go

```bash
cd BillingRules
dotnet build -c Release
```

```go
vm := vmnet.New()
plugin, err := vm.LoadFile("BillingRules/bin/Release/netstandard2.0/BillingRules.dll")
if err != nil {
    log.Fatal(err)
}
out, err := plugin.CallBytes("BillingRules.Entry", "Invoke", []byte(`{"name":"Ada"}`))
```

or, letting vmnet handle JSON marshaling on both sides:

```go
result, err := plugin.CallJSON("BillingRules.Entry", "Invoke", map[string]any{"name": "Ada"})
```

`vm.LoadFile` and `Assembly.CallBytes`/`CallJSON` are not new — they're the
same frozen API every other example in `examples/` already uses (see
`examples/rules` for the identical pattern against this project's own
shared fixture assembly). A plugin built from this template gets vmnet's
usual deny-by-default `Permissions` sandbox too (`docs/en/security.md`) —
nothing about being "a plugin" grants it any capability a normal loaded
assembly wouldn't already have.

## What `Entry.Invoke`'s generated body does, and why it's dependency-free

The generated starter reads a single `"name"` string field out of the input
JSON by hand (`IndexOf`/`Substring`, no JSON library) and returns a
greeting:

```csharp
public static byte[] Invoke(byte[] input)
{
    string json = Encoding.UTF8.GetString(input);
    string name = ReadStringField(json, "name") ?? "world";
    string output = "{\"message\":\"Hello, " + EscapeJson(name) + "!\"}";
    return Encoding.UTF8.GetBytes(output);
}
```

This is deliberately minimal — no nested objects, no unicode escapes — so a
freshly generated plugin builds and runs with nothing beyond the .NET SDK
itself, no NuGet restore required for the starter to work. Replace the body
with your real logic; if your plugin's real input/output shape is richer
than a handful of flat fields, add `Newtonsoft.Json` or `System.Text.Json`
to `BillingRules.csproj` — both work under vmnet for real, common
JSON-object-graph code today (see `docs/en/COMPATIBILITY.md`'s own
per-package write-ups for exactly what's verified and what still has open
gaps, e.g. [issue #3](https://github.com/arturoeanton/go-vmnet/issues/3) for
`dynamic`/`ExpandoObject` deserialization).

`examples/plugin-demo` checks in a second, filled-in version of the same
scaffold — `BillingRules/Entry.cs` there replaces the starter's greeting
with a small real business rule (an 8% flat tax line), showing the intended
generate-then-customize path end to end.

## A real bug this template found: `String.IndexOf(string, StringComparison)`

Building this template's own starter `Entry.cs` and running it for real
surfaced a genuine, general interpreter bug, not something specific to
plugins: vmnet's `String.IndexOf`/`LastIndexOf` natives
(`internal/bcl/system_string.go`) only ever receive a flat argument list —
no per-call signature metadata — so a trailing `int` argument was always
treated as a start index. `IndexOf(value, StringComparison.Ordinal)` passes
`StringComparison.Ordinal`'s own raw value (`4`) as that trailing argument,
which used to get silently misread as "start searching at rune index 4,"
either skipping a real earlier match or throwing a bogus
`ArgumentOutOfRangeException` outright on a short receiver string.

Fixed in `internal/interpreter/calls.go`'s `convertCharArgsForNative` (which
already threaded the call site's own resolved `paramTypeNames` through for
an analogous `char`-vs-`int` ambiguity, Fase 3.40): a new
`stringComparisonSensitiveNatives` table drops the trailing argument
entirely once `paramTypeNames` says it's really a `System.StringComparison`,
not an `int` — vmnet has no culture support anywhere (`CultureInfo`,
`StartsWith`, `Equals`, ... are all ordinal-only already), so there's
nothing to convert it to. See `TestCheapWins/IndexOf_with_StringComparison`
(`vmnet_test.go`) for the regression test.

### How to verify

```bash
go build ./...
go test ./...
go test -run TestCheapWins -v .
dotnet new install ./templates/vmnet-plugin
dotnet new vmnet-plugin -n VerifyPlugin -o /tmp/verify-plugin
cd /tmp/verify-plugin && dotnet build -c Release
dotnet new uninstall ./templates/vmnet-plugin   # (from the repo root)
cd examples/plugin-demo/BillingRules && dotnet build -c Release && cd ..
go run .
```
