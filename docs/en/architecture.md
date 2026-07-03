# Architecture

See `docs/en/spec.md` (full specification) and `docs/en/ROADMAP.md` (4-phase
delivery plan). This document is the quick map of "what lives where" in the
repo as currently implemented — it expands as each phase adds real
behavior.

## Pipeline (spec §8)

```txt
.dll (PE/CLI)
  → internal/pe          reads PE/COFF headers, locates CLI header + metadata root
  → internal/metadata    parses streams (#~ #Strings #US #Blob #GUID) and tables
  → internal/il          decodes method bodies IL into typed instructions
  → internal/ir          normalizes IL into vmnet's own IR
  → internal/interpreter evaluates the IR on a frame/stack, with limits
  → internal/runtime     managed object model (Type/Method/Field/Heap)
  → internal/bcl         System.* implemented natively in Go
```

`internal/nuget` and `internal/checker` are cross-cutting: the former
resolves `.nupkg` packages into assemblies that enter through the same
pipeline; the latter analyzes metadata/IR before execution to report
compatibility (spec §23).

## Package layout

```txt
/                     package vmnet — public API (spec §6)
/internal/pe          PE/CLI loader (spec §9)
/internal/metadata    metadata loader + signatures (spec §10)
/internal/il          IL decoder (spec §11)
/internal/ir          intermediate IR (spec §12)
/internal/interpreter stack-based interpreter (spec §13)
/internal/runtime     managed object model (spec §14-15, 17-18, 20)
/internal/bcl         partial BCL (spec §16)
/internal/nuget       .nupkg/.nuspec/TFM/resolver (spec §22)
/internal/checker     compatibility checker (spec §23-24)
/cmd/vmnet            CLI (spec §27)
/examples             hello, rules, calculator, nuget-basic
/tests/fixtures       C# fixtures used as golden input
/tests/golden         expected outputs for table-driven tests
```

Why `/internal` instead of the spec's flat layout: see
`docs/en/adr/0002-package-layout.md`.

## Why pure-Go (no CoreCLR in the core)

See `docs/en/adr/0001-pure-go-core.md`.

## Current status

Fase 0 (bootstrap), Fase 1 (IL core), Fase 2 (rules engine) and Fase 2.5
(hardening) complete. The `.dll → internal/pe →
internal/metadata → internal/il → internal/ir → internal/interpreter →
internal/bcl` pipeline runs end to end against a real assembly compiled with
the .NET SDK (`tests/fixtures/csharp`), exposed through the public API
(`vmnet.New()`, `Assembly.Call`/`CallBytes`/`CallJSON`) and the CLI
(`vmnet inspect` / `vmnet il` / `vmnet run`). Current scope: static and
instance methods, `newobj`/`callvirt`/fields (no vtable — direct
resolution), `List<T>` / `Dictionary<string,V>` with a native Go backing,
unhandled `throw` (propagated as a typed Go error,
`vmnet.ManagedException`), and the `byte[]`/JSON bridge. Interface/vtable
dispatch, `try/catch/finally`, generics beyond List/Dictionary, and
`DateTime`/`Guid` are left for later phases (`docs/en/ROADMAP.md`) — the IR
builder explicitly reports any unsupported opcode instead of executing it
incorrectly. (`System.Array` was added in Fase 3.5 — see below.)

Also (Fase 2.5): the interpreter recovers from any panic at the public
boundary (`Machine.Invoke`) instead of crashing the host process, actually
enforces `MaxStackDepth`, `*vmnet.Assembly` is safe to call from multiple
goroutines (`sync.RWMutex` over the method/type caches, verified with
`-race`), and `internal/pe`, `internal/metadata` and `internal/il` have
native Go fuzz tests (run manually for ~16.8M combined executions with no
panics).

Fase 3 (checker + NuGet) complete. `internal/checker` reuses the real
pipeline (not a separate heuristic reimplementation) to decide whether an
assembly is `compatible`/`partial`/`unsupported` per profile
(`minimal`/`rules`/`netstandard-lite`). `internal/nuget` reads real
`.nupkg`/`.nuspec` files (short and long TFM form), resolves transitive
dependencies against `api.nuget.org` (highest-version-wins, documented as a
simplification), caches into `.vmnet/packages/` and exposes
`vm.NuGet().Add/Restore/Packages()` + `vm.LoadPackage(id)`. Certified
against 7 real, popular NuGet packages (see `docs/en/ROADMAP.md` for the
full table); 3 of them have a real function executing correctly through
vmnet. The certification process found and fixed two real gaps:
`MethodSpec` resolution (calls to generic methods) and an unsigned
comparison bug (`.un` opcodes) that gave silently incorrect results, not
just "unsupported".

