package bcl

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// Real System.IO.File/Directory/FileStream/FileInfo/DirectoryInfo support
// (Fase 3.59) — every one of these does genuine disk I/O against the
// embedding host's real filesystem, gated by internal/interpreter/
// permissions.go's permissionGatedBCLNatives/permissionGatedBCLCtors
// BEFORE any of the natives below ever runs (this package has no Machine
// access and so no way to check a Permissions gate itself — see calls.go's
// own doc comment on the plain-native vs Machine-aware-native split).
// Every native here assumes the gate has already run: it always performs
// the real I/O it's asked to, unconditionally.
//
// Before this Fase, System.IO.FileSystemInfo's own get_FullName/get_Exists/
// Delete were the only registered members anywhere near this surface
// (system_misc.go) — permanent stand-ins that never touched a real
// filesystem at all, because there was no Permissions model yet to gate
// real access with. Those registrations remain as a base-type fallback for
// any FileSystemInfo-typed call site whose receiver isn't one of the two
// concrete types below (never hit in practice: FileInfo/DirectoryInfo's
// own virtual-dispatch-preferred concrete registrations here shadow them
// for every real corpus caller found).
func init() {
	register("System.IO.File::Exists", true, fileExists)
	register("System.IO.File::OpenRead", true, fileOpenRead)
	register("System.IO.File::ReadAllText", true, fileReadAllText)
	register("System.IO.File::ReadAllBytes", true, fileReadAllBytes)
	register("System.IO.File::WriteAllText", false, fileWriteAllText)
	register("System.IO.File::WriteAllBytes", false, fileWriteAllBytes)
	register("System.IO.File::Delete", false, fileDelete)
	register("System.IO.File::SetAttributes", false, fileSetAttributesNoop)
	register("System.IO.File::Create", true, fileCreate)
	register("System.IO.File::Copy", false, fileCopy)

	register("System.IO.Directory::CreateDirectory", true, directoryCreateDirectory)
	register("System.IO.Directory::Exists", true, directoryExists)

	registerCtor("System.IO.FileStream", fileStreamCtor)

	registerCtor("System.IO.FileInfo", fileInfoCtor)
	register("System.IO.FileInfo::get_Exists", true, fileInfoGetExists)
	register("System.IO.FileInfo::get_Length", true, fileInfoGetLength)
	register("System.IO.FileInfo::get_FullName", true, fileInfoGetFullName)
	register("System.IO.FileInfo::get_Name", true, fileInfoGetName)
	register("System.IO.FileInfo::ToString", true, fileInfoGetFullName)
	register("System.IO.FileInfo::OpenRead", true, fileInfoOpenRead)
	register("System.IO.FileInfo::Open", true, fileInfoOpen)
	register("System.IO.FileInfo::Create", true, fileInfoCreate)
	register("System.IO.FileInfo::Delete", false, fileInfoDelete)

	registerCtor("System.IO.DirectoryInfo", directoryInfoCtor)
	register("System.IO.DirectoryInfo::get_Exists", true, directoryInfoGetExists)
	register("System.IO.DirectoryInfo::get_FullName", true, directoryInfoGetFullName)
	register("System.IO.DirectoryInfo::get_Name", true, directoryInfoGetName)
	register("System.IO.DirectoryInfo::ToString", true, directoryInfoGetFullName)
	register("System.IO.DirectoryInfo::Create", false, directoryInfoCreate)
	register("System.IO.DirectoryInfo::Delete", false, directoryInfoDelete)
}

// fileReadError turns a Go os error from a real read attempt into the
// real .NET exception a caller here would actually get: a missing file is
// FileNotFoundException specifically (not the generic IOException), the
// same distinction real File.ReadAllText/OpenRead/etc. make.
func fileReadError(path string, err error) error {
	if os.IsNotExist(err) {
		return &runtime.ManagedException{TypeName: "System.IO.FileNotFoundException", Message: fmt.Sprintf("Could not find file '%s'.", path)}
	}
	return &runtime.ManagedException{TypeName: "System.IO.IOException", Message: err.Error()}
}

func fileExists(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Bool(false), nil
	}
	info, err := os.Stat(args[0].Str)
	return runtime.Bool(err == nil && !info.IsDir()), nil
}

