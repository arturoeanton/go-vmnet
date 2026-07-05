package checker

import (
	"errors"
	"fmt"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/il"
	"github.com/arturoeanton/go-vmnet/internal/ir"
	"github.com/arturoeanton/go-vmnet/internal/metadata"
	"github.com/arturoeanton/go-vmnet/internal/pe"
)

// Analyze walks every method vmnet's pipeline could plausibly execute and
// tries the exact same steps Assembly.Call would (IL decode, IR build,
// call-target resolution) — so a "compatible" verdict means "this will
// actually run", not a separate heuristic's guess. It never returns an
// error itself: parse/decode/build failures become Findings, because a
// checker that panics or bails on the first bad method defeats its own
// purpose (spec §23: "the checker is mandatory, without it the user
// suffers").
func Analyze(f *pe.File, md *metadata.Metadata, profile Profile) *Report {
	return AnalyzeWithDeps(f, md, nil, profile)
}

// AnalyzeWithDeps is Analyze, but a call/constructor target that isn't
// resolvable against md's own metadata is also tried against each of deps'
// metadata before being flagged (Fase 3.29) — mirroring
// Assembly.WithDependencies (Fase 3.27): a real package's IL frequently
// calls straight into another real package's own types (Jint -> Esprima,
// NPOI -> ZString/SkiaSharp/BouncyCastle.Cryptography), and those calls
// genuinely run at runtime once vm.LoadPackage attaches the resolved
// dependency chain — the callee's own method body gets decoded and
// interpreted on its own terms, exactly like a call within md itself, so
// flagging it as "unsupported" would be a false negative. deps should be
// the package's full transitive dependency graph (e.g. via
// internal/nuget.Resolver), not just its direct dependencies.
func AnalyzeWithDeps(f *pe.File, md *metadata.Metadata, deps []*metadata.Metadata, profile Profile) *Report {
	report := &Report{Profile: profile}
	if asm, err := md.Assembly(1); err == nil {
		report.AssemblyName = asm.Name
	}

	if md.RowCount(metadata.TableImplMap) > 0 {
		report.Findings = append(report.Findings, Finding{
			Kind:       KindPInvoke,
			Detail:     "assembly declares P/Invoke method(s) (ImplMap table present)",
			Suggestion: "P/Invoke is not supported in pure-Go mode",
		})
	}

	typeCount := md.RowCount(metadata.TableTypeDef)
	for typeRID := uint32(1); typeRID <= typeCount; typeRID++ {
		typeDef, err := md.TypeDef(typeRID)
		if err != nil {
			continue
		}
		if typeDef.Name == "<Module>" {
			continue
		}

		start, end, err := md.TypeDefMethodRange(typeRID)
		if err != nil {
			continue
		}
		for methodRID := start; methodRID < end; methodRID++ {
			row, err := md.MethodDef(methodRID)
			if err != nil {
				continue
			}
			if row.RVA == 0 {
				continue // abstract/extern/P-Invoke: no IL body to analyze
			}

			typeName, err := ir.QualifyTypeDefName(md, typeRID, typeDef)
			if err != nil {
				continue
			}
			fullName := typeName + "::" + row.Name
			report.MethodsAnalyzed++

			findings := analyzeMethod(f, md, deps, fullName, row, profile)
			if len(findings) > 0 {
				report.MethodsFlagged++
				report.Findings = append(report.Findings, findings...)
			}
		}
	}

	report.finalize()
	return report
}

