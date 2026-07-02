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
| `Arrays.cs` | `Arrays` | `newarr`/`ldlen`/`ldelem.i4`/`stelem.i4` | Fase 3.5 |
| `ByRef.cs` | `ByRef` | `out`/`ref` params (`ldarga.s`/`ldloca.s`/`stind.i4`/`ldind.i4`) | Fase 3.5 |
| `Unsupported.cs` | `Unsupported` | exception filter clause, `catch (T) when (cond)` (deliberately unsupported, for checker tests — repurposed as coverage grows: was `System.Array` until Fase 3.5, plain `try`/`finally` until Fase 3.10) | Fase 3.10 |
| `Statics.cs` | `Statics` | static fields (`ldsfld`/`stsfld`), lazy `.cctor` | Fase 3.5 |
| `SwitchTest.cs` | `SwitchTest` | `switch` opcode (jump table) | Fase 3.6 |
| `StringOps.cs` | `StringOps` | `StringBuilder`, `String.Format`/`Substring`/indexer/`Equals` | Fase 3.6 |
| `Structs.cs` | `Structs`, `Point` | value types (`initobj`/`constrained.`, copy semantics), `Nullable<T>` | Fase 3.7 |
| `TypeChecks.cs` | `TypeChecks`, `Animal`/`Dog`/`Cat`/`IShape` | `isinst`/`castclass` (`is`/`as`/cast) against real class/interface hierarchy | Fase 3.8 |
| `Delegates.cs` | `Delegates`, `IntTransform` | delegates (`ldftn`/`newobj`/`Invoke`), closures capturing/mutating locals | Fase 3.9 |
| `TryCatch.cs` | `TryCatch` | real `try`/`catch`/`finally` (catch by type/base type, nested finally, rethrow) | Fase 3.10 |
