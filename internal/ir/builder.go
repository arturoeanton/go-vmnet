package ir

import (
	"fmt"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/il"
	"github.com/arturoeanton/go-vmnet/internal/metadata"
)

// Build lowers one method's decoded IL into IR. md resolves ldstr/call
// tokens; retVoid tells Build how to lower `ret` (with or without a
// value) since IL's `ret` opcode carries no operand of its own; ehClauses
// (from il.ReadExceptionHandlers, nil if the method has none) become the
// returned []Handler, with IL byte offsets resolved to IR indices the
// same way branch targets are (Fase 3.10).
//
// Anything CIL can express that Fase 1-3.9 don't model — interface/vtable
// dispatch beyond isinst/castclass, generics beyond native BCL
// collections, exception filters (`catch (Foo) when (cond)`) — is
// reported as an explicit unsupported-opcode error instead of silently
// mis-translated (spec §11.3, §23).
func Build(instrs []il.Instruction, md *metadata.Metadata, retVoid bool, ehClauses []il.ExceptionHandler) ([]Instr, []Handler, error) {
	offsetToIndex := make(map[int]int, len(instrs))
	for i, instr := range instrs {
		offsetToIndex[instr.Offset] = i
	}
	resolveTarget := func(target int) (int, error) {
		idx, ok := offsetToIndex[target]
		if !ok {
			return 0, fmt.Errorf("ir: branch target offset %d is not an instruction boundary", target)
		}
		return idx, nil
	}

	out := make([]Instr, 0, len(instrs))
	for _, instr := range instrs {
		name := instr.OpCode.Name()
		switch name {
		case "nop", "break":
			out = append(out, Nop{})
		case "dup":
			out = append(out, Dup{})
		case "pop":
			out = append(out, Pop{})
		case "localloc":
			out = append(out, LocalAlloc{})

		case "ldarg.0":
			out = append(out, LoadArg{0})
		case "ldarg.1":
			out = append(out, LoadArg{1})
		case "ldarg.2":
			out = append(out, LoadArg{2})
		case "ldarg.3":
			out = append(out, LoadArg{3})
		case "ldarg.s", "ldarg":
			out = append(out, LoadArg{Index: operandIndex(instr.Operand)})
		case "starg.s", "starg":
			out = append(out, StoreArg{Index: operandIndex(instr.Operand)})
		case "ldarga.s", "ldarga":
			out = append(out, LoadArgAddr{Index: operandIndex(instr.Operand)})

		case "ldloc.0":
			out = append(out, LoadLocal{0})
		case "ldloc.1":
			out = append(out, LoadLocal{1})
		case "ldloc.2":
			out = append(out, LoadLocal{2})
		case "ldloc.3":
			out = append(out, LoadLocal{3})
		case "ldloc.s", "ldloc":
			out = append(out, LoadLocal{Index: operandIndex(instr.Operand)})
		case "ldloca.s", "ldloca":
			out = append(out, LoadLocalAddr{Index: operandIndex(instr.Operand)})

		case "stloc.0":
			out = append(out, StoreLocal{0})
		case "stloc.1":
			out = append(out, StoreLocal{1})
		case "stloc.2":
			out = append(out, StoreLocal{2})
		case "stloc.3":
			out = append(out, StoreLocal{3})
		case "stloc.s", "stloc":
			out = append(out, StoreLocal{Index: operandIndex(instr.Operand)})

		case "ldnull":
			out = append(out, LoadNull{})
		case "ldc.i4.m1":
			out = append(out, LoadConstI4{-1})
		case "ldc.i4.0":
			out = append(out, LoadConstI4{0})
		case "ldc.i4.1":
			out = append(out, LoadConstI4{1})
		case "ldc.i4.2":
			out = append(out, LoadConstI4{2})
		case "ldc.i4.3":
			out = append(out, LoadConstI4{3})
		case "ldc.i4.4":
			out = append(out, LoadConstI4{4})
		case "ldc.i4.5":
			out = append(out, LoadConstI4{5})
		case "ldc.i4.6":
			out = append(out, LoadConstI4{6})
		case "ldc.i4.7":
			out = append(out, LoadConstI4{7})
		case "ldc.i4.8":
			out = append(out, LoadConstI4{8})
		case "ldc.i4.s":
			out = append(out, LoadConstI4{int32(instr.Operand.(int8))})
		case "ldc.i4":
			out = append(out, LoadConstI4{instr.Operand.(int32)})
		case "ldc.i8":
			out = append(out, LoadConstI8{instr.Operand.(int64)})
		case "ldc.r4":
			out = append(out, LoadConstR4{instr.Operand.(float32)})
		case "ldc.r8":
			out = append(out, LoadConstR8{instr.Operand.(float64)})
		case "ldstr":
			token := instr.Operand.(uint32)
			s, err := md.UserString(token & 0x00FFFFFF)
			if err != nil {
				return nil, nil, fmt.Errorf("ir: ldstr at IL offset %d: %w", instr.Offset, err)
			}
			out = append(out, LoadString{s})

		case "add", "add.ovf", "add.ovf.un":
			out = append(out, BinOp{Op: OpAdd})
		case "sub", "sub.ovf", "sub.ovf.un":
			out = append(out, BinOp{Op: OpSub})
		case "mul", "mul.ovf", "mul.ovf.un":
			out = append(out, BinOp{Op: OpMul})
		case "div":
			out = append(out, BinOp{Op: OpDiv})
		case "div.un":
			out = append(out, BinOp{Op: OpDiv, Unsigned: true})
		case "rem":
			out = append(out, BinOp{Op: OpRem})
		case "rem.un":
			out = append(out, BinOp{Op: OpRem, Unsigned: true})
		case "and":
			out = append(out, BinOp{Op: OpAnd})
		case "or":
			out = append(out, BinOp{Op: OpOr})
		case "xor":
			out = append(out, BinOp{Op: OpXor})
		case "shl":
			out = append(out, BinOp{Op: OpShl})
		case "shr":
			out = append(out, BinOp{Op: OpShr})
		case "shr.un":
			out = append(out, BinOp{Op: OpShr, Unsigned: true})
		case "neg":
			out = append(out, Neg{})
		case "not":
			out = append(out, Not{})
		case "ceq":
			out = append(out, BinOp{Op: OpCeq})
		case "cgt":
			out = append(out, BinOp{Op: OpCgt})
		case "cgt.un":
			out = append(out, BinOp{Op: OpCgt, Unsigned: true})
		case "clt":
			out = append(out, BinOp{Op: OpClt})
		case "clt.un":
			out = append(out, BinOp{Op: OpClt, Unsigned: true})

		case "conv.i1", "conv.ovf.i1", "conv.ovf.i1.un":
			out = append(out, Conv{ConvI1})
		case "conv.u1", "conv.ovf.u1", "conv.ovf.u1.un":
			out = append(out, Conv{ConvU1})
		case "conv.i2", "conv.ovf.i2", "conv.ovf.i2.un":
			out = append(out, Conv{ConvI2})
		case "conv.u2", "conv.ovf.u2", "conv.ovf.u2.un":
			out = append(out, Conv{ConvU2})
		case "conv.i4", "conv.i", "conv.ovf.i4", "conv.ovf.i4.un", "conv.ovf.i", "conv.ovf.i.un":
			out = append(out, Conv{ConvI4})
		case "conv.u4", "conv.u", "conv.ovf.u4", "conv.ovf.u4.un", "conv.ovf.u", "conv.ovf.u.un":
			out = append(out, Conv{ConvU4})
		case "conv.i8", "conv.ovf.i8", "conv.ovf.i8.un":
			out = append(out, Conv{ConvI8})
		case "conv.u8", "conv.ovf.u8", "conv.ovf.u8.un":
			out = append(out, Conv{ConvU8})
		case "conv.r4":
			out = append(out, Conv{ConvR4})
		case "conv.r8", "conv.r.un":
			out = append(out, Conv{ConvR8})

		case "br.s", "br":
			target, err := resolveTarget(instr.Operand.(int))
			if err != nil {
				return nil, nil, err
			}
			out = append(out, Branch{target})
		case "brtrue.s", "brtrue":
			target, err := resolveTarget(instr.Operand.(int))
			if err != nil {
				return nil, nil, err
			}
			out = append(out, BranchIfTrue{target})
		case "brfalse.s", "brfalse":
			target, err := resolveTarget(instr.Operand.(int))
			if err != nil {
				return nil, nil, err
			}
			out = append(out, BranchIfFalse{target})
		case "switch":
			offsets := instr.Operand.([]int)
			targets := make([]int, len(offsets))
			for i, off := range offsets {
				target, err := resolveTarget(off)
				if err != nil {
					return nil, nil, err
				}
				targets[i] = target
			}
			out = append(out, Switch{Targets: targets})
		case "beq.s", "beq":
			target, err := resolveTarget(instr.Operand.(int))
			if err != nil {
				return nil, nil, err
			}
			out = append(out, BranchCompare{Target: target, Op: CmpEq})
		case "bge.s", "bge":
			target, err := resolveTarget(instr.Operand.(int))
			if err != nil {
				return nil, nil, err
			}
			out = append(out, BranchCompare{Target: target, Op: CmpGe})
		case "bge.un.s", "bge.un":
			target, err := resolveTarget(instr.Operand.(int))
			if err != nil {
				return nil, nil, err
			}
			out = append(out, BranchCompare{Target: target, Op: CmpGe, Unsigned: true})
		case "bgt.s", "bgt":
			target, err := resolveTarget(instr.Operand.(int))
			if err != nil {
				return nil, nil, err
			}
			out = append(out, BranchCompare{Target: target, Op: CmpGt})
		case "bgt.un.s", "bgt.un":
			target, err := resolveTarget(instr.Operand.(int))
			if err != nil {
				return nil, nil, err
			}
			out = append(out, BranchCompare{Target: target, Op: CmpGt, Unsigned: true})
		case "ble.s", "ble":
			target, err := resolveTarget(instr.Operand.(int))
			if err != nil {
				return nil, nil, err
			}
			out = append(out, BranchCompare{Target: target, Op: CmpLe})
		case "ble.un.s", "ble.un":
			target, err := resolveTarget(instr.Operand.(int))
			if err != nil {
				return nil, nil, err
			}
			out = append(out, BranchCompare{Target: target, Op: CmpLe, Unsigned: true})
		case "blt.s", "blt":
			target, err := resolveTarget(instr.Operand.(int))
			if err != nil {
				return nil, nil, err
			}
			out = append(out, BranchCompare{Target: target, Op: CmpLt})
		case "blt.un.s", "blt.un":
			target, err := resolveTarget(instr.Operand.(int))
			if err != nil {
				return nil, nil, err
			}
			out = append(out, BranchCompare{Target: target, Op: CmpLt, Unsigned: true})
		case "bne.un.s", "bne.un":
			target, err := resolveTarget(instr.Operand.(int))
			if err != nil {
				return nil, nil, err
			}
			out = append(out, BranchCompare{Target: target, Op: CmpNe, Unsigned: true})

		case "call":
			token := instr.Operand.(uint32)
			fullName, hasThis, argCount, hasReturn, paramTypeNames, methodGenericArgs, err := resolveCallTarget(md, token)
			if err != nil {
				return nil, nil, fmt.Errorf("ir: call at IL offset %d: %w", instr.Offset, err)
			}
			out = append(out, Call{FullName: fullName, ArgCount: argCount, HasThis: hasThis, HasReturn: hasReturn, ParamTypeNames: paramTypeNames, MethodGenericArgs: methodGenericArgs})

		case "callvirt":
			token := instr.Operand.(uint32)
			fullName, _, argCount, hasReturn, paramTypeNames, methodGenericArgs, err := resolveCallTarget(md, token)
			if err != nil {
				return nil, nil, fmt.Errorf("ir: callvirt at IL offset %d: %w", instr.Offset, err)
			}
			out = append(out, Call{FullName: fullName, ArgCount: argCount, HasThis: true, HasReturn: hasReturn, Virtual: true, ParamTypeNames: paramTypeNames, MethodGenericArgs: methodGenericArgs})

		case "newobj":
			token := instr.Operand.(uint32)
			typeFullName, ctorFullName, argCount, paramTypeNames, err := resolveNewObjTarget(md, token)
			if err != nil {
				return nil, nil, fmt.Errorf("ir: newobj at IL offset %d: %w", instr.Offset, err)
			}
			out = append(out, NewObj{TypeFullName: typeFullName, CtorFullName: ctorFullName, ArgCount: argCount, ParamTypeNames: paramTypeNames})

		case "ldfld":
			token := instr.Operand.(uint32)
			typeFullName, fieldName, err := resolveFieldTarget(md, token)
			if err != nil {
				return nil, nil, fmt.Errorf("ir: ldfld at IL offset %d: %w", instr.Offset, err)
			}
			out = append(out, LoadField{TypeFullName: typeFullName, FieldName: fieldName})

		case "stfld":
			token := instr.Operand.(uint32)
			typeFullName, fieldName, err := resolveFieldTarget(md, token)
			if err != nil {
				return nil, nil, fmt.Errorf("ir: stfld at IL offset %d: %w", instr.Offset, err)
			}
			out = append(out, StoreField{TypeFullName: typeFullName, FieldName: fieldName})

		case "ldsfld":
			token := instr.Operand.(uint32)
			typeFullName, fieldName, err := resolveFieldTarget(md, token)
			if err != nil {
				return nil, nil, fmt.Errorf("ir: ldsfld at IL offset %d: %w", instr.Offset, err)
			}
			out = append(out, LoadStaticField{TypeFullName: typeFullName, FieldName: fieldName})

		case "stsfld":
			token := instr.Operand.(uint32)
			typeFullName, fieldName, err := resolveFieldTarget(md, token)
			if err != nil {
				return nil, nil, fmt.Errorf("ir: stsfld at IL offset %d: %w", instr.Offset, err)
			}
			out = append(out, StoreStaticField{TypeFullName: typeFullName, FieldName: fieldName})

		case "ldflda":
			token := instr.Operand.(uint32)
			typeFullName, fieldName, err := resolveFieldTarget(md, token)
			if err != nil {
				return nil, nil, fmt.Errorf("ir: ldflda at IL offset %d: %w", instr.Offset, err)
			}
			out = append(out, LoadFieldAddr{TypeFullName: typeFullName, FieldName: fieldName})

		case "ldsflda":
			token := instr.Operand.(uint32)
			typeFullName, fieldName, err := resolveFieldTarget(md, token)
			if err != nil {
				return nil, nil, fmt.Errorf("ir: ldsflda at IL offset %d: %w", instr.Offset, err)
			}
			out = append(out, LoadStaticFieldAddr{TypeFullName: typeFullName, FieldName: fieldName})

		case "box", "unbox.any":
			// vmnet's runtime.Value is already a uniform tagged union —
			// boxing a value type doesn't need a representation change.
			// Correctness gap: unbox.any doesn't verify the target type.
			out = append(out, Nop{})

		case "throw":
			out = append(out, Throw{})

		case "newarr":
			token := instr.Operand.(uint32)
			typeFullName, err := resolveTypeTokenOrGeneric(md, token)
			if err != nil {
				return nil, nil, fmt.Errorf("ir: newarr at IL offset %d: %w", instr.Offset, err)
			}
			out = append(out, NewArr{TypeFullName: typeFullName})
		case "ldlen":
			out = append(out, LoadLen{})
		case "ldelem.i1", "ldelem.u1", "ldelem.i2", "ldelem.u2", "ldelem.i4", "ldelem.u4",
			"ldelem.i8", "ldelem.i", "ldelem.r4", "ldelem.r8", "ldelem.ref", "ldelem":
			out = append(out, LoadElem{})
		case "stelem.i", "stelem.i1", "stelem.i2", "stelem.i4", "stelem.i8",
			"stelem.r4", "stelem.r8", "stelem.ref", "stelem":
			out = append(out, StoreElem{})
		case "ldelema":
			out = append(out, LoadElemAddr{})

		case "ldind.i1", "ldind.u1", "ldind.i2", "ldind.u2", "ldind.i4", "ldind.u4",
			"ldind.i8", "ldind.i", "ldind.r4", "ldind.r8", "ldind.ref":
			out = append(out, LoadIndirect{})
		case "stind.ref", "stind.i1", "stind.i2", "stind.i4", "stind.i8",
			"stind.r4", "stind.r8", "stind.i":
			out = append(out, StoreIndirect{})
		case "ldobj":
			out = append(out, LoadIndirect{})
		case "stobj":
			out = append(out, StoreIndirect{})

		case "initobj":
			token := instr.Operand.(uint32)
			typeFullName, err := resolveTypeTokenOrGeneric(md, token)
			if err != nil {
				return nil, nil, fmt.Errorf("ir: initobj at IL offset %d: %w", instr.Offset, err)
			}
			out = append(out, InitObj{TypeFullName: typeFullName})

		case "isinst":
			token := instr.Operand.(uint32)
			typeFullName, err := resolveTypeTokenOrGeneric(md, token)
			if err != nil {
				return nil, nil, fmt.Errorf("ir: isinst at IL offset %d: %w", instr.Offset, err)
			}
			out = append(out, IsInst{TypeFullName: typeFullName})

		case "castclass":
			token := instr.Operand.(uint32)
			typeFullName, err := resolveTypeTokenOrGeneric(md, token)
			if err != nil {
				return nil, nil, fmt.Errorf("ir: castclass at IL offset %d: %w", instr.Offset, err)
			}
			out = append(out, CastClass{TypeFullName: typeFullName})

		case "ldftn":
			token := instr.Operand.(uint32)
			fullName, _, _, _, _, _, err := resolveCallTarget(md, token)
			if err != nil {
				return nil, nil, fmt.Errorf("ir: ldftn at IL offset %d: %w", instr.Offset, err)
			}
			out = append(out, LoadFtn{FullName: fullName})

		case "ldvirtftn":
			token := instr.Operand.(uint32)
			fullName, _, _, _, _, _, err := resolveCallTarget(md, token)
			if err != nil {
				return nil, nil, fmt.Errorf("ir: ldvirtftn at IL offset %d: %w", instr.Offset, err)
			}
			out = append(out, LoadFtn{FullName: fullName, Virtual: true})

		case "constrained.", "volatile.", "readonly.", "unaligned.":
			// Prefixes vmnet doesn't need: constrained. only matters for
			// choosing between boxing and a value type's own override at a
			// following callvirt — vmnet's Value is already a tagged union
			// carrying its real Kind, so a callvirt to e.g.
			// System.Object::ToString/Equals/GetHashCode already dispatches
			// on the actual value (see internal/bcl/system_object.go).
			// volatile./readonly. are pure optimizer hints (memory
			// ordering, "won't mutate through this address") that don't
			// affect a single-goroutine-per-call interpreter with
			// Value-based storage instead of raw memory. unaligned. (Fase
			// 3.40, found via System.Runtime.CompilerServices.Unsafe's own
			// WriteUnaligned/ReadUnaligned) marks the following ldind/stind/
			// ldobj/stobj/cpblk/initblk as safe on an unaligned address — a
			// real concern for hardware that faults on misaligned loads,
			// meaningless for vmnet's Value-based storage which has no
			// notion of memory alignment at all.
			out = append(out, Nop{})

		case "ret":
			out = append(out, Return{HasValue: !retVoid})

		case "leave", "leave.s":
			target, err := resolveTarget(instr.Operand.(int))
			if err != nil {
				return nil, nil, err
			}
			out = append(out, Leave{Target: target})

		case "endfinally":
			out = append(out, EndFinally{})

		case "endfilter":
			out = append(out, EndFilter{})

		case "rethrow":
			out = append(out, Rethrow{})

		case "ldtoken":
			// ldtoken targets a type (typeof(T): TypeDef/TypeRef/TypeSpec —
			// LoadTypeToken), a field (RuntimeFieldHandle, the
			// RuntimeHelpers.InitializeArray pattern behind an array
			// literal's blob initializer — LoadFieldToken, Fase 3.27), or a
			// method (RuntimeMethodHandle, far rarer, still unsupported).
			token := metadata.Token(instr.Operand.(uint32))
			switch token.Table() {
			case metadata.TableTypeRef, metadata.TableTypeDef:
				typeFullName, err := resolveTypeToken(md, token)
				if err != nil {
					return nil, nil, fmt.Errorf("ir: ldtoken at IL offset %d: %w", instr.Offset, err)
				}
				out = append(out, LoadTypeToken{TypeFullName: typeFullName})
			case metadata.TableTypeSpec:
				sig, err := md.TypeSpecSignature(token.RID())
				if err != nil {
					return nil, nil, fmt.Errorf("ir: ldtoken at IL offset %d: %w", instr.Offset, err)
				}
				parsed, err := metadata.ParseTypeSpec(sig)
				if err != nil {
					return nil, nil, fmt.Errorf("ir: ldtoken at IL offset %d: %w", instr.Offset, err)
				}
				if parsed.Kind == metadata.SigGenericParam && parsed.GenericParamIsMethod {
					// `typeof(TFeature)` on the ENCLOSING method's own
					// generic parameter (Fase 3.40) — resolved at runtime
					// from the call site's own MethodGenericArgs, not
					// here (every instantiation of this same method body
					// needs a different answer; see ir.Call.
					// MethodGenericArgs's own doc comment).
					out = append(out, LoadTypeToken{MethodGenericParamIndex: int(parsed.GenericParamIndex), IsMethodGenericParam: true})
					break
				}
				// Unlike resolveTypeTokenOrGeneric (used by initobj/ldobj/
				// stobj and MemberRef class resolution, which only ever need
				// the open generic type name), typeof(T) on a closed generic
				// instantiation needs its type arguments too (Fase 3.25:
				// System.Type.GetGenericArguments/MakeGenericType) —
				// resolveClosedTypeSpecName retains them as "Open`N[[Arg1],
				// [Arg2]]".
				typeFullName, err := resolveClosedTypeSpecName(md, token.RID())
				if err != nil {
					return nil, nil, fmt.Errorf("ir: ldtoken at IL offset %d: %w", instr.Offset, err)
				}
				out = append(out, LoadTypeToken{TypeFullName: typeFullName})
			case metadata.TableField:
				typeFullName, fieldName, err := resolveFieldTarget(md, uint32(token))
				if err != nil {
					return nil, nil, fmt.Errorf("ir: ldtoken at IL offset %d: %w", instr.Offset, err)
				}
				out = append(out, LoadFieldToken{TypeFullName: typeFullName, FieldName: fieldName})
			case metadata.TableMethodDef, metadata.TableMemberRef:
				// ldtoken on a Method (RuntimeMethodHandle) — see
				// LoadMethodToken's own doc comment. resolveCallTarget
				// already resolves both tables down to a single
				// "Type::Method" full name; only the name is needed here
				// (argument count/generic-instantiation info, also
				// returned, is irrelevant to a bare method-handle token).
				fullName, _, _, _, _, _, err := resolveCallTarget(md, uint32(token))
				if err != nil {
					return nil, nil, fmt.Errorf("ir: ldtoken at IL offset %d: %w", instr.Offset, err)
				}
				sep := strings.LastIndex(fullName, "::")
				if sep < 0 {
					return nil, nil, fmt.Errorf("ir: ldtoken at IL offset %d: malformed method full name %q", instr.Offset, fullName)
				}
				out = append(out, LoadMethodToken{TypeFullName: fullName[:sep], MethodName: fullName[sep+2:]})
			default:
				return nil, nil, &UnsupportedOpcodeError{OpCode: name, Offset: instr.Offset}
			}

		default:
			return nil, nil, &UnsupportedOpcodeError{OpCode: name, Offset: instr.Offset}
		}
	}

	handlers, err := buildHandlers(ehClauses, md, offsetToIndex, len(out))
	if err != nil {
		return nil, nil, err
	}
	return out, handlers, nil
}

