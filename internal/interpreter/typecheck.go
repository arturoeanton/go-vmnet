package interpreter

import (
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// isAssignableTo implements isinst/castclass's real type check (Fase
// 3.8): does v's runtime type equal, or derive from (class inheritance),
// or implement (interfaces, transitively through interface-extends-
// interface too) target? Callers handle KindNull separately — casting
// null always "succeeds" regardless of target, per spec, without even
// reaching this function.
func (m *Machine) isAssignableTo(v runtime.Value, target string) bool {
	if target == "" || target == "System.Object" {
		// "" is an unresolved generic type parameter (see ir.IsInst's doc
		// comment) — nothing can be confirmed to match an unknown type.
		// System.Object matches everything reference/value-shaped that
		// reaches here (KindNull is handled by the caller before this).
		return target == "System.Object"
	}
	switch v.Kind {
	case runtime.KindI4:
		switch target {
		case "System.Int32", "System.Boolean", "System.Char", "System.Byte",
			"System.SByte", "System.Int16", "System.UInt16", "System.UInt32":
			// vmnet's Value doesn't distinguish these at the Kind level
			// (all int32 on the CIL stack) — matching any of them is more
			// useful than guessing wrong and rejecting a legitimate `is`/
			// `as` check on an int-shaped primitive.
			return true
		}
		return false
	case runtime.KindI8:
		return target == "System.Int64" || target == "System.UInt64"
	case runtime.KindR4:
		return target == "System.Single"
	case runtime.KindR8:
		return target == "System.Double"
	case runtime.KindString:
		return target == "System.String"
	case runtime.KindArray, runtime.KindBytes:
		return target == "System.Array"
	case runtime.KindFunc:
		// A delegate value (runtime.Func) doesn't carry its own declared
		// delegate type (Action vs. Func`2 vs. a custom `delegate`
		// declaration) — see Func's doc comment, Fase 3.9: it's detected
		// structurally, not per-type. `(Action)Delegate.Combine(a, b)`
		// (Fase 3.24) is the first real castclass against a delegate value
		// this project has hit; with no declared type to check against,
		// any delegate-typed cast/isinst on a real delegate value succeeds
		// — matching the fact that vmnet never rejects a delegate
		// invocation for a type mismatch either.
		return true
	case runtime.KindStruct:
		if v.Struct == nil {
			return false
		}
		if target == "System.ValueType" {
			return true
		}
		return m.typeMatches(v.Struct.Type, target)
	case runtime.KindObject:
		if v.Obj == nil {
			return false
		}
		if v.Obj.Type != nil {
			return m.typeMatches(v.Obj.Type, target)
		}
		return m.nativeMatches(v.Obj.Native, target)
	default:
		return false
	}
}

// typeMatches walks t's class hierarchy (BaseTypeFullName) and, at every
// level, its directly-implemented interfaces — recursing into each
// interface's own Interfaces too, since one interface can extend another.
// A base/interface name vmnet can't resolve (a foreign BCL type, no
// TypeDef in the loaded assembly) ends that branch of the walk rather
// than failing the whole check.
func (m *Machine) typeMatches(t *runtime.Type, target string) bool {
	for t != nil {
		if fullTypeName(t) == target {
			return true
		}
		for _, iface := range t.Interfaces {
			if iface == target {
				return true
			}
			if m.ResolveType == nil {
				continue
			}
			if ifaceType, err := m.ResolveType(iface); err == nil && m.typeMatches(ifaceType, target) {
				return true
			}
		}
		if t.BaseTypeFullName == "" || m.ResolveType == nil {
			return false
		}
		base, err := m.ResolveType(t.BaseTypeFullName)
		if err != nil {
			return false
		}
		t = base
	}
	return false
}

func fullTypeName(t *runtime.Type) string {
	if t.QualifiedName != "" {
		return t.QualifiedName
	}
	if t.Namespace == "" {
		return t.Name
	}
	return t.Namespace + "." + t.Name
}

// receiverTypeName returns v's concrete BCL/plugin type full name, if
// determinable — the single piece of type identity the interface-call
// fallback (calls.go, Fase 3.13) needs. A struct receiver (foreach's own
// enumerator structs from Fase 3.7/3.11, possibly still boxed as a
// managed pointer from `ldloca`) and a plugin-class object both already
// carry a real *runtime.Type; a native-backed BCL object (List<T>,
// Dictionary<K,V>, ...) has none, so bcl.NativeTypeName supplies the
// same hand-maintained name its own register() calls use.
func receiverTypeName(v runtime.Value) (string, bool) {
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	switch v.Kind {
	case runtime.KindStruct:
		if v.Struct == nil || v.Struct.Type == nil {
			return "", false
		}
		return fullTypeName(v.Struct.Type), true
	case runtime.KindObject:
		if v.Obj == nil {
			return "", false
		}
		if v.Obj.Type != nil {
			return fullTypeName(v.Obj.Type), true
		}
		// interpreterNativeTypeName (elementfactory.go) covers native-
		// backed types that live in THIS package rather than bcl —
		// nativeKnownChildBuilder/nativeElementFactoryThunk (Fase 3.41)
		// need Machine access (m.newObj/m.call) their real dispatch
		// can't get from bcl's own Native map, so they're defined here
		// instead of alongside bcl's other native-backed BCL types.
		if name, ok := interpreterNativeTypeName(v.Obj.Native); ok {
			return name, true
		}
		return bcl.NativeTypeName(v.Obj.Native)
	case runtime.KindArray:
		// A real CIL SZArray (`T[]`) implicitly implements IEnumerable`1/
		// ICollection`1/IList`1 per the CLR's own array-covariance rules
		// (ECMA-335 §II.9.9) — there's no TypeDef or bcl.NativeTypeName
		// entry for "the receiver's own array type", but "System.Array"
		// itself already has a real GetEnumerator registered (Fase 3.5),
		// so reporting that here lets the virtual-dispatch chain walk
		// retry it. Found via a real case: ClosedXML's own XLWorkbook
		// constructor does `foreach` over a plain `T[]` through an
		// `IEnumerable<T>`-declared call site, reached just from opening
		// a real .xlsx workbook.
		return "System.Array", true
	case runtime.KindI4:
		// A boxed/constrained primitive receiver (Fase 3.40, found via a
		// real, load-bearing case: some generic collection's own internal
		// comparer calling `x.Equals(y)` through a `constrained. !!0
		// callvirt IEquatable\`1::Equals` on an int/enum/bool-typed T,
		// none of which have a real TypeDef the chain walk could resolve
		// anyway — reported as "System.Int32" purely so the walk has a
		// name to try before falling through to the System.Object::
		// Equals/GetHashCode/ToString last resort, which is what
		// actually answers it).
		return "System.Int32", true
	case runtime.KindI8:
		return "System.Int64", true
	case runtime.KindR4:
		return "System.Single", true
	case runtime.KindR8:
		return "System.Double", true
	case runtime.KindString:
		return "System.String", true
	default:
		return "", false
	}
}

// splitCallName splits a "Namespace.Type::Method" full name into its
// class and method parts, so the interface-call fallback can rebuild the
// same call against the receiver's concrete type name instead of the
// declared interface it was compiled against, and — if that plain name
// doesn't resolve either — ask ExplicitImplResolver about the original
// declared (class, method) pair.
func splitCallName(fullName string) (class, method string, ok bool) {
	idx := strings.LastIndex(fullName, "::")
	if idx < 0 {
		return "", "", false
	}
	return fullName[:idx], fullName[idx+2:], true
}

// exceptionBaseType is a small, hand-maintained slice of the real .NET
// exception hierarchy, covering exactly the exception types vmnet
// registers native constructors for (internal/bcl/system_exception.go) —
// enough for `ex is ArgumentException`-style checks on vmnet's own
// exceptions to give the right answer, not a general BCL type database.
var exceptionBaseType = map[string]string{
	"System.ArgumentNullException":       "System.ArgumentException",
	"System.ArgumentOutOfRangeException": "System.ArgumentException",
	"System.ArgumentException":           "System.SystemException",
	"System.InvalidOperationException":   "System.SystemException",
	"System.NotSupportedException":       "System.SystemException",
	"System.NullReferenceException":      "System.SystemException",
	"System.IndexOutOfRangeException":    "System.SystemException",
	"System.FormatException":             "System.SystemException",
	"System.OverflowException":           "System.ArithmeticException",
	"System.ArithmeticException":         "System.SystemException",
	"System.NotImplementedException":     "System.SystemException",
	"System.OperationCanceledException":  "System.SystemException",
	"System.SystemException":             "System.Exception",
}

// nativeMatches handles isinst/castclass against a native-backed
// KindObject (Obj.Type == nil): a *runtime.ManagedException walks up its
// type chain one step at a time, alternating between two sources as
// needed — the hand-maintained BCL exception hierarchy above, and (Fase
// 3.13) a plugin's own exception subclass's real TypeDef.BaseTypeFullName
// once base-constructor chaining lets one exist at all (see
// baseExceptionCtorInPlace, internal/bcl/system_exception.go, which sets
// TypeName to the real most-derived plugin type name, e.g.
// "Vmnet.Fixtures.MyException", not the fixed BCL base name it's
// registered under) — so `catch (MyException e)` matches immediately on
// the exact name, and `catch (Exception e)` still matches too, by
// resolving MyException's own TypeDef and following its base chain back
// into the hand-maintained map once it reaches a real BCL name.
//
// Any other native-backed type (Fase 3.41) falls back to
// bcl.NativeTypeName + bcl.NativeBaseTypeName's own hand-maintained chain
// (the same one assembly.go's valueIsAssignableToTypeName already uses
// for overload scoring) — found via a real, load-bearing case:
// DocumentFormat.OpenXml.Framework's own AddAttribute<T> does
// `expression.Body is MemberExpression` (a real `isinst`) against
// vmnet's own natively-constructed Expression-tree stand-ins
// (system_linq_expressions.go). Anything with no NativeTypeName entry at
// all still correctly returns false (isinst -> null, castclass ->
// throws), never a false positive — a documented gap (docs/en/
// ROADMAP.md Fase 3.8), not a silent wrong answer.
func (m *Machine) nativeMatches(native any, target string) bool {
	if ex, ok := native.(*runtime.ManagedException); ok {
		name := ex.TypeName
		for name != "" {
			if name == target {
				return true
			}
			if next, ok := exceptionBaseType[name]; ok {
				name = next
				continue
			}
			if m.ResolveType == nil {
				return false
			}
			t, err := m.ResolveType(name)
			if err != nil {
				return false
			}
			name = t.BaseTypeFullName
		}
		return false
	}
	name, ok := bcl.NativeTypeName(native)
	for ok {
		if name == target {
			return true
		}
		name, ok = bcl.NativeBaseTypeName(name)
	}
	return false
}
