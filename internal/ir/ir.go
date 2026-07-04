// Package ir defines vmnet's intermediate representation and the builder
// that lowers decoded IL (internal/il) into it. The IR exists so the
// interpreter, the compatibility checker and any future codegen backend
// work against one simplified, validated instruction set instead of raw
// CIL. See docs/en/ROADMAP.md, Fase 1, module "/ir".
package ir

// Instr is one IR instruction. Concrete types below are the Fase 1+2
// instruction set: primitives, objects, fields, callvirt and unhandled
// throw. Anything CIL can express beyond this (arrays, try/catch/finally,
// interface/vtable dispatch, generics beyond native BCL collections, ...)
// is a later-fase feature, and builder.Build reports it as an explicit
// unsupported-opcode error instead of guessing.
type Instr any

type Nop struct{}
type Dup struct{}
type Pop struct{}

// LocalAlloc implements localloc (spec §III.3.47) — pops a byte count and
// pushes a pointer to that many freshly zeroed bytes. Real C# only ever
// reaches this via `stackalloc T[n]` immediately assigned into a Span<T>
// (constructed right after via Span<T>(void*, int) — Fase 3.41, found via
// a real, load-bearing case: System.Text.Json's own JsonDocument.
// TryGetNamedPropertyValue stack-allocates a 256-byte scratch buffer for
// UTF-8-encoding a property name before comparing it against the parsed
// document). vmnet has no real stack-memory model to allocate from
// (spec's own pure-Go, no-tricks non-goal) — allocating a real
// runtime.Array instead and pushing a managed pointer to it (the same
// shape readOnlySpanFromPointerCtor already expects for the RVA-backed-
// array idiom, internal/bcl/system_span.go) is observably identical for
// every real caller: the memory is used exactly like an array for the
// rest of its (function-local) lifetime, and Go's GC keeps it alive
// exactly as long as anything still references it, longer than a real
// stack frame would but never incorrectly short.
type LocalAlloc struct{}

type LoadArg struct{ Index int }
type StoreArg struct{ Index int }
type LoadLocal struct{ Index int }
type StoreLocal struct{ Index int }

type LoadConstI4 struct{ Value int32 }
type LoadConstI8 struct{ Value int64 }
type LoadConstR4 struct{ Value float32 }
type LoadConstR8 struct{ Value float64 }
type LoadString struct{ Value string }
type LoadNull struct{}

type BinOpKind byte

const (
	OpAdd BinOpKind = iota
	OpSub
	OpMul
	OpDiv
	OpRem
	OpAnd
	OpOr
	OpXor
	OpShl
	OpShr
	OpCeq
	OpCgt
	OpClt
)

// BinOp pops two values and pushes one. Unsigned distinguishes div.un/
// rem.un/shr.un/cgt.un/clt.un from their signed counterparts: for Div/Rem
// it changes the division algorithm, for Shr it changes sign-extension
// vs. zero-fill, and for Cgt/Clt it changes how a negative bit pattern
// compares (spec-relevant: this is the standard idiomatic C# range check
// `(uint)(c - low) <= high` — get it wrong and the answer is silently
// incorrect, not just "unsupported"). Add/Sub/Mul/And/Or/Xor/Shl produce
// the same bits either way, so Unsigned is meaningless for them.
type BinOp struct {
	Op       BinOpKind
	Unsigned bool
}
type Neg struct{}
type Not struct{}

type ConvKind byte

const (
	ConvI1 ConvKind = iota
	ConvU1
	ConvI2
	ConvU2
	ConvI4
	ConvU4
	ConvI8
	ConvU8
	ConvR4
	ConvR8
)

type Conv struct{ Kind ConvKind }

// Branch is an unconditional jump. Target is an index into the enclosing
// method's IR slice (already resolved from the IL byte offset).
type Branch struct{ Target int }

// BranchIfTrue/BranchIfFalse implement brtrue/brfalse: pop one value, test
// Value.Truthy().
type BranchIfTrue struct{ Target int }
type BranchIfFalse struct{ Target int }

