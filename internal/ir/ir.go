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

type BinOp struct{ Op BinOpKind }
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
// variants: pop two values, compare, branch if true. Fase 1 does not
// distinguish signed/unsigned comparison (spec-documented limitation,
// harmless for the non-negative int32 fixtures Fase 1 targets).
type BranchCompare struct {
	Target int
	Op     CompareOp
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

// Throw pops the top of stack (an exception object) and aborts execution
// with it. Fase 2 only supports unhandled throw — try/catch/finally are
// deferred (docs/ROADMAP.md).
type Throw struct{}
