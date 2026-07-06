// Package bind implements `vmnet bind`: generating idiomatic Go wrapper
// code over a real assembly's or NuGet package's own public API, so a Go
// caller writes `engine.Evaluate("1 + 2")` instead of
// `asm.Call("Namespace.Type", "Method", vmnet.String("1 + 2"))`.
//
// The generator is deliberately conservative about what it can map
// precisely: real .NET method overloading has no Go equivalent, and
// vmnet's own public Value type only has six concrete constructors
// (Int32, Int64, Float32, Float64, String, ByteArray — see
// docs/en/api-stability.md's own frozen surface). A member this
// generator can't map exactly still gets a real, callable Go entry
// point — just a more general one (raw ...vmnet.Value args) instead of
// a precisely-typed signature — so nothing in the real public API is
// silently dropped from the generated package.
package bind

import (
	"fmt"
	"sort"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/ir"
	"github.com/arturoeanton/go-vmnet/internal/metadata"
)

// Raw ECMA-335 TypeAttributes/MethodAttributes bit values this package
// needs — mirrors internal/interpreter/reflection.go's own identical
// constants (kept as a separate copy rather than exported/shared: they
// belong to different layers of the package graph, and the values are
// fixed forever by the CLI spec, not something the two copies could
// ever drift apart on by accident).
const (
	typeAttrVisibilityMask = 0x00000007
	typeAttrPublic         = 0x00000001

	methodAttrMemberAccessMask = 0x0007
	methodAttrPublic           = 0x0006
	methodAttrStatic           = 0x0010
	methodAttrAbstract         = 0x0400
	methodAttrSpecialName      = 0x0800
)

// Model is everything Generate needs to render Go source — built once by
// BuildModel, entirely independent of the template/rendering step so the
// two concerns (what to generate vs. how to format it) stay separate.
type Model struct {
	GoPackage   string
	SourceName  string // the assembly/package name, for the file's own header comment
	Types       []*BoundType
	boundByFull map[string]*BoundType // real "Namespace.Type" -> BoundType, for return-type upgrading
}

// BoundType is one public, top-level class or struct this run will
// generate a Go wrapper for.
type BoundType struct {
	FullName string // real "Namespace.Type"
	GoName   string // real name, sanitized into a valid, exported Go identifier
	IsValue  bool   // a real .NET struct (value type), vs. a class (reference type)
	Ctors    []*BoundMethod
	Instance []*BoundMethod
	Static   []*BoundMethod
}

// BoundMethod is one method (or, for an ambiguous overload group, one
// stand-in covering all of them) this run will generate a Go function/
// method for.
type BoundMethod struct {
	Name       string // the real .NET method name
	GoName     string // sanitized into a valid, exported Go identifier
	Overloaded bool   // 2+ real methods share this Name on this type
	Params     []Param
	Return     *MappedType // nil for a real `void` return
}

// Param is one real method parameter, already mapped to a Go type (or
// left generic when a real, faithful mapping isn't possible).
type Param struct {
	Name string
	Type MappedType
}

// MappedType is one real .NET type's own Go-side representation —
// either a precise, native Go type (GoType, e.g. "int32", "string") with
// a known conversion to/from vmnet.Value, or the honest fallback
// (Generic == true): the caller passes/receives a raw vmnet.Value (for
// a parameter) or gets back a *vmnet.Instance (for a return value),
// unless the real return type happens to be one of THIS run's own bound
// types, in which case BoundAs names the generated wrapper struct to
// return instead of a bare *vmnet.Instance.
type MappedType struct {
	FullName string
	GoType   string
	Kind     string // "int32", "int64", "float32", "float64", "string", "bool", "bytes", "instance", "generic"
	BoundAs  string // non-"" only when Kind == "instance" and FullName matches one of this run's own bound types
}

