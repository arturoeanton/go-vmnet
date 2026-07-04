// Minimal fake ADO.NET provider (IDbConnection/IDbCommand/IDataReader/
// IDataParameter/IDataParameterCollection) exercising exactly the real
// System.Data interface surface Dapper's own SqlMapper.Query/Execute call
// through — no real database engine, just enough of the real ADO.NET
// contract for Dapper's real column-to-object mapping code to run
// against, entirely in memory. vmnet's own virtual-dispatch ancestor walk
// (internal/interpreter/calls.go) resolves every one of these interface
// members straight through to this class's own real method — the same
// mechanism IEnumerable`1/IEqualityComparer`1 already rely on for any
// other interface-typed call site — so none of this needs vmnet-specific
// glue beyond implementing the real .NET interfaces correctly.
using System;
using System.Collections.Generic;
using System.Data;
using Dapper;

namespace DapperDemo
{
    public class FakeParameter : IDbDataParameter
    {
        public DbType DbType { get; set; }
        public ParameterDirection Direction { get; set; }
        public bool IsNullable => true;
        public string ParameterName { get; set; } = "";
        public string SourceColumn { get; set; } = "";
        public DataRowVersion SourceVersion { get; set; }
        public object Value { get; set; }
        public byte Precision { get; set; }
        public byte Scale { get; set; }
        public int Size { get; set; }
    }

    // IDataParameterCollection extends IList, so a plain List<T> already
    // supplies almost everything real code here actually needs (Add,
    // get_Item(int), Count, ...) — only the string-keyed members are
    // genuinely new.
    public class FakeParameterCollection : List<FakeParameter>, IDataParameterCollection
    {
        public object this[string name]
        {
            get => Find(p => p.ParameterName == name);
            set { }
        }
        public bool Contains(string name) => Exists(p => p.ParameterName == name);
        public int IndexOf(string name) => FindIndex(p => p.ParameterName == name);
        public void RemoveAt(string name) => RemoveAll(p => p.ParameterName == name);
    }

    // A forward-only, in-memory row cursor over a fixed object[] table —
    // real ADO.NET semantics (Read() advances before the first row is
    // visible; GetOrdinal/GetName round-trip through the same column
    // name array).
    public class FakeReader : IDataReader
    {
        private readonly string[] names;
        private readonly List<object[]> rows;
        private int idx = -1;

        public FakeReader(string[] names, List<object[]> rows)
        {
            this.names = names;
            this.rows = rows;
        }

        public object this[int i] => rows[idx][i];
        public object this[string name] => rows[idx][GetOrdinal(name)];
        public int Depth => 0;
        public bool IsClosed { get; private set; }
        public int RecordsAffected => -1;
        public int FieldCount => names.Length;
        public void Close() => IsClosed = true;
        public void Dispose() => IsClosed = true;
        public bool NextResult() => false;
        public bool Read() { idx++; return idx < rows.Count; }
        public bool GetBoolean(int i) => (bool)rows[idx][i];
        public byte GetByte(int i) => (byte)rows[idx][i];
        public long GetBytes(int i, long fieldOffset, byte[] buffer, int bufferoffset, int length) => 0;
        public char GetChar(int i) => (char)rows[idx][i];
        public long GetChars(int i, long fieldoffset, char[] buffer, int bufferoffset, int length) => 0;
        public IDataReader GetData(int i) => this;
        public string GetDataTypeName(int i) => GetFieldType(i).Name;
        public DateTime GetDateTime(int i) => (DateTime)rows[idx][i];
        public decimal GetDecimal(int i) => Convert.ToDecimal(rows[idx][i]);
        public double GetDouble(int i) => (double)rows[idx][i];
        public Type GetFieldType(int i) => rows[0][i] != null ? rows[0][i].GetType() : typeof(object);
        public float GetFloat(int i) => (float)rows[idx][i];
        public Guid GetGuid(int i) => (Guid)rows[idx][i];
        public short GetInt16(int i) => (short)rows[idx][i];
        public int GetInt32(int i) => (int)rows[idx][i];
        public long GetInt64(int i) => (long)rows[idx][i];
        public string GetName(int i) => names[i];
        public int GetOrdinal(string name) => Array.IndexOf(names, name);
        public DataTable GetSchemaTable() => null;
        public string GetString(int i) => (string)rows[idx][i];
        public object GetValue(int i) => rows[idx][i] ?? DBNull.Value;
        public int GetValues(object[] values)
        {
            var row = rows[idx];
            int n = Math.Min(row.Length, values.Length);
            for (int i = 0; i < n; i++) values[i] = row[i] ?? DBNull.Value;
            return n;
        }
        public bool IsDBNull(int i) => rows[idx][i] == null;
    }

