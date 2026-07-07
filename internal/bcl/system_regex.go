package bcl

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// nativeRegex wraps a Go regexp.Regexp, compiled lazily (Fase 3.40) — see
// compiled() below for why. vmnet compiles patterns with Go's RE2
// engine, not .NET's real regex engine — the two dialects mostly overlap
// (character classes, quantifiers, anchors, groups, alternation all
// match), but RE2 has no backreferences and no lookaround
// ((?=...)/(?<=...)/(?!...)); a pattern using either fails to compile
// with a clear error rather than silently matching something different
// than real .NET would (spec: never a plausible-but-wrong result).
type nativeRegex struct {
	pattern    string
	re         *regexp.Regexp
	compileErr error
	didCompile bool
}

// compiled lazily compiles nr's pattern on first actual use (IsMatch/
// Match/Replace), caching the result either way — not at construction
// time. Found via a real, load-bearing case: ClosedXML's own
// XLWorkbook..cctor constructs several validation-regex static fields
// eagerly, at least one of which (a backreference-based quoted-sheet-
// name check) RE2 can't compile — but real code very often builds more
// regexes than any single code path actually exercises, so a real .NET-
// incompatible pattern that's simply never matched against shouldn't
// block everything downstream of its mere construction.
func (nr *nativeRegex) compiled() (*regexp.Regexp, error) {
	if !nr.didCompile {
		nr.re, nr.compileErr = compileRegex(nr.pattern)
		nr.didCompile = true
	}
	return nr.re, nr.compileErr
}

// nativeGroupVal backs both Group and (via the shared get_Value/
// get_Success natives) its base class Capture — Success mirrors real
// Group.Success: false for a capture group that simply didn't
// participate in the match (e.g. inside an alternation or an optional
// group), not just "the whole match failed".
type nativeGroupVal struct {
	success bool
	value   string
	// start/length back Capture.Index/Length (Fase 3.79) — both always
	// meaningful when success is true (0 for a non-participating group,
	// matched via the zero value, same as every other unset field here).
	start  int
	length int
}

// nativeMatchVal backs Match: Groups[0] is always the whole match
// (Group 0's real semantics), Groups[1:] are the pattern's capture
// groups in order.
type nativeMatchVal struct {
	groups []nativeGroupVal
	// re/input back NextMatch() (Fase 3.79) — nil/"" for a Match this
	// package doesn't yet attach them to (NewMatchValueFromLoc's own
	// MatchEvaluator-delegate caller, which never calls NextMatch on the
	// value it hands the delegate); NextMatch reports a clear error
	// rather than panicking on a nil re in that case.
	re    *regexp.Regexp
	input string
}

