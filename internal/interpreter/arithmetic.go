package interpreter

import (
	"fmt"
	"math"

	"github.com/arturoeanton/go-vmnet/internal/ir"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

func evalBinOp(in ir.BinOp, a, b runtime.Value) (runtime.Value, error) {
	if isReferenceShaped(a) && isReferenceShaped(b) {
		switch in.Op {
		case ir.OpCeq:
			return runtime.Bool(refEqual(a, b)), nil
		case ir.OpCgt:
			return runtime.Bool(refGreater(a, b)), nil
		case ir.OpClt:
			return runtime.Bool(refGreater(b, a)), nil
		}
	}
	// `box !T` followed immediately by a null check (`cgt.un x, null` /
	// `ceq x, null`) is real C#'s own compiler-emitted idiom for "is this
	// generic-parameter-typed value non-null" inside a method whose T
	// isn't known to be a reference or value type at compile time — real
	// .NET's own box on a genuine value type (int, double, a struct)
	// NEVER produces null, so the check always has one fixed answer
	// regardless of the actual value. vmnet's own `box` on a primitive
	// Kind (I4/I8/R4/R8) is a pure identity passthrough (it never becomes
	// a KindObject wrapper at all — same "boxing a primitive doesn't
	// change its Kind" posture ir/builder.go's own box/unbox.any handling
	// already documents), so by the time this binary op runs, one side is
	// still a bare primitive Kind and the other is a real KindNull —
	// exactly the shape isReferenceShaped's own "both reference-shaped"
	// check above can never satisfy. Found via FluentValidation's own
	// AbstractComparisonValidator<T,TProperty>.GetComparisonValue,
	// checking `box(valueToCompare) > null` (real IL: `box !TProperty` /
	// `ldnull` / `cgt.un`) to build its own HasValue-style ValueTuple.
	if aPrim, bNull := isPrimitiveValueKind(a.Kind), b.Kind == runtime.KindNull; aPrim && bNull {
		switch in.Op {
		case ir.OpCeq:
			return runtime.Bool(false), nil
		case ir.OpCgt:
			return runtime.Bool(true), nil
		case ir.OpClt:
			return runtime.Bool(false), nil
		}
	}
	if bPrim, aNull := isPrimitiveValueKind(b.Kind), a.Kind == runtime.KindNull; bPrim && aNull {
		switch in.Op {
		case ir.OpCeq:
			return runtime.Bool(false), nil
		case ir.OpCgt:
			return runtime.Bool(false), nil
		case ir.OpClt:
			return runtime.Bool(true), nil
		}
	}
	// Shift operations are the one binary numeric op ECMA-335 (III.1.5,
	// Table 2 "Shift Operations") allows a genuine width mismatch for:
	// the shift amount is always int32 regardless of the shifted
	// value's own width, and the compiler emits no widening `conv.i8`
	// on it — unlike every other binary numeric operator, which does
	// require same-width operands. Found via a real case: NPOI's own
	// POIFS block-offset arithmetic shifts an int64 by a plain int
	// bit-count with no conversion in between.
	if (in.Op == ir.OpShl || in.Op == ir.OpShr) && a.Kind != b.Kind && b.Kind == runtime.KindI4 {
		switch a.Kind {
		case runtime.KindI4:
			if in.Unsigned {
				return evalBinOpUint(in.Op, uint32(a.I4), uint32(b.I4), func(v uint32) runtime.Value { return runtime.Int32(int32(v)) })
			}
			return evalBinOpInt(in.Op, a.I4, b.I4, runtime.Int32)
		case runtime.KindI8:
			if in.Unsigned {
				return evalBinOpUint(in.Op, uint64(a.I8), uint64(b.I4), func(v uint64) runtime.Value { return runtime.Int64(int64(v)) })
			}
			return evalBinOpInt(in.Op, a.I8, int64(b.I4), runtime.Int64)
		}
	}
	if a.Kind != b.Kind {
		return runtime.Value{}, fmt.Errorf("interpreter: binary op on mismatched value kinds (%d, %d)", a.Kind, b.Kind)
	}
	switch a.Kind {
	case runtime.KindI4:
		if in.Unsigned {
			return evalBinOpUint(in.Op, uint32(a.I4), uint32(b.I4), func(v uint32) runtime.Value { return runtime.Int32(int32(v)) })
		}
		return evalBinOpInt(in.Op, a.I4, b.I4, runtime.Int32)
	case runtime.KindI8:
		if in.Unsigned {
			return evalBinOpUint(in.Op, uint64(a.I8), uint64(b.I8), func(v uint64) runtime.Value { return runtime.Int64(int64(v)) })
		}
		return evalBinOpInt(in.Op, a.I8, b.I8, runtime.Int64)
	case runtime.KindR4:
		return evalBinOpFloat(in.Op, a.R4, b.R4, runtime.Float32)
	case runtime.KindR8:
		return evalBinOpFloat(in.Op, a.R8, b.R8, runtime.Float64)
	default:
		return runtime.Value{}, fmt.Errorf("interpreter: binary op on unsupported value kind %d", a.Kind)
	}
}

func evalBinOpInt[T int32 | int64](op ir.BinOpKind, a, b T, wrap func(T) runtime.Value) (runtime.Value, error) {
	switch op {
	case ir.OpAdd:
		return wrap(a + b), nil
	case ir.OpSub:
		return wrap(a - b), nil
	case ir.OpMul:
		return wrap(a * b), nil
	case ir.OpDiv:
		if b == 0 {
			return runtime.Value{}, fmt.Errorf("interpreter: integer divide by zero")
		}
		return wrap(a / b), nil
	case ir.OpRem:
		if b == 0 {
			return runtime.Value{}, fmt.Errorf("interpreter: integer divide by zero")
		}
		return wrap(a % b), nil
	case ir.OpAnd:
		return wrap(a & b), nil
	case ir.OpOr:
		return wrap(a | b), nil
	case ir.OpXor:
		return wrap(a ^ b), nil
	case ir.OpShl:
		return wrap(a << uint(b)), nil
	case ir.OpShr:
		return wrap(a >> uint(b)), nil // arithmetic (sign-extending) shift, correct for signed T
	case ir.OpCeq:
		return runtime.Bool(a == b), nil
	case ir.OpCgt:
		return runtime.Bool(a > b), nil
	case ir.OpClt:
		return runtime.Bool(a < b), nil
	default:
		return runtime.Value{}, fmt.Errorf("interpreter: unsupported integer binary op %d", op)
	}
}

// evalBinOpUint backs the .un opcodes (div.un/rem.un/shr.un/cgt.un/clt.un):
// the same bit pattern as evalBinOpInt's T, but compared/divided/shifted
// as unsigned. This matters for real code, not just edge cases — e.g. the
// extremely common range-check idiom `(uint)(c - low) <= high` relies on
// unsigned wraparound turning "c < low" into a huge value that fails the
// `<=`, and gets a silently wrong answer under signed comparison.
func evalBinOpUint[T uint32 | uint64](op ir.BinOpKind, a, b T, wrap func(T) runtime.Value) (runtime.Value, error) {
	switch op {
	case ir.OpDiv:
		if b == 0 {
			return runtime.Value{}, fmt.Errorf("interpreter: integer divide by zero")
		}
		return wrap(a / b), nil
	case ir.OpRem:
		if b == 0 {
			return runtime.Value{}, fmt.Errorf("interpreter: integer divide by zero")
		}
		return wrap(a % b), nil
	case ir.OpShr:
		return wrap(a >> uint(b)), nil // logical (zero-fill) shift
	case ir.OpCgt:
		return runtime.Bool(a > b), nil
	case ir.OpClt:
		return runtime.Bool(a < b), nil
	default:
		return runtime.Value{}, fmt.Errorf("interpreter: unsupported unsigned integer binary op %d", op)
	}
}

func evalBinOpFloat[T float32 | float64](op ir.BinOpKind, a, b T, wrap func(T) runtime.Value) (runtime.Value, error) {
	switch op {
	case ir.OpAdd:
		return wrap(a + b), nil
	case ir.OpSub:
		return wrap(a - b), nil
	case ir.OpMul:
		return wrap(a * b), nil
	case ir.OpDiv:
		return wrap(a / b), nil
	case ir.OpRem:
		// CIL `rem` on floats is IEEE 754 fmod (spec §III.3.55) — same
		// sign as the dividend, unlike Math.IEEERemainder. Go's %
		// doesn't apply to floats, so math.Mod (float64-only) does the
		// work; float32 round-trips through it since T is constrained to
		// float32|float64 and math.Mod's inputs/output are float64.
		return wrap(T(math.Mod(float64(a), float64(b)))), nil
	case ir.OpCeq:
		return runtime.Bool(a == b), nil
	case ir.OpCgt:
		return runtime.Bool(a > b), nil
	case ir.OpClt:
		return runtime.Bool(a < b), nil
	default:
		return runtime.Value{}, fmt.Errorf("interpreter: unsupported float binary op %d", op)
	}
}

func evalNeg(v runtime.Value) (runtime.Value, error) {
	switch v.Kind {
	case runtime.KindI4:
		return runtime.Int32(-v.I4), nil
	case runtime.KindI8:
		return runtime.Int64(-v.I8), nil
	case runtime.KindR4:
		return runtime.Float32(-v.R4), nil
	case runtime.KindR8:
		return runtime.Float64(-v.R8), nil
	default:
		return runtime.Value{}, fmt.Errorf("interpreter: neg on unsupported value kind %d", v.Kind)
	}
}

func evalNot(v runtime.Value) (runtime.Value, error) {
	switch v.Kind {
	case runtime.KindI4:
		return runtime.Int32(^v.I4), nil
	case runtime.KindI8:
		return runtime.Int64(^v.I8), nil
	default:
		return runtime.Value{}, fmt.Errorf("interpreter: not on unsupported value kind %d", v.Kind)
	}
}

func evalConv(kind ir.ConvKind, v runtime.Value) (runtime.Value, error) {
	switch kind {
	case ir.ConvR4, ir.ConvR8:
		f, err := toFloat64(v)
		if err != nil {
			return runtime.Value{}, err
		}
		if kind == ir.ConvR4 {
			return runtime.Float32(float32(f)), nil
		}
		return runtime.Float64(f), nil
	}

	// A managed pointer (KindRef) hitting a pointer-sized conv — the
	// `conv.u`/`conv.i` a real `fixed (T* p = span)` statement always
	// emits right after pinning (ECMA-335 §III.3.24 lowering of `fixed`:
	// no other IL shape stores a pinned reference into a T* local) —
	// passed through unconverted rather than erroring. Found running
	// real System.Text.Json 8.0.5's netstandard2.0 build (the TFM vmnet
	// actually selects, nuget.go's own "favor netstandard2.0" policy):
	// JsonReaderHelper.GetUtf8ByteCount/GetUtf8FromText do exactly this
	// (`fixed (char* chars = text) { return s_utf8Encoding.GetByteCount(
	// chars, text.Length); }`) to reach Encoding's pointer-taking
	// overloads — netstandard2.0's Encoding has no ReadOnlySpan<char>-
	// shaped overload at all, unlike net8.0's corelib. vmnet has no real
	// address space to produce a meaningful integer for this conversion;
	// every real caller here only ever hands the resulting "pointer"
	// straight back into ANOTHER call vmnet natively intercepts
	// (Encoding.GetByteCount/GetBytes's pointer-taking overloads,
	// system_text.go's encodingGetByteCount/encodingGetBytesSpan), which
	// already knows how to dereference a KindRef directly via
	// GetPinnableReference's own native (system_span.go). Only the
	// pointer-sized conversions are affected (ConvI4/ConvU4/ConvI8/
	// ConvU8) — a real narrowing conversion (conv.i1/u1/i2/u2) is never
	// emitted for a pointer value by any real compiler, so those keep
	// erroring on an unexpected KindRef via the ordinary toInt64 path
	// below.
	if v.Kind == runtime.KindRef && (kind == ir.ConvI4 || kind == ir.ConvU4 || kind == ir.ConvI8 || kind == ir.ConvU8) {
		return v, nil
	}

	// conv.u8 (spec §III.3.27) is the one real widening conversion here
	// that needs its own path rather than the shared sign-extending i64
	// below: converting a 32-bit-or-narrower value to a 64-bit UNSIGNED
	// one must zero-extend the source's own bit pattern, not sign-extend
	// its signed numeric value. Every other case here is either a
	// truncation (conv.i1/u1/i2/u2/i4/u4 — bit-pattern-correct regardless
	// of the source's signedness, since only the low bits survive) or a
	// same-width reinterpretation (conv.i8 on an already-64-bit source),
	// where sign vs. zero extension makes no difference. Found running
	// real Jint (Fase 3.79): String.prototype.split's own internal
	// SplitWithStringSeparator does `(ulong)Math.Min(segmentCount,
	// (ulong)limit)`, where `limit` is `uint.MaxValue` (bit pattern
	// 0xFFFFFFFF, vmnet's own KindI4 representation -1) whenever no
	// explicit limit argument is given — sign-extending that to int64
	// produced -1 instead of the correct 4294967295, so Math.Min silently
	// picked "-1" as the smaller value, corrupting the resulting array's
	// own length to -1 for every split() call with no limit (the
	// overwhelmingly common case).
	if kind == ir.ConvU8 {
		switch v.Kind {
		case runtime.KindI4:
			return runtime.Int64(int64(uint32(v.I4))), nil
		case runtime.KindI8:
			// Already 64 bits wide — no extension happens either way,
			// this conversion is a pure bit-pattern reinterpretation
			// (signed vs. unsigned int64 share the same representation
			// here, runtime.Value has no separate unsigned-64 Kind).
			return runtime.Int64(v.I8), nil
		}
	}

	i64, err := toInt64(v)
	if err != nil {
		return runtime.Value{}, err
	}
	switch kind {
	case ir.ConvI1:
		return runtime.Int32(int32(int8(i64))), nil
	case ir.ConvU1:
		return runtime.Int32(int32(uint8(i64))), nil
	case ir.ConvI2:
		return runtime.Int32(int32(int16(i64))), nil
	case ir.ConvU2:
		return runtime.Int32(int32(uint16(i64))), nil
	case ir.ConvI4, ir.ConvU4:
		return runtime.Int32(int32(i64)), nil
	case ir.ConvI8, ir.ConvU8:
		return runtime.Int64(i64), nil
	default:
		return runtime.Value{}, fmt.Errorf("interpreter: unsupported conv kind %d", kind)
	}
}

func evalCompare(in ir.BranchCompare, a, b runtime.Value) (bool, error) {
	if isReferenceShaped(a) && isReferenceShaped(b) {
		switch in.Op {
		case ir.CmpEq:
			return refEqual(a, b), nil
		case ir.CmpNe:
			return !refEqual(a, b), nil
		case ir.CmpGt:
			return refGreater(a, b), nil
		case ir.CmpLt:
			return refGreater(b, a), nil
		case ir.CmpGe:
			return !refGreater(b, a), nil
		case ir.CmpLe:
			return !refGreater(a, b), nil
		}
	}
	if a.Kind != b.Kind {
		return false, fmt.Errorf("interpreter: compare on mismatched value kinds (%d, %d)", a.Kind, b.Kind)
	}
	switch a.Kind {
	case runtime.KindI4:
		if in.Unsigned {
			return compareOrdered(in.Op, uint32(a.I4), uint32(b.I4)), nil
		}
		return compareOrdered(in.Op, a.I4, b.I4), nil
	case runtime.KindI8:
		if in.Unsigned {
			return compareOrdered(in.Op, uint64(a.I8), uint64(b.I8)), nil
		}
		return compareOrdered(in.Op, a.I8, b.I8), nil
	case runtime.KindR4:
		return compareOrdered(in.Op, a.R4, b.R4), nil
	case runtime.KindR8:
		return compareOrdered(in.Op, a.R8, b.R8), nil
	default:
		return false, fmt.Errorf("interpreter: compare on unsupported value kind %d", a.Kind)
	}
}

func compareOrdered[T int32 | int64 | uint32 | uint64 | float32 | float64](op ir.CompareOp, a, b T) bool {
	switch op {
	case ir.CmpEq:
		return a == b
	case ir.CmpNe:
		return a != b
	case ir.CmpGe:
		return a >= b
	case ir.CmpGt:
		return a > b
	case ir.CmpLe:
		return a <= b
	case ir.CmpLt:
		return a < b
	default:
		return false
	}
}

func toInt64(v runtime.Value) (int64, error) {
	switch v.Kind {
	case runtime.KindI4:
		return int64(v.I4), nil
	case runtime.KindI8:
		return v.I8, nil
	case runtime.KindR4:
		return int64(v.R4), nil
	case runtime.KindR8:
		return int64(v.R8), nil
	default:
		return 0, fmt.Errorf("interpreter: cannot convert value kind %d to integer", v.Kind)
	}
}

func toFloat64(v runtime.Value) (float64, error) {
	switch v.Kind {
	case runtime.KindI4:
		return float64(v.I4), nil
	case runtime.KindI8:
		return float64(v.I8), nil
	case runtime.KindR4:
		return float64(v.R4), nil
	case runtime.KindR8:
		return v.R8, nil
	default:
		return 0, fmt.Errorf("interpreter: cannot convert value kind %d to float", v.Kind)
	}
}

// isReferenceShaped reports whether v's Kind is one ceq/cgt.un/clt.un can
// compare by reference identity or nullness instead of by numeric value
// (ECMA-335 Table III.4: these ops are also verifiable on O/& operands,
// not just numeric ones). Handling this here — rather than only inside
// isinst's own IR case — matters because the single most common compiled
// form of `x is T`/`x != null`/`x == null` is exactly `<value> ldnull
// cgt.un`/`ceq`: comparing a reference-shaped Value against the KindNull
// literal ldnull pushes, which used to hit the "mismatched value kinds"
// error below (they're never the same Kind unless x is itself null) —
// found via the first isinst-using fixture (Fase 3.8), not a hole
// isinst's own tests would have caught in isolation.
func isReferenceShaped(v runtime.Value) bool {
	switch v.Kind {
	case runtime.KindNull, runtime.KindObject, runtime.KindArray, runtime.KindRef, runtime.KindStruct, runtime.KindString:
		return true
	default:
		return false
	}
}

// isPrimitiveValueKind reports whether k is one of the bare numeric Kinds
// `box` never actually wraps into a KindObject (see evalBinOp's own
// "box then null check" case above) — every real CIL primitive value
// type EXCEPT the ones vmnet already represents as KindStruct (DateTime,
// TimeSpan, ...) or collapses to plain int32 (bool/byte/char/short, all
// KindI4).
func isPrimitiveValueKind(k runtime.Kind) bool {
	switch k {
	case runtime.KindI4, runtime.KindI8, runtime.KindR4, runtime.KindR8:
		return true
	default:
		return false
	}
}

// isNullish reports whether v is (or, for a Kind that could in principle
// carry a nil payload defensively, currently holds) a null reference.
func isNullish(v runtime.Value) bool {
	switch v.Kind {
	case runtime.KindNull:
		return true
	case runtime.KindObject:
		return v.Obj == nil
	case runtime.KindArray:
		return v.Arr == nil
	case runtime.KindRef:
		return v.Ref == nil
	default:
		return false
	}
}

// refEqual implements ceq/beq on reference-shaped operands: both null, or
// the same object/array/pointer identity, or (structs have no identity —
// spec: value type equality is structural) recursively equal fields, or
// (vmnet models strings as immutable Go values, not heap references with
// observable identity) equal content.
func refEqual(a, b runtime.Value) bool {
	aNull, bNull := isNullish(a), isNullish(b)
	if aNull || bNull {
		return aNull && bNull
	}
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case runtime.KindObject:
		return a.Obj == b.Obj
	case runtime.KindArray:
		return a.Arr == b.Arr
	case runtime.KindRef:
		return a.Ref == b.Ref
	case runtime.KindString:
		return a.Str == b.Str
	case runtime.KindStruct:
		return structFieldsEqual(a.Struct, b.Struct)
	default:
		return false
	}
}

