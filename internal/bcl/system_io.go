package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// nativeMemoryStream backs System.IO.MemoryStream — and, via base-class
// chaining, any real package's own subclass of it (NPOI declares several,
// e.g. NPOI.POIFS.FileSystem.NDocumentOutputStream extends MemoryStream
// directly; see baseExceptionCtorInPlace in system_exception.go for the
// same "chain into a native base" pattern, first established there).
//
// Registered under both "System.IO.MemoryStream::*" and
// "System.IO.Stream::*": real code very commonly holds a MemoryStream in
// a Stream-typed local/parameter (`Stream s = new MemoryStream();`),
// which compiles the call site against the declared Stream name.
// Machine.call's virtual dispatch (Fase 3.27) tries the receiver's real
// concrete type first — "System.IO.MemoryStream", from NativeTypeName
// below — so a callvirt site resolves through the MemoryStream
// registration alone regardless of which declared name compiled in; both
// are registered anyway to also cover a plain (non-virtual) call site
// naming Stream directly.
type nativeMemoryStream struct {
	buf    []byte
	pos    int
	closed bool
}

func (ms *nativeMemoryStream) writeAt(data []byte) {
	end := ms.pos + len(data)
	if end > len(ms.buf) {
		grown := make([]byte, end)
		copy(grown, ms.buf)
		ms.buf = grown
	}
	copy(ms.buf[ms.pos:end], data)
	ms.pos = end
}

func init() {
	registerCtor("System.IO.MemoryStream", newMemoryStreamCtor)
	register("System.IO.MemoryStream::.ctor", false, memoryStreamCtorInPlace)
	// System.IO.Stream itself is abstract in real .NET (never newobj'd
	// directly), but a package type extending it directly
	// (NPOI.Util.OutputStream, POIFSDocumentReader, ...) chains its own
	// ctor to `base()` — a no-op here since those subclasses keep their
	// own backing state in their own real fields, not in a native buffer.
	register("System.IO.Stream::.ctor", false, streamCtorNoop)

	for _, prefix := range []string{"System.IO.MemoryStream", "System.IO.Stream"} {
		register(prefix+"::Write", false, msWrite)
		register(prefix+"::WriteByte", false, msWriteByte)
		register(prefix+"::Read", true, msRead)
		register(prefix+"::ReadByte", true, msReadByte)
		register(prefix+"::Seek", true, msSeek)
		register(prefix+"::SetLength", false, msSetLength)
		register(prefix+"::Flush", false, msFlush)
		register(prefix+"::Close", false, msClose)
		register(prefix+"::Dispose", false, msClose)
		register(prefix+"::CopyTo", false, msCopyTo)
		register(prefix+"::get_Length", true, msGetLength)
		register(prefix+"::get_Position", true, msGetPosition)
		register(prefix+"::set_Position", false, msSetPosition)
		register(prefix+"::get_CanRead", true, msTrue)
		register(prefix+"::get_CanSeek", true, msTrue)
		register(prefix+"::get_CanWrite", true, msCanWrite)
	}
	// ToArray/GetBuffer are MemoryStream-only in real .NET (not declared
	// on Stream at all), so only that one registration makes sense.
	register("System.IO.MemoryStream::ToArray", true, msToArray)
	register("System.IO.MemoryStream::GetBuffer", true, msGetBuffer)
}

func newMemoryStreamBuf(ctorArgs []runtime.Value) *nativeMemoryStream {
	ms := &nativeMemoryStream{}
	// The byte[]-seeded overload (`new MemoryStream(bytes)`) is the only
	// one that needs the argument itself — the int-capacity overload is a
	// pure allocation hint (buf auto-grows regardless), same
	// simplification StringBuilder's capacity ctor already makes.
	if len(ctorArgs) > 0 && ctorArgs[0].Kind == runtime.KindArray && ctorArgs[0].Arr != nil {
		data, _ := arrayToBytes(ctorArgs[0])
		ms.buf = data
	}
	return ms
}

func newMemoryStreamCtor(args []runtime.Value) (*runtime.Object, error) {
	return &runtime.Object{Native: newMemoryStreamBuf(args)}, nil
}