func init() {
	registerCtor("System.Text.RegularExpressions.Regex", regexCtor)
	register("System.Text.RegularExpressions.Regex::IsMatch", true, regexIsMatch)
	register("System.Text.RegularExpressions.Regex::Match", true, regexMatch)
	register("System.Text.RegularExpressions.Regex::Matches", true, regexMatches)
	// Regex.Replace moved to internal/interpreter/regexreplace.go (Fase
	// 3.64): the MatchEvaluator-delegate overload needs Machine access to
	// invoke it, so both overloads are now handled at that one call site
	// instead of splitting "plain string replacement" (here) from
	// "delegate-based" (there) — see that file's own doc comment.
	// Regex.Escape(string) is a plain static string transform, no Machine
	// access needed — Go's regexp.QuoteMeta escapes a slightly different
	// (mostly overlapping) metacharacter set than real .NET's own Escape
	// (e.g. real .NET also escapes whitespace and '#', QuoteMeta doesn't),
	// same documented RE2-vs-.NET-dialect gap nativeRegex's own doc
	// comment already accepts for pattern matching itself.
	register("System.Text.RegularExpressions.Regex::Escape", true, regexEscape)

	register("System.Text.RegularExpressions.Match::get_Groups", true, matchGetGroups)

	register("System.Text.RegularExpressions.GroupCollection::get_Item", true, groupCollectionGetItem)
	register("System.Text.RegularExpressions.GroupCollection::get_Count", true, groupCollectionGetCount)

	// .Success/.Value on a Match, Group, or Capture instance all compile
	// to the SAME two call targets below — Capture declares Value,
	// Group declares Success, and Match (Capture -> Group -> Match)
	// inherits both without overriding either. See asSuccessValue's doc
	// comment.
	register("System.Text.RegularExpressions.Group::get_Success", true, groupGetSuccess)
	register("System.Text.RegularExpressions.Capture::get_Value", true, groupGetValue)
	// Capture.Index/Length (Fase 3.79) — same inheritance shape as
	// Value/Success above (Capture declares both, Group/Match inherit
	// them unchanged). Found running real Jint: RegExpPrototype.Exec's
	// own result construction reads Match.Index (inherited from Capture)
	// to populate the real JS `RegExp.prototype.exec` result's own
	// `.index` property.
	register("System.Text.RegularExpressions.Capture::get_Index", true, groupGetIndex)
	register("System.Text.RegularExpressions.Capture::get_Length", true, groupGetLength)
	// Match.NextMatch() (Fase 3.79) — re-runs the same regex starting
	// just past the current match's own end, the real, idiomatic
	// "iterate every match by hand" pattern (as opposed to Matches(),
	// which returns them all eagerly at once). Found running real .NET's
	// own regex-based String.Split(Regex) algorithm, which Esprima's own
	// ArrayList<T>-based string-splitting code mirrors internally.
	register("System.Text.RegularExpressions.Match::NextMatch", true, matchNextMatch)

	// MatchCollection (Regex.Matches's real return type, Fase 3.53) needs
	// no dedicated Go struct at all: it's backed by the exact same
	// *nativeList every List<T>/ArrayList already use, tagged with its own
	// real type name — the identical reuse trick ArrayList's own
	// registration above already applies to List`1's natives (see that
	// register() block's doc comment: "Machine.call's virtual dispatch
	// tries the receiver's actual concrete struct type first"). Only
	// get_Count/GetEnumerator are wired up — the overwhelmingly common real
	// usage is `foreach (Match m in regex.Matches(s))`, and indexer/other
	// ICollection members have no known load-bearing call site in this
	// loop's target packages.
	register("System.Text.RegularExpressions.MatchCollection::get_Count", true, listCount)
	register("System.Text.RegularExpressions.MatchCollection::GetEnumerator", true, listGetEnumerator)
	// get_Item (Fase 3.79) — same *nativeList reuse as Count/GetEnumerator
	// just above; missing here meant `matches[i]` (as opposed to a
	// foreach) threw unconditionally. Found running real Jint:
	// RegExpPrototype's own [Symbol.match] (global-flag `.match()`)
	// implementation indexes the MatchCollection directly rather than
	// enumerating it.
	register("System.Text.RegularExpressions.MatchCollection::get_Item", true, listGetItem)
}

func regexEscape(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: Regex.Escape expects a string argument")
	}
	return runtime.String(regexp.QuoteMeta(args[0].Str)), nil
}

func compileRegex(pattern string) (*regexp.Regexp, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, &runtime.ManagedException{
			TypeName: "System.ArgumentException",
			Message:  fmt.Sprintf("Invalid regex pattern %q for vmnet's RE2-based engine (no backreferences/lookaround): %v", pattern, err),
		}
	}
	return re, nil
}

func regexCtor(args []runtime.Value) (*runtime.Object, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return nil, fmt.Errorf("bcl: Regex constructor expects a pattern string")
	}
	return &runtime.Object{Native: &nativeRegex{pattern: args[0].Str}}, nil
}

