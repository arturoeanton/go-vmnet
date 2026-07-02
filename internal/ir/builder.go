package ir

import (
	"fmt"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/il"
	"github.com/arturoeanton/go-vmnet/internal/metadata"
)

// Build lowers one method's decoded IL into IR. md resolves ldstr/call
// tokens; retVoid tells Build how to lower `ret` (with or without a
// value) since IL's `ret` opcode carries no operand of its own.
//
// Anything CIL can express that Fase 1 doesn't model — objects, callvirt,
// arrays, exceptions, generics — is reported as an explicit unsupported-
// opcode error instead of silently mis-translated (spec §11.3, §23).
func Build(instrs []il.Instruction, md *metadata.Metadata, retVoid bool) ([]Instr, error) {
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
				return nil, fmt.Errorf("ir: ldstr at IL offset %d: %w", instr.Offset, err)
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
				return nil, err
			}
			out = append(out, Branch{target})
		case "brtrue.s", "brtrue":
			target, err := resolveTarget(instr.Operand.(int))
			if err != nil {
				return nil, err
			}
			out = append(out, BranchIfTrue{target})
		case "brfalse.s", "brfalse":
			target, err := resolveTarget(instr.Operand.(int))
			if err != nil {
				return nil, err
			}
			out = append(out, BranchIfFalse{target})
		case "switch":
			offsets := instr.Operand.([]int)
			targets := make([]int, len(offsets))
			for i, off := range offsets {
				target, err := resolveTarget(off)
				if err != nil {
					return nil, err
				}
				targets[i] = target
			}
			out = append(out, Switch{Targets: targets})
		case "beq.s", "beq":
			target, err := resolveTarget(instr.Operand.(int))
			if err != nil {
				return nil, err
			}
			out = append(out, BranchCompare{Target: target, Op: CmpEq})
		case "bge.s", "bge":
			target, err := resolveTarget(instr.Operand.(int))
			if err != nil {
				return nil, err
			}
			out = append(out, BranchCompare{Target: target, Op: CmpGe})
		case "bge.un.s", "bge.un":
			target, err := resolveTarget(instr.Operand.(int))
			if err != nil {
				return nil, err
			}
			out = append(out, BranchCompare{Target: target, Op: CmpGe, Unsigned: true})
		case "bgt.s", "bgt":
			target, err := resolveTarget(instr.Operand.(int))
			if err != nil {
				return nil, err
			}
			out = append(out, BranchCompare{Target: target, Op: CmpGt})
		case "bgt.un.s", "bgt.un":
			target, err := resolveTarget(instr.Operand.(int))
			if err != nil {
				return nil, err
			}
			out = append(out, BranchCompare{Target: target, Op: CmpGt, Unsigned: true})
		case "ble.s", "ble":
			target, err := resolveTarget(instr.Operand.(int))
			if err != nil {
				return nil, err
			}
			out = append(out, BranchCompare{Target: target, Op: CmpLe})
		case "ble.un.s", "ble.un":
			target, err := resolveTarget(instr.Operand.(int))
			if err != nil {
				return nil, err
			}
			out = append(out, BranchCompare{Target: target, Op: CmpLe, Unsigned: true})
		case "blt.s", "blt":
			target, err := resolveTarget(instr.Operand.(int))
			if err != nil {
				return nil, err
			}
			out = append(out, BranchCompare{Target: target, Op: CmpLt})
		case "blt.un.s", "blt.un":
			target, err := resolveTarget(instr.Operand.(int))
			if err != nil {
				return nil, err
			}
			out = append(out, BranchCompare{Target: target, Op: CmpLt, Unsigned: true})
		case "bne.un.s", "bne.un":
			target, err := resolveTarget(instr.Operand.(int))
			if err != nil {
				return nil, err
			}
			out = append(out, BranchCompare{Target: target, Op: CmpNe, Unsigned: true})

		case "call":
			token := instr.Operand.(uint32)
			fullName, hasThis, argCount, hasReturn, err := resolveCallTarget(md, token)
			if err != nil {
				return nil, fmt.Errorf("ir: call at IL offset %d: %w", instr.Offset, err)
			}
			out = append(out, Call{FullName: fullName, ArgCount: argCount, HasThis: hasThis, HasReturn: hasReturn})

		case "callvirt":
			token := instr.Operand.(uint32)
			fullName, _, argCount, hasReturn, err := resolveCallTarget(md, token)
			if err != nil {
				return nil, fmt.Errorf("ir: callvirt at IL offset %d: %w", instr.Offset, err)
			}
			out = append(out, Call{FullName: fullName, ArgCount: argCount, HasThis: true, HasReturn: hasReturn, Virtual: true})

		case "newobj":
			token := instr.Operand.(uint32)
			typeFullName, ctorFullName, argCount, err := resolveNewObjTarget(md, token)
			if err != nil {
				return nil, fmt.Errorf("ir: newobj at IL offset %d: %w", instr.Offset, err)
			}
			out = append(out, NewObj{TypeFullName: typeFullName, CtorFullName: ctorFullName, ArgCount: argCount})

		case "ldfld":
			token := instr.Operand.(uint32)
			typeFullName, fieldName, err := resolveFieldTarget(md, token)
			if err != nil {
				return nil, fmt.Errorf("ir: ldfld at IL offset %d: %w", instr.Offset, err)
			}
			out = append(out, LoadField{TypeFullName: typeFullName, FieldName: fieldName})

		case "stfld":
			token := instr.Operand.(uint32)
			typeFullName, fieldName, err := resolveFieldTarget(md, token)
			if err != nil {
				return nil, fmt.Errorf("ir: stfld at IL offset %d: %w", instr.Offset, err)
			}
			out = append(out, StoreField{TypeFullName: typeFullName, FieldName: fieldName})

		case "ldsfld":
			token := instr.Operand.(uint32)
			typeFullName, fieldName, err := resolveFieldTarget(md, token)
			if err != nil {
				return nil, fmt.Errorf("ir: ldsfld at IL offset %d: %w", instr.Offset, err)
			}
			out = append(out, LoadStaticField{TypeFullName: typeFullName, FieldName: fieldName})

		case "stsfld":
			token := instr.Operand.(uint32)
			typeFullName, fieldName, err := resolveFieldTarget(md, token)
			if err != nil {
				return nil, fmt.Errorf("ir: stsfld at IL offset %d: %w", instr.Offset, err)
			}
			out = append(out, StoreStaticField{TypeFullName: typeFullName, FieldName: fieldName})

		case "ldflda":
			token := instr.Operand.(uint32)
			typeFullName, fieldName, err := resolveFieldTarget(md, token)
			if err != nil {
				return nil, fmt.Errorf("ir: ldflda at IL offset %d: %w", instr.Offset, err)
			}
			out = append(out, LoadFieldAddr{TypeFullName: typeFullName, FieldName: fieldName})

		case "box", "unbox.any":
			// vmnet's runtime.Value is already a uniform tagged union —
			// boxing a value type doesn't need a representation change.
			// Correctness gap: unbox.any doesn't verify the target type.
			out = append(out, Nop{})

		case "throw":
			out = append(out, Throw{})

		case "newarr":
			out = append(out, NewArr{})
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
				return nil, fmt.Errorf("ir: initobj at IL offset %d: %w", instr.Offset, err)
			}
			out = append(out, InitObj{TypeFullName: typeFullName})

		case "isinst":
			token := instr.Operand.(uint32)
			typeFullName, err := resolveTypeTokenOrGeneric(md, token)
			if err != nil {
				return nil, fmt.Errorf("ir: isinst at IL offset %d: %w", instr.Offset, err)
			}
			out = append(out, IsInst{TypeFullName: typeFullName})

		case "castclass":
			token := instr.Operand.(uint32)
			typeFullName, err := resolveTypeTokenOrGeneric(md, token)
			if err != nil {
				return nil, fmt.Errorf("ir: castclass at IL offset %d: %w", instr.Offset, err)
			}
			out = append(out, CastClass{TypeFullName: typeFullName})

		case "ldftn":
			token := instr.Operand.(uint32)
			fullName, _, _, _, err := resolveCallTarget(md, token)
			if err != nil {
				return nil, fmt.Errorf("ir: ldftn at IL offset %d: %w", instr.Offset, err)
			}
			out = append(out, LoadFtn{FullName: fullName})

		case "ldvirtftn":
			token := instr.Operand.(uint32)
			fullName, _, _, _, err := resolveCallTarget(md, token)
			if err != nil {
				return nil, fmt.Errorf("ir: ldvirtftn at IL offset %d: %w", instr.Offset, err)
			}
			out = append(out, LoadFtn{FullName: fullName, Virtual: true})

		case "constrained.", "volatile.", "readonly.":
			// Prefixes vmnet doesn't need: constrained. only matters for
			// choosing between boxing and a value type's own override at a
			// following callvirt — vmnet's Value is already a tagged union
			// carrying its real Kind, so a callvirt to e.g.
			// System.Object::ToString/Equals/GetHashCode already dispatches
			// on the actual value (see internal/bcl/system_object.go).
			// volatile./readonly. are pure optimizer hints (memory
			// ordering, "won't mutate through this address") that don't
			// affect a single-goroutine-per-call interpreter with
			// Value-based storage instead of raw memory.
			out = append(out, Nop{})

		case "ret":
			out = append(out, Return{HasValue: !retVoid})

		default:
			return nil, &UnsupportedOpcodeError{OpCode: name, Offset: instr.Offset}
		}
	}
	return out, nil
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
	return fmt.Sprintf("ir: unsupported opcode %q at IL offset %d (not yet implemented — see docs/ROADMAP.md)", e.OpCode, e.Offset)
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

