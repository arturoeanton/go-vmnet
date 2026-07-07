package interpreter

import (
	"fmt"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// Real System.Text.RegularExpressions.Regex.Replace(string,
// MatchEvaluator) (Fase 3.64) — found via FluentValidation's own error-
// message placeholder substitution (`{PropertyName}` etc.), which needs
// the delegate genuinely invoked once per match, unlike the plain
// string-replacement overload (which stays a plain bcl.Native,
// internal/bcl/system_regex.go's own RegexReplaceString). Both overloads
// are dispatched from this one Machine-aware call site — the plain
// bcl.Lookup registration for "Regex::Replace" was removed entirely, see
// that file's own doc comment, since a plain bcl.Native can never be
// shadowed by a machineRegistry entry of the same name (tryCall checks
// bcl.Lookup first, unconditionally).
func init() {
	machineRegistry["System.Text.RegularExpressions.Regex::Replace"] = regexReplaceMachine
}

func regexReplaceMachine(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	// A trailing int32 4th argument (Fase 3.79) is the real Replace(...,
	// count)/Replace(..., count, beginning) overload's own replacement-
	// count limit — real Jint's own RegExpPrototype [Symbol.replace]
	// always calls this exact overload (1 for a non-global replace,
	// int.MaxValue for a global one), never the bare 3-arg "replace
	// everything" overload. -1 (unlimited) matches bcl.RegexReplaceString/
	// resolveRegexReplace's own convention for a bare 3-arg call.
	count := -1
	if len(args) == 4 && args[3].Kind == runtime.KindI4 {
		count = int(args[3].I4)
	} else if len(args) != 3 {
		return runtime.Value{}, fmt.Errorf("interpreter: Regex.Replace expects 3 arguments")
	}
	if args[2].Kind != runtime.KindFunc {
		return bcl.RegexReplaceString(args)
	}
	re, input, ok := bcl.ResolveRegexReplaceEvaluatorTarget(args)
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: Regex.Replace(string, MatchEvaluator): unsupported argument shape")
	}
	evaluator := args[2].Func
	locs := re.FindAllStringSubmatchIndex(input, -1)
	if count >= 0 && len(locs) > count {
		locs = locs[:count]
	}
	if len(locs) == 0 {
		return runtime.String(input), nil
	}
	var b strings.Builder
	prevEnd := 0
	for _, loc := range locs {
		start, end := loc[0], loc[1]
		b.WriteString(input[prevEnd:start])
		matchValue := bcl.NewMatchValueFromLoc(loc, input)
		result, _, err := m.invokeFunc(evaluator, []runtime.Value{matchValue}, depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		if result.Kind != runtime.KindString {
			return runtime.Value{}, fmt.Errorf("interpreter: Regex.Replace: MatchEvaluator must return a string")
		}
		b.WriteString(result.Str)
		prevEnd = end
	}
	b.WriteString(input[prevEnd:])
	return runtime.String(b.String()), nil
}
