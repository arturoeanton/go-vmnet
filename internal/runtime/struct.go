package runtime

// Struct is a value-type (struct) instance: Type.IsValueType is true, and
// Fields holds its field values by position (same indexing as Type.Fields
// — a value type reuses the class field-layout machinery, just with copy
// semantics instead of shared-reference semantics).
//
// Copying: unlike Object (a heap reference every alias shares), a value
// type instance must be an independent copy every time it lands in a new
// storage slot (a local, an argument, a field, an array element, the far
// end of a managed pointer write) — see Value.Clone, called at every such
// site in internal/interpreter.
type Struct struct {
	Type   *Type
	Fields []Value
}

// NewStruct builds a zero-valued (default(T)) instance of a value type,
// seeding each field from t.FieldDefaults exactly like a class instance's
// initial Fields — see internal/interpreter's InitObj handling. Each
// default is Clone()'d, not just copied: a plain copy() only duplicates
// the runtime.Value struct itself, and for a KindStruct default (a
// nested value-type field, e.g. Esprima's AdditionalDataSlot embedded in
// every AST node) that Value's Struct field is a pointer — every
// instance built from the same t.FieldDefaults would otherwise share the
// exact same *Struct, so a field write through one instance leaks into
// every other instance of the type until first overwritten. Found the
// hard way running real Jint/Esprima (Fase 3.27): two distinct Literal
// AST nodes ended up sharing one AdditionalDataSlot, so caching Jint's
// compiled expression for the "1" literal made "2" spuriously read back
// as "1" too (`1 + 2` evaluated to 2).
func NewStruct(t *Type) *Struct {
	fields := make([]Value, len(t.FieldDefaults))
	for i, def := range t.FieldDefaults {
		fields[i] = def.Clone()
	}
	return &Struct{Type: t, Fields: fields}
}

// Clone returns v unchanged for every Kind except KindStruct, where it
// returns a Value wrapping an independent *Struct with its own Fields
// slice (each field itself Clone()'d, so a struct nested inside a struct
// copies correctly too). Call this at every point a Value is written into
// a persistent slot — see the site list in internal/interpreter/eval.go's
// StoreLocal/StoreArg/StoreField/StoreStaticField/StoreElem/StoreIndirect
// and Machine.invoke's argument setup.
func (v Value) Clone() Value {
	if v.Kind != KindStruct || v.Struct == nil {
		return v
	}
	fields := make([]Value, len(v.Struct.Fields))
	for i, f := range v.Struct.Fields {
		fields[i] = f.Clone()
	}
	return Value{Kind: KindStruct, Struct: &Struct{Type: v.Struct.Type, Fields: fields}}
}