func analyzeMethod(f *pe.File, md *metadata.Metadata, deps []*metadata.Metadata, fullName string, row metadata.MethodDefRow, profile Profile) []Finding {
	sig, err := metadata.ParseMethodSig(row.Signature)
	if err != nil {
		return []Finding{{
			Kind:   KindUnsupportedOpcode,
			Method: fullName,
			Detail: fmt.Sprintf("unparseable method signature: %v", err),
		}}
	}

	var findings []Finding
	findings = append(findings, signatureFindings(fullName, sig)...)

	body, err := f.RVA(row.RVA)
	if err != nil {
		return append(findings, Finding{Kind: KindUnsupportedOpcode, Method: fullName, Detail: fmt.Sprintf("reading method body: %v", err)})
	}
	header, code, err := il.ReadMethodBody(body)
	if err != nil {
		return append(findings, Finding{Kind: KindUnsupportedOpcode, Method: fullName, Detail: fmt.Sprintf("reading method header: %v", err)})
	}
	instrs, err := il.Decode(code)
	if err != nil {
		return append(findings, Finding{Kind: KindUnsupportedOpcode, Method: fullName, Detail: fmt.Sprintf("decoding IL: %v", err)})
	}
	var ehClauses []il.ExceptionHandler
	if header.MoreSections {
		ehClauses, err = il.ReadExceptionHandlers(body, header, 12+int(header.CodeSize))
		if err != nil {
			return append(findings, Finding{Kind: KindUnsupportedOpcode, Method: fullName, Detail: fmt.Sprintf("reading exception handlers: %v", err)})
		}
	}

	retVoid := sig.RetType.Kind == metadata.SigVoid
	irInstrs, _, err := ir.Build(instrs, md, retVoid, ehClauses)
	if err != nil {
		var uo *ir.UnsupportedOpcodeError
		if errors.As(err, &uo) {
			return append(findings, Finding{
				Kind:       KindUnsupportedOpcode,
				Method:     fullName,
				Detail:     fmt.Sprintf("%s (IL offset %d)", uo.OpCode, uo.Offset),
				Suggestion: suggestionFor(uo.OpCode),
			})
		}
		return append(findings, Finding{Kind: KindUnsupportedOpcode, Method: fullName, Detail: err.Error()})
	}

	if !objectOpcodesAllowed(profile) {
		for _, instr := range irInstrs {
			if instrIsObjectModel(instr) {
				// One finding for the whole method, not one per instruction:
				// under `minimal` the method can't run at all regardless of
				// which particular object-model instructions it uses.
				return append(findings, Finding{
					Kind:       KindOutOfProfile,
					Method:     fullName,
					Detail:     "uses the object model (classes/fields/callvirt/throw), not part of this profile",
					Suggestion: "use profile \"rules\" or \"netstandard-lite\"",
				})
			}
		}
	}

	for _, instr := range irInstrs {
		switch in := instr.(type) {
		case ir.Call:
			findings = append(findings, checkTarget(md, deps, fullName, in.FullName, profile, resolvableMethod)...)
		case ir.NewObj:
			findings = append(findings, checkTarget(md, deps, fullName, in.CtorFullName, profile, func(md *metadata.Metadata, name string) bool {
				return resolvableCtor(md, in.TypeFullName, name)
			})...)
		}
	}
	return findings
}

func checkTarget(md *metadata.Metadata, deps []*metadata.Metadata, enclosing, target string, profile Profile, resolvable func(*metadata.Metadata, string) bool) []Finding {
	if resolvable(md, target) {
		if !inProfile(profile, target) && !isLocalMethod(md, target) {
			return []Finding{{
				Kind:   KindOutOfProfile,
				Method: enclosing,
				Detail: target,
			}}
		}
		return nil
	}
	for _, dep := range deps {
		if resolvable(dep, target) {
			// Resolves against a loaded dependency's own real IL/native —
			// not subject to the current profile's allowlist, same as a
			// call within md itself: the callee's own body (or its own
			// bcl.Lookup native) is what actually runs, not this call site.
			return nil
		}
	}
	return []Finding{{
		Kind:       categorize(target),
		Method:     enclosing,
		Detail:     target,
		Suggestion: suggestionForTarget(target),
	}}
}

