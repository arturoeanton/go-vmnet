package il

import (
	"encoding/binary"
	"fmt"
	"math"
)

// MethodHeader is a parsed CIL method-body header (ECMA-335 §II.25.4):
// tiny format for small straight-line methods with no locals, fat format
// otherwise.
type MethodHeader struct {
	Fat              bool
	MaxStack         uint16
	InitLocals       bool
	LocalVarSigToken uint32 // StandAloneSig token, 0 if the method has no locals
	CodeSize         uint32
	MoreSections     bool
}

const (
	corILMethodTinyFormat = 0x2
	corILMethodFatFormat  = 0x3
	corILMethodMoreSects  = 0x8
	corILMethodInitLocals = 0x10
)

// ReadMethodBody parses the method header at the start of data (bytes
// resolved from a MethodDef's RVA) and returns the header plus the raw IL
// code bytes that follow it. It does not decode the code into instructions
// or parse trailing exception-handling sections — see Decode.
func ReadMethodBody(data []byte) (MethodHeader, []byte, error) {
	if len(data) < 1 {
		return MethodHeader{}, nil, fmt.Errorf("il: empty method body")
	}

	format := data[0] & 0x3
	switch format {
	case corILMethodTinyFormat:
		size := uint32(data[0]) >> 2
		if uint64(1+size) > uint64(len(data)) {
			return MethodHeader{}, nil, fmt.Errorf("il: tiny method body truncated")
		}
		return MethodHeader{
			Fat:      false,
			MaxStack: 8,
			CodeSize: size,
		}, data[1 : 1+size], nil

	case corILMethodFatFormat:
		if len(data) < 12 {
			return MethodHeader{}, nil, fmt.Errorf("il: fat method header truncated")
		}
		flagsAndSize := binary.LittleEndian.Uint16(data[0:2])
		headerSizeDWords := flagsAndSize >> 12
		if headerSizeDWords != 3 {
			return MethodHeader{}, nil, fmt.Errorf("il: unexpected fat header size %d", headerSizeDWords)
		}
		h := MethodHeader{
			Fat:              true,
			MaxStack:         binary.LittleEndian.Uint16(data[2:4]),
			CodeSize:         binary.LittleEndian.Uint32(data[4:8]),
			LocalVarSigToken: binary.LittleEndian.Uint32(data[8:12]),
			MoreSections:     flagsAndSize&corILMethodMoreSects != 0,
			InitLocals:       flagsAndSize&corILMethodInitLocals != 0,
		}
		end := uint64(12) + uint64(h.CodeSize)
		if end > uint64(len(data)) {
			return MethodHeader{}, nil, fmt.Errorf("il: fat method body truncated")
		}
		return h, data[12:end], nil

	default:
		return MethodHeader{}, nil, fmt.Errorf("il: unrecognized method header format %#x", format)
	}
}

// Decode turns raw CIL bytes (as returned by ReadMethodBody) into a
// sequence of structured instructions, resolving branch/switch targets to
// absolute offsets within code.
func Decode(code []byte) ([]Instruction, error) {
	var out []Instruction
	pos := 0

	for pos < len(code) {
		start := pos
		b := code[pos]
		pos++

		var op OpCode
		if b == 0xFE {
			if pos >= len(code) {
				return nil, fmt.Errorf("il: truncated two-byte opcode at offset %d", start)
			}
			op = 0xFE00 | OpCode(code[pos])
			pos++
		} else {
			op = OpCode(b)
		}

		info, ok := opcodes[op]
		if !ok {
			return nil, fmt.Errorf("il: unknown opcode %#x at offset %d", op, start)
		}

		var operand any
		switch info.Operand {
		case OperandNone:
			operand = nil

		case OperandInt8:
			if pos+1 > len(code) {
				return nil, fmt.Errorf("il: truncated operand for %s at offset %d", info.Name, start)
			}
			operand = int8(code[pos])
			pos++

		case OperandUint8:
			if pos+1 > len(code) {
				return nil, fmt.Errorf("il: truncated operand for %s at offset %d", info.Name, start)
			}
			operand = code[pos]
			pos++

		case OperandInt32:
			if pos+4 > len(code) {
				return nil, fmt.Errorf("il: truncated operand for %s at offset %d", info.Name, start)
			}
			operand = int32(binary.LittleEndian.Uint32(code[pos : pos+4]))
			pos += 4

		case OperandInt64:
			if pos+8 > len(code) {
				return nil, fmt.Errorf("il: truncated operand for %s at offset %d", info.Name, start)
			}
			operand = int64(binary.LittleEndian.Uint64(code[pos : pos+8]))
			pos += 8

		case OperandFloat32:
			if pos+4 > len(code) {
				return nil, fmt.Errorf("il: truncated operand for %s at offset %d", info.Name, start)
			}
			operand = math.Float32frombits(binary.LittleEndian.Uint32(code[pos : pos+4]))
			pos += 4

		case OperandFloat64:
			if pos+8 > len(code) {
				return nil, fmt.Errorf("il: truncated operand for %s at offset %d", info.Name, start)
			}
			operand = math.Float64frombits(binary.LittleEndian.Uint64(code[pos : pos+8]))
			pos += 8

		case OperandUint16:
			if pos+2 > len(code) {
				return nil, fmt.Errorf("il: truncated operand for %s at offset %d", info.Name, start)
			}
			operand = binary.LittleEndian.Uint16(code[pos : pos+2])
			pos += 2

		case OperandToken, OperandString:
			if pos+4 > len(code) {
				return nil, fmt.Errorf("il: truncated operand for %s at offset %d", info.Name, start)
			}
			operand = binary.LittleEndian.Uint32(code[pos : pos+4])
			pos += 4

		case OperandBranchShort:
			if pos+1 > len(code) {
				return nil, fmt.Errorf("il: truncated operand for %s at offset %d", info.Name, start)
			}
			rel := int8(code[pos])
			pos++
			operand = pos + int(rel)

		case OperandBranchLong:
			if pos+4 > len(code) {
				return nil, fmt.Errorf("il: truncated operand for %s at offset %d", info.Name, start)
			}
			rel := int32(binary.LittleEndian.Uint32(code[pos : pos+4]))
			pos += 4
			operand = pos + int(rel)

		case OperandSwitch:
			if pos+4 > len(code) {
				return nil, fmt.Errorf("il: truncated switch count at offset %d", start)
			}
			n := binary.LittleEndian.Uint32(code[pos : pos+4])
			pos += 4
			if uint64(pos)+uint64(n)*4 > uint64(len(code)) {
				return nil, fmt.Errorf("il: truncated switch table at offset %d", start)
			}
			base := pos + int(n)*4 // targets are relative to the offset *after* the whole switch instruction
			targets := make([]int, n)
			for i := uint32(0); i < n; i++ {
				rel := int32(binary.LittleEndian.Uint32(code[pos : pos+4]))
				pos += 4
				targets[i] = base + int(rel)
			}
			operand = targets

		default:
			return nil, fmt.Errorf("il: unhandled operand kind for %s at offset %d", info.Name, start)
		}

		out = append(out, Instruction{Offset: start, OpCode: op, Operand: operand})
	}

	return out, nil
}
