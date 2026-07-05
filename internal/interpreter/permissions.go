package interpreter

import "github.com/arturoeanton/go-vmnet/internal/runtime"

// permissionGate reports whether p currently grants a permission-gated
// native's required capability, given its own call-site args (only
// System.IO.FileStream::.ctor and System.IO.FileInfo::Open need to look at
// an argument — the FileMode — to know which capability to require; every
// other gate below ignores args entirely). Returns nil when allowed, or
// the exact *runtime.ManagedException to raise (as a Go error, the same
// convention every other native/opcode in this package already uses —
// see e.g. array_ops.go) when denied.
//
// p == nil (a Machine built without WithPermissions — every existing test
// fixture, today) is treated identically to a real, explicit
// &runtime.Permissions{}: deny everything gated here. A missing
// Permissions must never silently behave as "allow everything" — that
// would make forgetting to wire Permissions through some future call path
// a silent security regression instead of a loud, obvious "nothing works
// until you grant it" one.
type permissionGate func(p *runtime.Permissions, args []runtime.Value) error

// permissionGatedBCLNatives lists every plain BCL native (registered via
// bcl.Lookup/register — see tryCall's own doc comment for the distinction
// from Machine-aware machineRegistry natives) that reaches real,
// host-visible I/O outside vmnet's own managed memory. Checked in tryCall
// BEFORE the native itself ever runs, so a denied capability never
// executes so much as a stat(2) syscall — deny-by-default (docs/en/
// security.md) means an interpreted program can't distinguish "permission
// denied" from "file doesn't exist" by a side-channel timing/partial
// effect, because no attempt is made at all.
//
// Two natives here (System.IO.Path::GetTempFileName, Microsoft.Data.
// Sqlite.SqliteConnection::Open) are retrofits, not new surface (Fase
// 3.59): both already did real, entirely ungated file I/O before this —
// see each one's own doc comment in internal/bcl for the pre-existing
// behavior this now gates instead of changing.
var permissionGatedBCLNatives = map[string]permissionGate{
	"System.IO.File::Exists":       gateFileRead,
	"System.IO.File::OpenRead":     gateFileRead,
	"System.IO.File::ReadAllText":  gateFileRead,
	"System.IO.File::ReadAllBytes": gateFileRead,

	"System.IO.File::WriteAllText":  gateFileWrite,
	"System.IO.File::WriteAllBytes": gateFileWrite,
	"System.IO.File::Delete":        gateFileWrite,
	"System.IO.File::SetAttributes": gateFileWrite,
	"System.IO.File::Create":        gateFileWrite,

	// Copy both reads the source and writes the destination.
	"System.IO.File::Copy": gateFileReadAndWrite,

	"System.IO.Directory::CreateDirectory": gateFileWrite,
	"System.IO.Directory::Exists":          gateFileRead,

	"System.IO.FileInfo::get_Exists": gateFileRead,
	"System.IO.FileInfo::get_Length": gateFileRead,
	"System.IO.FileInfo::OpenRead":   gateFileRead,
	// FileInfo.Open(FileMode[, FileAccess]) — args[1] (index 1, same
	// position newObj's FileStream ctor gate below inspects) carries the
	// FileMode, so the exact same mode-dependent logic applies.
	"System.IO.FileInfo::Open":   gateByFileModeArg1,
	"System.IO.FileInfo::Create": gateFileWrite,
	"System.IO.FileInfo::Delete": gateFileWrite,

	"System.IO.DirectoryInfo::get_Exists": gateFileRead,
	"System.IO.DirectoryInfo::Create":     gateFileWrite,
	"System.IO.DirectoryInfo::Delete":     gateFileWrite,

	// Retrofit: real, ungated os.CreateTemp since this native was first
	// added (internal/bcl/system_io_path.go) — it creates a real 0-byte
	// file on disk, not just a path string, so it needs AllowFileWrite
	// exactly like File.Create does.
	"System.IO.Path::GetTempFileName": gateFileWrite,

	// Retrofit: opening a real SQLite connection (internal/bcl/
	// system_data_sqlite.go) reads and can create/write the on-disk
	// database file — see parseSqliteConnectionString's own doc comment,
	// which documented this as a real, unrestricted-file-I/O finding
	// before a Permissions model existed to gate it at all.
	"Microsoft.Data.Sqlite.SqliteConnection::Open": gateFileReadAndWrite,
}

// permissionGatedBCLCtors mirrors permissionGatedBCLNatives for
// constructors, which newObj resolves through bcl.LookupCtor — a separate
// dispatch path from tryCall's bcl.Lookup, keyed by the type's full name
// rather than "Type::.ctor".
var permissionGatedBCLCtors = map[string]permissionGate{
	"System.IO.FileStream": gateByFileModeArg1,
}

func unauthorized(message string) error {
	return &runtime.ManagedException{
		TypeName: "System.UnauthorizedAccessException",
		Message:  message + " (vmnet: denied by Permissions — see VM.Permissions() and docs/en/security.md)",
	}
}

func gateFileRead(p *runtime.Permissions, args []runtime.Value) error {
	if p == nil || !p.AllowFileRead {
		return unauthorized("Access to the path is denied")
	}
	return nil
}

func gateFileWrite(p *runtime.Permissions, args []runtime.Value) error {
	if p == nil || !p.AllowFileWrite {
		return unauthorized("Access to the path is denied")
	}
	return nil
}

func gateFileReadAndWrite(p *runtime.Permissions, args []runtime.Value) error {
	if err := gateFileRead(p, args); err != nil {
		return err
	}
	return gateFileWrite(p, args)
}

// gateByFileModeArg1 backs both System.IO.FileStream::.ctor (args[0] is
// the path, args[1] the FileMode) and System.IO.FileInfo::Open (args[0]
// is the receiver, args[1] the FileMode) — real .NET's FileMode enum
// values: CreateNew=1, Create=2, Open=3, OpenOrCreate=4, Truncate=5,
// Append=6 (vmnet has no TypeDef to resolve this BCL enum's symbolic
// names against, so this switches on the raw underlying int32 exactly
// like msSeek's own SeekOrigin handling in system_io.go already does).
// Only Open(3) requires just a read; every other mode can create,
// truncate, or append, so all of them require a write.
func gateByFileModeArg1(p *runtime.Permissions, args []runtime.Value) error {
	mode := int32(3)
	if len(args) > 1 && args[1].Kind == runtime.KindI4 {
		mode = args[1].I4
	}
	if mode == 3 {
		return gateFileRead(p, args)
	}
	return gateFileWrite(p, args)
}
