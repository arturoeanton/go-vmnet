package bcl

import (
	"fmt"
	"time"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// dateTimeOffsetType models System.DateTimeOffset as (ticks, offsetTicks)
// — ticks is the UTC instant (same 100ns-tick representation as
// DateTime/TimeSpan), offsetTicks the UTC offset that was supplied at
// construction, kept only so get_Offset/get_DateTime could report it
// back (real .NET stores exactly this pair internally too).
var dateTimeOffsetType = runtime.NewValueType(
	"System", "DateTimeOffset",
	[]string{"ticks", "offsetTicks"},
	[]runtime.Value{runtime.Int64(0), runtime.Int64(0)},
)

func init() {
	registerValueType(dateTimeOffsetType)
	registerValueTypeCtor("System.DateTimeOffset", dateTimeOffsetCtor)
	register("System.DateTimeOffset::.ctor", false, dateTimeOffsetCtorInPlace)
	register("System.DateTimeOffset::get_UtcDateTime", true, dateTimeOffsetGetUtcDateTime)
	register("System.DateTimeOffset::get_DateTime", true, dateTimeOffsetGetDateTime)
	register("System.DateTimeOffset::get_Offset", true, dateTimeOffsetGetOffset)
	register("System.DateTimeOffset::get_Ticks", true, dateTimeOffsetGetTicks)
}

// dateTimeOffsetCtor covers (year,month,day,hour,minute,second,TimeSpan
// offset) — the overwhelmingly common real overload — and (DateTime,
// TimeSpan offset). The date/time components are interpreted as
// *local* (offset-relative) time, matching real DateTimeOffset — the
// stored ticks field is the UTC instant, components minus offset.
func dateTimeOffsetCtor(args []runtime.Value) (*runtime.Struct, error) {
	s := runtime.NewStruct(dateTimeOffsetType)
	switch {
	case len(args) == 7:
		ints := make([]int, 6)
		for i := 0; i < 6; i++ {
			if args[i].Kind != runtime.KindI4 {
				return nil, fmt.Errorf("bcl: DateTimeOffset(...) expects int32 components")
			}
			ints[i] = int(args[i].I4)
		}
		offsetTicks, err := timeSpanArgTicks(args[6])
		if err != nil {
			return nil, err
		}
		local := time.Date(ints[0], time.Month(ints[1]), ints[2], ints[3], ints[4], ints[5], 0, time.UTC)
		localTicks := timeToTicks(local)
		s.Fields[0] = runtime.Int64(localTicks - offsetTicks)
		s.Fields[1] = runtime.Int64(offsetTicks)
	case len(args) == 2:
		dt, err := timeSpanArgTicks(args[0]) // reuse: DateTime shares the same struct-with-ticks shape
		if err != nil {
			return nil, err
		}
		offsetTicks, err := timeSpanArgTicks(args[1])
		if err != nil {
			return nil, err
		}
		s.Fields[0] = runtime.Int64(dt - offsetTicks)
		s.Fields[1] = runtime.Int64(offsetTicks)
	default:
		return nil, fmt.Errorf("bcl: unsupported DateTimeOffset constructor arity %d", len(args))
	}
	return s, nil
}

// timeSpanArgTicks reads the "ticks" field off any DateTime/TimeSpan-
// shaped struct argument (both are a single int64 ticks field at index
// 0 — DateTime just carries a second "kind" field alongside it,
// unused here).
func timeSpanArgTicks(v runtime.Value) (int64, error) {
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	if v.Kind != runtime.KindStruct || v.Struct == nil || len(v.Struct.Fields) == 0 {
		return 0, fmt.Errorf("bcl: DateTimeOffset constructor: expected a DateTime/TimeSpan argument")
	}
	return v.Struct.Fields[0].I8, nil
}

func dateTimeOffsetCtorInPlace(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindRef || args[0].Ref == nil {
		return runtime.Value{}, fmt.Errorf("bcl: DateTimeOffset constructor called without a receiver")
	}
	s, err := dateTimeOffsetCtor(args[1:])
	if err != nil {
		return runtime.Value{}, err
	}
	*args[0].Ref = runtime.StructVal(s)
	return runtime.Value{}, nil
}

func asDateTimeOffset(args []runtime.Value) (*runtime.Struct, error) {
	return derefStructReceiver(args, "DateTimeOffset", "DateTimeOffset method")
}

func dateTimeOffsetGetUtcDateTime(args []runtime.Value) (runtime.Value, error) {
	s, err := asDateTimeOffset(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return dateTimeFromTimeKind(ticksToTime(s.Fields[0].I8), dateTimeKindUtc), nil
}

func dateTimeOffsetGetDateTime(args []runtime.Value) (runtime.Value, error) {
	s, err := asDateTimeOffset(args)
	if err != nil {
		return runtime.Value{}, err
	}
	localTicks := s.Fields[0].I8 + s.Fields[1].I8
	return dateTimeFromTimeKind(ticksToTime(localTicks), dateTimeKindUnspecified), nil
}

func dateTimeOffsetGetOffset(args []runtime.Value) (runtime.Value, error) {
	s, err := asDateTimeOffset(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return timeSpanFromTicks(s.Fields[1].I8), nil
}

func dateTimeOffsetGetTicks(args []runtime.Value) (runtime.Value, error) {
	s, err := asDateTimeOffset(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return s.Fields[0], nil
}
