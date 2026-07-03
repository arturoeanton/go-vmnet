package interpreter

import (
	"fmt"

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