// Switch implements the `switch` opcode (spec §III.3.68): pop a KindI4
// index and jump to Targets[index] if it's in range, or fall through
// (execute the next instruction) otherwise — per ECMA-335, not an error.
type Switch struct{ Targets []int }

type CompareOp byte

const (
	CmpEq CompareOp = iota
	CmpGe
	CmpGt
	CmpLe
	CmpLt
	CmpNe
)

// BranchCompare implements beq/bge/bgt/ble/blt/bne.un and their unsigned
// variants: pop two values, compare, branch if true. Unsigned matters for
// exactly the reason it matters on BinOp's Cgt/Clt — see its doc comment.
type BranchCompare struct {
	Target   int
	Op       CompareOp
	Unsigned bool
}

// Call invokes either a BCL native (internal/bcl) or another method in the
// same assembly, resolved to FullName at IR-build time. Virtual marks a
// callvirt: the interpreter null-checks the receiver before dispatching.
// Fase 2 resolves callvirt directly (no vtable) — see docs/en/ROADMAP.md.
type Call struct {
	FullName  string
	ArgCount  int // explicit IL args, not counting an implicit `this`
	HasThis   bool
	HasReturn bool
	Virtual   bool
	// ParamTypeNames holds the callee's own declared parameter type
	// names, in order, as compile-time-resolved from this call site's
	// original MethodDefOrRef token (Fase 3.40) — "" for a parameter
	// whose type doesn't resolve to a plain name (an open generic
	// parameter, most commonly). nil if this Call predates Fase 3.40 or
	// its signature couldn't be parsed at all.
	//
	// This exists because runtime argument Kind alone can't always
	// disambiguate an overload set: a bool and any enum both collapse to
	// the same KindI4 shape, so two same-arity overloads differing only
	// in "bool" vs "SomeEnum" are otherwise indistinguishable at the
	// value level. Found via a real, load-bearing case: DocumentFormat.
	// OpenXml's own OpenXmlPackageBuilderExtensions.Open(..., bool
	// isEditing) calls Open(..., PackageOpenMode mode) — same arity,
	// same KindI4 third argument — and without this, pickMethodOverload
	// resolved back onto the SAME overload that was calling it, forever.
	// Only ever used as an early, exact-match preference before falling
	// back to the existing Kind-based scoring — never a hard requirement,
	// since resolution must still work for callers with no Call-level IR
	// at all (Machine.New/CallInstance, the public host API).
	ParamTypeNames []string

	// MethodGenericArgs holds the call site's own resolved generic method
	// type arguments (Fase 3.40) — e.g. ["DocumentFormat.OpenXml.
	// Features.IDisposableFeature"] for a `features.Get<IDisposableFeature>()`
	// call site, parsed from the call's MethodSpec Instantiation blob.
	// nil for a non-generic call (the overwhelming majority) or one whose
	// instantiation couldn't be parsed.
	//
	// This exists for exactly one purpose: resolving `typeof(TFeature)`
	// inside the CALLED method's own body, which — unlike a class's own
	// generic parameter (fixed once the type is instantiated) — can't be
	// baked into that method's IR at build time at all, since the exact
	// same compiled method body runs for every different call site's
	// instantiation. It's threaded only as far as Machine.tryCall's
	// generic-native-registry lookup (internal/interpreter/calls.go), not
	// into ordinary interpreted method bodies — reifying method generics
	// generally is a much larger undertaking than this narrow, real need
	// (DocumentFormat.OpenXml.Packaging.FeatureCollectionBase.Get<T>,
	// the "typed feature bag" pattern OpenXml's whole package-opening
	// pipeline runs through) called for.
	MethodGenericArgs []string
}

type Return struct{ HasValue bool }

