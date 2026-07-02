# vmnet

A pure-Go IL/CIL interpreter for running C# plugins — and a growing set of
real NuGet packages — inside a Go program, with no .NET runtime installed
on the host.

```txt
Status: Fase 3.5 complete (checker + NuGet + hardening). Fase 4
(production readiness: benchmarks, full sandbox, docs polish) is next.
See docs/ROADMAP.md.
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
  reflection, no ASP.NET Core/EF Core/WPF) without a CoreCLR dependency

Before you load a third-party assembly, `vmnet check` tells you exactly
which methods will run and which won't — with a concrete reason for each
gap — instead of failing midway through execution.

The full technical specification is in [`docs/spec.md`](docs/spec.md).

## What actually works today

- **IL execution**: static and instance methods, arithmetic (signed and
  unsigned — `.un` opcodes have correct, distinct semantics), branches,
  loops, `newobj`/`callvirt`/instance fields (direct resolution, no vtable
  yet), `System.Array` (`SZARRAY` — `newarr`/`ldelem`/`stelem`/`ldlen`),
  managed pointers for `ref`/`out` parameters, static fields with a lazy
  `.cctor`, and unhandled `throw` propagated as a typed Go error
  (`vmnet.ManagedException`).
- **Partial BCL**: `List<T>`, `Dictionary<string,V>`, `System.String`/
  `System.Math`/`System.Text.Encoding` basics, exception constructors.
- **Go↔C# bridge**: call a method directly with typed arguments
  (`Assembly.Call`), or pass/return raw `byte[]`/JSON (`CallBytes`/
  `CallJSON`) for arbitrary shapes.
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

See [`docs/ROADMAP.md`](docs/ROADMAP.md) for the full phase-by-phase
history — including two real correctness bugs (signed/unsigned comparison,
a `.cctor` reentrancy deadlock) and a couple of checker "drift" bugs that
were found and fixed along the way, not swept under the rug.

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

Runnable, documented examples in [`examples/`](examples/):

| Example | Shows |
|---|---|
| [`examples/hello`](examples/hello) | The smallest possible `LoadFile` + `Call` |
| [`examples/rules`](examples/rules) | Objects, `List`/`Dictionary`, JSON bridge, managed exceptions, the instruction sandbox stopping a runaway plugin |
| [`examples/nuget-basic`](examples/nuget-basic) | Adding and restoring a real published NuGet package, then calling a real function from it |

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
[`docs/architecture.md`](docs/architecture.md) for the full pipeline,
package layout, and current-state notes, and
[`docs/adr/`](docs/adr) for the standing design decisions (why pure Go,
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
