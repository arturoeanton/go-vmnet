// Package bcl implements the partial Base Class Library (System.*) vmnet
// ships natively in Go: types and methods that are not interpreted from IL
// but registered as native implementations (e.g. System.Math, System.String,
// System.Collections.Generic.List[T]). Coverage grows by profile — see
// docs/en/ROADMAP.md Fase 1-3 and docs/en/spec.md section 16.
package bcl

import "github.com/arturoeanton/go-vmnet/internal/runtime"

// Native is a BCL method implemented directly in Go. args holds exactly
// the arguments the IL call site pushed (including an implicit `this` as
// args[0] for instance calls) — the interpreter does the popping.
type Native func(args []runtime.Value) (runtime.Value, error)

type entry struct {
	fn        Native
	hasReturn bool
}

var registry = map[string]entry{}

func register(fullName string, hasReturn bool, fn Native) {
	registry[fullName] = entry{fn: fn, hasReturn: hasReturn}
}

// Lookup returns the native registered for fullName ("Namespace.Type::Method").
func Lookup(fullName string) (fn Native, hasReturn bool, ok bool) {
	e, ok := registry[fullName]
	return e.fn, e.hasReturn, ok
}

// NativeCtor is a BCL constructor implemented directly in Go: it allocates
// and returns the new object rather than mutating one handed to it, since
// (unlike a normal call) there's no `this` yet when newobj runs.
type NativeCtor func(args []runtime.Value) (*runtime.Object, error)

var ctorRegistry = map[string]NativeCtor{}

func registerCtor(typeFullName string, fn NativeCtor) {
	ctorRegistry[typeFullName] = fn
}

// LookupCtor returns the native constructor registered for a type's full
// name ("Namespace.Type"), if any.
func LookupCtor(typeFullName string) (fn NativeCtor, ok bool) {
	fn, ok = ctorRegistry[typeFullName]
	return fn, ok
}

// valueTypeRegistry holds the synthetic runtime.Type descriptors for BCL
// value types vmnet models natively (Nullable`1, ...) — these have no
// TypeDef in a loaded assembly (they live in a system assembly vmnet never
// parses), so unlike a plugin's own structs they can't be resolved via
// Assembly.resolveTypeByFullName. Fase 3.7.
var valueTypeRegistry = map[string]*runtime.Type{}

func registerValueType(t *runtime.Type) {
	fullName := t.Name
	if t.Namespace != "" {
		fullName = t.Namespace + "." + t.Name
	}
	valueTypeRegistry[fullName] = t
}

// LookupValueType returns the synthetic Type descriptor for a native BCL
// value type by full name ("Namespace.Type"), if any — used by
// interpreter.Machine's initobj handling (internal/interpreter/structs.go).
func LookupValueType(typeFullName string) (*runtime.Type, bool) {
	t, ok := valueTypeRegistry[typeFullName]
	return t, ok
}

// NativeValueTypeCtor is a BCL value-type constructor: unlike NativeCtor
// (always builds a *runtime.Object), it builds a *runtime.Struct directly,
// since `newobj` on a value type pushes the value itself rather than a
// heap reference (spec §III.4.21).
type NativeValueTypeCtor func(args []runtime.Value) (*runtime.Struct, error)

var valueTypeCtorRegistry = map[string]NativeValueTypeCtor{}

func registerValueTypeCtor(typeFullName string, fn NativeValueTypeCtor) {
	valueTypeCtorRegistry[typeFullName] = fn
}

// LookupValueTypeCtor returns the native constructor registered for a BCL
// value type's full name, if any.
func LookupValueTypeCtor(typeFullName string) (fn NativeValueTypeCtor, ok bool) {
	fn, ok = valueTypeCtorRegistry[typeFullName]
	return fn, ok
}

// staticFieldHostRegistry holds synthetic runtime.Type descriptors for
// BCL types that need real static-field storage (ldsfld/stsfld) despite
// NOT being value types (Fase 3.27) — found running real Jint/Esprima
// code: `string.Empty` compiles to `ldsfld System.String::Empty`, but
// System.String is a KindString primitive-shaped Value in vmnet (Fase
// 1), never a KindStruct, so it can't go through valueTypeRegistry
// (LookupValueType) the way TimeSpan.Zero did (Fase 3.23) — critically,
// classifyTypeByName (Fase 3.25, Type.IsValueType) treats anything found
// in valueTypeRegistry as a real value type, which System.String is not.
// A separate, narrower registry keeps that classification correct while
// still giving resolveTypeByFullName somewhere to find "Empty"'s slot.
var staticFieldHostRegistry = map[string]*runtime.Type{}

func registerStaticFieldHost(t *runtime.Type) {
	fullName := t.Name
	if t.Namespace != "" {
		fullName = t.Namespace + "." + t.Name
	}
	staticFieldHostRegistry[fullName] = t
}

// LookupStaticFieldHost returns the synthetic Type descriptor for a BCL
// type's static-field storage (ldsfld/stsfld), if any — a narrower
// sibling of LookupValueType for reference-shaped BCL types (Fase 3.27,
// e.g. System.String::Empty).
func LookupStaticFieldHost(typeFullName string) (*runtime.Type, bool) {
	t, ok := staticFieldHostRegistry[typeFullName]
	return t, ok
}
