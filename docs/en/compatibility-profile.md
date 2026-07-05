# Compatibility profiles

This document is required by spec §33.2 (`/docs/compatibility-profile.md`, alongside
`architecture.md`, `supported-il.md`, `supported-bcl.md`, `nuget-support.md`, `security.md`,
`roadmap.md`). It explains, precisely and against the current code (not a design aspiration),
what each of vmnet's three compatibility profiles allows and forbids, how the checker
(`internal/checker`) actually runs, and what its report means.

## 1. Why profiles exist

A checker that just answers "compatible" or "not" is answering the wrong question, because
"compatible with what" depends entirely on what the caller intends to do with the assembly.
Loading one tightly-scoped business-rule function that only touches primitives and static methods
is a fundamentally different bet than loading an entire object-oriented, generics-heavy NuGet
package pulled straight off nuget.org. vmnet's checker doesn't produce a single verdict — it
produces a verdict *relative to a named profile*. `internal/checker/profile.go` states this
directly:

> "The checker's verdict is always relative to one: what's 'compatible' under minimal can be 'out
> of profile' under the same runtime, because the runtime itself supports more than minimal
> promises."

That's also the reasoning behind spec §33.3's required message, which appears verbatim in this
project's documentation:

> vmnet is not a full .NET implementation.
> vmnet executes a supported subset of CIL and selected BCL APIs.
> Use vmnet check before loading third-party assemblies.

"Before loading" is the operative phrase. The checker is meant to run ahead of
`vm.LoadPackage`/`Assembly.Call`, against an assembly or package you have not yet trusted, so a
caller can decide — before a single IL instruction executes — whether the target's real behavior
fits inside a profile they're willing to accept, and which one.

## 2. The three profiles, precisely

`internal/checker/profile.go` defines exactly three:

```go
const (
	ProfileMinimal         Profile = "minimal"
	ProfileRules           Profile = "rules"
	ProfileNetStandardLite Profile = "netstandard-lite"
)
```

Two independent gates decide whether something is "in profile":

- **The object-model gate** (`objectOpcodesAllowed`) — whether classes, fields, `callvirt`,
  `throw`, arrays, and static fields are permitted *at all*, regardless of what the runtime can
  technically execute. Only `minimal` fails this gate.
- **The BCL allowlist** (`bclPrefixes[profile]`, checked via `inProfile`) — a resolved call or
  constructor target's full name (`Namespace.Type::Member`) must match one of the profile's own
  listed prefixes. Critically: a target the runtime can actually run — it resolves via
  `bcl.Lookup`/`bcl.LookupCtor`, a Machine-aware registry, or a local method — but that isn't on
  the profile's list is *still flagged*, as `out-of-profile` rather than `unsupported`, because it
  would genuinely run today but isn't part of what that profile promises its callers.

### 2.1 `minimal` — static methods and primitives only

Spec §24.1's design intent: "for basic testing," supporting static methods, `int`/`bool`/`string`,
arithmetic, branches, `return`. In code, `objectOpcodesAllowed(ProfileMinimal)` returns `false`,
and `instrIsObjectModel` rejects an entire method the instant it uses `newobj`, `callvirt`, field
load/store, `throw`, arrays (`newarr`/`ldlen`/`ldelem`/`stelem`), or static fields — no matter how
small the rest of the method is. One finding covers the whole method, not one per instruction,
because under `minimal` the method can't run at all once it touches any of these, regardless of
which specific object-model instruction triggered it.

Deliberately **not** excluded: `ldarga`/`ldloca`/`ldind`/`stind` (address-of/indirect load-store on
a local or argument). A `ref`/`out` *primitive* parameter never touches the heap or a type's field
layout, so it stays inside `minimal`'s promise even though it looks structurally unusual next to
"static methods and primitives."

Its BCL allowlist is narrow and explicit: `System.Math`, `System.BitConverter`, a handful of
`RuntimeHelpers`/`MemoryMarshal` members, `System.Console`, most of `System.String`'s common
members (`Concat`, `Format`, `Substring`, `get_Chars`, `Equals`, `op_Equality`, `Join`,
`get_Length`), `System.Double::IsNaN`, `System.Activator::CreateInstance`, `System.Xml.XmlQualifiedName`,
and `System.Object::.ctor` — the last one needed structurally, since every value type's implicit
base-constructor call routes through it even under a "no object model" profile.

**Worked example.** `internal/checker/analyzer_test.go`'s `TestAnalyze_MinimalProfileFlagsObjectModel`
locks this behavior in against a real fixture assembly:

- `Vmnet.Fixtures.Customer::get_Name`, `Vmnet.Fixtures.Arrays::SumArray`,
  `Vmnet.Fixtures.Statics::GetInitValue`, and `Vmnet.Fixtures.Statics::IncrementAndGet` are all
  asserted to produce a `KindOutOfProfile` finding under `minimal` — a property getter needs a
  field/`callvirt`, `SumArray` needs `newarr`/`ldelem`, and both `Statics` methods touch static
  fields.