// NewObj allocates an instance of TypeFullName and runs CtorFullName
// (either a BCL native constructor or a local .ctor) with the popped
// constructor arguments, then pushes the new object reference.
type NewObj struct {
	TypeFullName string
	CtorFullName string
	ArgCount     int

	// ParamTypeNames is the constructed type's own .ctor overload
	// signature, as declared at THIS newobj site's MethodDefOrRef token —
	// exactly Call.ParamTypeNames's rationale (see its doc comment, Fase
	// 3.40), which newobj had been missing entirely (Fase 3.43). Found via
	// a real, load-bearing case reading a real .xlsx through ClosedXML
	// 0.105.0's `new XLWorkbook(stream)`: ClosedXML.Excel.XLFill has THREE
	// same-arity 2-parameter constructors ((XLStyle, XLFillValue),
	// (XLStyle, XLFillKey), (XLStyle?, IXLFill?) — XLFill.cs:124-138), and
	// `new XLFill()` (both optional arguments filled with null by the
	// compiler, XLWorkbook.cs:5593) gives Kind-based scoring two KindNull
	// args that match all three equally — picking the (XLStyle,
	// XLFillValue) overload, whose body assigns `_value = null` directly
	// instead of running the real (XLStyle?, IXLFill?) -> GenerateKey ->
	// FromKey chain, leaving a real XLFill permanently broken (NRE on its
	// first `.Key` read, from real ClosedXML style-loading code).
	ParamTypeNames []string
}

// LoadField/StoreField implement ldfld/stfld: pop an object reference
// (LoadField) or a value then an object reference (StoreField, matching
// CIL's stack order) and read/write FieldName on it.
type LoadField struct {
	TypeFullName string
	FieldName    string
}

type StoreField struct {
	TypeFullName string
	FieldName    string
}

// LoadStaticField/StoreStaticField implement ldsfld/stsfld. Unlike
// instance fields, the storage lives on the Type itself (shared across
// every instance and every caller — see runtime.Type) and the interpreter
// runs the type's static constructor (.cctor), if any, before the first
// access (Fase 3.5).
type LoadStaticField struct {
	TypeFullName string
	FieldName    string
}

type StoreStaticField struct {
	TypeFullName string
	FieldName    string
}

// Throw pops the top of stack (an exception object) and aborts execution
// with it. Fase 2 only supports unhandled throw — try/catch/finally are
// deferred (docs/en/ROADMAP.md).
type Throw struct{}

// NewArr pops a length and pushes a new zero-initialized array (spec
// §11.2 newarr; only SZARRAY — single-dimensional, zero-based — is
// modeled, matching the vast majority of real-world array usage).
// TypeFullName is the element type (empty if unresolvable, e.g. a generic
// parameter) — needed so the interpreter can seed a value-type element
// array with zero-valued structs/enums rather than a blanket Null(),
// matching real CLR array semantics where a value-type array element is
// never actually null.
type NewArr struct {
	TypeFullName string
}

// LoadLen pops an array reference and pushes its length (ldlen).
type LoadLen struct{}

// LoadElem/StoreElem implement every ldelem.*/stelem.* variant uniformly:
// vmnet's Value is already a tagged union, so there's no need to
// special-case by element type the way real CIL does.
type LoadElem struct{}
type StoreElem struct{}

// LoadArgAddr/LoadLocalAddr/LoadElemAddr/LoadFieldAddr push a managed
// pointer (runtime.KindRef) to a storage slot — an argument, a local, an
// array element or an instance field — instead of the value in it
// (ldarga/ldloca/ldelema/ldflda). This is also how `ref`/`out` parameters
// work: the caller pushes one of these before `call`, and the callee just
// receives it as a normal argument whose Value happens to be a KindRef —
// no special-casing needed anywhere in Call itself (Fase 3.5).
type LoadArgAddr struct{ Index int }
type LoadLocalAddr struct{ Index int }
type LoadElemAddr struct{}
type LoadFieldAddr struct {
	TypeFullName string
	FieldName    string
}

