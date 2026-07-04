package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// System.Data.Common.DbDataReader/DbCommand/DbConnection are real
// abstract BASE CLASSES (bcl/system_data.go's own doc comment) — vmnet
// registers a no-op base-ctor chain for them there, but their PUBLIC,
// CONCRETE (non-abstract) IDisposable.Dispose() is a different shape of
// gap: real DbDataReader.Dispose() is `public void Dispose() {
// Dispose(true); GC.SuppressFinalize(this); }`, calling the PROTECTED
// VIRTUAL Dispose(bool disposing) a concrete subclass typically
// overrides to release its own resources — e.g. Dapper's own internal
// WrappedBasicReader (a real TypeDef inside Dapper.dll, wrapping
// whatever plain IDataReader a caller's own IDbCommand.ExecuteReader
// returned) overrides Dispose(bool) to forward to its wrapped reader.
// Since vmnet has no TypeDef for DbDataReader itself to declare this
// base Dispose() on, calling it on any subclass that doesn't override
// the public zero-arg Dispose() itself (the overwhelmingly common
// case — real code always overrides Dispose(bool), never the public
// Dispose()) falls through Machine.call's ancestor walk all the way to
// "no native registered". Found via a real, load-bearing case: Dapper's
// own QueryImpl disposes its WrappedBasicReader this exact way at the
// end of every real query.
func init() {
	machineRegistry["System.Data.Common.DbDataReader::Dispose"] = dbDataReaderDispose
	machineRegistry["System.Data.Common.DbCommand::Dispose"] = dbDataReaderDispose
	machineRegistry["System.Data.Common.DbConnection::Dispose"] = dbDataReaderDispose
}

// dbDataReaderDispose backs the public, concrete Dispose() every real
// DbDataReader/DbCommand/DbConnection subclass inherits — tries the
// receiver's own Dispose(bool disposing) override directly (arity
// distinguishes it from this same public Dispose() by real argument
// count, exactly like any other real overload resolution in this
// project), silently doing nothing if no such override exists (matching
// the real base class's own Dispose(bool) being an empty virtual method
// with nothing to release). Uses Machine.tryCall directly rather than
// Machine.call: this IS already the concrete receiver (no further
// ancestor walk needed), and a virtual call back through this same
// target name the instant no override exists would recurse into this
// native forever.
func dbDataReaderDispose(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: Dispose expects a receiver")
	}
	concrete, ok := receiverTypeName(args[0])
	if !ok {
		return runtime.Value{}, nil
	}
	_, _, err, _, _ := m.tryCall(concrete+"::Dispose", []runtime.Value{args[0], runtime.Bool(true)}, depth, instrCount, nil, nil)
	return runtime.Value{}, err
}
