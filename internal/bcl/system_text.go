package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// Encoding.UTF8/GetEncoding return a stateless instance; vmnet's
// GetString/GetBytes ignore the receiver entirely (no real multi-codepage
// table — every encoding behaves as UTF-8/ASCII passthrough, a documented
// simplification), so any object works as the "instance".
func init() {
	register("System.Text.Encoding::get_UTF8", true, encodingGetUTF8)
	register("System.Text.Encoding::get_ASCII", true, encodingGetUTF8)
	register("System.Text.Encoding::get_Default", true, encodingGetUTF8)
	register("System.Text.Encoding::GetEncoding", true, encodingGetUTF8)
	register("System.Text.Encoding::GetString", true, encodingGetString)
	register("System.Text.Encoding::GetBytes", true, encodingGetBytes)
}

func encodingGetUTF8(args []runtime.Value) (runtime.Value, error) {
	return runtime.ObjRef(&runtime.Object{}), nil
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
	return runtime.String(string(data)), nil
}

func encodingGetBytes(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 || args[1].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: Encoding.GetBytes expects a string argument")
	}
	data := []byte(args[1].Str)
	elems := make([]runtime.Value, len(data))
	for i, b := range data {
		elems[i] = runtime.Int32(int32(b))
	}
	return runtime.ArrRef(&runtime.Array{Elems: elems}), nil
}
