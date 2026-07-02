package metadata

import "fmt"

// Element type tags (ECMA-335 §II.23.1.16) used inside method/field/local
// signature blobs.
const (
	elementVoid        = 0x01
	elementBoolean     = 0x02
	elementChar        = 0x03
	elementI1          = 0x04
	elementU1          = 0x05
	elementI2          = 0x06
	elementU2          = 0x07
	elementI4          = 0x08
	elementU4          = 0x09
	elementI8          = 0x0A
	elementU8          = 0x0B
	elementR4          = 0x0C
	elementR8          = 0x0D
	elementString      = 0x0E
	elementPtr         = 0x0F
	elementByRef       = 0x10
	elementValueType   = 0x11
	elementClass       = 0x12
	elementVar         = 0x13
	elementArray       = 0x14
	elementGenericInst = 0x15
	elementTypedByRef  = 0x16
	elementI           = 0x18
	elementU           = 0x19
	elementFnPtr       = 0x1B
	elementObject      = 0x1C
	elementSZArray     = 0x1D
	elementMVar        = 0x1E
	elementCModReqd    = 0x1F
	elementCModOpt     = 0x20
	elementSentinel    = 0x41
	elementPinned      = 0x45
)

// SigTypeKind is a simplified classification of a signature's element
// type. Every element type parses structurally (so trailing params stay
// correctly aligned even when vmnet can't execute the shape), but only
// the kinds through SigGenericParam are distinguished — anything else
// (multi-dim ARRAY, FNPTR, ...) is a hard parse error, not SigUnknown.
type SigTypeKind byte

const (
	SigVoid SigTypeKind = iota
	SigBoolean
	SigChar
	SigI1
	SigU1
	SigI2
	SigU2
	SigI4
	SigU4
	SigI8
	SigU8
	SigR4
	SigR8
	SigI
	SigU
	SigString
	SigObject
	SigClass
	SigValueType
	SigSZArray
	SigGenericInst
	SigPointer      // T* — unmanaged pointer (genuinely "unsafe" code)
	SigByRef        // ref/out/in T — safe, but vmnet doesn't model by-ref call semantics yet
	SigGenericParam // a method/type's own generic parameter (T, not a closed instantiation)
	SigUnknown
)

type SigType struct {
	Kind  SigTypeKind
	Token Token    // set for SigClass / SigValueType / SigGenericInst (the open generic type)
	Elem  *SigType // set for SigSZArray
}

// MethodSig is a parsed MethodDefSig/MethodRefSig (ECMA-335 §II.23.2.1/.2).
type MethodSig struct {
	HasThis       bool
	Generic       bool
	GenParamCount uint32
	ParamCount    uint32
	RetType       SigType
	Params        []SigType
}

// decodeTypeDefOrRefEncoded reads a §II.23.2.8 TypeDefOrRefEncoded value: a
// compressed integer whose low 2 bits select TypeDef/TypeRef/TypeSpec and
// whose remaining bits are the RID. This is distinct from the coded-index
// encoding used by metadata table columns (coded_index.go).
func decodeTypeDefOrRefEncoded(b []byte) (Token, int, error) {
	v, sz, err := decodeCompressed(b)
	if err != nil {
		return 0, 0, err
	}
	tag := v & 0x3
	rid := v >> 2
	var table TableID
	switch tag {
	case 0:
		table = TableTypeDef
	case 1:
		table = TableTypeRef
	case 2:
		table = TableTypeSpec
	default:
		return 0, 0, fmt.Errorf("metadata: invalid TypeDefOrRefEncoded tag %d", tag)
	}
	return NewToken(table, rid), sz, nil
}

