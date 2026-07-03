package interpreter

import (
	"fmt"
	"sort"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// machineNative is like bcl.Native but with Machine access — needed by
// any BCL method that must invoke a delegate argument (m.invokeFunc),
// drive an arbitrary IEnumerable<T> source via the real
// GetEnumerator/MoveNext/get_Current protocol (m.call, reusing the Fase
// 3.13 interface-dispatch fallback), or walk the real type hierarchy
// (m.typeMatches/m.ResolveType) — none of which a plain bcl.Native
// (func(args) (Value, error), no Machine) can do. First used for LINQ
// (Fase 3.15, this file); Fase 3.16 reuses it for
// Type::IsAssignableFrom (internal/interpreter/reflection.go).
type machineNative func(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error)

// machineRegistry is declared as an empty map literal (not populated
// inline) so each file's own init() can add its entries — a literal
// referencing e.g. linqSelect directly here (which transitively calls
// back into tryCall, which reads machineRegistry) trips Go's
// initialization-cycle detector, even though the actual read only ever
// happens at call time, long after init. Assigning from within a
// function body sidesteps that static check entirely.
var machineRegistry = map[string]machineNative{}

// LINQ here is eager (materializes into a []runtime.Value immediately),
// not the CLR's real lazy iterators — a deliberate simplification: a
// chained call like `xs.Where(...).Select(...).ToList()` still behaves
// identically from the caller's point of view, since every LINQ result
// is wrapped as a real, immediately-enumerable List<T>-shaped value
// (bcl.NewListValue).
func init() {
	machineRegistry["System.Linq.Enumerable::Select"] = linqSelect
	machineRegistry["System.Linq.Enumerable::Where"] = linqWhere
	machineRegistry["System.Linq.Enumerable::Any"] = linqAny
	machineRegistry["System.Linq.Enumerable::All"] = linqAll
	machineRegistry["System.Linq.Enumerable::ToList"] = linqToList
	machineRegistry["System.Linq.Enumerable::ToArray"] = linqToArray
	machineRegistry["System.Linq.Enumerable::FirstOrDefault"] = linqFirstOrDefault
	machineRegistry["System.Linq.Enumerable::SelectMany"] = linqSelectMany
	machineRegistry["System.Linq.Enumerable::Take"] = linqTake
	machineRegistry["System.Linq.Enumerable::Contains"] = linqContains
	machineRegistry["System.Linq.Enumerable::Empty"] = linqEmpty
	machineRegistry["System.Linq.Enumerable::Cast"] = linqCast
	machineRegistry["System.Linq.Enumerable::OfType"] = linqCast
	machineRegistry["System.Linq.Enumerable::First"] = linqFirst
	machineRegistry["System.Linq.Enumerable::LastOrDefault"] = linqLastOrDefault
	machineRegistry["System.Linq.Enumerable::Count"] = linqCount
	machineRegistry["System.Linq.Enumerable::Distinct"] = linqDistinct
	machineRegistry["System.Linq.Enumerable::OrderBy"] = linqOrderBy
	machineRegistry["System.Linq.Enumerable::Concat"] = linqConcat
	machineRegistry["System.Linq.Enumerable::ToDictionary"] = linqToDictionary
	machineRegistry["System.Linq.Enumerable::Max"] = linqMax
	machineRegistry["System.Collections.Generic.List`1::ForEach"] = listForEach
}

// enumerateAll drives an arbitrary IEnumerable<T>'s real iteration
// protocol to collect every element eagerly. runtime.KindArray and a
// native List<T> take a direct fast path (no need to allocate/drive a
// real enumerator when the elements are already a Go slice); anything
// else — Dictionary<K,V>, a plugin's own IEnumerable<T>, another LINQ
// result, a `yield return` iterator — goes through GetEnumerator/
// MoveNext/get_Current exactly like a real `foreach` would (virtual=true
// so the Fase 3.13 interface-dispatch fallback can redirect the declared
// IEnumerable`1/IEnumerator`1/IEnumerator names to the source's actual
// concrete type).
func (m *Machine) enumerateAll(source runtime.Value, depth int, instrCount *int64) ([]runtime.Value, error) {
	if source.Kind == runtime.KindRef && source.Ref != nil {
		source = *source.Ref
	}
	switch source.Kind {
	case runtime.KindNull:
		return nil, &runtime.ManagedException{TypeName: "System.ArgumentNullException", Message: "source"}
	case runtime.KindArray:
		if source.Arr == nil {
			return nil, nil
		}
		out := make([]runtime.Value, len(source.Arr.Elems))
		copy(out, source.Arr.Elems)
		return out, nil
	case runtime.KindObject:
		if source.Obj != nil {
			if items, ok := bcl.NativeListItems(source.Obj.Native); ok {
				out := make([]runtime.Value, len(items))
				copy(out, items)
				return out, nil
			}
		}
	}

	enumVal, _, err := m.call("System.Collections.Generic.IEnumerable`1::GetEnumerator", []runtime.Value{source}, true, depth, instrCount)
	if err != nil {
		return nil, err
	}
	var out []runtime.Value
	for {
		moved, _, err := m.call("System.Collections.IEnumerator::MoveNext", []runtime.Value{enumVal}, true, depth, instrCount)
		if err != nil {
			return nil, err
		}
		if !moved.Truthy() {
			break
		}
		cur, _, err := m.call("System.Collections.Generic.IEnumerator`1::get_Current", []runtime.Value{enumVal}, true, depth, instrCount)
		if err != nil {
			return nil, err
		}
		out = append(out, cur)
	}
	return out, nil
}

// linqInvoke calls a Func<...>/Predicate<T> argument — every LINQ method
// registered above takes exactly this shape as its second argument.
func (m *Machine) linqInvoke(fn runtime.Value, elem runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if fn.Kind != runtime.KindFunc || fn.Func == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: LINQ predicate/selector argument is not a delegate")
	}
	v, _, err := m.invokeFunc(fn.Func, []runtime.Value{elem}, depth, instrCount)
	return v, err
}

