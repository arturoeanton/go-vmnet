# Supported IL

This document exists because "vmnet supports CIL" is meaningless on its own — a decoder can
recognize every ECMA-335 opcode ever defined and still not *execute* half of them. This page draws
that line precisely, grounded in three real files rather than in general CIL knowledge:
`internal/il/opcode.go` (the decoder's opcode table — what vmnet can even parse), `internal/ir/builder.go`
(the single switch statement that turns decoded IL into vmnet's own IR — the real ground truth for
what vmnet *executes*), and `internal/interpreter/eval.go` (the loop that runs that IR — where the
actual runtime semantics, and their real gaps, live).

As spec §33.3 requires this project to say plainly, everywhere:

> vmnet is not a full .NET implementation. vmnet executes a supported subset of CIL and selected
> BCL APIs. Use `vmnet check` before loading third-party assemblies.

Nothing below changes that. This document is a map of the subset, not a promise that the subset is
"basically everything."

## vmnet's approach: decode, lower, execute — or refuse honestly

vmnet reads a real PE file and its real ECMA-335 metadata tables (no IL rewriting, no bytecode
"translation" step outside vmnet itself) and runs each method body through three stages:

1. **Decode** (`internal/il`) — `il.ReadMethodBody` parses the tiny/fat method header (spec
   §II.25.4) and `il.Decode` turns the raw bytes into a flat instruction list, resolving every
   branch/switch target to an absolute offset as it goes. The opcode table in `opcode.go` lists
   **every** documented ECMA-335 opcode, single- and two-byte alike — decoding a method never fails
   just because it contains an opcode vmnet doesn't yet execute. That line is drawn one stage later.
2. **Lower to IR** (`internal/ir`) — `ir.Build` walks the decoded instructions through one large
   `switch` over the opcode's mnemonic and produces vmnet's own IR (`ir.Instr` values: `LoadArg`,
   `BinOp`, `Call`, `Branch`, and so on). This is the file that actually decides what's supported:
   - An opcode with a `case` that appends a real, distinct IR node is supported and executed with
     real semantics (arithmetic, calls, field/array access, branches, exceptions, ...).
   - A handful of opcodes have a `case` that deliberately appends `ir.Nop` — not because they were
     forgotten, but because vmnet's `runtime.Value` representation makes them true no-ops (see
     "Identity no-ops" below). `box`/`unbox.any` are the one case here with a real, documented
     correctness caveat, also covered below.
   - Everything else falls to the `switch`'s `default` case and returns an
     `*ir.UnsupportedOpcodeError{OpCode, Offset}` — a distinct, structured error type (not a bare
     string) specifically so `internal/checker` can report it as a `KindUnsupportedOpcode` finding
     instead of a generic failure (spec §23.5, §11.3's "must recognize but may mark unsupported").
3. **Execute** (`internal/interpreter`) — `Machine.runFrame` interprets the IR node by node. This
   is where runtime semantics some CIL opcodes only *imply* actually get built: real virtual
   dispatch walking the type hierarchy, real `try`/`catch`/`finally`/filter unwinding, real
   `ldelem`/`stelem` bounds checks raising a managed `IndexOutOfRangeException` instead of a Go
   panic, and so on.

Because stage 2 is where "supported" gets decided, this document is organized around
`ir.Build`'s own switch, not around the raw opcode table.

## Identity no-ops: opcodes that are real CIL but do nothing in vmnet

`ir.Build` turns a small, deliberate set of opcodes into `ir.Nop` instead of a distinct IR node,
because vmnet's `runtime.Value` already models what they'd otherwise need to do:

- **`box`, `unbox.any`** — `runtime.Value` is already a uniform tagged union (a `Kind` field plus
  the payload), so "boxing" a value type never needs a representation change: an `int32` is already
  self-describing before and after a `box`. **The real, documented cost of this simplification**: it
  discards the one piece of information a `box`/`unbox.any` pair would otherwise preserve — that a
  particular `KindI4` value was specifically a `bool` or an `enum` member, not a plain `int`. This is
  a *known, currently unfixed* gap (see "Known gaps" below), not a theoretical one — it was found
  running real code.
- **`constrained.`, `volatile.`, `readonly.`** — pure prefixes that only matter to a real memory
  model or a real vtable dispatch choice. `constrained.` only matters for choosing between boxing
  and a value type's own override at a following `callvirt`; since `runtime.Value` already carries
  its real `Kind`, a `callvirt` to e.g. `System.Object::ToString`/`Equals`/`GetHashCode` already
  dispatches on the actual value without needing the hint (see `internal/bcl/system_object.go`).
  `volatile.`/`readonly.` are memory-ordering/aliasing hints meaningless to a `Value`-based
  interpreter with no raw memory model.
- **`unaligned.`** (Fase 3.40, found via `System.Runtime.CompilerServices.Unsafe`'s
  `WriteUnaligned`/`ReadUnaligned`) — a real concern on hardware that faults on misaligned loads,
  meaningless for vmnet's `Value`-based storage, which has no notion of memory alignment at all.
- **`nop`, `break`** — genuinely no-ops in real CIL too (`break` is a debugger breakpoint hint).

## What's supported, by category

Every entry below has a real `case` in `ir.Build`'s switch (`internal/ir/builder.go`) and real
execution semantics in `Machine.runFrame` (`internal/interpreter/eval.go`), unless noted otherwise.

### Arithmetic and comparisons
`add`/`sub`/`mul`/`div`/`div.un`/`rem`/`rem.un`/`and`/`or`/`xor`/`shl`/`shr`/`shr.un`/`neg`/`not`,
`ceq`/`cgt`/`cgt.un`/`clt`/`clt.un`, and the full `conv.*`/`conv.ovf.*`/`conv.ovf.*.un` numeric
conversion family — all collapse to a small set of IR nodes (`BinOp`, `Neg`, `Not`, `Conv`) since
Fase 1. The `.ovf` overflow-checking variants are accepted but currently execute with the *same*
non-checking semantics as their plain counterparts — vmnet does not yet raise
`OverflowException` on an overflowing `add.ovf`/`mul.ovf`/etc. (a real, narrower gap than the ones
below, since day-to-day arithmetic is unaffected).

### Branches, loops, switch
Short and long forms of every conditional/unconditional branch (`br`/`br.s`, `brtrue`/`brfalse`
and their `.s` forms, `beq`/`bge`/`bgt`/`ble`/`blt` and their unsigned/`.s` variants) resolve to
`Branch`/`BranchIfTrue`/`BranchIfFalse`/`BranchCompare` IR with targets pre-resolved to IR indices
at build time — loops are just backward branches, nothing special. `switch` (spec §III.3.68, a real
jump table, out-of-range index falls through to the next instruction) has been supported since
**Fase 3.6** — it was decoded since Fase 1 but not lowered to IR until then, alongside a first batch
of high-reach cheap BCL wins measured across the 7-package + Jint compatibility corpus.

### Method calls
`call` (static dispatch, or a known non-virtual instance target) and `callvirt` (virtual dispatch)
both resolve their `MethodDef`/`MemberRef`/`MethodSpec` token to a `Namespace.Type::Method` full
name at IR-build time. **`callvirt` execution is real virtual dispatch, not a vtable slot lookup**
(vmnet has no vtable at all): `Machine.call` tries the receiver's *concrete* runtime type first,
then climbs `BaseTypeFullName` one ancestor at a time until it finds an override — built in
**Fase 3.7/3.8** (real type hierarchy, `isinst`/`castclass`) and hardened significantly in
**Fase 3.27** (multi-assembly resolution, a full inheritance-chain walk instead of "concrete type
or nothing", real overload resolution by parameter `Kind`/subtype scoring). **Fase 3.13** added the
same mechanism for interface dispatch (`IEnumerable<T>`, `IComparable<T>`, ...), since there's no
`InterfaceImpl`-derived vtable slot to dispatch through either. A `callvirt` on a null receiver
raises a real `System.NullReferenceException`, not a Go nil-pointer panic. `newobj` constructs both
reference types (pushes a real object reference) and value types (spec §III.4.21: builds in a
temporary slot, calls `.ctor` with a managed pointer to it, pushes the *value*) since Fase 3.7.
Generic method calls (`MethodSpec`, e.g. `Guard.Against.Null<string>`) unwrap to a regular call —
vmnet's type erasure means the type arguments usually aren't needed to execute the call, with one
narrow, real exception: a `typeof(T)` inside that generic method's own body, resolved at the call
site via `Frame.MethodGenericArgs` (Fase 3.60).

### Static and instance fields
`ldfld`/`stfld`/`ldflda` (instance) and `ldsfld`/`stsfld`/`ldsflda` (static) have worked since
Fase 1/2. Field access accepts three receiver shapes uniformly per spec §III.4.10/4.28: a class
instance (`KindObject`), a managed pointer to a struct (`KindRef → KindStruct`, how a struct
receives `this` in its own instance methods), and a bare struct value handed over directly
(`KindStruct`, the shape a struct field read can take once its address was already taken earlier in
the same expression — Fase 3.23). Static fields trigger the owning type's `.cctor` on first access,
including safe re-entrant handling for a `.cctor` that reads its own type's statics.

### Instance/type construction: boxing, structs, initobj
`initobj` (real zero-init through an address) and `newobj` over a value type build a genuine
copy-semantics struct (`Value.Clone()` deep-copies `KindStruct`, wired into every point a `Value`
enters a persistent slot — `stloc`/`starg`/`stfld`/`stsfld`/`stelem`/`stind`), not a shared
reference — since **Fase 3.7**. See "Identity no-ops" above for `box`/`unbox.any`.

### Arrays
`newarr`, `ldlen`, every `ldelem.*`/`stelem.*` typed variant plus the generic `ldelem`/`stelem`
token forms, and `ldelema` are all supported, with real bounds checking: an out-of-range index
raises a managed `System.IndexOutOfRangeException`, a null array reference raises
`System.NullReferenceException` — never a Go panic. A value-type array's elements are seeded with a
real zero-valued default (not a blanket `null`), matching real CLR semantics that a value-type
array is never actually null-valued (Fase 3.27). `localloc` (`stackalloc`) is supported as a real
zeroed byte-shaped `runtime.Array` (Fase 3.7 era).

### Strings
`ldstr` resolves the `#US` heap token to a real Go string at IR-build time — string *opcodes* are
this thin; the actual `System.String` method surface (`Concat`, `Substring`, formatting, ...) is a
BCL question, not an IL one (see `docs/en/supported-bcl.md`).

### Exceptions: try/catch/finally/fault/filter
Real exception dispatch, not just an unhandled `throw` — the single architecturally biggest piece
of the IL layer, built in **Fase 3.10**: `il.ReadExceptionHandlers` parses the small/fat
exception-handling clause table (spec §II.25.4.5-6), and `ir.Build` resolves every clause's IL byte
offsets to IR indices exactly like a branch target. At runtime, a `*runtime.ManagedException`
leaving a frame is matched against that method's handlers innermost-to-outermost; a `catch` matches
by the same real type-hierarchy walk `isinst`/`castclass` use; `finally`/`fault` always run,
whether the exception is caught or keeps propagating; `leave`/`leave.s` correctly thread through any
pending `finally` between the exit point and the target before actually jumping; `rethrow` (C#'s
`throw;`) preserves the original exception. **`catch (Foo) when (cond)` exception filter clauses**
were the one form left unsupported at Fase 3.10 (a `HandlerFilter` clause hard-failed the whole
method) — closed in **Fase 3.51**: `FilterOffset` lowers to a real `ir.HandlerFilter` with its own
`FilterStart`, and `endfilter` (opcode `0xFE11`, distinct from `endfinally`'s `0xDC`) runs the filter
body inline to decide whether to enter the handler or keep searching. `throw` on a real, recognized
managed exception object propagates as a real Go error the caller's own frame can catch; throwing
something that isn't a recognized exception object is itself a reported error rather than silently
accepted.

### Generics
vmnet's generics support is **type erasure with two targeted, real exceptions**, not a generic
runtime in the CLR sense — `MethodSpec`/`TypeSpec` instantiations unwrap to their open
`MethodDef`/`TypeDef` at IR-build time, and vmnet's `Value` doesn't carry closed generic type
arguments as a rule. The two places that erasure genuinely breaks real code, both fixed:
- **Method-level**: `typeof(T)` inside a generic method's own body, resolved via
  `Frame.MethodGenericArgs` — carried on the call site as of **Fase 3.60**
  (`Microsoft.Extensions.DependencyInjection`'s
  `ServiceDescriptor.Singleton<TService,TImplementation>()` calling `typeof(TImplementation)` on its
  own open parameter was the real, load-bearing case that forced this).
- **Class-level**: `typeof(T)` on the *enclosing class's* own generic parameter, resolved from the
  current object instance's own `ClassGenericArgs` (populated at its `newobj` site) — added in
  **Fase 3.66** (root-caused via real `AutoMapper`/`CsvHelper` `TypeMap` registration bugs).

A generic method forwarding its own still-open type parameter into another generic call (e.g.
`Method2<T>() { Method1<T>(); }`) is handled with a `"!!N"` sentinel resolved fresh at every call
(`resolveForwardedGenericArgs`), the same mechanism both fixes above share.

### Virtual dispatch and interface dispatch
Covered under "Method calls" above — real, receiver-type-first dispatch with a full inheritance
chain walk (Fase 3.7/3.8/3.27), extended to interfaces without a real vtable (Fase 3.13). There is
no vtable slot anywhere in vmnet; every virtual/interface call is resolved by name against the
receiver's real runtime type at the moment of the call.

### Delegates and closures
`ldftn`/`ldvirtftn` (unbound/virtual method pointer) plus `newobj` on a delegate type all compile
to the exact same shape regardless of the delegate's name (`Action`, `Func`2`, a user's own
`delegate` declaration) — supported since **Fase 3.9** via `runtime.KindFunc`/`runtime.Func`
(`FullName` plus an optional bound receiver), deliberately without modeling
`System.Delegate`/`MulticastDelegate` as real BCL types at all. A delegate's `Invoke` is intercepted
by the receiver's `Kind`, not by a registered `"SomeDelegateType::Invoke"` name (the delegate type
name is unbounded).

## Known gaps

These are real, current, verified-by-reading-the-code gaps — not a hedge. Each one either has no
`case` at all in `ir.Build`'s switch (falls straight to `UnsupportedOpcodeError`) or is a documented,
narrower correctness caveat on an opcode that otherwise works.

**Permanently out of scope:**
- **`calli` — indirect calls through a function pointer** (C# 9+ `delegate*<...>`). There is no
  `case "calli"` in `ir.Build`'s switch at all, so it falls straight to the default
  `UnsupportedOpcodeError`. This isn't a "not implemented yet" gap — it's an architectural boundary:
  vmnet has no native code generation and no raw function-pointer indirection to dispatch through,
  the same boundary `Reflection.Emit` and P/Invoke already sit outside of for this interpreter. See
  `tests/fixtures/csharp/Unsupported.cs`, the checker's own reproducible fixture for exactly this
  case (`Unsupported.FunctionPointerCall`).

**Currently unimplemented (no `case` in `ir.Build` at all — a real assembly using one of these
opcodes gets an `UnsupportedOpcodeError`, not silently wrong behavior):**
- `jmp` (spec §III.3.32, tail-jump to another method with the same arguments — rare in real Roslyn
  output).
- `cpobj` (copy a value type through two managed pointers, distinct from `ldobj`/`stobj` which
  *are* both supported).
- Plain `unbox` (distinct from `unbox.any`, which *is* supported as a no-op) — `unbox` produces a
  managed pointer into the boxed value itself (used to mutate a boxed struct in place), a shape
  vmnet's identity-passthrough boxing model has no case for.
- `sizeof`, `cpblk`/`initblk` (raw unmanaged memory-block operations — meaningless for a
  `Value`-based interpreter with no flat memory model to address), `arglist`/`refanyval`/
  `refanytype`/`mkrefany` (C#'s rare `__arglist` varargs and `TypedReference` features), `ckfinite`.
- The `tail.` and `no.` opcode prefixes (as opposed to the four prefixes covered under "Identity
  no-ops" above, which *are* handled) — a method whose IL uses either one fails the same way as any
  other unhandled opcode.

**Real, narrower correctness caveats (the opcode executes, but not with full fidelity):**
- **`box`/`unbox.any` erase whether a boxed `KindI4` value was a `bool`/`enum` member versus a plain
  `int32`.** Found running real code, not theoretical: a boxed `bool`/enum value reaching
  `string.Format`/an interpolated string prints its raw numeric value (`"1"`/`"0"`, or an enum's
  underlying integer) instead of `"True"`/`"False"` or the member name, because by the time it
  reaches formatting code every `KindI4` looks identical (`docs/en/ROADMAP.md`, Fase 3.51's "Found,
  not fixed" section, and a related Fase 3.68 case in FluentValidation: a boxed value-type argument
  equal to its type's zero — e.g. boxed `0` — is indistinguishable from a real `null`, so
  `x?.ToString()`-style null-conditional checks on it are wrong).
- **`.ovf` arithmetic doesn't check for overflow.** `add.ovf`/`mul.ovf`/`sub.ovf` and their `.un`
  variants execute identically to their non-checking counterparts — no `OverflowException` on
  overflow.
- **Exception filters have one edge case**: `rethrow` tracks only the most recently entered catch's
  exception in a single slot, not a stack — a `rethrow` inside a catch handler that itself contains
  a nested `try`/`catch` sees the inner exception rather than restoring the outer one (documented
  since Fase 3.10, still true).

## Don't memorize this document — run `vmnet check`

This page tells you what's true of the IL layer *in general*. Whether a **specific** real assembly
you care about will actually run is a narrower, more useful question, and vmnet has a tool that
answers it directly instead of asking you to reason about opcode tables:

```bash
go build -o vmnet ./cmd/vmnet
./vmnet check ./YourAssembly.dll
./vmnet check package --profile=netstandard-lite <PackageId>@<Version>
```

`internal/checker`'s static analyzer walks every method in the target (and, for `check package`,
its full transitive dependency graph, resolved the same way `vm.LoadPackage` resolves it at
runtime) and reports, method by method, exactly which opcode or BCL call doesn't resolve — the same
`KindUnsupportedOpcode`/`KindUnsupportedBCL` findings this document's "Known gaps" section
describes, but against your actual code instead of a hypothetical one. See
`docs/en/COMPATIBILITY.md` for what a checker percentage does and doesn't prove (it's a coverage
estimate, not a correctness proof — a method with zero findings can still misbehave if a native
implementation has a bug the checker can't see), and `docs/en/ROADMAP.md` for the full history of
every real gap found and fixed getting the IL layer to where it is today.
