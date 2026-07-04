package bcl

import (
	"fmt"
	"strings"
	"time"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// dateTimeType models System.DateTime as a single int64 "ticks" field —
// 100-nanosecond intervals since 0001-01-01 00:00:00, matching the CLR's
// own internal representation closely enough that every other member
// (Year, AddDays, ...) is just arithmetic over it via Go's time.Time,
// converting at the boundary rather than reimplementing calendar math.
// "kind" is a System.DateTimeKind (Unspecified=0/Utc=1/Local=2) — only
// get_Now/get_UtcNow/get_Today set it to anything but the Unspecified
// default (matching real DateTime: an explicit constructor never infers
// a kind), since that's the only place vmnet has a real Utc-vs-local
// distinction to report at all (Fase 3.21).
var dateTimeType = runtime.NewValueType(
	"System", "DateTime",
	[]string{"ticks", "kind"},
	[]runtime.Value{runtime.Int64(0), runtime.Int32(0)},
)

const (
	dateTimeKindUnspecified int32 = 0
	dateTimeKindUtc         int32 = 1
	dateTimeKindLocal       int32 = 2
)

// unixEpochTicks is DateTime(1970,1,1).Ticks — the well-known constant
// relating .NET's tick epoch (0001-01-01) to Unix's (1970-01-01).
//
// Converting via a duration from the .NET epoch (dotnetEpoch.Add(...) /
// t.Sub(dotnetEpoch), the first version of this code) is wrong for any
// real date: time.Duration is an int64 count of *nanoseconds*, which only
// covers about ±292 years — a .NET epoch that's ~2000 years in the past
// overflows it immediately (Go's Sub silently clamps to
// math.MaxInt64/MinInt64 rather than erroring, which is how this shipped
// broken and was only caught by actually computing a real date, not
// assumed correct from the arithmetic looking right on paper). Anchoring
// on Unix seconds instead avoids the bound entirely: time.Unix/t.Unix()
// take/return a plain int64 of *seconds*, valid across the CLR's whole
// year 1-9999 range with room to spare.
const unixEpochTicks = 621355968000000000

func ticksToTime(ticks int64) time.Time {
	rel := ticks - unixEpochTicks
	secs := rel / 10_000_000
	subTicks := rel % 10_000_000
	return time.Unix(secs, subTicks*100).UTC()
}

func timeToTicks(t time.Time) int64 {
	return unixEpochTicks + t.Unix()*10_000_000 + int64(t.Nanosecond())/100
}

func init() {
	registerValueType(dateTimeType)
	registerValueTypeCtor("System.DateTime", dateTimeCtor)
	// A `new DateTime(...)` assigned straight to a local compiles as
	// `ldloca`+`call .ctor` directly on the local's (already zeroed)
	// storage, bypassing newobj/registerValueTypeCtor entirely — same
	// optimization already confirmed for plugin-defined structs in Fase
	// 3.7, just needing its own entry point here since a native value
	// type has no single interpreted .ctor method to naturally handle
	// both call shapes. Found via the first real DateTime construction
	// (Fase 3.12), not assumed.
	register("System.DateTime::.ctor", false, dateTimeCtorInPlace)

	register("System.DateTime::get_Now", true, dateTimeNow)
	register("System.DateTime::get_UtcNow", true, dateTimeUtcNow)
	register("System.DateTime::get_Today", true, dateTimeToday)

	register("System.DateTime::get_Year", true, dateTimeField(func(t time.Time) int32 { return int32(t.Year()) }))
	register("System.DateTime::get_Month", true, dateTimeField(func(t time.Time) int32 { return int32(t.Month()) }))
	register("System.DateTime::get_Day", true, dateTimeField(func(t time.Time) int32 { return int32(t.Day()) }))
	register("System.DateTime::get_Hour", true, dateTimeField(func(t time.Time) int32 { return int32(t.Hour()) }))
	register("System.DateTime::get_Minute", true, dateTimeField(func(t time.Time) int32 { return int32(t.Minute()) }))
	register("System.DateTime::get_Second", true, dateTimeField(func(t time.Time) int32 { return int32(t.Second()) }))
	register("System.DateTime::get_Millisecond", true, dateTimeField(func(t time.Time) int32 { return int32(t.Nanosecond() / 1_000_000) }))
	register("System.DateTime::get_DayOfYear", true, dateTimeField(func(t time.Time) int32 { return int32(t.YearDay()) }))
	register("System.DateTime::get_DayOfWeek", true, dateTimeField(func(t time.Time) int32 { return int32(t.Weekday()) }))

	register("System.DateTime::get_Ticks", true, dateTimeGetTicks)
	register("System.DateTime::get_Kind", true, dateTimeGetKind)
	register("System.DateTime::get_Date", true, dateTimeGetDate)

	register("System.DateTime::AddDays", true, dateTimeAdd(func(f float64) time.Duration { return time.Duration(f * float64(24*time.Hour)) }))
	register("System.DateTime::AddHours", true, dateTimeAdd(func(f float64) time.Duration { return time.Duration(f * float64(time.Hour)) }))
	register("System.DateTime::AddMinutes", true, dateTimeAdd(func(f float64) time.Duration { return time.Duration(f * float64(time.Minute)) }))
	register("System.DateTime::AddSeconds", true, dateTimeAdd(func(f float64) time.Duration { return time.Duration(f * float64(time.Second)) }))
	register("System.DateTime::AddMilliseconds", true, dateTimeAdd(func(f float64) time.Duration { return time.Duration(f * float64(time.Millisecond)) }))
	register("System.DateTime::AddYears", true, dateTimeAddCalendar(0, 1, 0))
	register("System.DateTime::AddMonths", true, dateTimeAddCalendar(0, 0, 1))

	register("System.DateTime::ToString", true, dateTimeToString)
	register("System.DateTime::CompareTo", true, dateTimeCompareTo)
	register("System.DateTime::Equals", true, dateTimeEquals)
	register("System.DateTime::op_Equality", true, dateTimeEquals)
	register("System.DateTime::op_Inequality", true, dateTimeNotEquals)
	register("System.DateTime::op_Subtraction", true, dateTimeSubtract)
	// vmnet has no real local-timezone concept (see Environment.NewLine's
	// same "no host OS to consult" reasoning, Fase 3.18) — ToUniversalTime
	// is the identity function, consistent with every DateTime already
	// effectively being UTC/unspecified internally.
	register("System.DateTime::ToUniversalTime", true, dateTimeIdentity)
	register("System.DateTime::ToLocalTime", true, dateTimeIdentity)
	register("System.DateTime::ParseExact", true, dateTimeParseExact)
	register("System.DateTime::TryParseExact", true, dateTimeTryParseExact)
}

func dateTimeIdentity(args []runtime.Value) (runtime.Value, error) {
	t, _, err := asDateTime(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return dateTimeFromTime(t), nil
}

// netDateFormatToGoLayout translates a .NET custom date/time format
// string (ECMA-335 has no metadata for this — it's just a runtime
// string) to the equivalent Go reference-time layout — needed since Fase
// 3.40: System.IO.Packaging's own PartBasedPackageProperties parses OPC
// "W3CDTF" dates via DateTime.ParseExact(s, "yyyy-MM-ddTHH:mm:ss.fffffffZ",
// ...). Only the handful of specifiers real callers in this loop's
// target packages actually use are covered — longest-token-first so
// "yyyy" isn't partially consumed as "yy"+"yy". Any character not one of
// these tokens (T, Z, -, :, ., ' ') passes through literally, matching
// both formats' own convention for literal separators.
func netDateFormatToGoLayout(format string) string {
	replacer := strings.NewReplacer(
		"yyyy", "2006",
		"yy", "06",
		"MM", "01",
		"dd", "02",
		"HH", "15",
		"hh", "03",
		"mm", "04",
		"ss", "05",
		"fffffff", "0000000",
		"ffffff", "000000",
		"fffff", "00000",
		"ffff", "0000",
		"fff", "000",
		"ff", "00",
		"f", "0",
		"tt", "PM",
	)
	return replacer.Replace(format)
}

func dateTimeParseFormats(args []runtime.Value) (s string, layouts []string, ok bool) {
	if len(args) < 2 || args[0].Kind != runtime.KindString {
		return "", nil, false
	}
	s = args[0].Str
	switch args[1].Kind {
	case runtime.KindString:
		layouts = []string{netDateFormatToGoLayout(args[1].Str)}
	case runtime.KindArray:
		if args[1].Arr == nil {
			return "", nil, false
		}
		for _, e := range args[1].Arr.Elems {
			if e.Kind == runtime.KindString {
				layouts = append(layouts, netDateFormatToGoLayout(e.Str))
			}
		}
	default:
		return "", nil, false
	}
	return s, layouts, true
}

func dateTimeParseExact(args []runtime.Value) (runtime.Value, error) {
	s, layouts, ok := dateTimeParseFormats(args)
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: DateTime.ParseExact expects (string, string-or-string[], ...)")
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return dateTimeFromTime(t), nil
		}
	}
	return runtime.Value{}, &runtime.ManagedException{TypeName: "System.FormatException", Message: fmt.Sprintf("String '%s' was not recognized as a valid DateTime.", s)}
}

