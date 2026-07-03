package bcl

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

func init() {
	// All String.Concat overloads (2/3/4-arg, params object[]) collapse to
	// this one native: it just concatenates whatever string args arrive.
	register("System.String::Concat", true, stringConcat)
	register("System.String::get_Length", true, stringLength)
	register("System.String::Format", true, stringFormat)
	register("System.String::Substring", true, stringSubstring)
	register("System.String::get_Chars", true, stringGetChars)
	// Instance a.Equals(b) (HasThis, args=[a,b]) and static
	// Equals(a,b)/op_Equality(a,b) (no this, args=[a,b]) both arrive as a
	// 2-element args slice comparing the same two positions either way, so
	// one native backs all three.
	register("System.String::Equals", true, stringEquals)
	register("System.String::op_Equality", true, stringEquals)
	register("System.String::op_Inequality", true, stringNotEquals)
	register("System.String::Join", true, stringJoin)
	register("System.String::IsNullOrEmpty", true, stringIsNullOrEmpty)
	register("System.String::IsNullOrWhiteSpace", true, stringIsNullOrWhiteSpace)
	register("System.String::StartsWith", true, stringStartsWith)
	register("System.String::IndexOf", true, stringIndexOf)
	register("System.String::LastIndexOf", true, stringLastIndexOf)
	register("System.String::Split", true, stringSplit)
	register("System.String::ToCharArray", true, stringToCharArray)
	register("System.String::Replace", true, stringReplace)
	register("System.String::Trim", true, stringTrim)
	register("System.String::Contains", true, stringContains)
	register("System.String::EndsWith", true, stringEndsWith)
}

func stringEndsWith(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[0].Kind != runtime.KindString || args[1].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.EndsWith expects a string argument")
	}
	return runtime.Bool(strings.HasSuffix(args[0].Str, args[1].Str)), nil
}

// NewStringFromCtor backs `new string(...)` — called directly from
// internal/interpreter/calls.go's newObj (not through the normal
// bcl.LookupCtor/registerCtor path, which always wraps its result as a
// KindObject; a vmnet string is a plain KindString value, never an
// Object). Covers the char[]-based overloads (char[], char[] with
// start+length, and char*repeated-count) — the overwhelming majority of
// real `new string(...)` call sites; the ReadOnlySpan<char>-based .NET
// Core-only overload isn't covered (netstandard2.0 target, spec's own
// certified-package scope).
func NewStringFromCtor(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 2 && args[0].Kind == runtime.KindI4 && args[1].Kind == runtime.KindI4 {
		// new string(char c, int count)
		count := int(args[1].I4)
		if count < 0 {
			return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "count must be non-negative"}
		}
		return runtime.String(strings.Repeat(string(rune(args[0].I4)), count)), nil
	}
	if len(args) >= 1 && args[0].Kind == runtime.KindArray {
		var runes []rune
		if args[0].Arr != nil {
			for _, e := range args[0].Arr.Elems {
				if e.Kind == runtime.KindI4 {
					runes = append(runes, rune(e.I4))
				}
			}
		}
		start, length := 0, len(runes)
		if len(args) >= 3 && args[1].Kind == runtime.KindI4 && args[2].Kind == runtime.KindI4 {
			start = int(args[1].I4)
			length = int(args[2].I4)
		}
		if start < 0 || length < 0 || start+length > len(runes) {
			return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "new string(char[], start, length): out of range"}
		}
		return runtime.String(string(runes[start : start+length])), nil
	}
	return runtime.Value{}, fmt.Errorf("bcl: unsupported System.String constructor overload")
}

func stringContains(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[0].Kind != runtime.KindString || args[1].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.Contains expects a string argument")
	}
	return runtime.Bool(strings.Contains(args[0].Str, args[1].Str)), nil
}