// resolveRegexAndInput disambiguates the static (input, pattern) and
// instance (receiver, input) call shapes by argument Kind — the same
// approach every other multi-overload native in this package uses, since
// vmnet's call dispatch doesn't distinguish overloads by signature.
func resolveRegexAndInput(args []runtime.Value) (*regexp.Regexp, string, error) {
	// A trailing int32 argument (Fase 3.79) is the real Match(string
	// input, int beginning)/IsMatch(string input, int beginning)
	// overload — used by Jint's own RegExpPrototype.Exec for a sticky
	// (`y` flag) or `lastIndex`-continued search. vmnet has no lazy/
	// position-aware Match value (buildMatchVal's own doc comment), so
	// this simplifies to searching the input from that offset onward —
	// correct for every property this package's own nativeMatchVal
	// currently exposes (no Match.Index/Length yet to get wrong relative
	// to the original, unsliced string).
	beginning := 0
	if len(args) == 3 && args[2].Kind == runtime.KindI4 {
		beginning = int(args[2].I4)
		args = args[:2]
	}
	if len(args) != 2 {
		return nil, "", fmt.Errorf("bcl: Regex method expects 2 arguments")
	}
	var re *regexp.Regexp
	var input string
	if args[0].Kind == runtime.KindObject && args[0].Obj != nil {
		nr, ok := args[0].Obj.Native.(*nativeRegex)
		if !ok || args[1].Kind != runtime.KindString {
			return nil, "", fmt.Errorf("bcl: Regex instance method: unsupported argument shape")
		}
		var err error
		re, err = nr.compiled()
		if err != nil {
			return nil, "", err
		}
		input = args[1].Str
	} else if args[0].Kind == runtime.KindString && args[1].Kind == runtime.KindString {
		var err error
		re, err = compileRegex(args[1].Str)
		if err != nil {
			return nil, "", err
		}
		input = args[0].Str
	} else {
		return nil, "", fmt.Errorf("bcl: Regex method: unsupported argument shape")
	}
	if beginning < 0 || beginning > len(input) {
		return nil, "", &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "beginning"}
	}
	return re, input[beginning:], nil
}

