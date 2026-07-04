package bcl

import (
	"fmt"
	"unicode"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// System.Char has no runtime.Value Kind of its own — a `char` is just a
// KindI4 on the CIL stack, same as every other integral type narrower
// than int32 (spec §III.1.1) — so these are plain static natives over an
// int32 argument, not instance methods on a distinct receiver.
func init() {
	register("System.Char::IsUpper", true, charPredicate(unicode.IsUpper))
	register("System.Char::IsLower", true, charPredicate(unicode.IsLower))
	register("System.Char::IsDigit", true, charPredicate(unicode.IsDigit))
	// Char.IsNumber is broader than IsDigit in real .NET (Unicode category
	// Nd/Nl/No — fractions, superscripts, Roman numerals, ... — not just
	// decimal digits), but vmnet has no exact per-category Unicode table
	// of its own; Go's unicode.IsNumber covers the same Unicode "Number"
	// general category, close enough for every real caller found so far
	// (Fase 3.47, Newtonsoft.Json 13.0.3's own JsonTextReader number-
	// token scanning, which only ever tests real ASCII/decimal digits in
	// practice).
	register("System.Char::IsNumber", true, charPredicate(unicode.IsNumber))
	register("System.Char::IsLetter", true, charPredicate(unicode.IsLetter))
	register("System.Char::IsLetterOrDigit", true, charPredicate(func(r rune) bool {
		return unicode.IsLetter(r) || unicode.IsDigit(r)
	}))
	register("System.Char::IsWhiteSpace", true, charPredicate(unicode.IsSpace))
	register("System.Char::ToString", true, charToString)
	register("System.Char::ToUpper", true, charTransform(unicode.ToUpper))
	register("System.Char::ToLower", true, charTransform(unicode.ToLower))
	// vmnet has no culture support anywhere (CultureInfo's stub since
	// Fase 3.6) — the *Invariant variants use the exact same
	// transformation as their culture-sensitive counterparts.
	register("System.Char::ToUpperInvariant", true, charTransform(unicode.ToUpper))
	register("System.Char::ToLowerInvariant", true, charTransform(unicode.ToLower))
}

// charArg accepts both real overload shapes these predicates/transforms
// have in the actual BCL — Char.IsWhiteSpace(char) and the (string,
// index) sibling every one of these methods also has (e.g.
// Char.IsWhiteSpace(string s, int index)), found running real Jint code.
// bcl's native registry is a flat name->func map with no arity awareness
// at all (unlike the metadata-driven overload resolution assembly.go's
// pickMethodOverload does for a plugin's own methods, Fase 3.27) — every
// multi-shape BCL native in this project already disambiguates by
// inspecting args itself; this is that same pattern.
func charArg(args []runtime.Value) (rune, error) {
	if len(args) >= 1 && args[0].Kind == runtime.KindRef && args[0].Ref != nil {
		// `constrained.`-prefixed calls (a generic type parameter bound to
		// char) and some real overloads (Char.IsWhiteSpace(ref
		// ReadOnlySpan-ish shapes)) pass their char argument by managed
		// pointer rather than by value — same deref-before-use pattern as
		// every struct receiver elsewhere in this project.
		deref := append([]runtime.Value{}, args...)
		deref[0] = *args[0].Ref
		args = deref
	}
	if len(args) == 1 && args[0].Kind == runtime.KindI4 {
		return rune(args[0].I4), nil
	}
	if len(args) == 2 && args[0].Kind == runtime.KindString && args[1].Kind == runtime.KindI4 {
		idx := int(args[1].I4)
		s := args[0].Str
		if idx < 0 || idx >= len(s) {
			return 0, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "index"}
		}
		return rune(s[idx]), nil
	}
	return 0, fmt.Errorf("bcl: System.Char method expects a char argument")
}

func charPredicate(f func(rune) bool) Native {
	return func(args []runtime.Value) (runtime.Value, error) {
		r, err := charArg(args)
		if err != nil {
			return runtime.Value{}, err
		}
		return runtime.Bool(f(r)), nil
	}
}

func charTransform(f func(rune) rune) Native {
	return func(args []runtime.Value) (runtime.Value, error) {
		r, err := charArg(args)
		if err != nil {
			return runtime.Value{}, err
		}
		return runtime.Int32(f(r)), nil
	}
}

func charToString(args []runtime.Value) (runtime.Value, error) {
	r, err := charArg(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.String(string(r)), nil
}
