// Command csvhelper-demo runs the real, unmodified CsvHelper 33.1.0 NuGet
// package's own CsvReader.GetRecords<T>() with NO ClassMap registered at
// all — CsvHelper falls back to its own AutoMap() reflection path,
// matching each CSV header column to a property by name and building a
// compiled Expression-tree delegate at runtime to construct and populate
// each record.
//
// This is the first genuinely working demo of that path: AutoMap()
// constructs a closed DefaultClassMap<Product> via reflection alone
// (Type.GetConstructor(s), Expression.New/Lambda/Compile — no
// `newobj DefaultClassMap\`1<Product>::.ctor()` IL instruction anywhere,
// since the record type is only known at runtime), which needed several
// real interpreter fixes to reach: closed-generic identity surviving
// Type.GetConstructor()/ConstructorInfo.Invoke(), a compiler-generated
// iterator (GetRecords<T>()'s own state machine) forwarding its class-level
// T into another generic method call, a nested generic parameter
// surviving one level of type instantiation, several missing
// System.Linq.Expressions.Expression factories (ArrayIndex, Bind,
// MakeMemberAccess, the string-name Call overload, the non-generic Lambda
// overload), and System.String.Join needing to drive a real plugin
// IEnumerable<string>'s own iteration protocol rather than only
// recognizing a vmnet-native list/array. See docs/en/ROADMAP.md's own
// Fase 3.81 entry for the full account.
package main

import (
	"fmt"
	"log"
	"os"

	vmnet "github.com/arturoeanton/go-vmnet"
)

func main() {
	vm := vmnet.New()

	if err := vm.NuGet().Add("CsvHelper", "33.1.0"); err != nil {
		log.Fatalf("NuGet().Add: %v", err)
	}
	if err := vm.NuGet().Restore(); err != nil {
		log.Fatalf("NuGet().Restore: %v (needs network access to nuget.org)", err)
	}

	csvAsm, err := vm.LoadPackage("CsvHelper")
	if err != nil {
		log.Fatalf("LoadPackage(CsvHelper): %v", err)
	}

	wrapperData, err := os.ReadFile("bin/Release/netstandard2.0/CsvHelperDemoWrapper.dll")
	if err != nil {
		log.Fatalf("read CsvHelperDemoWrapper.dll: %v (run `dotnet build CsvHelperDemoWrapper.csproj -c Release` in this directory first)", err)
	}
	wrapperAsm, err := vm.LoadBytes("CsvHelperDemoWrapper.dll", wrapperData)
	if err != nil {
		log.Fatalf("LoadBytes(CsvHelperDemoWrapper.dll): %v", err)
	}
	wrapperAsm.WithDependencies(csvAsm)

	const csvText = "Name,Quantity,Price,InStock\r\n" +
		"Widget,42,9.99,true\r\n" +
		"Gadget,7,149.5,false\r\n" +
		"Gizmo,100,3.25,true\r\n"

	fmt.Println("Reading CSV through CsvHelper's own AutoMap() — no ClassMap registered:")
	fmt.Print(csvText)

	productsVal, err := wrapperAsm.Call("CsvHelperDemo.CsvHelperDemoRunner", "ReadProducts", vmnet.String(csvText))
	if err != nil {
		log.Fatalf("ReadProducts: %v", err)
	}
	products := productsVal.(*vmnet.Instance)

	countVal, err := products.Call("get_Count")
	if err != nil {
		log.Fatalf("products.get_Count: %v", err)
	}
	count := int(countVal.Native().(int32))

	fmt.Printf("Parsed %d product(s):\n", count)
	for i := 0; i < count; i++ {
		productVal, err := products.Call("get_Item", vmnet.Int32(int32(i)))
		if err != nil {
			log.Fatalf("products.get_Item(%d): %v", i, err)
		}
		product := productVal.(*vmnet.Instance)

		nameVal, _ := product.Call("get_Name")
		quantityVal, _ := product.Call("get_Quantity")
		priceVal, _ := product.Call("get_Price")
		inStockVal, _ := product.Call("get_InStock")

		// vmnet has no distinct bool Kind — every C# bool is a plain int32
		// on the CIL stack (see examples/dapper-demo's own doc comment on
		// ConnectionState.Closed for the same reasoning) — 0/nonzero is
		// converted to Go's own bool here purely for display.
		inStock := inStockVal.Native().(int32) != 0

		fmt.Printf("  %-8s qty=%-4v price=%-7v inStock=%v\n",
			nameVal.Native(), quantityVal.Native(), priceVal.Native(), inStock)
	}
}
