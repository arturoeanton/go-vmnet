package vmnet

import (
	"errors"
	"fmt"
	"math"
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

		// Fase 3.13: `int? n = 42;` (ldloca+call .ctor directly on the
		// local, not newobj) — see DirectNullableAssignTest's doc comment.
		direct, err := asm.Call("Vmnet.Fixtures.Structs", "DirectNullableAssignTest")
		if err != nil {
			t.Fatalf("DirectNullableAssignTest() error = %v", err)
		}
		if v := direct.Native().(int32); v != 42 {
			t.Errorf("DirectNullableAssignTest() = %d, want 42", v)
		}
	})
}

// TestDateTimeSpan exercises System.DateTime and Span<T>/ReadOnlySpan<T>
// (Fase 3.12): DateTime construction directly on a local (`ldloca`+
// `call .ctor`, no newobj — the same shape confirmed for plugin structs
// in Fase 3.7, needing its own native entry point here), calendar
// arithmetic crossing a month boundary, CompareTo, a Span<T> over an
// array (Slice, indexed read *and* write-through — Span's indexer
// returns `ref T`, not T, confirmed against real IL), and a
// ReadOnlySpan<char> over a string including ToString() (which — like
// StringBuilder in Fase 3.6 — dispatches through Object::ToString, not
// a direct callvirt).
func TestDateTimeSpan(t *testing.T) {
	asm := loadFixture(t)

	t.Run("DateTime construction and fields", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.DateTimeSpanTest", "YearMonthDay")
		if err != nil {
			t.Fatalf("YearMonthDay() error = %v", err)
		}
		if got := out.Native().(int32); got != 20240315 {
			t.Errorf("YearMonthDay() = %d, want 20240315", got)
		}
	})

	t.Run("AddDays crosses a month boundary", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.DateTimeSpanTest", "AddDaysAcrossMonth")
		if err != nil {
			t.Fatalf("AddDaysAcrossMonth() error = %v", err)
		}
		if got := out.Native().(int32); got != 20240201 {
			t.Errorf("AddDaysAcrossMonth() = %d, want 20240201", got)
		}
	})

	t.Run("CompareTo", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.DateTimeSpanTest", "CompareDates")
		if err != nil {
			t.Fatalf("CompareDates() error = %v", err)
		}
		if got := out.Native().(int32); got != -1 {
			t.Errorf("CompareDates() = %d, want -1", got)
		}
	})

	t.Run("Span<int> over an array", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.DateTimeSpanTest", "SpanSum")
		if err != nil {
			t.Fatalf("SpanSum() error = %v", err)
		}
		if got := out.Native().(int32); got != 90 {
			t.Errorf("SpanSum() = %d, want 90", got)
		}
	})

	t.Run("ReadOnlySpan<char> over a string", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.DateTimeSpanTest", "ReadOnlySpanSubstring")
		if err != nil {
			t.Fatalf("ReadOnlySpanSubstring() error = %v", err)
		}
		if got := out.Native().(string); got != "World" {
			t.Errorf("ReadOnlySpanSubstring() = %q, want %q", got, "World")
		}
	})

	t.Run("Span<int> write-through", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.DateTimeSpanTest", "SpanWriteThrough")
		if err != nil {
			t.Fatalf("SpanWriteThrough() error = %v", err)
		}
		if got := out.Native().(int32); got != 600 {
			t.Errorf("SpanWriteThrough() = %d, want 600", got)
		}
	})
}

// TestInterfaceForeach exercises `foreach` over a collection accessed
// through an interface-typed reference (Fase 3.13), as opposed to
// TestForeach's concrete-type case (Fase 3.11): a List<int> assigned to
// an IEnumerable<int> local, and a compiler-generated `yield return`
// iterator whose own declared return type is the interface — both need
// the interpreter's runtime interface-call fallback (redirect a call
// site declared against IEnumerable`1/IEnumerator`1/IEnumerator to the
// receiver's actual concrete type, since vmnet has no vtable), not just
// isinst/castclass (Fase 3.8).
func TestInterfaceForeach(t *testing.T) {
	asm := loadFixture(t)

	t.Run("List<T> summed through IEnumerable<T>", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.InterfaceForeachTest", "SumViaInterface")
		if err != nil {
			t.Fatalf("SumViaInterface() error = %v", err)
		}
		if got := out.Native().(int32); got != 60 {
			t.Errorf("SumViaInterface() = %d, want 60", got)
		}
	})

	t.Run("yield-return iterator summed through IEnumerable<T>", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.InterfaceForeachTest", "SumCustomIterator")
		if err != nil {
			t.Fatalf("SumCustomIterator() error = %v", err)
		}
		if got := out.Native().(int32); got != 10 {
			t.Errorf("SumCustomIterator() = %d, want 10", got)
		}
	})
}

