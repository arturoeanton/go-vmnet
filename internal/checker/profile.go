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
		"System.Collections.Generic.Dictionary`2::",
		"System.Text.Encoding::",
		"System.Text.StringBuilder::",
		"System.Array::Empty",
		"System.Globalization.CultureInfo::",
		"System.Environment::get_CurrentManagedThreadId",
		"System.Nullable`1::",
		"System.Exception",
		"System.InvalidOperationException",
		"System.ArgumentException",
		"System.ArgumentNullException",
		"System.ArgumentOutOfRangeException",
		"System.NotSupportedException",
		"System.NullReferenceException",
		"System.IndexOutOfRangeException",
	)
	// netstandard-lite = rules plus the Fase 3 reflection-lite/Convert surface.
	bclPrefixes[ProfileNetStandardLite] = append(append([]string{}, bclPrefixes[ProfileRules]...),
		"System.Type::",
		"System.Convert::",
	)
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
// ir.InitObj (Fase 3.7) IS included: a value type's own field layout is
// type-system machinery in the same sense classes are, even though its
// instances aren't heap-allocated.
func instrIsObjectModel(instr ir.Instr) bool {
	switch v := instr.(type) {
	case ir.NewObj, ir.LoadField, ir.StoreField, ir.Throw,
		ir.NewArr, ir.LoadLen, ir.LoadElem, ir.StoreElem, ir.LoadElemAddr,
		ir.LoadFieldAddr, ir.LoadStaticField, ir.StoreStaticField,
		ir.InitObj:
		return true
	case ir.Call:
		return v.Virtual
	default:
		return false
	}
}
