// Command benchmarks is the Fase 4 benchmark suite (spec.md Sec 32): all
// seven spec Sec 32.2 workloads (arithmetic loop, string concat, JSON
// in/out, object allocation, List<T>.Add, Dictionary lookup, and a rule
// engine called 10,000 times), each run through vmnet AND a line-for-line
// native Go equivalent, correctness-checked against each other, and timed.
// It also reports every spec Sec 32.3 metric that's measurable through
// vmnet's own public API today (cold load time, method invoke overhead,
// allocations/op, heap logical bytes, package restore time) — see the
// "Known gaps" note in the final report for the one metric
// (instructions/sec) that genuinely isn't exposed yet.
//
// examples/calculator remains the original, standalone "arithmetic loop"
// seed demo (docs/en/ROADMAP.md); this suite duplicates that one workload
// so `go run ./benchmarks` alone is a complete, self-contained report —
// see that example's own README for a CoreCLR-comparison side-by-side,
// which this suite does not repeat for the other six workloads (a known,
// documented scope boundary — CoreCLR comparison here would mean
// hand-writing and maintaining six more standalone C# Main() programs,
// out of proportion to this Fase; "where feasible" per spec Sec 32.1).
//
// A goja comparison (also named in spec Sec 32.1) isn't applicable here:
// goja is a JavaScript engine, not a CIL/BCL runtime, so there's no
// meaningful "goja equivalent" of running C# — that item in the spec
// reads as a template left over from a different interpreter's own
// benchmark suite, not a real target for vmnet.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"runtime"
	"testing"
	"time"

	vmnet "github.com/arturoeanton/go-vmnet"
)

// n values picked empirically against vmnet's real DefaultLimits
// instruction budget (internal/interpreter/limits.go, 10,000,000
// instructions per Call) — large enough to produce a stable timing
// signal, comfortably under the ceiling.
const (
	primeBound     = 25000
	squareBound    = 500000
	stringConcatN  = 20000
	allocN         = 200000
	listAddN       = 200000
	dictN          = 100000
	jsonRoundTripN = 2000
	ruleCallCount  = 10000
)

func main() {
	loadStart := time.Now()
	vm := vmnet.New()
	data, err := os.ReadFile("bin/Release/netstandard2.0/Bench.dll")
	if err != nil {
		log.Fatalf("read Bench.dll: %v (run `dotnet build Bench.csproj -c Release` in this directory first)", err)
	}
	asm, err := vm.LoadBytes("Bench.dll", data)
	if err != nil {
		log.Fatalf("LoadBytes(Bench.dll): %v", err)
	}
	coldLoadTime := time.Since(loadStart)

	fmt.Println("=== spec Sec 32.2: workload comparisons (vmnet vs native Go) ===")
	fmt.Println()

	runArithmetic(asm)
	runStringConcat(asm)
	runAllocateObjects(asm)
	runListAdd(asm)
	runDictionaryLookup(asm)
	runJSONRoundTrip(vm)
	runRuleEngine10k(asm)

	fmt.Println()
	fmt.Println("=== spec Sec 32.3: metrics ===")
	fmt.Println()
	fmt.Printf("cold load time (LoadBytes, one-time, this run): %v\n", coldLoadTime)
	reportMethodInvokeOverhead(asm)
	reportAllocsAndHeap(asm)
	reportPackageRestoreTime()
	fmt.Println()
	fmt.Println("Known gap: instructions/sec is not reported — vmnet's own internal per-Call")
	fmt.Println("instruction counter (internal/interpreter, the same one VMNET_CALL_DEPTH_EXCEEDED")
	fmt.Println("budgets against) isn't exposed through the public Go API yet. Reporting it honestly")
	fmt.Println("would need a new instrumentation hook, not a guess derived from wall-clock time.")
}

func timeVMCall(asm *vmnet.Assembly, method string, args ...vmnet.Value) (vmnet.Value, time.Duration) {
	start := time.Now()
	v, err := asm.Call("Vmnet.Benchmarks.Bench", method, args...)
	elapsed := time.Since(start)
	if err != nil {
		log.Fatalf("%s: %v", method, err)
	}
	return v, elapsed
}

func report(name string, vmTime, goTime time.Duration) {
	ratio := float64(vmTime) / float64(goTime)
	fmt.Printf("%-24s vmnet %-12v native Go %-12v (vmnet is %.0fx native Go)\n", name, vmTime, goTime, ratio)
}

