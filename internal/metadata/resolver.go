package metadata

import (
	"fmt"
	"strings"
)

// Typed row accessors for the Fase 1 core table subset (spec §10.2,
// docs/ROADMAP.md). Every other table still parses (tables.go) but is only
// reachable through the untyped Table API for now.

type ModuleRow struct {
	Name string
}

type TypeRefRow struct {
	ResolutionScope Token
	Name            string
	Namespace       string
}

type TypeDefRow struct {
	Flags      uint32
	Name       string
	Namespace  string
	Extends    Token
	FieldList  uint32 // 1-based RID: first row of this type's fields
	MethodList uint32 // 1-based RID: first row of this type's methods
}

type FieldRow struct {
	Flags     uint16
	Name      string
	Signature []byte
}

type MethodDefRow struct {
	RVA       uint32
	ImplFlags uint16
	Flags     uint16
	Name      string
	Signature []byte
	ParamList uint32 // 1-based RID: first row of this method's params
}

type ParamRow struct {
	Flags    uint16
	Sequence uint16
	Name     string
}

type MemberRefRow struct {
	Class     Token
	Name      string
	Signature []byte
}

type ConstantRow struct {
	Type   byte
	Parent Token
	Value  []byte
}

type StandAloneSigRow struct {
	Signature []byte
}

type AssemblyRow struct {
	HashAlgId      uint32
	MajorVersion   uint16
	MinorVersion   uint16
	BuildNumber    uint16
	RevisionNumber uint16
	Flags          uint32
	PublicKey      []byte
	Name           string
	Culture        string
}

type AssemblyRefRow struct {
	MajorVersion     uint16
	MinorVersion     uint16
	BuildNumber      uint16
	RevisionNumber   uint16
	Flags            uint32
	PublicKeyOrToken []byte
	Name             string
	Culture          string
	HashValue        []byte
}

func (md *Metadata) tableOrErr(id TableID, rid uint32) (*Table, uint32, error) {
	t := md.tables[id]
	if t == nil || rid == 0 || rid > t.rowCount {
		return nil, 0, fmt.Errorf("%w: %s RID %d", ErrOutOfRange, tableSchemas[id].name, rid)
	}
	return t, rid - 1, nil
}

func (md *Metadata) Module(rid uint32) (ModuleRow, error) {
	t, row, err := md.tableOrErr(TableModule, rid)
	if err != nil {
		return ModuleRow{}, err
	}
	name, err := md.strings.String(t.col(row, 1))
	if err != nil {
		return ModuleRow{}, err
	}
	return ModuleRow{Name: name}, nil
}

func (md *Metadata) TypeRef(rid uint32) (TypeRefRow, error) {
	t, row, err := md.tableOrErr(TableTypeRef, rid)
	if err != nil {
		return TypeRefRow{}, err
	}
	scope, err := decodeCodedIndex(codedResolutionScope, t.col(row, 0))
	if err != nil {
		return TypeRefRow{}, err
	}
	name, err := md.strings.String(t.col(row, 1))
	if err != nil {
		return TypeRefRow{}, err
	}
	ns, err := md.strings.String(t.col(row, 2))
	if err != nil {
		return TypeRefRow{}, err
	}
	return TypeRefRow{ResolutionScope: scope, Name: name, Namespace: ns}, nil
}

func (md *Metadata) TypeDef(rid uint32) (TypeDefRow, error) {
	t, row, err := md.tableOrErr(TableTypeDef, rid)
	if err != nil {
		return TypeDefRow{}, err
	}
	name, err := md.strings.String(t.col(row, 1))
	if err != nil {
		return TypeDefRow{}, err
	}
	ns, err := md.strings.String(t.col(row, 2))
	if err != nil {
		return TypeDefRow{}, err
	}
	extends, err := decodeCodedIndex(codedTypeDefOrRef, t.col(row, 3))
	if err != nil {
		return TypeDefRow{}, err
	}
	return TypeDefRow{
		Flags:      t.col(row, 0),
		Name:       name,
		Namespace:  ns,
		Extends:    extends,
		FieldList:  t.col(row, 4),
		MethodList: t.col(row, 5),
	}, nil
}

