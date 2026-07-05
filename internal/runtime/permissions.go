package runtime

// Permissions gates what a loaded assembly's interpreted code can reach
// outside vmnet's own managed memory: real files, the real console, a real
// network. Every capability defaults to denied (the zero value) — vmnet's
// documented threat model (docs/en/security.md) is deny-by-default, not
// deny-by-exception, so loading a package that happens to call
// System.IO.File.ReadAllText never silently grants real disk access just
// because the package was loaded; the embedding host must opt in
// explicitly for exactly the capability it actually needs.
//
// Lives in internal/runtime (not internal/interpreter or the top-level
// vmnet package) so both internal/bcl and internal/interpreter can see the
// same type without an import cycle: the top-level vmnet package already
// depends on both of those, and internal/bcl must never depend on
// internal/interpreter (see internal/interpreter/calls.go's own doc
// comment on the split between plain bcl.Native and Machine-aware
// natives). The public API (permissions.go, package vmnet) exposes this
// same type under the name Permissions via a type alias.
type Permissions struct {
	// AllowFileRead permits real filesystem reads: System.IO.File.Exists/
	// OpenRead/ReadAllText/ReadAllBytes, System.IO.Directory.Exists,
	// System.IO.FileInfo/DirectoryInfo's read-only members, and opening a
	// System.IO.FileStream/File.OpenRead in a read mode. See
	// docs/en/security.md for the exact gated native surface.
	AllowFileRead bool

	// AllowFileWrite permits real filesystem writes, deletes, and
	// directory creation: System.IO.File.Create/WriteAllText/
	// WriteAllBytes/Delete/SetAttributes/Copy (Copy also needs
	// AllowFileRead, since it reads the source), System.IO.Directory.
	// CreateDirectory, and opening a System.IO.FileStream/File.Create in
	// a write/create/append mode. Independent of AllowFileRead — a host
	// can allow a plugin to read its own package cache without also
	// allowing it to delete or overwrite anything.
	AllowFileWrite bool

	// AllowConsole and AllowNetwork are reserved for a future Fase (see
	// docs/en/ROADMAP.md's long-standing "Permissions model (AllowConsole/
	// AllowFileRead/AllowNetwork, deny-by-default)" entry) — defined now
	// so a host that sets them today keeps compiling unchanged once
	// they're enforced, but neither gates anything yet:
	// System.Console.Write/WriteLine remains always-allowed (existing,
	// pre-Permissions behavior), and no network-touching native exists
	// at all yet.
	AllowConsole bool
	AllowNetwork bool
}