// memoryStreamCtorInPlace backs "System.IO.MemoryStream::.ctor" as a
// plain (non-newobj) call — the shape a derived package class's
// constructor uses to chain to its base via `: base()`/`: base(bytes)`.
// The receiver already exists (newObj allocated it as the derived type);
// this only adds Obj.Native alongside it, the same deliberate narrow
// exception to Object's "Type xor Native" rule system_exception.go's
// baseExceptionCtorInPlace documents.
func memoryStreamCtorInPlace(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return runtime.Value{}, fmt.Errorf("bcl: MemoryStream constructor called without a receiver")
	}
	args[0].Obj.Native = newMemoryStreamBuf(args[1:])
	return runtime.Value{}, nil
}

func streamCtorNoop(args []runtime.Value) (runtime.Value, error) {
	return runtime.Value{}, nil
}

func asMemoryStream(args []runtime.Value) (*nativeMemoryStream, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("bcl: Stream method called without a receiver")
	}
	v := args[0]
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	if v.Kind != runtime.KindObject || v.Obj == nil {
		return nil, fmt.Errorf("bcl: Stream method called without a receiver")
	}
	ms, ok := v.Obj.Native.(*nativeMemoryStream)
	if !ok {
		return nil, fmt.Errorf("bcl: receiver is not a vmnet-native Stream (only System.IO.MemoryStream and its subclasses are supported)")
	}
	return ms, nil
}

func arrayToBytes(v runtime.Value) ([]byte, error) {
	if v.Kind != runtime.KindArray || v.Arr == nil {
		return nil, fmt.Errorf("bcl: expected a byte[] argument")
	}
	out := make([]byte, len(v.Arr.Elems))
	for i, e := range v.Arr.Elems {
		out[i] = byte(e.I4)
	}
	return out, nil
}

func writeBytesIntoArray(arr *runtime.Array, offset int, data []byte) {
	for i, b := range data {
		arr.Elems[offset+i] = runtime.Int32(int32(b))
	}
}

func bytesToArrayValue(data []byte) runtime.Value {
	elems := make([]runtime.Value, len(data))
	for i, b := range data {
		elems[i] = runtime.Int32(int32(b))
	}
	return runtime.ArrRef(&runtime.Array{Elems: elems})
}