func regexIsMatch(args []runtime.Value) (runtime.Value, error) {
	re, input, err := resolveRegexAndInput(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(re.MatchString(input)), nil
}

// resolveRegexReplace disambiguates the static (input, pattern, replacement)
// and instance (receiver, input, replacement) call shapes the same way
// resolveRegexAndInput does for IsMatch/Match. .NET's `$1`/`${name}` group
// reference syntax in the replacement string overlaps Go's own, so
// ReplaceAllString handles the common cases directly — same RE2-dialect
// limitation already documented on nativeRegex above.
// resolveRegexReplace also returns count — the real Replace(input,
// replacement, count)/Replace(input, replacement, count, beginning)
// overloads' own replacement-count limit (Fase 3.79, found via real
// Jint: RegExpPrototype's own [Symbol.replace] implementation always
// calls the count-limited overload, passing 1 for a non-global (`g`
// flag absent) replace and int.MaxValue for a global one — a bare
// Replace(input, replacement) call, which really does mean "replace
// every match", never appears at all in real Jint's own compiled IL for
// this). -1 means "unlimited" (every match), matching int.MaxValue's own
// real effect without needing to special-case that exact sentinel value.
func resolveRegexReplace(args []runtime.Value) (re *regexp.Regexp, input, replacement string, count int, err error) {
	count = -1
	if len(args) == 4 && args[3].Kind == runtime.KindI4 {
		count = int(args[3].I4)
		args = args[:3]
	}
	if len(args) != 3 {
		return nil, "", "", 0, fmt.Errorf("bcl: Regex.Replace expects 3 arguments")
	}
	if args[0].Kind == runtime.KindObject && args[0].Obj != nil {
		nr, ok := args[0].Obj.Native.(*nativeRegex)
		if !ok || args[1].Kind != runtime.KindString || args[2].Kind != runtime.KindString {
			return nil, "", "", 0, fmt.Errorf("bcl: Regex.Replace instance method: unsupported argument shape")
		}
		re, err := nr.compiled()
		if err != nil {
			return nil, "", "", 0, err
		}
		return re, args[1].Str, args[2].Str, count, nil
	}
	if args[0].Kind == runtime.KindString && args[1].Kind == runtime.KindString && args[2].Kind == runtime.KindString {
		re, err := compileRegex(args[1].Str)
		if err != nil {
			return nil, "", "", 0, err
		}
		return re, args[0].Str, args[2].Str, count, nil
	}
	return nil, "", "", 0, fmt.Errorf("bcl: Regex.Replace: unsupported argument shape")
}

// replaceLimited applies replacement to at most count occurrences (Fase
// 3.79) — Go's regexp has no built-in "replace at most N" operation
// (ReplaceAllString always replaces every match); a negative count means
// unlimited, matching resolveRegexReplace's own convention.
func replaceLimited(re *regexp.Regexp, input, replacement string, count int) string {
	if count < 0 {
		return re.ReplaceAllString(input, replacement)
	}
	var out strings.Builder
	last := 0
	replaced := 0
	for replaced < count {
		loc := re.FindStringIndex(input[last:])
		if loc == nil {
			break
		}
		start, end := last+loc[0], last+loc[1]
		out.WriteString(input[last:start])
		out.WriteString(re.ReplaceAllString(input[start:end], replacement))
		last = end
		replaced++
	}
	out.WriteString(input[last:])
	return out.String()
}

// RegexReplaceString backs the plain string-replacement Regex.Replace
// overloads — exported (Fase 3.64) for internal/interpreter/
// regexreplace.go's own Machine-aware Replace to delegate to when the
// 3rd argument is a plain string rather than a MatchEvaluator delegate
// (invoking a real delegate needs Machine access this package doesn't
// have, so both overloads are now dispatched from that one call site).
func RegexReplaceString(args []runtime.Value) (runtime.Value, error) {
	re, input, replacement, count, err := resolveRegexReplace(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.String(replaceLimited(re, input, replacement, count)), nil
}

// ResolveRegexReplaceEvaluatorTarget resolves the (compiled regex, input)
// pair for the real Regex.Replace(string, MatchEvaluator) overloads —
// instance (receiver, input, evaluator) and static (input, pattern,
// evaluator) — the same instance-vs-static shape resolveRegexAndInput
// already disambiguates for the 2-argument IsMatch/Match/plain-string-
// Replace shapes, just with a 3rd (evaluator) argument along for the ride
// this function itself never inspects.
func ResolveRegexReplaceEvaluatorTarget(args []runtime.Value) (re *regexp.Regexp, input string, ok bool) {
	// A 4th argument, if present, is the real count/beginning overload's
	// own limit (Fase 3.79) — the caller (regexReplaceMachine) already
	// extracted it; only the first 3 (receiver/input, evaluator) matter
	// here.
	if len(args) != 3 && len(args) != 4 {
		return nil, "", false
	}
	if args[0].Kind == runtime.KindObject && args[0].Obj != nil {
		nr, isRegex := args[0].Obj.Native.(*nativeRegex)
		if !isRegex || args[1].Kind != runtime.KindString {
			return nil, "", false
		}
		compiled, err := nr.compiled()
		if err != nil {
			return nil, "", false
		}
		return compiled, args[1].Str, true
	}
	if args[0].Kind == runtime.KindString && args[1].Kind == runtime.KindString {
		compiled, err := compileRegex(args[1].Str)
		if err != nil {
			return nil, "", false
		}
		return compiled, args[0].Str, true
	}
	return nil, "", false
}

// NewMatchValueFromLoc wraps one FindStringSubmatchIndex-shaped result as
// a real Match value — exported (Fase 3.64) for internal/interpreter/
// regexreplace.go's own MatchEvaluator-invoking Regex.Replace, which
// needs one real Match per occurrence to pass to the delegate.
func NewMatchValueFromLoc(loc []int, input string) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: matchValFromLoc(loc, input)})
}

