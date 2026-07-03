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
| `Foreach.cs` | `ForeachTest` | `foreach` over `List<T>`/`Dictionary<K,V>`, `EqualityComparer<T>.Default`, `Math.Min`/`Max`, `String.Join` | Fase 3.11 |
| `DateTimeSpan.cs` | `DateTimeSpanTest` | `System.DateTime` (ctor, calendar math, `CompareTo`), `Span<T>`/`ReadOnlySpan<T>` (`AsSpan`, `Slice`, ref-returning indexer read/write, `ToString`) | Fase 3.12 |
| `InterfaceForeach.cs` | `InterfaceForeachTest` | `foreach` over a collection through an interface-typed reference (`IEnumerable<T>`) — a `List<T>` and a compiler-generated `yield return` iterator (explicit interface implementation) | Fase 3.13 |
| `CheapWins.cs` | `CheapWins` | `String`/`Char` predicates and helpers (`IsNullOrEmpty`, `Split`, `StartsWith`, `IndexOf`, `Replace`, `Trim`, `IsUpper`, `IsDigit`, ...), `Int32.ToString`, `List<T>`/`Dictionary<K,V>` extras (`set_Item`, `ToArray`, `AddRange`, `Contains`, `TryGetValue`) | Fase 3.13 |
| `TryCatch.cs` (`CustomException`/`CustomExceptionTest`) | `CustomExceptionTest` | a plugin-declared exception subclass: base-constructor chaining (`: base(message)`), catch by exact subtype and by real base type | Fase 3.13 |
| `Reflection.cs` | `ReflectionTest` | `typeof(T)` (`ldtoken` + `GetTypeFromHandle`), `Object.GetType()`, `System.Type` equality/`Name`/`FullName`/`IsAssignableFrom` | Fase 3.14, 3.16 |
| `Linq.cs` | `LinqTest` | `System.Linq.Enumerable` (`Where`/`Select`/`ToList` chained, `Any`/`All`/`FirstOrDefault`, `Select`/`ToArray` over an array source) — together with `Lazy.cs`, regression coverage for the Fase 3.17 nested-`<>c`-collision fix (both files' non-capturing lambdas produce separate same-named compiler-generated classes) | Fase 3.15, 3.17 |
| `Lazy.cs` | `LazyTest` | `System.Lazy<T>` (factory invoked exactly once, cached on repeat `.Value` access, `IsValueCreated`) | Fase 3.17 |
| `CheapWins2.cs` | `CheapWins2` | `String.Contains`/`.ctor(char[])`, `Environment.NewLine`, `Convert.ToInt32`, `Double.ToString`, `List.RemoveAt`/`Insert`, `Dictionary.Clear`, `FormatException`, `Interlocked.CompareExchange`, `StringComparer.Ordinal` | Fase 3.18 |
| `CollectionsExtra.cs` | `CollectionsExtra` | `HashSet<T>` (`Add`/`Contains`/`foreach`), `Stack<T>` (`Push`/`Pop`/`Count`), `TimeSpan` (ctor, `FromSeconds`, component properties) | Fase 3.19 |
