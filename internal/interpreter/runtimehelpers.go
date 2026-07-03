package interpreter

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

func init() {
	machineRegistry["System.Runtime.CompilerServices.RuntimeHelpers::InitializeArray"] = runtimeHelpersInitializeArray
}

// runtimeHelpersInitializeArray backs the array-literal-initializer
// pattern (`newarr` + `dup` + `ldtoken <RVA-backed field>` + `call
// RuntimeHelpers.InitializeArray(array, fieldHandle)`, Fase 3.27) — found
// running real third-party code (Esprima.Character's Unicode range
// tables). The array (already allocated by newarr, still filled with
// zero-Value placeholders — Fase 1's runtime.Array carries no element
// type of its own) gets its real values decoded from the field's raw
// embedded bytes (Machine.ResolveFieldBytes), inferring per-element byte
// width from len(bytes)/len(elements) since there's no other source of
// truth for it at this point. 1/2-byte elements decode unsigned (byte[]/
// char[] are the overwhelmingly common real-world case for those
// widths); 4/8-byte elements decode signed (int[]/long[], matching
// Character's own real tables) — a documented simplification, not a
// general float/double-array reader, since disambiguating "these 4 bytes
// are an int" from "these 4 bytes are a float" isn't possible from the
// blob alone and vmnet has never seen a real RuntimeHelpers.
// InitializeArray call over a float/double array to justify guessing.
func runtimeHelpersInitializeArray(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: RuntimeHelpers.InitializeArray expects 2 arguments")
	}
	arr := args[0]
	if arr.Kind != runtime.KindArray || arr.Arr == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: RuntimeHelpers.InitializeArray: first argument is not an array")
	}
	token := args[1]
	if token.Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("interpreter: RuntimeHelpers.InitializeArray: second argument is not a field token")
	}
	typeFullName, fieldName, ok := strings.Cut(token.Str, "::")
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: RuntimeHelpers.InitializeArray: malformed field token %q", token.Str)
	}
	if m.ResolveFieldBytes == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: RuntimeHelpers.InitializeArray: no field-bytes resolver configured")
	}
	raw, ok := m.ResolveFieldBytes(typeFullName, fieldName)
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: RuntimeHelpers.InitializeArray: field %s.%s has no embedded data", typeFullName, fieldName)
	}
	n := len(arr.Arr.Elems)
	if n == 0 {
		return runtime.Value{}, nil
	}
	if len(raw)%n != 0 {
		return runtime.Value{}, fmt.Errorf("interpreter: RuntimeHelpers.InitializeArray: %d embedded bytes doesn't evenly divide %d elements", len(raw), n)
	}
	elemSize := len(raw) / n
	switch elemSize {
	case 1:
		for i := 0; i < n; i++ {
			arr.Arr.Elems[i] = runtime.Int32(int32(raw[i]))
		}
	case 2:
		for i := 0; i < n; i++ {
			arr.Arr.Elems[i] = runtime.Int32(int32(binary.LittleEndian.Uint16(raw[i*2:])))
		}
	case 4:
		for i := 0; i < n; i++ {
			arr.Arr.Elems[i] = runtime.Int32(int32(binary.LittleEndian.Uint32(raw[i*4:])))
		}
	case 8:
		for i := 0; i < n; i++ {
			arr.Arr.Elems[i] = runtime.Int64(int64(binary.LittleEndian.Uint64(raw[i*8:])))
		}
	default:
		return runtime.Value{}, fmt.Errorf("interpreter: RuntimeHelpers.InitializeArray: unsupported element size %d (%d bytes / %d elements)", elemSize, len(raw), n)
	}
	return runtime.Value{}, nil
}