- `Vmnet.Fixtures.ByRef::CallIncrementTwice` — a method using only `ref`/`out` `int` parameters —
  is asserted to produce **zero** findings under `minimal`, confirming ref/out primitives stay
  in-profile even here.

### 2.2 `rules` — business rules: real objects, collections, LINQ, exceptions

Spec §24.2's design intent: "for business rules" — classes, objects, strings, arrays, `List<T>`,
`Dictionary<string, object>`, exceptions, `DateTime`, `Guid`, JSON helpers. In code this is
`bclPrefixes[ProfileMinimal]` plus everything appended in `profile.go`'s `init()` — the full
object/collection/exception/text surface built up across many roadmap phases: `List<T>`,
`Dictionary<K,V>`, `HashSet<T>`, `SortedSet<T>`, `Stack<T>`, `Queue<T>`, `LinkedList<T>`, the
`IEnumerable`/`IEnumerator`/`ICollection`/`IList`/`IDictionary` interface families, primitive
struct methods (`Int32`, `Char`, `Boolean`, `Single`, `Double`, ...), a wide
`System.Linq.Enumerable` surface plus `System.Linq.Expressions.*` (expression trees), `Regex`,
`Task`/async machinery (`AsyncTaskMethodBuilder`, `TaskAwaiter`, ...), `System.Data`/`System.Data.Common`
ADO.NET abstractions (the surface Dapper-style micro-ORMs run against), `System.IO` (`File`,
`Directory`, `FileStream`, `MemoryStream`, `Stream`, permission-gated per
`internal/interpreter/permissions.go`), reflection (`Type`, `MethodInfo`, `PropertyInfo`,
`ParameterInfo`, `ConstructorInfo`, `CustomAttributeData`, ...), `System.Xml`/`System.Xml.Linq`,
`System.Uri`, `System.Guid`, `System.DateTime`/`DateTimeOffset`, and more. `objectOpcodesAllowed`
returns `true` for `rules`, so the object model itself is fully permitted.

### 2.3 `netstandard-lite` — pure NuGet packages

Spec §24.3's design intent: "for pure NuGet" — an extended BCL, collections, a LINQ subset,
`Text.Encoding`, `MemoryStream`, basic `CultureInfo`, reflection-lite. In the *current* code,
though, `netstandard-lite`'s allowlist is defined as exactly `rules`'s own list, copied wholesale.
`profile.go`'s own comment is explicit about this:

> "netstandard-lite currently promises exactly the same BCL surface as rules (System.Type moved
> into `rules` in Fase 3.14, System.Convert in Fase 3.18, once each had real natives behind it) —
> kept as its own profile/slice rather than collapsed into one, so a future rules-only addition
> doesn't have to be reconsidered for both tiers by construction."

In other words: today, `rules` and `netstandard-lite` are behaviorally identical allowlists. The
split exists so the two can diverge in the future without a structural rewrite, not because one is
currently stricter than the other. It's worth stating this plainly rather than implying a
difference the shipped code doesn't have.

`netstandard-lite` is nonetheless the profile this project actually uses to measure real-world
NuGet packages (§5–6 below), and it's `vmnet check package`'s own default (§3).

## 3. Running the checker

Two entry points, both in `cmd/vmnet/main.go`:

```
vmnet check [--profile=minimal|rules|netstandard-lite] <dll>
vmnet check package [--profile=...] <id>@<version>
```

- **`vmnet check <dll>`** defaults to `--profile=rules` when the flag is omitted (`profile :=
  checker.ProfileRules` in `runCheck`), and calls `checker.Analyze(f, md, profile)` — a single
  assembly, with no dependency graph attached.
- **`vmnet check package <id>@<version>`** defaults to `--profile=netstandard-lite` when the flag
  is omitted (`profile := checker.ProfileNetStandardLite` in `runCheckPackage`). It resolves the
  package's own selected target asset, resolves its **full transitive dependency graph** via
  `nuget.NewResolver(...).Resolve(...)`, fetches and parses every dependency's own metadata, prints
  `Dependencies resolved: N`, and calls `checker.AnalyzeWithDeps(f, md, deps, profile)` — not the
  plain `Analyze`.
- Both paths validate the profile string through `validateProfile`: only `minimal`, `rules`, and
  `netstandard-lite` are accepted. Anything else is a hard error
  (`unknown profile %q (want minimal, rules or netstandard-lite)`), never a silent fallback.
- Either command exits with status 1 if the resulting report's `Status` isn't `compatible`.

**Why `AnalyzeWithDeps` matters, precisely.** `analyzer.go`'s own doc comment states the design
goal directly:

