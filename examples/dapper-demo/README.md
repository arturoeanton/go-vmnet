# dapper-demo

Runs the real, unmodified Dapper 2.1.79 NuGet package's own `SqlMapper.Query`/`Execute` against a
minimal in-memory fake ADO.NET provider (`DapperDemoWrapper.dll`, built from
`DapperDemoWrapper.cs` in this directory) — no real database engine, no dotnet SDK needed at
runtime. vmnet doesn't implement a database at all: Dapper's own real reflection-based
column-to-object mapping code runs unmodified against a plain fake `IDbConnection`/`IDbCommand`/
`IDataReader`, the same shape a real driver (SqlClient, Npgsql, Microsoft.Data.Sqlite) would
supply. vmnet's own virtual-dispatch ancestor walk (`internal/interpreter/calls.go`) resolves
every ADO.NET interface call straight through to the fake provider's own concrete methods — the
same mechanism every other interface-typed call site in this project already relies on
(`IEnumerable<T>`, `IEqualityComparer<T>`, ...).

```bash
dotnet build DapperDemoWrapper.csproj -c Release
go run .
```

Expected output: three in-memory rows read via Dapper's real dynamic-row deserializer, a real
`SqlMapper.Execute` deleting one of them, a re-query confirming it's gone, and the fake
connection's state after `Close()`.

## What this demonstrates as genuinely working

- `SqlMapper.Query`'s real dynamic-row deserializer (`DapperRow`, a private nested class inside
  `Dapper.dll`) — reached via the non-generic `Query(Type, string, ...)` overload rather than
  `Query<T>()`, see `DapperDemoWrapper.cs`'s own doc comment for why.
- `SqlMapper.Execute`.
- Real `IDbConnection`/`IDbCommand`/`IDataReader`/`IDataParameter`/`IDataParameterCollection`
  interface dispatch against `FakeConnection`/`FakeCommand`/`FakeReader`/`FakeParameter`.
- `System.Data.Common.DbDataReader`'s own base-class `Dispose()` pattern — Dapper wraps a plain
  `IDataReader` in its own internal `WrappedBasicReader : DbDataReader`, whose public `Dispose()`
  is inherited (not overridden) from `DbDataReader` itself; only the protected `Dispose(bool)` is
  overridden, exactly the base-class-chaining shape `docs/en/ROADMAP.md` Fase 3.52 documents.

## Two real, permanent limitations found getting here

**Generic `Query<T>()`/`Execute<T>()` don't work.** Both do `typeof(T)` on their own generic
*method* type parameter internally. vmnet's `ir.LoadTypeToken` has a real, documented case for
this (`IsMethodGenericParam`) but no way to resolve it: the same compiled IR runs for every
different call site's own instantiation, and nothing threads "which generic arguments was the
currently-executing method itself invoked with" through to a `ldtoken` deep inside that method's
body (unlike a call *site's* own resolved generic arguments, which are threaded through fine).
Fixing this generally would mean carrying generic-method-instantiation context through the whole
method-invocation pipeline — a real, invasive interpreter change, not attempted here. The
workaround: `Query(Type, string, ...)` — Dapper's own non-generic overload — passes `typeof(object)`
as an ordinary compile-time-resolved argument instead, which happens to hit the exact code path
(`GetDeserializer`'s `type == typeof(object)` branch) that skips Dapper's *other* real limitation
below entirely.

**Any call that supplies an actual parameters object hits a regex vmnet can never run.** Dapper's
parameter binding — regardless of shape (an anonymous type, `DynamicParameters`, even a plain
dictionary) — always scans the raw SQL text first for a `{=name}` literal-replacement token, via
`Dapper.SqlMapper.CompiledRegex.LiteralTokens`: `(?<![\p{L}\p{N}_])\{=([\p{L}\p{N}_]+)\}`. That's a
negative lookbehind, a real .NET regex feature Go's RE2-based `regexp` engine can never support (no
backreferences, no lookaround at all — see `internal/bcl/system_regex.go`'s own doc comment on the
RE2-vs-.NET dialect gap). Every call in this demo passes literal SQL with no parameters object at
all, which skips that scan entirely; a real call like
`conn.Execute("UPDATE Person SET Name = @name", new { name = "x" })` fails immediately with
`ArgumentException: Invalid regex pattern ... invalid named capture`.

## Real bugs fixed getting here (Fase 3.52)

Not Dapper-specific workarounds — every one is a general interpreter/reflection/BCL fix, verified
against real `dotnet run` output for the identical C# source, diffed against vmnet's own output for
the same compiled DLL. Highlights (full list in `docs/en/ROADMAP.md`):

- `Type.GetMethod`/`GetProperty` called on a **closed generic type** (via `Type.MakeGenericType`,
  not `typeof(T)` on an already-closed type) silently failed to resolve at all — `FindTypeDef` was
  never given the real (open, unbound) TypeDef name to look up, only the closed
  `Outer+Inner\`1[[Arg]]` encoding. Real, load-bearing case: Dapper's own `SqlMapper` static
  constructor reflects over `TypeHandlerCache<DataTable>`/`<XmlDocument>`/`<XDocument>`/
  `<XElement>` this way to cache each one's `SetHandler` method — this crashed the moment
  `Dapper.SqlMapper` was touched at all, before a single real query ran.
- `List<T>`/`Dictionary<K,V>` subclassed directly (`class FakeParameterCollection :
  List<FakeParameter>, IDataParameterCollection`) had no base-`.ctor`-chaining native for `List\`1`
  (`Dictionary\`2` already had one) — every native `List<T>` method reached through the ancestor
  walk panicked on a nil receiver.
- `StringComparer.Ordinal`/`OrdinalIgnoreCase` had no `NativeTypeName` case at all, so a call site
  declared against `IEqualityComparer<string>` (Dapper's own
  `connectionStringComparer` field) could never redirect to the already-registered
  `StringComparer::GetHashCode`/`Equals` natives.
- `System.Data`/`System.Data.Common` had no support at all: `IDbConnection`/`IDbCommand`/
  `IDataReader`/`IDataParameter`/`IDbDataParameter`/`IDataParameterCollection` interface dispatch,
  `DbConnection`/`DbCommand`/`DbDataReader` abstract-base-class `.ctor` chaining, and
  `DbDataReader`'s own concrete `Dispose()` calling a subclass's `Dispose(bool)` override.
- `Type.GetProperty(ies)`/`PropertyInfo.PropertyType`/`GetGetMethod`/`GetSetMethod`, plus a small,
  explicitly-scoped fallback (`wellKnownBclProperties`) for the handful of real BCL framework
  properties (`CultureInfo.InvariantCulture`, `DbDataReader`'s own `this[int]` indexer) Dapper's
  cctor reflects over — vmnet has no BCL reflection metadata database at all, so these two specific,
  load-bearing cases are hand-mapped rather than solved generally.
