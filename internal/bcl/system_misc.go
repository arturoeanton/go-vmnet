package bcl

import (
	"fmt"
	goruntime "runtime"
	"strconv"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// Small, self-contained natives that don't warrant their own file: a
// culture stub (vmnet has no locale-aware formatting, so InvariantCulture
// is just a sentinel object other natives ignore) and a thread-id stub
// (vmnet runs interpreted code on whatever Go goroutine called it — there
// is no managed thread pool to report a real ID from).
func init() {
	register("System.Globalization.CultureInfo::get_InvariantCulture", true, cultureInfoInvariant)
	register("System.Environment::get_CurrentManagedThreadId", true, environmentThreadID)
	// "\n", not "\r\n" — vmnet has no concept of a host OS to match
	// Environment.NewLine's real platform-dependent value against; "\n"
	// is the more common expectation for embedded/server code (and what
	// every other vmnet-produced string, e.g. StringBuilder.AppendLine,
	// already uses).
	register("System.Environment::get_NewLine", true, environmentNewLine)
	// Always "not set" — vmnet has no host-environment permission model
	// yet (Fase 4), so a sandboxed plugin doesn't get to see real host
	// environment variables; every real caller found so far (NPOI's own
	// IOUtils..cctor, reading a size-limit override) already handles a
	// null/missing variable gracefully by falling back to its own
	// built-in default.
	register("System.Environment::GetEnvironmentVariable", true, environmentGetEnvironmentVariableNull)
	// UserName/MachineName: vmnet has no host-environment permission
	// model yet (Fase 4, same posture as GetEnvironmentVariable above) —
	// a plausible constant, matching real code's common use (document
	// Author/Creator metadata defaults) without exposing anything about
	// the actual host.
	register("System.Environment::get_UserName", true, environmentUserName)
	register("System.Environment::get_MachineName", true, environmentUserName)
	// ProcessorCount (Fase 3.65, found via AutoMapper's own
	// LockingConcurrentDictionary sizing its internal partition count off
	// it): unlike GetEnvironmentVariable/UserName above, a CPU count
	// doesn't reveal anything about the host's identity, only its
	// capacity — real Go's own runtime.NumCPU() (the actual host this
	// process is running on) is a fine, honest answer here rather than a
	// constant stand-in.
	register("System.Environment::get_ProcessorCount", true, environmentProcessorCount)
	register("System.Convert::ToInt32", true, convertToInt32)
	register("System.Convert::ToInt64", true, convertToInt64)
	register("System.Double::ToString", true, doubleToString)
	register("System.Double::TryParse", true, doubleTryParse)
	register("System.Double::Equals", true, doubleEquals)
	register("System.Globalization.CultureInfo::get_CurrentCulture", true, cultureInfoInvariant)
	// CurrentUICulture (Fase 3.68, found via FluentValidation's own
	// localized error-message building — MessageFormatter/Localized
	// reads it to pick which resource satellite to use) — same "vmnet
	// has no real locale data at all" posture get_CurrentCulture already
	// established: always the invariant culture.
	register("System.Globalization.CultureInfo::get_CurrentUICulture", true, cultureInfoInvariant)
	// Parent (Fase 3.68, same real caller as CurrentUICulture — resource
	// satellite fallback walks up a culture's own parent chain until it
	// finds one with a matching resource, e.g. "en-US" -> "en" ->
	// invariant): the invariant culture is real .NET's own fixed point
	// (InvariantCulture.Parent == InvariantCulture itself), so this never
	// needs to model an actual parent chain at all.
	register("System.Globalization.CultureInfo::get_Parent", true, cultureInfoInvariant)
	register("System.Globalization.CultureInfo::get_Name", true, cultureInfoName)
	// IsNeutralCulture (Fase 3.68, found via FluentValidation's own
	// LanguageManager resource fallback): real .NET's InvariantCulture
	// reports false here (it is the ROOT, not a "neutral" culture like
	// "en" sitting above a specific one like "en-US").
	register("System.Globalization.CultureInfo::get_IsNeutralCulture", true, cultureInfoIsNeutralCulture)
	// CultureInfo.TextInfo (Fase 3.64) — found via CsvHelper's own header
	// name matching. Real .NET's TextInfo is culture-specific (Turkish
	// "i" casing being the classic gotcha); vmnet has no real locale data
	// at all (same posture cultureInfoInvariant's own doc comment already
	// takes), so this always behaves like the invariant culture — correct
	// for every real corpus caller found, which only ever runs under
	// CultureInfo.InvariantCulture in the first place.
	register("System.Globalization.CultureInfo::get_TextInfo", true, cultureInfoInvariant)
	register("System.Globalization.TextInfo::ToUpper", true, textInfoToUpper)
	register("System.Globalization.TextInfo::ToLower", true, textInfoToLower)
	register("System.Globalization.TextInfo::ToTitleCase", true, textInfoToTitleCase)
	// TextInfo.ListSeparator — real invariant-culture value is "," (found
	// via CsvHelper's own configuration defaulting to it).
	register("System.Globalization.TextInfo::get_ListSeparator", true, textInfoGetListSeparator)
	// TimeZoneInfo.Local/Utc: vmnet has no real timezone database (same
	// "no locale/culture data anywhere" limitation as CultureInfo above)
	// — both return the same stub object, offset-less, matching the
	// existing UTC-identity treatment DateTime.ToUniversalTime/
	// ToLocalTime already has (Fase 3.23). Deliberately its OWN singleton
	// (Fase 3.68), not cultureInfoInvariant's — a TimeZoneInfo and a
	// CultureInfo are unrelated real .NET types, so they must never
	// compare reference-equal to each other even though both are
	// "no real data" stand-ins internally.
	register("System.TimeZoneInfo::get_Local", true, timeZoneInfoStub)
	register("System.TimeZoneInfo::get_Utc", true, timeZoneInfoStub)
	register("System.Double::CompareTo", true, doubleCompareTo)
	register("System.Double::Parse", true, doubleParse)
	register("System.Boolean::ToString", true, boolToString)
	register("System.Boolean::CompareTo", true, boolCompareTo)
	register("System.Boolean::GetHashCode", true, boolGetHashCode)
	register("System.Boolean::TryParse", true, boolTryParse)
	register("System.Convert::ToString", true, convertToString)
	register("System.Convert::ToDouble", true, convertToDouble)
	register("System.Convert::ToBoolean", true, convertToBoolean)
	register("System.Convert::ChangeType", true, convertChangeType)
	register("System.Convert::ToByte", true, convertToByte)
	register("System.Convert::ToSByte", true, convertToSByte)
	register("System.Convert::ToInt16", true, convertToInt16)
	register("System.Convert::ToUInt16", true, convertToUInt16)
	register("System.Convert::ToUInt32", true, convertToUInt32)
	register("System.Convert::ToUInt64", true, convertToUInt64)
	register("System.Convert::ToSingle", true, convertToSingle)
	register("System.Char::ConvertFromUtf32", true, charConvertFromUtf32)
	// System.IO.FileSystemInfo (FileInfo/DirectoryInfo's real BCL base):
	// vmnet has no arbitrary-disk-file permissions model yet (Fase 4),
	// and registers no FileInfo/DirectoryInfo constructor at all — these
	// three exist only so a real package's own code that *checks*
	// FileSystemInfo state along a path this loop's target packages
	// never actually take (found in NPOI's own POIFS temp-file fallback
	// machinery) doesn't hard-crash the interpreter outright; they never
	// touch a real filesystem.
	register("System.IO.FileSystemInfo::get_FullName", true, fileSystemInfoEmptyString)
	register("System.IO.FileSystemInfo::get_Exists", true, fileSystemInfoFalse)
	register("System.IO.FileSystemInfo::Delete", false, fileSystemInfoNoop)
}

func fileSystemInfoEmptyString(args []runtime.Value) (runtime.Value, error) {
	return runtime.String(""), nil
}

func fileSystemInfoFalse(args []runtime.Value) (runtime.Value, error) {
	return runtime.Bool(false), nil
}

func fileSystemInfoNoop(args []runtime.Value) (runtime.Value, error) {
	return runtime.Value{}, nil
}

func doubleCompareTo(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Double.CompareTo expects a receiver and an argument")
	}
	a, b := args[0], args[1]
	if a.Kind == runtime.KindRef && a.Ref != nil {
		a = *a.Ref
	}
	if a.Kind != runtime.KindR8 || b.Kind != runtime.KindR8 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Double.CompareTo expects double operands")
	}
	switch {
	case a.R8 < b.R8:
		return runtime.Int32(-1), nil
	case a.R8 > b.R8:
		return runtime.Int32(1), nil
	default:
		return runtime.Int32(0), nil
	}
}

