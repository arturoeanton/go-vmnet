# API stability and semver commitment

This document is the Fase 4 "freeze the public Go API, semver commitment" deliverable
(`docs/en/ROADMAP.md`). It states plainly what's frozen, what isn't, and exactly what a version
bump means going forward — so a Go program embedding vmnet can decide how tightly to pin its
dependency.

## What's covered

**Only the root package, `github.com/arturoeanton/go-vmnet` (package `vmnet`) — everything
importable without an `internal/` path segment.** Every `internal/*` package (`internal/il`,
`internal/ir`, `internal/metadata`, `internal/pe`, `internal/runtime`, `internal/interpreter`,
`internal/bcl`, `internal/checker`, `internal/nuget`, `internal/migrate`, `internal/bind`) is
exactly what Go's own `internal/` convention means: implementation detail, free to change shape at
any time, in any release, without notice. Nothing in this document constrains them. `cmd/vmnet`
(the CLI) is covered separately, informally, by its own command/flag surface — see
`docs/en/compatibility-profile.md`
for the `check`/`check package` subcommands' current flags; the CLI isn't semver-tracked the way
the Go API is, since it's consumed as a binary, not as an imported package.

## The frozen surface, as of this snapshot

Every exported symbol in the root package, current as of Fase 3.70 (verifiable any time with
`go doc -all .` — that command's output is the actual source of truth; this list is a snapshot of
it):

**Entry point**
- `func New() *VM`
- `type VM struct{ ... }` (unexported fields) with methods:
  `LoadFile(path string) (*Assembly, error)`,
  `LoadBytes(name string, data []byte) (*Assembly, error)`,
  `LoadPackage(id string) (*Assembly, error)`,
  `NuGet() *NuGetManager`,
  `Permissions() *Permissions`

**Calling into loaded code**
- `type Assembly struct{ ... }` (unexported fields) with methods:
  `Call(typeName, methodName string, args ...Value) (Value, error)`,
  `CallBytes(typeName, methodName string, input []byte) ([]byte, error)`,
  `CallJSON(typeName, methodName string, input any) (any, error)`,
  `New(typeName string, args ...Value) (*Instance, error)`,
  `WithDependencies(deps ...*Assembly) *Assembly`,
  `Name() string`
- `type Instance struct{ ... }` (unexported fields) with methods:
  `Call(methodName string, args ...Value) (Value, error)`, `Native() any`, `TypeName() string`
- `type Value interface{ ... }` — the argument/return type for `Call`/`CallJSON`/`New`/
  `Instance.Call`; implemented by every constructor below and by `*Instance` itself (so a live
  object can be passed back in as an argument)
- Value constructors: `func Int32(v int32) Value`, `func Int64(v int64) Value`,
  `func Float32(v float32) Value`, `func Float64(v float64) Value`, `func String(v string) Value`,
  `func ByteArray(data []byte) Value`

**NuGet**
- `type NuGetManager struct{ ... }` (unexported fields) with methods:
  `Add(id, version string) error`, `Restore() error`, `Packages() ([]Package, error)`
- `type Package struct { ID, Version, SelectedAsset, Unselectable string; Dependencies []string }`
- Constants: `NuGetManifestFile = "vmnet.json"`, `NuGetLockFile = "vmnet.lock.json"`,
  `NuGetCacheDir = ".vmnet/packages"`

