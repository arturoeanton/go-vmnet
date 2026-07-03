// Command jint-nowrapper runs real JavaScript inside a Go process the
// same way examples/jint-demo does — the real, unmodified Jint 3.1.3
// NuGet package plus its whole transitive dependency chain — but without
// a compiled C# wrapper: it constructs a real Jint.Engine and drives its
// instance API (Evaluate/SetValue/ToString) directly from Go via
// Assembly.New/Instance.Call (Fase 3.28). No dotnet SDK needed at all —
// only network access to nuget.org.
//
// Two real C# language conveniences don't have a Go equivalent, so this
// example works around them explicitly rather than hiding them:
//
//   - Optional/default parameters are a compile-time C# feature (the
//     compiler fills in the omitted argument at the call site) — Jint's
//     real `Engine.Evaluate(string code, string source = null)` needs
//     both arguments passed explicitly here.
//   - Extension methods are just sugar for a static call on a different
//     type — `JsValue.AsNumber()` is declared on `Jint.JsValueExtensions`,
//     not on `JsValue`/`JsNumber` itself, so Instance.Call (which always
//     targets the receiver's own concrete type) can't reach it; this
//     example uses ToString() instead, which is a real instance method.
//
// See examples/jint-demo/README.md for when a compiled wrapper is still
// the better choice (anything leaning on C#-only sugar vmnet can't
// replicate from Go).
package main

import (
	"fmt"
	"log"

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

	engine, err := jintAsm.New("Jint.Engine")
	if err != nil {
		log.Fatalf("New(Jint.Engine): %v", err)
	}

	result, err := engine.Call("Evaluate", vmnet.String("1 + 2"), vmnet.String(""))
	if err != nil {
		log.Fatalf("Evaluate: %v", err)
	}
	str, err := result.(*vmnet.Instance).Call("ToString")
	if err != nil {
		log.Fatalf("ToString: %v", err)
	}
	fmt.Printf("Evaluate(\"1 + 2\").ToString() = %v\n", str.Native())

	engine2, err := jintAsm.New("Jint.Engine")
	if err != nil {
		log.Fatalf("New(Jint.Engine) #2: %v", err)
	}
	if _, err := engine2.Call("SetValue", vmnet.String("a"), vmnet.Float64(3)); err != nil {
		log.Fatalf("SetValue(a, 3): %v", err)
	}
	if _, err := engine2.Call("SetValue", vmnet.String("b"), vmnet.Float64(4)); err != nil {
		log.Fatalf("SetValue(b, 4): %v", err)
	}
	sum, err := engine2.Call("Evaluate", vmnet.String("a + b"), vmnet.String(""))
	if err != nil {
		log.Fatalf("Evaluate(a + b): %v", err)
	}
	sumStr, err := sum.(*vmnet.Instance).Call("ToString")
	if err != nil {
		log.Fatalf("ToString: %v", err)
	}
	fmt.Printf("a + b (a=3, b=4) = %v\n", sumStr.Native())
}
