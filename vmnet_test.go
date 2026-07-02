package vmnet

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/arturoeanton/go-vmnet/internal/interpreter"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

const fixtureRelPath = "tests/fixtures/csharp/bin/Release/netstandard2.0/Vmnet.Fixtures.dll"

func loadFixture(t *testing.T) *Assembly {
	t.Helper()
	vm := New()
	asm, err := vm.LoadFile(filepath.FromSlash(fixtureRelPath))
	if err != nil {
		t.Skipf("fixture assembly not built: %v (run `dotnet build tests/fixtures/csharp/Fixtures.csproj -c Release`)", err)
	}
	return asm
}

// TestFase1Demo exercises exactly the three methods docs/ROADMAP.md's
// Fase 1 demo script calls out: SimpleMath.Add, Strings.Hello, Loops.Sum.
func TestFase1Demo(t *testing.T) {
	asm := loadFixture(t)

	t.Run("SimpleMath.Add", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.SimpleMath", "Add", Int32(3), Int32(4))
		if err != nil {
			t.Fatalf("Call() error = %v", err)
		}
		if got := out.Native().(int32); got != 7 {
			t.Errorf("Add(3, 4) = %d, want 7", got)
		}
	})

	t.Run("Strings.Hello", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.Strings", "Hello", String("vmnet"))
		if err != nil {
			t.Fatalf("Call() error = %v", err)
		}
		if got := out.Native().(string); got != "Hello vmnet" {
			t.Errorf("Hello(%q) = %q, want %q", "vmnet", got, "Hello vmnet")
		}
	})

	t.Run("Loops.Sum", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.Loops", "Sum", Int32(10))
		if err != nil {
			t.Fatalf("Call() error = %v", err)
		}
		if got := out.Native().(int32); got != 55 {
			t.Errorf("Sum(10) = %d, want 55", got)
		}
	})
}

func TestCall_ArgumentCountMismatch(t *testing.T) {
	asm := loadFixture(t)
	_, err := asm.Call("Vmnet.Fixtures.SimpleMath", "Add", Int32(1))
	if err == nil {
		t.Fatal("Call() with wrong argument count: error = nil, want an error")
	}
}

func TestCall_UnknownMethod(t *testing.T) {
	asm := loadFixture(t)
	_, err := asm.Call("Vmnet.Fixtures.SimpleMath", "DoesNotExist")
	if err == nil {
		t.Fatal("Call() for an unknown method: error = nil, want an error")
	}
}

// TestFase2Demo exercises exactly what docs/ROADMAP.md's Fase 2 demo
// script calls out: a realistic Rules.Eval (objects, callvirt property
// accessors, List<T>, Dictionary<string,V>) driven through the JSON
// bridge, a managed exception surfacing as a clean Go error, and a
// runaway plugin killed by the instruction sandbox.
func TestFase2Demo(t *testing.T) {
	asm := loadFixture(t)

	t.Run("Rules.Eval via CallBytes", func(t *testing.T) {
		out, err := asm.CallBytes("Vmnet.Fixtures.Rules", "Eval", []byte("go request"))
		if err != nil {
			t.Fatalf("CallBytes() error = %v", err)
		}
		const want = `{"ok":true,"customer":"acme corp"}`
		if string(out) != want {
			t.Errorf("CallBytes() = %s, want %s", out, want)
		}
	})

	t.Run("Rules.Eval via CallJSON", func(t *testing.T) {
		result, err := asm.CallJSON("Vmnet.Fixtures.Rules", "Eval", "go request")
		if err != nil {
			t.Fatalf("CallJSON() error = %v", err)
		}
		m, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("CallJSON() result = %#v, want map[string]any", result)
		}
		if m["ok"] != true || m["customer"] != "acme corp" {
			t.Errorf("CallJSON() result = %#v, want ok=true customer=\"acme corp\"", m)
		}
	})

	t.Run("managed exception on empty input", func(t *testing.T) {
		_, err := asm.CallBytes("Vmnet.Fixtures.Rules", "Eval", []byte(""))
		if err == nil {
			t.Fatal("CallBytes(empty input): error = nil, want a managed exception")
		}
		var ex *runtime.ManagedException
		if !errors.As(err, &ex) {
			t.Fatalf("CallBytes(empty input) error = %v, want it to wrap *runtime.ManagedException", err)
		}
		if ex.TypeName != "System.InvalidOperationException" || ex.Message != "empty input" {
			t.Errorf("exception = %+v, want {System.InvalidOperationException, empty input}", ex)
		}
	})

	t.Run("sandbox kills a runaway plugin", func(t *testing.T) {
		start := time.Now()
		_, err := asm.Call("Vmnet.Fixtures.Loops", "Runaway")
		elapsed := time.Since(start)

		if !errors.Is(err, interpreter.ErrInstructionLimitExceeded) {
			t.Fatalf("Runaway() error = %v, want ErrInstructionLimitExceeded", err)
		}
		if elapsed > 5*time.Second {
			t.Errorf("Runaway() took %v to hit the instruction limit, want well under 5s", elapsed)
		}
	})
}