**Errors (spec §30, landed Fase 3.67)**
- `type Code string` and its 14 spec-defined constants (`CodeInvalidPE`, `CodeMissingCLIHeader`,
  `CodeInvalidMetadata`, `CodeUnsupportedOpcode`, `CodeUnsupportedBCLMethod`, `CodeTypeNotFound`,
  `CodeMethodNotFound`, `CodeFieldNotFound`, `CodeStackOverflow`, `CodeCallDepthExceeded`,
  `CodeManagedException`, `CodeNuGetResolveFailed`, `CodeUnsupportedPackage`,
  `CodePermissionDenied`) plus the one deliberate addition beyond the spec's own list,
  `CodeInternal` (a catch-all — see its own doc comment for what it means and doesn't mean)
- `type Error struct { Code Code; Message, Details string; Cause error }` with
  `Error() string`/`Unwrap() error`
- `type ManagedException = runtime.ManagedException` (a type alias — use `errors.As` to inspect a
  thrown-and-unhandled CIL exception a `Call`/`CallBytes`/`CallJSON` surfaced)

**Permissions (spec's security model, landed Fase 3.59)**
- `type Permissions = runtime.Permissions` (a type alias, not a wrapper — see its own doc comment
  for why) with fields `AllowFileRead`, `AllowFileWrite` (both enforced today), `AllowConsole`,
  `AllowNetwork` (defined for forward compatibility, not enforced by anything yet — see
  `docs/en/security.md`)

That's the entire surface. It's deliberately small: three "verb" types (`VM`, `Assembly`,
`Instance`), one error type, one permissions struct, one NuGet manager, and a handful of `Value`
constructors.

Fase 3.75 (HTML compatibility reports, `vmnet analyze`, `vmnet bind`) and Fase 3.76 (the
`dotnet new vmnet-plugin` SDK, plus a real `String.IndexOf(string, StringComparison)` bug fix) both
landed on `main` after `v0.7.0` without touching this surface at all: the new code they added lives
either in `cmd/vmnet` (new subcommands, covered informally, not by this document) or in two new
`internal/*` packages (`internal/migrate`, `internal/bind`, added to the list above). The root
package gained zero new exported symbols across both Fases — confirmed directly against
`go doc -all .` — so this snapshot needs no revision because of them. Both Fases shipped together
as **`v0.8.0`**, a minor bump past `v0.7.0` reflecting the real new CLI capability even though the
frozen Go surface above is unchanged — consistent with this document's own rule that a
minor-equivalent release may add, never break.

## The semver commitment

**vmnet is pre-1.0** (`go.mod` declares no version; git tags today are Fase-numbered, not yet
semver — see "On versioning today" below). Per semver's own rule for `0.y.z`, anything may change
in any release. This project narrows that down to a concrete, useful promise anyway, because "pre-
1.0" shouldn't mean "no promise at all" for anyone actually depending on this:

- **A PATCH-equivalent release** (a new Fase that only adds — a new native, a new supported
  opcode, a new package pushed over the checker's own 97% bar, a bug fix) never changes an existing
  exported signature, removes an exported symbol, or changes an existing `Code` constant's meaning.
  Safe to pull without reading a changelog.
- **A MINOR-equivalent release** may add new exported symbols (a new method, a new `Code`, a new
  optional-via-variadic parameter) — additive, but worth a skim of `docs/en/ROADMAP.md`'s newest
  Fase entry before upgrading in case a new capability changes a default you were relying on
  implicitly (e.g. a newly-enforced `Permissions` field).
- **A signature change, a removed exported symbol, or a changed `Code` constant's meaning is
  always treated as breaking** — even though semver's own `0.y.z` rule would technically permit it
  in a minor bump. When one is genuinely necessary, it gets called out explicitly, in bold, in that
  Fase's own `docs/en/ROADMAP.md` entry, not buried in prose.
- **Once this project reaches v1.0.0** (the full Fase 4 checklist — see `docs/en/ROADMAP.md`'s own
  "production-ready v1.0" section for exactly what's still outstanding as of this snapshot), real
  semver begins: a breaking change requires a major version bump, which per Go's own module
  convention (`golang.org/x/mod`, the `go.dev` modules reference) means a new `/v2`-suffixed module
  path — `github.com/arturoeanton/go-vmnet` itself never breaks under an existing importer's feet
  without that importer explicitly opting into the new path.

## On versioning today

As of this Fase, this project's git tags follow a `v0.0.3.<n>.faseNNN-<slug>` pattern (e.g.
`v0.0.3.70.fase370-docs-and-benchmark-suite`) — useful for this project's own internal, one-tag-
per-development-phase tracking, but **not a valid Go module semver tag** (too many numeric
components, no `-prerelease`/`+build` separator Go's own module resolver recognizes). A Go program
running `go get github.com/arturoeanton/go-vmnet@latest` today resolves to a pseudo-version off the
latest commit on `main`, not a pinned release. Fase-numbered tags keep being created alongside real
semver tags going forward (both can point at the same commit).

Real semver tagging for this project actually began earlier than this document — `v0.6.0-alpha`
(a real, published GitHub Release) predates the frozen-API snapshot above by several Fases. An
earlier draft of this document tagged this Fase's own commit `v0.1.0`, not realizing a
higher-numbered real release already existed; that tag was retracted before ever being used for a
release, in favor of continuing the ALREADY-public `v0.6.0-alpha` line instead of introducing a
second, contradictory, lower-numbered one. The first release built on this document's own frozen-
surface snapshot is **`v0.7.0`** — a minor bump past `0.6.0-alpha` (every gap that release's own
notes called out as blocking — no `Permissions` model, no `File`/`Process` BCL surface, no
formalized benchmarks — is resolved as of this Fase). **`v0.8.0`** follows it with Fase 3.75-3.76's
CLI tooling (see above) on the exact same frozen surface. Both are still short of `v1.0.0` since the
Fase 4 checklist genuinely has real items left (see `docs/en/ROADMAP.md`).

## What this document intentionally does NOT cover

- `docs/en/spec.md` §6.1's original API sketch (an `Options{Profile, Debug, MaxStackDepth,
  MaxHeapBytes}` struct passed to `New`, a low-level `ResolveMethod`/`NewFrame`/`Invoke` API,
  `BackendAuto`) was the project's own starting design vision, written before real implementation
  began — the API that actually got built and is frozen above diverged from it in several places
  (`New()` takes no options at all; `Permissions()` is its own separate, mutable-in-place accessor
  rather than a constructor-time struct field; there is no low-level Frame/Invoke API exposed
  publicly, only the three `Call*` methods). **This document, not spec §6.1, is the authoritative
  description of the current, real, frozen API** — spec.md remains the original design vision, kept
  for historical context, not a promise about what shipped.
- `cmd/vmnet`'s own CLI flags/subcommands (informal, see `docs/en/compatibility-profile.md`).
- Anything under `internal/` (Go's own convention already makes the promise here: none).