// buildHandlers converts il.ExceptionHandler's IL byte offsets to IR
// indices. End offsets (TryOffset+TryLength, HandlerOffset+HandlerLength)
// need a variant of resolveTarget's lookup: a handler region ending
// exactly at the end of the method body points one byte past the last
// instruction, which offsetToIndex — built only from actual instruction
// start offsets — has no entry for; that's resolved to instrCount (one
// past the last IR index), same convention as a normal slice's end index.
func buildHandlers(ehClauses []il.ExceptionHandler, md *metadata.Metadata, offsetToIndex map[int]int, instrCount int) ([]Handler, error) {
	if len(ehClauses) == 0 {
		return nil, nil
	}
	maxOffset := -1
	for offset := range offsetToIndex {
		if offset > maxOffset {
			maxOffset = offset
		}
	}
	resolveEnd := func(offset int) (int, error) {
		if idx, ok := offsetToIndex[offset]; ok {
			return idx, nil
		}
		if offset > maxOffset {
			return instrCount, nil
		}
		return 0, fmt.Errorf("ir: exception handler region end offset %d is not an instruction boundary", offset)
	}
	resolveStart := func(offset int) (int, error) {
		idx, ok := offsetToIndex[offset]
		if !ok {
			return 0, fmt.Errorf("ir: exception handler region start offset %d is not an instruction boundary", offset)
		}
		return idx, nil
	}

	handlers := make([]Handler, 0, len(ehClauses))
	for _, c := range ehClauses {
		tryStart, err := resolveStart(c.TryOffset)
		if err != nil {
			return nil, err
		}
		tryEnd, err := resolveEnd(c.TryOffset + c.TryLength)
		if err != nil {
			return nil, err
		}
		handlerStart, err := resolveStart(c.HandlerOffset)
		if err != nil {
			return nil, err
		}
		handlerEnd, err := resolveEnd(c.HandlerOffset + c.HandlerLength)
		if err != nil {
			return nil, err
		}

		h := Handler{TryStart: tryStart, TryEnd: tryEnd, HandlerStart: handlerStart, HandlerEnd: handlerEnd}
		switch c.Kind {
		case il.HandlerFinally:
			h.Kind = HandlerFinally
		case il.HandlerFault:
			h.Kind = HandlerFault
		case il.HandlerFilter:
			// `catch (Foo) when (cond)` — FilterOffset is a plain IL byte
			// offset here too (resolveStart, not resolveEnd: the filter
			// body is always a real instruction range starting exactly
			// there, never the "end of method" edge case resolveEnd
			// tolerates for a handler's closing boundary).
			h.Kind = HandlerFilter
			filterStart, err := resolveStart(c.FilterOffset)
			if err != nil {
				return nil, err
			}
			h.FilterStart = filterStart
		default:
			h.Kind = HandlerCatch
			typeName, err := resolveTypeTokenOrGeneric(md, c.ClassToken)
			if err != nil {
				return nil, fmt.Errorf("ir: exception handler catch type: %w", err)
			}
			h.CatchTypeFullName = typeName
		}
		handlers = append(handlers, h)
	}
	return handlers, nil
}