    public class FakeCommand : IDbCommand
    {
        private readonly FakeConnection conn;
        public FakeCommand(FakeConnection conn) { this.conn = conn; Connection = conn; }
        public string CommandText { get; set; } = "";
        public int CommandTimeout { get; set; }
        public CommandType CommandType { get; set; } = CommandType.Text;
        public IDbConnection Connection { get; set; }
        public IDataParameterCollection Parameters { get; } = new FakeParameterCollection();
        public IDbTransaction Transaction { get; set; }
        public UpdateRowSource UpdatedRowSource { get; set; }
        public void Cancel() { }
        public IDbDataParameter CreateParameter() => new FakeParameter();
        public void Dispose() { }
        public void Prepare() { }

        public int ExecuteNonQuery() => conn.RunDelete(CommandText);

        public IDataReader ExecuteReader() => ExecuteReader(CommandBehavior.Default);
        public IDataReader ExecuteReader(CommandBehavior behavior) => conn.RunQuery(CommandText);
        public object ExecuteScalar()
        {
            var reader = ExecuteReader();
            return reader.Read() ? reader.GetValue(0) : null;
        }
    }

    public class FakeConnection : IDbConnection
    {
        public string ConnectionString { get; set; } = "";
        public int ConnectionTimeout => 30;
        public string Database => "vmnet-demo";
        public ConnectionState State { get; private set; } = ConnectionState.Closed;
        public void Open() => State = ConnectionState.Open;
        public void Close() => State = ConnectionState.Closed;
        public void ChangeDatabase(string databaseName) { }
        public IDbCommand CreateCommand() => new FakeCommand(this);
        public IDbTransaction BeginTransaction() => throw new NotSupportedException("vmnet dapper-demo: transactions aren't modeled");
        public IDbTransaction BeginTransaction(IsolationLevel il) => BeginTransaction();
        public void Dispose() { }

        private readonly List<object[]> people = new List<object[]>
        {
            new object[] { 1, "Ada Lovelace", 36 },
            new object[] { 2, "Grace Hopper", 85 },
            new object[] { 3, "Alan Turing", 41 },
        };

        internal IDataReader RunQuery(string sql)
        {
            var names = new[] { "Id", "Name", "Age" };
            return new FakeReader(names, people);
        }

        // Toy "DELETE FROM Person WHERE Id = N" support — parses the
        // literal N out of CommandText itself (this fake provider's own
        // code, not Dapper's). Deliberately not real parameter binding:
        // see README.md's "what doesn't work" section — Dapper's real
        // parameter-binding path always scans the SQL text for a
        // `{=name}` literal-replacement token first, via a regex using
        // negative lookbehind that Go's RE2-based regexp engine can never
        // compile, so any Dapper call that supplies an actual parameters
        // object (of any shape — anonymous type, DynamicParameters, a
        // plain dictionary) hits that wall unconditionally. Every
        // operation below only ever passes literal SQL with no
        // parameters object at all, which skips that code path entirely.
        internal int RunDelete(string sql)
        {
            int marker = sql.LastIndexOf('=');
            if (marker < 0 || !int.TryParse(sql.Substring(marker + 1).Trim(), out int id))
            {
                return 0;
            }
            return people.RemoveAll(row => (int)row[0] == id);
        }
    }

    // Thin, deliberately non-generic entry points — Dapper's own
    // Query<T>()/Query() extension methods internally do `typeof(T)`
    // where T is the CALLING method's own generic type parameter, which
    // vmnet cannot resolve (ir.LoadTypeToken's own IsMethodGenericParam
    // case pushes an empty-name Type — a documented limitation, see
    // README.md). Using the non-generic Query(Type, string, ...) overload
    // instead sidesteps that: `typeof(object)` here is an ordinary,
    // always-resolvable ldtoken in OUR OWN compiled code, and passing it
    // as a real Type ARGUMENT (not a generic method parameter) makes
    // Dapper's internal GetDeserializer take its "type == typeof(object)"
    // branch — the real, non-Reflection.Emit DapperRow row mapper, not
    // the DynamicMethod-based compiled-per-type deserializer its generic
    // Query<T> path always builds (vmnet has no System.Reflection.Emit
    // support at all, by design — see docs/en/ROADMAP.md).
    public static class DapperDemoRunner
    {
        public static List<Dictionary<string, object>> Query(IDbConnection conn, string sql)
        {
            var result = new List<Dictionary<string, object>>();
            foreach (object row in SqlMapper.Query(conn, typeof(object), sql))
            {
                // DapperRow implements IDictionary<string,object> via an
                // EXPLICIT interface implementation (a real, mangled
                // MethodImpl, not a plain "get_Item" method) — copying
                // into an ordinary Dictionary<string,object> here, inside
                // real compiled IL that already knows it's talking to
                // IDictionary<string,object>, is what lets vmnet's own
                // explicit-interface-impl resolution (calls.go's
                // ResolveExplicitImpl) find it at all; the host Go side
                // only ever sees a plain Dictionary afterwards.
                var dict = new Dictionary<string, object>();
                foreach (KeyValuePair<string, object> kv in (IDictionary<string, object>)row)
                {
                    dict[kv.Key] = kv.Value;
                }
                result.Add(dict);
            }
            return result;
        }

        public static int Execute(IDbConnection conn, string sql)
        {
            return SqlMapper.Execute(conn, sql);
        }
    }
}
