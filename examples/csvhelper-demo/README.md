# csvhelper-demo

Runs the real, unmodified CsvHelper 33.1.0 NuGet package's own `CsvReader.GetRecords<T>()` with
**no `ClassMap` registered at all** — CsvHelper falls back to its own `AutoMap()` reflection path,
matching each CSV header column to a `Product` property by name and building a compiled
Expression-tree delegate at runtime to construct and populate each record. No column
attributes, no hand-written mapping class anywhere in `CsvHelperDemoWrapper.cs`.

```bash
dotnet build CsvHelperDemoWrapper.csproj -c Release
go run .
```

Expected output: three parsed `Product` rows (string/int/double/bool columns), each converted
through CsvHelper's own real `TypeConverter`s.

## What this demonstrates as genuinely working

- `CsvContext.AutoMap(Type)` building a closed `DefaultClassMap<Product>` **purely through
  reflection** — `Type.GetConstructor(s)`, `Expression.New`/`Lambda`/`Compile` — since the record
  type is only known at runtime, there is no `newobj DefaultClassMap\`1<Product>::.ctor()` IL
  instruction anywhere to resolve statically.
- The compiler-generated iterator behind `CsvReader.GetRecords<T>()` calling back into another
  generic method (`ValidateHeader<T>()`) using its own class-level type parameter.
- CsvHelper's real `ObjectResolver`/`ObjectCreator` constructing `MemberMap<TClass, TMember>`
  instances and the record type itself via compiled `Expression.New`/`Lambda` trees, including the
  constructor-argument-array pattern (`Expression.Convert(Expression.ArrayIndex(args, i), argType)`)
  and the member-assignment pattern (`Expression.Bind`/`MakeMemberAccess` inside a `BlockExpression`).
  the real `Int32Converter`/`DoubleConverter`/`BooleanConverter` type converters.

## Real bugs fixed getting here (Fase 3.81)

Not CsvHelper-specific workarounds — every one is a general interpreter/reflection/BCL fix,
verified against real `dotnet run` output for the identical C# source, diffed against vmnet's own
output for the same compiled DLL. Full account in `docs/en/ROADMAP.md`; highlights:

- **Closed-generic identity lost across `Type.GetConstructor()`/`ConstructorInfo.Invoke()`.**
  `GetConstructor(s)` stripped a closed generic type's `[[...]]` suffix (needed to look up the
  right `TypeDef`, always indexed by open name) but then baked that *stripped* name into the
  returned `ConstructorInfo`, so any later construction through it lost its own generic
  arguments entirely.
- **Reflection-based construction (`Machine.New`, `Expression.New(ctor).Compile()`) resolved
  `TypeDef`/`MethodDef` by the closed name instead of the open one** — the exact inverse mistake,
  found once the first fix above surfaced it: `CsvHelper.Configuration.DefaultClassMap\`1[[Person]]`
  is never a real metadata name (ECMA-335: one `TypeDef` per open generic, never one per closed
  instantiation).
- **A compiler-generated iterator's class-level generic parameter didn't survive being forwarded
  into another generic method call.** `GetRecords<T>()`'s own state machine (`<GetRecords>d__91<T>`)
  calling `ValidateHeader<T>()` compiles to a `MethodSpec` instantiated with `!0` (a class-level
  `VAR` reference), not `!!0` (the already-handled method-level `MVAR` case) — silently resolved to
  an empty type name instead.
- **The same class-level sentinel didn't survive being nested one level inside a closed generic
  type.** `CsvContext.AutoMap<T>()` forwarding `T` into `ObjectResolver.Current.Resolve<
  DefaultClassMap<T>>()` instantiates a `MethodSpec` with `DefaultClassMap\`1[[!!0]]`, not a bare
  `!!0` — the sentinel-substitution pass now scans for `!!N`/`!N` as a substring anywhere in a
  resolved type name, not just a whole-string match.
- **`System.String.Join` given a real, plugin-defined `IEnumerable<string>`** (CsvHelper's own
  `MemberNameCollection`, a genuine compiled class with a `yield return this[i]` iterator — not a
  vmnet-native list/array) **silently formatted the un-enumerated collection object itself as one
  opaque placeholder string**, identical for every distinct member. `GetFieldIndex`'s own
  per-member cache key collapsed every member after the first to the *first* member's own
  already-cached column index — the *Age* column silently read the *Name* column's text with no
  error at all, until the wrong-typed value finally failed `Int32` conversion. Moved to a
  Machine-aware override that drives the real `GetEnumerator`/`MoveNext`/`get_Current` protocol
  when the fast-path (array/native list) doesn't apply.
- Several missing `System.Linq.Expressions.Expression` factories: `ArrayIndex`, `Bind`
  (`MemberAssignment`), `MakeMemberAccess`, the string-name `Call(instance, methodName, Type[],
  Expression[])` overload (plus its `params Expression[]` always compiling to one real array
  argument, not several separate ones), and the non-generic `Lambda(Type delegateType, Expression
  body, ...)` overload (disambiguated from the generic `Lambda<TDelegate>(body, ...)` by whether
  the first argument is itself a recognized `Expression` node at all).
- `Enumerable.SequenceEqual` and `Boolean.TryParse` were simply missing.

## One more fixed since (Fase 3.83)

`new List<Product>(csv.GetRecords<Product>())` — the `List<T>(IEnumerable<T> collection)`
constructor given a real plugin iterator as its source — used to silently produce an **empty**
list instead of driving the source's real enumeration protocol. `CsvHelperDemoWrapper.cs` first
shipped with a `foreach` + `Add()` workaround; now that the constructor itself drives
`m.enumerateAll` like every other real-source-consuming native already did, it uses
`new List<Product>(csv.GetRecords<Product>())` directly.
