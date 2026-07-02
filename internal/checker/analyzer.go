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

			fullName := ir.Qualify(typeDef.Namespace, typeDef.Name) + "::" + row.Name
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
	_, code, err := il.ReadMethodBody(body)
	if err != nil {
		return append(findings, Finding{Kind: KindUnsupportedOpcode, Method: fullName, Detail: fmt.Sprintf("reading method header: %v", err)})
	}
	instrs, err := il.Decode(code)
	if err != nil {
		return append(findings, Finding{Kind: KindUnsupportedOpcode, Method: fullName, Detail: fmt.Sprintf("decoding IL: %v", err)})
	}

	retVoid := sig.RetType.Kind == metadata.SigVoid
	irInstrs, err := ir.Build(instrs, md, retVoid)
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
	return isLocalMethod(md, fullName)
}

func resolvableCtor(md *metadata.Metadata, typeFullName, ctorFullName string) bool {
	if _, ok := bcl.LookupCtor(typeFullName); ok {
		return true
	}
	return isLocalMethod(md, ctorFullName)
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
	case "leave", "leave.s", "endfinally":
		return "try/catch/finally are not supported yet — an unhandled throw is"
	case "newarr", "ldelem", "stelem", "ldlen":
		return "System.Array is not supported yet"
	default:
		return "not yet implemented — see docs/ROADMAP.md"
	}
}

// signatureFindings flags parameter/return shapes vmnet can't execute
// correctly even though the signature itself parses fine: raw unmanaged
// pointers (true `unsafe` code) and by-ref parameters (safe C#, but
// vmnet's interpreter doesn't model write-back semantics yet).
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
	case metadata.SigByRef:
		return []Finding{{Kind: KindByRefParameter, Method: fullName, Detail: where + " is by-ref (ref/out/in)", Suggestion: "vmnet doesn't model by-ref write-back yet"}}
	default:
		return nil
	}
}
