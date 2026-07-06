# vmnet

A pure-Go IL/CIL interpreter for running C# plugins — and a growing set of
real NuGet packages — inside a Go program, with no .NET runtime installed
on the host.

**Current release: [v0.7.0](https://github.com/arturoeanton/go-vmnet/releases/tag/v0.7.0)** —
see the [release notes](https://github.com/arturoeanton/go-vmnet/releases/tag/v0.7.0) for what's
new, and [`docs/en/api-stability.md`](docs/en/api-stability.md) for the frozen public API and this
pre-1.0 project's semver commitment.

## This runs a real JavaScript engine. Inside a Go binary. No CGo.

```go
vm := vmnet.New()
vm.NuGet().Add("Jint", "3.1.3")
vm.NuGet().Restore()
jintAsm, _ := vm.LoadPackage("Jint")

engine, _ := jintAsm.New("Jint.Engine")
result, _ := engine.Call("Evaluate", vmnet.String("1 + 2"), vmnet.String(""))
str, _ := result.(*vmnet.Instance).Call("ToString")
fmt.Println(str.Native())
```

```txt
$ go run .
3
```

That's [Jint](https://github.com/sebastienros/jint) 3.1.3 — a real, popular,
**unmodified** C# JavaScript engine pulled straight from nuget.org, along
with its full transitive dependency chain (Esprima, System.Memory,
System.Buffers, ...) — parsing real JavaScript source, building a real AST,
dispatching virtual methods across its real class hierarchy, and evaluating
the result. No subprocess, no `dotnet` installed on the host, no
hand-written shim standing in for the real library. `vmnet` is executing
Jint's actual compiled IL, byte by byte.

Try it yourself: [`examples/jint-nowrapper`](examples/jint-nowrapper) (pure
Go, no compilation step beyond `go run`) and
[`examples/jint-demo`](examples/jint-demo) (the same thing driven through a
tiny compiled C# wrapper, for APIs that lean on C#-only sugar).

```txt
Status: Fase 3.74 complete — a real, deny-by-default Permissions/
sandbox model with MaxStringBytes, a structured VMNET_* error model
with real spec-format stack traces, a general expression-tree evaluator
(Expression<T>.Compile()), a golden test suite audited against every
documented requirement, a frozen public Go API with a real semver
commitment, a real benchmark suite, and a method/token resolution cache
(~35% lower per-call overhead).

Current corpus: 19 real NuGet packages checked with transitive
dependencies under netstandard-lite. 7 of 19 now clear a
97%-individually-working bar (up from 5); simple average across the
corpus: 95.8% (see docs/en/COMPATIBILITY.md for the always-current
per-package breakdown — checker %, real demo, and confidence kept
deliberately separate).

Next: the rest of Fase 4 — real Process/socket support (deliberately
deferred, no real corpus demand found so far), a complete CLI command
set, a cross-platform CI matrix, and a final top-level README pass.
```

**Runtime-verified demos** — each one loads the real, unmodified package from nuget.org and
compares its output against real .NET:

| Package | What it proves |
|---|---|
| [Jint](examples/jint-demo) | A real JavaScript engine — parses, builds a real AST, evaluates |
| [NPOI](examples/npoi-demo) | Reads a real legacy `.xls` binary file |
| [DocumentFormat.OpenXml](examples/openxml-demo) | Generates a real `.docx`, round-tripped through the real .NET SDK |
| [ClosedXML](examples/closedxml-demo) | Reads a real `.xlsx` file |
| [System.Text.Json](examples/system-text-json-demo) / [Newtonsoft.Json](examples/newtonsoft-json-demo) | Real JSON parsing |
| [Dapper](examples/dapper-demo) | `Query`/`Execute` over a fake in-memory ADO.NET provider |
| [Dapper + Microsoft.Data.Sqlite](examples/sqlite-demo) | The same real Dapper code over a real, Go-native SQLite provider — independently verified with the real `sqlite3` CLI |
| [FluentValidation](examples/fluentvalidation-demo) | Real object validation, including a numeric range validator |
| [Microsoft.Extensions.DependencyInjection](examples/di-demo) | Microsoft's own official DI container resolving real constructor injection |
| [Permissions](examples/permissions-demo) | The deny-by-default `Permissions` gate — the same compiled C# run three times against three different capability grants |

*[Léelo en español →](README.es.md)*

## What it is, and isn't

`vmnet` is **not** .NET reimplemented in Go, and it doesn't promise to run
any .NET DLL you throw at it. It's an interpreter for a real, growing
subset of CIL (ECMA-335) plus a partial Base Class Library (`System.*`),
built for:

- C# plugins embedded in a Go application (pricing rules, validation,
  scoring logic — business logic your team already writes in C#)
- Incremental .NET → Go migration, one assembly at a time
- Reusing existing "pure" NuGet packages (no P/Invoke, no heavy
  reflection, no ASP.NET Core/EF Core/WPF) without a CoreCLR dependency —
  Jint above is the proof this scales to genuinely non-trivial,
  object-oriented, real-world code, not just small static-method libraries

Before you load a third-party assembly, `vmnet check` tells you exactly
which methods will run and which won't — with a concrete reason for each
gap — instead of failing midway through execution. Checked against 19
real, popular NuGet packages today, 7 of which already clear a
97%-individually-working bar under vmnet's `netstandard-lite` profile —
but no single number is the one that matters: see
[`docs/en/COMPATIBILITY.md`](docs/en/COMPATIBILITY.md) for the full
per-package breakdown, which deliberately keeps the static checker
percentage, whether a real running demo exists, and an honest confidence
note separate for every single package, instead of collapsing them into
one score.

The full technical specification is in [`docs/en/spec.md`](docs/en/spec.md).

## What actually works today

- **IL execution**: static and instance methods, arithmetic (signed and
  unsigned — `.un` opcodes have correct, distinct semantics), branches,
  loops/`switch`, real `try`/`catch`/`finally`, value types (`initobj`/
  `constrained.`/`Nullable<T>`), real virtual dispatch (`callvirt`
  resolves through the receiver's actual concrete type and its full
  inheritance chain, not just the declared type), `isinst`/`castclass`
  against real class/interface hierarchies, delegates/closures
  (`ldftn`/`Action`/`Func`/multicast), `System.Array` (`SZARRAY` —
  `newarr`/`ldelem`/`stelem`/`ldlen`, correctly zero-initialized for
  value-type elements), managed pointers for `ref`/`out` parameters,
  static fields with a lazy `.cctor`, and unhandled `throw` propagated as
  a typed Go error (`vmnet.ManagedException`).
- **Object construction and instance calls from Go**: `Assembly.New` +
  `Instance.Call` construct a real object and drive its instance API
  directly from Go — no compiled C# glue assembly needed for the common
  case (see [`examples/jint-nowrapper`](examples/jint-nowrapper)).
- **Multi-assembly resolution**: `vm.LoadPackage` loads a NuGet package's
  full transitive dependency graph automatically, with per-method
  assembly-scoped symbol resolution (no cross-assembly name collisions).
- **LINQ, `async`/`await`** (modeled synchronously), real `System.
  Reflection` (`Type.GetConstructor`/`GetMethod`/`GetField` plus
  `ConstructorInfo`/`MethodInfo`/`FieldInfo`'s own `Invoke`/`GetValue` —
  not `Reflection.Emit`, no code generation, every target is a real
  method/field vmnet already knows how to run), `Enum.GetValues`/
  `HasFlag`, `DateTime`/`Span<T>`/`ReadOnlySpan<T>`, `System.Text.
  RegularExpressions`, both the generic (`HashSet<T>`/`Stack<T>`/
  `ConcurrentDictionary`) and legacy non-generic (`ArrayList`/
  `Hashtable`/`SortedList`/`Stack`) collections, and a broad, steadily
  growing slice of `System.String`/`System.Math`/`System.Text.Encoding`/
  `StringBuilder`.
- **Go↔C# bridge**: call a method directly with typed arguments
  (`Assembly.Call`), construct and drive an object graph
  (`Assembly.New`/`Instance.Call`), or pass/return raw `byte[]`/JSON
  (`CallBytes`/`CallJSON`) for arbitrary shapes.
- **Compatibility checker**: `vmnet check <dll>` reuses the *real*
  execution pipeline to report, method by method, what will and won't run
  under a given profile (`minimal`/`rules`/`netstandard-lite`) — not a
  separate heuristic guess.
- **NuGet**: `vmnet add`/`restore`/`packages` resolve and download real
  packages from `api.nuget.org` (including transitive dependencies),
  cache them locally, and load them with `vm.LoadPackage`.
- **Sandbox**: instruction/call-depth/stack-depth/array-length/string-length
  limits, a panic anywhere in interpreted code recovered at the API
  boundary (a broken or adversarial plugin can't crash the host process),
  and a real, deny-by-default `Permissions` gate (`AllowFileRead`/
  `AllowFileWrite`) in front of every native that touches real disk I/O.
  This is a **stability-plus-file-I/O** boundary today, not yet a full
  trust boundary (no network/process-spawning surface exists at all yet,
  by design) — see [`docs/en/security.md`](docs/en/security.md) for the
  honest threat model before running untrusted C# through vmnet.

See [`docs/en/ROADMAP.md`](docs/en/ROADMAP.md) for the full phase-by-phase
history — including every real correctness bug found and fixed along the
way (signed/unsigned comparison, a `.cctor` reentrancy deadlock, a struct
field-default aliasing bug that made `1 + 2` evaluate to `2` inside real
Jint, and more), not swept under the rug.

## Quickstart

```bash
go get github.com/arturoeanton/go-vmnet
```

```go
package main

import (
	"fmt"
	"log"

	vmnet "github.com/arturoeanton/go-vmnet"
)

func main() {
	vm := vmnet.New()

	asm, err := vm.LoadFile("MyPlugin.dll")
	if err != nil {
		log.Fatal(err)
	}

	result, err := asm.Call("MyNamespace.MyClass", "Add", vmnet.Int32(3), vmnet.Int32(4))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.Native()) // 7
}
```

`MyPlugin.dll` is an ordinary assembly compiled by the official .NET SDK
(`dotnet build`) — the SDK is a **build-time** dependency for producing
the plugin, never a runtime dependency of the Go program that loads it.

For an object-oriented API (construct an instance, call its methods, use
what they return), `Assembly.New`/`Instance.Call` work the same way
without any static-method wrapper — this is exactly how the Jint demo
above works:

```go
engine, _ := jintAsm.New("Jint.Engine")
result, _ := engine.Call("Evaluate", vmnet.String("1 + 2"), vmnet.String(""))
str, _ := result.(*vmnet.Instance).Call("ToString")
fmt.Println(str.Native()) // "3"
```

Runnable, documented examples in [`examples/`](examples/):

| Example | Shows |
|---|---|
| [`examples/hello`](examples/hello) | The smallest possible `LoadFile` + `Call` |
| [`examples/rules`](examples/rules) | Objects, `List`/`Dictionary`, JSON bridge, managed exceptions, the instruction sandbox stopping a runaway plugin |
| [`examples/nuget-basic`](examples/nuget-basic) | Adding and restoring a real published NuGet package, then calling a real function from it |
| [`examples/jint-demo`](examples/jint-demo) | Real JavaScript execution via the real Jint NuGet package + its full dependency chain, driven through a small compiled C# wrapper |
| [`examples/jint-nowrapper`](examples/jint-nowrapper) | The same Jint demo with zero C# wrapper — `Assembly.New`/`Instance.Call` driving `Jint.Engine` directly from Go |
| [`examples/npoi-demo`](examples/npoi-demo) | Reading a real legacy `.xls` binary file (strings, numbers, a formula cell) through the real NPOI NuGet package, zero C# wrapper |
| [`examples/system-text-json-demo`](examples/system-text-json-demo) | Parsing real JSON through the real System.Text.Json package, zero C# wrapper |
| [`examples/newtonsoft-json-demo`](examples/newtonsoft-json-demo) | Parsing real JSON through the real Newtonsoft.Json "LINQ to JSON" DOM, zero C# wrapper |
| [`examples/openxml-demo`](examples/openxml-demo) | Generating a real `.docx` from scratch through the real DocumentFormat.OpenXml package, round-tripped through the real .NET SDK |
| [`examples/closedxml-demo`](examples/closedxml-demo) | Reading a real `.xlsx` file through the real ClosedXML package, via a tiny compiled C# wrapper for one font-metrics limitation |
| [`examples/calculator`](examples/calculator) | An arithmetic/loop workload run through vmnet, native Go, and (optionally) real CoreCLR side by side, for a correctness-and-speed comparison |
| [`examples/dapper-demo`](examples/dapper-demo) | The real Dapper NuGet package's own `SqlMapper.Query`/`Execute`, run against a minimal fake in-memory ADO.NET provider — no real database, no dotnet SDK needed at runtime |
| [`examples/sqlite-demo`](examples/sqlite-demo) | The same real Dapper code running against vmnet's own real, Go-native `Microsoft.Data.Sqlite` provider — a genuine embedded SQLite `.db` file, independently re-opened and integrity-checked by the real `sqlite3` CLI afterward |
| [`examples/fluentvalidation-demo`](examples/fluentvalidation-demo) | The real FluentValidation NuGet package validating a real object, including a numeric range validator (`GreaterThanOrEqualTo`) dispatched through a generic base/derived validator hierarchy |
| [`examples/di-demo`](examples/di-demo) | Microsoft's own official `Microsoft.Extensions.DependencyInjection` container resolving a service whose constructor depends on another registered service, unmodified |
| [`examples/permissions-demo`](examples/permissions-demo) | The same compiled C# run three times against three different `Permissions` grants — denied, file-read-only, and fully granted (independently re-read from Go to confirm a real file, not an in-memory illusion) |
| [`examples/bind-demo`](examples/bind-demo) | `vmnet bind`'s own generated Go wrapper code, called with typed Go functions/methods instead of `Assembly.Call` string literals |
| [`examples/plugin-demo`](examples/plugin-demo) | A plugin scaffolded from `dotnet new vmnet-plugin`, its generated starter replaced with a real business rule, loaded via `LoadFile` and called with `CallBytes`/`CallJSON` |
| [`benchmarks/`](benchmarks) | The full Fase 4 benchmark suite: seven workloads run through vmnet and native Go side by side, plus cold load time, method invoke overhead, allocations/op, and package restore time |

## CLI

```txt
vmnet inspect <dll>                                    # metadata summary
vmnet il <dll> <Type.Method>                            # decoded IL for one method
vmnet run <dll> <Type.Method> '<json-array-of-args>'    # execute it
vmnet check [--profile=minimal|rules|netstandard-lite] [--html=<file>] <dll>
vmnet check package [--profile=...] [--html=<file>] <id>@<version>  # check a NuGet package without adding it
vmnet analyze <dir> [--profile=...] [--html=<file>]     # scan a whole legacy .NET bin/ folder, ranked migration candidates
vmnet bind <dll> --out=<dir> [--package=<name>]         # generate idiomatic, typed Go wrappers
vmnet bind package <id>@<version> --out=<dir> [--package=<name>]
vmnet add <id>[@<version>]
vmnet restore
vmnet packages
```

`--html=<file>` writes the same result as a single, self-contained HTML page (no external
fonts/scripts) instead of only printing to the terminal — see
[`docs/en/compatibility-profile.md`](docs/en/compatibility-profile.md) §3.1 for `vmnet analyze`'s
own cross-assembly resolution and "best migration candidates" ranking, and §3.2 for `vmnet bind`'s
code generation (see also [`examples/bind-demo`](examples/bind-demo)).

Writing a plugin from scratch instead of calling into an existing package? `dotnet new install
./templates/vmnet-plugin && dotnet new vmnet-plugin -n BillingRules` scaffolds a `byte[]`-in/
`byte[]`-out `Entry.Invoke` project shaped for `Assembly.CallBytes`/`CallJSON` — see
[`docs/en/plugin-sdk.md`](docs/en/plugin-sdk.md) and [`examples/plugin-demo`](examples/plugin-demo).

## Architecture

```txt
.dll → internal/pe → internal/metadata → internal/il → internal/ir → internal/interpreter → internal/bcl
```

Public API and CLI live at the repo root; everything else is
implementation detail under `internal/`. See
[`docs/en/architecture.md`](docs/en/architecture.md) for the full pipeline,
package layout, and current-state notes, and
[`docs/en/adr/`](docs/en/adr) for the standing design decisions (why pure Go,
why the package layout deviates from the original spec, ...).

## Development

```bash
go build ./...
go vet ./...
go test ./... -race
```

Integration tests load real C# DLLs compiled from
`tests/fixtures/csharp`. The .NET SDK is a **development-only**
dependency, needed to regenerate those fixtures — never a dependency of
the `vmnet` runtime itself:

```bash
dotnet build tests/fixtures/csharp/Fixtures.csproj -c Release
```

See [`CONTRIBUTING.md`](CONTRIBUTING.md) before sending a PR.

## License

Apache License 2.0 — see [`LICENSE`](LICENSE).
