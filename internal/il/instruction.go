package il

// Instruction is one decoded CIL instruction. Operand's concrete type
// depends on OpCode's OperandKind:
//
//	OperandNone         nil
//	OperandInt8         int8
//	OperandUint8        uint8
//	OperandInt32        int32
//	OperandInt64        int64
//	OperandFloat32      float32
//	OperandFloat64      float64
//	OperandUint16       uint16
//	OperandToken        uint32 (metadata token)
//	OperandString       uint32 (#US heap token, top byte 0x70)
//	OperandBranchShort  int    (absolute target offset, already resolved)
//	OperandBranchLong   int    (absolute target offset, already resolved)
//	OperandSwitch       []int  (absolute target offsets, already resolved)
type Instruction struct {
	Offset  int
	OpCode  OpCode
	Operand any
}
