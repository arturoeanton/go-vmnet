package metadata

import "fmt"

// codedIndexKind identifies one of the coded-index column encodings
// defined in ECMA-335 §II.24.2.6.
type codedIndexKind byte

const (
	codedTypeDefOrRef codedIndexKind = iota
	codedHasConstant
	codedHasCustomAttribute
	codedHasFieldMarshal
	codedHasDeclSecurity
	codedMemberRefParent
	codedHasSemantics
	codedMethodDefOrRef
	codedMemberForwarded
	codedImplementation
	codedCustomAttributeType
	codedResolutionScope
	codedTypeOrMethodDef
)

// tableInvalid marks an unused tag slot within a coded index's target list
// (e.g. CustomAttributeType only actually targets MethodDef/MemberRef).
const tableInvalid TableID = 0xFF

// codedIndexTargets lists each coded index kind's target tables in tag
// order (tag 0 = targets[0], tag 1 = targets[1], ...).
var codedIndexTargets = map[codedIndexKind][]TableID{
	codedTypeDefOrRef: {TableTypeDef, TableTypeRef, TableTypeSpec},
	codedHasConstant:  {TableField, TableParam, TableProperty},
	codedHasCustomAttribute: {
		TableMethodDef, TableField, TableTypeRef, TableTypeDef, TableParam,
		TableInterfaceImpl, TableMemberRef, TableModule, TableDeclSecurity,
		TableProperty, TableEvent, TableStandAloneSig, TableModuleRef,
		TableTypeSpec, TableAssembly, TableAssemblyRef, TableFile,
		TableExportedType, TableManifestResource, TableGenericParam,
		TableGenericParamConstraint, TableMethodSpec,
	},
	codedHasFieldMarshal: {TableField, TableParam},
	codedHasDeclSecurity: {TableTypeDef, TableMethodDef, TableAssembly},
	codedMemberRefParent: {TableTypeDef, TableTypeRef, TableModuleRef, TableMethodDef, TableTypeSpec},
	codedHasSemantics:    {TableEvent, TableProperty},
	codedMethodDefOrRef:  {TableMethodDef, TableMemberRef},
	codedMemberForwarded: {TableField, TableMethodDef},
	codedImplementation:  {TableFile, TableAssemblyRef, TableExportedType},
	codedCustomAttributeType: {
		tableInvalid, tableInvalid, TableMethodDef, TableMemberRef, tableInvalid,
	},
	codedResolutionScope: {TableModule, TableModuleRef, TableAssemblyRef, TableTypeRef},
	codedTypeOrMethodDef: {TableTypeDef, TableMethodDef},
}

func codedIndexTagBits(kind codedIndexKind) uint {
	n := len(codedIndexTargets[kind])
	bits := uint(0)
	for (1 << bits) < n {
		bits++
	}
	return bits
}

// decodeCodedIndex splits a raw coded-index column value into a Token.
func decodeCodedIndex(kind codedIndexKind, raw uint32) (Token, error) {
	bits := codedIndexTagBits(kind)
	tag := raw & ((1 << bits) - 1)
	rid := raw >> bits

	targets := codedIndexTargets[kind]
	if int(tag) >= len(targets) || targets[tag] == tableInvalid {
		return 0, fmt.Errorf("metadata: invalid coded index tag %d for kind %d", tag, kind)
	}
	return NewToken(targets[tag], rid), nil
}

// codedIndexMaxRows returns the largest row count among a coded index's
// target tables, used to decide whether the column needs 2 or 4 bytes.
func codedIndexMaxRows(kind codedIndexKind, rowCounts map[TableID]uint32) uint32 {
	var max uint32
	for _, tbl := range codedIndexTargets[kind] {
		if tbl == tableInvalid {
			continue
		}
		if c := rowCounts[tbl]; c > max {
			max = c
		}
	}
	return max
}
