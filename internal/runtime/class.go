package runtime

// Type is a minimal runtime type descriptor: just enough to allocate
// instances and resolve field names to slots. Fase 2 only needs this for
// plain classes with instance fields — no base-type field inheritance,
// interfaces or generics modeling yet (a class's own Fields already
// include everything the CIL compiler emits for it, including
// compiler-generated auto-property backing fields).
type Type struct {
	Namespace string
	Name      string
	Fields    []string // declaration order, matches Object.Fields index
}

// FieldIndex returns the slot for a field name, or -1 if Type has no such
// field.
func (t *Type) FieldIndex(name string) int {
	for i, f := range t.Fields {
		if f == name {
			return i
		}
	}
	return -1
}
