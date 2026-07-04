package vmnet

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/il"
	"github.com/arturoeanton/go-vmnet/internal/ir"
	"github.com/arturoeanton/go-vmnet/internal/metadata"
	"github.com/arturoeanton/go-vmnet/internal/pe"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// Assembly is a loaded .NET assembly, ready to have its methods called.
// Safe for concurrent use: Call/CallBytes/CallJSON may be called from
// multiple goroutines on the same *Assembly (e.g. concurrent requests in a
// Go server embedding vmnet).
type Assembly struct {
	name string
	file *pe.File
	md   *metadata.Metadata

	// deps are other loaded assemblies this one's types/methods can
	// resolve into (Fase 3.27, multi-assembly resolution) — e.g. a small
	// glue assembly whose IL directly references a NuGet package's own
	// types (`new Jint.Engine()`). vmnet loads one assembly's bytes at a
	// time (LoadFile/LoadBytes/LoadPackage); it never walks an
	// assembly's own AssemblyRef table to auto-discover what else needs
	// loading — deps must be attached explicitly via WithDependencies,
	// or automatically by LoadPackage from the NuGet lockfile's already-
	// resolved dependency graph. Checked only after this Assembly's own
	// metadata has nothing under that name — self always wins on a name
	// collision, matching how a real CLR resolves a TypeRef against the
	// specific AssemblyRef the compiler recorded, just coarser (vmnet
	// doesn't disambiguate same-named types across different deps).
	deps []*Assembly

	// globalTypeIndex maps every TypeDef full name declared ANYWHERE in
	// one LoadPackage call's full transitive graph to the specific
	// Assembly that declares it (Fase 3.40) — shared (same map instance)
	// across every assembly LoadPackage loads together. deps only ever
	// resolves "down" a package's own declared dependency edges, which
	// breaks for a shared dependency's own generic method resolving
	// typeof(T) where T is a real type from a package that depends ON
	// that shared dependency, not the other way around (found via a
	// real, load-bearing case: System.Memory's own SpanHelpers.
	// IsReferenceOrContainsReferencesCore checking a real struct
	// declared in SixLabors.Fonts, reached just from ClosedXML's own
	// font-metrics engine). Consulted only as an absolute last resort,
	// after this assembly's own metadata and its own deps chain both
	// fail — and even then it jumps directly to the one specific owning
	// assembly's buildType, never recursing through that assembly's own
	// deps/globalTypeIndex again, so it can never loop no matter how the
	// real dependency graph is shaped. nil for an Assembly loaded via
	// LoadFile/LoadBytes directly (no package graph context at all).
	globalTypeIndex map[string]*Assembly

	cacheMu sync.RWMutex
	// methods is keyed by MethodDef RID, not "Namespace.Type::Method"
	// (Fase 3.27) — a real method can be overloaded (same name, different
	// signature; see pickMethodOverload), so a name alone doesn't
	// identify one specific method the way a RID always does. Every
	// caller already knows the exact RID by the time a *runtime.Method
	// needs building or caching (overload resolution happens first).
	methods map[uint32]*runtime.Method
	types   map[string]*runtime.Type // keyed by "Namespace.Type"

	// explicitImpls memoizes resolveExplicitImpl's own result (Fase
	// 3.45) — keyed by "concreteType\x00interfaceType\x00methodName".
	// Found via a real, load-bearing perf case: DocumentFormat.OpenXml's
	// own FeatureCollectionBase.Get<TFeature>() (features.go) calls
	// through IPackageInitializer/IMainPartFeature-style explicit-impl
	// dispatch on every single Get<T>() across every part opened, and
	// resolveExplicitImpl's own per-ancestor resolveExplicitImplExact
	// step does a fresh linear FindTypeDef scan of the metadata TypeDef
	// table every time (metadata/resolver.go) — with DocumentFormat.
	// OpenXml.dll's own thousands of TypeDefs, repeating that scan for
	// every Get<TFeature>() call while opening a real (even small) .xlsx
	// compounds into a multi-minute hang instead of the sub-second real
	// answer this deterministic, metadata-only resolution always
	// produces for the same three inputs. explicitImpls is safe to share
	// across concurrent Machines the same way methods/types already are
	// (cacheMu): resolveExplicitImpl is a pure function of the loaded
	// metadata, which never changes after LoadBytes/LoadFile returns.
	explicitImpls map[string]explicitImplResult
}

// explicitImplResult is resolveExplicitImpl's cached return value —
// Go's map can't cache a (string, bool) pair as a "found vs. not found"
// zero-value-ambiguous single string, so the miss case (ok == false) is
// cached explicitly too (a real miss re-walking the same expensive ancestor
// chain on every call would defeat the whole cache).
type explicitImplResult struct {
	name string
	ok   bool
}

// WithDependencies attaches other loaded assemblies asm can resolve
// types/methods into (Fase 3.27) — see the deps field's doc comment.
// Returns asm for chaining (`vm.LoadFile(...).WithDependencies(...)`),
// same style as interpreter.Machine's WithExplicitImplResolver/
// WithEnumResolver.
//
// asm also JOINS its deps' shared cross-package type index, when one
// exists (Fase 3.43): deps edges only resolve "down" (asm's own IL naming
// a dep's types), but a real library routinely calls BACK into a type its
// caller registered with it — found via a real, load-bearing case:
// examples/closedxml-demo's wrapper assembly (loaded via LoadBytes,
// depending on the LoadPackage-loaded ClosedXML graph) sets
// `LoadOptions.DefaultGraphicEngine = new NullGraphicEngine()`
// (GraphicEngineWrapper.cs:70), and ClosedXML's own real column-width
// code later dispatches `workbook.GraphicEngine.GetMaxDigitWidth(...)`
// (decompiled ClosedXML.Excel/XLColumn.cs:183) on it. That callvirt runs
// with ClosedXML's resolvers active, and "VmnetClosedXmlDemo.
// NullGraphicEngine" exists neither in ClosedXML's own metadata nor
// anywhere down its deps — exactly the reverse-edge shape globalTypeIndex
// (see its doc comment) already exists to cover within one LoadPackage
// graph. Indexing asm's own TypeDefs into that same shared map (same
// instance, so every already-loaded assembly in the graph sees them
// immediately) extends the identical last-resort mechanism across the
// LoadBytes/LoadPackage boundary; when no dep carries an index at all
// (plain LoadFile/LoadBytes on both sides), nothing changes.
func (asm *Assembly) WithDependencies(deps ...*Assembly) *Assembly {
	asm.deps = append(asm.deps, deps...)
	index := asm.globalTypeIndex
	if index == nil {
		for _, dep := range deps {
			if dep.globalTypeIndex != nil {
				index = dep.globalTypeIndex
				break
			}
		}
	}
	if index != nil {
		asm.indexOwnTypesInto(index)
		asm.globalTypeIndex = index
	}
	return asm
}

func (asm *Assembly) cachedMethodByRID(methodRID uint32) (*runtime.Method, bool) {
	asm.cacheMu.RLock()
	defer asm.cacheMu.RUnlock()
	m, ok := asm.methods[methodRID]
	return m, ok
}

func (asm *Assembly) storeMethodByRID(methodRID uint32, m *runtime.Method) {
	asm.cacheMu.Lock()
	defer asm.cacheMu.Unlock()
	asm.methods[methodRID] = m
}

// Name returns the name Assembly was loaded with (the file's base name for
// LoadFile, or the caller-supplied name for LoadBytes).
func (asm *Assembly) Name() string { return asm.name }

func (asm *Assembly) resolveMethod(typeName, methodName string, args []runtime.Value) (*runtime.Method, error) {
	namespace, name := splitTypeName(typeName)
	typeRID, _, err := asm.md.FindTypeDef(namespace, name)
	if err != nil {
		return asm.resolveMethodInDeps(typeName, methodName, args, notFoundErr(typeName, methodName, err))
	}
	// A host-driven Assembly.Call/CallBytes call site (the only caller of
	// resolveMethod, see call.go) never carries generic-method-argument
	// information — genericArgCount is always 0, matching a plain,
	// non-generic method (the overwhelming majority of host-driven
	// calls; a generic-method target from Go isn't supported here yet).
	methodRID, row, err := asm.pickMethodOverload(typeRID, methodName, args, nil, 0)
	if err != nil {
		return asm.resolveMethodInDeps(typeName, methodName, args, notFoundErr(typeName, methodName, err))
	}
	return asm.buildMethod(methodRID, row)
}

// notFoundErr wraps a genuine "no such type/method by that name" error
// with runtime.ErrMethodNotFound (Fase 3.27) — see that sentinel's doc
// comment for why the distinction from "found, but failed to build"
// matters. Only ever called at the two places in this file where the
// underlying failure really is a bare name lookup miss (FindTypeDef,
// FindMethodDefCandidates via pickMethodOverload) — never wraps a
// buildMethod error, which must stay a normal, unwrapped error so it
// propagates as the real problem it is.
func notFoundErr(typeName, methodName string, cause error) error {
	return fmt.Errorf("%s::%s: %w: %v", typeName, methodName, runtime.ErrMethodNotFound, cause)
}

func (asm *Assembly) resolveMethodInDeps(typeName, methodName string, args []runtime.Value, notFound error) (*runtime.Method, error) {
	lastErr := notFound
	for _, dep := range asm.deps {
		m, err := dep.resolveMethod(typeName, methodName, args)
		if err == nil {
			return m, nil
		}
		// A dep's error is usually more specific than this Assembly's own
		// "not found at all" (e.g. a real build failure deep inside that
		// dependency's own method body) — surfacing it, not the generic
		// outer error, is what actually let real bugs get diagnosed
		// running Jint (Fase 3.27): the alternative silently reports
		// "unsupported BCL method" for the wrapper's OWN call target no
		// matter how deep or specific the real failure actually was.
		lastErr = err
	}
	return nil, lastErr
}

// resolveByFullName implements interpreter.Resolver for local (non-BCL)
// calls discovered while executing another method's IR. args (the
// actual call-site arguments, receiver included for an instance call)
// disambiguate a real overload set — see pickMethodOverload.
func (asm *Assembly) resolveByFullName(fullName string, args []runtime.Value, paramTypeNames []string, genericArgCount int) (*runtime.Method, error) {
	namespace, typeName, methodName, err := splitFullName(fullName)
	if err != nil {
		return nil, err
	}
	fullTypeName := qualify(namespace, typeName)
	typeRID, _, err := asm.md.FindTypeDef(namespace, typeName)
	if err != nil {
		if m, ok := asm.resolveByFullNameCrossPackage(fullTypeName, methodName, args, paramTypeNames, genericArgCount); ok {
			return m, nil
		}
		return asm.resolveByFullNameInDeps(fullName, args, paramTypeNames, genericArgCount, notFoundErr(typeName, methodName, err))
	}
	methodRID, row, err := asm.pickMethodOverload(typeRID, methodName, args, paramTypeNames, genericArgCount)
	if err != nil {
		if m, ok := asm.resolveByFullNameCrossPackage(fullTypeName, methodName, args, paramTypeNames, genericArgCount); ok {
			return m, nil
		}
		return asm.resolveByFullNameInDeps(fullName, args, paramTypeNames, genericArgCount, notFoundErr(typeName, methodName, err))
	}
	return asm.buildMethod(methodRID, row)
}

