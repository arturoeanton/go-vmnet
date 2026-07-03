package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// timeSpanType models System.TimeSpan as a single int64 "ticks" field —
// 100-nanosecond intervals, matching the CLR's own representation
// exactly (same reasoning as System.DateTime, Fase 3.12).
var timeSpanType = runtime.NewValueType(
	"System", "TimeSpan",
	[]string{"ticks"},
	[]runtime.Value{runtime.Int64(0)},
)

const (
	ticksPerMillisecond int64 = 10_000
	ticksPerSecond            = 1000 * ticksPerMillisecond
	ticksPerMinute            = 60 * ticksPerSecond
	ticksPerHour              = 60 * ticksPerMinute
	ticksPerDay               = 24 * ticksPerHour
)

func init() {
	registerValueType(timeSpanType)
	registerValueTypeCtor("System.TimeSpan", timeSpanCtor)
	// `var ts = new TimeSpan(...)` assigned straight to a local compiles
	// as `ldloca`+`call .ctor`, not `newobj` — the same compiler
	// optimization already needing its own entry point for
	// System.DateTime (Fase 3.12) and System.Nullable`1 (Fase 3.13).
	register("System.TimeSpan::.ctor", false, timeSpanCtorInPlace)

	register("System.TimeSpan::FromDays", true, timeSpanFrom(ticksPerDay))
	register("System.TimeSpan::FromHours", true, timeSpanFrom(ticksPerHour))
	register("System.TimeSpan::FromMinutes", true, timeSpanFrom(ticksPerMinute))
	register("System.TimeSpan::FromSeconds", true, timeSpanFrom(ticksPerSecond))
	register("System.TimeSpan::FromMilliseconds", true, timeSpanFrom(ticksPerMillisecond))

	register("System.TimeSpan::get_Ticks", true, timeSpanField(func(t int64) int64 { return t }))
	register("System.TimeSpan::get_Days", true, timeSpanIntField(func(t int64) int32 { return int32(t / ticksPerDay) }))
	register("System.TimeSpan::get_Hours", true, timeSpanIntField(func(t int64) int32 { return int32(t / ticksPerHour % 24) }))
	register("System.TimeSpan::get_Minutes", true, timeSpanIntField(func(t int64) int32 { return int32(t / ticksPerMinute % 60) }))
	register("System.TimeSpan::get_Seconds", true, timeSpanIntField(func(t int64) int32 { return int32(t / ticksPerSecond % 60) }))
	register("System.TimeSpan::get_Milliseconds", true, timeSpanIntField(func(t int64) int32 { return int32(t / ticksPerMillisecond % 1000) }))

	register("System.TimeSpan::get_TotalDays", true, timeSpanTotalField(ticksPerDay))
	register("System.TimeSpan::get_TotalHours", true, timeSpanTotalField(ticksPerHour))
	register("System.TimeSpan::get_TotalMinutes", true, timeSpanTotalField(ticksPerMinute))
	register("System.TimeSpan::get_TotalSeconds", true, timeSpanTotalField(ticksPerSecond))
	register("System.TimeSpan::get_TotalMilliseconds", true, timeSpanTotalField(ticksPerMillisecond))
}

// timeSpanCtor covers (ticks), (hours,minutes,seconds), (days,hours,
// minutes,seconds), (days,hours,minutes,seconds,milliseconds) —
// distinguished by argument count/kind, same approach as DateTime's ctor.
func timeSpanCtor(args []runtime.Value) (*runtime.Struct, error) {
	s := runtime.NewStruct(timeSpanType)
	switch len(args) {
	case 1:
		if args[0].Kind != runtime.KindI8 {
			return nil, fmt.Errorf("bcl: TimeSpan(ticks) expects an int64")
		}
		s.Fields[0] = args[0]
	case 3, 4, 5:
		ints := make([]int64, len(args))
		for i, a := range args {
			if a.Kind != runtime.KindI4 {
				return nil, fmt.Errorf("bcl: TimeSpan(...) expects int32 components")
			}
			ints[i] = int64(a.I4)
		}
		var days, hours, minutes, seconds, millis int64
		switch len(ints) {
		case 3:
			hours, minutes, seconds = ints[0], ints[1], ints[2]
		case 4:
			days, hours, minutes, seconds = ints[0], ints[1], ints[2], ints[3]
		case 5:
			days, hours, minutes, seconds, millis = ints[0], ints[1], ints[2], ints[3], ints[4]
		}
		ticks := days*ticksPerDay + hours*ticksPerHour + minutes*ticksPerMinute + seconds*ticksPerSecond + millis*ticksPerMillisecond
		s.Fields[0] = runtime.Int64(ticks)
	default:
		return nil, fmt.Errorf("bcl: unsupported TimeSpan constructor arity %d", len(args))
	}
	return s, nil
}

func timeSpanCtorInPlace(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindRef || args[0].Ref == nil {
		return runtime.Value{}, fmt.Errorf("bcl: TimeSpan constructor called without a receiver")
	}
	s, err := timeSpanCtor(args[1:])
	if err != nil {
		return runtime.Value{}, err
	}
	*args[0].Ref = runtime.StructVal(s)
	return runtime.Value{}, nil
}

func timeSpanFromTicks(ticks int64) runtime.Value {
	s := runtime.NewStruct(timeSpanType)
	s.Fields[0] = runtime.Int64(ticks)
	return runtime.StructVal(s)
}

func timeSpanFrom(ticksPerUnit int64) Native {
	return func(args []runtime.Value) (runtime.Value, error) {
		if len(args) < 1 {
			return runtime.Value{}, fmt.Errorf("bcl: TimeSpan.From*: expects a numeric argument")
		}
		f, ok := valueAsFloat64(args[0])
		if !ok {
			if i, ok2 := valueAsInt64(args[0]); ok2 {
				f = float64(i)
			} else {
				return runtime.Value{}, fmt.Errorf("bcl: TimeSpan.From*: unsupported argument kind")
			}
		}
		return timeSpanFromTicks(int64(f * float64(ticksPerUnit))), nil
	}
}

func asTimeSpanTicks(args []runtime.Value) (int64, error) {
	s, err := derefStructReceiver(args, "TimeSpan", "TimeSpan method")
	if err != nil {
		return 0, err
	}
	return s.Fields[0].I8, nil
}

func timeSpanField(get func(int64) int64) Native {
	return func(args []runtime.Value) (runtime.Value, error) {
		ticks, err := asTimeSpanTicks(args)
		if err != nil {
			return runtime.Value{}, err
		}
		return runtime.Int64(get(ticks)), nil
	}
}

func timeSpanIntField(get func(int64) int32) Native {
	return func(args []runtime.Value) (runtime.Value, error) {
		ticks, err := asTimeSpanTicks(args)
		if err != nil {
			return runtime.Value{}, err
		}
		return runtime.Int32(get(ticks)), nil
	}
}

func timeSpanTotalField(ticksPerUnit int64) Native {
	return func(args []runtime.Value) (runtime.Value, error) {
		ticks, err := asTimeSpanTicks(args)
		if err != nil {
			return runtime.Value{}, err
		}
		return runtime.Float64(float64(ticks) / float64(ticksPerUnit)), nil
	}
}
