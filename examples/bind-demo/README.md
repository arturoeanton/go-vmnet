# bind-demo

Shows `vmnet bind`'s own generated Go wrapper code in real use — no
hand-written glue, no `Assembly.Call("Namespace.Type", "Method", ...)` string
literals at the call site, just typed Go functions and methods generated
straight from a real assembly's own metadata.

`generated/fixtures.go` was produced by running `vmnet bind` against this
project's own shared golden fixture assembly:

```bash
go run ./cmd/vmnet bind \
  tests/fixtures/csharp/bin/Release/netstandard2.0/Vmnet.Fixtures.dll \
  --out=examples/bind-demo/generated --package=fixtures
```

That single command walked `Vmnet.Fixtures.dll`'s TypeDef table and emitted
57 bound types, each as a real Go struct/function pair — `SimpleMath.Add(int,
int) int` (a static method with no overloads and two precisely-typed `int32`
parameters) became the free function `fixtures.SimpleMathStatic_Add(asm,
a, b)`, and `Customer`'s constructor and `Name`/`Age` properties became
`fixtures.NewCustomer(asm)` plus `(*Customer).GetName()` /
`(*Customer).SetName(string)` methods on a real Go type wrapping
`*vmnet.Instance`.

```bash
dotnet build ../../tests/fixtures/csharp/Fixtures.csproj -c Release
go run .
```

Expected output:

```txt
SimpleMath.Add(3, 4) = 7
Customer.Name = Ada
```

## What the generator does and doesn't do

`vmnet bind` maps every public, non-nested, non-interface, non-enum type's
public constructors and methods:

- A method with exactly one real overload and only types the generator
  understands (the numeric primitives, `string`, `bool`, `byte[]`, and any
  other bound type from the same run) gets a precise, typed Go signature —
  no `vmnet.Value` boxing at the call site.
- Everything else — real overloaded methods, or a signature using a type the
  generator doesn't map yet (generics, delegates, unbound reference types) —
  still gets a generated method, just with a generic
  `(...vmnet.Value) (vmnet.Value, error)` signature and a doc comment
  explaining why. Nothing is silently dropped.
- `get_X`/`set_X` compiler-generated property accessors become `GetX`/`SetX`,
  matching normal Go naming rather than the raw IL member name.

`vmnet bind` also works directly against a NuGet package id, matching
`vmnet check package`'s own resolution:

```bash
vmnet bind package Jint@3.1.3 --out ./jintgo
```

This was verified end-to-end against the real, unmodified `Jint` 3.1.3
package from nuget.org during development (111 bound types, real JavaScript
evaluation through the generated `jint.NewEngine`/`Evaluate` wrapper
producing the correct result) — see `docs/en/compatibility-profile.md` for
the full write-up.

Regenerate `generated/fixtures.go` any time `Vmnet.Fixtures.dll`'s public API
changes; the header comment in that file says as much (`DO NOT EDIT` — it's
generated output, and hand edits will be overwritten and are also just the
wrong place to make them).
