package bcl

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// int32ToString/int64ToString honor a real ToString(format) argument via
// formatValue (System.String.Format's own specifier parser, system_
// string.go) rather than ignoring it — real Int32.ToString("X")/("N0")/
// ("D6") are exactly as common as the plain no-argument overload, and by
// the time formatValue existed for String.Format composite specs there
// was no reason for this one call site to keep silently discarding it.
// The provider argument itself is still ignored either way (no culture
// support anywhere else — CultureInfo is a stub, Fase 3.6).
//
// numericToStringFormat finds a ToString(format)/(format, provider) call's
// format-string argument, if any — args[1:] rather than a fixed index,
// since the IFormatProvider-only overload (ToString(CultureInfo), no
// format string at all) and the (format, provider) overload both exist,
// and vmnet ignores the provider itself either way (culture-invariant
// only, no culture support anywhere else — CultureInfo is a stub, Fase
// 3.6).
func numericToStringFormat(args []runtime.Value) string {
	for _, a := range args[1:] {
		if a.Kind == runtime.KindString {
			return a.Str
		}
	}
	return ""
}

func init() {
	register("System.Int32::ToString", true, int32ToString)
	register("System.Int32::Parse", true, int32Parse)
	register("System.Int32::TryParse", true, int32TryParse)
	register("System.Int32::CompareTo", true, int32CompareTo)
	// IComparable`1::CompareTo (Fase 3.64) — reached when a generic method
	// constrained on IComparable<T> calls value.CompareTo(other) with T
	// still open at the call site (`constrained.` prefix), so the
	// compiled call names the INTERFACE, not any concrete numeric type;
	// vmnet's own type-erased generics have no TypeDef for T to redirect
	// through the ordinary virtual-dispatch ancestor walk, so this
	// dispatches directly off the receiver's own runtime Kind instead.
	// Found via FluentValidation's own GreaterThanOrEqualToValidator<T,
	// TProperty>, comparing a real int/double property value this way.
	register("System.IComparable`1::CompareTo", true, comparableCompareTo)
	register("System.Int64::ToString", true, int64ToString)
	register("System.Int64::Parse", true, int64Parse)
	register("System.Int64::TryParse", true, int64TryParse)
	register("System.Int32::GetHashCode", true, int32GetHashCode)
	// Int16/Byte are stored the same way Int32 is (a plain KindI4 on the
	// CIL stack — every integral type narrower than int32 widens to it,
	// spec §III.1.1), so ToString/GetHashCode reuse Int32's natives
	// directly rather than duplicating them.
	register("System.Int16::ToString", true, int32ToString)
	register("System.Int16::GetHashCode", true, int32GetHashCode)
	register("System.Byte::ToString", true, int32ToString)
	register("System.Byte::GetHashCode", true, int32GetHashCode)
	// SByte/UInt16 also widen to a plain KindI4 on the stack (same
	// reasoning as Int16/Byte above) and, unlike UInt32, their entire
	// real value range (-128..127, 0..65535) already prints correctly as
	// a signed int32 with no unsigned-specific formatting needed — so
	// they reuse int32ToString directly rather than needing their own
	// native (Fase 3.52, found auditing Dapper@2.1.79's own numeric
	// coercion/formatting helper, SqlMapper.Format, which switches over
	// every integral TypeCode).
	register("System.SByte::ToString", true, int32ToString)
	register("System.UInt16::ToString", true, int32ToString)
	register("System.UInt32::ToString", true, uint32ToString)
	register("System.UInt64::ToString", true, uint64ToString)
	register("System.Single::ToString", true, singleToString)
	// System.Decimal (Fase 3.53) — see decimalCtorInPlace's own doc
	// comment for why this codebase's total lack of a distinct Decimal
	// representation still lets ToString work correctly for every real
	// value found in this loop's target packages: a decimal collapses to
	// a plain KindR8 double, so its ToString is genuinely just
	// doubleToString (system_misc.go) reused verbatim, not new numeric
	// logic — formatValue's own "System.Double"/"System.Single"/
	// "System.Decimal" formatting bucket (system_misc.go) already treats
	// all three identically.
	register("System.Decimal::.ctor", false, decimalCtorInPlace)
	register("System.Decimal::ToString", true, doubleToString)
}

