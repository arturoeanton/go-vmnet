package bcl

import (
	"fmt"
	"unicode/utf16"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// nativeEncoding marks a real Encoding.Unicode/BigEndianUnicode instance
// (Fase 3.39) — found via a real, load-bearing case: NPOI's own
// NPOI.Util.StringUtil (its own internal string decoding helper, needed
// just to construct an HSSFWorkbook) keeps a static Encoding.Unicode
// field and uses it to decode BIFF's own "uncompressed" Unicode cell
// strings, which are genuinely UTF-16LE on the wire — 2 bytes per char,
// not 1. Every OTHER Encoding.* getter (UTF8/ASCII/Default/GetEncoding)
// still returns a bare, unmarked object and keeps the pre-existing
// UTF-8-passthrough simplification: real multi-codepage support beyond
// what a real caller has been found to depend on is still out of scope,
// same reasoning as ever documented here — this only widens the one case
// where "just treat it as UTF-8" would have produced silently wrong data
// (a 2-byte-per-char format decoded 1 byte at a time garbles every
// non-ASCII-range codepoint AND desyncs the byte offset for anything
// after it).
type nativeEncoding struct {
	utf16BigEndian bool
}

func init() {
	register("System.Text.Encoding::get_UTF8", true, encodingGetUTF8)
	register("System.Text.Encoding::get_ASCII", true, encodingGetUTF8)
	register("System.Text.Encoding::get_Default", true, encodingGetUTF8)
	register("System.Text.Encoding::get_Unicode", true, encodingGetUnicodeLE)
	register("System.Text.Encoding::get_BigEndianUnicode", true, encodingGetUnicodeBE)
	register("System.Text.Encoding::GetEncoding", true, encodingGetEncodingByName)
	register("System.Text.Encoding::GetString", true, encodingGetString)
	register("System.Text.Encoding::GetBytes", true, encodingGetBytes)
	register("System.Text.Encoding::GetByteCount", true, encodingGetByteCount)
	register("System.Text.Encoding::GetMaxByteCount", true, encodingGetMaxByteCount)
	// RegisterProvider(CodePagesEncodingProvider.Instance) is a real,
	// common NPOI/StringUtil startup call (registering support for
	// legacy codepages like windows-1252/big5 beyond .NET Core's
	// UTF-8/ASCII/Unicode-only default table) — a no-op here since
	// encodingGetEncodingByName already recognizes every codepage name
	// this loop's target packages actually request, without needing the
	// real provider-registration indirection at all.
	register("System.Text.Encoding::RegisterProvider", false, encodingNoopArg)
	registerStaticFieldHost(codePagesProviderStaticsType)
	register("System.Text.CodePagesEncodingProvider::get_Instance", true, encodingGetUTF8)

	// The concrete Encoding subclasses (System.Text.UTF8Encoding/
	// ASCIIEncoding/UnicodeEncoding) are directly `newobj`-constructible,
	// not just reachable via Encoding.UTF8/.ASCII/.Unicode's static
	// getters (Fase 3.40, found via a real, load-bearing case:
	// ClosedXML's own XLHelper..cctor does `new UTF8Encoding(...)`
	// directly). Registered under their own concrete names too (not just
	// the base Encoding::* names) so a call site whose declared local
	// variable type is the concrete class still resolves — same
	// "register under both the concrete and base name" precedent
	// MemoryStream/Stream already established.
	registerCtor("System.Text.UTF8Encoding", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{}, nil
	})
	registerCtor("System.Text.ASCIIEncoding", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{}, nil
	})
	registerCtor("System.Text.UnicodeEncoding", func(args []runtime.Value) (*runtime.Object, error) {
		bigEndian := len(args) > 0 && args[0].Kind == runtime.KindI4 && args[0].I4 != 0
		return &runtime.Object{Native: &nativeEncoding{utf16BigEndian: bigEndian}}, nil
	})
	for _, prefix := range []string{"System.Text.UTF8Encoding", "System.Text.ASCIIEncoding", "System.Text.UnicodeEncoding"} {
		register(prefix+"::GetString", true, encodingGetString)
		register(prefix+"::GetBytes", true, encodingGetBytes)
		register(prefix+"::GetByteCount", true, encodingGetByteCount)
	}
}

func encodingNoopArg(args []runtime.Value) (runtime.Value, error) {
	return runtime.Value{}, nil
}

// codePagesProviderStaticsType backs CodePagesEncodingProvider's own
// static Instance field in case some real IL reads it via ldsfld
// directly rather than through the get_Instance property accessor
// registered above (both must resolve to something, even though neither
// is ever consulted for anything: RegisterProvider is a no-op).
var codePagesProviderStaticsType = runtime.NewType("System.Text", "CodePagesEncodingProvider", nil,
	[]string{"Instance"}, nil, []runtime.Value{runtime.Null()})

