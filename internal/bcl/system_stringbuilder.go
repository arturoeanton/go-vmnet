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
	// set_Length (Fase 3.53) is genuinely bidirectional in real
	// StringBuilder, not just a truncation shortcut: assigning a value
	// LARGER than the current length PADS with '\0' characters rather
	// than throwing or leaving the tail uninitialized — confirmed against
	// real `dotnet run` output (`sb.Length = 6` on a 3-char buffer, then
	// reading each char back, prints three literal NUL characters,
	// (int)'\0' == 0 — not an exception, not left as whatever garbage a
	// naive append-only backing might have). Negative is the one real
	// error case (ArgumentOutOfRangeException).
	register("System.Text.StringBuilder::set_Length", false, sbSetLength)
	// get_Chars (the `sb[i]` indexer getter, Fase 3.53) — same rune-position
	// indexing as sbInsert's own index argument (get_Length's own rune-count
	// semantics), not a byte offset. Real StringBuilder's indexer setter
	// (set_Chars) has no known load-bearing call site in this loop's target
	// packages, so only the getter is wired up here — same "cover what's
	// actually used" posture listReverse's own doc comment already takes.
	register("System.Text.StringBuilder::get_Chars", true, sbGetChars)
	register("System.Text.StringBuilder::Clear", true, sbClear)
	// Capacity: vmnet's StringBuilder auto-grows with no separate,
	// meaningfully distinct notion of "allocated but unused" capacity to
	// report — the current length is a defensible stand-in (real
	// Capacity is always >= Length, and EnsureCapacity's usual caller
	// just wants "big enough", which the auto-growing backing already
	// guarantees regardless of what this returns).
	register("System.Text.StringBuilder::get_Capacity", true, sbLength)
	register("System.Text.StringBuilder::EnsureCapacity", true, sbLength)
	// set_Capacity (Fase 3.79) — same "auto-growing backing already
	// guarantees enough room" reasoning as get_Capacity/EnsureCapacity
	// above, and the same no-op convention System.Collections.Generic.
	// List`1::set_Capacity already takes for exactly this reason
	// (system_collections.go). Real StringBuilder.set_Capacity's one
	// genuine error case — a new capacity smaller than the current
	// Length — is still checked and thrown, rather than silently
	// accepted, since that's real, observable behavior a caller could
	// depend on (unlike the allocation hint itself). Found running real
	// Jint/Esprima: Esprima.Scanner.RegExpParser.ParsePattern sizes its
	// own reusable StringBuilder via `sb.Capacity = pattern.Length` before
	// every single regex literal it scans — with no native registered at
	// all, this fell through to a nonsensical "real interpreted body"
	// attempt (StringBuilder has no real TypeDef/IL body anywhere for
	// vmnet to run — it's native-only), surfacing as a confusing "type
	// System.Text.StringBuilder not found" instead of running successfully.
	register("System.Text.StringBuilder::set_Capacity", false, sbSetCapacity)
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
	switch {
	case len(args) == 2:
		sb.buf += displayString(args[1])
	case len(args) == 4 && args[1].Kind == runtime.KindString && args[2].Kind == runtime.KindI4 && args[3].Kind == runtime.KindI4:
		// The real Append(string value, int startIndex, int count)
		// substring overload (Fase 3.80) — every other Append overload
		// here collapses to the plain "stringify the one value"
		// 2-argument case above, but this one is a genuinely different
		// shape (3 real arguments) that fell through doing nothing at
		// all, silently dropping the substring instead of appending it.
		// Found running real Esprima: Scanner.RegExpParser.ParsePattern's
		// own `stringBuilder.Append(_pattern, index, 1 + ((int)
		// regExpGroupType >> 2))` appends a capturing/non-capturing
		// group's own opening delimiter(s) (`(`, `(?:`, `(?=`, ...) this
		// way — every regex literal with at least one parenthesized
		// group silently lost its own opening paren from the translated
		// .NET pattern, turning `(abc)` into the invalid `abc)`.
		runes := []rune(args[1].Str)
		start, count := int(args[2].I4), int(args[3].I4)
		if start < 0 || count < 0 || start+count > len(runes) {
			return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "Index and length must refer to a location within the string."}
		}
		sb.buf += string(runes[start : start+count])
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
	current := []rune(sb.buf)
	// The real (startIndex, length) overload (Fase 3.79) extracts a
	// substring instead of the whole buffer — ignoring it and always
	// returning the full buffer silently corrupted every caller relying
	// on it. Found running real Jint/Esprima: Scanner.ScanRegExpBody's
	// own `stringBuilder.ToString(1, stringBuilder.Length - 2)` strips a
	// scanned regex literal's own leading/trailing `/` delimiters this
	// way — without this, every regex literal's own pattern kept both
	// delimiters (e.g. "a" scanned from `/a/` came out as "/a/"), so
	// `/a/.test('abc')` compiled a literal substring search for the
	// three characters "/a/" instead of the letter "a", silently
	// returning false for every regex literal without exception.
	if len(args) >= 3 && args[1].Kind == runtime.KindI4 && args[2].Kind == runtime.KindI4 {
		start, length := int(args[1].I4), int(args[2].I4)
		if start < 0 || length < 0 || start+length > len(current) {
			return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "Index and length must refer to a location within the string."}
		}
		return runtime.String(string(current[start : start+length])), nil
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