// decimalCtorInPlace backs every real System.Decimal instance constructor
// overload, reached via `ldloca`+`call` — the compiler always initializes
// a value-type local this way rather than `newobj`+`stloc` (same
// established pattern DateTime/TimeSpan/KeyValuePair`2 already needed
// their own ctor-in-place entry for; see system_collections.go's
// keyValuePairCtorInPlace).
//
// vmnet has no dedicated Decimal Kind — System.Decimal still has no
// distinct representation anywhere in this codebase (docs/en/ROADMAP.md;
// system_data_sqlite.go's own GetDecimal doc comment already documents
// the same gap for a SQLite column read as DbType.Decimal). Every decimal
// value here collapses to a plain KindR8 double instead — the same
// simplification formatValue's own composite-format bucket already makes
// for "System.Decimal" formatting. That loses real decimal's exact
// base-10 arithmetic and its 28-29 significant digits of precision for
// genuinely huge/high-precision values, but every real constructor
// argument found in this loop's target packages (ordinary prices,
// quantities, totals — well within float64's own ~15-17 significant
// digits) round-trips through it exactly fine.
//
// The 5-int (lo, mid, hi, isNegative, scale) overload is what the C#
// compiler actually emits for every decimal LITERAL (`1234.5m` becomes
// `ldc.i4 12345 ... ldc.i4.1 (scale) ... call instance void
// System.Decimal::.ctor(int32, int32, int32, bool, uint8)`) — confirmed
// against a real `dotnet run` probe before assuming it, not guessed — by
// far the most common real shape. The narrower int/long/float/double
// overloads (an implicit numeric-to-decimal conversion, or
// Convert.ToDecimal) and the parameterless default(decimal)/`new
// Decimal()` are also covered, since none of them cost anything extra
// once the receiver-mutation plumbing already exists for the 5-int case.
// The int[4] bits-array overload (interop with decimal.GetBits' own round
// trip) is deliberately NOT covered — no real call site constructing a
// Decimal that way was found in this loop's target packages.
func decimalCtorInPlace(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindRef || args[0].Ref == nil {
		return runtime.Value{}, fmt.Errorf("bcl: Decimal constructor called without a receiver")
	}
	var v float64
	switch len(args) {
	case 1:
		// Decimal() / default(decimal) — no explicit value at all.
		v = 0
	case 2:
		switch args[1].Kind {
		case runtime.KindI4:
			v = float64(args[1].I4)
		case runtime.KindI8:
			v = float64(args[1].I8)
		case runtime.KindR4:
			v = float64(args[1].R4)
		case runtime.KindR8:
			v = args[1].R8
		default:
			return runtime.Value{}, fmt.Errorf("bcl: System.Decimal constructor: unsupported single-argument kind %v", args[1].Kind)
		}
	case 6:
		lo, mid, hi, isNegative, scale := args[1], args[2], args[3], args[4], args[5]
		if lo.Kind != runtime.KindI4 || mid.Kind != runtime.KindI4 || hi.Kind != runtime.KindI4 || scale.Kind != runtime.KindI4 {
			return runtime.Value{}, fmt.Errorf("bcl: System.Decimal(int,int,int,bool,byte) constructor: unsupported argument shape")
		}
		v = decimalFromBits(uint32(lo.I4), uint32(mid.I4), uint32(hi.I4), isNegative.Truthy(), uint(scale.I4))
	default:
		return runtime.Value{}, fmt.Errorf("bcl: System.Decimal constructor: unsupported argument count %d", len(args)-1)
	}
	*args[0].Ref = runtime.Float64(v)
	return runtime.Value{}, nil
}

// decimalFromBits reconstructs the double this codebase approximates a
// decimal with from its real (lo, mid, hi, isNegative, scale) 96-bit
// mantissa + scale representation — exactly what a decimal literal's
// compiled constructor call passes (decimalCtorInPlace's own doc
// comment). The 96-bit unsigned mantissa (hi:mid:lo, each a uint32) is
// combined as a plain float64 expression rather than via exact 96-bit
// integer math (e.g. math/big): float64 already only has ~15-17
// significant decimal digits, so doing exact integer arithmetic first and
// converting to float64 only at the very end would still lose precision
// at that same final step — no accuracy is actually gained by the extra
// complexity given this codebase's already-lossy decimal-as-double
// approximation.
func decimalFromBits(lo, mid, hi uint32, isNegative bool, scale uint) float64 {
	const twoPow32 = 4294967296.0
	mantissa := float64(hi)*twoPow32*twoPow32 + float64(mid)*twoPow32 + float64(lo)
	v := mantissa / math.Pow(10, float64(scale))
	if isNegative {
		v = -v
	}
	return v
}

