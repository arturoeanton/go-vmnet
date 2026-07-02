package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// Resolver looks up another method in the same assembly by its
// "Namespace.Type::Method" full name, for calls that aren't BCL natives.
type Resolver func(fullName string) (*runtime.Method, error)

func (m *Machine) call(fullName string, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, bool, error) {
	if native, hasReturn, ok := bcl.Lookup(fullName); ok {
		v, err := native(args)
		return v, hasReturn, err
	}
	if m.Resolve == nil {
		return runtime.Value{}, false, fmt.Errorf("interpreter: unsupported BCL method %q (no native registered)", fullName)
	}
	method, err := m.Resolve(fullName)
	if err != nil {
		return runtime.Value{}, false, fmt.Errorf("interpreter: unsupported BCL method %q: %w", fullName, err)
	}
	v, err := m.invoke(method, args, depth+1, instrCount)
	if err != nil {
		return runtime.Value{}, false, err
	}
	return v, method.HasReturn, nil
}
