package bcl

import (
	"crypto/rand"
	"fmt"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// guidType models System.Guid as its canonical lowercase "D"-format
// string ("00000000-0000-0000-0000-000000000000") rather than the real
// 16-byte layout — every real caller found in this loop's target
// packages (DocumentFormat.OpenXml's own temporary rewritten-part-URI
// generator, `$"rewritten://{Guid.NewGuid()}"`) only ever needs a
// unique, string-shaped identity, never the raw bytes or a non-default
// ToString() format.
var guidType = runtime.NewValueType(
	"System", "Guid",
	[]string{"value"},
	[]runtime.Value{runtime.String("00000000-0000-0000-0000-000000000000")},
)

func init() {
	registerValueType(guidType)
	registerValueTypeCtor("System.Guid", guidCtor)
	// `var g = new Guid(...);` assigned straight to a local compiles to
	// `ldloca`+`call instance .ctor`, not `newobj` — same real gap
	// system_collections.go's own KeyValuePair`2::.ctor registration
	// already documents and fixes for that type; found auditing every
	// registerValueTypeCtor entry for a missing in-place counterpart
	// (Fase 3.74).
	register("System.Guid::.ctor", false, guidCtorInPlace)
	register("System.Guid::NewGuid", true, guidNewGuid)
	register("System.Guid::ToString", true, guidToString)
	register("System.Guid::Equals", true, guidEquals)
	register("System.Guid::op_Equality", true, guidEquals)
	register("System.Guid::op_Inequality", true, guidNotEquals)
	register("System.Guid::GetHashCode", true, guidGetHashCode)
}

func newGuidString() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("bcl: Guid.NewGuid: %w", err)
	}
	// RFC 4122 version 4 (random) — sets the version/variant bits real
	// Guid.NewGuid() also sets, so a formatted value looks like a real
	// v4 GUID rather than raw random hex.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func guidNewGuid(args []runtime.Value) (runtime.Value, error) {
	s, err := newGuidString()
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.StructVal(&runtime.Struct{Type: guidType, Fields: []runtime.Value{runtime.String(s)}}), nil
}

func guidCtor(args []runtime.Value) (*runtime.Struct, error) {
	s, err := newGuidString()
	if err != nil {
		return nil, err
	}
	if len(args) > 0 && args[0].Kind == runtime.KindString {
		s = strings.ToLower(strings.TrimSpace(args[0].Str))
	}
	return &runtime.Struct{Type: guidType, Fields: []runtime.Value{runtime.String(s)}}, nil
}

// guidCtorInPlace mirrors guidCtor for the ldloca+call.ctor shape —
// args[0] is a KindRef to the already-allocated struct slot.
func guidCtorInPlace(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindRef || args[0].Ref == nil {
		return runtime.Value{}, fmt.Errorf("bcl: Guid constructor called without a receiver")
	}
	s, err := guidCtor(args[1:])
	if err != nil {
		return runtime.Value{}, err
	}
	*args[0].Ref = runtime.StructVal(s)
	return runtime.Value{}, nil
}

func asGuidString(v runtime.Value) (string, bool) {
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	if v.Kind != runtime.KindStruct || v.Struct == nil || len(v.Struct.Fields) == 0 {
		return "", false
	}
	return v.Struct.Fields[0].Str, true
}

func guidToString(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 {
		return runtime.Value{}, fmt.Errorf("bcl: Guid.ToString called without a receiver")
	}
	s, ok := asGuidString(args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: Guid.ToString receiver is not a Guid")
	}
	return runtime.String(s), nil
}

func guidEquals(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: Guid.Equals expects 2 arguments")
	}
	a, aok := asGuidString(args[0])
	b, bok := asGuidString(args[1])
	return runtime.Bool(aok && bok && a == b), nil
}

func guidNotEquals(args []runtime.Value) (runtime.Value, error) {
	v, err := guidEquals(args)
	if err != nil {
		return v, err
	}
	return runtime.Bool(v.I4 == 0), nil
}

func guidGetHashCode(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 {
		return runtime.Value{}, fmt.Errorf("bcl: Guid.GetHashCode called without a receiver")
	}
	s, ok := asGuidString(args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: Guid.GetHashCode receiver is not a Guid")
	}
	h := int32(0)
	for i := 0; i < len(s); i++ {
		h = h*31 + int32(s[i])
	}
	return runtime.Int32(h), nil
}