func resolvableMethod(md *metadata.Metadata, fullName string) bool {
	if _, _, ok := bcl.Lookup(fullName); ok {
		return true
	}
	// A delegate's Invoke (Fase 3.9) is resolved by the interpreter
	// structurally at runtime (any delegate type at all — Action, Func`2,
	// a local `delegate` declaration), not via per-type registration in
	// bcl.Lookup, so the checker has to recognize the same shape itself —
	// see isDelegateType.
	if typeName, ok := strings.CutSuffix(fullName, "::Invoke"); ok && isDelegateType(md, typeName) {
		return true
	}
	if interfaceDispatchTargets[fullName] {
		return true
	}
	if linqTargets[fullName] {
		return true
	}
	if fullName == "System.Type::IsAssignableFrom" || fullName == "System.Lazy`1::get_Value" ||
		fullName == "System.Collections.Concurrent.ConcurrentDictionary`2::GetOrAdd" {
		return true
	}
	if asyncMachineTargets[fullName] {
		return true
	}
	if reflectionMachineTargets[fullName] {
		return true
	}
	if arrayMachineTargets[fullName] {
		return true
	}
	if isAdoNetDispatchTarget(fullName) {
		return true
	}
	if fullName == "System.Runtime.CompilerServices.RuntimeHelpers::InitializeArray" {
		return true
	}
	return isLocalMethod(md, fullName)
}

// arrayMachineTargets lists System.Array/List<T> members resolved
// through the Machine-aware registry (array_ops.go/array_sort.go —
// Find/FindLast/FindIndex/FindAll/Exists/ForEach/TrueForAll/ConvertAll
// all take a Predicate`1/Action`1/Converter`2 delegate argument, and
// Sort/BinarySearch take an optional IComparer`1/Comparison`1, neither
// available to a plain bcl.Native) rather than bcl.Lookup — this map
// simply never mirrored them until now, the same parity gap
// reflectionMachineTargets' own Fase 3.51 entries just fixed for
// PropertyInfo.
var arrayMachineTargets = map[string]bool{
	"System.Array::Reverse":                        true,
	"System.Array::Fill":                           true,
	"System.Array::Find":                           true,
	"System.Array::FindLast":                       true,
	"System.Array::FindIndex":                      true,
	"System.Array::FindAll":                        true,
	"System.Array::Exists":                         true,
	"System.Array::ForEach":                        true,
	"System.Array::TrueForAll":                     true,
	"System.Array::ConvertAll":                     true,
	"System.Array::LastIndexOf":                    true,
	"System.Array::Sort":                           true,
	"System.Array::BinarySearch":                   true,
	"System.Collections.Generic.List`1::Sort":      true,
	"System.Collections.Generic.List`1::RemoveAll": true,
}

// adoNetDispatchTypes lists System.Data's connection/command/reader/
// parameter surface (Fase 3.52) — Dapper's SqlMapper (and any other
// ADO.NET-based micro-ORM) does its real object-relational mapping
// directly against these interfaces/abstract classes, never against a
// concrete provider type by name: the real concrete implementation is
// always supplied by whichever real driver (or, for vmnet's own
// examples/dapper-demo, a minimal in-memory fake) the caller passes in,
// and Machine.call's virtual-dispatch ancestor walk (calls.go) already
// resolves every one of these interface/abstract-class members straight
// through to that concrete implementation's own real method — the same
// mechanism interfaceDispatchTargets above documents for IEnumerable`1/
// IEqualityComparer`1/IComparer`1. Keyed by TYPE rather than enumerated
// per-method like interfaceDispatchTargets: real ADO.NET's own member
// count across IDbConnection/IDbCommand/IDataReader/IDataRecord/
// IDataParameter/IDbDataParameter/DbDataReader/DbCommand/DbConnection is
// large (~60 real members between them, most never called by any one
// package), and every single one resolves through the identical
// mechanism — enumerating each one individually the way
// interfaceDispatchTargets does would just repeat the same fact ~60
// times for no added precision.
var adoNetDispatchTypes = map[string]bool{
	"System.Data.IDbConnection":                true,
	"System.Data.IDbCommand":                   true,
	"System.Data.IDbTransaction":               true,
	"System.Data.IDataReader":                  true,
	"System.Data.IDataRecord":                  true,
	"System.Data.IDataParameter":               true,
	"System.Data.IDbDataParameter":             true,
	"System.Data.IDataParameterCollection":     true,
	"System.Data.Common.DbConnection":          true,
	"System.Data.Common.DbCommand":             true,
	"System.Data.Common.DbDataReader":          true,
	"System.Data.Common.DbParameter":           true,
	"System.Data.Common.DbParameterCollection": true,
	"System.Data.Common.DbTransaction":         true,
}

