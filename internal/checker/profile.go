// Package checker analyzes an assembly or NuGet package against a
// compatibility profile (minimal, rules, netstandard-lite) and reports
// which opcodes, BCL calls, generics, reflection or async usages are
// unsupported, with actionable reasons instead of a raw crash. See
// docs/ROADMAP.md, Fase 3, module "/checker".
package checker

import "github.com/arturoeanton/go-vmnet/internal/ir"

// Profile is a named compatibility surface (spec §24). The checker's
// verdict is always relative to one: what's "compatible" under minimal
// can be "out of profile" under the same runtime, because the runtime
// itself supports more than minimal promises.
type Profile string

const (
	ProfileMinimal         Profile = "minimal"
	ProfileRules           Profile = "rules"
	ProfileNetStandardLite Profile = "netstandard-lite"
)

// bclPrefixes lists which "Namespace.Type::" (or exact "Namespace.Type")
// prefixes count as in-profile for a BCL call/constructor target. A
// target the runtime can actually resolve (bcl.Lookup/LookupCtor or a
// local method) but that isn't listed here is still reported — as
// out-of-profile, not unsupported — because it would run today but isn't
// part of what that profile promises callers.
var bclPrefixes = map[Profile][]string{
	ProfileMinimal: {
		"System.Math::",
		"System.Double::IsNaN",
		"System.String::Concat",
		"System.String::get_Length",
		"System.String::Format",
		"System.String::Substring",
		"System.String::get_Chars",
		"System.String::Equals",
		"System.String::op_Equality",
		"System.String::Join",
		"System.Console::",
		"System.Object::.ctor",
	},
}

func init() {
	// rules = minimal plus the Fase 2 object/collection/exception/text surface.
	bclPrefixes[ProfileRules] = append(append([]string{}, bclPrefixes[ProfileMinimal]...),
		"System.Object::",
		"System.Attribute::",
		"System.Collections.Generic.List`1::",
		"System.Collections.Generic.List`1+Enumerator::",
		"System.Collections.Generic.Dictionary`2::",
		"System.Collections.Generic.Dictionary`2+Enumerator::",
		"System.Collections.Generic.KeyValuePair`2::",
		"System.Collections.Generic.EqualityComparer`1::",
		"System.IDisposable::Dispose",
		"System.Collections.Generic.IEnumerable`1::GetEnumerator",
		"System.Collections.IEnumerable::GetEnumerator",
		"System.Collections.Generic.IEnumerator`1::get_Current",
		"System.Collections.Generic.IEnumerator`1::MoveNext",
		"System.Collections.IEnumerator::get_Current",
		"System.Collections.IEnumerator::MoveNext",
		"System.Collections.IEnumerator::Reset",
		"System.Collections.Generic.ICollection`1::Add",
		"System.Collections.Generic.ICollection`1::get_Count",
		"System.Collections.ICollection::get_Count",
		"System.Collections.Generic.IDictionary`2::set_Item",
		"System.Collections.Generic.IDictionary`2::get_Item",
		"System.Collections.Generic.IDictionary`2::TryGetValue",
		"System.Collections.Generic.IDictionary`2::ContainsKey",
		"System.Collections.Generic.IList`1::get_Item",
		"System.Collections.Generic.IList`1::set_Item",
		"System.Collections.IList::Add",
		"System.Collections.IList::get_Item",
		"System.Collections.IList::set_Item",
		"System.Collections.Generic.IReadOnlyList`1::get_Item",
		"System.Collections.Generic.IReadOnlyCollection`1::get_Count",
		"System.Collections.Generic.IEqualityComparer`1::Equals",
		"System.Collections.Generic.IEqualityComparer`1::GetHashCode",
		"System.Char::",
		"System.Int32::",
		"System.String::",
		"System.Type::",
		"System.Reflection.MemberInfo::get_Name",
		"System.Linq.Enumerable::",
		"System.Lazy`1::",
		"System.Text.Encoding::",
		"System.Text.StringBuilder::",
		"System.Array::Empty",
		"System.Array::GetEnumerator",
		"System.Array+ArrayEnumerator::",
		"System.Globalization.CultureInfo::",
		"System.Environment::get_CurrentManagedThreadId",
		"System.Nullable`1::",
		"System.DateTime::",
		"System.Span`1::",
		"System.ReadOnlySpan`1::",
		"System.Memory`1::",
		"System.ReadOnlyMemory`1::",
		"System.MemoryExtensions::",
		"System.Action",
		"System.Func`",
		"System.Predicate`1::",
		"System.Comparison`1::",
		"System.EventHandler",
		"System.Exception",
		"System.InvalidOperationException",
		"System.ArgumentException",
		"System.ArgumentNullException",
		"System.ArgumentOutOfRangeException",
		"System.NotSupportedException",
		"System.NullReferenceException",
		"System.IndexOutOfRangeException",
		"System.InvalidCastException",
		"System.FormatException",
		"System.OverflowException",
		"System.NotImplementedException",
		"System.Environment::get_NewLine",
		"System.Double::",
		"System.Threading.Interlocked::CompareExchange",
		"System.StringComparer::",
		"System.Convert::",
		"System.Collections.Generic.HashSet`1::",
		"System.Collections.Generic.HashSet`1+Enumerator::",
		"System.Collections.Generic.Stack`1::",
		"System.TimeSpan::",
		"System.Text.RegularExpressions.Regex::",
		"System.Text.RegularExpressions.Match::",
		"System.Text.RegularExpressions.GroupCollection::",
		"System.Text.RegularExpressions.Group::",
		"System.Text.RegularExpressions.Capture::",
		"System.Threading.Tasks.Task::",
		"System.Threading.Tasks.Task`1::",
		"System.Runtime.CompilerServices.AsyncTaskMethodBuilder::",
		"System.Runtime.CompilerServices.AsyncTaskMethodBuilder`1::",
		"System.Runtime.CompilerServices.TaskAwaiter::",
		"System.Runtime.CompilerServices.TaskAwaiter`1::",
		"System.Runtime.CompilerServices.ConfiguredTaskAwaitable::",
		"System.Runtime.CompilerServices.ConfiguredTaskAwaitable`1::",
		"System.Runtime.CompilerServices.ConfiguredTaskAwaitable+ConfiguredTaskAwaiter::",
		"System.Runtime.CompilerServices.ConfiguredTaskAwaitable`1+ConfiguredTaskAwaiter::",
		"System.DateTimeOffset::",
		"System.ValueTuple`2::",
		"System.Int64::",
		"System.Collections.Concurrent.ConcurrentDictionary`2::",
		"System.Delegate::",
	)
	// netstandard-lite currently promises exactly the same BCL surface as
	// rules (System.Type moved into `rules` in Fase 3.14, System.Convert
	// in Fase 3.18, once each had real natives behind it) — kept as its
	// own profile/slice rather than collapsed into one, so a future
	// rules-only addition doesn't have to be reconsidered for both tiers
	// by construction.
	bclPrefixes[ProfileNetStandardLite] = append([]string{}, bclPrefixes[ProfileRules]...)
}

