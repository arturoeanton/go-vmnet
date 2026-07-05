package bcl

import (
	"fmt"
	"hash/fnv"
	"math"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

func init() {
	// Every class the C# compiler emits chains up to Object::.ctor(), even
	// when the source has no explicit base-class call.
	register("System.Object::.ctor", false, objectCtorNoop)
	register("System.Object::ToString", true, objectToString)
	register("System.Object::Equals", true, objectEquals)
	register("System.Object::GetHashCode", true, objectGetHashCode)
	// Attribute::.ctor: modern C# compilers emit attribute classes of
	// their own (e.g. EmbeddedAttribute, RefSafetyRulesAttribute) into
	// every assembly for certain language features, regardless of
	// whether the source uses them — their .ctor chains here.
	register("System.Attribute::.ctor", false, objectCtorNoop)
	// `new object()` (`newobj System.Object::.ctor()`) — a real, common
	// pattern (a private lock object: `private readonly object _lock =
	// new object();`) distinct from the base-call case above: this is
	// newObj's NativeCtor path (allocates a fresh object), not a plain
	// call on an already-allocated receiver. Found via a real case:
	// NPOI's own I/O wrapper classes declare exactly this kind of lock
	// field.
	registerCtor("System.Object", newObjectCtor)
}

func newObjectCtor(args []runtime.Value) (*runtime.Object, error) {
	return &runtime.Object{}, nil
}

func objectCtorNoop(args []runtime.Value) (runtime.Value, error) {
	return runtime.Value{}, nil
}

// objectToString backs a boxed value's virtual ToString() call: since
// box is a no-op in vmnet's Value model (see internal/ir/builder.go), the
// callvirt still carries the real Kind, so this can dispatch on it exactly
// like the CLR would dispatch on the boxed value's runtime type.
//
// It's also, today, the ONLY place a ToString() override on a native BCL
// type (StringBuilder, ...) gets a chance to run at all: vmnet resolves
// callvirt targets statically from the MemberRef's declared class, not by
// real virtual dispatch on the receiver's runtime type (no vtable yet —
// Fase 3.8), and the C# compiler is free to emit `.ToString()` call sites
// against the base System.Object::ToString MemberRef relying on the CLR's
// virtual dispatch to reach the override — which is exactly what happens
// for `StringBuilder.ToString()`. See nativeToString below.
func objectToString(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 {
		return runtime.String("null"), nil
	}
	return runtime.String(displayString(args[0])), nil
}

func displayString(v runtime.Value) string {
	// A value type's `this` (e.g. `item.ToString()` inside a generic
	// method over T, compiled as `constrained. !!0` + `callvirt
	// Object::ToString`) always arrives as a managed pointer, never the
	// struct value directly — same reasoning as derefReceiver below. This
	// is also why ReadOnlySpan<char>.ToString() needs the same treatment
	// as StringBuilder below: `constrained.`+`callvirt Object::ToString`
	// again, confirmed against real IL (Fase 3.12).
	v = derefReceiver(v)
	switch v.Kind {
	case runtime.KindObject:
		if v.Obj != nil {
			if s, ok := nativeToString(v.Obj.Native); ok {
				return s
			}
		}
	case runtime.KindStruct:
		if v.Struct != nil {
			if s, ok := spanToStringValue(v.Struct); ok {
				return s
			}
		}
	}
	return v.String()
}

// nativeToString special-cases the native-backed BCL types whose ToString()
// override needs to run even when the call site resolved to the base
// System.Object::ToString (see objectToString's doc comment). Types with
// no meaningful ToString (List/Dictionary, which use the CLR's unhelpful
// default of the type name — not useful to reproduce here) fall through
// to false, matching the pre-existing behavior for anything not listed.
func nativeToString(native any) (string, bool) {
	switch n := native.(type) {
	case *nativeStringBuilder:
		return n.buf, true
	// StringWriter overrides ToString() (returns its accumulated buffer)
	// same as StringBuilder above — a call site declared against the base
	// TextWriter (which does NOT override ToString itself) compiles its
	// `.ToString()` straight to the inherited System.Object::ToString
	// MemberRef, same reasoning as StringBuilder's own case here.
	case *nativeStringWriter:
		return n.buf.String(), true
	default:
		return "", false
	}
}

// NativeTypeName returns the BCL full type name of a native-backed
// Object (List<T>, Dictionary<K,V>, StringBuilder, ...) — vmnet gives
// these no *runtime.Type (they're backed by a plain Go struct in Native,
// not fields), so unlike a plugin object or a synthetic value type there
// is normally nothing to ask "what is your real type" at runtime. This
// exists for exactly one caller: the interpreter's interface-call
// fallback (Fase 3.13), which redirects a call site declared against an
// interface (e.g. IEnumerable`1::GetEnumerator) to the receiver's actual
// concrete type when the interface name itself has no native registered
// — the names returned here must match the strings register() calls use
// in system_collections.go/system_stringbuilder.go exactly.
func NativeTypeName(native any) (string, bool) {
	switch n := native.(type) {
	case *nativeList:
		// typeName distinguishes List`1 from the legacy ArrayList, both
		// backed by this same struct (Fase 3.39) — see nativeList's own
		// doc comment for the real bug an unconditional "always List`1"
		// answer caused.
		if n.typeName != "" {
			return n.typeName, true
		}
		return "System.Collections.Generic.List`1", true
	case *nativeDict:
		if n.typeName != "" {
			return n.typeName, true
		}
		return "System.Collections.Generic.Dictionary`2", true
	case *nativeStringBuilder:
		return "System.Text.StringBuilder", true
	case *nativeArrayEnumerator:
		return "System.Array+ArrayEnumerator", true
	case *nativeMemoryStream:
		return "System.IO.MemoryStream", true
	case *nativeConstructorInfo:
		return "System.Reflection.ConstructorInfo", true
	case *nativeMethodInfo:
		return "System.Reflection.MethodInfo", true
	case *nativeFieldInfo:
		return "System.Reflection.FieldInfo", true
	case *nativePropertyInfo:
		return "System.Reflection.PropertyInfo", true
	case *nativeSortedList:
		if n.typeName != "" {
			return n.typeName, true
		}
		return "System.Collections.SortedList", true
	case *nativeResourceManager:
		return "System.Resources.ResourceManager", true
	case *nativeStringComparer:
		// Missing here meant calls.go's virtual-dispatch ancestor walk
		// couldn't identify a StringComparer.Ordinal/OrdinalIgnoreCase
		// instance's concrete type at all (receiverTypeName's own "ok"
		// stayed false), so it never even tried redirecting a call site
		// declared against IEqualityComparer<string>/IComparer<string>
		// (e.g. Dapper's own SqlMapper.connectionStringComparer field,
		// typed IEqualityComparer<string>) back to the already-
		// registered "System.StringComparer::GetHashCode"/Equals/Compare
		// natives above — it fell through to the bare, unresolvable
		// interface name instead (Fase 3.52, found via a real, load-
		// bearing case: Dapper's SqlMapper static ctor assigns
		// StringComparer.Ordinal to exactly such a field).
		return "System.StringComparer", true
	case *nativeWeakReference:
		return "System.WeakReference", true
	case *nativeZipArchive:
		return "System.IO.Compression.ZipArchive", true
	case *nativeZipArchiveEntry:
		return "System.IO.Compression.ZipArchiveEntry", true
	case *nativeXmlReader:
		return "System.Xml.XmlReader", true
	case *nativeUri:
		return "System.Uri", true
	case *nativeNameTable:
		return "System.Xml.NameTable", true
	case *nativeEventSource:
		return "System.Diagnostics.Tracing.EventSource", true
	case *nativeLinkedList:
		return "System.Collections.Generic.LinkedList`1", true
	case *linkedListNode:
		return "System.Collections.Generic.LinkedListNode`1", true
	case *nativeStack:
		if n.typeName != "" {
			return n.typeName, true
		}
		return "System.Collections.Generic.Stack`1", true
	case *nativeParameterExpression:
		return "System.Linq.Expressions.ParameterExpression", true
	case *nativeMemberExpression:
		return "System.Linq.Expressions.MemberExpression", true
	case *nativeLambdaExpression:
		return "System.Linq.Expressions.Expression`1", true
	case *nativeMemberInfo:
		return "System.Reflection.MemberInfo", true
	case *nativeHashSet:
		// Missing here meant receiverTypeName (internal/interpreter/
		// typecheck.go) couldn't identify a HashSet<T> receiver at all,
		// so calls.go's virtual-dispatch ancestor walk never got to retry
		// System.Collections.Generic.HashSet`1::GetEnumerator (already
		// registered just above, system_hashset.go) for a call site
		// declared against IEnumerable<T> instead of HashSet<T> directly
		// (Fase 3.41, found via a real, load-bearing case: ClosedXML
		// 0.105.0 opening a real .xlsx iterates a HashSet<T> field
		// through exactly such an IEnumerable<T>-declared foreach). Also
		// backs SortedSet<T> (n.typeName distinguishes them exactly like
		// nativeList's own typeName field distinguishes List`1/ArrayList,
		// Fase 3.44) — missing that case meant `sortedSet.Select(...)`
		// failed outright with "IEnumerable`1::GetEnumerator... not found".
		if n.typeName != "" {
			return n.typeName, true
		}
		return "System.Collections.Generic.HashSet`1", true
	case *nativeStringReader:
		return "System.IO.StringReader", true
	case *nativeQueue:
		// Missing entirely until probed against a hand-written
		// `queue.Select(...)`/`foreach` fixture: without this case,
		// receiverTypeName can't redirect a Queue<T> reached through
		// IEnumerable`1 (LINQ's own enumerateAll fallback, or any
		// interface-typed foreach) back to "Queue`1::GetEnumerator" — the
		// same gap nativeStack's own case above already covers for
		// Stack<T> (Fase 3.44).
		return "System.Collections.Generic.Queue`1", true
	case *NativeOrdered:
		// A LINQ OrderBy/ThenBy chain result (system_linq_native.go,
		// Fase 3.44) — reached through further LINQ chaining or a direct
		// foreach via IEnumerable`1/IOrderedEnumerable`1.
		return "VmnetInternal.Ordered", true
	case *NativeGrouping:
		// One GroupBy result group (system_linq_native.go, Fase 3.44) —
		// reached through `group.Key`/`foreach (var x in group)`, both
		// interface-declared call sites needing the same redirection.
		return "VmnetInternal.Grouping", true
	case *nativeDBNull:
		return "System.DBNull", true
	case *nativeSqliteConnection:
		// Real, Go-native ADO.NET provider (Fase 3.53, system_data_sqlite.go)
		// — needed for the exact same reason nativeStringComparer's own case
		// above is: a Dapper SqlMapper.Query/Execute call site is declared
		// against IDbConnection, not SqliteConnection directly, so the
		// virtual-dispatch ancestor walk (calls.go) needs this receiver's
		// concrete type name to redirect back to this package's own
		// registered "Microsoft.Data.Sqlite.SqliteConnection::..." natives.
		return "Microsoft.Data.Sqlite.SqliteConnection", true
	case *nativeSqliteCommand:
		return "Microsoft.Data.Sqlite.SqliteCommand", true
	case *nativeSqliteDataReader:
		return "Microsoft.Data.Sqlite.SqliteDataReader", true
	case *nativeSqliteParameter:
		return "Microsoft.Data.Sqlite.SqliteParameter", true
	case *nativeSqliteParameterCollection:
		return "Microsoft.Data.Sqlite.SqliteParameterCollection", true
	case *nativeSqliteTransaction:
		return "Microsoft.Data.Sqlite.SqliteTransaction", true
	case *nativeMatchVal:
		// Regex.Matches's real return type, MatchCollection, declares
		// GetEnumerator() -> IEnumerator (the non-generic interface,
		// Current: object — confirmed against real .NET metadata before
		// assuming it), so `foreach (Match m in regex.Matches(s))`
		// compiles a `castclass System.Text.RegularExpressions.Match`
		// against every element Current yields, unlike the singular
		// Match(string) call (already the declared static type, no cast
		// at all). Missing this case made that cast throw
		// InvalidCastException unconditionally — found via this exact
		// hardening pass's own probe fixture, not assumed (Fase 3.53).
		return "System.Text.RegularExpressions.Match", true
	case *runtime.ManagedException:
		// A bare exception object (no TypeDef — either a plain BCL
		// exception type like ArgumentException, or ex.Object unset
		// entirely, see ManagedException.Object's own doc comment) still
		// needs a concrete type name for receiverTypeName's virtual-
		// dispatch ancestor walk (internal/interpreter/calls.go) to find
		// at all: without this, `e.GetType()`/`.ToString()`/`.Equals()`
		// called on any such exception skipped that walk outright (ok
		// was false), never reaching its own System.Object fallback —
		// found via a real, common pattern (`catch (Exception e) {
		// ...e.GetType()... }`, and just as much for a plugin exception
		// subclass caught as its base System.Exception, which has no
		// TypeDef of its own to carry BaseTypeFullName from). Already
		// precedented at the one other call site that needed this exact
		// answer (assembly.go's own qualifiedTypeNameOf, Fase 3.40).
		return n.TypeName, true
	default:
		return "", false
	}
}

// nativeBaseTypeNames maps a native-backed type's own name (NativeTypeName)
// to its immediate real BCL base type, if any — a native type has no
// TypeDef/BaseTypeFullName chain to walk (Fase 3.39), so overload
// resolution's assignability check (assembly.go's
// valueIsAssignableToTypeName) has no other way to learn e.g. "a
// MemoryStream IS-A Stream". Found via a real bug: NPOI's own
// POIFSFileSystem declares same-arity constructor overloads over
// unrelated reference types (FileInfo/FileStream/Stream); without this,
// a MemoryStream argument scored an exact tie against all of them and
// silently ran the wrong (file-based) one, picked by declaration order.
var nativeBaseTypeNames = map[string]string{
	"System.IO.MemoryStream":                      "System.IO.Stream",
	"System.Linq.Expressions.MemberExpression":    "System.Linq.Expressions.Expression",
	"System.Linq.Expressions.ParameterExpression": "System.Linq.Expressions.Expression",
	"System.Linq.Expressions.Expression`1":        "System.Linq.Expressions.LambdaExpression",
	"System.IO.StringReader":                      "System.IO.TextReader",
}

// NativeBaseTypeName returns typeName's immediate base type per
// nativeBaseTypeNames, if any — chain it (repeatedly looking up the
// result) to walk further than one level.
func NativeBaseTypeName(typeName string) (string, bool) {
	base, ok := nativeBaseTypeNames[typeName]
	return base, ok
}

// objectEquals/objectGetHashCode back Object::Equals/GetHashCode — the
// other pair of virtual methods (besides ToString) the `constrained.`
// prefix commonly precedes on a generic type parameter or value type
// (EqualityComparer<T>-style comparison code). A struct instance-method
// receiver arrives as a managed pointer (see fieldSlot in
// internal/interpreter/eval.go), hence the deref.
func objectEquals(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Object.Equals expects 2 arguments")
	}
	return runtime.Bool(valuesEqual(derefReceiver(args[0]), derefReceiver(args[1]))), nil
}

