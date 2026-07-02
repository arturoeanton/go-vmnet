package metadata

import "encoding/binary"

// TableID is a metadata table identifier (ECMA-335 §II.22).
type TableID byte

const (
	TableModule                 TableID = 0x00
	TableTypeRef                TableID = 0x01
	TableTypeDef                TableID = 0x02
	TableFieldPtr               TableID = 0x03
	TableField                  TableID = 0x04
	TableMethodPtr              TableID = 0x05
	TableMethodDef              TableID = 0x06
	TableParamPtr               TableID = 0x07
	TableParam                  TableID = 0x08
	TableInterfaceImpl          TableID = 0x09
	TableMemberRef              TableID = 0x0A
	TableConstant               TableID = 0x0B
	TableCustomAttribute        TableID = 0x0C
	TableFieldMarshal           TableID = 0x0D
	TableDeclSecurity           TableID = 0x0E
	TableClassLayout            TableID = 0x0F
	TableFieldLayout            TableID = 0x10
	TableStandAloneSig          TableID = 0x11
	TableEventMap               TableID = 0x12
	TableEventPtr               TableID = 0x13
	TableEvent                  TableID = 0x14
	TablePropertyMap            TableID = 0x15
	TablePropertyPtr            TableID = 0x16
	TableProperty               TableID = 0x17
	TableMethodSemantics        TableID = 0x18
	TableMethodImpl             TableID = 0x19
	TableModuleRef              TableID = 0x1A
	TableTypeSpec               TableID = 0x1B
	TableImplMap                TableID = 0x1C
	TableFieldRVA               TableID = 0x1D
	TableENCLog                 TableID = 0x1E
	TableENCMap                 TableID = 0x1F
	TableAssembly               TableID = 0x20
	TableAssemblyProcessor      TableID = 0x21
	TableAssemblyOS             TableID = 0x22
	TableAssemblyRef            TableID = 0x23
	TableAssemblyRefProcessor   TableID = 0x24
	TableAssemblyRefOS          TableID = 0x25
	TableFile                   TableID = 0x26
	TableExportedType           TableID = 0x27
	TableManifestResource       TableID = 0x28
	TableNestedClass            TableID = 0x29
	TableGenericParam           TableID = 0x2A
	TableMethodSpec             TableID = 0x2B
	TableGenericParamConstraint TableID = 0x2C
)

type colKind byte

const (
	colU16 colKind = iota
	colU32
	colStringHeap
	colGUIDHeap
	colBlobHeap
	colSimple
	colCoded
)

type column struct {
	name  string
	kind  colKind
	table TableID        // for colSimple
	coded codedIndexKind // for colCoded
}

type tableSchema struct {
	name    string
	columns []column
}

