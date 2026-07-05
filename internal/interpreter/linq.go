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
	// FirstOrDefault/LastOrDefault/SingleOrDefault<T> (unlike the plain
	// machineRegistry entries around them) need their own call site's
	// real generic type argument — an empty/no-match result must answer
	// with a real typed default(T), not always Null(), the same
	// "genuinely needs the call site's T" reasoning OfType<T> below
	// already documents (Fase 3.66, found via AutoMapper's own
	// ObjectFactory.CallConstructor: FirstOrDefault<ValueTuple`2<
	// ConstructorInfo, ParameterInfo[]>>() on an empty sequence used to
	// answer Null(), and the very next instruction's `ldfld ...::Item2`
	// crashed with a NullReferenceException — a real ValueTuple`2 zero
	// value would have let real AutoMapper code's own `brtrue` branch
	// correctly treat it as "no matching constructor found").
	genericMachineRegistry["System.Linq.Enumerable::FirstOrDefault"] = linqFirstOrDefault
	genericMachineRegistry["System.Linq.Enumerable::LastOrDefault"] = linqLastOrDefault
	genericMachineRegistry["System.Linq.Enumerable::SingleOrDefault"] = linqSingleOrDefault
	machineRegistry["System.Linq.Enumerable::SelectMany"] = linqSelectMany
	machineRegistry["System.Linq.Enumerable::Take"] = linqTake
	machineRegistry["System.Linq.Enumerable::Contains"] = linqContains
	machineRegistry["System.Linq.Enumerable::Empty"] = linqEmpty
	machineRegistry["System.Linq.Enumerable::Cast"] = linqCast
	// OfType<T> (unlike Cast<T>) needs its own call site's real generic
	// argument to filter correctly — see genericMachineRegistry's own
	// entry below and linqOfType's doc comment for why this can no longer
	// share linqCast's plain machineRegistry registration (Fase 3.42).
	genericMachineRegistry["System.Linq.Enumerable::OfType"] = linqOfType
	machineRegistry["System.Linq.Enumerable::First"] = linqFirst
	machineRegistry["System.Linq.Enumerable::Count"] = linqCount
	machineRegistry["System.Linq.Enumerable::Distinct"] = linqDistinct
	// OrderBy/OrderByDescending/ThenBy/ThenByDescending are registered by
	// linq_orderby.go's own init() (Fase 3.44's hardening pass replaced the
	// exact-Kind-match-only linqOrderBy that used to live here with a
	// version supporting ThenBy chaining, an explicit IComparer<T>/
	// Comparison<T> argument, and real IComparable dispatch).
	machineRegistry["System.Linq.Enumerable::Concat"] = linqConcat
	machineRegistry["System.Linq.Enumerable::ToDictionary"] = linqToDictionary
	machineRegistry["System.Linq.Enumerable::Max"] = linqMax
	machineRegistry["System.Linq.Enumerable::Min"] = linqMin
	machineRegistry["System.Linq.Enumerable::Sum"] = linqSum
	machineRegistry["System.Linq.Enumerable::Average"] = linqAverage
	machineRegistry["System.Linq.Enumerable::Aggregate"] = linqAggregate
	machineRegistry["System.Linq.Enumerable::Zip"] = linqZip
	machineRegistry["System.Linq.Enumerable::Except"] = linqExcept
	machineRegistry["System.Linq.Enumerable::Intersect"] = linqIntersect
	machineRegistry["System.Linq.Enumerable::SkipWhile"] = linqSkipWhile
	machineRegistry["System.Linq.Enumerable::TakeWhile"] = linqTakeWhile
	machineRegistry["System.Linq.Enumerable::Reverse"] = linqReverse
	machineRegistry["System.Linq.Enumerable::AsEnumerable"] = linqCast
	machineRegistry["System.Linq.Enumerable::ToHashSet"] = linqToHashSet
	machineRegistry["System.Collections.Generic.List`1::ForEach"] = listForEach
	machineRegistry["System.Linq.Enumerable::Single"] = linqSingle
	machineRegistry["System.Linq.Enumerable::ElementAt"] = linqElementAt
	machineRegistry["System.Linq.Enumerable::Skip"] = linqSkip
	machineRegistry["System.Linq.Enumerable::Union"] = linqUnion
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

	enumVal, _, err := m.call("System.Collections.Generic.IEnumerable`1::GetEnumerator", []runtime.Value{source}, true, depth, instrCount, nil, nil)
	if err != nil {
		return nil, err
	}
	var out []runtime.Value
	for {
		moved, _, err := m.call("System.Collections.IEnumerator::MoveNext", []runtime.Value{enumVal}, true, depth, instrCount, nil, nil)
		if err != nil {
			return nil, err
		}
		if !moved.Truthy() {
			break
		}
		cur, _, err := m.call("System.Collections.Generic.IEnumerator`1::get_Current", []runtime.Value{enumVal}, true, depth, instrCount, nil, nil)
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
// FirstOrDefault(source, predicate). An empty/no-match result answers
// with a real typed default(T) when the call site's own generic type
// argument is available (methodGenericArgs, Fase 3.66 — genuinely needed
// for a value-typed T: Null() there is not just "an approximation", it's
// a WRONG Kind that later crashes as a NullReferenceException the
// instant real code reads one of the zero value's own fields, e.g. real
// AutoMapper code doing `FirstOrDefault<ValueTuple<...>>().Item2`,
// mirroring Dictionary.TryGetValue's own analogous miss-case gap, Fase
// 3.13). Falls back to Null() when methodGenericArgs isn't available
// (e.g. a call reached through a path that doesn't thread it through
// yet) — no worse than this native's own previous, unconditional
// behavior.
func linqFirstOrDefault(m *Machine, args []runtime.Value, methodGenericArgs []string, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.FirstOrDefault expects a source")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	empty := linqOrDefaultZero(m, methodGenericArgs)
	if len(args) == 1 {
		if len(elems) == 0 {
			return empty, nil
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
	return empty, nil
}

// linqOrDefaultZero is FirstOrDefault/LastOrDefault/SingleOrDefault's
// shared "no result" answer: a real typed default(T) via
// Machine.defaultValueFor when the call site's own generic type argument
// is known, Null() otherwise (see linqFirstOrDefault's own doc comment).
func linqOrDefaultZero(m *Machine, methodGenericArgs []string) runtime.Value {
	if len(methodGenericArgs) < 1 || methodGenericArgs[0] == "" {
		return runtime.Null()
	}
	return m.defaultValueFor(methodGenericArgs[0])
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

// linqCast backs Cast<T>: vmnet's runtime.Value is already a uniform
// tagged union with no reified generic type-argument info at this call
// site (registered via plain machineRegistry, not genericMachineRegistry
// — see init() above), so it simply passes the sequence through
// unchanged rather than attempting a real type check — real Cast<T>
// would InvalidCastException on a wrong-shaped element, which vmnet
// can't distinguish without T. Documented approximation, same posture as
// Dictionary.TryGetValue's untyped miss case (Fase 3.13).
//
// OfType<T> used to share this exact function (and its "no T available"
// excuse) until Fase 3.42 found a real, load-bearing case where the
// silent pass-through actively corrupts a document read rather than just
// degrading gracefully — see linqOfType's own doc comment just below.
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

// linqOfType backs OfType<T> (Fase 3.42, found running a real .xlsx
// through ClosedXML 0.105.0/DocumentFormat.OpenXml 3.1.1's own
// `new XLWorkbook(stream)`): unlike Cast<T> (linqCast, just above),
// OfType<T>'s real call site IS a genuine, closed generic-method
// instantiation (`Enumerable.OfType<CustomFilePropertiesPart>()`, e.g.),
// whose own MethodSpec token gives us T just like LoadDomTree<T>'s call
// site does (see loaddomtree.go's own doc comment) — so, unlike Cast<T>,
// there's no real excuse left to skip the actual filter.
//
// The bug this fixes was found via real decompiled source
// (DocumentFormat.OpenXml.Packaging/OpenXmlPartContainer.cs:1266-1277,
// GetSubPartOfType<T>) and confirmed by temporary tracing: every
// `SomePart? Foo => ((OpenXmlPartContainer)this).GetSubPartOfType<Foo>()`
// accessor across the whole real OpenXml SDK (WorkbookPart.SharedString
// TablePart, SpreadsheetDocument.CustomFilePropertiesPart, dozens more)
// is built on `GetPartsOfType<T>().GetEnumerator().MoveNext()`, and
// GetPartsOfType<T> is just `ChildrenRelationshipParts.Parts.OfType<T>()`
// — a real LINQ filter over ALL of a container's child parts, of every
// type, not just T's. With the old Cast<T>-shared pass-through, asking
// for an OPTIONAL part that genuinely doesn't exist in the real package
// (here: docProps/custom.xml, so no CustomFilePropertiesPart at all)
// didn't return an empty sequence like real OfType<T> would — it
// returned the container's FIRST child part of ANY type unfiltered
// (confirmed via tracing: SpreadsheetDocument.CustomFilePropertiesPart
// resolved to the package's own WorkbookPart, since that's first in
// ChildrenRelationshipParts.Parts). The caller then ran
// CustomFilePropertiesPart::get_Properties with a WorkbookPart receiver,
// which went on to call OpenXmlPart::LoadDomTree<DocumentFormat.OpenXml.
// CustomProperties.Properties>() against the WORKBOOK part's own real
// stream (xl/workbook.xml) — parsing its real root element ("workbook")
// against the wrong expected QName ("Properties") and throwing a very
// real, but entirely vmnet-induced, Fmt_PartRootIsInvalid.
//
// isAssignableTo (typecheck.go, Fase 3.8's real isinst/castclass check)
// already implements exactly the "does this value's real runtime type
// equal-or-derive-from T" walk OfType<T> needs — reused here rather than
// duplicated. methodGenericArgs[0] being "" (an unresolved open type
// parameter some other call shape couldn't close, same caveat
// LoadDomTree<T>/AddChild<T> document) degrades to the old unfiltered
// pass-through rather than filtering everything out — wrong-but-
// permissive beats wrong-but-empty when T itself is unknown.
func linqOfType(m *Machine, args []runtime.Value, methodGenericArgs []string, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.OfType expects a source")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(methodGenericArgs) < 1 || methodGenericArgs[0] == "" {
		return bcl.NewListValue(elems), nil
	}
	target := methodGenericArgs[0]
	filtered := make([]runtime.Value, 0, len(elems))
	for _, e := range elems {
		if e.Kind == runtime.KindNull {
			continue
		}
		if m.isAssignableTo(e, target) {
			filtered = append(filtered, e)
		}
	}
	return bcl.NewListValue(filtered), nil
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

func linqLastOrDefault(m *Machine, args []runtime.Value, methodGenericArgs []string, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.LastOrDefault expects a source")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	empty := linqOrDefaultZero(m, methodGenericArgs)
	if len(args) == 1 {
		if len(elems) == 0 {
			return empty, nil
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
	return empty, nil
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

// linqDistinct's optional IEqualityComparer<T> argument (and its default,
// no-comparer equality) both go through equalsFunc (comparer.go) — a
// KindObject element's own real, possibly-overridden Equals is what real
// Distinct() would consult, not a blind reference comparison (Fase 3.44,
// found via `xs.Distinct(new CaseInsensitiveComparer())` and, for the
// default case, an anonymous-type element silently never deduplicating).
func linqDistinct(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 || len(args) > 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.Distinct expects a source[, comparer]")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	var comparerArg *runtime.Value
	if len(args) == 2 {
		comparerArg = &args[1]
	}
	eq, err := m.equalsFunc(comparerArg, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	var out []runtime.Value
	for _, e := range elems {
		dup := false
		for _, o := range out {
			same, err := eq(e, o)
			if err != nil {
				return runtime.Value{}, err
			}
			if same {
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

// linqMax/linqMin share this: both walk the (optionally selector-mapped)
// sequence keeping whichever element compareNatural (comparer.go, real
// IComparable dispatch, not the old exact-Kind-match-only linqCompare
// this hardening pass removed) ranks furthest in the wanted direction.
// Real Enumerable.Min/Max also have this same "throws on an empty
// non-nullable source" behavior — a nullable-typed source (e.g.
// Min(IEnumerable<int?>)) instead returns null on empty/all-null, which
// linqMinMax also matches: bcl.UnwrapNullable turns an empty Nullable<T>
// element into a genuine KindNull, and a KindNull participant is simply
// skipped rather than compared (compareNatural would otherwise sort every
// null first, corrupting the real answer).
func linqMax(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	return linqMinMax(m, args, 1, depth, instrCount)
}

func linqMin(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	return linqMinMax(m, args, -1, depth, instrCount)
}

// linqMinMax implements both: want=1 keeps the greater of each pair
// (Max), want=-1 keeps the lesser (Min).
func linqMinMax(m *Machine, args []runtime.Value, want int, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.Max/Min expects a source")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
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
	var best runtime.Value
	haveBest := false
	sawAny := false
	for _, raw := range vals {
		sawAny = true
		v := bcl.UnwrapNullable(raw)
		if v.Kind == runtime.KindNull {
			continue
		}
		if !haveBest {
			best, haveBest = v, true
			continue
		}
		c, err := m.compareNatural(v, best, depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		if c*want > 0 {
			best = v
		}
	}
	if !haveBest {
		if !sawAny {
			return runtime.Value{}, &runtime.ManagedException{TypeName: "System.InvalidOperationException", Message: "Sequence contains no elements"}
		}
		// Every element was an empty Nullable<T> — the nullable overload's
		// own "no value at all" result, matching real Min/Max<T?>.
		return runtime.Null(), nil
	}
	return best, nil
}

// linqSum/linqAverage both skip a null participant (an empty Nullable<T>
// element, e.g. Sum(IEnumerable<int?>)) exactly like real .NET's nullable
// overloads do — a null contributes neither to the running total nor to
// Average's own divisor. The accumulation Kind (int64 for I4/I8 sources,
// float64 for R4/R8) is picked from the first non-null element seen;
// Sum's own result is cast back to I4 when every contributing element
// was I4 (the common `Sum(IEnumerable<int>)` case returns int, not
// long), matching the real per-T overload set without needing vmnet to
// actually resolve which overload was called.
func linqSum(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.Sum expects a source")
	}
	vals, err := linqNumericVals(m, args, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	var isFloat, isI8, any bool
	var isum int64
	var fsum float64
	for _, v := range vals {
		v = bcl.UnwrapNullable(v)
		switch v.Kind {
		case runtime.KindNull:
			continue
		case runtime.KindI4:
			isum += int64(v.I4)
			any = true
		case runtime.KindI8:
			isum += v.I8
			isI8, any = true, true
		case runtime.KindR4:
			fsum += float64(v.R4)
			isFloat, any = true, true
		case runtime.KindR8:
			fsum += v.R8
			isFloat, any = true, true
		default:
			return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.Sum: unsupported value kind %v", v.Kind)
		}
	}
	if !any {
		return runtime.Int32(0), nil
	}
	if isFloat {
		return runtime.Float64(fsum + float64(isum)), nil
	}
	if isI8 {
		return runtime.Int64(isum), nil
	}
	return runtime.Int32(int32(isum)), nil
}

func linqAverage(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.Average expects a source")
	}
	vals, err := linqNumericVals(m, args, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	var sum float64
	var count int
	allFloat32 := true
	for _, v := range vals {
		v = bcl.UnwrapNullable(v)
		switch v.Kind {
		case runtime.KindNull:
			continue
		case runtime.KindI4:
			sum += float64(v.I4)
			count++
			allFloat32 = false
		case runtime.KindI8:
			sum += float64(v.I8)
			count++
			allFloat32 = false
		case runtime.KindR4:
			sum += float64(v.R4)
			count++
		case runtime.KindR8:
			sum += float64(v.R8)
			count++
			allFloat32 = false
		default:
			return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.Average: unsupported value kind %v", v.Kind)
		}
	}
	if count == 0 {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.InvalidOperationException", Message: "Sequence contains no elements"}
	}
	if allFloat32 {
		return runtime.Float32(float32(sum / float64(count))), nil
	}
	return runtime.Float64(sum / float64(count)), nil
}

// linqNumericVals is Sum/Average's shared "optionally project through a
// selector" step, factored out since both need exactly the same shape
// (source alone, or source+selector).
func linqNumericVals(m *Machine, args []runtime.Value, depth int, instrCount *int64) ([]runtime.Value, error) {
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return nil, err
	}
	if len(args) == 1 {
		return elems, nil
	}
	vals := make([]runtime.Value, len(elems))
	for i, e := range elems {
		v, err := m.linqInvoke(args[1], e, depth, instrCount)
		if err != nil {
			return nil, err
		}
		vals[i] = v
	}
	return vals, nil
}

// linqAggregate covers all three real overloads: Aggregate(source, func)
// (no seed — the first element becomes the seed, matching real
// Enumerable.Aggregate's own documented behavior, including its
// InvalidOperationException on an empty source), Aggregate(source, seed,
// func), and Aggregate(source, seed, func, resultSelector). func is
// always a 2-argument Func<TAccumulate,TSource,TAccumulate> — the one
// LINQ shape linqInvoke (1-argument selectors/predicates) doesn't cover,
// so this invokes the delegate directly instead.
func linqAggregate(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 2 || len(args) > 4 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.Aggregate expects (source, func) or (source, seed, func[, resultSelector])")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	var acc runtime.Value
	var fn runtime.Value
	var resultSel runtime.Value
	if len(args) == 2 {
		if len(elems) == 0 {
			return runtime.Value{}, &runtime.ManagedException{TypeName: "System.InvalidOperationException", Message: "Sequence contains no elements"}
		}
		acc, fn = elems[0], args[1]
		elems = elems[1:]
	} else {
		acc, fn = args[1], args[2]
		if len(args) == 4 {
			resultSel = args[3]
		}
	}
	if fn.Kind != runtime.KindFunc || fn.Func == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.Aggregate: func is not a delegate")
	}
	for _, e := range elems {
		v, _, err := m.invokeFunc(fn.Func, []runtime.Value{acc, e}, depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		acc = v
	}
	if resultSel.Kind == runtime.KindFunc {
		return m.linqInvoke(resultSel, acc, depth, instrCount)
	}
	return acc, nil
}

// linqZip covers Zip(first, second, resultSelector) — the length of the
// shorter sequence, matching real Enumerable.Zip (it stops as soon as
// either source is exhausted rather than throwing).
func linqZip(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 3 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.Zip expects (first, second, resultSelector)")
	}
	a, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	b, err := m.enumerateAll(args[1], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	fn := args[2]
	if fn.Kind != runtime.KindFunc || fn.Func == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.Zip: resultSelector is not a delegate")
	}
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	out := make([]runtime.Value, n)
	for i := 0; i < n; i++ {
		v, _, err := m.invokeFunc(fn.Func, []runtime.Value{a[i], b[i]}, depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		out[i] = v
	}
	return bcl.NewListValue(out), nil
}

// linqExcept/linqIntersect are Union's own set-algebra siblings: Except
// keeps first's elements that never equal (by comparer, or
// valuesDeepEqual) any of second's; Intersect keeps first's elements
// that equal at least one of second's. Both dedupe their own result and
// preserve first-occurrence order, same posture as Distinct/Union.
func linqExcept(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	return linqSetOp(m, args, false, depth, instrCount)
}

func linqIntersect(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	return linqSetOp(m, args, true, depth, instrCount)
}

func linqSetOp(m *Machine, args []runtime.Value, wantIn bool, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 2 || len(args) > 3 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.Except/Intersect expects (first, second[, comparer])")
	}
	a, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	b, err := m.enumerateAll(args[1], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	var comparerArg *runtime.Value
	if len(args) == 3 {
		comparerArg = &args[2]
	}
	eq, err := m.equalsFunc(comparerArg, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	inSecond := func(v runtime.Value) (bool, error) {
		for _, o := range b {
			same, err := eq(v, o)
			if err != nil {
				return false, err
			}
			if same {
				return true, nil
			}
		}
		return false, nil
	}
	var out []runtime.Value
	for _, e := range a {
		found, err := inSecond(e)
		if err != nil {
			return runtime.Value{}, err
		}
		if found != wantIn {
			continue
		}
		dup := false
		for _, o := range out {
			same, err := eq(e, o)
			if err != nil {
				return runtime.Value{}, err
			}
			if same {
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

// linqSkipWhile/linqTakeWhile cover the predicate-only overloads
// (SkipWhile(source, Func<T,bool>)/TakeWhile(source, Func<T,bool>)) —
// the (item, index) overload is a known, documented gap (like GroupBy's
// resultSelector overload, linq_groupby.go): vmnet's runtime.Func carries
// no declared arity to structurally tell the two overloads apart the way
// GroupBy's own argument-count/Kind disambiguation works, and the
// predicate-only shape is by far the dominant real-world pattern.
func linqSkipWhile(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.SkipWhile expects (source, predicate)")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	i := 0
	for ; i < len(elems); i++ {
		v, err := m.linqInvoke(args[1], elems[i], depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		if !v.Truthy() {
			break
		}
	}
	out := make([]runtime.Value, len(elems)-i)
	copy(out, elems[i:])
	return bcl.NewListValue(out), nil
}

func linqTakeWhile(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.TakeWhile expects (source, predicate)")
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
		if !v.Truthy() {
			break
		}
		out = append(out, e)
	}
	return bcl.NewListValue(out), nil
}

func linqReverse(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.Reverse expects a source")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	out := make([]runtime.Value, len(elems))
	for i, v := range elems {
		out[len(elems)-1-i] = v
	}
	return bcl.NewListValue(out), nil
}

// linqToHashSet materializes into a real HashSet<T> (bcl.NewHashSetValue)
// rather than a List<T> like every other LINQ terminal method here —
// callers that go on to use Contains/UnionWith/etc. on the result need
// the real receiver type, not a plain list wearing a HashSet-shaped hat.
func linqToHashSet(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 || len(args) > 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.ToHashSet expects a source[, comparer]")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	var comparerArg *runtime.Value
	if len(args) == 2 {
		comparerArg = &args[1]
	}
	eq, err := m.equalsFunc(comparerArg, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	var out []runtime.Value
	for _, e := range elems {
		dup := false
		for _, o := range out {
			same, err := eq(e, o)
			if err != nil {
				return runtime.Value{}, err
			}
			if same {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, e)
		}
	}
	return bcl.NewHashSetValue(out), nil
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

// linqSingle covers Single(source) and Single(source, predicate) — like
// First but also throws InvalidOperationException on more than one
// match, matching real Enumerable.Single.
func linqSingle(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.Single expects a source")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	var matches []runtime.Value
	if len(args) == 1 {
		matches = elems
	} else {
		for _, e := range elems {
			v, err := m.linqInvoke(args[1], e, depth, instrCount)
			if err != nil {
				return runtime.Value{}, err
			}
			if v.Truthy() {
				matches = append(matches, e)
			}
		}
	}
	switch len(matches) {
	case 0:
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.InvalidOperationException", Message: "Sequence contains no elements"}
	case 1:
		return matches[0], nil
	default:
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.InvalidOperationException", Message: "Sequence contains more than one element"}
	}
}

func linqSingleOrDefault(m *Machine, args []runtime.Value, methodGenericArgs []string, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.SingleOrDefault expects a source")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	var matches []runtime.Value
	if len(args) == 1 {
		matches = elems
	} else {
		for _, e := range elems {
			v, err := m.linqInvoke(args[1], e, depth, instrCount)
			if err != nil {
				return runtime.Value{}, err
			}
			if v.Truthy() {
				matches = append(matches, e)
			}
		}
	}
	switch len(matches) {
	case 0:
		return linqOrDefaultZero(m, methodGenericArgs), nil
	case 1:
		return matches[0], nil
	default:
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.InvalidOperationException", Message: "Sequence contains more than one element"}
	}
}

func linqElementAt(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 || args[1].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.ElementAt expects (source, index)")
	}
	elems, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	idx := int(args[1].I4)
	if idx < 0 || idx >= len(elems) {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "index"}
	}
	return elems[idx], nil
}

func linqSkip(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 || args[1].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.Skip expects (source, count)")
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
	out := make([]runtime.Value, len(elems)-n)
	copy(out, elems[n:])
	return bcl.NewListValue(out), nil
}

// linqUnion concatenates both sources like Concat but deduplicates the
// result — by an optional IEqualityComparer<T> argument, or defaultObjectEqual
// otherwise (comparer.go, same posture as Distinct/Except/Intersect/
// ToHashSet) — preserving first-occurrence order across both sequences.
func linqUnion(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 2 || len(args) > 3 {
		return runtime.Value{}, fmt.Errorf("interpreter: Enumerable.Union expects (first, second[, comparer])")
	}
	a, err := m.enumerateAll(args[0], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	b, err := m.enumerateAll(args[1], depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	var comparerArg *runtime.Value
	if len(args) == 3 {
		comparerArg = &args[2]
	}
	eq, err := m.equalsFunc(comparerArg, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	var out []runtime.Value
	for _, e := range append(a, b...) {
		dup := false
		for _, o := range out {
			same, err := eq(e, o)
			if err != nil {
				return runtime.Value{}, err
			}
			if same {
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