func regexMatch(args []runtime.Value) (runtime.Value, error) {
	re, input, err := resolveRegexAndInput(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.ObjRef(&runtime.Object{Native: buildMatchVal(re, input)}), nil
}

// buildMatchVal runs the match eagerly (vmnet has no lazy Match — every
// property is already known by the time Match()/IsMatch() returns, same
// simplification already made for LINQ, Fase 3.15) via
// FindStringSubmatchIndex rather than FindStringSubmatch: index pairs
// let a non-participating optional group be told apart from one that
// matched an empty string (both would be "" from the plain string API),
// matching Group.Success's real meaning.
func buildMatchVal(re *regexp.Regexp, input string) *nativeMatchVal {
	loc := re.FindStringSubmatchIndex(input)
	if loc == nil {
		return &nativeMatchVal{groups: []nativeGroupVal{{success: false}}, re: re, input: input}
	}
	m := matchValFromLoc(loc, input)
	m.re, m.input = re, input
	return m
}

// matchValFromLoc turns one FindStringSubmatchIndex-shaped index-pair
// slice into a Match — factored out of buildMatchVal so regexMatches
// (below) can build a whole collection of these from
// FindAllStringSubmatchIndex without duplicating the per-match group
// conversion.
func matchValFromLoc(loc []int, input string) *nativeMatchVal {
	groups := make([]nativeGroupVal, len(loc)/2)
	for i := range groups {
		start, end := loc[2*i], loc[2*i+1]
		if start < 0 || end < 0 {
			groups[i] = nativeGroupVal{success: false}
			continue
		}
		groups[i] = nativeGroupVal{success: true, value: input[start:end], start: start, length: end - start}
	}
	return &nativeMatchVal{groups: groups}
}

// regexMatches backs Regex.Matches(string) — the plural method, distinct
// from Match(string) above: every non-overlapping match in the input, not
// just the first. FindAllStringSubmatchIndex(input, -1) (-1 = unlimited)
// is Go's own direct equivalent, and already returns nil (zero matches)
// rather than a one-element "no match" sentinel the way
// FindStringSubmatchIndex does for the singular case — so, unlike
// buildMatchVal, there's no separate empty-collection special case to
// write here at all; a nil locs slice already turns into a correctly
// empty MatchCollection via make([]runtime.Value, 0).
func regexMatches(args []runtime.Value) (runtime.Value, error) {
	re, input, err := resolveRegexAndInput(args)
	if err != nil {
		return runtime.Value{}, err
	}
	locs := re.FindAllStringSubmatchIndex(input, -1)
	items := make([]runtime.Value, len(locs))
	for i, loc := range locs {
		items[i] = runtime.ObjRef(&runtime.Object{Native: matchValFromLoc(loc, input)})
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeList{
		items:    items,
		typeName: "System.Text.RegularExpressions.MatchCollection",
	}}), nil
}

func asMatchVal(args []runtime.Value) (*nativeMatchVal, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return nil, fmt.Errorf("bcl: Match method called without a receiver")
	}
	m, ok := args[0].Obj.Native.(*nativeMatchVal)
	if !ok {
		return nil, fmt.Errorf("bcl: receiver is not a Match")
	}
	return m, nil
}

// matchGetGroups returns the Match itself as the GroupCollection: vmnet
// doesn't model a separate GroupCollection wrapper object — its only two
// members (get_Item/get_Count) read the exact same groups slice a Match
// already carries, so reusing the Match's own Native avoids allocating a
// second wrapper for no behavioral difference (GroupCollection has no
// members of its own beyond those two).
func matchGetGroups(args []runtime.Value) (runtime.Value, error) {
	if _, err := asMatchVal(args); err != nil {
		return runtime.Value{}, err
	}
	return args[0], nil
}

func groupCollectionGetItem(args []runtime.Value) (runtime.Value, error) {
	m, err := asMatchVal(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 || args[1].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: GroupCollection indexer expects an int32 index")
	}
	idx := int(args[1].I4)
	if idx < 0 || idx >= len(m.groups) {
		return runtime.ObjRef(&runtime.Object{Native: &nativeGroupVal{success: false}}), nil
	}
	g := m.groups[idx]
	return runtime.ObjRef(&runtime.Object{Native: &g}), nil
}