// parseType parses one Type production from a signature blob and returns
// how many bytes it consumed.
func parseType(b []byte) (SigType, int, error) {
	pos := 0
	for pos < len(b) && (b[pos] == elementCModReqd || b[pos] == elementCModOpt) {
		pos++
		_, sz, err := decodeTypeDefOrRefEncoded(b[pos:])
		if err != nil {
			return SigType{}, 0, err
		}
		pos += sz
	}
	if pos >= len(b) {
		return SigType{}, 0, fmt.Errorf("metadata: truncated type signature")
	}

	et := b[pos]
	pos++

	switch et {
	case elementVoid:
		return SigType{Kind: SigVoid}, pos, nil
	case elementBoolean:
		return SigType{Kind: SigBoolean}, pos, nil
	case elementChar:
		return SigType{Kind: SigChar}, pos, nil
	case elementI1:
		return SigType{Kind: SigI1}, pos, nil
	case elementU1:
		return SigType{Kind: SigU1}, pos, nil
	case elementI2:
		return SigType{Kind: SigI2}, pos, nil
	case elementU2:
		return SigType{Kind: SigU2}, pos, nil
	case elementI4:
		return SigType{Kind: SigI4}, pos, nil
	case elementU4:
		return SigType{Kind: SigU4}, pos, nil
	case elementI8:
		return SigType{Kind: SigI8}, pos, nil
	case elementU8:
		return SigType{Kind: SigU8}, pos, nil
	case elementR4:
		return SigType{Kind: SigR4}, pos, nil
	case elementR8:
		return SigType{Kind: SigR8}, pos, nil
	case elementI:
		return SigType{Kind: SigI}, pos, nil
	case elementU:
		return SigType{Kind: SigU}, pos, nil
	case elementString:
		return SigType{Kind: SigString}, pos, nil
	case elementObject:
		return SigType{Kind: SigObject}, pos, nil
	case elementClass:
		tok, sz, err := decodeTypeDefOrRefEncoded(b[pos:])
		if err != nil {
			return SigType{}, 0, err
		}
		return SigType{Kind: SigClass, Token: tok}, pos + sz, nil
	case elementValueType:
		tok, sz, err := decodeTypeDefOrRefEncoded(b[pos:])
		if err != nil {
			return SigType{}, 0, err
		}
		return SigType{Kind: SigValueType, Token: tok}, pos + sz, nil
	case elementSZArray:
		elem, sz, err := parseType(b[pos:])
		if err != nil {
			return SigType{}, 0, err
		}
		return SigType{Kind: SigSZArray, Elem: &elem}, pos + sz, nil
	case elementPtr:
		_, sz, err := parseType(b[pos:])
		if err != nil {
			return SigType{}, 0, err
		}
		return SigType{Kind: SigPointer}, pos + sz, nil
	case elementByRef:
		_, sz, err := parseType(b[pos:])
		if err != nil {
			return SigType{}, 0, err
		}
		return SigType{Kind: SigByRef}, pos + sz, nil
	case elementVar, elementMVar:
		_, sz, err := decodeCompressed(b[pos:])
		if err != nil {
			return SigType{}, 0, err
		}
		return SigType{Kind: SigGenericParam}, pos + sz, nil
	case elementGenericInst:
		if pos >= len(b) {
			return SigType{}, 0, fmt.Errorf("metadata: truncated generic instantiation")
		}
		pos++ // CLASS or VALUETYPE marker byte
		openType, sz, err := decodeTypeDefOrRefEncoded(b[pos:])
		if err != nil {
			return SigType{}, 0, err
		}
		pos += sz
		n, sz2, err := decodeCompressed(b[pos:])
		if err != nil {
			return SigType{}, 0, err
		}
		pos += sz2
		for i := uint32(0); i < n; i++ {
			_, sz3, err := parseType(b[pos:])
			if err != nil {
				return SigType{}, 0, err
			}
			pos += sz3
		}
		// Type arguments aren't retained: vmnet's native collection backing
		// (internal/bcl) doesn't need to know T to store a runtime.Value.
		return SigType{Kind: SigGenericInst, Token: openType}, pos, nil
	default:
		return SigType{}, 0, fmt.Errorf("metadata: unsupported signature element type %#x", et)
	}
}

// ParseTypeSpec parses a TypeSpec signature blob (ECMA-335 §II.23.2.14) —
// structurally just one Type production.
func ParseTypeSpec(blob []byte) (SigType, error) {
	t, _, err := parseType(blob)
	return t, err
}

// ParseMethodSig parses a MethodDef or MemberRef method signature blob.
func ParseMethodSig(blob []byte) (MethodSig, error) {
	if len(blob) == 0 {
		return MethodSig{}, fmt.Errorf("metadata: empty method signature")
	}
	pos := 0
	conv := blob[pos]
	pos++

	sig := MethodSig{HasThis: conv&0x20 != 0}
	if conv&0x10 != 0 {
		sig.Generic = true
		n, sz, err := decodeCompressed(blob[pos:])
		if err != nil {
			return MethodSig{}, err
		}
		sig.GenParamCount = n
		pos += sz
	}

	paramCount, sz, err := decodeCompressed(blob[pos:])
	if err != nil {
		return MethodSig{}, err
	}
	sig.ParamCount = paramCount
	pos += sz

	ret, sz2, err := parseType(blob[pos:])
	if err != nil {
		return MethodSig{}, err
	}
	sig.RetType = ret
	pos += sz2

	sig.Params = make([]SigType, 0, paramCount)
	for i := uint32(0); i < paramCount; i++ {
		p, szp, err := parseType(blob[pos:])
		if err != nil {
			return MethodSig{}, err
		}
		sig.Params = append(sig.Params, p)
		pos += szp
	}
	return sig, nil
}

// ParseLocalVarSig parses a StandAloneSig blob referenced by a fat method
// header's LocalVarSigToken.
func ParseLocalVarSig(blob []byte) ([]SigType, error) {
	if len(blob) == 0 {
		return nil, fmt.Errorf("metadata: empty local var signature")
	}
	if blob[0] != 0x07 {
		return nil, fmt.Errorf("metadata: expected LOCAL_SIG marker (0x07), got %#x", blob[0])
	}
	pos := 1
	count, sz, err := decodeCompressed(blob[pos:])
	if err != nil {
		return nil, err
	}
	pos += sz

	locals := make([]SigType, 0, count)
	for i := uint32(0); i < count; i++ {
		for pos < len(blob) && blob[pos] == elementPinned {
			pos++
		}
		t, szp, err := parseType(blob[pos:])
		if err != nil {
			return nil, err
		}
		locals = append(locals, t)
		pos += szp
	}
	return locals, nil
}
