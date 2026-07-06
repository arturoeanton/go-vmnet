# jint-advanced-demo

Pushes the real, unmodified [Jint](https://github.com/sebastienros/jint)
3.1.3 engine (the same package `examples/jint-demo`/`jint-nowrapper` use)
harder than a one-line `"1 + 2"`: `var`/`let`/`const` (including a single
statement declaring three variables at once), nested object/array
literals with property and index access, arithmetic/comparison/ternary/
logical operators, `Math.*` built-ins, a real structured JSON document
passed in from the Go host and evaluated against, a heavier computational
loop, function declarations/closures/recursion/arrow functions, array
growth (`.push`/`.sort`/`.slice`/`.reverse`/`.filter`/`.reduce`), and
string methods (`.toUpperCase`/`.trim`/`.charAt`/`.indexOf`) — via a small
compiled C# wrapper (`JintAdvancedWrapper.cs` in this directory) that
drives `Engine.Evaluate`/`Engine.SetValue` directly.

Needs network access to nuget.org (to restore Jint) and a local `dotnet`
SDK (to compile the wrapper — vmnet only runs already-compiled IL, it
doesn't compile C#).

```bash
dotnet build -c Release
go run .
```

Expected output:

```txt
RunSuite() = 28
EvaluateWithData(order total) = 39
Loop(2000) = 2.664667e+09
RunFunctionsArraysAndStrings() = 55,3,42,1,3,5,8,9,3,5,9,8,5,3,1,5,8,9,26,HELLO WORLD,padded,H,6
```

## What building this found: eight real bugs fixed, one real gap still open

Building this demo's first drafts (closures, arrow-function callbacks on
array methods, a recursive Fibonacci, an ES6 class hierarchy, `sort`/
`slice`/`concat`/`reverse`, template literals, `JSON.stringify`, a named
function invoked from Go) meant running real Jint/Esprima code paths no
previous example in this project had exercised. Six real, narrow BCL gaps
were found and fixed early on:

- `System.Char::GetHashCode` didn't exist at all (Esprima's own tokenizer
  keys character-class lookup tables by `char`).
- `Nullable<T>` defaulted its "value" field to a hardcoded `Int32(0)`
  regardless of `T` — silently wrong for `Esprima.JavaScriptParser`'s own
  `ArrayList<Token>?` field (`T` a plugin-defined generic value type, not
  a numeric primitive). Fixed in two places: `assembly.go`'s
  `nullableValueTypeDefault` (field defaults, using the real closed
  generic argument already available in the signature) and
  `internal/interpreter/structs.go`'s `nullableDefaultFor` (`initobj`,
  which needed `ir/builder.go`'s `resolveTypeTokenOrGeneric` to actually
  encode `T`'s name for `Nullable\`1` specifically, since it previously
  discarded it for every generic instantiation).
- `System.Array::Copy`'s 3-arg overload (`Array, Array, int` — implicitly
  `sourceIndex=destinationIndex=0`) and `System.Array::Clear` (both real
  overloads) didn't exist — Esprima's own `ArrayList<T>.Clear()` calls
  `Array.Clear(_items, 0, _count)` directly.
- `System.Runtime.CompilerServices.RuntimeHelpers::GetHashCode` didn't
  exist — Jint's own internal property/object bookkeeping calls this to
  hash by reference identity.
- `System.Char::IsSurrogate`/`IsSurrogatePair` didn't exist — Jint's own
  `JSON.stringify` checks every character position for a UTF-16 surrogate
  pair while escaping a string.
- `System.Collections.Generic.List\`1::get_Capacity`/`set_Capacity` didn't
  exist — Jint's own internal property storage checks `Capacity` the same
  way `ArrayList<T>` does.

All six are real, regression-tested fixes (`tests/fixtures/csharp/
Arrays.cs`, `CheapWins.cs`, `Structs.cs`; see `docs/en/ROADMAP.md` for the
full account) — this demo's own `RunSuite`/`EvaluateWithData`/`Loop`
scripts exercise every one of them (the multi-declarator `var` statement
alone needs the `Nullable<T>` fix; the loop and object construction need
the rest).

Three deeper gaps were found and root-caused after that — function
declarations, array growth, and string methods all failed outright. Fase
3.77 (`docs/en/ROADMAP.md`) fixed the two that turned out to share a root
cause and account for the large majority of what was broken:

