package metadata

import (
	"errors"
	"fmt"
	"strings"
)

// Typed row accessors for the Fase 1 core table subset (spec §10.2,
// docs/en/ROADMAP.md). Every other table still parses (tables.go) but is only
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

// ManifestResourceRow (ECMA-335 §II.22.24) — a real .NET assembly can
// embed arbitrary named byte blobs (e.g. an icon, a template, a font)
// directly in its own PE image, retrieved at runtime via Assembly.
// GetManifestResourceStream(name) (Fase 3.40). Implementation is the
// nil Token (Table()==0, RID()==0) for the overwhelmingly common case —
// a resource embedded in THIS file — since Offset then indexes directly
// into this assembly's own CLI Resources data directory; a non-nil
// Implementation (another File/AssemblyRef/ExportedType) means the
// resource actually lives in a different file entirely, unsupported here.
type ManifestResourceRow struct {
	Offset         uint32
	Flags          uint32
	Name           string
	Implementation Token
}

type MemberRefRow struct {
	Class     Token
	Name      string
	Signature []byte
}

// PropertyRow (ECMA-335 §II.22.34) — a C# auto-property or explicit
// `{ get; set; }` property compiles to a real Property table row (Name +
// PropertySig blob) PLUS the underlying get_Xxx/set_Xxx MethodDefs a
// MethodSemantics row (below) links back to it; the row itself carries no
// getter/setter reference directly. Reflection.PropertyInfo (Fase 3.51)
// is the first real consumer.
type PropertyRow struct {
	Flags     uint16
	Name      string
	Signature []byte
}

func (md *Metadata) Property(rid uint32) (PropertyRow, error) {
	t, row, err := md.tableOrErr(TableProperty, rid)
	if err != nil {
		return PropertyRow{}, err
	}
	name, err := md.strings.String(t.col(row, 1))
	if err != nil {
		return PropertyRow{}, err
	}
	sig, err := md.blob.Blob(t.col(row, 2))
	if err != nil {
		return PropertyRow{}, err
	}
	return PropertyRow{Flags: uint16(t.col(row, 0)), Name: name, Signature: sig}, nil
}

// TypeDefPropertyRange returns the [start, end) 1-based Property RID
// range owned by TypeDef rid — unlike TypeDefFieldRange/
// TypeDefMethodRange, there's no direct column on TypeDef itself for
// this: PropertyMap is a separate, SPARSE table (one row only for a type
// that actually declares at least one property, not one per TypeDef at
// all), indirected through exactly like EventMap's own Event linkage.
// Real metadata always keeps PropertyMap rows in increasing Parent order
// (ECMA-335 §II.22.35's own row-ordering constraint every real compiler
// honors), so "the next PropertyMap row's PropertyList" is a safe range
// end here as much as it is for TypeDefFieldRange's direct TypeDef
// column case. Returns start==end==0, not an error, for a type with no
// PropertyMap row at all (the overwhelmingly common case: most types
// declare no properties).
func (md *Metadata) TypeDefPropertyRange(typeDefRID uint32) (start, end uint32, err error) {
	t := md.tables[TablePropertyMap]
	if t == nil {
		return 0, 0, nil
	}
	for row := uint32(0); row < t.rowCount; row++ {
		if t.col(row, 0) != typeDefRID {
			continue
		}
		start = t.col(row, 1)
		if row+1 < t.rowCount {
			end = t.col(row+1, 1)
		} else {
			end = md.RowCount(TableProperty) + 1
		}
		return start, end, nil
	}
	return 0, 0, nil
}

