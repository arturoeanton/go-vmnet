// Command sqlite-demo runs real, hand-written ADO.NET C# code — `using
// Microsoft.Data.Sqlite;`, `new SqliteConnection(...)`, real @name and
// positional `?` parameter binding, a real transaction — against vmnet's
// own native, Go-backed Microsoft.Data.Sqlite provider (internal/bcl/
// system_data_sqlite.go, Fase 3.53), itself backed by
// github.com/arturoeanton/go-r2-sqlite: a pure-Go, zero-CGO SQLite engine
// that is vmnet's first-ever external Go dependency (a deliberate,
// one-time exception to its previously-zero-dependency posture — see that
// file's own doc comment). No .NET SQLite driver, no CGO, no C library is
// ever touched — the whole stack from `INSERT INTO ...` down to the bytes
// on disk is Go.
//
// This is a separate example from examples/dapper-demo (which is left
// completely untouched) rather than an extension of it: dapper-demo's own
// FakeConnection proves Dapper's real SqlMapper mapping code runs
// correctly against ANY real IDbConnection shape, entirely in memory, no
// real database engine needed. This demo proves the complementary half —
// a REAL database engine, a REAL .db file on disk, real ADO.NET parameter
// binding and transactions — and then hands that same real connection to
// Dapper's SqlMapper too, showing it doesn't care which is which.
//
// The most convincing proof a real SQLite file resulted (not just a
// vmnet-internal illusion of one) is handing the same file to a
// completely independent, unmodified real SQLite tool: this program
// shells out to the real `sqlite3` CLI (if present on PATH) after
// closing the connection and reads the same rows back through it — the
// same "round-trip through the real, unmodified external tool" pattern
// examples/openxml-demo uses to confirm its own .docx opens in the real
// .NET SDK's OpenXML validator.
package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"

	vmnet "github.com/arturoeanton/go-vmnet"
)

func main() {
	dbPath := "sqlite-demo.db"
	os.Remove(dbPath) // idempotent: a repeat `go run .` starts from an empty file, same as every other demo re-running its own fixture from scratch.

	vm := vmnet.New()

	if err := vm.NuGet().Add("Dapper", "2.1.79"); err != nil {
		log.Fatalf("NuGet().Add: %v", err)
	}
	if err := vm.NuGet().Restore(); err != nil {
		log.Fatalf("NuGet().Restore: %v (needs network access to nuget.org)", err)
	}
	dapperAsm, err := vm.LoadPackage("Dapper")
	if err != nil {
		log.Fatalf("LoadPackage(Dapper): %v", err)
	}

	wrapperData, err := os.ReadFile("bin/Release/netstandard2.0/SqliteDemoWrapper.dll")
	if err != nil {
		log.Fatalf("read SqliteDemoWrapper.dll: %v (run `dotnet build SqliteDemoWrapper.csproj -c Release` in this directory first)", err)
	}
	wrapperAsm, err := vm.LoadBytes("SqliteDemoWrapper.dll", wrapperData)
	if err != nil {
		log.Fatalf("LoadBytes(SqliteDemoWrapper.dll): %v", err)
	}
	// Only Dapper.dll is attached as a dependency here — the real
	// Microsoft.Data.Sqlite.dll SqliteDemoWrapper.csproj compiled against
	// is NEVER loaded into vmnet at all. Every SqliteConnection/
	// SqliteCommand/... call below resolves through vmnet's own native
	// bcl registrations (newObj checks those before ever consulting an
	// attached assembly's TypeDefs — internal/interpreter/calls.go).
	wrapperAsm.WithDependencies(dapperAsm)

	// new SqliteConnection("Data Source=sqlite-demo.db") — a real
	// Microsoft.Data.Sqlite-shaped connection string (keyword=value,
	// case-insensitive "Data Source" key), not just a bare path, exercised
	// deliberately here to prove that parsing works too.
	conn, err := wrapperAsm.New("Microsoft.Data.Sqlite.SqliteConnection", vmnet.String("Data Source="+dbPath))
	if err != nil {
		log.Fatalf("new SqliteConnection: %v", err)
	}
	if _, err := conn.Call("Open"); err != nil {
		log.Fatalf("conn.Open: %v", err)
	}
	stateVal, err := conn.Call("get_State")
	if err != nil {
		log.Fatalf("conn.get_State: %v", err)
	}
	fmt.Printf("Opened %s (ConnectionState=%v, 1=Open)\n", dbPath, stateVal.Native())

	if _, err := wrapperAsm.Call("SqliteDemo.SqliteDemoRunner", "Setup", conn); err != nil {
		log.Fatalf("Setup: %v", err)
	}

	fmt.Println("\nInserting 3 rows through real @name-bound SqliteParameters:")
	people := []struct {
		id   int64
		name string
		age  int32
	}{
		{1, "Ada Lovelace", 36},
		{2, "Grace Hopper", 85},
		{3, "Alan Turing", 41},
	}
	for _, p := range people {
		if _, err := wrapperAsm.Call("SqliteDemo.SqliteDemoRunner", "InsertPerson", conn, vmnet.Int64(p.id), vmnet.String(p.name), vmnet.Int32(p.age)); err != nil {
			log.Fatalf("InsertPerson(%v): %v", p, err)
		}
		fmt.Printf("  inserted %d\t%s\t%d\n", p.id, p.name, p.age)
	}

	fmt.Println("\nInserting 2 more rows with positional `?` parameters, inside a real SqliteTransaction:")
	if _, err := wrapperAsm.Call("SqliteDemo.SqliteDemoRunner", "InsertPeopleInTransaction", conn); err != nil {
		log.Fatalf("InsertPeopleInTransaction: %v", err)
	}
	fmt.Println("  committed")

	printDirect := func(label string) {
		fmt.Println(label)
		rowsVal, err := wrapperAsm.Call("SqliteDemo.SqliteDemoRunner", "QueryAllDirect", conn)
		if err != nil {
			log.Fatalf("QueryAllDirect: %v", err)
		}
		rows := rowsVal.(*vmnet.Instance)
		countVal, err := rows.Call("get_Count")
		if err != nil {
			log.Fatalf("rows.get_Count: %v", err)
		}
		count := int(countVal.Native().(int32))
		for i := 0; i < count; i++ {
			rowVal, err := rows.Call("get_Item", vmnet.Int32(int32(i)))
			if err != nil {
				log.Fatalf("rows.get_Item(%d): %v", i, err)
			}
			fmt.Printf("  %v\n", rowVal.Native())
		}
	}
	printDirect("\nQuerying all 5 rows back through real SqliteDataReader.Read()/GetInt64/GetString/GetInt32 (no Dapper):")

	fmt.Println("\nDeleting Id=2 through a real @name-bound DELETE:")
	affected, err := wrapperAsm.Call("SqliteDemo.SqliteDemoRunner", "DeleteById", conn, vmnet.Int64(2))
	if err != nil {
		log.Fatalf("DeleteById: %v", err)
	}
	fmt.Printf("  %v row(s) affected\n", affected.Native())

	fmt.Println("\nQuerying the SAME real SqliteConnection through Dapper's real SqlMapper.Query (parameterless SQL — see doc comment for why):")
	rowsVal, err := wrapperAsm.Call("SqliteDemo.SqliteDemoRunner", "QueryViaDapper", conn, vmnet.String("SELECT Id, Name, Age FROM Person ORDER BY Id"))
	if err != nil {
		log.Fatalf("QueryViaDapper: %v", err)
	}
	rows := rowsVal.(*vmnet.Instance)
	countVal, err := rows.Call("get_Count")
	if err != nil {
		log.Fatalf("rows.get_Count: %v", err)
	}
	count := int(countVal.Native().(int32))
	for i := 0; i < count; i++ {
		rowVal, err := rows.Call("get_Item", vmnet.Int32(int32(i)))
		if err != nil {
			log.Fatalf("rows.get_Item(%d): %v", i, err)
		}
		row := rowVal.(*vmnet.Instance)
		idVal, _ := row.Call("get_Item", vmnet.String("Id"))
		nameVal, _ := row.Call("get_Item", vmnet.String("Name"))
		ageVal, _ := row.Call("get_Item", vmnet.String("Age"))
		fmt.Printf("  %v\t%v\t%v\n", idVal.Native(), nameVal.Native(), ageVal.Native())
	}

	fmt.Println("\nDeleting Id=3 through Dapper's real SqlMapper.Execute (same real connection):")
	execAffected, err := wrapperAsm.Call("SqliteDemo.SqliteDemoRunner", "ExecuteViaDapper", conn, vmnet.String("DELETE FROM Person WHERE Id = 3"))
	if err != nil {
		log.Fatalf("ExecuteViaDapper: %v", err)
	}
	fmt.Printf("  %v row(s) affected\n", execAffected.Native())
	printDirect("  confirming via plain ADO.NET (no Dapper):")

	if _, err := conn.Call("Close"); err != nil {
		log.Fatalf("conn.Close: %v", err)
	}
	stateVal, err = conn.Call("get_State")
	if err != nil {
		log.Fatalf("conn.get_State (after Close): %v", err)
	}
	fmt.Printf("\nconnection state after Close(): %v (0=Closed)\n", stateVal.Native())

	verifyWithRealSqlite3(dbPath)
}

