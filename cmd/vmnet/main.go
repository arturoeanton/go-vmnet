// Command vmnet is the CLI for the vmnet IL/CIL runtime. Fase 1 ships
// inspect/il/run; check/add/restore/packages land in Fase 3 (see
// docs/ROADMAP.md).
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	vmnet "github.com/arturoeanton/go-vmnet"
	"github.com/arturoeanton/go-vmnet/internal/il"
	"github.com/arturoeanton/go-vmnet/internal/metadata"
	"github.com/arturoeanton/go-vmnet/internal/pe"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "inspect":
		err = runInspect(os.Args[2:])
	case "il":
		err = runIL(os.Args[2:])
	case "run":
		err = runRun(os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "vmnet: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  vmnet inspect <dll>
  vmnet il <dll> <Type.Method>
  vmnet run <dll> <Type.Method> '<json-array-of-args>'`)
}

func loadRaw(path string) (*pe.File, *metadata.Metadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	f, err := pe.Parse(data)
	if err != nil {
		return nil, nil, err
	}
	md, err := metadata.Parse(f.Metadata)
	if err != nil {
		return nil, nil, err
	}
	return f, md, nil
}

func runInspect(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: vmnet inspect <dll>")
	}
	_, md, err := loadRaw(args[0])
	if err != nil {
		return err
	}

	if asmRow, err := md.Assembly(1); err == nil {
		fmt.Printf("Assembly: %s\n", asmRow.Name)
		fmt.Printf("Version: %d.%d.%d.%d\n", asmRow.MajorVersion, asmRow.MinorVersion, asmRow.BuildNumber, asmRow.RevisionNumber)
	}

	fmt.Println("Types:")
	typeCount := md.RowCount(metadata.TableTypeDef)
	for rid := uint32(1); rid <= typeCount; rid++ {
		t, err := md.TypeDef(rid)
		if err != nil {
			return err
		}
		if t.Name == "<Module>" {
			continue
		}
		fmt.Printf("- %s\n", qualify(t.Namespace, t.Name))
	}

	fmt.Println("Methods:")
	methodCount := md.RowCount(metadata.TableMethodDef)
	for rid := uint32(1); rid <= methodCount; rid++ {
		m, err := md.MethodDef(rid)
		if err != nil {
			return err
		}
		ownerRID, err := md.MethodDefOwner(rid)
		if err != nil {
			continue
		}
		t, err := md.TypeDef(ownerRID)
		if err != nil {
			continue
		}
		fmt.Printf("- %s.%s(...)\n", qualify(t.Namespace, t.Name), m.Name)
	}
	return nil
}

func runIL(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: vmnet il <dll> <Type.Method>")
	}
	f, md, err := loadRaw(args[0])
	if err != nil {
		return err
	}
	typeName, methodName, err := splitTypeMethod(args[1])
	if err != nil {
		return err
	}
	namespace, name := splitLastDot(typeName)
	typeRID, _, err := md.FindTypeDef(namespace, name)
	if err != nil {
		return err
	}
	_, m, err := md.FindMethodDef(typeRID, methodName)
	if err != nil {
		return err
	}
	if m.RVA == 0 {
		return fmt.Errorf("%s.%s has no body (abstract/extern)", typeName, methodName)
	}

	body, err := f.RVA(m.RVA)
	if err != nil {
		return err
	}
	_, code, err := il.ReadMethodBody(body)
	if err != nil {
		return err
	}
	instrs, err := il.Decode(code)
	if err != nil {
		return err
	}
	for _, instr := range instrs {
		if instr.Operand == nil {
			fmt.Printf("IL_%04x: %s\n", instr.Offset, instr.OpCode.Name())
		} else {
			fmt.Printf("IL_%04x: %s %v\n", instr.Offset, instr.OpCode.Name(), instr.Operand)
		}
	}
	return nil
}

func runRun(args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("usage: vmnet run <dll> <Type.Method> '<json-array-of-args>'")
	}
	vm := vmnet.New()
	asm, err := vm.LoadFile(args[0])
	if err != nil {
		return err
	}
	typeName, methodName, err := splitTypeMethod(args[1])
	if err != nil {
		return err
	}

	var rawArgs []json.RawMessage
	if err := json.Unmarshal([]byte(args[2]), &rawArgs); err != nil {
		return fmt.Errorf("invalid JSON argument array: %w", err)
	}
	values := make([]vmnet.Value, len(rawArgs))
	for i, raw := range rawArgs {
		v, err := jsonToValue(raw)
		if err != nil {
			return fmt.Errorf("argument %d: %w", i, err)
		}
		values[i] = v
	}

	result, err := asm.Call(typeName, methodName, values...)
	if err != nil {
		return err
	}
	if result == nil {
		fmt.Println("(void)")
		return nil
	}
	fmt.Println(result.Native())
	return nil
}

func jsonToValue(raw json.RawMessage) (vmnet.Value, error) {
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		if f == float64(int32(f)) {
			return vmnet.Int32(int32(f)), nil
		}
		return vmnet.Float64(f), nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return vmnet.String(s), nil
	}
	return nil, fmt.Errorf("unsupported JSON argument %s", raw)
}

func splitTypeMethod(s string) (typeName, methodName string, err error) {
	idx := strings.LastIndex(s, ".")
	if idx < 0 {
		return "", "", fmt.Errorf("expected <Type.Method>, got %q", s)
	}
	return s[:idx], s[idx+1:], nil
}

func splitLastDot(s string) (namespace, name string) {
	idx := strings.LastIndex(s, ".")
	if idx < 0 {
		return "", s
	}
	return s[:idx], s[idx+1:]
}

func qualify(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "." + name
}