// stringJoin backs every String.Join overload: the params-array shape
// (4+ elements, or an explicit array argument) collapses to the same
// element-expansion Format already does; non-string elements go through
// displayString like Concat's boxed-argument case. A List<T> argument
// (the compiler picks the IEnumerable<string> overload for
// `Join(sep, someList)`, which vmnet doesn't dispatch through a real
// enumerator here — reading the native backing directly is equivalent
// and far simpler) is unwrapped the same way.
func stringJoin(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.Join expects a separator string")
	}
	sep := args[0].Str
	values := args[1:]
	if len(values) == 1 {
		switch {
		case values[0].Kind == runtime.KindArray:
			values = values[0].Arr.Elems
		case values[0].Kind == runtime.KindObject && values[0].Obj != nil:
			if l, ok := values[0].Obj.Native.(*nativeList); ok {
				values = l.items
			}
		}
	}
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = displayString(v)
	}
	return runtime.String(strings.Join(parts, sep)), nil
}

// stringConcat backs every String.Concat overload, including the
// object-typed ones the compiler picks for `"literal" + nonStringExpr`
// (values arrive boxed — a no-op in vmnet, see internal/ir/builder.go —
// so non-string args are formatted the same way Object.ToString() would).
func stringConcat(args []runtime.Value) (runtime.Value, error) {
	var sb strings.Builder
	for _, a := range args {
		if a.Kind == runtime.KindString {
			sb.WriteString(a.Str)
		} else {
			sb.WriteString(displayString(a))
		}
	}
	return runtime.String(sb.String()), nil
}

func stringLength(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.get_Length expects a string receiver")
	}
	return runtime.Int32(int32(len([]rune(args[0].Str)))), nil
}

func stringEquals(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.Equals expects 2 string arguments")
	}
	a, b := args[0], args[1]
	if a.Kind == runtime.KindNull || b.Kind == runtime.KindNull {
		return runtime.Bool(a.Kind == b.Kind), nil
	}
	if a.Kind != runtime.KindString || b.Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.Equals expects 2 string arguments")
	}
	return runtime.Bool(a.Str == b.Str), nil
}

func stringNotEquals(args []runtime.Value) (runtime.Value, error) {
	v, err := stringEquals(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(!v.Truthy()), nil
}

func stringIsNullOrEmpty(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.IsNullOrEmpty expects 1 argument")
	}
	return runtime.Bool(args[0].Kind == runtime.KindNull || args[0].Str == ""), nil
}

func stringIsNullOrWhiteSpace(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.IsNullOrWhiteSpace expects 1 argument")
	}
	return runtime.Bool(args[0].Kind == runtime.KindNull || strings.TrimSpace(args[0].Str) == ""), nil
}

// stringStartsWith ignores a trailing StringComparison argument (ordinal
// comparison is all vmnet models — no culture support, same limitation
// documented for CultureInfo since Fase 3.6).
func stringStartsWith(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[0].Kind != runtime.KindString || args[1].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.StartsWith expects a string argument")
	}
	return runtime.Bool(strings.HasPrefix(args[0].Str, args[1].Str)), nil
}

// stringIndexOf/stringLastIndexOf work in rune positions, consistent with
// every other index vmnet exposes over a string (Substring, get_Chars) —
// not real UTF-16 code-unit positions, a documented simplification.
func stringIndexOf(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[0].Kind != runtime.KindString || args[1].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.IndexOf expects a string argument")
	}
	runes := []rune(args[0].Str)
	needle := []rune(args[1].Str)
	start := 0
	if len(args) >= 3 && args[2].Kind == runtime.KindI4 {
		start = int(args[2].I4)
	}
	if start < 0 || start > len(runes) {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "IndexOf start index out of range"}
	}
	for i := start; i+len(needle) <= len(runes); i++ {
		if runesEqual(runes[i:i+len(needle)], needle) {
			return runtime.Int32(int32(i)), nil
		}
	}
	return runtime.Int32(-1), nil
}

func stringLastIndexOf(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[0].Kind != runtime.KindString || args[1].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.LastIndexOf expects a string argument")
	}
	runes := []rune(args[0].Str)
	needle := []rune(args[1].Str)
	for i := len(runes) - len(needle); i >= 0; i-- {
		if runesEqual(runes[i:i+len(needle)], needle) {
			return runtime.Int32(int32(i)), nil
		}
	}
	return runtime.Int32(-1), nil
}