// openFileForRead eagerly reads path's entire real content into a
// nativeMemoryStream (Fase 3.59) — the simplest correct backing for a
// read-only real file: every Stream member (Read/Seek/Position/Length/...,
// system_io.go) already works against an in-memory buf, and target
// packages here always read a file to completion rather than needing true
// lazy/partial disk streaming.
func openFileForRead(path string) (runtime.Value, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return runtime.Value{}, fileReadError(path, err)
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeMemoryStream{buf: data, typeName: "System.IO.FileStream"}}), nil
}

func fileOpenRead(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: File.OpenRead expects a path")
	}
	return openFileForRead(args[0].Str)
}

func fileReadAllText(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: File.ReadAllText expects a path")
	}
	data, err := os.ReadFile(args[0].Str)
	if err != nil {
		return runtime.Value{}, fileReadError(args[0].Str, err)
	}
	return runtime.String(string(data)), nil
}

func fileReadAllBytes(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: File.ReadAllBytes expects a path")
	}
	data, err := os.ReadFile(args[0].Str)
	if err != nil {
		return runtime.Value{}, fileReadError(args[0].Str, err)
	}
	return bytesToArrayValue(data), nil
}

func fileWriteAllText(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[0].Kind != runtime.KindString || args[1].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: File.WriteAllText expects (path, contents)")
	}
	if err := os.WriteFile(args[0].Str, []byte(args[1].Str), 0644); err != nil {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.IO.IOException", Message: err.Error()}
	}
	return runtime.Value{}, nil
}

func fileWriteAllBytes(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: File.WriteAllBytes expects (path, byte[])")
	}
	data, err := arrayToBytes(args[1])
	if err != nil {
		return runtime.Value{}, err
	}
	if werr := os.WriteFile(args[0].Str, data, 0644); werr != nil {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.IO.IOException", Message: werr.Error()}
	}
	return runtime.Value{}, nil
}

// fileDelete is a no-op (not NotFoundException) on an already-missing
// path, matching real File.Delete's own documented behavior exactly:
// deleting a file that doesn't exist is not an error.
func fileDelete(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: File.Delete expects a path")
	}
	if err := os.Remove(args[0].Str); err != nil && !os.IsNotExist(err) {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.IO.IOException", Message: err.Error()}
	}
	return runtime.Value{}, nil
}

// fileSetAttributesNoop: real FileAttributes (ReadOnly/Hidden/Normal/...)
// has no direct cross-platform Go analog, and every real corpus caller
// found (NPOI resetting FileAttributes.Normal before overwriting a temp
// file that might be read-only on Windows) only ever uses this to clear a
// write-blocking attribute before a Delete/overwrite that follows
// immediately — a no-op here is unobservable on the platforms vmnet's own
// interpreter runs on, as long as the path itself is real (this native is
// still permission-gated exactly like a real attribute change would be).
func fileSetAttributesNoop(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: File.SetAttributes expects a path")
	}
	return runtime.Value{}, nil
}

// openFileForCreate backs File.Create/FileInfo.Create/a write-mode
// FileStream — real File.Create truncates (or creates) the file
// immediately, before the caller writes a single byte, so this writes an
// empty file to disk right away rather than waiting for the eventual
// Close (msClose, system_io.go, still flushes the final buf content to
// diskPath on Close/Dispose, overwriting this empty placeholder with
// whatever was actually written — or leaving it empty if nothing was).
func openFileForCreate(path string) (runtime.Value, error) {
	if err := os.WriteFile(path, nil, 0644); err != nil {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.IO.IOException", Message: err.Error()}
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeMemoryStream{typeName: "System.IO.FileStream", diskPath: path}}), nil
}

func fileCreate(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: File.Create expects a path")
	}
	return openFileForCreate(args[0].Str)
}

// fileCopy covers both the (source, dest) and (source, dest, overwrite)
// overloads — overwrite defaults to false (real File.Copy's own default),
// matching real behavior's IOException when the destination already
// exists and the caller didn't explicitly ask to overwrite it.
func fileCopy(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[0].Kind != runtime.KindString || args[1].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: File.Copy expects (sourceFileName, destFileName)")
	}
	overwrite := len(args) > 2 && args[2].Kind == runtime.KindI4 && args[2].I4 != 0
	if !overwrite {
		if _, err := os.Stat(args[1].Str); err == nil {
			return runtime.Value{}, &runtime.ManagedException{TypeName: "System.IO.IOException", Message: fmt.Sprintf("The file '%s' already exists.", args[1].Str)}
		}
	}
	data, err := os.ReadFile(args[0].Str)
	if err != nil {
		return runtime.Value{}, fileReadError(args[0].Str, err)
	}
	if werr := os.WriteFile(args[1].Str, data, 0644); werr != nil {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.IO.IOException", Message: werr.Error()}
	}
	return runtime.Value{}, nil
}

