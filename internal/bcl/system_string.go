package bcl

import (
	"fmt"
	"math"
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
	// System.String::Join is registered as a Machine-aware native instead
	// (internal/interpreter's own stringJoin, Fase 3.81) — a real plugin
	// IEnumerable<string> (as opposed to a vmnet-native list/array) needs
	// Machine access to actually enumerate, which a plain bcl.Native like
	// every other registration in this file can't provide. See that
	// override's own doc comment for the real, load-bearing case this
	// fixed (CsvHelper's own MemberNameCollection).
	register("System.String::IsNullOrEmpty", true, stringIsNullOrEmpty)
	register("System.String::IsNullOrWhiteSpace", true, stringIsNullOrWhiteSpace)
	register("System.String::StartsWith", true, stringStartsWith)
	register("System.String::IndexOf", true, stringIndexOf)
	register("System.String::LastIndexOf", true, stringLastIndexOf)
	register("System.String::Split", true, stringSplit)
	register("System.String::ToCharArray", true, stringToCharArray)
	register("System.String::Replace", true, stringReplace)
	register("System.String::Trim", true, stringTrim)
	register("System.String::TrimStart", true, stringTrimStart)
	register("System.String::TrimEnd", true, stringTrimEnd)
	register("System.String::PadLeft", true, stringPadLeft)
	register("System.String::PadRight", true, stringPadRight)
	register("System.String::Insert", true, stringInsert)
	register("System.String::Remove", true, stringRemove)
	register("System.String::Contains", true, stringContains)
	register("System.String::EndsWith", true, stringEndsWith)
	register("System.String::ToUpper", true, stringToUpper)
	register("System.String::ToUpperInvariant", true, stringToUpper)
	register("System.String::ToLower", true, stringToLower)
	register("System.String::ToLowerInvariant", true, stringToLower)
	register("System.String::Compare", true, stringCompare)
	register("System.String::CompareTo", true, stringCompare)
	register("System.String::CompareOrdinal", true, stringCompare)
}

// stringToUpper backs both ToUpper() and ToUpperInvariant() — vmnet has
// no CultureInfo model to distinguish culture-sensitive casing from the
// invariant culture's, so both collapse to Go's Unicode-aware
// strings.ToUpper.
func stringToUpper(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.ToUpper expects a string receiver")
	}
	return runtime.String(strings.ToUpper(args[0].Str)), nil
}

func stringToLower(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.ToLower expects a string receiver")
	}
	return runtime.String(strings.ToLower(args[0].Str)), nil
}

// stringCompare backs both the static String.Compare(a, b[, ignoreCase])
// and the instance a.CompareTo(b) — ordinal comparison (Go's
// strings.Compare), ignoring any StringComparison/ignoreCase/culture
// argument beyond a literal `true` bool immediately following the two
// strings (the common `Compare(a, b, true)` shape) — case-sensitivity
// is honored, full culture-aware collation is not, matching every other
// natively-implemented string operation in this file.
func stringCompare(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[0].Kind != runtime.KindString || args[1].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.Compare expects two string arguments")
	}
	a, b := args[0].Str, args[1].Str
	if len(args) >= 3 && args[2].Kind == runtime.KindI4 && args[2].I4 != 0 {
		a, b = strings.ToLower(a), strings.ToLower(b)
	}
	return runtime.Int32(int32(strings.Compare(a, b))), nil
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