// resolveByFullNameCrossPackage is the same cross-package last resort
// globalTypeIndex gives type resolution (Fase 3.40, see that field's own
// doc comment) — a shared dependency's own method calling into a type
// declared by one of ITS dependents, not the other way around (found
// via a real, load-bearing case: DocumentFormat.OpenXml.Framework's own
// OpenXmlPackageBuilder<T>.BuildPipeline calls the abstract Clone()
// method, which is overridden on a private nested class declared in the
// main DocumentFormat.OpenXml assembly — a real dependent of Framework,
// never reachable through Framework's own tree-shaped deps list). Jumps
// directly to the owning assembly's own pickMethodOverload/buildMethod,
// never back through resolveByFullName/deps, so this can never recurse
// regardless of how the real dependency graph is shaped.
func (asm *Assembly) resolveByFullNameCrossPackage(fullTypeName, methodName string, args []runtime.Value, paramTypeNames []string, genericArgCount int) (*runtime.Method, bool) {
	owner, ok := asm.globalTypeIndex[fullTypeName]
	if !ok || owner == asm {
		return nil, false
	}
	ownerTypeRID, _, err := owner.md.FindTypeDef(splitTypeName(fullTypeName))
	if err != nil {
		return nil, false
	}
	methodRID, row, err := owner.pickMethodOverload(ownerTypeRID, methodName, args, paramTypeNames, genericArgCount)
	if err != nil {
		return nil, false
	}
	m, err := owner.buildMethod(methodRID, row)
	if err != nil {
		return nil, false
	}
	return m, true
}

// resolveByFullNameInDeps is the multi-assembly fallback (Fase 3.27):
// tried only after this Assembly's own metadata has nothing under
// fullName — e.g. a glue assembly's `newobj Jint.Engine::.ctor` call
// target, which doesn't exist in the glue assembly's own TypeDef table
// at all (it's a TypeRef into a dependency the compiler recorded, but
// vmnet's IR layer never tracked which AssemblyRef a TypeRef pointed at
// — see qualifyTypeRefName — so by the time a full name like
// "Jint.Engine::.ctor" reaches here, the assembly boundary is already
// gone; this is what puts it back, by simply trying every attached dep
// in turn).
func (asm *Assembly) resolveByFullNameInDeps(fullName string, args []runtime.Value, paramTypeNames []string, genericArgCount int, notFound error) (*runtime.Method, error) {
	lastErr := notFound
	for _, dep := range asm.deps {
		m, err := dep.resolveByFullName(fullName, args, paramTypeNames, genericArgCount)
		if err == nil {
			return m, nil
		}
		// See resolveMethodInDeps's identical comment: a dep's own error
		// is more diagnostic than this Assembly's generic "not found."
		lastErr = err
	}
	return nil, lastErr
}

// candidateMatchesArgs checks a single method candidate (no sibling
// overloads to score against) against the real call-site args before
// pickMethodOverload trusts it — see that function's "len(rids) == 1"
// comment for why this validation is needed at all.
func (asm *Assembly) candidateMatchesArgs(row metadata.MethodDefRow, args []runtime.Value, genericArgCount int) (metadata.MethodDefRow, bool) {
	sig, err := metadata.ParseMethodSig(row.Signature)
	if err != nil {
		return row, false
	}
	// No generic-arity check here, deliberately, unlike the tie-break
	// loop below: a single same-named candidate has nothing to
	// disambiguate FROM, so it's always the right one regardless of its
	// own generic arity — genericArgCount is 0 for every host-driven
	// Instance.Call/Assembly.Call (Fase 3.28's public API has no way to
	// name a specific generic instantiation at all), which would
	// otherwise wrongly reject the sole real candidate for a generic-only
	// member like OpenXmlCompositeElement's own `AppendChild<T>(T)` —
	// found via a real regression while adding the tie-break loop's own
	// GenParamCount filter (Fase 3.41).
	declared := args
	if sig.HasThis && len(declared) > 0 {
		declared = declared[1:]
	}
	if len(declared) != len(sig.Params) {
		return row, false
	}
	if asm.hasHardShapeMismatch(sig.Params, declared) {
		return row, false
	}
	return row, true
}

// hasHardShapeMismatch reports whether any parameter/argument pair is a
// shape combination real CIL can never produce at a call site without an
// explicit, separately-visible conversion — i.e. a combination
// scoreParamMatch's coarse positive-but-low fallback score would
// otherwise let slip through as "plausible." Currently just the one
// combination found causing real damage (Fase 3.27): a reference value
// (KindObject — always some heap-allocated class instance) can never
// directly back a value-type parameter (SigValueType — a struct, always
// passed by value) the way `Key key` is declared; real IL always emits a
// visible conversion call (an operator overload, a field read, ...)
// before such a call site, never an implicit reference-to-struct
// coercion. Found running real Jint: GlobalObject's own non-virtual
// `GetOwnProperty(Key)` fast-path overload has the same name and arity
// as the real virtual `GetOwnProperty(JsValue)` it doesn't override —
// without this check, a JsValue argument would silently "match" the
// Key-typed candidate at a low-but-positive score instead of being
// rejected outright.
func (asm *Assembly) hasHardShapeMismatch(params []metadata.SigType, args []runtime.Value) bool {
	for i, p := range params {
		if args[i].Kind == runtime.KindObject && p.Kind == metadata.SigValueType {
			return true
		}
		// A real array argument (KindArray) is never a delegate: vmnet's
		// own KindFunc is the only shape a real delegate value ever
		// takes (runtime.Func's doc comment). A class-typed parameter
		// (SigClass) whose real declared type turns out to be a delegate
		// (BaseTypeFullName == System.MulticastDelegate) can therefore
		// never legitimately bind an array argument — found the hard way
		// (Fase 3.40): System.IO.Packaging.InternalRelationshipCollection
		// calls `new XmlCompatibilityReader(reader, string[])`, which has
		// two same-arity 2nd-parameter overloads (one
		// IsXmlNamespaceSupportedCallback, one IEnumerable<string>) —
		// without this, the array argument scored as a plausible match
		// for the delegate-typed overload too, picking it at random
		// instead of the only real match.
		if args[i].Kind == runtime.KindArray && p.Kind == metadata.SigClass {
			if name, ok := paramTypeName(asm.md, p); ok {
				if t, err := asm.resolveTypeByFullName(name); err == nil && t != nil && t.BaseTypeFullName == "System.MulticastDelegate" {
					return true
				}
			}
		}
		// A class-instance reference (KindObject) can never back an
		// SZARRAY parameter either — arrays and class objects are
		// distinct reference shapes in real CIL, and no implicit
		// conversion between them exists (Fase 3.45). Found running real
		// Newtonsoft.Json 13.0.3: JContainer.InsertItem's `ValidateToken
		// (item, null)` — a callvirt to the virtual, 2-JToken-param
		// `JContainer::ValidateToken(JToken,JToken)` (JContainer.cs:535,
		// /tmp/nj_ns20/Newtonsoft.Json.Linq/JContainer.cs) — is NOT
		// overridden by JProperty, so Machine.call's ancestor walk
		// (internal/interpreter/calls.go's virtual-dispatch loop) tries
		// every base class's own "ValidateToken" by name, including
		// JToken, whose ONLY method of that name is the unrelated private
		// static `ValidateToken(JToken o, JTokenType[] validTypes, bool
		// nullable)` (JToken.cs:718). That candidate's arity (3)
		// coincidentally equals the call site's own popped-arg count
		// (receiver + item + null, since HasThis was true at the call
		// site), so pickMethodOverload's single-candidate path
		// (len(rids)==1) accepted it: a JToken/JValue argument (KindObject)
		// silently "matched" the JTokenType[] validTypes parameter with no
		// mismatch flagged, and ValidateToken ran with its own args
		// misread as (o=the JProperty receiver, validTypes=the real item
		// object, nullable=null) — corrupting the args Array.IndexOf(
		// validTypes, o.Type) then received (bcl: Array.IndexOf expects
		// (T[], T[, startIndex[, count]])). Rejecting the mismatch here
		// makes pickMethodOverload correctly report "not found" for this
		// wrong JToken candidate, so calls.go's chain walk keeps going
		// past it to the real fallback: the exact declared-type name
		// "JContainer::ValidateToken", which resolves the correct,
		// signature-matching virtual method.
		if args[i].Kind == runtime.KindObject && p.Kind == metadata.SigSZArray {
			return true
		}
		// A numeric primitive (KindI4/I8/R4/R8) can never legitimately
		// back a System.String parameter either — vmnet always
		// represents a real string as KindString (Fase 1), never as any
		// numeric Kind, and CIL has no implicit int/float-to-string
		// coercion (Fase 3.46). Found running real Newtonsoft.Json
		// 13.0.3: JObject's real ChildrenTokens (`_properties`, an
		// IList<JToken>) is indexed by position in several places (e.g.
		// JContainer.InsertItem's own `childrenTokens[index]`,
		// JContainer.cs:533-534, /tmp/nj_ns20/Newtonsoft.Json.Linq/
		// JContainer.cs) via the inherited `Collection<JToken>.get_Item
		// (int)` (natively modeled — system_collection_objectmodel.go's
		// listGetItem). JPropertyKeyedCollection (the real backing type,
		// itself a `Collection<JToken>` subclass) separately declares its
		// OWN, unrelated `this[string key]` indexer (JPropertyKeyedCollection.
		// cs:15) — same compiled method name "get_Item", totally
		// different parameter type. Machine.call's ancestor walk (calls.
		// go) tries JPropertyKeyedCollection's own "get_Item" first (by
		// name, from the concrete receiver type) and, same bug shape as
		// the ValidateToken case just above, pickMethodOverload's single-
		// candidate path accepted an int index argument against the
		// string-typed candidate with no shape check catching it —
		// running the real string indexer with key=<the int reinterpreted
		// as a non-string, non-null Value>, which "if (key == null)"
		// (JPropertyKeyedCollection.cs:19) then threw ArgumentNullException
		// on. Rejecting the mismatch here makes the ancestor walk correctly
		// fall through past this wrong candidate to Collection`1::get_Item,
		// the real match.
		if p.Kind == metadata.SigString {
			switch args[i].Kind {
			case runtime.KindI4, runtime.KindI8, runtime.KindR4, runtime.KindR8:
				return true
			}
		}
	}
	return false
}

