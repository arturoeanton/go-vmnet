package vmnet

import (
	"fmt"
	"strings"
	"sync"

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

	retVoid := sig.RetType.Kind == metadata.SigVoid
	irInstrs, err := ir.Build(instrs, asm.md, retVoid)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", fullName, err)
	}

	localCount := 0
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
	}

	m := &runtime.Method{
		FullName:   fullName,
		HasThis:    sig.HasThis,
		HasReturn:  !retVoid,
		ParamCount: int(sig.ParamCount),
		LocalCount: localCount,
		MaxStack:   int(header.MaxStack),
		IR:         irInstrs,
	}
	asm.storeMethod(fullName, m)
	return m, nil
}

// resolveTypeByFullName implements interpreter.TypeResolver: it builds a
// runtime.Type (field layout) for a plain class discovered while executing
// newobj/ldfld/stfld.
//
// Unlike buildMethod, this holds cacheMu for the whole check-build-store
// sequence instead of just the individual reads/writes: since Fase 3.5 a
// Type carries real mutable state (static fields, a .cctor latch), so two
// goroutines racing to resolve the same not-yet-cached type could each
// build and use their own separate *runtime.Type — one goroutine's .cctor
// writes would then be invisible to everyone using the other instance. A
// duplicate *runtime.Method has no such state and stays harmless to build
// twice, so buildMethod is left as check-then-build-then-store.
func (asm *Assembly) resolveTypeByFullName(fullName string) (*runtime.Type, error) {
	asm.cacheMu.Lock()
	defer asm.cacheMu.Unlock()
	if t, ok := asm.types[fullName]; ok {
		return t, nil
	}
	namespace, name := splitTypeName(fullName)
	typeRID, typeDef, err := asm.md.FindTypeDef(namespace, name)
	if err != nil {
		return nil, err
	}

	start, end, err := asm.md.TypeDefFieldRange(typeRID)
	if err != nil {
		return nil, err
	}
	var fields, staticFields []string
	var fieldDefaults, staticFieldDefaults []runtime.Value
	for rid := start; rid < end; rid++ {
		f, err := asm.md.Field(rid)
		if err != nil {
			return nil, err
		}
		def := runtime.Null()
		if sig, err := metadata.ParseFieldSig(f.Signature); err == nil {
			def = fieldDefaultValue(sig.Kind)
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
	asm.types[fullName] = t
	return t, nil
}

// fieldAttrStatic is FieldAttributes.Static (ECMA-335 §II.23.1.5).
const fieldAttrStatic = 0x0010

// fieldDefaultValue maps a field's signature type to its CLR implicit
// zero-init value: a typed numeric zero for value types (so arithmetic on
// a never-explicitly-assigned field works, matching real `static int x;`
// semantics), or Null() for anything reference-shaped (string/class/array/
// pointer) or not modeled here (user-defined value types), which is
// already vmnet's existing null-reference representation.
func fieldDefaultValue(kind metadata.SigTypeKind) runtime.Value {
	switch kind {
	case metadata.SigBoolean, metadata.SigChar,
		metadata.SigI1, metadata.SigU1, metadata.SigI2, metadata.SigU2,
		metadata.SigI4, metadata.SigU4, metadata.SigI, metadata.SigU:
		return runtime.Int32(0)
	case metadata.SigI8, metadata.SigU8:
		return runtime.Int64(0)
	case metadata.SigR4:
		return runtime.Float32(0)
	case metadata.SigR8:
		return runtime.Float64(0)
	default:
		return runtime.Null()
	}
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
