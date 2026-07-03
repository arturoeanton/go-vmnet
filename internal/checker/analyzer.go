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

			findings := analyzeMethod(f, md, fullName, row, profile)
			if len(findings) > 0 {
				report.MethodsFlagged++
				report.Findings = append(report.Findings, findings...)
			}
		}
	}

	report.finalize()
	return report
}

func analyzeMethod(f *pe.File, md *metadata.Metadata, fullName string, row metadata.MethodDefRow, profile Profile) []Finding {
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
			findings = append(findings, checkTarget(md, fullName, in.FullName, profile, resolvableMethod)...)
		case ir.NewObj:
			findings = append(findings, checkTarget(md, fullName, in.CtorFullName, profile, func(md *metadata.Metadata, name string) bool {
				return resolvableCtor(md, in.TypeFullName, name)
			})...)
		}
	}
	return findings
}

func checkTarget(md *metadata.Metadata, enclosing, target string, profile Profile, resolvable func(*metadata.Metadata, string) bool) []Finding {
	if !resolvable(md, target) {
		return []Finding{{
			Kind:       categorize(target),
			Method:     enclosing,
			Detail:     target,
			Suggestion: suggestionForTarget(target),
		}}
	}
	if !inProfile(profile, target) && !isLocalMethod(md, target) {
		return []Finding{{
			Kind:   KindOutOfProfile,
			Method: enclosing,
			Detail: target,
		}}
	}
	return nil
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
	if fullName == "System.Type::IsAssignableFrom" || fullName == "System.Lazy`1::get_Value" {
		return true
	}
	return isLocalMethod(md, fullName)
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
	"System.Linq.Enumerable::Select":         true,
	"System.Linq.Enumerable::Where":          true,
	"System.Linq.Enumerable::Any":            true,
	"System.Linq.Enumerable::All":            true,
	"System.Linq.Enumerable::ToList":         true,
	"System.Linq.Enumerable::ToArray":        true,
	"System.Linq.Enumerable::FirstOrDefault": true,
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
	"System.Collections.Generic.IEnumerable`1::GetEnumerator":     true,
	"System.Collections.IEnumerable::GetEnumerator":               true,
	"System.Collections.Generic.IEnumerator`1::get_Current":       true,
	"System.Collections.Generic.IEnumerator`1::MoveNext":          true,
	"System.Collections.IEnumerator::get_Current":                 true,
	"System.Collections.IEnumerator::MoveNext":                    true,
	"System.Collections.IEnumerator::Reset":                       true,
	"System.Collections.Generic.ICollection`1::Add":               true,
	"System.Collections.Generic.ICollection`1::get_Count":         true,
	"System.Collections.ICollection::get_Count":                   true,
	"System.Collections.Generic.IDictionary`2::set_Item":          true,
	"System.Collections.Generic.IDictionary`2::get_Item":          true,
	"System.Collections.Generic.IDictionary`2::TryGetValue":       true,
	"System.Collections.Generic.IDictionary`2::ContainsKey":       true,
	"System.Collections.Generic.IList`1::get_Item":                true,
	"System.Collections.Generic.IList`1::set_Item":                true,
	"System.Collections.Generic.IReadOnlyList`1::get_Item":        true,
	"System.Collections.Generic.IReadOnlyCollection`1::get_Count": true,
	"System.Collections.Generic.IEqualityComparer`1::Equals":      true,
	"System.Collections.Generic.IEqualityComparer`1::GetHashCode": true,
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
		return "not yet implemented — see docs/ROADMAP.md"
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