// pickMethodOverload disambiguates a real overload set (Fase 3.27) —
// discovered the hard way running Jint's actual Engine class, which has
// 5 constructors and 9 SetValue overloads: FindMethodDef alone always
// returns whichever same-named method happens to come first in the
// metadata table, regardless of arity. A real IL call site never has
// this problem (its operand is an exact MethodDef/MemberRef token,
// naming one specific overload) — the ambiguity is purely a side effect
// of vmnet collapsing every call target to a "Namespace.Type::Method"
// string for its own Resolver/checker/BCL-registry machinery, which
// loses that precision. args (the actual runtime call-site arguments,
// receiver included for an instance call) recovers it approximately:
// arity is a hard filter (a real overload set's candidates always differ
// in arity OR in per-parameter types, so this alone resolves most real
// cases, including every one of Engine's 5 constructors), and
// scoreParamMatch breaks same-arity ties by how well each parameter's
// declared type matches the actual argument's runtime Kind. This is a
// heuristic, not full C# overload resolution (no real type identity
// comparison, since vmnet's Value model doesn't carry one) — but it is
// unconditionally better than "first match by name," which is wrong
// every time there's more than one candidate.
func (asm *Assembly) pickMethodOverload(typeRID uint32, methodName string, args []runtime.Value, paramTypeNames []string, genericArgCount int) (uint32, metadata.MethodDefRow, error) {
	rids, rows, err := asm.md.FindMethodDefCandidates(typeRID, methodName)
	if err != nil {
		return 0, metadata.MethodDefRow{}, err
	}
	if len(rids) == 1 {
		// A single same-named candidate isn't automatically the right one
		// — Machine.call's virtual-dispatch chain walk (calls.go, Fase
		// 3.27) retries a callvirt's method name against every ancestor
		// of the receiver's concrete type, and a class can perfectly
		// legally declare an unrelated NON-virtual method with the exact
		// same name and arity as a virtual one it inherits (ordinary C#
		// overloading, not overriding). Found running real Jint:
		// GlobalObject declares its own non-virtual `GetOwnProperty(Key
		// property)` fast-path lookup alongside (but not overriding)
		// ObjectInstance's virtual `GetOwnProperty(JsValue property)` —
		// same name, same arity (1), so the old unconditional "only one
		// candidate, must be it" trust picked GlobalObject's Key-typed
		// overload for a callvirt whose actual argument was a JsValue,
		// silently corrupting Jint's own property lookups instead of
		// correctly falling through to the real virtual method further up
		// the chain. Reject it exactly like the tie-breaking loop below
		// would (arity mismatch, or a confirmed hard shape mismatch) so
		// the chain walk sees "not found here" and keeps looking; every
		// genuinely-single-overload call (the overwhelming majority) is
		// unaffected, since a real single overload's own arity/shape
		// always does match its own call sites' real arguments.
		if row, ok := asm.candidateMatchesArgs(rows[0], args, genericArgCount); ok {
			return rids[0], row, nil
		}
		return 0, metadata.MethodDefRow{}, fmt.Errorf("metadata: %q candidate's signature doesn't match the call site's %d argument(s)", methodName, len(args))
	}
	bestIdx := -1
	bestScore := math.MinInt // any candidate whose arity matches must win over "no candidate found" (bestIdx == -1) — a real match's total score can go negative (e.g. a confirmed type-name mismatch's -3 penalty with no compensating positive), which a 0-ish starting threshold would incorrectly treat as "worse than nothing" and silently fall through to the arity-mismatch fallback (rids[0]) instead (found the hard way: this exact bug re-triggered the same infinite-.ctor-recursion class as the original overload-resolution fix, on Jint's real PropertyDescriptor, which has both a 1-arg struct-typed ctor and a 1-arg self-typed copy ctor).
	for i, row := range rows {
		sig, err := metadata.ParseMethodSig(row.Signature)
		if err != nil {
			continue
		}
		if sig.GenParamCount != uint32(genericArgCount) {
			// Same hard generic-arity filter as candidateMatchesArgs'
			// identical check above — a same-named, same-real-arity
			// plain/generic method pair (Descendants()/Descendants<T>())
			// must never tie here either.
			continue
		}
		declared := args
		if sig.HasThis && len(declared) > 0 {
			declared = declared[1:]
		}
		if len(declared) != len(sig.Params) {
			continue
		}
		if asm.hasHardShapeMismatch(sig.Params, declared) {
			continue
		}
		score := 0
		for j, p := range sig.Params {
			score += scoreParamMatch(p.Kind, declared[j].Kind)
			// Exact type-name match refinement: scoreParamMatch alone
			// treats every reference-typed parameter alike (any KindObject
			// arg scores the same against ANY SigClass param, since vmnet's
			// Value model doesn't carry real type identity at the Kind
			// level) — found insufficient running real Jint, whose
			// Function::.ctor chain has multiple same-arity, same-Kind-
			// shaped overloads (one takes a JintFunctionDefinition, another
			// a JsString) that the coarse score alone can't tell apart.
			// When the declared parameter's own type name IS resolvable
			// (a SigClass/SigValueType/SigGenericInst token) and the
			// actual argument's real type name is too, an exact match is
			// worth far more than any coarse Kind-only score.
			// Call-site exact match (Fase 3.40): the ORIGINAL call site's
			// own compile-time-resolved parameter type name, if the
			// caller has one at all (ir.Call.ParamTypeNames — nil for
			// callers with no Call-level IR context, e.g. Machine.New).
			// This is strictly more reliable than runtimeValueTypeName
			// below for exactly the case that scoring alone can't
			// resolve: a bool argument and any enum member both collapse
			// to the same KindI4 shape, so runtimeValueTypeName(declared[j])
			// can only ever report "System.Int32" for either — tying two
			// overloads that differ only in "bool" vs "SomeEnum" forever.
			// A confirmed call-site match short-circuits the whole
			// per-parameter score with a bonus no coarse-Kind or subtype
			// match could ever match or beat.
			if j < len(paramTypeNames) && paramTypeNames[j] != "" {
				if name, ok := paramTypeName(asm.md, p); ok && name == paramTypeNames[j] {
					score += 1000
					continue
				}
			}
			if name, ok := paramTypeName(asm.md, p); ok {
				if actual, ok := runtimeValueTypeName(declared[j]); ok {
					switch {
					case actual == name:
						score += 50
					case asm.valueIsAssignableToTypeName(declared[j], name):
						// The argument's concrete type is a subclass of the
						// declared parameter type (e.g. a JsNumber argument
						// against a JsValue-typed parameter) — not an exact
						// match, but still a real, specific match, and
						// strictly more specific than an unconstrained
						// `object` parameter (which scores no bonus/penalty
						// at all here, since paramTypeName only resolves
						// SigClass/SigValueType/SigGenericInst, not
						// SigObject). Found the hard way running real Jint:
						// without this, Engine.SetValue(string, JsValue)
						// lost the overload pick to SetValue(string,
						// object) for every already-converted JsValue
						// argument (the -3 mismatch penalty below made the
						// more-specific overload score lower than the
						// generic one), so the object overload's own
						// fallback path — convert via FromObject, then
						// call SetValue again — looped forever on a value
						// that was already fully converted.
						score += 20
					default:
						// A confirmed mismatch (both names resolved, and
						// differ, and the argument isn't a subtype of the
						// declared parameter type either) — a small
						// penalty, not disqualifying: still less certain
						// than an outright arity mismatch, since e.g. an
						// interface/base-class parameter legitimately
						// never matches its concrete argument's own type
						// name.
						score -= 3
					}
				}
			}
		}
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}
	if bestIdx < 0 {
		// No candidate's arity matched the call site at all — fall back
		// to the first (old, name-only) behavior rather than hard-
		// failing; better to try something plausible than refuse
		// outright (e.g. a param-kind vmnet's Value model can't score at
		// all, or a caller that legitimately doesn't have real args to
		// disambiguate with, like resolveEnumMembers' internal lookups).
		return rids[0], rows[0], nil
	}
	return rids[bestIdx], rows[bestIdx], nil
}

// valueIsAssignableToTypeName reports whether v's concrete runtime type is
// targetName itself or a subclass of it, walking v.Obj.Type's own
// BaseTypeFullName chain — used by pickMethodOverload's exact-match
// refinement to give a real (if partial) score bonus to a subtype match,
// not just an identical-name one (Fase 3.27). Only classes are walked
// (structs/primitives already get their own exact-name-or-nothing
// treatment in the caller — a value type's inheritance is fixed by the
// CLR to System.ValueType, never a user base class).
func (asm *Assembly) valueIsAssignableToTypeName(v runtime.Value, targetName string) bool {
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	if v.Kind != runtime.KindObject || v.Obj == nil {
		return false
	}
	if v.Obj.Type == nil {
		// A native-backed object (no TypeDef) has no BaseTypeFullName
		// chain to walk — fall back to bcl's own small hand-maintained
		// native-base-type table (Fase 3.39: MemoryStream IS-A Stream).
		// See NativeBaseTypeName's doc comment for the real bug this
		// closes (a same-arity overload set over unrelated reference
		// types silently picking the wrong one for a native argument).
		name, ok := bcl.NativeTypeName(v.Obj.Native)
		if !ok {
			return false
		}
		for {
			if name == targetName {
				return true
			}
			base, ok := bcl.NativeBaseTypeName(name)
			if !ok {
				return false
			}
			name = base
		}
	}
	for t := v.Obj.Type; t != nil; {
		if qualifiedOrPlainName(t) == targetName {
			return true
		}
		// A base class's OR the concrete type's own directly-implemented
		// interfaces (Fase 3.39) — not just the class chain. Found via a
		// real, load-bearing overload-resolution bug: NPOI's own
		// AreaPtg(ILittleEndianInput) / AreaPtg(AreaReference) same-arity
		// constructor pair (reading a Ptg's binary token data vs building
		// one from a resolved reference) — a real LittleEndianByteArray
		// InputStream argument was previously never recognized as
		// assignable to ILittleEndianInput (this loop only ever walked
		// BaseTypeFullName), so it silently scored no better than the
		// unrelated AreaReference-typed overload and picked whichever
		// tied first — constructing a genuinely broken AreaPtg whose
		// "AreaReference" field was actually the input stream.
		for _, iface := range t.Interfaces {
			if iface == targetName {
				return true
			}
		}
		if t.BaseTypeFullName == "" {
			return false
		}
		next, err := asm.resolveTypeByFullName(t.BaseTypeFullName)
		if err != nil {
			return false
		}
		t = next
	}
	return false
}

// paramTypeName resolves a declared parameter's own type name, if it has
// one (SigClass/SigValueType carry a Token directly; SigGenericInst's
// Token names its open generic type) — used by pickMethodOverload's
// exact-match refinement.
//
// This must go through ir.SigTypeFullName rather than the bare
// resolveTypeTokenName(md, p.Token) it used to call directly: for
// SigGenericInst, resolveTypeTokenName only ever resolves the OPEN
// generic type (e.g. "System.ReadOnlyMemory`1"), discarding p.Args
// entirely — so two overloads differing only in a generic argument
// (Parse(ReadOnlyMemory<byte>, ...) vs Parse(ReadOnlyMemory<char>, ...),
// the real System.Text.Json.JsonDocument::Parse overload set) produced
// the exact same candidate name and could never be told apart by the
// exact-match refinement below. ir.SigTypeFullName keeps the closed type
// arguments ("System.ReadOnlyMemory`1[[System.Byte]]" vs
// "...[[System.Char]]") and is also what sigParamTypeNames
// (internal/ir/builder.go) now uses to build the call site's own
// paramTypeNames — both sides of the j < len(paramTypeNames) comparison
// below must use the same naming scheme or the exact-match bonus can
// never fire at all.
func paramTypeName(md *metadata.Metadata, p metadata.SigType) (string, bool) {
	switch p.Kind {
	case metadata.SigClass, metadata.SigValueType, metadata.SigGenericInst:
		name, err := ir.SigTypeFullName(md, p)
		if err != nil {
			return "", false
		}
		return name, true
	default:
		return "", false
	}
}