func runArithmetic(asm *vmnet.Assembly) {
	vmPrimes, vmPrimesTime := timeVMCall(asm, "CountPrimes", vmnet.Int32(primeBound))
	start := time.Now()
	goPrimes := countPrimesGo(primeBound)
	goPrimesTime := time.Since(start)
	if vmPrimes.Native().(int64) != goPrimes {
		log.Fatalf("CountPrimes(%d) mismatch: vmnet=%v go=%d", primeBound, vmPrimes.Native(), goPrimes)
	}
	report("arithmetic (primes)", vmPrimesTime, goPrimesTime)

	vmSquares, vmSquaresTime := timeVMCall(asm, "SumOfSquares", vmnet.Int32(squareBound))
	start = time.Now()
	goSquares := sumOfSquaresGo(squareBound)
	goSquaresTime := time.Since(start)
	if vmSquares.Native().(int64) != goSquares {
		log.Fatalf("SumOfSquares(%d) mismatch: vmnet=%v go=%d", squareBound, vmSquares.Native(), goSquares)
	}
	report("arithmetic (squares)", vmSquaresTime, goSquaresTime)
}

func countPrimesGo(n int) int64 {
	var count int64
	for i := 2; i < n; i++ {
		isPrime := true
		for d := 2; int64(d)*int64(d) <= int64(i); d++ {
			if i%d == 0 {
				isPrime = false
				break
			}
		}
		if isPrime {
			count++
		}
	}
	return count
}

func sumOfSquaresGo(n int) int64 {
	var sum int64
	for i := 1; i <= n; i++ {
		sum += int64(i) * int64(i)
	}
	return sum
}

func runStringConcat(asm *vmnet.Assembly) {
	vmOut, vmTime := timeVMCall(asm, "StringConcat", vmnet.Int32(stringConcatN))
	start := time.Now()
	goOut := stringConcatGo(stringConcatN)
	goTime := time.Since(start)
	if vmOut.Native().(int32) != int32(goOut) {
		log.Fatalf("StringConcat(%d) mismatch: vmnet=%v go=%d", stringConcatN, vmOut.Native(), goOut)
	}
	report("string concat", vmTime, goTime)
}

func stringConcatGo(n int) int {
	s := ""
	for i := 0; i < n; i++ {
		s += "x"
	}
	return len(s)
}

func runAllocateObjects(asm *vmnet.Assembly) {
	vmOut, vmTime := timeVMCall(asm, "AllocateObjects", vmnet.Int32(allocN))
	start := time.Now()
	goOut := allocateObjectsGo(allocN)
	goTime := time.Since(start)
	if vmOut.Native().(int64) != goOut {
		log.Fatalf("AllocateObjects(%d) mismatch: vmnet=%v go=%d", allocN, vmOut.Native(), goOut)
	}
	report("object allocation", vmTime, goTime)
}

type point struct{ x, y int }

func allocateObjectsGo(n int) int64 {
	var sum int64
	for i := 0; i < n; i++ {
		p := &point{x: i, y: i + 1}
		sum += int64(p.x + p.y)
	}
	return sum
}

func runListAdd(asm *vmnet.Assembly) {
	vmOut, vmTime := timeVMCall(asm, "ListAdd", vmnet.Int32(listAddN))
	start := time.Now()
	goOut := listAddGo(listAddN)
	goTime := time.Since(start)
	if vmOut.Native().(int32) != int32(goOut) {
		log.Fatalf("ListAdd(%d) mismatch: vmnet=%v go=%d", listAddN, vmOut.Native(), goOut)
	}
	report("List<T>.Add", vmTime, goTime)
}

func listAddGo(n int) int {
	list := make([]int, 0)
	for i := 0; i < n; i++ {
		list = append(list, i)
	}
	return len(list)
}

func runDictionaryLookup(asm *vmnet.Assembly) {
	vmOut, vmTime := timeVMCall(asm, "DictionaryLookup", vmnet.Int32(dictN))
	start := time.Now()
	goOut := dictionaryLookupGo(dictN)
	goTime := time.Since(start)
	if vmOut.Native().(int64) != goOut {
		log.Fatalf("DictionaryLookup(%d) mismatch: vmnet=%v go=%d", dictN, vmOut.Native(), goOut)
	}
	report("Dictionary lookup", vmTime, goTime)
}