// uint32ToString backs UInt32.ToString([format]) — UNLIKE SByte/UInt16
// above, a uint32's real value range (0..4294967295) does NOT fit inside
// int32's positive range, so int32ToString's plain
// strconv.FormatInt(int64(v.I4)) would print a value above 2^31-1 as
// negative (its two's-complement bit pattern reinterpreted as signed) —
// genuinely wrong output, not just a missing feature. Reinterprets the
// same stored KindI4 bits as unsigned before formatting instead.
func uint32ToString(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.UInt32.ToString expects a receiver")
	}
	v := args[0]
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	if v.Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: System.UInt32.ToString expects a uint32 receiver")
	}
	if format := numericToStringFormat(args); format != "" {
		s, err := formatValue(v, format)
		if err != nil {
			return runtime.Value{}, err
		}
		return runtime.String(s), nil
	}
	return runtime.String(strconv.FormatUint(uint64(uint32(v.I4)), 10)), nil
}

// uint64ToString mirrors uint32ToString for the KindI8-stored ulong case.
func uint64ToString(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.UInt64.ToString expects a receiver")
	}
	v := args[0]
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	if v.Kind != runtime.KindI8 {
		return runtime.Value{}, fmt.Errorf("bcl: System.UInt64.ToString expects a uint64 receiver")
	}
	if format := numericToStringFormat(args); format != "" {
		s, err := formatValue(v, format)
		if err != nil {
			return runtime.Value{}, err
		}
		return runtime.String(s), nil
	}
	return runtime.String(strconv.FormatUint(uint64(v.I8), 10)), nil
}

// singleToString is Double.ToString's own doubleToString (system_misc.go)
// narrowed to a KindR4 receiver — vmnet keeps `float`/System.Single as
// its own distinct Kind (runtime.KindR4), unlike Decimal (no dedicated
// representation at all here, a genuine gap — see docs/en/ROADMAP.md),
// so it needs its own native rather than reusing doubleToString's KindR8
// check directly.
func singleToString(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Single.ToString expects a receiver")
	}
	v := args[0]
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	if v.Kind != runtime.KindR4 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Single.ToString expects a float receiver")
	}
	if format := numericToStringFormat(args); format != "" {
		s, err := formatValue(runtime.Float64(float64(v.R4)), format)
		if err != nil {
			return runtime.Value{}, err
		}
		return runtime.String(s), nil
	}
	return runtime.String(strconv.FormatFloat(float64(v.R4), 'G', -1, 32)), nil
}

// int32GetHashCode returns the value itself, matching real
// Int32.GetHashCode's documented identity behavior.
func int32GetHashCode(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: Int32.GetHashCode expects a receiver")
	}
	v := args[0]
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	if v.Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: Int32.GetHashCode expects an int32 receiver")
	}
	return runtime.Int32(v.I4), nil
}

func int64Parse(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.Int64.Parse expects a string argument")
	}
	n, err := strconv.ParseInt(args[0].Str, 10, 64)
	if err != nil {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.FormatException", Message: fmt.Sprintf("Input string %q was not in a correct format.", args[0].Str)}
	}
	return runtime.Int64(n), nil
}

// int64TryParse's `out long result` arrives as a managed pointer, same
// mechanism as Int32.TryParse — but unlike Int32.TryParse, real code
// calling Int64.TryParse commonly uses the ReadOnlySpan<char> overload
// (`TryParse(ReadOnlySpan<char>, out long)`, found running real Jint/
// Esprima number-parsing code) or the NumberStyles/IFormatProvider
// overload, not just the plain (string, out long) shape — parseTextArg/
// lastOutRef handle any of these uniformly rather than hard-requiring
// exactly 2 arguments.
func int64TryParse(args []runtime.Value) (runtime.Value, error) {
	text, ok := parseTextArg(args)
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: System.Int64.TryParse: unsupported argument shape")
	}
	outRef := lastOutRef(args)
	if outRef == nil {
		return runtime.Value{}, fmt.Errorf("bcl: System.Int64.TryParse: no out parameter found")
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		*outRef = runtime.Int64(0)
		return runtime.Bool(false), nil
	}
	*outRef = runtime.Int64(n)
	return runtime.Bool(true), nil
}