// tableSchemas defines every table's column layout so vmnet can compute row
// sizes and skip tables it doesn't yet model without failing to parse real
// assemblies (spec §10.2). Typed row accessors (resolver.go) only cover the
// Fase 1 core subset; the rest still parse correctly, just untyped.
var tableSchemas = map[TableID]tableSchema{
	TableModule: {"Module", []column{
		{name: "Generation", kind: colU16},
		{name: "Name", kind: colStringHeap},
		{name: "Mvid", kind: colGUIDHeap},
		{name: "EncId", kind: colGUIDHeap},
		{name: "EncBaseId", kind: colGUIDHeap},
	}},
	TableTypeRef: {"TypeRef", []column{
		{name: "ResolutionScope", kind: colCoded, coded: codedResolutionScope},
		{name: "Name", kind: colStringHeap},
		{name: "Namespace", kind: colStringHeap},
	}},
	TableTypeDef: {"TypeDef", []column{
		{name: "Flags", kind: colU32},
		{name: "Name", kind: colStringHeap},
		{name: "Namespace", kind: colStringHeap},
		{name: "Extends", kind: colCoded, coded: codedTypeDefOrRef},
		{name: "FieldList", kind: colSimple, table: TableField},
		{name: "MethodList", kind: colSimple, table: TableMethodDef},
	}},
	TableFieldPtr: {"FieldPtr", []column{
		{name: "Field", kind: colSimple, table: TableField},
	}},
	TableField: {"Field", []column{
		{name: "Flags", kind: colU16},
		{name: "Name", kind: colStringHeap},
		{name: "Signature", kind: colBlobHeap},
	}},
	TableMethodPtr: {"MethodPtr", []column{
		{name: "Method", kind: colSimple, table: TableMethodDef},
	}},
	TableMethodDef: {"MethodDef", []column{
		{name: "RVA", kind: colU32},
		{name: "ImplFlags", kind: colU16},
		{name: "Flags", kind: colU16},
		{name: "Name", kind: colStringHeap},
		{name: "Signature", kind: colBlobHeap},
		{name: "ParamList", kind: colSimple, table: TableParam},
	}},
	TableParamPtr: {"ParamPtr", []column{
		{name: "Param", kind: colSimple, table: TableParam},
	}},
	TableParam: {"Param", []column{
		{name: "Flags", kind: colU16},
		{name: "Sequence", kind: colU16},
		{name: "Name", kind: colStringHeap},
	}},
	TableInterfaceImpl: {"InterfaceImpl", []column{
		{name: "Class", kind: colSimple, table: TableTypeDef},
		{name: "Interface", kind: colCoded, coded: codedTypeDefOrRef},
	}},
	TableMemberRef: {"MemberRef", []column{
		{name: "Class", kind: colCoded, coded: codedMemberRefParent},
		{name: "Name", kind: colStringHeap},
		{name: "Signature", kind: colBlobHeap},
	}},
	TableConstant: {"Constant", []column{
		{name: "Type", kind: colU16}, // 1-byte type tag + 1-byte padding
		{name: "Parent", kind: colCoded, coded: codedHasConstant},
		{name: "Value", kind: colBlobHeap},
	}},
	TableCustomAttribute: {"CustomAttribute", []column{
		{name: "Parent", kind: colCoded, coded: codedHasCustomAttribute},
		{name: "Type", kind: colCoded, coded: codedCustomAttributeType},
		{name: "Value", kind: colBlobHeap},
	}},
	TableFieldMarshal: {"FieldMarshal", []column{
		{name: "Parent", kind: colCoded, coded: codedHasFieldMarshal},
		{name: "NativeType", kind: colBlobHeap},
	}},
	TableDeclSecurity: {"DeclSecurity", []column{
		{name: "Action", kind: colU16},
		{name: "Parent", kind: colCoded, coded: codedHasDeclSecurity},
		{name: "PermissionSet", kind: colBlobHeap},
	}},
	TableClassLayout: {"ClassLayout", []column{
		{name: "PackingSize", kind: colU16},
		{name: "ClassSize", kind: colU32},
		{name: "Parent", kind: colSimple, table: TableTypeDef},
	}},
	TableFieldLayout: {"FieldLayout", []column{
		{name: "Offset", kind: colU32},
		{name: "Field", kind: colSimple, table: TableField},
	}},
	TableStandAloneSig: {"StandAloneSig", []column{
		{name: "Signature", kind: colBlobHeap},
	}},
	TableEventMap: {"EventMap", []column{
		{name: "Parent", kind: colSimple, table: TableTypeDef},
		{name: "EventList", kind: colSimple, table: TableEvent},
	}},
	TableEventPtr: {"EventPtr", []column{
		{name: "Event", kind: colSimple, table: TableEvent},
	}},
	TableEvent: {"Event", []column{
		{name: "EventFlags", kind: colU16},
		{name: "Name", kind: colStringHeap},
		{name: "EventType", kind: colCoded, coded: codedTypeDefOrRef},
	}},
	TablePropertyMap: {"PropertyMap", []column{
		{name: "Parent", kind: colSimple, table: TableTypeDef},
		{name: "PropertyList", kind: colSimple, table: TableProperty},
	}},
	TablePropertyPtr: {"PropertyPtr", []column{
		{name: "Property", kind: colSimple, table: TableProperty},
	}},
	TableProperty: {"Property", []column{
		{name: "Flags", kind: colU16},
		{name: "Name", kind: colStringHeap},
		{name: "Type", kind: colBlobHeap},
	}},
	TableMethodSemantics: {"MethodSemantics", []column{
		{name: "Semantics", kind: colU16},
		{name: "Method", kind: colSimple, table: TableMethodDef},
		{name: "Association", kind: colCoded, coded: codedHasSemantics},
	}},
	TableMethodImpl: {"MethodImpl", []column{
		{name: "Class", kind: colSimple, table: TableTypeDef},
		{name: "MethodBody", kind: colCoded, coded: codedMethodDefOrRef},
		{name: "MethodDeclaration", kind: colCoded, coded: codedMethodDefOrRef},
	}},
	TableModuleRef: {"ModuleRef", []column{
		{name: "Name", kind: colStringHeap},
	}},
	TableTypeSpec: {"TypeSpec", []column{
		{name: "Signature", kind: colBlobHeap},
	}},
	TableImplMap: {"ImplMap", []column{
		{name: "MappingFlags", kind: colU16},
		{name: "MemberForwarded", kind: colCoded, coded: codedMemberForwarded},
		{name: "ImportName", kind: colStringHeap},
		{name: "ImportScope", kind: colSimple, table: TableModuleRef},
	}},
	TableFieldRVA: {"FieldRVA", []column{
		{name: "RVA", kind: colU32},
		{name: "Field", kind: colSimple, table: TableField},
	}},
	TableENCLog: {"ENCLog", []column{
		{name: "Token", kind: colU32},
		{name: "FuncCode", kind: colU32},
	}},
	TableENCMap: {"ENCMap", []column{
		{name: "Token", kind: colU32},
	}},
	TableAssembly: {"Assembly", []column{
		{name: "HashAlgId", kind: colU32},
		{name: "MajorVersion", kind: colU16},
		{name: "MinorVersion", kind: colU16},
		{name: "BuildNumber", kind: colU16},
		{name: "RevisionNumber", kind: colU16},
		{name: "Flags", kind: colU32},
		{name: "PublicKey", kind: colBlobHeap},
		{name: "Name", kind: colStringHeap},
		{name: "Culture", kind: colStringHeap},
	}},
	TableAssemblyProcessor: {"AssemblyProcessor", []column{
		{name: "Processor", kind: colU32},
	}},
	TableAssemblyOS: {"AssemblyOS", []column{
		{name: "OSPlatformID", kind: colU32},
		{name: "OSMajorVersion", kind: colU32},
		{name: "OSMinorVersion", kind: colU32},
	}},
	TableAssemblyRef: {"AssemblyRef", []column{
		{name: "MajorVersion", kind: colU16},
		{name: "MinorVersion", kind: colU16},
		{name: "BuildNumber", kind: colU16},
		{name: "RevisionNumber", kind: colU16},
		{name: "Flags", kind: colU32},
		{name: "PublicKeyOrToken", kind: colBlobHeap},
		{name: "Name", kind: colStringHeap},
		{name: "Culture", kind: colStringHeap},
		{name: "HashValue", kind: colBlobHeap},
	}},
	TableAssemblyRefProcessor: {"AssemblyRefProcessor", []column{
		{name: "Processor", kind: colU32},
		{name: "AssemblyRef", kind: colSimple, table: TableAssemblyRef},
	}},
	TableAssemblyRefOS: {"AssemblyRefOS", []column{
		{name: "OSPlatformID", kind: colU32},
		{name: "OSMajorVersion", kind: colU32},
		{name: "OSMinorVersion", kind: colU32},
		{name: "AssemblyRef", kind: colSimple, table: TableAssemblyRef},
	}},
	TableFile: {"File", []column{
		{name: "Flags", kind: colU32},
		{name: "Name", kind: colStringHeap},
		{name: "HashValue", kind: colBlobHeap},
	}},
	TableExportedType: {"ExportedType", []column{
		{name: "Flags", kind: colU32},
		{name: "TypeDefId", kind: colU32},
		{name: "TypeName", kind: colStringHeap},
		{name: "TypeNamespace", kind: colStringHeap},
		{name: "Implementation", kind: colCoded, coded: codedImplementation},
	}},
	TableManifestResource: {"ManifestResource", []column{
		{name: "Offset", kind: colU32},
		{name: "Flags", kind: colU32},
		{name: "Name", kind: colStringHeap},
		{name: "Implementation", kind: colCoded, coded: codedImplementation},
	}},
	TableNestedClass: {"NestedClass", []column{
		{name: "NestedClass", kind: colSimple, table: TableTypeDef},
		{name: "EnclosingClass", kind: colSimple, table: TableTypeDef},
	}},
	TableGenericParam: {"GenericParam", []column{
		{name: "Number", kind: colU16},
		{name: "Flags", kind: colU16},
		{name: "Owner", kind: colCoded, coded: codedTypeOrMethodDef},
		{name: "Name", kind: colStringHeap},
	}},
	TableMethodSpec: {"MethodSpec", []column{
		{name: "Method", kind: colCoded, coded: codedMethodDefOrRef},
		{name: "Instantiation", kind: colBlobHeap},
	}},
	TableGenericParamConstraint: {"GenericParamConstraint", []column{
		{name: "Owner", kind: colSimple, table: TableGenericParam},
		{name: "Constraint", kind: colCoded, coded: codedTypeDefOrRef},
	}},
}