func objectGetHashCode(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("bcl: System.Object.GetHashCode expects a receiver")
	}
	return runtime.Int32(valueHash(derefReceiver(args[0]))), nil
}

func derefReceiver(v runtime.Value) runtime.Value {
	if v.Kind == runtime.KindRef && v.Ref != nil {
		return *v.Ref
	}
	return v
}

// ValuesEqual exports valuesEqual (Fase 3.50) for
// internal/interpreter/collection_objectmodel.go's collectionRemove,
// which needs the exact same "find this item's index" equality List<T>.
// Remove/ArrayList.Remove already use (listRemove, below) — real
// Collection<T>.Remove(T item) is spec'd as `int index = IndexOf(item);
// if index<0 return false; RemoveItem(index); return true;`, the same
// notion of equality as every other Remove overload in this package.
func ValuesEqual(a, b runtime.Value) bool {
	return valuesEqual(a, b)
}

// valuesEqual implements Object.Equals' default value-equality semantics:
// same bits for primitives, same content for strings, field-wise
// (recursive) equality for structs, reference identity for
// classes/arrays — matching how the CLR's default Equals behaves absent a
// type-specific override, which is the common case for generic comparison
// code (EqualityComparer<T>.Default and friends, Fase 3.8).
func valuesEqual(a, b runtime.Value) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case runtime.KindNull:
		return true
	case runtime.KindI4:
		return a.I4 == b.I4
	case runtime.KindI8:
		return a.I8 == b.I8
	case runtime.KindR4:
		return a.R4 == b.R4
	case runtime.KindR8:
		return a.R8 == b.R8
	case runtime.KindString:
		return a.Str == b.Str
	case runtime.KindObject:
		return a.Obj == b.Obj
	case runtime.KindArray:
		return a.Arr == b.Arr
	case runtime.KindStruct:
		if a.Struct == nil || b.Struct == nil {
			return a.Struct == b.Struct
		}
		if a.Struct.Type != b.Struct.Type || len(a.Struct.Fields) != len(b.Struct.Fields) {
			return false
		}
		for i := range a.Struct.Fields {
			if !valuesEqual(a.Struct.Fields[i], b.Struct.Fields[i]) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func valueHash(v runtime.Value) int32 {
	switch v.Kind {
	case runtime.KindNull:
		return 0
	case runtime.KindI4:
		return v.I4
	case runtime.KindI8:
		return int32(v.I8 ^ (v.I8 >> 32))
	case runtime.KindR4:
		return int32(math.Float32bits(v.R4))
	case runtime.KindR8:
		bits := math.Float64bits(v.R8)
		return int32(bits ^ (bits >> 32))
	case runtime.KindString:
		h := fnv.New32a()
		h.Write([]byte(v.Str))
		return int32(h.Sum32())
	case runtime.KindObject:
		return hashPointer(v.Obj)
	case runtime.KindArray:
		return hashPointer(v.Arr)
	case runtime.KindStruct:
		if v.Struct == nil {
			return 0
		}
		h := int32(17)
		for _, f := range v.Struct.Fields {
			h = h*31 + valueHash(f)
		}
		return h
	default:
		return 0
	}
}

// hashPointer gives a stable, if not identity-strong, hash for a
// reference-typed Value backed by a Go pointer, without resorting to the
// unsafe package (out of step with vmnet's pure-Go, no-tricks philosophy —
// see docs/en/adr/0001-pure-go-core.md) to read the pointer bits directly.
func hashPointer(p any) int32 {
	h := fnv.New32a()
	fmt.Fprintf(h, "%p", p)
	return int32(h.Sum32())
}
