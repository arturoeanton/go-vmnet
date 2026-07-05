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
// same *Assembly (see docs/en/ROADMAP.md Fase 2.5). statics is guarded by
// staticsMu; cctorOnce ensures the type initializer runs exactly once
// however many goroutines race to trigger it.
type Type struct {
	Namespace    string
	Name         string
	Fields       []string // instance fields, declaration order, matches Object.Fields index
	StaticFields []string // static fields, declaration order, matches statics index

	// QualifiedName is the real "+"-nested full name (e.g.
	// "LinqTest+<>c") when this Type is a nested type — "" for a
	// top-level type or a synthetic BCL value type, where Namespace+"."+
	// Name (or just Name) is already correct and unambiguous. Set once,
	// at construction (assembly.go's buildType); anything reconstructing
	// a Type's full name (fullTypeName, internal/interpreter/
	// typecheck.go) must prefer this over Namespace+Name whenever it's
	// non-empty — two different nested types (most commonly the
	// compiler's own per-enclosing-type "<>c" lambda cache class) can
	// share the exact same bare Name/Namespace (Fase 3.17).
	QualifiedName string

	// IsValueType marks a struct (extends System.ValueType/System.Enum in
	// its TypeDef, or one of vmnet's synthetic BCL value types like
	// Nullable`1) — Fase 3.7. Instances are runtime.Struct, copied by
	// Value.Clone at every persistent-slot write, instead of runtime.Object
	// (a shared heap reference).
	IsValueType bool

	// IsEnum marks a TypeDef that extends System.Enum specifically (a
	// subset of IsValueType — every enum is also a value type, but not
	// every value type is an enum). IsInterface marks a TypeDef declared
	// with the TypeAttributes.Interface flag. Both Fase 3.25 (Type
	// reflection): isAssignableTo/typeMatches (Fase 3.8) never needed this
	// distinction (an enum's underlying storage and identity checks work
	// the same as any other value type), but System.Type.IsEnum/
	// IsInterface do.
	IsEnum      bool
	IsInterface bool

	// IsAbstract marks a TypeDef declared with the TypeAttributes.Abstract
	// flag (Fase 3.39, System.Type.IsAbstract) — an interface is always
	// abstract too in real reflection terms, but this only tracks the
	// class-level flag itself; callers needing "is this abstract in the
	// broader sense" should check IsInterface as well.
	IsAbstract bool

	// BaseTypeFullName ("" if none — interfaces and System.Object itself)
	// and Interfaces (directly implemented only, not transitively expanded
	// — spec §II.22.23) back isinst/castclass's real type-hierarchy walk
	// (Fase 3.8, internal/interpreter/typecheck.go), replacing the flat
	// "every class is unrelated to every other" model Fase 1-3.7 got away
	// with because nothing needed to ask "is A a B".
	BaseTypeFullName string
	Interfaces       []string

	// BaseTypeGenericArgs holds BaseTypeFullName's own real closed
	// generic type argument names, SEPARATELY from BaseTypeFullName
	// itself (which stays the bare open name — every existing consumer
	// of it, e.g. the virtual-dispatch ancestor walk and field
	// inheritance, resolves it straight back into a TypeDef and would
	// break if it suddenly carried a "[[...]]" suffix). nil when the
	// base isn't a generic instantiation at all. An entry may be a "!N"
	// sentinel (ir.NewObj.ClassGenericArgs's own encoding) when the base
	// is instantiated over THIS type's own still-open generic parameter
	// (e.g. `class DefaultClassMap<TClass> : ClassMap<TClass>`) —
	// resolved against a receiver's own real ClassGenericArgs only at
	// Type.BaseType's own call time (internal/interpreter/reflection.go's
	// own typeGetBaseType), Fase 3.66.
	BaseTypeGenericArgs []string

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

// StaticFieldAddr returns a raw pointer to static field idx's storage
// slot, for ldsflda (Fase 3.27) — e.g. a lazy-initialization pattern
// passing a static field by ref (`LoadData(ref s_cachedData)`), found
// running real third-party code (Esprima's Character.s_characterData).
// Deliberately bypasses staticsMu: a real CLR pointer from ldsflda has
// no built-in synchronization either — whatever the caller does with it
// (an Interlocked.CompareExchange, a lock, or just single-threaded lazy
// init) is on them, same as StaticField's readers/SetStaticField's
// writers racing a raw pointer write would be regardless.
func (t *Type) StaticFieldAddr(idx int) *Value {
	return &t.statics[idx]
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
