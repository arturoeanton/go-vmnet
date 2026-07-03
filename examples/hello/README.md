# hello

Smallest possible vmnet example: load a compiled C# DLL and call two of its
static methods from Go. This is the Go side of the Fase 1 demo — see
`docs/en/ROADMAP.md`.

```bash
dotnet build ../../tests/fixtures/csharp/Fixtures.csproj -c Release
go run .
```

Expected output:

```txt
SimpleMath.Add(3, 4) = 7
Strings.Hello("vmnet") = Hello vmnet
```