// TestObjectsAndCollections exercises newobj/callvirt/ldfld/stfld
// (Customer's auto-property accessors) and List<T> independently of the
// Rules.Eval demo scenario.
func TestObjectsAndCollections(t *testing.T) {
	asm := loadFixture(t)

	out, err := asm.Call("Vmnet.Fixtures.CollectionsTest", "Count")
	if err != nil {
		t.Fatalf("CollectionsTest.Count() error = %v", err)
	}
	if got := out.Native().(int32); got != 2 {
		t.Errorf("CollectionsTest.Count() = %d, want 2", got)
	}
}

func TestCall_InstanceMethodRejected(t *testing.T) {
	// Customer.get_Name is an instance method; Call only invokes statics
	// (use CallBytes/CallJSON, or construct via a static factory method).
	asm := loadFixture(t)
	_, err := asm.Call("Vmnet.Fixtures.Customer", "get_Name")
	if err == nil {
		t.Fatal("Call(Customer.get_Name): error = nil, want an instance-method error")
	}
}

// TestConcurrentCalls proves a single *Assembly is safe to share across
// goroutines — e.g. concurrent requests in a Go server. Run with -race:
// before the cacheMu fix this triggered a concurrent map read/write panic
// almost immediately (asm.methods/asm.types are populated lazily on first
// use, from whichever goroutine gets there first).
func TestConcurrentCalls(t *testing.T) {
	asm := loadFixture(t)

	const goroutines = 32
	const perGoroutine = 50

	errCh := make(chan error, goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			for i := 0; i < perGoroutine; i++ {
				sum, err := asm.Call("Vmnet.Fixtures.SimpleMath", "Add", Int32(3), Int32(4))
				if err != nil {
					errCh <- err
					return
				}
				if got := sum.Native().(int32); got != 7 {
					errCh <- fmt.Errorf("Add(3,4) = %d, want 7", got)
					return
				}

				if _, err := asm.CallBytes("Vmnet.Fixtures.Rules", "Eval", []byte("concurrent")); err != nil {
					errCh <- err
					return
				}
			}
			errCh <- nil
		}()
	}

	for g := 0; g < goroutines; g++ {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}
}

// TestArrays exercises System.Array support added in Fase 3.5: newarr,
// individual stelem/ldelem writes and reads, and ldlen via .Length.
func TestArrays(t *testing.T) {
	asm := loadFixture(t)
	out, err := asm.Call("Vmnet.Fixtures.Arrays", "SumArray")
	if err != nil {
		t.Fatalf("Arrays.SumArray() error = %v", err)
	}
	if got := out.Native().(int32); got != 6 {
		t.Errorf("Arrays.SumArray() = %d, want 6", got)
	}
}

