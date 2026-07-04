package interpreter

import (
	"fmt"
	"sort"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// OrderBy/OrderByDescending/ThenBy/ThenByDescending — Fase 3.44 replaced
// the exact-Kind-match-only, single-key OrderBy/OrderByDescending pair
// that used to live in linq.go with this general version (ThenBy/
// ThenByDescending didn't exist at all before). ThenBy in particular
// can't just be "sort again by the new key": that would treat the new
// key as primary and silently throw away the first OrderBy's own
// ordering for every tie under the new key — the opposite of
// `xs.OrderBy(a).ThenBy(b)`, which must break only a's OWN ties using b,
// preserving a's order everywhere else. bcl.NativeOrdered
// (system_linq_native.go) keeps both the original pre-sort element order
// and every key applied so far alongside its current sorted Items, so
// ThenBy can recompute the full composite-key sort from scratch
// (materializeOrdered below) rather than naively re-sorting the
// already-sorted result by the new key alone.
func init() {
	machineRegistry["System.Linq.Enumerable::OrderBy"] = linqOrderBy
	machineRegistry["System.Linq.Enumerable::OrderByDescending"] = linqOrderByDescending
	machineRegistry["System.Linq.Enumerable::ThenBy"] = linqThenBy
	machineRegistry["System.Linq.Enumerable::ThenByDescending"] = linqThenByDescending
	machineRegistry["VmnetInternal.Ordered::GetEnumerator"] = orderedGetEnumerator
}

// linqOrderBy covers OrderBy(source, keySelector) and OrderBy(source,
// keySelector, comparer) — the comparer, when present, is a real
// IComparer<TKey> instance (KindObject), never a delegate: OrderBy has
// no Comparison<TKey>-only overload (unlike Sort), so no KindFunc
// disambiguation is needed here the way listSort/arraySort need it.
func linqOrderBy(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	return newOrderedChain(m, args, false, depth, instrCount)
}

func linqOrderByDescending(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	return newOrderedChain(m, args, true, depth, instrCount)
}

func newOrderedChain(m *Machine, args []runtime.Value, descending bool, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 2 || len(args) > 3 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.OrderBy/OrderByDescending expects (source, keySelector[, comparer])")
	}
	if args[1].Kind != runtime.KindFunc {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.OrderBy: keySelector is not a delegate")
	}
	source, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	key := bcl.OrderKey{Selector: args[1], Descending: descending}
	if len(args) == 3 {
		key.Comparer = args[2]
	}
	keys := []bcl.OrderKey{key}
	sorted, err := m.materializeOrdered(source, keys, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	return bcl.NewOrderedValue(sorted, source, keys), nil
}

// linqThenBy/linqThenByDescending append one more key onto an existing
// OrderBy/ThenBy chain and recompute the full sort. Real ThenBy is only
// ever called on an IOrderedEnumerable<T> (the C# compiler won't emit it
// on anything else), so args[0] is always the bcl.NativeOrdered a prior
// OrderBy call produced — the "not actually one" branch below is a
// defensive fallback (treat it as a fresh single-key OrderBy) rather
// than an error, in case some other path ever hands ThenBy a plain
// sequence.
func linqThenBy(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	return appendOrderedKey(m, args, false, depth, instrCount)
}

func linqThenByDescending(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	return appendOrderedKey(m, args, true, depth, instrCount)
}

func appendOrderedKey(m *Machine, args []runtime.Value, descending bool, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 2 || len(args) > 3 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.ThenBy/ThenByDescending expects (source, keySelector[, comparer])")
	}
	if args[1].Kind != runtime.KindFunc {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.ThenBy: keySelector is not a delegate")
	}
	key := bcl.OrderKey{Selector: args[1], Descending: descending}
	if len(args) == 3 {
		key.Comparer = args[2]
	}
	source := []runtime.Value(nil)
	var priorKeys []bcl.OrderKey
	if ord, ok := bcl.AsNativeOrdered(args[0]); ok {
		source = ord.Source
		priorKeys = ord.Keys
	} else {
		elems, err := m.enumerateAll(args[0], depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		source = elems
	}
	keys := make([]bcl.OrderKey, len(priorKeys)+1)
	copy(keys, priorKeys)
	keys[len(priorKeys)] = key
	sorted, err := m.materializeOrdered(source, keys, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	return bcl.NewOrderedValue(sorted, source, keys), nil
}

// orderedGetEnumerator is a bcl.NativeOrdered's real GetEnumerator —
// Items is already fully sorted (newOrderedChain/appendOrderedKey sort
// eagerly, at construction time), so this is a plain delegation to
// List<T>'s own enumerator, reached whenever something drives a
// NativeOrdered receiver through the IEnumerable<T>/IOrderedEnumerable<T>
// interface directly instead of via enumerateAll's NativeListItems fast
// path (e.g. a plugin's own generic method taking `IEnumerable<T> xs`
// and foreach-ing it, receiver-typed as the interface at that call site).
func orderedGetEnumerator(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: VmnetInternal.Ordered::GetEnumerator expects a receiver")
	}
	ord, ok := bcl.AsNativeOrdered(args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: VmnetInternal.Ordered::GetEnumerator: receiver is not an ordered sequence")
	}
	v, _, err := m.call("System.Collections.Generic.List`1::GetEnumerator", []runtime.Value{bcl.NewListValue(ord.Items)}, false, depth, instrCount, nil, nil)
	return v, err
}

// materializeOrdered computes every key (once per element per key, not
// per comparison — a keySelector could be expensive or, in principle,
// impure, and real OrderedEnumerable<T> only ever evaluates each key
// once too) and does ONE stable sort over the full key tuple, most
// significant first: a tie on key[0] falls through to key[1], and so on,
// exactly matching `OrderBy(k0).ThenBy(k1).ThenBy(k2)...` semantics.
func (m *Machine) materializeOrdered(source []runtime.Value, keys []bcl.OrderKey, depth int, instrCount *int64) ([]runtime.Value, error) {
	n := len(source)
	keyVals := make([][]runtime.Value, n)
	for i, e := range source {
		row := make([]runtime.Value, len(keys))
		for k, spec := range keys {
			v, err := m.linqInvoke(spec.Selector, e, depth, instrCount)
			if err != nil {
				return nil, err
			}
			row[k] = v
		}
		keyVals[i] = row
	}
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	compareKey := make([]func(a, b runtime.Value) (int, error), len(keys))
	for k, spec := range keys {
		var comparerArg *runtime.Value
		if spec.Comparer.Kind != runtime.KindNull {
			c := spec.Comparer
			comparerArg = &c
		}
		less, err := m.compareFunc(comparerArg, depth, instrCount)
		if err != nil {
			return nil, err
		}
		compareKey[k] = less
	}
	var sortErr error
	sort.SliceStable(idx, func(a, b int) bool {
		ia, ib := idx[a], idx[b]
		for k, spec := range keys {
			c, err := compareKey[k](keyVals[ia][k], keyVals[ib][k])
			if err != nil {
				sortErr = err
				return false
			}
			if spec.Descending {
				c = -c
			}
			if c != 0 {
				return c < 0
			}
		}
		return false
	})
	if sortErr != nil {
		return nil, sortErr
	}
	out := make([]runtime.Value, n)
	for i, id := range idx {
		out[i] = source[id]
	}
	return out, nil
}