// parseTextArg reads the "what to parse" argument any Xxx.Parse/TryParse
// native receives as its first real argument — either a plain string or
// a ReadOnlySpan<char> (real modern code commonly prefers the latter to
// avoid allocating a substring first).
func parseTextArg(args []runtime.Value) (string, bool) {
	if len(args) == 0 {
		return "", false
	}
	if args[0].Kind == runtime.KindString {
		return args[0].Str, true
	}
	if args[0].Kind == runtime.KindStruct && args[0].Struct != nil {
		return spanToStringValue(args[0].Struct)
	}
	return "", false
}

// lastOutRef finds a TryParse-shaped native's `out` parameter: real
// TryParse overloads always declare it last, regardless of how many
// NumberStyles/IFormatProvider arguments come before it.
func lastOutRef(args []runtime.Value) *runtime.Value {
	for i := len(args) - 1; i >= 0; i-- {
		if args[i].Kind == runtime.KindRef && args[i].Ref != nil {
			return args[i].Ref
		}
	}
	return nil
}

func int64ToString(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Int64.ToString expects an int64 receiver")
	}
	v := args[0]
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	if v.Kind != runtime.KindI8 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Int64.ToString expects an int64 receiver")
	}
	if format := numericToStringFormat(args); format != "" {
		s, err := formatValue(v, format)
		if err != nil {
			return runtime.Value{}, err
		}
		return runtime.String(s), nil
	}
	return runtime.String(strconv.FormatInt(v.I8, 10)), nil
}

func int32Parse(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.Int32.Parse expects a string argument")
	}
	n, err := strconv.ParseInt(args[0].Str, 10, 32)
	if err != nil {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.FormatException", Message: fmt.Sprintf("Input string %q was not in a correct format.", args[0].Str)}
	}
	return runtime.Int32(int32(n)), nil
}

// numberStylesAllowHexSpecifier is System.Globalization.NumberStyles.
// AllowHexSpecifier's real bit value (0x00000200) — HexNumber itself is
// AllowLeadingWhite|AllowTrailingWhite|AllowHexSpecifier (0x00000203), so
// testing just this one bit catches either constant a caller passes.
const numberStylesAllowHexSpecifier = 0x200

// numberStylesArg finds a TryParse(string, NumberStyles, IFormatProvider,
// out T) call's own NumberStyles argument — always a plain int32 enum
// value, always between the text and the trailing out parameter
// (lastOutRef's own convention), so scanning by Kind rather than a fixed
// index handles both this 4-arg overload and the plain 2-arg
// TryParse(string, out T) uniformly (returns 0/false for the latter,
// same as NumberStyles.None would).
func numberStylesArg(args []runtime.Value) (int32, bool) {
	for i := 1; i < len(args)-1; i++ {
		if args[i].Kind == runtime.KindI4 {
			return args[i].I4, true
		}
	}
	return 0, false
}

// int32TryParse's `out int result` arrives as a managed pointer, same
// mechanism as any other `ref`/`out` primitive since Fase 3.5. Both real
// overload shapes are covered (Fase 3.43, the second found reading a real
// .xlsx through ClosedXML 0.105.0's own worksheet reader): the plain
// `TryParse(string, out int)` and the culture-explicit `TryParse(string,
// NumberStyles, IFormatProvider, out int)` — the out parameter is always
// the LAST argument, the string always the first. The styles/provider
// pair is usually NumberStyles.Integer-shaped decimal digits (what base-10
// ParseInt already is regardless), but AllowHexSpecifier/HexNumber
// (Fase 3.51, a real, common pattern for parsing a raw hex literal, e.g.
// `int.TryParse(s, NumberStyles.HexNumber, CultureInfo.InvariantCulture,
// out var n)`) is a genuine semantic difference this now honors instead
// of silently mis-parsing every hex-styled input as decimal and failing.
func int32TryParse(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.Int32.TryParse expects (string, out int)")
	}
	out := args[len(args)-1]
	if out.Kind != runtime.KindRef || out.Ref == nil {
		return runtime.Value{}, fmt.Errorf("bcl: System.Int32.TryParse expects an out parameter")
	}
	if styles, ok := numberStylesArg(args); ok && styles&numberStylesAllowHexSpecifier != 0 {
		n, err := strconv.ParseInt(strings.TrimSpace(args[0].Str), 16, 32)
		if err != nil {
			*out.Ref = runtime.Int32(0)
			return runtime.Bool(false), nil
		}
		*out.Ref = runtime.Int32(int32(n))
		return runtime.Bool(true), nil
	}
	n, err := strconv.ParseInt(strings.TrimSpace(args[0].Str), 10, 32)
	if err != nil {
		*out.Ref = runtime.Int32(0)
		return runtime.Bool(false), nil
	}
	*out.Ref = runtime.Int32(int32(n))
	return runtime.Bool(true), nil
}