// runtimeValueTypeName returns v's real runtime type name, if
// determinable — same information receiverTypeName
// (internal/interpreter/typecheck.go) extracts for the interface-
// dispatch fallback, reimplemented here since assembly.go (root
// package) can't reach that unexported function directly. Used only by
// pickMethodOverload's exact-match refinement; a "no" here just means
// the coarse Kind-based score in scoreParamMatch is all that's
// available for this argument, not an error.
func runtimeValueTypeName(v runtime.Value) (string, bool) {
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	switch v.Kind {
	case runtime.KindString:
		return "System.String", true
	case runtime.KindI4:
		return "System.Int32", true
	case runtime.KindI8:
		return "System.Int64", true
	case runtime.KindR4:
		return "System.Single", true
	case runtime.KindR8:
		return "System.Double", true
	case runtime.KindFunc:
		// A delegate's own declared type (Fase 3.40, DelegateTypeName —
		// see runtime.Func's own doc comment for why this needs its own
		// case: every delegate collapses to the same KindFunc shape
		// regardless of type, which previously made two different
		// delegate-typed overload parameters indistinguishable here).
		if v.Func == nil || v.Func.DelegateTypeName == "" {
			return "", false
		}
		return v.Func.DelegateTypeName, true
	case runtime.KindStruct:
		if v.Struct == nil || v.Struct.Type == nil {
			return "", false
		}
		return qualifiedOrPlainName(v.Struct.Type), true
	case runtime.KindObject:
		if v.Obj == nil {
			return "", false
		}
		if v.Obj.Type != nil {
			return qualifiedOrPlainName(v.Obj.Type), true
		}
		if ex, ok := v.Obj.Native.(*runtime.ManagedException); ok {
			return ex.TypeName, true
		}
		if name, ok := bcl.NativeTypeName(v.Obj.Native); ok {
			return name, true
		}
		if name, ok := bcl.TypeFullNameOf(v); ok {
			return name, true
		}
		return "", false
	default:
		return "", false
	}
}

func qualifiedOrPlainName(t *runtime.Type) string {
	if t.QualifiedName != "" {
		return t.QualifiedName
	}
	if t.Namespace == "" {
		return t.Name
	}
	return t.Namespace + "." + t.Name
}

// scoreParamMatch rates how well a declared parameter type (from a
// method signature) matches an actual argument's runtime Kind — higher
// is a better match. Not a real type system: vmnet's Value model only
// carries a coarse Kind (KindI4 covers int32/bool/char/short/byte alike,
// same documented simplification isAssignableTo's KindI4 branch has had
// since Fase 3.8), so this can only approximate real C# overload
// resolution, not reproduce it exactly.
func scoreParamMatch(sigKind metadata.SigTypeKind, argKind runtime.Kind) int {
	switch argKind {
	case runtime.KindI4:
		switch sigKind {
		case metadata.SigI4:
			return 10
		case metadata.SigBoolean, metadata.SigChar, metadata.SigI1, metadata.SigU1, metadata.SigI2, metadata.SigU2, metadata.SigU4:
			return 8
		case metadata.SigI8, metadata.SigU8, metadata.SigR4, metadata.SigR8:
			return 5
		case metadata.SigValueType:
			// A real C# enum's underlying storage is int32 by default
			// (spec §I.8.5.2) — vmnet has no separate "this int32 is
			// really an enum" Kind (Fase 3.7 doesn't distinguish), so a
			// struct/enum-typed parameter receiving a KindI4 argument is
			// very plausibly a legitimate enum match, not a real
			// mismatch — moderate, not high, confidence (a genuinely
			// non-enum struct parameter can't take a bare int at all in
			// real C#, so this errs toward "probably an enum").
			return 4
		case metadata.SigObject:
			return 2
		}
	case runtime.KindI8:
		switch sigKind {
		case metadata.SigI8, metadata.SigU8:
			return 10
		case metadata.SigR4, metadata.SigR8:
			return 5
		case metadata.SigObject:
			return 2
		}
	case runtime.KindR4:
		switch sigKind {
		case metadata.SigR4:
			return 10
		case metadata.SigR8:
			return 8
		case metadata.SigObject:
			return 2
		}
	case runtime.KindR8:
		switch sigKind {
		case metadata.SigR8:
			return 10
		case metadata.SigR4:
			return 6
		case metadata.SigObject:
			return 2
		}
	case runtime.KindString:
		switch sigKind {
		case metadata.SigString:
			return 10
		case metadata.SigObject, metadata.SigClass:
			return 3
		}
	case runtime.KindNull:
		switch sigKind {
		// A null argument carries no runtime type at all — there's no
		// signal left to recover which overload the original call site's
		// (now-lost) static type actually meant. Real C# overload
		// resolution still disambiguates here at compile time by
		// preferring the more specific/derived applicable type over
		// System.Object (found the hard way: Jint's real compiler-
		// generated record Equals(object) calling Equals(other-as-T) with
		// a genuinely null argument was scoring System.Object's
		// Equals(object) and Equals(T)'s exact overload identically,
		// tie-breaking toward Equals(object) and recursing into itself
		// forever) — a specific reference type outscores System.Object
		// for exactly that reason.
		case metadata.SigClass, metadata.SigString, metadata.SigSZArray, metadata.SigGenericInst:
			return 5
		case metadata.SigObject:
			return 3
		}
	case runtime.KindRef:
		// A real managed pointer — the only Value shape that can
		// legitimately back a byref parameter (ref/in/out T). Scored on
		// its own, separately from KindObject/KindFunc below: found the
		// hard way running real Esprima (Fase 3.27), where two
		// same-arity, same-name overloads of ChildNodes.Enumerator's
		// generic MoveNext helper differ only in whether the one
		// declared parameter is byref (`in NodeList<T> list`, the real
		// intended target when the call site passes a ref-returning
		// property's result) or a plain reference (`Node? node`, an
		// unrelated single-child overload). Before this case existed,
		// KindRef fell into the KindObject/KindFunc branch below and
		// scored a KindRef arg *higher* against SigClass (5, via the
		// shared branch) than against the correct SigByRef (1, via that
		// branch's default) — SigByRef isn't listed there at all — so
		// the wrong overload won and returned the whole NodeList byref
		// as if it were a single Node.
		switch sigKind {
		case metadata.SigByRef:
			return 10
		default:
			return 1
		}
	case runtime.KindObject, runtime.KindFunc:
		switch sigKind {
		case metadata.SigClass, metadata.SigObject, metadata.SigGenericInst:
			return 5
		default:
			return 1
		}
	case runtime.KindStruct:
		switch sigKind {
		case metadata.SigValueType, metadata.SigGenericInst:
			return 5
		default:
			return 1
		}
	case runtime.KindArray, runtime.KindBytes:
		switch sigKind {
		case metadata.SigSZArray:
			return 10
		case metadata.SigObject:
			return 2
		}
	}
	return 0
}

// resolveExplicitImpl implements interpreter.ExplicitImplResolver (Fase
// 3.13): given a concrete type ("Namespace.Type", already known at the
// call site to be the receiver's real runtime type — see
// receiverTypeName in internal/interpreter/typecheck.go) and an
// interface method it was actually called through
// (interfaceFullName+methodName, e.g.
// "System.Collections.Generic.IEnumerable`1"+"GetEnumerator"), finds the
// real method name that implements it, if the class implements that
// interface method *explicitly* — a mangled name like
// "System.Collections.Generic.IEnumerable<System.Int32>.GetEnumerator"
// rather than a plain "GetEnumerator", which is exactly what the C#
// compiler emits for a `yield return` iterator's state machine (it needs
// both the generic and non-generic GetEnumerator/Current, which can't
// both be a same-named method). Ordinary (non-explicit) interface
// implementations need no help here — plain isLocalMethod/Resolve by
// concrete-type-plus-method-name already finds those directly.
// resolveExplicitImpl walks from concreteTypeFullName up through its own
// BaseTypeFullName chain (Fase 3.40), not just the exact concrete type:
// found via a real, load-bearing case, DocumentFormat.OpenXml.Framework's
// own `void IPackageInitializer.Initialize(OpenXmlPackage package)`,
// explicitly implemented on the ABSTRACT PackageFeatureBase rather than
// any of its concrete leaf subclasses (StreamPackageFeature, ...) —
// interface dispatch resolves against the receiver's most-derived type,
// but the explicit MethodImpl entry itself can live on any ancestor, the
// same way a plain (non-derived) override can.
//
// The returned name is already fully qualified as "<declaringType>::
// <mangledMethod>" (declaringType being whichever ancestor's own
// MethodImpl table actually matched, not necessarily concreteTypeFullName
// itself) — ExplicitImplResolver's caller (Machine.call) used to combine
// concrete+"::"+implMethod itself, which was only ever correct back when
// this only ever checked the exact concrete type; now that it walks
// ancestors too, the match's real owner has to travel with it.
func (asm *Assembly) resolveExplicitImpl(concreteTypeFullName, interfaceFullName, methodName string) (string, bool) {
	key := concreteTypeFullName + "\x00" + interfaceFullName + "\x00" + methodName
	asm.cacheMu.RLock()
	cached, hit := asm.explicitImpls[key]
	asm.cacheMu.RUnlock()
	if hit {
		return cached.name, cached.ok
	}

	name, ok := asm.resolveExplicitImplUncached(concreteTypeFullName, interfaceFullName, methodName)

	asm.cacheMu.Lock()
	asm.explicitImpls[key] = explicitImplResult{name: name, ok: ok}
	asm.cacheMu.Unlock()
	return name, ok
}

// resolveExplicitImplUncached is resolveExplicitImpl's real ancestor-chain
// walk, split out so resolveExplicitImpl's own cache check above can wrap
// it without recursing back through the cache — see explicitImpls' own
// doc comment for why this needs memoizing at all.
func (asm *Assembly) resolveExplicitImplUncached(concreteTypeFullName, interfaceFullName, methodName string) (string, bool) {
	seen := map[string]bool{}
	for typeName := concreteTypeFullName; typeName != "" && !seen[typeName]; {
		seen[typeName] = true
		if m, ok := asm.resolveExplicitImplExact(typeName, interfaceFullName, methodName); ok {
			return typeName + "::" + m, true
		}
		t, err := asm.resolveTypeByFullName(typeName)
		if err != nil || t == nil {
			break
		}
		typeName = t.BaseTypeFullName
	}
	return "", false
}