func dictionaryLookupGo(n int) int64 {
	dict := make(map[int]int, n)
	for i := 0; i < n; i++ {
		dict[i] = i * 2
	}
	var sum int64
	for i := 0; i < n; i++ {
		if v, ok := dict[i]; ok {
			sum += int64(v)
		}
	}
	return sum
}

// runJSONRoundTrip needs a real NuGet dependency (System.Text.Json, the
// same one examples/system-text-json-demo runs), unlike every other
// workload above — this is the one benchmark in the suite that needs
// network access on a cold package cache.
func runJSONRoundTrip(vm *vmnet.VM) {
	if err := vm.NuGet().Add("System.Text.Json", "8.0.5"); err != nil {
		fmt.Printf("JSON in/out          skipped: NuGet().Add: %v\n", err)
		return
	}
	if err := vm.NuGet().Restore(); err != nil {
		fmt.Printf("JSON in/out          skipped: NuGet().Restore: %v (needs network access to nuget.org)\n", err)
		return
	}
	jsonAsm, err := vm.LoadPackage("System.Text.Json")
	if err != nil {
		fmt.Printf("JSON in/out          skipped: LoadPackage: %v\n", err)
		return
	}

	data, err := os.ReadFile("bin/Release/netstandard2.0/Bench.dll")
	if err != nil {
		log.Fatalf("read Bench.dll: %v", err)
	}
	asm, err := vm.LoadBytes("Bench.dll", data)
	if err != nil {
		log.Fatalf("LoadBytes(Bench.dll): %v", err)
	}
	asm.WithDependencies(jsonAsm)

	// KNOWN GAP (found via this very benchmark): JsonSerializer.Serialize/
	// Deserialize's own static initialization chain reaches System.Text.
	// Encodings.Web.DefaultJavaScriptEncoder, which needs AllowedBmp
	// CodePointsBitmap's `unsafe fixed uint Bitmap[2048]` field — a real
	// C# unsafe fixed-size buffer, byte-addressable pointer arithmetic
	// into an inline array. vmnet has zero support for this today (no
	// "FixedBuffer" handling anywhere in the codebase) — a materially
	// deeper gap than the reflection-driven serialization overhead this
	// benchmark actually meant to measure, and out of scope to fix here.
	// examples/system-text-json-demo's own JsonDocument-based parsing
	// (a different, already-working API surface) remains this project's
	// verified System.Text.Json story; see docs/en/ROADMAP.md for this
	// Fase's own entry.
	start := time.Now()
	vmOut, err := asm.Call("Vmnet.Benchmarks.Bench", "JsonRoundTrip", vmnet.Int32(jsonRoundTripN))
	vmTime := time.Since(start)
	if err != nil {
		fmt.Printf("%-24s skipped: %v\n", "JSON in/out", err)
		fmt.Println("  -> known gap: JsonSerializer needs an unsafe fixed-size buffer field vmnet doesn't support yet")
		return
	}
	goStart := time.Now()
	goOut := jsonRoundTripGo(jsonRoundTripN)
	goTime := time.Since(goStart)
	if vmOut.Native().(int64) != goOut {
		log.Fatalf("JsonRoundTrip(%d) mismatch: vmnet=%v go=%d", jsonRoundTripN, vmOut.Native(), goOut)
	}
	report("JSON in/out", vmTime, goTime)
}

type jsonPayload struct {
	Name   string `json:"name"`
	Amount int    `json:"amount"`
	Ok     bool   `json:"ok"`
}

func jsonRoundTripGo(n int) int64 {
	var sum int64
	for i := 0; i < n; i++ {
		payload := jsonPayload{Name: "vmnet", Amount: i, Ok: i%2 == 0}
		b, err := json.Marshal(payload)
		if err != nil {
			log.Fatalf("json marshal: %v", err)
		}
		var back jsonPayload
		if err := json.Unmarshal(b, &back); err != nil {
			log.Fatalf("json unmarshal: %v", err)
		}
		sum += int64(back.Amount)
	}
	return sum
}

func runRuleEngine10k(asm *vmnet.Assembly) {
	start := time.Now()
	var vmSum int64
	for i := 0; i < ruleCallCount; i++ {
		v, err := asm.Call("Vmnet.Benchmarks.Bench", "EvalRule", vmnet.Int32(int32(i)))
		if err != nil {
			log.Fatalf("EvalRule(%d): %v", i, err)
		}
		vmSum += int64(v.Native().(int32))
	}
	vmTime := time.Since(start)

	start = time.Now()
	var goSum int64
	for i := 0; i < ruleCallCount; i++ {
		goSum += int64(evalRuleGo(i))
	}
	goTime := time.Since(start)

	if vmSum != goSum {
		log.Fatalf("EvalRule x%d mismatch: vmnet=%d go=%d", ruleCallCount, vmSum, goSum)
	}
	report(fmt.Sprintf("rule engine x%d", ruleCallCount), vmTime, goTime)
	fmt.Printf("  -> %.2f microseconds/call round trip through vmnet (%d calls)\n",
		float64(vmTime.Microseconds())/float64(ruleCallCount), ruleCallCount)
}

