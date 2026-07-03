// Package il decodes raw CIL method-body bytes into structured
// instructions (opcode, operand, offset). It recognizes the full CIL
// opcode set so decoding never fails on unknown-but-valid instructions,
// even when a given opcode is not yet supported by the interpreter. See
// docs/en/ROADMAP.md, Fase 1, module "/il".
package il

// OpCode identifies a CIL instruction (ECMA-335 partition III). Single-byte
// opcodes keep their byte value (0x00-0xFD); two-byte opcodes (0xFE prefix)
// are represented as 0xFE00 | secondByte.
type OpCode uint16

// OperandKind describes how many bytes follow an opcode and how to
// interpret them.
type OperandKind byte

const (
	OperandNone OperandKind = iota
	OperandInt8
	OperandUint8
	OperandInt32
	OperandInt64
	OperandFloat32
	OperandFloat64
	OperandUint16
	OperandToken       // 4-byte metadata token (call, ldfld, box, ...)
	OperandString      // 4-byte #US heap token (ldstr)
	OperandBranchShort // 1-byte relative offset, resolved to an absolute target
	OperandBranchLong  // 4-byte relative offset, resolved to an absolute target
	OperandSwitch      // 4-byte count + count 4-byte relative offsets
)

type opInfo struct {
	Name    string
	Operand OperandKind
}