// BuildModel walks md's own TypeDef table and collects every qualifying
// public, top-level type — real classes/structs only (an interface has
// no constructor and nothing to `New`, so it's out of scope for a
// generated wrapper the same way it would be for a real `new SomeType()`
// call site).
func BuildModel(md *metadata.Metadata, goPackage, sourceName string) (*Model, error) {
	m := &Model{GoPackage: goPackage, SourceName: sourceName, boundByFull: map[string]*BoundType{}}

	typeCount := md.RowCount(metadata.TableTypeDef)
	type rawType struct {
		rid uint32
		row metadata.TypeDefRow
	}
	var candidates []rawType
	for rid := uint32(1); rid <= typeCount; rid++ {
		row, err := md.TypeDef(rid)
		if err != nil {
			continue
		}
		if row.Name == "<Module>" || strings.HasPrefix(row.Name, "<") {
			continue
		}
		if row.Flags&typeAttrVisibilityMask != typeAttrPublic {
			continue // not a public, top-level type (nested types have a different visibility value)
		}
		if isInterfaceOrEnum(md, row) {
			continue
		}
		candidates = append(candidates, rawType{rid: rid, row: row})
	}

	// First pass: register every bound type's own identity before
	// building any method signature, so a return-type lookup against
	// m.boundByFull below can find a type declared later in the table
	// too (real metadata has no guaranteed declaration order relative
	// to use). usedTypeNames deduplicates a real, if rare, collision:
	// two distinct real types whose own names only differ by a
	// character sanitizeIdent collapses to the same thing (e.g. two
	// generic arities of the same name reached through different
	// nesting) would otherwise both try to declare the same Go type —
	// a real compile failure in the generated package, not just an
	// unlikely edge case worth ignoring.
	usedTypeNames := map[string]int{}
	for _, c := range candidates {
		fullName, err := ir.QualifyTypeDefName(md, c.rid, c.row)
		if err != nil {
			continue
		}
		bt := &BoundType{
			FullName: fullName,
			GoName:   dedupeIdent(usedTypeNames, sanitizeIdent(c.row.Name)),
			IsValue:  isValueType(md, c.row),
		}
		m.Types = append(m.Types, bt)
		m.boundByFull[fullName] = bt
	}

	// Second pass: fill in each type's own ctors/methods, now that
	// every OTHER bound type's identity is known for return-type
	// upgrading.
	for i, c := range candidates {
		bt := m.Types[i]
		start, end, err := md.TypeDefMethodRange(c.rid)
		if err != nil {
			continue
		}
		byName := map[string][]metadata.MethodDefRow{}
		var order []string
		for rid := start; rid < end; rid++ {
			row, err := md.MethodDef(rid)
			if err != nil {
				continue
			}
			if row.Flags&methodAttrMemberAccessMask != methodAttrPublic {
				continue
			}
			if row.Flags&methodAttrAbstract != 0 {
				continue // nothing to call without a real instance vmnet can't manufacture generically
			}
			if row.Flags&methodAttrSpecialName != 0 && !strings.HasPrefix(row.Name, "get_") && !strings.HasPrefix(row.Name, "set_") && row.Name != ".ctor" {
				continue // operator overloads (op_*), indexers, and other compiler-magic special names have no clean Go call shape
			}
			if _, ok := byName[row.Name]; !ok {
				order = append(order, row.Name)
			}
			byName[row.Name] = append(byName[row.Name], row)
		}

		usedInstanceNames := map[string]int{}
		usedStaticNames := map[string]int{}
		for _, name := range order {
			rows := byName[name]
			bm, err := buildBoundMethod(md, m, name, rows)
			if err != nil {
				continue
			}
			switch {
			case name == ".ctor":
				bt.Ctors = append(bt.Ctors, bm)
			case rows[0].Flags&methodAttrStatic != 0:
				bm.GoName = dedupeIdent(usedStaticNames, bm.GoName)
				bt.Static = append(bt.Static, bm)
			default:
				bm.GoName = dedupeIdent(usedInstanceNames, bm.GoName)
				bt.Instance = append(bt.Instance, bm)
			}
		}
	}

	sort.Slice(m.Types, func(i, j int) bool { return m.Types[i].FullName < m.Types[j].FullName })
	for _, bt := range m.Types {
		sortMethods(bt.Ctors)
		sortMethods(bt.Instance)
		sortMethods(bt.Static)
	}
	return m, nil
}