func dateTimeTryParseExact(args []runtime.Value) (runtime.Value, error) {
	s, layouts, ok := dateTimeParseFormats(args)
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: DateTime.TryParseExact expects (string, string-or-string[], ...)")
	}
	out := args[len(args)-1]
	if out.Kind != runtime.KindRef || out.Ref == nil {
		return runtime.Value{}, fmt.Errorf("bcl: DateTime.TryParseExact expects an out parameter")
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			*out.Ref = dateTimeFromTime(t)
			return runtime.Bool(true), nil
		}
	}
	*out.Ref = dateTimeFromTime(time.Time{})
	return runtime.Bool(false), nil
}

func dateTimeNotEquals(args []runtime.Value) (runtime.Value, error) {
	v, err := dateTimeEquals(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(!v.Truthy()), nil
}

// dateTimeSubtract backs DateTime::op_Subtraction(DateTime,DateTime),
// returning a TimeSpan (defined in system_timespan.go, Fase 3.19) — the
// tick difference maps directly since both share the same 100ns-tick
// representation.
func dateTimeSubtract(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: DateTime.op_Subtraction expects 2 DateTime arguments")
	}
	a, _, err := asDateTime(args[0:1])
	if err != nil {
		return runtime.Value{}, err
	}
	b, _, err := asDateTime(args[1:2])
	if err != nil {
		return runtime.Value{}, err
	}
	return timeSpanFromTicks(timeToTicks(a) - timeToTicks(b)), nil
}