// UnsupportedOpcodeError is returned by Build when IL uses an opcode Fase
// 1-2 don't lower to IR yet. It's a distinct type (rather than a bare
// fmt.Errorf) so internal/checker can extract the opcode/offset
// programmatically instead of parsing an error string.
type UnsupportedOpcodeError struct {
	OpCode string
	Offset int
}

func (e *UnsupportedOpcodeError) Error() string {
	return fmt.Sprintf("ir: unsupported opcode %q at IL offset %d (not yet implemented — see docs/en/ROADMAP.md)", e.OpCode, e.Offset)
}

func operandIndex(operand any) int {
	switch v := operand.(type) {
	case uint8:
		return int(v)
	case uint16:
		return int(v)
	}
	return 0
}

func resolveCallTarget(md *metadata.Metadata, token uint32) (fullName string, hasThis bool, argCount int, hasReturn bool, paramTypeNames []string, methodGenericArgs []string, err error) {
	t := metadata.Token(token)
	switch t.Table() {
	case metadata.TableMethodDef:
		row, err := md.MethodDef(t.RID())
		if err != nil {
			return "", false, 0, false, nil, nil, err
		}
		typeRID, err := md.MethodDefOwner(t.RID())
		if err != nil {
			return "", false, 0, false, nil, nil, err
		}
		typeDef, err := md.TypeDef(typeRID)
		if err != nil {
			return "", false, 0, false, nil, nil, err
		}
		sig, err := metadata.ParseMethodSig(row.Signature)
		if err != nil {
			return "", false, 0, false, nil, nil, err
		}
		typeName, err := QualifyTypeDefName(md, typeRID, typeDef)
		if err != nil {
			return "", false, 0, false, nil, nil, err
		}
		full := typeName + "::" + row.Name
		return full, sig.HasThis, int(sig.ParamCount), sig.RetType.Kind != metadata.SigVoid, sigParamTypeNames(md, sig), nil, nil

	case metadata.TableMemberRef:
		row, err := md.MemberRef(t.RID())
		if err != nil {
			return "", false, 0, false, nil, nil, err
		}
		sig, err := metadata.ParseMethodSig(row.Signature)
		if err != nil {
			return "", false, 0, false, nil, nil, err
		}
		typeName, err := resolveMemberRefClassName(md, row.Class)
		if err != nil {
			return "", false, 0, false, nil, nil, err
		}
		full := typeName + "::" + row.Name
		return full, sig.HasThis, int(sig.ParamCount), sig.RetType.Kind != metadata.SigVoid, sigParamTypeNames(md, sig), nil, nil

	case metadata.TableMethodSpec:
		// A generic method instantiation (e.g. Guard.Against.Null<string>)
		// unwraps to a regular MethodDef/MemberRef call — vmnet's
		// runtime.Value is already type-erased, so the type arguments in
		// row.Instantiation usually aren't needed to execute it. The one
		// real exception (Fase 3.40): `typeof(TFeature)` inside the
		// method's own body (a generic method parameter, not a generic
		// TYPE's), which a fixed-at-build-time IR has no other way to
		// resolve — so the instantiation's own type names are resolved
		// here and carried on the call site for exactly that.
		row, err := md.MethodSpec(t.RID())
		if err != nil {
			return "", false, 0, false, nil, nil, err
		}
		name, hasThis, argCount, hasReturn, paramTypeNames, _, err := resolveCallTarget(md, uint32(row.Method))
		if err != nil {
			return "", false, 0, false, nil, nil, err
		}
		genericArgs, gerr := methodSpecGenericArgNames(md, row.Instantiation)
		if gerr != nil {
			genericArgs = nil
		}
		return name, hasThis, argCount, hasReturn, paramTypeNames, genericArgs, nil

	default:
		return "", false, 0, false, nil, nil, fmt.Errorf("unsupported call target table %#x", byte(t.Table()))
	}
}

