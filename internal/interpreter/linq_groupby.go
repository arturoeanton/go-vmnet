package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// System.Linq.Enumerable.GroupBy (Fase 3.43, found reading a real .xlsx
// through ClosedXML 0.105.0's `new XLWorkbook(stream)`): ClosedXML's own
// IXLStylized.ModifyStyle (real decompiled source, /tmp/closedxml_ns20/
// ClosedXML.Excel/XLStylizedBase.cs:108) does
//
//	GetChildrenRecursively(this).GroupBy(child => child.StyleValue, _comparer)
//	// then, per group:  item.Key / foreach (var x in item)
//
// on the document-READ path (loading a real worksheet's styles applies
// them through ModifyStyle). GroupBy is implemented machineRegistry-style
// exactly like every other linq.go native, with the standard overload
// shapes disambiguated structurally (a selector is always KindFunc, a
// comparer never is — same convention real Enumerable's own overload set
// guarantees):
//
//	GroupBy(source, keySelector)
//	GroupBy(source, keySelector, comparer)
//	GroupBy(source, keySelector, elementSelector)
//	GroupBy(source, keySelector, elementSelector, comparer)
//	GroupBy(source, keySelector, elementSelector, resultSelector[, comparer])
//
// Known gap, documented rather than guessed at: the two-selector
// `GroupBy(source, keySelector, resultSelector)` overload (resultSelector
// taking (key, group)) is structurally identical to the elementSelector
// shape at the value level — vmnet's runtime.Func carries no declared
// arity to tell them apart — and is treated as elementSelector here. No
// call site in ClosedXML/DocumentFormat.OpenXml uses that overload
// (grepped the full decompiled surface: every real GroupBy there is
// keySelector-only, keySelector+comparer, or the full three-selector
// shape).
//
// Grouping identity honors a caller-supplied IEqualityComparer<TKey> by
// really calling its Equals through the ordinary virtual-dispatch path
// (the same way real GroupBy consults it — ClosedXML's _comparer is a
// real, load-bearing custom comparer over XLStyleValue), and falls back
// to valuesDeepEqual (linqDistinct's own default-equality posture)
// otherwise. Each emitted group is a bcl.NativeGrouping (system_linq_
// native.go) — see its own doc comment for why the result TYPE lives in
// bcl despite the actual grouping algorithm living here.
func init() {
	machineRegistry["System.Linq.Enumerable::GroupBy"] = linqGroupBy
	machineRegistry["VmnetInternal.Grouping::get_Key"] = groupingGetKey
	machineRegistry["VmnetInternal.Grouping::GetEnumerator"] = groupingGetEnumerator
}

func linqGroupBy(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.GroupBy expects (source, keySelector, ...)")
	}
	source, keySel := args[0], args[1]
	if keySel.Kind != runtime.KindFunc {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.GroupBy: keySelector is not a delegate")
	}

	// Structural overload disambiguation — see this file's top doc comment.
	var elemSel, resultSel, comparer runtime.Value
	rest := args[2:]
	if len(rest) > 0 && rest[len(rest)-1].Kind != runtime.KindFunc && rest[len(rest)-1].Kind != runtime.KindNull {
		comparer = rest[len(rest)-1]
		rest = rest[:len(rest)-1]
	}
	switch len(rest) {
	case 0:
	case 1:
		if rest[0].Kind == runtime.KindFunc {
			elemSel = rest[0]
		}
	case 2:
		elemSel, resultSel = rest[0], rest[1]
	default:
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.GroupBy: unsupported argument shape (%d args)", len(args))
	}

	elems, err := m.enumerateAll(source, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}

	keysEqual := func(a, b runtime.Value) (bool, error) {
		if comparer.Kind == runtime.KindObject && comparer.Obj != nil {
			eq, _, err := m.call("System.Collections.Generic.IEqualityComparer`1::Equals", []runtime.Value{comparer, a, b}, true, depth, instrCount, nil, nil)
			if err != nil {
				return false, err
			}
			return eq.Kind == runtime.KindI4 && eq.I4 != 0, nil
		}
		// defaultObjectEqual (comparer.go, Fase 3.44), not a bare
		// valuesDeepEqual — the dominant real "GroupBy with multiple
		// keys" pattern is `GroupBy(x => new { x.A, x.B })`, an anonymous-
		// type key with a compiler-generated, value-based Equals override
		// that plain valuesDeepEqual's KindObject case (reference equality
		// only) can't see, silently splitting one true group into several
		// (found via this exact probe: `GroupBy(e => new { e.Dept, High =
		// e.Salary >= 60 })` produced two separate "Eng:True" groups of 1
		// instead of one real group of 2).
		return m.defaultObjectEqual(a, b, depth, instrCount)
	}

	// Sequential scan preserving first-appearance group order — exactly
	// real GroupBy's documented ordering (keys in order of first
	// occurrence, elements in source order within each group).
	type group struct {
		key   runtime.Value
		items []runtime.Value
	}
	var groups []*group
	for _, e := range elems {
		k, err := m.linqInvoke(keySel, e, depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		item := e
		if elemSel.Kind == runtime.KindFunc {
			item, err = m.linqInvoke(elemSel, e, depth, instrCount)
			if err != nil {
				return runtime.Value{}, err
			}
		}
		placed := false
		for _, g := range groups {
			eq, err := keysEqual(g.key, k)
			if err != nil {
				return runtime.Value{}, err
			}
			if eq {
				g.items = append(g.items, item)
				placed = true
				break
			}
		}
		if !placed {
			groups = append(groups, &group{key: k, items: []runtime.Value{item}})
		}
	}

	out := make([]runtime.Value, 0, len(groups))
	for _, g := range groups {
		items := bcl.NewListValue(g.items)
		if resultSel.Kind == runtime.KindFunc {
			// GroupBy(..., resultSelector): project (key, group) directly,
			// never materializing an IGrouping — matching the real overload.
			v, _, err := m.invokeFunc(resultSel.Func, []runtime.Value{g.key, items}, depth, instrCount)
			if err != nil {
				return runtime.Value{}, err
			}
			out = append(out, v)
			continue
		}
		out = append(out, bcl.NewGroupingValue(g.key, items))
	}
	return bcl.NewListValue(out), nil
}

func groupingGetKey(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	g, err := groupingReceiver(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return g.Key, nil
}

func groupingGetEnumerator(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	g, err := groupingReceiver(args)
	if err != nil {
		return runtime.Value{}, err
	}
	v, _, err := m.call("System.Collections.Generic.List`1::GetEnumerator", []runtime.Value{g.Items}, false, depth, instrCount, nil, nil)
	return v, err
}

func groupingReceiver(args []runtime.Value) (*bcl.NativeGrouping, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("interpreter: Grouping method called without a receiver")
	}
	g, ok := bcl.AsNativeGrouping(args[0])
	if !ok {
		return nil, fmt.Errorf("interpreter: Grouping method: receiver is not a grouping")
	}
	return g, nil
}
