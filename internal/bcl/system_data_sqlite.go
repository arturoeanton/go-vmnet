package bcl

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	// Registers the "r2sqlite" database/sql driver name (sql.Register,
	// in the package's own init()) — never referenced by symbol, exactly
	// like every other database/sql driver import. This is vmnet's FIRST
	// external Go dependency ever (go.mod had none before this, by design
	// — see docs/en/adr/0001-pure-go-core.md): a deliberate, one-time
	// exception, not a precedent for casually adding more. go-r2-sqlite
	// was chosen specifically because it's pure Go (CGO_ENABLED=0 still
	// works, matching vmnet's own no-native-toolchain posture) and
	// binary-compatible with real SQLite 3 database files (its own
	// README: files it writes pass `PRAGMA integrity_check` under the
	// real C sqlite3, and vice versa) — see examples/sqlite-demo for the
	// round-trip proof.
	_ "github.com/arturoeanton/go-r2-sqlite"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// Real, Go-native Microsoft.Data.Sqlite ADO.NET provider (Fase 3.53).
//
// Unlike System.Data.Common's DbConnection/DbCommand/.../DbDataReader
// (system_data.go's own doc comment) — real ABSTRACT base classes a
// PLUGIN's own concrete class extends, needing only a no-op base-.ctor
// chain since vmnet has no state of its own to model on them — the six
// types here (SqliteConnection, SqliteCommand, SqliteDataReader,
// SqliteParameter, SqliteParameterCollection, SqliteTransaction) are
// themselves the CONCRETE leaf implementation: vmnet supplies the whole
// class natively in Go, the same way ZipArchive (system_io_compression_
// zip.go) or MemoryStream (system_io.go) supply a real, concrete,
// Go-backed BCL type rather than interpreting one from IL. Registered
// under the exact real namespace/type names Microsoft's own actual
// Microsoft.Data.Sqlite NuGet package uses (confirmed against its public
// API), so real, unmodified C# doing `using Microsoft.Data.Sqlite;` +
// `new SqliteConnection(...)` needs zero source changes to run against
// this — see examples/sqlite-demo/SqliteDemoWrapper.cs, which is compiled
// with a real PackageReference to the actual Microsoft.Data.Sqlite
// package for its type surface at BUILD time, then run against vmnet's
// OWN implementation at EXECUTION time: the real package's own DLL is
// never loaded into vmnet at all (Assembly.WithDependencies only attaches
// Dapper.dll), so every call resolves through the plain string-keyed
// native lookups below (newObj checks bcl.LookupCtor before ever
// consulting an attached assembly's own TypeDefs — calls.go) regardless
// of which assembly the compiled MemberRef's TypeRef nominally points at.
//
// Placed in internal/bcl (plain Native/NativeCtor signatures, no Machine
// access) rather than internal/interpreter's machineRegistry/
// genericMachineRegistry (adonet.go's own pattern) after checking: unlike
// adonet.go's dbDataReaderDispose (which genuinely needs Machine.tryCall
// to re-dispatch to a PLUGIN subclass's own possibly-overridden
// Dispose(bool)), nothing here ever calls back into interpreted plugin
// code — every operation is a leaf call straight into Go's own
// database/sql, exactly like ZipArchive's real archive/zip calls or
// MemoryStream's real bytes.Buffer ones. DateTime values reuse this
// package's own existing dateTimeType/asDateTime/dateTimeFromTime
// (system_datetime.go) directly, which a separate interpreter-package
// file could not do (those are unexported).
//
// Decimal is a known, documented gap: vmnet has no distinct System.Decimal
// representation anywhere in this codebase (system_misc.go's own
// formatValue already folds "System.Decimal" into the same bucket as
// Double/Single) — a SQLite column bound through DbType.Decimal or read
// via GetDecimal is handled as an ordinary float64 (R8), the same
// simplification every other native numeric path in this project already
// makes.

// nativeSqliteConnection backs Microsoft.Data.Sqlite.SqliteConnection.
// db is nil until Open() succeeds — matching the real type's own
// lazy-connect semantics (the constructor never touches the file, only
// Open() does). dataSource is whatever real file path the connection
// string named; go-r2-sqlite's own engine.Open takes that path directly,
// the same as passing it straight to sql.Open("r2sqlite", dataSource).
type nativeSqliteConnection struct {
	dataSource string
	db         *sql.DB
	open       bool
}

// nativeSqliteTransaction backs Microsoft.Data.Sqlite.SqliteTransaction.
// done tracks whether Commit/Rollback already ran, so Dispose (real
// DbTransaction.Dispose semantics: rolls back an uncommitted transaction)
// doesn't double-close Go's own sql.Tx, which errors on a second
// Commit/Rollback call.
type nativeSqliteTransaction struct {
	tx   *sql.Tx
	done bool
}

// nativeSqliteCommand backs Microsoft.Data.Sqlite.SqliteCommand. connVal/
// txVal store the actual boxed runtime.Value handed to set_Connection/
// set_Transaction (not just the bare *nativeSqliteConnection/
// *nativeSqliteTransaction pointer) so get_Connection/get_Transaction
// hand back the SAME object identity a caller stored — matters for any
// real code that reference-compares (`cmd.Connection == conn`), even
// though nothing in this project's own demo happens to rely on that.
// paramsVal is created once (at CreateCommand time, or this type's own
// ctor) and never replaced, matching real SqliteCommand.Parameters being
// a fixed collection instance for the command's whole lifetime.
type nativeSqliteCommand struct {
	connVal   runtime.Value
	txVal     runtime.Value
	text      string
	paramsVal runtime.Value
	timeout   int32
	cmdType   int32
}

// nativeSqliteParameter backs Microsoft.Data.Sqlite.SqliteParameter.
// dbType/size/direction are accepted and stored (so a real get_DbType
// after set_DbType round-trips correctly) but never consulted by this
// provider's own parameter-binding code (bindParams below) — go-r2-
// sqlite's driver infers SQLite's own storage class straight from the Go
// value's dynamic type (see sqliteGoValue), the same dynamically-typed
// column model real SQLite itself uses regardless of what a caller's
// DbType claims.
type nativeSqliteParameter struct {
	name      string
	value     runtime.Value
	dbType    int32
	size      int32
	direction int32
}

// nativeSqliteParameterCollection backs Microsoft.Data.Sqlite.
// SqliteParameterCollection. Real Microsoft.Data.Sqlite backs this with a
// List<SqliteParameter> internally (DbParameterCollection : IList); this
// mirrors that shape directly rather than going through vmnet's own
// nativeList; needed since parameter items here are plain Go pointers
// (*nativeSqliteParameter), not boxed runtime.Values, for bindParams'
// convenience.
type nativeSqliteParameterCollection struct {
	items []*nativeSqliteParameter
}

// nativeSqliteDataReader backs Microsoft.Data.Sqlite.SqliteDataReader — a
// real, forward-only, streaming cursor over Go's own *sql.Rows (not
// materialized eagerly like examples/dapper-demo's own in-memory
// FakeReader over a fixed object[] table): current holds the most
// recently Read() row's raw driver-scanned Go values (int64/float64/
// string/[]byte/bool/time.Time/nil, exactly what database/sql/driver.
// Value itself allows), read fresh off the wire on every Read() call.
type nativeSqliteDataReader struct {
	rows    *sql.Rows
	cols    []string
	current []any
	closed  bool
}

