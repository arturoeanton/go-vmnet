# tests/golden

Reserved for golden output fixtures (expected IL dumps, expected
`Call`/`CallJSON` results, expected `vmnet check` reports) for table-driven
tests, once the loader/interpreter existed to produce them.

In practice this directory has stayed empty: every table-driven test built
since (including the full spec §28.1-28.5 golden suite, audited and
gap-closed in Fase 3.69 — see `docs/en/ROADMAP.md`) keeps its expected
values inline in the Go test file itself, next to the fixture that produces
them, rather than as separate files here. `tests/fixtures/csharp` is the
directory that's actually in active use — see its own `README.md`.