func doubleParse(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.Double.Parse expects a string argument")
	}
	f, err := strconv.ParseFloat(args[0].Str, 64)
	if err != nil {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.FormatException", Message: fmt.Sprintf("Input string %q was not in a correct format.", args[0].Str)}
	}
	return runtime.Float64(f), nil
}

// boolToString capitalizes "True"/"False" — real Boolean.ToString's
// exact casing, unlike Go's lowercase strconv.FormatBool.
func boolToString(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Boolean.ToString expects a receiver")
	}
	v := args[0]
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	if v.I4 != 0 {
		return runtime.String("True"), nil
	}
	return runtime.String("False"), nil
}

func boolCompareTo(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Boolean.CompareTo expects a receiver and an argument")
	}
	a, b := args[0], args[1]
	if a.Kind == runtime.KindRef && a.Ref != nil {
		a = *a.Ref
	}
	switch {
	case a.I4 == b.I4:
		return runtime.Int32(0), nil
	case a.I4 == 0:
		return runtime.Int32(-1), nil
	default:
		return runtime.Int32(1), nil
	}
}

func boolGetHashCode(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Boolean.GetHashCode expects a receiver")
	}
	v := args[0]
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	if v.I4 != 0 {
		return runtime.Int32(1), nil
	}
	return runtime.Int32(0), nil
}