> "Analyze walks every method vmnet's pipeline could plausibly execute and tries the exact same
> steps Assembly.Call would (IL decode, IR build, call-target resolution) — so a 'compatible'
> verdict means 'this will actually run', not a separate heuristic's guess."

`AnalyzeWithDeps` extends that same guarantee across package boundaries. When a call or
constructor target doesn't resolve against the package's own metadata, `checkTarget` tries it
against each transitive dependency's metadata before flagging it. A real package's IL frequently
calls straight into a dependency's own types — the doc comment's own examples are Jint calling into
Esprima, and NPOI calling into ZString, SkiaSharp, and BouncyCastle.Cryptography — and those calls
genuinely run once `vm.LoadPackage` attaches the resolved dependency chain at runtime, mirroring
what `Assembly.WithDependencies` does. Flagging such a call as unsupported would be a false
negative, not a real gap. `deps` is meant to be the package's **full** transitive dependency graph
(e.g. via `internal/nuget.Resolver`), not just its direct dependencies — that's the whole point of
the mechanism.

## 4. The Report shape

`internal/checker/report.go` defines:

```go
type Report struct {
	AssemblyName    string
	Profile         Profile
	MethodsAnalyzed int
	MethodsFlagged  int
	Findings        []Finding
	Status          Status
}

type Finding struct {
	Kind       FindingKind
	Method     string // "Namespace.Type::Method" where this was found ("" for assembly-wide findings)
	Detail     string // the opcode, the unresolved call target, ...
	Suggestion string
}
```

### 4.1 Status — the exact `finalize()` rule

```go
func (r *Report) finalize() {
	switch {
	case len(r.Findings) == 0:
		r.Status = StatusCompatible
	case r.MethodsAnalyzed == 0 || r.MethodsFlagged >= r.MethodsAnalyzed:
		r.Status = StatusUnsupported
	default:
		r.Status = StatusPartial
	}
}
```

Read in order, this is exactly:

1. **`compatible`** — if and only if `len(r.Findings) == 0`. Not "zero methods flagged": literally
   no `Finding` at all, including assembly-wide ones with no associated method (a `KindPInvoke`
   finding from a present `ImplMap` table, for instance).
2. **`unsupported`** — if either `MethodsAnalyzed == 0` (no analyzable method body existed at all —
   e.g. a stub assembly) **or** `MethodsFlagged >= MethodsAnalyzed` (every single analyzed method
   was flagged at least once). Note the `>=`, not `==`: it's a safety bound against
   `MethodsFlagged` somehow exceeding `MethodsAnalyzed`, not a strict equality check.
3. **`partial`** — the default case: some findings exist, but not every analyzed method was
   flagged.

