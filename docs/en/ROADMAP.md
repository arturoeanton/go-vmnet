# vmnet — 4-phase execution plan

> Status: initial proposal · Date: 2026-07-02 · Repo in greenfield state (no code yet)

This document translates the complete technical specification of `vmnet` (pure Go IL/CIL
interpreter for running embedded C#/NuGet plugins in Go) into **4 executable phases**, each
closing with a **concrete demo** designed to secure approval/continued budget. Each
phase is a stage-gate: incremental value is demonstrated and a decision is made on whether to fund the next one.

Default staffing assumption: 1–2 dedicated senior Go engineers. Durations are
estimates in person-weeks; adjust according to the actual team.

Out of scope until v1.0 (reminder, see spec §3): ASP.NET Core, EF Core, WPF/WinForms,
Reflection.Emit, advanced `dynamic`, P/Invoke, `unsafe`, real threading, full async/await,
arbitrary NuGet, CoreCLR backend. These remain as post-v1.0 roadmap (v1.5 "hybrid backend").

---

## Executive summary (for stakeholders)

| Fase | Name | Est. duration | What it proves | One-line demo |
|---|---|---|---|---|
| 1 | Functional IL core | 5–7 wk | Technical viability: Go can parse and execute real IL | `vmnet run SimpleMath.dll Add 3 4` → `7`, with no .NET installed |
| 2 | Business rules engine | 6–8 wk | It's a usable product, not just a parser | Real C# rule engine called from Go via JSON, with sandbox |
| 2.5 | Hardening *(internal gate, no sales demo)* | 2–3 days | The interpreter doesn't crash the host under adversarial input or concurrency | `go test ./... -race` + fuzzing (~16.8M runs, 0 panics) |
| 3 | Checker + NuGet ecosystem | 6–9 wk | Low-risk adoption + reuse of existing libraries | 7 real NuGet packages checked, 3 with functions actually executing |
| 3.5 | Hardening + real compatibility *(internal gate, no sales demo)* | 3–4 days | The engine covers the most common real C# code pattern (arrays, `ref`/`out`, static fields), not just what was easy | Re-certification of the same 7 packages: average clean-method rate rises from ~45% to ~57% |
| 3.6+ | Path to 85% + Jint demo *(multi-phase; 85% reached in Fase 3.21, target revised to ~97%)* | several weeks | The engine runs a genuinely large portion of real C# code, not just curated cases — validated against a full JS engine (Jint), not just small libraries | 85%+ reached (85.1%/85.3% with Jint); ~97% in progress; `Engine().SetValue(...).Execute(...)` actually running |
| 4 | v1.0 production | 5–7 wk | Ready for real pilots | Benchmarks, security, docs, cross-platform CI, 5 min to "hello world" |

**Biggest project risk**: it's not the IL parser, it's the BCL (`System.*`). That's why the 4 phases
are ordered to expose that risk as early as possible (Fase 2) and mitigate it with a strong
checker (Fase 3) before promising broad compatibility.

---

## Fase 0 — Bootstrap (before Fase 1, ~3–5 days)

Not a "sellable" phase but a technical prerequisite.

- [ ] `go mod init` — decide the final module name (`github.com/arturoeanton/go-vmnet`,
      public package `vmnet`, internal codename `gocil`)
- [ ] Folder scaffolding per architecture (`/pe /metadata /il /ir /interpreter /runtime
      /bcl /nuget /checker /cmd/vmnet /examples /tests`)
- [ ] Minimal CI (GitHub Actions): build + test on Linux/macOS/Windows, `CGO_ENABLED=0`
- [ ] `/tests/fixtures/csharp`: .NET SDK project with the fixtures from spec §29
      (`SimpleMath`, `Strings`, `Loops`, `Objects`, `CollectionsTest`, `ExceptionTest`) +
      build script (`Makefile`/`justfile`) that compiles the test `.dll`s.
      **Important note**: the .NET SDK is a *dev-only* dependency (to generate
      the test binaries), never a `vmnet` runtime dependency — this needs to be communicated clearly to
      avoid confusing stakeholders.
- [ ] `docs/en/architecture.md` skeleton (referencing this spec), `CONTRIBUTING.md`
- [ ] Short ADR documenting the decision of "pure Go, no cgo, no hostfxr" as the core

---

## Fase 1 — Functional IL core ("Proof of Concept")

**Goal:** demonstrate that Go can read a real `.NET` assembly (compiled with Microsoft's
official compiler, unmodified) and correctly execute a subset of IL, with no .NET
runtime installed. This is the project's biggest technical risk and is tested first.

### Tasks

**`/pe` — PE/CLI loader**
- [x] DOS header, PE header, COFF header, optional header
- [x] Section headers + RVA → file offset conversion
- [x] Locating the CLI header and metadata root
- [x] Errors: `ErrInvalidPE`, `ErrMissingCLIHeader`, `ErrInvalidRVA`, `ErrInvalidMetadataRoot`
- [x] Tests: valid/invalid PE, no CLI header, invalid RVA, multiple sections

**`/metadata` — metadata loader**
- [x] Streams: `#~`, `#Strings`, `#US`, `#Blob`, `#GUID`
- [x] Core tables: Module, TypeRef, TypeDef, Field, MethodDef, Param, MemberRef, Constant,
      StandAloneSig, Assembly, AssemblyRef (the rest of the §10.2 tables parse without failing via
      the generic schema, though not used yet)
- [x] Token model + coded index resolution
- [x] Signature parser: primitives, `SZARRAY`, `CLASS`, `VALUETYPE`, `MethodDefSig`,
      `LocalVarSig` (generics/`GENERICINST` are parsed so as not to break alignment, but are
      exposed as `SigUnknown` — real resolution in Fase 2/3)
- [x] Per-table tests + signature decoding (against the real fixtures DLL)

**`/il` — decoder**
- [x] Complete opcode table (v0.1 set from spec §11.2 + v0.2+ opcodes from §11.3, all
      recognized by the decoder — see scope note below)
- [x] `Instruction{Offset, OpCode, Operand}` with offset tracking
- [x] Method header (tiny/fat) + recognition of unsupported opcodes without crashing

**`/ir`**
- [x] IR instruction set (`LoadArg`, `LoadLocal`, `StoreLocal`, `LoadConstI4`, `BinOp`,
      `Call`, `Branch`, `Return`, ...)
- [x] IL → IR builder, with an explicit error localized (IL offset) for any opcode the
      IR doesn't lower yet (callvirt, newobj, ldfld, arrays, exceptions — Fase 2)

**`/interpreter` + `/runtime` (minimum viable)**
- [x] Frame/stack model, `eval` loop, dispatch
- [x] Arithmetic + branches + loops
- [x] Static method resolution and invocation (includes calls to native BCL and to other
      static methods of the same assembly, with a recursion depth limit)
- [x] Minimal `Value`/`Method` runtime model
- [x] Limits: `MaxCallDepth`, `MaxInstructions` (`ErrCallDepthExceeded`,
      `ErrInstructionLimitExceeded`)

**`/bcl` (v0.1 subset)**
- [x] `System.Math.Abs`, `System.String.Concat`/`get_Length`, `System.Console.WriteLine`
- [x] Native registration mechanism (`bcl.Lookup`/`register`)

**`/cmd/vmnet` CLI**
- [x] `vmnet inspect <dll>` — lists types/methods
- [x] `vmnet il <dll> <Type.Method>` — dumps decoded IL
- [x] `vmnet run <dll> <Type.Method> '<json-array>'` — executes a static method

**Public Go API (subset of §6.1)**
- [x] `vmnet.New()`, `VM.LoadFile/LoadBytes`, `Assembly.Call`, `Value` types (Int32/Int64/
      Float32/Float64/String)

**Tests / acceptance**
- [x] Golden tests: `SimpleMath.Add`, `Strings.Hello`, `Loops.Sum` (Go API + CLI, against the real
      DLL compiled with the .NET SDK)
- [x] MVP acceptance criteria §35 #1–5, #9, #10, #11, #12

> **Scope adjustment vs. the original spec:** this phase's original task table included
> "basic object allocation + field read/write" citing MVP criteria #6–8 (spec §35: creating
> objects, reading/writing fields, basic `call`/`callvirt`). During implementation,
> those three points were moved to Fase 2 along with the rest of the object model
> (`newobj`/`callvirt`/instance fields), because none of the three Fase 1 demo methods
> (`SimpleMath.Add`, `Strings.Hello`, `Loops.Sum`) need them, and separating them avoids
> duplicating work once the full object model lands in Fase 2. The IL decoder does
> recognize `newobj`/`callvirt`/`ldfld`/etc. without crashing; the IR builder reports them as
> an explicit "unsupported opcode" (verified with a test) until Fase 2.

### Fase 1 closing demo — "This is real" (~10 min)

1. Show a plain `.cs` compiled with `dotnet build` unmodified — emphasize
   "this is exactly what Microsoft's official compiler produces."
2. `vmnet inspect SimpleMath.dll` → types/methods read from real metadata.
3. `vmnet il SimpleMath.dll SimpleMath.Add` → decoded IL, comparable to `ildasm`.
4. `vmnet run SimpleMath.dll SimpleMath.Add '[3,4]'` → `7`; then `Loops.Sum(1000)` to
   test branches/loops.
5. The same example from a Go program (`vmnet.New()` / `asm.Call(...)`), running on a
   machine/container **with no .NET installed** — to really drive the point home.

**Sales message:** "We built from scratch, in pure Go, a PE/CLI/IL parser and an interpreter
that execute real C# code. This was the riskiest 20% and it already works — everything else
builds on this foundation."

---

## Fase 2 — Business rules engine ("Usable product")

**Goal:** support the OO patterns of C# that appear in real code (classes, virtual
dispatch, collections, exceptions) and close the JSON bridge that turns this into a truly
usable product, with the first level of sandboxing.

### Tasks

**`/runtime`**
- [x] `newobj` + constructor execution (includes `System.Object::.ctor` as a native no-op)
- [x] Instance field read/write (`ldfld`/`stfld`, resolved by name)
- [x] `callvirt` resolved directly (no vtable) + null check → managed `NullReferenceException`
      if the receiver is `null`
- [x] Boxing/unboxing (`box`/`unbox.any`) as a no-op, since `runtime.Value` is already a uniform
      tagged union
- [ ] ~~Class hierarchy (BaseType, Interfaces) + real vtable~~ — deferred: no fixture
      needs polymorphism (overriding a virtual method in a subclass). `callvirt` today
      resolves the exact method of the token, not the runtime override.
- [ ] ~~Interface dispatch~~ — deferred, same reason

**`/bcl` (v0.2 subset)**
- [x] `System.Collections.Generic.List`1` (native Go backing): `Add`, `get_Count`, `get_Item`
- [x] `System.Collections.Generic.Dictionary`2` (native Go backing, **string keys only** —
      covers `Dictionary<string,string>`/`Dictionary<string,object>` from spec §17.1): `Add`,
      `get_Item`, `set_Item`, `ContainsKey`, `get_Count`
- [x] `System.Text.Encoding.UTF8.GetString`/`GetBytes` — needed for the `CallBytes` bridge
- [x] `System.String.Concat` extended to accept boxed arguments (not just string), as the
      C# compiler does in mixed concatenations