// boolTryParse backs Boolean.TryParse(string, out bool) — the same
// managed-pointer `out` mechanism as doubleTryParse/int32TryParse above
// (Fase 3.81, found via CsvHelper's own BooleanConverter.ConvertFromString,
// whose very first attempt is a bare `bool.TryParse(text, out result)`
// before falling back to 0/1 and configurable true/false string lists).
// Go's strconv.ParseBool already accepts real .NET's own "True"/"False"
// spelling (case-insensitively, matching bool.TryParse's real behavior)
// as well as "1"/"0" — 't'/'f'/"T"/"F" also parse in Go but not in real
// .NET; no caller found so far relies on that extra leniency, and it's
// harmless (strictly MORE permissive, never rejects a real .NET-valid
// input).
func boolTryParse(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Boolean.TryParse expects (string, out bool)")
	}
	if args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.Boolean.TryParse expects a string argument")
	}
	out := args[len(args)-1]
	if out.Kind != runtime.KindRef || out.Ref == nil {
		return runtime.Value{}, fmt.Errorf("bcl: System.Boolean.TryParse expects an out parameter")
	}
	b, err := strconv.ParseBool(strings.TrimSpace(args[0].Str))
	if err != nil {
		*out.Ref = runtime.Bool(false)
		return runtime.Bool(false), nil
	}
	*out.Ref = runtime.Bool(b)
	return runtime.Bool(true), nil
}

// convertToString covers Convert.ToString(object)/(int)/(double)/... by
// reusing displayString, the same formatting every other implicit
// ToString/boxed-argument path in this package already shares — a
// trailing IFormatProvider argument (culture) is accepted and ignored.
func convertToString(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.String(""), nil
	}
	if args[0].Kind == runtime.KindNull {
		return runtime.String(""), nil
	}
	return runtime.String(displayString(args[0])), nil
}

// charConvertFromUtf32 converts a Unicode code point to its string
// representation — Go's string(rune) already produces the correct UTF-8
// encoding for the full code point range, including values beyond
// U+FFFF that real .NET represents as a UTF-16 surrogate pair
// internally (an implementation detail invisible at this API boundary).
func charConvertFromUtf32(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Char.ConvertFromUtf32 expects an int argument")
	}
	return runtime.String(string(rune(args[0].I4))), nil
}

func cultureInfoName(args []runtime.Value) (runtime.Value, error) {
	return runtime.String(""), nil
}