// resolveExplicitImplExact checks only typeFullName's own MethodImpl
// table — resolveExplicitImpl's per-ancestor step.
func (asm *Assembly) resolveExplicitImplExact(typeFullName, interfaceFullName, methodName string) (string, bool) {
	namespace, name := splitTypeName(typeFullName)
	typeRID, _, err := asm.md.FindTypeDef(namespace, name)
	if err != nil {
		// Multi-assembly fallback (Fase 3.27 style): typeFullName may
		// name a type that lives in a dependency, not this assembly.
		// asm.deps alone isn't always enough — a generic base class one
		// concrete leaf type's own assembly extends (found via a real,
		// load-bearing case, Fase 3.40: DocumentFormat.OpenXml.dll's own
		// SpreadsheetDocumentFeatures extends TypedPackageFeatureCollection
		// `2, whose real MethodImpl for `IMainPartFeature.Part` lives in
		// the SAME assembly — but that assembly isn't necessarily in
		// *this* asm.deps if resolveExplicitImpl's own walk started from
		// a receiver whose Resolvers happen to be scoped elsewhere) — so
		// this also tries globalTypeIndex, the same cross-package
		// last-resort owner lookup resolveTypeByFullNameAt itself already
		// relies on (see that field's own doc comment).
		for _, dep := range asm.deps {
			if m, ok := dep.resolveExplicitImplExact(typeFullName, interfaceFullName, methodName); ok {
				return m, true
			}
		}
		if owner, ok := asm.globalTypeIndex[typeFullName]; ok && owner != asm {
			return owner.resolveExplicitImplExact(typeFullName, interfaceFullName, methodName)
		}
		return "", false
	}
	impls, err := asm.md.MethodImpls(typeRID)
	if err != nil {
		return "", false
	}
	for _, impl := range impls {
		declClass, declMethod, err := resolveMethodDefOrRefName(asm.md, impl.MethodDeclaration)
		if err != nil || declMethod != methodName || declClass != interfaceFullName {
			continue
		}
		_, bodyMethod, err := resolveMethodDefOrRefName(asm.md, impl.MethodBody)
		if err != nil {
			continue
		}
		return bodyMethod, true
	}
	return "", false
}

// resolveEnumMembers backs the interpreter's EnumResolver (Fase 3.26,
// System.Enum.GetValues/GetNames/IsDefined/ToObject) — only resolves a
// plugin-declared enum (a real TypeDef in this assembly's own metadata,
// or a dependency's — Fase 3.27); a BCL-only enum like System.DayOfWeek
// has none, so ok=false there (vmnet has no BCL enum member database,
// same documented limitation as every other "no real BCL metadata" gap
// in this project).
func (asm *Assembly) resolveEnumMembers(fullName string) ([]string, []int64, bool) {
	namespace, name := splitTypeName(fullName)
	typeRID, _, err := asm.md.FindTypeDef(namespace, name)
	if err != nil {
		for _, dep := range asm.deps {
			if names, values, ok := dep.resolveEnumMembers(fullName); ok {
				return names, values, true
			}
		}
		return nil, nil, false
	}
	names, values, err := asm.md.EnumMembers(typeRID)
	if err != nil {
		return nil, nil, false
	}
	return names, values, true
}

// resolveMethodDefOrRefName resolves a MethodDefOrRef-coded token (spec
// §II.24.2.6) to its owning type's full name and its own method name —
// used only by resolveExplicitImpl above, which needs both halves of a
// MethodImpl row's tokens (almost always MemberRefs pointing at an
// interface, sometimes a TypeSpec-instantiated generic interface like
// IEnumerable<int>, which resolveTypeTokenName already collapses back to
// its open form "IEnumerable`1" the same way every other call-target
// resolution in this file does).
func resolveMethodDefOrRefName(md *metadata.Metadata, tok metadata.Token) (className, methodName string, err error) {
	switch tok.Table() {
	case metadata.TableMethodDef:
		row, err := md.MethodDef(tok.RID())
		if err != nil {
			return "", "", err
		}
		ownerRID, err := md.MethodDefOwner(tok.RID())
		if err != nil {
			return "", "", err
		}
		owner, err := md.TypeDef(ownerRID)
		if err != nil {
			return "", "", err
		}
		ownerName, err := qualifyTypeDefName(md, ownerRID, owner)
		if err != nil {
			return "", "", err
		}
		return ownerName, row.Name, nil
	case metadata.TableMemberRef:
		row, err := md.MemberRef(tok.RID())
		if err != nil {
			return "", "", err
		}
		className, err := resolveTypeTokenName(md, row.Class)
		if err != nil {
			return "", "", err
		}
		return className, row.Name, nil
	default:
		return "", "", fmt.Errorf("vmnet: unsupported MethodDefOrRef token table %#x", byte(tok.Table()))
	}
}

// buildMethod resolves a MethodDef row all the way down to executable IR:
// signature, method body bytes (via RVA), IL decode and IR lowering. The
// result is cached by full name.
func (asm *Assembly) buildMethod(methodRID uint32, row metadata.MethodDefRow) (*runtime.Method, error) {
	if m, ok := asm.cachedMethodByRID(methodRID); ok {
		return m, nil
	}

	typeRID, err := asm.md.MethodDefOwner(methodRID)
	if err != nil {
		return nil, err
	}
	typeDef, err := asm.md.TypeDef(typeRID)
	if err != nil {
		return nil, err
	}
	typeName, err := qualifyTypeDefName(asm.md, typeRID, typeDef)
	if err != nil {
		return nil, err
	}
	fullName := typeName + "::" + row.Name

	sig, err := metadata.ParseMethodSig(row.Signature)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", fullName, err)
	}
	if row.RVA == 0 {
		return nil, fmt.Errorf("%s: method has no body (abstract/extern methods are unsupported)", fullName)
	}

	body, err := asm.file.RVA(row.RVA)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", fullName, err)
	}
	header, code, err := il.ReadMethodBody(body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", fullName, err)
	}
	instrs, err := il.Decode(code)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", fullName, err)
	}

	var ehClauses []il.ExceptionHandler
	if header.MoreSections {
		ehClauses, err = il.ReadExceptionHandlers(body, header, 12+int(header.CodeSize))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", fullName, err)
		}
	}

	retVoid := sig.RetType.Kind == metadata.SigVoid
	irInstrs, handlers, err := ir.Build(instrs, asm.md, retVoid, ehClauses)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", fullName, err)
	}

	localCount := 0
	var localDefaults []runtime.Value
	if header.Fat && header.LocalVarSigToken != 0 {
		sigRID := metadata.Token(header.LocalVarSigToken).RID()
		localSigRow, err := asm.md.StandAloneSig(sigRID)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", fullName, err)
		}
		locals, err := metadata.ParseLocalVarSig(localSigRow.Signature)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", fullName, err)
		}
		localCount = len(locals)
		localDefaults = make([]runtime.Value, localCount)
		for i, l := range locals {
			def, err := asm.fieldOrLocalDefault(l, 0)
			if err != nil {
				return nil, fmt.Errorf("%s: local %d: %w", fullName, i, err)
			}
			localDefaults[i] = def
		}
	}

	m := &runtime.Method{
		FullName:      fullName,
		HasThis:       sig.HasThis,
		HasReturn:     !retVoid,
		ParamCount:    int(sig.ParamCount),
		LocalCount:    localCount,
		MaxStack:      int(header.MaxStack),
		IR:            irInstrs,
		LocalDefaults: localDefaults,
		Handlers:      handlers,
		Resolvers:     asm.resolvers(),
	}
	asm.storeMethodByRID(methodRID, m)
	return m, nil
}

// resolvers bundles asm's own four resolver methods (Fase 3.27) — stamped
// onto every *runtime.Method asm builds, so interpreter.Machine.invoke can
// scope name resolution to the right assembly for the whole time that
// method's body (and anything it calls transitively) runs. The exact same
// four functions call.go's asm.machine() uses to configure the top-level
// Machine — this is what makes a method built by a dependency assembly
// resolve against ITS OWN metadata instead of silently inheriting the
// entry-point assembly's.
func (asm *Assembly) resolvers() *runtime.Resolvers {
	return &runtime.Resolvers{
		Resolve:                 asm.resolveByFullName,
		ResolveType:             asm.resolveTypeByFullName,
		ResolveExplicitImpl:     asm.resolveExplicitImpl,
		ResolveEnum:             asm.resolveEnumMembers,
		ResolveFieldBytes:       asm.resolveFieldBytes,
		ResolveMember:           asm.resolveMember,
		ResolveManifestResource: asm.resolveManifestResource,
		ResolveProperties:       asm.resolveProperties,
		ResolveMemberParams:     asm.resolveMemberParams,
	}
}

// resolveManifestResource backs Assembly.GetManifestResourceStream (Fase
// 3.40) — looks up name in THIS assembly's own ManifestResource table
// only (no dependency fallback: Assembly.GetExecutingAssembly() always
// names one specific assembly, and Machine.invoke already swaps the
// active resolver to match whichever assembly's method is currently
// running, so there's never a "which assembly did the caller mean"
// ambiguity the way a TypeRef into a dependency has). ok=false for a
// resource that doesn't exist, or one whose Implementation names another
// file/assembly entirely (a real but rare shape — every resource found
// in real packages so far is embedded directly in the requesting
// assembly's own PE image).
func (asm *Assembly) resolveManifestResource(name string) ([]byte, bool) {
	row, found, err := asm.md.FindManifestResource(name)
	if err != nil || !found || !row.Implementation.IsNil() {
		return nil, false
	}
	section, err := asm.file.RVA(asm.file.CLI.ResourcesRVA)
	if err != nil || uint64(row.Offset)+4 > uint64(len(section)) {
		return nil, false
	}
	entry := section[row.Offset:]
	length := binary.LittleEndian.Uint32(entry[:4])
	if uint64(4+length) > uint64(len(entry)) {
		return nil, false
	}
	data := make([]byte, length)
	copy(data, entry[4:4+length])
	return data, true
}

// resolveFieldBytes backs the interpreter's FieldBytesResolver (Fase
// 3.27, RuntimeHelpers.InitializeArray) — only resolves a field declared
// in this assembly's own metadata (or a dependency's); see
// rvaFieldBytes for what "has embedded data" actually means.
func (asm *Assembly) resolveFieldBytes(typeFullName, fieldName string) ([]byte, bool) {
	namespace, name := splitTypeName(typeFullName)
	typeRID, _, err := asm.md.FindTypeDef(namespace, name)
	if err != nil {
		for _, dep := range asm.deps {
			if data, ok := dep.resolveFieldBytes(typeFullName, fieldName); ok {
				return data, true
			}
		}
		return nil, false
	}
	start, end, err := asm.md.TypeDefFieldRange(typeRID)
	if err != nil {
		return nil, false
	}
	for rid := start; rid < end; rid++ {
		f, err := asm.md.Field(rid)
		if err != nil || f.Name != fieldName {
			continue
		}
		sig, err := metadata.ParseFieldSig(f.Signature)
		if err != nil {
			return nil, false
		}
		data, ok, err := asm.rvaFieldBytes(rid, sig)
		if err != nil || !ok {
			return nil, false
		}
		// rvaFieldBytes returns the exact declared byte size (ClassLayout),
		// always correct as raw bytes regardless of the caller's own
		// element-width interpretation of them (runtimeHelpersInitializeArray).
		return data, true
	}
	return nil, false
}