// verifyWithRealSqlite3 is the actual round-trip proof: everything above
// ran through vmnet's own native, Go-backed provider — this instead
// shells out to the real, unmodified `sqlite3` CLI (whatever real SQLite
// engine happens to be installed on this machine, entirely independent
// of go-r2-sqlite) and asks IT to read the same file back. If the row
// count and data match what vmnet itself just wrote, the .db file on
// disk is a genuine, standard SQLite 3 file — not something only
// go-r2-sqlite's own reader can make sense of. Skipped gracefully (not a
// failure) when `sqlite3` isn't on PATH: go-r2-sqlite's own README
// documents the same binary-compatibility claim independently (files it
// writes pass a real `sqlite3`'s own PRAGMA integrity_check, and vice
// versa) as a fallback citation.
func verifyWithRealSqlite3(dbPath string) {
	fmt.Println("\nIndependent verification: opening the same .db file with the REAL sqlite3 CLI (not go-r2-sqlite):")
	sqlite3Path, err := exec.LookPath("sqlite3")
	if err != nil {
		fmt.Println("  sqlite3 CLI not found on PATH — skipping live verification.")
		fmt.Println("  (go-r2-sqlite's own README claims the same binary compatibility independently: files it writes")
		fmt.Println("   pass `PRAGMA integrity_check` under the real C sqlite3, and files sqlite3 writes are readable here.)")
		return
	}
	out, err := exec.Command(sqlite3Path, dbPath, "SELECT 'sqlite3 sees ' || count(*) || ' row(s):' FROM Person; SELECT Id, Name, Age FROM Person ORDER BY Id;").CombinedOutput()
	if err != nil {
		log.Fatalf("real sqlite3 CLI failed to read %s: %v\n%s", dbPath, err, out)
	}
	fmt.Print(string(out))
	integrity, err := exec.Command(sqlite3Path, dbPath, "PRAGMA integrity_check;").CombinedOutput()
	if err != nil {
		log.Fatalf("real sqlite3 CLI integrity_check failed: %v\n%s", err, integrity)
	}
	fmt.Printf("PRAGMA integrity_check (real sqlite3): %s", integrity)
}