func sortMethods(ms []*BoundMethod) {
	sort.Slice(ms, func(i, j int) bool { return ms[i].Name < ms[j].Name })
}

// buildBoundMethod maps one real method name's own overload set to a
// single BoundMethod — a precise, typed signature when there's exactly
// one real overload and every parameter/return type maps cleanly, or a
// generic (...vmnet.Value) stand-in otherwise (2+ overloads, or a
// signature this generator can't map faithfully).
func buildBoundMethod(md *metadata.Metadata, m *Model, name string, rows []metadata.MethodDefRow) (*BoundMethod, error) {
	goName := sanitizeIdent(name)
	switch {
	case name == ".ctor":
		goName = "New"
	case strings.HasPrefix(name, "get_") && len(name) > 4:
		// A real C# auto-property/explicit getter (`get_Name`) reads far
		// more idiomatically as Go's own conventional `GetName` than a
		// literal, underscore-preserving sanitizeIdent("get_Name") would
		// produce ("Get_Name") — special-cased here rather than in
		// sanitizeIdent itself, which stays a generic, name-agnostic
		// character whitelist.
		goName = "Get" + sanitizeIdent(name[4:])
	case strings.HasPrefix(name, "set_") && len(name) > 4:
		goName = "Set" + sanitizeIdent(name[4:])
	}
	bm := &BoundMethod{Name: name, GoName: goName, Overloaded: len(rows) > 1}
	if bm.Overloaded {
		return bm, nil
	}

	sig, err := metadata.ParseMethodSig(rows[0].Signature)
	if err != nil {
		bm.Overloaded = true // can't parse it precisely; fall back to the generic shape rather than fail the whole run
		return bm, nil
	}
	if sig.Generic {
		bm.Overloaded = true // no closed generic-argument story at Go codegen time; same honest fallback
		return bm, nil
	}

	for _, p := range sig.Params {
		mapped, ok := mapSigType(md, m, p)
		if !ok {
			bm.Overloaded = true
			return bm, nil
		}
		bm.Params = append(bm.Params, Param{Type: mapped})
	}
	if sig.RetType.Kind != metadata.SigVoid {
		mapped, ok := mapSigType(md, m, sig.RetType)
		if !ok {
			bm.Overloaded = true
			return bm, nil
		}
		bm.Return = &mapped
	}
	return bm, nil
}

// mapSigType maps one real parameter/return SigType to its Go-side
// representation. ok is false when this generator has no faithful
// mapping at all (an open generic parameter, or a signature type
// ir.SigTypeFullName itself can't name) — the caller falls the whole
// method back to the generic (...vmnet.Value) shape in that case, never
// half-typed.
func mapSigType(md *metadata.Metadata, m *Model, t metadata.SigType) (MappedType, bool) {
	fullName, err := ir.SigTypeFullName(md, t)
	if err != nil || fullName == "" {
		return MappedType{}, false
	}
	switch fullName {
	case "System.Int32":
		return MappedType{FullName: fullName, GoType: "int32", Kind: "int32"}, true
	case "System.Int64":
		return MappedType{FullName: fullName, GoType: "int64", Kind: "int64"}, true
	case "System.Single":
		return MappedType{FullName: fullName, GoType: "float32", Kind: "float32"}, true
	case "System.Double":
		return MappedType{FullName: fullName, GoType: "float64", Kind: "float64"}, true
	case "System.String":
		return MappedType{FullName: fullName, GoType: "string", Kind: "string"}, true
	case "System.Boolean":
		return MappedType{FullName: fullName, GoType: "bool", Kind: "bool"}, true
	case "System.Byte[]":
		return MappedType{FullName: fullName, GoType: "[]byte", Kind: "bytes"}, true
	}
	// A class/struct type this same run is ALSO generating a wrapper
	// for — return/accept the generated wrapper struct itself instead
	// of a bare *vmnet.Instance, the one precise mapping available for
	// a non-primitive type.
	if bt, ok := m.boundByFull[fullName]; ok {
		return MappedType{FullName: fullName, GoType: "*" + bt.GoName, Kind: "instance", BoundAs: bt.GoName}, true
	}
	// Anything else (System.Object, a BCL type this run isn't binding,
	// an open generic parameter that slipped through, ...) — the honest
	// fallback: a raw vmnet.Value in, a raw *vmnet.Instance out. Real
	// and usable, just not precisely typed.
	return MappedType{FullName: fullName, GoType: "vmnet.Value", Kind: "generic"}, true
}