// isAdoNetDispatchTarget reports whether fullName names a method on one
// of adoNetDispatchTypes' own types (any member — this deliberately
// doesn't enumerate which ones, see adoNetDispatchTypes' own doc
// comment).
func isAdoNetDispatchTarget(fullName string) bool {
	idx := strings.LastIndex(fullName, "::")
	if idx < 0 {
		return false
	}
	return adoNetDispatchTypes[fullName[:idx]]
}

// reflectionMachineTargets lists the System.Type introspection methods
// resolved through the Machine-aware registry (Fase 3.25,
// internal/interpreter/reflection.go) rather than bcl.Lookup — a plugin
// type's real IsValueType/IsEnum/IsInterface/BaseType/GetInterfaces needs
// its actual TypeDef (Machine.ResolveType), unavailable to a plain
// bcl.Native.
var reflectionMachineTargets = map[string]bool{
	"System.Type::get_IsValueType": true,
	"System.Type::get_IsEnum":      true,
	"System.Type::get_IsInterface": true,
	"System.Type::get_IsAbstract":  true,
	"System.Type::get_IsPrimitive": true,
	"System.Type::get_BaseType":    true,
	"System.Type::GetInterfaces":   true,
	"System.Type::GetType":         true,
	"System.Enum::GetValues":       true,
	"System.Enum::GetNames":        true,
	"System.Enum::IsDefined":       true,
	"System.Enum::ToObject":        true,
	// Real reflection (Fase 3.39) — see internal/interpreter/reflection.go.
	"System.Type::GetConstructor":                           true,
	"System.Type::GetMethod":                                true,
	"System.Type::GetField":                                 true,
	"System.Reflection.ConstructorInfo::Invoke":             true,
	"System.Reflection.MethodInfo::Invoke":                  true,
	"System.Reflection.MethodBase::Invoke":                  true,
	"System.Reflection.FieldInfo::GetValue":                 true,
	"System.Reflection.ConstructorInfo::op_Inequality":      true,
	"System.Reflection.ConstructorInfo::op_Equality":        true,
	"System.Reflection.MethodInfo::op_Inequality":           true,
	"System.Reflection.MethodInfo::op_Equality":             true,
	"System.Reflection.Assembly::GetManifestResourceStream": true,
	// Type.GetProperties/GetProperty plus PropertyInfo.GetValue/SetValue
	// (Fase 3.51) — real natives (internal/interpreter/reflection.go)
	// this map simply never mirrored until now, so every real call site
	// using them was misreported as unsupported despite already working
	// at runtime (found auditing Dapper@2.1.79's own checker findings
	// against its real, verified-working reflection-based row mapper).
	"System.Type::GetProperties":                    true,
	"System.Type::GetProperty":                      true,
	"System.Reflection.PropertyInfo::GetValue":      true,
	"System.Reflection.PropertyInfo::SetValue":      true,
	"System.Reflection.PropertyInfo::op_Inequality": true,
	"System.Reflection.PropertyInfo::op_Equality":   true,
	// Type.GetConstructors (plural) plus MethodBase.GetParameters/
	// ParameterInfo (Fase 3.52) — see internal/interpreter/reflection.go's
	// typeGetConstructors/methodBaseGetParameters.
	"System.Type::GetConstructors":                     true,
	"System.Reflection.ConstructorInfo::GetParameters": true,
	"System.Reflection.MethodInfo::GetParameters":      true,
	"System.Reflection.MethodBase::GetParameters":      true,
	// Activator.CreateInstance(Type, object[]) has been a real,
	// working genericMachineRegistry entry since Fase 3.39
	// (internal/interpreter/activator.go) — this map simply never
	// mirrored it, the same class of gap as every other entry in this
	// comment block, found the same way (auditing the full 19-package
	// corpus's aggregated checker findings).
	"System.Activator::CreateInstance": true,
	// Type.GetFields()/GetMethods() (plural, Fase 3.56) — real
	// machineRegistry entries (internal/interpreter/reflection.go's
	// typeGetFields/typeGetMethods) mirroring the exact same gap this
	// map's own GetProperties/GetConstructors entries already fixed once
	// for their own plural reflection methods.
	"System.Type::GetFields":  true,
	"System.Type::GetMethods": true,
}

