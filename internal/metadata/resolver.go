package metadata

import "fmt"

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
func (md *Metadata) FindTypeDef(namespace, name string) (rid uint32, row TypeDefRow, err error) {
	t := md.tables[TableTypeDef]
	if t == nil {
		return 0, TypeDefRow{}, fmt.Errorf("%w: no TypeDef table", ErrOutOfRange)
	}
	for i := uint32(1); i <= t.rowCount; i++ {
		r, err := md.TypeDef(i)
		if err != nil {
			return 0, TypeDefRow{}, err
		}
		if r.Name == name && r.Namespace == namespace {
			return i, r, nil
		}
	}
	return 0, TypeDefRow{}, fmt.Errorf("%w: type %s.%s not found", ErrOutOfRange, namespace, name)
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
// method range owned by TypeDef typeRID.
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