// methodSpecGenericArgNames resolves a MethodSpec's own Instantiation blob
// to each type argument's full name, reusing SigTypeFullName the same way
// ldtoken's closed-TypeSpec handling already does — see
// resolveCallTarget's TableMethodSpec case for why this matters.
func methodSpecGenericArgNames(md *metadata.Metadata, instantiation []byte) ([]string, error) {
	types, err := metadata.ParseMethodSpec(instantiation)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(types))
	for i, t := range types {
		name, err := SigTypeFullName(md, t)
		if err != nil {
			return nil, err
		}
		names[i] = name
	}
	return names, nil
}

// sigParamTypeNames resolves each of sig's declared parameter types to a
// plain "Namespace.Type" name where possible (Fase 3.40) — "" for a
// parameter whose type doesn't resolve this way (an open generic method
// parameter, most commonly; see ir.Call.ParamTypeNames's own doc comment
// for why this exists at all). Best-effort: a resolution error for one
// parameter degrades that slot to "" rather than failing the whole call
// site, since this is only ever an optional disambiguation aid, never a
// hard requirement for the call to resolve at all.
//
// SigGenericInst must resolve through SigTypeFullName (which keeps the
// closed type arguments, e.g. "System.ReadOnlyMemory`1[[System.Char]]"),
// not the bare open-generic resolveTypeToken(md, p.Token) this used to
// call — found running real System.Text.Json 8.0.5:
// JsonDocument.Parse(string, JsonDocumentOptions)'s own real IL calls
// `Parse(json.AsMemory(), options)`, whose target overload set has BOTH
// Parse(ReadOnlyMemory<byte>, JsonDocumentOptions) and
// Parse(ReadOnlyMemory<char>, JsonDocumentOptions) — same arity, and
// (with the open-name-only resolution) the identical erased param name
// "System.ReadOnlyMemory`1" for both, so pickMethodOverload's exact-match
// bonus (assembly.go) applied equally to either candidate and the tie
// silently fell to whichever came first in the metadata table (the byte
// overload) — feeding the JSON's raw UTF-16 char buffer straight into the
// UTF-8 Utf8JsonReader with no transcoding at all, corrupting the parse
// (Utf8JsonReader then desyncs mid-token, surfacing as a confusing
// downstream "JsonReaderException: EndOfStringNotFound" with no hint the
// real bug was overload resolution). Retaining the closed generic
// argument here makes the two overloads' captured ParamTypeNames actually
// differ ("...[[System.Byte]]" vs "...[[System.Char]]"), so the
// exact-match bonus only ever applies to the real target.
func sigParamTypeNames(md *metadata.Metadata, sig metadata.MethodSig) []string {
	names := make([]string, len(sig.Params))
	for i, p := range sig.Params {
		switch p.Kind {
		case metadata.SigClass, metadata.SigValueType, metadata.SigGenericInst:
			if name, err := SigTypeFullName(md, p); err == nil {
				names[i] = name
			}
		case metadata.SigChar:
			// The one primitive worth resolving here (rather than every
			// ELEMENT_TYPE primitive): char and int32 both collapse to
			// the same KindI4 Value at runtime with nothing else to tell
			// them apart (spec §17.1, no distinct char Kind) — needed by
			// convertCharArgsForNative (internal/interpreter/calls.go,
			// Fase 3.40) to recover which KindI4 call-site argument was
			// actually a char, e.g. StringBuilder.Append('/').
			names[i] = "System.Char"
		}
	}
	return names
}

