package runtime

import (
	"errors"

	"github.com/arturoeanton/go-vmnet/internal/ir"
)

// ErrMethodNotFound is what a Resolvers.Resolve implementation should
// wrap its error with when fullName genuinely names no method at all
// (Fase 3.27) — as opposed to a method that exists but failed to build
// (an unsupported opcode somewhere in its own IL body, a malformed
// signature, ...), a categorically different, real failure. Distinguishing
// the two matters most for a type's .cctor: internal/interpreter/
// statics.go's runCctor must silently skip a genuinely absent
// constructor (the overwhelmingly common case — most types have none)
// but MUST NOT silently skip one that exists and simply couldn't be
// built, since that constructor may have already been about to set real
// static state (e.g. a static delegate field) before whatever made it
// fail — silently treating "exists but broken" as "doesn't exist" would
// leave that state at its zero value with no error at all, exactly the
// "plausible-but-wrong" failure mode this project treats as worse than a
// hard error (found running real Jint/Esprima: Character's .cctor sets
// three delegate fields before hitting an unsupported opcode later in
// the same method, and swallowing the build error silently left them
// null instead of surfacing the real problem).
var ErrMethodNotFound = errors.New("runtime: method not found")

// Method is a resolved, IR-lowered CIL method ready to execute — the
// interpreter never touches il.Instruction or metadata.Token directly.
type Method struct {
	FullName   string // "Namespace.Type::Method"
	HasThis    bool
	HasReturn  bool
	ParamCount int
	LocalCount int
	MaxStack   int
	IR         []ir.Instr

	// LocalDefaults holds each local's default(T) (parallel to
	// LocalCount), seeded into the frame before the method body runs — the
	// CLR's InitLocals guarantee (ECMA-335 §II.25.4.4: C# always sets it),
	// which real compiled code relies on: a struct local can be
	// constructed via `ldloca` + `call .ctor` with no preceding `initobj`
	// at all (Fase 3.7), since the JIT is required to have already zeroed
	// it. A nil entry (the common case: scalars, references) costs nothing
	// since Value's own zero value already means Null()/0.
	LocalDefaults []Value

	// Handlers are this method's exception handler regions (try/catch/
	// finally/fault — Fase 3.10), IR-index-based. Nil for the overwhelming
	// majority of methods, which have none.
	Handlers []ir.Handler

	// Resolvers binds this specific Method to the Assembly whose metadata
	// produced it (Fase 3.27, multi-assembly resolution) — every "Namespace.
	// Type::Method"/"Namespace.Type" name this method's own IR references
	// (a call target, a field's owning type, ...) must resolve against
	// THIS assembly first, not whichever assembly's Call()/LoadFile
	// happened to be the original entry point. Nil for a method built
	// outside the normal Assembly.buildMethod path (e.g. a test harness
	// constructing a *Method by hand) — interpreter.Machine.invoke then
	// just keeps using whatever resolvers were already active, matching
	// the simpler pre-Fase-3.27 single-assembly behavior.
	Resolvers *Resolvers
}