// objectOpcodesAllowed reports whether profile permits object-model IR
// (newobj/callvirt/fields/throw) at all — spec §24.1: `minimal` is
// static-methods-and-primitives only, regardless of what the runtime can
// technically execute.
func objectOpcodesAllowed(p Profile) bool {
	return p != ProfileMinimal
}

// inProfile reports whether a resolved call/ctor target's full name is
// part of profile p's promised surface.
func inProfile(p Profile, fullName string) bool {
	for _, prefix := range bclPrefixes[p] {
		if len(fullName) >= len(prefix) && fullName[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

// instrIsObjectModel reports whether instr requires the object model
// (excluded from ProfileMinimal — spec §24.1: `minimal` is
// static-methods-and-primitives only). Besides classes/fields/callvirt/
// throw, that also rules out arrays (heap-allocated System.Array
// instances, not primitives) and static fields (shared mutable state, not
// a "static methods and primitives" promise) added in Fase 3.5.
// LoadArgAddr/LoadLocalAddr/LoadIndirect/StoreIndirect are deliberately
// NOT included: a `ref`/`out` primitive parameter never touches the heap
// or a type's field layout, so it stays within minimal's promised surface.
// ir.InitObj (Fase 3.7) and ir.IsInst/ir.CastClass (Fase 3.8) ARE
// included: a value type's own field layout, and the class/interface
// hierarchy walk isinst/castclass need, are type-system machinery in the
// same sense classes are, even when nothing gets heap-allocated.
// ir.LoadFtn (Fase 3.9) is included too: a delegate is a heap-allocated
// closure once ir.NewObj (already excluded) constructs it, and ldftn only
// exists to feed that construction.
// ir.Leave/ir.EndFinally/ir.Rethrow (Fase 3.10) are included: a plain
// `try { } finally { }` with no throw or catch anywhere still compiles to
// leave/endfinally, so excluding only ir.Throw would miss it.
// ir.LoadTypeToken (Fase 3.14) is included: `typeof(T)` pushes a real
// System.Type object (a heap-allocated instance, same as ir.NewObj).
func instrIsObjectModel(instr ir.Instr) bool {
	switch v := instr.(type) {
	case ir.NewObj, ir.LoadField, ir.StoreField, ir.Throw,
		ir.NewArr, ir.LoadLen, ir.LoadElem, ir.StoreElem, ir.LoadElemAddr,
		ir.LoadFieldAddr, ir.LoadStaticField, ir.StoreStaticField,
		ir.InitObj, ir.IsInst, ir.CastClass, ir.LoadFtn, ir.LoadTypeToken,
		ir.Leave, ir.EndFinally, ir.Rethrow:
		return true
	case ir.Call:
		return v.Virtual
	default:
		return false
	}
}
