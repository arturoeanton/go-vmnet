package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// nativeStringBuilder backs System.Text.StringBuilder. Every Append/
// Insert overload (string/char/int/bool/object/...) collapses to the
// same native (see resolveCallTarget in internal/ir/builder.go — call
// targets aren't disambiguated by parameter type), so a `char` argument
// stringifies as its numeric code point rather than the character, same
// documented limitation String.Concat already has for boxed non-string
// args.
//
// buf is a plain string, not a strings.Builder (Fase 3.39) — Insert
// needs to splice content at an arbitrary rune position, which
// strings.Builder's append-only design can't do at all. Found via a
// real, load-bearing case: NPOI's own formula-rendering code (building
// up a formula string token-by-token, e.g. prefixing an operator) calls
// StringBuilder.Insert directly. Rebuilding the string on every Append/
// Insert is O(n) instead of amortized O(1), but every real StringBuilder
// found in this loop's target packages stays small (a single formula or
// short XML fragment), never the kind of large streaming-append use case
// strings.Builder exists for.
type nativeStringBuilder struct {
	buf string
}

func init() {
	registerCtor("System.Text.StringBuilder", func(args []runtime.Value) (*runtime.Object, error) {
		sb := &nativeStringBuilder{}
		// The string-seeded overload (`new StringBuilder(initial)`) is the
		// only one that needs the ctor argument itself — the capacity-int
		// overload is a pure allocation hint, nothing to apply here.
		if len(args) > 0 && args[0].Kind == runtime.KindString {
			sb.buf = args[0].Str
		}
		return &runtime.Object{Native: sb}, nil
	})
	register("System.Text.StringBuilder::Append", true, sbAppend)
	register("System.Text.StringBuilder::AppendLine", true, sbAppendLine)
	register("System.Text.StringBuilder::AppendFormat", true, sbAppendFormat)
	register("System.Text.StringBuilder::Insert", true, sbInsert)
	register("System.Text.StringBuilder::ToString", true, sbToString)
	register("System.Text.StringBuilder::get_Length", true, sbLength)
	register("System.Text.StringBuilder::Clear", true, sbClear)
	// Capacity: vmnet's StringBuilder auto-grows with no separate,
	// meaningfully distinct notion of "allocated but unused" capacity to
	// report — the current length is a defensible stand-in (real
	// Capacity is always >= Length, and EnsureCapacity's usual caller
	// just wants "big enough", which the auto-growing backing already
	// guarantees regardless of what this returns).
	register("System.Text.StringBuilder::get_Capacity", true, sbLength)
	register("System.Text.StringBuilder::EnsureCapacity", true, sbLength)
}

func asStringBuilder(args []runtime.Value) (*nativeStringBuilder, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return nil, fmt.Errorf("bcl: StringBuilder method called without a receiver")
	}
	sb, ok := args[0].Obj.Native.(*nativeStringBuilder)
	if !ok {
		return nil, fmt.Errorf("bcl: receiver is not a StringBuilder")
	}
	return sb, nil
}

// sbAppend and sbAppendLine return the receiver (args[0]) so fluent
// chaining (`sb.Append(x).Append(y)`) works exactly like real
// StringBuilder.
func sbAppend(args []runtime.Value) (runtime.Value, error) {
	sb, err := asStringBuilder(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) == 2 {
		sb.buf += displayString(args[1])
	}
	return args[0], nil
}

func sbAppendLine(args []runtime.Value) (runtime.Value, error) {
	sb, err := asStringBuilder(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) == 2 {
		sb.buf += displayString(args[1])
	}
	sb.buf += "\n"
	return args[0], nil
}

// sbInsert backs Insert(int index, value) — index is a rune position
// (matching get_Length's own rune-count semantics), not a byte offset.
func sbInsert(args []runtime.Value) (runtime.Value, error) {
	sb, err := asStringBuilder(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 3 || args[1].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: StringBuilder.Insert expects (int index, value)")
	}
	idx := int(args[1].I4)
	current := []rune(sb.buf)
	if idx < 0 || idx > len(current) {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "Index and length must refer to a location within the string."}
	}
	inserted := displayString(args[2])
	sb.buf = string(current[:idx]) + inserted + string(current[idx:])
	return args[0], nil
}

// sbAppendFormat mirrors System.String.Format's own overload handling
// (stringFormat, system_string.go) exactly — same composite-format
// grammar (formatComposite), same optional leading IFormatProvider
// argument (ignored, no culture support anywhere else either), same
// `params object[]` collapse for a 4th-or-later substitution value —
// just appended to the buffer instead of returned as a new string.
// Returns the receiver so `sb.Append(...).AppendFormat(...)` fluent
// chaining keeps working, like every other StringBuilder method here.
func sbAppendFormat(args []runtime.Value) (runtime.Value, error) {
	sb, err := asStringBuilder(args)
	if err != nil {
		return runtime.Value{}, err
	}
	rest := args[1:]
	if len(rest) >= 2 && rest[0].Kind != runtime.KindString && rest[1].Kind == runtime.KindString {
		rest = rest[1:]
	}
	if len(rest) < 1 || rest[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: StringBuilder.AppendFormat expects a format string")
	}
	values := rest[1:]
	if len(values) == 1 && values[0].Kind == runtime.KindArray {
		values = values[0].Arr.Elems
	}
	out, err := formatComposite(rest[0].Str, values)
	if err != nil {
		return runtime.Value{}, err
	}
	sb.buf += out
	return args[0], nil
}

func sbToString(args []runtime.Value) (runtime.Value, error) {
	sb, err := asStringBuilder(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.String(sb.buf), nil
}

func sbLength(args []runtime.Value) (runtime.Value, error) {
	sb, err := asStringBuilder(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(int32(len([]rune(sb.buf)))), nil
}

func sbClear(args []runtime.Value) (runtime.Value, error) {
	sb, err := asStringBuilder(args)
	if err != nil {
		return runtime.Value{}, err
	}
	sb.buf = ""
	return args[0], nil
}