// TestByRef exercises the managed-pointer support added in Fase 3.5
// (ldarga.s/ldloca.s/ldind.i4/stind.i4) via `out`/`ref` parameters — the
// single largest real-world blocker found during Fase 3 certification
// (docs/ROADMAP.md).
func TestByRef(t *testing.T) {
	asm := loadFixture(t)

	t.Run("out parameter", func(t *testing.T) {
		tests := []struct{ in, want int32 }{{5, 10}, {0, 0}, {-1, -1}}
		for _, tt := range tests {
			out, err := asm.Call("Vmnet.Fixtures.ByRef", "CallTryDouble", Int32(tt.in))
			if err != nil {
				t.Fatalf("CallTryDouble(%d) error = %v", tt.in, err)
			}
			if got := out.Native().(int32); got != tt.want {
				t.Errorf("CallTryDouble(%d) = %d, want %d", tt.in, got, tt.want)
			}
		}
	})

	t.Run("ref parameter mutated across two calls", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.ByRef", "CallIncrementTwice", Int32(10))
		if err != nil {
			t.Fatalf("CallIncrementTwice(10) error = %v", err)
		}
		if got := out.Native().(int32); got != 12 {
			t.Errorf("CallIncrementTwice(10) = %d, want 12", got)
		}
	})
}

// TestStatics exercises static fields (ldsfld/stsfld) and the lazy .cctor
// added in Fase 3.5 (internal/interpreter/statics.go). It checks three
// things a naive implementation gets wrong: the .cctor runs exactly once
// and its writes are visible afterward, static state persists across
// separate top-level Call()s on the same Assembly (Type is cached), and a
// never-explicitly-assigned int field defaults to 0, not an untyped null
// that would break arithmetic.
func TestStatics(t *testing.T) {
	asm := loadFixture(t)

	out, err := asm.Call("Vmnet.Fixtures.Statics", "GetInitValue")
	if err != nil {
		t.Fatalf("Statics.GetInitValue() error = %v", err)
	}
	if got := out.Native().(int32); got != 42 {
		t.Errorf("Statics.GetInitValue() = %d, want 42 (.cctor should have run)", got)
	}

	for i, want := range []int32{1, 2, 3} {
		out, err := asm.Call("Vmnet.Fixtures.Statics", "IncrementAndGet")
		if err != nil {
			t.Fatalf("Statics.IncrementAndGet() call %d error = %v", i+1, err)
		}
		if got := out.Native().(int32); got != want {
			t.Errorf("Statics.IncrementAndGet() call %d = %d, want %d", i+1, got, want)
		}
	}
}

// TestSwitch exercises the `switch` opcode added in Fase 3.6 (a jump
// table, decoded since Fase 1 but never lowered by the IR builder until
// now) — including the out-of-range case, which per ECMA-335 §III.3.68
// falls through to the next instruction rather than erroring.
func TestSwitch(t *testing.T) {
	asm := loadFixture(t)
	tests := []struct {
		day  int32
		want string
	}{
		{0, "Sunday"}, {1, "Monday"}, {2, "Tuesday"}, {3, "Wednesday"},
		{4, "Thursday"}, {5, "Unknown"}, {-1, "Unknown"},
	}
	for _, tt := range tests {
		out, err := asm.Call("Vmnet.Fixtures.SwitchTest", "DayName", Int32(tt.day))
		if err != nil {
			t.Fatalf("DayName(%d) error = %v", tt.day, err)
		}
		if got := out.Native().(string); got != tt.want {
			t.Errorf("DayName(%d) = %q, want %q", tt.day, got, tt.want)
		}
	}
}

