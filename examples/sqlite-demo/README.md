# sqlite-demo

Runs real, hand-written ADO.NET C# code — `using Microsoft.Data.Sqlite;`, `new
SqliteConnection(...)`, real `@name` and positional `?` parameter binding, a real
`SqliteTransaction` — against vmnet's own native, Go-backed `Microsoft.Data.Sqlite` provider
(`internal/bcl/system_data_sqlite.go`), itself backed by
[go-r2-sqlite](https://github.com/arturoeanton/go-r2-sqlite): a pure-Go, zero-CGO SQLite engine
that is vmnet's first-ever external Go dependency (see that file's own doc comment for why this
one, deliberate exception was made). No .NET SQLite driver, no CGO, no C library is ever touched —
the whole stack from `INSERT INTO ...` down to the bytes on disk is Go.

This is a separate example from `examples/dapper-demo` (left completely untouched) rather than an
extension of it: dapper-demo's own `FakeConnection` proves Dapper's real `SqlMapper` mapping code
runs correctly against any real `IDbConnection` shape, entirely in memory, no real database engine
needed. This demo proves the complementary half — a **real** database engine, a **real** `.db`
file on disk, real ADO.NET parameter binding and transactions — and then hands that same real
connection to Dapper's `SqlMapper` too, showing it doesn't care which is which.

```bash
dotnet build SqliteDemoWrapper.csproj -c Release
go run .
```

`SqliteDemoWrapper.csproj` references the *real* `Microsoft.Data.Sqlite` NuGet package — but only
so `dotnet build` has a real type to check `SqliteDemoWrapper.cs` against. That package's actual
DLL is never loaded into vmnet at runtime (`main.go` only attaches `Dapper.dll` as a dependency);
every `SqliteConnection`/`SqliteCommand`/`SqliteDataReader`/`SqliteParameter`/`SqliteTransaction`
call resolves against vmnet's own native implementation instead. The same source file would also
compile and run correctly against the real `Microsoft.Data.Sqlite` on a real CLR, unmodified.

Expected output: 3 rows inserted with `@name`-bound `SqliteParameter`s, 2 more inserted with
positional `?` parameters inside a committed `SqliteTransaction`, all 5 read back through plain
`SqliteDataReader.Read()`/`GetInt64`/`GetString`/`GetInt32`, a `@name`-bound `DELETE`, the same
connection queried and mutated through Dapper's real `SqlMapper.Query`/`Execute`, the connection
closed — and finally the same `.db` file opened independently by the real `sqlite3` CLI (if
present on `PATH`), printing the same rows back and a passing `PRAGMA integrity_check`.

## What this demonstrates as genuinely working

- `Microsoft.Data.Sqlite.SqliteConnection` — both a bare file path and a real
  `"Data Source=file.db"` connection string, `Open()`/`Close()`/`get_State()`, `CreateCommand()`,
  `BeginTransaction()`.
- `SqliteCommand.CreateParameter()` + `SqliteParameterCollection.Add()` — real `@name` binding
  (`INSERT ... VALUES (@id, @name, @age)`) and real positional `?` binding, both through Go's own
  `database/sql` (`sql.Named` for the former).
- A real `SqliteTransaction`: `BeginTransaction()`/`Commit()`, backed by a genuine Go `sql.Tx`.
- `SqliteDataReader.Read()`/`GetFieldCount`/`GetName`/`GetValue`/`GetFieldType`/`IsDBNull`/
  `GetInt64`/`GetString`/`GetInt32`/`Dispose()` — a real, streaming, forward-only cursor over
  `*sql.Rows`, not an in-memory table like `dapper-demo`'s own `FakeReader`.
- Dapper's real `SqlMapper.Query`/`Execute` against this real connection — the same
  `DapperRow`/`typeof(object)` path as `dapper-demo`, same real `Dapper.dll`, this time reading
  from and writing to an actual SQLite file.
- The `.db` file's binary authenticity: the real `sqlite3` CLI reads it back independently and
  passes `PRAGMA integrity_check`.

## What doesn't work here either (same root cause as dapper-demo)

Any Dapper call passing an actual parameters object (anonymous type, `DynamicParameters`, a plain
dictionary — any shape) always scans the SQL text first for a `{=name}` literal-replacement token,
via a .NET regex using a negative lookbehind Go's RE2-based `regexp` can never compile. This is a
Dapper-internal limitation, unrelated to which ADO.NET provider is underneath — real, unrelated to
this demo's own real parameter binding (`InsertPerson`/`InsertPeopleInTransaction` bind real
parameters directly through `SqliteCommand`/`SqliteParameter`, nowhere near Dapper's own scanning
code). Both `QueryViaDapper`/`ExecuteViaDapper` here only ever pass literal SQL with no parameters
object, the same documented workaround `dapper-demo` uses.

`Decimal` has no distinct representation in vmnet anywhere (`system_misc.go`'s own `formatValue`
already folds it into the same bucket as `Double`/`Single`) — a column bound or read as
`DbType.Decimal` is handled as an ordinary `double`, not a precise `System.Decimal`.