func encodingGetUTF8(args []runtime.Value) (runtime.Value, error) {
	return runtime.ObjRef(&runtime.Object{}), nil
}

func encodingGetUnicodeLE(args []runtime.Value) (runtime.Value, error) {
	return runtime.ObjRef(&runtime.Object{Native: &nativeEncoding{}}), nil
}

func encodingGetUnicodeBE(args []runtime.Value) (runtime.Value, error) {
	return runtime.ObjRef(&runtime.Object{Native: &nativeEncoding{utf16BigEndian: true}}), nil
}

// encodingGetEncodingByName backs the static Encoding.GetEncoding(string
// name) overload — recognizes the handful of codepage names this loop's
// target packages actually request by name (NPOI.Util.StringUtil's own
// .cctor: "ISO-8859-1", "UTF-16BE", legacy "big5"/"windows-1252" for
// non-Unicode BIFF strings). Only the UTF-16 names get real, correct
// decoding (encodingAsUTF16); every single-byte codepage name still
// falls back to the pre-existing UTF-8-passthrough simplification — a
// real windows-1252/big5 table is out of scope until a real caller is
// found depending on one of their actual non-ASCII mappings, matching
// every other "good enough for what's actually been observed" note in
// this file.
func encodingGetEncodingByName(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: Encoding.GetEncoding expects a string name")
	}
	switch args[0].Str {
	case "UTF-16BE", "utf-16BE", "unicodeFFFE":
		return encodingGetUnicodeBE(nil)
	case "UTF-16LE", "utf-16", "utf-16LE", "Unicode":
		return encodingGetUnicodeLE(nil)
	default:
		return encodingGetUTF8(nil)
	}
}

// encodingAsUTF16 reports whether receiver (args[0], possibly a managed
// pointer) is a real Encoding.Unicode/BigEndianUnicode instance.
func encodingAsUTF16(receiver runtime.Value) (bigEndian, ok bool) {
	if receiver.Kind == runtime.KindRef && receiver.Ref != nil {
		receiver = *receiver.Ref
	}
	if receiver.Kind != runtime.KindObject || receiver.Obj == nil {
		return false, false
	}
	e, ok := receiver.Obj.Native.(*nativeEncoding)
	if !ok {
		return false, false
	}
	return e.utf16BigEndian, true
}

// byteArrayArgToBytes reads a byte[] argument in whichever shape it
// arrives: a real CIL byte[] (KindArray of KindI4 elements — what
// interpreted code actually produces via newarr) or KindBytes (the
// separate Go<->C# CallBytes/CallJSON bridge representation). Every
// prior version of this native only accepted KindBytes, silently
// rejecting the far more common real-array case — found opening a real
// NPOI workbook, whose own internal string decoding calls
// Encoding.GetString/GetBytes against genuine interpreted byte[] locals.
// byteArrayArgToBytes accepts either a real byte[] (KindArray/KindBytes)
// or a Span<byte>/ReadOnlySpan<byte> struct (Fase 3.41, found via a real,
// load-bearing case: System.Text.Json's own JsonElement.GetString calls
// the real Encoding.UTF8.GetString(ReadOnlySpan<byte>) overload, not the
// plain-array one) — the span case reads exactly its own (backing,
// start, length) window, not the whole backing array.
func byteArrayArgToBytes(v runtime.Value) ([]byte, bool) {
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	switch v.Kind {
	case runtime.KindBytes:
		return v.Bytes, true
	case runtime.KindArray:
		if v.Arr == nil {
			return nil, false
		}
		out := make([]byte, len(v.Arr.Elems))
		for i, e := range v.Arr.Elems {
			out[i] = byte(e.I4)
		}
		return out, true
	case runtime.KindStruct:
		backing, start, length, ok := SpanBacking(v)
		if !ok || backing.Kind != runtime.KindArray || backing.Arr == nil {
			return nil, false
		}
		elems := backing.Arr.Elems
		if start+length > len(elems) {
			return nil, false
		}
		out := make([]byte, length)
		for i := 0; i < length; i++ {
			out[i] = byte(elems[start+i].I4)
		}
		return out, true
	default:
		return nil, false
	}
}

func encodingGetString(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: Encoding.GetString expects a byte[] argument")
	}
	data, ok := byteArrayArgToBytes(args[1])
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: Encoding.GetString expects a byte[] argument")
	}
	// GetString(bytes, index, count) — the 4-arg (this, bytes, index,
	// count) overload — slices to the requested range.
	if len(args) >= 4 && args[2].Kind == runtime.KindI4 && args[3].Kind == runtime.KindI4 {
		start, count := int(args[2].I4), int(args[3].I4)
		if start >= 0 && count >= 0 && start+count <= len(data) {
			data = data[start : start+count]
		}
	}
	if bigEndian, ok := encodingAsUTF16(args[0]); ok {
		return runtime.String(decodeUTF16(data, bigEndian)), nil
	}
	return runtime.String(string(data)), nil
}