// dateTimeCtor covers the fixed-arity overloads real code actually uses:
// (ticks), (year, month, day), (year, month, day, hour, minute, second).
// Distinguished by argument count, same approach as every other
// multi-overload native in this package.
func dateTimeCtor(args []runtime.Value) (*runtime.Struct, error) {
	s := runtime.NewStruct(dateTimeType)
	switch len(args) {
	case 1:
		if args[0].Kind != runtime.KindI8 {
			return nil, fmt.Errorf("bcl: DateTime(ticks) expects an int64")
		}
		s.Fields[0] = args[0]
	case 3, 6:
		ints := make([]int, len(args))
		for i, a := range args {
			if a.Kind != runtime.KindI4 {
				return nil, fmt.Errorf("bcl: DateTime(...) expects int32 components")
			}
			ints[i] = int(a.I4)
		}
		hour, min, sec := 0, 0, 0
		if len(ints) == 6 {
			hour, min, sec = ints[3], ints[4], ints[5]
		}
		t := time.Date(ints[0], time.Month(ints[1]), ints[2], hour, min, sec, 0, time.UTC)
		s.Fields[0] = runtime.Int64(timeToTicks(t))
	default:
		return nil, fmt.Errorf("bcl: unsupported DateTime constructor arity %d", len(args))
	}
	return s, nil
}