// TestCheapWins exercises the Fase 3.13 cheap-win BCL bundle: a set of
// high-breadth, low-effort String/Char/List/Dictionary natives found by
// the Fase 3.13 probe (same "measure, then bundle the cheap wins"
// pattern as Fase 3.6).
func TestCheapWins(t *testing.T) {
	asm := loadFixture(t)

	t.Run("String checks", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.CheapWins", "StringChecks", String("Hello World"))
		if err != nil {
			t.Fatalf("StringChecks() error = %v", err)
		}
		want := "0116;Hello Go;Hello World"
		if got := out.Native().(string); got != want {
			t.Errorf("StringChecks() = %q, want %q", got, want)
		}
	})

	t.Run("Split/Join", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.CheapWins", "SplitJoin", String("a,b,c"))
		if err != nil {
			t.Fatalf("SplitJoin() error = %v", err)
		}
		if got := out.Native().(string); got != "a|b|c" {
			t.Errorf("SplitJoin() = %q, want %q", got, "a|b|c")
		}
	})

	t.Run("Char checks", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.CheapWins", "CharChecks")
		if err != nil {
			t.Fatalf("CharChecks() error = %v", err)
		}
		if got := out.Native().(string); got != "111x" {
			t.Errorf("CharChecks() = %q, want %q", got, "111x")
		}
	})

	t.Run("Int32.ToString", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.CheapWins", "IntToString", Int32(42))
		if err != nil {
			t.Fatalf("IntToString(42) error = %v", err)
		}
		if got := out.Native().(string); got != "42" {
			t.Errorf("IntToString(42) = %q, want %q", got, "42")
		}
	})

	t.Run("List extras", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.CheapWins", "ListExtras")
		if err != nil {
			t.Fatalf("ListExtras() error = %v", err)
		}
		if got := out.Native().(int32); got != 331 {
			t.Errorf("ListExtras() = %d, want 331", got)
		}
	})

	t.Run("Dictionary.TryGetValue", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.CheapWins", "DictTryGetValue")
		if err != nil {
			t.Fatalf("DictTryGetValue() error = %v", err)
		}
		if got := out.Native().(int32); got != 200 {
			t.Errorf("DictTryGetValue() = %d, want 200", got)
		}
	})
}

// TestReflection exercises reflection-lite (Fase 3.14): `typeof(T)`
// (ldtoken on a type token + the identity-function GetTypeFromHandle,
// see ir.LoadTypeToken's doc comment), Object.GetType(), and
// System.Type equality/Name/FullName — confirmed against real IL that
// `x.GetType() == typeof(T)` is exactly `callvirt GetType` + `ldtoken T`
// + `call GetTypeFromHandle` + `call Type::op_Equality`.
func TestReflection(t *testing.T) {
	asm := loadFixture(t)

	t.Run("GetType() == typeof(T)", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.ReflectionTest", "TypeofEqualsGetType")
		if err != nil {
			t.Fatalf("TypeofEqualsGetType() error = %v", err)
		}
		if got := out.Native().(int32); got == 0 {
			t.Errorf("TypeofEqualsGetType() = %d, want nonzero", got)
		}
	})

	t.Run("GetType() does not match the base type", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.ReflectionTest", "GetTypeDoesNotMatchBaseType")
		if err != nil {
			t.Fatalf("GetTypeDoesNotMatchBaseType() error = %v", err)
		}
		if got := out.Native().(int32); got != 0 {
			t.Errorf("GetTypeDoesNotMatchBaseType() = %d, want 0", got)
		}
	})

	t.Run("Type.Name", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.ReflectionTest", "TypeName")
		if err != nil {
			t.Fatalf("TypeName() error = %v", err)
		}
		if got := out.Native().(string); got != "Car" {
			t.Errorf("TypeName() = %q, want %q", got, "Car")
		}
	})

	t.Run("Type.FullName", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.ReflectionTest", "TypeFullName")
		if err != nil {
			t.Fatalf("TypeFullName() error = %v", err)
		}
		if got := out.Native().(string); got != "Vmnet.Fixtures.Car" {
			t.Errorf("TypeFullName() = %q, want %q", got, "Vmnet.Fixtures.Car")
		}
	})

	t.Run("Type op_Inequality", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.ReflectionTest", "TypeNotEquals")
		if err != nil {
			t.Fatalf("TypeNotEquals() error = %v", err)
		}
		if got := out.Native().(int32); got == 0 {
			t.Errorf("TypeNotEquals() = %d, want nonzero", got)
		}
	})

	t.Run("Type.IsAssignableFrom", func(t *testing.T) {
		yes, err := asm.Call("Vmnet.Fixtures.ReflectionTest", "VehicleAssignableFromCar")
		if err != nil {
			t.Fatalf("VehicleAssignableFromCar() error = %v", err)
		}
		if got := yes.Native().(int32); got == 0 {
			t.Errorf("VehicleAssignableFromCar() = %d, want nonzero", got)
		}

		no, err := asm.Call("Vmnet.Fixtures.ReflectionTest", "CarNotAssignableFromVehicle")
		if err != nil {
			t.Fatalf("CarNotAssignableFromVehicle() error = %v", err)
		}
		if got := no.Native().(int32); got != 0 {
			t.Errorf("CarNotAssignableFromVehicle() = %d, want 0", got)
		}
	})
}

