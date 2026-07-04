package bcl

// System.Data / System.Data.Common (Fase 3.52) — Dapper's own SqlMapper
// (and any other ADO.NET-based micro-ORM) does its real object-
// relational mapping directly against IDbConnection/IDbCommand/
// IDbTransaction/IDataReader/IDataRecord/IDataParameter/
// IDbDataParameter/IDataParameterCollection — never against a concrete
// driver type by name. vmnet ships no real database driver (no SQL
// Server/SQLite wire protocol here, and none is needed for this):
// whichever real .NET class actually implements these interfaces (a
// real driver in production, or examples/dapper-demo's own minimal
// in-memory fake connection/command/reader) is a genuine plugin TypeDef,
// and Machine.call's existing virtual-dispatch ancestor walk
// (internal/interpreter/calls.go) already resolves every one of these
// interface members straight through to that concrete implementation's
// own real method with NO interpreter changes needed at all — the exact
// same mechanism IEnumerable`1/IEqualityComparer`1/IComparer`1 already
// rely on (comparer.go). Nothing needs registering here for the plain
// interfaces themselves at all.
//
// DbConnection/DbCommand/DbDataReader/DbParameter/DbParameterCollection/
// DbTransaction are different: real ADO.NET abstract BASE CLASSES (every
// real driver — SqlClient, Npgsql, Microsoft.Data.Sqlite — derives its
// own concrete connection/command/reader from these, not from the bare
// interfaces directly; Dapper's own async code paths specifically
// require a DbCommand/DbDataReader, since ExecuteReaderAsync/ReadAsync
// aren't part of the plain IDbCommand/IDataReader interfaces at all).
// A plugin class `class FakeReader : DbDataReader` extends one of these
// via a real (usually implicit, parameterless) `base()` constructor
// chain — a plain `call System.Data.Common.DbDataReader::.ctor()` on the
// already-allocated derived object, the exact same base-ctor-chaining
// shape baseExceptionCtorInPlace (system_exception.go) and
// dictCtorInPlace (system_collections.go) already handle for
// System.Exception/Dictionary`2. None of these base classes carry any
// real state of their own that vmnet needs to model here (every field a
// caller observes lives on the plugin's own derived class, exactly like
// every fixture's own exception subclass), so the chained ctor is a
// plain no-op — objectCtorNoop, shared verbatim with System.Object's own
// base-chaining registration (system_object.go).
func init() {
	for _, name := range []string{
		"System.Data.Common.DbConnection",
		"System.Data.Common.DbCommand",
		"System.Data.Common.DbDataReader",
		"System.Data.Common.DbParameter",
		"System.Data.Common.DbParameterCollection",
		"System.Data.Common.DbTransaction",
	} {
		register(name+"::.ctor", false, objectCtorNoop)
	}
}