// dateTimeCtorInPlace backs the `ldloca`+`call .ctor` shape (see the
// registration comment above): args[0] is a managed pointer to the
// already-zeroed DateTime local, not a fresh allocation to return.
func dateTimeCtorInPlace(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindRef || args[0].Ref == nil {
		return runtime.Value{}, fmt.Errorf("bcl: DateTime constructor called without a receiver")
	}
	s, err := dateTimeCtor(args[1:])
	if err != nil {
		return runtime.Value{}, err
	}
	*args[0].Ref = runtime.StructVal(s)
	return runtime.Value{}, nil
}

func dateTimeFromTime(t time.Time) runtime.Value {
	return dateTimeFromTimeKind(t, dateTimeKindUnspecified)
}

func dateTimeFromTimeKind(t time.Time, kind int32) runtime.Value {
	s := runtime.NewStruct(dateTimeType)
	s.Fields[0] = runtime.Int64(timeToTicks(t))
	s.Fields[1] = runtime.Int32(kind)
	return runtime.StructVal(s)
}

func dateTimeNow(args []runtime.Value) (runtime.Value, error) {
	return dateTimeFromTimeKind(time.Now(), dateTimeKindLocal), nil
}

func dateTimeUtcNow(args []runtime.Value) (runtime.Value, error) {
	return dateTimeFromTimeKind(time.Now().UTC(), dateTimeKindUtc), nil
}

func dateTimeToday(args []runtime.Value) (runtime.Value, error) {
	now := time.Now()
	return dateTimeFromTimeKind(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()), dateTimeKindLocal), nil
}

func asDateTime(args []runtime.Value) (time.Time, *runtime.Struct, error) {
	s, err := derefStructReceiver(args, "DateTime", "DateTime method")
	if err != nil {
		return time.Time{}, nil, err
	}
	return ticksToTime(s.Fields[0].I8), s, nil
}

func dateTimeField(get func(time.Time) int32) Native {
	return func(args []runtime.Value) (runtime.Value, error) {
		t, _, err := asDateTime(args)
		if err != nil {
			return runtime.Value{}, err
		}
		return runtime.Int32(get(t)), nil
	}
}