// resolveMember backs the interpreter's MemberResolver (Fase 3.39,
// System.Reflection.ConstructorInfo/MethodInfo — Type.GetConstructor/
// GetMethod). Matches by real declared parameter type names, not
// pickMethodOverload's runtime-argument-Kind scoring — there are no real
// arguments yet at this point, only the caller's own declared Type[]
// signature, so an exact name match is required wherever a name is
// resolvable at all (SigClass/SigValueType/SigGenericInst); a parameter
// whose type name can't be resolved this way (a primitive, a generic
// method parameter, ...) is accepted leniently rather than rejected —
// the same "best effort, not full type identity" posture
// hasHardShapeMismatch/scoreParamMatch already document for overload
// resolution proper.
//
// paramTypeFullNames == nil (a real Go nil, distinct from a non-nil
// zero-length slice — bcl.TypeArrayToFullNames preserves that
// distinction for a real, empty Type[] argument via Go's own make()
// semantics) means Type.GetMethod(string) — the plain, no-Type[]-at-all
// overload (Fase 3.51, found via a real, common pattern: a generic
// method like `T Identity<T>(T)`, whose eventual MakeGenericMethod call
// has no way to spell out a still-open T in a Type[] up front, so real
// code looks it up by bare name first). Matches the FIRST candidate by
// name alone in that case, same simplification GetProperty/GetField
// already make (no overload-ambiguity detection at all, unlike real
// reflection's AmbiguousMatchException) — acceptable since every real
// target here uses this shape for an unambiguous, non-overloaded name.
func (asm *Assembly) resolveMember(typeFullName, memberName string, paramTypeFullNames []string) (string, bool) {
	// typeFullName can be a CLOSED generic instantiation's own encoded
	// name (e.g. "Dapper.SqlMapper+TypeHandlerCache`1[[System.Data.
	// DataTable]]", ir/builder.go's sigTypeFullName encoding — reached
	// via Type.GetMethod/GetConstructor/GetField called on a Type
	// obtained from Type.MakeGenericType) — but FindTypeDef only ever
	// finds a type's OPEN/unbound TypeDef (there's no separate TypeDef
	// per closed instantiation in real metadata, ECMA-335's own model:
	// one TypeDef, however many closed uses). bcl.GenericOpenName strips
	// the "[[...]]" suffix first; every plain (non-generic) name passes
	// through unchanged, so this is safe as an unconditional first step
	// (Fase 3.52, found via Dapper's own SqlMapper static ctor reflecting
	// over TypeHandlerCache<DataTable>/<XmlDocument>/<XDocument>/
	// <XElement> to cache each one's SetHandler method).
	typeFullName = bcl.GenericOpenName(typeFullName)
	namespace, typeName := splitTypeName(typeFullName)
	typeRID, _, err := asm.md.FindTypeDef(namespace, typeName)
	if err != nil {
		for _, dep := range asm.deps {
			if name, ok := dep.resolveMember(typeFullName, memberName, paramTypeFullNames); ok {
				return name, true
			}
		}
		return "", false
	}
	_, rows, err := asm.md.FindMethodDefCandidates(typeRID, memberName)
	if err != nil {
		return "", false
	}
	if paramTypeFullNames == nil && len(rows) > 0 {
		return typeFullName + "::" + memberName, true
	}
	for _, row := range rows {
		sig, err := metadata.ParseMethodSig(row.Signature)
		if err != nil || len(sig.Params) != len(paramTypeFullNames) {
			continue
		}
		match := true
		for i, p := range sig.Params {
			name, ok := paramTypeName(asm.md, p)
			if ok && name != paramTypeFullNames[i] {
				match = false
				break
			}
		}
		if match {
			return typeFullName + "::" + memberName, true
		}
	}
	return "", false
}

// resolveProperties backs the interpreter's PropertyResolver (Fase 3.51,
// Type.GetProperties/GetProperty) — reads typeFullName's own declared
// properties directly off the real Property/PropertyMap/MethodSemantics
// tables (metadata.TypeDefPropertyRange/Property/PropertyAccessors), not
// derived from a get_Xxx/set_Xxx naming guess: MethodSemantics is the
// real linkage a property's accessors use, so this stays correct even
// for a non-standard accessor name (vanishingly rare in practice, but
// free to get right here since the real linkage is already read anyway).
func (asm *Assembly) resolveProperties(typeFullName string) (names []string, canRead []bool, canWrite []bool, propTypes []string, ok bool) {
	namespace, typeName := splitTypeName(typeFullName)
	typeRID, _, err := asm.md.FindTypeDef(namespace, typeName)
	if err != nil {
		for _, dep := range asm.deps {
			if n, r, w, pt, depOK := dep.resolveProperties(typeFullName); depOK {
				return n, r, w, pt, true
			}
		}
		return nil, nil, nil, nil, false
	}
	start, end, err := asm.md.TypeDefPropertyRange(typeRID)
	if err != nil {
		return nil, nil, nil, nil, false
	}
	for rid := start; rid < end; rid++ {
		prop, err := asm.md.Property(rid)
		if err != nil {
			continue
		}
		getterRID, setterRID, err := asm.md.PropertyAccessors(rid)
		if err != nil {
			continue
		}
		names = append(names, prop.Name)
		canRead = append(canRead, getterRID != 0)
		canWrite = append(canWrite, setterRID != 0)
		propTypes = append(propTypes, asm.propertyTypeFullName(getterRID, setterRID))
	}
	return names, canRead, canWrite, propTypes, true
}

// propertyTypeFullName answers PropertyInfo.PropertyType (Fase 3.52) —
// read off whichever real accessor exists rather than a separate parse
// of the Property row's own PropertySig blob (metadata.Property's own
// doc comment: the row carries a PropertySig, but every property found
// in practice has at least one real accessor, and reusing
// metadata.ParseMethodSig here means no second signature parser has to
// exist just for this). The getter's return type is authoritative when
// there is one; a set-only property (get_Xxx absent, real if rare) falls
// back to the setter's own single "value" parameter type instead.
func (asm *Assembly) propertyTypeFullName(getterRID, setterRID uint32) string {
	rid := getterRID
	useReturn := true
	if rid == 0 {
		rid = setterRID
		useReturn = false
	}
	if rid == 0 {
		return ""
	}
	row, err := asm.md.MethodDef(rid)
	if err != nil {
		return ""
	}
	sig, err := metadata.ParseMethodSig(row.Signature)
	if err != nil {
		return ""
	}
	var t metadata.SigType
	if useReturn {
		t = sig.RetType
	} else if len(sig.Params) > 0 {
		t = sig.Params[0]
	} else {
		return ""
	}
	name, err := ir.SigTypeFullName(asm.md, t)
	if err != nil {
		return ""
	}
	return name
}

// resolveMemberParams backs the interpreter's MemberParamsResolver (Fase
// 3.52: Type.GetConstructors, MethodBase.GetParameters/ParameterInfo) —
// every real overload of typeFullName's member named memberName
// (memberName is ".ctor" for a constructor, same convention
// resolveMember already uses), each described by its own declared
// parameter type names (metadata.ParseMethodSig + ir.SigTypeFullName,
// same as propertyTypeFullName above) and real parameter NAMES
// (paramNamesFor, below) — found via Dapper's own constructor-based
// row-to-object mapper, which enumerates a target type's constructors to
// find the best parameter match against a query's column set.
func (asm *Assembly) resolveMemberParams(typeFullName, memberName string) (paramTypes [][]string, paramNames [][]string, ok bool) {
	namespace, typeName := splitTypeName(typeFullName)
	typeRID, _, err := asm.md.FindTypeDef(namespace, typeName)
	if err != nil {
		for _, dep := range asm.deps {
			if pt, pn, depOK := dep.resolveMemberParams(typeFullName, memberName); depOK {
				return pt, pn, true
			}
		}
		return nil, nil, false
	}
	rids, rows, err := asm.md.FindMethodDefCandidates(typeRID, memberName)
	if err != nil {
		// A real type with no overload of this member at all (e.g. a
		// class relying purely on the compiler-synthesized default
		// .ctor, which has no MethodDef row of its own to find) — ok=true
		// with zero results, not an error: the type itself did resolve.
		return nil, nil, true
	}
	for i, row := range rows {
		sig, err := metadata.ParseMethodSig(row.Signature)
		if err != nil {
			continue
		}
		types := make([]string, len(sig.Params))
		for j, p := range sig.Params {
			if name, err := ir.SigTypeFullName(asm.md, p); err == nil {
				types[j] = name
			}
		}
		paramTypes = append(paramTypes, types)
		paramNames = append(paramNames, asm.paramNamesFor(rids[i], len(sig.Params)))
	}
	return paramTypes, paramNames, true
}

// paramNamesFor reads methodRID's real Param row names
// (metadata.MethodDefParamRange/Param — Sequence 1..n map to parameter
// position 0..n-1; Sequence 0 is the method's own return-value
// pseudo-param, skipped here), falling back to a synthesized "argN"
// placeholder for any position with no real Param row at all (rare: only
// an entirely reflection-emitted or heavily trimmed/optimized method
// omits a name for a real source-level parameter — every normal C#
// compiler emits one for every declared parameter).
func (asm *Assembly) paramNamesFor(methodRID uint32, paramCount int) []string {
	names := make([]string, paramCount)
	for i := range names {
		names[i] = fmt.Sprintf("arg%d", i)
	}
	start, end, err := asm.md.MethodDefParamRange(methodRID)
	if err != nil {
		return names
	}
	for rid := start; rid < end; rid++ {
		p, err := asm.md.Param(rid)
		if err != nil || p.Sequence == 0 || int(p.Sequence) > paramCount {
			continue
		}
		names[p.Sequence-1] = p.Name
	}
	return names
}

// resolveTypeByFullName implements interpreter.TypeResolver: it builds a
// runtime.Type (field layout) for a plain class discovered while executing
// newobj/ldfld/stfld.
//
// Since Fase 3.5 a Type carries real mutable state (static fields, a
// .cctor latch), so two goroutines racing to resolve the same not-yet-
// cached type must never end up with each using its own separate
// *runtime.Type — one goroutine's .cctor writes would then be invisible
// to the other. That's handled below by a check-build-check-store
// sequence that only holds cacheMu for the cheap map operations, NOT
// across buildType: a value-typed field or local's default (Fase 3.7)
// requires recursively resolving that nested type, which — if cacheMu
// were held across the whole build, like the very first version of this
// fix was — would deadlock immediately on Go's non-reentrant sync.Mutex.
// On a genuine concurrent-first-access race, both goroutines build a full
// Type and the loser's is simply discarded (wasted work, not a
// correctness problem: every caller still ends up with the one stored in
// asm.types, so .cctor-once semantics hold).
func (asm *Assembly) resolveTypeByFullName(fullName string) (*runtime.Type, error) {
	return asm.resolveTypeByFullNameAt(fullName, 0)
}

