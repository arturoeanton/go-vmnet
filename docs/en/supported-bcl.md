# Supported BCL surface

vmnet is not a full .NET implementation. vmnet executes a supported subset of CIL and selected
BCL APIs (spec §33.3). This document describes that subset as it exists in the code today —
`internal/bcl` (plain Go natives) plus the Machine-aware natives in `internal/interpreter`
(`linq.go`, `async.go`, `reflection.go`, `activator.go`, and friends). It is a snapshot, not a
contract: the surface grows every Fase (see `docs/en/ROADMAP.md`), and a method that's missing
today may exist by the time you read this. **`vmnet check` against your actual assembly is the
only authoritative answer to "will my code work"** — this document tells you what's there in
general; the checker tells you what's there for *your* IL, method by method, and where the gaps
are. `docs/en/compatibility-profile.md` covers the three profiles (`minimal`/`rules`/
`netstandard-lite`) that gate this surface at the checker level; this document is about what the
*runtime* can execute at all, independent of profile.

```bash
go build -o vmnet ./cmd/vmnet
./vmnet check --profile=netstandard-lite <your.dll>
./vmnet check package --profile=netstandard-lite <PackageId>@<Version>
```

## How to read this document

Each section below lists the real breadth of what's implemented for that namespace/area, with a
few named methods as concrete anchors — not an exhaustive method-by-method index (that would be
both enormous and stale within a week). For the literal, current list, `grep -rn 'register(' internal/bcl/*.go`
and `grep -rn 'machineRegistry\[\|genericMachineRegistry\[' internal/interpreter/*.go` are the
ground truth; this document is the curated tour of it.

## System (core types)

- **Primitives and boxing**: `Int16`/`Int32`/`Int64`/`UInt16`/`UInt32`/`UInt64`/`Byte`/`SByte`/
  `Single`/`Double`/`Decimal`/`Boolean`/`Char` — parsing (`TryParse`/`Parse`), formatting,
  `CompareTo`, arithmetic-adjacent members. `System.Convert` covers the common cross-type
  conversions (`ToInt32`, `ToString`, `ToBoolean`, `ToDateTime`, ...) plus `Convert.FromBase64String`/
  `ToBase64String` (`system_convert_base64.go`).
- **`System.String`**: the deepest single file (`system_string.go` + `system_string_statics.go`,
  ~33 registered members) — `Concat`, `Format`, `Substring`, `Split`, `Replace`, `IndexOf`/
  `LastIndexOf`, `Trim` family, `PadLeft`/`PadRight`, `Equals`/`op_Equality`, `Join`, `Contains`,
  `StartsWith`/`EndsWith`, `ToUpper`/`ToLower` (culture-aware via `CultureInfo`), `get_Chars`/
  `get_Length`. `System.MemoryExtensions` adds the `Span<char>`-flavored overloads (`AsSpan`, span
  `IndexOf`/`Trim`) real code increasingly prefers.
- **`System.Math`**: the full common surface (`Pow`, `Round`, `Log`/`Log2`/`Log10`, `Sqrt`, the
  trig functions, `Ceiling`/`Floor`/`Truncate`, `Abs`, `Min`/`Max`, `Clamp`) — closed since Fase
  3.31.
- **`System.DateTime`/`DateTimeOffset`/`TimeSpan`/`TimeZoneInfo`**: construction, arithmetic
  (`Add*`, subtraction producing a `TimeSpan`), comparison, `ToString` with format strings,
  `Now`/`UtcNow`/`Today`, `Parse`/`TryParse`. `System.DateTime` alone registers ~32 members.
  `TimeZoneInfo` covers the common local/UTC lookups.
- **`System.Guid`**: `NewGuid`, `Parse`/`TryParse`, `ToString` (all standard format specifiers),
  equality.
- **`System.Random`**: `Next`/`NextDouble`/`NextBytes` with real, deterministic-when-seeded PRNG
  behavior.
- **`System.Exception` hierarchy**: construction, `Message`/`InnerException`/`StackTrace`, and a
  wide roster of concrete types with real base-type relationships for `catch` matching —
  `ArgumentException`/`ArgumentNullException`/`ArgumentOutOfRangeException`,
  `InvalidOperationException`, `NotSupportedException`, `NotImplementedException`,
  `NullReferenceException`, `IndexOutOfRangeException`, `InvalidCastException`,
  `FormatException`, `OverflowException`, `ObjectDisposedException`, `ApplicationException`,
  `AggregateException` (with real `InnerExceptions`), plus `System.IO`'s `IOException`/
  `FileNotFoundException`/`DirectoryNotFoundException`/`EndOfStreamException` and
  `System.UnauthorizedAccessException` (Fase 3.59) and `System.Data.DataException`.
  `ExceptionDispatchInfo.Capture`/`Throw` (Fase 3.57) preserve a captured stack.
