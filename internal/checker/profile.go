// Package checker analyzes an assembly or NuGet package against a
// compatibility profile (minimal, rules, netstandard-lite) and reports
// which opcodes, BCL calls, generics, reflection or async usages are
// unsupported, with actionable reasons instead of a raw crash. See
// docs/en/ROADMAP.md, Fase 3, module "/checker".
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
		"System.BitConverter::",
		"System.Runtime.CompilerServices.RuntimeHelpers::InitializeArray",
		"System.Runtime.CompilerServices.RuntimeHelpers::IsReferenceOrContainsReferences",
		"System.Runtime.CompilerServices.RuntimeHelpers::EnsureSufficientExecutionStack",
		"System.Activator::CreateInstance",
		"System.Runtime.InteropServices.MemoryMarshal::Read",
		"System.Runtime.InteropServices.MemoryMarshal::Write",
		"System.Xml.XmlQualifiedName::",
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
		"System.Collections.ObjectModel.Collection`1::",
		"System.Collections.Generic.Dictionary`2::",
		"System.Collections.Generic.Dictionary`2+Enumerator::",
		"System.Collections.Generic.Dictionary`2+ValueCollection::",
		"System.Collections.Generic.Dictionary`2+ValueCollection+Enumerator::",
		"System.Collections.Generic.Dictionary`2+KeyCollection::",
		"System.Collections.Generic.Dictionary`2+KeyCollection+Enumerator::",
		"System.Collections.Generic.KeyValuePair`2::",
		"System.Collections.Generic.EqualityComparer`1::",
		"System.Collections.Generic.Comparer`1::",
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
		"System.Collections.Generic.IDictionary`2::Add",
		"System.Collections.Generic.IDictionary`2::Remove",
		"System.Collections.Generic.IDictionary`2::get_Keys",
		"System.Collections.Generic.IList`1::get_Item",
		"System.Collections.Generic.IList`1::set_Item",
		"System.Collections.IList::Add",
		"System.Collections.IList::get_Item",
		"System.Collections.IList::set_Item",
		"System.Collections.IList::Clear",
		"System.Collections.Generic.IReadOnlyList`1::get_Item",
		"System.Collections.Generic.IReadOnlyCollection`1::get_Count",
		"System.Collections.Generic.IEqualityComparer`1::Equals",
		"System.Collections.Generic.IEqualityComparer`1::GetHashCode",
		"System.Char::",
		"System.Int32::",
		"System.Int16::",
		"System.Byte::",
		"System.SByte::",
		"System.UInt16::",
		"System.UInt32::",
		"System.UInt64::",
		"System.Single::",
		"System.Boolean::",
		"System.String::",
		"System.Type::",
		"System.Reflection.MemberInfo::get_Name",
		"System.Reflection.MemberInfo::get_DeclaringType",
		"System.Reflection.MemberInfo::GetCustomAttributes",
		"System.Reflection.MemberInfo::IsDefined",
		"System.Reflection.ConstructorInfo::",
		"System.Reflection.MethodInfo::",
		"System.Reflection.MethodBase::",
		"System.Reflection.FieldInfo::",
		// PropertyInfo (Fase 3.51) and ParameterInfo (Fase 3.52) were
		// simply missing here even though real natives already existed —
		// every real property/parameter reflection call fully resolved
		// but was still reported "out of profile", the exact same gap
		// class as reflectionMachineTargets' own Fase 3.51/3.52 fixes in
		// analyzer.go (a resolvable-but-unlisted target is reported
		// out-of-profile rather than unsupported, see inProfile's own doc
		// comment above — this fixes the "unlisted" half of that).
		"System.Reflection.PropertyInfo::",
		"System.Reflection.ParameterInfo::",
		"System.Linq.Expressions.Expression::",
		"System.Linq.Expressions.Expression`1::",
		"System.Linq.Expressions.LambdaExpression::",
		"System.Linq.Expressions.MemberExpression::",
		"System.Linq.Enumerable::",
		// VmnetInternal.Ordered/VmnetInternal.Grouping are vmnet's own
		// synthetic result types for a LINQ OrderBy/ThenBy chain
		// (bcl.NativeOrdered) and one GroupBy result group
		// (bcl.NativeGrouping) — never a real BCL type name a package's
		// own IL could reference directly, but a real package's IL DOES
		// reach their methods indirectly (`group.Key`, `foreach` over an
		// OrderBy/GroupBy result) via calls.go's virtual-dispatch
		// receiver-type redirection, so the checker needs to recognize
		// them the same way it recognizes every other native-backed type.
		"VmnetInternal.Ordered::",
		"VmnetInternal.Grouping::",
		// A real call site can also be declared directly against the BCL
		// interface names themselves (IGrouping`2/IOrderedEnumerable`1),
		// not just the synthetic VmnetInternal.* names above — same real
		// runtime redirection, just seen from the caller's own declared
		// type instead of the concrete one.
		"System.Linq.IGrouping`2::",
		"System.Linq.IOrderedEnumerable`1::",
		"System.Lazy`1::",
		"System.Text.Encoding::",
		"System.Text.UTF8Encoding::",
		"System.Text.ASCIIEncoding::",
		"System.Text.UnicodeEncoding::",
		"System.Text.StringBuilder::",
		"System.Array::Empty",
		"System.Array::GetEnumerator",
		"System.Array::Resize",
		"System.Array::IndexOf",
		"System.Array::LastIndexOf",
		"System.Array::Copy",
		"System.Array::Clone",
		"System.Array::get_Length",
		"System.Array::GetLength",
		"System.Array::Sort",
		"System.Array::BinarySearch",
		"System.Array::CreateInstance",
		"System.Array::GetValue",
		"System.Array::SetValue",
		"System.Array::Reverse",
		"System.Array::Fill",
		"System.Array::Find",
		"System.Array::FindLast",
		"System.Array::FindIndex",
		"System.Array::FindAll",
		"System.Array::Exists",
		"System.Array::ForEach",
		"System.Array::TrueForAll",
		"System.Array::ConvertAll",
		"System.Array+ArrayEnumerator::",
		"System.Globalization.CultureInfo::",
		"System.TimeZoneInfo::",
		"System.Environment::get_CurrentManagedThreadId",
		"System.Nullable`1::",
		"System.DateTime::",
		"System.Guid::",
		"System.Drawing.Point::",
		"System.Drawing.Color::",
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
		"System.AggregateException",
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
		"System.ApplicationException",
		"System.ObjectDisposedException",
		"System.Data.DataException",
		// System.Data/System.Data.Common (Fase 3.52) — Dapper's own
		// SqlMapper (and any other ADO.NET-based micro-ORM) does its real
		// object-relational mapping directly against these interfaces/
		// abstract classes; see adoNetDispatchTypes' own doc comment
		// (analyzer.go) for why the runtime resolution itself doesn't need
		// a native per member. Listed here too since a resolvable target
		// not in this profile's own allowlist is still reported —
		// out-of-profile rather than unsupported (inProfile's own doc
		// comment above).
		"System.Data.IDbConnection::",
		"System.Data.IDbCommand::",
		"System.Data.IDbTransaction::",
		"System.Data.IDataReader::",
		"System.Data.IDataRecord::",
		"System.Data.IDataParameter::",
		"System.Data.IDbDataParameter::",
		"System.Data.IDataParameterCollection::",
		"System.Data.Common.DbConnection::",
		"System.Data.Common.DbCommand::",
		"System.Data.Common.DbDataReader::",
		"System.Data.Common.DbParameter::",
		"System.Data.Common.DbParameterCollection::",
		"System.Data.Common.DbTransaction::",
		"System.Environment::get_NewLine",
		"System.Environment::GetEnvironmentVariable",
		"System.Double::",
		"System.Threading.Interlocked::",
		"System.WeakReference::",
		"System.WeakReference`1::",
		"System.StringComparer::",
		"System.Convert::",
		"System.Collections.Generic.HashSet`1::",
		"System.Collections.Generic.HashSet`1+Enumerator::",
		"System.Collections.Generic.SortedSet`1::",
		"System.Collections.Generic.SortedSet`1+Enumerator::",
		"System.Collections.Generic.Stack`1::",
		"System.Collections.Generic.Queue`1::",
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
		"System.Nullable::GetUnderlyingType",
		"System.Reflection.Assembly::",
		"System.Reflection.IntrospectionExtensions::",
		"System.Runtime.CompilerServices.Unsafe::",
		"System.Resources.ResourceManager::",
		"System.Enum::GetValues",
		"System.Enum::GetNames",
		"System.Enum::IsDefined",
		"System.Enum::ToObject",
		"System.Enum::HasFlag",
		"System.Enum::GetUnderlyingType",
		"System.IO.MemoryStream::",
		"System.IO.Stream::",
		"System.IO.StringReader::",
		"System.IO.TextReader::",
		"System.IO.IOException",
		"System.IO.EndOfStreamException",
		"System.Xml.XmlWriter::",
		"System.Xml.XmlWriterSettings::",
		"System.Xml.Linq.XDocument::",
		"System.Xml.Linq.XContainer::",
		"System.Xml.Linq.XElement::",
		"System.Xml.Linq.XAttribute::",
		"System.Xml.Linq.XName::",
		"System.Collections.ArrayList::",
		"System.Collections.Hashtable::",
		"System.Collections.SortedList::",
		"System.Collections.Generic.SortedList`2::",
		"System.Collections.Generic.SortedDictionary`2::",
		"System.Collections.Generic.SortedDictionary`2+Enumerator::",
		"System.IO.Compression.ZipArchive::",
		"System.IO.Compression.ZipArchiveEntry::",
		"System.Xml.XmlReader::",
		"System.Xml.XmlReaderSettings::",
		"System.Uri::",
		"System.UriParser::",
		"System.IntPtr::",
		"System.GC::",
		"System.Threading.Volatile::",
		"System.Diagnostics.Debugger::",
		"System.Threading.SpinLock::",
		"System.Diagnostics.Tracing.EventSource::",
		"System.Xml.NameTable::",
		"System.Xml.XmlConvert::",
		"System.Collections.Generic.LinkedList`1::",
		"System.Collections.Generic.LinkedListNode`1::",
		"System.AppContext::",
		"System.Collections.Stack::",
		"System.Random::",
		"System.IO.FileSystemInfo::",
		"System.IO.Path::",
		"System.Threading.Monitor::",
		// Fase 3.59: real, Permissions-gated System.IO.File/Directory/
		// FileStream/FileInfo/DirectoryInfo (internal/bcl/system_io_file.
		// go, internal/interpreter/permissions.go) plus the two new
		// exception types the deny-by-default gate and the new
		// FileNotFoundException-on-a-missing-file path throw.
		"System.IO.File::",
		"System.IO.Directory::",
		"System.IO.FileStream::",
		"System.IO.FileInfo::",
		"System.IO.DirectoryInfo::",
		"System.UnauthorizedAccessException",
		"System.IO.FileNotFoundException",
		"System.IO.DirectoryNotFoundException",
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
		ir.LoadFieldAddr, ir.LoadStaticField, ir.StoreStaticField, ir.LoadStaticFieldAddr,
		ir.InitObj, ir.IsInst, ir.CastClass, ir.LoadFtn, ir.LoadTypeToken, ir.LoadFieldToken,
		ir.Leave, ir.EndFinally, ir.Rethrow:
		return true
	case ir.Call:
		return v.Virtual
	default:
		return false
	}
}
