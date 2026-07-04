// Command dapper-demo runs the real, unmodified Dapper 2.1.79 NuGet
// package's own SqlMapper.Query/Execute against a minimal in-memory fake
// ADO.NET provider (DapperDemoWrapper.dll, built from
// DapperDemoWrapper.cs in this directory) — no real database engine, no
// dotnet SDK installed at runtime. vmnet doesn't need to implement a
// database at all: Dapper's own real reflection-based column-to-object
// mapping code runs unmodified against a plain fake IDbConnection/
// IDbCommand/IDataReader, exactly the same shape a real driver (SqlClient,
// Npgsql, Microsoft.Data.Sqlite) would supply — vmnet's own virtual-
// dispatch ancestor walk (internal/interpreter/calls.go) resolves every
// ADO.NET interface call straight through to the fake provider's own
// concrete methods, the same mechanism every other interface-typed call
// site in this project already relies on.
//
// What this demonstrates as genuinely working, end to end, through
// vmnet: SqlMapper.Query's real dynamic-row deserializer (DapperRow, a
// private nested class inside Dapper.dll — reached via the non-generic
// Query(Type, ...) overload, see DapperDemoWrapper.cs's own doc comment
// for why), SqlMapper.Execute, real IDbConnection/IDbCommand/IDataReader/
// IDataParameter/IDataParameterCollection interface dispatch, and
// DbDataReader's own base-class Dispose() pattern (Dapper wraps a plain
// IDataReader in its own internal WrappedBasicReader : DbDataReader).
//
// What does NOT work, found running this exact package: Dapper's generic
// Query<T>()/Execute<T>() convenience overloads do `typeof(T)` on their
// OWN generic method type parameter internally, which vmnet cannot
// resolve (a documented limitation — ir.LoadTypeToken has no way to
// learn a generic METHOD parameter's real closed type at the point a
// ldtoken instruction executes, only a generic CLASS parameter's).
// Separately, ANY Dapper call that supplies an actual parameters object
// (an anonymous type, Dapper's own DynamicParameters, or even a plain
// dictionary) always scans the SQL text first for a `{=name}` literal-
// replacement token, via a regex using negative lookbehind
// ("(?<![\p{L}\p{N}_])\{=...\}") — a real .NET regex feature Go's
// RE2-based regexp engine can never support (no backreferences, no
// lookaround at all). Both are genuine, permanent architectural limits
// documented here rather than worked around.
package main

import (
	"fmt"
	"log"
	"os"

	vmnet "github.com/arturoeanton/go-vmnet"
)

func main() {
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

	wrapperData, err := os.ReadFile("bin/Release/netstandard2.0/DapperDemoWrapper.dll")
	if err != nil {
		log.Fatalf("read DapperDemoWrapper.dll: %v (run `dotnet build DapperDemoWrapper.csproj -c Release` in this directory first)", err)
	}
	wrapperAsm, err := vm.LoadBytes("DapperDemoWrapper.dll", wrapperData)
	if err != nil {
		log.Fatalf("LoadBytes(DapperDemoWrapper.dll): %v", err)
	}
	wrapperAsm.WithDependencies(dapperAsm)

	conn, err := wrapperAsm.New("DapperDemo.FakeConnection")
	if err != nil {
		log.Fatalf("new FakeConnection: %v", err)
	}
	if _, err := conn.Call("Open"); err != nil {
		log.Fatalf("conn.Open: %v", err)
	}

	fmt.Println("Querying an in-memory fake IDbConnection through Dapper's real SqlMapper.Query:")
	rowsVal, err := wrapperAsm.Call("DapperDemo.DapperDemoRunner", "Query", conn, vmnet.String("SELECT Id, Name, Age FROM Person"))
	if err != nil {
		log.Fatalf("Query: %v", err)
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
		idVal, err := row.Call("get_Item", vmnet.String("Id"))
		if err != nil {
			log.Fatalf("row[Id]: %v", err)
		}
		nameVal, err := row.Call("get_Item", vmnet.String("Name"))
		if err != nil {
			log.Fatalf("row[Name]: %v", err)
		}
		ageVal, err := row.Call("get_Item", vmnet.String("Age"))
		if err != nil {
			log.Fatalf("row[Age]: %v", err)
		}
		fmt.Printf("  %v\t%v\t%v\n", idVal.Native(), nameVal.Native(), ageVal.Native())
	}

	fmt.Println("\nDeleting Id=2 through Dapper's real SqlMapper.Execute:")
	affectedVal, err := wrapperAsm.Call("DapperDemo.DapperDemoRunner", "Execute", conn, vmnet.String("DELETE FROM Person WHERE Id = 2"))
	if err != nil {
		log.Fatalf("Execute: %v", err)
	}
	fmt.Printf("  %v row(s) affected\n", affectedVal.Native())

	fmt.Println("\nRe-querying to confirm the delete stuck:")
	rowsVal, err = wrapperAsm.Call("DapperDemo.DapperDemoRunner", "Query", conn, vmnet.String("SELECT Id, Name, Age FROM Person"))
	if err != nil {
		log.Fatalf("Query (2nd): %v", err)
	}
	rows = rowsVal.(*vmnet.Instance)
	countVal, err = rows.Call("get_Count")
	if err != nil {
		log.Fatalf("rows.get_Count (2nd): %v", err)
	}
	count = int(countVal.Native().(int32))
	for i := 0; i < count; i++ {
		rowVal, err := rows.Call("get_Item", vmnet.Int32(int32(i)))
		if err != nil {
			log.Fatalf("rows.get_Item(%d) (2nd): %v", i, err)
		}
		row := rowVal.(*vmnet.Instance)
		idVal, _ := row.Call("get_Item", vmnet.String("Id"))
		nameVal, _ := row.Call("get_Item", vmnet.String("Name"))
		ageVal, _ := row.Call("get_Item", vmnet.String("Age"))
		fmt.Printf("  %v\t%v\t%v\n", idVal.Native(), nameVal.Native(), ageVal.Native())
	}

	if _, err := conn.Call("Close"); err != nil {
		log.Fatalf("conn.Close: %v", err)
	}
	stateVal, err := conn.Call("get_State")
	if err != nil {
		log.Fatalf("conn.get_State: %v", err)
	}
	// ConnectionState.Closed == 0 (vmnet has no distinct enum Kind — see
	// examples/closedxml-demo's own doc comment on hasFormula for why
	// every enum value here is a plain int32).
	fmt.Printf("\nconnection state after Close(): %v\n", stateVal.Native())
}