- **`System.Uri`/`UriParser`**: parsing, component accessors, `IsWellFormedUriString`.
- **`System.Object`/`System.Delegate`/`System.Array`**: `Equals`/`GetHashCode`/`ToString`,
  multicast delegate combine/remove, and a wide `System.Array` surface — `Sort`/`BinarySearch`
  (Machine-aware, `internal/interpreter/array_sort.go`), `Reverse`/`Fill`/`Find`/`FindLast`/
  `FindIndex`/`FindAll`/`Exists`/`ForEach`/`TrueForAll`/`ConvertAll`/`LastIndexOf`
  (`internal/interpreter/array_ops.go`), plus the plain-native `Copy`/`Clone`/`Resize`/`IndexOf`/
  `CreateInstance`/`GetValue`/`SetValue`/`get_Length`/`GetLength`/`Empty`/`GetEnumerator`.
- **`System.Nullable<T>`**: real value-type semantics (`HasValue`/`Value`/`GetValueOrDefault`),
  `Nullable.GetUnderlyingType`.
- **`System.ValueTuple`, `System.IntPtr`, `System.BitConverter`, `System.GC`, `System.Environment`**
  (`get_NewLine`, `GetEnvironmentVariable`, `get_ProcessorCount`, `get_CurrentManagedThreadId`),
  `System.AppContext`, `System.WeakReference`/`WeakReference<T>`, `System.Lazy<T>` (`get_Value` is
  Machine-aware to invoke the factory delegate on first access), `System.DBNull`.
- **`System.Drawing.Color`/`Point`**: a small, self-contained subset — enough for packages that
  carry basic drawing-adjacent value types without a real `System.Drawing` dependency.

## System.Collections / System.Collections.Generic / System.Collections.Concurrent

Every mainstream generic collection has a real, native backing store and constructor:
`List<T>` (also `Sort`/`RemoveAll`/`ForEach` via the Machine-aware path for delegate support),
`Dictionary<K,V>` (plus `KeyCollection`/`ValueCollection` and their enumerators), `HashSet<T>`,
`Stack<T>`, `Queue<T>`, `LinkedList<T>`/`LinkedListNode<T>`, `SortedDictionary<K,V>`,
`SortedSet<T>`, `KeyValuePair<K,V>`, `EqualityComparer<T>`/`Comparer<T>` (`Comparer<T>.Create` is
Machine-aware to invoke a comparison delegate). `System.Collections.ObjectModel.Collection<T>`/
`ReadOnlyCollection<T>` and `System.Runtime.CompilerServices.ReadOnlyCollectionBuilder<T>` cover
the wrapper types real libraries expose on their public APIs. Legacy non-generic collections
(`ArrayList`, `Hashtable`, `System.Collections.Stack`, `SortedList`) are supported too — real
code, especially anything targeting older .NET Framework-era APIs (NPOI, for instance), still
uses them. `System.Collections.Concurrent.ConcurrentDictionary<K,V>` (including the Machine-aware
`GetOrAdd` overload that takes a factory delegate) and `ConcurrentQueue<T>` (Fase 3.61) are
supported for the common thread-safe-collection use case, though vmnet has no real concurrent
scheduler underneath — see the async note below.

Every one of these collections' enumerators implements the real `GetEnumerator`/`MoveNext`/
`get_Current` protocol, so a plain `foreach` over any of them — or over an interface-typed
reference to one (`IEnumerable<T>`, `IList<T>`, `IDictionary<K,V>`, `ICollection<T>`) — dispatches
correctly (Fase 3.11/3.13).

## System.Linq (`Enumerable`)