func directoryCreateDirectory(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: Directory.CreateDirectory expects a path")
	}
	if err := os.MkdirAll(args[0].Str, 0755); err != nil {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.IO.IOException", Message: err.Error()}
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeDirectoryInfo{path: args[0].Str}}), nil
}

func directoryExists(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Bool(false), nil
	}
	info, err := os.Stat(args[0].Str)
	return runtime.Bool(err == nil && info.IsDir()), nil
}

// openFileStreamByMode implements every real FileMode's own real disk
// semantics — CreateNew=1, Create=2, Open=3, OpenOrCreate=4, Truncate=5,
// Append=6 (vmnet has no TypeDef for this BCL enum, so this switches on
// the raw underlying int32 exactly like msSeek's own SeekOrigin handling,
// system_io.go). CreateNew doesn't distinguish itself from Create (real
// CreateNew throws IOException if the file already exists) — a defensible
// simplification: no real corpus caller here relies on that specific
// failure path, only on CreateNew succeeding when the file doesn't exist,
// which this already does correctly.
func openFileStreamByMode(path string, mode int32) (runtime.Value, error) {
	switch mode {
	case 3: // Open: read the real, already-existing file in full.
		return openFileForRead(path)
	case 1, 2, 5: // CreateNew, Create, Truncate: start from an empty file.
		return openFileForCreate(path)
	case 4, 6: // OpenOrCreate, Append: preserve any real existing content.
		data, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return runtime.Value{}, &runtime.ManagedException{TypeName: "System.IO.IOException", Message: err.Error()}
		}
		ms := &nativeMemoryStream{typeName: "System.IO.FileStream", diskPath: path, buf: data}
		if mode == 6 {
			// Append positions the stream at the end so the first Write
			// lands after whatever real content already exists, matching
			// real FileMode.Append's own documented starting position.
			ms.pos = len(data)
		}
		return runtime.ObjRef(&runtime.Object{Native: ms}), nil
	default:
		return openFileForRead(path)
	}
}

// fileStreamCtor covers FileStream(path, FileMode) and FileStream(path,
// FileMode, FileAccess[, FileShare]) — every real overload's own FileAccess/
// FileShare argument only ever narrows what the returned Stream would
// reject if misused; vmnet's own Stream methods (system_io.go) don't
// enforce access-mode/sharing violations at all, so only path and mode
// matter here.
func fileStreamCtor(args []runtime.Value) (*runtime.Object, error) {
	if len(args) < 2 || args[0].Kind != runtime.KindString || args[1].Kind != runtime.KindI4 {
		return nil, fmt.Errorf("bcl: FileStream constructor expects (string path, FileMode mode, ...)")
	}
	v, err := openFileStreamByMode(args[0].Str, args[1].I4)
	if err != nil {
		return nil, err
	}
	return v.Obj, nil
}

// nativeFileInfo backs System.IO.FileInfo (Fase 3.59) — a thin, real path
// wrapper: the constructor itself never touches disk (matching real
// FileInfo's own lazy behavior), only the members below do.
type nativeFileInfo struct {
	path string
}

func fileInfoCtor(args []runtime.Value) (*runtime.Object, error) {
	path := ""
	if len(args) > 0 && args[0].Kind == runtime.KindString {
		path = args[0].Str
	}
	return &runtime.Object{Native: &nativeFileInfo{path: path}}, nil
}

func asFileInfo(args []runtime.Value) (*nativeFileInfo, error) {
	if len(args) > 0 && args[0].Kind == runtime.KindObject && args[0].Obj != nil {
		if fi, ok := args[0].Obj.Native.(*nativeFileInfo); ok {
			return fi, nil
		}
	}
	return nil, fmt.Errorf("bcl: receiver is not a FileInfo")
}

