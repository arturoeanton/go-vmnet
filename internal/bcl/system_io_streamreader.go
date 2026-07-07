package bcl

import (
	"bytes"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// System.IO.StreamReader (Fase 3.83, found via ClosedXML's own real
// .xlsx-parsing internals — previously never reached at all, masked by
// the List<T>(IEnumerable<T>) construction bug this same Fase fixed
// silently short-circuiting some earlier real code path). Backed by the
// exact same nativeStringReader struct/Read/Peek/ReadLine/ReadToEnd
// natives StringReader already uses (system_io_stringreader.go) — once
// the underlying Stream's bytes are decoded to text at construction
// time, a StreamReader behaves identically to a StringReader for every
// method this codebase implements; only the construction differs.
//
// The constructor itself is Machine-aware (internal/interpreter/calls.go's
// own Machine.newObj special case, mirroring the List<T>/ArrayList real-
// enumeration fix this same Fase already added there) rather than a plain
// bcl.NativeCtor — the real Stream argument found in practice (ClosedXML's
// own internal .xlsx zip-part reading) is a genuine, resolvable plugin
// TypeDef (a real compiled Stream subclass, not one of this codebase's own
// native wrappers), so decoding it needs to drive its own real Read(byte[],
// int, int) method via a real virtual call — the exact same "no Machine
// access, so can't do this from a plain bcl.Native" gap this Fase's other
// two fixes hit. NewStreamReaderFromBytes (below) is the shared tail end
// both the fast path (an already-materialized nativeMemoryStream) and the
// slow path (calls.go's own real read loop) hand their decoded bytes to.
func init() {
	register("System.IO.StreamReader::Read", true, stringReaderRead)
	register("System.IO.StreamReader::Peek", true, stringReaderPeek)
	register("System.IO.StreamReader::ReadLine", true, stringReaderReadLine)
	register("System.IO.StreamReader::ReadToEnd", true, stringReaderReadToEnd)
	register("System.IO.StreamReader::Close", false, stringReaderNoop)
	register("System.IO.StreamReader::Dispose", false, stringReaderNoop)
}

// MemoryStreamBytesFromCurrentPosition returns ms's own remaining bytes
// (from its current read position onward) if v is a native MemoryStream/
// FileStream (system_io.go) — the fast path internal/interpreter/calls.go's
// own StreamReader construction special case tries first, before falling
// back to driving a real Stream.Read loop for anything else.
func MemoryStreamBytesFromCurrentPosition(v runtime.Value) ([]byte, bool) {
	ms, ok := nativeOf[*nativeMemoryStream](v)
	if !ok {
		return nil, false
	}
	data := ms.buf
	if ms.pos > 0 && ms.pos <= len(data) {
		data = data[ms.pos:]
	}
	return data, true
}

// NewStreamReaderFromBytes decodes data as UTF-8 (stripping a leading BOM
// if present, matching real StreamReader's own default encoding-detection
// behavior for the common case — a real Encoding argument, if the caller
// passed a non-default one, is otherwise ignored, same posture
// StringReader's own doc comment documents for TextReader more broadly)
// and wraps the result exactly the way StringReader's own constructor
// does — every read method the two share afterward is the identical
// nativeStringReader-backed native.
func NewStreamReaderFromBytes(data []byte) runtime.Value {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}) // UTF-8 BOM
	return runtime.ObjRef(&runtime.Object{Native: &nativeStringReader{runes: []rune(string(data))}})
}