// TestLazy exercises System.Lazy<T> (Fase 3.17): a static field
// initialized from a Func<T> factory (compiled into the class's .cctor
// calling Lazy`1::.ctor — a plain bcl.NativeCtor, no Machine needed),
// and Value access, which does need Machine access (Fase 3.17's
// machineRegistry entry) to invoke the factory lazily on first access
// and cache it — verified by counting real factory invocations, not
// just checking the returned value happens to be consistent.
func TestLazy(t *testing.T) {
	asm := loadFixture(t)

	t.Run("factory invoked once, cached on second access", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.LazyTest", "ValueTwiceAndCallCount")
		if err != nil {
			t.Fatalf("ValueTwiceAndCallCount() error = %v", err)
		}
		if got := out.Native().(int32); got != 101001 {
			t.Errorf("ValueTwiceAndCallCount() = %d, want 101001", got)
		}
	})

	t.Run("IsValueCreated flips after the first access", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.LazyTest", "IsValueCreatedBeforeAndAfterAccess")
		if err != nil {
			t.Fatalf("IsValueCreatedBeforeAndAfterAccess() error = %v", err)
		}
		if got := out.Native().(int32); got == 0 {
			t.Errorf("IsValueCreatedBeforeAndAfterAccess() = %d, want nonzero", got)
		}
	})
}

// TestCheapWins2 exercises the Fase 3.18 cheap-win BCL bundle: String
// (Contains, the char[]-based .ctor), Environment.NewLine,
// Convert.ToInt32, Double.ToString, List (RemoveAt/Insert),
// Dictionary.Clear, FormatException, Interlocked.CompareExchange, and
// StringComparer.Ordinal.
func TestCheapWins2(t *testing.T) {
	asm := loadFixture(t)

	cases := []struct {
		method string
		args   []Value
		want   any
	}{
		{"ContainsTest", []Value{String("Hello")}, int32(1)},
		{"NewLine", nil, "\n"},
		{"ConvertToInt32Test", nil, int32(42)},
		{"DoubleToStringTest", []Value{Float64(3.14)}, "3.14"},
		{"StringFromChars", nil, "abc"},
		{"ListRemoveAtInsert", nil, int32(107)},
		{"DictClear", nil, int32(0)},
		{"FormatExceptionTest", nil, "bad format"},
		{"InterlockedTest", nil, int32(10)},
		{"StringComparerOrdinalTest", nil, int32(1)},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			out, err := asm.Call("Vmnet.Fixtures.CheapWins2", tc.method, tc.args...)
			if err != nil {
				t.Fatalf("%s() error = %v", tc.method, err)
			}
			if got := out.Native(); got != tc.want {
				t.Errorf("%s() = %#v, want %#v", tc.method, got, tc.want)
			}
		})
	}
}

// TestCollectionsExtra exercises HashSet<T>, Stack<T>, and TimeSpan
// (Fase 3.19).
func TestCollectionsExtra(t *testing.T) {
	asm := loadFixture(t)

	cases := []struct {
		method string
		want   int32
	}{
		{"HashSetTest", 31},
		{"StackTest", 302},
		{"TimeSpanFromSecondsTest", 130},
		{"TimeSpanCtorTest", 10203},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			out, err := asm.Call("Vmnet.Fixtures.CollectionsExtra", tc.method)
			if err != nil {
				t.Fatalf("%s() error = %v", tc.method, err)
			}
			if got := out.Native().(int32); got != tc.want {
				t.Errorf("%s() = %d, want %d", tc.method, got, tc.want)
			}
		})
	}
}

