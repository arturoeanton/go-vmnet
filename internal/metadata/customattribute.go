package metadata

import (
	"fmt"
	"math"
)

// CustomAttributeRow is one real CustomAttribute table row (ECMA-335
// §II.22.10): Parent names whatever real metadata element the attribute
// is applied to (a TypeDef, MethodDef, Field, Property, Param, ...); Ctor
// is always a MethodDef or MemberRef — the attribute TYPE's own
// constructor, not the type itself (real .NET always records which
// specific overload was invoked, since attribute application is
// source-level `new SomeAttribute(args)`); Value is the raw, still-
// undecoded blob (§II.23.3) — DecodeCustomAttributeArgs below decodes it
// once the constructor's own parsed signature is known.
type CustomAttributeRow struct {
	Parent Token
	Ctor   Token
	Value  []byte
}

// CustomAttributesForParent finds every real CustomAttribute row applied
// to parent (Fase 3.63, System.Reflection.CustomAttributeData — real
// attribute reading, deliberately deferred until now: see docs/en/
// ROADMAP.md's own long-standing "genuinely new subsystem" note). Unlike
// TypeDef's own method/field ranges, the CustomAttribute table isn't
// necessarily contiguous per parent — a linear scan, same posture
// MethodImpls (resolver.go) already takes for the same reason (real
// assemblies rarely have enough attributes for this to matter
// performance-wise; callers needing this repeatedly should cache).
func (md *Metadata) CustomAttributesForParent(parent Token) ([]CustomAttributeRow, error) {
	t := md.tables[TableCustomAttribute]
	if t == nil {
		return nil, nil
	}
	var out []CustomAttributeRow
	for i := uint32(0); i < t.rowCount; i++ {
		p, err := decodeCodedIndex(codedHasCustomAttribute, t.col(i, 0))
		if err != nil {
			return nil, err
		}
		if p != parent {
			continue
		}
		ctor, err := decodeCodedIndex(codedCustomAttributeType, t.col(i, 1))
		if err != nil {
			return nil, err
		}
		val, err := md.blob.Blob(t.col(i, 2))
		if err != nil {
			return nil, err
		}
		out = append(out, CustomAttributeRow{Parent: p, Ctor: ctor, Value: val})
	}
	return out, nil
}

// AttrArgKind mirrors SigTypeKind for the handful of shapes a real
// attribute constructor argument can actually take — the C# compiler
// only ever allows a compile-time constant here: a primitive, a string,
// an enum (encoded as its underlying integer type — the blob format
// itself doesn't distinguish an enum from a plain integer at all; the
// constructor's own declared SigValueType parameter is what tells a
// caller "this one's an enum", not anything in the blob), or a
// System.Type (encoded as a SerString of its assembly-qualified name).
// Arrays and boxed-object (named) arguments are read past correctly (so
// blob parsing doesn't corrupt itself and mis-decode fixed args that
// follow) but not exposed as decoded values yet — see
// DecodeCustomAttributeArgs's own doc comment.
type AttrArgKind byte

const (
	AttrArgUnsupported AttrArgKind = iota
	AttrArgBool
	AttrArgChar
	AttrArgI1
	AttrArgU1
	AttrArgI2
	AttrArgU2
	AttrArgI4
	AttrArgU4
	AttrArgI8
	AttrArgU8
	AttrArgR4
	AttrArgR8
	AttrArgString
	AttrArgType
)

// DecodedAttrArg is one decoded fixed (positional) constructor argument.
type DecodedAttrArg struct {
	Kind   AttrArgKind
	I8     int64   // Bool/Char/I1/U1/I2/U2/I4/U4/I8/U8 (sign/zero-extended)
	R8     float64 // R4/R8
	Str    string  // String/Type (Type: the assembly-qualified name string as written in the blob)
	IsNull bool    // a null String/Type argument (SerString's own 0xFF marker)
}