// TestStructs exercises value types (Fase 3.7): a struct constructed
// in-place via `ldloca` + `call .ctor` with no preceding `initobj` at all
// (relying on locals starting pre-zeroed — runtime.Method.LocalDefaults),
// `Point p = default;` (an explicit `initobj`), copy-on-assignment
// semantics (mutating a copy must not affect the original), and
// `constrained.` dispatching `item.ToString()` on a generic type
// parameter bound to a struct.
func TestStructs(t *testing.T) {
	asm := loadFixture(t)

	t.Run("construct in place", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.Structs", "CreateAndSum")
		if err != nil {
			t.Fatalf("CreateAndSum() error = %v", err)
		}
		if got := out.Native().(int32); got != 7 {
			t.Errorf("CreateAndSum() = %d, want 7", got)
		}
	})

	t.Run("default is zeroed", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.Structs", "DefaultIsZero")
		if err != nil {
			t.Fatalf("DefaultIsZero() error = %v", err)
		}
		if got := out.Native().(int32); got != 0 {
			t.Errorf("DefaultIsZero() = %d, want 0", got)
		}
	})

	t.Run("copy semantics", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.Structs", "CopySemantics")
		if err != nil {
			t.Fatalf("CopySemantics() error = %v", err)
		}
		// a=(1,1) untouched -> 2; b=(1,1) scaled by 10 -> (10,10) -> 20.
		if got := out.Native().(int32); got != 220 {
			t.Errorf("CopySemantics() = %d, want 220 (mutating the copy leaked into the original)", got)
		}
	})

	t.Run("constrained. dispatches ToString on a generic struct param", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.Structs", "DescribePoint")
		if err != nil {
			t.Fatalf("DescribePoint() error = %v", err)
		}
		if got := out.Native().(string); got != "<Point>" {
			t.Errorf("DescribePoint() = %q, want %q", got, "<Point>")
		}
	})

	t.Run("Nullable<T>", func(t *testing.T) {
		hasValue, err := asm.Call("Vmnet.Fixtures.Structs", "HasValueTest", Int32(1), Int32(5))
		if err != nil {
			t.Fatalf("HasValueTest(true, 5) error = %v", err)
		}
		if got := hasValue.Native().(int32); got == 0 {
			t.Errorf("HasValueTest(true, 5) = %d, want nonzero", got)
		}

		noValue, err := asm.Call("Vmnet.Fixtures.Structs", "HasValueTest", Int32(0), Int32(5))
		if err != nil {
			t.Fatalf("HasValueTest(false, 5) error = %v", err)
		}
		if got := noValue.Native().(int32); got != 0 {
			t.Errorf("HasValueTest(false, 5) = %d, want 0", got)
		}

		got, err := asm.Call("Vmnet.Fixtures.Structs", "GetValueOrDefaultTest", Int32(1), Int32(42))
		if err != nil {
			t.Fatalf("GetValueOrDefaultTest(true, 42) error = %v", err)
		}
		if v := got.Native().(int32); v != 42 {
			t.Errorf("GetValueOrDefaultTest(true, 42) = %d, want 42", v)
		}

		gotDefault, err := asm.Call("Vmnet.Fixtures.Structs", "GetValueOrDefaultTest", Int32(0), Int32(42))
		if err != nil {
			t.Fatalf("GetValueOrDefaultTest(false, 42) error = %v", err)
		}
		if v := gotDefault.Native().(int32); v != 0 {
			t.Errorf("GetValueOrDefaultTest(false, 42) = %d, want 0", v)
		}
	})
}

// TestStructsConcurrentResolve races many goroutines to resolve a
// struct-typed type (Point) for the first time on the same Assembly —
// the scenario resolveTypeByFullName's Fase 3.7 redesign exists for (it
// no longer holds cacheMu across the whole build, to avoid a deadlock
// when a struct field/local recursively resolves another type — see its
// doc comment in assembly.go). Run with -race: must neither deadlock nor
// report a data race, and every goroutine must get a correct result
// regardless of which one "wins" the race to populate the cache.
func TestStructsConcurrentResolve(t *testing.T) {
	asm := loadFixture(t)

	const goroutines = 32
	results := make(chan int32, goroutines)
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			out, err := asm.Call("Vmnet.Fixtures.Structs", "CreateAndSum")
			if err != nil {
				errs <- err
				return
			}
			results <- out.Native().(int32)
		}()
	}
	for i := 0; i < goroutines; i++ {
		select {
		case err := <-errs:
			t.Fatalf("CreateAndSum() error = %v", err)
		case got := <-results:
			if got != 7 {
				t.Errorf("CreateAndSum() = %d, want 7", got)
			}
		}
	}
}