// TestRegex exercises System.Text.RegularExpressions (Fase 3.20):
// static and instance IsMatch/Match, and Match.Groups[i].Value — which
// confirmed a real hierarchy surprise against actual IL: .Success/.Value
// compile to Group::get_Success/Capture::get_Value even when called on a
// Match (Capture -> Group -> Match inherits both, overrides neither).
func TestRegex(t *testing.T) {
	asm := loadFixture(t)

	t.Run("static IsMatch", func(t *testing.T) {
		yes, err := asm.Call("Vmnet.Fixtures.RegexTest", "IsMatchTest", String("12345"))
		if err != nil {
			t.Fatalf("IsMatchTest(\"12345\") error = %v", err)
		}
		if got := yes.Native().(int32); got == 0 {
			t.Errorf("IsMatchTest(\"12345\") = %d, want nonzero", got)
		}

		no, err := asm.Call("Vmnet.Fixtures.RegexTest", "IsMatchTest", String("abc"))
		if err != nil {
			t.Fatalf("IsMatchTest(\"abc\") error = %v", err)
		}
		if got := no.Native().(int32); got != 0 {
			t.Errorf("IsMatchTest(\"abc\") = %d, want 0", got)
		}
	})

	t.Run("Match with capture groups", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.RegexTest", "MatchGroupTest", String("user@domain.com"))
		if err != nil {
			t.Fatalf("MatchGroupTest() error = %v", err)
		}
		if got := out.Native().(string); got != "user|domain" {
			t.Errorf("MatchGroupTest() = %q, want %q", got, "user|domain")
		}

		noMatch, err := asm.Call("Vmnet.Fixtures.RegexTest", "MatchGroupTest", String("no-at-sign"))
		if err != nil {
			t.Fatalf("MatchGroupTest(no match) error = %v", err)
		}
		if got := noMatch.Native().(string); got != "no-match" {
			t.Errorf("MatchGroupTest(no match) = %q, want %q", got, "no-match")
		}
	})

	t.Run("instance Regex.Match", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.RegexTest", "InstanceRegexTest", String("abc123def"))
		if err != nil {
			t.Fatalf("InstanceRegexTest() error = %v", err)
		}
		if got := out.Native().(string); got != "123" {
			t.Errorf("InstanceRegexTest() = %q, want %q", got, "123")
		}
	})
}

// TestLinq exercises System.Linq.Enumerable (Fase 3.15): a chained
// Where().Select().ToList() over a List<int>, Any/All predicates,
// FirstOrDefault, and Select/ToArray over an int[] source — the eager,
// Machine-aware LINQ registry (internal/interpreter/linq.go), not the
// CLR's real lazy iterators.
func TestLinq(t *testing.T) {
	asm := loadFixture(t)

	t.Run("Where().Select().ToList()", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.LinqTest", "SumOfEvenDoubled")
		if err != nil {
			t.Fatalf("SumOfEvenDoubled() error = %v", err)
		}
		if got := out.Native().(int32); got != 24 {
			t.Errorf("SumOfEvenDoubled() = %d, want 24", got)
		}
	})

	t.Run("Any", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.LinqTest", "AnyOver10")
		if err != nil {
			t.Fatalf("AnyOver10() error = %v", err)
		}
		if got := out.Native().(int32); got != 0 {
			t.Errorf("AnyOver10() = %d, want 0", got)
		}
	})

	t.Run("All", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.LinqTest", "AllPositive")
		if err != nil {
			t.Fatalf("AllPositive() error = %v", err)
		}
		if got := out.Native().(int32); got == 0 {
			t.Errorf("AllPositive() = %d, want nonzero", got)
		}
	})

	t.Run("FirstOrDefault", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.LinqTest", "FirstEven")
		if err != nil {
			t.Fatalf("FirstEven() error = %v", err)
		}
		if got := out.Native().(int32); got != 4 {
			t.Errorf("FirstEven() = %d, want 4", got)
		}
	})

	t.Run("Select/ToArray over an array source", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.LinqTest", "ArraySelectSum")
		if err != nil {
			t.Fatalf("ArraySelectSum() error = %v", err)
		}
		if got := out.Native().(int32); got != 12 {
			t.Errorf("ArraySelectSum() = %d, want 12", got)
		}
	})
}