The full common LINQ-to-Objects surface is implemented as Machine-aware natives
(`internal/interpreter/linq.go`, `linq_orderby.go`, `linq_groupby.go`, `linq_range.go`) because
each one needs to invoke a delegate argument and/or drive an arbitrary `IEnumerable<T>` source
through the real enumerator protocol — not something a plain `bcl.Native` can do (Fase 3.15's own
architectural discovery). Covered: `Select`, `Where`, `SelectMany`, `Any`, `All`, `Count`,
`ToList`, `ToArray`, `ToDictionary`, `ToHashSet`, `First`/`FirstOrDefault`, `Single`/
`SingleOrDefault`, `Last`/`LastOrDefault`, `ElementAt`, `Take`/`Skip`/`TakeWhile`/`SkipWhile`,
`Contains`, `Distinct`, `Concat`/`Union`/`Except`/`Intersect`, `Zip`, `Aggregate`, `Sum`/`Average`/
`Min`/`Max`, `Reverse`, `Cast`/`OfType`/`AsEnumerable`, `Empty`, `Range`, `OrderBy`/
`OrderByDescending`/`ThenBy`/`ThenByDescending` (with a real, lazily-materialized `Ordered` result
type), and `GroupBy` (with a real `Grouping` result exposing `.Key` and enumeration). A resolvable
call site typed against the real interfaces `IGrouping<K,T>`/`IOrderedEnumerable<T>` dispatches to
the same natives via virtual-dispatch receiver-type redirection.

## System.Text / System.Text.RegularExpressions

- **`System.Text.StringBuilder`**: `Append` (all common overloads), `Insert`, `Remove`, `Replace`,
  `ToString`, `Clear`, `get_Length`/`Capacity` — the mutable-string workhorse most real code
  reaches for over raw `String` concatenation in a loop.
- **`System.Text.Encoding`/`UTF8Encoding`/`ASCIIEncoding`/`UnicodeEncoding`**: `GetBytes`/
  `GetString`, the static `Encoding.UTF8`/`ASCII`/`Unicode` accessors — real UTF-16↔UTF-8
  transcoding at the byte level (exercised end to end by `examples/system-text-json-demo`, Fase
  3.41), not a stub.
- **`System.Text.RegularExpressions.Regex`**: `IsMatch`, `Match`, `Matches` (`MatchCollection`),
  `Group`/`GroupCollection`/`Capture`, and `Regex.Replace` (Machine-aware,
  `internal/interpreter/regexreplace.go`, to support a replacement-callback delegate, not just a
  replacement string). Built on Go's own `regexp` (RE2) engine — see Known gaps below for what
  that costs.

## System.Threading / System.Threading.Tasks

`System.Threading.Interlocked`, `Volatile`, `Monitor`, `SpinLock`, `ThreadLocal<T>`/`AsyncLocal<T>`
(Fase 3.61), `CancellationToken`/`CancellationTokenSource`/`CancellationTokenRegistration` are all
implemented for real single-threaded semantics (vmnet has no OS-level concurrency underneath, so
these give correct results for code that uses them as synchronization/cancellation primitives
without actually needing real parallel execution).

`async`/`await` (Fase 3.22, "the biggest jump in the sequence") works under a deliberate design
simplification: **every `Task`/`Task<T>` is completed by construction** — there is no real
scheduler or thread pool. `Task.FromResult`, `AsyncTaskMethodBuilder.SetResult`/`SetException`,
`Task.Run`, `Task.Factory.StartNew` all produce an already-completed task, so the compiler-
generated `MoveNext()` state machine's `awaiter.IsCompleted` check is always true and every
`await` proceeds synchronously, in program order, on the same goroutine. Real, sequential `async`
control flow (including `try`/`catch`/`finally` around an `await`) works correctly under this
model; genuine concurrent execution, races, or `Task.WhenAll`/`WhenAny` timing-dependent behavior
do not — see Known gaps.

## System.Reflection / System.Linq.Expressions

Reflection landed incrementally (Fase 3.14 `typeof(T)`/`ldtoken`, 3.16 `IsAssignableFrom`, 3.25
deep `Type` introspection, 3.26 `Enum`, 3.51/3.56/3.58 hardening passes, 3.62 `IsSubclassOf`/
`RuntimeTypeHandle`, 3.63 `CustomAttributeData`) and is now broad enough for real dependency-
injection containers and ORMs to walk their own target types at runtime:

- **`System.Type`**: `GetType()`/`typeof(T)` (pushes a real `System.Type`, no separate
  `RuntimeTypeHandle` Kind needed), `IsAssignableFrom`/`IsInstanceOfType`/`IsSubclassOf`,
  `get_IsValueType`/`IsEnum`/`IsInterface`/`IsClass`/`IsAbstract`/`IsPrimitive`, `GetInterfaces`,
  `get_BaseType`, `GetConstructor(s)`, `GetMethod(s)`, `GetField(s)`, `GetProperty/GetProperties`,
  `GetMember`.
