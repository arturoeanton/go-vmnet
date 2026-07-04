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
| `NPOI@2.8.0` | 97.8% (14,202 methods, 311 flagged) | [`examples/npoi-demo`](../../examples/npoi-demo) | **Verified.** Reads a real legacy `.xls` binary file end to end (strings, numbers, a `SUM` formula cell); one known, documented, cosmetic gap remains (formula cell-range text renders numeric code points instead of column letters — cell *values* are correct). |
| `Dapper@2.1.79` | 93.7% (1,047 methods, 66 flagged) | [`examples/dapper-demo`](../../examples/dapper-demo), [`examples/sqlite-demo`](../../examples/sqlite-demo) | **Verified, two ways.** `dapper-demo` runs Dapper's real `SqlMapper.Query`/`Execute` against a fake in-memory ADO.NET provider; `sqlite-demo` runs the identical real Dapper code against vmnet's own real, Go-native `Microsoft.Data.Sqlite` provider, then independently re-opens the resulting `.db` file with the real `sqlite3` CLI and runs `PRAGMA integrity_check`. Two known, permanent, documented architectural gaps remain (a generic-method `typeof(T)` limitation, and a Dapper regex feature Go's RE2 engine can never compile) — see `docs/en/ROADMAP.md` Fase 3.52/3.53. |
| `ClosedXML@0.105.0` | 96.4% (10,444 methods, 379 flagged) | [`examples/closedxml-demo`](../../examples/closedxml-demo) | **Verified**, with one honest caveat: a tiny compiled C# wrapper supplies a minimal `IXLGraphicEngine`, because ClosedXML's own default font-metrics engine hits a real, deep architectural limitation (generic type-parameter substitution inside a generic class's own static field initializers) unrelated to reading cell data itself. Reads a real `.xlsx` correctly; also the subject of a real, fixed non-deterministic hang (Fase 3.44) — now stable across repeated runs. |
| `System.Text.Json@8.0.5` | 96.1% (3,577 methods, 140 flagged) | [`examples/system-text-json-demo`](../../examples/system-text-json-demo) | **Verified.** Parses real JSON through the real `JsonDocument` API, confirmed against real .NET output. |
| `Jint@3.1.3` | 95.4% (5,414 methods, 249 flagged) | [`examples/jint-demo`](../../examples/jint-demo), [`examples/jint-nowrapper`](../../examples/jint-nowrapper) | **Verified.** Runs a real JavaScript engine end to end — parses real JS source, builds a real AST, evaluates it, and returns a real result — both through a compiled wrapper and with zero C# glue at all. The strongest evidence vmnet handles genuinely non-trivial, deeply object-oriented real-world code, not just small static-method libraries. |
| `Newtonsoft.Json@13.0.3` | 85.3% (4,064 methods, 597 flagged) | [`examples/newtonsoft-json-demo`](../../examples/newtonsoft-json-demo) | **Verified for the demonstrated path** (real "LINQ to JSON" DOM parsing and indexer access), but the lowest checker % of any package with a demo — its `Dynamic`/`ExpandoObject`-based dynamic-typing surface (`JValue+JValueDynamicProxy`) is a real, unimplemented gap the demo doesn't exercise. Don't read the demo passing as "this whole package works." |

## Packages measured by the checker only (no demo yet)

No demo existing yet is not a red flag by itself — every one of the packages above started here
too. It does mean nobody has yet run this specific package's real code end to end and compared the
output against real .NET; treat the percentage as a coverage estimate of what *would* likely work,
not confirmation that it does.

| Package | Checker % | Confidence |
|---|---|---|
| `Humanizer.Core@2.14.1` | 97.5% (1,597 methods, 40 flagged) | High coverage estimate; unverified by a real run. |
| `Ardalis.GuardClauses@5.0.0` | 97.5% (285 methods, 7 flagged) | High coverage estimate; unverified by a real run. |
| `FluentValidation@11.9.2` | 96.4% (1,289 methods, 46 flagged) | High coverage estimate; unverified by a real run. |
| `Polly@8.7.0` | 95.5% (2,049 methods, 92 flagged) | High coverage estimate; unverified by a real run. |
| `NodaTime@3.3.2` | 94.3% (3,098 methods, 177 flagged) | High coverage estimate; unverified by a real run. |
| `YamlDotNet@18.1.0` | 93.9% (2,182 methods, 133 flagged) | Good coverage estimate; unverified by a real run. |
| `Semver@2.3.0` | 92.9% (423 methods, 30 flagged) | Good coverage estimate; unverified by a real run. |
| `SimpleBase@4.0.0` | 92.2% (258 methods, 20 flagged) | Good coverage estimate; unverified by a real run. |
| `Serilog@4.3.1` | 91.4% (1,115 methods, 96 flagged) | Good coverage estimate; unverified by a real run. |
| `CsvHelper@33.1.0` | 91.4% (1,393 methods, 120 flagged) | Good coverage estimate; unverified by a real run. |
| `MediatR@14.2.0` | 89.3% (441 methods, 47 flagged) | Moderate coverage estimate; unverified by a real run. |
| `AutoMapper@16.2.0` | 87.0% (2,319 methods, 301 flagged) | Its own heavy `System.Linq.Expressions`/`Expression.Compile()`-based mapping-plan generation is a real, likely source of the remaining gap — treat this number as more optimistic than the others in this table until a real demo exercises it. |

## Aggregate numbers, and why the per-package number matters more

- **Simple average across all 19 packages: 93.9%.**
- **Methods-weighted average: ~98%** — but this is dominated by `DocumentFormat.OpenXml`'s own
  67,234 analyzed methods (62% of every method analyzed across all 19 packages combined) sitting
  at 100%. A weighted average answers "what fraction of all analyzed method calls across this
  whole corpus resolve," which is a real number but not the one that predicts whether *your*
  specific package will work — the **per-package number above is the one that matters** for that.
- The working target for every package here is **97%+, individually** — not a corpus-wide average.
  An average can hide a badly-covered package that breaks the moment someone actually depends on
  it, even while other packages compensate for it in the mean. As of this writing, 4 of 19 packages
  are at or above that bar (`DocumentFormat.OpenXml`, `NPOI`, `Ardalis.GuardClauses`,
  `Humanizer.Core`); the rest are active hardening targets, prioritized by how far below 97% they
  sit and by how much real-world usage they represent.

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