// asyncMachineTargets lists the async-related methods resolved through
// the Machine-aware registry (Fase 3.22, internal/interpreter/async.go)
// rather than bcl.Lookup — AsyncTaskMethodBuilder.Start/
// AwaitUnsafeOnCompleted need to invoke the compiler-generated state
// machine's own MoveNext() method, and Task.Run needs to invoke a
// delegate argument, neither available to a plain bcl.Native.
var asyncMachineTargets = map[string]bool{
	"System.Runtime.CompilerServices.AsyncTaskMethodBuilder::Start":                    true,
	"System.Runtime.CompilerServices.AsyncTaskMethodBuilder`1::Start":                  true,
	"System.Runtime.CompilerServices.AsyncTaskMethodBuilder::AwaitUnsafeOnCompleted":   true,
	"System.Runtime.CompilerServices.AsyncTaskMethodBuilder`1::AwaitUnsafeOnCompleted": true,
	"System.Threading.Tasks.Task::Run":                                                 true,
}

// linqTargets lists the System.Linq.Enumerable methods the interpreter
// resolves through a dedicated Machine-aware registry (Fase 3.14,
// internal/interpreter/linq.go) rather than bcl.Lookup — LINQ needs to
// invoke a delegate argument and drive an arbitrary source's real
// iteration protocol, neither available to a plain bcl.Native, so these
// never appear in the bcl.Lookup checked above. Kept as its own map
// (not folded into interfaceDispatchTargets) since the reason a
// checker-only allowlist is needed here is different: not "the checker
// can't know the concrete receiver type," but "the checker doesn't know
// about the interpreter's separate LINQ registry at all."
var linqTargets = map[string]bool{
	"System.Linq.Enumerable::Select":            true,
	"System.Linq.Enumerable::Where":             true,
	"System.Linq.Enumerable::Any":               true,
	"System.Linq.Enumerable::All":               true,
	"System.Linq.Enumerable::ToList":            true,
	"System.Linq.Enumerable::ToArray":           true,
	"System.Linq.Enumerable::FirstOrDefault":    true,
	"System.Linq.Enumerable::SelectMany":        true,
	"System.Linq.Enumerable::Take":              true,
	"System.Linq.Enumerable::Contains":          true,
	"System.Linq.Enumerable::Empty":             true,
	"System.Linq.Enumerable::Cast":              true,
	"System.Linq.Enumerable::OfType":            true,
	"System.Linq.Enumerable::First":             true,
	"System.Linq.Enumerable::LastOrDefault":     true,
	"System.Linq.Enumerable::Count":             true,
	"System.Linq.Enumerable::Distinct":          true,
	"System.Linq.Enumerable::OrderBy":           true,
	"System.Linq.Enumerable::Concat":            true,
	"System.Linq.Enumerable::ToDictionary":      true,
	"System.Linq.Enumerable::Max":               true,
	"System.Linq.Enumerable::Single":            true,
	"System.Linq.Enumerable::SingleOrDefault":   true,
	"System.Linq.Enumerable::OrderByDescending": true,
	"System.Linq.Enumerable::ElementAt":         true,
	"System.Linq.Enumerable::Skip":              true,
	"System.Linq.Enumerable::Union":             true,
	// The Fase 3.44/3.45 LINQ hardening pass added all of these as real,
	// working machineRegistry entries (internal/interpreter/linq.go,
	// linq_orderby.go) but never mirrored them here — every real call
	// site using any of them was misreported as unsupported despite
	// already running correctly, the same parity gap already fixed once
	// for Type.GetProperties/GetProperty (Fase 3.51) and Type.
	// GetConstructors (Fase 3.52). Found auditing the full 19-package
	// corpus's own aggregated checker findings, not a single package's.
	"System.Linq.Enumerable::GroupBy":          true,
	"System.Linq.Enumerable::ThenBy":           true,
	"System.Linq.Enumerable::ThenByDescending": true,
	"System.Linq.Enumerable::Min":              true,
	"System.Linq.Enumerable::Sum":              true,
	"System.Linq.Enumerable::Average":          true,
	"System.Linq.Enumerable::Aggregate":        true,
	"System.Linq.Enumerable::Zip":              true,
	"System.Linq.Enumerable::Except":           true,
	"System.Linq.Enumerable::Intersect":        true,
	"System.Linq.Enumerable::SkipWhile":        true,
	"System.Linq.Enumerable::TakeWhile":        true,
	"System.Linq.Enumerable::Reverse":          true,
	"System.Linq.Enumerable::AsEnumerable":     true,
	"System.Linq.Enumerable::ToHashSet":        true,
	// Not System.Linq.Enumerable, but the same "resolved through a
	// Machine-aware registry, not bcl.Lookup" reason applies (Fase 3.32):
	// List<T>.ForEach needs to invoke its Action<T> argument.
	"System.Collections.Generic.List`1::ForEach": true,
}

