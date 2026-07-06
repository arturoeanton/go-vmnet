// Command jint-advanced-demo pushes the real, unmodified Jint 3.1.3
// engine (the same package examples/jint-demo/jint-nowrapper use) harder
// than a one-line "1 + 2": var/let/const (including a single statement
// declaring three variables at once), nested object/array literals with
// property and index access, arithmetic/comparison/ternary/logical
// operators, Math.* built-ins, real structured data passed in from the
// Go host, a heavier computational loop, function declarations/closures/
// recursion/arrow functions, array growth (push/sort/slice/reverse/
// filter/reduce), and string methods (toUpperCase/trim/charAt/indexOf)
// — all running as real, unmodified Jint IL inside vmnet, with no CGo
// and no dotnet runtime installed anywhere this Go binary actually runs.
//
// One real gap remains open: `.concat`/`.map` and `JSON.stringify` on
// anything but a single-digit number still need a multi-character
// `Span[T]` write, which needs the real `sizeof` CIL opcode on an open
// generic type parameter — a genuinely deep gap, not a narrow miss. See
// docs/en/ROADMAP.md's Fase 3.77 entry for the full account of what got
// fixed to make everything else above work, and this directory's own
// README for what's still open.
package main

import (
	"fmt"
	"log"
	"os"

	vmnet "github.com/arturoeanton/go-vmnet"
)

func main() {
	vm := vmnet.New()

	if err := vm.NuGet().Add("Jint", "3.1.3"); err != nil {
		log.Fatalf("NuGet().Add: %v", err)
	}
	if err := vm.NuGet().Restore(); err != nil {
		log.Fatalf("NuGet().Restore: %v (needs network access to nuget.org)", err)
	}

	jintAsm, err := vm.LoadPackage("Jint")
	if err != nil {
		log.Fatalf("LoadPackage(Jint): %v", err)
	}

	wrapperData, err := os.ReadFile("bin/Release/netstandard2.0/JintAdvancedWrapper.dll")
	if err != nil {
		log.Fatalf("read JintAdvancedWrapper.dll: %v (run `dotnet build -c Release` in this directory first)", err)
	}
	wrapperAsm, err := vm.LoadBytes("JintAdvancedWrapper.dll", wrapperData)
	if err != nil {
		log.Fatalf("LoadBytes(JintAdvancedWrapper.dll): %v", err)
	}
	wrapperAsm.WithDependencies(jintAsm)

	// var/let/const, nested object/array literals, operators, and Math
	// built-ins — all in one script.
	suite, err := wrapperAsm.Call("VmnetJintAdvancedDemo.JintAdvancedWrapper", "RunSuite")
	if err != nil {
		log.Fatalf("RunSuite: %v", err)
	}
	fmt.Println("RunSuite() =", suite.Native())

	// A real, structured JSON document built on the Go side, passed into
	// the engine, and evaluated against — not just a single number or
	// string crossing the Go<->Jint boundary.
	const orderJSON = `{"customer": "Ada", "items": [{"sku": "A1", "qty": 2, "price": 9.5}, {"sku": "B7", "qty": 1, "price": 20}]}`
	total, err := wrapperAsm.Call("VmnetJintAdvancedDemo.JintAdvancedWrapper", "EvaluateWithData",
		vmnet.String(orderJSON),
		vmnet.String("data.items[0].qty * data.items[0].price + data.items[1].qty * data.items[1].price"))
	if err != nil {
		log.Fatalf("EvaluateWithData: %v", err)
	}
	fmt.Println("EvaluateWithData(order total) =", total.Native())

	// A heavier loop — 2,000 iterations of real JavaScript, each doing
	// real arithmetic through a `let` binding — to prove vmnet's own
	// sandbox limits don't choke on ordinary, non-trivial computation.
	// 2,000 is deliberately not a round "impressive" number: each JS loop
	// iteration compiles to several real vmnet instructions/calls (the
	// comparison, the increment, the body), so vmnet's own default
	// per-call instruction budget (internal/interpreter/limits.go) caps
	// how many are safe to run in one Evaluate() call lower than "JS loop
	// iterations" alone would suggest — confirmed empirically while
	// building this demo (1,000-3,000 succeed, 5,000+ hit the limit).
	// Real embedders needing more raise the limit explicitly; this demo
	// stays inside the honest, out-of-the-box default.
	sum, err := wrapperAsm.Call("VmnetJintAdvancedDemo.JintAdvancedWrapper", "Loop", vmnet.Int32(2000))
	if err != nil {
		log.Fatalf("Loop(2000): %v", err)
	}
	fmt.Println("Loop(2000) =", sum.Native())

	// Function declarations, closures, recursion, arrow functions, array
	// growth, and string methods — every one of these used to fail
	// running under this exact Jint version inside vmnet; Fase 3.77 fixed
	// the root cause (see docs/en/ROADMAP.md).
	features, err := wrapperAsm.Call("VmnetJintAdvancedDemo.JintAdvancedWrapper", "RunFunctionsArraysAndStrings")
	if err != nil {
		log.Fatalf("RunFunctionsArraysAndStrings: %v", err)
	}
	fmt.Println("RunFunctionsArraysAndStrings() =", features.Native())
}
