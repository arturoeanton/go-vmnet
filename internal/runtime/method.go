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
}
