package bcl

import (
	"fmt"
	"math/rand"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// nativeRandom backs System.Random — found via a real, load-bearing
// case: NPOI's own AnalysisToolPak.RandBetween (a spreadsheet formula
// function registered eagerly by a static field initializer that runs
// on first touch of the formula-function registry, not something a
// caller opts into) does `new Random()` and `.NextDouble()` in its own
// static/instance constructors, so opening basically any real workbook
// through NPOI needs this to exist at all, independent of whether the
// workbook's own cells actually use RANDBETWEEN().
type nativeRandom struct {
	r *rand.Rand
}

func init() {
	registerCtor("System.Random", newRandomCtor)
	register("System.Random::Next", true, randomNext)
	register("System.Random::NextDouble", true, randomNextDouble)
}

func newRandomCtor(args []runtime.Value) (*runtime.Object, error) {
	// A seed argument (Random(int seed)), when given, makes the sequence
	// reproducible, matching real Random's documented seeded-ctor
	// behavior; the no-arg overload is seeded from Go's own
	// runtime-random default source (rand.New with no explicit seed
	// uses a randomly-initialized global source as of Go 1.20).
	if len(args) > 0 && args[0].Kind == runtime.KindI4 {
		return &runtime.Object{Native: &nativeRandom{r: rand.New(rand.NewSource(int64(args[0].I4)))}}, nil
	}
	return &runtime.Object{Native: &nativeRandom{r: rand.New(rand.NewSource(rand.Int63()))}}, nil
}

func asRandom(args []runtime.Value) (*nativeRandom, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return nil, fmt.Errorf("bcl: Random method called without a receiver")
	}
	r, ok := args[0].Obj.Native.(*nativeRandom)
	if !ok {
		return nil, fmt.Errorf("bcl: receiver is not a Random")
	}
	return r, nil
}

// randomNext covers Next(), Next(maxValue) and Next(minValue, maxValue)
// — disambiguated by argument count, same pattern as every other
// multi-overload native in this package.
func randomNext(args []runtime.Value) (runtime.Value, error) {
	r, err := asRandom(args)
	if err != nil {
		return runtime.Value{}, err
	}
	switch len(args) {
	case 1:
		return runtime.Int32(r.r.Int31()), nil
	case 2:
		if args[1].Kind != runtime.KindI4 {
			return runtime.Value{}, fmt.Errorf("bcl: Random.Next expects an int argument")
		}
		if args[1].I4 <= 0 {
			return runtime.Int32(0), nil
		}
		return runtime.Int32(r.r.Int31n(args[1].I4)), nil
	default:
		if args[1].Kind != runtime.KindI4 || args[2].Kind != runtime.KindI4 {
			return runtime.Value{}, fmt.Errorf("bcl: Random.Next expects int arguments")
		}
		lo, hi := args[1].I4, args[2].I4
		if hi <= lo {
			return runtime.Int32(lo), nil
		}
		return runtime.Int32(lo + r.r.Int31n(hi-lo)), nil
	}
}

func randomNextDouble(args []runtime.Value) (runtime.Value, error) {
	r, err := asRandom(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Float64(r.r.Float64()), nil
}