- [x] `System.Object.ToString()` (dispatches by the boxed value's `Kind`)
- [ ] Extended `System.String` (Substring, Equals, ToUpper/Lower, Split, Format) — deferred, no
      Fase 2 fixture requires it
- [x] `System.Array` + `SZARRAY` runtime support (`newarr`/`ldelem`/`stelem`/`ldlen`) —
      deferred at the time (see the `CallBytes` bridge scope note below), implemented
      in Fase 3.5
- [ ] `System.DateTime`, `System.TimeSpan`, `System.Guid` — deferred

**Generics (minimum, spec §17.1)**
- [x] Resolving `TypeSpec`/`GENERICINST` to the open generic type name (e.g.
      `List`1`), ignoring type arguments — sufficient because the native List/Dictionary
      backing doesn't need to know `T`

**Exceptions**
- [x] `throw` (`runtime.ManagedException`, re-exported as `vmnet.ManagedException`),
      propagated as a normal Go error via `%w` (`errors.As` works)
- [x] Native constructors for `System.Exception`/`InvalidOperationException`/
      `ArgumentException`/`ArgumentNullException`/`NotSupportedException`
- [ ] ~~`try`/`catch`/`finally` (`leave`, `leave.s`, `endfinally`)~~ — explicitly deferred:
      the Fase 2 demo only needs an **unhandled** exception to reach Go as a clear error,
      not for C# to catch it. The IL decoder already recognizes `leave`/`endfinally`; the IR
      builder reports them as an unsupported opcode if they appear.
- [ ] Multi-frame stack trace format (spec §18.3) — today the error is single-frame
      (`Type.Method: Exception: message`), without the full `at ...` chain

**JSON bridge + public API**
- [x] `Assembly.CallBytes`, `Assembly.CallJSON`

**Sandbox**
- [x] `MaxInstructions`/`MaxCallDepth` connected to the eval loop since Fase 1, now verified
      with a real infinite-loop fixture
- [ ] `MaxHeapBytes`, `MaxStackDepth`, `Permissions` (`AllowConsole` stub) — deferred to Fase 4
      (spec already groups them there as part of the full security model)

**Tests**
- [x] `Objects` (Customer), `CollectionsTest`, `ExceptionTest` fixtures — already existed since
      Fase 0, now actually executable
- [x] New `Rules.cs` fixture: objects + `List<int>` + `Dictionary<string,int>` + `Encoding` +
      throw, all in a single `Eval(byte[]) -> byte[]` method
- [x] `Loops.Runaway()` fixture: real infinite loop, killed by `MaxInstructions`
- [x] Golden tests: `TestFase2Demo` (CallBytes, CallJSON, typed managed exception via
      `errors.As`, sandbox), `TestObjectsAndCollections`

> **Scope adjustment vs. the original spec:** as in Fase 1, what the concrete demo doesn't
> need was trimmed. No vtable/interface dispatch (nothing uses real polymorphism), no
> `try/catch/finally` (the demo is "unhandled exception reaches Go," not "C# catches it"), no
> `System.Array`/`SZARRAY` (the `CallBytes` bridge passes `byte[]` back and forth without C#
> indexing it — see the comment in `tests/fixtures/csharp/Rules.cs`), no `DateTime`/`Guid`. Everything
> deferred is documented here instead of failing silently: the `IR builder` reports an
> explicit error with the opcode name if a third-party assembly needs something from this
> list.

### Fase 2 closing demo — "This is the product" (~10–15 min)

Runnable today with `examples/rules` (`go run .` after building the fixtures):

1. Real `Rules.Eval`: a `Customer` class with properties (`callvirt` on the compiler-generated
   accessors), a `List<int>`, a `Dictionary<string,int>` of taxes.
2. From a Go host, `asm.CallJSON("Vmnet.Fixtures.Rules", "Eval", "checkout request")` →
   `map[customer:acme corp ok:true]` — JSON in/out with no manual serialization code.
3. Invalid input (`CallBytes` with `[]byte("")`) → managed exception caught as a typed Go
   error (`errors.As(err, &vmnet.ManagedException{})`): `System.InvalidOperationException:
   empty input`.
4. `Loops.Runaway()` (real infinite loop) → `MaxInstructions` kills it in ~100ms instead of
   hanging the host process.
5. Hot-swap `Rules.dll` for `Rules_v2.dll`, without recompiling or restarting the
   Go binary — emphasize "hot-swappable business logic" (demo choreography, no new
   code required: `vm.LoadFile` already supports loading multiple assemblies).

**Sales message:** "This is what a customer buys: C# business rules safely embedded
in a Go service, with fault isolation and a one-liner JSON in/out."

---
## Fase 2.5 — Hardening (before Fase 3, ~2–3 days)

**Objective:** Fase 3 (checker + NuGet) adds new surface on top of an interpreter that, until
now, only ran its own trusted assemblies. Before that, close the robustness gaps that were
documented as debt during Fase 1/2 — especially the ones that break the core promise that
"a plugin cannot bring down the host". This isn't a phase with a sales demo; it's an internal
quality gate, but it leaves concrete evidence (fuzzing, `-race`) to back up the security
argument later in Fase 4.

### Tasks

**`internal/interpreter` — the interpreter cannot crash the host process**
- [x] `recover()` at the public boundary (`Machine.Invoke`): any panic anywhere in the
      invocation tree (missing bounds check, type assertion, malformed IR) is converted into a
      regular `error` instead of propagating to the caller's goroutine
- [x] Real `Limits.MaxStackDepth` — it existed in the `Limits` struct since Fase 1 but was never
      enforced; a plugin that does `push` without `pop` (bug or adversarial) could grow the
      stack without bound until it hit `MaxInstructions` (potentially hundreds of MB before
      failing). It's now cut off with `ErrStackOverflow` much earlier.
- [x] Direct tests with hand-built IR (`internal/interpreter/eval_test.go`): recovered panic,
      `MaxStackDepth` triggered, `MaxCallDepth` triggered by infinite recursion

**`vmnet` (root package) — concurrency safety**
- [x] `*Assembly` is now safe to call from multiple goroutines: the `methods`/`types` caches
      are populated lazily on first use, and without a lock two goroutines writing to the same
      map at the same time panic (`fatal error: concurrent map writes`, not recoverable with
      `recover()`). A `sync.RWMutex` was added.
- [x] `TestConcurrentCalls`: 32 goroutines calling the same `*Assembly` in parallel, run with
      `-race`

**Native Go fuzzing (`internal/pe`, `internal/metadata`, `internal/il`)**
- [x] `FuzzParse` in `/pe` and `/metadata`, `FuzzDecode` and `FuzzReadMethodBody` in `/il` —
      arbitrary bytes must never panic, only return an error. The seed corpus (includes the
      real fixtures DLL) runs as regular tests under `go test`, at no CI cost
- [x] Real fuzzing runs done locally: ~16.8M combined executions (pe + metadata + il), 0 panics
      found

**CI**
- [x] `go test ./... -race` on Linux/macOS (GitHub Actions Windows-hosted runners don't
      reliably have a C toolchain for the race detector, which needs cgo — it runs without
      `-race` there, still covered by the rest of the matrix)
- [x] The `Build` step still forces `CGO_ENABLED=0` explicitly, so as not to lose the "pure Go"
      guarantee just by enabling `-race` in Test

### What was explicitly left out of this gate

This is not a complete hardening pass — there's still documented debt that doesn't block Fase 3:

```txt
- MaxHeapBytes / logical memory accounting: still deferred to Fase 4 (full Permissions/sandbox),
  same as in the original plan.
- Frame.pop() still has no explicit bounds check (it relies on recover() as the safety net
  instead of returning a more specific error right there). Enough for "doesn't crash the
  host"; a more precise error message is a future improvement, not a security requirement.
- Not every slice `data[a:b]` in /pe and /metadata was exhaustively audited — the fuzzing run
  so far (seconds, not hours) is evidence of robustness, not a formal guarantee. It's worth
  running longer fuzzing (`-fuzztime=1h`+) periodically, not just once in Fase 2.5.
```

### How to verify this phase

```bash
go test ./... -race                                              # all green, no race warnings
go test ./internal/interpreter/... -run TestInvoke -v             # recover / MaxStackDepth / MaxCallDepth
go test ./ -run TestConcurrentCalls -race -v                      # concurrency in Assembly
go test ./internal/pe/... -run '^$' -fuzz '^FuzzParse$' -fuzztime=30s
go test ./internal/metadata/... -run '^$' -fuzz '^FuzzParse$' -fuzztime=30s
go test ./internal/il/... -run '^$' -fuzz '^FuzzDecode$' -fuzztime=30s
```

---

## Fase 3 — Compatibility checker + NuGet ecosystem ("Trust + reuse")

**Objective:** lower adoption risk by stating exactly what runs and what doesn't, and open the
door to reusing already-published NuGet packages instead of relying only on our own DLLs.

### Tasks

**`/checker`**
- [x] Analyzer that reuses the real pipeline (`il.Decode` → `ir.Build` → resolving each
      `Call`/`NewObj`/`CallVirt` against `bcl.Lookup`/`bcl.LookupCtor`/local methods) instead of
      reimplementing separate heuristics — if the checker says "compatible", `Assembly.Call`
      will actually run it, because it's literally the same resolution code
- [x] Detection of P/Invoke (`ImplMap` table), unsafe pointers (`SigPointer`, real typing added
      in Fase 3 — previously it collapsed together with `byref`/generics into `SigUnknown`), and
      `ref`/`out` parameters (`SigByRef`, not executable yet — its own finding, not just "not
      supported")
- [x] Report model with categorized `FindingKind` (`unsupported-opcode`,
      `unsupported-bcl-method`, `reflection`, `async`, `p-invoke`, `unsafe-pointer`,
      `byref-parameter`, `out-of-profile`) and a suggestion per finding (spec §23.2–23.4)
- [x] `minimal` (rejects the *entire* object model, not just individual opcodes — spec §24.1),
      `rules`, `netstandard-lite` profiles — implemented as a real allowlist of BCL prefixes +
      allowed IR instructions, not decorative metadata
- [x] `vmnet check <dll> [--profile=...]` and `vmnet check package <id>@<version> [--profile=...]`

**`/nuget`**
- [x] `.nupkg` reader (zip, stdlib's `archive/zip`, 256MB-per-entry limit against zip bombs)
- [x] `.nuspec` parser: TFM-grouped form and legacy flat form, **real long form**
      (`.NETStandard2.0`, `.NETFramework4.7.2`, ...) in addition to the short one — verified
      against real `.nuspec` files, not just the spec
- [x] TFM parser with a general regex for both notations + the exact priority from spec §22.5
      (`netstandard2.0` > `netstandard2.1` > `net5.0+` only with opt-in `AllowModernNet` > `ref/`
      analysis-only > `runtimes/*/native` unsupported)
- [x] Real transitive dependency resolver (full closure, cycles detected, highest-version-wins
      documented as a simplification vs. real NuGet)
- [x] Local package cache (`.vmnet/packages/`, atomic write via temp file + rename)
- [x] JSON lockfile (spec §22.6) + our own manifest (`vmnet.json`, equivalent to
      `<PackageReference>` since vmnet has no project file)
- [x] CLI: `vmnet add <id>[@version]`, `vmnet restore`, `vmnet packages`
- [x] Real NuGet v3 client (`api.nuget.org/v3-flatcontainer`, hardcoded endpoint — see scope
      note)
- [x] Public Go API: `vm.NuGet().Add/Restore/Packages()`, `vm.LoadPackage(id)`

**Generics — unplanned finding, more valuable than what it replaced**
- [x] `MethodSpec` resolution (table `0x2B`, generic method instantiation: e.g.
      `Guard.Against.Null<T>`) — discovered DURING certification of real packages that this was
      the cause of most "unsupported call target" cases in real libraries, not missing specific
      BCL methods. It's resolved by unwrapping to the underlying `MethodDef`/`MemberRef`,
      ignoring the type arguments (same as was already done for `TypeSpec`)

**Signed/unsigned comparison fix — a real bug found by testing, not just debt**
- [x] `div.un`/`rem.un`/`shr.un`/`cgt.un`/`clt.un` and the `bge.un`/`bgt.un`/`ble.un`/
      `blt.un`/`bne.un` branches now have real **unsigned** semantics, distinct from their
      signed counterparts. Previously both collapsed into the same signed operation — it worked
      for the project's own fixtures (non-negative integers) but produced **silently
      incorrect results** in the idiomatic C# pattern `(uint)(c - low) <= high` (a very common
      range check in real code). It was found by running `System.HexConverter.IsHexUpperChar`
      from `System.Text.Json` against the character `' '` and seeing `true` instead of `false`.

**BCL v0.3 — superseded by the above**
- [ ] ~~`System.Linq.Enumerable` subset~~ — deferred: it requires delegates/lambdas (spec §20,
      never implemented in any phase), which is a big new feature, not a loose BCL method.
      Without this, LINQ isn't viable even if `Where`/`Select` are added as names.
- [ ] ~~`System.Nullable<T>`, `System.Convert`, reflection-lite (`typeof`/`GetType`)~~ —
      evaluated against the 7 real packages tested (see certification below): the measured
      impact was low compared to `MethodSpec` and unsigned comparisons, which **were**
      implemented. A priority adjustment, not overlooked pending work.

**Tests**
- [x] Checker: the project's own assembly self-certifies as compatible under
      `rules`/`netstandard-lite` (`TestAnalyze_OwnAssemblyIsCompatible`), the `minimal` profile
      rejects the object model, a new fixture `Unsupported.cs` (uses `System.Array`,
      deliberately unsupported) tests the exact finding, compatible/partial/unsupported
      boundaries tested with synthetic data
- [x] NuGet: TFM (short and long forms, priority, `net6.0-windows` excluded, opt-in
      `AllowModernNet`), `.nuspec` (grouped + legacy + malformed XML), resolver (transitive
      chain, diamond with version conflict resolved to the highest, cycle detection, a
      dependency with no selectable asset doesn't abort resolution), lockfile and manifest
      round-trip — all with synthetic in-memory `.nupkg` packages, no network
- [x] Native Go fuzz tests: `FuzzParseNuSpec`, `FuzzOpenPackage` (in addition to the Fase 2.5
      ones in pe/metadata/il) — combined manual runs ~5.3M executions, 0 panics

### Real NuGet package certification

**7 real, popular packages** downloaded live from `api.nuget.org` were tested against
`vmnet check package --profile=netstandard-lite` (metrics with the code state at the close of
Fase 3, including the `MethodSpec` and unsigned fixes):

| Package | Methods analyzed | Clean methods | Main reason for what's missing |
|---|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 285 | 85.6% | `ldsfld`/`ldarga.s` in overloads with a custom message |
| `Newtonsoft.Json@13.0.3` | 4064 | ~46% | `System.Array`, static fields, some reflection |
| `System.Text.Json@8.0.5` | 3577 | ~41% | `System.Array`, `byref`, low-level intrinsics |
| `FluentValidation@11.9.2` | 1289 | ~41% | heavy reflection — matches the example in spec §23.4 |
| `Semver@2.3.0` | 423 | ~38% | `byref`, `System.Array` |
| `Humanizer.Core@2.14.1` | 1597 | ~34% | `System.Array`, text-formatting BCL |
| `SimpleBase@4.0.0` | 258 | ~33% | `byref`, `System.Array` (encoding algorithms) |

None certifies as 100% "compatible" — expected and honest: these are real libraries that use
arrays, static fields, and reflection extensively, none of which is in the current scope
(`docs/en/ROADMAP.md` already documents it as deferred). The checker's value is exactly in
showing this precisely, method by method, instead of inflating the result.

**But beyond that, `vmnet` executes real functions from 3 of those packages**, without
modifying the `.dll` or the source code — spec §36 asks for certifying "pure" NuGet packages
with real execution:

- `Newtonsoft.Json.Utilities.MathUtils.ApproxEquals(double, double)` — floating-point comparison
  with epsilon, including real edge cases
- `System.HexConverter.IsHexUpperChar(int)` — the same method that exposed the unsigned
  comparison bug; it now passes, including the `' '` case that used to fail
- `SimpleBase.Base32.getAllocationByteCountForDecoding(int)` — integer arithmetic

Verifiable with `VMNET_NETWORK_TESTS=1 go test ./ -run TestCertifiedNuGetPackages -v` (downloads
the three packages live) or by running `examples/nuget-basic` (adds + restores + runs real
`SimpleBase` via the public API, including resolving its 4 transitive dependencies).

### Scope notes

```txt
- NuGet client: hardcoded flat-container endpoint (api.nuget.org), no discovery via
  v3/index.json — private feeds/mirrors are not supported yet. Documented, not accidental.
- Version resolution: highest-version-wins, not NuGet's real range algorithm.
- DependenciesFor does not re-validate the TFM against vmnet's selection rules — it assumes
  the caller (the resolver) already picked a valid TFM. This was found and fixed during
  testing: the first version conflated "which dependency group corresponds to this TFM" with
  "is this TFM valid for vmnet", which are different questions.
- System.Array, try/catch/finally, delegates/lambdas (and therefore LINQ), reflection beyond
  what the checker already resolves generically: still unsupported at the close of Fase 3. Based
  on the data in the table above, System.Array (and, as discovered while measuring in Fase 3.5,
  `ref`/`out` more than reflection) was the real #1 blocker in real NuGet packages —
  **System.Array, `ref`/`out`, and static fields were implemented in Fase 3.5** (see that
  section below for the re-certification); try/catch/finally, delegates, and extended
  reflection are still pending.
```

### Fase 3 closing demo — "We know what works, and we reuse the ecosystem" (~10 min)

1. `vmnet check package FluentValidation@11.9.2 --profile=netstandard-lite` → "partial" report
   with concrete grouped reasons (reflection, opcodes), matching the example in spec §23.4
   almost verbatim.
2. `vmnet check package SimpleBase@4.0.0` → also "partial", but show that it's honest:
   258 methods analyzed, not a generic "doesn't work".
3. `examples/nuget-basic`: `vmnet add SimpleBase@4.0.0` + live restore (resolves 4 real
   transitive dependencies) + `vm.LoadPackage("SimpleBase")` + actually calling
   `Base32.getAllocationByteCountForDecoding`, with correct results.
4. Technical bonus (for an engineering audience): tell the story of how the unsigned
   comparison bug was found by testing real `System.Text.Json` — the checker and the
   certification aren't just demos, they found and drove a real correctness fix.

**Sales message:** "We don't promise the world — we transparently test exactly what C# code
runs, with real numbers across 7 popular libraries. And we already run real functions from
published NuGet packages, not just our own toy DLLs — the process of testing it against real
code made us find and fix a correctness bug that no test of our own would have caught."

---
## Fase 3.5 — Hardening + real DLL compatibility (before Fase 4, ~3–4 days)

**Goal:** Fase 3 certification measured precisely what was missing, and blocker #1 was not
reflection or async — it was boring, ubiquitous C# code patterns: `System.Array`,
`ref`/`out` (address-of), and static fields. Before entering Fase 4 (production), close these
three gaps and re-measure against the same 7 packages, so Fase 4 starts with an engine that
already runs a substantially larger portion of real code, not just our own fixtures.

Same as Fase 2.5, this is not a sales-demo phase — it's an internal quality gate, but with
concrete before/after metrics that do serve as evidence of real progress.

### Tasks

**Data-driven prioritization, not intuition**
- [x] Before writing any code: a temporary probe was run (`checker.Analyze` against the 7
      packages already downloaded in Fase 3) to count findings grouped by opcode/kind. The
      result completely reordered the expected priority: address-of opcodes
      (`ldarga`/`ldloca`/`starg`/`ldflda` — the basis of `ref`/`out`) were the measured #1
      blocker (2532 findings), far ahead of arrays (295) and static fields (689) — not what
      would have been assumed by looking only at the Fase 3 table.

**`internal/runtime`, `internal/ir`, `internal/interpreter` — System.Array**
- [x] `runtime.Array` (heap-allocated, SZARRAY only — no multidimensional arrays, covers the
      vast majority of real-world usage) and `runtime.Value.KindArray`
- [x] IR + interpreter: `newarr`/`ldlen`/`ldelem.*`/`stelem.*` (all typed variants collapse
      into a single implementation — `Value` is already a tagged union, so there's no need to
      distinguish by element type the way CIL does)
- [x] Real bounds-checking: out-of-range index or null array throw a `ManagedException`, not
      a generically recovered Go panic
- [x] `Limits.MaxArrayLength` (16M elements by default) — a `newarr` with an adversarial
      length cannot exhaust the host's memory

**`internal/runtime`, `internal/ir`, `internal/interpreter` — managed pointers (`ref`/`out`)**
- [x] `runtime.Value.KindRef`: a managed pointer is literally a Go `*Value` pointing into a
      fixed-size slice (`Args`/`Locals`/`Object.Fields`/`Array.Elems`). Key design decision:
      this means `ref`/`out` need *no* special case in `Call`/`NewObj` — a byref parameter is
      simply an `Arg` whose `Value` happens to have `Kind == KindRef`
- [x] IR + interpreter: `ldarga(.s)`/`ldloca(.s)`/`ldelema`/`ldflda` (address-of) and
      `ldind.*`/`stind.*` (read/write through the pointer)
- [x] `ldsflda` (address of a **static** field) deliberately **not** implemented: unlike the
      other four, exposing a raw `*Value` into a `Type`'s static slice (protected by a
      `sync.RWMutex`) would let whoever holds the pointer bypass that mutex on every future
      read/write — a real concurrency risk, not just pending work. Documented as an explicit
      gap, not a silent one.

**`internal/runtime`, `internal/interpreter` — static fields + lazy `.cctor`**
- [x] `runtime.Type` now carries real state: `statics []Value` (protected by
      `sync.RWMutex`, because unlike instance fields this really is mutable state shared
      across concurrent callers) and a `.cctor` that runs lazily on first static access,
      exactly once, via `sync.Once`
- [x] IR + interpreter: `ldsfld`/`stsfld`
- [x] **Real bug found and fixed — reentrancy deadlock**: a `.cctor` that writes its own
      static field (the *common* case, not a rare one — `static Foo() { Bar = 42; }`) would
      re-enter `EnsureCctor` on the same `sync.Once`, which is not reentrant, and hang the
      process. Fixed by tracking, per `Machine` (which is never shared across goroutines —
      one per top-level `Assembly.Call`), which types have their `.cctor` running in this
      same call chain; a reentrant access within the same chain no longer re-enters the
      `sync.Once`, while another goroutine arriving first still correctly blocks against the
      in-progress `.cctor`.
- [x] **Real bug found and fixed — race condition in the type cache**: before this phase,
      `resolveTypeByFullName` did "read from cache, if missing build and store" with separate
      locks for each step — harmless when `Type` only described field layout (immutable), but
      with statics and `.cctor` in the picture, two goroutines resolving the same type for the
      first time could each build *their own* `runtime.Type`; the `SetStaticField` calls from
      the "loser" of the race ended up on an object nobody else ever saw again. Fixed by
      holding a single lock over the whole "read or build and store", verified with
      `TestStaticsConcurrentCctor` (32 goroutines, `-race`, `-count=3`).
- [x] **Real bug found and fixed — incorrect default(T)**: a field (static or instance) never
      explicitly assigned (`static int Counter;`, without `= 0`, the common case in real C#)
      ended up with Go's empty `Value{}` (`KindNull`), which is not arithmetically compatible
      with any numeric type — the first arithmetic operation on that field failed. Added
      `metadata.ParseFieldSig` (new parser for the field signature, ECMA-335 §II.23.2.4) to
      compute the real `default(T)` per field — typed zero for every value type, `null` for
      everything else — and `runtime.Type` now stores `FieldDefaults`/`StaticFieldDefaults`
      alongside field names.

**`internal/checker` — the checker cannot silently go stale**
- [x] **Real drift found and fixed**: `sigShapeFindings` was still flagging every `ref`/`out`
      parameter (`SigByRef`) as unsupported, written before byref was implemented — the
      checker's own "dogfood" test (the fixtures assembly self-certifies against its own
      checker) caught it as soon as `ByRef.cs` was added, exactly the purpose of that test.
- [x] **Real drift found and fixed**: `instrIsObjectModel` (what the `minimal` profile
      excludes — spec §24.1, "static methods and primitives only") was never updated when
      arrays and static fields were added; the checker was certifying code that uses
      `System.Array` or shared static state as "compatible" under `minimal`, contradicting its
      own documented contract. `ldarga`/`ldloca`/`ldind`/`stind` over primitives are
      deliberately left **outside** that exclusion — a `ref int` never touches the heap or a
      class's layout, so it stays within what `minimal` promises.
- [x] Suggestion message for `newarr`/`ldelem`/`stelem`/`ldlen` (used to say "`System.Array`
      not supported yet") fixed — it's now supported; the only real case that still falls
      through that path is array-literal-initializer syntactic sugar (`ldtoken` +
      `RuntimeHelpers.InitializeArray`), not the opcode itself.

**Fixtures and tests**
- [x] `Arrays.cs`, `ByRef.cs`, `Statics.cs` — new fixtures compiled with the real .NET SDK,
      each with its corresponding Go test (`TestArrays`, `TestByRef`, `TestStatics`,
      `TestStaticsConcurrentCctor`)
- [x] `Unsupported.cs` rewritten (`try`/`finally`, previously used arrays — now that arrays
      work, it was replaced with another genuinely unsupported construct, so as not to lose
      the checker's negative test case)
- [x] `TestAnalyze_MinimalProfileFlagsObjectModel` extended: tests that arrays and static
      fields fall outside `minimal`, and that `ref`/`out` over primitives stay inside — guards
      against a future regression on either side
- [x] `FuzzParseSignatures` (`internal/metadata`) — the new `ParseFieldSig` parser receives
      untrusted bytes (part of the `#Blob` stream of a loaded DLL), and along the way it closed
      a gap where `ParseMethodSig`/`ParseLocalVarSig`/`ParseTypeSpec` had never had their own
      fuzz test (`FuzzParse` in `/metadata` only reaches the raw rows, never parses their
      signature blobs)

### What was explicitly left out of this phase

```txt
- ldsflda (address of a static field): see design note above — a real concurrency risk,
  not unexamined pending work.
- Multidimensional/jagged arrays beyond one dimension: SZARRAY only.
- Array-literal initializers (`new int[] {1,2,3}` compiles to newarr+ldtoken+
  RuntimeHelpers.InitializeArray, which reads raw bytes from a FieldRVA) — the same result
  can be achieved by assigning element by element, which does work.
- try/catch/finally (leave/endfinally), isinst/castclass, switch, ldftn/delegates, localloc,
  initobj (structs/value-type generics): confirmed as the next real blockers by volume in the
  re-certification below — natural candidates for the next interpreter-expansion phase, not a
  surprise.
- New BCL surface (DateTime, Span<T>/ReadOnlySpan<T>, Nullable<T>, String.Format,
  CultureInfo): still the dominant blocker in absolute finding volume
  (unsupported-bcl-method), but it's "add a native method" work, not interpreter work —
  out of scope for this phase, which focused on engine opcodes/semantics.
```

### Re-certification against the same 7 real packages

Same 7 packages from Fase 3, same profile (`netstandard-lite`), code state at the close of
Fase 3.5:

| Package | Methods analyzed | % clean Fase 3 | % clean Fase 3.5 |
|---|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 285 | 85.6% | **86.7%** |
| `System.Text.Json@8.0.5` | 3577 | ~41% | **60.5%** |
| `FluentValidation@11.9.2` | 1289 | ~41% | **58.0%** |
| `Semver@2.3.0` | 423 | ~38% | **56.0%** |
| `Newtonsoft.Json@13.0.3` | 4064 | ~46% | **52.5%** |
| `Humanizer.Core@2.14.1` | 1597 | ~34% | **43.0%** |
| `SimpleBase@4.0.0` | 258 | ~33% | **40.7%** |
| **Average** | | **~45.5%** | **~56.8%** |

`System.Text.Json` and `Semver` show the biggest jumps — both use `System.Array` and `ref`/
`out` extensively for low-level parsing/comparison, exactly the pattern this phase targeted.
The remaining findings (see opcode breakdown above) are no longer dominated by address-of —
they're now mostly `initobj`/`ldftn`/`isinst`/`switch`/`ldtoken`/`leave.s` (future-phase
features) and BCL surface (`DateTime`/`Span`/`Nullable`), not a coverage gap in the
interpreter on basic language constructs.

### How to verify this phase

```bash
go test ./... -race -count=3                                       # all green, includes concurrency
go test ./ -run TestStatics -v                                     # lazy .cctor + typed defaults
go test ./ -run TestStaticsConcurrentCctor -race -v                # 32 goroutines, no deadlock or data race
go test ./ -run TestArrays -v
go test ./ -run TestByRef -v
go test ./internal/checker/... -run TestAnalyze_MinimalProfileFlagsObjectModel -v
go test ./internal/metadata/... -run '^$' -fuzz '^FuzzParseSignatures$' -fuzztime=30s
```

---

## Fase 3.6+ — Path to 85% real compatibility + Jint demo

**Original goal:** before Fase 4, raise measured real compatibility to **at least 85%**
average (a firm closing criterion, not an aspirational one) over the 7 already-certified
packages **plus Jint** (a full JavaScript engine for .NET, ~5400 methods), and validate a real
"dynamic language running inside vmnet" demo executing the
`new Engine().SetValue(...).Execute(...)` pattern end to end — not just the aggregate number.
An ASP.NET Core/MVC demo was explicitly ruled out (documented out of scope, spec §3).

**85% was reached in Fase 3.21** (85.1%/85.3% with Jint — see that section). The goal was
revised upward at that point: the new closing criterion is a hardened BCL targeting **~97%**
("100% can be 97%"), with all documentation kept current at every sub-phase — the sequence of
sub-phases below continues under that goal, it does not stop at 85%.

Given the real size of the gap, this is **not a single phase**: it's a sequence of sub-phases,
each with its own measurement, tests, docs, and commit/tag/push — same as Fase 2.5/3.5, but
chained. The order was decided with the same method as Fase 3.5 (measure before guessing): the
same findings-per-opcode/BCL probe was run, now including Jint, against the 8 targets.

| Sub-phase | What it targets | Why that order |
|---|---|---|
| **3.6** | `switch` (jump table) + high-reach cheap BCL (`StringBuilder`, `String.Format`/`Substring`/indexer/`Equals`, `Array.Empty`, `Double.IsNaN`, `CultureInfo.InvariantCulture`, `ArgumentOutOfRangeException`, `Environment.CurrentManagedThreadId`) | High reach (several reach 6-8/8 packages), low cost — none of this needs a new type machine |
| **3.7** | Value types: `initobj`/`ldobj`/`stobj`/`constrained.` + `Nullable<T>` | 8/8 packages use `initobj`; vmnet doesn't model structs yet, only classes |
| **3.8** | Real type hierarchy + `isinst`/`castclass` | 8/8 and 6/8 packages; `runtime.Type` is flat today (no inheritance/interfaces); unlocks `EqualityComparer<T>` |
| **3.9** | Delegates/closures (`ldftn`, `Action<T>`/`Func<T>`, `Invoke`) | Needed for the literal Jint demo (`SetValue(new Action<string>(...))` is the first line) |
| **3.10** | `try`/`catch`/`finally` (`leave`/`leave.s`/`endfinally`) | 8/8 packages; today only unhandled throw exists |
| **3.11** | `foreach`/enumerators + cheap wins (re-prioritized with data — see section) | The probe showed `IDisposable::Dispose`/`IEnumerator`1`/`EqualityComparer`1` in 7-8/8 packages, wider than DateTime/Span (2-5/8) |
| **3.12** | `DateTime`, `Span<T>`/`ReadOnlySpan<T>`/`Memory<T>`/`ReadOnlyMemory<T>` | Large but concentrated impact (mainly Humanizer.Core/SimpleBase/System.Text.Json) |
| **3.13** | `foreach` over a collection typed as an interface (dispatch by the receiver's real type) + cheap-wins package (`String`/`Char`/`List`/`Dictionary`) | `IEnumerable`1::GetEnumerator`/`IEnumerator`1::get_Current`/`IEnumerator::MoveNext` were the widest finding (7/8) after Fase 3.12 |
| **3.14** | Reflection-lite: `ldtoken`/`typeof(T)`, `Object.GetType()`, `System.Type` (equality/`Name`/`FullName`) | `ldtoken` (6/8), `Object::GetType` (5/8) and `MemberInfo::get_Name` (5/8) were the three widest findings after Fase 3.13 |
| **3.15** | LINQ (`System.Linq.Enumerable`: `Select`/`Where`/`Any`/`All`/`ToList`/`ToArray`/`FirstOrDefault`) | ~174 cases across 4-5/8 after Fase 3.14, viable now that delegates (3.9), real enumerators (3.11), and interface dispatch (3.13) exist |
| **3.16** | `Type::IsAssignableFrom` | Second-widest reflection finding after 3.14 (84 cases, 4/8); mechanical once the Machine-aware registry from 3.15 exists |
| **3.x** | Final re-measurement, closing the remaining gap toward 85%, literal validation of the Jint demo | Confirms the number AND that the concrete scenario runs, not just the average |

### Fase 3.6 — `switch` + high-reach cheap BCL

**Tasks**

- [x] IR + interpreter: `switch` (spec §III.3.68) — was already being decoded since Fase 1
      (`internal/il/decoder.go` resolves the offset table), but `ir.Build` never lowered it;
      it fell through as an unsupported opcode. Out-of-range falls through to the next
      instruction (not an error, per spec), verified with the fixture.
- [x] `System.Text.StringBuilder`: ctor (parameterless + seed-string), `Append`/`AppendLine`
      (return the receiver — fluent chaining `sb.Append(a).Append(b)` works), `ToString`,
      `get_Length`, `Clear`.
- [x] `System.String`: `Format` (composite grammar `{index[,alignment][:formatString]}`,
      `{{`/`}}` escapes, `D`/`F`/`N`/`X`/`P`/`G` specifiers — an unrecognized one is an explicit
      error, not a guessed result), `Substring` (1 and 2 args), `get_Chars` (indexer),
      `Equals`/`op_Equality` (a single native covers instance + static + `==`, see the comment
      in the code).
- [x] `System.Array::Empty`, `System.Double::IsNaN`, `System.Globalization.CultureInfo::
      get_InvariantCulture` (stub), `System.Environment::get_CurrentManagedThreadId` (stub),
      constructor for `System.ArgumentOutOfRangeException`/`System.IndexOutOfRangeException`
      (same pattern as the exceptions already registered in Fase 2).
- [x] **Real bug found and fixed — `StringBuilder.ToString()` did nothing useful**: the C#
      compiler emits `sb.ToString()` as `callvirt System.Object::ToString` (it trusts the CLR's
      real virtual dispatch to reach the override), not as
      `callvirt System.Text.StringBuilder::ToString`. vmnet resolves `callvirt` statically via
      the declared `MemberRef` (spec: "no vtable" — real virtual dispatch is Fase 3.8), so
      without a fix this always ran `Object`'s generic `ToString` and returned `<object>`. It
      was fixed by extending `displayString`/`objectToString` (already designed to dispatch "as
      if it had a vtable" over boxed values) to recognize known native-backed types —
      StringBuilder for now, the same mechanism covers future cases.
      It's a targeted patch, not a general solution — real virtual dispatch arrives in Fase 3.8.
- [x] **Hardening**: `String.Format` caps the alignment width (`{0,N}`) at a fixed maximum —
      without the cap, a `{0,999999999}` (from an adversarial plugin or from the
      `CallBytes`/`CallJSON` bridge, where the format string can come from outside the process)
      would make `strings.Repeat` try to allocate hundreds of MB of padding. Same kind of risk
      as `MaxArrayLength` (Fase 3.5) for `newarr`.

**Fixtures and tests**

- [x] `SwitchTest.cs` (dense switch 0-4 + default) / `TestSwitch`, includes the out-of-range case
- [x] `StringOps.cs` (chained StringBuilder, Format, Substring, indexer, Equals) /
      `TestStringOps`

**Measurement (7 Fase 3 packages + Jint, `netstandard-lite` profile)**

| Package | % clean Fase 3.5 | % clean Fase 3.6 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 86.7% | 86.7% |
| `System.Text.Json@8.0.5` | 60.5% | 61.4% |
| `FluentValidation@11.9.2` | 58.0% | 62.8% |
| `Semver@2.3.0` | 56.0% | 63.8% |
| `Newtonsoft.Json@13.0.3` | 52.5% | 55.6% |
| `Humanizer.Core@2.14.1` | 43.0% | 45.2% |
| `SimpleBase@4.0.0` | 40.7% | 43.4% |
| **Average (7 packages)** | **56.8%** | **59.8%** |
| `Jint@3.1.3` (new, not in Fase 3.5) | — | 63.7% |
| **Average (7 packages + Jint)** | — | **60.3%** |

Modest movement (+3 points across the usual 7), expected for a "cheap wins" sub-phase — the big
jump is in 3.7-3.10 (value types, type hierarchy, delegates, try/catch), which are the dominant
blockers across the 8 targets by actual measured volume.

### How to verify Fase 3.6

```bash
go test ./... -race -count=1
go test ./ -run TestSwitch -v
go test ./ -run TestStringOps -v
```

### Fase 3.7 — Value types: `initobj`/`ldobj`/`stobj`/`constrained.` + `Nullable<T>`

**Tasks**

- [x] `runtime.KindStruct`/`runtime.Struct` (Fields by position, same as an object, but with
      **copy semantics** instead of shared reference) and `runtime.Type.IsValueType`
      (detected via `Extends == System.ValueType`/`System.Enum`, or registered directly for
      synthetic BCL types like `Nullable`1`)
- [x] `Value.Clone()`: a no-op for every Kind except `KindStruct`, where it clones `Fields`
      (recursively — a struct nested inside another struct also copies correctly). Wired into
      **every** point where a `Value` enters a persistent slot: `stloc`/`starg`/`stfld`/
      `stsfld`/`stelem`/`stind`, and the initial `Locals` setup of each invocation — without
      this, two struct-typed locals end up sharing the same underlying `*Struct` and mutating
      one mutates the other
- [x] IR + interpreter: `initobj` (real zero-init via address; `ldloca`/`ldflda`/etc. already
      existed), `ldobj`/`stobj` — turn out to be **exactly** `ldind.*`/`stind.*` reused with no
      new IR instruction, because a vmnet pointer is already a typed `*runtime.Value`, not raw
      memory — and `constrained.`/`volatile.`/`readonly.` as explicit no-ops (prefixes that
      don't apply to vmnet's `Value` model)
- [x] `newobj` over a value type pushes the **value**, not a reference (spec §III.4.21): it's
      built in a temporary slot, the `.ctor` is called with `this` = managed pointer to that
      slot (same as any instance method of a struct), and the value is pushed
- [x] `ldfld`/`stfld`/`ldflda` extended to accept a `KindRef → KindStruct` receiver in addition
      to `KindObject` — this is how a struct receives `this` in its own instance methods
- [x] `System.Nullable`1`: synthetic type with two fields (`hasValue`, `value`), native ctor,
      `get_HasValue`/`get_Value`/`GetValueOrDefault`
- [x] `System.Object::Equals`/`GetHashCode`: by-value equality/hash for primitives and structs
      (recursive field by field), by-reference for classes/arrays — necessary because
      `constrained.` + `callvirt Object::Equals/GetHashCode` is the actual most common pattern
      in generic comparison code (`EqualityComparer<T>`, Fase 3.8)
- [x] `metadata.SigType.GenericInstIsValueType`: the signature parser was discarding the
      CLASS/VALUETYPE marker byte of a generic instantiation (`GENERICINST`) — necessary to
      distinguish `List<T>` (reference, defaults to `null`) from `KeyValuePair<K,V>`/
      `Nullable<T>` (value, defaults to a zero struct) within the same `SigGenericInst`
- [x] **Real bug found and fixed — uninitialized struct locals**: `var p = new
      Point(3, 4);` assigned directly to a local compiles as `ldloca` + `call .ctor` **without**
      a prior `initobj` — the C# compiler relies on the CLI's `InitLocals` guarantee (all
      locals start at zero, not only the ones with an explicit `initobj`). vmnet initialized
      every local to Go's empty `Value{}` without looking at its declared type; for a struct
      local that means `KindNull`, not a zero struct, so the first `stfld` through the pointer
      failed with `NullReferenceException`. `runtime.Method.LocalDefaults` was added (parallel
      to `LocalCount`, resolved once when the method is built, same as already existed for
      fields), cloned on each invocation.
- [x] **Real bug found and fixed — recursion deadlock in `resolveTypeByFullName`**: the Fase 3.5
      lock (which covers the whole "read or build and store" cycle to prevent two goroutines
      from building duplicate `Type`s) assumed that building a type never needs to resolve
      ANOTHER type — true until a field or local of struct type needed to recursively resolve
      its own nested type, against Go's same non-reentrant `sync.Mutex`. Found immediately when
      running the first fixture with a struct. It was redesigned to
      "check cache → build WITHOUT the lock (may recurse) → check again and store": under a
      genuine race, both goroutines may build a complete `Type`, but only the winner is stored
      and every caller ends up seeing the same instance — the Fase 3.5 guarantee holds, only
      redundant work is lost in the race, not correctness.
      Verified with `TestStructsConcurrentResolve` (32 goroutines, `-race`, `-count=3`).

**Fixtures and tests**

- [x] `Structs.cs` (`Point`: struct with its own ctor and its own method) / `TestStructs` —
      in-place construction, `default`, copy semantics (mutating a copy doesn't affect the
      original — the case that most naive implementations get wrong), `constrained.` dispatching
      `ToString()` over a generic type parameter bound to a struct, and `Nullable<int>`
      end to end
- [x] `TestStructsConcurrentResolve` — concurrency hardening for the lock redesign

### What was explicitly left out of this phase

```txt
- `initobj` over a generic type parameter with no known closed instantiation (`initobj
  !!0` inside the body of a generic method, in the abstract) falls through to Null() — vmnet
  erases generic type arguments when resolving a MethodSpec (Fase 3, already-documented
  decision), so there's no way to know the real T at that point. Matches the pattern already
  accepted for other generics-erasure gaps.
- A foreign value type that vmnet doesn't model (DateTime, Guid, TimeSpan, KeyValuePair<K,V>
  beyond Nullable<T>, ...) also falls through to Null() instead of failing resolution of the
  whole method — same principle as an unresolvable Call target: the gap is reported at actual
  point of use, not when the method is loaded.
- `constrained.` only guarantees correct dispatch for ToString/Equals/GetHashCode (the three
  actual dominant cases measured). Other virtual overrides over a value type without a real
  vtable still go to the base implementation — genuine virtual dispatch is Fase 3.8.
```

### Re-certification against the same 8 targets (7 packages + Jint)

| Package | % clean Fase 3.6 | % clean Fase 3.7 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 86.7% | 87.4% |
| `Semver@2.3.0` | 63.8% | **72.6%** |
| `System.Text.Json@8.0.5` | 61.4% | **66.7%** |
| `FluentValidation@11.9.2` | 62.8% | 63.5% |
| `Newtonsoft.Json@13.0.3` | 55.6% | **60.6%** |
| `Humanizer.Core@2.14.1` | 45.2% | 46.0% |
| `SimpleBase@4.0.0` | 43.4% | 45.7% |
| **Average (7 packages)** | **59.8%** | **63.2%** |
| `Jint@3.1.3` | 63.7% | 66.1% |
| **Average (7 packages + Jint)** | **60.3%** | **63.6%** |

`Semver` and `System.Text.Json` are the biggest jumps — both do low-level parsing/comparison
leaning heavily on structs (ranges, logical spans, comparers). There's still considerable ground
left for the 85% target: real type hierarchy (`isinst`/`castclass`, Fase 3.8) and
delegates/closures (Fase 3.9) are the next blockers by measured volume.

### How to verify Fase 3.7

```bash
go test ./... -race -count=3
go test ./ -run TestStructs -v
go test ./ -run TestStructsConcurrentResolve -race -count=3 -v
```

### Fase 3.8 — Real type hierarchy + `isinst`/`castclass`

**Tasks**

- [x] `runtime.Type.BaseTypeFullName`/`Interfaces` (only directly implemented ones — spec
      §II.22.23; extending one interface with another, or inheriting from a base class, is
      resolved recursively during the walk, not flattened up front)
- [x] `metadata.InterfaceImpls` (new accessor for the `InterfaceImpl` table, unused until now)
      and `resolveTypeTokenName` extended to resolve a `TypeSpec` (generic interface
      instantiation — `IEnumerable<T>`/`IComparable<T>`, the *dominant* case in real
      `InterfaceImpl`) to its open generic type
- [x] IR + interpreter: `isinst`/`castclass` — same TypeDefOrRefOrSpec token as `initobj`
      (`resolveTypeTokenOrGeneric`, renamed from `resolveInitObjTarget` now that three opcodes
      share it), dispatching by Kind with the real hierarchy walk for `KindObject`/
      `KindStruct`, common-sense rules for primitives/string/array, and `null` always passes
      unchecked (behavior mandated by spec)
- [x] `internal/interpreter/typecheck.go`: `isAssignableTo` — walks `BaseTypeFullName` +
      `Interfaces` (recursive for interface-extends-interface) for the plugin's own classes;
      small hand-maintained table for the real exception hierarchy (`ArgumentNullException`
      → `ArgumentException` → `SystemException` → `Exception`) so that `ex is ArgumentException`
      gives the right answer over the exceptions vmnet already builds natively
- [x] `System.InvalidCastException` registered as a native exception (same pattern as the others)
- [x] **Real bug found and fixed — reference comparison against `null`**: the most common
      compiled form of `x is T`/`x != null`/`x == null` is exactly
      `<value> ldnull cgt.un`/`ceq` — comparing a `KindObject` against the literal `KindNull` of
      `ldnull`. `evalBinOp`/`evalCompare` required the same `Kind` on both sides and failed with
      "mismatched value kinds" as soon as the first `isinst` fixture exercised them — a
      preexisting gap no earlier fixture had touched (nothing had explicitly compared a
      reference against `null` via IL until now). Reference-identity/nullity comparison
      (`refEqual`/`refGreater` in `internal/interpreter/arithmetic.go`) was added for every
      reference-shaped Kind (`Object`/`Array`/`Ref`/`Struct`/`String`), including recursive
      structural equality for structs.
- [x] **Real bug found and fixed — inherited fields didn't exist**: `runtime.Type` had never
      needed to look beyond its own `TypeDef` (original comment: "no base-type field
      inheritance yet"). As soon as the first inheritance fixture (`Dog : Animal`) accessed a
      field declared on the *base* class, it failed with "has no field" — `Dog`'s field list
      never included `Animal`'s. This was fixed by building the base type recursively (same
      recursion-safe pattern as Fase 3.7) and prepending its fields, matching the CLR's actual
      memory layout (base fields first).

**Fixtures and tests**

- [x] `TypeChecks.cs` (`Animal`/`Dog`/`Cat`/`IShape`) / `TestTypeChecks` — `is`/`as`/explicit
      cast over a base-type reference that at runtime is a subtype, failed cast throwing
      `InvalidCastException` (not silently succeeding nor panicking), `isinst` against the
      exception hierarchy without needing try/catch (building the exception directly, since
      try/catch isn't until Fase 3.10)

### What was explicitly left out of this phase

```txt
- List<T>/Dictionary<T>/StringBuilder (native Go backing, no runtime.Type): isinst/castclass
  against them only recognizes System.Object, not their real interfaces (IEnumerable,
  ICollection<T>, IList<T>, ...) — nativeMatches in typecheck.go only models the exception
  hierarchy. It never produces a false positive (worst case isinst returns null when it should
  have matched), documented as a gap, not a silent bug.
- isinst/castclass against System.Enum specifically (vs. the generic System.ValueType, which
  does work) — vmnet doesn't yet distinguish "is an enum" from "is some struct" on the Type.
- Static field inheritance: each type has its own separate static storage, without inheriting
  from or sharing with the base — matches real CLR semantics (statics aren't laid out like
  instance fields), not a simplification.
```

### Re-certification against the same 8 targets (7 packages + Jint)

| Package | % clean Fase 3.7 | % clean Fase 3.8 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 87.4% | 87.4% |
| `Semver@2.3.0` | 72.6% | 74.9% |
| `System.Text.Json@8.0.5` | 66.7% | 68.1% |
| `Newtonsoft.Json@13.0.3` | 60.6% | 62.9% |
| `FluentValidation@11.9.2` | 63.5% | 63.9% |
| `Humanizer.Core@2.14.1` | 46.0% | 46.3% |
| `SimpleBase@4.0.0` | 45.7% | 46.1% |
| **Average (7 packages)** | **63.2%** | **64.2%** |
| `Jint@3.1.3` | 66.1% | **74.4%** |
| **Average (7 packages + Jint)** | **63.6%** | **65.5%** |

Jint is the big jump of this phase (+8.3 points) — a JS engine does type dispatch and casting
constantly (representing every JS value type as a subclass of `JsValue`, checked with `is`/`as`
throughout the evaluation code). The usual 7 packages climb more modestly (+1 point): they
already had proportionally less `isinst`/`castclass` relative to their size than Jint.

### How to verify Fase 3.8

```bash
go test ./... -race -count=3
go test ./ -run TestTypeChecks -v
```

### Fase 3.9 — Delegates/closures: `ldftn`, `Action`/`Func`, `Invoke`

**Tasks**

- [x] `runtime.KindFunc`/`runtime.Func` (`FullName` of the target method + optional `Receiver`,
      `nil` for a static target) — deliberately **without** modeling `System.Delegate`/
      `MulticastDelegate` as real BCL types: every delegate type (`Action`, `Func`2`, one
      declared by the user) compiles its construction to the **exact same shape** regardless of
      the name — `ldftn` pushes an unbound target, `newobj SomeDelegate::.ctor(object,
      native int)` combines it with the receiver pushed right before (`null` for a static
      target). Detecting that shape structurally instead of registering each delegate type by
      name is what makes `Action<T>`/`Func<T,TResult>`/a custom delegate all work with no extra
      effort.
- [x] IR + interpreter: `ldftn`/`ldvirtftn` (`ldvirtftn` discards the popped receiver — no real
      vtable, same as `constrained.` in Fase 3.7), and `Invoke` dispatch intercepts by
      **receiver Kind** (`KindFunc`), not by method name — there is never a need to register
      "SomeDelegate::Invoke" anywhere.
- [x] **Closures with no extra work**: a lambda that captures outer variables compiles to a
      compiler-generated class with the captured variables as *real fields* and the lambda body
      as an instance method on it — the object/field mechanism that has already existed since
      Fase 2 is enough with no special case. Verified with a closure that also **mutates** a
      captured local (the compiler rewrites the local to share the field between the containing
      method and the lambda) — it worked on the first try against a real fixture.
- [x] **Real bug found and fixed — checker drift**: the dogfood test itself caught it
      immediately — the checker had no way to know that `Func`2::Invoke`/
      `Action`1::.ctor` now resolve, because detection is purely structural in the
      interpreter (never registered in `bcl.Lookup`). `isDelegateType` was added to the checker:
      it recognizes known BCL prefixes (`Action`, `Func\``, `Predicate\`1`, ...) by name, and
      a locally declared delegate (`public delegate ...`) by resolving its real `TypeDef` and
      checking that its `Extends` is `System.MulticastDelegate`/`System.Delegate` — the same
      pattern as `isValueType` in `assembly.go`.

**Fixtures and tests**

- [x] `Delegates.cs` (`Delegates`, `IntTransform`) / `TestDelegates` — method-group conversion
      (static target, cached by the compiler in a static field), a closure capturing a
      parameter, a closure capturing *and mutating* a local, and a locally declared delegate
      type (exercises the `TypeDef` path of `isDelegateType`, not just the known-BCL-prefix
      path)

### What was explicitly left out of this phase

```txt
- Multicast delegates (`+=`/`-=`, System.Delegate.Combine/Remove): runtime.Func models a single
  target, not an invocation list. The dominant measured case (single-use Action<T>/Func<T,TResult>
  — validation predicates, callbacks) doesn't need them.
- BeginInvoke/EndInvoke (IAsyncResult-based async invocation) and DynamicInvoke
  (reflection): only Invoke is supported.
- Delegate covariance/contravariance: not checked and not needed — vmnet doesn't do static
  type checking anyway.
```

### Re-certification against the same 8 targets (7 packages + Jint)

| Package | % clean Fase 3.8 | % clean Fase 3.9 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 87.4% | 90.5% |
| `FluentValidation@11.9.2` | 63.9% | **77.3%** |
| `Semver@2.3.0` | 74.9% | 76.8% |
| `System.Text.Json@8.0.5` | 68.1% | 69.3% |
| `Newtonsoft.Json@13.0.3` | 62.9% | 63.8% |
| `SimpleBase@4.0.0` | 46.1% | 48.4% |
| `Humanizer.Core@2.14.1` | 46.3% | 46.8% |
| **Average (7 packages)** | **64.2%** | **67.6%** |
| `Jint@3.1.3` | 74.4% | 77.3% |
| **Average (7 packages + Jint)** | **65.5%** | **68.8%** |

`FluentValidation` is the biggest jump on the whole road to 85% so far (+13.4 points) —
a validation library is, literally, a tree of predicates (`Func<T,bool>`) and composable
callbacks. It confirms that delegates, along with the type hierarchy, was one of the two
truly dominant blockers.

### How to verify Fase 3.9

```bash
go test ./... -race -count=3
go test ./ -run TestDelegates -v
```

### Fase 3.10 — real `try`/`catch`/`finally`

The architecturally biggest piece of the road to 85%: real exception handling, not just
unhandled `throw`.

**Tasks**

- [x] `internal/il`: new parser for the exception-handling clause table (spec
      §II.25.4.5-6, *small* and *fat* forms, chained sections via `MoreSects`) — until now
      `ReadMethodBody` didn't even read the bytes that follow a method's code when it had
      `try`/`catch`/`finally`. New fuzz test (`FuzzReadExceptionHandlers`, ~4.6M runs
      executed manually, 0 panics).
- [x] IR: `Leave` (spec §III.3.44 — unlike a plain `Branch`, it has to run any
      `finally`/`fault` between the exit point and the target before jumping), `EndFinally`,
      `Rethrow` (C#'s `throw;`, with no operand). `Build` now also returns `[]Handler`
      (IL offsets already resolved to IR indices, same as branch targets) — a signature
      change that touched both call sites (`assembly.go`, the checker).
- [x] `internal/interpreter`: a complete exception-dispatch engine —
  - A `*runtime.ManagedException` exiting `runFrame` (whether from a direct `throw`, a
    `rethrow`, or propagated from any nested call — `frame.IP` already points at the exact
    instruction that was running, with no need to track anything special) is matched
    against the current method's `Handler`s, from innermost to outermost.
  - A `catch` matches by reusing **the same real hierarchy walk from Fase 3.8**
    (`isAssignableTo`) — so `catch (ArgumentException)` correctly catches an
    `ArgumentNullException` thrown inside, not just an exact type match.
  - A `finally`/`fault` on the path always runs, whether the exception ends up caught or
    keeps propagating — `endfinally` resumes exactly the control transfer that entered the
    handler (a `leave` chaining into the next pending `finally`, or catch search resuming
    from where it left off).
  - `rethrow` preserves the original exception (`frame.currentException`, set upon entering
    any catch) instead of requiring the handler to keep its own reference.
  - `System.Exception::get_Message` — was completely missing; without it, `catch (T ex) { ...
    ex.Message ... }` (the single most common pattern of all) had no way to read the message.
- [x] **Low-risk refactor, not a rewrite**: the existing giant loop (`switch` with
      ~40 cases) was left intact — it was extracted as-is into `runFrame`, and `invoke` became
      a thin loop that calls `runFrame`, catches a `*runtime.ManagedException` if it comes out,
      and retries dispatching it against the method's handlers before letting it propagate.
      Zero changes to the internal logic of the existing ~40 cases — all the risk stayed
      concentrated in the new mechanism, not spread across the whole file.

**Fixtures and tests**

- [x] `TryCatch.cs` / `TestTryCatch` — catch by exact type and by base type, `finally` running
      on both the caught and uncaught paths, a nested `finally` running before reaching an
      outer `catch`, the first matching `catch` winning among several, `rethrow` preserving the
      original message, and an exception with no matching `catch` propagating as a Go
      error — **all cases that did not depend on the CLI's pre-existing limitation with
      boolean JSON arguments passed on the first real run**, including the nested exception.
- [x] `internal/checker`: `Unsupported.cs` repurposed once again (third time vmnet's coverage
      has grown through it) — now it uses a filter clause (`catch (T) when (cond)`), the only
      form of exception handling this phase deliberately doesn't lower to IR.

### What was explicitly left out of this phase

```txt
- Filter clauses (`catch (T) when (cond)`): buildHandlers in ir/builder.go explicitly rejects
  them as an unsupported opcode instead of executing them incorrectly. Uncommon in real code
  compared to plain catch/finally.
- `rethrow` only tracks the exception of the most recently entered catch (a single slot, not a
  stack) — a `rethrow` after a nested try/catch *inside* that same catch handler would see
  the inner exception instead of restoring the outer one. Rare edge case, documented.
- User-defined exception types (classes inheriting from Exception with their own fields): the
  exceptions supported are still just the native types vmnet already registers
  (docs/en/ROADMAP.md Fase 2) — the catch-by-hierarchy mechanism works the same for any type
  that does resolve, but constructing a custom exception with `newobj` still needs its `.ctor`
  to be interpretable, which is not especially exercised today.
```

### Re-certification against the same 8 targets (7 packages + Jint)

| Package | % clean Fase 3.9 | % clean Fase 3.10 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 90.5% | 90.5% |
| `System.Text.Json@8.0.5` | 69.3% | 69.7% |
| `Newtonsoft.Json@13.0.3` | 63.8% | 64.3% |
| `Humanizer.Core@2.14.1` | 46.8% | 47.0% |
| `FluentValidation@11.9.2` | 77.3% | 77.3% |
| `Semver@2.3.0` | 76.8% | 76.8% |
| `SimpleBase@4.0.0` | 48.4% | 48.4% |
| **Average (7 packages)** | **67.6%** | **67.7%** |
| `Jint@3.1.3` | 77.3% | 78.1% |
| **Average (7 packages + Jint)** | **68.8%** | **69.0%** |

A small, honest movement: unlike the type hierarchy or delegates (which directly unblocked
other call targets that used to fail), `try`/`catch`/`finally` only "cleans" a method if
**that was the only** obstacle — many methods that use exceptions in the real packages also
touch other things that still aren't supported (DateTime, Span, reflection). The value of
this phase is architectural (real exceptions, not just unhandled throw) rather than a big
jump in the number, and it was the riskiest piece to implement well — worth having done
carefully even though the number doesn't reflect it as much as Fase 3.8/3.9 do.

### How to verify Fase 3.10

```bash
go test ./... -race -count=3
go test ./ -run TestTryCatch -v
go test ./internal/il/... -run '^$' -fuzz '^FuzzReadExceptionHandlers$' -fuzztime=30s
```

### Fase 3.11 — `foreach`/enumerators + cheap wins (re-prioritized with data)

The original plan for this phase was "DateTime, Span/ReadOnlySpan/Memory". Before writing any
code, the same findings-per-target probe as always was run (7 packages + Jint) — and
`System.IDisposable::Dispose`, `IEnumerator`1::get_Current`, `IEnumerable`1::GetEnumerator` and
`EqualityComparer`1` turned out to be much wider (7-8/8 packages) than DateTime/Span (2-5/8,
though with more absolute volume). The cause: **`foreach` over `List<T>`/`Dictionary<K,V>`
didn't work at all** — Fase 2 only gave indexed access (`xs[i]`/`xs.Count`), the
`GetEnumerator`/`MoveNext`/`get_Current`/`Dispose` pattern that the C# compiler generates for
every `foreach` was never added. The phase was re-prioritized to close this first — the same
"measure before guessing" principle that already reordered Fase 3.5 and Fase 3.6. DateTime/
Span/Memory are documented as Fase 3.12 (see below), not dropped.

**Tasks**

- [x] `List<T>.Enumerator`/`Dictionary<K,V>.Enumerator` as real synthetic value types
      (same pattern as `Nullable`1` in Fase 3.7) — confirmed against real IL before writing
      the native: `List<T>.GetEnumerator()` returns a **struct**, not a reference, so the
      call site uses `ldloca`+`call` (not `callvirt`) for `MoveNext`/`get_Current`,
      exactly the by-pointer-receiver mechanism that already existed since Fase 3.7.
- [x] `System.Collections.Generic.KeyValuePair`2` as a value type — what
      `Dictionary<K,V>.Enumerator.Current` produces. The dictionary enumerator snapshots the
      keys at `GetEnumerator()` time (its own array, Fase 3.5) instead of iterating Go's live
      `map[string]Value` — a Go map's iteration order is random per run, which would make
      `MoveNext` non-deterministic even *within* a single enumeration, not just across runs.
- [x] `System.IDisposable::Dispose` — a generic no-op. Covers both the `Dispose()` that
      `foreach` always compiles inside a `finally` (whether or not there's anything to
      release) and explicit `using` usage.
- [x] `System.Collections.Generic.EqualityComparer`1::get_Default`/`Equals`/`GetHashCode` —
      literally reuses `valuesEqual`/`valueHash` from `system_object.go` (Fase 3.7), the same
      default equality/hash the CLR uses absent a custom `IEquatable<T>`.
- [x] `System.Math::Min`/`Max`, `System.String::Join` (including the `IEnumerable<string>`
      overload — the call site passes the `List<T>` directly, not an array, when the argument
      is a `List<T>`) — cheap wins from the original list.
- [x] **Real bug found and fixed — nested type name collision**: before registering the
      `List<T>` enumerator, it was verified against real IL what fully qualified name
      `ir.Build` resolves for `List`1.Enumerator::MoveNext` — and it turned out to be
      literally `"Enumerator"`, with no prefix at all, because `resolveTypeToken`/
      `resolveMemberRefClassName` had never needed to walk `ResolutionScope` for a nested
      `TypeRef` (spec §II.22.38: a nested type has no namespace of its own, it inherits the
      one of its containing type). Registering a native under that unqualified name would have
      **silently hijacked** any other type named `Enumerator` in any loaded assembly — Jint,
      for example, has its own (confirmed in this same phase's probe). `qualifyTypeRefName`
      was added (duplicated in `internal/ir` and in the root package, same pattern as other
      already-duplicated resolvers) that builds `Type1+Type2` just like .NET's real
      `Type.FullName`, found and fixed **before** it caused any damage, not after.

**Fixtures and tests**

- [x] `Foreach.cs` / `TestForeach` — `foreach` over `List<int>`, `foreach` over
      `Dictionary<string,int>` (`kv.Value`), `EqualityComparer<int>.Default.Equals`,
      `Math.Min`/`Max`, `String.Join` over a `List<string>`

### What was explicitly left out of this phase

```txt
- `foreach` over a collection typed as an interface (`IEnumerable<T> e = ...; foreach (x in
  e)`), instead of the concrete type (`List<T> xs = ...; foreach (x in xs)`): the former compiles
  against IEnumerable<T>::GetEnumerator directly, which needs real virtual dispatch (Fase
  3.8 only covers isinst/castclass, not method dispatch) — the actual dominant pattern is the
  latter (a local collection of concrete type), which does work.
- Nested type name collisions for TypeDefs belonging to the plugin itself (a nested class
  DECLARED in the assembly itself, via the NestedClass table): this phase's fix only covers
  nested TypeRef (foreign BCL types, which is what the enumerators needed). Pre-existing risk,
  not made worse, documented.
- LINQ (`System.Linq.Enumerable` — Where/Select/Any/Count/...): now that delegates exist
  (Fase 3.9) and real enumerators do too (this phase), it would be feasible, but it's a large
  surface on its own — a natural candidate for a future phase, not a one-off.
```

### Re-certification against the same 8 targets (7 packages + Jint)

| Package | % clean Fase 3.10 | % clean Fase 3.11 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 90.5% | 90.9% |
| `FluentValidation@11.9.2` | 77.3% | 78.9% |
| `System.Text.Json@8.0.5` | 69.7% | 71.1% |
| `Newtonsoft.Json@13.0.3` | 64.3% | 65.7% |
| `Semver@2.3.0` | 76.8% | 78.0% |
| `SimpleBase@4.0.0` | 48.4% | 49.2% |
| `Humanizer.Core@2.14.1` | 47.0% | 48.0% |
| **Average (7 packages)** | **67.7%** | **68.8%** |
| `Jint@3.1.3` | 78.1% | 80.6% |
| **Average (7 packages + Jint)** | **69.0%** | **70.3%** |

### How to verify Fase 3.11

```bash
go test ./... -race -count=3
go test ./ -run TestForeach -v
```

### Fase 3.12 — `DateTime`, `Span<T>`/`ReadOnlySpan<T>`/`Memory<T>`/`ReadOnlyMemory<T>`

The original plan postponed from Fase 3.11 (see above): two BCL surfaces with large but
concentrated impact on a few packages, rather than wide across all 8 targets — that's why
they came after `foreach`, not before.

**Tasks**

- [x] `System.DateTime` as a single-field synthetic value type (`ticks int64`, the same
      internal representation the CLR uses: 100ns intervals since year 1) —
      `get_Year`/`Month`/`Day`/`Hour`/`Minute`/`Second`/`Millisecond`/`DayOfYear`/`DayOfWeek`
      (all via a single factory `dateTimeField(func(time.Time) int32)`), `get_Now`/`get_UtcNow`/
      `get_Today`, `get_Ticks`, `get_Date`, `AddDays`/`AddHours`/`AddMinutes`/`AddSeconds`/
      `AddMilliseconds` (via `dateTimeAdd`), `AddYears`/`AddMonths` (via `dateTimeAddCalendar`,
      real calendar arithmetic from `time.Time.AddDate`, not just adding a fixed duration),
      `ToString` (fixed invariant format — vmnet doesn't model culture, same as `CultureInfo`
      since Fase 3.6), `CompareTo`, `Equals`.
- [x] `Span<T>`/`ReadOnlySpan<T>`/`Memory<T>`/`ReadOnlyMemory<T>` as a single 3-field shape
      (`backing`, `start`, `length`) reused by all 4 — a defensive view over a
      `runtime.Array` or a string's characters, not real unmanaged pointer semantics
      (vmnet has no raw pointers). `MemoryExtensions.AsSpan`/`AsMemory`, `get_Length`,
      `get_Item`, `Slice`, `ToString`, `ToArray`, `Memory<T>.get_Span`.
- [x] `tests/fixtures/csharp/Fixtures.csproj`: added `System.Memory@4.5.5` as a dev-only NuGet
      dependency — `netstandard2.0` doesn't ship `Span<T>`/`ReadOnlySpan<T>`/`AsSpan` out of the
      box (they only arrive in `netstandard2.1`), and `System.Memory` is exactly the polyfill
      that real packages targeting `netstandard2.0` (including older versions of
      `System.Text.Json`) use to get Span — the same real IL shape, not a test shortcut.
- [x] **Real bug found and fixed — `Span<T>`'s indexer returned the value, not a
      reference**: `Span<T>.this[int]` is declared `ref T` (`ref readonly T` in
      `ReadOnlySpan<T>`) — confirmed against real IL before fixing: both `span[i]` and
      `span[i] = v` compile to the **same** `call get_Item` followed by `ldind.i4`/`stind.i4`;
      there's no separate `set_Item` in the metadata for an indexer that returns `ref`. The
      first version returned the element directly, which made the following `ldind.i4` fail
      with "dereferencing a null managed pointer" (it received a value, not a `KindRef`).
      Fixed by returning `runtime.RefTo(&backing.Arr.Elems[start+idx])` for the array case, or
      a pointer to a freshly boxed `Int32` for the string case (Go strings don't have
      addressable per-rune storage — safe only because that pointer is used transiently,
      dereferenced immediately by the following `ldind`). The `set_Item` that had originally
      been registered was removed — dead code, no real call site ever uses it.
- [x] **Real bug found and fixed — `ReadOnlySpan<char>.ToString()` wasn't dispatching**:
      it returned the generic placeholder `<ReadOnlySpan``1>` instead of the real substring.
      Same pattern as `StringBuilder.ToString()` in Fase 3.6: the call site compiles to
      `constrained.` + `callvirt Object::ToString`, not a direct call to the method declared
      on `Span`1`. Fixed by extending `displayString` (`system_object.go`) to also recognize
      `KindStruct` and dispatch via a new shared helper, `spanToStringValue`.
- [x] **Real bug found and fixed — `time.Duration` overflow in the ticks conversion
      (the most serious of the phase)**: the first version of `timeToTicks`/`ticksToTime` used
      `t.Sub(dotnetEpoch)` / `dotnetEpoch.Add(time.Duration(secs)*time.Second)`. `time.Duration`
      is an *int64 in nanoseconds*, only valid for spans of ~292 years — the ~2000-year gap
      between .NET's epoch (year 1) and any real 21st-century date overflows silently
      (`time.Time.Sub` doesn't error, it clamps to `math.MaxInt64`/`MinInt64`), and every test
      date collapsed to the same wrong result regardless of input. It wasn't found by code
      inspection — the arithmetic reasoning looked correct on paper — but by adding temporary
      debug prints that showed correct input arguments (2024, 3, 15) against nonsensical
      output ticks, isolating the bug to the conversion itself. Fixed by rewriting both
      functions on top of Unix-second arithmetic (`time.Unix`/`t.Unix()`, which doesn't share
      `Duration`'s limit), anchored to the known constant
      `unixEpochTicks = 621355968000000000`.
- [x] **Real bug found and fixed — `System.DateTime::.ctor` didn't resolve for direct
      construction onto a local**: `new DateTime(2024,3,15)` assigned directly to a local
      compiles to `ldloca.s`+arguments+`call .ctor` (confirmed against real IL), **not**
      `newobj` — the same pattern that had already forced a fix in Fase 3.7 for plugin
      structs, but had never been replicated for a native value type. Without the fix, that
      call form fell into the regular `bcl.Lookup` registry (which only had the `newobj` entry
      via `registerValueTypeCtor`) and failed as an unresolved method. Fixed by also
      registering `"System.DateTime::.ctor"` in the regular registry, with a function
      (`dateTimeCtorInPlace`) that mutates `*args[0].Ref` in place instead of returning a new
      value.

**Fixtures and tests**

- [x] `DateTimeSpan.cs` / `TestDateTimeSpan` — `DateTimeSpanTest`: `YearMonthDay` (construction
      + field reading), `AddDaysAcrossMonth` (calendar arithmetic crossing a month boundary),
      `CompareDates` (`CompareTo`), `SpanSum` (`Span<int>` over an array via `AsSpan`, sum by
      index), `ReadOnlySpanSubstring` (`ReadOnlySpan<char>` over a string, `Slice` +
      `ToString`), `SpanWriteThrough` (write by index through the `ref` indexer, confirming the
      value persists in the backing array).

### What was explicitly left out of this phase

```txt
- Real cultural DateTime formatting/parsing (ToString with a format string, non-invariant
  cultures, DateTime.Parse/TryParse): vmnet doesn't model CultureInfo beyond the Fase 3.6
  stub; ToString uses a fixed format. None of the 8 target packages needed it to pass the
  checker.
- TimeSpan as its own type: it shows up in several of the 8 targets (Humanizer especially),
  but DateTime.Add* already covers the arithmetic the measured real cases needed; TimeSpan as
  a first-class value type is left for a future phase if the probe justifies it.
- Span<T>/Memory<T> over unmanaged memory (raw pointers, `stackalloc`, `fixed`):
  permanently out of scope, not just for this phase — vmnet has no unmanaged memory (spec
  §3, "what it is not").
- DateTimeOffset, TimeZoneInfo: didn't show up in the probe over the 8 targets with enough
  volume to justify the extra surface.
```

### Re-certification against the same 8 targets (7 packages + Jint)

| Package | % clean Fase 3.11 | % clean Fase 3.12 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 90.9% | 91.2% |
| `FluentValidation@11.9.2` | 78.9% | 78.9% |
| `System.Text.Json@8.0.5` | 71.1% | 77.1% |
| `Newtonsoft.Json@13.0.3` | 65.7% | 65.8% |
| `Semver@2.3.0` | 78.0% | 78.0% |
| `SimpleBase@4.0.0` | 49.2% | 60.5% |
| `Humanizer.Core@2.14.1` | 48.0% | 82.4% |
| **Average (7 packages)** | **68.8%** | **76.3%** |
| `Jint@3.1.3` | 80.6% | 81.0% |
| **Average (7 packages + Jint)** | **70.3%** | **76.9%** |

The biggest jump in the whole 3.6-3.12 sequence: +7.5 points across the 7 packages (+6.6 with
Jint). `Humanizer.Core` alone (+34.4 points) accounts for most of it — it's literally a
"humanize" library for dates/times ("3 days ago", "in 2 hours"), so `DateTime` was its
dominant blocker, not just one among several. `SimpleBase` (+11.3) and `System.Text.Json`
(+6.0) confirm the probe's original hypothesis: both use `Span<byte>`/`ReadOnlySpan<char>` in
their low-level encoding/parsing paths. `Ardalis.GuardClauses`/`FluentValidation`/
`Newtonsoft.Json`/`Semver`/`Jint` move little or not at all — they already had relatively less
DateTime/Span usage relative to their size than the three that did jump, confirming that the
data-driven prioritization (concentrated impact, not wide) was the correct read of the probe.

At 76.9% (7 packages + Jint), the firm 85% closure criterion is **still not reached** — one
more Fase 3.x is needed before Fase 3.6+ can be closed and Fase 4 started.

### How to verify Fase 3.12

```bash
go test ./... -race -count=3
go test ./ -run TestDateTimeSpan -v
```

### Fase 3.13 — `foreach` over interface (dispatch by real type) + cheap-wins package

With the same findings-per-target probe as always, run again after Fase 3.12: the three widest
findings in the entire project were `System.Collections.IEnumerator::MoveNext` (7/8),
`IEnumerator`1::get_Current` (7/8) and `IEnumerable`1::GetEnumerator` (7/8) — `foreach` over a
collection typed as an interface (`IEnumerable<T> xs = list; foreach (x in xs)`), instead of a
concrete type (`List<T> xs = ...`), exactly what Fase 3.11 had explicitly left out for needing
"real virtual dispatch, not just isinst/castclass".

**Tasks — interface dispatch**

- [x] `Machine.call` gains a fallback (`internal/interpreter/calls.go`): when the name
      declared at the call site (`"IEnumerable`1::GetEnumerator"`, baked in at compile
      time from the `MemberRef` — vmnet has no vtable) resolves neither as native nor
      as an interpreted method, it is retried once against the **receiver's real
      concrete type** (`receiverTypeName`, `internal/interpreter/typecheck.go`): the
      `Struct.Type`/`Obj.Type` of most values is already enough; for a native `List<T>`/
      `Dictionary<K,V>` (with no `runtime.Type` of its own, only `Native`) `bcl.NativeTypeName`
      was added — the same dispatch-by-Go-type pattern as `nativeToString` (Fase 3.6). This
      uniformly covers both BCL collections accessed by interface and the plugin's own classes
      implementing an interface (a hand-written `IEnumerator`, an `IEquatable<T>` of its own),
      without registering anything extra per type.
- [x] **Real bug found and fixed — infinite recursion in base constructor chaining**: the
      fallback above, applied unconditionally, made `MyException(string) :
      base(message)` (a `call System.Exception::.ctor(this, msg)` — not `newobj`, since only
      the *exact* type gets `newobj`'d; a base constructor call runs on the already-allocated
      object of the *derived* type) redirect toward the receiver's concrete type... which is
      the derived type itself under construction, re-invoking its own constructor and
      exhausting the stack (`interpreter: call depth exceeded`). Root cause: the fallback
      should never have applied to a plain (non-virtual) `call` — only `callvirt` needs
      redispatch by real type; a `call` names an exact target on purpose (base constructor,
      sealed/private method). Fixed by adding the `virtual bool` flag (already existing in
      `ir.Call.Virtual`, never before propagated down to `Machine.call`) and gating the
      fallback on `virtual == true`.
- [x] `ExplicitImplResolver` (`internal/interpreter/calls.go`, implemented in
      `assembly.go:resolveExplicitImpl`): a `yield return` iterator compiles its
      `GetEnumerator`/`Current` as an **explicit interface implementation** — a `MethodDef` with
      a mangled name (`"System.Collections.Generic.IEnumerable<System.Int32>.GetEnumerator"`, not
      a plain `"GetEnumerator"`), confirmed with `strings` against the real DLL before assuming
      anything. The plain-name fallback above doesn't find it; `metadata.MethodImpls` was added
      (same pattern as `InterfaceImpls` from Fase 3.8, `MethodImpl` table,
      spec §II.22.27) to walk the concrete type's explicit implementations and find
      the real name behind the declared interface.
- [x] Checker (`internal/checker/analyzer.go`): `interfaceDispatchTargets`, an explicit allowlist
      of the interface targets the runtime fallback resolves — the checker is static and
      cannot know a receiver's real concrete type (it would need data-flow analysis),
      so this is "best effort", the same spirit as `isDelegateType`.

**Tasks — custom exception correctness (found while verifying the fix above)**

- [x] **Real bug found and fixed — `System.Exception::.ctor` never resolved for a
      plugin's own subclass**: the same "only `newobj` was covered" pattern that had already
      bitten `DateTime`/`Nullable`1` in earlier phases, this time for base constructor
      chaining. `"System.Exception::.ctor"` (and every already-known exception) was also
      registered as a plain `call`, mutating the already-allocated object (`Obj.Native = &ManagedException{
      ...}`) — an exception to the "Type xor Native" rule of `runtime.Object` explicitly
      documented, necessary because `ir.Throw` requires `Obj.Native` on *any* thrown
      object, plugin or not.
- [x] **Real bug found and fixed — the type name stayed pinned to the base type, not to the
      real derived one**: the first version set `TypeName: "System.Exception"` (the fixed
      name under which the native is registered), so `catch (MyException e)` never matched —
      fixed by reading the receiver's real `Obj.Type` (the plugin's TypeDef) for the name.
- [x] **Real bug found and fixed — `catch (Exception e)` did not catch a plugin's own
      subclass once the above was fixed**: the catch matching (`exceptionMatchesCatch`)
      never looked at the plugin's real type hierarchy — only a fixed `exceptionBaseType`
      map of known BCL names. `nativeMatches` (now a `Machine` method, since it needs
      `ResolveType`) walks a single chain alternating between both sources: the fixed map when
      the name is a known BCL exception, or the plugin TypeDef's real `BaseTypeFullName`
      when it isn't — so `MyException -> System.Exception` (via the real TypeDef) splices with
      `System.Exception -> ...` (via the map) in the same walk.

**Tasks — cheap-wins package**

- [x] `System.String`: `IsNullOrEmpty`, `IsNullOrWhiteSpace`, `StartsWith`, `IndexOf`/
      `LastIndexOf` (at rune positions, consistent with the already-existing `Substring`/
      `get_Chars`), `Split` (`char[]`/`string[]` separator, empty or absent = whitespace —
      same documented behavior as real `Split(null)`; `StringSplitOptions.
      RemoveEmptyEntries` is honored, a count limit is not), `ToCharArray`, `Replace` (covers
      `(string,string)` and `(char,char)`), `Trim`/`Trim(char[])`, `op_Inequality`.
- [x] `System.Char` (`internal/bcl/system_char.go`, new file): `IsUpper`/`IsLower`/`IsDigit`/
      `IsLetter`/`IsLetterOrDigit`/`IsWhiteSpace`/`ToUpper`/`ToLower`/`ToString` — all over a
      plain `int32` (`char` has no `Kind` of its own in `runtime.Value`, spec §III.1.1).
- [x] `System.Int32::ToString` (`internal/bcl/system_numeric.go`, new file) — no
      format string support (same limitation already documented for `CultureInfo`).
- [x] `List<T>`: `set_Item`, `ToArray`, `AddRange` (accepts another `List<T>` or an array),
      `Contains` (reuses `valuesEqual` from Fase 3.7). `Dictionary<K,V>::TryGetValue` (the `out`
      parameter uses the same managed-pointer mechanism as any primitive `ref`/`out`
      since Fase 3.5; on a miss it writes `Null()`, not a typed `default(TValue)` — a documented
      approximation, vmnet doesn't have the generic type argument at this call site).
- [x] `ICollection`1::Add`/`get_Count` and non-generic `ICollection::get_Count` added to
      the checker's allowlist — the runtime already resolved them for free via the
      interface-dispatch fallback above, reusing the already-existing `List`1::Add`/`get_Count`
      natives; nothing new to register.
- [x] `Nullable`1::.ctor` as a plain `call` in addition to `newobj`: `int? n = 42;` (direct
      assignment to a local, no ternary) compiles a direct `ldloca`+`call .ctor` on the local,
      confirmed against real IL before fixing — the exact same pattern `DateTime` needed in
      Fase 3.12, found this time by direct suspicion (same bug "shape") and confirmed
      empirically, not assumed.

**Fixtures and tests**

- [x] `InterfaceForeach.cs` / `TestInterfaceForeach` — sum over a `List<int>` accessed via
      `IEnumerable<int>`, sum over a `yield return` iterator (explicit interface
      implementation)
- [x] `TryCatch.cs` (`CustomException`/`CustomExceptionTest`) / `TestCustomException` — catch by
      exact subtype and by real base type
- [x] `Structs.cs` (`DirectNullableAssignTest`) — covered inside `TestStructs`
- [x] `CheapWins.cs` / `TestCheapWins` — String/Char/Int32/List/Dictionary from the package above

### What was explicitly left out of this phase

```txt
- Reflection-lite beyond the already-existing System.Type stub: `object.GetType()`,
  `MemberInfo.get_Name`, `Type::op_Equality`/`IsAssignableFrom`/`get_FullName`, and the `ldtoken`
  opcode (typeof(T)) — the second-widest finding after this phase (5/8, ldtoken 6/8), but
  it's new surface (needs a real System.Type object, not the current stub) that deserves its
  own sub-phase, not a rushed addition to this one.
- LINQ (System.Linq.Enumerable: Select/Any/Where/ToList/ToArray/FirstOrDefault/All) — viable
  now that delegates exist (3.9) and real enumerators + interface dispatch (3.11/3.13), but
  it's a large surface on its own — the same candidate already noted as pending in Fase 3.11.
- Regex (System.Text.RegularExpressions) — Go's regex engine is RE2 (no backreferences,
  no lookaround), semantically different from .NET's engine; translating syntax or limiting
  the supported subset is its own design decision, not a one-line native.
- Async/Task (AsyncTaskMethodBuilder) — permanently out of scope, not just for this phase (spec
  §3, "what this is not"; already documented in the risk register).
- HashSet<T>, Stack<T>, ConcurrentDictionary<K,V>, TimeSpan, StringComparer — appeared in the
  probe (4/8, moderate volume) but each is its own new surface, not an extension of
  something that already exists (unlike the List/Dictionary methods added in this phase).
- The nested-type-name collision for the plugin's own TypeDefs (documented as a
  preexisting risk since Fase 3.11) remains unresolved — not worsened by this phase.
```

### Re-certification against the same 8 targets (7 packages + Jint)

| Package | % clean Fase 3.12 | % clean Fase 3.13 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 91.2% | 93.0% |
| `FluentValidation@11.9.2` | 78.9% | 82.0% |
| `System.Text.Json@8.0.5` | 77.1% | 78.2% |
| `Newtonsoft.Json@13.0.3` | 65.8% | 68.1% |
| `Semver@2.3.0` | 78.0% | 82.7% |
| `SimpleBase@4.0.0` | 60.5% | 60.9% |
| `Humanizer.Core@2.14.1` | 82.4% | 87.9% |
| **Average (7 packages)** | **76.3%** | **79.0%** |
| `Jint@3.1.3` | 81.0% | 82.6% |
| **Average (7 packages + Jint)** | **76.9%** | **79.4%** |

+2.7 points across the 7 packages (+2.5 with Jint) — solid, well-distributed movement (all
packages go up, none a single dominant jump like Humanizer in 3.12), consistent with having
tackled both a genuinely wide finding (interface dispatch, 7/8) and a scattered package of
lower-individual-volume cheap wins. At 79.4% (7 packages + Jint), the firm closure
criterion of 85% is **still not reached** — the widest remaining finding is reflection-lite
(`ldtoken`/`GetType`/`Type`, 5-6/8), a natural candidate for the next sub-phase.

### How to verify Fase 3.13

```bash
go test ./... -race -count=3
go test ./ -run 'TestInterfaceForeach|TestCustomException|TestCheapWins' -v
```

### Fase 3.14 — Reflection-lite: `ldtoken`/`typeof(T)`, `GetType()`, `System.Type`

The post-3.13 probe confirmed the previous phase's prediction: `ldtoken` (6/8), `System.Object::
GetType` (5/8) and `System.Reflection.MemberInfo::get_Name` (5/8) were the three widest
findings in the project.

**Tasks**

- [x] `ldtoken` (spec §III.4.16, decoded since Fase 1 but never lowered to IR) — only for the
      `typeof(T)` form (`TypeDef`/`TypeRef`/`TypeSpec` token). Confirmed against real IL
      before implementing: `typeof(T)` always compiles `ldtoken T` + `call System.Type::
      GetTypeFromHandle(RuntimeTypeHandle)` — vmnet doesn't model `RuntimeTypeHandle` as its
      own Kind: `ir.LoadTypeToken` pushes a real `System.Type` directly, and
      `GetTypeFromHandle` is registered as an identity function, so the instruction pair
      behaves exactly like the CLR without needing an intermediate "handle" representation.
      The other form of `ldtoken` (`Field` token, the `RuntimeHelpers.InitializeArray` pattern
      behind an array literal initializer) remains unsupported, same message as before.
- [x] `System.Type` modeled as a minimal native-backed object (`nativeTypeInfo{FullName
      string}`, `internal/bcl/system_type.go`) — with no real reference identity (`typeof(X)`
      called twice produces two distinct Go `*nativeTypeInfo`, unlike the CLR's single
      canonical Type); every supported operation compares by `FullName`
      (string), never by pointer identity — the only thing observable from `Type`'s public
      API anyway.
- [x] `System.Object::GetType` — reuses the same "real runtime shape" inspection that
      `isAssignableTo` (Fase 3.8) already does for `isinst`/`castclass`, without duplicating a
      second type-identity mechanism. A boxed primitive has the same ambiguity already
      documented in `isAssignableTo` (`KindI4` covers `int32`/`bool`/`char`/`short`/`byte`) —
      the dominant case (`int32`) is assumed.
- [x] `System.Type::get_Name`/`get_FullName`/`ToString`/`op_Equality`/`op_Inequality`/`Equals`,
      `System.Reflection.MemberInfo::get_Name` (exact alias of `get_Name` — `System.Type` is a
      real `MemberInfo` in the BCL, so the same call site can resolve against either
      name depending on how the compiler typed the expression).
- [x] Checker: `ir.LoadTypeToken` is added to `instrIsObjectModel` (a `System.Type` is a
      heap-allocated object, just like any `newobj`); `System.Type::`/`System.Reflection.
      MemberInfo::get_Name` promoted to `rules` (the `System.Type::` stub that had only lived in
      `netstandard-lite` since before this phase is removed — redundant now that
      `netstandard-lite` inherits `rules`).

**Fixtures and tests**

- [x] `Reflection.cs` / `TestReflection` — `GetType() == typeof(T)` (true for the exact
      type, false against the base type — confirms the comparison doesn't collapse to "any
      Type equals"), `Type.Name`, `Type.FullName`, `!=`

### What was explicitly left out of this phase

```txt
- Type::IsAssignableFrom — the second-widest finding remaining (84 cases, 4/8) after this
  phase, but it needs to walk the real type hierarchy (BaseTypeFullName/Interfaces) with
  access to Machine.ResolveType, something a bcl.Native (a plain func(args) (Value, error),
  with no Machine) doesn't have today — it would need the same kind of plumbing as
  ExplicitImplResolver (Fase 3.13), not a one-line native.
- Type::MakeGenericType/GetGenericTypeDefinition/GetInterfaces/get_IsGenericType/get_IsEnum,
  Nullable.GetUnderlyingType — reflection over generics and real shape introspection; vmnet
  doesn't model generic type arguments in runtime.Value at all (spec §17.1, "minimal
  generics" — type-erased).
- System.Reflection.MethodBase::Invoke/MethodInfo — real dynamic invocation, a completely
  different surface (and considerably riskier to expose to a plugin) than "just querying
  the name/type", out of scope for "reflection-lite".
- LINQ (System.Linq.Enumerable) — still the widest non-async finding in the project after
  this phase (Select/Any/ToList/Where/ToArray add up to ~174 cases at 4-5/8), already noted as
  pending since Fase 3.11/3.13 — a natural candidate for the next sub-phase.
- Regex, async/Task, HashSet<T>/Interlocked/StringComparer — unchanged from what's already
  documented as out in Fase 3.13.
```

### Re-certification against the same 8 targets (7 packages + Jint)

| Package | % clean Fase 3.13 | % clean Fase 3.14 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 93.0% | 93.3% |
| `FluentValidation@11.9.2` | 82.0% | 84.6% |
| `System.Text.Json@8.0.5` | 78.2% | 80.4% |
| `Newtonsoft.Json@13.0.3` | 68.1% | 70.4% |
| `Semver@2.3.0` | 82.7% | 82.7% |
| `SimpleBase@4.0.0` | 60.9% | 60.9% |
| `Humanizer.Core@2.14.1` | 87.9% | 88.0% |
| **Average (7 packages)** | **79.0%** | **80.1%** |
| `Jint@3.1.3` | 82.6% | 83.8% |
| **Average (7 packages + Jint)** | **79.4%** | **80.5%** |

+1.1 points across the 7 packages (+1.1 with Jint) — `Semver`/`SimpleBase` don't move at all
(they don't use reflection on their public surface), `FluentValidation`/`System.Text.Json`/
`Newtonsoft.Json`/`Jint` do (generic type validation, type-based serialization, a JS
engine with type-based dispatch — all four touch `GetType()`/`typeof` with real volume). At 80.5%
(7 packages + Jint) the firm closure criterion of 85% **is still not reached** — LINQ is now
the widest remaining non-async/non-regex finding, a natural candidate for the next sub-phase.

### How to verify Fase 3.14

```bash
go test ./... -race -count=3
go test ./ -run TestReflection -v
```

### Fase 3.15 — LINQ (`System.Linq.Enumerable`)

The post-3.14 probe confirmed what was already noted: LINQ (`Select`/`Any`/`ToList`/`Where`/`ToArray`,
~174 cases at 4-5/8) was the widest remaining non-async/non-regex finding, and it was
already viable — delegates (3.9), real enumerators (3.11) and interface dispatch (3.13) cover
everything LINQ needs to operate over any real source.

**Tasks**

- [x] **Core architectural discovery**: `Enumerable` methods cannot be plain `bcl.Native`
      (`func(args) (Value, error)`, with no access to `Machine`) — each one needs to invoke
      the argument delegate (`m.invokeFunc`) and/or walk an arbitrary `IEnumerable<T>`
      source via the real `GetEnumerator`/`MoveNext`/`get_Current` protocol
      (`m.call`, reusing the interface-dispatch fallback from Fase 3.13). A parallel
      `linqRegistry` registry was added (`internal/interpreter/linq.go`, new) of
      `linqNative func(m *Machine, args []runtime.Value, ...) (runtime.Value, error)`, consulted
      in `Machine.tryCall` before any resolution that has no `Machine`. The same kind of new
      plumbing `ExplicitImplResolver` needed in Fase 3.13, not a surprise.
- [x] `enumerateAll` — a single helper that drains any source into a `[]runtime.Value`:
      a fast path for `KindArray` and a native `List<T>` (already a Go slice), a general
      path via the real iteration protocol for anything else (`Dictionary<K,V>`,
      a plugin class, a `yield return` iterator, another LINQ result) — the same
      mechanism `foreach` already uses, not a second parallel iteration implementation.
- [x] `Select`/`Where`/`Any`/`All`/`ToList`/`ToArray`/`FirstOrDefault` — **eager**
      (materialize immediately into a `[]runtime.Value`), not the CLR's real lazy
      iterators — a deliberate simplification: a chained call
      (`xs.Where(...).Select(...).ToList()`) behaves identically from the caller's
      point of view, because every LINQ result is wrapped as a real, fully enumerable
      `List<T>` (`bcl.NewListValue`, new exported constructor — same pattern
      as `bcl.NewTypeValue` from Fase 3.14) instead of a lazy promise.
- [x] `bcl.NativeListItems` (new, exported) — read-only access to a native `List<T>`'s
      items from `internal/interpreter`, since `nativeList` is an unexported type
      of `internal/bcl`; needed for `enumerateAll`'s fast path.
- [x] Checker: `linqTargets` (`internal/checker/analyzer.go`) — a separate allowlist from
      `interfaceDispatchTargets`, not merged with it: the reason the checker cannot
      resolve these names is different (not "doesn't know the receiver's concrete type", but
      "doesn't know the `linqRegistry` registry of `internal/interpreter` exists at all" — the
      checker cannot import that package without breaking its purely-static-analysis
      boundary). The `"System.Linq.Enumerable::"` prefix was added to the `rules` profile.
- [x] **Hardening verified during the phase**: `new int[] { 1, 2, 3 }` (array literal
      initializer) compiles `newarr` + `ldtoken <FieldDef>` + `call RuntimeHelpers.
      InitializeArray` — the `ldtoken` form Fase 3.14 explicitly left unsupported
      (field token, not type token). Confirmed while writing the LINQ-over-array fixture: the
      array source must be built by element-by-element assignment, not with a collection
      initializer — a preexisting limitation, not introduced or worsened by this phase, just
      rediscovered while verifying.

**Fixtures and tests**

- [x] `Linq.cs` / `TestLinq` — chained `Where().Select().ToList()` over `List<int>`,
      `Any`/`All` with a predicate, `FirstOrDefault` with a predicate, `Select`/`ToArray` over an
      `int[]` (confirms `enumerateAll`'s fast path works for arrays too, not just
      `List<T>`)

### What was explicitly left out of this phase

```txt
- The indexed overloads of Select/Where (Func<T,int,TResult>/Func<T,int,bool>) — only the
  single-argument form is covered; adding the indexed one is mechanical but not measured
  as needed yet.
- OrderBy/GroupBy/Skip/Take/Sum/Min/Max/Distinct/Concat/Reverse — did not appear with
  significant volume in the 8-target probe; candidates to add on demand if a
  future phase measures them as relevant, not an aspirational list.
- Truly lazy chaining (real LINQ is streaming; this implementation is eager
  at every step) — an infinite or very large source with a `.Take(n)` somewhere in the
  chain would over-materialize; none of the 8 target packages exercises that pattern today.
- Type::IsAssignableFrom is still out (see Fase 3.14) — now that the "Machine-aware native"
  pattern (linqRegistry) exists it would be mechanically simpler to add it, but it wasn't
  done in this phase to keep it focused on LINQ.
```

### Re-certification against the same 8 targets (7 packages + Jint)

| Package | % clean Fase 3.14 | % clean Fase 3.15 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 93.3% | 93.3% |
| `FluentValidation@11.9.2` | 84.6% | 86.3% |
| `System.Text.Json@8.0.5` | 80.4% | 80.4% |
| `Newtonsoft.Json@13.0.3` | 70.4% | 70.7% |
| `Semver@2.3.0` | 82.7% | 83.7% |
| `SimpleBase@4.0.0` | 60.9% | 60.9% |
| `Humanizer.Core@2.14.1` | 88.0% | 88.3% |
| **Average (7 packages)** | **80.1%** | **80.5%** |
| `Jint@3.1.3` | 83.8% | 83.8% |
| **Average (7 packages + Jint)** | **80.5%** | **80.9%** |

+0.4 points — smaller than the raw finding volume (~174 cases) suggested, the same pattern
already documented in Fase 3.10: LINQ only "cleans up" a method if it was the sole obstacle, and
several of the methods that use LINQ in these real packages *also* touch deep reflection or regex,
which remain unsupported. This phase's real value is unlocking the `Where`/`Select`/`ToList`
pattern itself — which now works end to end, chained and over any source — more than the
movement in the aggregate average. At 80.9% the firm closure criterion of 85% still isn't
reached.

### How to verify Fase 3.15

```bash
go test ./... -race -count=3
go test ./ -run TestLinq -v
```

### Fase 3.16 — `Type::IsAssignableFrom`

Small sub-phase: the second-widest reflection finding explicitly left out of Fase
3.14 (84 cases, 4/8) — not done then because it needed access to `Machine` (walking the
real type hierarchy requires `Machine.ResolveType`, not available to a plain `bcl.Native`),
but that exact same kind of plumbing already exists since Fase 3.15 (`machineRegistry`, generalized
from `linqRegistry` — same registry, now with a name that doesn't assume only LINQ will use it).

**Tasks**

- [x] `typeIsAssignableFrom` (`internal/interpreter/reflection.go`, new file) — re-derives the
      logic of `isAssignableTo` (Fase 3.8) starting from a type **name** instead of an already-known
      `runtime.Value`/`Kind` (both operands are `System.Type`, which only carries a
      `FullName` string): exact equality or `target == "System.Object"` short-circuits
      immediately, otherwise it resolves the real `TypeDef` of the candidate and walks with
      `m.typeMatches` — the same walk that `isinst`/`castclass` and exception catch-matching
      (Fase 3.13) already use.
- [x] `bcl.TypeFullNameOf` (new, exported) — extracts the `FullName` from a `System.Type` value
      from outside `internal/bcl`, since `nativeTypeInfo` is an unexported type.
- [x] Checker: direct entry for `"System.Type::IsAssignableFrom"` in `resolvableMethod`
      (no new single-element map was created, unlike `linqTargets`).

**Fixtures and tests**

- [x] `Reflection.cs` (`VehicleAssignableFromCar`/`CarNotAssignableFromVehicle`) — covered
      inside `TestReflection`

### Re-certification against the same 8 targets (7 packages + Jint)

| Package | % clean Fase 3.15 | % clean Fase 3.16 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 93.3% | 93.3% |
| `FluentValidation@11.9.2` | 86.3% | 86.4% |
| `System.Text.Json@8.0.5` | 80.4% | 80.9% |
| `Newtonsoft.Json@13.0.3` | 70.7% | 71.0% |
| `Semver@2.3.0` | 83.7% | 83.7% |
| `SimpleBase@4.0.0` | 60.9% | 60.9% |
| `Humanizer.Core@2.14.1` | 88.3% | 88.3% |
| **Average (7 packages)** | **80.5%** | **80.6%** |
| `Jint@3.1.3` | 83.8% | 83.8% |
| **Average (7 packages + Jint)** | **80.9%** | **81.0%** |

+0.1 points — minimal movement, expected for a method with volume concentrated in a few
methods that probably also touch other unsupported surfaces (same pattern as LINQ in
this same phase). At 81.0% the firm closing criterion of 85% is still not reached.

### How to verify Fase 3.16

```bash
go test ./... -race -count=3
go test ./ -run TestReflection -v
```

### Fase 3.17 — Critical bug: collision of plugin's own nested type names + `System.Lazy<T>`

When adding `Lazy.cs` (a second file with non-capturing lambdas, alongside `Linq.cs` from Fase
3.15) and running the full suite with `-count=3` (not just once), `TestLinq` started failing with
`"<>c has no static field \"<>9__0_0\""` — a real bug, unrelated to `Lazy<T>` itself, that the
addition of a second file with lambdas simply made reachable for the first time.

**Root cause**: the C# compiler emits a non-capturing lambda cache class (literally
called `<>c`) **per containing type** that has any — an assembly with lambdas in two
different classes (`LinqTest` and `LazyTest`) ends up with **two separate TypeDefs, both called
`<>c`** (same `Name`, both with `Namespace=""`, since a nested type always has an empty
namespace — spec §II.22.32). All vmnet code that resolved a `TypeDef` token to a full
name (`ldsfld`/`stsfld`/`newobj`/`call`/`ir.Build`, plus the duplicates in `assembly.go` and
`internal/checker/analyzer.go`) collapsed directly to `Qualify(typeDef.Namespace, typeDef.Name)`
— **without walking the `NestedClass` table** — so both `<>c` collapsed to the same string `"<>c"`,
and `metadata.FindTypeDef` returned whichever one it scanned first, regardless of which one the
actual call site needed. This is the SAME class of bug that Fase 3.11 had already fixed for `TypeRef`
(*foreign* nested types, via `qualifyTypeRefName`/`ResolutionScope`) — and that same phase
had **explicitly documented as a pre-existing risk, not fixed**, for `TypeDef` (the plugin's
*own* nested types, via `NestedClass`). The risk, as predicted, turned out to be
real.

**Measured impact — much bigger than a fixtures issue**: measuring against the 8 targets
after the fix, the average jumped from 80.6% to **82.8%** (7 packages) and from 81.0% to **83.0%**
with Jint — the largest jump of the whole 3.6-3.17 sequence after Fase 3.12. `SimpleBase`
alone jumped from 60.9% to 75.6% (+14.7 points). The reason: **any real package with more than one
class using non-capturing lambdas** (an extremely common pattern, not an edge case) was
already silently resolving `ldsfld`/`call` against the wrong `<>c` at some point,
producing "static field/method not found" errors in methods that had nothing to do with
lambdas per se, they just shared an assembly with another class that also used one.

**Tasks — the fix**

- [x] `metadata.EnclosingClass(typeRID) (uint32, bool, error)` (new, `internal/metadata/
      resolver.go`) — reads the `NestedClass` table (spec §II.22.32), with no prior function
      reading it at all (confirmed before writing anything).
- [x] `qualifyTypeDefName`/`QualifyTypeDefName` (new, duplicated in `internal/ir/builder.go`
      —exported, since `internal/checker` also needs it and can indeed import `internal/ir`—
      and in `assembly.go` —unexported, same pattern already established for `qualifyTypeRefName`—):
      walks `NestedClass` recursively building `Enclosing+Nested`, same as
      `qualifyTypeRefName` already does for `ResolutionScope`. Replaces the direct `Qualify(ns,name)`
      in the 8 real sites that resolve a `TypeDef` token to a name: `resolveCallTarget`,
      `resolveMemberRefClassName`, `resolveTypeToken`, `resolveNewObjTarget`, `resolveFieldTarget`
      (the exact site of the bug — `ldsfld`/`stsfld`) in `internal/ir/builder.go`;
      `resolveMethodDefOrRefName`, `buildMethod`, `resolveTypeTokenName` in `assembly.go`;
      `Analyze` in `internal/checker/analyzer.go`.
- [x] `metadata.FindTypeDef` extended to accept a `"+"`-qualified name (the round trip: the
      qualified name that `qualifyTypeDefName` produces needs to resolve back to the real
      `TypeDef` row later, via `buildType`/`resolveByFullName`) — a simple match by
      `Name`+`Namespace` isn't enough when there are several TypeDefs with the same `Name` nested
      in different types; now it walks `NestedClass` upward from each candidate to confirm
      that the chain of containers matches what was requested, with `Namespace` anchored only
      at the outermost level (the only one that has a real one).
- [x] `runtime.Type.QualifiedName` (new field) — `buildType` sets it to the already-qualified name
      it received as input; `fullTypeName` (`internal/interpreter/typecheck.go`, used by the
      interface dispatch from Fase 3.13 and exception catch-matching) prefers it over
      reconstructing from `Namespace`+`Name`, which would lose the qualification again for any
      of the plugin's own nested types.

**Tasks — `System.Lazy<T>`**

- [x] `nativeLazy` (`internal/bcl/system_lazy.go`, new file): constructor covers the
      overloads with a `Func<T>` factory (with or without a trailing `bool`/`LazyThreadSafetyMode`,
      ignored — all access is already serialized via the instance's own mutex regardless of the
      requested mode); `get_IsValueCreated` (plain native); `get_Value` — needs `Machine` (invoking
      the factory uses `m.invokeFunc`), so it goes into the `machineRegistry` generalized in Fase 3.16
      (`internal/interpreter/lazy.go`, new file). `bcl.LazyGetOrCompute` holds the instance's lock
      for the **entire** computation (not just around the check), so that two
      goroutines racing for the same `Lazy<T>.Value` for the first time serialize into "one
      computes, the other blocks and sees the same cached result" instead of "both compute, one
      silently overwrites the other's result" — a real risk, not a hypothetical one: a
      static `Lazy<T>` field is the dominant real-world use, and `Assembly.Call` is documented as safe
      for concurrent goroutines.

**Fixtures and tests**

- [x] `Lazy.cs` / `TestLazy` — factory invoked exactly once (verified by counting actual
      invocations, not just checking that the returned value is consistent),
      `IsValueCreated` before/after first access
- [x] `Linq.cs` + `Lazy.cs` together, run with `-count>=3`, are the regression coverage for the
      `<>c` bug — both files already have non-capturing lambdas in different classes, the
      exact shape that reproduced it

### Re-certification against the same 8 targets (7 packages + Jint)

| Package | % clean Fase 3.16 | % clean Fase 3.17 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 93.3% | 93.3% |
| `FluentValidation@11.9.2` | 86.4% | 86.4% |
| `System.Text.Json@8.0.5` | 80.9% | 80.9% |
| `Newtonsoft.Json@13.0.3` | 71.0% | 71.0% |
| `Semver@2.3.0` | 83.7% | 83.7% |
| `SimpleBase@4.0.0` | 60.9% | **75.6%** |
| `Humanizer.Core@2.14.1` | 88.3% | 88.8% |
| **Average (7 packages)** | **80.6%** | **82.8%** |
| `Jint@3.1.3` | 83.8% | 84.0% |
| **Average (7 packages + Jint)** | **81.0%** | **83.0%** |

+2.2 points (+2.0 with Jint) from a correctness fix, not a new feature — `SimpleBase`
alone explains almost the whole jump across the 7 packages. At 83.0% the firm closing criterion of
85% is still not reached, but the margin has closed considerably.

### How to verify Fase 3.17

```bash
go test ./... -race -count=5
go test ./ -run 'TestLinq|TestLazy' -count=3 -v
```

### Fase 3.18 — Second batch of cheap wins + `IDictionary<K,V>` via interface

After the big jump of Fase 3.17, this phase tackles the next batch of concentrated, cheap
findings from the probe, plus the same interface-dispatch pattern from Fase 3.13 applied to
`IDictionary<K,V>`.

**Tasks**

- [x] `System.String::Contains`, `System.String::.ctor` (covers `new string(char[])`,
      `new string(char[], start, length)`, `new string(char, count)`) — needed its own path
      in `newObj` (`internal/interpreter/calls.go`), not the usual `bcl.LookupCtor`/
      `registerCtor` registry: a `string` in vmnet is a plain `KindString`, not a `KindObject`,
      so wrapping the result in `runtime.ObjRef` (what every other native ctor does) would be
      incorrect — confirmed before writing anything, not assumed.
- [x] `System.Environment::get_NewLine` (always `"\n"` — vmnet has no real OS against which to
      match the platform-dependent value), `System.Convert::ToInt32` (by argument `Kind` —
      string/int64/float/null; an unparseable string throws `FormatException`, not a
      guessed result), `System.Double::ToString` (invariant `G` format, the same culture
      limitation documented across the whole BCL).
- [x] `List<T>::RemoveAt`/`Insert`, `Dictionary<K,V>::Clear` — cheap extras on collections already
      supported.
- [x] `System.FormatException`/`System.OverflowException` added to the registry of constructible
      exceptions (same pattern as the rest since Fase 2), with their corresponding entries in
      `exceptionBaseType` (`internal/interpreter/typecheck.go`) so that `catch (Exception e)`
      also catches them correctly.
- [x] `System.Threading.Interlocked::CompareExchange` — the `ref` argument arrives as a managed
      pointer (`KindRef`), same mechanism as any `ref`/`out` since Fase 3.5; the
      compare-and-exchange semantics are real, not just a stub that always assigns (vmnet has no
      real multi-core memory model to be atomic against, but the observable result — what real
      code actually depends on for correctness — is indeed correct).
- [x] `System.StringComparer` (`Ordinal`/`OrdinalIgnoreCase`/`InvariantCulture`/
      `InvariantCultureIgnoreCase` — the culture variants collapse to ordinal comparison, the same
      "no culture support" limitation documented across the whole BCL; only `IgnoreCase` is
      actually distinguished) with `Equals`/`Compare`/`GetHashCode`.
- [x] `IDictionary<K,V>::set_Item`/`get_Item`/`TryGetValue`/`ContainsKey` added to the checker's
      allowlist (`interfaceDispatchTargets`) — the runtime already resolved them for free via
      the interface-dispatch fallback from Fase 3.13, reusing the existing `Dictionary`2` natives;
      nothing new to register, same pattern as `ICollection`1` in Fase 3.13.
- [x] `System.Convert::` promoted from `netstandard-lite` to `rules` (same treatment as
      `System.Type::` in Fase 3.14) — with real natives behind it, `netstandard-lite` and `rules`
      now promise exactly the same BCL surface; the `netstandard-lite` profile remains an
      explicit copy of `rules` instead of an additional list, documented in the code so that a
      future `rules`-only addition doesn't need to be reconsidered for both levels.

**Fixtures and tests**

- [x] `CheapWins2.cs` / `TestCheapWins2` — one case per native from the list above

### Re-certification against the same 8 targets (7 packages + Jint)

| Package | % clean Fase 3.17 | % clean Fase 3.18 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 93.3% | 93.3% |
| `FluentValidation@11.9.2` | 86.4% | 87.3% |
| `System.Text.Json@8.0.5` | 80.9% | 81.7% |
| `Newtonsoft.Json@13.0.3` | 71.0% | 71.6% |
| `Semver@2.3.0` | 83.7% | 84.6% |
| `SimpleBase@4.0.0` | 75.6% | 75.6% |
| `Humanizer.Core@2.14.1` | 88.8% | 89.2% |
| **Average (7 packages)** | **82.8%** | **83.3%** |
| `Jint@3.1.3` | 84.0% | 84.4% |
| **Average (7 packages + Jint)** | **83.0%** | **83.5%** |

At 83.5% the firm closing criterion of 85% is still not reached, but the remaining margin is
small. What's left with real volume: async (permanently out of scope), regex (pending design
decision), and deeper reflection (`Type.MakeGenericType`/`GetGenericTypeDefinition`/
`GetInterfaces`, `Enum.GetValues`/`GetNames`/`IsDefined`).

### How to verify Fase 3.18

```bash
go test ./... -race -count=3
go test ./ -run TestCheapWins2 -v
```

### Fase 3.19 — `HashSet<T>`, `Stack<T>`, `TimeSpan`

Three new surfaces from the probe (33/29/11+6 cases respectively) with moderate volume (4/8) —
each a new collection/value type, not an extension of something already existing.

**Tasks**

- [x] `HashSet<T>` (`internal/bcl/system_hashset.go`, new file): `Add`/`Contains`/`get_Count`/
      `GetEnumerator` + `HashSet`1+Enumerator::MoveNext`/`get_Current` (struct value type, same
      pattern as `List`1.Enumerator` from Fase 3.11, confirmed against real IL before assuming it).
      Deduplication/`Contains` via linear scan with `valuesEqual` (`system_object.go`), not a real
      Go `map` — `runtime.Value` isn't intrinsically hashable/comparable in the sense
      of a Go map key (a `KindStruct`/`KindObject` would need to be canonicalized first);
      same pragmatic simplification already accepted for `List<T>.Contains`. `Add` returns whether
      the element was actually added (real `HashSet<T>.Add` semantics), not `void`.
- [x] `Stack<T>` (`internal/bcl/system_stack.go`, new file): `Push`/`Pop`/`Peek`/`get_Count`
      over a Go slice used directly as a LIFO (`append`/truncate).
- [x] `System.TimeSpan` (`internal/bcl/system_timespan.go`, new file): synthetic single-field
      value type (`ticks int64`, the same 100ns representation as `DateTime` since Fase 3.12).
      Covers `(ticks)`, `(hours,minutes,seconds)`, `(days,hours,minutes,seconds[,milliseconds])`;
      `FromDays`/`FromHours`/`FromMinutes`/`FromSeconds`/`FromMilliseconds`; component
      properties (`Days`/`Hours`/`Minutes`/`Seconds`/`Milliseconds`, each the remainder after
      dividing by the unit above, not the total) and total properties (`TotalDays`/.../`TotalMilliseconds`,
      `double`). Also registered as a plain `call` in addition to `newobj` (`timeSpanCtorInPlace`) —
      the same "direct assignment to a local" bug that `DateTime`/`Nullable`1` already needed
      to fix, anticipated this time by the already-known pattern and confirmed against real IL before
      writing the fixture, not discovered by surprise.

**Fixtures and tests**

- [x] `CollectionsExtra.cs` / `TestCollectionsExtra` — `HashSet<int>` with a duplicate (confirms
      real deduplication), `Stack<int>` (`Push`×3/`Pop`/`Count`), `TimeSpan.FromSeconds`,
      `new TimeSpan(1,2,3)` directly to a local

### Re-certification against the same 8 targets (7 packages + Jint)

| Package | % clean Fase 3.18 | % clean Fase 3.19 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 93.3% | 93.3% |
| `FluentValidation@11.9.2` | 87.3% | 87.5% |
| `System.Text.Json@8.0.5` | 81.7% | 81.8% |
| `Newtonsoft.Json@13.0.3` | 71.6% | 71.7% |
| `Semver@2.3.0` | 84.6% | 84.6% |
| `SimpleBase@4.0.0` | 75.6% | 75.6% |
| `Humanizer.Core@2.14.1` | 89.2% | 89.9% |
| **Average (7 packages)** | **83.3%** | **83.5%** |
| `Jint@3.1.3` | 84.4% | 84.8% |
| **Average (7 packages + Jint)** | **83.5%** | **83.7%** |

Small movement (+0.2/+0.2) — expected for moderate-volume, not-especially-wide surfaces
(4/8). At 83.7% the firm closing criterion of 85% is still not reached; ~1.3-1.5 points missing.

### How to verify Fase 3.19

```bash
go test ./... -race -count=3
go test ./ -run TestCollectionsExtra -v
```

### Fase 3.20 — `System.Text.RegularExpressions`

Design decision already noted as pending since Fase 3.13: vmnet compiles patterns with Go's
RE2 engine (`regexp`), not the real .NET engine — the two dialects agree on the vast majority
of real-world usage (character classes, quantifiers, anchors, groups, alternation), but RE2 has
no backreferences or lookaround (`(?=...)`/`(?<=...)`/`(?!...)`); a pattern that uses them fails
to compile with a clear error (`ArgumentException`), not a plausible-but-incorrect result —
the same "never a silently wrong answer" discipline the rest of the project already
follows.

**Tasks**

- [x] `Regex` (`internal/bcl/system_regex.go`, new file): constructor (compiles the pattern via
      `regexp.Compile`), `IsMatch`/`Match` in both static (`Regex.IsMatch(input,
      pattern)`) and instance (`regex.IsMatch(input)`) forms, distinguished by the shape of the
      arguments same as any other multi-overload native in this package. The match runs entirely
      eagerly at `Match()` time (there's no real lazy `Match`) — the same simplification already
      made for LINQ (Fase 3.15).
- [x] **Real bug found and fixed — real hierarchy surprise confirmed against IL**: the
      first version registered `Match::get_Success`/`Match::get_Value` directly and they were
      never called at all. The real hierarchy is `Capture -> Group -> Match`: `Value` is declared by
      `Capture`, `Success` is declared by `Group`, and `Match` **inherits both without overriding them**
      — so `m.Success`/`m.Value` on a `Match` instance compile to `callvirt
      Group::get_Success`/`callvirt Capture::get_Value`, never directly against `Match::`.
      Found by running the real fixture and seeing the error "receiver is not a Group/Capture,
      got *nativeMatchVal" — not assumed beforehand. Fixed with a single shared accessor
      (`asSuccessValue`) that reads `(Success, Value)` from both a `*nativeGroupVal` (a capture
      group) and a `*nativeMatchVal` (Group 0, the full match), registered once
      under the real names `Group::get_Success`/`Capture::get_Value`.
- [x] `Match.Groups[i]` via `GroupCollection::get_Item`/`get_Count` — `Groups[0]` is always the
      full match (real Group 0 semantics), `Groups[1:]` the pattern's capture groups in
      order. `FindStringSubmatchIndex` is used (index pairs), not `FindStringSubmatch`
      (plain strings): this distinguishes an optional group that didn't participate in the match
      (`Success = false`) from one that captured an empty string — both would be `""` with the
      strings API. `Match.Groups` itself doesn't allocate a separate `GroupCollection` object: since
      its only two members read exactly the same group slice that `Match` itself already has, the
      same Match/Native object is reused instead of allocating a wrapper with no observable
      difference.

**Fixtures and tests**

- [x] `Regex.cs` / `TestRegex` — static `IsMatch` (match and non-match), `Match` with capture
      groups (`(\w+)@(\w+)\.com`, match and non-match), instance `Regex` + `Match`

### Re-certification against the same 8 targets (7 packages + Jint)

| Package | % clean Fase 3.19 | % clean Fase 3.20 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 93.3% | 93.7% |
| `FluentValidation@11.9.2` | 87.5% | 88.1% |
| `System.Text.Json@8.0.5` | 81.8% | 81.8% |
| `Newtonsoft.Json@13.0.3` | 71.7% | 71.8% |
| `Semver@2.3.0` | 84.6% | 84.9% |
| `SimpleBase@4.0.0` | 75.6% | 75.6% |
| `Humanizer.Core@2.14.1` | 89.9% | 90.2% |
| **Average (7 packages)** | **83.5%** | **83.7%** |
| `Jint@3.1.3` | 84.8% | 84.8% |
| **Average (7 packages + Jint)** | **83.7%** | **83.9%** |

Small movement (+0.2/+0.2) — regex is almost never the sole obstacle for a method in these
real packages (the same pattern seen with LINQ, Fase 3.15). At 83.9% the firm closing criterion
of 85% is still not reached; ~1.1-1.3 points missing.

### How to verify Fase 3.20

```bash
go test ./... -race -count=3
go test ./ -run TestRegex -v
```

### Fase 3.21 — Third cheap-wins package: **crosses 85%** 🎯

Third round of concentrated, cheap probe findings. This phase crosses the original firm
closing criterion from Fase 3.6+ (85%) — see the note at the beginning of this section about the
target revised to ~97%.

**Tasks**

- [x] `System.NotImplementedException` added to the registry of constructible exceptions (same
      pattern as the others since Fase 2), with its entry in `exceptionBaseType`.
- [x] `System.Double::IsInfinity`/`IsPositiveInfinity`/`IsNegativeInfinity`, `System.Math::Floor`.
- [x] `System.String::EndsWith`.
- [x] `List<T>::Clear`, `Dictionary<K,V>::Remove`.
- [x] `System.Int32::Parse`/`TryParse`/`CompareTo` — `TryParse`'s `out int` uses the same
      managed-pointer mechanism as any primitive `ref`/`out` since Fase 3.5.
- [x] `System.DateTime::get_Kind` — needed adding a second field (`kind`, a
      `System.DateTimeKind` as `int32`) to `DateTime`'s synthetic value type (Fase 3.12), ahead of
      the single `ticks` field. Only `get_Now`/`get_UtcNow`/`get_Today` set it to something other
      than `Unspecified` (the only place where vmnet has a real Utc-vs-local distinction worth
      reporting) — `Add*`/`get_Date` do not propagate the original's `Kind`, a documented
      simplification (not measured as necessary: no probe finding asked for `Kind` fidelity
      across arithmetic, only the property itself).
- [x] `KeyValuePair<K,V>` also gains the plain `call` registration (`.ctor`) in addition to
      `registerValueTypeCtor` — same "direct assignment to a local" pattern that
      `DateTime`/`Nullable`1`/`TimeSpan` already needed, this time anticipated by the known
      pattern and confirmed against real IL before writing the fixture.
- [x] `IList<T>::get_Item`/`set_Item`, `IReadOnlyList<T>::get_Item`,
      `IReadOnlyCollection<T>::get_Count`, `IEqualityComparer<T>::Equals`/`GetHashCode` added
      to Fase 3.13's interface-dispatch allowlist — the runtime already resolved them for free
      by reusing the existing `List`1`/`EqualityComparer`1` natives, same pattern as
      `ICollection`1`/`IDictionary`2` in earlier phases.
- [x] `System.Double::`/`System.Int32::` promoted to broad prefixes in the `rules` profile
      (previously only pinpoint entries) — with real natives covering the common surface,
      listing each member separately no longer added anything over the full prefix.

**Fixtures and tests**

- [x] `CheapWins3.cs` / `TestCheapWins3` — one case per native from the list above, including
      `IList<T>`/`IReadOnlyCollection<T>` over the same concrete `List<T>` instance

### Re-certification against the same 8 targets (7 packages + Jint)

| Package | % clean Fase 3.20 | % clean Fase 3.21 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 93.7% | 93.7% |
| `FluentValidation@11.9.2` | 88.1% | 88.3% |
| `System.Text.Json@8.0.5` | 81.8% | 82.1% |
| `Newtonsoft.Json@13.0.3` | 71.8% | 72.4% |
| `Semver@2.3.0` | 84.9% | **90.8%** |
| `SimpleBase@4.0.0` | 75.6% | 75.6% |
| `Humanizer.Core@2.14.1` | 90.2% | 92.6% |
| **Average (7 packages)** | **83.7%** | **85.1%** |
| `Jint@3.1.3` | 84.8% | 86.8% |
| **Average (7 packages + Jint)** | **83.9%** | **85.3%** |

**The original firm closing criterion of 85% is crossed** (85.1% on 7 packages, 85.3% with Jint).
`Semver` jumps +5.9 points on its own (Int32.Parse/TryParse and version comparison are its
core surface); `Humanizer.Core` and `Jint` also climb with real volume. With the target already
revised to ~97% (see the note at the beginning of this section), the sub-phase sequence continues.

### How to verify Fase 3.21

```bash
go test ./... -race -count=3
go test ./ -run TestCheapWins3 -v
```

### Fase 3.22 — `async`/`await` (synchronous model) — the biggest jump in the sequence

A ceiling analysis run before this phase (fixing EVERYTHING non-async, leaving async
permanently out as the risk register had said until now) gave a ceiling of **89.6%**
(7 packages) / **89.3%** with Jint — below the new ~97% target. With async
accounting for most of what remained uncovered in `Newtonsoft.Json`/
`System.Text.Json`/`SimpleBase` specifically, getting close to 97% without touching it was
mathematically unviable. The "permanently out of scope" decision recorded since the
beginning of the project was revisited.

**Design decision — every `Task` is completed by construction**: vmnet has no real scheduler
or thread pool, so instead of trying to model genuine cooperative concurrency, every
`Task`/`Task<T>` that any native produces (`Task.FromResult`, `AsyncTaskMethodBuilder.
SetResult`/`SetException`, `Task.Run`) is **completed from the moment it's created**. This
has one key architectural consequence: the `MoveNext()` method the compiler generates for
any `async` (a real state machine, with its own try/catch/finally region for
routing exceptions) checks `awaiter.IsCompleted` at every `await` — and since that property
always returns `true` in this model, the branch that suspends (`AwaitUnsafeOnCompleted` +
`return`) is never taken in practice. A single call to `MoveNext()` runs the entire `async`
method start to finish, including any number of chained or nested `await`s. **The interpreter
did not need to be touched at all** for the body of `MoveNext()` itself — it's plain ordinary
IL (fields, branches, a real try/catch/finally), already fully supported since Fase 1/3.10.
All the work in this phase was BCL surface.

**Tasks**

- [x] `AsyncTaskMethodBuilder`/`AsyncTaskMethodBuilder`1` (`internal/bcl/system_task.go`, new
      file) as single-field synthetic value types (a reference to the `Task` they're
      building, so it survives the containing struct being copied): `Create` (static),
      `SetStateMachine` (no-op — only relevant for boxing a struct-based state machine
      that needs to survive a real suspension, which never happens in this model),
      `SetResult`/`SetException`, `get_Task`.
- [x] `AsyncTaskMethodBuilder::Start`/`AwaitUnsafeOnCompleted` (`internal/interpreter/async.go`,
      new file, generalizing Fase 3.15/3.16's `machineRegistry` once again) — need
      `Machine` to invoke the compiler-generated state machine's `MoveNext()`
      (unbounded type, resolved by the receiver's real type via `receiverTypeName`, the same
      mechanism Fase 3.13's interface dispatch already uses). `AwaitUnsafeOnCompleted` in
      practice never runs (the branch invoking it is never taken, see above) — it was left as
      a defensive fallback that still continues the state machine instead of failing, in case
      some future case does end up needing it.
- [x] `Task`/`Task<T>` as the same instance also acting as its own *awaiter* —
      `TaskAwaiter`/`ConfiguredTaskAwaitable(+Awaiter)` have no members of their own beyond
      `GetAwaiter`/`get_IsCompleted`/`GetResult`, so assigning a separate wrapper in each case
      would not have changed anything observable. `Task::ConfigureAwait` is the identity
      function (vmnet has no synchronization context to jump between).
- [x] `Task.FromResult<T>`, `Task.CompletedTask`, `Task.Delay` (ignores the real wait, already
      completed immediately — documented, not a hidden decision), `Task.Run` (invokes the
      delegate right now synchronously — needs `Machine`, also goes in
      `internal/interpreter/async.go`; does not unwrap a nested `Task` if the delegate itself is
      async, a documented simplification not measured as necessary by the probe).
- [x] Checker: `asyncMachineTargets` (allowlist, same pattern as `linqTargets`/
      `interfaceDispatchTargets`) for the Machine-aware targets; profile prefixes for
      `System.Threading.Tasks.Task(`1)::`, `AsyncTaskMethodBuilder(`1)::`,
      `TaskAwaiter(`1)::`, `ConfiguredTaskAwaitable(`1)(+ConfiguredTaskAwaiter)::`.

**Fixtures and tests**

- [x] `Async.cs` / `TestAsync` — two sequential `await`s (`ComputeAsync`), an exception thrown
      **after** an `await` correctly propagating through
      `GetAwaiter().GetResult()` up to a synchronous `catch` (confirms that `SetException` +
      `GetResult`'s re-throw work, not just the happy path), an `async Task` void method,
      and a chain of `await` over **another `async` method** (not just `Task.FromResult`,
      confirming that nested chains truly chain) — all four cases worked end to end on the
      first real attempt against real IL, with no bug found during
      verification (unlike almost every previous phase).

### What was explicitly left out of this phase

```txt
- Real cooperative concurrency: Task.Delay doesn't really wait, Task.Run doesn't use a real
  thread pool (runs the delegate right away, synchronously), there's no real Task.WhenAll/WhenAny
  with parallelism — everything delegated to "already complete," correct for the real dominant
  pattern (a plugin using async for API convenience, not for genuinely concurrent I/O) but not a
  real concurrency model. Documented in the post-v1.0 roadmap as "real cooperative async/Task"
  in case it's ever needed.
- Task.Run(Func<Task<T>>) (a delegate that itself returns a Task) doesn't unwrap the nested
  result — it produces a Task<Task<T>>, not the real flattened Task<T>. Not measured as necessary
  by the probe.
- IAsyncEnumerable<T>/await foreach — different surface (asynchronous enumeration), not
  measured with volume in the probe.
```

### Re-certification against the same 8 targets (7 packages + Jint)

| Package | % clean Fase 3.21 | % clean Fase 3.22 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 93.7% | 96.8% |
| `FluentValidation@11.9.2` | 88.3% | 91.5% |
| `System.Text.Json@8.0.5` | 82.1% | 82.4% |
| `Newtonsoft.Json@13.0.3` | 72.4% | **78.6%** |
| `Semver@2.3.0` | 90.8% | 90.8% |
| `SimpleBase@4.0.0` | 75.6% | **84.1%** |
| `Humanizer.Core@2.14.1` | 92.6% | 92.6% |
| **Average (7 packages)** | **85.1%** | **88.1%** |
| `Jint@3.1.3` | 86.8% | 86.8% |
| **Average (7 packages + Jint)** | **85.3%** | **88.0%** |

**+3.0 points on the 7 packages (+2.7 with Jint) — the biggest jump of the whole
3.6-3.22 sequence.** `SimpleBase` (+8.5) and `Newtonsoft.Json` (+6.2) confirm exactly the
ceiling analysis's hypothesis: they were the packages with the most real async surface. At
88.0% the new ~97% target still isn't reached, but the jump confirms that attacking async was
the right decision.

### How to verify Fase 3.22

```bash
go test ./... -race -count=5
go test ./ -run TestAsync -v
```

### Fase 3.23 — Fourth cheap-wins package + two real correctness bugs

Fourth round of probe findings (`DateTimeOffset`, `DateTime` operators,
`Double.TryParse`, `Convert.ToInt64`, `Char.ToLowerInvariant`, `Int64.ToString`, `ValueTuple`,
more LINQ, `CultureInfo`, `IList`). Verifying these natives against real IL exposed two
genuine bugs in already-existing mechanisms (Fase 3.13's interface dispatch and `fieldSlot`
since Fase 3.7), not just surface gaps.

**Tasks — cheap wins**

- [x] `System.DateTimeOffset` (`internal/bcl/system_datetimeoffset.go`, new file): two-field
      synthetic value type (UTC `ticks` + `offsetTicks`) — same double `newobj`+plain `call`
      registration that `DateTime`/`Nullable`1`/`TimeSpan`/`KeyValuePair` already needed.
      `get_UtcDateTime`/`get_DateTime`/`get_Offset`/`get_Ticks`.
- [x] `DateTime::op_Subtraction` (returns `TimeSpan`, reusing the same 100ns `ticks` field),
      `op_Equality`/`op_Inequality`, `ToUniversalTime`/`ToLocalTime` (identity function — vmnet
      has no real local time zone to convert against, same reasoning as
      `Environment.NewLine` since Fase 3.18).
- [x] `Double.TryParse` (same managed-pointer `out` mechanism as `Int32.TryParse`),
      `Double.Equals`, `Convert.ToInt64`, `Char.ToUpperInvariant`/`ToLowerInvariant` (same
      transformation as the culture-sensitive variants — vmnet has no culture support
      anywhere), `Int64.ToString`.
- [x] `System.ValueTuple`2` (`internal/bcl/system_valuetuple.go`, new file) — unlike
      any other value type in this package, its members (`Item1`/`Item2`) are real public
      fields, not properties: registering it as a synthetic value type with those two fields
      is enough, `ldfld`/`stfld` already resolve generically against any registered
      `Type.FieldIndex` — zero native getter/setter code needed.
- [x] LINQ: `SelectMany` (flattens by invoking the selector and enumerating its result with the
      same generic `enumerateAll`), `Take`, `Contains`, `Empty`.
- [x] `System.Collections.IList::Add`/`get_Item`/`set_Item` added to Fase 3.13's interface
      dispatch allowlist (`IList.Count` already worked for free: in the real BCL `Count` is
      declared by `ICollection`, not `IList`, and `System.Collections.ICollection::get_Count`
      already existed since Fase 3.13 — same "inherited member, not redeclared" pattern as
      `Match.Success`/`Value` in Fase 3.20).
- [x] `CultureInfo::get_CurrentCulture`/`get_Name` (stubs).

**Tasks — real bugs found and fixed**

- [x] **Bug — interface dispatch (Fase 3.13) could leave the stack short when the concrete
      method's real signature differs from the declared interface's**: `System.Collections.
      IList::Add` returns `int` (the inserted index), but redirects to `List`1::Add`, which is
      `void`. The stack became unbalanced (nothing pushed where the call site expected a
      value), causing a real panic (`index out of range [-1]`) at the next instruction that
      tried to consume it — found by running the real fixture, not by inspection. Fixed
      in `internal/interpreter/eval.go`: the decision to push a result now uses
      `in.HasReturn` (the signature declared at the call site, known at IR-construction time)
      as authority, not the `hasReturn` reported by the finally-resolved callee — if they
      differ, `Null()` is pushed as a placeholder to keep the stack
      balanced (the real result is only lost if someone actually captures `IList.Add`'s
      return value, a rare pattern in practice).
- [x] **Bug — `fieldSlot` never handled a struct receiver passed by direct value (without a
      managed pointer)**: until now, every struct field access seen in this project
      used `ldloca`+`ldfld` (managed pointer, `fieldSlot`'s `KindRef` case). The `ValueTuple`
      fixture revealed that the real compiler sometimes emits `ldloc`+`ldfld` direct (no
      address) for the *second* field access in the same expression (`t.Item1 + t.Item2`:
      `Item1` via `ldloca`+`ldflda`, but `Item2` via plain `ldloc`+`ldfld`) — legal per spec
      §III.4.10, but a case never before exercised in practice. `fieldSlot` only had cases
      for `KindObject`/`KindRef`; a bare `KindStruct` fell through to `default:` and threw
      `NullReferenceException`. The direct `KindStruct` case was added.
- [x] **Architectural discovery — a native BCL value type had never needed a real *static*
      field until `TimeSpan.Zero`**: it's a real static public field (`ldsfld
      System.TimeSpan::Zero`), not a property. `runtime.NewValueType` doesn't support static
      fields at all (documented in its own comment, never needed until now); `timeSpanType`
      was rebuilt using `runtime.NewType` directly plus `SetStaticField` for the real value
      (a self-referencing zero `TimeSpan`, so it can't go in the construction literal). A
      fallback was also added in `resolveTypeByFullName` (`assembly.go`)
      to query `bcl.LookupValueType` when the type has no `TypeDef` in the plugin's
      assembly — needed for `ir.LoadStaticField` to be able to resolve `System.TimeSpan`'s
      `*runtime.Type` at all.

**Fixtures and tests**

- [x] `CheapWins4.cs` / `TestCheapWins4` — one case per native from the list above, plus
      `IListAddTest` (regression for the different-signature bug) and `ValueTupleTest`
      (regression for the `fieldSlot` bug)

### Re-certification against the same 8 targets (7 packages + Jint)

| Package | % clean Fase 3.22 | % clean Fase 3.23 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 96.8% | 96.8% |
| `FluentValidation@11.9.2` | 91.5% | 92.7% |
| `System.Text.Json@8.0.5` | 82.4% | 82.7% |
| `Newtonsoft.Json@13.0.3` | 78.6% | 79.2% |
| `Semver@2.3.0` | 90.8% | 91.0% |
| `SimpleBase@4.0.0` | 84.1% | 85.3% |
| `Humanizer.Core@2.14.1` | 92.6% | 93.3% |
| **Average (7 packages)** | **88.1%** | **88.7%** |
| `Jint@3.1.3` | 86.8% | 87.2% |
| **Average (7 packages + Jint)** | **88.0%** | **88.5%** |

+0.6 points (+0.5 with Jint) — small movement as expected for a round of scattered wins, but the
real value of this phase is the two correctness bugs fixed (one of them, the unbalanced-stack
one, is a risk that had existed silently since Fase 3.13 in ANY interface dispatch
with an incompatible signature, not just `IList.Add`). At 88.5% the ~97% target still
hasn't been reached.

### How to verify Fase 3.23

```bash
go test ./... -race -count=5
go test ./ -run TestCheapWins4 -v
```

### Fase 3.24 — Fifth cheap-wins package: ConcurrentDictionary, Regex.Replace, Delegate multicast

Fifth round of the findings-per-target probe. Unlike previous rounds, the post-3.23 probe no
longer showed a long tail of scattered moderate-volume surfaces: the widest findings (5-4/8
packages) are now concentrated almost entirely in deep reflection (`Type.MakeGenericType`/
`GetGenericTypeDefinition`/`GetInterfaces`/`get_IsGenericType`/`get_IsEnum`/`GetMethod(s)`/
`GetProperties`/`GetConstructors`/`get_BaseType`, `System.Reflection.
MethodInfo`/`PropertyInfo`/`ParameterInfo`/`MemberInfo`/`Assembly`, `MethodBase.Invoke`,
`Activator.CreateInstance`, `System.Enum.GetValues`/`GetNames`/`IsDefined`/`ToObject` —
these require introspection backed by real metadata, not just one more native). This phase takes
the last harvest of cheap surface that does NOT depend on reflection before tackling that larger
block in a dedicated phase.

**Tasks**

- [x] `System.Collections.Concurrent.ConcurrentDictionary`2` (`internal/bcl/
      system_concurrentdictionary.go`, new file): same mutex + `map[string]
      Value` backing as `Dictionary`2` (string-keys-only limitation already documented, Fase 2),
      plus a real `sync.Mutex` — the whole point of this type over `Dictionary`2` is safe
      concurrent access, and although a single vmnet `Machine` never runs on more than one
      goroutine at a time, a host application legitimately can share a `ConcurrentDictionary`
      across several. `GetOrAdd` has two real overloads (`(key, TValue value)` and `(key,
      Func<TKey,TValue> factory)`) resolved under the same call-target name — since vmnet's
      dispatch doesn't distinguish overloads by signature, the `Kind` of the third argument
      distinguishes them at runtime (same pattern as `resolveRegexAndInput`). Invoking the factory
      needs `Machine` access, so `GetOrAdd` is resolved through the Machine-aware registry
      (`internal/interpreter/concurrentdict.go`), not a plain native — same reason as
      `Lazy`1.Value` (Fase 3.17). The rest (`TryAdd`/`TryGetValue`/`TryRemove`/`ContainsKey`/
      indexer/`get_Count`) are plain natives.
- [x] `Regex.Replace` (`internal/bcl/system_regex.go`): same static-vs-instance disambiguation
      mechanism by `Kind` as `IsMatch`/`Match` (`resolveRegexReplace`), reusing Go's RE2 engine
      (`ReplaceAllString`) — .NET's `$1`/`${name}` replacement syntax matches Go's in the common
      cases, same dialect limitation already documented for `IsMatch`/`Match` (Fase 3.20).
- [x] `Delegate.Combine`/`Delegate.Remove` (`internal/bcl/system_delegate.go`, new file): the
      project's first real multicast delegate support. `runtime.Func` gained a `Chain []*Func`
      field (list of additional targets); `Machine.invokeFunc`
      (`internal/interpreter/calls.go`) now invokes its own target and then each `Chain` entry in
      order, discarding all results but the last — same as the real `MulticastDelegate.Invoke`.
      `Combine`/`Remove` are plain natives: they only manipulate lists of `*Func`, they don't need
      to invoke anything.
- [x] `System.Array::GetEnumerator` plus its enumerator (`internal/bcl/system_array.go`): unlike
      `List`1.Enumerator` (a struct inlined directly at the `foreach` call site, Fase 3.11), an
      array walked through the non-generic `IEnumerable` protocol receives a real reference-type
      enumerator (`System.Array+SZArrayEnumerator` in the real BCL) — confirmed against real IL
      (Fase 3.24): a `foreach` over a typed `Array`/`IEnumerable` source compiles directly to
      `callvirt System.Array::GetEnumerator`, and the *result* is walked through the
      `IEnumerator` interface (`callvirt MoveNext`/`get_Current`). Added `nativeArrayEnumerator`
      with a real entry in `NativeTypeName` (`system_object.go`) — Fase 3.13's interface dispatch
      is what redirects those interface-typed calls to the concrete natives registered under
      `System.Array+ArrayEnumerator::`.
- [x] **Real bug found while verifying `(Action)Delegate.Combine(a1, a2)` against real IL**:
      `isAssignableTo` (`internal/interpreter/typecheck.go`) had no case at all for `KindFunc` — a
      delegate had never needed to go through a real `castclass`/`isinst` until now (`Action a =
      SomeMethod;` doesn't emit `castclass`; the compiler already builds the correct type).
      `Delegate.Combine` returns `Delegate` (the base type), so assigning it to `Action` does
      compile to a real `castclass Action`. Since `runtime.Func` doesn't carry its own declared
      delegate type (it's detected structurally, not by type — see the `Func` comment, Fase 3.9),
      there's nothing to check against: added `case runtime.KindFunc: return true`, accepting any
      delegate-to-delegate cast/isinst over a real delegate value.

**Fixtures and tests**

- [x] `CheapWins5.cs` / `TestCheapWins5` — one case per native from the list above, plus
      `ConcurrentDictGetOrAddFactoryTest` (confirms the factory runs exactly once despite three
      calls with the same key) and `DelegateCombineThenRemoveTest` (multicast regression: combine
      two, remove one, invoke the one remaining).

**What was explicitly left out**

- `System.Enum.GetValues`/`GetNames`/`IsDefined`/`ToObject`: require reading a real `enum`'s
  static field literals from its `TypeDef` (`Constant` table, no parser in `internal/metadata`
  yet) — part of the same deep-reflection block as the rest of the list below, not an isolated
  surface.
- Full deep reflection (`Type.GetMethods`/`GetProperties`/`GetConstructors`/
  `get_BaseType`/`GetInterfaces`/`MakeGenericType`/`GetGenericTypeDefinition`/
  `GetGenericArguments`/`get_IsGenericType`/`get_IsEnum`/`get_IsValueType`/`get_IsInterface`,
  `System.Reflection.MethodInfo`/`PropertyInfo`/`ConstructorInfo`/`ParameterInfo`/`MemberInfo`/
  `Assembly`, `MethodBase.Invoke`, `Activator.CreateInstance`): confirmed by the probe as the
  dominant remaining category (4-5/8-width findings, the largest concentration seen since Fase
  3.13) — a natural candidate for its own dedicated phase, with its own design (needs
  introspection backed by real metadata plus dynamic invocation, not just one more native).
- `System.Linq.Expressions` (expression trees — `Expression.Parameter`/`Lambda`): appeared with
  moderate volume (3/8) in the same probe, but it's an entirely new surface (parsing and
  evaluating an expression tree) unrelated to the reflection above beyond sharing a client (Jint
  uses it for JIT/interpretation of compiled JS expressions).
- `Span`1::op_Implicit` (53 cases, 3/8): implicit `T[] -> Span<T>`/`Span<T> ->
  ReadOnlySpan<T>` conversion with no native of its own; a cheap candidate for a future phase if
  the probe keeps showing it with volume after reflection is resolved.
- `ldsflda`/`localloc` (opcodes, 3/8 each): address of a real static field and dynamic stack
  buffer — neither has a fixture of its own verified against real IL yet.

### Re-certification against the same 8 targets (7 packages + Jint)

| Package | % clean Fase 3.23 | % clean Fase 3.24 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 96.8% | 96.8% |
| `FluentValidation@11.9.2` | 92.7% | 93.3% |
| `System.Text.Json@8.0.5` | 82.7% | 82.8% |
| `Newtonsoft.Json@13.0.3` | 79.2% | 79.6% |
| `Semver@2.3.0` | 91.0% | 91.0% |
| `SimpleBase@4.0.0` | 85.3% | 85.3% |
| `Humanizer.Core@2.14.1` | 93.3% | 93.4% |
| **Average (7 packages)** | **88.7%** | **88.9%** |
| `Jint@3.1.3` | 87.2% | 87.5% |
| **Average (7 packages + Jint)** | **88.5%** | **88.7%** |

+0.2 points — the smallest move in the "cheap wins" sequence (expected: `Concurrent
Dictionary`/`Delegate.Combine`/`Regex.Replace` are real but narrow surfaces compared to LINQ or
async). `FluentValidation` (+0.6) and `Newtonsoft.Json` (+0.4) account for nearly all of the
movement — consistent with both using delegate-based caches/concurrent dictionaries internally.
This confirms the previous section's reading: at 88.7%, with no more visible-volume cheap surface
in the probe, the path toward ~97% runs through deep reflection, not another round of scattered
wins.

### How to verify Fase 3.24

```bash
go test ./... -race -count=5
go test ./ -run TestCheapWins5 -v
```

### Fase 3.25 — Deep reflection, first slice: System.Type introspection

First slice of the "deep reflection" block identified as the dominant remaining category at the
close of Fase 3.24 (4-5/8-width findings). Scope deliberately limited to `System.Type` — pure
introspection (generics, `IsValueType`/`IsEnum`/`IsInterface`/`BaseType`/
`GetInterfaces`, `Type.GetType(string)`) — leaving the still-larger block out
(`System.Reflection.MethodInfo`/`PropertyInfo`/`ConstructorInfo`/`ParameterInfo`, dynamic
invocation via `MethodBase.Invoke`/`Activator.CreateInstance`), which needs a real object
hierarchy backed by metadata, not just name manipulation.

**Tasks — `System.Type` generics**

- [x] **Root-cause change — `internal/metadata/signatures.go`**: `SigType` gained an `Args
      []SigType` field, populated in the `elementGenericInst` branch of `parseType` (previously it
      discarded every parsed argument: `_, sz3, err := parseType(...)`). Purely additive — every
      existing consumer of `SigType` keeps ignoring `Args` exactly as before; no previous behavior
      changes.
- [x] **`internal/ir/builder.go`**: new `resolveClosedTypeSpecName`/`sigTypeFullName`, used *only*
      by the `ldtoken` case (`typeof(T)`) — unlike `resolveTypeTokenOrGeneric` (used by
      `initobj`/`ldobj`/`stobj` and `MemberRef` resolution, which still don't need more than the
      open name), `typeof(List<int>)` now retains its arguments as
      `"System.Collections.Generic.List\`1[[System.Int32]]"` — confirmed against real IL that
      `typeof(List<>)` (open generic) still resolves directly to a `TypeDef`/`TypeRef` with no
      `TypeSpec` at all, so the open name never accidentally gains brackets.
- [x] `Type.get_IsGenericType` (`internal/bcl/system_type.go`): `strings.Contains(name, "\`")`
      over the portion before `[[` — true for both the open and closed type, same as the real
      contract.
- [x] `Type.GetGenericTypeDefinition()`: trims the `[[...]]` suffix if present.
- [x] `Type.GetGenericArguments()`: `splitGenericArgs` parser with bracket-depth tracking (an
      argument can itself be a nested closed generic). Empty for an open generic
      (`typeof(List<>)`) — real .NET returns the parameters (`T`) there, which vmnet has no way to
      name (documented limitation).
- [x] `Type.MakeGenericType(params Type[])`: unlike `typeof(T)`, it ALWAYS receives real names at
      runtime (the compiler always lowers `params Type[]` to a real array at the call site) —
      builds the closed name directly, without relying on the original open generic having
      retained anything.
- [x] `System.Nullable::GetUnderlyingType(Type)` (note: the non-generic helper class
      `System.Nullable`, not `System.Nullable\`1` — a distinct real method) — same bracket parser,
      `null` for any type that isn't a closed `Nullable\`1[[...]]`.

**Tasks — type classification (Machine-aware, `internal/interpreter/reflection.go`)**

- [x] `runtime.Type` gained `IsEnum`/`IsInterface` (previously only `IsValueType` existed, which
      collapsed struct and enum together — Fase 3.7 never needed the distinction). `assembly.go`'s
      `buildType` populates them: `classifyTypeDef` (previously `isValueType`) now also reports
      `isEnum` (`Extends == "System.Enum"` specifically, not just `"System.ValueType"`), and
      `isInterface` reads the `TypeAttributes.Interface` bit (`0x20`) directly from
      `TypeDefRow.Flags` — the only one of the three that couldn't be derived from `Extends` (an
      interface, just like `System.Object` itself, has no `Extends` at all).
- [x] `Type.IsValueType`/`IsEnum`/`IsInterface`/`BaseType`/`GetInterfaces()`: two-level
      classification — a fixed map of known primitives/BCL interfaces first (same pattern as
      `exceptionBaseType`/`interfaceDispatchTargets`), then the real `TypeDef` of a plugin type via
      `Machine.ResolveType`. `GetInterfaces()` returns only what's directly implemented
      (`runtime.Type.Interfaces`, without transitive expansion — same scope as
      `isinst`/`castclass` since Fase 3.8).
- [x] `Type.GetType(string)`: resolves a plugin type via `Machine.ResolveType` or a native BCL
      value type via `bcl.LookupValueType`; any other name (a real cross-assembly lookup, which
      needs an assembly-qualified name and a loader vmnet doesn't have) returns
      `null`, matching the real contract of `Type.GetType` for a name it can't resolve.
- [x] `Type.Assembly` (`internal/bcl/system_type.go`): `System.Reflection.Assembly` stub — vmnet
      doesn't model multiple real assemblies, so every `Assembly` value is interchangeable;
      only `.ToString()`/`.FullName` return a plausible constant (same precedent as the
      `CultureInfo` stub, Fase 3.23).

**Real bug found and fixed**

- [x] **Infinite recursion in `buildType` when building the first plugin-declared `enum` in the
      whole project**: every member of an enum (`Red` in `enum TrafficLight`) is a `static
      literal` field whose type, in real IL, is the enum itself (`static literal valuetype
      TrafficLight Red = int32(0)`) — not `int32` as one might assume. `buildType` computed a
      default for *every* field (static or not) before separating them, so that self-referencing
      field triggered `fieldOrLocalDefault` → `valueTypeDefault` →
      `resolveTypeByFullName("TrafficLight")` → `buildType("TrafficLight")` again — the type
      wasn't in the cache yet (`asm.types`) because its own construction hadn't finished, so each
      lap repeated the same chain until the stack was exhausted
      (a real `stack overflow`, found by running the fixture, not by inspection). Fixed by
      skipping `fieldOrLocalDefault` for any `FieldAttributes.Literal` (`0x40`) field: its real
      value lives in the `Constant` table, which vmnet still doesn't read (same reason
      `Enum.GetValues`/`IsDefined` remain out of scope — see below), so there was no useful
      default to compute anyway.

**Fixtures and tests**

- [x] `Reflection2.cs` / `TestReflection2` (22 cases) — reuses the `Animal`/`Dog`/`IShape`
      hierarchy from `TypeChecks.cs` and the `Point` struct from `Structs.cs` so that
      `BaseType`/`GetInterfaces` exercise a real plugin `TypeDef`, not just BCL names; declares
      `TrafficLight`, the project's first plugin `enum` (direct regression of the bug above).

**What was explicitly left out**

- `System.Enum.GetValues`/`GetNames`/`IsDefined`/`ToObject`: need to read the `Constant` table
  (each member's real literal value) — no parser yet in `internal/metadata`. It's the most
  frequently recurring item in the probe (5/8 width) but it's a separate module, not a cheap
  extension of this phase.
- The rest of deep reflection: `System.Reflection.MethodInfo`/`PropertyInfo`/
  `ConstructorInfo`/`ParameterInfo`/`MemberInfo`/`Assembly` as real objects,
  `MethodBase.Invoke`/`Activator.CreateInstance` (dynamic invocation), `Type.GetMethod(s)`/
  `GetProperties`/`GetConstructors`/`GetFields`/`GetElementType`/`get_IsArray`/`get_IsAbstract` —
  confirmed by the post-3.25 probe as the largest remaining volume block; needs an object
  hierarchy backed by real metadata (method/property/field RID) plus genuine dynamic invocation
  (`Machine.call` from an arbitrary `MethodInfo`), a considerably bigger design than anything in
  this phase — candidate for Fase 3.26.
- `System.Linq.Expressions` (`Expression.Parameter`/`Lambda`, 3/8): expression trees, still
  unrelated to the rest of reflection beyond sharing a client (Jint).
- `Span\`1::op_Implicit`/`CopyTo` (3/8), `ldsflda`/`localloc` (opcodes, 3/8), `Convert.ChangeType`,
  `Array.IndexOf`, `List\`1::Remove`, `RuntimeHelpers.GetHashCode`: scattered moderate-volume
  surface unrelated to reflection — candidates for a future cheap-wins package.

### Re-certification against the same 8 targets (7 packages + Jint)

| Package | % clean Fase 3.24 | % clean Fase 3.25 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 96.8% | 96.8% |
| `FluentValidation@11.9.2` | 93.3% | 93.7% |
| `System.Text.Json@8.0.5` | 82.8% | 83.7% |
| `Newtonsoft.Json@13.0.3` | 79.6% | 80.4% |
| `Semver@2.3.0` | 91.0% | 91.0% |
| `SimpleBase@4.0.0` | 85.3% | 85.3% |
| `Humanizer.Core@2.14.1` | 93.4% | 93.4% |
| **Average (7 packages)** | **88.9%** | **89.2%** |
| `Jint@3.1.3` | 87.5% | 87.6% |
| **Average (7 packages + Jint)** | **88.7%** | **89.0%** |

+0.3 points — moderate movement for a deliberately narrow slice (only `Type` introspection,
without touching `MethodInfo`/`PropertyInfo`/dynamic invocation yet). `System.Text.Json`
(+0.9) and `Newtonsoft.Json` (+0.8) account for most of it, consistent with both using
`Type.IsGenericType`/`GetGenericArguments`/`IsValueType`/`IsEnum` in their reflection-based
serialization/deserialization paths. At 89.0% the ~97% target still isn't reached — the probe
confirms that the rest of the reflection block (`MethodInfo`/`PropertyInfo`/
dynamic invocation, `Enum.*`) is now, clearly, the largest remaining volume surface.

### How to verify Fase 3.25

```bash
go test ./... -race -count=5
go test ./ -run TestReflection2 -v
```

### Fase 3.26 — System.Enum.GetValues/GetNames/IsDefined/ToObject

The widest finding after Fase 3.25 (`System.Enum::IsDefined`, 5/8 packages). Unlike `Type`
introspection (Fase 3.25, pure name manipulation), this needs data vmnet had never read before:
the real value of each enum member, which lives in the metadata `Constant` table (spec §II.22.9)
— no parser until this phase.

**Tasks**

- [x] **`internal/metadata/constant.go`** (new file): `constantForField` (linear search over the
      `Constant` table, which has no direct index from a field RID — same as real .NET's
      System.Reflection.Metadata also computing it lazily; the table is small and this is only
      called per-enum, not per-field-access), `decodeConstantInt64`
      (decodes the blob according to its type tag: boolean/char/i1/u1/i2/u2/i4/u4/i8/u8 — the
      only set of shapes an enum member's underlying value can take), and
      `EnumMembers(typeRID)` (real names + values, in declaration order, skipping the
      non-literal `value__` field that Fase 3.25 already identified). `ConstantRow`/`md.Constant(rid)`
      already existed in `internal/metadata/resolver.go` (apparently from an earlier phase, never
      wired to anything) — this phase is what finally uses them.
- [x] **New resolver in the `Machine` chain**: `EnumResolver` (`internal/interpreter/calls.go`)
      + `Machine.ResolveEnum` + `WithEnumResolver` (`eval.go`), same pattern as
      `ExplicitImplResolver` (Fase 3.13) — wired in `call.go` via `asm.resolveEnumMembers`
      (`assembly.go`: `FindTypeDef` + `md.EnumMembers`). Only resolves an enum declared by the
      plugin itself (a real `TypeDef`); a BCL-only enum like `System.DayOfWeek` has no metadata at
      all in the plugin's assembly, so it fails there — vmnet doesn't have (and won't soon have) a
      database of real BCL enum members.
- [x] `Enum.GetValues(Type)`/`GetNames(Type)` (Machine-aware, `internal/interpreter/
      reflection.go`): `Int32`/`String` arrays in declaration order. `GetValues` needed no change
      in the interpreter — the resulting array already flows through `System.Array::GetEnumerator`
      (Fase 3.24) for the `foreach` that almost always consumes it.
- [x] `Enum.IsDefined(Type, object)`: accepts both the underlying integer value and the member's
      name (two real forms of the same overload) — the `Kind` of the second argument chooses the
      comparison, same pattern as every other multi-overload native in this project.
- [x] `Enum.ToObject(Type, object)`: a no-op over the underlying value — boxing an enum doesn't
      change its representation in vmnet's `Value` model (same reasoning as the `objectToString`
      comment), and — just like the real implementation — it doesn't validate that the value is
      actually a defined member.

**Fixtures and tests**

- [x] `Reflection3.cs` / `TestReflection3` (6 cases) — reuses the `enum TrafficLight` from
      `Reflection2.cs` (Fase 3.25).

**What was explicitly left out**

- A BCL-only enum (`System.DayOfWeek`, `System.ConsoleColor`, ...) still doesn't work: none has a
  `TypeDef` in the plugin's assembly. Covering this would need a full hardcoded database of known
  BCL enum members — high maintenance, low value against the real reflection block that still
  remains (see below).
- The large reflection block remains untouched: `System.Reflection.MethodInfo`/`PropertyInfo`/
  `ConstructorInfo`/`ParameterInfo`/`MemberInfo` as real objects, `MethodBase.Invoke`/
  `Activator.CreateInstance` (genuine dynamic invocation), `Type.GetMethod(s)`/`GetProperties`/
  `GetConstructors`/`GetFields`/`GetElementType`/`get_IsArray`/`get_IsAbstract` — confirmed by the
  post-3.26 probe as, clearly, the largest remaining volume block (4/8 width:
  `MethodBase.Invoke`, `MethodInfo::op_Inequality`, `PropertyInfo::get_PropertyType`,
  `CustomAttributeExtensions::GetCustomAttribute`).
- `System.Linq.Expressions` (`Expression.Parameter`/`Lambda`, 3/8), `Span\`1::op_Implicit`/
  `CopyTo`/`Fill` (3/8), `ldsflda`/`localloc` (opcodes, 3/8), `Convert.ChangeType`,
  `Array.IndexOf`, `List\`1::Remove`, `System.Numerics.BigInteger`, `RuntimeHelpers.GetHashCode`,
  `Math.Sign`: scattered surface unrelated to reflection — candidates for a future cheap-wins
  package.

### Re-certification against the same 8 targets (7 packages + Jint)

| Package | % clean Fase 3.25 | % clean Fase 3.26 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 96.8% | 96.8% |
| `FluentValidation@11.9.2` | 93.7% | 93.9% |
| `System.Text.Json@8.0.5` | 83.7% | 83.8% |
| `Newtonsoft.Json@13.0.3` | 80.4% | 80.4% |
| `Semver@2.3.0` | 91.0% | 91.0% |
| `SimpleBase@4.0.0` | 85.3% | 85.3% |
| `Humanizer.Core@2.14.1` | 93.4% | 93.4% |
| **Average (7 packages)** | **89.2%** | **89.2%** |
| `Jint@3.1.3` | 87.6% | 87.6% |
| **Average (7 packages + Jint)** | **89.0%** | **89.0%** |

No movement at the table's precision level (89.2%/89.0% in both), but real under the
hood: the total count of individual *findings* dropped in every touched package (`System.Text.Json`
1306→1301, `Newtonsoft.Json` 1581→1572, `Humanizer.Core` 215→209, `Jint` 1733→1730,
`FluentValidation` 188→185, `Ardalis.GuardClauses` 16→14) — the four `Enum` calls stopped
appearing as a *finding* at all. Certification doesn't move because it's a *per-method* metric:
the methods that call `Enum.GetValues`/`IsDefined` in these packages almost always ALSO call
something from the large reflection block (`MethodInfo`, `Expression`, ...) in the same method, so
they still count as a "method with findings" regardless. This confirms even more strongly the
reading from Fase 3.25: the only real path toward ~97% now runs through the
`MethodInfo`/`PropertyInfo`/dynamic invocation block — any smaller, isolated surface will keep
failing to move the aggregate number while that block remains untouched.

### How to verify Fase 3.26

```bash
go test ./... -race -count=5
go test ./ -run TestReflection3 -v
```

---
### Fase 3.27 — Multi-assembly resolution + real `Jint.Engine.Evaluate()` demo

Triggered by a direct question: can vmnet actually run the real Jint example, not a stub?
The answer started at "no": `Call()` only invoked static methods from a single assembly, and
Jint needs to resolve symbols across its own real NuGet dependency chain (Jint →
Esprima → System.Memory → System.Buffers/System.Numerics.Vectors/
System.Runtime.CompilerServices.Unsafe). This phase built that architecture from scratch and then
chased, one by one, every real bug that surfaced running `new Engine().Evaluate("1 + 2")`
against the real DLLs — no fixtures of its own, no shortcuts.

**New architecture: multi-assembly resolution**

- [x] `Assembly.deps []*Assembly` + `WithDependencies(...*Assembly) *Assembly`. `vm.LoadPackage`
      now automatically loads the full graph of a package's transitive dependencies
      (`loadLockedPackage`, recursive over `Dependencies []string` from the lockfile), wiring
      each one via `WithDependencies`.
- [x] Every resolver (`resolveMethod`, `resolveByFullName`, `resolveExplicitImpl`,
      `resolveEnumMembers`, `resolveFieldBytes`) falls back to `asm.deps` when it doesn't find the
      symbol locally, propagating the real error from the deepest dep — not a generic "not found"
      that hides the actual problem.
- [x] **Resolution scoped to the real assembly**: `runtime.Resolvers` (defined in
      `internal/runtime/method.go` to avoid an import cycle) groups the 5 resolvers of a
      `*runtime.Method`; `Machine.invoke` swaps the Machine's active resolvers for
      `method.Resolvers` during that call. Fixes name collisions across assemblies —
      `<PrivateImplementationDetails>` exists separately in `Jint.dll` and in `Esprima.dll`, and
      before this the global-resolver design could silently resolve against the wrong assembly.
- [x] `runtime.ErrMethodNotFound`: distinguishes "no such method exists" (safe to ignore in
      `runCctor`) from "the method exists but failed to build" (a real error that must
      propagate, because a failing `.cctor` may have already mutated real static state before
      failing).

**Real overload resolution (before: "first match by name wins")**

- [x] `pickMethodOverload` (`assembly.go`): a hard arity filter + `scoreParamMatch` (score table
      keyed by `Kind`) + refinement of exact type-name matching (+50 for an exact match, -3 for a
      confirmed mismatch, +20 if the argument is a subclass of the declared type — see below).
      Found by running real Jint: `Engine` has 5 constructors and 9 overloads of `SetValue`;
      Jint's class engine has multiple overloads of the same name and arity that are
      distinguished only by parameter type.
- [x] **Subtype vs. generic type bug**: an argument whose concrete type is a subclass of a
      parameter's declared type (e.g. a `JsNumber` against a `JsValue` parameter) received the
      same -3 penalty as a real mismatch, causing the correct overload to lose against an
      unrelated `object` overload — causing a real infinite recursion in `Engine.SetValue`. Fix:
      `valueIsAssignableToTypeName` walks the argument type's `BaseTypeFullName` chain; a
      confirmed subtype adds +20 instead of subtracting 3.
- [x] **Hard-shape bug (final phase, the most subtle one)**: `GlobalObject` in Jint declares its
      own non-virtual `GetOwnProperty(Key property)` (an internal performance shortcut) with the
      same name and arity as the virtual `GetOwnProperty(JsValue property)` it inherits but does
      not override. The *chain walk* of virtual dispatch (see below) found that single candidate
      by name on `GlobalObject` and accepted it outright — a `JsValue` (reference) value can never
      directly be a `Key` (struct) without a conversion visible in the IL, so this was silently
      corrupting every property lookup. Fix: `hasHardShapeMismatch` disqualifies the
      `KindObject` argument vs. `SigValueType` parameter combination even when it's the only
      candidate by name (`candidateMatchesArgs`) — the chain walk then keeps climbing until it
      finds the actual virtual method.
- [x] **`KindRef` scoring bug**: a `byref` (`ref`/`in`) argument scored the same as a
      `KindObject` against a `SigClass`/`SigObject` parameter (score 5) but only 1 (the
      default) against the correct `SigByRef` — inverted. Fixed with a dedicated `KindRef` case
      (10 if `SigByRef`, 1 otherwise). This caused `StringDictionarySlim<T>`'s generic helper
      `MoveNext<T>(ref Node? node, in NodeList<T> list)` to lose against an unrelated overload of
      a single `Node` — returning the entire `NodeList` as if it were a single node.

**Real virtual dispatch (before: only tried the concrete type as a fallback after "unresolved")**

- [x] `Machine.call` now, for every virtual call, tries the receiver's concrete type *first* —
      not only when the declared name fails to resolve at all. A base-class method can exist and
      resolve perfectly fine (a real, invocable `MethodDef`) while still being the wrong one when
      the receiver's actual type has its own override.
- [x] **Full chain walk**: if the concrete type doesn't have the method (name-only lookup, no
      inheritance inside `resolveMethod`), it climbs `BaseTypeFullName` trying each ancestor
      until reaching the declared type — not just the leaf type. Esprima's `Node.GetChildNodes()`
      deliberately throws `NotImplementedException` ("you forgot to override this") — with only
      the leaf type tried, every concrete node (which doesn't override `GetChildNodes` directly,
      but through an intermediate class) triggered that guard on every call.

**`newarr`/structs: correct default typing (before: blind `Null()` regardless of type)**

- [x] `ir.NewArr` now carries `TypeFullName` (resolved from the element type token, same as
      `initobj`). The interpreter seeds each slot with a real `runtime.Value` for value types
      (struct, enum, primitives: int/long/float/double/bool/char/byte/...) instead of a generic
      `Null()` — a value-type array is never null on the real CLR. `internal/interpreter/structs.go`
      gains `primitiveDefaults` (a map from CIL primitive names to their default, none of which had
      a `TypeDef` or an entry in the BCL value-type registry).
- [x] **The real bug this exposed, not a cosmetic one**: `Jint.Collections.StringDictionarySlim`
      uses `int[] _buckets`; without the correct default, `buckets[i]` read `Null()`, and a
      subtraction against that failed with "binary op on mismatched value kinds".
- [x] **Real aliasing bug, the most expensive one to find in the whole phase**: `newObj`/
      `runtime.NewStruct` copied field defaults with `copy()` — a shallow copy of `runtime.Value`.
      When a default is `KindStruct`, `Value.Struct` is a pointer: **every** instance of a type
      shared the same underlying `*Struct` for that field, until something explicitly overwrote
      it. This caused `Esprima.Utils.AdditionalDataSlot` (embedded in every AST node, used by Jint
      to cache per-node compiled expressions) to be shared between two distinct AST literals ("1"
      and "2") — caching "1"'s compiled result made "2" read that same cache, and `"1 + 2"`
      evaluated to `2`. Fix: each default is cloned (`Value.Clone()`) per field instead of copied
      in bulk — in `internal/runtime/struct.go` (`NewStruct`) and `internal/interpreter/calls.go`
      (`newObj`).

**Small wins found along the way (each one a real gap hit while running Jint/Esprima)**

- [x] `ir.LoadStaticFieldAddr` (`ldsflda`) + `runtime.Type.StaticFieldAddr` + `bcl.
      LookupStaticFieldHost`/`registerStaticFieldHost` (a registry separate from `LookupValueType`,
      since `System.String` needs static storage for `string.Empty` but isn't a value type).
- [x] `RuntimeHelpers.InitializeArray` (array-literal initializer pattern): `ir.
      LoadFieldToken`, `runtime.Resolvers.ResolveFieldBytes`, reading RVA-backed fields via the
      metadata `FieldRVA` table (`internal/metadata/fieldrva.go`, new).
- [x] Recursion-depth guard when constructing value types (`maxValueTypeDepth = 24`) — a real Go
      stack overflow (not the interpreter's), from a self-referential value-type field chain.
- [x] **Enum default bug**: an enum is always represented in CIL as its direct underlying
      primitive on the stack, never as a struct — but `valueTypeDefault`/
      `defaultValueFor` wrapped *every* value-type default (including enums) in a struct.
      `Jint.Runtime.Debugger.StepMode` (a real enum from the plugin) triggered "switch on non-int32
      value kind" until this fix.
- [x] `isinst`/`castclass` against an array (`is T[]`) — `resolveTypeTokenOrGeneric` had no case
      for `SigSZArray`.
- [x] `OpRem` (`%`) for floats — CIL `rem` is IEEE 754 `fmod` (same sign as the dividend,
      different from `Math.IEEERemainder`); Go's `%` doesn't apply to floats, so it uses `math.Mod`.
- [x] `System.Delegate::op_Equality`/`op_Inequality`, `System.Enum::HasFlag`,
      `System.Array::Copy` (5-argument overload) — mundane BCL surface, each found as the next
      exact blocker while re-running the demo.

**The demo: `examples/jint-demo/`**

- [x] `JintWrapper.cs`/`JintWrapper.csproj` (committed, `netstandard2.0`, referencing
      `Jint@3.1.3`) + `main.go`: loads Jint via `vm.NuGet()`/`vm.LoadPackage`, loads
      `JintWrapper.dll` (built separately with `dotnet build -c Release`) via `vm.LoadBytes` +
      `WithDependencies`, and calls `RunJs("1 + 2")` → `"3"` and `AddNumbers(3, 4)` → `7` — both
      through the real, unmodified Jint engine, running inside vmnet.
- [x] `TestJintDemoE2E` (repo root, gated behind `VMNET_NETWORK_TESTS=1`, with a clean skip if
      `JintWrapper.dll` isn't built — same pattern as `tests/fixtures/csharp`).

**What was explicitly left out**

- The full certification across all 8 targets (7 packages + Jint) was not re-run: this phase's
  goal was the working demo, not moving the aggregate percentage. This phase's fixes are all
  real correctness fixes (not just new coverage), so they should move the number at the next
  measurement, but that's left for a future phase dedicated to re-measuring.
- `hasHardShapeMismatch` only covers the `KindObject` vs. `SigValueType` combination — the only
  one that caused real observed damage. The symmetric combination (`KindStruct` vs. a specific
  `SigClass`, not `SigObject`, since boxing to `object` is indeed valid) is left uncovered on
  purpose: less certainty that it's always a real mismatch, and no real case has triggered it yet.

### How to verify Fase 3.27

```bash
go test ./... -race -count=5
dotnet build examples/jint-demo/JintWrapper.csproj -c Release
VMNET_NETWORK_TESTS=1 go test ./ -run TestJintDemoE2E -v
cd examples/jint-demo && go run .
```

---

### Fase 3.28 — Instance API (`Assembly.New`/`Instance.Call`)

Direct question after Fase 3.27: can Jint be run without the C# wrapper? The public API
(`Call`/`CallBytes`/`CallJSON`) only invokes **static** methods — Jint needs `new Engine()` +
`engine.Evaluate(...)`, both instance calls. This phase exposes the internal `newobj`/
`callvirt` mechanism the interpreter already used (Fase 3.27 made it real end to end) directly to
the Go host.

**New API**

- [x] `Machine.New(typeFullName string, args []runtime.Value) (runtime.Value, error)`
      (`internal/interpreter/eval.go`) — an exported wrapper over `Machine.newObj` (the same
      machinery `ir.NewObj` triggers internally), with panic recovery + a fresh instruction
      counter, same pattern as `Invoke`.
- [x] `Machine.CallInstance(fullName string, args []runtime.Value) (runtime.Value, bool, error)`
      — an exported wrapper over `Machine.call` with `virtual=true` always: the real receiver
      (`args[0]`) is tried first by its concrete type, climbing the whole inheritance chain if
      needed (the real virtual dispatch from Fase 3.27) — safe even for a genuinely non-virtual
      method, since the receiver's concrete type matches the declared type in that case.
- [x] `Assembly.New(typeName string, args ...Value) (*Instance, error)` and
      `(*Instance).Call(methodName string, args ...Value) (Value, error)` (`instance.go`, new)
      — the public facade. The `.ctor`/method is resolved by arity + `args`'s Kind, same as any
      static overload (`pickMethodOverload`).
- [x] `*Instance` implements `Value` (can be passed as an argument to another `Call`/`New`, or
      chained: `engine.Call("Evaluate", ...)` → a `JsValue` `*Instance` →
      `.Call("ToString")`). `wrapResult` replaces the direct use of `fromRuntime` in `Call` and
      `Instance.Call`: a `KindObject`/`KindStruct` result is now wrapped in an `*Instance`
      instead of silently getting lost as `nil` — a real improvement to `Call` too, not just the
      new surface.

**The aliasing bug that confirmed the struct design**

- [x] `Instance.Call` on a value type (e.g. `Point` with `Scale(int factor)` mutating
      `X`/`Y`) correctly mutates the instance held by the host — verified with the existing
      `Point` fixture (`Structs.cs`, Fase 3.7). It works because `runtime.Struct` is always
      referenced via pointer: passing `in.value` (a shallow copy of the `runtime.Value`) still
      shares the same underlying `*runtime.Struct`, so a field mutation through the method is
      reflected back into the `Instance` the host keeps holding — the same shared-pointer
      mechanism that caused Fase 3.27's real bug (`newObj`/`NewStruct` copying defaults with
      `copy()`), here working in favor instead of against, because it's exactly ONE instance,
      not N instances sharing a default.

**A real limit, not a bug: C# syntactic sugar that the compiler resolves, not the CLR**

- [x] `examples/jint-nowrapper/` (new, no `dotnet build` needed — just Go + network): runs
      `Engine.Evaluate("1 + 2")` → `"3"` and `SetValue` + `a + b` → `"7"` without any compiled
      wrapper. Along the way it found this API's two real limits:
    - **Optional parameters with a default value** are a compile-time mechanism (the compiler
      inserts the omitted argument at the call site) — the real `Engine.Evaluate` is
      `Evaluate(string code, string source = null)`; `Instance.Call` needs both arguments
      explicit, since there's no "optional parameter" information at runtime to recover
      automatically.
    - **Extension methods** are sugar over a static call to a *different* type —
      `JsValue.AsNumber()` is declared on `Jint.JsValueExtensions`, not on `JsValue`/`JsNumber`;
      `Instance.Call` always targets the receiver's own concrete type, so it can't reach it.
      `ToString()` (a real instance method) serves as a substitute in the demo.
    - User-defined implicit conversions (`operator implicit`) would have the same
      limitation, though no real case triggering it was found in this specific demo.
      Documented in `examples/jint-nowrapper/README.md`.

**Tests**

- [x] `TestInstanceAPI` (`vmnet_test.go`) — a class with a no-argument ctor + property
      getter/setter (`Customer`, Fase 2), a struct with a parameterized ctor + mutating method
      (`Point`, Fase 3.7), and the error case (nonexistent type).
- [x] `TestJintNoWrapperE2E` (root, gated behind `VMNET_NETWORK_TESTS=1`) — the same two cases as
      `TestJintDemoE2E` but without the wrapper.

### How to verify Fase 3.28

```bash
go test ./... -race -count=5
go test ./ -run TestInstanceAPI -v
VMNET_NETWORK_TESTS=1 go test ./ -run TestJintNoWrapperE2E -v
cd examples/jint-nowrapper && go run .
```

### Re-certification against the same 8 targets (7 packages + Jint) after Fase 3.27/3.28

Fase 3.27 explicitly left this re-measurement pending (the goal was the working demo, not
moving the aggregate number). With real overload resolution, full virtual dispatch, and the
struct aliasing fix already in the tree, this is that measurement.

| Package | Fase 3.26 clean % | Fase 3.27/3.28 clean % |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 96.8% | 96.8% |
| `FluentValidation@11.9.2` | 93.9% | 93.9% |
| `System.Text.Json@8.0.5` | 83.8% | 84.5% |
| `Newtonsoft.Json@13.0.3` | 80.4% | 81.1% |
| `Semver@2.3.0` | 91.0% | 91.0% |
| `SimpleBase@4.0.0` | 85.3% | 85.3% |
| `Humanizer.Core@2.14.1` | 93.4% | 93.6% |
| **Average (7 packages)** | **89.2%** | **89.5%** |
| `Jint@3.1.3` | 87.6% | 88.7% |
| **Average (7 packages + Jint)** | **89.0%** | **89.4%** |

This time there's real movement at the table level, not just under the hood: `System.Text.Json`
(83.8%→84.5%), `Newtonsoft.Json` (80.4%→81.1%), `Humanizer.Core` (93.4%→93.6%) and above all
`Jint` (87.6%→88.7%, +1.1pp) — the package that drove every fix in this phase, consistent with
most of the real bugs found (overload resolution with subtypes, full virtual dispatch, typed
`newarr`, the struct aliasing) having been found specifically by running Jint code.
`Ardalis.GuardClauses`/`FluentValidation`/`Semver`/`SimpleBase` didn't move: none of them
exercises the code shapes (deep class hierarchies, structs sharing type metadata, overloads
ambiguous by Kind) that these fixes correct. The aggregate average is still nowhere near 97% for
the same reason documented since Fase 3.25/3.26: the large block of
`MethodInfo`/`PropertyInfo`/dynamic invocation remains, clearly, the largest-volume remaining
surface.

### Fase 3.29 — Checker: dependency-aware resolution (`AnalyzeWithDeps`)

New initiative: push two more real, popular NuGet packages (`NPOI`, spreadsheets/legacy `.xls`;
`ClosedXML`, `.xlsx`) toward 100% clean under `netstandard-lite`, each ending in a real runnable
demo — same bar as Jint (Fase 3.27/3.28), not just "compiles". `DocumentFormat.OpenXml`
(Word/PPTX) is explicitly out of this loop: a first pass measured it at 36.9% clean, dominated by
thousands of `ldtoken` findings inside auto-generated OOXML schema class constructors — a
reflection-heavy structural pattern, consistent with the non-goals already in spec.md §3
(`Reflection.Emit`, heavy `dynamic`), not a simple missing-native gap.

Before implementing anything for NPOI, its baseline measurement (`vmnet check package
NPOI@2.8.0`) surfaced a checker blind spot, not an interpreter gap: NPOI's `.nuspec` lists real
transitive dependencies — `ZString` (`Cysharp.Text.Utf16ValueStringBuilder`, 234 call sites),
`SkiaSharp`, `BouncyCastle.Cryptography`, `ExtendedNumerics.BigDecimal` — exactly the shape
`vm.LoadPackage` already resolves correctly at runtime (Fase 3.27: `Jint` → `Esprima` →
`System.Memory` → ...). `checker.Analyze`, though, only ever decoded the one DLL it was handed —
it had no notion of "this call resolves against a *different* real assembly's real IL", so it
flagged all ~400 of those findings as `unsupported-bcl-method`, a false negative: those calls
genuinely run once `LoadPackage` attaches the resolved dependency chain, the same way a call
within the package's own DLL does.

**Fix**

- [x] `checker.AnalyzeWithDeps(f *pe.File, md *metadata.Metadata, deps []*metadata.Metadata,
      profile Profile) *Report` (`internal/checker/analyzer.go`) — `Analyze` is now a thin
      wrapper calling this with `deps=nil` (fully backward compatible, every existing caller
      untouched). `checkTarget` tries `resolvable(md, target)` first (unchanged behavior); on
      failure it retries `resolvable(dep, target)` against each dependency's own metadata before
      giving up. A dependency-resolved target is treated as compatible outright, skipping the
      profile allowlist check — matching `isLocalMethod`'s existing treatment of a call within
      `md` itself: the callee's own body is what actually runs, not this call site, so it isn't
      "in" or "out" of the caller's profile.
- [x] `vmnet check package` (`cmd/vmnet/main.go`) now resolves the target package's full
      transitive dependency graph via `nuget.NewResolver` (the same resolver `NuGetManager.
      Restore` uses, just without needing a manifest/lockfile on disk first — `check package` has
      always been a look-before-you-add command), fetches and parses each dependency's selected
      asset, and passes them all to `AnalyzeWithDeps`. Prints `Dependencies resolved: N`.

**Impact on measurement, not yet on capability**

This phase makes NPOI's number *honest*, not higher through new capability — the underlying
interpreter/BCL is unchanged. `NPOI@2.8.0`: 91.3% → 92.0% clean (`MethodsFlagged` 1235 → 1131),
and the third-party-dependency findings (`Cysharp.Text`, `SkiaSharp.SKColor`,
`Org.BouncyCastle`, `ExtendedNumerics.BigDecimal` — ~400 combined findings) are gone from the
report entirely. The remaining ~1131 flagged methods are now, with much higher confidence, genuine
gaps in vmnet's own BCL coverage or opcode support — exactly the signal the rest of this loop
needs to prioritize correctly.

Known scoping limit, left as-is: `AnalyzeWithDeps` checks whether a call *resolves* into a
dependency's real method, not whether that dependency's own method body would itself run clean
(no recursive whole-graph analysis). A dependency's own unsupported call would surface at actual
runtime, not as a `vmnet check package NPOI@2.8.0` finding today. Full transitive checking is a
larger change (report attribution across N assemblies, cycle handling in the check walk itself)
that isn't needed for this loop's goal — flag it here rather than leave it a silent gap.

### How to verify Fase 3.29

```bash
go build ./...
go vet ./...
go test ./... -race -count=5
/tmp/vmnet-cli check package NPOI@2.8.0 --profile=netstandard-lite   # Dependencies resolved: 21
```

### Fase 3.30 — `System.IO.MemoryStream`/`Stream` + a real NuGet resolver bug

Highest-impact blocker after Fase 3.29's honest measurement: `System.IO.Stream`/`MemoryStream`
(709 combined findings in NPOI, 92 in ClosedXML) — the single largest genuinely-BCL gap, and one
that recurs in both target packages, matching this loop's "pick the highest-leverage blocker"
methodology. Probing NPOI's real IL first (`NPOI.POIDocument::WritePropertySet`,
`NPOI.Util.HexDump::Dump`, ...) surfaced two things worth designing around before writing any
native:

1. NPOI declares real subclasses of `MemoryStream`/`Stream` directly (e.g.
   `NPOI.POIFS.FileSystem.NDocumentOutputStream extends System.IO.MemoryStream`,
   `NPOI.Util.OutputStream extends System.IO.Stream`) — a managed class chaining its own `.ctor`
   into a *native* BCL base class, the exact same shape `system_exception.go`'s
   `baseExceptionCtorInPlace` already solved for exception subclasses (Fase 3.13). No interpreter
   change needed at all: `newObj` already allocates the derived object with its own `Type`/fields
   and then calls its `.ctor`, which itself chains via a plain `call` into
   `System.IO.MemoryStream::.ctor` — registering that name as a regular (non-newobj) native that
   mutates the *existing* receiver's `Obj.Native` in place is purely additive `internal/bcl` work.
2. Real code overwhelmingly holds a `MemoryStream` in a `Stream`-typed local/parameter
   (`Stream s = new MemoryStream();`), so call sites compile against the declared `System.IO.
   Stream::Method` name, not `MemoryStream::Method`. Fase 3.27's virtual dispatch already tries the
   receiver's real concrete type first (via `bcl.NativeTypeName`) before ever falling back to the
   declared name — so registering everything under `System.IO.MemoryStream::*` alone would already
   resolve a `Stream`-declared callvirt site correctly; both names are registered anyway to also
   cover a plain (non-virtual) call site naming `Stream` directly.

**New native: `internal/bcl/system_io.go`**

- [x] `nativeMemoryStream{buf []byte, pos int, closed bool}` — `Write`/`WriteByte`/`Read`/
      `ReadByte`/`Seek`/`SetLength`/`Flush`/`Close`/`Dispose`/`CopyTo`/`get_Length`/`get_Position`/
      `set_Position`/`get_CanRead`/`get_CanWrite`/`get_CanSeek`, plus `ToArray`/`GetBuffer`
      (MemoryStream-only in real .NET, registered once). `System.IO.IOException` and
      `System.IO.EndOfStreamException` added to `system_exception.go`'s existing flat exception
      list (65 findings; `throw new IOException(...)` needed nothing beyond that one line).
- [x] `internal/checker/profile.go`: `System.IO.MemoryStream::`/`System.IO.Stream::`/
      `System.IO.IOException`/`System.IO.EndOfStreamException` added to the `netstandard-lite`
      allowlist — forgetting this step made the first re-measurement show *zero* movement despite
      `bcl.Lookup` resolving correctly: `checkTarget` still flags a resolvable-but-out-of-profile
      target as `KindOutOfProfile` (see Fase 3.27's `System.Enum::HasFlag`/`System.Array::Copy`
      addition for the same two-step pattern).

**A second, unrelated real bug found while re-measuring ClosedXML**

- [x] `ClosedXML@0.105.0`'s own `.nuspec` declares its `DocumentFormat.OpenXml` dependency as a
      NuGet version *range* (`[3.1.1, 4.0.0)`), not a plain pin — common in real `.nuspec` files,
      but `internal/nuget`'s resolver had no range parsing at all: `Resolver.visit` handed the raw
      range string straight to `Cache.Fetch` as if it were an exact version, which 404's against
      `api.nuget.org`. This broke `vmnet check package ClosedXML@0.105.0`'s new (Fase 3.29)
      dependency resolution outright — and would equally have broken the real
      `vm.NuGet().Restore()` → `vm.LoadPackage("ClosedXML")` runtime path, since both share the
      same `Resolver.Resolve`. `nuget.ParseMinVersion(v string) string` (`internal/nuget/
      version.go`) extracts a range's lower bound (`"[3.1.1, 4.0.0)"` → `"3.1.1"`) — the same
      "lowest applicable version" NuGet itself defaults to for a plain `PackageReference` with no
      floating notation, and deterministic without an extra round-trip to enumerate every
      available version. `Resolver.visit` normalizes through it before every fetch.

**Result**

| Package | Fase 3.29 clean % | Fase 3.30 clean % |
|---|---|---|
| `NPOI@2.8.0` | 92.0% (`MethodsFlagged` 1131) | 94.2% (`MethodsFlagged` 825) |
| `ClosedXML@0.105.0` | n/a (dependency resolution failed — see above) | 90.2% (`MethodsFlagged` 1029, `Dependencies resolved: 12`) |

`System.IO`-prefixed findings are gone from both packages' top findings entirely. Re-checked the
existing 8 certified targets (`Jint`, `Newtonsoft.Json`, `Ardalis.GuardClauses`) after the resolver
fix — dependency counts and method counts unchanged, no regression from either fix.

### How to verify Fase 3.30

```bash
go build ./...
go vet ./...
go test ./... -race -count=5
/tmp/vmnet-cli check package NPOI@2.8.0 --profile=netstandard-lite       # Clean-relevant: no System.IO findings
/tmp/vmnet-cli check package ClosedXML@0.105.0 --profile=netstandard-lite # Dependencies resolved: 12 (was a hard error before)
```

### Fase 3.31 — `System.Math` gaps (`Pow`/`Round`/`Log`/trig/`Ceiling`/`Truncate`/...)

Second-highest cross-package blocker after Fase 3.30: `System.Math` had only `Abs`/`Min`/`Max`/
`Floor` natively implemented — `Pow` (53 findings in NPOI, 19 in ClosedXML) and `Round` (40 + 24)
alone accounted for real formula/formatting code paths in both packages, plus `Log`/`Ceiling`/
`Truncate`/`Sqrt`/the trig functions individually smaller but part of the same easily-fixed gap
class. `"System.Math::"` was already a wildcard prefix in every profile including `minimal` (it
predates this loop) — the earlier findings were pure `unsupported-bcl-method` (nothing registered
in `bcl.Lookup`), not an `out-of-profile` allowlist gap, so no `profile.go` change was needed this
time.

- [x] `internal/bcl/system_math.go`: added `Ceiling`/`Truncate`/`Pow`/`Sqrt`/`Log`/`Log10`/`Log2`/
      `Exp`/`Sign`/`Round`/`Sin`/`Cos`/`Tan`/`Atan`/`Atan2`. `Round(double)`/`Round(double, digits)`
      share one native disambiguated by arg count (the same shape `Log(double)`/`Log(double,
      newBase)` needs — `resolveCallTarget` never disambiguates overloads by signature, only by
      the call target's bare name). Matches real .NET's default `MidpointRounding.ToEven`
      ("banker's rounding") via Go's `math.RoundToEven`, not naive round-half-away-from-zero —
      correctness matters here since spreadsheet formula results are exactly the kind of value a
      demo would display and compare against a known-correct answer. A `MidpointRounding` enum
      argument, when present, is accepted but not distinguished (no target package's real IL in
      this loop was found relying on `AwayFromZero` specifically); `System.Decimal`-typed
      `Math.Round` overloads are out of scope until `System.Decimal` itself has a real native
      (`System.Decimal::op_Explicit`, 17 findings, remains a separate open gap).

**Result**

| Package | Fase 3.30 clean % | Fase 3.31 clean % |
|---|---|---|
| `NPOI@2.8.0` | 94.2% (`MethodsFlagged` 825) | 94.7% (`MethodsFlagged` 748) |
| `ClosedXML@0.105.0` | 90.2% (`MethodsFlagged` 1029) | 90.9% (`MethodsFlagged` 947) |

### How to verify Fase 3.31

```bash
go build ./...
go vet ./...
go test ./... -race -count=5
/tmp/vmnet-cli check package NPOI@2.8.0 --profile=netstandard-lite       # no System.Math findings
/tmp/vmnet-cli check package ClosedXML@0.105.0 --profile=netstandard-lite # no System.Math findings
```

### Fase 3.32 — `Dictionary.Values`/`.Keys`, `List.Remove`/`ForEach`, and 11 more LINQ methods

ClosedXML's single largest remaining blocker class: `System.Linq.Enumerable` gaps (`Cast`/`First`/
`LastOrDefault`/`Count`/`Distinct`/`OrderBy`/`Concat`/`OfType`/`ToDictionary`/`Max` — ~370 combined
findings), plus `Dictionary.Values`/`.Keys` and their `ValueCollection`/`KeyCollection` enumerators
(~130 combined across both packages) and `List<T>.Remove`/`.ForEach` (25 + 43). All machine-aware
LINQ work reuses `internal/interpreter/linq.go`'s existing `enumerateAll`/`linqInvoke` machinery
(Fase 3.14) — genuinely additive, no interpreter-core change.

- [x] `internal/bcl/system_collections.go`: `Dictionary.get_Values`/`.get_Keys` return a plain
      snapshot `nativeList` — `ValueCollection`/`KeyCollection`'s own `GetEnumerator`/`MoveNext`/
      `get_Current` are registered to `List<T>`'s existing enumerator natives verbatim rather than
      duplicated: nothing downstream inspects an enumerator's own reported struct type name, only
      its `MoveNext`/`get_Current` behavior. `List<T>.Remove` added (reference/value equality via
      the existing `valuesEqual`, same notion `Object.Equals` already uses). New exported
      `bcl.NewDictValue(map[string]runtime.Value)` — `linq.go`'s `ToDictionary` needs to build a
      real `Dictionary` instance without reaching into `bcl`'s unexported `nativeDict`.
- [x] `internal/interpreter/linq.go`: `Cast`/`OfType` (pass-through — no reified generic
      type-argument info to type-check/filter against, same documented approximation as
      `Dictionary.TryGetValue`'s untyped miss case), `First` (throws
      `InvalidOperationException` on empty/no-match, unlike `FirstOrDefault`), `LastOrDefault`,
      `Count`, `Distinct` (O(n²) `valuesDeepEqual` dedup — fine at the sizes real code hits),
      `Concat`, `OrderBy` + a new `linqCompare` helper (numeric/string ordering; a
      mismatched-Kind or non-primitive comparison is reported, not guessed — vmnet has no
      `IComparable` dispatch), `Max`, `ToDictionary` (keys stringified via `Value.String()` — the
      existing string-keys-only scope), and `List<T>.ForEach` (machine-aware for the same reason
      every other delegate-invoking LINQ method is — needs `Machine.invokeFunc`).
- [x] `internal/checker/analyzer.go`'s `linqTargets` and `internal/checker/profile.go`'s
      `netstandard-lite` allowlist both updated for every new name — the same two-step
      registration Fase 3.27/3.30/3.31 already established (a native alone isn't enough; the
      checker has its own separate awareness of the Machine-registry surface, by design — see
      `linqTargets`'s own doc comment).

**Result**

| Package | Fase 3.31 clean % | Fase 3.32 clean % |
|---|---|---|
| `NPOI@2.8.0` | 94.7% (`MethodsFlagged` 748) | 95.1% (`MethodsFlagged` 694) |
| `ClosedXML@0.105.0` | 90.9% (`MethodsFlagged` 947) | 92.8% (`MethodsFlagged` 757) |

ClosedXML's remaining top blockers shifted decisively toward XML serialization itself —
`System.Xml.XmlWriter` (`WriteStartElement`/`WriteEndElement`/`WriteAttributeString`/
`WriteStartAttribute`/`WriteEndAttribute`, ~205 combined) and `System.Xml.Linq`
(`XName`/`XElement`/`XAttribute`/`XContainer`, ~65 combined) — squarely the machinery an
`.xlsx`-writing demo will actually exercise, a good sign for sequencing.

### How to verify Fase 3.32

```bash
go build ./...
go vet ./...
go test ./... -race -count=5
/tmp/vmnet-cli check package NPOI@2.8.0 --profile=netstandard-lite
/tmp/vmnet-cli check package ClosedXML@0.105.0 --profile=netstandard-lite
```

### Fase 3.33 — `System.Xml.XmlWriter`

The XML serialization machinery a `.xlsx`-writing demo will actually exercise:
`WriteStartElement`/`WriteEndElement`/`WriteAttributeString`/`WriteStartAttribute`/
`WriteEndAttribute` alone were ~205 combined findings in ClosedXML. Probing real ClosedXML IL
(`ClosedXML.Excel.IO.CommentPartWriter::GenerateWorksheetCommentsPartContent`) confirmed the
concrete shape: `XmlWriter.Create(Stream, XmlWriterSettings)` — the same `System.IO.Stream`
Fase 3.30 already built, meaning `XmlWriter` can write incrementally straight into an existing
`nativeMemoryStream`'s buffer rather than needing any new I/O primitive underneath it.

- [x] `internal/bcl/system_xml.go` (new): `nativeXmlWriter{dest *nativeMemoryStream, stack
      []xmlWriterFrame, ...}` — a small explicit element stack tracks, per open element, whether
      its start tag's `'>'` has been emitted yet (`tagClosed`), so `WriteEndElement` can correctly
      choose between self-closing `"/>"` (nothing written since the start tag) and a real
      `"</name>"` — verified against a direct probe: `<root id="1"><child>hello &amp;
      &lt;world&gt;</child><empty/><leaf>v</leaf></root>`, confirming self-closing, nesting, and
      entity-escaping (`&`/`<`/`>`/`"`) all come out well-formed. `WriteStartElement`/
      `WriteAttributeString`/`WriteElementString` each collapse several real overloads
      (`(name)`/`(name, ns)`/`(prefix, name, ns)`) into one native disambiguated by argument count,
      same pattern as every other multi-overload BCL native in this codebase; the namespace URI
      argument itself is dropped (documented — ClosedXML emits its own explicit `xmlns:` attributes
      where it needs them, so vmnet doesn't need to synthesize any). `WriteStartAttribute`/
      `WriteString`/`WriteEndAttribute` share state via `inAttr`/`attrBuf`, matching how real
      `WriteString` dispatches to either the open attribute's value or the element's text content
      depending on writer state. `Close`/`Dispose` walk the remaining open-element stack to close
      out any unbalanced sequence, matching real `XmlWriter`. `XmlWriterSettings` is a trivial
      object with no-op property setters — none of them (`CloseOutput`/`Encoding`/`Indent`/...)
      change the writer's actual output shape for any real IL found in this loop.
- [x] `internal/checker/profile.go`: `System.Xml.XmlWriter::`/`System.Xml.XmlWriterSettings::`
      added to the `netstandard-lite` allowlist (first `System.Xml.*` entry at all).

**Result**

| Package | Fase 3.32 clean % | Fase 3.33 clean % |
|---|---|---|
| `NPOI@2.8.0` | 95.1% (`MethodsFlagged` 694) | 95.1% (unchanged — no `System.Xml.XmlWriter` usage) |
| `ClosedXML@0.105.0` | 92.8% (`MethodsFlagged` 757) | 92.9% (`MethodsFlagged` 741) |

`XmlWriter` findings are gone from ClosedXML's top findings entirely; the modest method-count
delta (757→741, smaller than the ~200 raw findings removed) is the same overlap effect Fase 3.29
first documented — many of those methods were *already* flagged for a different reason too
(`System.Xml.Linq`, LINQ methods still missing) and only drop off the flagged count once every
finding on them clears. ClosedXML's remaining top blockers are now `System.Xml.Linq`
(`XName`/`XElement`/`XAttribute`/`XContainer`, LINQ-to-XML — used for *reading* existing XML parts,
a different concern from `XmlWriter`'s write path) and a handful more `System.Linq.Enumerable`
methods (`Single`/`SingleOrDefault`/`OrderByDescending`/`ElementAt`/`Skip`/`Union`).

### How to verify Fase 3.33

```bash
go build ./...
go vet ./...
go test ./... -race -count=5
/tmp/vmnet-cli check package ClosedXML@0.105.0 --profile=netstandard-lite # no System.Xml.XmlWriter findings
```

### Fase 3.34 — 6 more `System.Linq.Enumerable` methods

Quick, mechanical follow-up clearing out the rest of ClosedXML's small LINQ tail before moving to
the bigger `System.Xml.Linq` blocker: `Single`/`SingleOrDefault` (like `First`/`FirstOrDefault` but
also throwing `InvalidOperationException` on more than one match), `OrderByDescending` (built by
reversing `OrderBy`'s ascending result rather than duplicating the sort), `ElementAt`, `Skip`,
`Union` (`Concat` + `Distinct`'s dedup, preserving first-occurrence order across both sequences).
Every one reuses `enumerateAll`/`linqInvoke`/`linqCompare` from Fase 3.14/3.32 — no new machinery.

- [x] `internal/interpreter/linq.go`: the 6 methods above.
- [x] `internal/checker/analyzer.go`'s `linqTargets` updated to match — same two-step registration
      every Machine-registry addition needs.

**Result**

| Package | Fase 3.33 clean % | Fase 3.34 clean % |
|---|---|---|
| `ClosedXML@0.105.0` | 92.9% (`MethodsFlagged` 741) | 93.3% (`MethodsFlagged` 703) |

Remaining top blockers: `System.Xml.Linq` (`XName`/`XElement`/`XAttribute`/`XContainer` — reading
existing XML parts, e.g. template worksheets), `System.Collections.Generic.IReadOnlyDictionary`2::
get_Item` (45, an interface-typed call the Fase 3.13 interface-dispatch fallback doesn't cover
yet), and a long tail of smaller items (`System.Drawing.Color`/`Point`, `DateTime.ToOADate`/
`FromOADate`, `System.Reflection.CustomAttributeData`).

### How to verify Fase 3.34

```bash
go build ./...
go vet ./...
go test ./... -race -count=5
/tmp/vmnet-cli check package ClosedXML@0.105.0 --profile=netstandard-lite
```

### Fase 3.35 — `System.Xml.Linq` (`XDocument`/`XElement`/`XAttribute`/`XName`)

ClosedXML's LINQ-to-XML *reading* path (`System.Xml.Linq`) — a different concern from Fase 3.33's
`XmlWriter` *writing* path, used by ClosedXML to read its own previously-written VML comment/shape
XML parts back out during load (`XLWorkbook.GetCommentShapes`/`DeleteExistingCommentsShapes`,
`XDocumentExtensions.Load`). Probing confirmed the entry point: `XDocument.Load(Stream)` — the same
`System.IO.Stream`/`MemoryStream` Fase 3.30 built, so no new I/O primitive was needed here either.

- [x] `internal/bcl/system_xmllinq.go` (new): `nativeXElement{name, attrs, children, text}` is a
      small parsed tree built with Go's stdlib `encoding/xml.Decoder` (token-by-token — `xml.
      StartElement`/`EndElement`/`CharData`), not a hand-rolled parser. `XDocument` just wraps a
      root `XElement`, matching real `XDocument.Root`. Verified end to end against a real
      round-trip: `XmlWriter` (Fase 3.33) writes `<shapes><shape id="42" /></shapes>` into a
      `MemoryStream`, `XDocument.Load` parses it back, `.Root.Elements()` finds the one child,
      `.Attribute("id").Value` reads `"42"` back out correctly.
    - Namespace URIs are dropped throughout, matched only by local name — same simplification
      `XmlWriter` already made for its own namespace arguments; reading back a package's own
      previously-written XML never actually needs namespace disambiguation to find what it's
      looking for by local name alone.
    - `XName` is modeled as a plain `System.String`, not its own object type: every consumer here
      only ever needs a local name to match against, so `op_Implicit`/`get_LocalName`/`Get` are all
      the identity function on the string itself (`Get`'s namespace argument is dropped the same
      way).
    - `Elements()`/`Element(name)` are registered under both `XContainer::` (the real declared
      base) and `XElement::` directly (real code sometimes calls it against a local already typed
      as the concrete `XElement`) — confirmed both shapes appear in real ClosedXML IL.
    - `XElement.Value` concatenates all descendant text recursively, matching real semantics for
      the general case, even though the leaf-element-with-no-children shape is what ClosedXML's
      own usage actually hits.
    - Known gap, left undone: `System.Xml.Linq.Extensions::Remove` (an extension method removing
      an element from its parent) would need `nativeXElement` to track a parent pointer, which the
      current tree doesn't. Only used by `DeleteExistingCommentsShapes` — cleaning up existing VML
      comment shapes before regenerating them when *loading* a file that already has comments, not
      exercised by this loop's create-from-scratch demo scenario. Left as a documented gap rather
      than built speculatively.
- [x] `internal/checker/profile.go`: `System.Xml.Linq.XDocument::`/`XContainer::`/`XElement::`/
      `XAttribute::`/`XName::` added to the `netstandard-lite` allowlist.

**Result**

| Package | Fase 3.34 clean % | Fase 3.35 clean % |
|---|---|---|
| `NPOI@2.8.0` | 95.1% (`MethodsFlagged` 694) | 95.1% (unchanged — no `System.Xml.Linq` usage) |
| `ClosedXML@0.105.0` | 93.3% (`MethodsFlagged` 703) | 93.5% (`MethodsFlagged` 684) |

`System.Xml.Linq` findings are gone from ClosedXML's top findings entirely. Remaining top
blockers are now a long tail of smaller, unrelated items: `IReadOnlyDictionary`2::get_Item` (45,
an interface-typed call), `DateTime.ToOADate`/`FromOADate` (Excel's serial-date conversion, 24
combined), `System.Drawing.Color`/`Point`, `System.Reflection.CustomAttributeData`, and several
more `System.Linq.Enumerable` methods (`GroupBy`/`Range`/`Min`/`SequenceEqual`) — none individually
large enough to justify its own phase title the way `System.IO`/`System.Math`/`System.Linq`/
`System.Xml.*` did.

### How to verify Fase 3.35

```bash
go build ./...
go vet ./...
go test ./... -race -count=5
/tmp/vmnet-cli check package ClosedXML@0.105.0 --profile=netstandard-lite # no System.Xml.Linq findings
```

---
## Fase 4 — production-ready v1.0 ("Ready to ship")

**Goal:** turn the functional engine into an adoptable product — reliable, documented, and
benchmarked — the complete package for an engineering team to approve a real pilot.

### Tasks

**Security / sandbox**
- [ ] Complete `Permissions` model (`AllowConsole/AllowFileRead/AllowNetwork`, deny-by-default)
      wired into every native BCL method
- [x] `MaxArrayLength` — pulled forward into Fase 3.5 alongside `System.Array` support (it had to
      exist from day one of `newarr`, no point waiting for Fase 4)
- [ ] `MaxStringBytes`
- [ ] `docs/security.md` — threat model, what gets blocked by default

**Error model**
- [ ] Full catalog of `VMNET_*` codes (spec §30.2) implemented consistently
- [ ] Polished managed exception stack traces (format from spec §18.3)

**Performance / benchmarks**
- [ ] Benchmark suite (spec §32): arithmetic loop, string concat, JSON in/out,
      object allocation, `List.Add`, `Dictionary` lookup, 10k rule engine calls
- [ ] Comparison vs native Go and, where feasible, vs native CoreCLR execution
- [ ] Method/token resolution cache, hot-path optimization pass

**Stable API/CLI**
- [ ] Freeze the public Go API (spec §6) for v1.0, semver commitment
- [ ] Complete CLI command set (inspect/il/check/run/add/restore/packages)
- [ ] Cross-platform CI matrix: Linux/macOS/Windows, verify `CGO_ENABLED=0`

**Tests**
- [ ] Complete golden suite (spec §28.1–28.5)
- [ ] Coverage target agreed with stakeholders (e.g. ≥70% on core packages)

**Documentation (spec §33)**
- [ ] Complete README (what it is / what it isn't, quickstart, profiles, known limits)
- [ ] `docs/en/architecture.md`, `supported-il.md`, `supported-bcl.md`, `nuget-support.md`,
      `compatibility-profile.md`, `security.md`, `roadmap.md`
- [ ] `/examples`: hello, rules, calculator, nuget-basic — runnable and documented

### Fase 4 closing demo — "Production ready" (~15 min, executive focus)

1. Zero-to-running in under 5 minutes on a clean machine: `go get`, `dotnet build` of
   a plugin, `vmnet run` — timed on screen.
2. Benchmark chart on screen: vmnet vs CoreCLR vs plain Go for the rule
   engine workload — honest numbers, showing it's good enough for the target use case.
3. Security demo: a plugin that tries to read a file or make an HTTP call gets
   blocked by the default permissions, with a clear log.
4. Docs/README walkthrough — supported IL/BCL tables, compatibility profiles, list of
   certified NuGet packages.
5. Green CI on Linux/macOS/Windows with no .NET SDK installed on the runners (it's only used in
   a separate dev step to build the test fixtures).

**Sales pitch:** "This is no longer a prototype — it's versioned, documented, benchmarked,
secured, and cross-platform. It's ready for a real integration pilot."

---

## Risk register (mapped to phases)

| Risk | Phase where it's exposed | Mitigation |
|---|---|---|
| BCL (`System.*`) is harder than the IL parser | 2–3 | Start minimal, implement on demand, strong checker, certify concrete packages |
| Arbitrary NuGet has too much variety | 3 | `netstandard2.0` only initially, block native assets/P-Invoke/heavy reflection, curated catalog |
| Expectation of "runs any .NET DLL" | All (communication) | Clear naming, mandatory `vmnet check` before loading third-party code, explicit docs on what it isn't |
| Interpreter performance vs CoreCLR | 4 | Own IR, token/method cache, honest benchmarks, future IL→Go codegen roadmap |
| Dependency on .NET SDK to generate test fixtures | 0–1 | Document that it's dev-dependency only, never runtime; consider pre-compiled fixture DLLs versioned in the repo |

## Beyond the 4 phases (post-v1.0 roadmap)

- v1.5 — hybrid backend (`pure-go` / `coreclr` fallback / `worker` process) — spec §39
- `vmnet transpile` — IL → Go source codegen (C# → Go migration) — spec §38
- Expansion of the `netstandard-lite` profile beyond the initial certified packages
- Full reflection (`Type.MakeGenericType`/`GetMethod`/`Assembly.GetType`, attribute/parameter
  reflection)
- **genuinely cooperative** async/Task (scheduler, continuations that truly suspend,
  `Task.Delay` with real waiting, parallelism) — Fase 3.22 already covers the dominant real-world
  pattern (`async`/`await` modeled entirely synchronously: every `Task` any native code produces
  is already completed by construction, so the compiler-generated `MoveNext()` itself
  runs end-to-end in a single call) without needing any of these pieces

## Reference acceptance criteria

See the original spec §35 (MVP) and §36 (NuGet v1) — used as the exit checklist for Fase 1/2 and
Fase 3 respectively, not duplicated here.