func resolveCallTarget(md *metadata.Metadata, token uint32) (fullName string, hasThis bool, argCount int, hasReturn bool, err error) {
	t := metadata.Token(token)
	switch t.Table() {
	case metadata.TableMethodDef:
		row, err := md.MethodDef(t.RID())
		if err != nil {
			return "", false, 0, false, err
		}
		typeRID, err := md.MethodDefOwner(t.RID())
		if err != nil {
			return "", false, 0, false, err
		}
		typeDef, err := md.TypeDef(typeRID)
		if err != nil {
			return "", false, 0, false, err
		}
		sig, err := metadata.ParseMethodSig(row.Signature)
		if err != nil {
			return "", false, 0, false, err
		}
		full := Qualify(typeDef.Namespace, typeDef.Name) + "::" + row.Name
		return full, sig.HasThis, int(sig.ParamCount), sig.RetType.Kind != metadata.SigVoid, nil

	case metadata.TableMemberRef:
		row, err := md.MemberRef(t.RID())
		if err != nil {
			return "", false, 0, false, err
		}
		sig, err := metadata.ParseMethodSig(row.Signature)
		if err != nil {
			return "", false, 0, false, err
		}
		typeName, err := resolveMemberRefClassName(md, row.Class)
		if err != nil {
			return "", false, 0, false, err
		}
		full := typeName + "::" + row.Name
		return full, sig.HasThis, int(sig.ParamCount), sig.RetType.Kind != metadata.SigVoid, nil

	case metadata.TableMethodSpec:
		// A generic method instantiation (e.g. Guard.Against.Null<string>)
		// unwraps to a regular MethodDef/MemberRef call — vmnet's
		// runtime.Value is already type-erased, so the type arguments in
		// row.Instantiation aren't needed to execute it.
		row, err := md.MethodSpec(t.RID())
		if err != nil {
			return "", false, 0, false, err
		}
		return resolveCallTarget(md, uint32(row.Method))

	default:
		return "", false, 0, false, fmt.Errorf("unsupported call target table %#x", byte(t.Table()))
	}
}

