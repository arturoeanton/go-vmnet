# jint-advanced-demo

Pushes the real, unmodified [Jint](https://github.com/sebastienros/jint)
3.1.3 engine (the same package `examples/jint-demo`/`jint-nowrapper` use)
harder than a one-line `"1 + 2"`: `var`/`let`/`const` (including a single
statement declaring three variables at once), nested object/array
literals with property and index access, arithmetic/comparison/ternary/
logical operators, `Math.*` built-ins, a real structured JSON document
passed in from the Go host and evaluated against, and a heavier
computational loop — via a small compiled C# wrapper
(`JintAdvancedWrapper.cs` in this directory) that drives `Engine.Evaluate`/
`Engine.SetValue` directly.

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
```

## What building this found: six real bugs fixed, three real gaps documented

Building this demo's first drafts (closures, arrow-function callbacks on
array methods, a recursive Fibonacci, an ES6 class hierarchy, `sort`/
`slice`/`concat`/`reverse`, template literals, `JSON.stringify`, a named
function invoked from Go) meant running real Jint/Esprima code paths no
previous example in this project had exercised. Six real, narrow BCL gaps
were found and fixed along the way:

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
full account) — this demo's own script exercises every one of them (the
multi-declarator `var` statement alone needs the `Nullable<T>` fix; the
loop and object construction need the rest).

Three deeper, genuinely still-open gaps were found and root-caused, but
**not** fixed — each one is a wall hit from a different real feature, not
a single narrow miss:

- **Any JS `function`** (named, anonymous, or arrow — even one never
  called) fails while Jint builds the function object itself. Root cause:
  `Function.SetFunctionName`'s own `_nameDescriptor` field (declared type
  `PropertyDescriptor`, a reference type) ends up holding a reference to
  the function object itself instead of `null`, so the next read passes
  the wrong object into `ObjectInstance.UnwrapJsValue(PropertyDescriptor,
  ...)`, which then fails looking up `_flags` on it. `buildType` itself
  was checked and is NOT the problem — every `PropertyDescriptor` subclass
  (`AllForbiddenDescriptor`, `LazyPropertyDescriptor`, `GetSetProperty
  Descriptor+ThrowerPropertyDescriptor`) builds with the correct,
  correctly-inherited `_flags`/`_value` fields. The bug is a field
  aliasing/identity issue somewhere in how `Jint.Native.Function.
  ScriptFunction` gets constructed, not in vmnet's general type-building
  machinery.
- **Array growth** (`.push`, `.slice()`, `.sort()`, `.concat()`,
  `.reverse()` on a copy) fails with `"compare on mismatched value kinds
  (7, 1)"` (`KindObject` vs `KindI4`) — something in Jint's own internal
  array-storage growth path boxes or compares a value incorrectly. A
  plain array literal, indexing, and `.length` all work fine; it's
  specifically growing/copying an array's backing storage that doesn't.
- **String methods** (`.split`, `.trim`, `.charAt`, chained
  `.toUpperCase()`), template literals, and `JSON.stringify` all fail —
  two different ways. String methods hit a `Jint.Native.JsValue._type`
  null-reference (a different bug from the array one above). Template
  literals and `JSON.stringify` hit an unimplemented `sizeof` IL opcode
  inside `System.SpanHelpers.CopyTo` — a real, general gap: `sizeof` on
  an open generic type parameter needs a per-instantiation memory-layout
  size vmnet's type-erased `Value` model doesn't track anywhere today,
  not something fixable by adding one native.

See `docs/en/ROADMAP.md`'s own entry for this demo for the complete,
citable account of all nine findings (six fixed, three open), including
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