// decodeUTF16 decodes a real UTF-16LE/BE byte slice (Encoding.Unicode/
// BigEndianUnicode.GetString) — an odd trailing byte (malformed input)
// is dropped rather than erroring, matching how the rest of this file
// already tolerates malformed byte-array input.
func decodeUTF16(data []byte, bigEndian bool) string {
	n := len(data) / 2
	units := make([]uint16, n)
	for i := 0; i < n; i++ {
		if bigEndian {
			units[i] = uint16(data[i*2])<<8 | uint16(data[i*2+1])
		} else {
			units[i] = uint16(data[i*2]) | uint16(data[i*2+1])<<8
		}
	}
	return string(utf16.Decode(units))
}

// encodeUTF16 is decodeUTF16's inverse (Encoding.Unicode/BigEndianUnicode.
// GetBytes) — 2 bytes per UTF-16 code unit, surrogate pairs included.
func encodeUTF16(s string, bigEndian bool) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, len(units)*2)
	for i, u := range units {
		if bigEndian {
			out[i*2] = byte(u >> 8)
			out[i*2+1] = byte(u)
		} else {
			out[i*2] = byte(u)
			out[i*2+1] = byte(u >> 8)
		}
	}
	return out
}

func encodingGetBytes(args []runtime.Value) (runtime.Value, error) {
	// Encoding.GetBytes(ReadOnlySpan<char>, Span<byte>) -> int and its
	// netstandard2.0 pointer-taking twin GetBytes(char*, int, byte*, int)
	// -> int (Fase 3.41) both collapse into this same native, same
	// "overload-name-collapse" convention as every other multi-overload
	// native in this file (Fase 3.12) — their 3-arg (this, chars, bytes)
	// and 5-arg (this, chars, charCount, bytes, byteCount) shapes are
	// both unambiguous against the (this, string) shape below.
	if len(args) == 3 || len(args) == 5 {
		return encodingGetBytesSpan(args)
	}
	if len(args) != 2 || args[1].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: Encoding.GetBytes expects a string argument")
	}
	if bigEndian, ok := encodingAsUTF16(args[0]); ok {
		encoded := encodeUTF16(args[1].Str, bigEndian)
		elems := make([]runtime.Value, len(encoded))
		for i, b := range encoded {
			elems[i] = runtime.Int32(int32(b))
		}
		return runtime.ArrRef(&runtime.Array{Elems: elems}), nil
	}
	data := []byte(args[1].Str)
	elems := make([]runtime.Value, len(data))
	for i, b := range data {
		elems[i] = runtime.Int32(int32(b))
	}
	return runtime.ArrRef(&runtime.Array{Elems: elems}), nil
}

// spanStructArg unwraps a Span<T>/ReadOnlySpan<T> argument to its backing
// Struct, however it arrives: a plain by-value KindStruct (the ordinary
// ReadOnlySpan<char>/Span<byte>-typed parameter shape), a KindRef to one
// (an actual `ref`/`in` parameter), OR a KindRef produced by
// spanGetPinnableReference (system_span.go) — which, per its own doc
// comment, hands back a ref to a freshly reboxed copy of the same span
// struct as vmnet's stand-in for a real `fixed (T* p = span)` pointer.
// Both KindRef shapes collapse to the same deref here, so callers below
// don't need to know which one they got.
func spanStructArg(v runtime.Value) (*runtime.Struct, bool) {
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	if v.Kind != runtime.KindStruct || v.Struct == nil {
		return nil, false
	}
	return v.Struct, true
}

// spanCharArg extracts a ReadOnlySpan<char>/Span<char> argument's actual
// character content (Fase 3.41) — shared by the Encoding.GetByteCount/
// GetBytes natives below. Needed for a real, load-bearing case: System.
// Text.Json 8.0.5's netstandard2.0 build (the TFM vmnet's own nuget.go
// actually selects, favoring netstandard2.0 over net8.0) has
// JsonReaderHelper.GetUtf8ByteCount/GetUtf8FromText (JsonDocument.Parse
// (string, JsonDocumentOptions)'s real transcoding step) do `fixed (char*
// chars = text) { return s_utf8Encoding.GetByteCount(chars, text.Length);
// }` — real Encoding pointer-taking overloads with no IL vmnet has
// loaded anywhere (no real System.Private.CoreLib assembly), and no
// native registered here at all before this Fase, unlike the plain
// (string)-shaped GetBytes/GetString above. Before spanGetPinnableReference
// (system_span.go) existed, the `fixed` pattern's own pointer conversion
// failed outright ("cannot convert value kind 9 (KindRef) to integer",
// arithmetic.go's evalConv) — now the "pointer" arriving here is a
// KindRef to a reboxed span struct (spanStructArg unwraps either that or
// a plain by-value span, so the same native serves BOTH the pointer-
// taking overloads AND the plain ReadOnlySpan<char>-taking ones a
// different TFM build might call instead).
func spanCharArg(v runtime.Value) (string, bool) {
	s, ok := spanStructArg(v)
	if !ok {
		return "", false
	}
	return spanToStringValue(s)
}

