# NuGet support

vmnet consumes real, unmodified packages from the same registry every .NET project uses —
nuget.org — entirely in pure Go. There is no `dotnet` CLI, no `NuGet.exe`, and no .NET runtime
involved anywhere in fetching, parsing, resolving, or reading a package. Everything documented
here lives in `internal/nuget/` (the parser/resolver/lockfile engine) and the root `nuget.go`
(the public `NuGetManager`/`VM.LoadPackage` API a Go host actually calls). This is spec §22's
implementation.

The short version: `vm.NuGet().Add("NodaTime", "3.2.0")` then `vm.NuGet().Restore()` downloads
the real `.nupkg` from nuget.org (or vmnet's own local cache), parses its real `.nuspec`, resolves
its real transitive dependency graph, and writes `vmnet.lock.json` — vmnet's own answer to
`packages.lock.json`. `vm.LoadPackage("NodaTime")` then loads the exact DLL bytes Restore locked,
wires up the resolved dependency graph, and hands back a `*vmnet.Assembly` ready to `Call`/`New`.

## What's actually parsed

A `.nupkg` is a real zip archive; `nuget.OpenPackage` (`internal/nuget/nupkg.go`) opens it with
Go's standard `archive/zip` and keeps only the entries the resolver could ever need —
`lib/`, `ref/`, `runtimes/`, and the root `.nuspec` — discarding OPC packaging noise
(`_rels/`, `package/`, `[Content_Types].xml`) that real NuGet tooling emits but vmnet has no use
for. Each retained entry is read under a 256MB cap, so a hostile or corrupt `.nupkg` can't OOM the
resolver.

The `.nuspec` (`internal/nuget/nuspec.go`) is parsed as plain XML into `NuSpec` — deliberately a
narrow model of the real schema: package identity (`id`, `version`) and dependency groups. A real
`.nuspec` carries authors, license, icon, release notes, and more; none of that affects whether a
package's IL can run inside vmnet, so none of it is modeled. Two dependency shapes are handled,
because both appear in real packages depending on which tool generated the `.nuspec`:

- **Legacy/flat**: `<dependencies><dependency id="..." version="..."/></dependencies>` — applies
  unconditionally, no per-framework distinction.
- **Modern/grouped**: one `<group targetFramework="...">` per TFM, each with its own
  `<dependency>` list — this is the shape that requires TFM-aware resolution (below).

## TFM selection

vmnet's resolver understands both notations real `.nuspec` files use for a target framework
moniker: the short folder-name form (`netstandard2.0`, `net8.0`, `net472`) and the long form some
tooling still emits (`.NETStandard,Version=v2.0` and the abbreviated dotted variant,
`.NETFramework4.7.2`). `ParseTFM` (`internal/nuget/tfm.go`) normalizes both into a single `TFM`
struct with a `Family` (`FamilyNetStandard`, `FamilyNetCoreApp`, `FamilyNetModern` for net5.0+,
`FamilyNetFramework`) plus major/minor/patch numbers. A TFM carrying an OS qualifier
(`net6.0-windows`) is tagged `IsPlatformSpecific` and is never selectable — vmnet is pure-Go and
cross-platform by construction, so a platform-specific asset is a hard no regardless of tier.

Selection tiers, from `tier()` in `tfm.go`, matching spec §22.5's priority list exactly:

1. **`netstandard2.0`** — tier 1, the preferred target.
2. **`netstandard2.1`** — tier 2.
3. **`net5.0`+ (modern .NET)** — tier 3, but *only* if the caller opts in via
   `SelectOptions.AllowModernNet` (off by default). This is deliberate: vmnet's IL/BCL profile is
   shaped around netstandard2.0-style code, not the modern BCL surface net5.0+ packages can
   assume is present.
4. **`netstandard1.x`** — tier 5 (older, still usually source-compatible, but ranked below modern
   `net5.0` even when modern isn't allowed, since it's a legacy fallback rather than a real target).
5. Anything else (`net472`-style .NET Framework, `netcoreapp3.1`-style direct netcoreapp targets,
   an unrecognized moniker) — tier 0, not selectable, with a human-readable reason from
   `Selectable()` (e.g. `"net472 targets .NET Framework, not netstandard2.0-compatible"`).

`SelectTFM` picks the lowest (best) tier across every `lib/<tfm>/` folder a package ships. Practical
consequence, confirmed by `docs/en/COMPATIBILITY.md`'s own 19-package corpus: most real packages
that expose "pure logic" (parsers, validators, serializers, date/time libraries) still ship a
`netstandard2.0` asset even years after net5.0+ shipped, precisely because `netstandard2.0` is
still how a library author reaches the widest audience (old .NET Framework consumers included).
`vmnet check package` against that corpus almost always reports `netstandard2.0` as the selected
target — `AllowModernNet` exists for the packages that don't, but isn't vmnet's default posture.

## Dependency resolution

`NuSpec.DependenciesFor(target TFM)` picks the dependency list that applies once a concrete target
TFM is already chosen (typically the same TFM `SelectLibAsset` picked): it looks for the
`<group>` whose own `targetFramework` matches `target`'s family/major/minor, falling back to an
explicit empty-TFM group ("applies to everything") if there is one, or the flat legacy list if the
`.nuspec` has no groups at all.

`Resolver.Resolve` (`internal/nuget/resolver.go`) walks the full transitive closure starting from a
project's direct dependencies: for each package, fetch it (via `Cache.Fetch`, cache-first,
falling back to `Client.Download` from nuget.org), select its best asset, read that asset's own
dependency group, and recurse into each of *those* dependencies — exactly the graph a real
`dotnet restore` would walk, just resolved by vmnet's own simpler algorithm. Two simplifications
are explicit, not accidental:

- **Version conflicts resolve by highest-version-wins.** If two paths in the graph request
  different versions of the same package, the resolver keeps the higher one and re-resolves from
  there. Real NuGet does full version-range negotiation; spec §22.3 deliberately asks for
  "dependencias transitivas simples" instead.
- **A dependency version string may be a range** (`"[3.1.1, 4.0.0)"`, `"[3.1.1]"`, `"(1.0.0, )"` —
  real `.nuspec` files use these routinely; `ClosedXML@0.105.0`'s own dependency on
  `DocumentFormat.OpenXml` is declared exactly this way). `ParseMinVersion`
  (`internal/nuget/version.go`) always resolves to the range's lower bound — the same "lowest
  applicable version" behavior real NuGet defaults to for a plain `PackageReference` with no
  floating notation — rather than round-tripping nuget.org to find the actual highest version
  satisfying the range.
- A dependency cycle (vanishingly unlikely in a real graph, but not impossible) is detected and
  reported as an error rather than looping forever.

Each resolved node becomes a `ResolvedPackage`: its selected asset path and TFM (or an
`Unselectable` reason if nothing could be selected — see below), plus its own direct dependency
IDs. `NuGetManager.Restore()` feeds `Resolver.Resolve`'s output straight into `BuildLockFile`.

### Wiring the resolved graph into the interpreter

Resolving the graph on disk is only half the story — `VM.LoadPackage` (`nuget.go`) is what makes
cross-package calls actually execute. Since Fase 3.27 (the phase that made `Jint.Engine.Evaluate()`
run for real), `Assembly` carries a `deps []*Assembly` field and a `WithDependencies(...*Assembly)`
method; every one of the interpreter's method/field resolvers falls back to `asm.deps` when a
symbol isn't found in the local assembly. `LoadPackage` builds this automatically:
`loadLockedPackage` recursively walks the lockfile's own already-computed `Dependencies` list for
the requested package ID, loads each dependency's own selected asset (skipping any dependency with
no selectable managed asset — see below — not as an error, since it may legitimately have nothing
of its own to load), and attaches each loaded dependency via `WithDependencies`. Diamond
dependencies (a package reachable through more than one path in the graph) are loaded once and
cached within a single `LoadPackage` call, and a dependency cycle terminates cleanly rather than
recursing forever.

