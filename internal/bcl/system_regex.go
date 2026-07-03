package bcl

import (
	"fmt"
	"regexp"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// nativeRegex wraps a compiled Go regexp.Regexp. vmnet compiles patterns
// with Go's RE2 engine, not .NET's real regex engine — the two dialects
// mostly overlap (character classes, quantifiers, anchors, groups,
// alternation all match), but RE2 has no backreferences and no
// lookaround ((?=...)/(?<=...)/(?!...)); a pattern using either fails to
// compile here with a clear error rather than silently matching
// something different than real .NET would (spec: never a
// plausible-but-wrong result).
type nativeRegex struct {
	re *regexp.Regexp
}

// nativeGroupVal backs both Group and (via the shared get_Value/
// get_Success natives) its base class Capture — Success mirrors real
// Group.Success: false for a capture group that simply didn't
// participate in the match (e.g. inside an alternation or an optional
// group), not just "the whole match failed".
type nativeGroupVal struct {
	success bool
	value   string
}

// nativeMatchVal backs Match: Groups[0] is always the whole match
// (Group 0's real semantics), Groups[1:] are the pattern's capture
// groups in order.
type nativeMatchVal struct {
	groups []nativeGroupVal
}

func init() {
	registerCtor("System.Text.RegularExpressions.Regex", regexCtor)
	register("System.Text.RegularExpressions.Regex::IsMatch", true, regexIsMatch)
	register("System.Text.RegularExpressions.Regex::Match", true, regexMatch)

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
	re, err := compileRegex(args[0].Str)
	if err != nil {
		return nil, err
	}
	return &runtime.Object{Native: &nativeRegex{re: re}}, nil
}

// resolveRegexAndInput disambiguates the static (input, pattern) and
// instance (receiver, input) call shapes by argument Kind — the same
// approach every other multi-overload native in this package uses, since
// vmnet's call dispatch doesn't distinguish overloads by signature.
func resolveRegexAndInput(args []runtime.Value) (*regexp.Regexp, string, error) {
	if len(args) != 2 {
		return nil, "", fmt.Errorf("bcl: Regex method expects 2 arguments")
	}
	if args[0].Kind == runtime.KindObject && args[0].Obj != nil {
		nr, ok := args[0].Obj.Native.(*nativeRegex)
		if !ok || args[1].Kind != runtime.KindString {
			return nil, "", fmt.Errorf("bcl: Regex instance method: unsupported argument shape")
		}
		return nr.re, args[1].Str, nil
	}
	if args[0].Kind == runtime.KindString && args[1].Kind == runtime.KindString {
		re, err := compileRegex(args[1].Str)
		if err != nil {
			return nil, "", err
		}
		return re, args[0].Str, nil
	}
	return nil, "", fmt.Errorf("bcl: Regex method: unsupported argument shape")
}

func regexIsMatch(args []runtime.Value) (runtime.Value, error) {
	re, input, err := resolveRegexAndInput(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(re.MatchString(input)), nil
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
		return &nativeMatchVal{groups: []nativeGroupVal{{success: false}}}
	}
	groups := make([]nativeGroupVal, len(loc)/2)
	for i := range groups {
		start, end := loc[2*i], loc[2*i+1]
		if start < 0 || end < 0 {
			groups[i] = nativeGroupVal{success: false}
			continue
		}
		groups[i] = nativeGroupVal{success: true, value: input[start:end]}
	}
	return &nativeMatchVal{groups: groups}
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
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return false, "", fmt.Errorf("bcl: Group/Capture method called without a receiver")
	}
	switch n := args[0].Obj.Native.(type) {
	case *nativeGroupVal:
		return n.success, n.value, nil
	case *nativeMatchVal:
		return n.groups[0].success, n.groups[0].value, nil
	default:
		return false, "", fmt.Errorf("bcl: receiver is not a Group/Capture/Match (got %T)", args[0].Obj.Native)
	}
}

func groupGetSuccess(args []runtime.Value) (runtime.Value, error) {
	success, _, err := asSuccessValue(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(success), nil
}

func groupGetValue(args []runtime.Value) (runtime.Value, error) {
	_, value, err := asSuccessValue(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.String(value), nil
}