// PropertyAccessors returns propertyRID's linked get_Xxx/set_Xxx MethodDef
// RIDs (0 if that property has no getter, or no setter — a get-only or
// set-only property is common), found by scanning MethodSemantics for a
// Property-tagged Association matching propertyRID. MethodSemantics.
// Semantics flags: 0x1 Setter, 0x2 Getter, 0x4 Other (an add/remove/raise
// accessor on an Event, never seen for a Property — ignored here).
func (md *Metadata) PropertyAccessors(propertyRID uint32) (getterRID, setterRID uint32, err error) {
	t := md.tables[TableMethodSemantics]
	if t == nil {
		return 0, 0, nil
	}
	for row := uint32(0); row < t.rowCount; row++ {
		semantics := t.col(row, 0)
		assoc, err := decodeCodedIndex(codedHasSemantics, t.col(row, 2))
		if err != nil {
			return 0, 0, err
		}
		if assoc.Table() != TableProperty || assoc.RID() != propertyRID {
			continue
		}
		method := t.col(row, 1)
		switch {
		case semantics&0x2 != 0:
			getterRID = method
		case semantics&0x1 != 0:
			setterRID = method
		}
	}
	return getterRID, setterRID, nil
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
	key := namespace + "\x00" + name
	md.typeDefCacheMu.RLock()
	if e, ok := md.typeDefCache[key]; ok {
		md.typeDefCacheMu.RUnlock()
		if !e.found {
			return 0, TypeDefRow{}, fmt.Errorf("%w: type %s.%s not found", ErrOutOfRange, namespace, name)
		}
		return e.rid, e.row, nil
	}
	md.typeDefCacheMu.RUnlock()

	rid, row, err = md.findTypeDefUncached(namespace, name)

	md.typeDefCacheMu.Lock()
	if md.typeDefCache == nil {
		md.typeDefCache = make(map[string]typeDefCacheEntry)
	}
	// A non-ErrOutOfRange err (e.g. a malformed string-heap index) means
	// the scan itself failed, not that the type is absent — don't cache
	// that as a miss, since a transient/environmental read could differ
	// on retry in a way "not found" never does.
	if err == nil {
		md.typeDefCache[key] = typeDefCacheEntry{rid: rid, row: row, found: true}
	} else if errors.Is(err, ErrOutOfRange) {
		md.typeDefCache[key] = typeDefCacheEntry{found: false}
	}
	md.typeDefCacheMu.Unlock()

	return rid, row, err
}

func (md *Metadata) findTypeDefUncached(namespace, name string) (rid uint32, row TypeDefRow, err error) {
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

// MethodDefParamRange returns the [start, end) 1-based Param RID range
// owned by MethodDef rid, mirroring TypeDefFieldRange/TypeDefMethodRange
// (same "next row's own range-start column is this row's range end"
// trick, valid here for the same reason: real metadata always keeps
// Param rows in increasing owning-method order). Used by
// System.Reflection.MethodBase.GetParameters (Fase 3.52) to read a
// method's real declared parameter names — Param rows are optional (a
// method can have zero even with real declared parameters, if no
// parameter has a name/attributes/marshaling), but every parameter a
// normal C# compiler emits always gets one, so this is reliable for real
// source-compiled methods like Dapper's own.
func (md *Metadata) MethodDefParamRange(rid uint32) (start, end uint32, err error) {
	t := md.tables[TableMethodDef]
	if t == nil || rid == 0 || rid > t.rowCount {
		return 0, 0, fmt.Errorf("%w: MethodDef RID %d", ErrOutOfRange, rid)
	}
	start = t.col(rid-1, 5)
	if rid < t.rowCount {
		end = t.col(rid, 5)
	} else {
		end = md.RowCount(TableParam) + 1
	}
	return start, end, nil
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

// ManifestResource reads a ManifestResource table row by RID.
func (md *Metadata) ManifestResource(rid uint32) (ManifestResourceRow, error) {
	t, row, err := md.tableOrErr(TableManifestResource, rid)
	if err != nil {
		return ManifestResourceRow{}, err
	}
	name, err := md.strings.String(t.col(row, 2))
	if err != nil {
		return ManifestResourceRow{}, err
	}
	impl, err := decodeCodedIndex(codedImplementation, t.col(row, 3))
	if err != nil {
		return ManifestResourceRow{}, err
	}
	return ManifestResourceRow{
		Offset:         t.col(row, 0),
		Flags:          t.col(row, 1),
		Name:           name,
		Implementation: impl,
	}, nil
}

// FindManifestResource looks up a ManifestResource row by exact name —
// a linear scan over the (typically tiny) ManifestResource table, same
// reasoning as constantForField's own scan.
func (md *Metadata) FindManifestResource(name string) (ManifestResourceRow, bool, error) {
	n := md.RowCount(TableManifestResource)
	for rid := uint32(1); rid <= n; rid++ {
		row, err := md.ManifestResource(rid)
		if err != nil {
			return ManifestResourceRow{}, false, err
		}
		if row.Name == name {
			return row, true, nil
		}
	}
	return ManifestResourceRow{}, false, nil
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