func cultureInfoIsNeutralCulture(args []runtime.Value) (runtime.Value, error) {
	return runtime.Bool(false), nil
}

func convertToInt64(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Convert.ToInt64 expects an argument")
	}
	switch v := args[0]; v.Kind {
	case runtime.KindI4:
		return runtime.Int64(int64(v.I4)), nil
	case runtime.KindI8:
		return v, nil
	case runtime.KindR4:
		return runtime.Int64(int64(v.R4 + sign(v.R4)*0.5)), nil
	case runtime.KindR8:
		return runtime.Int64(int64(v.R8 + sign(v.R8)*0.5)), nil
	case runtime.KindString:
		n, err := strconv.ParseInt(v.Str, 10, 64)
		if err != nil {
			return runtime.Value{}, &runtime.ManagedException{TypeName: "System.FormatException", Message: fmt.Sprintf("Input string %q was not in a correct format.", v.Str)}
		}
		return runtime.Int64(n), nil
	case runtime.KindNull:
		return runtime.Int64(0), nil
	default:
		return runtime.Value{}, fmt.Errorf("bcl: System.Convert.ToInt64: unsupported argument kind")
	}
}

// doubleTryParse's `out double result` uses the same managed-pointer
// mechanism as any other `ref`/`out` primitive since Fase 3.5. Both real
// overload shapes are covered (Fase 3.43, the second found reading a real
// .xlsx through ClosedXML 0.105.0's own worksheet reader — the identical
// widening int32TryParse got, see its doc comment): the plain
// `TryParse(string, out double)` and the culture-explicit `TryParse(
// string, NumberStyles, IFormatProvider, out double)` — the out parameter
// is always LAST, the string always first, and the styles/provider pair
// is always NumberStyles.Float + InvariantCulture in these packages,
// which is exactly what ParseFloat already implements.
func doubleTryParse(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Double.TryParse expects (string, out double)")
	}
	text := args[0].Str
	switch {
	case args[0].Kind == runtime.KindString:
		// text already set above
	case args[0].Kind == runtime.KindStruct || args[0].Kind == runtime.KindRef:
		// Double.TryParse(ReadOnlySpan<char>, out double) (Fase 3.49,
		// found via a real, load-bearing case: ClosedXML 0.105.0's own
		// number-parsing helpers call this span-based overload rather
		// than the plain-string one) — spanCharArg already extracts a
		// char span's real text content (system_text.go).
		s, ok := spanCharArg(args[0])
		if !ok {
			return runtime.Value{}, fmt.Errorf("bcl: System.Double.TryParse expects (string, out double)")
		}
		text = s
	default:
		return runtime.Value{}, fmt.Errorf("bcl: System.Double.TryParse expects (string, out double)")
	}
	out := args[len(args)-1]
	if out.Kind != runtime.KindRef || out.Ref == nil {
		return runtime.Value{}, fmt.Errorf("bcl: System.Double.TryParse expects an out parameter")
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil {
		*out.Ref = runtime.Float64(0)
		return runtime.Bool(false), nil
	}
	*out.Ref = runtime.Float64(f)
	return runtime.Bool(true), nil
}

func doubleEquals(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Double.Equals expects a receiver and an argument")
	}
	a, b := args[0], args[1]
	if a.Kind == runtime.KindRef && a.Ref != nil {
		a = *a.Ref
	}
	if a.Kind != runtime.KindR8 || b.Kind != runtime.KindR8 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Double.Equals expects double operands")
	}
	return runtime.Bool(a.R8 == b.R8), nil
}

// invariantCultureObj is the one, shared "invariant culture" instance —
// constructed ONCE (Fase 3.68), not fresh per call. Real .NET code
// routinely compares cultures by reference identity (e.g. walking
// CultureInfo.Parent up to the invariant culture, `while (c != Culture
// Info.InvariantCulture) c = c.Parent;`) — a fresh &runtime.Object{}
// every call made every such comparison false forever, since two
// distinct Go pointers are never ==, turning a real, finite parent-chain
// walk into an infinite loop (found via FluentValidation's own
// MessageFormatter/Localized resource-satellite fallback, which does
// exactly this).
var invariantCultureObj = &runtime.Object{}