// resolveTypeByFullNameAt is resolveTypeByFullName's real implementation,
// carrying a recursion depth (Fase 3.27) that only a value-typed field's
// own default (valueTypeDefault) ever increments — see maxValueTypeDepth's
// doc comment for why this exists at all. The public zero-arg
// resolveTypeByFullName (the interpreter.Resolvers.ResolveType shape) is
// always the depth-0 entry point; nothing outside this file needs to
// know depth exists.
func (asm *Assembly) resolveTypeByFullNameAt(fullName string, depth int) (*runtime.Type, error) {
	if t, ok := asm.cachedType(fullName); ok {
		return t, nil
	}
	// A native BCL value type (System.TimeSpan, ...) has no TypeDef in
	// the plugin's own metadata — buildType/FindTypeDef below would
	// never find it. Found the hard way (Fase 3.23): TimeSpan.Zero is a
	// real static field (`ldsfld System.TimeSpan::Zero`), and resolving
	// ir.LoadStaticField's owning type goes through this exact function.
	// bcl's own synthetic Type is already the single shared instance
	// backing every value of that type (initobj/newobj already resolve
	// it the same way, internal/interpreter/structs.go) — returned
	// directly, not cached into asm.types, since it isn't owned by this
	// Assembly at all.
	if t, ok := bcl.LookupValueType(fullName); ok {
		return t, nil
	}
	// A reference-shaped BCL type that still needs real static-field
	// storage (Fase 3.27, e.g. `ldsfld System.String::Empty`) — see
	// LookupStaticFieldHost's doc comment for why this can't share the
	// LookupValueType path above.
	if t, ok := bcl.LookupStaticFieldHost(fullName); ok {
		return t, nil
	}
	t, err := asm.buildType(fullName, depth)
	if err != nil {
		// Multi-assembly fallback (Fase 3.27) — see
		// resolveByFullNameInDeps's doc comment for why this is needed at
		// all: a glue assembly's own TypeRefs into a dependency carry no
		// assembly-boundary information by the time they reach here.
		for _, dep := range asm.deps {
			if dt, derr := dep.resolveTypeByFullName(fullName); derr == nil {
				return dt, nil
			}
		}
		// Cross-package last resort (Fase 3.40) — see globalTypeIndex's
		// own doc comment. Jumps directly to the owning assembly's own
		// buildType, never back through resolveTypeByFullNameAt/deps, so
		// this can never recurse regardless of how the real dependency
		// graph is shaped.
		if owner, ok := asm.globalTypeIndex[fullName]; ok && owner != asm {
			if dt, derr := owner.buildType(fullName, depth); derr == nil {
				owner.cacheMu.Lock()
				if existing, ok := owner.types[fullName]; ok {
					dt = existing
				} else {
					owner.types[fullName] = dt
				}
				owner.cacheMu.Unlock()
				return dt, nil
			}
		}
		return nil, err
	}
	asm.cacheMu.Lock()
	defer asm.cacheMu.Unlock()
	if existing, ok := asm.types[fullName]; ok {
		return existing, nil
	}
	asm.types[fullName] = t
	return t, nil
}

func (asm *Assembly) cachedType(fullName string) (*runtime.Type, bool) {
	asm.cacheMu.Lock()
	defer asm.cacheMu.Unlock()
	t, ok := asm.types[fullName]
	return t, ok
}

// indexOwnTypesInto records every TypeDef this assembly itself declares
// (by full name, "+"-qualified for nested types) into index — see
// globalTypeIndex's own doc comment. A name a later assembly ALSO
// declares (a real, if rare, cross-package collision) keeps whichever
// assembly claimed it first; the index is a best-effort last resort,
// not a promise of perfect disambiguation.
func (asm *Assembly) indexOwnTypesInto(index map[string]*Assembly) {
	n := asm.md.RowCount(metadata.TableTypeDef)
	for rid := uint32(1); rid <= n; rid++ {
		row, err := asm.md.TypeDef(rid)
		if err != nil {
			continue
		}
		name, err := qualifyTypeDefName(asm.md, rid, row)
		if err != nil {
			continue
		}
		if _, exists := index[name]; !exists {
			index[name] = asm
		}
	}
}

func (asm *Assembly) buildType(fullName string, depth int) (*runtime.Type, error) {
	namespace, name := splitTypeName(fullName)
	typeRID, typeDef, err := asm.md.FindTypeDef(namespace, name)
	if err != nil {
		return nil, err
	}

	isValueType, isEnum, err := asm.classifyTypeDef(typeDef)
	if err != nil {
		return nil, err
	}
	isInterface := typeDef.Flags&typeAttrInterface != 0
	isAbstract := typeDef.Flags&typeAttrAbstract != 0

	// Instance fields are inherited (real CLR field layout: a base type's
	// fields come first in memory, before its own) — a struct can't have
	// a user-defined base (isValueType guard), so this only ever recurses
	// for classes. Resolving the base now, rather than lazily, means
	// ldfld/stfld against a field declared on a base class finds it on
	// every subtype's own runtime.Type.Fields, not just the base's own —
	// found via the first isinst fixture with an inherited field access
	// (Fase 3.8): without this, `Dog : Animal` simply has no `Name` field
	// at all, since Fase 1-3.7 never needed to look past a type's own
	// TypeDef. Safe to recurse: resolveTypeByFullName doesn't hold cacheMu
	// across a build (Fase 3.7's fix for the same shape of problem).
	var baseName string
	var fields []string
	var fieldDefaults []runtime.Value
	if !isValueType && !typeDef.Extends.IsNil() {
		if resolved, err := resolveTypeTokenName(asm.md, typeDef.Extends); err == nil &&
			resolved != "System.Object" && resolved != "System.ValueType" && resolved != "System.Enum" {
			baseName = resolved
			if base, err := asm.resolveTypeByFullName(baseName); err == nil {
				fields = append(fields, base.Fields...)
				fieldDefaults = append(fieldDefaults, base.FieldDefaults...)
			}
		}
	}

	start, end, err := asm.md.TypeDefFieldRange(typeRID)
	if err != nil {
		return nil, err
	}
	var staticFields []string
	var staticFieldDefaults []runtime.Value
	for rid := start; rid < end; rid++ {
		f, err := asm.md.Field(rid)
		if err != nil {
			return nil, err
		}
		def := runtime.Null()
		sig, sigErr := metadata.ParseFieldSig(f.Signature)
		// An RVA-backed field (FieldAttributes + a FieldRVA table row) is a
		// compiler-emitted embedded data blob — e.g. a large `byte[]`/
		// `ReadOnlySpan<byte>` literal the compiler stores as raw bytes in
		// the PE image rather than building at runtime (Fase 3.27, found
		// running real third-party code: Esprima's Character.
		// s_characterData, a 32KB Unicode classification table). Checked
		// before the literal-field case below since these are also
		// static+initonly but need a real embedded value, not Null().
		if sigErr == nil {
			if rvaBytes, ok, err := asm.rvaFieldBytes(rid, sig); err == nil && ok {
				def = runtime.ArrRef(bytesToInt32Array(rvaBytes))
				staticFields = append(staticFields, f.Name)
				staticFieldDefaults = append(staticFieldDefaults, def)
				continue
			}
		}
		// A literal field (FieldAttributes.Literal — every enum member,
		// e.g. `Red` on `enum TrafficLight`, and every plain `const`
		// field, e.g. `const short sid = ...;`) is a real Constant-table
		// value baked in at compile time, never a computed runtime
		// default — and, for an enum member specifically, its own field
		// signature is a self-referential valuetype token (real IL
		// declares `static literal valuetype TrafficLight Red =
		// int32(0)`, not `int32 Red`), so running fieldOrLocalDefault on
		// it would recurse into building this exact same Type again to
		// compute ITS OWN default, which hasn't finished being built yet
		// — infinite recursion (found the hard way, Fase 3.25). Reading
		// the real value via the Constant table instead (Fase 3.39)
		// sidesteps that entirely: the Constant row's own type tag is
		// always a plain integer/float/string/null, even for an enum
		// member (whose underlying value the CLI always records with its
		// plain underlying-integer tag) — the field's declared signature
		// is never consulted here at all. Falls back to Null() (this
		// function's existing zero-value default) only if the Constant
		// table row itself can't be decoded, not silently for every
		// literal field as before.
		if f.Flags&fieldAttrLiteral != 0 {
			if kind, n, fl, s, ok, cerr := asm.md.ConstantForField(rid); cerr == nil && ok {
				switch kind {
				case metadata.ConstantInt32:
					def = runtime.Int32(int32(n))
				case metadata.ConstantInt64:
					def = runtime.Int64(n)
				case metadata.ConstantFloat:
					def = runtime.Float64(fl)
				case metadata.ConstantString:
					def = runtime.String(s)
				case metadata.ConstantNull:
					def = runtime.Null()
				}
			}
		} else if sigErr == nil {
			def, err = asm.fieldOrLocalDefault(sig, depth)
			if err != nil {
				return nil, err
			}
		}
		if f.Flags&fieldAttrStatic != 0 {
			staticFields = append(staticFields, f.Name)
			staticFieldDefaults = append(staticFieldDefaults, def)
		} else {
			fields = append(fields, f.Name)
			fieldDefaults = append(fieldDefaults, def)
		}
	}

	t := runtime.NewType(typeDef.Namespace, typeDef.Name, fields, staticFields, fieldDefaults, staticFieldDefaults)
	t.IsValueType = isValueType
	t.IsEnum = isEnum
	t.IsInterface = isInterface
	t.IsAbstract = isAbstract
	t.BaseTypeFullName = baseName
	// fullName is already correctly "+"-qualified for a nested type (this
	// function's own caller chain always resolves it via
	// qualifyTypeDefName before calling resolveTypeByFullName) — t.Name
	// alone is just the bare TypeDef name ("<>c"), which fullTypeName
	// (internal/interpreter/typecheck.go) would otherwise reconstruct
	// into a colliding, unqualified name for any nested plugin type
	// (Fase 3.17, same bug class as the ldsfld one this fase fixed).
	t.QualifiedName = fullName

	ifaceTokens, err := asm.md.InterfaceImpls(typeRID)
	if err != nil {
		return nil, err
	}
	for _, tok := range ifaceTokens {
		if name, err := resolveTypeTokenName(asm.md, tok); err == nil {
			t.Interfaces = append(t.Interfaces, name)
		}
		// A genuinely unresolvable interface reference is skipped rather
		// than failing the whole type: isinst/castclass just won't match
		// through that specific interface, not a hard error.
	}

	return t, nil
}

// rvaFieldBytes returns fieldRID's embedded initial-value blob, if it has
// one (Fase 3.27) — a FieldRVA table row (the starting address) plus its
// own value type's ClassLayout row (the byte count; Field/FieldRVA alone
// never record a length). ok=false for the overwhelming majority of
// fields, which have neither.
func (asm *Assembly) rvaFieldBytes(fieldRID uint32, sig metadata.SigType) ([]byte, bool, error) {
	rva, ok, err := asm.md.FieldRVA(fieldRID)
	if err != nil || !ok {
		return nil, false, err
	}
	var size uint32
	switch {
	case sig.Kind == metadata.SigValueType && sig.Token.Table() == metadata.TableTypeDef:
		// The common case for a longer array literal: a compiler-
		// synthesized `<PrivateImplementationDetails>/__StaticArrayInit
		// TypeSize=N` value type with an explicit [StructLayout(Size=N)],
		// N read from the ClassLayout table.
		size, ok, err = asm.md.ClassLayout(sig.Token.RID())
		if err != nil || !ok {
			return nil, false, err
		}
	case sig.Kind == metadata.SigI4 || sig.Kind == metadata.SigU4:
		// A short (<=4-byte) array literal: the compiler skips the
		// custom-struct/ClassLayout dance entirely and declares the
		// field as a plain int — its own natural 4-byte size doubles as
		// the blob's declared length, with no ClassLayout row needed at
		// all (found via a real case: NPOI's own POIFSConstants.
		// OOXML_FILE_HEADER, a 4-byte array literal).
		size = 4
	case sig.Kind == metadata.SigI8 || sig.Kind == metadata.SigU8:
		size = 8
	default:
		return nil, false, nil
	}
	data, err := asm.file.RVA(rva)
	if err != nil {
		return nil, false, err
	}
	if uint32(len(data)) < size {
		return nil, false, fmt.Errorf("vmnet: RVA-backed field: embedded blob (%d bytes available) shorter than declared size %d", len(data), size)
	}
	return data[:size], true, nil
}

