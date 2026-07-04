package bcl

import "github.com/arturoeanton/go-vmnet/internal/runtime"

// System.Threading.SpinLock backs a real lightweight mutual-exclusion
// struct — vmnet has no real concurrency within a single call chain (a
// Machine is never shared across goroutines), so Enter/TryEnter always
// succeed immediately and Exit is a no-op, the same "no real concurrency
// primitive needed" posture as System.Threading.Volatile (Fase 3.40,
// found via System.Text.Json's own internal metadata cache using a
// SpinLock for thread-safe lazy initialization).
var spinLockType = runtime.NewValueType("System.Threading", "SpinLock", nil, nil)

func init() {
	registerValueType(spinLockType)
	registerValueTypeCtor("System.Threading.SpinLock", spinLockCtor)
	register("System.Threading.SpinLock::.ctor", false, spinLockCtorInPlace)
	register("System.Threading.SpinLock::Enter", false, spinLockEnter)
	register("System.Threading.SpinLock::TryEnter", false, spinLockEnter)
	register("System.Threading.SpinLock::Exit", false, spinLockNoop)
	register("System.Threading.SpinLock::get_IsHeld", true, spinLockFalse)
	register("System.Threading.SpinLock::get_IsHeldByCurrentThread", true, spinLockFalse)
}

func spinLockCtor(args []runtime.Value) (*runtime.Struct, error) {
	return runtime.NewStruct(spinLockType), nil
}

func spinLockCtorInPlace(args []runtime.Value) (runtime.Value, error) {
	return runtime.Value{}, nil
}

// spinLockEnter backs both Enter(ref bool lockTaken) and TryEnter(ref
// bool lockTaken)/TryEnter(int, ref bool) — lockTaken is always the LAST
// argument across every real overload, and always set true since this
// never actually contends.
func spinLockEnter(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 {
		return runtime.Value{}, nil
	}
	last := args[len(args)-1]
	if last.Kind == runtime.KindRef && last.Ref != nil {
		*last.Ref = runtime.Bool(true)
	}
	return runtime.Value{}, nil
}

func spinLockNoop(args []runtime.Value) (runtime.Value, error) {
	return runtime.Value{}, nil
}

func spinLockFalse(args []runtime.Value) (runtime.Value, error) {
	return runtime.Bool(false), nil
}