// TestForeach exercises `foreach` over List<T>/Dictionary<K,V> (Fase
// 3.11): the compiler-generated struct enumerator (GetEnumerator/
// MoveNext/get_Current/Dispose, confirmed against real IL — List<T>'s
// Enumerator is a value type, so this also exercises struct receivers
// through a managed pointer once more), Dictionary iteration yielding
// KeyValuePair<K,V>, EqualityComparer<T>.Default reusing Fase 3.7's
// value/reference equality, Math.Min/Max, and String.Join over a List<T>
// argument (the IEnumerable<string> overload, not the array one).
func TestForeach(t *testing.T) {
	asm := loadFixture(t)

	t.Run("List<T>", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.ForeachTest", "SumList")
		if err != nil {
			t.Fatalf("SumList() error = %v", err)
		}
		if got := out.Native().(int32); got != 6 {
			t.Errorf("SumList() = %d, want 6", got)
		}
	})

	t.Run("Dictionary<K,V>", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.ForeachTest", "SumDictionaryValues")
		if err != nil {
			t.Fatalf("SumDictionaryValues() error = %v", err)
		}
		if got := out.Native().(int32); got != 60 {
			t.Errorf("SumDictionaryValues() = %d, want 60", got)
		}
	})

	t.Run("EqualityComparer<T>.Default", func(t *testing.T) {
		eq, err := asm.Call("Vmnet.Fixtures.ForeachTest", "EqualityComparerDefaultEquals", Int32(5), Int32(5))
		if err != nil {
			t.Fatalf("EqualityComparerDefaultEquals(5, 5) error = %v", err)
		}
		if got := eq.Native().(int32); got == 0 {
			t.Errorf("EqualityComparerDefaultEquals(5, 5) = %d, want nonzero", got)
		}

		neq, err := asm.Call("Vmnet.Fixtures.ForeachTest", "EqualityComparerDefaultEquals", Int32(5), Int32(6))
		if err != nil {
			t.Fatalf("EqualityComparerDefaultEquals(5, 6) error = %v", err)
		}
		if got := neq.Native().(int32); got != 0 {
			t.Errorf("EqualityComparerDefaultEquals(5, 6) = %d, want 0", got)
		}
	})

	t.Run("Math.Min/Max", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.ForeachTest", "MathMinMax", Int32(3), Int32(7))
		if err != nil {
			t.Fatalf("MathMinMax(3, 7) error = %v", err)
		}
		if got := out.Native().(int32); got != 307 {
			t.Errorf("MathMinMax(3, 7) = %d, want 307", got)
		}
	})

	t.Run("String.Join over List<T>", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.ForeachTest", "JoinStrings")
		if err != nil {
			t.Fatalf("JoinStrings() error = %v", err)
		}
		if got := out.Native().(string); got != "a,b,c" {
			t.Errorf("JoinStrings() = %q, want %q", got, "a,b,c")
		}
	})
}