func structFieldsEqual(a, b *runtime.Struct) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Type != b.Type || len(a.Fields) != len(b.Fields) {
		return false
	}
	for i := range a.Fields {
		if !valuesDeepEqual(a.Fields[i], b.Fields[i]) {
			return false
		}
	}
	return true
}

// valuesDeepEqual is refEqual's numeric-aware counterpart, used only for
// comparing a struct's own fields: unlike the top-level ceq/cgt.un
// dispatch (which only ever sees two reference-shaped operands together —
// numeric ceq already has its own path via evalBinOpInt/Float), a
// struct's fields can be any mix of numeric and reference-shaped Kinds.
func valuesDeepEqual(a, b runtime.Value) bool {
	if isReferenceShaped(a) && isReferenceShaped(b) {
		return refEqual(a, b)
	}
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case runtime.KindI4:
		return a.I4 == b.I4
	case runtime.KindI8:
		return a.I8 == b.I8
	case runtime.KindR4:
		return a.R4 == b.R4
	case runtime.KindR8:
		return a.R8 == b.R8
	case runtime.KindBytes:
		return string(a.Bytes) == string(b.Bytes)
	default:
		return false
	}
}

// refGreater backs cgt.un/clt.un's dominant real use — the `x != null`
// idiom (`<x> ldnull cgt.un`, spec's null-check compiles to comparing a
// reference's bit pattern against zero unsigned) — as "a is non-null and
// b is null". Two non-null references have no meaningful order in vmnet's
// model (there's no raw pointer to compare), so refGreater is false for
// that case rather than an arbitrary answer — real code never branches on
// relative ordering of two arbitrary object references anyway, only on
// nullness.
func refGreater(a, b runtime.Value) bool {
	return !isNullish(a) && isNullish(b)
}
