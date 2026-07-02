package interpreter

import (
	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// defaultValueFor computes default(T) for initobj: a zero-valued struct
// for a value type, Null() for a reference type or anything vmnet can't
// resolve — typeFullName == "" (an unresolved generic type parameter, see
// ir.InitObj's doc comment) or a foreign value type vmnet doesn't model
// (no TypeDef in the loaded assembly and no native registration) both
// fall back the same way. This matches vmnet's existing stance on
// unmodeled BCL surface elsewhere (an unresolvable Call target isn't a
// build-time failure either — internal/interpreter/calls.go) rather than
// making initobj a stricter special case: a method whose struct local
// gets fully overwritten before any field read never actually needed a
// correct default in the first place.
func (m *Machine) defaultValueFor(typeFullName string) runtime.Value {
	if typeFullName == "" {
		return runtime.Null()
	}
	if t, ok := bcl.LookupValueType(typeFullName); ok {
		return runtime.StructVal(runtime.NewStruct(t))
	}
	if m.ResolveType == nil {
		return runtime.Null()
	}
	t, err := m.ResolveType(typeFullName)
	if err != nil || !t.IsValueType {
		return runtime.Null()
	}
	return runtime.StructVal(runtime.NewStruct(t))
}