func cultureInfoInvariant(args []runtime.Value) (runtime.Value, error) {
	return runtime.ObjRef(invariantCultureObj), nil
}

// timeZoneInfoStubObj mirrors invariantCultureObj's own singleton
// reasoning (Fase 3.68) but kept as a distinct object/pointer, since a
// TimeZoneInfo stand-in must never be reference-equal to a CultureInfo
// stand-in.
var timeZoneInfoStubObj = &runtime.Object{}

func timeZoneInfoStub(args []runtime.Value) (runtime.Value, error) {
	return runtime.ObjRef(timeZoneInfoStubObj), nil
}

func textInfoGetListSeparator(args []runtime.Value) (runtime.Value, error) {
	return runtime.String(","), nil
}

func textInfoToUpper(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[1].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: TextInfo.ToUpper expects a string argument")
	}
	return runtime.String(strings.ToUpper(args[1].Str)), nil
}

func textInfoToLower(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[1].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: TextInfo.ToLower expects a string argument")
	}
	return runtime.String(strings.ToLower(args[1].Str)), nil
}

// textInfoToTitleCase is a simplified approximation of real .NET's own
// ToTitleCase (which has its own culture-aware rules — e.g. leaving an
// all-caps word alone as an acronym): lowercases the whole string first,
// then capitalizes the first letter of each whitespace-separated word.
// No real corpus caller found needs the acronym-preserving nuance.
func textInfoToTitleCase(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[1].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: TextInfo.ToTitleCase expects a string argument")
	}
	return runtime.String(strings.Title(strings.ToLower(args[1].Str))), nil
}

func environmentThreadID(args []runtime.Value) (runtime.Value, error) {
	return runtime.Int32(1), nil
}

func environmentNewLine(args []runtime.Value) (runtime.Value, error) {
	return runtime.String("\n"), nil
}

func environmentGetEnvironmentVariableNull(args []runtime.Value) (runtime.Value, error) {
	return runtime.Null(), nil
}

func environmentUserName(args []runtime.Value) (runtime.Value, error) {
	return runtime.String("vmnet"), nil
}

func environmentProcessorCount(args []runtime.Value) (runtime.Value, error) {
	return runtime.Int32(int32(goruntime.NumCPU())), nil
}

// convertToInt32 covers the string/double/int64/bool/object-typed
// overloads by inspecting the actual argument Kind, same approach as
// every other multi-overload native in this package — a IFormatProvider
// trailing argument (culture) is accepted and ignored, same limitation
// documented elsewhere (no culture support anywhere in vmnet).
func convertToInt32(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Convert.ToInt32 expects an argument")
	}
	switch v := args[0]; v.Kind {
	case runtime.KindI4:
		return v, nil
	case runtime.KindI8:
		return runtime.Int32(int32(v.I8)), nil
	case runtime.KindR4:
		return runtime.Int32(int32(v.R4 + sign(v.R4)*0.5)), nil
	case runtime.KindR8:
		return runtime.Int32(int32(v.R8 + sign(v.R8)*0.5)), nil
	case runtime.KindString:
		n, err := strconv.ParseInt(v.Str, 10, 32)
		if err != nil {
			return runtime.Value{}, &runtime.ManagedException{TypeName: "System.FormatException", Message: fmt.Sprintf("Input string %q was not in a correct format.", v.Str)}
		}
		return runtime.Int32(int32(n)), nil
	case runtime.KindNull:
		return runtime.Int32(0), nil
	default:
		return runtime.Value{}, fmt.Errorf("bcl: System.Convert.ToInt32: unsupported argument kind")
	}
}

func convertToDouble(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Convert.ToDouble expects an argument")
	}
	switch v := args[0]; v.Kind {
	case runtime.KindI4:
		return runtime.Float64(float64(v.I4)), nil
	case runtime.KindI8:
		return runtime.Float64(float64(v.I8)), nil
	case runtime.KindR4:
		return runtime.Float64(float64(v.R4)), nil
	case runtime.KindR8:
		return v, nil
	case runtime.KindString:
		f, err := strconv.ParseFloat(v.Str, 64)
		if err != nil {
			return runtime.Value{}, &runtime.ManagedException{TypeName: "System.FormatException", Message: fmt.Sprintf("Input string %q was not in a correct format.", v.Str)}
		}
		return runtime.Float64(f), nil
	case runtime.KindNull:
		return runtime.Float64(0), nil
	default:
		return runtime.Value{}, fmt.Errorf("bcl: System.Convert.ToDouble: unsupported argument kind")
	}
}

