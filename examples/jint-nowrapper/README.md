# jint-nowrapper

Same demo as `examples/jint-demo`, but without a compiled C# wrapper: it
constructs a real `Jint.Engine` and drives its instance API
(`Evaluate`/`SetValue`/`ToString`) directly from Go via
`Assembly.New`/`Instance.Call` (Fase 3.28). No `dotnet` SDK needed at
all — only network access to nuget.org.

```bash
go run .
```

Expected output:

```txt
Evaluate("1 + 2").ToString() = 3
a + b (a=3, b=4) = 7
```

## Why this needs two small workarounds

Two real C# language conveniences don't have a Go equivalent, so
`main.go` works around them explicitly instead of hiding them:

- **Optional/default parameters** are a compile-time-only C# feature —
  the compiler fills in the omitted argument at the call site. Jint's
  real `Engine.Evaluate(string code, string source = null)` needs both
  arguments passed explicitly here (`vmnet.String("")` for `source`).
- **Extension methods** are sugar for a static call on a *different*
  type — `JsValue.AsNumber()` is declared on `Jint.JsValueExtensions`,
  not on `JsValue`/`JsNumber` itself, so `Instance.Call` (which always
  targets the receiver's own concrete type) can't reach it. This example
  calls `ToString()` instead, a real instance method.

See `examples/jint-demo/README.md` for when a compiled wrapper is still
the better choice — anything leaning harder on C#-only sugar (implicit
user-defined conversions, more extension methods, generic type
inference from usage) than these two cases.
