package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// Comparer<T>.Create(Comparison<T>) wraps a real delegate as an IComparer
// (Fase 3.40, found via System.IO.Packaging.PackUriHelper sorting part
// URIs) — needs Machine access to invoke that delegate, unlike
// Comparer<T>.Default's stateless natural-ordering sentinel
// (internal/bcl/system_comparer.go), so both Create and Compare live here
// rather than in bcl's plain native registry.
func init() {
	machineRegistry["System.Collections.Generic.Comparer`1::Create"] = comparerCreate
	machineRegistry["System.Collections.Generic.Comparer`1::Compare"] = comparerCompareMachine
}

type funcComparer struct {
	fn *runtime.Func
}

func comparerCreate(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 1 || args[0].Kind != runtime.KindFunc || args[0].Func == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: Comparer<T>.Create expects a Comparison<T> delegate")
	}
	return runtime.ObjRef(&runtime.Object{Native: &funcComparer{fn: args[0].Func}}), nil
}

func comparerCompareMachine(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 3 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: Comparer.Compare expects (this, x, y)")
	}
	if fc, ok := args[0].Obj.Native.(*funcComparer); ok {
		v, _, err := m.invokeFunc(fc.fn, []runtime.Value{args[1], args[2]}, depth, instrCount)
		return v, err
	}
	c, err := m.compareNatural(args[1], args[2], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(int32(c)), nil
}

// compareNatural is Comparer<T>.Default's real fallback, and every one of
// List<T>.Sort()/Array.Sort(T[])/OrderBy(keySelector)/Min/Max's own
// "no comparer argument at all" ordering (Fase 3.44 widened this from a
// Comparer<T>.Default-only helper to their shared one — see compareFunc
// just below): for a primitive/string it's bcl.CompareOrdinaryValues
// directly; for a real managed object (e.g. System.IO.Packaging's own
// ValidatedPartUri, declared `: Uri, IComparable<ValidatedPartUri>`,
// Fase 3.40) it dispatches to that type's own real, interpreted
// CompareTo(T) — the actual ordering a genuine IComparable<T>
// implementation defines, which vmnet has no way to reproduce
// generically otherwise.
//
// int?/double?/... (Nullable<T>) operands are unwrapped first: real
// Comparer<T>.Default for Nullable<T> treats an empty (HasValue == false)
// instance as sorting before every non-null value and equal to another
// empty instance — the same rule already applied to a plain null
// reference just below, so unwrapping first reuses that one branch
// instead of duplicating it.
func (m *Machine) compareNatural(a, b runtime.Value, depth int, instrCount *int64) (int, error) {
	a = bcl.UnwrapNullable(a)
	b = bcl.UnwrapNullable(b)
	if a.Kind == runtime.KindNull || b.Kind == runtime.KindNull {
		switch {
		case a.Kind == runtime.KindNull && b.Kind == runtime.KindNull:
			return 0, nil
		case a.Kind == runtime.KindNull:
			return -1, nil
		default:
			return 1, nil
		}
	}
	if a.Kind == runtime.KindObject && a.Obj != nil && a.Obj.Type != nil {
		// Dispatched against the interface name (not the concrete type),
		// virtual=true: a real IComparable<T>/IComparable implementation
		// is very often explicit (`int IComparable<T>.CompareTo(T
		// other)`), which compiles to a mangled real method name only
		// m.call's own ExplicitImplResolver fallback can find — calling
		// "<concrete>::CompareTo" directly (the plain name) misses that
		// entirely (Fase 3.40, found via System.IO.Packaging's own
		// ValidatedPartUri : Uri, IComparable<ValidatedPartUri>).
		for _, iface := range []string{"System.IComparable`1", "System.IComparable"} {
			v, _, err := m.call(iface+"::CompareTo", []runtime.Value{a, b}, true, depth, instrCount, nil, nil)
			if err == nil {
				return int(v.I4), nil
			}
		}
	}
	return bcl.CompareOrdinaryValues(a, b)
}

