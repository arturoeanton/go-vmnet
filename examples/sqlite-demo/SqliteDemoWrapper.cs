// Real, hand-written ADO.NET code — `using Microsoft.Data.Sqlite;`,
// `new SqliteConnection(...)`, real @name and positional `?` parameter
// binding, a real transaction — run unmodified against vmnet's own
// native, Go-backed Microsoft.Data.Sqlite provider (internal/bcl/
// system_data_sqlite.go, Fase 3.53). This file is compiled with a real
// PackageReference to the ACTUAL Microsoft.Data.Sqlite NuGet package
// (SqliteDemoWrapper.csproj) purely so `dotnet build` has a real type to
// check this source against — the real package's own DLL is never
// loaded into vmnet at runtime (main.go only attaches Dapper.dll as a
// dependency), so every SqliteConnection/SqliteCommand/... call below
// resolves straight through to vmnet's own native implementation. This
// is the whole point: the exact same source file would also compile AND
// run correctly against the real Microsoft.Data.Sqlite on a real CLR,
// with zero changes.
using System;
using System.Collections.Generic;
using System.Data;
using Microsoft.Data.Sqlite;
using Dapper;

namespace SqliteDemo
{
    public static class SqliteDemoRunner
    {
        public static void Setup(SqliteConnection conn)
        {
            var cmd = conn.CreateCommand();
            cmd.CommandText = "CREATE TABLE Person (Id INTEGER PRIMARY KEY, Name TEXT NOT NULL, Age INTEGER NOT NULL)";
            cmd.ExecuteNonQuery();
        }

        // Real @name parameter binding, entirely independent of Dapper:
        // SqliteCommand.CreateParameter() + SqliteParameterCollection.Add()
        // + SqliteParameter.ParameterName/Value, exactly like real ADO.NET
        // code anywhere else. Dapper's own {=name}-literal-token regex
        // (see examples/dapper-demo's own doc comment for why it can never
        // even compile under Go's RE2 regexp engine) is nowhere near this
        // path — real parameter binding through vmnet works fine, it's
        // only DAPPER'S OWN parameter-object scanning that's unusable.
        public static void InsertPerson(SqliteConnection conn, long id, string name, int age)
        {
            var cmd = conn.CreateCommand();
            cmd.CommandText = "INSERT INTO Person (Id, Name, Age) VALUES (@id, @name, @age)";
            var pId = cmd.CreateParameter();
            pId.ParameterName = "@id";
            pId.Value = id;
            cmd.Parameters.Add(pId);
            var pName = cmd.CreateParameter();
            pName.ParameterName = "@name";
            pName.Value = name;
            cmd.Parameters.Add(pName);
            var pAge = cmd.CreateParameter();
            pAge.ParameterName = "@age";
            pAge.Value = age;
            cmd.Parameters.Add(pAge);
            cmd.ExecuteNonQuery();
        }

        // Positional `?` binding (a second real parameter-binding shape,
        // distinct from InsertPerson's own @name style above), inside a
        // real transaction committed only at the very end — exercises
        // SqliteConnection.BeginTransaction(), SqliteCommand.Transaction,
        // and SqliteTransaction.Commit(), all backed by a real Go sql.Tx
        // (system_data_sqlite.go). The row data is fixed here rather than
        // passed in from Go: vmnet's public host API (value.go) has no
        // general int[]/string[] argument constructor yet (only ByteArray
        // for byte[]), so a hand-written C# entry point with its own
        // literal data is simpler than marshaling one across that boundary
        // for what a demo needs.
        public static void InsertPeopleInTransaction(SqliteConnection conn)
        {
            long[] ids = { 4, 5 };
            string[] names = { "Katherine Johnson", "Margaret Hamilton" };
            int[] ages = { 33, 34 };
            IDbTransaction tx = conn.BeginTransaction();
            for (int i = 0; i < ids.Length; i++)
            {
                var cmd = conn.CreateCommand();
                cmd.Transaction = (SqliteTransaction)tx;
                cmd.CommandText = "INSERT INTO Person (Id, Name, Age) VALUES (?, ?, ?)";
                var p1 = cmd.CreateParameter(); p1.Value = ids[i]; cmd.Parameters.Add(p1);
                var p2 = cmd.CreateParameter(); p2.Value = names[i]; cmd.Parameters.Add(p2);
                var p3 = cmd.CreateParameter(); p3.Value = ages[i]; cmd.Parameters.Add(p3);
                cmd.ExecuteNonQuery();
            }
            tx.Commit();
        }

        // Reads every row back through plain ADO.NET (no Dapper at all) —
        // real SqliteDataReader.Read()/GetInt64/GetString/GetInt32, formatted
        // as one string per row for main.go to print.
        public static List<string> QueryAllDirect(SqliteConnection conn)
        {
            var result = new List<string>();
            var cmd = conn.CreateCommand();
            cmd.CommandText = "SELECT Id, Name, Age FROM Person ORDER BY Id";
            var reader = cmd.ExecuteReader();
            while (reader.Read())
            {
                result.Add($"{reader.GetInt64(0)}\t{reader.GetString(1)}\t{reader.GetInt32(2)}");
            }
            reader.Dispose();
            return result;
        }

        public static int DeleteById(SqliteConnection conn, long id)
        {
            var cmd = conn.CreateCommand();
            cmd.CommandText = "DELETE FROM Person WHERE Id = @id";
            var p = cmd.CreateParameter();
            p.ParameterName = "@id";
            p.Value = id;
            cmd.Parameters.Add(p);
            return cmd.ExecuteNonQuery();
        }

        // Same non-generic Dapper entry point as examples/dapper-demo's own
        // DapperDemoRunner.Query — handed a REAL SqliteConnection this
        // time instead of the fake in-memory one. Dapper's own SqlMapper
        // has no idea (nor does it need to) that the IDbConnection it was
        // given is vmnet's own native Go-backed type rather than a real
        // .NET ADO.NET driver instance — same real Dapper.dll, same real
        // SqlMapper.Query/Execute code path, this time reading from an
        // actual SQLite file on disk. Only parameterless SQL text is used
        // here for the same documented reason as examples/dapper-demo (the
        // {=name} literal-token regex).
        public static List<Dictionary<string, object>> QueryViaDapper(IDbConnection conn, string sql)
        {
            var result = new List<Dictionary<string, object>>();
            foreach (object row in SqlMapper.Query(conn, typeof(object), sql))
            {
                var dict = new Dictionary<string, object>();
                foreach (KeyValuePair<string, object> kv in (IDictionary<string, object>)row)
                {
                    dict[kv.Key] = kv.Value;
                }
                result.Add(dict);
            }
            return result;
        }

        public static int ExecuteViaDapper(IDbConnection conn, string sql)
        {
            return SqlMapper.Execute(conn, sql);
        }
    }
}