// TypeDefMethodRange returns the [start, end) 1-based MethodDef RID range
// owned by TypeDef rid, per the "next type's MethodList marks the end"
// convention (ECMA-335 §II.22.37).
func (md *Metadata) TypeDefMethodRange(rid uint32) (start, end uint32, err error) {
	t := md.tables[TableTypeDef]
	if t == nil || rid == 0 || rid > t.rowCount {
		return 0, 0, fmt.Errorf("%w: TypeDef RID %d", ErrOutOfRange, rid)
	}
	start = t.col(rid-1, 5)
	if rid < t.rowCount {
		end = t.col(rid, 5)
	} else {
		end = md.RowCount(TableMethodDef) + 1
	}
	return start, end, nil
}

// TypeDefFieldRange returns the [start, end) 1-based Field RID range owned
// by TypeDef rid, mirroring TypeDefMethodRange.
func (md *Metadata) TypeDefFieldRange(rid uint32) (start, end uint32, err error) {
	t := md.tables[TableTypeDef]
	if t == nil || rid == 0 || rid > t.rowCount {
		return 0, 0, fmt.Errorf("%w: TypeDef RID %d", ErrOutOfRange, rid)
	}
	start = t.col(rid-1, 4)
	if rid < t.rowCount {
		end = t.col(rid, 4)
	} else {
		end = md.RowCount(TableField) + 1
	}
	return start, end, nil
}

// FindTypeDef looks up a type by namespace+name, scanning TypeDef rows.
// name may be "+"-qualified (e.g. "LinqTest+<>c", outermost first) for a
// nested type — the round-trip counterpart of qualifyTypeDefName
// (internal/ir/builder.go, root assembly.go), which produces exactly
// this format. This isn't optional: the C# compiler emits one
// non-capturing-lambda cache class (literally named "<>c") PER enclosing
// type that has any, so an assembly can easily have several entirely
// separate TypeDefs sharing the same bare Name (all with an empty
// Namespace, since nested types always do) — a plain Name+Namespace
// match alone can't tell them apart at all (Fase 3.17).
func (md *Metadata) FindTypeDef(namespace, name string) (rid uint32, row TypeDefRow, err error) {
	t := md.tables[TableTypeDef]
	if t == nil {
		return 0, TypeDefRow{}, fmt.Errorf("%w: no TypeDef table", ErrOutOfRange)
	}
	parts := strings.Split(name, "+")
	simpleName := parts[len(parts)-1]
	for i := uint32(1); i <= t.rowCount; i++ {
		r, err := md.TypeDef(i)
		if err != nil {
			return 0, TypeDefRow{}, err
		}
		if r.Name != simpleName {
			continue
		}
		ok, err := md.typeDefMatchesPath(i, namespace, parts)
		if err != nil {
			return 0, TypeDefRow{}, err
		}
		if ok {
			return i, r, nil
		}
	}
	return 0, TypeDefRow{}, fmt.Errorf("%w: type %s.%s not found", ErrOutOfRange, namespace, name)
}

// typeDefMatchesPath confirms TypeDef rid's real enclosing-type chain
// (walked via the NestedClass table) matches parts (outermost first,
// rid's own simple name last) with namespace anchored on the outermost
// enclosing type — the only level with a real, non-empty Namespace
// column (spec §II.22.32: every nested type's own Namespace is always
// "").
func (md *Metadata) typeDefMatchesPath(rid uint32, namespace string, parts []string) (bool, error) {
	enclosingRID, nested, err := md.EnclosingClass(rid)
	if err != nil {
		return false, err
	}
	if !nested {
		if len(parts) != 1 {
			return false, nil
		}
		row, err := md.TypeDef(rid)
		if err != nil {
			return false, err
		}
		return row.Namespace == namespace, nil
	}
	if len(parts) < 2 {
		return false, nil
	}
	enclosingRow, err := md.TypeDef(enclosingRID)
	if err != nil {
		return false, err
	}
	if enclosingRow.Name != parts[len(parts)-2] {
		return false, nil
	}
	return md.typeDefMatchesPath(enclosingRID, namespace, parts[:len(parts)-1])
}

// InterfaceImpls returns the token list of interfaces TypeDef rid directly
// implements (spec §II.22.23) — not interfaces inherited from a base
// class, nor interfaces a directly-implemented interface itself extends;
// see the ancestor walk in internal/interpreter/typecheck.go (Fase 3.7's
// "assembly.go" doc pattern: aggregate at the caller, keep this a plain
// row scan). Class is a simple (uncoded) index — compared directly.
func (md *Metadata) InterfaceImpls(typeRID uint32) ([]Token, error) {
	t := md.tables[TableInterfaceImpl]
	if t == nil {
		return nil, nil
	}
	var out []Token
	for i := uint32(0); i < t.rowCount; i++ {
		if t.col(i, 0) != typeRID {
			continue
		}
		iface, err := decodeCodedIndex(codedTypeDefOrRef, t.col(i, 1))
		if err != nil {
			return nil, err
		}
		out = append(out, iface)
	}
	return out, nil
}

