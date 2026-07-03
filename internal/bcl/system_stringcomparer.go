package bcl

import (
	"fmt"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// nativeStringComparer is a marker: vmnet only ever compares strings
// ordinally (no culture support anywhere else either, see CultureInfo's
// stub since Fase 3.6), so every StringComparer property
// (Ordinal/OrdinalIgnoreCase/InvariantCulture/...) returns the same
// comparer, and only the IgnoreCase variants are distinguished (the rest
// collapse to plain ordinal comparison — a documented simplification,
// not a silent wrong answer for the dominant real use, dictionary/set
// key comparison).
type nativeStringComparer struct {
	ignoreCase bool
}

func init() {
	register("System.StringComparer::get_Ordinal", true, stringComparerOrdinal)
	register("System.StringComparer::get_OrdinalIgnoreCase", true, stringComparerOrdinalIgnoreCase)
	register("System.StringComparer::get_InvariantCulture", true, stringComparerOrdinal)
	register("System.StringComparer::get_InvariantCultureIgnoreCase", true, stringComparerOrdinalIgnoreCase)
	register("System.StringComparer::Equals", true, stringComparerEquals)
	register("System.StringComparer::Compare", true, stringComparerCompare)
	register("System.StringComparer::GetHashCode", true, stringComparerGetHashCode)
}

func stringComparerOrdinal(args []runtime.Value) (runtime.Value, error) {
	return runtime.ObjRef(&runtime.Object{Native: &nativeStringComparer{}}), nil
}

func stringComparerOrdinalIgnoreCase(args []runtime.Value) (runtime.Value, error) {
	return runtime.ObjRef(&runtime.Object{Native: &nativeStringComparer{ignoreCase: true}}), nil
}

func asStringComparer(args []runtime.Value) (*nativeStringComparer, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return nil, fmt.Errorf("bcl: StringComparer method called without a receiver")
	}
	c, ok := args[0].Obj.Native.(*nativeStringComparer)
	if !ok {
		return nil, fmt.Errorf("bcl: receiver is not a StringComparer")
	}
	return c, nil
}

func stringComparerEquals(args []runtime.Value) (runtime.Value, error) {
	c, err := asStringComparer(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 3 || args[1].Kind != runtime.KindString || args[2].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: StringComparer.Equals expects 2 string arguments")
	}
	a, b := args[1].Str, args[2].Str
	if c.ignoreCase {
		return runtime.Bool(strings.EqualFold(a, b)), nil
	}
	return runtime.Bool(a == b), nil
}

func stringComparerCompare(args []runtime.Value) (runtime.Value, error) {
	c, err := asStringComparer(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 3 || args[1].Kind != runtime.KindString || args[2].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: StringComparer.Compare expects 2 string arguments")
	}
	a, b := args[1].Str, args[2].Str
	if c.ignoreCase {
		a, b = strings.ToLower(a), strings.ToLower(b)
	}
	return runtime.Int32(int32(strings.Compare(a, b))), nil
}

func stringComparerGetHashCode(args []runtime.Value) (runtime.Value, error) {
	c, err := asStringComparer(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 2 || args[1].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: StringComparer.GetHashCode expects a string argument")
	}
	s := args[1].Str
	if c.ignoreCase {
		s = strings.ToLower(s)
	}
	return runtime.Int32(valueHash(runtime.String(s))), nil
}