- **`System.Reflection.ConstructorInfo`/`MethodInfo`/`FieldInfo`/`PropertyInfo`/`MemberInfo`/
  `ParameterInfo`**: `Invoke`, `GetValue`/`SetValue`, `GetParameters`, `get_Name`/
  `get_DeclaringType`, the `IsPublic`/`IsPrivate`/`IsFamily`/`IsAssembly`/`IsStatic`/`IsVirtual`/
  `IsAbstract`/`IsFinal` accessor family, and member-identity `op_Equality`/`op_Inequality`.
- **`System.Reflection.CustomAttributeData`/`CustomAttributeTypedArgument`** (Fase 3.63): reads a
  real attribute's constructor arguments and named arguments without instantiating the attribute,
  the deferred-metadata reading path `GetCustomAttributesData` needs.
- **`System.Activator.CreateInstance`** (Machine-aware, `internal/interpreter/activator.go`): real
  reflection-based construction, including through a generic type argument.
- **`System.Enum`**: `GetValues`/`GetNames`/`IsDefined`/`ToObject`/`Parse`/`TryParse`/`HasFlag`/
  `GetUnderlyingType`.
- **`System.Linq.Expressions`**: a genuine expression-tree evaluator (Fase 3.65, generalized from
  an earlier narrower Fase 3.64 slice) — `Expression`/`Expression<T>`, `ParameterExpression`,
  `ConstantExpression`, `MemberExpression`, `MethodCallExpression`, `NewExpression`/
  `NewArrayExpression`, `UnaryExpression`/`BinaryExpression`, `BlockExpression`,
  `ConditionalExpression`, `InvocationExpression`, `TryExpression`/`CatchBlock`, and a real
  `ExpressionVisitor` base with all the standard `Visit*` overrides. `LambdaExpression.Compile()`/
  `Expression<TDelegate>.Compile()` (Machine-aware, `internal/interpreter/exprcompile.go`) produce
  a real, invocable delegate — enough for `FluentValidation`'s property-access lambda compilation
  and `Microsoft.Extensions.DependencyInjection`'s `CallSiteRuntimeResolver` path. Class-level
  generic type-parameter tracking (Fase 3.66) lets this work correctly even inside a generic
  class's own instance methods.

## System.IO

`System.IO.MemoryStream`/`Stream`/`StringReader`/`StringWriter`/`TextReader`/`TextWriter` (Fase
3.30/3.57) are unconditionally available. Real disk I/O — `System.IO.File`/`Directory`/
`FileStream`/`FileInfo`/`DirectoryInfo`/`Path` (Fase 3.59) — is implemented with real, unconditional
Go-level file operations (`ReadAllText`, `WriteAllBytes`, `Create`, `Copy`, `Delete`, every
`FileMode`, ...), gated **deny-by-default** behind a `Permissions` capability model
(`AllowFileRead`/`AllowFileWrite`, checked by `internal/interpreter/permissions.go` before the
native ever runs) — see `docs/en/security.md`. A denied call throws a real
`System.UnauthorizedAccessException`, not a silent no-op. `System.IO.Compression.ZipArchive`/
`ZipArchiveEntry` covers reading/writing zip-based formats (used by `DocumentFormat.OpenXml`'s own
`.docx`/`.xlsx` package format under the hood).

## System.Globalization

`CultureInfo`/`TextInfo` cover the common culture-aware casing and comparison paths `String`'s own
culture-sensitive members (`ToUpper`/`ToLower`, string comparison) delegate to.

## System.Xml / System.Xml.Linq

A genuinely broad surface, driven by real usage inside `DocumentFormat.OpenXml`'s own `.docx`/
`.xlsx` XML package parts: `System.Xml.XmlReader`/`XmlReaderSettings` (~27+2 members),
`XmlWriter`/`XmlWriterSettings` (~17+1), `XmlConvert`, `XmlNameTable`, `XmlQualifiedName`, and the
LINQ-to-XML surface `System.Xml.Linq.XDocument`/`XContainer`/`XElement`/`XAttribute`/`XName`.

## System.Runtime.CompilerServices / System.Runtime.InteropServices / unsafe-adjacent

