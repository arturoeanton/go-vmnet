# tests/fixtures/csharp

Small `netstandard2.0` class library compiled by the official .NET SDK,
used as golden input for vmnet's PE/metadata/IL/interpreter tests.

**This is a dev-only dependency.** The .NET SDK is needed to *generate*
these `.dll` files; vmnet itself never requires .NET installed to *run*
them. Build artifacts (`bin/`, `obj/`) are gitignored — regenerate locally
or in CI before running tests that load them.

```bash
dotnet build tests/fixtures/csharp/Fixtures.csproj -c Release
```

Output: `tests/fixtures/csharp/bin/Release/netstandard2.0/Vmnet.Fixtures.dll`

| File | Type | Exercises | Used from |
|---|---|---|---|
| `SimpleMath.cs` | `SimpleMath` | static call, arithmetic, `ret` | Fase 1 |
| `Strings.cs` | `Strings` | string concat, `ldstr` | Fase 1 |
| `Loops.cs` | `Loops` | branches, loop | Fase 1 |
| `Objects.cs` | `Customer` | `newobj`, instance fields, auto-properties | Fase 2 |
| `CollectionsTest.cs` | `CollectionsTest` | `List<T>` | Fase 2 |
| `ExceptionTest.cs` | `ExceptionTest` | `throw` / managed exceptions | Fase 2 |