Fase 3.5 (hardening + real DLL compatibility) complete. The engine now
supports `System.Array` (`newarr`/`ldlen`/`ldelem.*`/`stelem.*`, SZARRAY
only, with `Limits.MaxArrayLength`), managed pointers for `ref`/`out`
(`ldarga`/`ldloca`/`ldelema`/`ldflda` + `ldind.*`/`stind.*` — modeled as a
Go `*runtime.Value` pointing into a fixed-size slice, with no special case
whatsoever in `Call`/`NewObj`) and static fields with lazy `.cctor`
(`ldsfld`/`stsfld`, `sync.Once` per `Type`). Re-certified against the same
7 packages from Fase 3: the average of clean methods rose from ~45.5% to
~56.8% (`docs/en/ROADMAP.md` has the full per-package table). The process
found and fixed three real concurrency/correctness bugs that didn't exist
as a risk before `runtime.Type` started loading mutable state: a
reentrancy deadlock when a `.cctor` writes its own static field, a race
condition in `Assembly`'s type cache that could duplicate a `Type` under
concurrent access, and an incorrect `default(T)` for value-type fields
never explicitly assigned (now resolved by parsing the field's real
signature — `metadata.ParseFieldSig`, new). It also detected and fixed two
cases of "drift" in `internal/checker` (the `minimal` profile did not
exclude arrays/static fields as it should have, and `sigShapeFindings`
kept flagging `ref`/`out` as unsupported after it had actually been
implemented) — both caught by the checker's own dogfood test.

Fase 3.6 (first sub-phase on the path to 85% compatibility, see
`docs/en/ROADMAP.md`) complete: the `switch` opcode (already decoded since
Fase 1 but never lowered to IR) and a batch of high-reach BCL natives
(`StringBuilder`, `String.Format`/`Substring`/indexer/`Equals`,
`Array.Empty`, `Double.IsNaN`, `CultureInfo`/`Environment` stubs). It
exposed the first concrete case of the already-documented "callvirt
without a real vtable" limitation: the C# compiler emits
`StringBuilder.ToString()` as `callvirt Object::ToString`, relying on the
CLR's real virtual dispatch — vmnet resolves it statically by the declared
`MemberRef`, so without a targeted patch in `objectToString` it always ran
the generic `ToString`. Real virtual dispatch (type hierarchy +
`isinst`/`castclass`) is Fase 3.8. Certification (7 packages from Fase 3 +
Jint, the full JavaScript engine for .NET used as the target for the
planned "dynamic language" demo): the average of the 7 packages rises from
~56.8% to ~59.8%; with Jint included, ~60.3%.

Fase 3.7 (value types) complete: the engine now models real structs
(`runtime.KindStruct`, copied by value via `Value.Clone()` at every point
where a value enters a persistent slot, not shared by reference like
`Object`) — `initobj`/`ldobj`/`stobj`/`constrained.`, `newobj` pushing the
value instead of a reference for a value type, and `System.Nullable`1`.
Found and fixed two real bugs exposed as soon as it was tested against a
fixture with structs: struct-typed locals started out uninitialized (the
C# compiler relies on the CLI's `InitLocals` guarantee and omits `initobj`
when it can prove the struct is fully overwritten before use — now
`runtime.Method.LocalDefaults` pre-zeroes them the same way it already did
for fields), and a recursion deadlock in Fase 3.5's type-resolution lock
(it assumed building a type never needs to resolve another one, false as
soon as a struct field/local references another type — a redesign
verified with `TestStructsConcurrentResolve`). Certification: the average
of the 7 packages rises from ~59.8% to ~63.2%; with Jint, ~63.6%.

Fase 3.8 (real type hierarchy) complete: `runtime.Type` now knows its
`BaseTypeFullName` and its `Interfaces` (spec §II.22.23, `InterfaceImpl`
table, unused until now), and `isinst`/`castclass` dispatch against that
real tree instead of not existing at all. Two real bugs exposed by the
first inheritance fixture: comparing a reference against `null` (`<value>
ldnull cgt.un`/`ceq`, the most common compiled form of `is`/`!= null`/`==
null`) failed with "mismatched value kinds" — no prior fixture had
explicitly compared a reference against `null` via IL; and fields declared
in a base class simply did not exist on instances of its subclasses
(`runtime.Type` had never needed to look beyond its own `TypeDef`) — now
the base type is resolved recursively and its fields are prepended, just
like the CLR's real memory layout. Certification (7 packages + Jint): the
average of the 7 rises from ~63.2% to ~64.2%; Jint gets the big jump,
~66.1% to ~74.4% (type-based dispatch/constant casts in a JS engine),
~63.6% to ~65.5% with Jint included in the 8-package average.

Fase 3.9 (delegates/closures) complete: `runtime.KindFunc` represents a
delegate as its target method's full name plus an optional receiver,
detected **structurally** (not by type name) both in `newobj` (`ldftn` +
receiver + `.ctor(object, native int)`, the same shape for any delegate
type) and in `Invoke` dispatch (by the receiver's Kind). Closures needed
no extra work at all: the C# compiler already lowers them to a real class
with the captured variables as fields, which the object model already in
place since Fase 2 handles with no special cases — verified even with a
closure that mutates a captured local. The checker's own dogfood test
immediately caught the expected drift (the checker didn't know that
`Func`2::Invoke` now resolves, since it is never registered in
`bcl.Lookup`) — `isDelegateType` was added, recognizing known BCL prefixes
plus locally declared delegates via their real `TypeDef`. Certification:
the average of the 7 packages rises from ~64.2% to ~67.6% (~65.5% to
~68.8% with Jint); `FluentValidation` (a predicates/callbacks library)
gets the biggest jump measured along the entire path to 85%, +13.4 points.