// interfaceDispatchTargets lists the foreach iteration protocol's
// interface-declared "Type::Method" call targets — what
// System.Collections.Generic.IEnumerable`1::GetEnumerator etc. actually
// look like at a call site typed against the interface rather than a
// concrete collection. The interpreter's runtime fallback (Fase 3.13,
// internal/interpreter/calls.go's tryCall/receiverTypeName) resolves
// these by redirecting to the receiver's real concrete type at call
// time — something the checker, being static, can't determine itself
// (it would need real data-flow analysis of which concrete type flows
// into an interface-typed local). This is a best-effort allowlist of
// exactly what the Fase 3.13 probe measured as the dominant real-world
// pattern, not a claim that every interface call resolves; same
// approximate posture as isDelegateType's well-known prefixes.
var interfaceDispatchTargets = map[string]bool{
	"System.Collections.Generic.IEnumerable`1::GetEnumerator": true,
	"System.Collections.IEnumerable::GetEnumerator":           true,
	"System.Collections.Generic.IEnumerator`1::get_Current":   true,
	"System.Collections.Generic.IEnumerator`1::MoveNext":      true,
	"System.Collections.IEnumerator::get_Current":             true,
	"System.Collections.IEnumerator::MoveNext":                true,
	"System.Collections.IEnumerator::Reset":                   true,
	"System.Collections.Generic.ICollection`1::Add":           true,
	"System.Collections.Generic.ICollection`1::get_Count":     true,
	"System.Collections.ICollection::get_Count":               true,
	"System.Collections.IList::Add":                           true,
	"System.Collections.IList::get_Item":                      true,
	"System.Collections.IList::set_Item":                      true,
	// IList::Clear resolves against a concrete List`1/ArrayList receiver
	// exactly like Add/get_Item/set_Item above (both already register a
	// real "...List`1::Clear"/"ArrayList::Clear" native — system_
	// collections.go); this entry was simply missing before Fase 3.52,
	// misreporting a real, already-working call as unsupported.
	"System.Collections.IList::Clear":                             true,
	"System.Collections.Generic.IDictionary`2::set_Item":          true,
	"System.Collections.Generic.IDictionary`2::get_Item":          true,
	"System.Collections.Generic.IDictionary`2::TryGetValue":       true,
	"System.Collections.Generic.IDictionary`2::ContainsKey":       true,
	"System.Collections.Generic.IDictionary`2::Add":               true,
	"System.Collections.Generic.IDictionary`2::Remove":            true,
	"System.Collections.Generic.IDictionary`2::get_Keys":          true,
	"System.Collections.Generic.IList`1::get_Item":                true,
	"System.Collections.Generic.IList`1::set_Item":                true,
	"System.Collections.Generic.IReadOnlyList`1::get_Item":        true,
	"System.Collections.Generic.IReadOnlyCollection`1::get_Count": true,
	"System.Collections.Generic.IEqualityComparer`1::Equals":      true,
	"System.Collections.Generic.IEqualityComparer`1::GetHashCode": true,
	// A LINQ GroupBy/OrderBy result is a real, working native
	// (bcl.NativeGrouping/NativeOrdered, Fase 3.44/3.45) reached through
	// exactly this same runtime redirection — `group.Key`/a direct
	// `foreach` over either result, when the call site happens to be
	// declared against the real BCL interface name instead of the
	// already-materialized IEnumerable<T> case above. The checker can no
	// more see through this virtual dispatch than any other case in this
	// map; found the same way, auditing the full 19-package corpus.
	"System.Linq.IGrouping`2::get_Key":                true,
	"System.Linq.IGrouping`2::GetEnumerator":          true,
	"System.Linq.IOrderedEnumerable`1::GetEnumerator": true,
}

