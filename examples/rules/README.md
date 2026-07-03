# rules

Business-rules plugin example: a `Rules.Eval` C# method (objects, property
accessors, `List<int>`, `Dictionary<string,int>`) called from a Go host via
`Assembly.CallJSON`/`CallBytes`, plus a managed exception and a runaway
plugin caught by the instruction sandbox. This is the Fase 2 demo — see
`docs/en/ROADMAP.md`.

```bash
dotnet build ../../tests/fixtures/csharp/Fixtures.csproj -c Release
go run .
```

Expected output:

```txt
Rules.Eval("checkout request") = map[customer:acme corp ok:true]
Rules.Eval("") raised a managed exception: System.InvalidOperationException: empty input
Loading a buggy plugin with an infinite loop...
Runaway plugin stopped by the sandbox: vmnet: Vmnet.Fixtures.Loops.Runaway: interpreter: instruction limit exceeded
```