// The full CIL opcode table. Every documented ECMA-335 opcode is present
// so the decoder can recognize instructions it can't execute yet — the
// interpreter (Fase 1-2) and IR builder are what draw the supported/
// unsupported line (spec §11.3), not this table.
var opcodes = map[OpCode]opInfo{
	0x00: {"nop", OperandNone},
	0x01: {"break", OperandNone},
	0x02: {"ldarg.0", OperandNone},
	0x03: {"ldarg.1", OperandNone},
	0x04: {"ldarg.2", OperandNone},
	0x05: {"ldarg.3", OperandNone},
	0x06: {"ldloc.0", OperandNone},
	0x07: {"ldloc.1", OperandNone},
	0x08: {"ldloc.2", OperandNone},
	0x09: {"ldloc.3", OperandNone},
	0x0A: {"stloc.0", OperandNone},
	0x0B: {"stloc.1", OperandNone},
	0x0C: {"stloc.2", OperandNone},
	0x0D: {"stloc.3", OperandNone},
	0x0E: {"ldarg.s", OperandUint8},
	0x0F: {"ldarga.s", OperandUint8},
	0x10: {"starg.s", OperandUint8},
	0x11: {"ldloc.s", OperandUint8},
	0x12: {"ldloca.s", OperandUint8},
	0x13: {"stloc.s", OperandUint8},
	0x14: {"ldnull", OperandNone},
	0x15: {"ldc.i4.m1", OperandNone},
	0x16: {"ldc.i4.0", OperandNone},
	0x17: {"ldc.i4.1", OperandNone},
	0x18: {"ldc.i4.2", OperandNone},
	0x19: {"ldc.i4.3", OperandNone},
	0x1A: {"ldc.i4.4", OperandNone},
	0x1B: {"ldc.i4.5", OperandNone},
	0x1C: {"ldc.i4.6", OperandNone},
	0x1D: {"ldc.i4.7", OperandNone},
	0x1E: {"ldc.i4.8", OperandNone},
	0x1F: {"ldc.i4.s", OperandInt8},
	0x20: {"ldc.i4", OperandInt32},
	0x21: {"ldc.i8", OperandInt64},
	0x22: {"ldc.r4", OperandFloat32},
	0x23: {"ldc.r8", OperandFloat64},
	0x25: {"dup", OperandNone},
	0x26: {"pop", OperandNone},
	0x27: {"jmp", OperandToken},
	0x28: {"call", OperandToken},
	0x29: {"calli", OperandToken},
	0x2A: {"ret", OperandNone},
	0x2B: {"br.s", OperandBranchShort},
	0x2C: {"brfalse.s", OperandBranchShort},
	0x2D: {"brtrue.s", OperandBranchShort},
	0x2E: {"beq.s", OperandBranchShort},
	0x2F: {"bge.s", OperandBranchShort},
	0x30: {"bgt.s", OperandBranchShort},
	0x31: {"ble.s", OperandBranchShort},
	0x32: {"blt.s", OperandBranchShort},
	0x33: {"bne.un.s", OperandBranchShort},
	0x34: {"bge.un.s", OperandBranchShort},
	0x35: {"bgt.un.s", OperandBranchShort},
	0x36: {"ble.un.s", OperandBranchShort},
	0x37: {"blt.un.s", OperandBranchShort},
	0x38: {"br", OperandBranchLong},
	0x39: {"brfalse", OperandBranchLong},
	0x3A: {"brtrue", OperandBranchLong},
	0x3B: {"beq", OperandBranchLong},
	0x3C: {"bge", OperandBranchLong},
	0x3D: {"bgt", OperandBranchLong},
	0x3E: {"ble", OperandBranchLong},
	0x3F: {"blt", OperandBranchLong},
	0x40: {"bne.un", OperandBranchLong},
	0x41: {"bge.un", OperandBranchLong},
	0x42: {"bgt.un", OperandBranchLong},
	0x43: {"ble.un", OperandBranchLong},
	0x44: {"blt.un", OperandBranchLong},
	0x45: {"switch", OperandSwitch},
	0x46: {"ldind.i1", OperandNone},
	0x47: {"ldind.u1", OperandNone},
	0x48: {"ldind.i2", OperandNone},
	0x49: {"ldind.u2", OperandNone},
	0x4A: {"ldind.i4", OperandNone},
	0x4B: {"ldind.u4", OperandNone},
	0x4C: {"ldind.i8", OperandNone},
	0x4D: {"ldind.i", OperandNone},
	0x4E: {"ldind.r4", OperandNone},
	0x4F: {"ldind.r8", OperandNone},
	0x50: {"ldind.ref", OperandNone},
	0x51: {"stind.ref", OperandNone},
	0x52: {"stind.i1", OperandNone},
	0x53: {"stind.i2", OperandNone},
	0x54: {"stind.i4", OperandNone},
	0x55: {"stind.i8", OperandNone},
	0x56: {"stind.r4", OperandNone},
	0x57: {"stind.r8", OperandNone},
	0x58: {"add", OperandNone},
	0x59: {"sub", OperandNone},
	0x5A: {"mul", OperandNone},
	0x5B: {"div", OperandNone},
	0x5C: {"div.un", OperandNone},
	0x5D: {"rem", OperandNone},
	0x5E: {"rem.un", OperandNone},
	0x5F: {"and", OperandNone},
	0x60: {"or", OperandNone},
	0x61: {"xor", OperandNone},
	0x62: {"shl", OperandNone},
	0x63: {"shr", OperandNone},
	0x64: {"shr.un", OperandNone},
	0x65: {"neg", OperandNone},
	0x66: {"not", OperandNone},
	0x67: {"conv.i1", OperandNone},
	0x68: {"conv.i2", OperandNone},
	0x69: {"conv.i4", OperandNone},
	0x6A: {"conv.i8", OperandNone},
	0x6B: {"conv.r4", OperandNone},
	0x6C: {"conv.r8", OperandNone},
	0x6D: {"conv.u4", OperandNone},
	0x6E: {"conv.u8", OperandNone},
	0x6F: {"callvirt", OperandToken},
	0x70: {"cpobj", OperandToken},
	0x71: {"ldobj", OperandToken},
	0x72: {"ldstr", OperandString},
	0x73: {"newobj", OperandToken},
	0x74: {"castclass", OperandToken},
	0x75: {"isinst", OperandToken},
	0x76: {"conv.r.un", OperandNone},
	0x79: {"unbox", OperandToken},
	0x7A: {"throw", OperandNone},
	0x7B: {"ldfld", OperandToken},
	0x7C: {"ldflda", OperandToken},
	0x7D: {"stfld", OperandToken},
	0x7E: {"ldsfld", OperandToken},
	0x7F: {"ldsflda", OperandToken},
	0x80: {"stsfld", OperandToken},
	0x81: {"stobj", OperandToken},
	0x82: {"conv.ovf.i1.un", OperandNone},
	0x83: {"conv.ovf.i2.un", OperandNone},
	0x84: {"conv.ovf.i4.un", OperandNone},
	0x85: {"conv.ovf.i8.un", OperandNone},
	0x86: {"conv.ovf.u1.un", OperandNone},
	0x87: {"conv.ovf.u2.un", OperandNone},
	0x88: {"conv.ovf.u4.un", OperandNone},
	0x89: {"conv.ovf.u8.un", OperandNone},
	0x8A: {"conv.ovf.i.un", OperandNone},
	0x8B: {"conv.ovf.u.un", OperandNone},
	0x8C: {"box", OperandToken},
	0x8D: {"newarr", OperandToken},
	0x8E: {"ldlen", OperandNone},
	0x8F: {"ldelema", OperandToken},
	0x90: {"ldelem.i1", OperandNone},
	0x91: {"ldelem.u1", OperandNone},
	0x92: {"ldelem.i2", OperandNone},
	0x93: {"ldelem.u2", OperandNone},
	0x94: {"ldelem.i4", OperandNone},
	0x95: {"ldelem.u4", OperandNone},
	0x96: {"ldelem.i8", OperandNone},
	0x97: {"ldelem.i", OperandNone},
	0x98: {"ldelem.r4", OperandNone},
	0x99: {"ldelem.r8", OperandNone},
	0x9A: {"ldelem.ref", OperandNone},
	0x9B: {"stelem.i", OperandNone},
	0x9C: {"stelem.i1", OperandNone},
	0x9D: {"stelem.i2", OperandNone},
	0x9E: {"stelem.i4", OperandNone},
	0x9F: {"stelem.i8", OperandNone},
	0xA0: {"stelem.r4", OperandNone},
	0xA1: {"stelem.r8", OperandNone},
	0xA2: {"stelem.ref", OperandNone},
	0xA3: {"ldelem", OperandToken},
	0xA4: {"stelem", OperandToken},
	0xA5: {"unbox.any", OperandToken},
	0xB3: {"conv.ovf.i1", OperandNone},
	0xB4: {"conv.ovf.u1", OperandNone},
	0xB5: {"conv.ovf.i2", OperandNone},
	0xB6: {"conv.ovf.u2", OperandNone},
	0xB7: {"conv.ovf.i4", OperandNone},
	0xB8: {"conv.ovf.u4", OperandNone},
	0xB9: {"conv.ovf.i8", OperandNone},
	0xBA: {"conv.ovf.u8", OperandNone},
	0xC2: {"refanyval", OperandToken},
	0xC3: {"ckfinite", OperandNone},
	0xC6: {"mkrefany", OperandToken},
	0xD0: {"ldtoken", OperandToken},
	0xD1: {"conv.u2", OperandNone},
	0xD2: {"conv.u1", OperandNone},
	0xD3: {"conv.i", OperandNone},
	0xD4: {"conv.ovf.i", OperandNone},
	0xD5: {"conv.ovf.u", OperandNone},
	0xD6: {"add.ovf", OperandNone},
	0xD7: {"add.ovf.un", OperandNone},
	0xD8: {"mul.ovf", OperandNone},
	0xD9: {"mul.ovf.un", OperandNone},
	0xDA: {"sub.ovf", OperandNone},
	0xDB: {"sub.ovf.un", OperandNone},
	0xDC: {"endfinally", OperandNone}, // aka endfault, same encoding
	0xDD: {"leave", OperandBranchLong},
	0xDE: {"leave.s", OperandBranchShort},
	0xDF: {"stind.i", OperandNone},
	0xE0: {"conv.u", OperandNone},

	// Two-byte opcodes (0xFE prefix). Represented here as 0xFE00|secondByte.
	0xFE00: {"arglist", OperandNone},
	0xFE01: {"ceq", OperandNone},
	0xFE02: {"cgt", OperandNone},
	0xFE03: {"cgt.un", OperandNone},
	0xFE04: {"clt", OperandNone},
	0xFE05: {"clt.un", OperandNone},
	0xFE06: {"ldftn", OperandToken},
	0xFE07: {"ldvirtftn", OperandToken},
	0xFE09: {"ldarg", OperandUint16},
	0xFE0A: {"ldarga", OperandUint16},
	0xFE0B: {"starg", OperandUint16},
	0xFE0C: {"ldloc", OperandUint16},
	0xFE0D: {"ldloca", OperandUint16},
	0xFE0E: {"stloc", OperandUint16},
	0xFE0F: {"localloc", OperandNone},
	0xFE11: {"endfilter", OperandNone},
	0xFE12: {"unaligned.", OperandUint8},
	0xFE13: {"volatile.", OperandNone},
	0xFE14: {"tail.", OperandNone},
	0xFE15: {"initobj", OperandToken},
	0xFE16: {"constrained.", OperandToken},
	0xFE17: {"cpblk", OperandNone},
	0xFE18: {"initblk", OperandNone},
	0xFE19: {"no.", OperandUint8},
	0xFE1A: {"rethrow", OperandNone},
	0xFE1C: {"sizeof", OperandToken},
	0xFE1D: {"refanytype", OperandNone},
	0xFE1E: {"readonly.", OperandNone},
}

// Name returns the mnemonic for op, or "" if op is not a recognized opcode.
func (op OpCode) Name() string {
	return opcodes[op].Name
}

// Recognized reports whether op is a documented CIL opcode.
func (op OpCode) Recognized() bool {
	_, ok := opcodes[op]
	return ok
}
