# Compatibility: 19 real packages, measured three separate ways

This document exists because a single number — "X% compatible" — hides more than it reveals. A
static checker score, a real running demo, and actual confidence in correctness are three
different things, and conflating them is exactly how a project ends up shipping something that
"looks" 97% ready but breaks the moment a real user runs it. This page keeps the three separate,
on purpose, for every package vmnet is measured against.

## The three columns, and what each one actually means

- **Checker %** — `internal/checker`'s static analyzer walks every method in the package (plus its
  full transitive dependency graph, resolved exactly the way `vm.LoadPackage` does at runtime) and
  reports, method by method, whether every BCL call/opcode it uses resolves against something
  vmnet actually implements, under the `netstandard-lite` profile. The percentage is `(methods
  with zero findings) / (methods analyzed)`. **This is a coverage estimate, not a correctness
  proof** — a method can have zero findings and still behave subtly wrong if a native
  implementation has a bug the checker has no way to see (that's what real demos are for, below).
  Reproduce any number here yourself: `vmnet check package --profile=netstandard-lite <id>@<version>`.
- **Real demo** — whether `examples/` has an actual, runnable program that loads the real,
  unmodified package from nuget.org and exercises its real code end to end, with output compared
  against real `dotnet run`/the real .NET SDK where applicable. This is the strongest signal vmnet
  has: it means someone actually ran this specific package's real logic and confirmed the output
  matches real .NET, not just that the checker didn't flag anything.
  Reproduce it yourself: `cd examples/<name> && go run .`.
- **Confidence** — a plain-language note on what you should actually conclude from the first two
  columns for this specific package, written to resist the temptation to round a high checker %
  up to "fully verified." Each package's own note lives in the "Confidence notes" subsection right
  after its table, not crammed into the table itself — long enough to want real line breaks and
  headings, not a single unbroken table cell.

## Packages with a real, running demo (strongest signal)

| Package | Checker % | Demo |
|---|---|---|
| `DocumentFormat.OpenXml@3.1.1` | 100.0% (67,234 methods, 7 flagged) | [`examples/openxml-demo`](../../examples/openxml-demo) |
| `NPOI@2.8.0` | 98.2% (14,202 methods, 249 flagged) | [`examples/npoi-demo`](../../examples/npoi-demo) |
| `System.Text.Json@8.0.5` | 98.2% (3,577 methods, 66 flagged) | [`examples/system-text-json-demo`](../../examples/system-text-json-demo) |
| `FluentValidation@11.9.2` | 98.2% (1,289 methods, 23 flagged) | [`examples/fluentvalidation-demo`](../../examples/fluentvalidation-demo) |
| `ClosedXML@0.105.0` | 97.6% (10,444 methods, 252 flagged) | [`examples/closedxml-demo`](../../examples/closedxml-demo) |
| `Jint@3.1.3` | 96.7% (5,414 methods, 178 flagged) | [`examples/jint-demo`](../../examples/jint-demo), [`examples/jint-nowrapper`](../../examples/jint-nowrapper), [`examples/jint-advanced-demo`](../../examples/jint-advanced-demo) |
| `Dapper@2.1.79` | 95.5% (1,047 methods, 47 flagged) | [`examples/dapper-demo`](../../examples/dapper-demo), [`examples/sqlite-demo`](../../examples/sqlite-demo) |
| `CsvHelper@33.1.0` | 94.8% (1,393 methods, 73 flagged) | [`examples/csvhelper-demo`](../../examples/csvhelper-demo) |
| `Microsoft.Extensions.DependencyInjection@8.0.0` | 94.1% (437 methods, 26 flagged) | [`examples/di-demo`](../../examples/di-demo) |
| `Newtonsoft.Json@13.0.3` | 89.2% (4,064 methods, 438 flagged) | [`examples/newtonsoft-json-demo`](../../examples/newtonsoft-json-demo) |

Six of these nine numbers moved up in Fase 3.79 — not because that Fase targeted these packages
directly (it was chasing Jint's own remaining gaps), but because several of its fixes are general
CIL/BCL correctness fixes (a `constrained.`-prefixed generic interface call never being
dereferenced, `conv.u8` sign- instead of zero-extending, `TimeSpan`'s comparison operators,
`StringBuilder.set_Capacity`/`ToString(start,length)`, and half a dozen `Regex`/`Span<T>` natives)
that plenty of other packages' own real code paths happen to hit too. Reproduced with the exact
same `vmnet check package` invocation documented below, right after Fase 3.79 landed.

### Confidence notes

#### `DocumentFormat.OpenXml@3.1.1`

**Verified.** Generates a real `.docx` from scratch; the output is round-tripped through the real,
unmodified .NET SDK to confirm it opens correctly — not just that vmnet produced *some* bytes.

#### `NPOI@2.8.0`

**Verified.** Reads a real legacy `.xls` binary file end to end (strings, numbers, a `SUM` formula
cell); one known, documented, cosmetic gap remains (formula cell-range text renders numeric code
points instead of column letters — cell *values* are correct).

Checker % up in Fase 3.74 (`IReadOnlyDictionary\`2`/`CancellationToken` profile fixes — see the
`ClosedXML`/`System.Text.Json` notes below for what those fixes were).

#### `System.Text.Json@8.0.5`

**Verified.** Parses real JSON through the real `JsonDocument` API, confirmed against real .NET
output.

Crossed the 97% bar in Fase 3.74: new natives for `ArraySegment\`1`, `Array.CopyTo`,
`Exception.Source`, `KeyNotFoundException`, and `ICollection\`1.IsReadOnly`, plus the same
`IReadOnlyDictionary\`2`/checker-profile fixes as `ClosedXML` below.

`JsonSerializer.Serialize`/`Deserialize` itself remains blocked by a separate, deeper, real gap
found in Fase 3.70 (an unsafe fixed-size buffer field) — this demo's own `JsonDocument`-based
parsing is a different, already-working API surface. Tracked as
[issue #4](https://github.com/arturoeanton/go-vmnet/issues/4) — see `docs/en/ROADMAP.md` Fase 3.70
for the full root-cause writeup.

#### `FluentValidation@11.9.2`

**Verified for real object validation, including numeric range validators.** A real validator
(`RuleFor`/`NotEmpty`/`WithMessage`/`GreaterThanOrEqualTo`) accepts a valid object and rejects an
invalid one with the correct error message.

Getting here needed `Expression<TDelegate>.Compile()` to genuinely work for a real (if narrow)
class of expression trees (Fase 3.64) — FluentValidation compiles and invokes the property-access
lambda to read the value being validated, not just inspects its shape.

Fase 3.66 correctly diagnosed (but did not yet fix) the numeric-validator dispatch bug: two
same-named, same-arity `IsValid` overloads across a generic base/derived class pair
(`AbstractComparisonValidator<T,TProperty>` and `GreaterThanOrEqualValidator<T,TProperty>`),
distinguishable in real .NET only by full signature/vtable slot, were being conflated by vmnet's
own by-name ancestor walk. **Fixed in Fase 3.68** with a general overload-resolution rule (two
positions declaring the same still-open generic parameter must bind to the same runtime `Kind`).

**One remaining, narrower, documented limitation**: a boxed value-type argument whose value equals
its type's zero (e.g. a boxed `int` holding `0`) is indistinguishable from a real null by vmnet's
identity-passthrough `box`, so `x?.ToString()`-style null-conditional checks on such a value
incorrectly treat it as null — hit by `InclusiveBetween`'s multi-placeholder message formatting
only when a bound is exactly `0`; the demo avoids this narrow edge.

#### `ClosedXML@0.105.0`

**Verified**, with one honest caveat: a tiny compiled C# wrapper supplies a minimal
`IXLGraphicEngine`, because ClosedXML's own default font-metrics engine hits a real, deep
architectural limitation (generic type-parameter substitution inside a generic class's own static
field initializers) unrelated to reading cell data itself. Reads a real `.xlsx` correctly; also the
subject of a real, fixed non-deterministic hang (Fase 3.44) — now stable across repeated runs.

**Crossed the 97% bar in Fase 3.74**: `IReadOnlyDictionary\`2` (a real `Dictionary\`2` receiver
dispatches to it identically to `IDictionary\`2`, verified with a real round-trip test) accounted
for the largest single chunk of what was flagged.

Up again in Fase 3.82: `System.Net.Http.HttpClient.GetAsync`/`HttpResponseMessage`/
`HttpContent.ReadAs*Async` — the exact real surface an internal netstandard2.0 polyfill shim
(`PolyfillExtensions`) needs — went from unsupported to real, gated by `Permissions.AllowNetwork`.

#### `Jint@3.1.3`

**Verified.** Runs a real JavaScript engine end to end — parses real JS source, builds a real AST,
evaluates it, and returns a real result — both through a compiled wrapper and with zero C# glue at
all. The strongest evidence vmnet handles genuinely non-trivial, deeply object-oriented real-world
code, not just small static-method libraries.

Fase 3.77-3.79 took this from three whole documented-broken classes of real JavaScript (function
declarations, array growth/string methods, ES6 classes/`.concat`/`.map`/`JSON.stringify`/regex) down
to one narrower remaining gap (regex parenthesized groups and `\d`/`\w`/`\s` shorthand classes) —
`examples/jint-advanced-demo` is the running proof, exercising closures, recursion, arrow functions,
array higher-order methods, ES6 inheritance with `super`, regex `.test`/`.exec`/`.match`/`.replace`,
`JSON.stringify` on real nested multi-digit data, and template literals, all in one script. See
`docs/en/ROADMAP.md`'s Fase 3.77/3.78/3.79 entries for the complete, citable account.

#### `Dapper@2.1.79`

**Verified, two ways.** `dapper-demo` runs Dapper's real `SqlMapper.Query`/`Execute` against a fake
in-memory ADO.NET provider; `sqlite-demo` runs the identical real Dapper code against vmnet's own
real, Go-native `Microsoft.Data.Sqlite` provider, then independently re-opens the resulting `.db`
file with the real `sqlite3` CLI and runs `PRAGMA integrity_check`.

Two known, permanent, documented architectural gaps remain (a generic-method `typeof(T)`
limitation, and a Dapper regex feature Go's RE2 engine can never compile) — see
`docs/en/ROADMAP.md` Fase 3.52/3.53.

#### `CsvHelper@33.1.0`

**Verified.** `csvhelper-demo` runs `CsvReader.GetRecords<T>()` with **no `ClassMap` registered at
all**, forcing CsvHelper's own reflection-only `AutoMap()` path (`Type.GetConstructor(s)`,
`Expression.New`/`Lambda`/`Compile`) to construct the record type and every member map purely at
runtime — the exact gap this doc had previously flagged as "not yet a working demo," fixed for real
in Fase 3.81 (eight distinct bugs: closed-generic identity across `Type.GetConstructor()`/
`ConstructorInfo.Invoke()`, the same identity lost the inverse way at construction time, a
compiler-generated iterator's class-level generic parameter not surviving a forwarded generic-method
call, the same sentinel not surviving one level of nesting inside a closed generic type,
`System.String.Join` given a real plugin `IEnumerable<string>`, and several missing
`System.Linq.Expressions` factories). Up again in Fase 3.83: `new List<T>(somePluginIEnumerable)`
(previously constructing a silently-empty list) and `Single.TryParse`
(`CsvHelper.TypeConversion.SingleConverter`'s own entire `ConvertFromString` body) both fixed —
`csvhelper-demo`'s own wrapper now uses `new List<Product>(csv.GetRecords<Product>())` directly
instead of a manual `foreach`/`Add()` loop.

#### `Microsoft.Extensions.DependencyInjection@8.0.0`

**Verified for real constructor injection** — Microsoft's own official DI container resolves a
service whose constructor depends on another registered service, through its real
`ServiceCollection`/`ServiceProvider`/`GetRequiredService<T>()` API, unmodified. Getting here
required three real interpreter fixes (Fase 3.60): a method-overload-resolution tie-break causing
an infinite self-recursion, `typeof(T)` never resolving on a generic method's own open type
parameter, and a cross-package reflection gap.

**Still not verifiable in practice**: `DependencyInjection`'s own compiled-expression-tree fast
path (`ExpressionResolverBuilder`) — Fase 3.65 built the general expression-tree evaluator this
needs, but reading the real IL shows the fast path is a background, best-effort optimization
(`ThreadPool`-queued after a service's 2nd resolution, with any compile failure silently swallowed)
that behaves identically to a real caller whether it succeeds or not; `di-demo` exercises the
OTHER, always-active resolution path (`CallSiteRuntimeResolver`), which doesn't need
`Expression.Compile()` at all.

#### `Newtonsoft.Json@13.0.3`

**Verified for the demonstrated path** (real "LINQ to JSON" DOM parsing and indexer access), but
the lowest checker % of any package with a demo — its `Dynamic`/`ExpandoObject`-based
dynamic-typing surface (`JValue+JValueDynamicProxy`) is a real, unimplemented gap the demo doesn't
exercise. Don't read the demo passing as "this whole package works." Tracked as
[issue #3](https://github.com/arturoeanton/go-vmnet/issues/3).

Ticked up slightly in Fase 3.83 (the same `List<T>`/`ArrayList` constructor fix as `CsvHelper`'s
own entry above).

## Packages measured by the checker only (no demo yet)

No demo existing yet is not a red flag by itself — every one of the packages above started here
too. It does mean nobody has yet run this specific package's real code end to end and compared the
output against real .NET; treat the percentage as a coverage estimate of what *would* likely work,
not confirmation that it does.

| Package | Checker % |
|---|---|
| `Ardalis.GuardClauses@5.0.0` | 98.6% (285 methods, 4 flagged) |
| `Humanizer.Core@2.14.1` | 98.4% (1,597 methods, 25 flagged) |
| `Polly@8.7.0` | 97.0% (2,049 methods, 61 flagged) |
| `YamlDotNet@18.1.0` | 96.2% (2,182 methods, 82 flagged) |
| `Serilog@4.3.1` | 96.1% (1,115 methods, 43 flagged) |
| `MediatR@14.2.0` | 95.5% (441 methods, 20 flagged) |
| `NodaTime@3.3.2` | 94.8% (3,098 methods, 160 flagged) |
| `AutoMapper@16.2.0` | 94.3% (2,319 methods, 133 flagged) |
| `SimpleBase@4.0.0` | 92.6% (258 methods, 19 flagged) |
| `Semver@2.3.0` | 92.9% (423 methods, 30 flagged) |

### Confidence notes

#### `Ardalis.GuardClauses@5.0.0`, `Humanizer.Core@2.14.1`

High coverage estimate; unverified by a real run.

#### `Polly@8.7.0`

High coverage estimate; unverified by a real run. Up in Fase 3.74 — `CancellationToken` had real
natives since well before this Fase but no checker-profile allowlist entry at all. **Crossed the
97% bar in Fase 3.79** on the same general `TimeSpan`/`Regex`/`conv.u8`/`constrained.` fixes that
moved several other packages in this doc — Polly's own retry/circuit-breaker timing logic leans on
`TimeSpan` comparisons.

#### `YamlDotNet@18.1.0`

Good coverage estimate; unverified by a real run.

#### `Serilog@4.3.1`

Good coverage estimate; unverified by a real run. Up in Fase 3.74 (`CancellationToken` profile
fix); up again in Fase 3.79 (the same general `TimeSpan`/`Regex`/`Span<T>` fixes as `Polly` above).

#### `MediatR@14.2.0`

Moderate coverage estimate; unverified by a real run. Up in Fase 3.74 (`CancellationToken` profile
fix).

#### `NodaTime@3.3.2`, `SimpleBase@4.0.0`, `Semver@2.3.0`

Good-to-high coverage estimate; unverified by a real run. `NodaTime`'s own number moved slightly in
Fase 3.79 (`TimeSpan`/`conv.u8`) and again in Fase 3.83 (the same `List<T>`/`ArrayList` constructor
fix as `CsvHelper`'s own entry above).

#### `AutoMapper@16.2.0`

Fase 3.66 root-caused and fixed the Fase 3.65 `ValueTuple` NRE for real (a general
`Enumerable.FirstOrDefault/LastOrDefault/SingleOrDefault<T>` typed-default gap) AND fixed a real,
deep TypeMap-registration bug (`typeof(TSource)`/`typeof(TDestination)` never resolving inside a
generic class's own instance methods — a genuinely new, general capability, class-level generic
type parameter tracking). Up again in Fase 3.83 (the same `List<T>`/`ArrayList` constructor fix).

A real, unmodified `AutoMapper` now gets past its own static initialization, reflection layer,
constructor-selection machinery, AND the TypeMap-registration step — but its real
`Mapper.Map<T>(source)` call hits a new, deeper wall: its own compiled mapping-plan expression tree
recurses far beyond a safety limit added this Fase specifically to convert what used to be a raw
process crash into a graceful error — see `docs/en/ROADMAP.md` Fase 3.66's own "Found, not fixed"
section. Tracked as [issue #1](https://github.com/arturoeanton/go-vmnet/issues/1).

Not yet a working demo. Checker % up slightly in Fase 3.79 (the same general `conv.u8`/
`constrained.` fixes as several other packages in this doc).

## Aggregate numbers, and why the per-package number matters more

- **Simple average across all 19 packages: 95.9%** (up from 95.8% before Fase 3.79's own general
  CIL/BCL fixes — `constrained.` receiver dereferencing, `conv.u8` zero-extension, `TimeSpan`
  comparison operators, and half a dozen `Regex`/`StringBuilder`/`Span<T>` natives — moved twelve
  of the nineteen; up from 94.45% before Fase 3.74's own corpus-wide sweep before that. See
  `docs/en/ROADMAP.md` for each Fase's own methodology).
- **Methods-weighted average: ~98.4%** — but this is dominated by `DocumentFormat.OpenXml`'s own
  67,234 analyzed methods (55% of every method analyzed across all 19 packages combined) sitting
  at 100%. A weighted average answers "what fraction of all analyzed method calls across this
  whole corpus resolve," which is a real number but not the one that predicts whether *your*
  specific package will work — the **per-package number above is the one that matters** for that.
- The working target for every package here is **97%+, individually** — not a corpus-wide
  average. An average can hide a badly-covered package that breaks the moment someone actually
  depends on it, even while other packages compensate for it in the mean.

As of this writing, 8 of 19 packages are at or above that bar:

| Package | Checker % |
|---|---|
| `DocumentFormat.OpenXml` | 100.0% |
| `Ardalis.GuardClauses` | 98.6% |
| `Humanizer.Core` | 98.4% |
| `NPOI` | 98.2% |
| `System.Text.Json` | 98.2% |
| `FluentValidation` | 98.2% |
| `ClosedXML` | 97.5% (crossed the bar in Fase 3.74) |
| `Polly` | 97.0% (crossed the bar in Fase 3.79) |

The rest are active hardening targets, prioritized by how far below 97% they sit and by how much
real-world usage they represent. `Jint` (96.7%) and `YamlDotNet`/`Serilog` (96.2%/96.1%) are the
closest of the remaining eleven.

## The `Microsoft.Extensions.*` family — official Microsoft frameworks, a separate, ongoing measurement

Distinct from the 19-package corpus above (this project's own long-running compatibility target),
Fase 3.60 started measuring official Microsoft `Microsoft.Extensions.*` packages specifically —
the modern .NET building blocks (dependency injection, configuration, logging, options, caching)
every ASP.NET Core and worker-service app is built on. Checker %, `netstandard-lite` profile, full
transitive deps, refreshed after Fase 3.79 (most of this family moved — the same general
`constrained.`/`conv.u8`/`TimeSpan`/`Regex`/`StringBuilder`/`Span<T>` fixes as the main 19-package
corpus above):

| Package | Checker % |
|---|---|
| `Microsoft.Extensions.Configuration.Abstractions@8.0.0` | 100.0% |
| `Microsoft.Extensions.Options.ConfigurationExtensions@8.0.0` | 100.0% |
| `Microsoft.Extensions.Options@8.0.0` | 99.7% |
| `Microsoft.Extensions.Caching.Abstractions@8.0.0` | 99.2% (up from 95.9%) |
| `Microsoft.Extensions.Logging@8.0.0` | 99.6% (up from 98.1%) |
| `Microsoft.Extensions.Configuration.Json@8.0.0` | 98.8% |
| `Microsoft.Extensions.Logging.Abstractions@8.0.0` | 98.8% (up from 97.8%) |
| `Microsoft.Extensions.Configuration@8.0.0` | 98.8% (up from 97.2%) |
| `Microsoft.Extensions.Primitives@8.0.0` | 98.3% (up from 96.9%) |
| `Microsoft.Extensions.Configuration.EnvironmentVariables@8.0.0` | 96.1% (down from 98.0% — an
  unrelated, pre-existing `System.Environment.GetEnvironmentVariables`/`IDictionary` enumeration
  gap; not touched by Fase 3.79, and not a regression it caused) |
| `Microsoft.Extensions.Configuration.FileExtensions@8.0.0` | 95.9% |
| `System.ComponentModel.Annotations@5.0.0` | 95.8% (up from 94.1%) |
| `Microsoft.Extensions.DependencyInjection.Abstractions@8.0.0` | 95.5% (up from 94.0%) |
| `Microsoft.Extensions.DependencyInjection@8.0.0` | 94.1% (**verified with a real demo**, above) |
| `Microsoft.Extensions.Logging.Console@8.0.0` | 93.6% (up from 90.6%) |
| `Microsoft.Extensions.Caching.Memory@8.0.0` | 92.6% (up from 87.3%) |
| `Microsoft.Extensions.Configuration.Binder@8.0.0` | 90.1% (up from 89.4%) |

Simple average: 96.9% (up from 95.5%). `DependencyInjection`'s own real end-to-end demo (see above)
is the strongest proof so far: a real, unmodified official package running its actual
constructor-injection logic, not just a static estimate. The rest of this family is next in line
for the same real-run treatment.

## Methodology and reproducibility

Every checker percentage above was measured freshly against the exact package/version listed,
including that package's own full transitive dependency graph (resolved the same way
`vm.LoadPackage` resolves it at runtime — a dependency's own real code is not misreported as
unsupported just because only the top-level package's own DLL was decoded). Reproduce any single
number:

```bash
go build -o vmnet ./cmd/vmnet
./vmnet check package --profile=netstandard-lite <PackageId>@<Version>
```

Every real demo listed above is runnable directly: `cd examples/<name> && go run .` — most need no
.NET SDK installed at all; a few (where a small, dev-time-only compiled C# wrapper is involved,
noted in each demo's own `README.md`) need `dotnet build` run once first. See
`docs/en/ROADMAP.md` for the full, phase-by-phase history of every bug found and fixed getting each
of these numbers to where they are today — nothing here is swept under the rug.
