package interpreter

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// System.Runtime.InteropServices.MemoryMarshal.Read<T>/Write<T> (backed
// by Unsafe.ReadUnaligned<T>/WriteUnaligned<T> in real CoreLib, both real
// JIT intrinsics with no interpretable IL body of their own) reinterpret
// raw bytes as an arbitrary value type T — found via a real, load-bearing
// case (Fase 3.41): System.Text.Json's own JsonDocument.MetadataDb packs
// each parsed token as a 12-byte `DbRow` struct (3 packed int32 fields)
// directly into a rented byte[] buffer via MemoryMarshal.Write, then
// reads back either the whole struct (MemoryMarshal.Read<DbRow>) or a
// single packed int32 field at a byte offset (MemoryMarshal.Read<int>/
// Write<int>) to mutate bit-packed flags in place.
//
// vmnet's byte[] is already a plain KindArray of one-KindI4-per-byte
// elements (0-255 each — see internal/bcl/system_unsafe.go's own doc
// comment, "a byte offset IS an element index one-for-one"), so real
// byte-level reinterpretation is possible generically: T's own real
// Value shape (for Write, already in hand) or its Type.FieldDefaults
// (for Read, giving each field's real Kind/width) says exactly how many
// bytes each primitive/field occupies and in what order — sequential,
// matching every real caller found here (structs of same-size int32
// fields, where the CLR's default "Auto" layout is observably sequential
// anyway). This is NOT a general unsafe-memory model (spec's own
// pure-Go, no-tricks non-goal stands) — only value <-> consecutive-bytes
// marshaling for the Kinds vmnet actually has (I4/I8/R4/R8, and structs
// built purely from those), which is what every real caller of this
// idiom in the wild (binary file/protocol parsers, not just System.Text.
// Json) needs.
func init() {
	genericMachineRegistry["System.Runtime.InteropServices.MemoryMarshal::Write"] = memoryMarshalWrite
	genericMachineRegistry["System.Runtime.InteropServices.MemoryMarshal::Read"] = memoryMarshalRead
}

func memoryMarshalWrite(m *Machine, args []runtime.Value, methodGenericArgs []string, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: MemoryMarshal.Write<T> expects (Span<byte>, in T)")
	}
	backing, start, _, ok := bcl.SpanBacking(args[0])
	if !ok || backing.Kind != runtime.KindArray || backing.Arr == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: MemoryMarshal.Write<T>: destination is not a byte-array-backed span")
	}
	value := args[1]
	if value.Kind == runtime.KindRef && value.Ref != nil {
		value = *value.Ref
	}
	encoded, err := encodeValueBytes(value)
	if err != nil {
		return runtime.Value{}, err
	}
	dst := backing.Arr.Elems
	if start+len(encoded) > len(dst) {
		return runtime.Value{}, fmt.Errorf("interpreter: MemoryMarshal.Write<T>: destination span too small (need %d bytes)", len(encoded))
	}
	for i, b := range encoded {
		dst[start+i] = runtime.Int32(int32(b))
	}
	return runtime.Value{}, nil
}

func memoryMarshalRead(m *Machine, args []runtime.Value, methodGenericArgs []string, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 || len(methodGenericArgs) < 1 || methodGenericArgs[0] == "" {
		return runtime.Value{}, fmt.Errorf("interpreter: MemoryMarshal.Read<T> expects a resolved T and a ReadOnlySpan<byte> source")
	}
	backing, start, length, ok := bcl.SpanBacking(args[0])
	if !ok || backing.Kind != runtime.KindArray || backing.Arr == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: MemoryMarshal.Read<T>: source is not a byte-array-backed span")
	}
	elems := backing.Arr.Elems
	if start+length > len(elems) {
		length = len(elems) - start
	}
	raw := make([]byte, length)
	for i := 0; i < length; i++ {
		raw[i] = byte(elems[start+i].I4)
	}
	v, _, err := decodeValueBytes(m, methodGenericArgs[0], raw)
	return v, err
}

// encodeValueBytes serializes v into its real little-endian byte layout —
// a struct recurses field-by-field, in declaration order (Fields is
// already ordered this way — see runtime.Type.Fields's own doc comment).
func encodeValueBytes(v runtime.Value) ([]byte, error) {
	switch v.Kind {
	case runtime.KindI4:
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], uint32(v.I4))
		return b[:], nil
	case runtime.KindI8:
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], uint64(v.I8))
		return b[:], nil
	case runtime.KindR4:
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], math.Float32bits(v.R4))
		return b[:], nil
	case runtime.KindR8:
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], math.Float64bits(v.R8))
		return b[:], nil
	case runtime.KindStruct:
		if v.Struct == nil {
			return nil, fmt.Errorf("interpreter: MemoryMarshal.Write<T>: nil struct value")
		}
		var out []byte
		for _, f := range v.Struct.Fields {
			enc, err := encodeValueBytes(f)
			if err != nil {
				return nil, err
			}
			out = append(out, enc...)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("interpreter: MemoryMarshal.Write<T>: unsupported value kind %v for byte-level marshaling", v.Kind)
	}
}