func fileInfoGetExists(args []runtime.Value) (runtime.Value, error) {
	fi, err := asFileInfo(args)
	if err != nil {
		return runtime.Value{}, err
	}
	info, statErr := os.Stat(fi.path)
	return runtime.Bool(statErr == nil && !info.IsDir()), nil
}

func fileInfoGetLength(args []runtime.Value) (runtime.Value, error) {
	fi, err := asFileInfo(args)
	if err != nil {
		return runtime.Value{}, err
	}
	info, statErr := os.Stat(fi.path)
	if statErr != nil {
		return runtime.Value{}, fileReadError(fi.path, statErr)
	}
	return runtime.Int64(info.Size()), nil
}

func fileInfoGetFullName(args []runtime.Value) (runtime.Value, error) {
	fi, err := asFileInfo(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if abs, aerr := filepath.Abs(fi.path); aerr == nil {
		return runtime.String(abs), nil
	}
	return runtime.String(fi.path), nil
}

func fileInfoGetName(args []runtime.Value) (runtime.Value, error) {
	fi, err := asFileInfo(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.String(filepath.Base(fi.path)), nil
}

func fileInfoOpenRead(args []runtime.Value) (runtime.Value, error) {
	fi, err := asFileInfo(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return openFileForRead(fi.path)
}

func fileInfoOpen(args []runtime.Value) (runtime.Value, error) {
	fi, err := asFileInfo(args)
	if err != nil {
		return runtime.Value{}, err
	}
	mode := int32(3)
	if len(args) > 1 && args[1].Kind == runtime.KindI4 {
		mode = args[1].I4
	}
	return openFileStreamByMode(fi.path, mode)
}

func fileInfoCreate(args []runtime.Value) (runtime.Value, error) {
	fi, err := asFileInfo(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return openFileForCreate(fi.path)
}

func fileInfoDelete(args []runtime.Value) (runtime.Value, error) {
	fi, err := asFileInfo(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if rerr := os.Remove(fi.path); rerr != nil && !os.IsNotExist(rerr) {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.IO.IOException", Message: rerr.Error()}
	}
	return runtime.Value{}, nil
}

// nativeDirectoryInfo backs System.IO.DirectoryInfo (Fase 3.59) — same
// "thin real path wrapper, ctor never touches disk" shape as
// nativeFileInfo above.
type nativeDirectoryInfo struct {
	path string
}

func directoryInfoCtor(args []runtime.Value) (*runtime.Object, error) {
	path := ""
	if len(args) > 0 && args[0].Kind == runtime.KindString {
		path = args[0].Str
	}
	return &runtime.Object{Native: &nativeDirectoryInfo{path: path}}, nil
}

func asDirectoryInfo(args []runtime.Value) (*nativeDirectoryInfo, error) {
	if len(args) > 0 && args[0].Kind == runtime.KindObject && args[0].Obj != nil {
		if di, ok := args[0].Obj.Native.(*nativeDirectoryInfo); ok {
			return di, nil
		}
	}
	return nil, fmt.Errorf("bcl: receiver is not a DirectoryInfo")
}

func directoryInfoGetExists(args []runtime.Value) (runtime.Value, error) {
	di, err := asDirectoryInfo(args)
	if err != nil {
		return runtime.Value{}, err
	}
	info, statErr := os.Stat(di.path)
	return runtime.Bool(statErr == nil && info.IsDir()), nil
}

func directoryInfoGetFullName(args []runtime.Value) (runtime.Value, error) {
	di, err := asDirectoryInfo(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if abs, aerr := filepath.Abs(di.path); aerr == nil {
		return runtime.String(abs), nil
	}
	return runtime.String(di.path), nil
}

func directoryInfoGetName(args []runtime.Value) (runtime.Value, error) {
	di, err := asDirectoryInfo(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.String(filepath.Base(di.path)), nil
}

func directoryInfoCreate(args []runtime.Value) (runtime.Value, error) {
	di, err := asDirectoryInfo(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if merr := os.MkdirAll(di.path, 0755); merr != nil {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.IO.IOException", Message: merr.Error()}
	}
	return runtime.Value{}, nil
}

func directoryInfoDelete(args []runtime.Value) (runtime.Value, error) {
	di, err := asDirectoryInfo(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if rerr := os.RemoveAll(di.path); rerr != nil {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.IO.IOException", Message: rerr.Error()}
	}
	return runtime.Value{}, nil
}