Fase 3.10 (real `try`/`catch`/`finally`) complete — the architecturally
largest piece of the path to 85%. `internal/il` gains a parser for the
exception-handling clauses table (spec §II.25.4.5-6, *small*/*fat* forms,
never read before) and `internal/interpreter` gains a full dispatch
engine: a `*runtime.ManagedException` that exits a method's execution (via
`throw`, `rethrow`, or propagated from any nested call) is matched against
the method's handlers from innermost to outermost, a `catch` matches by
reusing the same real hierarchy walk from Fase 3.8 (not just exact type
comparison), and any `finally`/`fault` along the path always runs before
the exception continues on its way. The refactor was deliberately
low-risk: the existing ~40-case giant `switch` was extracted intact into
its own function (`runFrame`), without touching the internal logic of any
previous case — all the new risk was concentrated in the dispatch
mechanism, not spread across the file. Certification: the average of the
7 packages rises only slightly from ~67.6% to ~67.7% (~68.8% to ~69.0%
with Jint) — a small, expected move, since exceptions only "clean up" a
method if they were the sole obstacle; this phase's value is
architectural, not a big jump in the number.

Fase 3.11 (`foreach`/enumerators) complete — reprioritized based on data:
the original plan targeted DateTime/Span, but the same
findings-per-target probe as always showed that `foreach` over
`List<T>`/`Dictionary<K,V>` **did not work at all** (Fase 2 only gave
indexed access) and that this was much broader (7-8/8 packages) than
DateTime/Span (2-5/8). `List<T>.Enumerator`/`Dictionary<K,V>.
Enumerator`/`KeyValuePair<K,V>` are modeled as synthetic value types (same
pattern as `Nullable`1` from Fase 3.7), confirmed against real IL before
writing the native. Found and fixed a real risk before it caused damage:
`List`1.Enumerator::MoveNext` resolved to an unqualified `"Enumerator"`
(`resolveTypeToken` had never needed to walk `ResolutionScope` for a
nested `TypeRef`), which would have silently hijacked any other
`Enumerator` type in any loaded assembly (Jint has its own) —
`qualifyTypeRefName` assembles `Type1+Type2` just like a real
`Type.FullName`. It also added `IDisposable::Dispose` (no-op),
`EqualityComparer`1.Default` (reuses `valuesEqual`/`valueHash` from Fase
3.7), `Math.Min`/`Max` and `String.Join`. Certification: the average of
the 7 packages rises from ~67.7% to ~68.8% (~69.0% to ~70.3% with Jint).
DateTime/Span/Memory remain documented as Fase 3.12, not dropped.

Fase 3.12 (`System.DateTime`, `Span<T>`/`ReadOnlySpan<T>`/`Memory<T>`/
`ReadOnlyMemory<T>`) complete — the plan postponed from 3.11. `DateTime`
is modeled as a synthetic single-field value type (`ticks int64`, the same
representation the CLR uses); the four Span types share a single 3-field
shape (`backing`, `start`, `length`), a defensive view over a
`runtime.Array` or a string's characters — vmnet has no unmanaged pointers
to model the real semantics. Three real bugs found and fixed: `Span<T>`'s
indexer is declared `ref T`, not `T` — both reading and writing compile to
the same `call get_Item` followed by `ldind`/`stind` (there is no separate
`set_Item`), so returning the value instead of a reference broke any
writer; `ReadOnlySpan<char>.ToString()` dispatches via
`constrained.`+`callvirt Object::ToString` (same pattern as
`StringBuilder` in Fase 3.6), not a direct call; and the ticks↔`time.Time`
conversion silently overflowed `time.Duration` (an `int64` of
nanoseconds, valid for only ~292 years) when bridging .NET's epoch (year
1) with a modern date — fixed by anchoring on Unix-seconds arithmetic
instead of a duration. Certification: the average of the 7 packages rises
from ~68.8% to ~76.3% (~70.3% to ~76.9% with Jint) — the biggest jump of
the entire 3.6-3.12 sequence, dominated almost entirely by
`Humanizer.Core` (+34.4 points: it's a date-"humanizing" library,
DateTime was its only real blocker). At 76.9% the firm 85% closing
criterion still isn't reached; at least one more Fase 3.x remains before
Fase 4.

Fase 3.13 (`foreach` over an interface-typed collection + cheap-wins
batch) complete. The post-3.12 probe showed that
`IEnumerable`1::GetEnumerator`/`IEnumerator`1::get_Current`/
`IEnumerator::MoveNext` were the widest finding in the entire project
(7/8 targets) — exactly what Fase 3.11 had left out because it needed
real virtual dispatch. `Machine.call` gains a fallback: when the name
declared at the `callvirt` call site (baked in at compile time, e.g.
`IEnumerable`1::GetEnumerator`, since vmnet has no vtable) fails to
resolve, it retries once against the receiver's real concrete type
(`receiverTypeName` — `Struct.Type`/`Obj.Type` for most values,
`bcl.NativeTypeName` for native collections with no own `runtime.Type`
such as `List<T>`), uniformly covering both BCL collections accessed via
interface and plugin classes implementing an interface. A `yield return`
iterator needed one more piece: its `GetEnumerator`/`Current` compiles as
an *explicit* interface implementation (a mangled name like
`"IEnumerable<System.Int32>.GetEnumerator"`, confirmed with `strings`
against the DLL before assuming anything) — `ExplicitImplResolver` walks
the `MethodImpl` table (spec §II.22.27, same pattern as `InterfaceImpl`
from Fase 3.8) to find it.

Applying the fallback as-is exposed a real infinite recursion: a custom
exception constructor chaining `: base(message)` (a plain `call`, not
`newobj` — only the exact type gets `newobj`ed) redirected to itself,
exhausting the stack. Root cause: the fallback should never have applied
to a non-virtual `call`, only to `callvirt` — fixed by propagating the
`Virtual` flag (already present in the IR, never before threaded through
to `Machine.call`) and conditioning the fallback on it. Fixing this at the
root revealed that `System.Exception::.ctor` had never resolved for a
plugin's own subclass at all (the same "only newobj was covered" pattern
that had already bitten DateTime/Nullable`1`), and that once resolved,
the type name stayed stuck to the *base* type instead of the real derived
one, and that `catch` matching did not walk the plugin's real hierarchy —
all three fixed in a chain (base-ctor chaining also registered as a plain
`call`; `TypeName` taken from the receiver's real `Obj.Type`;
`nativeMatches` — now a `Machine` method — alternating between the fixed
map of BCL exceptions and the plugin's real `BaseTypeFullName` in the
same walk).

The cheap-wins batch (measured, not guessed) adds `String`
(`IsNullOrEmpty`/`Split`/`StartsWith`/`IndexOf`/`Replace`/`Trim`/...),
`Char` (`IsUpper`/`IsDigit`/`ToString`/...), `Int32.ToString`, extras for
`List<T>`/`Dictionary<K,V>` (`set_Item`/`ToArray`/`AddRange`/`Contains`/
`TryGetValue`), and confirms that `Nullable`1::.ctor` needed the same
"direct assignment to a local" fix (`ldloca`+`call .ctor` without
`newobj`) as `DateTime` in Fase 3.12. Certification: the average of the 7
packages rises from 76.3% to 79.0% (76.9% to 79.4% with Jint) — a solid,
spread-out move, with no single dominant jump.
At 79.4% the firm 85% closing criterion still isn't reached; the widest
remaining finding is reflection-lite (`ldtoken`/`GetType`/`Type`, 5-6/8),
a natural candidate for the next sub-phase.

Fase 3.14 (reflection-lite: `ldtoken`/`typeof(T)`, `Object.GetType()`,
`System.Type`) complete — exactly the finding noted above. `typeof(T)`
always compiles to `ldtoken T` + `call Type::
GetTypeFromHandle(RuntimeTypeHandle)`, confirmed against real IL; vmnet
does not model `RuntimeTypeHandle` as its own Kind — `ir.LoadTypeToken`
pushes the real `System.Type` directly, and `GetTypeFromHandle` is the
identity function, so the pair behaves like the CLR without an
intermediate representation. `System.Type` is a minimal native-backed
object (`nativeTypeInfo{FullName string}`) with no real reference
identity — every comparison (`op_Equality`, `Equals`) is by the
`FullName` string, never by Go pointer, the only thing observable from
the public `Type` API anyway. `Object.GetType()` reuses the same "real
runtime shape" inspection that `isAssignableTo` (Fase 3.8) already does
for `isinst`/`castclass`. Certification: the average of the 7 packages
rises from 79.0% to 80.1% (79.4% to 80.5% with Jint) — a smaller move
than Fase 3.13 (reflection is more scattered than interface dispatch),
but clean: `Semver`/`SimpleBase` don't move at all (no reflection in
their surface), the four packages that do use `GetType()`/`typeof` with
real volume (`FluentValidation`, `System.Text.Json`, `Newtonsoft.Json`,
`Jint`) do rise. At 80.5% 85% still isn't reached; LINQ is now the widest
non-async/non-regex remaining finding (~174 cases in 4-5/8, Select/Any/
ToList/Where/ToArray), viable now that delegates (3.9), real enumerators
(3.11) and interface dispatch (3.13) exist — a natural candidate for the
next sub-phase.

Fase 3.15 (LINQ: `System.Linq.Enumerable`) complete. Central discovery:
`Enumerable`'s methods cannot be plain `bcl.Native` — each one needs to
invoke the delegate argument (`m.invokeFunc`) and/or walk an arbitrary
`IEnumerable<T>` source via the real `GetEnumerator`/`MoveNext`/
`get_Current` protocol (`m.call`, reusing the interface-dispatch fallback
from 3.13), neither available to a plain `func(args) (Value, error)`
without a `Machine`. A parallel registry (`linqRegistry`,
`internal/interpreter/linq.go`) of "Machine-aware" natives was added, the
same kind of new plumbing `ExplicitImplResolver` had already needed in
3.13. `Select`/`Where`/`Any`/`All`/`ToList`/`ToArray`/`FirstOrDefault`
are eager (materialize immediately), not the CLR's real lazy iterators —
a chained call (`xs.Where(...).Select(...).ToList()`) still behaves
identically from the caller's point of view, because every LINQ result
is wrapped as a real `List<T>` via `bcl.NewListValue` (same pattern as
`bcl.NewTypeValue` from 3.14). `enumerateAll` unifies the source: a fast
path for a native array/`List<T>` (already a Go slice), the real
iteration protocol for anything else — the same mechanism `foreach`
already uses, not a second iteration implementation. Certification: the
average of the 7 packages rises from 80.1% to 80.5% (80.5% to 80.9% with
Jint) — a smaller move than the raw finding volume (~174 cases) suggested,
the same pattern already seen in Fase 3.10: LINQ only "cleans up" a
method if it was the sole obstacle, and several methods using LINQ in
these packages also touch deep reflection or regex, which remain
unsupported. At 80.9% 85% still isn't reached.

Fase 3.16 (`Type::IsAssignableFrom`) complete — the second-widest
reflection finding explicitly left out of 3.14, now mechanical thanks to
the Machine-aware registry 3.15 already generalized (`linqRegistry`
renamed to `machineRegistry`, no behavior change). `typeIsAssignableFrom`
re-derives `isAssignableTo` (Fase 3.8) starting from a type name instead
of an already-known `Value`/`Kind`, resolving the candidate's real
`TypeDef` and walking with `m.typeMatches`. Certification: 80.5% to
80.6% (80.9% to 81.0% with Jint) — a minimal move, the same "not the sole
obstacle" pattern already seen with LINQ. At 81.0% 85% still isn't reached.

Fase 3.17 (critical bug: name collision among the plugin's own nested
types + `System.Lazy<T>`) complete — the biggest jump of the 3.6-3.17
sequence after 3.12, and not from a new feature but from a correctness
fix. The C# compiler emits a non-capturing lambda cache class (literally
named `<>c`) PER containing type that has any — an assembly with lambdas
in two different classes ends up with two separate TypeDefs, both named
`<>c` (same Name, empty Namespace, since a nested type always has one).
All code resolving a TypeDef token to a full name collapsed straight to
`Namespace.Name` without walking the `NestedClass` table (spec §II.22.32)
— the SAME class of bug Fase 3.11 had already fixed for TypeRef (foreign
nested types) but had left explicitly documented as a preexisting risk
for TypeDef (the plugin's own types). The risk became real: after adding
a second file with lambdas and running the suite with `-count=3` (not
just once), `ldsfld` started resolving against the wrong `<>c`. Fixed
with `metadata.EnclosingClass` (new, reads `NestedClass`, no prior
reader), `qualifyTypeDefName`/`QualifyTypeDefName` (new, walks the table
recursively just like `qualifyTypeRefName` already does with
`ResolutionScope`, replacing the direct `Qualify` call at 8 real sites
across `internal/ir/builder.go`, `assembly.go` and
`internal/checker/analyzer.go`), `metadata.FindTypeDef` extended to
accept `"+"`-qualified names in the round trip, and
`runtime.Type.QualifiedName` (new field) so that `fullTypeName`
(interface dispatch from 3.13, exception catch-matching) doesn't
reconstruct and lose the qualification again. Measured impact: 80.6% to
82.8% (81.0% to 83.0% with Jint) — `SimpleBase` alone jumped +14.7
points, confirming that any real package with more than one class using
lambdas (an extremely common pattern) was already silently resolving
against the wrong `<>c` at some point. Along the way, `System.Lazy<T>`
was added (a `Func<T>` factory invoked exactly once, cached, with the
instance's lock held for the entire computation to correctly serialize
concurrent accesses to the same static field — the real dominant use of
`Lazy<T>`), whose fixture (added alongside the LINQ one) was what
exposed the bug in the first place. At 83.0% 85% still isn't reached,
but the gap closed considerably.

Fase 3.18 (second cheap-wins batch + `IDictionary<K,V>` by interface)
complete. `System.String::.ctor` needed its own path in `newObj` instead
of the normal `bcl.LookupCtor` registry: a string in vmnet is a plain
`KindString`, not a `KindObject` — wrapping it in a `runtime.ObjRef` like
any other native ctor would have been incorrect.
`Interlocked.CompareExchange` implements the real compare-and-swap
semantics (not a stub that always assigns), even though vmnet has no real
multi-core memory model to be atomic against. `IDictionary<K,V>::
set_Item`/`get_Item`/`TryGetValue`/`ContainsKey` are added to Fase 3.13's
interface-dispatch allowlist with no new code — the runtime already
resolved them for free by reusing `Dictionary`2`'s existing natives.
`System.Convert::` is promoted from `netstandard-lite` to `rules` (same
treatment as `System.Type::` in Fase 3.14), so `netstandard-lite` now
stands as an explicit copy of `rules` instead of an additional list.
Certification: 82.8% to 83.3% (83.0% to 83.5% with Jint). At 83.5% 85%
still isn't reached, but the remaining gap is small — what's left with
real volume is async (permanently out of scope), regex (a pending design
decision), and deeper reflection over generics/enums.

Fase 3.19 (`HashSet<T>`, `Stack<T>`, `TimeSpan`) complete — three new
surfaces with moderate volume (4/8), not extensions of something
existing. `HashSet<T>` deduplicates/searches by linear scan with
`valuesEqual`, not a real Go `map` (`runtime.Value` isn't intrinsically
hashable in the map-key sense), the same pragmatic simplification as
`List<T>.Contains`. `TimeSpan` repeats the `DateTime` design (Fase 3.12):
a single-field `ticks int64` value type, also registered as a plain
`call` for direct assignment to a local — this time anticipated by the
already-known pattern, not discovered by surprise. Certification: 83.3%
to 83.5% (83.5% to 83.7% with Jint). ~1.3-1.5 points short of 85%.

Fase 3.20 (`System.Text.RegularExpressions`) complete. Compiles patterns
with Go's RE2 engine, not .NET's real engine — they agree on the vast
majority of real-world usage, but RE2 has no backreferences or
lookaround; a pattern that uses them fails to compile with a clear error,
not a plausible-but-wrong result. A real bug found while running the
fixture: the real hierarchy is `Capture -> Group -> Match`, and `Match`
inherits `Success`/`Value` from `Group`/`Capture` without overriding
them — `m.Success`/`m.Value` compile to `callvirt Group::get_Success`/
`callvirt Capture::get_Value`, never against `Match::` directly. The
first version registered `Match::get_Success`/`get_Value` and they were
never called at all; fixed with a single shared accessor that reads
`(Success, Value)` from either a capture group or the full match (Group
0), registered under the real names. `Match.Groups[i]` uses
`FindStringSubmatchIndex` (index pairs), not `FindStringSubmatch` (plain
strings), to distinguish an optional group that didn't participate
(`Success = false`) from one that captured an empty string.
Certification: 83.5% to 83.7% (83.7% to 83.9% with Jint) — regex is
almost never the sole obstacle for a real method, the same pattern seen
with LINQ. ~1.1-1.3 points short of 85%.

Fase 3.21 (third cheap-wins batch) complete — **crosses 85%**.
`DateTime.Kind` needed a second field added (`kind`, `DateTimeKind` as
`int32`) to the synthetic single-field value type that had existed since
Fase 3.12. `IList<T>`/`IReadOnlyList<T>`/`IReadOnlyCollection<T>`/
`IEqualityComparer<T>` are added to Fase 3.13's interface-dispatch
allowlist with no new code. Certification: 83.7% to **85.1%** (83.9% to
**85.3%** with Jint) — crosses the original firm closing criterion from
Fase 3.6+. `Semver` jumps +5.9 points on its own
(`Int32.Parse`/`TryParse` are its central surface). With the target now
revised to ~97% ("100% can be 97%", hardened BCL, up-to-date
documentation), the sub-phase sequence continues past this crossing.

Fase 3.22 (`async`/`await`, synchronous model) complete — the biggest
jump of the entire 3.6-3.22 sequence. A ceiling analysis (fixing
EVERYTHING non-async) gave a ceiling of 89.6%/89.3% with Jint, below the
~97% target — with async explaining most of what remained in
`Newtonsoft.Json`/`System.Text.Json`/`SimpleBase`, the decision to leave
it out permanently was revisited. Design decision: every `Task` that any
native produces is completed from the moment it is created (vmnet has no
real scheduler) — key consequence: the `MoveNext()` the compiler
generates for any `async` method checks `awaiter.IsCompleted` at every
`await`, and since it's always `true` in this model, the suspending
branch is never actually taken. A single call to `MoveNext()` runs the
whole method end to end, no matter how many `await`s it has. No
interpreter changes were needed: `MoveNext()`'s body is ordinary IL
(fields, branches, a real try/catch/finally), already supported since
Fase 1/3.10 — all the work was BCL surface (`AsyncTaskMethodBuilder`,
`Task`/`TaskAwaiter`, `Task.FromResult`/`Run`). The fixture's four cases
(sequential awaits, an exception after an await, a void method, nested
chaining) worked end to end on the first attempt against real IL, with
no bug found during verification — unlike almost every previous phase.
Certification: 85.1% to **88.1%** (85.3% to **88.0%** with Jint) —
`SimpleBase` (+8.5) and `Newtonsoft.Json` (+6.2) confirm the ceiling
analysis hypothesis. At 88.0% the ~97% target still isn't reached.

Fase 3.23 (fourth cheap-wins batch) complete, with two real correctness
bugs found while verifying against real IL, not just new surface. First:
Fase 3.13's interface dispatch could leave the stack short when the
concrete method's real signature differs from the declared interface
(`IList.Add` returns `int`, redirects to `List`1::Add`, which is `void`)
— caused a real panic (`index out of range`), fixed by using the
signature declared at the call site (`in.HasReturn`) as the authority
for deciding whether to push a result, not whatever the finally-resolved
callee reports. Second: `fieldSlot` never handled a struct receiver
passed directly by value (no managed pointer) — until now every observed
struct field access used `ldloca`+`ldfld`, but `ValueTuple`'s
`t.Item1 + t.Item2` revealed that the real compiler sometimes emits a
plain `ldloc`+`ldfld` for the second access in the same expression, legal
per spec §III.4.10 but never before exercised. It was also discovered
that no native BCL value type had needed a real *static* field until
`TimeSpan.Zero` (a public field, not a property) — `runtime.
NewValueType` doesn't support it at all, so `timeSpanType` was rebuilt
via `runtime.NewType` + `SetStaticField`, with a new fallback in
`resolveTypeByFullName` so that native BCL types without a `TypeDef` in
the plugin assembly can still resolve. Certification: 88.1% to 88.7%
(88.0% to 88.5% with Jint) — a small move as expected for scattered
wins, but the real value is the two correctness bugs (one of them was a
silent risk since Fase 3.13 in any interface dispatch with a mismatched
signature). At 88.5% the ~97% target still isn't reached.

Fase 3.24 (fifth cheap-wins batch) complete: `ConcurrentDictionary`1`
(a real mutex + `GetOrAdd` with both literal-value and factory-delegate
overloads, resolved via the Machine-aware registry since invoking the
factory needs a `Machine`), `Regex.Replace` (same RE2 engine and Kind
disambiguation as `IsMatch`/`Match`), and the project's first real
multicast delegate support — `Delegate.Combine`/`Remove`, backed by a
new `Chain []*Func` field in `runtime.Func` that `invokeFunc` walks
after the target itself, discarding intermediate results just like a
real `MulticastDelegate.Invoke`. Also `System.Array::GetEnumerator` +
a real reference enumerator (unlike `List<T>`'s inlined struct), and a
real bug found while verifying `(Action)Delegate.
Combine(...)` against real IL: `isAssignableTo` had no case at all for
`KindFunc` — a delegate had never gone through a real `castclass` until
now — fixed by accepting any delegate-to-delegate cast/isinst over a
real delegate value, since `runtime.Func` carries no declared delegate
type of its own to check against (it's detected structurally, not by
type, since Fase 3.9). Certification: 88.7% to **88.9%** (88.5% to
**88.7%** with Jint) — the smallest move of the cheap-wins sequence, as
expected: these are real but narrow surfaces. The post-3.24 probe
confirms the cheap high-volume surface queue is exhausted: the widest
remaining findings (4-5/8 packages) are now concentrated almost entirely
in deep reflection (`Type.MakeGenericType`/`GetInterfaces`/
`get_IsGenericType`/`GetMethod(s)`/`GetProperties`/`GetConstructors`,
`System.Reflection.MethodInfo`/`PropertyInfo`/`ParameterInfo`/
`MemberInfo`/`Assembly`, `MethodBase.Invoke`, `Activator.CreateInstance`,
`System.Enum.*`) — the natural candidate for the next phase, but an
architecturally bigger block (introspection backed by real metadata plus
dynamic invocation) than anything tackled so far. At 88.7% the ~97%
target still isn't reached.

Fase 3.25 (deep reflection, first slice: `System.Type` introspection)
complete. A root change in `internal/metadata/
signatures.go`: `SigType` now retains its generic arguments (`Args
[]SigType`, previously discarded when parsing a generic instantiation) —
purely additive, every existing consumer keeps ignoring them. New
`resolveClosedTypeSpecName`/`sigTypeFullName` in
`internal/ir/builder.go`, used only by `ldtoken` (`typeof(T)`):
`typeof(List<int>)` now retains its arguments as
`"List\`1[[System.Int32]]"`, while `initobj`/`ldobj`/`stobj`/`MemberRef`
resolution still don't need them. On that basis: `Type.IsGenericType`/
`GetGenericTypeDefinition`/`GetGenericArguments`/`MakeGenericType`
(pure bracket parsing, no `Machine` access),
`Nullable.GetUnderlyingType`. `runtime.Type` gained `IsEnum`/`IsInterface`
(previously only `IsValueType`, which collapsed struct and enum
together) — `assembly.go` populates them from the real `TypeDef`
(`IsInterface` reads the `TypeAttributes.Interface` bit directly from
`Flags`, the only one of the three not derived from `Extends`). On top of
that: `Type.IsValueType`/`IsEnum`/`IsInterface`/`BaseType`/
`GetInterfaces()`/`GetType(string)` — a fixed map of known
primitives/BCL interfaces first, the plugin's real `TypeDef` after
(Machine-aware, `internal/interpreter/reflection.go`). A real bug found
with the project's very first plugin `enum` (`TrafficLight`, new
fixture): `buildType` entered infinite recursion, because an enum member
is a self-referencing `static literal` field in real IL (`static literal
valuetype TrafficLight Red = int32(0)`, not `int32`) —
`fieldOrLocalDefault` tried to compute its default by recursing into
`resolveTypeByFullName` on the very type still being built. Fixed by
skipping default computation for any `FieldAttributes.Literal` field (its
real value lives in the `Constant` table, which vmnet doesn't yet read).
Certification: 88.7% to **89.0%** (with Jint) — `System.Text.Json`/
`Newtonsoft.Json` account for most of the move. The rest of the
reflection block (`System.Reflection.MethodInfo`/`PropertyInfo`/
`ConstructorInfo` as real objects, `MethodBase.Invoke`/
`Activator.CreateInstance`, `Type.GetMethod(s)`/`GetProperties`/
`GetConstructors`/`GetFields`, `Enum.GetValues`/`GetNames`/`IsDefined`)
stands confirmed as the remaining highest-volume surface — a bigger
design (an object hierarchy backed by real metadata plus genuine dynamic
invocation), a candidate for Fase 3.26. At 89.0% the ~97% target still
isn't reached.

Fase 3.26 (`System.Enum.GetValues`/`GetNames`/`IsDefined`/`ToObject`)
complete. Needed a piece of data vmnet had never read: each enum
member's real value lives in metadata's `Constant` table, with no parser
until now. `internal/metadata/constant.go` (new): `constantForField`
(linear search — no direct field-RID-to-`Constant`-row index in the
format itself), `decodeConstantInt64`, `EnumMembers(typeRID)`.
`ConstantRow`/`md.Constant(rid)` already existed in `resolver.go` from
an earlier phase, never wired to anything — this phase uses them for the
first time. A new `EnumResolver` in the `Machine` chain (same pattern as
`ExplicitImplResolver`, Fase 3.13), wired via `asm.resolveEnumMembers` —
it only resolves an enum declared by the plugin itself (a real
`TypeDef`); a BCL-only enum like `System.DayOfWeek` still doesn't work
(vmnet has no database of BCL enum members). `Enum.GetValues`/`GetNames`
needed no interpreter changes at all — the resulting array flows
straight through `System.Array::GetEnumerator` (Fase 3.24).
Certification: 89.2%/89.0% unchanged at the table level, but real under
the hood — the individual *finding* count dropped in every touched
package (`Enum.*` stopped appearing as a finding), but the metric is
per-method and those same methods almost always also call something from
the still-pending large reflection block, so they keep counting as a
"method with findings". This confirms even more strongly that the only
real path toward ~97% now runs through `MethodInfo`/`PropertyInfo`/
dynamic invocation.

Fase 3.27 (multi-assembly resolution + real `Jint.Engine.Evaluate()`
demo) complete — the biggest architectural change since Fase 3. `Call()`
only invoked static methods of a single assembly; actually running Jint
requires resolving symbols across its own NuGet dependency chain (Jint →
Esprima → System.Memory → ...). `Assembly.deps []*Assembly` +
`WithDependencies`, `vm.LoadPackage` automatically loading the full
transitive graph, and `runtime.Resolvers` (5 resolvers grouped per
`*runtime.Method`, swapped in `Machine.invoke` during each call) so each
method resolves against the real assembly that produced it — not the
original entry point — avoiding name collisions between assemblies
(`<PrivateImplementationDetails>` exists separately in `Jint.dll` and
`Esprima.dll`). Also: real overload resolution by arity + Kind + exact/
subtype type name (`pickMethodOverload`/`scoreParamMatch`,
`assembly.go`) with a hard disqualifier for impossible shape
combinations in real CIL (`hasHardShapeMismatch`: a `KindObject` can
never be a `SigValueType` without a visible conversion in the IL); real
virtual dispatch that tries the receiver's concrete type first and
climbs the whole inheritance chain, not just as a fallback after "not
resolved"; `newarr` seeding value-type arrays with their real default
instead of a blind `Null()`; and a real aliasing bug in `newObj`/
`runtime.NewStruct` (they copied field defaults with `copy()` —
shallow for a `KindStruct` default, whose `Value.Struct` is a pointer
shared across every instance of the type until the first write). Each
of these was found by running the full pipeline against Jint/Esprima's
real DLLs, not against a purpose-built fixture. Result:
`examples/jint-demo/` runs real JavaScript end to end
(`Engine.Evaluate("1 + 2")` → `"3"`, `Engine.SetValue` + variables → `7`)
through the unmodified Jint 3.1.3 engine — see `docs/en/ROADMAP.md` for
the complete bug-by-bug breakdown.

Fase 3.28 (instance API: `Assembly.New`/`Instance.Call`) complete.
`Call`/`CallBytes`/`CallJSON` only invoke static methods; `New`/`Call`
expose to the Go host the same internal `newobj`/`callvirt` mechanism
that Fase 3.27 made real end to end — `Machine.New`/`Machine.CallInstance`
(`internal/interpreter/eval.go`) are thin exported wrappers over
`Machine.newObj`/`Machine.call`, and `*Instance` (`instance.go`)
implements `Value` so it can be chained (`engine.Call("Evaluate", ...)` →
`*Instance` → `.Call("ToString")`). Result: `examples/jint-nowrapper/`
runs the same `Engine.Evaluate`/`SetValue` as `examples/jint-demo` with no
C# glue assembly at all — only two real limitations (not bugs) remain
documented: optional parameters with a default value and extension
methods are syntactic sugar the C# compiler resolves at compile time, not
something the CLR/CIL models at runtime, so `Instance.Call` cannot
reconstruct them automatically (see
`examples/jint-nowrapper/README.md`).