// decodeValueBytes reconstructs a T-shaped Value (primitive or struct)
// from raw, consuming exactly as many bytes as T's own shape needs;
// consumed reports how many of raw's bytes were used, so a struct's own
// field-by-field decode can advance through the same buffer.
func decodeValueBytes(m *Machine, typeFullName string, raw []byte) (v runtime.Value, consumed int, err error) {
	open := bcl.GenericOpenName(typeFullName)
	switch open {
	case "System.Int32", "System.UInt32":
		if len(raw) < 4 {
			return runtime.Value{}, 0, fmt.Errorf("interpreter: MemoryMarshal.Read<T>: not enough bytes for %s", open)
		}
		return runtime.Int32(int32(binary.LittleEndian.Uint32(raw))), 4, nil
	case "System.Int64", "System.UInt64":
		if len(raw) < 8 {
			return runtime.Value{}, 0, fmt.Errorf("interpreter: MemoryMarshal.Read<T>: not enough bytes for %s", open)
		}
		return runtime.Int64(int64(binary.LittleEndian.Uint64(raw))), 8, nil
	case "System.Single":
		if len(raw) < 4 {
			return runtime.Value{}, 0, fmt.Errorf("interpreter: MemoryMarshal.Read<T>: not enough bytes for %s", open)
		}
		return runtime.Float32(math.Float32frombits(binary.LittleEndian.Uint32(raw))), 4, nil
	case "System.Double":
		if len(raw) < 8 {
			return runtime.Value{}, 0, fmt.Errorf("interpreter: MemoryMarshal.Read<T>: not enough bytes for %s", open)
		}
		return runtime.Float64(math.Float64frombits(binary.LittleEndian.Uint64(raw))), 8, nil
	case "System.Byte", "System.SByte":
		if len(raw) < 1 {
			return runtime.Value{}, 0, fmt.Errorf("interpreter: MemoryMarshal.Read<T>: not enough bytes for %s", open)
		}
		return runtime.Int32(int32(raw[0])), 1, nil
	case "System.Int16", "System.UInt16":
		if len(raw) < 2 {
			return runtime.Value{}, 0, fmt.Errorf("interpreter: MemoryMarshal.Read<T>: not enough bytes for %s", open)
		}
		return runtime.Int32(int32(binary.LittleEndian.Uint16(raw))), 2, nil
	}
	if m.ResolveType == nil {
		return runtime.Value{}, 0, fmt.Errorf("interpreter: MemoryMarshal.Read<T>: %q is not a known primitive and no type resolver is available", typeFullName)
	}
	t, rerr := m.ResolveType(open)
	if rerr != nil {
		return runtime.Value{}, 0, fmt.Errorf("interpreter: MemoryMarshal.Read<T>: %q: %w", typeFullName, rerr)
	}
	fields := make([]runtime.Value, len(t.FieldDefaults))
	offset := 0
	for i, def := range t.FieldDefaults {
		fv, n, ferr := decodeValueBytes(m, primitiveTypeNameForKind(def.Kind), raw[offset:])
		if ferr != nil {
			return runtime.Value{}, 0, fmt.Errorf("interpreter: MemoryMarshal.Read<T>: field %d of %q: %w", i, typeFullName, ferr)
		}
		fields[i] = fv
		offset += n
	}
	return runtime.StructVal(&runtime.Struct{Type: t, Fields: fields}), offset, nil
}

// primitiveTypeNameForKind answers the reverse of decodeValueBytes' own
// primitive-name switch — enough to decode a struct field whose real
// declared type is erased by the time it reaches here (t.FieldDefaults
// only carries each field's zero-valued Kind, not its original type
// name), covering exactly the Kinds a struct field can actually have
// (I4/I8/R4/R8 — every other Kind falls back to Int32, matching the
// overwhelmingly common real-world case: every field of this idiom's
// known real callers is a plain int32).
func primitiveTypeNameForKind(k runtime.Kind) string {
	switch k {
	case runtime.KindI8:
		return "System.Int64"
	case runtime.KindR4:
		return "System.Single"
	case runtime.KindR8:
		return "System.Double"
	default:
		return "System.Int32"
	}
}
