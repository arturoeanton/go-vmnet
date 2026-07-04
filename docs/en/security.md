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
- **Panic recovery at the API boundary**: any Go-level panic inside the interpreter (a bug in
  vmnet itself, not just interpreted code behaving unexpectedly) is recovered and surfaced as a Go
  `error` from `Assembly.Call`/etc., never a crash of the host process. A broken or actively
  adversarial plugin cannot bring down the Go program that embeds it through this path.

These four limits are real, load-bearing, and already prevent the most common way an
untrusted-but-not-malicious plugin misbehaves: it runs forever, recurses forever, or allocates an
unreasonable amount of memory. That's the "stability boundary" this document's opening paragraph
refers to.

## What vmnet does NOT enforce today

This is the section to actually read before deciding whether to run someone else's C# through
vmnet.

### There is no capability/permission model yet

The Roadmap (`docs/en/ROADMAP.md`, Fase 4) has always planned a `Permissions` model
(`AllowConsole`/`AllowFileRead`/`AllowNetwork`, deny-by-default) wired into every native BCL
method that touches the outside world. **It does not exist yet.** Every native BCL method
implemented so far runs with exactly the same privileges as the Go host process itself — there is
no per-assembly, per-call, or per-capability gate of any kind.

### Real file-system write access exists today, with zero restriction

As of Fase 3.53 (the `Microsoft.Data.Sqlite` provider, `internal/bcl/system_data_sqlite.go`),
interpreted C# code can do this:

```csharp
using Microsoft.Data.Sqlite;
var conn = new SqliteConnection("Data Source=/any/path/the/host/process/can/write");
conn.Open();
// real file I/O happens here, at exactly the path the interpreted code chose
```

`SqliteConnection`'s own connection-string parsing (`parseSqliteConnectionString`) does no
validation, no allow-listing, and no restriction to any particular directory — the string the
interpreted code supplies becomes the literal path passed to Go's own `sql.Open`. **Any C# code
running inside vmnet can create, read, or write a real file anywhere the host OS process has
permission to touch**, subject only to whatever the real SQLite file format will tolerate at that
path (an arbitrary path, if writable, gets a new SQLite database file created there; an existing
non-database file will generally fail on the first real query, not on `Open()` itself). This is a
real, working capability today, not a hypothetical one — it is the first genuine persistent-storage
capability interpreted code has ever had in this project, and it has no gate of any kind.

### There is no network or process-spawning surface — yet, deliberately

As of this writing, vmnet has no native implementation of `System.IO.File` (beyond the SQLite path
above), `System.Diagnostics.Process`, or any socket/HTTP client type at all
(`System.Net.Sockets`/`System.Net.Http`). Interpreted code cannot open an arbitrary file through
`File.*`, cannot spawn a subprocess, and cannot make a network connection — not because any of
these are blocked by a security control, but because the BCL surface simply isn't implemented.

This is explicitly **not** an oversight to be casually fixed. Adding real `System.IO.File`/
`System.Diagnostics.Process`/socket support is a planned, wanted feature — but it is deliberately
deferred until the `Permissions` model above lands first, or alongside it. Shipping unrestricted
file/process/network capability to interpreted code before there is any deny-by-default gate would
repeat, at much larger scope, exactly the gap the SQLite provider above already illustrates on a
narrower surface. This ordering is an explicit project decision, not a timeline accident.

## What this means in practice, today

- **Treat vmnet's current sandbox as a stability boundary, not a trust boundary.** It reliably
  stops a buggy or accidentally-adversarial plugin from hanging or crashing your host process. It
  does **not** stop a *deliberately* malicious assembly from reading/writing files the host OS
  process can reach (via the SQLite path above) or from consuming CPU/memory up to the sandbox's
  own limits (which are generous enough to do real, if bounded, work).
- **Only run C# you trust** — your own team's code, or a real, published NuGet package you've
  actually reviewed (or that this project's own `vmnet check`/`docs/en/COMPATIBILITY.md` already
  give you a concrete picture of) — until the `Permissions` model ships. Do not treat vmnet today
  as safe to run arbitrary, adversarial, untrusted C# submitted by a third party (e.g. user-uploaded
  plugins in a multi-tenant service) without your own additional isolation.
- **If you need to run less-trusted code today**, put your own OS-level boundary around the whole
  host process (a container with a read-only or minimally-writable filesystem, a restricted OS
  user, a `seccomp`/jail profile, a dedicated worker process you're willing to kill and restart) —
  vmnet does not yet provide this internally, and the instruction/depth/stack/array limits above
  are not a substitute for it.

## Host-side I/O vs. interpreted-code capability — a distinction worth being explicit about

vmnet's own Go code — the library and CLI you import/run, not the C# it interprets — does real
file I/O (loading a `.dll` you point it at) and real network I/O (`vm.NuGet().Restore()` fetching
packages from `api.nuget.org`) as an ordinary part of its own operation. That is the same trust
level as any other Go dependency you'd import, and is not what this document's threat model is
about. What this document is about is specifically: what can the **interpreted C# code itself**,
running inside the VM, do — independent of what capabilities you as the embedding application
already have.

## Roadmap

- `Permissions` model (`AllowConsole`/`AllowFileRead`/`AllowNetwork`, deny-by-default), wired into
  every native BCL method that touches the outside world — not yet implemented (Fase 4,
  `docs/en/ROADMAP.md`).
- `MaxStringBytes` — a bound on individual string allocation size, alongside the existing
  `MaxArrayLength` — not yet implemented.
- Real `System.IO.File`/`System.Diagnostics.Process`/socket support — planned, but explicitly held
  until the `Permissions` model above lands first or alongside it, per the reasoning above.
- Configurable `Limits` (today's instruction/call-depth/stack-depth/array-length values are fixed
  constants, not yet exposed for a caller to tighten or loosen per use case).
