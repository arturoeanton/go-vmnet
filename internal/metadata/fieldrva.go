package metadata

// FieldRVA returns the RVA a Field's FieldRVA table row records (ECMA-335
// §II.22.18) — a compiler-emitted initial-value blob for a static field
// (Fase 3.27, e.g. a large embedded `byte[]`/`ReadOnlySpan<byte>` literal
// the compiler stores as raw bytes in the PE image rather than
// constructing at runtime — found running real third-party code,
// Esprima's Character.s_characterData). Linear scan: FieldRVA is
// normally tiny (one row per RVA-initialized static field, not per
// field in the assembly), same reasoning as constantForField.
func (md *Metadata) FieldRVA(fieldRID uint32) (rva uint32, ok bool, err error) {
	n := md.RowCount(TableFieldRVA)
	for rid := uint32(1); rid <= n; rid++ {
		t, row, err := md.tableOrErr(TableFieldRVA, rid)
		if err != nil {
			return 0, false, err
		}
		if t.col(row, 1) == fieldRID {
			return t.col(row, 0), true, nil
		}
	}
	return 0, false, nil
}

// ClassLayout returns a TypeDef's declared byte size, if it has a
// ClassLayout row (ECMA-335 §II.22.8) — used to know how many bytes a
// FieldRVA-backed field's embedded blob actually is (Fase 3.27): the
// Field/FieldRVA rows alone only give a starting address, not a length,
// so this is essential, not optional, to reading the blob correctly.
func (md *Metadata) ClassLayout(typeRID uint32) (size uint32, ok bool, err error) {
	n := md.RowCount(TableClassLayout)
	for rid := uint32(1); rid <= n; rid++ {
		t, row, err := md.tableOrErr(TableClassLayout, rid)
		if err != nil {
			return 0, false, err
		}
		if t.col(row, 2) == typeRID {
			return t.col(row, 1), true, nil
		}
	}
	return 0, false, nil
}