func runesEqual(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// stringSplit backs every Split overload vmnet is likely to see: a
// char[]/string[] separator array (empty or absent means "split on
// whitespace", matching real Split(null)'s documented behavior), plus an
// optional trailing StringSplitOptions/count argument — only
// RemoveEmptyEntries (a nonzero int among the trailing args) is honored,
// a max-count limit is not.
func stringSplit(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.Split expects a string receiver")
	}
	var seps []string
	if len(args) >= 2 && args[1].Kind == runtime.KindArray && args[1].Arr != nil {
		for _, e := range args[1].Arr.Elems {
			switch e.Kind {
			case runtime.KindI4:
				seps = append(seps, string(rune(e.I4)))
			case runtime.KindString:
				if e.Str != "" {
					seps = append(seps, e.Str)
				}
			}
		}
	}
	removeEmpty := false
	if len(args) > 2 {
		for _, a := range args[2:] {
			if a.Kind == runtime.KindI4 && a.I4 != 0 {
				removeEmpty = true
			}
		}
	}

	isSep := func(r rune) bool {
		if len(seps) == 0 {
			return unicode.IsSpace(r)
		}
		for _, sep := range seps {
			if []rune(sep)[0] == r && len([]rune(sep)) == 1 {
				return true
			}
		}
		return false
	}

	var parts []string
	var cur strings.Builder
	for _, r := range args[0].Str {
		if isSep(r) {
			parts = append(parts, cur.String())
			cur.Reset()
		} else {
			cur.WriteRune(r)
		}
	}
	parts = append(parts, cur.String())

	if removeEmpty {
		filtered := parts[:0]
		for _, p := range parts {
			if p != "" {
				filtered = append(filtered, p)
			}
		}
		parts = filtered
	}

	elems := make([]runtime.Value, len(parts))
	for i, p := range parts {
		elems[i] = runtime.String(p)
	}
	return runtime.ArrRef(&runtime.Array{Elems: elems}), nil
}

func stringToCharArray(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.ToCharArray expects a string receiver")
	}
	runes := []rune(args[0].Str)
	elems := make([]runtime.Value, len(runes))
	for i, r := range runes {
		elems[i] = runtime.Int32(r)
	}
	return runtime.ArrRef(&runtime.Array{Elems: elems}), nil
}

// stringReplace covers both Replace(string,string) and Replace(char,char)
// — the compiler picks the char overload for single-quoted literals.
func stringReplace(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 3 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.Replace expects a string receiver and 2 arguments")
	}
	oldArg, newArg := args[1], args[2]
	toStr := func(v runtime.Value) (string, bool) {
		switch v.Kind {
		case runtime.KindString:
			return v.Str, true
		case runtime.KindI4:
			return string(rune(v.I4)), true
		default:
			return "", false
		}
	}
	oldStr, ok1 := toStr(oldArg)
	newStr, ok2 := toStr(newArg)
	if !ok1 || !ok2 {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.Replace: unsupported argument kind")
	}
	return runtime.String(strings.ReplaceAll(args[0].Str, oldStr, newStr)), nil
}

// stringTrim covers both Trim() and Trim(params char[]).
func stringTrim(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.Trim expects a string receiver")
	}
	if len(args) >= 2 && args[1].Kind == runtime.KindArray && args[1].Arr != nil && len(args[1].Arr.Elems) > 0 {
		var cutset []rune
		for _, e := range args[1].Arr.Elems {
			if e.Kind == runtime.KindI4 {
				cutset = append(cutset, rune(e.I4))
			}
		}
		return runtime.String(strings.Trim(args[0].Str, string(cutset))), nil
	}
	return runtime.String(strings.TrimSpace(args[0].Str)), nil
}

func stringSubstring(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[0].Kind != runtime.KindString || args[1].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.Substring expects (int) or (int, int)")
	}
	runes := []rune(args[0].Str)
	start := int(args[1].I4)
	end := len(runes)
	if len(args) >= 3 {
		if args[2].Kind != runtime.KindI4 {
			return runtime.Value{}, fmt.Errorf("bcl: System.String.Substring length must be int")
		}
		end = start + int(args[2].I4)
	}
	if start < 0 || end < start || end > len(runes) {
		return runtime.Value{}, &runtime.ManagedException{
			TypeName: "System.ArgumentOutOfRangeException",
			Message:  "Index and length must refer to a location within the string.",
		}
	}
	return runtime.String(string(runes[start:end])), nil
}