- **Any JS `function`** (named, anonymous, or arrow) used to fail while
  Jint built the function object itself. Root cause: `assembly.go`'s
  overload-resolution heuristic (`pickMethodOverload`) reused the ORIGINAL
  call site's own resolved parameter type names positionally against a
  same-named candidate of a DIFFERENT arity, so `Function.
  SetFunctionName`'s real, one-argument instance call to `UnwrapJsValue`
  lost the tie to an unrelated two-argument static overload that
  coincidentally shared its first parameter's type name.
- **Array growth** (`.push`, `.sort()`, `.slice()`, `.reverse()` on a
  copy) and **most string methods** (`.toUpperCase()`, `.trim()`,
  `.charAt()`, `.indexOf()`, `.filter()`/`.reduce()` callbacks) all
  shared the SAME actual root cause, once traced all the way through:
  `Engine.GetValue(object, bool)` — the real target for every property
  lookup on a non-reference JS value — kept losing its own overload tie
  to the unrelated public `Engine.GetValue(JsValue, JsValue)`, silently
  passing a stray `bool` argument where a real property-name `JsValue`
  was expected. Fixed via two changes: a new `hasHardShapeMismatch` check
  (a raw numeric-primitive argument can never legitimately bind a
  resolvable class-typed parameter — CIL always emits an explicit `box`
  first) and teaching `paramTypeName` to resolve `char`/`bool` parameters
  by name too (needed separately for `JsString.Create`'s own four
  same-arity overloads — `'abc'.charAt(1)` used to return `"98"`, the
  numeric code point as a string, instead of `"b"`). Getting past that
  overload tie also surfaced a second, smaller gap: the plain `unbox` CIL
  opcode (distinct from `unbox.any`, already supported) had no
  translation at all — `Engine.GetValue`'s own `((Completion)value).Value`
  needs it to read a field directly off a boxed struct. All three fixes
  are regression-tested (`tests/fixtures/csharp/OverloadTieBreak2.cs`,
  `PrimitiveClassShapeMismatch.cs`, `CharOverloadPick.cs`,
  `UnboxOpcodeTest.cs`).

One real gap remains, narrower than it first looked: **`.concat()`/
`.map()`, template literals, and `JSON.stringify` on anything but a
single-digit number** still hit an unimplemented `sizeof` IL opcode
inside `System.SpanHelpers.CopyTo` whenever the operation needs a
multi-character `Span<T>` write (a two-digit number's own decimal
formatting, a multi-character `.join` separator, ...) — a real, general
gap: `sizeof` on an open generic type parameter needs a per-instantiation
memory-layout size vmnet's type-erased `Value` model doesn't track
anywhere today, not something fixable by adding one native.
`.filter()`/`.reduce()`/single-character `.join(',')` all take a
different code path that doesn't need it — `RunFunctionsArraysAndStrings`
above sticks to those.

See `docs/en/ROADMAP.md`'s own entries for this demo for the complete,
citable account of every finding (eight fixed, one open), including exact
error text and the precise script fragment each one reproduces with.

## Why the loop runs 2,000 iterations, not 100,000

Each JS loop iteration compiles to several real vmnet instructions/calls
(the comparison, the increment, the body) — and since Jint itself is a
real, unmodified C# program being interpreted, "one JS statement"
unwinds into dozens of real CIL instructions across Jint's own tree-
walking evaluator before vmnet ever gets back to the next JS statement.
vmnet's own default instruction budget (`internal/interpreter/
limits.go`'s `DefaultLimits`, 10,000,000 instructions) is generous for
running compiled C# directly, but running *interpreted JavaScript on top
of an interpreted C# JS engine* compounds the per-operation cost —
confirmed empirically while building this demo (1,000-3,000 iterations
succeed, 5,000+ hit `VMNET_CALL_DEPTH_EXCEEDED`/"instruction limit
exceeded"). A real embedder needing more raises the limit explicitly
(`vm` exposes no `Limits` knob yet — see `docs/en/ROADMAP.md`'s Fase 4
checklist); this demo stays inside the honest, out-of-the-box default
rather than picking a number that only works because it was tuned to
just barely pass.

## See also

- `examples/jint-demo`/`examples/jint-nowrapper` for the original,
  minimal Jint demos this one builds on.
- `docs/en/security.md` for `VM.Permissions()` — this demo, like every
  other one in `examples/`, gets vmnet's usual deny-by-default sandbox.
