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
  up to "fully verified."

## Packages with a real, running demo (strongest signal)

| Package | Checker % | Demo | Confidence |
|---|---|---|---|
| `DocumentFormat.OpenXml@3.1.1` | 100.0% (67,234 methods, 7 flagged) | [`examples/openxml-demo`](../../examples/openxml-demo) | **Verified.** Generates a real `.docx` from scratch; the output is round-tripped through the real, unmodified .NET SDK to confirm it opens correctly — not just that vmnet produced *some* bytes. |
| `NPOI@2.8.0` | 98.2% (14,202 methods, 256 flagged) | [`examples/npoi-demo`](../../examples/npoi-demo) | **Verified.** Reads a real legacy `.xls` binary file end to end (strings, numbers, a `SUM` formula cell); one known, documented, cosmetic gap remains (formula cell-range text renders numeric code points instead of column letters — cell *values* are correct). Checker % up in Fase 3.74 (`IReadOnlyDictionary`2`/`CancellationToken` profile fixes below). |
| `Dapper@2.1.79` | 95.4% (1,047 methods, 48 flagged) | [`examples/dapper-demo`](../../examples/dapper-demo), [`examples/sqlite-demo`](../../examples/sqlite-demo) | **Verified, two ways.** `dapper-demo` runs Dapper's real `SqlMapper.Query`/`Execute` against a fake in-memory ADO.NET provider; `sqlite-demo` runs the identical real Dapper code against vmnet's own real, Go-native `Microsoft.Data.Sqlite` provider, then independently re-opens the resulting `.db` file with the real `sqlite3` CLI and runs `PRAGMA integrity_check`. Two known, permanent, documented architectural gaps remain (a generic-method `typeof(T)` limitation, and a Dapper regex feature Go's RE2 engine can never compile) — see `docs/en/ROADMAP.md` Fase 3.52/3.53. |
| `ClosedXML@0.105.0` | 97.5% (10,444 methods, 266 flagged) | [`examples/closedxml-demo`](../../examples/closedxml-demo) | **Verified**, with one honest caveat: a tiny compiled C# wrapper supplies a minimal `IXLGraphicEngine`, because ClosedXML's own default font-metrics engine hits a real, deep architectural limitation (generic type-parameter substitution inside a generic class's own static field initializers) unrelated to reading cell data itself. Reads a real `.xlsx` correctly; also the subject of a real, fixed non-deterministic hang (Fase 3.44) — now stable across repeated runs. **Crossed the 97% bar in Fase 3.74**: `IReadOnlyDictionary\`2` (a real `Dictionary\`2` receiver dispatches to it identically to `IDictionary\`2`, verified with a real round-trip test) accounted for the largest single chunk of what was flagged. |
| `System.Text.Json@8.0.5` | 98.1% (3,577 methods, 69 flagged) | [`examples/system-text-json-demo`](../../examples/system-text-json-demo) | **Verified.** Parses real JSON through the real `JsonDocument` API, confirmed against real .NET output. **Crossed the 97% bar in Fase 3.74** (`ArraySegment\`1`, `Array.CopyTo`, `Exception.Source`, `KeyNotFoundException`, `ICollection\`1.IsReadOnly`, and the same `IReadOnlyDictionary\`2`/profile fixes as ClosedXML). `JsonSerializer.Serialize`/`Deserialize` itself remains blocked by a separate, deeper, real gap found in Fase 3.70 (an unsafe fixed-size buffer field) — this demo's own `JsonDocument`-based parsing is a different, already-working API surface. |
| `Jint@3.1.3` | 96.4% (5,414 methods, 193 flagged) | [`examples/jint-demo`](../../examples/jint-demo), [`examples/jint-nowrapper`](../../examples/jint-nowrapper) | **Verified.** Runs a real JavaScript engine end to end — parses real JS source, builds a real AST, evaluates it, and returns a real result — both through a compiled wrapper and with zero C# glue at all. The strongest evidence vmnet handles genuinely non-trivial, deeply object-oriented real-world code, not just small static-method libraries. |
| `Newtonsoft.Json@13.0.3` | 89.2% (4,064 methods, 441 flagged) | [`examples/newtonsoft-json-demo`](../../examples/newtonsoft-json-demo) | **Verified for the demonstrated path** (real "LINQ to JSON" DOM parsing and indexer access), but the lowest checker % of any package with a demo — its `Dynamic`/`ExpandoObject`-based dynamic-typing surface (`JValue+JValueDynamicProxy`) is a real, unimplemented gap the demo doesn't exercise. Don't read the demo passing as "this whole package works." |
| `Microsoft.Extensions.DependencyInjection@8.0.0` | 94.1% (437 methods, 26 flagged) | [`examples/di-demo`](../../examples/di-demo) | **Verified for real constructor injection** — Microsoft's own official DI container resolves a service whose constructor depends on another registered service, through its real `ServiceCollection`/`ServiceProvider`/`GetRequiredService<T>()` API, unmodified. Getting here required three real interpreter fixes (Fase 3.60): a method-overload-resolution tie-break causing an infinite self-recursion, `typeof(T)` never resolving on a generic method's own open type parameter, and a cross-package reflection gap. **Still not verifiable in practice**: `DependencyInjection`'s own compiled-expression-tree fast path (`ExpressionResolverBuilder`) — Fase 3.65 built the general expression-tree evaluator this needs, but reading the real IL shows the fast path is a background, best-effort optimization (`ThreadPool`-queued after a service's 2nd resolution, with any compile failure silently swallowed) that behaves identically to a real caller whether it succeeds or not; `di-demo` exercises the OTHER, always-active resolution path (`CallSiteRuntimeResolver`), which doesn't need `Expression.Compile()` at all. |
| `FluentValidation@11.9.2` | 98.1% (1,289 methods, 24 flagged) | [`examples/fluentvalidation-demo`](../../examples/fluentvalidation-demo) | **Verified for real object validation, including numeric range validators.** A real validator (`RuleFor`/`NotEmpty`/`WithMessage`/`GreaterThanOrEqualTo`) accepts a valid object and rejects an invalid one with the correct error message. Getting here needed `Expression<TDelegate>.Compile()` to genuinely work for a real (if narrow) class of expression trees (Fase 3.64) — FluentValidation compiles and invokes the property-access lambda to read the value being validated, not just inspects its shape. Fase 3.66 correctly diagnosed (but did not yet fix) the numeric-validator dispatch bug: two same-named, same-arity `IsValid` overloads across a generic base/derived class pair (`AbstractComparisonValidator<T,TProperty>` and `GreaterThanOrEqualValidator<T,TProperty>`), distinguishable in real .NET only by full signature/vtable slot, were being conflated by vmnet's own by-name ancestor walk. **Fixed in Fase 3.68** with a general overload-resolution rule (two positions declaring the same still-open generic parameter must bind to the same runtime `Kind`). **One remaining, narrower, documented limitation**: a boxed value-type argument whose value equals its type's zero (e.g. a boxed `int` holding `0`) is indistinguishable from a real null by vmnet's identity-passthrough `box`, so `x?.ToString()`-style null-conditional checks on such a value incorrectly treat it as null — hit by `InclusiveBetween`'s multi-placeholder message formatting only when a bound is exactly `0`; the demo avoids this narrow edge. |

