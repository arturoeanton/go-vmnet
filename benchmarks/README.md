# benchmarks

The Fase 4 benchmark suite (`docs/en/ROADMAP.md`; spec.md Sec 32): all seven workloads spec Sec
32.2 names, each run through vmnet AND a line-for-line native Go equivalent, correctness-checked
against each other, then timed — plus every spec Sec 32.3 metric that's measurable through
vmnet's own public API today.

```bash
dotnet build Bench.csproj -c Release   # compiles Bench.cs into Bench.dll
go run .
```

The JSON in/out workload additionally needs network access on a cold NuGet cache (it restores the
real `System.Text.Json` package, the same one `examples/system-text-json-demo` uses) — every
other workload is fully self-contained, no network and no NuGet restore required.

Sample output (numbers vary by machine):

```
=== spec Sec 32.2: workload comparisons (vmnet vs native Go) ===

arithmetic (primes)      vmnet 136ms        native Go 237µs        (vmnet is 572x native Go)
arithmetic (squares)     vmnet 130ms        native Go 125µs        (vmnet is 1037x native Go)
string concat            vmnet 20ms         native Go 14ms         (vmnet is 1x native Go)
object allocation        vmnet 215ms        native Go 91µs         (vmnet is 2372x native Go)
List<T>.Add              vmnet 75ms         native Go 232µs        (vmnet is 326x native Go)
Dictionary lookup        vmnet 104ms        native Go 1.4ms        (vmnet is 73x native Go)
JSON in/out              skipped: ... (known gap, see below)
rule engine x10000       vmnet 8ms          native Go 3µs          (vmnet is 2965x native Go)
  -> 0.80 microseconds/call round trip through vmnet (10000 calls)

=== spec Sec 32.3: metrics ===

cold load time (LoadBytes, one-time, this run): 169µs
method invoke overhead: 0.86 microseconds/call (5000 calls, trivial body)
allocations/op (host-side, EvalRule call): 29.0 allocs/call
heap logical bytes (host-side, 2000 calls): 5616016 bytes total, 2808.0 bytes/call
package restore time (System.Text.Json@8.0.5, warm local cache): 27ms
```

## Reading these numbers honestly

vmnet is an interpreter walking a real IL decode -> IR build -> tree-walking execution pipeline
on every call; native Go is compiled machine code. A 300x-3000x gap on tight, allocation-heavy
loops (object allocation, arithmetic, List/Dictionary) is the expected shape for this kind of
comparison — the interesting number for a real host integration is less "how many times slower
than native Go" and more the absolute per-call numbers in the metrics section (method invoke
overhead, allocations/op), which describe what a real Go program embedding vmnet actually pays per
`Assembly.Call`.

`string concat` is the one workload that lands close to native Go (roughly 1x) — both languages
pay the same real cost here (repeated string reallocation on every `+=`), so this workload mostly
measures string-handling overhead rather than interpreter dispatch overhead, and isn't a
representative "how much does vmnet cost" data point on its own.

## Known gaps found by running this suite

- **JSON in/out is currently blocked**, not slow: `System.Text.Json.JsonSerializer.Serialize`/
  `Deserialize`'s own static initialization reaches `System.Text.Encodings.Web.
  DefaultJavaScriptEncoder`, which needs `AllowedBmpCodePointsBitmap`'s own `unsafe fixed uint
  Bitmap[2048]` field — a real C# unsafe fixed-size buffer (byte-addressable pointer arithmetic
  into an inline array). vmnet has no support for this at all today. This is a different, deeper
  API surface than `examples/system-text-json-demo`'s own `JsonDocument`-based parsing, which
  already works and remains this project's verified System.Text.Json story. See
  `docs/en/ROADMAP.md` for this Fase's own entry.
- **instructions/sec isn't reported.** vmnet's own internal per-`Call` instruction counter
  (`internal/interpreter`, the same one `VMNET_CALL_DEPTH_EXCEEDED` budgets against) isn't exposed
  through the public Go API yet — reporting this metric honestly needs a new instrumentation hook,
  not a guess derived from wall-clock time.
- **No CoreCLR comparison for the six new workloads.** `examples/calculator` (the original
  "arithmetic loop" seed of this suite) has a real, working CoreCLR side-by-side comparison
  (`coreclr/`, a `ProjectReference` running the identical C# source through the real .NET
  runtime) — this suite doesn't repeat that setup for the other six workloads, a deliberate scope
  boundary (six more hand-maintained CoreCLR comparison programs is out of proportion to this
  Fase; spec Sec 32.1 itself says "where feasible").
- **No goja comparison.** Spec Sec 32.1 also names "goja equivalent where possible" — goja is a
  JavaScript engine, not a CIL/BCL runtime, so there's no meaningful "goja equivalent" of running
  C#. That item reads as a template left over from a different interpreter's own benchmark suite,
  not a real target for vmnet.