// qualifyTypeRefName resolves a TypeRef's full name, walking ResolutionScope
// when it points to another TypeRef instead of a Module/ModuleRef/
// AssemblyRef — spec §II.22.38: that's how a *nested* type (List`1's own
// Enumerator, say) is encoded, and a nested type's own Namespace column is
// always empty (it inherits context from its enclosing type instead). Left
// unresolved, every nested type's name collapses to its bare Name with no
// qualification at all — "Enumerator", indistinguishable from any other
// type named Enumerator nested anywhere in any loaded assembly. Found
// while wiring up List<T>/Dictionary<T> foreach support (Fase 3.11):
// registering a bcl native under that bare, colliding name would have
// silently hijacked an unrelated type's real (interpreted) method of the
// same name — the "+" separator matches .NET's own Type.FullName
// convention for nested types.
func qualifyTypeRefName(md *metadata.Metadata, row metadata.TypeRefRow) (string, error) {
	if row.ResolutionScope.Table() != metadata.TableTypeRef {
		return Qualify(row.Namespace, row.Name), nil
	}
	enclosing, err := md.TypeRef(row.ResolutionScope.RID())
	if err != nil {
		return "", err
	}
	enclosingName, err := qualifyTypeRefName(md, enclosing)
	if err != nil {
		return "", err
	}
	return enclosingName + "+" + row.Name, nil
}

