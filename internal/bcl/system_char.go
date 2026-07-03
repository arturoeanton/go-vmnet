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

func charArg(args []runtime.Value) (rune, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindI4 {
		return 0, fmt.Errorf("bcl: System.Char method expects a char argument")
	}
	return rune(args[0].I4), nil
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