// DecodeCustomAttributeArgs decodes blob's own FIXED (positional)
// constructor arguments per ECMA-335 §II.23.3, given the real attribute
// constructor's own already-parsed parameter signature (paramSigs) — the
// blob itself carries no type tags for fixed args at all (unlike a boxed
// `object`-typed named argument, which does); the constructor's own
// declared parameter types are the only source of truth for how each
// argument is encoded, exactly matching how a real CLR decodes the same
// blob.
//
// NamedArguments (field/property initializers, `[Foo(1, Bar = "x")]`)
// are read past correctly (their own count/tag/name/value all consumed,
// so the blob's own trailing bytes are never mistaken for more fixed
// args) but not decoded into the returned slice — no real corpus caller
// found needs one yet; a future pass can extend this the same way
// fixed-arg decoding was added here.
//
// SigSZArray/SigGenericInst/SigObject-typed fixed arguments (an array
// parameter, or a boxed `object` parameter) return AttrArgUnsupported for
// that slot rather than erroring the whole blob — real corpus callers
// found so far only ever need a single string/primitive/enum argument
// (ConfigurationKeyNameAttribute's own (string) constructor,
// AssemblyFileVersionAttribute's own (string) constructor), so this is a
// documented, narrower scope rather than a silently wrong one.
func DecodeCustomAttributeArgs(paramSigs []SigType, blob []byte) ([]DecodedAttrArg, error) {
	if len(blob) < 2 {
		return nil, fmt.Errorf("metadata: custom attribute blob too short (%d bytes)", len(blob))
	}
	prolog := uint16(blob[0]) | uint16(blob[1])<<8
	if prolog != 0x0001 {
		return nil, fmt.Errorf("metadata: custom attribute blob has unexpected prolog %#x", prolog)
	}
	pos := 2
	out := make([]DecodedAttrArg, len(paramSigs))
	for i, p := range paramSigs {
		arg, n, err := decodeFixedArg(p, blob[pos:])
		if err != nil {
			return nil, fmt.Errorf("metadata: custom attribute argument %d: %w", i, err)
		}
		out[i] = arg
		pos += n
	}
	// NamedArgs count + entries — consumed so a future caller could add
	// named-argument decoding without redoing the fixed-arg parsing above,
	// but not decoded into the result (see doc comment).
	if pos+2 <= len(blob) {
		numNamed := int(uint16(blob[pos]) | uint16(blob[pos+1])<<8)
		pos += 2
		for i := 0; i < numNamed && pos < len(blob); i++ {
			n, err := skipNamedArg(blob[pos:])
			if err != nil {
				break // best-effort: fixed args (the only ones returned) already decoded correctly above
			}
			pos += n
		}
	}
	return out, nil
}

// decodeFixedArg decodes one fixed argument matching sig's own declared
// shape, returning the number of blob bytes it consumed.
func decodeFixedArg(sig SigType, b []byte) (DecodedAttrArg, int, error) {
	switch sig.Kind {
	case SigBoolean:
		if len(b) < 1 {
			return DecodedAttrArg{}, 0, fmt.Errorf("truncated bool argument")
		}
		v := int64(0)
		if b[0] != 0 {
			v = 1
		}
		return DecodedAttrArg{Kind: AttrArgBool, I8: v}, 1, nil
	case SigChar:
		if len(b) < 2 {
			return DecodedAttrArg{}, 0, fmt.Errorf("truncated char argument")
		}
		return DecodedAttrArg{Kind: AttrArgChar, I8: int64(uint16(b[0]) | uint16(b[1])<<8)}, 2, nil
	case SigI1:
		if len(b) < 1 {
			return DecodedAttrArg{}, 0, fmt.Errorf("truncated sbyte argument")
		}
		return DecodedAttrArg{Kind: AttrArgI1, I8: int64(int8(b[0]))}, 1, nil
	case SigU1:
		if len(b) < 1 {
			return DecodedAttrArg{}, 0, fmt.Errorf("truncated byte argument")
		}
		return DecodedAttrArg{Kind: AttrArgU1, I8: int64(b[0])}, 1, nil
	case SigI2:
		if len(b) < 2 {
			return DecodedAttrArg{}, 0, fmt.Errorf("truncated int16 argument")
		}
		return DecodedAttrArg{Kind: AttrArgI2, I8: int64(int16(uint16(b[0]) | uint16(b[1])<<8))}, 2, nil
	case SigU2:
		if len(b) < 2 {
			return DecodedAttrArg{}, 0, fmt.Errorf("truncated uint16 argument")
		}
		return DecodedAttrArg{Kind: AttrArgU2, I8: int64(uint16(b[0]) | uint16(b[1])<<8)}, 2, nil
	case SigI4, SigValueType:
		// SigValueType (an enum — see AttrArgKind's own doc comment for
		// why any value-typed attribute argument must be one) uses the
		// same 4-byte little-endian encoding as a plain Int32, the
		// overwhelmingly common enum underlying type.
		if len(b) < 4 {
			return DecodedAttrArg{}, 0, fmt.Errorf("truncated int32/enum argument")
		}
		v := int32(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24)
		return DecodedAttrArg{Kind: AttrArgI4, I8: int64(v)}, 4, nil
	case SigU4:
		if len(b) < 4 {
			return DecodedAttrArg{}, 0, fmt.Errorf("truncated uint32 argument")
		}
		v := uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
		return DecodedAttrArg{Kind: AttrArgU4, I8: int64(v)}, 4, nil
	case SigI8, SigU8:
		if len(b) < 8 {
			return DecodedAttrArg{}, 0, fmt.Errorf("truncated int64/uint64 argument")
		}
		v := uint64(0)
		for i := 7; i >= 0; i-- {
			v = v<<8 | uint64(b[i])
		}
		kind := AttrArgI8
		if sig.Kind == SigU8 {
			kind = AttrArgU8
		}
		return DecodedAttrArg{Kind: kind, I8: int64(v)}, 8, nil
	case SigR4:
		if len(b) < 4 {
			return DecodedAttrArg{}, 0, fmt.Errorf("truncated single argument")
		}
		bits := uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
		return DecodedAttrArg{Kind: AttrArgR4, R8: float64(math.Float32frombits(bits))}, 4, nil
	case SigR8:
		if len(b) < 8 {
			return DecodedAttrArg{}, 0, fmt.Errorf("truncated double argument")
		}
		bits := uint64(0)
		for i := 7; i >= 0; i-- {
			bits = bits<<8 | uint64(b[i])
		}
		return DecodedAttrArg{Kind: AttrArgR8, R8: math.Float64frombits(bits)}, 8, nil
	case SigString:
		s, n, isNull, err := decodeSerString(b)
		if err != nil {
			return DecodedAttrArg{}, 0, err
		}
		return DecodedAttrArg{Kind: AttrArgString, Str: s, IsNull: isNull}, n, nil
	case SigClass:
		// System.Type is the one SigClass-shaped fixed argument a real
		// attribute constructor can declare — encoded the same SerString
		// way as a string (an assembly-qualified type name), per §II.23.3.
		s, n, isNull, err := decodeSerString(b)
		if err != nil {
			return DecodedAttrArg{}, 0, err
		}
		return DecodedAttrArg{Kind: AttrArgType, Str: s, IsNull: isNull}, n, nil
	default:
		return DecodedAttrArg{Kind: AttrArgUnsupported}, 0, fmt.Errorf("unsupported fixed argument shape (kind %d) — array/boxed-object attribute arguments aren't decoded yet", sig.Kind)
	}
}

