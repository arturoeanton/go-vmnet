# Security model and threat model

This document describes what vmnet actually enforces today, what it does not, and what a host
application embedding vmnet should and should not assume. It is written to be read before you run
any C# assembly you didn't write yourself — the honest answer, as of this writing, is that vmnet's
sandbox is a **stability boundary**, not yet a full **trust boundary**. Read on for exactly what
that distinction means in practice.

## What vmnet enforces today (a real, working sandbox)

Every `Assembly.Call`/`Instance.Call`/`Assembly.New` invocation runs under `internal/interpreter`'s
own resource limits (`Limits`, `internal/interpreter/limits.go`), currently fixed (not yet
caller-configurable — see the Roadmap section below):

- **Instruction count** (`MaxInstructions`, default 10,000,000 per top-level call): a hard step
  budget on the interpreter's own bytecode dispatch loop. An infinite loop, a runaway recursive
  method, or a deliberately adversarial busy-loop hits this and returns
  `interpreter.ErrInstructionLimitExceeded` instead of hanging the host process forever. Verified
  against a genuine `while(true)` fixture (`tests/fixtures/csharp/Loops.cs`'s `Runaway()`) —
  the sandbox trips reliably, in well under 5 seconds on ordinary hardware.
- **Call depth** (`MaxCallDepth`, default 256) and **stack depth** (`MaxStackDepth`, default
  10,000): bound unbounded recursion and pathological expression-stack growth the same way a real
  CLR's own stack would eventually fault, but deterministically and recoverably instead of a real
  OS-level stack overflow.
- **Array length** (`MaxArrayLength`, default 16 MiB elements): bounds a single `newarr` from
  requesting an unreasonably large allocation.
- **String length** (`MaxStringBytes`, default 64 MiB, Fase 3.72): the same idea as `MaxArrayLength`
  for a single string-producing call — checked *before* the allocation for the two known call sites
  that can request an attacker-chosen size straight from a bare `int` argument (`new string(char,
  int)`, `String.PadLeft`/`PadRight`), plus a general post-call check on every other native's result
  as a safety net.
- **Panic recovery at the API boundary**: any Go-level panic inside the interpreter (a bug in
  vmnet itself, not just interpreted code behaving unexpectedly) is recovered and surfaced as a Go
  `error` from `Assembly.Call`/etc., never a crash of the host process. A broken or actively
  adversarial plugin cannot bring down the Go program that embeds it through this path.

These four limits are real, load-bearing, and already prevent the most common way an
untrusted-but-not-malicious plugin misbehaves: it runs forever, recurses forever, or allocates an
unreasonable amount of memory. That's the "stability boundary" this document's opening paragraph
refers to.

### A real, deny-by-default `Permissions` gate (Fase 3.59)

As of Fase 3.59, `vmnet.VM` carries a `Permissions` capability gate (`permissions.go`,
`vm.Permissions()`), and every native BCL method that reaches real disk I/O is checked against it
**before it runs at all** — a denied capability never executes so much as a `stat(2)`:

```go
vm := vmnet.New()
// vm.Permissions() starts as the zero value: everything below is denied.
vm.Permissions().AllowFileRead = true
vm.Permissions().AllowFileWrite = true
asm, _ := vm.LoadPackage("NPOI@2.8.0")
```

Two independent fields, both `false` by default:

- **`AllowFileRead`** gates `System.IO.File.Exists`/`OpenRead`/`ReadAllText`/`ReadAllBytes`,
  `System.IO.Directory.Exists`, `FileInfo`/`DirectoryInfo`'s read-only members, and opening a
  `FileStream`/`FileInfo.Open` in `FileMode.Open`.
- **`AllowFileWrite`** gates `System.IO.File.Create`/`WriteAllText`/`WriteAllBytes`/`Delete`/
  `SetAttributes`, `System.IO.Directory.CreateDirectory`, `FileInfo`/`DirectoryInfo.Create`/
  `Delete`, and opening a `FileStream` in any mode other than `Open` (`Copy` needs both, since it
  reads the source and writes the destination).

A denied call throws a real `System.UnauthorizedAccessException` — catchable from interpreted C#
exactly like any other exception (`catch (UnauthorizedAccessException)`, or `catch (Exception)`;
see `examples/permissions-demo`, which runs the identical compiled C# three times against three
different `Permissions` configurations and shows all three outcomes, including an independent
re-read from Go confirming the granted case touched a real file on disk, not an in-memory
illusion).

Two **pre-existing** natives that did real, entirely ungated file I/O before this Fase were
retrofitted under the same gate rather than left inconsistent:

- Opening a real `Microsoft.Data.Sqlite.SqliteConnection` (Fase 3.53, `internal/bcl/
  system_data_sqlite.go`) now requires both `AllowFileRead` and `AllowFileWrite`.
- `System.IO.Path.GetTempFileName` (creates a real, empty file on disk, not just a path string)
  now requires `AllowFileWrite`.

`AllowConsole` and `AllowNetwork` fields also exist on `Permissions` today, for forward
compatibility with this document's own long-standing roadmap promise — **neither is enforced
yet**: `System.Console.Write`/`WriteLine` remains always-allowed (unchanged, pre-Permissions
behavior), and no network-touching native exists at all. See "What vmnet does NOT enforce today"
below for exactly what that leaves open.

A `*interpreter.Machine` built without `Permissions` configured (nil) is treated identically to an
explicit, all-denied `Permissions{}` — a missing gate can never silently mean "allow everything."

## What vmnet does NOT enforce today

This is the section to actually read before deciding whether to run someone else's C# through
vmnet.

### Console output and NuGet-package-loaded code still run with full host privilege for everything else

The `Permissions` gate above covers real disk I/O specifically — it does not yet cover console
output (`AllowConsole` is defined but unenforced) or anything else a native BCL method might do
outside vmnet's own managed memory in the future. Every native BCL method that isn't listed above
runs with exactly the same privileges as the Go host process itself.

### There is no network or process-spawning surface — yet, deliberately

As of this writing, vmnet has no native implementation of `System.Diagnostics.Process` or any
socket/HTTP client type at all (`System.Net.Sockets`/`System.Net.Http`). Interpreted code cannot
spawn a subprocess and cannot make a network connection — not because either is blocked by a
security control, but because the BCL surface simply isn't implemented. A corpus-wide scan across
all 19 packages this project tracks (`docs/en/COMPATIBILITY.md`) found **zero real uses of
`System.Diagnostics.Process`** and **zero real uses of raw `System.Net.Sockets`** — only a modest,
real amount of `System.Net.Http` (`ClosedXML`) and `System.Net.IPAddress` (`SimpleBase`, likely for
formatting/validation, not actual networking). Adding either is a planned, wanted feature for when
real demand justifies it, gated by `AllowNetwork` from day one rather than retrofitted the way the
two file-I/O natives above had to be.

## What this means in practice, today

- **Treat vmnet's current sandbox as a stability-plus-file-I/O boundary, not a full trust
  boundary.** It reliably stops a buggy or accidentally-adversarial plugin from hanging or crashing
  your host process, and (as of Fase 3.59) it reliably stops any file read or write you haven't
  explicitly granted via `vm.Permissions()`. It does **not** stop a *deliberately* malicious
  assembly from consuming CPU/memory up to the sandbox's own limits (which are generous enough to
  do real, if bounded, work), and — if you've granted `AllowFileRead`/`AllowFileWrite` at all — from
  doing anything else a real .NET program could do with that same file access.
