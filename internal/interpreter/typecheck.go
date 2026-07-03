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
		return bcl.NativeTypeName(v.Obj.Native)
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
// Anything else (List/Dictionary/StringBuilder) has no interface
// modeling yet — a documented gap (docs/ROADMAP.md Fase 3.8), not a
// silent wrong answer, since it only ever returns false (isinst -> null,
// castclass -> throws), never a false positive.
func (m *Machine) nativeMatches(native any, target string) bool {
	ex, ok := native.(*runtime.ManagedException)
	if !ok {
		return false
	}
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