func init() {
	registerCtor("Microsoft.Data.Sqlite.SqliteConnection", sqliteConnectionCtor)
	register("Microsoft.Data.Sqlite.SqliteConnection::Open", false, sqliteConnectionOpen)
	register("Microsoft.Data.Sqlite.SqliteConnection::Close", false, sqliteConnectionClose)
	register("Microsoft.Data.Sqlite.SqliteConnection::Dispose", false, sqliteConnectionClose)
	register("Microsoft.Data.Sqlite.SqliteConnection::get_State", true, sqliteConnectionGetState)
	register("Microsoft.Data.Sqlite.SqliteConnection::get_ConnectionString", true, sqliteConnectionGetConnectionString)
	register("Microsoft.Data.Sqlite.SqliteConnection::set_ConnectionString", false, sqliteConnectionSetConnectionString)
	register("Microsoft.Data.Sqlite.SqliteConnection::get_Database", true, sqliteConnectionGetDatabase)
	register("Microsoft.Data.Sqlite.SqliteConnection::get_ConnectionTimeout", true, sqliteConnectionGetTimeout)
	register("Microsoft.Data.Sqlite.SqliteConnection::CreateCommand", true, sqliteConnectionCreateCommand)
	register("Microsoft.Data.Sqlite.SqliteConnection::BeginTransaction", true, sqliteConnectionBeginTransaction)
	register("Microsoft.Data.Sqlite.SqliteConnection::ChangeDatabase", false, objectCtorNoop)

	registerCtor("Microsoft.Data.Sqlite.SqliteCommand", sqliteCommandCtor)
	register("Microsoft.Data.Sqlite.SqliteCommand::get_CommandText", true, sqliteCommandGetText)
	register("Microsoft.Data.Sqlite.SqliteCommand::set_CommandText", false, sqliteCommandSetText)
	register("Microsoft.Data.Sqlite.SqliteCommand::get_Connection", true, sqliteCommandGetConnection)
	register("Microsoft.Data.Sqlite.SqliteCommand::set_Connection", false, sqliteCommandSetConnection)
	register("Microsoft.Data.Sqlite.SqliteCommand::get_Transaction", true, sqliteCommandGetTransaction)
	register("Microsoft.Data.Sqlite.SqliteCommand::set_Transaction", false, sqliteCommandSetTransaction)
	register("Microsoft.Data.Sqlite.SqliteCommand::get_Parameters", true, sqliteCommandGetParameters)
	register("Microsoft.Data.Sqlite.SqliteCommand::get_CommandTimeout", true, sqliteCommandGetTimeout)
	register("Microsoft.Data.Sqlite.SqliteCommand::set_CommandTimeout", false, sqliteCommandSetTimeout)
	register("Microsoft.Data.Sqlite.SqliteCommand::get_CommandType", true, sqliteCommandGetType)
	register("Microsoft.Data.Sqlite.SqliteCommand::set_CommandType", false, sqliteCommandSetType)
	register("Microsoft.Data.Sqlite.SqliteCommand::CreateParameter", true, sqliteCommandCreateParameter)
	register("Microsoft.Data.Sqlite.SqliteCommand::ExecuteReader", true, sqliteCommandExecuteReader)
	register("Microsoft.Data.Sqlite.SqliteCommand::ExecuteNonQuery", true, sqliteCommandExecuteNonQuery)
	register("Microsoft.Data.Sqlite.SqliteCommand::ExecuteScalar", true, sqliteCommandExecuteScalar)
	register("Microsoft.Data.Sqlite.SqliteCommand::Prepare", false, objectCtorNoop)
	register("Microsoft.Data.Sqlite.SqliteCommand::Cancel", false, objectCtorNoop)
	register("Microsoft.Data.Sqlite.SqliteCommand::Dispose", false, sqliteCommandDispose)

	registerCtor("Microsoft.Data.Sqlite.SqliteParameter", sqliteParameterCtor)
	register("Microsoft.Data.Sqlite.SqliteParameter::get_ParameterName", true, sqliteParameterGetName)
	register("Microsoft.Data.Sqlite.SqliteParameter::set_ParameterName", false, sqliteParameterSetName)
	register("Microsoft.Data.Sqlite.SqliteParameter::get_Value", true, sqliteParameterGetValue)
	register("Microsoft.Data.Sqlite.SqliteParameter::set_Value", false, sqliteParameterSetValue)
	register("Microsoft.Data.Sqlite.SqliteParameter::get_DbType", true, sqliteParameterGetDbType)
	register("Microsoft.Data.Sqlite.SqliteParameter::set_DbType", false, sqliteParameterSetDbType)
	register("Microsoft.Data.Sqlite.SqliteParameter::get_Size", true, sqliteParameterGetSize)
	register("Microsoft.Data.Sqlite.SqliteParameter::set_Size", false, sqliteParameterSetSize)
	register("Microsoft.Data.Sqlite.SqliteParameter::get_Direction", true, sqliteParameterGetDirection)
	register("Microsoft.Data.Sqlite.SqliteParameter::set_Direction", false, sqliteParameterSetDirection)
	register("Microsoft.Data.Sqlite.SqliteParameter::get_IsNullable", true, sqliteParameterGetIsNullable)

	register("Microsoft.Data.Sqlite.SqliteParameterCollection::Add", true, sqliteParamsAdd)
	register("Microsoft.Data.Sqlite.SqliteParameterCollection::AddWithValue", true, sqliteParamsAddWithValue)
	register("Microsoft.Data.Sqlite.SqliteParameterCollection::get_Count", true, sqliteParamsCount)
	register("Microsoft.Data.Sqlite.SqliteParameterCollection::get_Item", true, sqliteParamsGetItem)
	register("Microsoft.Data.Sqlite.SqliteParameterCollection::Clear", false, sqliteParamsClear)
	register("Microsoft.Data.Sqlite.SqliteParameterCollection::Contains", true, sqliteParamsContains)
	register("Microsoft.Data.Sqlite.SqliteParameterCollection::IndexOf", true, sqliteParamsIndexOf)

	register("Microsoft.Data.Sqlite.SqliteTransaction::Commit", false, sqliteTransactionCommit)
	register("Microsoft.Data.Sqlite.SqliteTransaction::Rollback", false, sqliteTransactionRollback)
	register("Microsoft.Data.Sqlite.SqliteTransaction::Dispose", false, sqliteTransactionDispose)
	register("Microsoft.Data.Sqlite.SqliteTransaction::get_Connection", true, sqliteTransactionGetConnection)

	register("Microsoft.Data.Sqlite.SqliteDataReader::Read", true, sqliteReaderRead)
	register("Microsoft.Data.Sqlite.SqliteDataReader::NextResult", true, sqliteReaderNextResult)
	register("Microsoft.Data.Sqlite.SqliteDataReader::get_FieldCount", true, sqliteReaderFieldCount)
	register("Microsoft.Data.Sqlite.SqliteDataReader::get_RecordsAffected", true, sqliteReaderRecordsAffected)
	register("Microsoft.Data.Sqlite.SqliteDataReader::get_Depth", true, sqliteReaderDepth)
	register("Microsoft.Data.Sqlite.SqliteDataReader::get_IsClosed", true, sqliteReaderIsClosed)
	register("Microsoft.Data.Sqlite.SqliteDataReader::get_HasRows", true, sqliteReaderHasRows)
	register("Microsoft.Data.Sqlite.SqliteDataReader::GetName", true, sqliteReaderGetName)
	register("Microsoft.Data.Sqlite.SqliteDataReader::GetOrdinal", true, sqliteReaderGetOrdinal)
	register("Microsoft.Data.Sqlite.SqliteDataReader::GetValue", true, sqliteReaderGetValue)
	register("Microsoft.Data.Sqlite.SqliteDataReader::GetValues", true, sqliteReaderGetValues)
	register("Microsoft.Data.Sqlite.SqliteDataReader::GetFieldType", true, sqliteReaderGetFieldType)
	register("Microsoft.Data.Sqlite.SqliteDataReader::GetDataTypeName", true, sqliteReaderGetDataTypeName)
	register("Microsoft.Data.Sqlite.SqliteDataReader::IsDBNull", true, sqliteReaderIsDBNull)
	register("Microsoft.Data.Sqlite.SqliteDataReader::GetBoolean", true, sqliteReaderGetBoolean)
	register("Microsoft.Data.Sqlite.SqliteDataReader::GetByte", true, sqliteReaderGetByte)
	register("Microsoft.Data.Sqlite.SqliteDataReader::GetChar", true, sqliteReaderGetChar)
	register("Microsoft.Data.Sqlite.SqliteDataReader::GetInt16", true, sqliteReaderGetInt16)
	register("Microsoft.Data.Sqlite.SqliteDataReader::GetInt32", true, sqliteReaderGetInt32)
	register("Microsoft.Data.Sqlite.SqliteDataReader::GetInt64", true, sqliteReaderGetInt64)
	register("Microsoft.Data.Sqlite.SqliteDataReader::GetFloat", true, sqliteReaderGetFloat)
	register("Microsoft.Data.Sqlite.SqliteDataReader::GetDouble", true, sqliteReaderGetDouble)
	register("Microsoft.Data.Sqlite.SqliteDataReader::GetDecimal", true, sqliteReaderGetDouble)
	register("Microsoft.Data.Sqlite.SqliteDataReader::GetString", true, sqliteReaderGetString)
	register("Microsoft.Data.Sqlite.SqliteDataReader::GetDateTime", true, sqliteReaderGetDateTime)
	register("Microsoft.Data.Sqlite.SqliteDataReader::GetData", true, sqliteReaderGetData)
	register("Microsoft.Data.Sqlite.SqliteDataReader::GetSchemaTable", true, sqliteReaderGetSchemaTable)
	register("Microsoft.Data.Sqlite.SqliteDataReader::GetBytes", true, sqliteReaderGetBytesStub)
	register("Microsoft.Data.Sqlite.SqliteDataReader::GetChars", true, sqliteReaderGetBytesStub)
	register("Microsoft.Data.Sqlite.SqliteDataReader::get_Item", true, sqliteReaderGetItem)
	register("Microsoft.Data.Sqlite.SqliteDataReader::Close", false, sqliteReaderDispose)
	register("Microsoft.Data.Sqlite.SqliteDataReader::Dispose", false, sqliteReaderDispose)
}