// LoadStaticFieldAddr implements ldsflda (Fase 3.27) — same shape as
// LoadFieldAddr but for a static field's shared storage on the Type
// itself, not an instance. Runs the owning type's .cctor first, same as
// LoadStaticField (Fase 3.5).
type LoadStaticFieldAddr struct {
	TypeFullName string
	FieldName    string
}

// LoadIndirect/StoreIndirect (ldind.*/stind.*, and also ldobj/stobj —
// "load/store a value of any type through a pointer" is the exact same
// operation once pointers are *runtime.Value rather than raw memory, so
// builder.Build lowers all of them to these two instructions) read/write
// through a managed pointer. vmnet doesn't distinguish the ldind.i4 vs
// ldind.r8 vs ... variants — the pointed-to Value already carries its own
// Kind.
type LoadIndirect struct{}
type StoreIndirect struct{}

// InitObj implements `initobj T` (spec §III.4.10): pop a managed pointer
// and overwrite the pointee with default(T) — zero fields for a value
// type, null for a reference type. TypeFullName is "" when T is an
// unresolved generic type parameter (a TypeSpec encoding VAR/MVAR):
// vmnet erases method-generic type arguments at IR-build time (same
// limitation MethodSpec resolution already has, see ir/builder.go), so
// there's no way to know the real T — the interpreter falls back to
// KindNull, which is only wrong if T is later bound to a value type, a
// narrower gap than getting it right would be worth the complexity of
// tracking generic instantiations (docs/en/ROADMAP.md Fase 3.7).
type InitObj struct{ TypeFullName string }

// IsInst/CastClass implement isinst/castclass (spec §III.4.6/4.6): pop an
// object reference and check it against TypeFullName using the real
// class/interface hierarchy (Fase 3.8 — runtime.Type.BaseTypeFullName/
// Interfaces, walked by internal/interpreter/typecheck.go). IsInst pushes
// the reference on a match or null otherwise, never throwing; CastClass
// pushes the reference on a match or throws InvalidCastException. Both
// pass a null receiver through unchanged without even checking the type
// (spec-mandated: casting null always "succeeds"). TypeFullName is ""
// for the same unresolved-generic-parameter reason as InitObj — nothing
// can match an unknown type, so IsInst always yields null and CastClass
// always throws in that case.
type IsInst struct{ TypeFullName string }
type CastClass struct{ TypeFullName string }

// LoadTypeToken implements ldtoken (spec §III.4.16) when its operand is a
// type token — the `typeof(T)` pattern, always compiled as `ldtoken T` +
// `call System.Type::GetTypeFromHandle(RuntimeTypeHandle)`. vmnet skips
// modeling RuntimeTypeHandle as its own value kind: this instruction
// pushes a real System.Type value directly (see bcl.NewTypeValue,
// Fase 3.14), and GetTypeFromHandle is registered as the identity
// function over whatever LoadTypeToken already produced — the two
// together behave exactly like the real two-step sequence without vmnet
// needing an intermediate handle representation at all.
// LoadTypeToken implements ldtoken (spec §III.4.16) for a Type operand
// (typeof(T)). IsMethodGenericParam (Fase 3.40) marks the one case
// TypeFullName can't answer at IR-build time at all: `typeof(TFeature)`
// on the ENCLOSING method's own generic parameter (an MVAR, `!!N`,
// MethodGenericParamIndex == N) — the same method body runs for every
// different call site's instantiation, so this has to be resolved from
// that specific CALL's own MethodGenericArgs (ir.Call's own field)
// instead, at the point of executing the ldtoken, not when building its
// IR. TypeFullName is meaningless when this is true.
type LoadTypeToken struct {
	TypeFullName            string
	IsMethodGenericParam    bool
	MethodGenericParamIndex int
}