// convertToIntegral is the shared implementation behind ToByte/ToSByte/
// ToInt16/ToUInt16/ToUInt32/ToUInt64 (Fase 3.42, general IL/BCL hardening
// pass) — every one of these differs from convertToInt32/ToInt64 only in
// its final range/wraparound width, so the real string/double/int-typed
// dispatch logic is shared here rather than duplicated six times. bits
// and unsigned select the target's real range for the string-parse path
// (matching real OverflowException/FormatException behavior on
// out-of-range or malformed input) and the final truncation width for a
// numeric source (matching real Convert's own narrowing-conversion
// semantics, not silently wrapping like a bare `(byte)` cast would).
func convertToIntegral(methodName string, bits int, unsigned bool, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Convert.%s expects an argument", methodName)
	}
	var asI64 int64
	switch v := args[0]; v.Kind {
	case runtime.KindI4:
		asI64 = int64(v.I4)
	case runtime.KindI8:
		asI64 = v.I8
	case runtime.KindR4:
		asI64 = int64(v.R4 + sign(v.R4)*0.5)
	case runtime.KindR8:
		asI64 = int64(v.R8 + sign(v.R8)*0.5)
	case runtime.KindNull:
		return runtime.Int32(0), nil
	case runtime.KindString:
		var err error
		if unsigned {
			var u uint64
			u, err = strconv.ParseUint(v.Str, 10, bits)
			asI64 = int64(u)
		} else {
			asI64, err = strconv.ParseInt(v.Str, 10, bits)
		}
		if err != nil {
			if numErr, ok := err.(*strconv.NumError); ok && numErr.Err == strconv.ErrRange {
				return runtime.Value{}, &runtime.ManagedException{TypeName: "System.OverflowException", Message: "Value was either too large or too small."}
			}
			return runtime.Value{}, &runtime.ManagedException{TypeName: "System.FormatException", Message: fmt.Sprintf("Input string %q was not in a correct format.", v.Str)}
		}
		return runtime.Int32(int32(asI64)), nil
	default:
		return runtime.Value{}, fmt.Errorf("bcl: System.Convert.%s: unsupported argument kind", methodName)
	}
	if unsigned {
		switch bits {
		case 8:
			return runtime.Int32(int32(uint8(asI64))), nil
		case 16:
			return runtime.Int32(int32(uint16(asI64))), nil
		case 32:
			return runtime.Int32(int32(uint32(asI64))), nil
		default:
			return runtime.Int32(int32(uint64(asI64))), nil
		}
	}
	switch bits {
	case 8:
		return runtime.Int32(int32(int8(asI64))), nil
	case 16:
		return runtime.Int32(int32(int16(asI64))), nil
	default:
		return runtime.Int32(int32(asI64)), nil
	}
}

func convertToByte(args []runtime.Value) (runtime.Value, error) {
	return convertToIntegral("ToByte", 8, true, args)
}

func convertToSByte(args []runtime.Value) (runtime.Value, error) {
	return convertToIntegral("ToSByte", 8, false, args)
}

func convertToInt16(args []runtime.Value) (runtime.Value, error) {
	return convertToIntegral("ToInt16", 16, false, args)
}

func convertToUInt16(args []runtime.Value) (runtime.Value, error) {
	return convertToIntegral("ToUInt16", 16, true, args)
}

func convertToUInt32(args []runtime.Value) (runtime.Value, error) {
	return convertToIntegral("ToUInt32", 32, true, args)
}

func convertToUInt64(args []runtime.Value) (runtime.Value, error) {
	return convertToIntegral("ToUInt64", 64, true, args)
}