// encodingGetByteCount backs Encoding.GetByteCount for BOTH real
// overloads that reach it (Fase 3.41): the plain ReadOnlySpan<char>-
// taking one (args = this, span — 2 total) and netstandard2.0's pointer-
// taking one (args = this, chars-pointer, charCount — 3 total, the extra
// int simply not needed since spanCharArg already reads the exact
// pinned span's own bounded content). See spanCharArg's doc comment for
// which real call site needs which shape.
// encodingGetMaxByteCount backs Encoding.GetMaxByteCount(int charCount) —
// real UTF8Encoding's own worst-case formula ((charCount+1)*3, the "+1"
// covering a possible trailing incomplete surrogate needing a
// replacement character) — found via a real, load-bearing case (Fase
// 3.41): System.Text.Json's own JsonDocument.TryGetNamedPropertyValue
// sizes a stackalloc scratch buffer from this before transcoding a
// property name for comparison.
func encodingGetMaxByteCount(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[1].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: Encoding.GetMaxByteCount expects an int charCount")
	}
	return runtime.Int32((args[1].I4 + 1) * 3), nil
}

func encodingGetByteCount(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: Encoding.GetByteCount expects a ReadOnlySpan<char> argument")
	}
	text, ok := spanCharArg(args[1])
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: Encoding.GetByteCount expects a ReadOnlySpan<char> argument")
	}
	if bigEndian, ok := encodingAsUTF16(args[0]); ok {
		return runtime.Int32(int32(len(encodeUTF16(text, bigEndian)))), nil
	}
	// vmnet stores every managed string as a Go string, which is always
	// already UTF-8 — its own byte length IS the real UTF-8 encoding's
	// byte count, no actual transcoding needed.
	return runtime.Int32(int32(len(text))), nil
}

// encodingGetBytesSpan backs Encoding.GetBytes for BOTH real overloads
// that reach it (Fase 3.41, see spanCharArg's doc comment): the plain
// (ReadOnlySpan<char>, Span<byte>) one (args = this, chars, dest — 3
// total) and netstandard2.0's pointer-taking one (args = this, chars-
// pointer, charCount, bytes-pointer, byteCount — 5 total). Either way,
// writes the UTF-8 (or UTF-16, for a real Encoding.Unicode/
// BigEndianUnicode receiver) encoding of the source into destination's
// own backing array and returns the number of bytes written, real
// semantics (ArgumentException when destination is too short) included.
func encodingGetBytesSpan(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 3 && len(args) != 5 {
		return runtime.Value{}, fmt.Errorf("bcl: Encoding.GetBytes: unsupported argument count %d", len(args))
	}
	text, ok := spanCharArg(args[1])
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: Encoding.GetBytes: first argument isn't a char span")
	}
	// destArg is args[2] for the (ReadOnlySpan<char>, Span<byte>) shape,
	// args[3] for the (char*, int, byte*, int) pointer shape — the extra
	// int in between (charCount) isn't needed, same reasoning as
	// encodingGetByteCount above.
	destArg := args[2]
	if len(args) == 5 {
		destArg = args[3]
	}
	destStruct, ok := spanStructArg(destArg)
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: Encoding.GetBytes: destination argument isn't a byte span")
	}
	var encoded []byte
	if bigEndian, ok := encodingAsUTF16(args[0]); ok {
		encoded = encodeUTF16(text, bigEndian)
	} else {
		encoded = []byte(text)
	}
	backing := destStruct.Fields[0]
	if backing.Kind != runtime.KindArray || backing.Arr == nil {
		return runtime.Value{}, fmt.Errorf("bcl: Encoding.GetBytes: destination span has no backing array")
	}
	start, length := int(destStruct.Fields[1].I4), int(destStruct.Fields[2].I4)
	if len(encoded) > length {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentException", Message: "Destination is too short."}
	}
	for i, b := range encoded {
		backing.Arr.Elems[start+i] = runtime.Int32(int32(b))
	}
	return runtime.Int32(int32(len(encoded))), nil
}