// QualifyTypeDefName resolves a TypeDef's full name, walking the
// NestedClass table (spec §II.22.32) when it's a nested type — the
// TypeDef-table counterpart of qualifyTypeRefName above, needed for a
// PLUGIN's own nested types (as opposed to a foreign/BCL one, referenced
// via TypeRef). Found the hard way (Fase 3.17): the C# compiler emits
// one non-capturing-lambda cache class (named literally "<>c") PER
// enclosing type that has any — an assembly with lambdas in two
// different classes ends up with two entirely separate TypeDefs both
// named "<>c" (same bare Name, both Namespace ""). Collapsing either to
// its bare name — what every call site below did before this fix — picks
// whichever one metadata.FindTypeDef happens to scan first, silently
// resolving ldsfld/ldfld/newobj/call against the WRONG type's members
// (a real, reproduced bug: two fixture files each with lambdas produced
// two "<>c" TypeDefs, and the second file's lambda cache field lookups
// resolved against the first file's "<>c" instead). A nested TypeDef's
// own Namespace column is always empty (spec: it inherits the enclosing
// type's), same reasoning as qualifyTypeRefName.
func QualifyTypeDefName(md *metadata.Metadata, typeRID uint32, row metadata.TypeDefRow) (string, error) {
	enclosingRID, ok, err := md.EnclosingClass(typeRID)
	if err != nil {
		return "", err
	}
	if !ok {
		return Qualify(row.Namespace, row.Name), nil
	}
	enclosingRow, err := md.TypeDef(enclosingRID)
	if err != nil {
		return "", err
	}
	enclosingName, err := QualifyTypeDefName(md, enclosingRID, enclosingRow)
	if err != nil {
		return "", err
	}
	return enclosingName + "+" + row.Name, nil
}

