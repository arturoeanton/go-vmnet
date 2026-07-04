package bcl

import (
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// NativeOrdered backs a LINQ OrderBy/OrderByDescending/ThenBy/
// ThenByDescending chain's result (a real IOrderedEnumerable<T>) —
// defined here (not in internal/interpreter, where the actual sorting
// logic lives) so NativeTypeName, just below, can recognize it: without
// that, `foreach (var x in xs.OrderBy(...))` or any further LINQ call
// reached through the declared IEnumerable<T>/IOrderedEnumerable<T>
// interface type (rather than already-materialized via ToList/ToArray)
// has no way to redirect back to this receiver's real concrete type
// (see receiverTypeName's own doc comment, internal/interpreter/
// typecheck.go).
//
// Items is always kept fully sorted by every key applied SO FAR (Machine
// access is available at both OrderBy's and ThenBy's own call sites, so
// there's no need to defer) — this is the field NativeListItems, just
// below, exposes: a plain bcl.Native with no Machine access at all
// (String.Join, List<T>.Contains, ...) that already special-cases "an
// IEnumerable source might really be a native List" must keep working
// unchanged for an OrderBy/ThenBy result exactly like it does for one
// from Select/Where/any other LINQ terminal — this was a real, probed
// regression found the hard way: routing OrderBy through a DIFFERENT,
// deferred-sort shape broke `string.Join(",", xs.OrderBy(...))` outright
// (silently printed the receiver's own placeholder ToString() instead of
// its elements) the moment NativeListItems stopped recognizing it.
//
// Source/Keys are kept alongside Items purely so ThenBy/ThenByDescending
// can recompute the FULL multi-key sort from the original, pre-sort
// order plus every key applied so far (its own key appended to Keys) —
// re-sorting from Source on each ThenBy, rather than re-sorting the
// already-sorted Items in place, is what makes a later ThenBy able to
// use a DIFFERENT, less significant tie-breaking rule than a naive
// "stable-sort the current Items by just the new key" would (which would
// wrongly make the new key primary for any earlier tie).
type NativeOrdered struct {
	Items  []runtime.Value
	Source []runtime.Value
	// Keys is the applied ordering, most significant first: index 0 is
	// the original OrderBy/OrderByDescending call, each later entry one
	// more ThenBy/ThenByDescending appended after it.
	Keys []OrderKey
}

// OrderKey is one key selector in an OrderBy/ThenBy chain.
type OrderKey struct {
	Selector   runtime.Value // Func<TSource,TKey> — always KindFunc.
	Descending bool
	// Comparer is an explicit IComparer<TKey> argument, or KindNull for
	// natural ordering (interpreter's compareFunc/compareNatural).
	Comparer runtime.Value
}

// NewOrderedValue wraps an already-sorted items/source/keys triple
// (interpreter/linq_orderby.go computes the sort itself, since only it
// has the Machine access needed to invoke key selectors/comparers) as a
// real IOrderedEnumerable<T>-shaped value.
func NewOrderedValue(items, source []runtime.Value, keys []OrderKey) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &NativeOrdered{Items: items, Source: source, Keys: keys}})
}

// AsNativeOrdered extracts v's own *NativeOrdered, if it wraps one —
// ThenBy/ThenByDescending need this to append one more key onto an
// existing chain rather than starting a brand new one (interpreter/
// linq_orderby.go).
func AsNativeOrdered(v runtime.Value) (*NativeOrdered, bool) {
	if v.Kind != runtime.KindObject || v.Obj == nil {
		return nil, false
	}
	o, ok := v.Obj.Native.(*NativeOrdered)
	return o, ok
}

// NativeGrouping backs one IGrouping<TKey,TElement> LINQ GroupBy result
// group (internal/interpreter/linq_groupby.go constructs these via
// NewGroupingValue — GroupBy itself needs Machine access to invoke the
// caller's keySelector/elementSelector/IEqualityComparer<TKey>, so the
// actual grouping algorithm stays in that package; only the result TYPE
// lives here). Defined in bcl (not interpreter, where it used to live
// before this hardening pass) for the same reason NativeOrdered is: so
// NativeListItems, just below, and NativeTypeName (system_object.go) can
// both recognize it without either needing Machine access — a
// interpreter-package-local type is invisible to bcl's own plain
// natives (String.Join, List<T>.AddRange/Contains, ...), which have no
// way to import a package that itself imports bcl.
type NativeGrouping struct {
	Key   runtime.Value
	Items runtime.Value // Always an already-built native List value.
}

// NewGroupingValue wraps one GroupBy result group as a real
// IGrouping<TKey,TElement>-shaped value. items must already be a native
// List value (e.g. built via NewListValue).
func NewGroupingValue(key, items runtime.Value) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &NativeGrouping{Key: key, Items: items}})
}

// AsNativeGrouping extracts v's own *NativeGrouping, if it wraps one —
// linq_groupby.go's groupingGetKey/groupingGetEnumerator need this the
// same way AsNativeOrdered serves ThenBy.
func AsNativeGrouping(v runtime.Value) (*NativeGrouping, bool) {
	if v.Kind != runtime.KindObject || v.Obj == nil {
		return nil, false
	}
	g, ok := v.Obj.Native.(*NativeGrouping)
	return g, ok
}