// bytesToInt32Array wraps raw bytes as a runtime.Array of Int32-boxed
// byte values — matching vmnet's existing convention that a `byte`/
// `System.Byte` is represented as a plain KindI4 (Fase 1), the same
// representation any other byte[] element already has.
func bytesToInt32Array(b []byte) *runtime.Array {
	elems := make([]runtime.Value, len(b))
	for i, v := range b {
		elems[i] = runtime.Int32(int32(v))
	}
	return &runtime.Array{Elems: elems}
}

// classifyTypeDef reports whether typeDef is a struct (extends
// System.ValueType) or an enum (extends System.Enum, itself a
// System.ValueType) rather than a plain class, distinguishing the two
// (Fase 3.25, System.Type.IsEnum) — isAssignableTo/typeMatches (Fase 3.8)
// never needed that distinction (an enum's identity checks work the same
// as any other value type), but reflection does. Interfaces and
// System.Object itself have no Extends entry at all.
func (asm *Assembly) classifyTypeDef(typeDef metadata.TypeDefRow) (isValueType, isEnum bool, err error) {
	if typeDef.Extends.IsNil() {
		return false, false, nil
	}
	name, err := resolveTypeTokenName(asm.md, typeDef.Extends)
	if err != nil {
		// A base type vmnet can't resolve (e.g. a TypeSpec-encoded base,
		// vanishingly rare) isn't a value type as far as we can tell —
		// treat it as a class rather than failing type resolution outright.
		return false, false, nil
	}
	return name == "System.ValueType" || name == "System.Enum", name == "System.Enum", nil
}

// typeAttrInterface is TypeAttributes.Interface (ECMA-335 §II.23.1.15) —
// Fase 3.25, System.Type.IsInterface: a TypeDef with no Extends entry is
// either an interface or System.Object itself, indistinguishable without
// checking this flag.
const typeAttrInterface = 0x00000020

// typeAttrAbstract is TypeAttributes.Abstract (ECMA-335 §II.23.1.15) —
// Fase 3.39, System.Type.IsAbstract.
const typeAttrAbstract = 0x00000080

// qualifyTypeRefName resolves a TypeRef's full name, walking ResolutionScope
// when it points to another TypeRef (a nested type, e.g. List<T>'s own
// Enumerator) instead of a Module/ModuleRef/AssemblyRef — spec §II.22.38.
// A nested type's own Namespace column is always empty, so without this a
// nested type's name collapses to its bare Name, indistinguishable from
// any other same-named nested type anywhere. Narrower duplicate of
// internal/ir/builder.go's qualifyTypeRefName (unexported there, and this
// package can't import an internal/ package's unexported helpers).
func qualifyTypeRefName(md *metadata.Metadata, row metadata.TypeRefRow) (string, error) {
	if row.ResolutionScope.Table() != metadata.TableTypeRef {
		return qualify(row.Namespace, row.Name), nil
	}
	enclosing, err := md.TypeRef(row.ResolutionScope.RID())
	if err != nil {
		return "", err
	}
	enclosingName, err := qualifyTypeRefName(md, enclosing)
	if err != nil {
		return "", err
	}
	return enclosingName + "+" + row.Name, nil
}

// qualifyTypeDefName resolves a TypeDef's full name, walking the
// NestedClass table (spec §II.22.32) when it's a nested type — the
// TypeDef-table counterpart of qualifyTypeRefName above, needed for a
// plugin's own nested types. Found the hard way (Fase 3.17): the C#
// compiler emits one non-capturing-lambda cache class (literally named
// "<>c") PER enclosing type that has any, so an assembly with lambdas in
// two different classes ends up with two separate TypeDefs both named
// "<>c" — collapsing either to its bare name picks whichever
// metadata.FindTypeDef happens to scan first, silently resolving a
// static field/method against the WRONG type. Narrower duplicate of
// internal/ir/builder.go's qualifyTypeDefName (unexported there, and
// this package can't import an internal/ package's unexported helpers).
func qualifyTypeDefName(md *metadata.Metadata, typeRID uint32, row metadata.TypeDefRow) (string, error) {
	enclosingRID, ok, err := md.EnclosingClass(typeRID)
	if err != nil {
		return "", err
	}
	if !ok {
		return qualify(row.Namespace, row.Name), nil
	}
	enclosingRow, err := md.TypeDef(enclosingRID)
	if err != nil {
		return "", err
	}
	enclosingName, err := qualifyTypeDefName(md, enclosingRID, enclosingRow)
	if err != nil {
		return "", err
	}
	return enclosingName + "+" + row.Name, nil
}

// resolveTypeTokenName resolves a TypeDef/TypeRef/TypeSpec token to
// "Namespace.Name" — a TypeSpec (a generic interface instantiation like
// IEnumerable<T>/IComparable<T>, extremely common in a class's
// InterfaceImpl rows) resolves to its *open* generic type's name, same
// simplification internal/ir/builder.go's resolveTypeSpecName already
// makes for newobj/call targets: vmnet's type-hierarchy walk (Fase 3.8)
// only needs the name to match against, not the closed type arguments.
func resolveTypeTokenName(md *metadata.Metadata, tok metadata.Token) (string, error) {
	switch tok.Table() {
	case metadata.TableTypeRef:
		row, err := md.TypeRef(tok.RID())
		if err != nil {
			return "", err
		}
		return qualifyTypeRefName(md, row)
	case metadata.TableTypeDef:
		row, err := md.TypeDef(tok.RID())
		if err != nil {
			return "", err
		}
		return qualifyTypeDefName(md, tok.RID(), row)
	case metadata.TableTypeSpec:
		sig, err := md.TypeSpecSignature(tok.RID())
		if err != nil {
			return "", err
		}
		t, err := metadata.ParseTypeSpec(sig)
		if err != nil {
			return "", err
		}
		if t.Kind != metadata.SigGenericInst {
			return "", fmt.Errorf("vmnet: unsupported TypeSpec kind %d as a base/interface type", t.Kind)
		}
		return resolveTypeTokenName(md, t.Token)
	default:
		return "", fmt.Errorf("vmnet: unsupported base-type token table %#x", byte(tok.Table()))
	}
}

// fieldAttrStatic is FieldAttributes.Static (ECMA-335 §II.23.1.5).
const fieldAttrStatic = 0x0010

// fieldAttrLiteral is FieldAttributes.Literal (ECMA-335 §II.23.1.5) — set
// on every enum member (`Red` on `enum TrafficLight`) and any other C#
// `const` field.
const fieldAttrLiteral = 0x0040

// fieldOrLocalDefault maps a field's or local's signature type to its CLR
// implicit zero-init value (spec: fields via beforefieldinit/allocation,
// locals via the InitLocals flag C# always sets — see
// runtime.Method.LocalDefaults): a typed numeric zero for numeric-kind
// value types (so arithmetic on a never-explicitly-assigned field/local
// works, matching real `static int x;`/`int x;` semantics), a real
// zero-valued struct for a value type with fields (Fase 3.7), or Null()
// for anything reference-shaped or unresolvable.
func (asm *Assembly) fieldOrLocalDefault(sig metadata.SigType, depth int) (runtime.Value, error) {
	switch sig.Kind {
	case metadata.SigBoolean, metadata.SigChar,
		metadata.SigI1, metadata.SigU1, metadata.SigI2, metadata.SigU2,
		metadata.SigI4, metadata.SigU4, metadata.SigI, metadata.SigU:
		return runtime.Int32(0), nil
	case metadata.SigI8, metadata.SigU8:
		return runtime.Int64(0), nil
	case metadata.SigR4:
		return runtime.Float32(0), nil
	case metadata.SigR8:
		return runtime.Float64(0), nil
	case metadata.SigValueType:
		return asm.valueTypeDefault(sig.Token, depth), nil
	case metadata.SigGenericInst:
		if sig.GenericInstIsValueType {
			return asm.valueTypeDefault(sig.Token, depth), nil
		}
		return runtime.Null(), nil
	default:
		return runtime.Null(), nil
	}
}

// maxValueTypeDepth bounds how many nested value-typed fields
// valueTypeDefault will chase before giving up and returning Null()
// (Fase 3.27) — a real safety net, not a tuning knob: legitimate nesting
// (a struct containing a struct containing a struct, ...) never goes
// remotely this deep in practice, but a genuinely self-referential or
// mutually-cyclic field signature does exist in the wild (found running
// real third-party code — a synthetic Roslyn-generated value type whose
// own field, still being investigated, appears to reference itself)
// and previously crashed the whole process with a Go stack overflow
// instead of erroring gracefully — precisely the kind of unbounded
// recursion Machine.Invoke's own MaxCallDepth guards against for
// interpreted method calls, applied here to type-building recursion,
// which has no interpreter frame to bound it naturally.
const maxValueTypeDepth = 24

// valueTypeDefault resolves tok (a TypeDef/TypeRef naming a value type) to
// a zero-valued runtime.Struct: a native BCL value type (Nullable`1, ...)
// via bcl.LookupValueType, else a plugin's own struct via
// resolveTypeByFullName (which may recurse here again for a nested
// struct field — bounded by maxValueTypeDepth, not unconditionally safe
// as this comment once assumed). A type vmnet can't resolve at all (a
// foreign BCL struct it doesn't model, e.g. DateTime) falls back to
// Null() rather than failing the whole field/local's type resolution
// over it — consistent with how an unresolvable Call target only errors
// when actually invoked, not at load time.
func (asm *Assembly) valueTypeDefault(tok metadata.Token, depth int) runtime.Value {
	if depth >= maxValueTypeDepth {
		return runtime.Null()
	}
	name, err := resolveTypeTokenName(asm.md, tok)
	if err != nil {
		return runtime.Null()
	}
	if t, ok := bcl.LookupValueType(name); ok {
		return runtime.StructVal(runtime.NewStruct(t))
	}
	t, err := asm.resolveTypeByFullNameAt(name, depth+1)
	if err != nil || !t.IsValueType {
		return runtime.Null()
	}
	// A real C# enum is ALWAYS represented as its underlying primitive
	// (int32, unless declared otherwise — vmnet only ever models int32,
	// same simplification the rest of the enum support has) directly on
	// the CIL stack — never as a struct, even though its own TypeDef is
	// technically a value type with a value__ field (Fase 3.7/3.17/3.25
	// never needed to distinguish this, since nothing before now built an
	// enum-typed field/local's *default* — every other enum access so
	// far went through a real assignment, an int32 already). Found the
	// hard way running real Jint (Fase 3.27): Jint.Runtime.Debugger.
	// StepMode's default field value reached a `switch` still
	// struct-wrapped, which expects a plain int32 like any other real
	// enum value.
	if t.IsEnum {
		return runtime.Int32(0)
	}
	return runtime.StructVal(runtime.NewStruct(t))
}

func splitTypeName(typeName string) (namespace, name string) {
	dot := strings.LastIndex(typeName, ".")
	if dot < 0 {
		return "", typeName
	}
	return typeName[:dot], typeName[dot+1:]
}

func splitFullName(fullName string) (namespace, typeName, methodName string, err error) {
	idx := strings.LastIndex(fullName, "::")
	if idx < 0 {
		return "", "", "", fmt.Errorf("vmnet: invalid method full name %q", fullName)
	}
	ns, tn := splitTypeName(fullName[:idx])
	return ns, tn, fullName[idx+2:], nil
}

func qualify(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "." + name
}