func resolvableCtor(md *metadata.Metadata, typeFullName, ctorFullName string) bool {
	if typeFullName == "System.String" {
		return true
	}
	if _, ok := bcl.LookupCtor(typeFullName); ok {
		return true
	}
	if _, ok := bcl.LookupValueTypeCtor(typeFullName); ok {
		return true
	}
	if strings.HasSuffix(ctorFullName, "::.ctor") && isDelegateType(md, typeFullName) {
		return true
	}
	return isLocalMethod(md, ctorFullName)
}

// wellKnownDelegatePrefixes covers the overwhelming majority of
// real-world delegate usage — BCL delegate types have no TypeDef in the
// loaded assembly (same problem as Nullable`1 in Fase 3.7), so unlike a
// plugin's own `delegate` declaration (checked below via its real
// TypeDef's Extends) there's no metadata to inspect at all, only the name.
var wellKnownDelegatePrefixes = []string{
	"System.Action",
	"System.Func`",
	"System.Predicate`1",
	"System.Comparison`1",
	"System.EventHandler",
	"System.AsyncCallback",
}

// isDelegateType reports whether typeFullName names a delegate: a
// well-known BCL one by prefix, or a plugin's own `delegate` declaration
// (a real TypeDef extending System.MulticastDelegate/System.Delegate,
// resolvable the same way analyzer.go's isValueType-equivalent check in
// assembly.go works for structs).
func isDelegateType(md *metadata.Metadata, typeFullName string) bool {
	for _, prefix := range wellKnownDelegatePrefixes {
		if strings.HasPrefix(typeFullName, prefix) {
			return true
		}
	}
	dot := strings.LastIndex(typeFullName, ".")
	namespace, name := "", typeFullName
	if dot >= 0 {
		namespace, name = typeFullName[:dot], typeFullName[dot+1:]
	}
	_, typeDef, err := md.FindTypeDef(namespace, name)
	if err != nil || typeDef.Extends.IsNil() {
		return false
	}
	base, err := resolveBaseTypeName(md, typeDef.Extends)
	return err == nil && (base == "System.MulticastDelegate" || base == "System.Delegate")
}