func resolveMemberRefClassName(md *metadata.Metadata, class metadata.Token) (string, error) {
	switch class.Table() {
	case metadata.TableTypeRef:
		row, err := md.TypeRef(class.RID())
		if err != nil {
			return "", err
		}
		return qualifyTypeRefName(md, row)
	case metadata.TableTypeDef:
		row, err := md.TypeDef(class.RID())
		if err != nil {
			return "", err
		}
		return QualifyTypeDefName(md, class.RID(), row)
	case metadata.TableTypeSpec:
		return resolveTypeSpecName(md, class.RID())
	default:
		return "", fmt.Errorf("unsupported MemberRef class table %#x", byte(class.Table()))
	}
}

// resolveTypeSpecName resolves a TypeSpec (used for generic instantiations
// like List<int>) to its *open* generic type's name — e.g.
// "System.Collections.Generic.List`1". Type arguments aren't retained:
// vmnet's native collection backing doesn't need them (see
// internal/bcl/system_collections.go).
func resolveTypeSpecName(md *metadata.Metadata, rid uint32) (string, error) {
	sig, err := md.TypeSpecSignature(rid)
	if err != nil {
		return "", err
	}
	t, err := metadata.ParseTypeSpec(sig)
	if err != nil {
		return "", err
	}
	if t.Kind != metadata.SigGenericInst {
		return "", fmt.Errorf("unsupported TypeSpec kind %d", t.Kind)
	}
	return resolveTypeToken(md, t.Token)
}

// resolveClosedTypeSpecName resolves a TypeSpec to its full reflection-style
// name INCLUDING type arguments (Fase 3.25) — used only by ldtoken's
// typeof(T) path (System.Type reflection needs GetGenericArguments() to
// work), unlike resolveTypeSpecName above which every other TypeSpec
// consumer in this file still uses (they only need the open generic name).
func resolveClosedTypeSpecName(md *metadata.Metadata, rid uint32) (string, error) {
	sig, err := md.TypeSpecSignature(rid)
	if err != nil {
		return "", err
	}
	t, err := metadata.ParseTypeSpec(sig)
	if err != nil {
		return "", err
	}
	return SigTypeFullName(md, t)
}

// SigTypeFullName resolves any parsed SigType to a reflection-style full
// name — for a closed generic instantiation, recursively including its
// type arguments as "Open`N[[Arg1],[Arg2]]" (a simplified form of real
// .NET's assembly-qualified closed-generic FullName: good enough for
// vmnet's own Type.FullName-string-based equality/parsing, since nothing
// here ever needs to round-trip through a real CLR loader).
func SigTypeFullName(md *metadata.Metadata, t metadata.SigType) (string, error) {
	switch t.Kind {
	case metadata.SigBoolean:
		return "System.Boolean", nil
	case metadata.SigChar:
		return "System.Char", nil
	case metadata.SigI1:
		return "System.SByte", nil
	case metadata.SigU1:
		return "System.Byte", nil
	case metadata.SigI2:
		return "System.Int16", nil
	case metadata.SigU2:
		return "System.UInt16", nil
	case metadata.SigI4:
		return "System.Int32", nil
	case metadata.SigU4:
		return "System.UInt32", nil
	case metadata.SigI8:
		return "System.Int64", nil
	case metadata.SigU8:
		return "System.UInt64", nil
	case metadata.SigR4:
		return "System.Single", nil
	case metadata.SigR8:
		return "System.Double", nil
	case metadata.SigI:
		return "System.IntPtr", nil
	case metadata.SigU:
		return "System.UIntPtr", nil
	case metadata.SigString:
		return "System.String", nil
	case metadata.SigObject:
		return "System.Object", nil
	case metadata.SigClass, metadata.SigValueType:
		return resolveTypeToken(md, t.Token)
	case metadata.SigSZArray:
		if t.Elem == nil {
			return "", fmt.Errorf("ir: array type signature with no element type")
		}
		elemName, err := SigTypeFullName(md, *t.Elem)
		if err != nil {
			return "", err
		}
		return elemName + "[]", nil
	case metadata.SigGenericInst:
		openName, err := resolveTypeToken(md, t.Token)
		if err != nil {
			return "", err
		}
		if len(t.Args) == 0 {
			return openName, nil
		}
		argNames := make([]string, len(t.Args))
		for i, arg := range t.Args {
			argName, err := SigTypeFullName(md, arg)
			if err != nil {
				return "", err
			}
			argNames[i] = argName
		}
		return openName + "[[" + strings.Join(argNames, "],[") + "]]", nil
	case metadata.SigGenericParam:
		// An unresolved method/type generic parameter (T, !!0) — same ""
		// convention resolveTypeTokenOrGeneric already uses for this case.
		return "", nil
	default:
		return "", fmt.Errorf("ir: unsupported generic type argument kind %d", t.Kind)
	}
}

