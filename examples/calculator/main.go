// Command calculator runs a slightly larger arithmetic/loop C# workload
// (Calculator.dll, built from Calculator.cs in this directory) through
// vmnet, times it, runs the exact same two algorithms as native Go for
// a correctness-plus-speed comparison, and — when a locally built
// CoreCLR comparison binary is available (see coreclr/; build it
// yourself with `dotnet build coreclr -c Release`, entirely optional
// and skipped gracefully otherwise) — times the real .NET runtime
// executing the identical C# source too. This is the seed of the Fase 4
// benchmark suite (docs/en/ROADMAP.md; spec.md Sec 32): "arithmetic
// loop... vs native Go and, where feasible, vs native CoreCLR
// execution."
package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	vmnet "github.com/arturoeanton/go-vmnet"
)

// primeBound/squareBound were picked empirically against vmnet's real
// 10,000,000-instruction-per-Call sandbox (internal/interpreter/
// limits.go's DefaultLimits): CountPrimes(50000) and
// SumOfSquares(700000) already exceed it, so these values keep a
// healthy margin under that ceiling rather than sitting right at the
// edge.
const (
	primeBound  = 25000
	squareBound = 500000
)

func main() {
	vm := vmnet.New()

	data, err := os.ReadFile("bin/Release/netstandard2.0/Calculator.dll")
	if err != nil {
		log.Fatalf("read Calculator.dll: %v (run `dotnet build Calculator.csproj -c Release` in this directory first)", err)
	}
	asm, err := vm.LoadBytes("Calculator.dll", data)
	if err != nil {
		log.Fatalf("LoadBytes(Calculator.dll): %v", err)
	}

	vmPrimes, vmPrimesTime := runVM(asm, "CountPrimes", primeBound)
	vmSquares, vmSquaresTime := runVM(asm, "SumOfSquares", squareBound)

	start := time.Now()
	goPrimes := countPrimesGo(primeBound)
	goPrimesTime := time.Since(start)

	start = time.Now()
	goSquares := sumOfSquaresGo(squareBound)
	goSquaresTime := time.Since(start)

	if vmPrimes != goPrimes {
		log.Fatalf("CountPrimes(%d) mismatch: vmnet=%d go=%d", primeBound, vmPrimes, goPrimes)
	}
	if vmSquares != goSquares {
		log.Fatalf("SumOfSquares(%d) mismatch: vmnet=%d go=%d", squareBound, vmSquares, goSquares)
	}

	fmt.Printf("CountPrimes(%d)  = %-10d  vmnet %-12v native Go %v\n", primeBound, vmPrimes, vmPrimesTime, goPrimesTime)
	fmt.Printf("SumOfSquares(%d) = %-10d  vmnet %-12v native Go %v\n", squareBound, vmSquares, vmSquaresTime, goSquaresTime)

	runCoreCLR(primeBound, squareBound, vmPrimes, vmSquares)
}

func runVM(asm *vmnet.Assembly, method string, n int32) (int64, time.Duration) {
	start := time.Now()
	v, err := asm.Call("Calculator.Bench", method, vmnet.Int32(n))
	elapsed := time.Since(start)
	if err != nil {
		log.Fatalf("%s(%d): %v", method, n, err)
	}
	return v.Native().(int64), elapsed
}

// countPrimesGo/sumOfSquaresGo are line-for-line native Go translations
// of Calculator.cs's own Bench.CountPrimes/Bench.SumOfSquares — the
// comparison only means something if both sides run the identical
// algorithm.
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

// runCoreCLR shells out to coreclr/'s own build output (never `dotnet
// run`, so build/restore overhead never pollutes the timing) for a
// real-CoreCLR comparison point. Best-effort: silently skipped (with a
// one-line note) if the project hasn't been built or the dotnet host
// isn't on PATH — a fresh clone of vmnet never needs the .NET SDK
// installed to run any demo; this is the one, clearly-labeled exception,
// and only for a side-by-side timing curiosity, never for correctness.
func runCoreCLR(n, m int, wantPrimes, wantSquares int64) {
	dllCandidates, _ := filepath.Glob("coreclr/bin/Release/*/coreclr.dll")
	if len(dllCandidates) == 0 {
		fmt.Println("(CoreCLR comparison skipped: coreclr/ not built — run `dotnet build coreclr -c Release` first)")
		return
	}
	if _, err := exec.LookPath("dotnet"); err != nil {
		fmt.Println("(CoreCLR comparison skipped: dotnet not found on PATH)")
		return
	}

	start := time.Now()
	out, err := exec.Command("dotnet", dllCandidates[0], strconv.Itoa(n), strconv.Itoa(m)).Output()
	elapsed := time.Since(start)
	if err != nil {
		fmt.Printf("(CoreCLR comparison skipped: running coreclr.dll failed: %v)\n", err)
		return
	}

	gotPrimes, gotSquares, ok := parseCoreCLROutput(string(out))
	if !ok {
		fmt.Println("(CoreCLR comparison skipped: unexpected output from coreclr.dll)")
		return
	}
	if gotPrimes != wantPrimes || gotSquares != wantSquares {
		log.Fatalf("CoreCLR result mismatch: primes=%d (want %d) squares=%d (want %d)", gotPrimes, wantPrimes, gotSquares, wantSquares)
	}
	fmt.Printf("(same computation via real CoreCLR: %v)\n", elapsed)
}

func parseCoreCLROutput(out string) (primes, squares int64, ok bool) {
	found := 0
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if v, has := strings.CutPrefix(line, "primes="); has {
			p, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return 0, 0, false
			}
			primes = p
			found++
		} else if v, has := strings.CutPrefix(line, "squares="); has {
			s, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return 0, 0, false
			}
			squares = s
			found++
		}
	}
	return primes, squares, found == 2
}