// resolveBaseTypeName resolves a TypeDef/TypeRef token to "Namespace.Name"
// — a narrower duplicate of assembly.go's resolveTypeTokenName (root
// package, not importable from here) sized to exactly what isDelegateType
// needs: a delegate's Extends is always a plain TypeRef, never a TypeSpec.
func resolveBaseTypeName(md *metadata.Metadata, tok metadata.Token) (string, error) {
	switch tok.Table() {
	case metadata.TableTypeRef:
		row, err := md.TypeRef(tok.RID())
		if err != nil {
			return "", err
		}
		if row.Namespace == "" {
			return row.Name, nil
		}
		return row.Namespace + "." + row.Name, nil
	case metadata.TableTypeDef:
		row, err := md.TypeDef(tok.RID())
		if err != nil {
			return "", err
		}
		if row.Namespace == "" {
			return row.Name, nil
		}
		return row.Namespace + "." + row.Name, nil
	default:
		return "", fmt.Errorf("unsupported base-type token table %#x", byte(tok.Table()))
	}
}

func isLocalMethod(md *metadata.Metadata, fullName string) bool {
	namespace, typeName, methodName, err := ir.SplitFullName(fullName)
	if err != nil {
		return false
	}
	typeRID, _, err := md.FindTypeDef(namespace, typeName)
	if err != nil {
		return false
	}
	_, _, err = md.FindMethodDef(typeRID, methodName)
	return err == nil
}

// categorize turns an unresolved call target's full name into a
// human-meaningful reason category, mirroring spec §23.3's grouped
// output ("heavy reflection", "async/Task usage", ...).
func categorize(fullName string) FindingKind {
	switch {
	case strings.HasPrefix(fullName, "System.Reflection."):
		return KindReflection
	case strings.HasPrefix(fullName, "System.Threading.Tasks."):
		return KindAsync
	default:
		return KindUnsupportedMethod
	}
}

func suggestionForTarget(fullName string) string {
	switch categorize(fullName) {
	case KindReflection:
		return "avoid reflection-heavy code paths; only typeof/GetType/Type.Name are supported"
	case KindAsync:
		return "avoid async/Task — vmnet has no async runtime yet"
	default:
		return "this BCL method has no native implementation yet"
	}
}

func suggestionFor(opcode string) string {
	switch opcode {
	case "ldtoken":
		return "array literal initializers (RuntimeHelpers.InitializeArray) are not supported yet — assign elements individually instead"
	case "filter (catch-when)":
		return "exception filter clauses (catch (T) when (cond)) are not supported yet — catch (T) without the filter is"
	default:
		return "not yet implemented — see docs/en/ROADMAP.md"
	}
}

// signatureFindings flags parameter/return shapes vmnet can't execute
// correctly even though the signature itself parses fine. As of Fase 3.5,
// that's only raw unmanaged pointers (true `unsafe` code, spec-illegal in
// normal C#) — by-ref parameters (ref/out/in) execute correctly via
// managed pointers (runtime.KindRef), so they're no longer flagged here.
func signatureFindings(fullName string, sig metadata.MethodSig) []Finding {
	var findings []Finding
	findings = append(findings, sigShapeFindings(fullName, "return type", sig.RetType)...)
	for i, p := range sig.Params {
		findings = append(findings, sigShapeFindings(fullName, fmt.Sprintf("parameter %d", i), p)...)
	}
	return findings
}

func sigShapeFindings(fullName, where string, t metadata.SigType) []Finding {
	switch t.Kind {
	case metadata.SigPointer:
		return []Finding{{Kind: KindUnsafePointer, Method: fullName, Detail: where + " is an unmanaged pointer (unsafe code)"}}
	default:
		return nil
	}
}