- **Grant only the file capabilities a specific package actually needs.** `AllowFileRead` and
  `AllowFileWrite` are independent — a host that only needs to open its own package cache read-only
  never has to grant write access at all.
- **Only run C# you trust** for anything this document's Permissions gate doesn't cover yet
  (console output, and whatever a future native BCL method might do before `AllowConsole`/
  `AllowNetwork` are actually enforced) — your own team's code, or a real, published NuGet package
  you've actually reviewed (or that this project's own `vmnet check`/`docs/en/COMPATIBILITY.md`
  already give you a concrete picture of).
- **If you need to run less-trusted code today**, put your own OS-level boundary around the whole
  host process (a container with a read-only or minimally-writable filesystem, a restricted OS
  user, a `seccomp`/jail profile, a dedicated worker process you're willing to kill and restart) —
  vmnet's own limits and Permissions gate are not a substitute for it, only a second layer.

## Host-side I/O vs. interpreted-code capability — a distinction worth being explicit about

vmnet's own Go code — the library and CLI you import/run, not the C# it interprets — does real
file I/O (loading a `.dll` you point it at) and real network I/O (`vm.NuGet().Restore()` fetching
packages from `api.nuget.org`) as an ordinary part of its own operation. That is the same trust
level as any other Go dependency you'd import, and is not what this document's threat model is
about. What this document is about is specifically: what can the **interpreted C# code itself**,
running inside the VM, do — independent of what capabilities you as the embedding application
already have.

## Roadmap

- `AllowConsole`/`AllowNetwork` enforcement — the fields exist on `Permissions` today but gate
  nothing yet (Fase 3.59 only wires up `AllowFileRead`/`AllowFileWrite`); real
  `System.Net.Http`/`System.Net.Sockets` support (see "no network surface" above) would land behind
  `AllowNetwork` from day one.
- Real `System.Diagnostics.Process` support — not planned unless real corpus demand appears (the
  Fase 3.59 scan found none across all 19 tracked packages).
- Configurable `Limits` (today's instruction/call-depth/stack-depth/array-length values are fixed
  constants, not yet exposed for a caller to tighten or loosen per use case).