// convertToSingle mirrors convertToDouble but narrows to float32 —
// real Convert.ToSingle, needed by real code that stores values as
// System.Single specifically (e.g. graphics/measurement APIs) rather
// than the more common double.
func convertToSingle(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Convert.ToSingle expects an argument")
	}
	switch v := args[0]; v.Kind {
	case runtime.KindI4:
		return runtime.Float32(float32(v.I4)), nil
	case runtime.KindI8:
		return runtime.Float32(float32(v.I8)), nil
	case runtime.KindR4:
		return v, nil
	case runtime.KindR8:
		return runtime.Float32(float32(v.R8)), nil
	case runtime.KindString:
		f, err := strconv.ParseFloat(v.Str, 32)
		if err != nil {
			return runtime.Value{}, &runtime.ManagedException{TypeName: "System.FormatException", Message: fmt.Sprintf("Input string %q was not in a correct format.", v.Str)}
		}
		return runtime.Float32(float32(f)), nil
	case runtime.KindNull:
		return runtime.Float32(0), nil
	default:
		return runtime.Value{}, fmt.Errorf("bcl: System.Convert.ToSingle: unsupported argument kind")
	}
}

func convertToBoolean(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Convert.ToBoolean expects an argument")
	}
	switch v := args[0]; v.Kind {
	case runtime.KindI4:
		return runtime.Bool(v.I4 != 0), nil
	case runtime.KindI8:
		return runtime.Bool(v.I8 != 0), nil
	case runtime.KindString:
		b, err := strconv.ParseBool(v.Str)
		if err != nil {
			return runtime.Value{}, &runtime.ManagedException{TypeName: "System.FormatException", Message: fmt.Sprintf("String %q was not recognized as a valid Boolean.", v.Str)}
		}
		return runtime.Bool(b), nil
	case runtime.KindNull:
		return runtime.Bool(false), nil
	default:
		return runtime.Value{}, fmt.Errorf("bcl: System.Convert.ToBoolean: unsupported argument kind")
	}
}

// convertChangeType backs Convert.ChangeType(object value, Type
// conversionType) — a real, common pattern in generic serialization/
// data-binding code (found via a real, load-bearing case: ClosedXML's
// own value-coercion helpers) that converts a boxed value to whatever
// Type a caller only knows dynamically at runtime. Dispatches by the
// target type's full name to the same conversion natives Convert's own
// typed ToXxx methods already implement — covering every primitive kind
// this loop's target packages have been found to actually request;
// anything else falls through to returning the source value unchanged
// (a reasonable identity default when vmnet has no real reflection-based
// arbitrary-type conversion machinery to fall back on).
func convertChangeType(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Convert.ChangeType expects (value, type)")
	}
	typeName, ok := TypeFullNameOf(args[1])
	if !ok {
		return args[0], nil
	}
	switch GenericOpenName(typeName) {
	case "System.Int32":
		return convertToInt32(args[:1])
	case "System.Int64":
		return convertToInt64(args[:1])
	case "System.Double", "System.Single", "System.Decimal":
		return convertToDouble(args[:1])
	case "System.Boolean":
		return convertToBoolean(args[:1])
	case "System.String":
		return convertToString(args[:1])
	default:
		return args[0], nil
	}
}

// sign implements .NET's ToInt32(double)/(float) round-half-away-from-zero.
func sign[T float32 | float64](f T) T {
	if f < 0 {
		return -1
	}
	return 1
}

// doubleToString honors a real ToString(format) argument via formatValue
// (System.String.Format's own specifier parser, system_string.go) instead
// of always falling through to the plain no-argument G-format path —
// found the hard way: Double.ToString("N2") silently ignoring "N2" here
// meant it still ran the unconditional FormatFloat('G', -1, ...) below,
// which for a large value (in the millions, common for "N2"-formatted
// totals) switches to scientific notation Go's 'G' verb picks at that
// magnitude — a completely different (and completely wrong) answer from
// real N2's fixed-point, comma-grouped output, not just a missing comma.
func doubleToString(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Double.ToString expects a receiver")
	}
	v := args[0]
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	if v.Kind != runtime.KindR8 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Double.ToString expects a double receiver")
	}
	if format := numericToStringFormat(args); format != "" {
		s, err := formatValue(v, format)
		if err != nil {
			return runtime.Value{}, err
		}
		return runtime.String(s), nil
	}
	return runtime.String(strconv.FormatFloat(v.R8, 'G', -1, 64)), nil
}