// TestTryCatch exercises real try/catch/finally (Fase 3.10): catching by
// exact type and by base type (the real class hierarchy from Fase 3.8,
// not just an exact-match check), finally running on both the caught and
// uncaught exception paths (including nested try/finally where the inner
// finally runs before the exception reaches an outer catch), first-match-
// wins among multiple catch clauses, rethrow preserving the original
// exception (including its Message), and an exception with no matching
// catch propagating out of the method as a Go error.
func TestTryCatch(t *testing.T) {
	asm := loadFixture(t)

	t.Run("catch by exact type", func(t *testing.T) {
		noThrow, err := asm.Call("Vmnet.Fixtures.TryCatch", "CatchByType", Int32(0))
		if err != nil {
			t.Fatalf("CatchByType(false) error = %v", err)
		}
		if got := noThrow.Native().(int32); got != 1 {
			t.Errorf("CatchByType(false) = %d, want 1", got)
		}

		caught, err := asm.Call("Vmnet.Fixtures.TryCatch", "CatchByType", Int32(1))
		if err != nil {
			t.Fatalf("CatchByType(true) error = %v", err)
		}
		if got := caught.Native().(int32); got != 2 {
			t.Errorf("CatchByType(true) = %d, want 2", got)
		}
	})

	t.Run("catch by base type", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.TryCatch", "CatchByBaseType")
		if err != nil {
			t.Fatalf("CatchByBaseType() error = %v", err)
		}
		if got := out.Native().(int32); got != 42 {
			t.Errorf("CatchByBaseType() = %d, want 42", got)
		}
	})

	t.Run("finally always runs", func(t *testing.T) {
		noThrow, err := asm.Call("Vmnet.Fixtures.TryCatch", "FinallyAlwaysRuns", Int32(0))
		if err != nil {
			t.Fatalf("FinallyAlwaysRuns(false) error = %v", err)
		}
		if got := noThrow.Native().(string); got != "try;finally;" {
			t.Errorf("FinallyAlwaysRuns(false) = %q, want %q", got, "try;finally;")
		}

		threw, err := asm.Call("Vmnet.Fixtures.TryCatch", "FinallyAlwaysRuns", Int32(1))
		if err != nil {
			t.Fatalf("FinallyAlwaysRuns(true) error = %v", err)
		}
		if got := threw.Native().(string); got != "try;catch;finally;" {
			t.Errorf("FinallyAlwaysRuns(true) = %q, want %q", got, "try;catch;finally;")
		}
	})

	t.Run("finally runs on uncaught exception before an outer catch", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.TryCatch", "FinallyRunsOnUncaughtException")
		if err != nil {
			t.Fatalf("FinallyRunsOnUncaughtException() error = %v", err)
		}
		want := "inner-try;inner-finally;outer-catch;"
		if got := out.Native().(string); got != want {
			t.Errorf("FinallyRunsOnUncaughtException() = %q, want %q", got, want)
		}
	})

	t.Run("first matching catch wins", func(t *testing.T) {
		argNull, err := asm.Call("Vmnet.Fixtures.TryCatch", "FirstMatchingCatchWins", Int32(1))
		if err != nil {
			t.Fatalf("FirstMatchingCatchWins(true) error = %v", err)
		}
		if got := argNull.Native().(int32); got != 1 {
			t.Errorf("FirstMatchingCatchWins(true) = %d, want 1", got)
		}

		invalidOp, err := asm.Call("Vmnet.Fixtures.TryCatch", "FirstMatchingCatchWins", Int32(0))
		if err != nil {
			t.Fatalf("FirstMatchingCatchWins(false) error = %v", err)
		}
		if got := invalidOp.Native().(int32); got != 2 {
			t.Errorf("FirstMatchingCatchWins(false) = %d, want 2", got)
		}
	})

	t.Run("rethrow preserves the exception", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.TryCatch", "Rethrow")
		if err != nil {
			t.Fatalf("Rethrow() error = %v", err)
		}
		want := "inner-catch;outer-catch:original"
		if got := out.Native().(string); got != want {
			t.Errorf("Rethrow() = %q, want %q", got, want)
		}
	})

	t.Run("uncaught exception propagates", func(t *testing.T) {
		_, err := asm.Call("Vmnet.Fixtures.TryCatch", "UncaughtExceptionPropagates")
		if err == nil {
			t.Fatal("UncaughtExceptionPropagates() succeeded, want a propagated NotSupportedException")
		}
		var ex *runtime.ManagedException
		if !errors.As(err, &ex) || ex.TypeName != "System.NotSupportedException" {
			t.Errorf("UncaughtExceptionPropagates() error = %v, want a System.NotSupportedException", err)
		}
	})
}

// TestCustomException exercises a plugin-declared exception subclass
// (Fase 3.13): base-constructor chaining (`: base(message)`, a plain
// non-virtual `call System.Exception::.ctor`, not newobj), catching by
// the exact declared subtype, and catching by the real base type
// (System.Exception) — both need the thrown ManagedException's TypeName
// to be the actual most-derived plugin type, not the fixed BCL name
// registerCtor's exact-type path uses.
func TestCustomException(t *testing.T) {
	asm := loadFixture(t)

	t.Run("catch by exact subtype", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.CustomExceptionTest", "CatchExact")
		if err != nil {
			t.Fatalf("CatchExact() error = %v", err)
		}
		if got := out.Native().(string); got != "exact:custom-boom" {
			t.Errorf("CatchExact() = %q, want %q", got, "exact:custom-boom")
		}
	})

	t.Run("catch by real base type", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.CustomExceptionTest", "CatchBase")
		if err != nil {
			t.Fatalf("CatchBase() error = %v", err)
		}
		if got := out.Native().(string); got != "base:custom-boom-2" {
			t.Errorf("CatchBase() = %q, want %q", got, "base:custom-boom-2")
		}
	})
}