// LoadFieldToken implements ldtoken (spec §III.4.16) when its operand is
// a Field token — the RuntimeHelpers.InitializeArray pattern behind an
// array literal initializer's blob (`ldtoken <RVA-backed field>` +
// `call RuntimeHelpers.InitializeArray(array, fldHandle)`, Fase 3.27).
// Unlike LoadTypeToken, this doesn't produce a real Value on its own —
// RuntimeHelpers.InitializeArray (internal/interpreter, Machine-aware:
// it needs the owning Assembly's raw PE bytes) is the only real consumer,
// so this just carries enough to name the field again at that call site.
type LoadFieldToken struct {
	TypeFullName string
	FieldName    string
}

// LoadMethodToken implements ldtoken (spec §III.4.16) when its operand is
// a Method token (RuntimeMethodHandle) — found via a real, pervasive
// pattern (Fase 3.41): every OpenXml element's own ConfigureMetadata
// builds an Expression<Func<TElement,TValue>> for each attribute (`a =>
// a.Space`), which the compiler lowers to `ldtoken <property getter>` +
// `MethodBase.GetMethodFromHandle` + `Expression.Property` rather than a
// real closure/lambda body — same "produce the real Value directly, skip
// the handle indirection" shortcut LoadTypeToken already takes for
// Type.GetTypeFromHandle (see that type's own doc comment and
// bcl.NewMethodInfoValue).
type LoadMethodToken struct {
	TypeFullName string
	MethodName   string
}

// LoadFtn implements ldftn/ldvirtftn (spec §III.4.19/4.20): push an
// unbound delegate target (runtime.KindFunc) referencing FullName.
// ldvirtftn additionally pops a receiver first to resolve the method
// virtually — vmnet already resolves callvirt targets statically (no real
// vtable for method dispatch; Fase 3.8 covers isinst/castclass, a
// different mechanism), so that receiver is popped and discarded, not
// used to pick a different FullName. The delegate's actual receiver (for
// an instance-method or closure target) is bound later, from whatever
// `ldarg.0`/`ldloc`/`ldnull` was pushed just before ldftn, when a
// `newobj SomeDelegateType::.ctor(object, native int)` combines the two —
// see internal/interpreter/calls.go's newObj and runtime.BindDelegate.
type LoadFtn struct {
	FullName string
	Virtual  bool
}

// Leave implements leave/leave.s (spec §III.3.44): an unconditional jump
// out of a try or catch block that, unlike Branch, must first run any
// finally/fault handlers between the leave site and Target — see
// internal/interpreter/exceptions.go, which computes that chain from the
// enclosing Method's Handlers using TryStart/TryEnd exactly like
// dispatching a thrown exception does.
type Leave struct{ Target int }

// EndFinally implements endfinally/endfault (spec §III.3.14): resumes
// whatever control transfer (a Leave chaining through this handler, or an
// exception propagating through it) brought execution into the
// finally/fault block that's ending.
type EndFinally struct{}

// HandlerKind is one exception handler's kind (spec §II.25.4.6). Filter
// clauses (`catch (Foo) when (cond)`) parse structurally at the il layer
// but aren't lowered here — see Build's doc comment — so there's no
// HandlerFilter case to dispatch on.
type HandlerKind byte

const (
	HandlerCatch HandlerKind = iota
	HandlerFinally
	HandlerFault
)

// Handler is one exception handler region, converted from an
// il.ExceptionHandler's IL byte offsets to IR indices (TryStart/TryEnd/
// HandlerStart/HandlerEnd are a [start, end) range over the enclosing
// Method's IR slice, same convention as Branch's Target). CatchTypeFullName
// is set only for HandlerCatch, resolved once here rather than re-resolved
// per exception at runtime.
type Handler struct {
	Kind              HandlerKind
	TryStart, TryEnd  int
	HandlerStart      int
	HandlerEnd        int
	CatchTypeFullName string
}

// Rethrow implements `rethrow` (spec §III.4.31 — C#'s `throw;` with no
// operand, valid only inside a catch block): re-raises the exception
// currently being handled rather than requiring the handler to have kept
// its own reference to it.
type Rethrow struct{}