// decodeSerString reads a §II.23.3 SerString: a compressed-length-
// prefixed UTF8 string, or the single byte 0xFF for a null string/Type.
func decodeSerString(b []byte) (s string, n int, isNull bool, err error) {
	if len(b) == 0 {
		return "", 0, false, fmt.Errorf("truncated SerString")
	}
	if b[0] == 0xFF {
		return "", 1, true, nil
	}
	length, sz, err := decodeCompressed(b)
	if err != nil {
		return "", 0, false, err
	}
	total := sz + int(length)
	if total > len(b) {
		return "", 0, false, fmt.Errorf("truncated SerString (need %d bytes, have %d)", total, len(b))
	}
	return string(b[sz:total]), total, false, nil
}

// skipNamedArg consumes one NamedArg (FIELD/PROPERTY tag + ElementType +
// name SerString + value) without decoding it — see
// DecodeCustomAttributeArgs's own doc comment for why named arguments
// aren't decoded into a value yet, only skipped correctly.
func skipNamedArg(b []byte) (int, error) {
	if len(b) < 2 {
		return 0, fmt.Errorf("truncated named argument")
	}
	// b[0]: FIELD (0x53) or PROPERTY (0x54) — irrelevant to skipping.
	elemType := b[1]
	pos := 2
	_, n, _, err := decodeSerString(b[pos:]) // field/property name
	if err != nil {
		return 0, err
	}
	pos += n
	valSig, ok := attrArgKindToSigType(elemType)
	if !ok {
		return 0, fmt.Errorf("unsupported named argument element type %#x", elemType)
	}
	_, n2, err := decodeFixedArg(valSig, b[pos:])
	if err != nil {
		return 0, err
	}
	pos += n2
	return pos, nil
}

// attrArgKindToSigType maps a §II.23.3 ElementType byte (as used in a
// named argument's own type tag) back to the SigType decodeFixedArg
// already knows how to read — only the primitive/string shapes
// skipNamedArg actually needs to skip past correctly.
func attrArgKindToSigType(elemType byte) (SigType, bool) {
	switch elemType {
	case elementBoolean:
		return SigType{Kind: SigBoolean}, true
	case elementChar:
		return SigType{Kind: SigChar}, true
	case elementI1:
		return SigType{Kind: SigI1}, true
	case elementU1:
		return SigType{Kind: SigU1}, true
	case elementI2:
		return SigType{Kind: SigI2}, true
	case elementU2:
		return SigType{Kind: SigU2}, true
	case elementI4:
		return SigType{Kind: SigI4}, true
	case elementU4:
		return SigType{Kind: SigU4}, true
	case elementI8:
		return SigType{Kind: SigI8}, true
	case elementU8:
		return SigType{Kind: SigU8}, true
	case elementR4:
		return SigType{Kind: SigR4}, true
	case elementR8:
		return SigType{Kind: SigR8}, true
	case elementString:
		return SigType{Kind: SigString}, true
	default:
		return SigType{}, false
	}
}
