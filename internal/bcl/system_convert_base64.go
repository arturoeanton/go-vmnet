package bcl

import (
	"encoding/base64"
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// Convert.ToBase64String/FromBase64String are among the most common real
// .NET Core BCL surface not yet covered anywhere in vmnet (Fase 3.42,
// general IL/BCL hardening pass) — embedding binary data as text (crypto
// hashes, tokens, images, OOXML's own binary custom-property blobs) is
// pervasive in real-world code well beyond this project's original NPOI/
// ClosedXML/OpenXml/System.Text.Json target set.
func init() {
	register("System.Convert::ToBase64String", true, convertToBase64String)
	register("System.Convert::FromBase64String", true, convertFromBase64String)
	register("System.Convert::TryToBase64Chars", true, convertTryToBase64Chars)
}

// convertToBase64String covers every real overload that reaches here —
// (byte[]), (byte[], Base64FormattingOptions), and (byte[], int offset,
// int length[, Base64FormattingOptions]) — Base64FormattingOptions is
// ignored (vmnet has no concept of inserted line breaks in its own
// output; real code producing OOXML/JSON/HTTP payloads never wants them
// either) and offset/length slice the source first when both present.
func convertToBase64String(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: Convert.ToBase64String expects a byte[] argument")
	}
	data, ok := byteArrayArgToBytes(args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: Convert.ToBase64String expects a byte[] argument")
	}
	if len(args) >= 3 && args[1].Kind == runtime.KindI4 && args[2].Kind == runtime.KindI4 {
		start, length := int(args[1].I4), int(args[2].I4)
		if start < 0 || length < 0 || start+length > len(data) {
			return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "offset or length"}
		}
		data = data[start : start+length]
	}
	return runtime.String(base64.StdEncoding.EncodeToString(data)), nil
}

// convertFromBase64String decodes a real Base64 string back to a byte[]
// (Fase 3.42) — matches real Convert.FromBase64String's FormatException
// on malformed input rather than silently truncating/ignoring bad
// characters.
func convertFromBase64String(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: Convert.FromBase64String expects a string")
	}
	data, err := base64.StdEncoding.DecodeString(args[0].Str)
	if err != nil {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.FormatException", Message: "The input is not a valid Base-64 string as it contains a non-base 64 character, more than two padding characters, or an illegal character among the padding characters."}
	}
	elems := make([]runtime.Value, len(data))
	for i, b := range data {
		elems[i] = runtime.Int32(int32(b))
	}
	return runtime.ArrRef(&runtime.Array{Elems: elems}), nil
}

// convertTryToBase64Chars backs Convert.TryToBase64Chars(ReadOnlySpan
// <byte> bytes, Span<char> chars, out int charsWritten) — a real,
// allocation-free overload some low-level serialization code prefers
// over ToBase64String. vmnet's char-span support already models a
// Span<char> exactly like a byte span (system_span.go); this writes the
// encoded characters as individual KindI4 code points into the
// destination's own backing array.
func convertTryToBase64Chars(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 3 {
		return runtime.Value{}, fmt.Errorf("bcl: Convert.TryToBase64Chars expects (bytes, chars, out charsWritten)")
	}
	data, ok := byteArrayArgToBytes(args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: Convert.TryToBase64Chars: first argument is not a byte span")
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	backing, start, length, ok := SpanBacking(args[1])
	if !ok || backing.Kind != runtime.KindArray || backing.Arr == nil {
		return runtime.Value{}, fmt.Errorf("bcl: Convert.TryToBase64Chars: second argument is not a char span")
	}
	if len(encoded) > length {
		if args[2].Kind == runtime.KindRef && args[2].Ref != nil {
			*args[2].Ref = runtime.Int32(0)
		}
		return runtime.Bool(false), nil
	}
	dst := backing.Arr.Elems
	for i, r := range encoded {
		dst[start+i] = runtime.Int32(int32(r))
	}
	if args[2].Kind == runtime.KindRef && args[2].Ref != nil {
		*args[2].Ref = runtime.Int32(int32(len(encoded)))
	}
	return runtime.Bool(true), nil
}