// EnclosingClass returns the TypeDef RID that directly encloses TypeDef
// typeRID (spec §II.22.32's NestedClass table), and false if typeRID is
// not a nested type at all. NestedClass is a simple (uncoded) index pair
// — compared directly, same pattern as InterfaceImpls above.
func (md *Metadata) EnclosingClass(typeRID uint32) (uint32, bool, error) {
	t := md.tables[TableNestedClass]
	if t == nil {
		return 0, false, nil
	}
	for i := uint32(0); i < t.rowCount; i++ {
		if t.col(i, 0) != typeRID {
			continue
		}
		return t.col(i, 1), true, nil
	}
	return 0, false, nil
}

// MethodImplRow is one explicit interface implementation (spec
// §II.22.27): Class overrides/implements MethodDeclaration (a method on a
// base class or, overwhelmingly the common case in practice, an
// interface) via its own MethodBody. The C# compiler emits this whenever
// a class implements an interface method under a different name than the
// interface declares it — always true for one of a pair when a class
// implements both IEnumerable.GetEnumerator (non-generic) and
// IEnumerable<T>.GetEnumerator (generic), since they can't both be a
// plain "GetEnumerator" method (same name, return types alone don't
// overload); the compiler-generated `yield return` state machine is the
// single most common real-world source of this (Fase 3.13).
type MethodImplRow struct {
	MethodBody        Token
	MethodDeclaration Token
}

// MethodImpls returns every MethodImpl row owned by TypeDef typeRID.
// Class is a simple (uncoded) index — compared directly, same pattern as
// InterfaceImpls above.
func (md *Metadata) MethodImpls(typeRID uint32) ([]MethodImplRow, error) {
	t := md.tables[TableMethodImpl]
	if t == nil {
		return nil, nil
	}
	var out []MethodImplRow
	for i := uint32(0); i < t.rowCount; i++ {
		if t.col(i, 0) != typeRID {
			continue
		}
		body, err := decodeCodedIndex(codedMethodDefOrRef, t.col(i, 1))
		if err != nil {
			return nil, err
		}
		decl, err := decodeCodedIndex(codedMethodDefOrRef, t.col(i, 2))
		if err != nil {
			return nil, err
		}
		out = append(out, MethodImplRow{MethodBody: body, MethodDeclaration: decl})
	}
	return out, nil
}

// FieldDefOwner finds which TypeDef owns Field fieldRID, scanning TypeDef
// field ranges (TypeDefFieldRange).
func (md *Metadata) FieldDefOwner(fieldRID uint32) (typeRID uint32, err error) {
	t := md.tables[TableTypeDef]
	if t == nil {
		return 0, fmt.Errorf("%w: no TypeDef table", ErrOutOfRange)
	}
	for rid := uint32(1); rid <= t.rowCount; rid++ {
		start, end, err := md.TypeDefFieldRange(rid)
		if err != nil {
			return 0, err
		}
		if fieldRID >= start && fieldRID < end {
			return rid, nil
		}
	}
	return 0, fmt.Errorf("%w: Field RID %d has no owning TypeDef", ErrOutOfRange, fieldRID)
}

// MethodDefOwner finds which TypeDef owns MethodDef methodRID, scanning
// TypeDef method ranges (TypeDefMethodRange).
func (md *Metadata) MethodDefOwner(methodRID uint32) (typeRID uint32, err error) {
	t := md.tables[TableTypeDef]
	if t == nil {
		return 0, fmt.Errorf("%w: no TypeDef table", ErrOutOfRange)
	}
	for rid := uint32(1); rid <= t.rowCount; rid++ {
		start, end, err := md.TypeDefMethodRange(rid)
		if err != nil {
			return 0, err
		}
		if methodRID >= start && methodRID < end {
			return rid, nil
		}
	}
	return 0, fmt.Errorf("%w: MethodDef RID %d has no owning TypeDef", ErrOutOfRange, methodRID)
}

func (md *Metadata) Field(rid uint32) (FieldRow, error) {
	t, row, err := md.tableOrErr(TableField, rid)
	if err != nil {
		return FieldRow{}, err
	}
	name, err := md.strings.String(t.col(row, 1))
	if err != nil {
		return FieldRow{}, err
	}
	sig, err := md.blob.Blob(t.col(row, 2))
	if err != nil {
		return FieldRow{}, err
	}
	return FieldRow{Flags: uint16(t.col(row, 0)), Name: name, Signature: sig}, nil
}

