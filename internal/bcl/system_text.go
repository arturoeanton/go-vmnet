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
func byteArrayArgToBytes(v runtime.Value) ([]byte, bool) {
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
