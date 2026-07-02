package runtime

import "sync"

// Type is a minimal runtime type descriptor: just enough to allocate
// instances and resolve field names to slots. Fase 2 only needs this for
// plain classes with instance fields — no base-type field inheritance,
// interfaces or generics modeling yet (a class's own Fields already
// include everything the CIL compiler emits for it, including
// compiler-generated auto-property backing fields).
//
// Static fields (Fase 3.5) live on the Type itself rather than on any one
// Object, since unlike instance fields they're genuinely shared, mutable
// state across every caller — including concurrent goroutines calling the
// same *Assembly (see docs/ROADMAP.md Fase 2.5). statics is guarded by
// staticsMu; cctorOnce ensures the type initializer runs exactly once
// however many goroutines race to trigger it.
type Type struct {
	Namespace    string
	Name         string
	Fields       []string // instance fields, declaration order, matches Object.Fields index
	StaticFields []string // static fields, declaration order, matches statics index

	// IsValueType marks a struct (extends System.ValueType/System.Enum in
	// its TypeDef, or one of vmnet's synthetic BCL value types like
	// Nullable`1) — Fase 3.7. Instances are runtime.Struct, copied by
	// Value.Clone at every persistent-slot write, instead of runtime.Object
	// (a shared heap reference).
	IsValueType bool

	// BaseTypeFullName ("" if none — interfaces and System.Object itself)
	// and Interfaces (directly implemented only, not transitively expanded
	// — spec §II.22.23) back isinst/castclass's real type-hierarchy walk
	// (Fase 3.8, internal/interpreter/typecheck.go), replacing the flat
	// "every class is unrelated to every other" model Fase 1-3.7 got away
	// with because nothing needed to ask "is A a B".
	BaseTypeFullName string
	Interfaces       []string

	// FieldDefaults/StaticFieldDefaults hold default(T) for each field
	// (parallel to Fields/StaticFields) — a typed zero (Int32(0), Int64(0),
	// ...) for value-typed fields, or Null() for reference-typed ones
	// (string/class/array/pointer). This matters for real code: a field
	// never explicitly assigned (e.g. `static int Counter;`, relying on the
	// CLR's implicit zero-init) must still support arithmetic, not carry
	// the untyped Value{} zero, which mismatches every numeric Kind.
	FieldDefaults       []Value
	StaticFieldDefaults []Value

	statics   []Value
	staticsMu sync.RWMutex
	cctorOnce sync.Once
	cctorErr  error
}

// NewType constructs a Type with its static storage allocated, seeded from
// staticFieldDefaults until the type initializer, if any, overwrites it.
func NewType(namespace, name string, fields, staticFields []string, fieldDefaults, staticFieldDefaults []Value) *Type {
	statics := make([]Value, len(staticFields))
	copy(statics, staticFieldDefaults)
	return &Type{
		Namespace:           namespace,
		Name:                name,
		Fields:              fields,
		StaticFields:        staticFields,
		FieldDefaults:       fieldDefaults,
		StaticFieldDefaults: staticFieldDefaults,
		statics:             statics,
	}
}

// NewValueType constructs a struct Type descriptor: no static fields (a
// value type's own statics are exceedingly rare in practice and unneeded
// by anything vmnet models today — user-defined structs with statics
// would still resolve via NewType, this is only for the synthetic BCL
// value types internal/bcl registers, like Nullable`1).
func NewValueType(namespace, name string, fields []string, fieldDefaults []Value) *Type {
	t := NewType(namespace, name, fields, nil, fieldDefaults, nil)
	t.IsValueType = true
	return t
}

// FieldIndex returns the slot for an instance field name, or -1 if Type
// has no such field.
func (t *Type) FieldIndex(name string) int {
	return indexOf(t.Fields, name)
}

// StaticFieldIndex returns the slot for a static field name, or -1.
func (t *Type) StaticFieldIndex(name string) int {
	return indexOf(t.StaticFields, name)
}

func indexOf(names []string, name string) int {
	for i, f := range names {
		if f == name {
			return i
		}
	}
	return -1
}

// StaticField reads static field idx.
func (t *Type) StaticField(idx int) Value {
	t.staticsMu.RLock()
	defer t.staticsMu.RUnlock()
	return t.statics[idx]
}

// SetStaticField writes static field idx.
func (t *Type) SetStaticField(idx int, v Value) {
	t.staticsMu.Lock()
	defer t.staticsMu.Unlock()
	t.statics[idx] = v
}

// EnsureCctor runs fn — the type initializer (.cctor) invocation — at
// most once for this Type, no matter how many goroutines call it
// concurrently (spec's "beforefieldinit" semantics, simplified: vmnet runs
// it lazily on first static access rather than eagerly). fn itself reading
// or writing this same Type's statics is fine — interpreter.Machine tracks
// same-call-chain reentrancy so that doesn't re-enter this Once (see
// internal/interpreter/statics.go); only a *different* goroutine racing to
// access this Type mid-initialization blocks here, on the Once itself.
func (t *Type) EnsureCctor(fn func() error) error {
	t.cctorOnce.Do(func() { t.cctorErr = fn() })
	return t.cctorErr
}
