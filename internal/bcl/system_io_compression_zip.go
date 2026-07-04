package bcl

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// System.IO.Compression.ZipArchive backs OPC packages (.xlsx/.docx/...):
// System.IO.Packaging.ZipPackage (a real NuGet-cached BCL assembly,
// DocumentFormat.OpenXml's own transitive dependency) opens every part
// through `new ZipArchive(stream, mode, leaveOpen: true)`, then
// `.Entries`/`.GetEntry`/`.CreateEntry`/entry `.Open()` — found via a real,
// load-bearing case: ClosedXML's XLWorkbook(Stream) constructor, itself
// going through OpenXml's package layer to read the real .xlsx zip parts
// (Fase 3.40).
//
// ZipArchiveMode's real ordinal values: Read=0, Create=1, Update=2.
const (
	zipModeRead   = 0
	zipModeCreate = 1
	zipModeUpdate = 2
)

// nativeZipArchive backs both reading (Read/Update: parsed eagerly via Go's
// archive/zip at construction) and writing (Create/Update: entries
// accumulate in their own MemoryStream, and the whole archive is only
// actually zip-encoded once, in Dispose — matching how a real ZipArchive
// only flushes its central directory when closed).
type nativeZipArchive struct {
	mode      int32
	entries   []*nativeZipArchiveEntry
	container *nativeMemoryStream
	disposed  bool
}

// nativeZipArchiveEntry: content holds the decompressed bytes for an entry
// read from an existing archive; writeObj (set immediately by CreateEntry,
// matching every real caller's own CreateEntry-then-Open sequence) is the
// persistent accumulation buffer for a freshly created entry. Dispose's
// finalize pass prefers writeObj when present.
type nativeZipArchiveEntry struct {
	archive  *nativeZipArchive
	fullName string
	content  []byte
	writeObj *runtime.Object
}

func init() {
	registerCtor("System.IO.Compression.ZipArchive", zipArchiveCtor)
	register("System.IO.Compression.ZipArchive::get_Entries", true, zipArchiveGetEntries)
	register("System.IO.Compression.ZipArchive::GetEntry", true, zipArchiveGetEntry)
	register("System.IO.Compression.ZipArchive::CreateEntry", true, zipArchiveCreateEntry)
	register("System.IO.Compression.ZipArchive::get_Mode", true, zipArchiveGetMode)
	register("System.IO.Compression.ZipArchive::Dispose", false, zipArchiveDispose)
	register("System.IO.Compression.ZipArchive::Close", false, zipArchiveDispose)

	register("System.IO.Compression.ZipArchiveEntry::get_FullName", true, zipEntryGetFullName)
	register("System.IO.Compression.ZipArchiveEntry::get_Name", true, zipEntryGetName)
	register("System.IO.Compression.ZipArchiveEntry::get_Length", true, zipEntryGetLength)
	register("System.IO.Compression.ZipArchiveEntry::get_Archive", true, zipEntryGetArchive)
	register("System.IO.Compression.ZipArchiveEntry::Open", true, zipEntryOpen)
	register("System.IO.Compression.ZipArchiveEntry::Delete", false, zipEntryDelete)
}

// valueAsMemoryStream finds the nativeMemoryStream backing a Stream-typed
// value, walking through any number of real managed wrapper layers first
// (e.g. DocumentFormat.OpenXml.Framework.ReadOnlyStream/DelegatingStream,
// which hold the real inner Stream in a plain field and override every
// member to forward to it — a genuine BCL pattern, not vmnet-specific).
// Each such wrapper's object graph has exactly one Stream-shaped field, so
// a depth-first search for the first nativeMemoryStream reachable through
// any field is the general, structural fix rather than hardcoding a field
// name like "_innerStream" (Fase 3.40).
func valueAsMemoryStream(v runtime.Value) (*nativeMemoryStream, error) {
	seen := map[*runtime.Object]bool{}
	var walk func(v runtime.Value) *nativeMemoryStream
	walk = func(v runtime.Value) *nativeMemoryStream {
		if v.Kind == runtime.KindRef && v.Ref != nil {
			v = *v.Ref
		}
		if v.Kind != runtime.KindObject || v.Obj == nil || seen[v.Obj] {
			return nil
		}
		seen[v.Obj] = true
		if ms, ok := v.Obj.Native.(*nativeMemoryStream); ok {
			return ms
		}
		for _, f := range v.Obj.Fields {
			if ms := walk(f); ms != nil {
				return ms
			}
		}
		return nil
	}
	if ms := walk(v); ms != nil {
		return ms, nil
	}
	return nil, fmt.Errorf("bcl: ZipArchive only supports a vmnet-native Stream (MemoryStream), directly or through a real wrapper Stream, as its backing")
}

func zipArchiveCtor(args []runtime.Value) (*runtime.Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("bcl: ZipArchive constructor expects a stream")
	}
	container, err := valueAsMemoryStream(args[0])
	if err != nil {
		return nil, err
	}
	mode := int32(zipModeRead)
	if len(args) > 1 {
		m, _ := valueAsInt64(args[1])
		mode = int32(m)
	}
	za := &nativeZipArchive{mode: mode, container: container}
	if mode == zipModeRead || mode == zipModeUpdate {
		data := container.buf
		if container.pos > 0 && container.pos <= len(data) {
			data = data[container.pos:]
		}
		if len(data) > 0 {
			zr, zerr := zip.NewReader(bytes.NewReader(data), int64(len(data)))
			if zerr != nil {
				return nil, &runtime.ManagedException{TypeName: "System.IO.InvalidDataException", Message: "the archive entry was compressed using an unsupported compression method, or the archive is corrupt"}
			}
			for _, f := range zr.File {
				rc, rerr := f.Open()
				if rerr != nil {
					return nil, rerr
				}
				content, rerr := io.ReadAll(rc)
				rc.Close()
				if rerr != nil {
					return nil, rerr
				}
				za.entries = append(za.entries, &nativeZipArchiveEntry{archive: za, fullName: f.Name, content: content})
			}
		}
	}
	return &runtime.Object{Native: za}, nil
}

