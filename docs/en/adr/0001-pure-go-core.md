# ADR 0001 — Pure-Go core, no CoreCLR

- Status: accepted
- Date: 2026-07-02

## Context

`vmnet` needs to run C#/IL code from Go. The obvious alternative is
hosting CoreCLR via `nethost`/`hostfxr` (native hosting documented by
Microsoft). That gives near-total compatibility, but requires an
installed .NET runtime, `runtimeconfig.json`, cgo, and runtime
resolution — in other words, it stops being an embeddable Go library and
becomes an external process integrator.

## Decision

`vmnet`'s core is an IL/CIL interpreter written in pure Go:
`CGO_ENABLED=0`, no `hostfxr`, no hard dependency on `dotnet` being
installed on the host running the Go application. CoreCLR can only ever
exist as a future *optional* backend (`vmnet.BackendCoreCLR`, see spec
§39), never as a requirement of the default mode.

## Consequences

- vmnet will never run "any .NET DLL" — only the subset of IL/BCL the
  interpreter supports. This is communicated explicitly (spec §3.3,
  §33.3) and made verifiable with `vmnet check` (Fase 3).
- The project's biggest risk becomes the BCL (`System.*`), not runtime
  hosting — see the risk register in `docs/en/ROADMAP.md`.
- CI can validate on Linux/macOS/Windows without installing the .NET SDK
  on the runners that build `vmnet` (the .NET SDK is only needed to
  generate the test fixture DLLs, a separate development-time step).
