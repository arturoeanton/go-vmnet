# Contributing to vmnet

## Before anything

Read `docs/en/spec.md` (what we're building and why) and `docs/en/ROADMAP.md`
(what Fase we're in and what's left). A PR that adds something outside the
scope of the active Fase will probably have to wait — see "Non-goals" in the
spec (§3) before proposing support for reflection, async, P/Invoke, etc.

## Hard rules

- **No cgo in the core.** `go build`/`go vet`/`go test` must pass with
  `CGO_ENABLED=0`. This is a product commitment (ADR 0001), not just a CI
  flag.
- **`internal/` is internal.** The packages under `internal/pe`,
  `internal/metadata`, `internal/il`, `internal/ir`, `internal/interpreter`,
  `internal/runtime`, `internal/bcl`, `internal/nuget`, `internal/checker`,
  `internal/migrate`, and `internal/bind` are not public API (ADR 0002).
  Anything the end user needs to call is exposed deliberately from the root
  `vmnet` package.
- **`vmnet check` before promising compatibility.** If you add support for a
  new opcode or BCL method, the `internal/checker` analyzer has to stop
  flagging it as unsupported — it's not enough for the interpreter to just
  not crash.

## Build and test

```bash
go build ./...
go vet ./...
go test ./... -race
```

CI (`.github/workflows/`) runs the same build/vet/test on Linux, macOS, and
Windows, plus a `CGO_ENABLED=0` build on every OS. Windows runners fall back
to plain `go test ./... -count=1` because the race detector needs a C
toolchain that isn't reliably available there.

## C# fixtures

The PE/metadata/IL/interpreter tests load real DLLs compiled from
`tests/fixtures/csharp`. You need the .NET SDK installed **only for
this** — never to run `vmnet` itself:

```bash
dotnet build tests/fixtures/csharp/Fixtures.csproj -c Release
```

If you're touching `internal/migrate`, `vmnet analyze`, or P/Invoke
detection in `internal/checker`, also build the second fixture project they
exercise:

```bash
dotnet build tests/fixtures/csharp-pinvoke/PInvokeFixture.csproj -c Release
```

Tests that depend on it skip themselves with a clear message if it isn't
built.

If you add a new case (an opcode, a class pattern, a BCL call), add the
corresponding C# fixture first in `tests/fixtures/csharp/README.md` with a
row describing what it exercises.

## Commits and PRs

- One PR per `docs/en/ROADMAP.md` task when possible; reference the Fase and
  module in the description (e.g. "Fase 1 · /pe: RVA→offset").
- Add tests alongside the code, not in a separate PR.
- If the PR introduces a non-trivial architecture decision, document it as
  an ADR in `docs/en/adr/` — and mirror it in `docs/es/adr/` so both stay
  true translations of each other, per this repo's bilingual documentation
  convention.
