package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// Encoding.UTF8 is a static property returning a stateless instance;
// vmnet's GetString/GetBytes ignore the receiver entirely, so any object
// works as the "instance".
func init() {
	register("System.Text.Encoding::get_UTF8", true, encodingGetUTF8)
	register("System.Text.Encoding::GetString", true, encodingGetString)
	register("System.Text.Encoding::GetBytes", true, encodingGetBytes)
}

func encodingGetUTF8(args []runtime.Value) (runtime.Value, error) {
	return runtime.ObjRef(&runtime.Object{}), nil
}

func encodingGetString(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 || args[1].Kind != runtime.KindBytes {
		return runtime.Value{}, fmt.Errorf("bcl: Encoding.GetString expects a byte[] argument")
	}
	return runtime.String(string(args[1].Bytes)), nil
}

func encodingGetBytes(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 || args[1].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: Encoding.GetBytes expects a string argument")
	}
	return runtime.Bytes([]byte(args[1].Str)), nil
}
