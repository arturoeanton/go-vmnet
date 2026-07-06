package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// System.Threading.CancellationToken/CancellationTokenSource — found via
// the same corpus-wide static-checker scan as this package's other new
// files, across 7 of the 19 tracked packages (Polly, MediatR, CsvHelper,
// FluentValidation, Dapper, ...): 36+20+21+11 real call sites, mostly
// ThrowIfCancellationRequested/get_Token/get_IsCancellationRequested/
// Cancel/CreateLinkedTokenSource — confirmed by disassembling Polly.dll's
// real Polly.Timeout.TimeoutEngine.Implementation and CircuitBreakerEngine
// .Implementation, which both guard their retry/circuit logic with
// `cancellationToken.ThrowIfCancellationRequested()`.
//
// This project already models every `async`/`await` SYNCHRONOUSLY —
// internal/interpreter/async.go's own doc comment: a single MoveNext()
// call always runs a whole async method straight through to completion,
// since every awaiter here is already-completed and no real concurrency
// (a second thread, a timer callback firing later, a user pressing
// Ctrl+C mid-await) ever exists to cancel anything WHILE some other code
// is suspended waiting. That's the actual scenario CancellationToken
// exists to solve in real .NET — cooperative cancellation of work that
// might otherwise run for an unbounded time on another thread/callback
// — and it provably cannot arise here: nothing in vmnet ever runs two
// call chains concurrently, so no token can ever transition from
// "not yet cancelled" to "cancelled" out from under code that already
// checked it. What CAN happen, and IS modeled honestly rather than
// stubbed out, is a caller explicitly calling Cancel() first and then
// checking/throwing afterward in plain, sequential code (a common real
// pattern in tests and pre-cancelled fast paths) — cancelState below is
// a real, if trivial, mutable flag for exactly that, not a permanently-
// false sentinel that would silently give the wrong answer for it.
//
// Deliberately NOT modeled: CancellationTokenSource.CancelAfter's real
// delayed-cancellation-via-timer behavior (there is no timer thread here
// to ever fire it — a genuine no-op, not a cut corner, symmetric with
// SpinLock's own "no real concurrency primitive needed" posture,
// system_spinlock.go) and CancellationToken.Register's real callback
// invocation (no real corpus call site's demo exercises a callback firing
// at all; Register returns a working CancellationTokenRegistration whose
// Dispose/Unregister are honest no-ops, but the callback itself is simply
// never invoked — a narrower, clearly-documented scope cut rather than
// silently-wrong behavior, since nothing here ever fires it "late" the
// way real code relies on).
type cancelState struct {
	cancelled bool
	// linked holds any upstream sources this one was derived FROM
	// (CreateLinkedTokenSource) — cancelling one of those must be
	// visible here too (real linked-token-source semantics), but
	// cancelling THIS source must never propagate backward to them, so
	// Cancel() below only ever sets this state's own `cancelled` field.
	linked []*cancelState
}

func (s *cancelState) isRequested() bool {
	if s == nil {
		return false
	}
	if s.cancelled {
		return true
	}
	for _, l := range s.linked {
		if l.isRequested() {
			return true
		}
	}
	return false
}

// cancellationTokenType's one field holds the *cancelState this token
// reads through, wrapped in a plain *runtime.Object (Value has no "raw Go
// pointer" kind of its own) — nil/Null for CancellationToken.None/
// default(CancellationToken), which per real .NET semantics can never be
// cancelled. Value.Clone() (runtime/struct.go) only deep-copies KindStruct
// fields, so copying a token (by value, into a local/argument/field —
// ordinary struct copy semantics) still shares the SAME underlying
// *cancelState — exactly like real CancellationToken's own "value-type
// handle onto shared source state" semantics.
var cancellationTokenType = runtime.NewValueType("System.Threading", "CancellationToken", []string{"state"}, []runtime.Value{runtime.Null()})

// cancellationTokenRegistrationType backs Register's return value — no
// fields needed at all, since Dispose/Unregister are both honest no-ops
// (see this file's own doc comment for why the callback itself is never
// invoked either).
var cancellationTokenRegistrationType = runtime.NewValueType("System.Threading", "CancellationTokenRegistration", nil, nil)