func msWrite(args []runtime.Value) (runtime.Value, error) {
	ms, err := asMemoryStream(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 4 || args[1].Kind != runtime.KindArray || args[1].Arr == nil {
		return runtime.Value{}, fmt.Errorf("bcl: Stream.Write expects (byte[], int, int)")
	}
	data, err := arrayToBytes(args[1])
	if err != nil {
		return runtime.Value{}, err
	}
	offset := int(args[2].I4)
	count := int(args[3].I4)
	if offset < 0 || count < 0 || offset+count > len(data) {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentException", Message: "offset and count exceed the buffer's length"}
	}
	ms.writeAt(data[offset : offset+count])
	return runtime.Value{}, nil
}

func msWriteByte(args []runtime.Value) (runtime.Value, error) {
	ms, err := asMemoryStream(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: Stream.WriteByte expects a byte argument")
	}
	ms.writeAt([]byte{byte(args[1].I4)})
	return runtime.Value{}, nil
}

func msRead(args []runtime.Value) (runtime.Value, error) {
	ms, err := asMemoryStream(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 4 || args[1].Kind != runtime.KindArray || args[1].Arr == nil {
		return runtime.Value{}, fmt.Errorf("bcl: Stream.Read expects (byte[], int, int)")
	}
	offset := int(args[2].I4)
	count := int(args[3].I4)
	avail := len(ms.buf) - ms.pos
	if avail < 0 {
		avail = 0
	}
	n := count
	if n > avail {
		n = avail
	}
	if n < 0 {
		n = 0
	}
	if n > 0 {
		writeBytesIntoArray(args[1].Arr, offset, ms.buf[ms.pos:ms.pos+n])
		ms.pos += n
	}
	return runtime.Int32(int32(n)), nil
}

func msReadByte(args []runtime.Value) (runtime.Value, error) {
	ms, err := asMemoryStream(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if ms.pos >= len(ms.buf) {
		return runtime.Int32(-1), nil
	}
	b := ms.buf[ms.pos]
	ms.pos++
	return runtime.Int32(int32(b)), nil
}

// msSeek implements System.IO.SeekOrigin semantics (Begin=0, Current=1,
// End=2) directly against the origin argument's raw underlying int32 —
// vmnet has no TypeDef for this BCL enum to resolve a symbolic name
// against, same posture as every other BCL enum argument elsewhere in
// this package (e.g. StringComparison in system_string.go).
func msSeek(args []runtime.Value) (runtime.Value, error) {
	ms, err := asMemoryStream(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 3 {
		return runtime.Value{}, fmt.Errorf("bcl: Stream.Seek expects (long, SeekOrigin)")
	}
	offset, _ := valueAsInt64(args[1])
	origin, _ := valueAsInt64(args[2])
	var base int64
	switch origin {
	case 1:
		base = int64(ms.pos)
	case 2:
		base = int64(len(ms.buf))
	default:
		base = 0
	}
	newPos := base + offset
	if newPos < 0 {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.IO.IOException", Message: "an attempt was made to move the position before the beginning of the stream"}
	}
	ms.pos = int(newPos)
	return runtime.Int64(newPos), nil
}

func msSetLength(args []runtime.Value) (runtime.Value, error) {
	ms, err := asMemoryStream(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: Stream.SetLength expects a long argument")
	}
	raw, _ := valueAsInt64(args[1])
	n := int(raw)
	if n < 0 {
		n = 0
	}
	if n <= len(ms.buf) {
		ms.buf = ms.buf[:n]
	} else {
		grown := make([]byte, n)
		copy(grown, ms.buf)
		ms.buf = grown
	}
	if ms.pos > n {
		ms.pos = n
	}
	return runtime.Value{}, nil
}

func msFlush(args []runtime.Value) (runtime.Value, error) {
	if _, err := asMemoryStream(args); err != nil {
		return runtime.Value{}, err
	}
	return runtime.Value{}, nil
}

// msClose marks the stream closed but doesn't enforce use-after-close
// errors on later calls — a pragmatic simplification (matching
// StringBuilder's Capacity stand-in): real code paths in these packages
// close a stream once, at the very end, so there's nothing meaningful
// left to guard against in practice.
func msClose(args []runtime.Value) (runtime.Value, error) {
	ms, err := asMemoryStream(args)
	if err != nil {
		return runtime.Value{}, err
	}
	ms.closed = true
	return runtime.Value{}, nil
}

// msCopyTo only supports another vmnet-native MemoryStream as the
// destination (the only Stream implementation vmnet has) — real
// System.IO.Stream.CopyTo works against any Stream subclass, but every
// concrete destination this loop's target packages construct themselves
// is a MemoryStream.
func msCopyTo(args []runtime.Value) (runtime.Value, error) {
	ms, err := asMemoryStream(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: Stream.CopyTo expects a destination stream")
	}
	dest, err := asMemoryStream(args[1:2])
	if err != nil {
		return runtime.Value{}, fmt.Errorf("bcl: Stream.CopyTo: destination is not a supported stream type")
	}
	dest.writeAt(ms.buf[ms.pos:])
	ms.pos = len(ms.buf)
	return runtime.Value{}, nil
}

func msGetLength(args []runtime.Value) (runtime.Value, error) {
	ms, err := asMemoryStream(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int64(int64(len(ms.buf))), nil
}

func msGetPosition(args []runtime.Value) (runtime.Value, error) {
	ms, err := asMemoryStream(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int64(int64(ms.pos)), nil
}

func msSetPosition(args []runtime.Value) (runtime.Value, error) {
	ms, err := asMemoryStream(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: Stream.set_Position expects a long argument")
	}
	raw, _ := valueAsInt64(args[1])
	ms.pos = int(raw)
	return runtime.Value{}, nil
}

func msToArray(args []runtime.Value) (runtime.Value, error) {
	ms, err := asMemoryStream(args)
	if err != nil {
		return runtime.Value{}, err
	}
	out := make([]byte, len(ms.buf))
	copy(out, ms.buf)
	return bytesToArrayValue(out), nil
}

// msGetBuffer returns a copy of the exact byte content (vmnet's buf is
// always precisely Length long) rather than a capacity-padded internal
// array real GetBuffer can return — a defensible simplification: callers
// combining GetBuffer with get_Length to know the "real" extent still
// see identical bytes, only the rarely-relied-upon padding tail differs.
func msGetBuffer(args []runtime.Value) (runtime.Value, error) {
	ms, err := asMemoryStream(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return bytesToArrayValue(ms.buf), nil
}

func msTrue(args []runtime.Value) (runtime.Value, error) {
	if _, err := asMemoryStream(args); err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(true), nil
}

func msCanWrite(args []runtime.Value) (runtime.Value, error) {
	ms, err := asMemoryStream(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(!ms.closed), nil
}