// ---- connection string parsing ----

// parseSqliteConnectionString accepts both a bare file path (vmnet's own
// convenience, not real ADO.NET syntax) and a real Microsoft.Data.Sqlite
// "Data Source=file.db;..." keyword=value connection string (semicolon-
// separated, case-insensitive keys — "Data Source"/"DataSource"/
// "Filename" are the real synonyms for the same key). A string with no
// "=" at all can't be a keyword=value string regardless (every real key
// needs one), so it's treated as a bare path directly.
func parseSqliteConnectionString(s string) string {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "=") {
		return s
	}
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(kv[0])) {
		case "data source", "datasource", "filename":
			return strings.TrimSpace(kv[1])
		}
	}
	return s
}

// ---- SqliteConnection ----

func sqliteConnectionCtor(args []runtime.Value) (*runtime.Object, error) {
	conn := &nativeSqliteConnection{}
	if len(args) > 0 && args[0].Kind == runtime.KindString {
		conn.dataSource = parseSqliteConnectionString(args[0].Str)
	}
	return &runtime.Object{Native: conn}, nil
}

func sqliteConnectionOf(v runtime.Value) (*nativeSqliteConnection, bool) {
	v = derefReceiver(v)
	if v.Kind != runtime.KindObject || v.Obj == nil {
		return nil, false
	}
	c, ok := v.Obj.Native.(*nativeSqliteConnection)
	return c, ok
}

func asSqliteConnection(args []runtime.Value) (*nativeSqliteConnection, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("bcl: SqliteConnection method called without a receiver")
	}
	c, ok := sqliteConnectionOf(args[0])
	if !ok {
		return nil, fmt.Errorf("bcl: SqliteConnection method receiver is not a SqliteConnection")
	}
	return c, nil
}

// sqliteManagedException wraps a real Go error (a real go-r2-sqlite
// engine error — bad SQL, constraint violation, missing file directory,
// ...) as the same kind of catchable managed exception any other native
// in this project raises for a real runtime failure, so `catch
// (Exception e)` in real C# around a real, honest error still works
// (real Microsoft.Data.Sqlite throws SqliteException for these; a plain
// System.Exception is close enough — vmnet has no SqliteException
// TypeDef of its own to raise instead, same posture as every other
// foreign BCL exception type this project doesn't model 1:1).
func sqliteManagedException(err error) error {
	if err == nil {
		return nil
	}
	return &runtime.ManagedException{TypeName: "System.Exception", Message: err.Error()}
}

