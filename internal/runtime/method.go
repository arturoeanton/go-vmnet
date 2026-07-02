package runtime

import "github.com/arturoeanton/go-vmnet/internal/ir"

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
}