func (md *Metadata) MethodDef(rid uint32) (MethodDefRow, error) {
	t, row, err := md.tableOrErr(TableMethodDef, rid)
	if err != nil {
		return MethodDefRow{}, err
	}
	name, err := md.strings.String(t.col(row, 3))
	if err != nil {
		return MethodDefRow{}, err
	}
	sig, err := md.blob.Blob(t.col(row, 4))
	if err != nil {
		return MethodDefRow{}, err
	}
	return MethodDefRow{
		RVA:       t.col(row, 0),
		ImplFlags: uint16(t.col(row, 1)),
		Flags:     uint16(t.col(row, 2)),
		Name:      name,
		Signature: sig,
		ParamList: t.col(row, 5),
	}, nil
}

// FindMethodDef looks up a static/instance method by name within the
// method range owned by TypeDef typeRID, returning the first match —
// callers that need to disambiguate a real overload set (same name,
// different signature) should use FindMethodDefCandidates instead (Fase
// 3.27); kept as its own simpler function since the overwhelming
// majority of methods in practice aren't overloaded at all, and every
// existing caller that doesn't have real call-site arguments to
// disambiguate with (resolveExplicitImpl's mangled-name lookups, ...)
// has no better option anyway.
func (md *Metadata) FindMethodDef(typeRID uint32, name string) (rid uint32, row MethodDefRow, err error) {
	start, end, err := md.TypeDefMethodRange(typeRID)
	if err != nil {
		return 0, MethodDefRow{}, err
	}
	for i := start; i < end; i++ {
		r, err := md.MethodDef(i)
		if err != nil {
			return 0, MethodDefRow{}, err
		}
		if r.Name == name {
			return i, r, nil
		}
	}
	return 0, MethodDefRow{}, fmt.Errorf("%w: method %s not found", ErrOutOfRange, name)
}

// FindMethodDefCandidates returns every method matching name within
// TypeDef typeRID's own method range (Fase 3.27) — a real .NET method
// can be overloaded (same name, different signature; discovered the
// hard way running Jint's real Engine class, which has 5 constructors
// and 9 SetValue overloads), which FindMethodDef alone can't
// disambiguate: it always returns whichever happens to come first in
// the metadata table. vmnet's overload resolution (assembly.go's
// pickMethodOverload) scores these against the actual call-site
// arguments.
func (md *Metadata) FindMethodDefCandidates(typeRID uint32, name string) (rids []uint32, rows []MethodDefRow, err error) {
	start, end, err := md.TypeDefMethodRange(typeRID)
	if err != nil {
		return nil, nil, err
	}
	for i := start; i < end; i++ {
		r, err := md.MethodDef(i)
		if err != nil {
			return nil, nil, err
		}
		if r.Name == name {
			rids = append(rids, i)
			rows = append(rows, r)
		}
	}
	if len(rids) == 0 {
		return nil, nil, fmt.Errorf("%w: method %s not found", ErrOutOfRange, name)
	}
	return rids, rows, nil
}

func (md *Metadata) Param(rid uint32) (ParamRow, error) {
	t, row, err := md.tableOrErr(TableParam, rid)
	if err != nil {
		return ParamRow{}, err
	}
	name, err := md.strings.String(t.col(row, 2))
	if err != nil {
		return ParamRow{}, err
	}
	return ParamRow{Flags: uint16(t.col(row, 0)), Sequence: uint16(t.col(row, 1)), Name: name}, nil
}

func (md *Metadata) MemberRef(rid uint32) (MemberRefRow, error) {
	t, row, err := md.tableOrErr(TableMemberRef, rid)
	if err != nil {
		return MemberRefRow{}, err
	}
	class, err := decodeCodedIndex(codedMemberRefParent, t.col(row, 0))
	if err != nil {
		return MemberRefRow{}, err
	}
	name, err := md.strings.String(t.col(row, 1))
	if err != nil {
		return MemberRefRow{}, err
	}
	sig, err := md.blob.Blob(t.col(row, 2))
	if err != nil {
		return MemberRefRow{}, err
	}
	return MemberRefRow{Class: class, Name: name, Signature: sig}, nil
}