func dateTimeGetTicks(args []runtime.Value) (runtime.Value, error) {
	_, s, err := asDateTime(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return s.Fields[0], nil
}

func dateTimeGetKind(args []runtime.Value) (runtime.Value, error) {
	_, s, err := asDateTime(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return s.Fields[1], nil
}

func dateTimeGetDate(args []runtime.Value) (runtime.Value, error) {
	t, _, err := asDateTime(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return dateTimeFromTime(time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())), nil
}

func dateTimeAdd(delta func(float64) time.Duration) Native {
	return func(args []runtime.Value) (runtime.Value, error) {
		t, _, err := asDateTime(args)
		if err != nil {
			return runtime.Value{}, err
		}
		if len(args) < 2 {
			return runtime.Value{}, fmt.Errorf("bcl: DateTime.Add* expects a numeric argument")
		}
		f, ok := valueAsFloat64(args[1])
		if !ok {
			if i, ok2 := valueAsInt64(args[1]); ok2 {
				f, ok = float64(i), true
			}
		}
		if !ok {
			return runtime.Value{}, fmt.Errorf("bcl: DateTime.Add*: unsupported argument kind")
		}
		return dateTimeFromTime(t.Add(delta(f))), nil
	}
}

func dateTimeAddCalendar(years, months, days int) Native {
	return func(args []runtime.Value) (runtime.Value, error) {
		t, _, err := asDateTime(args)
		if err != nil {
			return runtime.Value{}, err
		}
		if len(args) < 2 || args[1].Kind != runtime.KindI4 {
			return runtime.Value{}, fmt.Errorf("bcl: DateTime.AddYears/AddMonths expects an int32 argument")
		}
		n := int(args[1].I4)
		return dateTimeFromTime(t.AddDate(years*n, months*n, days*n)), nil
	}
}

// dateTimeStandardFormats maps DateTime.ToString's single-letter standard
// format specifiers (not to be confused with a custom pattern like
// "yyyy-MM-dd" — real .NET tells them apart the same way formatValue's
// own isStandardSpecifierShape does: a bare recognized letter with no
// other characters) directly to the equivalent Go reference-time layout.
// Real values are culture-dependent (this is the en-US-shaped default —
// consistent with every other culture-sensitive native in this project,
// e.g. formatValue's own "C" currency using "$": no culture support
// anywhere, CultureInfo is a stub, Fase 3.6). "K" (the round-trip
// specifiers' own timezone-offset-or-Z suffix) is omitted: every
// DateTime here is effectively Unspecified/UTC internally (Fase 3.21),
// so there is no real offset to print beyond a literal "Z" for the
// Utc-labeled formats.
var dateTimeStandardFormats = map[byte]string{
	'd': "1/2/2006",
	'D': "Monday, January 2, 2006",
	't': "3:04 PM",
	'T': "3:04:05 PM",
	'f': "Monday, January 2, 2006 3:04 PM",
	'F': "Monday, January 2, 2006 3:04:05 PM",
	'g': "1/2/2006 3:04 PM",
	'G': "1/2/2006 3:04:05 PM",
	's': "2006-01-02T15:04:05",
	'u': "2006-01-02 15:04:05Z",
	'o': "2006-01-02T15:04:05.0000000",
	'O': "2006-01-02T15:04:05.0000000",
	'r': "Mon, 02 Jan 2006 15:04:05 GMT",
	'R': "Mon, 02 Jan 2006 15:04:05 GMT",
}

// dateTimeToString honors a real ToString(format) argument: a standard
// single-letter specifier via dateTimeStandardFormats, or a custom
// pattern (e.g. "yyyy-MM-dd HH:mm:ss") via netDateFormatToGoLayout — the
// same translator ParseExact already uses, just running in the other
// direction (Format instead of Parse). Falls back to the original fixed
// culture-invariant default when no format argument is given at all.
func dateTimeToString(args []runtime.Value) (runtime.Value, error) {
	t, _, err := asDateTime(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if format := numericToStringFormat(args); format != "" {
		if len(format) == 1 {
			if layout, ok := dateTimeStandardFormats[format[0]]; ok {
				return runtime.String(t.Format(layout)), nil
			}
		}
		return runtime.String(t.Format(netDateFormatToGoLayout(format))), nil
	}
	// A fixed, culture-invariant format when no format argument is given
	// at all — real DateTime.ToString() defaults to CurrentCulture's own
	// general format, which vmnet doesn't model (no culture support
	// anywhere else either — CultureInfo is a stub, Fase 3.6).
	return runtime.String(t.Format("01/02/2006 15:04:05")), nil
}

func dateTimeCompareTo(args []runtime.Value) (runtime.Value, error) {
	a, _, err := asDateTime(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: DateTime.CompareTo expects a DateTime argument")
	}
	b, _, err := asDateTime(args[1:2])
	if err != nil {
		return runtime.Value{}, err
	}
	switch {
	case a.Before(b):
		return runtime.Int32(-1), nil
	case a.After(b):
		return runtime.Int32(1), nil
	default:
		return runtime.Int32(0), nil
	}
}

func dateTimeEquals(args []runtime.Value) (runtime.Value, error) {
	a, _, err := asDateTime(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: DateTime.Equals expects a DateTime argument")
	}
	b, _, err := asDateTime(args[1:2])
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(a.Equal(b)), nil
}