## Packages measured by the checker only (no demo yet)

No demo existing yet is not a red flag by itself — every one of the packages above started here
too. It does mean nobody has yet run this specific package's real code end to end and compared the
output against real .NET; treat the percentage as a coverage estimate of what *would* likely work,
not confirmation that it does.

| Package | Checker % | Confidence |
|---|---|---|
| `Humanizer.Core@2.14.1` | 98.3% (1,597 methods, 28 flagged) | High coverage estimate; unverified by a real run. |
| `Ardalis.GuardClauses@5.0.0` | 98.6% (285 methods, 4 flagged) | High coverage estimate; unverified by a real run. |
| `Polly@8.7.0` | 96.3% (2,049 methods, 75 flagged) | High coverage estimate; unverified by a real run. Up in Fase 3.74 — `CancellationToken` had real natives since well before this Fase but no checker-profile allowlist entry at all. |
| `NodaTime@3.3.2` | 94.7% (3,098 methods, 163 flagged) | High coverage estimate; unverified by a real run. |
| `YamlDotNet@18.1.0` | 96.2% (2,182 methods, 82 flagged) | Good coverage estimate; unverified by a real run. |
| `Semver@2.3.0` | 92.9% (423 methods, 30 flagged) | Good coverage estimate; unverified by a real run. |
| `SimpleBase@4.0.0` | 92.6% (258 methods, 19 flagged) | Good coverage estimate; unverified by a real run. |
| `Serilog@4.3.1` | 95.8% (1,115 methods, 47 flagged) | Good coverage estimate; unverified by a real run. Up in Fase 3.74 (`CancellationToken` profile fix). |
| `CsvHelper@33.1.0` | 94.2% (1,393 methods, 81 flagged) | Fase 3.66 fixed the Fase 3.64 `Dictionary`-array-key gap for real (its own key encoder now handles an array-shaped key component). A real, unmodified `csv.GetRecords<T>()` now gets past that AND a second, real bug (`ClassMap.GetGenericType()`'s own `Type.BaseType.GetGenericArguments()` chain, fixed by a new general class-level-generic-type-parameter-tracking capability) — but its `AutoMap()`-based construction path still loses closed-generic identity at the `Type.GetConstructor()` reflection boundary, a separate, deliberate, pre-existing simplification (see `docs/en/ROADMAP.md` Fase 3.66's own "Found, not fixed" section). Not yet a working demo. |
| `MediatR@14.2.0` | 95.5% (441 methods, 20 flagged) | Moderate coverage estimate; unverified by a real run. Up in Fase 3.74 (`CancellationToken` profile fix). |
| `AutoMapper@16.2.0` | 94.1% (2,319 methods, 137 flagged) | Fase 3.66 root-caused and fixed the Fase 3.65 `ValueTuple` NRE for real (a general `Enumerable.FirstOrDefault/LastOrDefault/SingleOrDefault<T>` typed-default gap) AND fixed a real, deep TypeMap-registration bug (`typeof(TSource)`/`typeof(TDestination)` never resolving inside a generic class's own instance methods — a genuinely new, general capability, class-level generic type parameter tracking). A real, unmodified `AutoMapper` now gets past its own static initialization, reflection layer, constructor-selection machinery, AND the TypeMap-registration step — but its real `Mapper.Map<T>(source)` call hits a new, deeper wall: its own compiled mapping-plan expression tree recurses far beyond a safety limit added this Fase specifically to convert what used to be a raw process crash into a graceful error — see `docs/en/ROADMAP.md` Fase 3.66's own "Found, not fixed" section. Not yet a working demo. |

## Aggregate numbers, and why the per-package number matters more

- **Simple average across all 19 packages: 95.8%** (up from 94.45% before Fase 3.74's own corpus-
  wide sweep — see `docs/en/ROADMAP.md` for that Fase's own methodology, in the same "aggregate the
  checker's findings across the WHOLE corpus by real callee, not per-package" spirit as the earlier
  Fase 3.54-3.58 sweep: `IReadOnlyDictionary\`2`/`ArraySegment\`1`/`Array.CopyTo`/`Exception.Source`/
  `KeyNotFoundException`/`ICollection\`1.IsReadOnly` natives, plus a `CancellationToken` checker-
  profile allowlist entry that had simply never existed despite real natives backing it since well
  before this Fase).
- **Methods-weighted average: ~98.4%** — but this is dominated by `DocumentFormat.OpenXml`'s own
  67,234 analyzed methods (55% of every method analyzed across all 19 packages combined) sitting
  at 100%. A weighted average answers "what fraction of all analyzed method calls across this
  whole corpus resolve," which is a real number but not the one that predicts whether *your*
  specific package will work — the **per-package number above is the one that matters** for that.
- The working target for every package here is **97%+, individually** — not a corpus-wide average.
  An average can hide a badly-covered package that breaks the moment someone actually depends on
  it, even while other packages compensate for it in the mean. As of this writing, 7 of 19 packages
  are at or above that bar (`DocumentFormat.OpenXml` 100.0%, `NPOI` 98.2%, `System.Text.Json` 98.1%,
  `FluentValidation` 98.1%, `Humanizer.Core` 98.3%, `Ardalis.GuardClauses` 98.6%, `ClosedXML` 97.5%
  — the last three crossed the bar in Fase 3.74); the rest are active hardening targets, prioritized
  by how far below 97% they sit and by how much real-world usage they represent. `Jint` (96.4%) and
  `Polly`/`YamlDotNet` (96.3%/96.2%) are the closest of the remaining twelve.

## The `Microsoft.Extensions.*` family — official Microsoft frameworks, a separate, ongoing measurement

Distinct from the 19-package corpus above (this project's own long-running compatibility target),
Fase 3.60 started measuring official Microsoft `Microsoft.Extensions.*` packages specifically —
the modern .NET building blocks (dependency injection, configuration, logging, options, caching)
every ASP.NET Core and worker-service app is built on. Checker %, `netstandard-lite` profile, full
transitive deps, as of Fase 3.60:

| Package | Checker % |
|---|---|
| `Microsoft.Extensions.Configuration.Abstractions@8.0.0` | 100.0% |
| `Microsoft.Extensions.Options.ConfigurationExtensions@8.0.0` | 100.0% |
| `Microsoft.Extensions.Options@8.0.0` | 99.7% |
| `Microsoft.Extensions.Configuration.Json@8.0.0` | 98.8% |
| `Microsoft.Extensions.Logging@8.0.0` | 98.1% |
| `Microsoft.Extensions.Configuration.EnvironmentVariables@8.0.0` | 98.0% |
| `Microsoft.Extensions.Logging.Abstractions@8.0.0` | 97.8% |
| `Microsoft.Extensions.Configuration@8.0.0` | 97.2% |
| `Microsoft.Extensions.Primitives@8.0.0` | 96.9% |
| `Microsoft.Extensions.Configuration.FileExtensions@8.0.0` | 95.9% |
| `Microsoft.Extensions.Caching.Abstractions@8.0.0` | 95.9% |
| `Microsoft.Extensions.DependencyInjection.Abstractions@8.0.0` | 94.0% |
| `System.ComponentModel.Annotations@5.0.0` | 94.1% |
| `Microsoft.Extensions.Logging.Console@8.0.0` | 90.6% |
| `Microsoft.Extensions.Configuration.Binder@8.0.0` | 89.4% |
| `Microsoft.Extensions.DependencyInjection@8.0.0` | 89.5% (**verified with a real demo**, above) |
| `Microsoft.Extensions.Caching.Memory@8.0.0` | 87.3% |

Simple average: 95.50%. `DependencyInjection`'s own real end-to-end demo (see above) is the strongest
proof so far: a real, unmodified official package running its actual constructor-injection logic,
not just a static estimate. The rest of this family is next in line for the same real-run treatment.

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