func (md *Metadata) Constant(rid uint32) (ConstantRow, error) {
	t, row, err := md.tableOrErr(TableConstant, rid)
	if err != nil {
		return ConstantRow{}, err
	}
	parent, err := decodeCodedIndex(codedHasConstant, t.col(row, 1))
	if err != nil {
		return ConstantRow{}, err
	}
	val, err := md.blob.Blob(t.col(row, 2))
	if err != nil {
		return ConstantRow{}, err
	}
	return ConstantRow{Type: byte(t.col(row, 0)), Parent: parent, Value: val}, nil
}

// TypeSpecSignature returns the raw signature blob for a TypeSpec row
// (e.g. a generic instantiation like List<int>) — parse it with
// ParseTypeSpec.
func (md *Metadata) TypeSpecSignature(rid uint32) ([]byte, error) {
	t, row, err := md.tableOrErr(TableTypeSpec, rid)
	if err != nil {
		return nil, err
	}
	return md.blob.Blob(t.col(row, 0))
}

// MethodSpecRow is a generic method instantiation, e.g. the
// `Guard.Against.Null<T>` in a call to `Guard.Against.Null<string>(...)`.
// Method is the underlying (non-generic-instantiated) MethodDef/MemberRef
// token; Instantiation is the raw GenericMethodInstantiation signature
// blob (type arguments) — unused by vmnet's resolver, since a native
// runtime.Value is already type-erased (see internal/ir/builder.go).
type MethodSpecRow struct {
	Method        Token
	Instantiation []byte
}

func (md *Metadata) MethodSpec(rid uint32) (MethodSpecRow, error) {
	t, row, err := md.tableOrErr(TableMethodSpec, rid)
	if err != nil {
		return MethodSpecRow{}, err
	}
	method, err := decodeCodedIndex(codedMethodDefOrRef, t.col(row, 0))
	if err != nil {
		return MethodSpecRow{}, err
	}
	inst, err := md.blob.Blob(t.col(row, 1))
	if err != nil {
		return MethodSpecRow{}, err
	}
	return MethodSpecRow{Method: method, Instantiation: inst}, nil
}

func (md *Metadata) StandAloneSig(rid uint32) (StandAloneSigRow, error) {
	t, row, err := md.tableOrErr(TableStandAloneSig, rid)
	if err != nil {
		return StandAloneSigRow{}, err
	}
	sig, err := md.blob.Blob(t.col(row, 0))
	if err != nil {
		return StandAloneSigRow{}, err
	}
	return StandAloneSigRow{Signature: sig}, nil
}

func (md *Metadata) Assembly(rid uint32) (AssemblyRow, error) {
	t, row, err := md.tableOrErr(TableAssembly, rid)
	if err != nil {
		return AssemblyRow{}, err
	}
	pk, err := md.blob.Blob(t.col(row, 6))
	if err != nil {
		return AssemblyRow{}, err
	}
	name, err := md.strings.String(t.col(row, 7))
	if err != nil {
		return AssemblyRow{}, err
	}
	culture, err := md.strings.String(t.col(row, 8))
	if err != nil {
		return AssemblyRow{}, err
	}
	return AssemblyRow{
		HashAlgId:      t.col(row, 0),
		MajorVersion:   uint16(t.col(row, 1)),
		MinorVersion:   uint16(t.col(row, 2)),
		BuildNumber:    uint16(t.col(row, 3)),
		RevisionNumber: uint16(t.col(row, 4)),
		Flags:          t.col(row, 5),
		PublicKey:      pk,
		Name:           name,
		Culture:        culture,
	}, nil
}

func (md *Metadata) AssemblyRef(rid uint32) (AssemblyRefRow, error) {
	t, row, err := md.tableOrErr(TableAssemblyRef, rid)
	if err != nil {
		return AssemblyRefRow{}, err
	}
	pk, err := md.blob.Blob(t.col(row, 5))
	if err != nil {
		return AssemblyRefRow{}, err
	}
	name, err := md.strings.String(t.col(row, 6))
	if err != nil {
		return AssemblyRefRow{}, err
	}
	culture, err := md.strings.String(t.col(row, 7))
	if err != nil {
		return AssemblyRefRow{}, err
	}
	hash, err := md.blob.Blob(t.col(row, 8))
	if err != nil {
		return AssemblyRefRow{}, err
	}
	return AssemblyRefRow{
		MajorVersion:     uint16(t.col(row, 0)),
		MinorVersion:     uint16(t.col(row, 1)),
		BuildNumber:      uint16(t.col(row, 2)),
		RevisionNumber:   uint16(t.col(row, 3)),
		Flags:            t.col(row, 4),
		PublicKeyOrToken: pk,
		Name:             name,
		Culture:          culture,
		HashValue:        hash,
	}, nil
}
