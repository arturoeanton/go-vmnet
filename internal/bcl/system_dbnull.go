package bcl

import "github.com/arturoeanton/go-vmnet/internal/runtime"

// nativeDBNull backs System.DBNull — a real BCL singleton sentinel used
// throughout ADO.NET's own object-typed getters (IDataRecord.GetValue,
// DbParameter.Value's "no value" case, ...) to represent "this column/
// parameter genuinely holds SQL NULL," a different concept from vmnet's
// own KindNull (a C# `null` reference). DBNull.Value is never itself
// null: real code commonly branches on `is DBNull` (a real Dapper
// SqlMapper pattern, checked before falling back to an actual `null`)
// or a direct `== DBNull.Value` reference comparison, neither of which
// KindNull could ever satisfy. Added for the real, Go-native SQLite ADO.NET
// provider (system_data_sqlite.go) — the first thing in this codebase
// that can observe a genuine SQL NULL coming back from a real row, rather
// than just a C# `null` a plugin's own code produced.
type nativeDBNull struct{}

// dbNullValue is the one and only DBNull instance every native that needs
// to report SQL NULL as a boxed `object` hands back — real DBNull.Value is
// itself a process-wide singleton (a private static readonly field on a
// sealed class with no public constructor), so sharing this one instance
// keeps a direct `== DBNull.Value` reference-equality check working, not
// just an `is DBNull` type-pattern check (which only needs NativeTypeName
// below to match, and would work even with a freshly allocated instance
// per call).
var dbNullValue = runtime.ObjRef(&runtime.Object{Native: &nativeDBNull{}})

// DBNullValue exports dbNullValue for other packages' natives (only
// internal/interpreter's ADO.NET glue today, if any) that need to report a
// SQL NULL as a boxed `object` using this exact same shared instance.
func DBNullValue() runtime.Value { return dbNullValue }

// dbNullStaticsType backs `DBNull.Value` itself (`ldsfld System.DBNull::
// Value`) — see LookupStaticFieldHost's own doc comment for why a
// reference-shaped BCL type with static fields needs this registry
// instead of valueTypeRegistry.
var dbNullStaticsType = runtime.NewType("System", "DBNull", nil, []string{"Value"}, nil, []runtime.Value{dbNullValue})

func init() {
	registerStaticFieldHost(dbNullStaticsType)
	register("System.DBNull::ToString", true, dbNullToString)
	register("System.DBNull::GetType", true, dbNullGetType)
}

// dbNullToString matches real DBNull.ToString(): always String.Empty,
// regardless of any IFormatProvider argument real overloads accept.
func dbNullToString(args []runtime.Value) (runtime.Value, error) {
	return runtime.String(""), nil
}

func dbNullGetType(args []runtime.Value) (runtime.Value, error) {
	return NewTypeValue("System.DBNull"), nil
}