// stringConcat backs every String.Concat overload, including the
// object-typed ones the compiler picks for `"literal" + nonStringExpr`
// (values arrive boxed — a no-op in vmnet, see internal/ir/builder.go —
// so non-string args are formatted the same way Object.ToString() would).
// stringConcat covers every Concat overload (2/3/4-arg object/string
// params, and the single string[]/object[] params array shape a call
// site with more than 4 arguments — or an explicit array — compiles to).
// The single-array shape needs its own branch: `Concat(someArray)`
// arrives here as exactly one KindArray argument, whose own elements are
// the real pieces to join, not one opaque value to stringify wholesale
// (found via a real bug: without this, `string.Concat(new[] {a, b, c,
// d, e})` produced the literal text "<array[5]>" as an exception
// message instead of the real joined text).
func stringConcat(args []runtime.Value) (runtime.Value, error) {
	pieces := args
	if len(args) == 1 && args[0].Kind == runtime.KindArray && args[0].Arr != nil {
		pieces = args[0].Arr.Elems
	}
	var sb strings.Builder
	for _, a := range pieces {
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

// stringEquals handles both the 2-arg shape (instance Equals(string) or
// static Equals(string, string)) and the 3-arg shape with a trailing
// System.StringComparison — vmnet has no real BCL enum metadata for
// StringComparison (a BCL-only enum, Fase 3.27), so its raw underlying
// int32 value is checked directly against the real enum's known values:
// CurrentCultureIgnoreCase=1/InvariantCultureIgnoreCase=3/
// OrdinalIgnoreCase=5 (the three odd/IgnoreCase ones) mean
// case-insensitive; anything else (including no culture support
// anywhere else in vmnet either, see StringComparer/CultureInfo's own
// stubs) compares ordinally.
func stringEquals(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.Equals expects 2 string arguments")
	}
	a, b := args[0], args[1]
	if a.Kind == runtime.KindNull || b.Kind == runtime.KindNull {
		return runtime.Bool(a.Kind == b.Kind), nil
	}
	if a.Kind != runtime.KindString || b.Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.Equals expects 2 string arguments")
	}
	ignoreCase := false
	if len(args) >= 3 && args[2].Kind == runtime.KindI4 {
		switch args[2].I4 {
		case 1, 3, 5:
			ignoreCase = true
		}
	}
	if ignoreCase {
		return runtime.Bool(strings.EqualFold(a.Str, b.Str)), nil
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
	if len(args) < 2 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.IndexOf expects a string receiver")
	}
	needle, ok := stringOrCharRunes(args[1])
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.IndexOf: unsupported argument shape")
	}
	runes := []rune(args[0].Str)
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
	if len(args) < 2 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.LastIndexOf expects a string receiver")
	}
	needle, ok := stringOrCharRunes(args[1])
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.LastIndexOf: unsupported argument shape")
	}
	runes := []rune(args[0].Str)
	for i := len(runes) - len(needle); i >= 0; i-- {
		if runesEqual(runes[i:i+len(needle)], needle) {
			return runtime.Int32(int32(i)), nil
		}
	}
	return runtime.Int32(-1), nil
}

// stringOrCharRunes accepts either overload IndexOf/LastIndexOf's second
// argument commonly takes in real code — a full string, or a single char
// (KindI4, e.g. `s.IndexOf(':')`, found running real Jint/Esprima).
func stringOrCharRunes(v runtime.Value) ([]rune, bool) {
	switch v.Kind {
	case runtime.KindString:
		return []rune(v.Str), true
	case runtime.KindI4:
		return []rune{rune(v.I4)}, true
	default:
		return nil, false
	}
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

// trimCutset reads TrimStart/TrimEnd's own optional `params char[]`
// argument — empty (whitespace, matching real TrimStart()/TrimEnd()'s
// no-arg overload) when absent.
func trimCutset(args []runtime.Value) string {
	if len(args) >= 2 && args[1].Kind == runtime.KindArray && args[1].Arr != nil && len(args[1].Arr.Elems) > 0 {
		var cutset []rune
		for _, e := range args[1].Arr.Elems {
			if e.Kind == runtime.KindI4 {
				cutset = append(cutset, rune(e.I4))
			}
		}
		return string(cutset)
	}
	return ""
}

// stringTrimStart/stringTrimEnd (Fase 3.42, general IL/BCL hardening
// pass) cover both the no-arg (whitespace) and `params char[]` cutset
// overloads, the same shape stringTrim already handles for the two-
// sided Trim().
func stringTrimStart(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.TrimStart expects a string receiver")
	}
	if cutset := trimCutset(args); cutset != "" {
		return runtime.String(strings.TrimLeft(args[0].Str, cutset)), nil
	}
	return runtime.String(strings.TrimLeft(args[0].Str, " \t\n\r\v\f")), nil
}

