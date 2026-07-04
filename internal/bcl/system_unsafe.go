package bcl

import (
	"fmt"
	"unsafe"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// System.Runtime.CompilerServices.Unsafe is a real JIT intrinsic on a
// genuine CLR — its own IL body is a stub the JIT substitutes real
// pointer-reinterpretation code for, never actually interpreted as
// written (Fase 3.40). vmnet has no raw memory model to reinterpret
// through in general (a real non-goal, spec.md §3), but As/ByteOffset
// specifically are used almost everywhere in the wild for exactly one
// idiom — "get a byref to element 0 of an array via a fake Pinnable<T>
// wrapper" (System.Memory's own SpanHelpers.PerTypeValues<T>, found via
// a real, load-bearing case: ClosedXML's own font-metrics engine, which
// depends on System.Memory being loaded as a real dependency at all,
// reached just from constructing an XLWorkbook). As is a pure identity
// passthrough (the value's real Kind — KindArray, most commonly — never
// actually changes shape; see fieldSlot's own KindArray case in
// internal/interpreter/eval.go for how `.Data` on the "reinterpreted"
// result still resolves correctly against the real array). ByteOffset
// always answers 0: without a real memory model there's no other
// meaningful number to give, and 0 is what this idiom's only real
// caller (MeasureArrayAdjustment) needs regardless — an aligned,
// garbage-collected array's first element already occupies exactly
// where Data would sit in a Pinnable<T>-shaped reinterpretation.
func init() {
	register("System.Runtime.CompilerServices.Unsafe::As", true, unsafeAs)
	register("System.Runtime.CompilerServices.Unsafe::AsRef", true, unsafeAs)
	register("System.Runtime.CompilerServices.Unsafe::ByteOffset", true, unsafeByteOffsetZero)
	// AddByteOffset: real, non-zero-offset Go-pointer arithmetic — see
	// unsafeAddByteOffset's own doc comment (Fase 3.41) for why an
	// unconditional 0/identity-passthrough (this idiom's ORIGINAL only
	// known caller, MeasureArrayAdjustment, always passes 0) stopped
	// being sufficient once a real byte-granular scan (a different real
	// caller) started passing a genuine non-zero offset.
	register("System.Runtime.CompilerServices.Unsafe::AddByteOffset", true, unsafeAddByteOffset)
	register("System.Runtime.CompilerServices.Unsafe::Add", true, unsafeAdd)
}

// unsafeAdd backs Unsafe.Add<T>(ref T source, int elementOffset) — real
// pointer arithmetic, needed by System.Memory's own Span<T> indexer
// (`Unsafe.Add(ref Unsafe.AddByteOffset(ref _pinnable.Data, _byteOffset),
// index)`, Fase 3.40). vmnet's KindRef is ultimately always a Go pointer
// into a runtime.Value slice here (fieldSlot's Pinnable<T>.Data case
// always returns &array.Elems[0]) — Go's own unsafe.Add lets this shift
// within that same backing array correctly and safely (the array stays
// GC-reachable through the resulting pointer), which is the only actual
// pointer arithmetic vmnet ever needs to perform.
func unsafeAdd(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 || args[0].Kind != runtime.KindRef || args[0].Ref == nil {
		return runtime.Value{}, fmt.Errorf("bcl: Unsafe.Add expects (ref T, int)")
	}
	offset, ok := valueAsInt64(args[1])
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: Unsafe.Add expects an int32/int64 element offset")
	}
	if offset == 0 {
		return args[0], nil
	}
	shifted := (*runtime.Value)(unsafe.Add(unsafe.Pointer(args[0].Ref), int(offset)*int(unsafe.Sizeof(runtime.Value{}))))
	return runtime.RefTo(shifted), nil
}

func unsafeAs(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("bcl: Unsafe.As expects 1 argument")
	}
	return args[0], nil
}

func unsafeByteOffsetZero(args []runtime.Value) (runtime.Value, error) {
	return runtime.Int64(0), nil
}

// unsafeAddByteOffset backs Unsafe.AddByteOffset(ref T source, IntPtr
// byteOffset). Every caller this native was originally written for
// (System.Memory's own SpanHelpers.PerTypeValues<T> "fake Pinnable<T>"
// idiom, this file's own top-of-file doc comment) always passes 0, so a
// pure identity passthrough was indistinguishable from real arithmetic
// there. A real, non-zero offset shows up too, though (Fase 3.41, found
// running real System.Text.Json 8.0.5's netstandard2.0 build):
// JsonReaderHelper.IndexOfQuoteOrAnyControlOrBackSlash's own real IL
// walks a ReadOnlySpan<byte> one-or-more bytes at a time entirely via
// `Unsafe.AddByteOffset(ref reference, intPtr)` — a real byte-granular
// pinnable reference (see MemoryMarshal.GetReference/GetPinnableReference
// docs, system_span.go), where a byte offset IS an element index one-
// for-one. The identity-passthrough version silently re-read byte 0
// forever, so the scan never found the real closing quote and every
// JSON string overran to end-of-buffer ("EndOfStringNotFound"). Reusing
// unsafeAdd's own established Go-pointer-arithmetic approach here keeps
// offset==0 (every pre-existing caller) byte-for-byte identical to the
// old passthrough while making a genuine non-zero offset actually shift
// the reference.
func unsafeAddByteOffset(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 || args[0].Kind != runtime.KindRef || args[0].Ref == nil {
		return runtime.Value{}, fmt.Errorf("bcl: Unsafe.AddByteOffset expects (ref T, IntPtr)")
	}
	offset, ok := valueAsInt64(args[1])
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: Unsafe.AddByteOffset expects an IntPtr byte offset")
	}
	if offset == 0 {
		return args[0], nil
	}
	shifted := (*runtime.Value)(unsafe.Add(unsafe.Pointer(args[0].Ref), int(offset)*int(unsafe.Sizeof(runtime.Value{}))))
	return runtime.RefTo(shifted), nil
}