There is no percentage field anywhere in `Report`, and `printReport` (`cmd/vmnet/main.go`) never
computes or prints one — it prints `MethodsAnalyzed`/`MethodsFlagged` as raw integers. "X%
compatible" is a number this project computes and states in prose (`(MethodsAnalyzed -
MethodsFlagged) / MethodsAnalyzed`, see §6), not something the tool itself emits.

### 4.2 FindingKind — all 7 categories, and their real suggestion text

```go
const (
	KindUnsupportedOpcode FindingKind = "unsupported-opcode"
	KindUnsupportedMethod FindingKind = "unsupported-bcl-method"
	KindReflection        FindingKind = "reflection"
	KindAsync             FindingKind = "async"
	KindPInvoke           FindingKind = "p-invoke"
	KindUnsafePointer     FindingKind = "unsafe-pointer"
	KindOutOfProfile      FindingKind = "out-of-profile"
)
```

- **`unsupported-opcode`** — IL decode or IR-build failure: an opcode `ir.Build` has no IR
  translation for (`ir.UnsupportedOpcodeError`), an unparseable method signature, or a
  body/header/exception-handler read failure. Suggestion is opcode-specific where known: for
  `ldtoken`, "array literal initializers (RuntimeHelpers.InitializeArray) are not supported yet —
  assign elements individually instead"; for a `catch (T) when (cond)` filter clause, "exception
  filter clauses (catch (T) when (cond)) are not supported yet — catch (T) without the filter is";
  otherwise a generic "not yet implemented — see docs/en/ROADMAP.md".
- **`out-of-profile`** — two shapes: (a) the whole-method object-model rejection under `minimal`
  ("uses the object model (classes/fields/callvirt/throw), not part of this profile" /
  `suggestion: use profile "rules" or "netstandard-lite"`), or (b) a per-call finding where the
  target resolves (the runtime *can* run it) but isn't listed in the active profile's `bclPrefixes`
  and isn't a local method. The per-call case carries no canned suggestion — `Detail` is simply the
  unresolved-but-runnable target's full name.
- **`unsupported-bcl-method`** — the fallback category (`categorize`'s default branch): a
  call/constructor target that resolves against neither the package's own metadata nor any
  dependency in `deps`, and isn't reflection- or Task-shaped by name. Suggestion: "this BCL method
  has no native implementation yet."
- **`reflection`** — an unresolved target under the `System.Reflection.*` namespace. Suggestion:
  "avoid reflection-heavy code paths; only typeof/GetType/Type.Name are supported." This wording
  predates the current, much wider real reflection surface (`GetConstructor`/`GetMethod`/
  `GetField`/`GetMember`, `PropertyInfo`, `ParameterInfo`, `CustomAttributeData`, and more, all
  resolved through the Machine-aware `reflectionMachineTargets` registry) — it only fires for
  whatever specific reflection call genuinely doesn't resolve, so treat it as accurate about that
  one call, not about reflection support as a whole.
- **`async`** — an unresolved target under `System.Threading.Tasks.*`. Suggestion: "avoid
  async/Task — vmnet has no async runtime yet." Same caveat as `reflection`: real `Task`,
  `AsyncTaskMethodBuilder`, and awaiter machinery exists and works under `rules`/`netstandard-lite`
  today; this only fires on whatever specific Task-shaped call genuinely fails to resolve.
- **`p-invoke`** — assembly-wide, not per-method: emitted once if the assembly's `ImplMap`
  metadata table has any rows at all (i.e. it declares any P/Invoke method). Suggestion: "P/Invoke
  is not supported in pure-Go mode."
- **`unsafe-pointer`** — a method's return type or any parameter type is an unmanaged pointer
  (`SigPointer` — real `unsafe` C#). No suggestion text is set for this kind.

## 5. Worked example: `fluentvalidation@11.9.2`

`docs/en/COMPATIBILITY.md`'s own currently-measured number: `FluentValidation@11.9.2` →
**98.1% (1,289 methods, 25 flagged)**, measured under `--profile=netstandard-lite`. Reproduce it
yourself:

```bash
go build -o vmnet ./cmd/vmnet
./vmnet check package --profile=netstandard-lite fluentvalidation@11.9.2
```

Given `printReport`'s exact current formatting and these real, published numbers, the header of
the output is exactly:

```
FluentValidation
Status: partial
Profile: netstandard-lite
Methods analyzed: 1289
Methods flagged: 25

Findings:
...
```

Every field and label above (`Status:`, `Profile:`, `Methods analyzed:`, `Methods flagged:`,
`Findings:`) is the real, exact text `printReport` emits, and `1289`/`25` are the real, current
counts from COMPATIBILITY.md. `Status` is `partial`, not `unsupported`, because `finalize()`'s rule
only escalates to `unsupported` when `MethodsFlagged >= MethodsAnalyzed` — 25 out of 1,289 is far
below that bound. COMPATIBILITY.md does not publish the 25 findings' individual method/target names
verbatim, only their documented root causes: two same-named, same-arity `IsValid` overloads across
a generic base/derived validator pair that vmnet's by-name ancestor walk used to conflate (fixed in
Fase 3.68 with a general overload-resolution rule), and one remaining, narrower gap — a boxed
value-type argument whose value equals its type's zero (e.g. boxed `0`) is indistinguishable from a
real `null` by vmnet's identity-passthrough `box`, so an `x?.ToString()`-style null-conditional
check on such a value is misevaluated. Each of those would show up as one or more
`out-of-profile`/`unsupported-bcl-method` findings in the real `Findings:` list — the two lines
shown above (`Status`/`Profile`/counts) are the part of this transcript that's exact; the specific
per-finding lines are not reproduced here because they aren't published verbatim anywhere in this
project's docs.

## 6. How to read the percentage, honestly

This project's own established position, stated in `docs/en/COMPATIBILITY.md`, applies here too —
the checker percentage is `(methods with zero findings) / (methods analyzed)`, and it is
**a coverage estimate, not a correctness proof**: a method can have zero findings and still behave
subtly wrong if a native implementation has a real bug the checker has no way to see (that's what
end-to-end demos are for, not something profile checking can substitute for).

The same document also argues, in its own words, that **the per-package number matters more than
any corpus-wide average**: a simple or methods-weighted average across many packages can hide one
badly-covered package that breaks the moment someone actually depends on it, even while other
packages compensate for it in the mean. Its own stated working target is 97%+ **per package**, not
as an aggregate — as of that document, 5 of 19 measured packages meet that bar individually
(`DocumentFormat.OpenXml` 100.0%, `Humanizer.Core` 97.9%, `NPOI` 97.9%, `Ardalis.GuardClauses`
97.5%, `FluentValidation` 98.1%). Reading a single checker percentage for the specific package you
intend to load — always under `netstandard-lite`, always including its full transitive dependency
graph — is the number that predicts whether *that* package will run for *you*; a corpus-wide
average predicts nothing about any one package in particular.
