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
// initial Fields — see internal/interpreter's InitObj handling.
func NewStruct(t *Type) *Struct {
	fields := make([]Value, len(t.FieldDefaults))
	copy(fields, t.FieldDefaults)
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