// sbGetChars backs the `sb[i]` indexer getter — a rune position into buf,
// same convention sbInsert's own index argument already uses. Real
// StringBuilder[int] throws ArgumentOutOfRangeException (not String's own
// IndexOutOfRangeException — stringGetChars, system_string.go) on a bad
// index; the two BCL types simply disagree on which exception their
// respective indexers raise.
func sbGetChars(args []runtime.Value) (runtime.Value, error) {
	sb, err := asStringBuilder(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 2 || args[1].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: StringBuilder indexer expects an int32 index")
	}
	current := []rune(sb.buf)
	idx := int(args[1].I4)
	if idx < 0 || idx >= len(current) {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "Index was out of range. Must be non-negative and less than the size of the collection."}
	}
	return runtime.Int32(current[idx]), nil
}

// sbSetLength backs the Length property setter — see the register() call
// site's own doc comment for why growing pads with '\0' rather than
// erroring or leaving anything uninitialized (real, confirmed CLR
// behavior, not assumed). Shrinking is a plain rune-slice truncation,
// same rune-position convention every other index into buf already uses.
func sbSetLength(args []runtime.Value) (runtime.Value, error) {
	sb, err := asStringBuilder(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 2 || args[1].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: StringBuilder.set_Length expects an int32 length")
	}
	n := int(args[1].I4)
	if n < 0 {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "Length must be non-negative."}
	}
	current := []rune(sb.buf)
	switch {
	case n <= len(current):
		sb.buf = string(current[:n])
	default:
		padded := make([]rune, n)
		copy(padded, current)
		// The zero value of `rune` is already 0 ('\0') — Go's make()
		// zero-initializes the tail, so no explicit fill loop is needed.
		sb.buf = string(padded)
	}
	return runtime.Value{}, nil
}

// sbSetCapacity backs the Capacity property setter — see its own
// register() call site's doc comment for why this is a no-op otherwise
// (vmnet's StringBuilder auto-grows already), keeping only the one real,
// observable error real StringBuilder.set_Capacity has: a new capacity
// smaller than the current Length.
func sbSetCapacity(args []runtime.Value) (runtime.Value, error) {
	sb, err := asStringBuilder(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 2 || args[1].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: StringBuilder.set_Capacity expects an int32 capacity")
	}
	n := int(args[1].I4)
	if n < len([]rune(sb.buf)) {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "capacity was less than the current size."}
	}
	return runtime.Value{}, nil
}