// TestDelegates exercises delegates/closures (Fase 3.9): a method-group
// conversion (static target, no receiver, cached by the compiler in a
// static field), a closure capturing a parameter, a closure capturing
// *and mutating* an outer local (the compiler rewrites the local into a
// shared display-class field — vmnet needs no special support beyond its
// existing object/field model for this to work), and a locally-declared
// `delegate` type (exercising isDelegateType's TypeDef path in the
// checker, not just the well-known-BCL-prefix one).
func TestDelegates(t *testing.T) {
	asm := loadFixture(t)

	tests := []struct {
		method string
		args   []Value
		want   int32
	}{
		{"InvokeStaticFunc", []Value{Int32(5)}, 10},
		{"InvokeClosure", []Value{Int32(3), Int32(10)}, 30},
		{"InvokeMutatingClosure", []Value{Int32(7)}, 8},
		{"InvokeLocalDelegateType", []Value{Int32(5)}, 10},
	}
	for _, tt := range tests {
		out, err := asm.Call("Vmnet.Fixtures.Delegates", tt.method, tt.args...)
		if err != nil {
			t.Fatalf("%s(...) error = %v", tt.method, err)
		}
		if got := out.Native().(int32); got != tt.want {
			t.Errorf("%s(...) = %d, want %d", tt.method, got, tt.want)
		}
	}
}