// Resolvers bundles the four name-resolution callbacks a Machine needs to
// run interpreted IR that isn't a BCL native (Fase 3.27) — one bundle per
// loaded Assembly. Defined here (not in internal/interpreter, where the
// individual Resolver/TypeResolver/... types used to live) so a
// *runtime.Method can carry its own bundle directly without an import
// cycle: runtime already defines Method/Type/Value, everything these
// closures need to reference.
type Resolvers struct {
	// Resolve looks up another method by full name, given the actual
	// call-site arguments (receiver included for an instance call), the
	// call site's own compile-time-resolved parameter type names (nil if
	// unavailable, Fase 3.40), and the call site's own generic-method
	// instantiation arity (0 for a plain, non-generic call — Fase 3.41)
	// to disambiguate a real overload set — see assembly.go's
	// pickMethodOverload.
	Resolve func(fullName string, args []Value, paramTypeNames []string, genericArgCount int) (*Method, error)
	// ResolveType looks up a type's field layout by full name.
	ResolveType func(fullName string) (*Type, error)
	// ResolveExplicitImpl finds the real (mangled) method name a concrete
	// type uses to explicitly implement an interface method (Fase 3.13).
	ResolveExplicitImpl func(concreteTypeFullName, interfaceFullName, methodName string) (implMethodName string, ok bool)
	// ResolveEnum reads a plugin-declared enum's members (Fase 3.26).
	ResolveEnum func(fullName string) (names []string, values []int64, ok bool)
	// ResolveFieldBytes returns a field's compiler-embedded initial-value
	// blob, if it has one (Fase 3.27: RuntimeHelpers.InitializeArray, the
	// pattern behind an array literal's blob initializer — `ldtoken
	// <field>` names the field, this fetches its actual raw bytes from
	// the owning assembly's PE image).
	ResolveFieldBytes func(typeFullName, fieldName string) ([]byte, bool)
	// ResolveMember finds a real method/constructor by exact name and
	// declared parameter type names (Fase 3.39: System.Reflection —
	// Type.GetConstructor/GetMethod), returning its full callable name.
	ResolveMember func(typeFullName, memberName string, paramTypeFullNames []string) (fullName string, ok bool)
	// ResolveManifestResource returns an embedded manifest resource's raw
	// bytes by name (Fase 3.40: Assembly.GetManifestResourceStream — a
	// real .NET assembly can embed arbitrary files, e.g. ClosedXML's own
	// bundled .ttf font data, in its own PE image).
	ResolveManifestResource func(name string) ([]byte, bool)
	// ResolveProperties reads a plugin type's own declared properties
	// (Fase 3.51: System.Reflection — Type.GetProperties/GetProperty) in
	// declaration order — parallel slices (names[i]/canRead[i]/
	// canWrite[i]/propTypes[i] all describe the same i'th property),
	// matching ResolveEnum's own parallel-slice convention above rather
	// than a descriptor struct. canRead[i]/canWrite[i] come from the real
	// get_Xxx/set_Xxx MethodDef linkage (a property can be get-only,
	// set-only, or both), not just guessed from the property's name.
	// propTypes[i] (Fase 3.52: PropertyInfo.PropertyType, added for
	// Dapper's own reflection-based row-to-object mapper) is read off
	// whichever real accessor exists (the getter's return type, or the
	// setter's own single parameter type when there's no getter at all —
	// a set-only property is real, if rare) — "" only if somehow neither
	// resolves, which shouldn't happen for any real property. ok=false
	// for a type with no TypeDef at all (a BCL-only type vmnet has no
	// metadata for), matching every other resolver's own "no data
	// available" contract here.
	ResolveProperties func(typeFullName string) (names []string, canRead []bool, canWrite []bool, propTypes []string, ok bool)
	// ResolveMemberParams reads every real overload of typeFullName's
	// member named memberName (memberName is ".ctor" for a constructor,
	// same convention ResolveMember already uses) — parallel slices,
	// paramTypes[i]/paramNames[i] both describing overload i's own
	// declared parameter list in order (Fase 3.52: System.Reflection —
	// Type.GetConstructors, MethodBase.GetParameters/ParameterInfo,
	// needed for Dapper's own constructor-based row-to-object mapper,
	// which enumerates a target type's constructors to find the best
	// parameter match against a query's column set). ok=false for a type
	// with no TypeDef at all, matching every other resolver's own "no
	// data available" contract; ok=true with zero overloads is real too
	// (a type with no matching member at all).
	ResolveMemberParams func(typeFullName, memberName string) (paramTypes [][]string, paramNames [][]string, ok bool)
	// ResolveFields reads every field typeFullName's own TypeDef declares
	// (Fase 3.53: System.Reflection — Type.GetFields, plus FieldInfo.
	// FieldType) — parallel slices (names[i]/fieldTypes[i]/isStatic[i] all
	// describe the same i'th field), same convention ResolveProperties'
	// own parallel-slice fields already use. fieldTypes[i] is read
	// straight off the field's own declared signature (no getter/setter
	// indirection needed, unlike PropertyInfo.PropertyType — a field's
	// type IS its signature). ok=false for a type with no TypeDef at all,
	// matching every other resolver's own "no data available" contract.
	ResolveFields func(typeFullName string) (names []string, fieldTypes []string, isStatic []bool, ok bool)
	// ResolveMethods reads every method name typeFullName's own TypeDef
	// declares (Fase 3.53: System.Reflection — Type.GetMethods), excluding
	// real constructors (.ctor/.cctor — never returned by GetMethods,
	// only GetConstructors). Unlike ResolveMember/ResolveMemberParams,
	// this never disambiguates by signature: a same-named overload set
	// appears once per real MethodDef row, the same "one MethodInfo per
	// declared method, no per-overload parameter tracking" simplification
	// Type.GetMethod's own doc comment already documents accepting.
	// ok=false for a type with no TypeDef at all, matching every other
	// resolver's own "no data available" contract.
	ResolveMethods func(typeFullName string) (names []string, ok bool)
	// ResolveMemberFlags reads every real overload of typeFullName's
	// member named memberName's own raw ECMA-335 MethodAttributes bitmask
	// (Fase 3.60: System.Reflection — MethodBase.IsPublic/IsPrivate/
	// IsStatic/IsVirtual/IsAbstract/IsFinal/IsFamily/IsAssembly) —
	// flags[i] parallels ResolveMemberParams's own paramTypes[i]/
	// paramNames[i], describing the same i'th overload, so a
	// ConstructorInfo/MethodInfo wrapper's existing (typeFullName,
	// memberName, overloadIndex) triple re-resolves flags the same way it
	// already re-resolves parameters. ok=false for a type with no TypeDef
	// at all, matching every other resolver's own "no data available"
	// contract.
	ResolveMemberFlags func(typeFullName, memberName string) (flags []uint16, ok bool)
}
