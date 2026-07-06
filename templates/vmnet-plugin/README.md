# PluginName

A [vmnet](https://github.com/arturoeanton/go-vmnet) plugin — build it once with
the real .NET SDK, then load and call it from Go with no .NET runtime
installed anywhere the Go program actually runs.

```bash
dotnet build -c Release
```

```go
vm := vmnet.New()
plugin, err := vm.LoadFile("bin/Release/netstandard2.0/PluginName.dll")
if err != nil {
    log.Fatal(err)
}
out, err := plugin.CallBytes("PluginName.Entry", "Invoke", []byte(`{"name":"Ada"}`))
if err != nil {
    log.Fatal(err)
}
fmt.Println(string(out)) // {"message":"Hello, Ada!"}
```

Or let vmnet handle the JSON marshaling on both sides:

```go
var result map[string]any
out, err := plugin.CallJSON("PluginName.Entry", "Invoke", map[string]any{"name": "Ada"})
```

`Entry.Invoke` in `Entry.cs` is the only contract vmnet requires: a public
static `byte[] Invoke(byte[])` method. Replace its body with your real
business logic — add whatever other classes, methods, and NuGet package
references your plugin needs to `PluginName.csproj`; only `Entry.Invoke`
itself needs to keep this exact signature, since that's the one method the
Go side calls into.