func stringGetChars(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 || args[0].Kind != runtime.KindString || args[1].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.get_Chars expects an int index")
	}
	runes := []rune(args[0].Str)
	idx := int(args[1].I4)
	if idx < 0 || idx >= len(runes) {
		return runtime.Value{}, &runtime.ManagedException{
			TypeName: "System.IndexOutOfRangeException",
			Message:  "Index was outside the bounds of the string.",
		}
	}
	return runtime.Int32(runes[idx]), nil
}

// stringFormat backs every String.Format overload. The fixed-arity
// overloads (up to 3 substitution values) arrive as flat args after the
// format string; the `params object[]` overload (4+ values) arrives as a
// single trailing array argument, expanded here so both shapes share one
// composite-format parser.
func stringFormat(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.Format expects a format string")
	}
	values := args[1:]
	if len(values) == 1 && values[0].Kind == runtime.KindArray {
		values = values[0].Arr.Elems
	}
	out, err := formatComposite(args[0].Str, values)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.String(out), nil
}

// formatComposite implements the composite format string grammar (spec
// "{index[,alignment][:formatString]}", `{{`/`}}` as literal braces).
func formatComposite(format string, values []runtime.Value) (string, error) {
	var sb strings.Builder
	runes := []rune(format)
	for i := 0; i < len(runes); {
		c := runes[i]
		switch c {
		case '{':
			if i+1 < len(runes) && runes[i+1] == '{' {
				sb.WriteByte('{')
				i += 2
				continue
			}
			end := i + 1
			for end < len(runes) && runes[end] != '}' {
				end++
			}
			if end >= len(runes) {
				return "", fmt.Errorf("bcl: malformed format string: unmatched '{'")
			}
			piece, err := formatSpec(string(runes[i+1:end]), values)
			if err != nil {
				return "", err
			}
			sb.WriteString(piece)
			i = end + 1
		case '}':
			if i+1 < len(runes) && runes[i+1] == '}' {
				sb.WriteByte('}')
				i += 2
				continue
			}
			return "", fmt.Errorf("bcl: malformed format string: unmatched '}'")
		default:
			sb.WriteRune(c)
			i++
		}
	}
	return sb.String(), nil
}

// maxFormatAlignment bounds String.Format's `{index,alignment}` padding
// width — see the allocation-safety note in formatSpec.
const maxFormatAlignment = 1 << 16

func formatSpec(spec string, values []runtime.Value) (string, error) {
	idxPart, formatStr, _ := strings.Cut(spec, ":")
	alignment := 0
	if base, alignStr, ok := strings.Cut(idxPart, ","); ok {
		idxPart = base
		a, err := strconv.Atoi(strings.TrimSpace(alignStr))
		if err != nil {
			return "", fmt.Errorf("bcl: malformed format alignment %q", alignStr)
		}
		// A plugin (or untrusted CallJSON/CallBytes input, which can carry
		// a format string straight from outside the process) could ask for
		// an enormous alignment to make strings.Repeat below allocate
		// unbounded memory — same class of risk MaxArrayLength guards
		// against for newarr (Fase 3.5). maxFormatAlignment keeps a single
		// Format call's padding bounded regardless of caller input.
		if a > maxFormatAlignment || a < -maxFormatAlignment {
			return "", fmt.Errorf("bcl: format alignment %d exceeds the %d-character limit", a, maxFormatAlignment)
		}
		alignment = a
	}
	idx, err := strconv.Atoi(strings.TrimSpace(idxPart))
	if err != nil {
		return "", fmt.Errorf("bcl: malformed format index %q", idxPart)
	}
	if idx < 0 || idx >= len(values) {
		return "", fmt.Errorf("bcl: format index %d out of range (%d value(s))", idx, len(values))
	}
	s, err := formatValue(values[idx], formatStr)
	if err != nil {
		return "", err
	}
	if width := alignment; width != 0 {
		if width < 0 {
			width = -width
		}
		if pad := width - len([]rune(s)); pad > 0 {
			if alignment < 0 {
				s += strings.Repeat(" ", pad)
			} else {
				s = strings.Repeat(" ", pad) + s
			}
		}
	}
	return s, nil
}