This is what makes Jint's real dependency chain — `Jint` → `Esprima` → `System.Memory` →
`System.Buffers`/`System.Numerics.Vectors`/`System.Runtime.CompilerServices.Unsafe` — resolve
end-to-end at runtime: a call from Jint's own IL straight into one of Esprima's own types isn't a
special case, it's the same resolver fallback every local call already uses, just extended across
an assembly boundary. Since Fase 3.40, `LoadPackage` also builds a shared cross-package type index
(`globalTypeIndex`) as a last-resort fallback for a generic method's `typeof(T)` resolving against
a type declared in one of its own dependents — best-effort, never required for a package to load,
skipped silently for any `TypeDef` row it can't decode.

## The lockfile (`vmnet.lock.json`)

`vmnet.lock.json` is vmnet's own resolved-dependency-graph format (`internal/nuget/lockfile.go`) —
explicitly documented as *not* compatible with real NuGet's `packages.lock.json`, in the same
spirit (reproducible restores that don't silently drift between machines/runs) but deliberately
simpler, matching the resolver's own deliberate simplifications above. Its shape, matching spec
§22.6:

```json
{
  "version": 1,
  "target": "netstandard2.0",
  "packages": [
    {
      "id": "NodaTime",
      "version": "3.2.0",
      "selectedAsset": "lib/netstandard2.0/NodaTime.dll",
      "unselectable": "",
      "dependencies": []
    }
  ]
}
```

`BuildLockFile` sorts packages by ID and each package's own dependency list alphabetically, so two
restores of the identical dependency graph always produce byte-identical output — a lockfile diff
in version control only ever shows a real change, never resolver/map-iteration nondeterminism.
`WriteLockFile`/`ReadLockFile` round-trip through `encoding/json` with no lossy fields: every field
`BuildLockFile` writes survives a read back unchanged (covered directly by
`internal/nuget/lockfile_test.go`), which is what lets `LoadPackage` trust the lockfile's own
`SelectedAsset`/`Dependencies` at load time without re-running resolution.

`NuGetManager.Packages()` reads the lockfile back and returns each entry as a public `vmnet.Package`
— the same fields, just without requiring a caller to import the `internal/nuget` package to
inspect what got resolved.

## What's explicitly NOT supported

vmnet is honest about two categories of package content it cannot execute — both are detected and
reported with a reason, never silently dropped or misreported as "compatible":

**Native-only assets.** `Package.HasNativeAssets()` detects `runtimes/*/native/*` content — real
native binaries (a `.dll`/`.so`/`.dylib`) a package ships instead of, or alongside, managed IL.
vmnet is pure Go with no CGo/native-loading path, so a package whose *only* selectable content is
native is marked `Unselectable` with an explicit reason
(`"package only ships native assets (...) — unsupported in pure-Go mode"`), both in
`SelectLibAsset`'s own return value and, end to end, in the lockfile's `unselectable` field and
`vmnet check package`'s `Status: unsupported` output. This is a deliberate, permanent limitation
(spec §22.5 tier 5), not a bug to be silently swallowed — a Go host calling `LoadPackage` on such a
package gets a clear error pointing at `Packages()` for the reason, and a dependency reachable only
in native form is skipped when wiring `WithDependencies`, exactly like any other package with
nothing of its own to load.

**Reference-only assemblies (`ref/`).** A `ref/<tfm>/Foo.dll` is a real .NET convention: a
compile-time-only reference assembly whose method bodies are stripped (real signatures, no real
IL), used by the .NET build toolchain purely to resolve API shape at compile time while a separate
runtime asset supplies the actual implementation. `SelectLibAsset` only falls back to `ref/` when
no `lib/` asset is selectable for the target TFM at all, and marks the result `ReferenceOnly: true`
with an explicit note: `"selected from ref/ (compile-time reference only) — cannot be executed,
only inspected"`. The practical implication: vmnet's checker and metadata reader can open a
`ref/`-only asset and tell you its real API surface — type names, method signatures — but there is
no method body to interpret, so nothing in it can actually run. `vmnet check package` surfaces this
directly (`Note: reference-only asset (ref/) — inspected, but cannot be executed`); a package that
only offers a `ref/` asset for vmnet's target TFM is inspectable but not a candidate for
`LoadPackage`-and-run. When a package ships *both* `lib/` and `ref/` for compatible TFMs, vmnet
always prefers the real `lib/` implementation — `ref/` is strictly the last resort, never chosen
over a real, executable asset.

Neither of these is silent: every `Unselectable` reason and every `ReferenceOnly` flag survives
all the way from `Package.SelectLibAsset` through the `Resolver`, into the lockfile, and out to
both `NuGetManager.Packages()` and `vmnet check package`'s own console output — the same
"explain, don't just fail" posture spec §23 requires of the compatibility checker applies equally
here.

## `vmnet check package` — evaluate a package before writing any Go code

The recommended way to find out whether a real NuGet package will actually work in vmnet is not to
add it to your project and see what breaks — it's `vmnet check package`, which does the entire
fetch/select/resolve/analyze pipeline read-only, without touching your project's manifest or
lockfile:

```bash
vmnet check package NodaTime@3.2.0
vmnet check package --profile=netstandard-lite Jint@3.1.3
vmnet check package Newtonsoft.Json          # no @version: resolves nuget.org's latest
```

End to end (`runCheckPackage` in `cmd/vmnet/main.go`), this: downloads (or reuses the cached)
`.nupkg` straight from nuget.org, calls `SelectLibAsset` and prints the selected TFM and asset path
(or the `Unselectable`/`ReferenceOnly` note above if that's what applies), resolves the package's
*full* transitive dependency graph exactly the way `vm.LoadPackage` does at runtime, decodes every
dependency's own metadata, and finally runs `checker.AnalyzeWithDeps` — the same static analyzer
`docs/en/COMPATIBILITY.md`'s per-package numbers come from, given the same real dependency context
so a call from the package's own IL straight into a resolved dependency's type (Jint → Esprima,
NPOI → ZString, ClosedXML → DocumentFormat.OpenXml) is checked against that dependency's real
methods instead of being misreported as unresolved just because only the top-level DLL was decoded
(Fase 3.29).

The output reports a status (`compatible`/`partial`/`unsupported`), the profile checked against,
how many methods were analyzed vs. flagged, and — this is the percentage `COMPATIBILITY.md` reports
per package — `(methods analyzed − methods flagged) / methods analyzed`. It is a coverage estimate
of what the static checker could confirm resolves against something vmnet actually implements, not
a correctness proof; a subtly wrong native implementation the checker has no way to see is exactly
why `COMPATIBILITY.md` also tracks real, running demos separately. `docs/en/compatibility-profile.md`
(a sibling doc) covers what each profile (`minimal`/`rules`/`netstandard-lite`) actually allows and
the full findings/report format in depth — this document only covers the NuGet-specific half of
the pipeline that feeds into it.

`vmnet bind package <id>@<version>` (`internal/bind`, Fase 3.75) is the other CLI command that
resolves a real package straight from nuget.org: `runBindPackage` (`cmd/vmnet/main.go`) reuses the
exact same `nuget.Client`/`nuget.Cache`/`OpenPackage`/`SelectLibAsset` building blocks `check
package` does to fetch the `.nupkg` and pick its best asset, but stops there rather than walking the
full transitive dependency graph — generating an idiomatic Go wrapper only needs the target
package's own decoded metadata, not its dependents'. `docs/en/compatibility-profile.md`'s §3.2
covers what it generates and how to use the result; this document's job is just to note that it
draws on the same fetch/select machinery documented above.
