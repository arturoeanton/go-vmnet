# vmnet

A pure-Go IL/CIL interpreter for running C# plugins — and a growing set of
real NuGet packages — inside a Go program, with no .NET runtime installed
on the host.

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
Status: Fase 3.28 complete — checker + NuGet + real virtual dispatch +
multi-assembly resolution + an instance-object API (Assembly.New /
Instance.Call). Fase 4 (production readiness: benchmarks, full sandbox,
docs polish) is next. See docs/en/ROADMAP.md.
```

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
gap — instead of failing midway through execution. Averaged across 7 real,
popular NuGet packages plus Jint, ~89% of methods run clean under vmnet's
`netstandard-lite` profile today (see [`docs/en/ROADMAP.md`](docs/en/ROADMAP.md)
for the per-package breakdown and methodology).

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
- **LINQ, `async`/`await`** (modeled synchronously), reflection-lite
  (`typeof`/`GetType`/`System.Type` introspection, `Enum.GetValues`/
  `HasFlag`), `DateTime`/`Span<T>`/`ReadOnlySpan<T>`, `System.Text.
  RegularExpressions`, `HashSet<T>`/`Stack<T>`/`ConcurrentDictionary`, and
  a broad, steadily growing slice of `System.String`/`System.Math`/
  `System.Text.Encoding`/`StringBuilder`.
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
- **Sandbox**: instruction/call-depth/stack-depth/array-length limits, and
  a panic anywhere in interpreted code is recovered at the API boundary —
  a broken or adversarial plugin can't crash the host process.

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

## CLI

```txt
vmnet inspect <dll>                                    # metadata summary
vmnet il <dll> <Type.Method>                            # decoded IL for one method
vmnet run <dll> <Type.Method> '<json-array-of-args>'    # execute it
vmnet check [--profile=minimal|rules|netstandard-lite] <dll>
vmnet check package [--profile=...] <id>@<version>       # check a NuGet package without adding it
vmnet add <id>[@<version>]
vmnet restore
vmnet packages
```

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
