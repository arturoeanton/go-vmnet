package vmnet

import (
	"fmt"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/il"
	"github.com/arturoeanton/go-vmnet/internal/ir"
	"github.com/arturoeanton/go-vmnet/internal/metadata"
	"github.com/arturoeanton/go-vmnet/internal/pe"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// Assembly is a loaded .NET assembly, ready to have its methods called.
type Assembly struct {
	name string
	file *pe.File
	md   *metadata.Metadata

	methods map[string]*runtime.Method // keyed by "Namespace.Type::Method"
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
	if m, ok := asm.methods[fullName]; ok {
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

	if m, ok := asm.methods[fullName]; ok {
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
	asm.methods[fullName] = m
	return m, nil
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
