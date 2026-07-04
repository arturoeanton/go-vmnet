# calculator

Slightly larger arithmetic/loop example — the seed of the Fase 4 benchmark suite
(`docs/en/ROADMAP.md`; spec.md Sec 32: "arithmetic loop... vs native Go and, where feasible, vs
native CoreCLR execution"). `Calculator.cs` in this directory has no NuGet dependency at all —
two plain, loop-driven methods (`Bench.CountPrimes`, a nested loop + modulo + branch shape, and
`Bench.SumOfSquares`, a single multiply-accumulate loop) — so the only thing under test is
vmnet's own bytecode dispatch loop, not any interpreted BCL method.

```bash
dotnet build Calculator.csproj -c Release   # compiles Calculator.cs into Calculator.dll
go run .
```

`main.go` runs both methods through vmnet, runs the identical algorithm as native Go, and fails
loudly (`log.Fatalf`) if the two ever disagree — this is a correctness check as much as a timing
one. Sample output:

```
CountPrimes(25000)  = 2762        vmnet 120ms          native Go 250µs
SumOfSquares(500000) = 41666791666750000  vmnet 137ms  native Go 125µs
```

The loop bounds (`primeBound`, `squareBound` in `main.go`) were picked empirically against
vmnet's real 10,000,000-instruction-per-`Call` sandbox (`internal/interpreter/limits.go`'s
`DefaultLimits`) — `CountPrimes(50000)` and `SumOfSquares(700000)` already exceed it, so the
chosen values keep a healthy margin under that ceiling.

## Optional: comparison against real CoreCLR

`coreclr/` is a small executable project that `ProjectReference`s `Calculator.csproj` directly —
so it's always running the exact same C# source vmnet interprets, never a hand-duplicated copy
that could drift. Build it once (needs the real .NET SDK; entirely optional, and never a
dependency of the demo itself):

```bash
dotnet build coreclr -c Release
```

Once built, `go run .` also shells out to `coreclr`'s own build output (not `dotnet run`, so
build overhead never pollutes the timing) and prints a third timing line, again verifying the
result matches. If `coreclr/` hasn't been built, or `dotnet` isn't on `PATH`, this step is
skipped with a one-line note — a fresh clone of vmnet never needs the .NET SDK installed to run
this demo, only to build the CoreCLR comparison binary.