// formatValue applies a standard numeric format specifier (D/F/N/X/P/G —
// spec's most common composite-format cases). An unrecognized specifier is
// a Go error rather than a silent guess, matching vmnet's rule of never
// producing a plausible-but-wrong result for something it doesn't model.
func formatValue(v runtime.Value, spec string) (string, error) {
	if spec == "" {
		return displayString(v), nil
	}
	kind := byte(0)
	if len(spec) > 0 {
		kind = spec[0]
		if kind >= 'a' && kind <= 'z' {
			kind -= 'a' - 'A'
		}
	}
	precision := -1
	if len(spec) > 1 {
		p, err := strconv.Atoi(spec[1:])
		if err != nil {
			return "", fmt.Errorf("bcl: unsupported format specifier %q", spec)
		}
		precision = p
	}

	asFloat, isFloat := valueAsFloat64(v)
	asInt, isInt := valueAsInt64(v)

	switch kind {
	case 'D':
		if !isInt {
			return "", fmt.Errorf("bcl: format specifier \"D\" requires an integer value")
		}
		s := strconv.FormatInt(asInt, 10)
		neg := strings.HasPrefix(s, "-")
		digits := strings.TrimPrefix(s, "-")
		if precision > len(digits) {
			digits = strings.Repeat("0", precision-len(digits)) + digits
		}
		if neg {
			return "-" + digits, nil
		}
		return digits, nil
	case 'F':
		if !isFloat && !isInt {
			return "", fmt.Errorf("bcl: format specifier \"F\" requires a numeric value")
		}
		if precision < 0 {
			precision = 2
		}
		f := asFloat
		if !isFloat {
			f = float64(asInt)
		}
		return strconv.FormatFloat(f, 'f', precision, 64), nil
	case 'N':
		if !isFloat && !isInt {
			return "", fmt.Errorf("bcl: format specifier \"N\" requires a numeric value")
		}
		if precision < 0 {
			precision = 2
		}
		f := asFloat
		if !isFloat {
			f = float64(asInt)
		}
		return groupThousands(strconv.FormatFloat(f, 'f', precision, 64)), nil
	case 'X':
		if !isInt {
			return "", fmt.Errorf("bcl: format specifier \"X\" requires an integer value")
		}
		s := strings.ToUpper(strconv.FormatInt(asInt, 16))
		if precision > len(s) {
			s = strings.Repeat("0", precision-len(s)) + s
		}
		return s, nil
	case 'P':
		if !isFloat && !isInt {
			return "", fmt.Errorf("bcl: format specifier \"P\" requires a numeric value")
		}
		if precision < 0 {
			precision = 2
		}
		f := asFloat
		if !isFloat {
			f = float64(asInt)
		}
		return strconv.FormatFloat(f*100, 'f', precision, 64) + "%", nil
	case 'G':
		return displayString(v), nil
	default:
		return "", fmt.Errorf("bcl: unsupported format specifier %q", spec)
	}
}

func valueAsInt64(v runtime.Value) (int64, bool) {
	switch v.Kind {
	case runtime.KindI4:
		return int64(v.I4), true
	case runtime.KindI8:
		return v.I8, true
	default:
		return 0, false
	}
}

func valueAsFloat64(v runtime.Value) (float64, bool) {
	switch v.Kind {
	case runtime.KindR4:
		return float64(v.R4), true
	case runtime.KindR8:
		return v.R8, true
	default:
		return 0, false
	}
}

// groupThousands inserts "," every 3 digits in the integer part of a
// decimal string produced by strconv.FormatFloat, matching "N" format's
// thousands separator (invariant-culture comma, not full culture support).
func groupThousands(s string) string {
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	intPart, fracPart, hasFrac := strings.Cut(s, ".")

	var grouped []byte
	for i, c := range []byte(intPart) {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			grouped = append(grouped, ',')
		}
		grouped = append(grouped, c)
	}
	out := string(grouped)
	if hasFrac {
		out += "." + fracPart
	}
	if neg {
		out = "-" + out
	}
	return out
}