`RuntimeHelpers.InitializeArray` (the `ldtoken Field` pattern for array literals) and
`IsReferenceOrContainsReferences`/`EnsureSufficientExecutionStack`, `Unsafe` (a small, real subset
— enough for code that touches it incidentally, not a general unsafe-pointer model),
`MemoryMarshal.Read`/`Write` (Machine-aware), `ConditionalWeakTable<TKey,TValue>`,
`AsyncTaskMethodBuilder`/`AsyncTaskMethodBuilder<T>`, `TaskAwaiter`/`TaskAwaiter<T>`,
`ConfiguredTaskAwaitable` and its nested `ConfiguredTaskAwaiter` — the compiler-generated async
state-machine plumbing, not something user code calls directly but required for every `async`
method to actually run.

## System.Data / ADO.NET / Microsoft.Data.Sqlite

`System.Data.IDbConnection`/`IDbCommand`/`IDbDataParameter`/`IDataReader`/`IDataRecord` and the
`System.Data.Common.Db*` abstract classes resolve as real interface/base-class dispatch targets —
no native is needed per member because a real ADO.NET-based micro-ORM (Dapper's `SqlMapper`, most
notably) calls through these abstractions polymorphically. Behind them, vmnet ships one real,
Go-native concrete provider: `Microsoft.Data.Sqlite` (Fase 3.53) — `SqliteConnection`/
`SqliteCommand`/`SqliteParameter`/`SqliteParameterCollection`/`SqliteDataReader`/
`SqliteTransaction`, backed by a real SQLite engine, verified by writing a `.db` file and
re-opening it with the real `sqlite3` CLI (`examples/sqlite-demo`).

## Also present: DocumentFormat.OpenXml and Jint-adjacent hosting hooks

Several `internal/interpreter` files (`elementfactory.go`, `elements.go`, `getattribute.go`,
`getelement.go`, `partcontainer.go`, `loaddomtree.go`, `cloneimp.go`, `attribute_createnew.go`,
`features.go`) register Machine-aware natives keyed to `DocumentFormat.OpenXml.*` types
specifically. These aren't generic BCL — they're targeted natives for one specific, heavily-used
NuGet package's own internal machinery (the thing that makes `DocumentFormat.OpenXml`'s checker
score 100% and its demo produce a real, SDK-round-tripped `.docx`). They're included here because
"supported surface" for a real package includes this kind of package-specific plumbing, not just
`System.*`.

## How natives are registered (for contributors)

Two distinct registration paths exist, and the difference matters if you're deciding where a
missing method belongs. `internal/bcl` registers plain natives — `func(args []runtime.Value)
(runtime.Value, error)` via `register(fullName, hasReturn, fn)` for methods, or `func(args
[]runtime.Value) (*runtime.Object, error)` via `registerCtor`/`registerValueTypeCtor` for
constructors — looked up purely by `"Namespace.Type::Method"` string key, with **no access to the
interpreter's `Machine`**. That's sufficient for the large majority of the BCL: pure functions over
their arguments (`Math.Pow`), or methods that only touch their own receiver's native state
(`StringBuilder.Append`). It is *not* sufficient for anything that needs to invoke a delegate
argument (a LINQ `Select` predicate), walk an arbitrary `IEnumerable<T>` through the real
`GetEnumerator`/`MoveNext` protocol, resolve a generic method's own type arguments, or consult a
capability gate — those live in `internal/interpreter`'s `machineRegistry`/`genericMachineRegistry`
maps (`func(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value,
error)`, plus `methodGenericArgs` for the generic variant), consulted by `Machine.tryCall` in
`calls.go` after `bcl.Lookup` and before falling back to an interpreted method body. If you're
adding a native and it only ever touches its own arguments, it belongs in `internal/bcl`; if it
needs to call back into interpreted code or inspect the call site's resolved generics, it belongs
in `internal/interpreter`.

## Known gaps

These are the real, documented gaps — not guesses. Cited from `docs/en/COMPATIBILITY.md` and
`docs/en/ROADMAP.md`; nothing here is invented.

- **`dynamic`/`ExpandoObject`**: not implemented. `Newtonsoft.Json`'s own `Dynamic`-typing surface
  (`JValue+JValueDynamicProxy`) is the concrete example — the lowest checker score of any package
  with a real demo (85.6%) specifically because of this gap, and the demo deliberately avoids
  exercising it. Don't read a passing demo as "the whole package works" when it has a known
  dynamic-typing surface.
