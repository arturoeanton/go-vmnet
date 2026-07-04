package metadata

import (
	"encoding/binary"
	"fmt"
	"math"
	"unicode/utf16"
)

// fieldAttrLiteral is FieldAttributes.Literal (ECMA-335 §II.23.1.5) — set
// on every enum member and any other C# `const` field. Duplicated from
// the root package's own fieldAttrLiteral (assembly.go): this package
// can't import it (internal/ dependency direction goes the other way),
// same reasoning as every other small constant duplicated across the
// package boundary in this project.
const fieldAttrLiteral = 0x0040

// constantForField finds the Constant table row backing fieldRID's
// compile-time value (ECMA-335 §II.22.9) — every enum member and C#
// `const` field has exactly one, found by a linear scan over the (usually
// small) Constant table matching Parent against this field's own token.
// There's no index from Field RID to Constant RID in the metadata format
// itself (System.Reflection.Metadata computes one lazily too), and vmnet
// only calls this per-enum, not per-field-access, so a scan is fine.
func (md *Metadata) constantForField(fieldRID uint32) (ConstantRow, bool, error) {
	n := md.RowCount(TableConstant)
	for rid := uint32(1); rid <= n; rid++ {
		row, err := md.Constant(rid)
		if err != nil {
			return ConstantRow{}, false, err
		}
		if row.Parent.Table() == TableField && row.Parent.RID() == fieldRID {
			return row, true, nil
		}
	}
	return ConstantRow{}, false, nil
}

// decodeConstantInt64 decodes a Constant blob (ECMA-335 §II.23.2.16) whose
// Type tag is one of the fixed-width integer/boolean/char element types —
// the only shapes an enum member's value ever takes (its underlying type
// is always an integral primitive, spec §I.8.5.2).
func decodeConstantInt64(tag byte, blob []byte) (int64, error) {
	switch tag {
	case elementBoolean, elementI1:
		if len(blob) < 1 {
			return 0, fmt.Errorf("metadata: truncated constant (i1)")
		}
		return int64(int8(blob[0])), nil
	case elementU1:
		if len(blob) < 1 {
			return 0, fmt.Errorf("metadata: truncated constant (u1)")
		}
		return int64(blob[0]), nil
	case elementChar, elementI2:
		if len(blob) < 2 {
			return 0, fmt.Errorf("metadata: truncated constant (i2)")
		}
		return int64(int16(binary.LittleEndian.Uint16(blob))), nil
	case elementU2:
		if len(blob) < 2 {
			return 0, fmt.Errorf("metadata: truncated constant (u2)")
		}
		return int64(binary.LittleEndian.Uint16(blob)), nil
	case elementI4:
		if len(blob) < 4 {
			return 0, fmt.Errorf("metadata: truncated constant (i4)")
		}
		return int64(int32(binary.LittleEndian.Uint32(blob))), nil
	case elementU4:
		if len(blob) < 4 {
			return 0, fmt.Errorf("metadata: truncated constant (u4)")
		}
		return int64(binary.LittleEndian.Uint32(blob)), nil
	case elementI8, elementU8:
		if len(blob) < 8 {
			return 0, fmt.Errorf("metadata: truncated constant (i8)")
		}
		return int64(binary.LittleEndian.Uint64(blob)), nil
	default:
		return 0, fmt.Errorf("metadata: unsupported enum constant type tag %#x", tag)
	}
}

// ConstantKind identifies which of ConstantForField's return values is
// meaningful for a given literal (Fase 3.39). ConstantInt32/ConstantInt64
// are split rather than one "ConstantInt" so a genuine `const long`
// doesn't get silently truncated by a caller that only asked for "is
// this an integer" and wrapped it as int32.
type ConstantKind byte

const (
	ConstantInt32 ConstantKind = iota
	ConstantInt64
	ConstantFloat
	ConstantString
	ConstantNull
)

// ConstantForField returns fieldRID's compile-time literal value (Fase
// 3.39) — every C# `const` field and enum member (FieldAttributes.
// Literal) has exactly one Constant table row. Decoded by the Constant
// row's own type tag, never the field's declared *signature* type —
// this is what lets it handle an enum member (whose signature names the
// enum's own, not-yet-fully-built TypeDef) with no risk of the
// self-referential recursion buildType's own literal-field handling
// documents avoiding: the Constant table always stores an enum member's
// value with a plain integer type tag matching its underlying type, the
// signature is never consulted here at all. ok=false means fieldRID has
// no Constant row (not every field is literal).
func (md *Metadata) ConstantForField(fieldRID uint32) (kind ConstantKind, i int64, f float64, s string, ok bool, err error) {
	row, found, err := md.constantForField(fieldRID)
	if err != nil || !found {
		return 0, 0, 0, "", false, err
	}
	switch row.Type {
	case elementR4:
		if len(row.Value) < 4 {
			return 0, 0, 0, "", false, fmt.Errorf("metadata: truncated constant (r4)")
		}
		return ConstantFloat, 0, float64(math.Float32frombits(binary.LittleEndian.Uint32(row.Value))), "", true, nil
	case elementR8:
		if len(row.Value) < 8 {
			return 0, 0, 0, "", false, fmt.Errorf("metadata: truncated constant (r8)")
		}
		return ConstantFloat, 0, math.Float64frombits(binary.LittleEndian.Uint64(row.Value)), "", true, nil
	case elementString:
		u16 := make([]uint16, len(row.Value)/2)
		for i := range u16 {
			u16[i] = binary.LittleEndian.Uint16(row.Value[i*2:])
		}
		return ConstantString, 0, 0, string(utf16.Decode(u16)), true, nil
	case elementClass:
		// A null reference constant (`const string s = null;`, or any
		// other reference-typed const) — the CLI encodes it as a
		// zero-length blob tagged ELEMENT_TYPE_CLASS (ECMA-335 §II.23.2.16).
		return ConstantNull, 0, 0, "", true, nil
	case elementI8, elementU8:
		n, err := decodeConstantInt64(row.Type, row.Value)
		if err != nil {
			return 0, 0, 0, "", false, err
		}
		return ConstantInt64, n, 0, "", true, nil
	default:
		n, err := decodeConstantInt64(row.Type, row.Value)
		if err != nil {
			return 0, 0, 0, "", false, err
		}
		return ConstantInt32, n, 0, "", true, nil
	}
}

// EnumMembers reads a TypeDef's literal static fields (its enum members,
// e.g. Red/Yellow/Green on `enum TrafficLight`) in declaration order,
// resolving each one's real value from the Constant table — the
// TrafficLight fixture's own `value__` field is skipped (not literal, no
// Constant row); only Fase 3.25's fieldAttrLiteral check protected against
// recursing into it as a value-typed field, this is the other half: doing
// something useful with what that check skips.
func (md *Metadata) EnumMembers(typeRID uint32) (names []string, values []int64, err error) {
	start, end, err := md.TypeDefFieldRange(typeRID)
	if err != nil {
		return nil, nil, err
	}
	for rid := start; rid < end; rid++ {
		f, err := md.Field(rid)
		if err != nil {
			return nil, nil, err
		}
		if f.Flags&fieldAttrLiteral == 0 {
			continue
		}
		row, ok, err := md.constantForField(rid)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			continue
		}
		v, err := decodeConstantInt64(row.Type, row.Value)
		if err != nil {
			return nil, nil, err
		}
		names = append(names, f.Name)
		values = append(values, v)
	}
	return names, values, nil
}