func groupCollectionGetCount(args []runtime.Value) (runtime.Value, error) {
	m, err := asMatchVal(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(int32(len(m.groups))), nil
}

// asSuccessValue reads (Success, Value) off either a *nativeGroupVal (a
// single capture group) or a *nativeMatchVal (Group 0, the whole match)
// — real .NET's Capture -> Group -> Match hierarchy means `.Success`/
// `.Value` on a Match instance compile to `callvirt Group::get_Success`/
// `callvirt Capture::get_Value` (Match inherits both, it never overrides
// either), confirmed against real IL before assuming Match had its own
// get_Success/get_Value at all — the first version of this file
// registered those under Match:: directly and they were simply never
// called.
func asSuccessValue(args []runtime.Value) (success bool, value string, err error) {
	g, err := asGroupVal(args)
	if err != nil {
		return false, "", err
	}
	return g.success, g.value, nil
}

// asGroupVal is asSuccessValue's own superset (Fase 3.79) — also needed
// by Capture.Index/Length, which asSuccessValue's own (success, value)
// pair has no room for.
func asGroupVal(args []runtime.Value) (nativeGroupVal, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return nativeGroupVal{}, fmt.Errorf("bcl: Group/Capture method called without a receiver")
	}
	switch n := args[0].Obj.Native.(type) {
	case *nativeGroupVal:
		return *n, nil
	case *nativeMatchVal:
		return n.groups[0], nil
	default:
		return nativeGroupVal{}, fmt.Errorf("bcl: receiver is not a Group/Capture/Match (got %T)", args[0].Obj.Native)
	}
}

func groupGetSuccess(args []runtime.Value) (runtime.Value, error) {
	success, _, err := asSuccessValue(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(success), nil
}

func groupGetIndex(args []runtime.Value) (runtime.Value, error) {
	g, err := asGroupVal(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(int32(g.start)), nil
}

func groupGetLength(args []runtime.Value) (runtime.Value, error) {
	g, err := asGroupVal(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(int32(g.length)), nil
}

// matchNextMatch backs Match.NextMatch() — see this native's own
// register() call site doc comment. A failed/empty current match has
// nowhere meaningful to resume from (real .NET's own NextMatch on an
// unsuccessful Match just returns another unsuccessful Match), matching
// that directly rather than erroring.
func matchNextMatch(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return runtime.Value{}, fmt.Errorf("bcl: Match.NextMatch called without a receiver")
	}
	m, ok := args[0].Obj.Native.(*nativeMatchVal)
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: receiver is not a Match")
	}
	if m.re == nil || !m.groups[0].success {
		return runtime.ObjRef(&runtime.Object{Native: &nativeMatchVal{groups: []nativeGroupVal{{success: false}}, re: m.re, input: m.input}}), nil
	}
	from := m.groups[0].start + m.groups[0].length
	// A zero-length match must still advance by at least one position,
	// or NextMatch() would return the exact same match forever (real
	// .NET's own Match.NextMatch has this same one-position bump for a
	// zero-length match).
	if m.groups[0].length == 0 {
		from++
	}
	if from > len(m.input) {
		return runtime.ObjRef(&runtime.Object{Native: &nativeMatchVal{groups: []nativeGroupVal{{success: false}}, re: m.re, input: m.input}}), nil
	}
	loc := m.re.FindStringSubmatchIndex(m.input[from:])
	if loc == nil {
		return runtime.ObjRef(&runtime.Object{Native: &nativeMatchVal{groups: []nativeGroupVal{{success: false}}, re: m.re, input: m.input}}), nil
	}
	for i := range loc {
		if loc[i] >= 0 {
			loc[i] += from
		}
	}
	next := matchValFromLoc(loc, m.input)
	next.re, next.input = m.re, m.input
	return runtime.ObjRef(&runtime.Object{Native: next}), nil
}

func groupGetValue(args []runtime.Value) (runtime.Value, error) {
	_, value, err := asSuccessValue(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.String(value), nil
}