func isInterfaceOrEnum(md *metadata.Metadata, row metadata.TypeDefRow) bool {
	const typeAttrInterface = 0x00000020
	if row.Flags&typeAttrInterface != 0 {
		return true
	}
	return simpleBaseTypeName(md, row.Extends) == "System.Enum"
}

func isValueType(md *metadata.Metadata, row metadata.TypeDefRow) bool {
	return simpleBaseTypeName(md, row.Extends) == "System.ValueType"
}

// simpleBaseTypeName resolves a TypeDef's own Extends coded token to a
// bare "Namespace.Type" name — deliberately narrow (TypeRef/TypeDef
// only, no TypeSpec/generic handling) since the only two real base
// types this package ever needs to recognize (System.Enum, System.
// ValueType) are never generic. "" for a nil Extends (System.Object
// itself, or an interface) or anything this narrow resolver can't
// place.
func simpleBaseTypeName(md *metadata.Metadata, tok metadata.Token) string {
	if tok.IsNil() {
		return ""
	}
	switch tok.Table() {
	case metadata.TableTypeRef:
		if row, err := md.TypeRef(tok.RID()); err == nil {
			if row.Namespace == "" {
				return row.Name
			}
			return row.Namespace + "." + row.Name
		}
	case metadata.TableTypeDef:
		if row, err := md.TypeDef(tok.RID()); err == nil {
			if row.Namespace == "" {
				return row.Name
			}
			return row.Namespace + "." + row.Name
		}
	}
	return ""
}

// dedupeIdent registers name in used (mutating it) and returns a
// guaranteed-unique variant — name itself the first time it's seen,
// name+"2", name+"3", ... on every real collision after that. Small
// enough a real collision essentially never happens in practice, but
// cheap enough to always run rather than trust that assumption.
func dedupeIdent(used map[string]int, name string) string {
	n := used[name]
	used[name] = n + 1
	if n == 0 {
		return name
	}
	return name + fmt.Sprint(n+1)
}

// sanitizeIdent turns a real .NET member/type name into a valid,
// exported Go identifier. Real C# identifiers are already valid Go
// identifiers in the overwhelming majority of cases (letters, digits,
// underscore), but real, compiler-generated or otherwise unusual names
// do turn up in practice (a backtick-arity suffix on a generic type
// name, `+`-nested type names, and — found running this generator
// against real, unmodified Jint@3.1.3 — a literal `$` in at least one
// real compiler-generated member name) — so this is a real whitelist
// (keep only ASCII letters/digits/underscore, drop or replace
// everything else), not a denylist of the handful of characters this
// generator happened to anticipate in advance.
func sanitizeIdent(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			// Every other real character seen in practice (`.`, `+`,
			// `<`, `>`, `,`, `` ` ``, `$`, ...) — collapse to a single
			// underscore rather than dropping it outright, so two
			// otherwise-identical names that only differ in one of
			// these punctuation characters don't collide into the same
			// Go identifier.
			b.WriteRune('_')
		}
	}
	out := b.String()
	if out == "" {
		return "X"
	}
	if out[0] >= '0' && out[0] <= '9' {
		out = "X" + out
	}
	if out[0] >= 'a' && out[0] <= 'z' {
		out = strings.ToUpper(out[:1]) + out[1:]
	}
	return out
}