// resolveTypeTokenOrGeneric resolves the inline TypeDefOrRefOrSpec token used
// by initobj/ldobj/stobj (spec §III.4.10 et al.) to a type full name, or
// "" if it names an unresolved generic type parameter (a TypeSpec whose
// blob is VAR/MVAR — see InitObj's doc comment in ir.go).
func resolveTypeTokenOrGeneric(md *metadata.Metadata, token uint32) (string, error) {
	tok := metadata.Token(token)
	if tok.Table() != metadata.TableTypeSpec {
		return resolveTypeToken(md, tok)
	}
	sig, err := md.TypeSpecSignature(tok.RID())
	if err != nil {
		return "", err
	}
	t, err := metadata.ParseTypeSpec(sig)
	if err != nil {
		return "", err
	}
	switch t.Kind {
	case metadata.SigGenericParam:
		return "", nil
	case metadata.SigGenericInst, metadata.SigClass, metadata.SigValueType:
		return resolveTypeToken(md, t.Token)
	case metadata.SigSZArray:
		// `isinst T[]`/`is T[]` (Fase 3.27, found running real Jint/
		// Esprima) — vmnet's isAssignableTo (internal/interpreter/
		// typecheck.go) already collapses every array to the single
		// name "System.Array" regardless of element type (a documented
		// simplification since Fase 3.5: KindArray only ever matches
		// that literal string), so resolving here to anything more
		// specific would just never match anyway; this keeps the two
		// consistent.
		return "System.Array", nil
	default:
		return "", fmt.Errorf("unsupported initobj/ldobj/stobj TypeSpec kind %d", t.Kind)
	}
}

func resolveTypeToken(md *metadata.Metadata, tok metadata.Token) (string, error) {
	switch tok.Table() {
	case metadata.TableTypeRef:
		row, err := md.TypeRef(tok.RID())
		if err != nil {
			return "", err
		}
		return qualifyTypeRefName(md, row)
	case metadata.TableTypeDef:
		row, err := md.TypeDef(tok.RID())
		if err != nil {
			return "", err
		}
		return QualifyTypeDefName(md, tok.RID(), row)
	default:
		return "", fmt.Errorf("unsupported type token table %#x", byte(tok.Table()))
	}
}

func resolveNewObjTarget(md *metadata.Metadata, token uint32) (typeFullName, ctorFullName string, argCount int, paramTypeNames []string, err error) {
	t := metadata.Token(token)
	switch t.Table() {
	case metadata.TableMethodDef:
		row, err := md.MethodDef(t.RID())
		if err != nil {
			return "", "", 0, nil, err
		}
		typeRID, err := md.MethodDefOwner(t.RID())
		if err != nil {
			return "", "", 0, nil, err
		}
		typeDef, err := md.TypeDef(typeRID)
		if err != nil {
			return "", "", 0, nil, err
		}
		sig, err := metadata.ParseMethodSig(row.Signature)
		if err != nil {
			return "", "", 0, nil, err
		}
		typeFullName, err = QualifyTypeDefName(md, typeRID, typeDef)
		if err != nil {
			return "", "", 0, nil, err
		}
		return typeFullName, typeFullName + "::" + row.Name, int(sig.ParamCount), sigParamTypeNames(md, sig), nil

	case metadata.TableMemberRef:
		row, err := md.MemberRef(t.RID())
		if err != nil {
			return "", "", 0, nil, err
		}
		sig, err := metadata.ParseMethodSig(row.Signature)
		if err != nil {
			return "", "", 0, nil, err
		}
		typeFullName, err = resolveMemberRefClassName(md, row.Class)
		if err != nil {
			return "", "", 0, nil, err
		}
		return typeFullName, typeFullName + "::" + row.Name, int(sig.ParamCount), sigParamTypeNames(md, sig), nil

	default:
		return "", "", 0, nil, fmt.Errorf("unsupported newobj target table %#x", byte(t.Table()))
	}
}

func resolveFieldTarget(md *metadata.Metadata, token uint32) (typeFullName, fieldName string, err error) {
	t := metadata.Token(token)
	switch t.Table() {
	case metadata.TableField:
		row, err := md.Field(t.RID())
		if err != nil {
			return "", "", err
		}
		typeRID, err := md.FieldDefOwner(t.RID())
		if err != nil {
			return "", "", err
		}
		typeDef, err := md.TypeDef(typeRID)
		if err != nil {
			return "", "", err
		}
		typeFullName, err = QualifyTypeDefName(md, typeRID, typeDef)
		if err != nil {
			return "", "", err
		}
		return typeFullName, row.Name, nil

	case metadata.TableMemberRef:
		// A field declared outside this assembly (e.g. on a BCL type) is
		// referenced the same way an external method is — resolving the
		// owning type only needs Class, never the signature blob (which
		// for a field uses a different, FIELD-tagged format than a
		// method's MethodDefSig/MethodRefSig).
		row, err := md.MemberRef(t.RID())
		if err != nil {
			return "", "", err
		}
		typeName, err := resolveMemberRefClassName(md, row.Class)
		if err != nil {
			return "", "", err
		}
		return typeName, row.Name, nil

	default:
		return "", "", fmt.Errorf("unsupported field target table %#x", byte(t.Table()))
	}
}

// Qualify joins a namespace and a type/member name the way vmnet full
// names use ("Namespace.Type"), omitting the dot when namespace is empty.
func Qualify(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "." + name
}

// SplitTypeName reverses Qualify: it splits "Namespace.Type" at the last
// dot (a type's own name never contains one, even nested/generic types —
// nested classes use NestedClass rows, and generic arity like `List`1`
// has no dot either).
func SplitTypeName(typeName string) (namespace, name string) {
	dot := strings.LastIndex(typeName, ".")
	if dot < 0 {
		return "", typeName
	}
	return typeName[:dot], typeName[dot+1:]
}

// SplitFullName splits a "Namespace.Type::Method" full name (as produced
// by resolveCallTarget/resolveNewObjTarget) into its three parts.
func SplitFullName(fullName string) (namespace, typeName, methodName string, err error) {
	idx := strings.LastIndex(fullName, "::")
	if idx < 0 {
		return "", "", "", fmt.Errorf("ir: invalid method full name %q", fullName)
	}
	ns, tn := SplitTypeName(fullName[:idx])
	return ns, tn, fullName[idx+2:], nil
}
