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

// LoadIndirect/StoreIndirect (ldind.*/stind.*) read/write through a
// managed pointer. vmnet doesn't distinguish the ldind.i4 vs ldind.r8 vs
// ... variants — the pointed-to Value already carries its own Kind.
type LoadIndirect struct{}
type StoreIndirect struct{}
