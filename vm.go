package vmnet

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/arturoeanton/go-vmnet/internal/metadata"
	"github.com/arturoeanton/go-vmnet/internal/pe"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// VM is the vmnet entry point: it loads assemblies and runs methods from
// them. See docs/en/spec.md §6. Fase 1 has no configurable options yet
// (profiles, limits, permissions land in Fase 2-4 — see docs/en/ROADMAP.md).
type VM struct{}

// New creates a VM.
func New() *VM {
	return &VM{}
}

// LoadFile reads and parses a .NET assembly from disk.
func (vm *VM) LoadFile(path string) (*Assembly, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("vmnet: %w", err)
	}
	return vm.LoadBytes(filepath.Base(path), data)
}

// LoadBytes parses a .NET assembly already in memory. name is used only in
// error messages.
func (vm *VM) LoadBytes(name string, data []byte) (*Assembly, error) {
	f, err := pe.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("vmnet: %s: %w", name, err)
	}
	md, err := metadata.Parse(f.Metadata)
	if err != nil {
		return nil, fmt.Errorf("vmnet: %s: %w", name, err)
	}
	return &Assembly{
		name:    name,
		file:    f,
		md:      md,
		methods: map[uint32]*runtime.Method{},
		types:   map[string]*runtime.Type{},
	}, nil
}
