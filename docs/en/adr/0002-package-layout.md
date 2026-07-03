# ADR 0002 — Public package at the root, implementation under `/internal`

- Status: accepted
- Date: 2026-07-02

## Context

The original spec (spec §7) proposes a `/vmnet` folder (public API)
alongside implementation folders at the repo root: `/pe`, `/metadata`,
`/il`, `/ir`, `/interpreter`, `/runtime`, `/bcl`, `/nuget`, `/checker`.
Laid out that way, all of them would be public, importable-from-outside-
the-module Go packages from the very first commit.

## Decision

1. The public package lives at the module root: `package vmnet` in
   `github.com/arturoeanton/go-vmnet`, with no `/vmnet` subfolder — so
   `go get github.com/arturoeanton/go-vmnet` resolves directly to the
   package the end user imports (spec §6).
2. Everything the spec listed as `/pe /metadata /il /ir /interpreter
   /runtime /bcl /nuget /checker` moves under `/internal/...`.
   `cmd/vmnet` and the `examples/` can still import them because they're
   inside the same module — `internal/` only blocks imports from
   *outside* the repository.

## Rationale

During Fases 1-3 the internal design (the `Value` representation, the
IR, the object model) is going to change with every phase. Publishing
those packages from day one as a stable Go API would force maintaining
backward compatibility before the interpreter even works. `internal/`
leaves the only public surface committed to in v1.0 (spec §6, frozen in
Fase 4) without taking anything away from the architecture or the
separation of concerns the spec defines.

## Consequences

- Any real need to expose something low-level (e.g. spec §6.3's
  low-level API, `ResolveMethod`/`NewFrame`/`Invoke`) is added
  deliberately to the root `vmnet` package, not by re-exporting
  `internal/*` directly.
- `docs/en/spec.md` keeps the original layout as a reference; this ADR
  is the note on the one deliberate deviation from it.