func linqSelect(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.Select expects (source, selector)")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	out := make([]runtime.Value, len(elems))
	for i, e := range elems {
		v, err := m.linqInvoke(args[1], e, depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		out[i] = v
	}
	return bcl.NewListValue(out), nil
}

func linqWhere(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.Where expects (source, predicate)")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	var out []runtime.Value
	for _, e := range elems {
		v, err := m.linqInvoke(args[1], e, depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		if v.Truthy() {
			out = append(out, e)
		}
	}
	return bcl.NewListValue(out), nil
}

// linqAny covers both Any(source) (any elements at all) and
// Any(source, predicate) (any element matching), distinguished by
// argument count like every other multi-overload native in this codebase.
func linqAny(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.Any expects a source")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) == 1 {
		return runtime.Bool(len(elems) > 0), nil
	}
	for _, e := range elems {
		v, err := m.linqInvoke(args[1], e, depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		if v.Truthy() {
			return runtime.Bool(true), nil
		}
	}
	return runtime.Bool(false), nil
}

func linqAll(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.All expects (source, predicate)")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	for _, e := range elems {
		v, err := m.linqInvoke(args[1], e, depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		if !v.Truthy() {
			return runtime.Bool(false), nil
		}
	}
	return runtime.Bool(true), nil
}

func linqToList(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.ToList expects a source")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	return bcl.NewListValue(elems), nil
}

func linqToArray(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.ToArray expects a source")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.ArrRef(&runtime.Array{Elems: elems}), nil
}

// linqFirstOrDefault covers FirstOrDefault(source) and
// FirstOrDefault(source, predicate); an empty/no-match result is Null(),
// not a real typed default(T) — same documented approximation as
// Dictionary.TryGetValue's miss case (Fase 3.13): vmnet has no generic
// type-argument info at this call site to produce a typed zero instead.
func linqFirstOrDefault(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.FirstOrDefault expects a source")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) == 1 {
		if len(elems) == 0 {
			return runtime.Null(), nil
		}
		return elems[0], nil
	}
	for _, e := range elems {
		v, err := m.linqInvoke(args[1], e, depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		if v.Truthy() {
			return e, nil
		}
	}
	return runtime.Null(), nil
}

// linqSelectMany flattens each element's own inner sequence (produced by
// invoking the selector) into one result — the selector's return value
// is enumerated the same general way any LINQ source is (m.enumerateAll),
// so an inner List<T>, array, or another LINQ result all work uniformly.
func linqSelectMany(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.SelectMany expects (source, selector)")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	var out []runtime.Value
	for _, e := range elems {
		inner, err := m.linqInvoke(args[1], e, depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		innerElems, err := m.enumerateAll(inner, depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		out = append(out, innerElems...)
	}
	return bcl.NewListValue(out), nil
}

func linqTake(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 || args[1].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.Take expects (source, count)")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	n := int(args[1].I4)
	if n < 0 {
		n = 0
	}
	if n > len(elems) {
		n = len(elems)
	}
	out := make([]runtime.Value, n)
	copy(out, elems[:n])
	return bcl.NewListValue(out), nil
}

func linqContains(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.Contains expects (source, value)")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	for _, e := range elems {
		if valuesDeepEqual(e, args[1]) {
			return runtime.Bool(true), nil
		}
	}
	return runtime.Bool(false), nil
}

func linqEmpty(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	return bcl.NewListValue(nil), nil
}

// linqCast backs both Cast<T> and OfType<T>: vmnet's runtime.Value is
// already a uniform tagged union with no reified generic type-argument
// info at this call site, so both simply pass the sequence through
// unchanged rather than attempting a real type check/filter — real
// Cast<T> would InvalidCastException on a wrong-shaped element and real
// OfType<T> would silently drop one, neither of which vmnet can
// distinguish without T. Documented approximation, same posture as
// Dictionary.TryGetValue's untyped miss case (Fase 3.13).
func linqCast(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.Cast/OfType expects a source")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	return bcl.NewListValue(elems), nil
}

// linqFirst covers First(source) and First(source, predicate) — unlike
// FirstOrDefault, an empty/no-match result throws
// InvalidOperationException, matching real Enumerable.First.
func linqFirst(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.First expects a source")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) == 1 {
		if len(elems) == 0 {
			return runtime.Value{}, &runtime.ManagedException{TypeName: "System.InvalidOperationException", Message: "Sequence contains no elements"}
		}
		return elems[0], nil
	}
	for _, e := range elems {
		v, err := m.linqInvoke(args[1], e, depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		if v.Truthy() {
			return e, nil
		}
	}
	return runtime.Value{}, &runtime.ManagedException{TypeName: "System.InvalidOperationException", Message: "Sequence contains no matching element"}
}

func linqLastOrDefault(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.LastOrDefault expects a source")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) == 1 {
		if len(elems) == 0 {
			return runtime.Null(), nil
		}
		return elems[len(elems)-1], nil
	}
	for i := len(elems) - 1; i >= 0; i-- {
		v, err := m.linqInvoke(args[1], elems[i], depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		if v.Truthy() {
			return elems[i], nil
		}
	}
	return runtime.Null(), nil
}

func linqCount(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.Count expects a source")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) == 1 {
		return runtime.Int32(int32(len(elems))), nil
	}
	n := 0
	for _, e := range elems {
		v, err := m.linqInvoke(args[1], e, depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		if v.Truthy() {
			n++
		}
	}
	return runtime.Int32(int32(n)), nil
}

func linqDistinct(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.Distinct expects a source")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	var out []runtime.Value
	for _, e := range elems {
		dup := false
		for _, o := range out {
			if valuesDeepEqual(e, o) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, e)
		}
	}
	return bcl.NewListValue(out), nil
}

func linqConcat(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.Concat expects (first, second)")
	}
	a, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	b, err := m.enumerateAll(args[1], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	out := make([]runtime.Value, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	return bcl.NewListValue(out), nil
}

// linqCompare orders two same-Kind primitive values — the numeric/string
// ordering OrderBy/Max need. vmnet has no IComparable dispatch, so a
// mismatched-Kind or non-primitive comparison (e.g. two plugin objects
// with a custom IComparable) is reported rather than guessed.
func linqCompare(a, b runtime.Value) (int, error) {
	if a.Kind != b.Kind {
		return 0, fmt.Errorf("interpreter: LINQ ordering: mismatched value kinds")
	}
	switch a.Kind {
	case runtime.KindI4:
		switch {
		case a.I4 < b.I4:
			return -1, nil
		case a.I4 > b.I4:
			return 1, nil
		default:
			return 0, nil
		}
	case runtime.KindI8:
		switch {
		case a.I8 < b.I8:
			return -1, nil
		case a.I8 > b.I8:
			return 1, nil
		default:
			return 0, nil
		}
	case runtime.KindR4:
		switch {
		case a.R4 < b.R4:
			return -1, nil
		case a.R4 > b.R4:
			return 1, nil
		default:
			return 0, nil
		}
	case runtime.KindR8:
		switch {
		case a.R8 < b.R8:
			return -1, nil
		case a.R8 > b.R8:
			return 1, nil
		default:
			return 0, nil
		}
	case runtime.KindString:
		switch {
		case a.Str < b.Str:
			return -1, nil
		case a.Str > b.Str:
			return 1, nil
		default:
			return 0, nil
		}
	default:
		return 0, fmt.Errorf("interpreter: LINQ ordering: unsupported value kind")
	}
}

func linqOrderBy(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.OrderBy expects (source, keySelector)")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	type keyed struct {
		key runtime.Value
		val runtime.Value
	}
	pairs := make([]keyed, len(elems))
	for i, e := range elems {
		k, err := m.linqInvoke(args[1], e, depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		pairs[i] = keyed{key: k, val: e}
	}
	var sortErr error
	sort.SliceStable(pairs, func(i, j int) bool {
		c, err := linqCompare(pairs[i].key, pairs[j].key)
		if err != nil {
			sortErr = err
			return false
		}
		return c < 0
	})
	if sortErr != nil {
		return runtime.Value{}, sortErr
	}
	out := make([]runtime.Value, len(pairs))
	for i, p := range pairs {
		out[i] = p.val
	}
	return bcl.NewListValue(out), nil
}

func linqMax(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.Max expects a source")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(elems) == 0 {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.InvalidOperationException", Message: "Sequence contains no elements"}
	}
	vals := elems
	if len(args) >= 2 {
		vals = make([]runtime.Value, len(elems))
		for i, e := range elems {
			v, err := m.linqInvoke(args[1], e, depth, instrCount)
			if err != nil {
				return runtime.Value{}, err
			}
			vals[i] = v
		}
	}
	best := vals[0]
	for _, v := range vals[1:] {
		c, err := linqCompare(v, best)
		if err != nil {
			return runtime.Value{}, err
		}
		if c > 0 {
			best = v
		}
	}
	return best, nil
}

// linqToDictionary covers ToDictionary(source, keySelector) and
// ToDictionary(source, keySelector, valueSelector). Keys are stringified
// via Value.String() — nativeDict only supports string keys (Fase 2's
// documented scope) — and a duplicate key overwrites rather than
// throwing ArgumentException like real ToDictionary: a pragmatic
// simplification, not yet load-bearing for any target package's real
// usage found in this loop.
func linqToDictionary(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.ToDictionary expects (source, keySelector[, valueSelector])")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	pairs := make(map[string]runtime.Value, len(elems))
	for _, e := range elems {
		k, err := m.linqInvoke(args[1], e, depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		v := e
		if len(args) >= 3 {
			v, err = m.linqInvoke(args[2], e, depth, instrCount)
			if err != nil {
				return runtime.Value{}, err
			}
		}
		pairs[k.String()] = v
	}
	return bcl.NewDictValue(pairs), nil
}

// listForEach backs List<T>.ForEach — a machine-aware native (needs to
// invoke the Action<T> argument) despite living conceptually next to
// system_collections.go's plain-native List methods; registered here
// alongside LINQ since both need machineRegistry/Machine access for the
// same reason (Fase 3.32).
func listForEach(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: List.ForEach expects (this, action)")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	for _, e := range elems {
		if _, err := m.linqInvoke(args[1], e, depth, instrCount); err != nil {
			return runtime.Value{}, err
		}
	}
	return runtime.Value{}, nil
}