func columnWidth(c column, hs heapSizes, rowCounts map[TableID]uint32) uint32 {
	switch c.kind {
	case colU16:
		return 2
	case colU32:
		return 4
	case colStringHeap:
		if hs.wideStrings {
			return 4
		}
		return 2
	case colGUIDHeap:
		if hs.wideGUID {
			return 4
		}
		return 2
	case colBlobHeap:
		if hs.wideBlob {
			return 4
		}
		return 2
	case colSimple:
		if rowCounts[c.table] > 0xFFFF {
			return 4
		}
		return 2
	case colCoded:
		bits := codedIndexTagBits(c.coded)
		threshold := uint32(1) << (16 - bits)
		if codedIndexMaxRows(c.coded, rowCounts) >= threshold {
			return 4
		}
		return 2
	}
	return 0
}

// Table is one parsed metadata table: raw row bytes plus the column
// offsets/widths needed to decode any row without re-walking the schema.
type Table struct {
	id         TableID
	schema     tableSchema
	rowCount   uint32
	rowSize    uint32
	data       []byte
	colOffsets []uint32
	colWidths  []uint32
}

// RowCount returns how many rows this table has.
func (t *Table) RowCount() uint32 { return t.rowCount }

// col reads column idx of the given 0-based row.
func (t *Table) col(row uint32, idx int) uint32 {
	off := row*t.rowSize + t.colOffsets[idx]
	w := t.colWidths[idx]
	b := t.data[off : off+w]
	if w == 2 {
		return uint32(binary.LittleEndian.Uint16(b))
	}
	return binary.LittleEndian.Uint32(b)
}