func stringTrimEnd(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.TrimEnd expects a string receiver")
	}
	if cutset := trimCutset(args); cutset != "" {
		return runtime.String(strings.TrimRight(args[0].Str, cutset)), nil
	}
	return runtime.String(strings.TrimRight(args[0].Str, " \t\n\r\v\f")), nil
}

// stringPadLeft/stringPadRight (Fase 3.42) cover both PadLeft(width) and
// PadLeft(width, char) — real .NET pads with plain spaces when no fill
// character is given, and does nothing when the string is already at
// least totalWidth long (never truncates).
func stringPadLeft(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[0].Kind != runtime.KindString || args[1].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.PadLeft expects (int) or (int, char)")
	}
	pad := byte(' ')
	if len(args) >= 3 && args[2].Kind == runtime.KindI4 {
		pad = byte(args[2].I4)
	}
	runes := []rune(args[0].Str)
	width := int(args[1].I4)
	if len(runes) >= width {
		return args[0], nil
	}
	prefix := strings.Repeat(string(rune(pad)), width-len(runes))
	return runtime.String(prefix + args[0].Str), nil
}

func stringPadRight(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[0].Kind != runtime.KindString || args[1].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.PadRight expects (int) or (int, char)")
	}
	pad := byte(' ')
	if len(args) >= 3 && args[2].Kind == runtime.KindI4 {
		pad = byte(args[2].I4)
	}
	runes := []rune(args[0].Str)
	width := int(args[1].I4)
	if len(runes) >= width {
		return args[0], nil
	}
	suffix := strings.Repeat(string(rune(pad)), width-len(runes))
	return runtime.String(args[0].Str + suffix), nil
}

// stringInsert backs String.Insert(int startIndex, string value) — real
// semantics throw ArgumentOutOfRangeException for an index outside
// [0, Length], matching Substring's own bounds-checking posture.
func stringInsert(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 3 || args[0].Kind != runtime.KindString || args[1].Kind != runtime.KindI4 || args[2].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.Insert expects (int startIndex, string value)")
	}
	runes := []rune(args[0].Str)
	idx := int(args[1].I4)
	if idx < 0 || idx > len(runes) {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "startIndex"}
	}
	return runtime.String(string(runes[:idx]) + args[2].Str + string(runes[idx:])), nil
}

// stringRemove covers both Remove(int startIndex) (removes to the end)
// and Remove(int startIndex, int count).
func stringRemove(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[0].Kind != runtime.KindString || args[1].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: System.String.Remove expects (int startIndex[, int count])")
	}
	runes := []rune(args[0].Str)
	start := int(args[1].I4)
	end := len(runes)
	if len(args) >= 3 {
		if args[2].Kind != runtime.KindI4 {
			return runtime.Value{}, fmt.Errorf("bcl: System.String.Remove count must be int")
		}
		end = start + int(args[2].I4)
	}
	if start < 0 || end < start || end > len(runes) {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "startIndex or count"}
	}
	return runtime.String(string(runes[:start]) + string(runes[end:])), nil
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
	// String.Format(IFormatProvider provider, string format, object[] args)
	// — a real, common overload (library code following the "always pass
	// a culture" analyzer convention) whose first argument is a format
	// provider (vmnet's CultureInfo stub or similar), not the format
	// string itself. vmnet has no real culture-sensitive formatting
	// anywhere (CultureInfo's stub since Fase 3.6) — the provider is
	// simply ignored, same simplification every other culture-aware
	// native already makes.
	if len(args) >= 2 && args[0].Kind != runtime.KindString && args[1].Kind == runtime.KindString {
		args = args[1:]
	}
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

