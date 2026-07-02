package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// staticType resolves typeFullName and, on this Type's first static
// access ever (across every goroutine — runtime.Type.EnsureCctor is the
// synchronization point), runs its .cctor if it has one.
//
// If typeFullName's .cctor is already running on this Machine's own call
// chain (it read or wrote one of its own type's static fields — the
// overwhelmingly common case, not a rare one), staticType returns the Type
// immediately instead of re-entering EnsureCctor, which would deadlock on
// the underlying sync.Once. This mirrors the CLR: a thread already running
// a type's initializer sees its own partially-initialized statics rather
// than blocking on itself.
func (m *Machine) staticType(typeFullName string, depth int, instrCount *int64) (*runtime.Type, error) {
	if m.ResolveType == nil {
		return nil, fmt.Errorf("interpreter: no type resolver configured for %s", typeFullName)
	}
	t, err := m.ResolveType(typeFullName)
	if err != nil {
		return nil, err
	}
	if m.cctorsRunning[t] {
		return t, nil
	}
	if err := t.EnsureCctor(func() error {
		if m.cctorsRunning == nil {
			m.cctorsRunning = make(map[*runtime.Type]bool)
		}
		m.cctorsRunning[t] = true
		defer delete(m.cctorsRunning, t)
		return m.runCctor(typeFullName, depth, instrCount)
	}); err != nil {
		return nil, fmt.Errorf("interpreter: %s..cctor: %w", typeFullName, err)
	}
	return t, nil
}

// runCctor runs typeFullName's static constructor if it has one. Most
// types don't — that's not an error, just nothing to do.
func (m *Machine) runCctor(typeFullName string, depth int, instrCount *int64) error {
	if m.Resolve == nil {
		return nil
	}
	cctor, err := m.Resolve(typeFullName + "::.cctor")
	if err != nil {
		return nil // no static constructor
	}
	_, err = m.invoke(cctor, nil, depth+1, instrCount)
	return err
}
