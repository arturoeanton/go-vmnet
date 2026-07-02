// Package ir defines vmnet's intermediate representation and the builder
// that lowers decoded IL (internal/il) into it. The IR exists so the
// interpreter, the compatibility checker and any future codegen backend
// work against one simplified, validated instruction set instead of raw
// CIL. See docs/ROADMAP.md, Fase 1, module "/ir".
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
// Fase 2 resolves callvirt directly (no vtable) — see docs/ROADMAP.md.
type Call struct {
	FullName  string
	ArgCount  int // explicit IL args, not counting an implicit `this`
	HasThis   bool
	HasReturn bool
	Virtual   bool
}

type Return struct{ HasValue bool }

// NewObj allocates an instance of TypeFullName and runs CtorFullName
// (either a BCL native constructor or a local .ctor) with the popped
// constructor arguments, then pushes the new object reference.
type NewObj struct {
	TypeFullName string
	CtorFullName string
	ArgCount     int
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
// deferred (docs/ROADMAP.md).
type Throw struct{}

// NewArr pops a length and pushes a new zero-initialized array (spec
// §11.2 newarr; only SZARRAY — single-dimensional, zero-based — is
// modeled, matching the vast majority of real-world array usage).
type NewArr struct{}

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
// tracking generic instantiations (docs/ROADMAP.md Fase 3.7).
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
type LoadTypeToken struct{ TypeFullName string }

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
