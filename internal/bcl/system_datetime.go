package bcl

import (
	"fmt"
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
}

func dateTimeIdentity(args []runtime.Value) (runtime.Value, error) {
	t, _, err := asDateTime(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return dateTimeFromTime(t), nil
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

func dateTimeToString(args []runtime.Value) (runtime.Value, error) {
	t, _, err := asDateTime(args)
	if err != nil {
		return runtime.Value{}, err
	}
	// A fixed, culture-invariant format — real DateTime.ToString() is
	// culture/format-string driven, which vmnet doesn't model (no culture
	// support anywhere else either — CultureInfo is a stub, Fase 3.6).
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
