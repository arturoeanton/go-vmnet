package vmnet

import "github.com/arturoeanton/go-vmnet/internal/runtime"

// Permissions gates what a VM's loaded assemblies can reach outside
// vmnet's own managed memory — see runtime.Permissions's own doc comment
// (internal/runtime/permissions.go) for the full field-by-field rationale.
// Aliased (not wrapped) so the exact same value flows through Assembly and
// into every interpreter.Machine built from it with no copying/conversion
// at any layer.
type Permissions = runtime.Permissions

// Permissions returns vm's own capability gate, mutable in place: every
// capability starts denied (the zero value — deny-by-default, docs/en/
// security.md), so an embedding program must explicitly enable exactly
// what it needs before loading a package that exercises it, e.g.:
//
//	vm := vmnet.New()
//	vm.Permissions().AllowFileRead = true
//	asm, err := vm.LoadPackage("NPOI@2.8.0")
//
// Unlike NuGet() (which returns a fresh, stateless manifest/lockfile
// reader on every call), the returned pointer refers to state stored on vm
// itself — mutating it after LoadFile/LoadBytes/LoadPackage still takes
// effect on every assembly already loaded from this vm, since each
// Assembly keeps a pointer back to this same struct rather than a copy
// (see call.go's Assembly.machine()).
func (vm *VM) Permissions() *Permissions {
	return &vm.permissions
}