- **Generic type-parameter substitution inside a generic class's own static field initializers**:
  a real, deep architectural limitation — `ClosedXML`'s own default font-metrics engine hits it,
  requiring a small compiled C# wrapper supplying a minimal `IXLGraphicEngine` to work around it in
  the demo. Class-level generic type parameter tracking for *instance* methods was fixed (Fase
  3.66, unblocking `AutoMapper`'s `TypeMap` registration and `CsvHelper`'s `ClassMap.GetGenericType`
  chain), but the static-initializer shape above remains open.
  `Type.GetConstructor()` also loses closed-generic identity at the reflection boundary
  (`CsvHelper`'s `AutoMap()` construction path) — a separate, deliberate, pre-existing
  simplification.
- **Boxed-zero null-conditional edge** (found and root-caused in Fase 3.68, still an open,
  narrow limitation): a boxed value type whose value equals its type's zero (a boxed `int`
  holding `0`) is indistinguishable from a real null by vmnet's identity-passthrough `box`, so
  `x?.ToString()`-style null-conditional checks on such a value incorrectly treat it as null. Hits
  `FluentValidation`'s `InclusiveBetween` message formatting only when a bound is exactly `0`.
- **Regex is Go's RE2, not the real .NET regex engine** (Fase 3.20): the two dialects agree on the
  vast majority of real-world usage (character classes, quantifiers, anchors, groups,
  alternation), but RE2 has **no backreferences and no lookaround** (`(?=...)`/`(?<=...)`/
  `(?!...)`) at all — not a partial implementation, a hard engine limitation that can't be worked
  around without replacing the regex engine entirely. `Dapper`'s own SQL-parameter regex
  (`(?<![\p{L}\p{N}_])\{=([\p{L}\p{N}_]+)\}`, a negative lookbehind) is a real, permanent,
  documented example of a pattern that can never compile under vmnet.
- **No real concurrency under `async`/`Task`**: every `Task` completes synchronously at creation
  (Fase 3.22's deliberate design decision) — real sequential `async` control flow works, but
  genuine parallelism, races, and timing-dependent `Task.WhenAll`/`WhenAny` semantics do not exist.
- **No `System.Diagnostics.Process`, no raw `System.Net.Sockets`, no P/Invoke, no `unsafe` pointer
  arithmetic, no `Reflection.Emit`**: `Process`/raw sockets were deliberately left unimplemented
  after a corpus-wide scan (Fase 3.59) found zero real demand for either across 19 tracked
  packages — not a technical wall, a "build it when real demand shows up" decision.
  `System.Net.Http`/`System.Net.IPAddress` had modest real demand found (`ClosedXML`'s `HttpClient`
  usage, `SimpleBase`'s `IPAddress`) but aren't implemented yet; any future networking native would
  be gated by the same `Permissions.AllowNetwork` capability the model already reserves for it
  (currently defined but enforced nowhere, since nothing network-touching exists yet).
- **`AllowConsole`/`AllowNetwork` gate nothing today**: `Permissions` defines both fields for
  forward compatibility, but `System.Console.Write`/`WriteLine` remains always-allowed and no
  network-touching native exists to gate.
- **`FileStream`'s `FileAccess`/`FileShare` constructor arguments are accepted but not enforced**,
  and **`File.Copy`'s `CreateNew` `FileMode` doesn't distinguish itself from `Create`/`Truncate`**
  (real .NET throws `IOException` if the destination exists; vmnet always succeeds) — both
  documented, narrow simplifications from Fase 3.59, with no known real corpus caller relying on
  either specific failure path.
- **Dependency-injection expression-tree fast paths are unverifiable, not necessarily broken**:
  `Microsoft.Extensions.DependencyInjection`'s own `ExpressionResolverBuilder` compiled fast path is
  a background, best-effort optimization that silently falls back on any compile failure — real
  demo coverage exercises the always-active `CallSiteRuntimeResolver` path instead, so the fast
  path's actual behavior under vmnet isn't independently confirmed either way.

## The authoritative answer for your assembly

Everything above describes the general shape of the supported surface. For any real assembly or
NuGet package, run the checker — it walks every method your code (and its full transitive
dependency graph) actually calls and reports, resolvable-target by resolvable-target, exactly what
would and wouldn't run, under the profile you choose:

```bash
./vmnet check --profile=netstandard-lite path/to/YourAssembly.dll
./vmnet check package --profile=netstandard-lite SomePackage@1.2.3
```

See `docs/en/COMPATIBILITY.md` for 19 real packages measured this way (plus, where one exists, a
real running demo confirming actual behavior against real .NET output — the checker percentage
alone is a coverage estimate, not a correctness proof).
