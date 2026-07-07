# jint-advanced-demo

Pushes the real, unmodified [Jint](https://github.com/sebastienros/jint)
3.1.3 engine (the same package `examples/jint-demo`/`jint-nowrapper` use)
harder than a one-line `"1 + 2"`: `var`/`let`/`const`, nested object/array
literals, arithmetic/comparison/ternary/logical operators, `Math.*`
built-ins, a real structured JSON document from the Go host, a heavier
computational loop, function declarations/closures/recursion/arrow
functions, array growth and higher-order methods (`.push`/`.sort`/
`.slice`/`.reverse`/`.filter`/`.reduce`/`.map`/`.concat`), string methods,
**ES6 classes** (inheritance, `super`, method overriding), **regular
expressions — including parenthesized capturing groups and backslash
shorthand classes (`\d`/`\w`/`\s`), not just character classes**
(`.test`/`.exec`/`.match`/`.replace`, global and non-global),
`JSON.stringify` on real nested data with real numbers, and template
literals — via a small compiled C# wrapper (`JintAdvancedWrapper.cs` in
this directory) that drives `Engine.Evaluate`/`Engine.SetValue` directly.

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
RunClassesRegexAndJson() = Processed 2 shapes and 3 orders | {"shapes":["Circle area=28","Square area=16"],"orderIds":["1234","5678","9999"],"masked":"order-XXXX, order-XXXX, order-XXXX","firstOrder":"1234"}
```

## What building this found: thirteen real bugs fixed, zero gaps left in what this demo exercises

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

That left one gap from Fase 3.77 still open — ES6 classes, and
`.concat()`/`.map()`/`JSON.stringify()`/template literals beyond a
single-digit number, all still failed. Fase 3.79 went back for it and,
chasing it down, found this wasn't one bug but a chain of them:

- **ES6 classes** used to fail on any class with at least one member
  (constructor, method, getter, ...) — a `NullReferenceException`
  dereferencing `Esprima.Ast.ClassProperty.<Key>k__BackingField`. Root
  cause: `Jint.AstExtensions.GetKey<T>(this T property, Engine engine)
  where T : IProperty` calls an interface method (`((IProperty)property).
  Key`) on its own still-open generic parameter — real IL for this always
  compiles a `constrained. !!T` prefix ahead of the `callvirt`, which
  loads the parameter's ADDRESS (not its value) so the same bytecode
  works whether `T` closes over a value type (needing a box) or a
  reference type (needing a plain dereference). `ir/builder.go` drops
  `constrained.` as a no-op — correct for a value-typed receiver (already
  a managed pointer to a struct, handled directly), but not for a
  reference-typed one: without a fix, `GetKey<T>`'s own "this" stayed a
  raw pointer instead of the real object, crashing the moment its body
  did a plain field read. Fixed by auto-dereferencing a call's own
  receiver whenever it's a managed pointer to anything that isn't a
  struct or an about-to-be-constructed value type's default.
- **`.concat()`/`.map()`, template literals, and `JSON.stringify()`
  beyond a single-digit number** turned out to need only a *native*
  `Span<T>`/`ReadOnlySpan<T>.CopyTo`/`TryCopyTo` — sidestepping
  `System.SpanHelpers.CopyTo`'s real body (real unsafe pointer-address
  arithmetic, plus the still-unimplemented `sizeof` opcode) with a plain
  Go slice `copy()` over the same span representation vmnet already has.
  Getting there also surfaced a real, general correctness bug: the plain
  `conv.u8` CIL opcode was sign-extending instead of zero-extending,
  which silently corrupted `String.prototype.split()` with no explicit
  limit argument (the overwhelmingly common case) into returning an
  array of length `-1`.
- **Regular expressions** — `.test()`/`.exec()`/`.match()`/`.replace()`,
  global and non-global — needed a chain of half a dozen missing/broken
  natives: a `Regex` object was silently discarded by its own `as Regex`
  cast (an isinst-registry gap), `StringBuilder.set_Capacity`/
  `ToString(start, length)` were either missing or ignored their own
  arguments (the latter is why every regex literal's own delimiters
  stayed attached — `/a/` compiled as a literal search for `"/a/"`, not
  `a`), the count-limited `Match`/`Replace` overloads Jint always calls
  had no native, and `Capture.Index`/`Length`, `Match.NextMatch()`, and
  `MatchCollection`'s own indexer were never wired up at all.

All of this is regression-tested against real C# fixtures reproducing
each exact shape (`tests/fixtures/csharp/ConstrainedGenericTest.cs`,
`ConvU8Test.cs`, `SpanCopyToTest.cs`, `StringBuilderCapacityTest.cs`,
`RegexFeaturesTest.cs`, `TimeSpanComparisonTest.cs`) — each confirmed to
fail without its fix and pass with it.

**Fase 3.79 left one gap open, and Fase 3.80 closed it**: regex patterns
using a parenthesized group (capturing or not) or a backslash shorthand
class (`\d`/`\w`/`\s`) used to translate incorrectly — `(abc)` became the
invalid pattern `abc)` (missing its own opening paren), and `\d`
disappeared entirely. Root cause: Esprima's own regex-to-.NET pattern
translator (`Scanner.RegExpParser.ParsePattern`) appends a group's own
opening delimiter(s) and a shorthand class's own two characters via
`StringBuilder.Append(string value, int startIndex, int count)` — the
real substring-append overload, which had no native at all and silently
did nothing (every other `Append` overload here collapses to "stringify
the one value," a genuinely different 2-argument shape). One fix,
`internal/bcl/system_stringbuilder.go`'s `sbAppend` gaining this
3-argument overload, closed both symptoms at once — regression-tested in
`tests/fixtures/csharp/StringBuilderCapacityTest.cs`'s own
`StringBuilderAppendSubstringTest`. `RunClassesRegexAndJson` above now
uses real groups and `\d` directly.

See `docs/en/ROADMAP.md`'s own entries for this demo (Fase 3.77/3.78/
3.79/3.80) for the complete, citable account of every finding, including
exact error text and the precise script fragment each one reproduces
with.

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