// formatValue applies a standard numeric format specifier (D/F/N/X/P/G/C/E
// — spec's most common composite-format cases, and the same set every
// ToString(format) native below delegates to) or a CUSTOM numeric format
// string (formatCustomNumeric — a sequence of placeholder/literal
// characters like "0.00%"/"000.00"/"#,##0.00", told apart from a standard
// specifier by shape: real .NET treats a format string as "standard" only
// when it's a single letter optionally followed by plain digits —
// anything else is always custom, even if it happens to start with a
// letter). An unrecognized standard letter, or a custom pattern using a
// character this simplified implementation doesn't model (a ';'
// negative/zero section, scientific notation, ...), is a Go error rather
// than a silent guess, matching vmnet's rule of never producing a
// plausible-but-wrong result for something it doesn't model.
func formatValue(v runtime.Value, spec string) (string, error) {
	if spec == "" {
		return displayString(v), nil
	}
	if !isStandardSpecifierShape(spec) {
		return formatCustomNumeric(v, spec)
	}
	// origKind (case preserved) only matters for X/x: real .NET's hex
	// format specifier is the one standard specifier whose CASE is part
	// of its meaning ("X" uppercase digits, "x" lowercase) rather than
	// just a case-insensitive alias for the same behavior (every other
	// letter here — F/N/D/P/G/C/E/e — means the same thing regardless of
	// case).
	origKind := spec[0]
	kind := origKind
	if kind >= 'a' && kind <= 'z' {
		kind -= 'a' - 'A'
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
	case 'C':
		// Invariant-culture-adjacent: real Currency format is
		// culture-specific (symbol, symbol placement, grouping), but
		// vmnet has no culture support anywhere (CultureInfo is a stub,
		// Fase 3.6) — "$" prefix + N's own grouping is what every
		// culture-less/en-US-like environment (this project's only
		// realistic target) actually produces.
		if !isFloat && !isInt {
			return "", fmt.Errorf("bcl: format specifier \"C\" requires a numeric value")
		}
		if precision < 0 {
			precision = 2
		}
		f := asFloat
		if !isFloat {
			f = float64(asInt)
		}
		neg := f < 0
		s := "$" + groupThousands(strconv.FormatFloat(math.Abs(f), 'f', precision, 64))
		if neg {
			return "-" + s, nil
		}
		return s, nil
	case 'X':
		// Two's-complement of the value's ORIGINAL integral width, not a
		// naive decimal-negative-sign hex string: real (-1).ToString("X")
		// is "FFFFFFFF" for a 32-bit int, "FFFFFFFFFFFFFFFF" for a 64-bit
		// long — the CLR's X format only ever prints the bit pattern,
		// which has no sign of its own. v.Kind (not just asInt's value)
		// is what distinguishes a widened-to-64-bit long from a 32-bit
		// int/short/byte, since valueAsInt64 already sign-extends either
		// into the same int64 asInt.
		if !isInt {
			return "", fmt.Errorf("bcl: format specifier \"X\" requires an integer value")
		}
		var s string
		if v.Kind == runtime.KindI8 {
			s = strconv.FormatUint(uint64(asInt), 16)
		} else {
			s = strconv.FormatUint(uint64(uint32(asInt)), 16)
		}
		if origKind == 'X' {
			s = strings.ToUpper(s)
		}
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
		return groupThousands(strconv.FormatFloat(f*100, 'f', precision, 64)) + "%", nil
	case 'E':
		// .NET's E format always uses a signed, MINIMUM-3-digit exponent
		// ("E+003"), unlike Go's FormatFloat ('E'/'e' verb), which pads to
		// only 2 — padExponent bridges that one difference; everything
		// else (mantissa digit count defaulting to 6, the E/e case itself
		// selecting upper/lowercase) already matches.
		if !isFloat && !isInt {
			return "", fmt.Errorf("bcl: format specifier \"E\" requires a numeric value")
		}
		if precision < 0 {
			precision = 6
		}
		f := asFloat
		if !isFloat {
			f = float64(asInt)
		}
		goVerb := byte('E')
		if origKind == 'e' {
			goVerb = 'e'
		}
		return padExponent(strconv.FormatFloat(f, goVerb, precision, 64)), nil
	case 'G':
		return displayString(v), nil
	default:
		return "", fmt.Errorf("bcl: unsupported format specifier %q", spec)
	}
}