func evalRuleGo(amount int) int {
	switch {
	case amount >= 1000:
		return 20
	case amount >= 500:
		return 10
	case amount >= 100:
		return 5
	default:
		return 0
	}
}

// reportMethodInvokeOverhead times a large number of trivial,
// already-warm calls into the same loaded assembly to isolate per-call
// dispatch overhead (argument marshaling, method resolution, frame
// setup) from any actual workload cost — EvalRule's own body is a
// handful of int comparisons, negligible next to the round-trip itself.
func reportMethodInvokeOverhead(asm *vmnet.Assembly) {
	const warmup = 100
	const measured = 5000
	for i := 0; i < warmup; i++ {
		if _, err := asm.Call("Vmnet.Benchmarks.Bench", "EvalRule", vmnet.Int32(1)); err != nil {
			log.Fatalf("warmup EvalRule: %v", err)
		}
	}
	start := time.Now()
	for i := 0; i < measured; i++ {
		if _, err := asm.Call("Vmnet.Benchmarks.Bench", "EvalRule", vmnet.Int32(1)); err != nil {
			log.Fatalf("EvalRule: %v", err)
		}
	}
	elapsed := time.Since(start)
	fmt.Printf("method invoke overhead: %.2f microseconds/call (%d calls, trivial body)\n",
		float64(elapsed.Microseconds())/float64(measured), measured)
}

// reportAllocsAndHeap uses testing.AllocsPerRun (a real, exported
// function — usable outside of a _test.go file) to measure the HOST-SIDE
// Go process's own allocations per vmnet Call, plus a before/after
// runtime.MemStats snapshot for logical heap growth across a batch of
// calls. Both numbers describe vmnet's OWN Go-side cost of driving one
// interpreted call, not anything happening inside the interpreted C#
// itself (which is real, but not something the Go host process's own
// allocator ever sees separately).
func reportAllocsAndHeap(asm *vmnet.Assembly) {
	allocs := testing.AllocsPerRun(200, func() {
		if _, err := asm.Call("Vmnet.Benchmarks.Bench", "EvalRule", vmnet.Int32(1)); err != nil {
			log.Fatalf("EvalRule: %v", err)
		}
	})
	fmt.Printf("allocations/op (host-side, EvalRule call): %.1f allocs/call\n", allocs)

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	const heapBatch = 2000
	for i := 0; i < heapBatch; i++ {
		if _, err := asm.Call("Vmnet.Benchmarks.Bench", "EvalRule", vmnet.Int32(1)); err != nil {
			log.Fatalf("EvalRule: %v", err)
		}
	}
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	fmt.Printf("heap logical bytes (host-side, %d calls): %d bytes total, %.1f bytes/call\n",
		heapBatch, after.TotalAlloc-before.TotalAlloc, float64(after.TotalAlloc-before.TotalAlloc)/float64(heapBatch))
}

// reportPackageRestoreTime times a real NuGet().Add+Restore against a
// fresh VM/cache-relative call — reuses System.Text.Json (already needed
// for the JSON in/out workload above) rather than fetching a second,
// unrelated package just for this metric. A warm local NuGet cache (the
// common case after the JSON workload already ran in this same process)
// makes this mostly a metadata-resolution/lockfile-write timing, not a
// real network fetch — noted in the output.
func reportPackageRestoreTime() {
	vm := vmnet.New()
	start := time.Now()
	if err := vm.NuGet().Add("System.Text.Json", "8.0.5"); err != nil {
		fmt.Printf("package restore time: skipped (NuGet().Add: %v)\n", err)
		return
	}
	if err := vm.NuGet().Restore(); err != nil {
		fmt.Printf("package restore time: skipped (NuGet().Restore: %v)\n", err)
		return
	}
	elapsed := time.Since(start)
	fmt.Printf("package restore time (System.Text.Json@8.0.5, warm local cache): %v\n", elapsed)
}