func init() {
	registerCtor("System.Threading.CancellationTokenSource", cancellationTokenSourceCtor)
	register("System.Threading.CancellationTokenSource::get_Token", true, cancellationTokenSourceGetToken)
	register("System.Threading.CancellationTokenSource::get_IsCancellationRequested", true, cancellationTokenSourceIsCancellationRequested)
	// Cancel() and Cancel(bool throwOnFirstException) both just flip the
	// flag — the bool-arg overload's real distinction (whether a
	// registered callback's own exception aborts the remaining callbacks)
	// only matters for Register's real callback-invocation behavior,
	// which this file's own doc comment already explains is out of scope.
	register("System.Threading.CancellationTokenSource::Cancel", false, cancellationTokenSourceCancel)
	register("System.Threading.CancellationTokenSource::CancelAfter", false, cancellationTokenNoop)
	register("System.Threading.CancellationTokenSource::Dispose", false, cancellationTokenNoop)
	register("System.Threading.CancellationTokenSource::CreateLinkedTokenSource", true, cancellationTokenSourceCreateLinkedTokenSource)

	registerValueType(cancellationTokenType)
	registerValueTypeCtor("System.Threading.CancellationToken", cancellationTokenCtor)
	// `var token = new CancellationToken(...);` assigned straight to a
	// local compiles to `ldloca`+`call instance .ctor`, not `newobj` —
	// same real gap system_collections.go's own KeyValuePair`2::.ctor
	// registration already documents and fixes for that type; found
	// auditing every registerValueTypeCtor entry for a missing in-place
	// counterpart (Fase 3.74).
	register("System.Threading.CancellationToken::.ctor", false, cancellationTokenCtorInPlace)
	register("System.Threading.CancellationToken::get_None", true, cancellationTokenNone)
	register("System.Threading.CancellationToken::get_IsCancellationRequested", true, cancellationTokenIsCancellationRequested)
	// CanBeCanceled: real semantics is "could this token EVER transition
	// to cancelled" — false only for a token with no real source behind
	// it at all (None/default), matching IsCancellationRequested's own
	// nil-state check below.
	register("System.Threading.CancellationToken::get_CanBeCanceled", true, cancellationTokenCanBeCanceled)
	register("System.Threading.CancellationToken::ThrowIfCancellationRequested", false, cancellationTokenThrowIfCancellationRequested)
	register("System.Threading.CancellationToken::Equals", true, cancellationTokenEquals)
	register("System.Threading.CancellationToken::op_Equality", true, cancellationTokenEquals)
	register("System.Threading.CancellationToken::op_Inequality", true, cancellationTokenNotEquals)
	register("System.Threading.CancellationToken::GetHashCode", true, cancellationTokenGetHashCode)
	register("System.Threading.CancellationToken::Register", true, cancellationTokenRegister)

	registerValueType(cancellationTokenRegistrationType)
	register("System.Threading.CancellationTokenRegistration::Dispose", false, cancellationTokenNoop)
	register("System.Threading.CancellationTokenRegistration::Unregister", true, cancellationTokenRegistrationUnregister)
}

func cancellationTokenSourceCtor(args []runtime.Value) (*runtime.Object, error) {
	// The (TimeSpan)/(int millisecondsDelay) overloads schedule a real
	// delayed self-Cancel() — no timer thread exists here to honor that
	// (CancelAfter's own doc comment above), so both degrade to the same
	// plain, never-yet-cancelled state as the parameterless overload.
	return &runtime.Object{Native: &cancelState{}}, nil
}

func cancelStateValue(s *cancelState) runtime.Value {
	if s == nil {
		return runtime.Null()
	}
	return runtime.ObjRef(&runtime.Object{Native: s})
}

func cancelStateOf(v runtime.Value) *cancelState {
	v = derefReceiver(v)
	if v.Kind != runtime.KindObject || v.Obj == nil {
		return nil
	}
	s, _ := v.Obj.Native.(*cancelState)
	return s
}

// tokenState reads a CancellationToken struct's own backing state,
// whether it arrives as the plain KindStruct value (an ordinary argument)
// or a managed pointer to one (a `this` receiver, or a `ref`/`in`
// parameter — every real ThrowIfCancellationRequested/Equals overload
// vmnet's calling convention could produce).
func tokenState(v runtime.Value) *cancelState {
	v = derefReceiver(v)
	if v.Kind != runtime.KindStruct || v.Struct == nil || len(v.Struct.Fields) == 0 {
		return nil
	}
	return cancelStateOf(v.Struct.Fields[0])
}

func cancellationTokenSourceGetToken(args []runtime.Value) (runtime.Value, error) {
	s := cancelStateOf(firstArg(args))
	tok := runtime.NewStruct(cancellationTokenType)
	tok.Fields[0] = cancelStateValue(s)
	return runtime.StructVal(tok), nil
}

func cancellationTokenSourceIsCancellationRequested(args []runtime.Value) (runtime.Value, error) {
	return runtime.Bool(cancelStateOf(firstArg(args)).isRequested()), nil
}

func cancellationTokenSourceCancel(args []runtime.Value) (runtime.Value, error) {
	if s := cancelStateOf(firstArg(args)); s != nil {
		s.cancelled = true
	}
	return runtime.Value{}, nil
}