// TestStringOps exercises the BCL natives added in Fase 3.6 alongside
// `switch`: StringBuilder (including ToString(), which needs the
// objectToString-dispatch workaround in internal/bcl/system_object.go
// since vmnet has no real virtual dispatch yet — see its doc comment),
// String.Format's composite grammar, Substring, the string indexer, and
// Equals.
func TestStringOps(t *testing.T) {
	asm := loadFixture(t)

	t.Run("StringBuilder", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.StringOps", "BuildGreeting", String("World"))
		if err != nil {
			t.Fatalf("BuildGreeting error = %v", err)
		}
		if got := out.Native().(string); got != "Hello, World!" {
			t.Errorf("BuildGreeting(\"World\") = %q, want %q", got, "Hello, World!")
		}
	})

	t.Run("String.Format", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.StringOps", "FormatReport", String("cpu"), Int32(42), Float64(0.756))
		if err != nil {
			t.Fatalf("FormatReport error = %v", err)
		}
		want := "cpu: 42 items (75.6%)"
		if got := out.Native().(string); got != want {
			t.Errorf("FormatReport(...) = %q, want %q", got, want)
		}
	})

	t.Run("Substring", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.StringOps", "FirstThree", String("Hello"))
		if err != nil {
			t.Fatalf("FirstThree error = %v", err)
		}
		if got := out.Native().(string); got != "Hel" {
			t.Errorf("FirstThree(\"Hello\") = %q, want %q", got, "Hel")
		}
	})

	t.Run("get_Chars", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.StringOps", "FirstChar", String("Hello"))
		if err != nil {
			t.Fatalf("FirstChar error = %v", err)
		}
		if got := out.Native().(int32); got != 'H' {
			t.Errorf("FirstChar(\"Hello\") = %d, want %d ('H')", got, int32('H'))
		}
	})

	t.Run("Equals", func(t *testing.T) {
		tests := []struct {
			a, b string
			want bool
		}{{"abc", "abc", true}, {"abc", "abd", false}}
		for _, tt := range tests {
			out, err := asm.Call("Vmnet.Fixtures.StringOps", "SameText", String(tt.a), String(tt.b))
			if err != nil {
				t.Fatalf("SameText(%q, %q) error = %v", tt.a, tt.b, err)
			}
			if got := out.Native().(int32) != 0; got != tt.want {
				t.Errorf("SameText(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		}
	})
}

// TestStaticsConcurrentCctor races many goroutines to trigger Statics'
// .cctor on the same Assembly's first static access — the exact scenario
// runtime.Type.EnsureCctor and interpreter.Machine.cctorsRunning exist for.
// Run with -race: this must neither deadlock (the bug fixed in Fase 3.5,
// where a .cctor writing its own type's static field re-entered its own
// sync.Once) nor report a data race on the shared statics slice.
func TestStaticsConcurrentCctor(t *testing.T) {
	asm := loadFixture(t)

	const goroutines = 32
	results := make(chan int32, goroutines)
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			out, err := asm.Call("Vmnet.Fixtures.Statics", "GetInitValue")
			if err != nil {
				errs <- err
				return
			}
			results <- out.Native().(int32)
		}()
	}
	for i := 0; i < goroutines; i++ {
		select {
		case err := <-errs:
			t.Fatalf("Statics.GetInitValue() error = %v", err)
		case got := <-results:
			if got != 42 {
				t.Errorf("Statics.GetInitValue() = %d, want 42", got)
			}
		}
	}
}