func asZipArchive(args []runtime.Value) (*nativeZipArchive, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return nil, fmt.Errorf("bcl: ZipArchive method called without a receiver")
	}
	za, ok := args[0].Obj.Native.(*nativeZipArchive)
	if !ok {
		return nil, fmt.Errorf("bcl: receiver is not a ZipArchive")
	}
	return za, nil
}

func asZipEntry(args []runtime.Value) (*nativeZipArchiveEntry, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return nil, fmt.Errorf("bcl: ZipArchiveEntry method called without a receiver")
	}
	e, ok := args[0].Obj.Native.(*nativeZipArchiveEntry)
	if !ok {
		return nil, fmt.Errorf("bcl: receiver is not a ZipArchiveEntry")
	}
	return e, nil
}

func zipArchiveGetEntries(args []runtime.Value) (runtime.Value, error) {
	za, err := asZipArchive(args)
	if err != nil {
		return runtime.Value{}, err
	}
	items := make([]runtime.Value, len(za.entries))
	for i, e := range za.entries {
		items[i] = runtime.ObjRef(&runtime.Object{Native: e})
	}
	return NewListValue(items), nil
}

func zipArchiveGetEntry(args []runtime.Value) (runtime.Value, error) {
	za, err := asZipArchive(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 || args[1].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: ZipArchive.GetEntry expects an entry name")
	}
	for _, e := range za.entries {
		if e.fullName == args[1].Str {
			return runtime.ObjRef(&runtime.Object{Native: e}), nil
		}
	}
	return runtime.Null(), nil
}

func zipArchiveCreateEntry(args []runtime.Value) (runtime.Value, error) {
	za, err := asZipArchive(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 || args[1].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: ZipArchive.CreateEntry expects an entry name")
	}
	e := &nativeZipArchiveEntry{
		archive:  za,
		fullName: args[1].Str,
		writeObj: &runtime.Object{Native: &nativeMemoryStream{}},
	}
	za.entries = append(za.entries, e)
	return runtime.ObjRef(&runtime.Object{Native: e}), nil
}

func zipArchiveGetMode(args []runtime.Value) (runtime.Value, error) {
	za, err := asZipArchive(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(za.mode), nil
}

// zipArchiveDispose finalizes Create/Update archives: real ZipArchive only
// writes its central directory (and, here, only actually zip-encodes
// anything at all) when disposed — Read archives already have everything
// they'll ever need parsed at construction time, so there's nothing to
// flush.
func zipArchiveDispose(args []runtime.Value) (runtime.Value, error) {
	za, err := asZipArchive(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if za.disposed || za.mode == zipModeRead {
		za.disposed = true
		return runtime.Value{}, nil
	}
	za.disposed = true
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range za.entries {
		w, werr := zw.Create(e.fullName)
		if werr != nil {
			return runtime.Value{}, werr
		}
		content := e.content
		if e.writeObj != nil {
			if ms, ok := e.writeObj.Native.(*nativeMemoryStream); ok {
				content = ms.buf
			}
		}
		if _, werr := w.Write(content); werr != nil {
			return runtime.Value{}, werr
		}
	}
	if werr := zw.Close(); werr != nil {
		return runtime.Value{}, werr
	}
	if za.container != nil {
		za.container.buf = buf.Bytes()
		za.container.pos = len(za.container.buf)
	}
	return runtime.Value{}, nil
}

func zipEntryGetFullName(args []runtime.Value) (runtime.Value, error) {
	e, err := asZipEntry(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.String(e.fullName), nil
}

func zipEntryGetName(args []runtime.Value) (runtime.Value, error) {
	e, err := asZipEntry(args)
	if err != nil {
		return runtime.Value{}, err
	}
	name := e.fullName
	if idx := strings.LastIndexByte(name, '/'); idx >= 0 {
		name = name[idx+1:]
	}
	return runtime.String(name), nil
}

func zipEntryGetLength(args []runtime.Value) (runtime.Value, error) {
	e, err := asZipEntry(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if e.writeObj != nil {
		if ms, ok := e.writeObj.Native.(*nativeMemoryStream); ok {
			return runtime.Int64(int64(len(ms.buf))), nil
		}
	}
	return runtime.Int64(int64(len(e.content))), nil
}

func zipEntryGetArchive(args []runtime.Value) (runtime.Value, error) {
	e, err := asZipEntry(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.ObjRef(&runtime.Object{Native: e.archive}), nil
}

func zipEntryOpen(args []runtime.Value) (runtime.Value, error) {
	e, err := asZipEntry(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if e.writeObj != nil {
		return runtime.ObjRef(e.writeObj), nil
	}
	buf := make([]byte, len(e.content))
	copy(buf, e.content)
	return runtime.ObjRef(&runtime.Object{Native: &nativeMemoryStream{buf: buf}}), nil
}

func zipEntryDelete(args []runtime.Value) (runtime.Value, error) {
	e, err := asZipEntry(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if e.archive != nil {
		for i, other := range e.archive.entries {
			if other == e {
				e.archive.entries = append(e.archive.entries[:i], e.archive.entries[i+1:]...)
				break
			}
		}
	}
	return runtime.Value{}, nil
}
