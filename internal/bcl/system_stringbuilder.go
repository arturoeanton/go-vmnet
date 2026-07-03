package bcl

import (
	"fmt"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// nativeStringBuilder backs System.Text.StringBuilder. Every Append
// overload (string/char/int/bool/object/...) collapses to the same native
// (see resolveCallTarget in internal/ir/builder.go — call targets aren't
// disambiguated by parameter type), so a `char` argument stringifies as
// its numeric code point rather than the character, same documented
// limitation String.Concat already has for boxed non-string args.
type nativeStringBuilder struct {
	buf strings.Builder
}

func init() {
	registerCtor("System.Text.StringBuilder", func(args []runtime.Value) (*runtime.Object, error) {
		sb := &nativeStringBuilder{}
		// The string-seeded overload (`new StringBuilder(initial)`) is the
		// only one that needs the ctor argument itself — the capacity-int
		// overload is a pure allocation hint, nothing to apply here.
		if len(args) > 0 && args[0].Kind == runtime.KindString {
			sb.buf.WriteString(args[0].Str)
		}
		return &runtime.Object{Native: sb}, nil
	})
	register("System.Text.StringBuilder::Append", true, sbAppend)
	register("System.Text.StringBuilder::AppendLine", true, sbAppendLine)
	register("System.Text.StringBuilder::ToString", true, sbToString)
	register("System.Text.StringBuilder::get_Length", true, sbLength)
	register("System.Text.StringBuilder::Clear", true, sbClear)
	// Capacity: vmnet's StringBuilder is backed by a Go strings.Builder,
	// which auto-grows with no separate, meaningfully distinct notion of
	// "allocated but unused" capacity to report — the current length is
	// a defensible stand-in (real Capacity is always >= Length, and
	// EnsureCapacity's usual caller just wants "big enough", which the
	// auto-growing backing already guarantees regardless of what this
	// returns).
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
		sb.buf.WriteString(displayString(args[1]))
	}
	return args[0], nil
}

func sbAppendLine(args []runtime.Value) (runtime.Value, error) {
	sb, err := asStringBuilder(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) == 2 {
		sb.buf.WriteString(displayString(args[1]))
	}
	sb.buf.WriteByte('\n')
	return args[0], nil
}

func sbToString(args []runtime.Value) (runtime.Value, error) {
	sb, err := asStringBuilder(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.String(sb.buf.String()), nil
}

func sbLength(args []runtime.Value) (runtime.Value, error) {
	sb, err := asStringBuilder(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(int32(len([]rune(sb.buf.String())))), nil
}

func sbClear(args []runtime.Value) (runtime.Value, error) {
	sb, err := asStringBuilder(args)
	if err != nil {
		return runtime.Value{}, err
	}
	sb.buf.Reset()
	return args[0], nil
}