func int32CompareTo(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Int32.CompareTo expects a receiver and an argument")
	}
	a, b := args[0], args[1]
	if a.Kind == runtime.KindRef && a.Ref != nil {
		a = *a.Ref
	}
	if a.Kind != runtime.KindI4 || b.Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Int32.CompareTo expects int32 operands")
	}
	switch {
	case a.I4 < b.I4:
		return runtime.Int32(-1), nil
	case a.I4 > b.I4:
		return runtime.Int32(1), nil
	default:
		return runtime.Int32(0), nil
	}
}

// comparableCompareTo backs IComparable`1::CompareTo for a still-open
// generic type parameter (see its own registration comment above) —
// dispatches directly off the receiver's runtime Kind rather than
// delegating to a per-type CompareTo native, since not every numeric
// Kind here (Int64, Single) has its own registered CompareTo to reuse.
func comparableCompareTo(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: IComparable.CompareTo expects a receiver and an argument")
	}
	a, b := args[0], args[1]
	if a.Kind == runtime.KindRef && a.Ref != nil {
		a = *a.Ref
	}
	if b.Kind == runtime.KindRef && b.Ref != nil {
		b = *b.Ref
	}
	cmp := func(less, greater bool) (runtime.Value, error) {
		switch {
		case less:
			return runtime.Int32(-1), nil
		case greater:
			return runtime.Int32(1), nil
		default:
			return runtime.Int32(0), nil
		}
	}
	switch a.Kind {
	case runtime.KindI4:
		return cmp(a.I4 < b.I4, a.I4 > b.I4)
	case runtime.KindI8:
		return cmp(a.I8 < b.I8, a.I8 > b.I8)
	case runtime.KindR4:
		return cmp(a.R4 < b.R4, a.R4 > b.R4)
	case runtime.KindR8:
		return cmp(a.R8 < b.R8, a.R8 > b.R8)
	case runtime.KindString:
		return cmp(a.Str < b.Str, a.Str > b.Str)
	default:
		// A reference-typed receiver with no meaningful numeric/string
		// ordering vmnet can compute (found via a real, surprising case:
		// FluentValidation's own internal code calls this against a
		// ValidationContext<T> — almost certainly a generic utility
		// (pooling/caching, not user-facing rule comparison) that doesn't
		// actually need a real, correct answer to produce correct
		// validation RESULTS). Reference equality is the only honest
		// answer available without modeling that specific internal
		// utility's exact intent — 0 (equal) for the same object, else an
		// arbitrary-but-stable non-zero, rather than crashing the whole
		// call outright.
		if a.Kind == runtime.KindObject && b.Kind == runtime.KindObject {
			if a.Obj == b.Obj {
				return runtime.Int32(0), nil
			}
			return runtime.Int32(-1), nil
		}
		return runtime.Value{}, fmt.Errorf("bcl: IComparable.CompareTo: unsupported receiver kind %v (only numeric/string types are supported for a still-open generic type parameter)", a.Kind)
	}
}

// int32ToString's receiver may arrive as a managed pointer (KindRef)
// rather than the raw int32: `n.ToString()` on a boxed/generic-context
// value compiles through `constrained.`+callvirt, the same by-pointer
// receiver shape every other value type's instance methods use (Fase
// 3.7 onward).
func int32ToString(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Int32.ToString expects an int32 receiver")
	}
	v := args[0]
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	if v.Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Int32.ToString expects an int32 receiver")
	}
	if format := numericToStringFormat(args); format != "" {
		s, err := formatValue(v, format)
		if err != nil {
			return runtime.Value{}, err
		}
		return runtime.String(s), nil
	}
	return runtime.String(strconv.FormatInt(int64(v.I4), 10)), nil
}