// padExponent widens FormatFloat's 'E'/'e'-verb exponent field (Go always
// emits at least 2 digits, e.g. "E+03") to .NET's own minimum of 3
// ("E+003") — the exponent digit COUNT is the only thing FormatFloat gets
// wrong for real .NET E-format output; sign and mantissa already match.
func padExponent(s string) string {
	idx := strings.IndexAny(s, "Ee")
	if idx < 0 || idx+1 >= len(s) {
		return s
	}
	mantissa := s[:idx+1]
	sign := s[idx+1]
	digits := s[idx+2:]
	for len(digits) < 3 {
		digits = "0" + digits
	}
	return mantissa + string(sign) + digits
}

// isStandardSpecifierShape tells a standard numeric format string
// ("X", "N2", "F4", ...) apart from a custom one ("0.00%", "#,##0.00",
// "000.00", ...) by shape alone, matching real .NET's own rule: a
// standard specifier is always exactly one letter followed by nothing
// but plain decimal digits (the precision). Anything else — including a
// single character that happens to be a letter .NET doesn't recognize as
// standard, e.g. "A" — is a custom format string instead of a "supported
// standard specifier" error, so it reaches formatCustomNumeric's own
// (separate) unsupported-character error instead, which is the more
// accurate diagnostic for it.
func isStandardSpecifierShape(spec string) bool {
	c := spec[0]
	if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) {
		return false
	}
	for i := 1; i < len(spec); i++ {
		if spec[i] < '0' || spec[i] > '9' {
			return false
		}
	}
	return true
}

// formatCustomNumeric implements the common core of .NET's custom numeric
// format strings: '0' (mandatory digit, zero-padded), '#' (optional
// digit), ',' anywhere left of the decimal point (thousands grouping —
// real .NET also gives a trailing ',' immediately before the decimal
// point a "scale by 1000" meaning, not modeled here), '.' (decimal
// point), '%' (multiply by 100, literal '%' in the output), any other
// character passed through literally. Deliberately does NOT support
// custom format SECTIONS (";"-separated positive/negative/zero patterns)
// or scientific notation ('0.00E+00') — reports those as an unsupported
// character rather than guessing, same posture as formatValue's own
// unrecognized-standard-specifier error.
func formatCustomNumeric(v runtime.Value, spec string) (string, error) {
	asFloat, isFloat := valueAsFloat64(v)
	asInt, isInt := valueAsInt64(v)
	if !isFloat && !isInt {
		return "", fmt.Errorf("bcl: custom numeric format %q requires a numeric value", spec)
	}
	for _, r := range spec {
		switch r {
		case '0', '#', ',', '.', '%':
		default:
			return "", fmt.Errorf("bcl: unsupported character %q in custom numeric format %q", r, spec)
		}
	}
	f := asFloat
	if !isFloat {
		f = float64(asInt)
	}
	hasPercent := strings.Contains(spec, "%")
	if hasPercent {
		f *= 100
	}
	intSpec, fracSpec, _ := strings.Cut(spec, ".")
	minIntDigits := strings.Count(intSpec, "0")
	hasGrouping := strings.Contains(intSpec, ",")
	fracDigits := 0
	for i := 0; i < len(fracSpec); i++ {
		if fracSpec[i] != '0' && fracSpec[i] != '#' {
			break
		}
		fracDigits++
	}

	s := strconv.FormatFloat(f, 'f', fracDigits, 64)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	intPart, fracPart, hasFrac := strings.Cut(s, ".")
	if minIntDigits > len(intPart) {
		intPart = strings.Repeat("0", minIntDigits-len(intPart)) + intPart
	}
	out := intPart
	if hasFrac {
		out += "." + fracPart
	}
	if hasGrouping {
		out = groupThousands(out)
	}
	if neg {
		out = "-" + out
	}
	if hasPercent {
		out += "%"
	}
	return out, nil
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