// TestTypeChecks exercises isinst/castclass (Fase 3.8) against a real
// class/interface hierarchy: `is`/`as`/explicit cast on a base-typed
// reference actually holding a subtype, a failed cast throwing
// InvalidCastException instead of silently succeeding or panicking, and
// isinst against the hand-maintained exception hierarchy
// (internal/interpreter/typecheck.go) without needing try/catch (not
// implemented until Fase 3.10) to construct the exception.
func TestTypeChecks(t *testing.T) {
	asm := loadFixture(t)

	t.Run("is", func(t *testing.T) {
		tests := []struct {
			dog      int32
			wantIs   int32
			wantIShp int32
		}{{1, 1, 1}, {0, 0, 0}}
		for _, tt := range tests {
			isDog, err := asm.Call("Vmnet.Fixtures.TypeChecks", "IsDog", Int32(tt.dog))
			if err != nil {
				t.Fatalf("IsDog(%d) error = %v", tt.dog, err)
			}
			if got := isDog.Native().(int32); (got != 0) != (tt.wantIs != 0) {
				t.Errorf("IsDog(%d) = %d, want nonzero=%v", tt.dog, got, tt.wantIs != 0)
			}

			isShape, err := asm.Call("Vmnet.Fixtures.TypeChecks", "ImplementsIShape", Int32(tt.dog))
			if err != nil {
				t.Fatalf("ImplementsIShape(%d) error = %v", tt.dog, err)
			}
			if got := isShape.Native().(int32); (got != 0) != (tt.wantIShp != 0) {
				t.Errorf("ImplementsIShape(%d) = %d, want nonzero=%v", tt.dog, got, tt.wantIShp != 0)
			}
		}
	})

	t.Run("as", func(t *testing.T) {
		succeeds, err := asm.Call("Vmnet.Fixtures.TypeChecks", "AsDogSucceeds", Int32(1))
		if err != nil {
			t.Fatalf("AsDogSucceeds(dog) error = %v", err)
		}
		if got := succeeds.Native().(int32); got == 0 {
			t.Errorf("AsDogSucceeds(dog) = %d, want nonzero", got)
		}

		fails, err := asm.Call("Vmnet.Fixtures.TypeChecks", "AsDogSucceeds", Int32(0))
		if err != nil {
			t.Fatalf("AsDogSucceeds(cat) error = %v", err)
		}
		if got := fails.Native().(int32); got != 0 {
			t.Errorf("AsDogSucceeds(cat) = %d, want 0", got)
		}
	})

	t.Run("castclass success", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.TypeChecks", "CastToDogName", Int32(1))
		if err != nil {
			t.Fatalf("CastToDogName(dog) error = %v", err)
		}
		if got := out.Native().(string); got != "Rex" {
			t.Errorf("CastToDogName(dog) = %q, want %q", got, "Rex")
		}
	})

	t.Run("castclass failure throws InvalidCastException", func(t *testing.T) {
		_, err := asm.Call("Vmnet.Fixtures.TypeChecks", "CastToDogName", Int32(0))
		if err == nil {
			t.Fatal("CastToDogName(cat) succeeded, want InvalidCastException")
		}
		var ex *runtime.ManagedException
		if !errors.As(err, &ex) || ex.TypeName != "System.InvalidCastException" {
			t.Errorf("CastToDogName(cat) error = %v, want a System.InvalidCastException", err)
		}
	})

	t.Run("isinst against the exception hierarchy", func(t *testing.T) {
		isArgEx, err := asm.Call("Vmnet.Fixtures.TypeChecks", "ArgNullIsArgException")
		if err != nil {
			t.Fatalf("ArgNullIsArgException() error = %v", err)
		}
		if got := isArgEx.Native().(int32); got == 0 {
			t.Errorf("ArgNullIsArgException() = %d, want nonzero (ArgumentNullException derives from ArgumentException)", got)
		}

		isInvalidOp, err := asm.Call("Vmnet.Fixtures.TypeChecks", "ArgNullIsInvalidOp")
		if err != nil {
			t.Fatalf("ArgNullIsInvalidOp() error = %v", err)
		}
		if got := isInvalidOp.Native().(int32); got != 0 {
			t.Errorf("ArgNullIsInvalidOp() = %d, want 0 (unrelated exception branches)", got)
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

// TestCheapWins3 exercises the Fase 3.21 cheap-win BCL bundle:
// NotImplementedException, Double.IsInfinity family, String.EndsWith,
// Math.Floor, List.Clear, Int32.Parse/TryParse/CompareTo,
// Dictionary.Remove, DateTime.Kind, KeyValuePair<K,V> direct-local
// construction, and IList<T>/IReadOnlyCollection<T> interface dispatch.
func TestCheapWins3(t *testing.T) {
	asm := loadFixture(t)

	inf := Float64(math.Inf(1))
	neginf := Float64(math.Inf(-1))
	finite := Float64(3.5)

	cases := []struct {
		method string
		args   []Value
		want   any
	}{
		{"NotImplTest", nil, "nope"},
		{"InfinityTest", []Value{inf}, int32(1)},
		{"InfinityTest", []Value{finite}, int32(0)},
		{"PosInfTest", []Value{inf}, int32(1)},
		{"NegInfTest", []Value{neginf}, int32(1)},
		{"EndsWithTest", []Value{String("Hello")}, int32(1)},
		{"FloorTest", []Value{Float64(3.7)}, float64(3)},
		{"ListClearTest", nil, int32(0)},
		{"ParseTest", nil, int32(42)},
		{"TryParseTest", []Value{String("99")}, int32(99)},
		{"TryParseTest", []Value{String("nope")}, int32(-1)},
		{"CompareToTest", []Value{Int32(5), Int32(10)}, int32(-1)},
		{"DictRemoveTest", nil, int32(1)},
		{"DateTimeKindTest", nil, int32(1)},
		{"KeyValuePairCtorTest", nil, "k=42"},
		{"InterfaceCollectionTest", nil, int32(23)},
	}
	for i, tc := range cases {
		t.Run(fmt.Sprintf("%s#%d", tc.method, i), func(t *testing.T) {
			out, err := asm.Call("Vmnet.Fixtures.CheapWins3", tc.method, tc.args...)
			if err != nil {
				t.Fatalf("%s() error = %v", tc.method, err)
			}
			if got := out.Native(); got != tc.want {
				t.Errorf("%s() = %#v, want %#v", tc.method, got, tc.want)
			}
		})
	}
}

// TestAsync exercises async/await (Fase 3.22): a real compiler-generated
// state machine run synchronously to completion via
// AsyncTaskMethodBuilder.Start — including two sequential awaits, an
// exception thrown after an await propagating out through
// GetAwaiter().GetResult() to a synchronous catch, a void async method,
// and awaiting another async method's own Task (nested async chains).
func TestAsync(t *testing.T) {
	asm := loadFixture(t)

	t.Run("two sequential awaits", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.AsyncTest", "RunSync")
		if err != nil {
			t.Fatalf("RunSync() error = %v", err)
		}
		if got := out.Native().(int32); got != 30 {
			t.Errorf("RunSync() = %d, want 30", got)
		}
	})

	t.Run("exception after an await propagates to a sync catch", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.AsyncTest", "RunThrowing")
		if err != nil {
			t.Fatalf("RunThrowing() error = %v", err)
		}
		if got := out.Native().(string); got != "caught:boom" {
			t.Errorf("RunThrowing() = %q, want %q", got, "caught:boom")
		}
	})

	t.Run("void async method", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.AsyncTest", "RunVoid")
		if err != nil {
			t.Fatalf("RunVoid() error = %v", err)
		}
		if got := out.Native().(int32); got != 42 {
			t.Errorf("RunVoid() = %d, want 42", got)
		}
	})

	t.Run("nested async call chain", func(t *testing.T) {
		out, err := asm.Call("Vmnet.Fixtures.AsyncTest", "RunNested")
		if err != nil {
			t.Fatalf("RunNested() error = %v", err)
		}
		if got := out.Native().(int32); got != 60 {
			t.Errorf("RunNested() = %d, want 60", got)
		}
	})
}