// compareFunc builds a (a, b) -> (signed comparison, error) closure from
// whichever shape a caller-supplied comparer argument actually has —
// List<T>.Sort/Array.Sort/OrderBy/OrderByDescending/ThenBy/
// ThenByDescending all accept the same three real overload shapes, so
// one dispatcher covers all of them:
//
//   - comparerArg == nil (no argument at that call site at all), or a
//     KindNull value (an explicit `null` IComparer<T>/Comparison<T>
//     argument — real .NET treats that exactly like the parameterless
//     overload too): natural ordering, compareNatural above.
//   - a Comparison<T> delegate (KindFunc: `(a, b) => ...`): invoked
//     directly.
//   - a real IComparer<T> instance (KindObject): dispatched through
//     ordinary virtual dispatch (m.call's own concrete-type-first
//     chain-walk) so a plugin's own override of Compare is honored,
//     exactly like any other interface-declared call site.
func (m *Machine) compareFunc(comparerArg *runtime.Value, depth int, instrCount *int64) (func(a, b runtime.Value) (int, error), error) {
	natural := func(a, b runtime.Value) (int, error) {
		return m.compareNatural(a, b, depth, instrCount)
	}
	if comparerArg == nil || comparerArg.Kind == runtime.KindNull {
		return natural, nil
	}
	switch comparerArg.Kind {
	case runtime.KindFunc:
		fn := comparerArg.Func
		return func(a, b runtime.Value) (int, error) {
			v, _, err := m.invokeFunc(fn, []runtime.Value{a, b}, depth, instrCount)
			if err != nil {
				return 0, err
			}
			return int(v.I4), nil
		}, nil
	case runtime.KindObject:
		if comparerArg.Obj == nil {
			return natural, nil
		}
		typeName, ok := receiverTypeName(*comparerArg)
		if !ok {
			// No resolvable concrete type (e.g. a bare native stub with
			// neither Type nor a NativeTypeName entry) — degrade to
			// natural ordering rather than error, same "wrong-but-
			// permissive beats an outright failure" posture the rest of
			// this codebase's documented approximations take.
			return natural, nil
		}
		arg := *comparerArg
		return func(a, b runtime.Value) (int, error) {
			v, _, err := m.call(typeName+"::Compare", []runtime.Value{arg, a, b}, true, depth, instrCount, nil, nil)
			if err != nil {
				return 0, err
			}
			return int(v.I4), nil
		}, nil
	default:
		return nil, fmt.Errorf("interpreter: unsupported comparer argument shape (Kind %v)", comparerArg.Kind)
	}
}

// equalsFunc is compareFunc's equality-only counterpart for
// Distinct/Except/Intersect/Union/GroupBy/ToHashSet's optional
// IEqualityComparer<T> overload: nil/KindNull falls back to
// defaultObjectEqual just below, a real IEqualityComparer<T> instance is
// dispatched through ordinary virtual dispatch exactly like compareFunc's
// IComparer<T> case. There is no Comparison<T>-style bare-delegate
// overload for any of these real methods (unlike Sort/OrderBy), so
// KindFunc isn't a case here.
func (m *Machine) equalsFunc(comparerArg *runtime.Value, depth int, instrCount *int64) (func(a, b runtime.Value) (bool, error), error) {
	deepEqual := func(a, b runtime.Value) (bool, error) {
		return m.defaultObjectEqual(a, b, depth, instrCount)
	}
	if comparerArg == nil || comparerArg.Kind == runtime.KindNull {
		return deepEqual, nil
	}
	if comparerArg.Kind != runtime.KindObject || comparerArg.Obj == nil {
		return deepEqual, nil
	}
	typeName, ok := receiverTypeName(*comparerArg)
	if !ok {
		return deepEqual, nil
	}
	arg := *comparerArg
	return func(a, b runtime.Value) (bool, error) {
		v, _, err := m.call(typeName+"::Equals", []runtime.Value{arg, a, b}, true, depth, instrCount, nil, nil)
		if err != nil {
			return false, err
		}
		return v.Truthy(), nil
	}, nil
}

// defaultObjectEqual is Distinct/GroupBy/Except/Intersect/Union's
// "no explicit IEqualityComparer<T> supplied" default equality — probed
// against a hand-written fixture and found genuinely wrong for a very
// common real GroupBy pattern: `xs.GroupBy(x => new { x.A, x.B })`
// (grouping by more than one field via an anonymous-type key). An
// anonymous type is a real heap object (KindObject) with a compiler-
// generated, value-based override of Equals(object) — plain
// valuesDeepEqual's KindObject case (arithmetic.go's refEqual) only ever
// does reference equality, so two anonymous-type instances with equal
// field values were wrongly treated as different keys, silently
// splitting what should have been one GroupBy group into several.
//
// Dispatching the receiver's own real Equals (virtual, so a genuine
// override is found before any base fallback) fixes that generally for
// every KindObject pair — not just anonymous types, any plugin class
// overriding Equals — while degrading to valuesDeepEqual's existing
// reference-equality behavior whenever no override resolves (a plain
// class with no override, or a native-backed receiver with no
// interpreted Equals at all), which is exactly what real
// System.Object::Equals would have computed anyway. KindStruct (a
// ValueTuple<...> multi-key, e.g. `.GroupBy(x => (x.A, x.B))`) already
// gets correct field-wise comparison from valuesDeepEqual directly (no
// Equals override involved for a plain tuple), so only the KindObject
// case needs this extra dispatch attempt.
func (m *Machine) defaultObjectEqual(a, b runtime.Value, depth int, instrCount *int64) (bool, error) {
	if a.Kind == runtime.KindObject && a.Obj != nil && b.Kind == runtime.KindObject && b.Obj != nil {
		if typeName, ok := receiverTypeName(a); ok {
			v, _, err := m.call(typeName+"::Equals", []runtime.Value{a, b}, true, depth, instrCount, nil, nil)
			if err == nil {
				return v.Truthy(), nil
			}
		}
	}
	return valuesDeepEqual(a, b), nil
}