func sqliteConnectionOpen(args []runtime.Value) (runtime.Value, error) {
	conn, err := asSqliteConnection(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if conn.open && conn.db != nil {
		// Real SqliteConnection.Open() throws InvalidOperationException on
		// an already-open connection; treated as an idempotent no-op here
		// instead — simpler, and no real fixture in this project's own
		// demo needs the throwing behavior.
		return runtime.Value{}, nil
	}
	db, err := sql.Open("r2sqlite", conn.dataSource)
	if err != nil {
		return runtime.Value{}, sqliteManagedException(err)
	}
	// sql.Open never actually dials/opens anything by itself (Go's own
	// database/sql is always lazy) — Ping forces a real connection
	// attempt right now, matching real SqliteConnection.Open()'s own
	// eager behavior (a bad path/permission error surfaces here, not
	// silently deferred to the first query).
	if err := db.Ping(); err != nil {
		db.Close()
		return runtime.Value{}, sqliteManagedException(err)
	}
	conn.db = db
	conn.open = true
	return runtime.Value{}, nil
}

func sqliteConnectionClose(args []runtime.Value) (runtime.Value, error) {
	conn, err := asSqliteConnection(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if conn.db != nil {
		conn.db.Close()
	}
	conn.open = false
	return runtime.Value{}, nil
}

// ConnectionState's real ordinal values: Closed=0, Open=1 (Connecting=2,
// Executing=4, Fetching=8, Broken=16 all unused here — vmnet's connection
// is only ever synchronously fully-open or fully-closed, never
// mid-transition).
func sqliteConnectionGetState(args []runtime.Value) (runtime.Value, error) {
	conn, err := asSqliteConnection(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if conn.open {
		return runtime.Int32(1), nil
	}
	return runtime.Int32(0), nil
}

func sqliteConnectionGetConnectionString(args []runtime.Value) (runtime.Value, error) {
	conn, err := asSqliteConnection(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.String(conn.dataSource), nil
}

func sqliteConnectionSetConnectionString(args []runtime.Value) (runtime.Value, error) {
	conn, err := asSqliteConnection(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) > 1 && args[1].Kind == runtime.KindString {
		conn.dataSource = parseSqliteConnectionString(args[1].Str)
	}
	return runtime.Value{}, nil
}

func sqliteConnectionGetDatabase(args []runtime.Value) (runtime.Value, error) {
	conn, err := asSqliteConnection(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.String(conn.dataSource), nil
}

func sqliteConnectionGetTimeout(args []runtime.Value) (runtime.Value, error) {
	return runtime.Int32(30), nil
}

func sqliteConnectionCreateCommand(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 {
		return runtime.Value{}, fmt.Errorf("bcl: SqliteConnection.CreateCommand called without a receiver")
	}
	cmd := &nativeSqliteCommand{connVal: derefReceiver(args[0]), paramsVal: newSqliteParameterCollectionValue()}
	return runtime.ObjRef(&runtime.Object{Native: cmd}), nil
}

func sqliteConnectionBeginTransaction(args []runtime.Value) (runtime.Value, error) {
	conn, err := asSqliteConnection(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if conn.db == nil {
		return runtime.Value{}, fmt.Errorf("bcl: SqliteConnection.BeginTransaction: connection is not open")
	}
	tx, err := conn.db.Begin()
	if err != nil {
		return runtime.Value{}, sqliteManagedException(err)
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeSqliteTransaction{tx: tx}}), nil
}

// ---- SqliteTransaction ----

func sqliteTransactionOf(v runtime.Value) (*nativeSqliteTransaction, bool) {
	v = derefReceiver(v)
	if v.Kind != runtime.KindObject || v.Obj == nil {
		return nil, false
	}
	t, ok := v.Obj.Native.(*nativeSqliteTransaction)
	return t, ok
}

func asSqliteTransaction(args []runtime.Value) (*nativeSqliteTransaction, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("bcl: SqliteTransaction method called without a receiver")
	}
	t, ok := sqliteTransactionOf(args[0])
	if !ok {
		return nil, fmt.Errorf("bcl: SqliteTransaction method receiver is not a SqliteTransaction")
	}
	return t, nil
}

func sqliteTransactionCommit(args []runtime.Value) (runtime.Value, error) {
	t, err := asSqliteTransaction(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if t.done {
		return runtime.Value{}, nil
	}
	if err := t.tx.Commit(); err != nil {
		return runtime.Value{}, sqliteManagedException(err)
	}
	t.done = true
	return runtime.Value{}, nil
}

func sqliteTransactionRollback(args []runtime.Value) (runtime.Value, error) {
	t, err := asSqliteTransaction(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if t.done {
		return runtime.Value{}, nil
	}
	if err := t.tx.Rollback(); err != nil {
		return runtime.Value{}, sqliteManagedException(err)
	}
	t.done = true
	return runtime.Value{}, nil
}

// sqliteTransactionDispose matches real DbTransaction.Dispose(): rolls
// back if the transaction was never explicitly committed (or rolled
// back) — the standard "using (var tx = conn.BeginTransaction()) { ...
// forgot to Commit ... }" safety net.
func sqliteTransactionDispose(args []runtime.Value) (runtime.Value, error) {
	t, err := asSqliteTransaction(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if !t.done {
		_ = t.tx.Rollback()
		t.done = true
	}
	return runtime.Value{}, nil
}

func sqliteTransactionGetConnection(args []runtime.Value) (runtime.Value, error) {
	return runtime.Value{}, nil
}

// ---- SqliteCommand ----

// sqlExecQuerier is exactly the subset of *sql.DB/*sql.Tx a command needs
// to run against — whichever one is actually bound (a command with a
// Transaction set runs through that *sql.Tx directly, same as real ADO.
// NET requiring an explicit `cmd.Transaction = tx` to participate).
type sqlExecQuerier interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
}

func sqliteCommandCtor(args []runtime.Value) (*runtime.Object, error) {
	cmd := &nativeSqliteCommand{paramsVal: newSqliteParameterCollectionValue(), cmdType: 1}
	if len(args) > 0 && args[0].Kind == runtime.KindString {
		cmd.text = args[0].Str
	}
	if len(args) > 1 {
		cmd.connVal = derefReceiver(args[1])
	}
	if len(args) > 2 {
		cmd.txVal = derefReceiver(args[2])
	}
	return &runtime.Object{Native: cmd}, nil
}

func sqliteCommandOf(v runtime.Value) (*nativeSqliteCommand, bool) {
	v = derefReceiver(v)
	if v.Kind != runtime.KindObject || v.Obj == nil {
		return nil, false
	}
	c, ok := v.Obj.Native.(*nativeSqliteCommand)
	return c, ok
}

func asSqliteCommand(args []runtime.Value) (*nativeSqliteCommand, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("bcl: SqliteCommand method called without a receiver")
	}
	c, ok := sqliteCommandOf(args[0])
	if !ok {
		return nil, fmt.Errorf("bcl: SqliteCommand method receiver is not a SqliteCommand")
	}
	return c, nil
}

func sqliteCommandGetText(args []runtime.Value) (runtime.Value, error) {
	c, err := asSqliteCommand(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.String(c.text), nil
}

func sqliteCommandSetText(args []runtime.Value) (runtime.Value, error) {
	c, err := asSqliteCommand(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) > 1 && args[1].Kind == runtime.KindString {
		c.text = args[1].Str
	}
	return runtime.Value{}, nil
}

func sqliteCommandGetConnection(args []runtime.Value) (runtime.Value, error) {
	c, err := asSqliteCommand(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return c.connVal, nil
}

func sqliteCommandSetConnection(args []runtime.Value) (runtime.Value, error) {
	c, err := asSqliteCommand(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) > 1 {
		c.connVal = derefReceiver(args[1])
	}
	return runtime.Value{}, nil
}

func sqliteCommandGetTransaction(args []runtime.Value) (runtime.Value, error) {
	c, err := asSqliteCommand(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return c.txVal, nil
}

func sqliteCommandSetTransaction(args []runtime.Value) (runtime.Value, error) {
	c, err := asSqliteCommand(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) > 1 {
		c.txVal = derefReceiver(args[1])
	}
	return runtime.Value{}, nil
}

func sqliteCommandGetParameters(args []runtime.Value) (runtime.Value, error) {
	c, err := asSqliteCommand(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return c.paramsVal, nil
}

func sqliteCommandGetTimeout(args []runtime.Value) (runtime.Value, error) {
	c, err := asSqliteCommand(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(c.timeout), nil
}

func sqliteCommandSetTimeout(args []runtime.Value) (runtime.Value, error) {
	c, err := asSqliteCommand(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) > 1 && args[1].Kind == runtime.KindI4 {
		c.timeout = args[1].I4
	}
	return runtime.Value{}, nil
}

func sqliteCommandGetType(args []runtime.Value) (runtime.Value, error) {
	c, err := asSqliteCommand(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(c.cmdType), nil
}

func sqliteCommandSetType(args []runtime.Value) (runtime.Value, error) {
	c, err := asSqliteCommand(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) > 1 && args[1].Kind == runtime.KindI4 {
		c.cmdType = args[1].I4
	}
	return runtime.Value{}, nil
}

func sqliteCommandCreateParameter(args []runtime.Value) (runtime.Value, error) {
	if _, err := asSqliteCommand(args); err != nil {
		return runtime.Value{}, err
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeSqliteParameter{value: runtime.Null()}}), nil
}

func sqliteCommandDispose(args []runtime.Value) (runtime.Value, error) {
	// Real SqliteCommand.Dispose() releases its own prepared-statement
	// handle; this provider prepares nothing ahead of time (each Execute*
	// call runs the SQL text directly through database/sql, which does
	// its own statement caching internally), so there's nothing to
	// release here — matches DbCommand's base Dispose() being a no-op
	// absent a real unmanaged handle (adonet.go's own doc comment).
	return runtime.Value{}, nil
}

// target picks whichever of *sql.DB/*sql.Tx this command should actually
// run against: its own bound Transaction if one is set (real ADO.NET
// requires `cmd.Transaction = tx` explicitly — a command never
// auto-joins a transaction just because its Connection happens to have
// one open), falling back to the connection's own pooled *sql.DB.
func (c *nativeSqliteCommand) target() (sqlExecQuerier, error) {
	if t, ok := sqliteTransactionOf(c.txVal); ok && t.tx != nil && !t.done {
		return t.tx, nil
	}
	conn, ok := sqliteConnectionOf(c.connVal)
	if !ok || conn.db == nil {
		return nil, fmt.Errorf("bcl: SqliteCommand has no open Connection")
	}
	return conn.db, nil
}

// bindParams converts this command's own Parameters collection into the
// plain []any database/sql itself accepts: sql.Named(name, value) when
// ANY parameter has a real ParameterName set (the overwhelmingly common
// case — Dapper-style `@name`/`:name`/`$name` placeholders), or bare
// positional values in declaration order for `?`-style placeholders
// otherwise. go-r2-sqlite's own named-parameter matching (engine/expr.go)
// tries the name both with and without its SQL-text sigil, so passing
// ParameterName through verbatim (whatever prefix — "@id", ":id", "id" —
// the caller itself chose) works either way without vmnet needing to
// guess which sigil convention a given command's SQL text used.
func (c *nativeSqliteCommand) bindParams() ([]any, error) {
	pc, ok := sqliteParamCollectionOf(c.paramsVal)
	if !ok || len(pc.items) == 0 {
		return nil, nil
	}
	named := false
	for _, p := range pc.items {
		if p.name != "" {
			named = true
			break
		}
	}
	out := make([]any, 0, len(pc.items))
	for _, p := range pc.items {
		gv, err := sqliteGoValue(p.value)
		if err != nil {
			return nil, err
		}
		if named {
			// Go's own database/sql (not go-r2-sqlite specifically —
			// convert.go's validateNamedValueName) requires sql.Named's
			// Name to "begin with a letter": no "@"/":"/"$" sigil, unlike
			// a real Microsoft.Data.Sqlite ParameterName, which normally
			// includes one (real code overwhelmingly writes
			// `new SqliteParameter("@id", ...)`, matching the SQL text's
			// own "@id" placeholder verbatim). Stripping it here is the
			// bridge between the two conventions; go-r2-sqlite's own
			// named-parameter lookup (engine/expr.go) already tries the
			// SQL text's placeholder both with and without its sigil, so
			// a stripped name still matches "@id"/":id"/"$id" in the
			// command text regardless of which sigil the caller used.
			out = append(out, sql.Named(stripParamSigil(p.name), gv))
		} else {
			out = append(out, gv)
		}
	}
	return out, nil
}

// stripParamSigil removes a single leading "@"/":"/"$" from a real
// SqliteParameter.ParameterName, if present — see bindParams' own doc
// comment for why sql.Named needs the bare name.
func stripParamSigil(name string) string {
	if name == "" {
		return name
	}
	switch name[0] {
	case '@', ':', '$':
		return name[1:]
	default:
		return name
	}
}

func sqliteCommandExecuteReader(args []runtime.Value) (runtime.Value, error) {
	c, err := asSqliteCommand(args)
	if err != nil {
		return runtime.Value{}, err
	}
	goArgs, err := c.bindParams()
	if err != nil {
		return runtime.Value{}, err
	}
	target, err := c.target()
	if err != nil {
		return runtime.Value{}, err
	}
	rows, err := target.Query(c.text, goArgs...)
	if err != nil {
		return runtime.Value{}, sqliteManagedException(err)
	}
	cols, err := rows.Columns()
	if err != nil {
		rows.Close()
		return runtime.Value{}, sqliteManagedException(err)
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeSqliteDataReader{rows: rows, cols: cols}}), nil
}

func sqliteCommandExecuteNonQuery(args []runtime.Value) (runtime.Value, error) {
	c, err := asSqliteCommand(args)
	if err != nil {
		return runtime.Value{}, err
	}
	goArgs, err := c.bindParams()
	if err != nil {
		return runtime.Value{}, err
	}
	target, err := c.target()
	if err != nil {
		return runtime.Value{}, err
	}
	res, err := target.Exec(c.text, goArgs...)
	if err != nil {
		return runtime.Value{}, sqliteManagedException(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return runtime.Value{}, sqliteManagedException(err)
	}
	return runtime.Int32(int32(n)), nil
}

func sqliteCommandExecuteScalar(args []runtime.Value) (runtime.Value, error) {
	c, err := asSqliteCommand(args)
	if err != nil {
		return runtime.Value{}, err
	}
	goArgs, err := c.bindParams()
	if err != nil {
		return runtime.Value{}, err
	}
	target, err := c.target()
	if err != nil {
		return runtime.Value{}, err
	}
	rows, err := target.Query(c.text, goArgs...)
	if err != nil {
		return runtime.Value{}, sqliteManagedException(err)
	}
	defer rows.Close()
	if !rows.Next() {
		// Real ExecuteScalar returns a genuine C# null (not DBNull.Value)
		// when the result set is empty — DBNull.Value is only for a real
		// row whose first column itself holds SQL NULL (sqliteValueFromGo
		// handles that case for the len(dest)>0 path below).
		return runtime.Null(), nil
	}
	cols, err := rows.Columns()
	if err != nil {
		return runtime.Value{}, sqliteManagedException(err)
	}
	dest := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range dest {
		ptrs[i] = &dest[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return runtime.Value{}, sqliteManagedException(err)
	}
	if len(dest) == 0 {
		return runtime.Null(), nil
	}
	return sqliteValueFromGo(dest[0]), nil
}

// ---- SqliteParameter ----

func sqliteParameterCtor(args []runtime.Value) (*runtime.Object, error) {
	p := &nativeSqliteParameter{value: runtime.Null()}
	if len(args) > 0 && args[0].Kind == runtime.KindString {
		p.name = args[0].Str
	}
	if len(args) > 1 {
		p.value = derefReceiver(args[1])
	}
	return &runtime.Object{Native: p}, nil
}

func sqliteParameterOf(v runtime.Value) (*nativeSqliteParameter, bool) {
	v = derefReceiver(v)
	if v.Kind != runtime.KindObject || v.Obj == nil {
		return nil, false
	}
	p, ok := v.Obj.Native.(*nativeSqliteParameter)
	return p, ok
}

func asSqliteParameter(args []runtime.Value) (*nativeSqliteParameter, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("bcl: SqliteParameter method called without a receiver")
	}
	p, ok := sqliteParameterOf(args[0])
	if !ok {
		return nil, fmt.Errorf("bcl: SqliteParameter method receiver is not a SqliteParameter")
	}
	return p, nil
}

func sqliteParameterGetName(args []runtime.Value) (runtime.Value, error) {
	p, err := asSqliteParameter(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.String(p.name), nil
}

func sqliteParameterSetName(args []runtime.Value) (runtime.Value, error) {
	p, err := asSqliteParameter(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) > 1 && args[1].Kind == runtime.KindString {
		p.name = args[1].Str
	}
	return runtime.Value{}, nil
}

func sqliteParameterGetValue(args []runtime.Value) (runtime.Value, error) {
	p, err := asSqliteParameter(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return p.value, nil
}

func sqliteParameterSetValue(args []runtime.Value) (runtime.Value, error) {
	p, err := asSqliteParameter(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) > 1 {
		p.value = derefReceiver(args[1])
	}
	return runtime.Value{}, nil
}

func sqliteParameterGetDbType(args []runtime.Value) (runtime.Value, error) {
	p, err := asSqliteParameter(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(p.dbType), nil
}

func sqliteParameterSetDbType(args []runtime.Value) (runtime.Value, error) {
	p, err := asSqliteParameter(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) > 1 && args[1].Kind == runtime.KindI4 {
		p.dbType = args[1].I4
	}
	return runtime.Value{}, nil
}

func sqliteParameterGetSize(args []runtime.Value) (runtime.Value, error) {
	p, err := asSqliteParameter(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(p.size), nil
}

func sqliteParameterSetSize(args []runtime.Value) (runtime.Value, error) {
	p, err := asSqliteParameter(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) > 1 && args[1].Kind == runtime.KindI4 {
		p.size = args[1].I4
	}
	return runtime.Value{}, nil
}

func sqliteParameterGetDirection(args []runtime.Value) (runtime.Value, error) {
	p, err := asSqliteParameter(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(p.direction), nil
}

func sqliteParameterSetDirection(args []runtime.Value) (runtime.Value, error) {
	p, err := asSqliteParameter(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) > 1 && args[1].Kind == runtime.KindI4 {
		p.direction = args[1].I4
	}
	return runtime.Value{}, nil
}

func sqliteParameterGetIsNullable(args []runtime.Value) (runtime.Value, error) {
	return runtime.Bool(true), nil
}

// ---- SqliteParameterCollection ----

func newSqliteParameterCollectionValue() runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeSqliteParameterCollection{}})
}

func sqliteParamCollectionOf(v runtime.Value) (*nativeSqliteParameterCollection, bool) {
	v = derefReceiver(v)
	if v.Kind != runtime.KindObject || v.Obj == nil {
		return nil, false
	}
	pc, ok := v.Obj.Native.(*nativeSqliteParameterCollection)
	return pc, ok
}

func asSqliteParamCollection(args []runtime.Value) (*nativeSqliteParameterCollection, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("bcl: SqliteParameterCollection method called without a receiver")
	}
	pc, ok := sqliteParamCollectionOf(args[0])
	if !ok {
		return nil, fmt.Errorf("bcl: SqliteParameterCollection method receiver is not a SqliteParameterCollection")
	}
	return pc, nil
}

// sqliteParamsAdd backs the real, strongly-typed
// `int Add(SqliteParameter value)` overload (DbParameterCollection's own
// IList.Add(object) is satisfied by the same real member) — returns the
// new item's index, matching IList.Add's own real return contract.
func sqliteParamsAdd(args []runtime.Value) (runtime.Value, error) {
	pc, err := asSqliteParamCollection(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: SqliteParameterCollection.Add expects a SqliteParameter")
	}
	p, ok := sqliteParameterOf(args[1])
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: SqliteParameterCollection.Add expects a SqliteParameter")
	}
	pc.items = append(pc.items, p)
	return runtime.Int32(int32(len(pc.items) - 1)), nil
}

// sqliteParamsAddWithValue backs the real Microsoft.Data.Sqlite-specific
// convenience method `SqliteParameter AddWithValue(string parameterName,
// object value)` — builds and adds a new SqliteParameter itself, then
// hands it back (real code commonly chains straight off the return
// value, e.g. `cmd.Parameters.AddWithValue("@id", id).DbType = ...`).
func sqliteParamsAddWithValue(args []runtime.Value) (runtime.Value, error) {
	pc, err := asSqliteParamCollection(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 3 || args[1].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: SqliteParameterCollection.AddWithValue expects (string, object)")
	}
	p := &nativeSqliteParameter{name: args[1].Str, value: derefReceiver(args[2])}
	pc.items = append(pc.items, p)
	return runtime.ObjRef(&runtime.Object{Native: p}), nil
}

func sqliteParamsCount(args []runtime.Value) (runtime.Value, error) {
	pc, err := asSqliteParamCollection(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(int32(len(pc.items))), nil
}

// sqliteParamsGetItem backs both real indexer overloads — `this[int
// index]` and `this[string parameterName]` — telling them apart by the
// actual index argument's own Kind, the same approach every other
// multi-overload native in this package uses (dateTimeCtor's own doc
// comment).
func sqliteParamsGetItem(args []runtime.Value) (runtime.Value, error) {
	pc, err := asSqliteParamCollection(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: SqliteParameterCollection indexer expects an index")
	}
	idx := args[1]
	if idx.Kind == runtime.KindRef && idx.Ref != nil {
		idx = *idx.Ref
	}
	switch idx.Kind {
	case runtime.KindString:
		for _, p := range pc.items {
			if p.name == idx.Str {
				return runtime.ObjRef(&runtime.Object{Native: p}), nil
			}
		}
		return runtime.Value{}, nil
	case runtime.KindI4:
		i := int(idx.I4)
		if i < 0 || i >= len(pc.items) {
			return runtime.Value{}, &runtime.ManagedException{TypeName: "System.IndexOutOfRangeException", Message: "Index was outside the bounds of the collection."}
		}
		return runtime.ObjRef(&runtime.Object{Native: pc.items[i]}), nil
	default:
		return runtime.Value{}, fmt.Errorf("bcl: SqliteParameterCollection indexer expects an int or string")
	}
}

func sqliteParamsClear(args []runtime.Value) (runtime.Value, error) {
	pc, err := asSqliteParamCollection(args)
	if err != nil {
		return runtime.Value{}, err
	}
	pc.items = nil
	return runtime.Value{}, nil
}

func sqliteParamsContains(args []runtime.Value) (runtime.Value, error) {
	pc, err := asSqliteParamCollection(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) > 1 && args[1].Kind == runtime.KindString {
		for _, p := range pc.items {
			if p.name == args[1].Str {
				return runtime.Bool(true), nil
			}
		}
	}
	return runtime.Bool(false), nil
}

func sqliteParamsIndexOf(args []runtime.Value) (runtime.Value, error) {
	pc, err := asSqliteParamCollection(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) > 1 && args[1].Kind == runtime.KindString {
		for i, p := range pc.items {
			if p.name == args[1].Str {
				return runtime.Int32(int32(i)), nil
			}
		}
	}
	return runtime.Int32(-1), nil
}

// ---- value conversion: runtime.Value <-> Go's database/sql boundary ----

// sqliteGoValue converts a boxed CLR value (whatever a real
// SqliteParameter.Value setter received — an `object` in C#) to the
// plain Go value database/sql/driver.Value itself understands:
// nil/int64/float64/string/[]byte/bool/time.Time. This is genuinely the
// same handful of shapes every database/sql driver's own Valuer boundary
// accepts, not a vmnet-specific narrowing.
func sqliteGoValue(v runtime.Value) (any, error) {
	v = derefReceiver(v)
	switch v.Kind {
	case runtime.KindNull:
		return nil, nil
	case runtime.KindI4:
		return int64(v.I4), nil
	case runtime.KindI8:
		return v.I8, nil
	case runtime.KindR4:
		return float64(v.R4), nil
	case runtime.KindR8:
		return v.R8, nil
	case runtime.KindString:
		return v.Str, nil
	case runtime.KindBytes, runtime.KindArray:
		if b, ok := byteArrayArgToBytes(v); ok {
			return b, nil
		}
		return nil, fmt.Errorf("bcl: unsupported array parameter value for Sqlite (only byte[] is supported)")
	case runtime.KindStruct:
		if v.Struct != nil && v.Struct.Type == dateTimeType {
			t, _, err := asDateTime([]runtime.Value{v})
			if err != nil {
				return nil, err
			}
			return t, nil
		}
		return nil, fmt.Errorf("bcl: unsupported parameter value struct type for Sqlite")
	case runtime.KindObject:
		if v.Obj == nil {
			return nil, nil
		}
		if _, ok := v.Obj.Native.(*nativeDBNull); ok {
			return nil, nil
		}
		return nil, fmt.Errorf("bcl: unsupported parameter object value for Sqlite")
	default:
		return nil, fmt.Errorf("bcl: unsupported parameter value kind for Sqlite")
	}
}

// sqliteValueFromGo is sqliteGoValue's inverse: whatever raw Go value
// go-r2-sqlite's own driver handed back through database/sql's Rows.Scan
// (into a plain `any` destination, so no ambient type coercion happens —
// see sqliteReaderRead) becomes the boxed CLR `object` a real
// IDataRecord.GetValue caller expects, including DBNull.Value (never a
// plain vmnet KindNull) for a genuine SQL NULL — see system_dbnull.go's
// own doc comment for why that distinction matters to real ADO.NET
// callers like Dapper's SqlMapper.
func sqliteValueFromGo(v any) runtime.Value {
	switch t := v.(type) {
	case nil:
		return DBNullValue()
	case int64:
		return runtime.Int64(t)
	case float64:
		return runtime.Float64(t)
	case string:
		return runtime.String(t)
	case []byte:
		return runtime.Bytes(t)
	case bool:
		return runtime.Bool(t)
	case time.Time:
		return dateTimeFromTime(t)
	default:
		return runtime.String(fmt.Sprintf("%v", t))
	}
}

// sqliteFieldTypeFullName names the real CLR type a scanned Go value
// corresponds to, for GetFieldType(i)/GetDataTypeName(i) — "System.Object"
// when the current row's own value is null, matching examples/dapper-
// demo's own FakeReader.GetFieldType fallback for the same "don't know
// yet" case (real SQLite is dynamically typed per-ROW, not per-column, so
// there is no better answer available without the column's own declared
// type affinity, which go-r2-sqlite's driver doesn't surface here).
func sqliteFieldTypeFullName(v any) string {
	switch v.(type) {
	case int64:
		return "System.Int64"
	case float64:
		return "System.Double"
	case string:
		return "System.String"
	case []byte:
		return "System.Byte[]"
	case bool:
		return "System.Boolean"
	case time.Time:
		return "System.DateTime"
	default:
		return "System.Object"
	}
}

// ---- SqliteDataReader ----

func sqliteReaderOf(v runtime.Value) (*nativeSqliteDataReader, bool) {
	v = derefReceiver(v)
	if v.Kind != runtime.KindObject || v.Obj == nil {
		return nil, false
	}
	r, ok := v.Obj.Native.(*nativeSqliteDataReader)
	return r, ok
}

func asSqliteReader(args []runtime.Value) (*nativeSqliteDataReader, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("bcl: SqliteDataReader method called without a receiver")
	}
	r, ok := sqliteReaderOf(args[0])
	if !ok {
		return nil, fmt.Errorf("bcl: SqliteDataReader method receiver is not a SqliteDataReader")
	}
	return r, nil
}

func sqliteReaderRead(args []runtime.Value) (runtime.Value, error) {
	r, err := asSqliteReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if r.closed || !r.rows.Next() {
		return runtime.Bool(false), nil
	}
	dest := make([]any, len(r.cols))
	ptrs := make([]any, len(r.cols))
	for i := range dest {
		ptrs[i] = &dest[i]
	}
	if err := r.rows.Scan(ptrs...); err != nil {
		return runtime.Value{}, sqliteManagedException(err)
	}
	r.current = dest
	return runtime.Bool(true), nil
}

// NextResult always answers false: this provider (like go-r2-sqlite's own
// driver) only ever executes one statement per Query call, so there is
// never a second result set to advance into — matches examples/dapper-
// demo's own FakeReader.NextResult, which has the same single-result-set
// shape for the same reason.
func sqliteReaderNextResult(args []runtime.Value) (runtime.Value, error) {
	if _, err := asSqliteReader(args); err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(false), nil
}

func sqliteReaderFieldCount(args []runtime.Value) (runtime.Value, error) {
	r, err := asSqliteReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(int32(len(r.cols))), nil
}

func sqliteReaderRecordsAffected(args []runtime.Value) (runtime.Value, error) {
	if _, err := asSqliteReader(args); err != nil {
		return runtime.Value{}, err
	}
	// Real DbDataReader.RecordsAffected is -1 for a SELECT (the only
	// statement shape ExecuteReader ever runs here) — matches
	// FakeReader.RecordsAffected exactly.
	return runtime.Int32(-1), nil
}

func sqliteReaderDepth(args []runtime.Value) (runtime.Value, error) {
	if _, err := asSqliteReader(args); err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(0), nil
}

func sqliteReaderIsClosed(args []runtime.Value) (runtime.Value, error) {
	r, err := asSqliteReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(r.closed), nil
}

func sqliteReaderHasRows(args []runtime.Value) (runtime.Value, error) {
	r, err := asSqliteReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(r.current != nil), nil
}

func sqliteReaderGetName(args []runtime.Value) (runtime.Value, error) {
	r, err := asSqliteReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	i, err := intArg(args, 1, "SqliteDataReader.GetName")
	if err != nil {
		return runtime.Value{}, err
	}
	if i < 0 || i >= len(r.cols) {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.IndexOutOfRangeException", Message: "Index was outside the bounds of the collection."}
	}
	return runtime.String(r.cols[i]), nil
}

func sqliteReaderGetOrdinal(args []runtime.Value) (runtime.Value, error) {
	r, err := asSqliteReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 || args[1].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: SqliteDataReader.GetOrdinal expects a string")
	}
	for i, name := range r.cols {
		if name == args[1].Str {
			return runtime.Int32(int32(i)), nil
		}
	}
	return runtime.Value{}, &runtime.ManagedException{TypeName: "System.IndexOutOfRangeException", Message: fmt.Sprintf("%s is not among the result columns.", args[1].Str)}
}

// intArg reads args[idx] as a plain int32 index argument (deref'd through
// a managed pointer first, same as every receiver above) — shared by
// every GetXxx(int i) native below.
func intArg(args []runtime.Value, idx int, method string) (int, error) {
	if len(args) <= idx {
		return 0, fmt.Errorf("bcl: %s expects an int argument", method)
	}
	v := args[idx]
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	if v.Kind != runtime.KindI4 {
		return 0, fmt.Errorf("bcl: %s expects an int argument", method)
	}
	return int(v.I4), nil
}

func sqliteReaderCurrent(args []runtime.Value, method string) (any, error) {
	r, err := asSqliteReader(args)
	if err != nil {
		return nil, err
	}
	i, err := intArg(args, 1, method)
	if err != nil {
		return nil, err
	}
	if r.current == nil {
		return nil, fmt.Errorf("bcl: %s: no current row (call Read() first)", method)
	}
	if i < 0 || i >= len(r.current) {
		return nil, &runtime.ManagedException{TypeName: "System.IndexOutOfRangeException", Message: "Index was outside the bounds of the collection."}
	}
	return r.current[i], nil
}

// sqliteReaderCurrentLenient is sqliteReaderCurrent's schema-only sibling
// for GetFieldType/GetDataTypeName: real ADO.NET readers answer these
// from column METADATA, available as soon as the reader is open,
// independent of cursor position — unlike GetValue/GetInt32/..., which
// need an actual row. Found via a real, load-bearing case: Dapper's own
// SqlMapper (SqlMapper.GetDapperRowDeserializer, the "typeof(object)" row
// path examples/dapper-demo and examples/sqlite-demo both use) calls
// GetFieldType(i) for every column before this reader's own current row
// state is necessarily populated. Answers ("System.Object", true) rather
// than erroring when there's genuinely no row yet or the index is past
// what was actually scanned — matches examples/dapper-demo's own
// FakeReader.GetFieldType falling back to typeof(object) for the exact
// same "don't know yet" case.
func sqliteReaderCurrentLenient(args []runtime.Value, method string) (any, error) {
	r, err := asSqliteReader(args)
	if err != nil {
		return nil, err
	}
	i, err := intArg(args, 1, method)
	if err != nil {
		return nil, err
	}
	if i < 0 || i >= len(r.cols) {
		return nil, &runtime.ManagedException{TypeName: "System.IndexOutOfRangeException", Message: "Index was outside the bounds of the collection."}
	}
	if r.current == nil || i >= len(r.current) {
		return nil, nil
	}
	return r.current[i], nil
}

func sqliteReaderGetValue(args []runtime.Value) (runtime.Value, error) {
	v, err := sqliteReaderCurrent(args, "SqliteDataReader.GetValue")
	if err != nil {
		return runtime.Value{}, err
	}
	return sqliteValueFromGo(v), nil
}

func sqliteReaderGetItem(args []runtime.Value) (runtime.Value, error) {
	if _, err := asSqliteReader(args); err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: SqliteDataReader indexer expects an argument")
	}
	idx := args[1]
	if idx.Kind == runtime.KindRef && idx.Ref != nil {
		idx = *idx.Ref
	}
	if idx.Kind == runtime.KindString {
		ord, err := sqliteReaderGetOrdinal(args)
		if err != nil {
			return runtime.Value{}, err
		}
		return sqliteReaderGetValue([]runtime.Value{args[0], ord})
	}
	return sqliteReaderGetValue(args)
}

func sqliteReaderGetValues(args []runtime.Value) (runtime.Value, error) {
	r, err := asSqliteReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 || args[1].Kind != runtime.KindArray || args[1].Arr == nil {
		return runtime.Value{}, fmt.Errorf("bcl: SqliteDataReader.GetValues expects an object[]")
	}
	if r.current == nil {
		return runtime.Value{}, fmt.Errorf("bcl: SqliteDataReader.GetValues: no current row (call Read() first)")
	}
	dest := args[1].Arr.Elems
	n := len(r.current)
	if len(dest) < n {
		n = len(dest)
	}
	for i := 0; i < n; i++ {
		dest[i] = sqliteValueFromGo(r.current[i])
	}
	return runtime.Int32(int32(n)), nil
}

func sqliteReaderGetFieldType(args []runtime.Value) (runtime.Value, error) {
	v, err := sqliteReaderCurrentLenient(args, "SqliteDataReader.GetFieldType")
	if err != nil {
		return runtime.Value{}, err
	}
	return NewTypeValue(sqliteFieldTypeFullName(v)), nil
}

func sqliteReaderGetDataTypeName(args []runtime.Value) (runtime.Value, error) {
	v, err := sqliteReaderCurrentLenient(args, "SqliteDataReader.GetDataTypeName")
	if err != nil {
		return runtime.Value{}, err
	}
	switch v.(type) {
	case int64, bool:
		return runtime.String("INTEGER"), nil
	case float64:
		return runtime.String("REAL"), nil
	case []byte:
		return runtime.String("BLOB"), nil
	case time.Time:
		return runtime.String("TEXT"), nil
	case string:
		return runtime.String("TEXT"), nil
	default:
		return runtime.String(""), nil
	}
}

func sqliteReaderIsDBNull(args []runtime.Value) (runtime.Value, error) {
	v, err := sqliteReaderCurrent(args, "SqliteDataReader.IsDBNull")
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(v == nil), nil
}

// asSqliteInt64/asSqliteFloat64/asSqliteString below convert whichever Go
// type the driver actually scanned a column as into the specific numeric/
// string shape a real GetInt32/GetDouble/GetString/... caller asked for —
// real Microsoft.Data.Sqlite performs the same kind of narrowing
// conversion across SQLite's own dynamically-typed storage classes
// (a column declared INTEGER can still legally store a REAL, etc.).
func sqliteAsInt64(v any) (int64, bool) {
	switch t := v.(type) {
	case int64:
		return t, true
	case float64:
		return int64(t), true
	case bool:
		if t {
			return 1, true
		}
		return 0, true
	case string:
		var n int64
		if _, err := fmt.Sscanf(t, "%d", &n); err == nil {
			return n, true
		}
	}
	return 0, false
}

func sqliteAsFloat64(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int64:
		return float64(t), true
	case string:
		var f float64
		if _, err := fmt.Sscanf(t, "%g", &f); err == nil {
			return f, true
		}
	}
	return 0, false
}

func sqliteGetterInt(args []runtime.Value, method string) (runtime.Value, error) {
	v, err := sqliteReaderCurrent(args, method)
	if err != nil {
		return runtime.Value{}, err
	}
	n, ok := sqliteAsInt64(v)
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: %s: column value %v is not numeric", method, v)
	}
	return runtime.Int64(n), nil
}

func sqliteReaderGetBoolean(args []runtime.Value) (runtime.Value, error) {
	v, err := sqliteReaderCurrent(args, "SqliteDataReader.GetBoolean")
	if err != nil {
		return runtime.Value{}, err
	}
	if b, ok := v.(bool); ok {
		return runtime.Bool(b), nil
	}
	n, ok := sqliteAsInt64(v)
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: SqliteDataReader.GetBoolean: column value %v is not boolean", v)
	}
	return runtime.Bool(n != 0), nil
}

func sqliteReaderGetByte(args []runtime.Value) (runtime.Value, error) {
	return sqliteGetterInt(args, "SqliteDataReader.GetByte")
}

func sqliteReaderGetChar(args []runtime.Value) (runtime.Value, error) {
	v, err := sqliteReaderCurrent(args, "SqliteDataReader.GetChar")
	if err != nil {
		return runtime.Value{}, err
	}
	if s, ok := v.(string); ok && len(s) > 0 {
		return runtime.Int32(int32(s[0])), nil
	}
	return runtime.Value{}, fmt.Errorf("bcl: SqliteDataReader.GetChar: column value %v is not a single character", v)
}

func sqliteReaderGetInt16(args []runtime.Value) (runtime.Value, error) {
	v, err := sqliteGetterInt(args, "SqliteDataReader.GetInt16")
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(int32(int16(v.I8))), nil
}

func sqliteReaderGetInt32(args []runtime.Value) (runtime.Value, error) {
	v, err := sqliteGetterInt(args, "SqliteDataReader.GetInt32")
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(int32(v.I8)), nil
}

func sqliteReaderGetInt64(args []runtime.Value) (runtime.Value, error) {
	return sqliteGetterInt(args, "SqliteDataReader.GetInt64")
}

func sqliteReaderGetFloat(args []runtime.Value) (runtime.Value, error) {
	v, err := sqliteReaderCurrent(args, "SqliteDataReader.GetFloat")
	if err != nil {
		return runtime.Value{}, err
	}
	f, ok := sqliteAsFloat64(v)
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: SqliteDataReader.GetFloat: column value %v is not numeric", v)
	}
	return runtime.Float32(float32(f)), nil
}

func sqliteReaderGetDouble(args []runtime.Value) (runtime.Value, error) {
	v, err := sqliteReaderCurrent(args, "SqliteDataReader.GetDouble")
	if err != nil {
		return runtime.Value{}, err
	}
	f, ok := sqliteAsFloat64(v)
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: SqliteDataReader.GetDouble: column value %v is not numeric", v)
	}
	return runtime.Float64(f), nil
}

func sqliteReaderGetString(args []runtime.Value) (runtime.Value, error) {
	v, err := sqliteReaderCurrent(args, "SqliteDataReader.GetString")
	if err != nil {
		return runtime.Value{}, err
	}
	if s, ok := v.(string); ok {
		return runtime.String(s), nil
	}
	return runtime.String(fmt.Sprintf("%v", v)), nil
}

func sqliteReaderGetDateTime(args []runtime.Value) (runtime.Value, error) {
	v, err := sqliteReaderCurrent(args, "SqliteDataReader.GetDateTime")
	if err != nil {
		return runtime.Value{}, err
	}
	switch t := v.(type) {
	case time.Time:
		return dateTimeFromTime(t), nil
	case string:
		// SQLite has no native datetime storage class — a value written
		// through convertValue (go-r2-sqlite's own driver.go) as a plain
		// TEXT column round-trips back as a string; real Microsoft.Data.
		// Sqlite parses it the same layout its own writer used.
		for _, layout := range []string{"2006-01-02 15:04:05.999999999", time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
			if parsed, err := time.Parse(layout, t); err == nil {
				return dateTimeFromTime(parsed), nil
			}
		}
		return runtime.Value{}, fmt.Errorf("bcl: SqliteDataReader.GetDateTime: %q is not a recognized date/time format", t)
	default:
		return runtime.Value{}, fmt.Errorf("bcl: SqliteDataReader.GetDateTime: column value %v is not a date/time", v)
	}
}

// GetData(i) real semantics: returns a NESTED IDataReader for a
// hierarchical result (SQL Server's own FOR XML/nested cursors, mostly) —
// SQLite has no such feature, so, matching examples/dapper-demo's own
// FakeReader.GetData, this just returns the same reader back.
func sqliteReaderGetData(args []runtime.Value) (runtime.Value, error) {
	if _, err := asSqliteReader(args); err != nil {
		return runtime.Value{}, err
	}
	return args[0], nil
}

func sqliteReaderGetSchemaTable(args []runtime.Value) (runtime.Value, error) {
	if _, err := asSqliteReader(args); err != nil {
		return runtime.Value{}, err
	}
	return runtime.Null(), nil
}

// GetBytes/GetChars (streaming a BLOB/CLOB into a caller-provided buffer,
// returning how many bytes/chars were copied) always return 0, matching
// examples/dapper-demo's own FakeReader stub — nothing in this project's
// own fixtures streams a BLOB this way (GetValue already returns a whole
// byte[] directly for a BLOB column, the path real code overwhelmingly
// uses in practice).
func sqliteReaderGetBytesStub(args []runtime.Value) (runtime.Value, error) {
	if _, err := asSqliteReader(args); err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int64(0), nil
}

func sqliteReaderDispose(args []runtime.Value) (runtime.Value, error) {
	r, err := asSqliteReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if !r.closed {
		r.rows.Close()
		r.closed = true
	}
	return runtime.Value{}, nil
}