func resolveMemberRefClassName(md *metadata.Metadata, class metadata.Token) (string, error) {
	switch class.Table() {
	case metadata.TableTypeRef:
		row, err := md.TypeRef(class.RID())
		if err != nil {
			return "", err
		}
		return Qualify(row.Namespace, row.Name), nil
	case metadata.TableTypeDef:
		row, err := md.TypeDef(class.RID())
		if err != nil {
			return "", err
		}
		return Qualify(row.Namespace, row.Name), nil
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
		return Qualify(row.Namespace, row.Name), nil
	case metadata.TableTypeDef:
		row, err := md.TypeDef(tok.RID())
		if err != nil {
			return "", err
		}
		return Qualify(row.Namespace, row.Name), nil
	default:
		return "", fmt.Errorf("unsupported type token table %#x", byte(tok.Table()))
	}
}

func resolveNewObjTarget(md *metadata.Metadata, token uint32) (typeFullName, ctorFullName string, argCount int, err error) {
	t := metadata.Token(token)
	switch t.Table() {
	case metadata.TableMethodDef:
		row, err := md.MethodDef(t.RID())
		if err != nil {
			return "", "", 0, err
		}
		typeRID, err := md.MethodDefOwner(t.RID())
		if err != nil {
			return "", "", 0, err
		}
		typeDef, err := md.TypeDef(typeRID)
		if err != nil {
			return "", "", 0, err
		}
		sig, err := metadata.ParseMethodSig(row.Signature)
		if err != nil {
			return "", "", 0, err
		}
		typeFullName = Qualify(typeDef.Namespace, typeDef.Name)
		return typeFullName, typeFullName + "::" + row.Name, int(sig.ParamCount), nil

	case metadata.TableMemberRef:
		row, err := md.MemberRef(t.RID())
		if err != nil {
			return "", "", 0, err
		}
		sig, err := metadata.ParseMethodSig(row.Signature)
		if err != nil {
			return "", "", 0, err
		}
		typeFullName, err = resolveMemberRefClassName(md, row.Class)
		if err != nil {
			return "", "", 0, err
		}
		return typeFullName, typeFullName + "::" + row.Name, int(sig.ParamCount), nil

	default:
		return "", "", 0, fmt.Errorf("unsupported newobj target table %#x", byte(t.Table()))
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
		return Qualify(typeDef.Namespace, typeDef.Name), row.Name, nil

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
