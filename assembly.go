package vmnet

import (
	"fmt"
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

	cacheMu sync.RWMutex
	methods map[string]*runtime.Method // keyed by "Namespace.Type::Method"
	types   map[string]*runtime.Type   // keyed by "Namespace.Type"
}

func (asm *Assembly) cachedMethod(fullName string) (*runtime.Method, bool) {
	asm.cacheMu.RLock()
	defer asm.cacheMu.RUnlock()
	m, ok := asm.methods[fullName]
	return m, ok
}

func (asm *Assembly) storeMethod(fullName string, m *runtime.Method) {
	asm.cacheMu.Lock()
	defer asm.cacheMu.Unlock()
	asm.methods[fullName] = m
}

// Name returns the name Assembly was loaded with (the file's base name for
// LoadFile, or the caller-supplied name for LoadBytes).
func (asm *Assembly) Name() string { return asm.name }

func (asm *Assembly) resolveMethod(typeName, methodName string) (*runtime.Method, error) {
	namespace, name := splitTypeName(typeName)
	typeRID, _, err := asm.md.FindTypeDef(namespace, name)
	if err != nil {
		return nil, err
	}
	methodRID, row, err := asm.md.FindMethodDef(typeRID, methodName)
	if err != nil {
		return nil, err
	}
	return asm.buildMethod(methodRID, row)
}

// resolveByFullName implements interpreter.Resolver for local (non-BCL)
// calls discovered while executing another method's IR.
func (asm *Assembly) resolveByFullName(fullName string) (*runtime.Method, error) {
	if m, ok := asm.cachedMethod(fullName); ok {
		return m, nil
	}
	namespace, typeName, methodName, err := splitFullName(fullName)
	if err != nil {
		return nil, err
	}
	typeRID, _, err := asm.md.FindTypeDef(namespace, typeName)
	if err != nil {
		return nil, err
	}
	methodRID, row, err := asm.md.FindMethodDef(typeRID, methodName)
	if err != nil {
		return nil, err
	}
	return asm.buildMethod(methodRID, row)
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
func (asm *Assembly) resolveExplicitImpl(concreteTypeFullName, interfaceFullName, methodName string) (string, bool) {
	namespace, name := splitTypeName(concreteTypeFullName)
	typeRID, _, err := asm.md.FindTypeDef(namespace, name)
	if err != nil {
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
		return qualify(owner.Namespace, owner.Name), row.Name, nil
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
	typeRID, err := asm.md.MethodDefOwner(methodRID)
	if err != nil {
		return nil, err
	}
	typeDef, err := asm.md.TypeDef(typeRID)
	if err != nil {
		return nil, err
	}
	fullName := qualify(typeDef.Namespace, typeDef.Name) + "::" + row.Name

	if m, ok := asm.cachedMethod(fullName); ok {
		return m, nil
	}

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
			def, err := asm.fieldOrLocalDefault(l)
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
	}
	asm.storeMethod(fullName, m)
	return m, nil
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
	if t, ok := asm.cachedType(fullName); ok {
		return t, nil
	}
	t, err := asm.buildType(fullName)
	if err != nil {
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

func (asm *Assembly) buildType(fullName string) (*runtime.Type, error) {
	namespace, name := splitTypeName(fullName)
	typeRID, typeDef, err := asm.md.FindTypeDef(namespace, name)
	if err != nil {
		return nil, err
	}

	isValueType, err := asm.isValueType(typeDef)
	if err != nil {
		return nil, err
	}

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
		if sig, err := metadata.ParseFieldSig(f.Signature); err == nil {
			def, err = asm.fieldOrLocalDefault(sig)
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
	t.BaseTypeFullName = baseName

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

// isValueType reports whether typeDef is a struct (extends
// System.ValueType) or an enum (extends System.Enum, itself a
// System.ValueType) rather than a plain class. Interfaces and
// System.Object itself have no Extends entry at all.
func (asm *Assembly) isValueType(typeDef metadata.TypeDefRow) (bool, error) {
	if typeDef.Extends.IsNil() {
		return false, nil
	}
	name, err := resolveTypeTokenName(asm.md, typeDef.Extends)
	if err != nil {
		// A base type vmnet can't resolve (e.g. a TypeSpec-encoded base,
		// vanishingly rare) isn't a value type as far as we can tell —
		// treat it as a class rather than failing type resolution outright.
		return false, nil
	}
	return name == "System.ValueType" || name == "System.Enum", nil
}

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
		return qualify(row.Namespace, row.Name), nil
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

// fieldOrLocalDefault maps a field's or local's signature type to its CLR
// implicit zero-init value (spec: fields via beforefieldinit/allocation,
// locals via the InitLocals flag C# always sets — see
// runtime.Method.LocalDefaults): a typed numeric zero for numeric-kind
// value types (so arithmetic on a never-explicitly-assigned field/local
// works, matching real `static int x;`/`int x;` semantics), a real
// zero-valued struct for a value type with fields (Fase 3.7), or Null()
// for anything reference-shaped or unresolvable.
func (asm *Assembly) fieldOrLocalDefault(sig metadata.SigType) (runtime.Value, error) {
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
		return asm.valueTypeDefault(sig.Token), nil
	case metadata.SigGenericInst:
		if sig.GenericInstIsValueType {
			return asm.valueTypeDefault(sig.Token), nil
		}
		return runtime.Null(), nil
	default:
		return runtime.Null(), nil
	}
}

// valueTypeDefault resolves tok (a TypeDef/TypeRef naming a value type) to
// a zero-valued runtime.Struct: a native BCL value type (Nullable`1, ...)
// via bcl.LookupValueType, else a plugin's own struct via
// resolveTypeByFullName (which may recurse here again for a nested
// struct field — safe, see resolveTypeByFullName's doc comment). A type
// vmnet can't resolve at all (a foreign BCL struct it doesn't model, e.g.
// DateTime) falls back to Null() rather than failing the whole field/
// local's type resolution over it — consistent with how an unresolvable
// Call target only errors when actually invoked, not at load time.
func (asm *Assembly) valueTypeDefault(tok metadata.Token) runtime.Value {
	name, err := resolveTypeTokenName(asm.md, tok)
	if err != nil {
		return runtime.Null()
	}
	if t, ok := bcl.LookupValueType(name); ok {
		return runtime.StructVal(runtime.NewStruct(t))
	}
	t, err := asm.resolveTypeByFullName(name)
	if err != nil || !t.IsValueType {
		return runtime.Null()
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