// cancellationTokenSourceCreateLinkedTokenSource covers both the real
// two-token overload (CreateLinkedTokenSource(CancellationToken,
// CancellationToken)) and the `params CancellationToken[]` one — the
// compiler collapses the latter into a real array argument by the call
// site, same as every other params-array native in this package (see
// aggregateExceptionCtor's own doc comment, system_exception.go, for the
// identical pattern). A static method: args holds only the tokens
// themselves, no receiver.
func cancellationTokenSourceCreateLinkedTokenSource(args []runtime.Value) (runtime.Value, error) {
	var linked []*cancelState
	collect := func(v runtime.Value) {
		if s := tokenState(v); s != nil {
			linked = append(linked, s)
		}
	}
	for _, a := range args {
		if a.Kind == runtime.KindArray {
			if a.Arr != nil {
				for _, e := range a.Arr.Elems {
					collect(e)
				}
			}
			continue
		}
		collect(a)
	}
	return runtime.ObjRef(&runtime.Object{Native: &cancelState{linked: linked}}), nil
}

// cancellationTokenCtor backs the real public CancellationToken(bool
// canceled) constructor overload — no real corpus call site found using
// it, but cheap to support correctly rather than silently ignoring the
// argument.
func cancellationTokenCtor(args []runtime.Value) (*runtime.Struct, error) {
	s := runtime.NewStruct(cancellationTokenType)
	if len(args) > 0 && args[0].Truthy() {
		s.Fields[0] = cancelStateValue(&cancelState{cancelled: true})
	}
	return s, nil
}

// cancellationTokenCtorInPlace mirrors cancellationTokenCtor for the
// ldloca+call.ctor shape — args[0] is a KindRef to the already-allocated
// struct slot.
func cancellationTokenCtorInPlace(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindRef || args[0].Ref == nil {
		return runtime.Value{}, fmt.Errorf("bcl: CancellationToken constructor called without a receiver")
	}
	s, err := cancellationTokenCtor(args[1:])
	if err != nil {
		return runtime.Value{}, err
	}
	*args[0].Ref = runtime.StructVal(s)
	return runtime.Value{}, nil
}

func cancellationTokenNone(args []runtime.Value) (runtime.Value, error) {
	return runtime.StructVal(runtime.NewStruct(cancellationTokenType)), nil
}

func cancellationTokenIsCancellationRequested(args []runtime.Value) (runtime.Value, error) {
	return runtime.Bool(tokenState(firstArg(args)).isRequested()), nil
}

func cancellationTokenCanBeCanceled(args []runtime.Value) (runtime.Value, error) {
	return runtime.Bool(tokenState(firstArg(args)) != nil), nil
}

// cancellationTokenThrowIfCancellationRequested is the one real, honest
// behavior this whole file exists for: if (and only if) some source this
// token derives from was genuinely Cancel()'d — via plain, sequential
// code, the only way that can ever happen here (this file's own doc
// comment) — this throws exactly like the real BCL method does. Every
// other call site, in every one of the 8 demos and every real corpus
// package scanned, never actually cancels anything, so this never fires
// in practice — but it is a real check, not a permanently-disarmed one.
func cancellationTokenThrowIfCancellationRequested(args []runtime.Value) (runtime.Value, error) {
	if tokenState(firstArg(args)).isRequested() {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.OperationCanceledException", Message: "The operation was canceled."}
	}
	return runtime.Value{}, nil
}

// cancellationTokenEquals covers both Equals(CancellationToken) and the
// static op_Equality(CancellationToken, CancellationToken) — real
// CancellationToken equality compares the underlying source reference
// (both None/default counts as "the same," i.e. nil == nil), not any
// per-instance identity, so comparing the two *cancelState pointers
// directly (nil included) matches real behavior exactly.
func cancellationTokenEquals(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Bool(false), nil
	}
	return runtime.Bool(tokenState(args[0]) == tokenState(args[1])), nil
}

func cancellationTokenNotEquals(args []runtime.Value) (runtime.Value, error) {
	v, err := cancellationTokenEquals(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(!v.Truthy()), nil
}

// cancellationTokenGetHashCode is a fixed constant for every token,
// cancelled or not, linked or not — no real corpus caller found hashes a
// CancellationToken at all (Equals/op_Equality above, which every real
// comparison here actually relies on, are already correct); a real,
// distinct hash per *cancelState would need a pointer-to-int conversion
// that's simply not worth adding for a property nothing here reads.
func cancellationTokenGetHashCode(args []runtime.Value) (runtime.Value, error) {
	return runtime.Int32(0), nil
}

func cancellationTokenRegister(args []runtime.Value) (runtime.Value, error) {
	// The callback argument (args[1], a real Action/Action<object>
	// delegate) is deliberately never invoked — see this file's own doc
	// comment for why. Returning a working, disposable
	// CancellationTokenRegistration (rather than erroring) is what lets
	// real code's own `using var registration = token.Register(...);`
	// cleanup pattern keep working exactly as it does today.
	return runtime.StructVal(runtime.NewStruct(cancellationTokenRegistrationType)), nil
}

func cancellationTokenRegistrationUnregister(args []runtime.Value) (runtime.Value, error) {
	return runtime.Bool(true), nil
}

func cancellationTokenNoop(args []runtime.Value) (runtime.Value, error) {
	return runtime.Value{}, nil
}
