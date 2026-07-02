package il

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/arturoeanton/go-vmnet/internal/metadata"
	"github.com/arturoeanton/go-vmnet/internal/pe"
)

const fixtureRelPath = "../../tests/fixtures/csharp/bin/Release/netstandard2.0/Vmnet.Fixtures.dll"

func loadFixtureMethod(t *testing.T, typeName, methodName string) (MethodHeader, []Instruction) {
	t.Helper()
	path := filepath.FromSlash(fixtureRelPath)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("fixture assembly not built: %v (run `dotnet build tests/fixtures/csharp/Fixtures.csproj -c Release`)", err)
	}

	f, err := pe.Parse(data)
	if err != nil {
		t.Fatalf("pe.Parse() error = %v", err)
	}
	md, err := metadata.Parse(f.Metadata)
	if err != nil {
		t.Fatalf("metadata.Parse() error = %v", err)
	}

	typeRID, _, err := md.FindTypeDef("Vmnet.Fixtures", typeName)
	if err != nil {
		t.Fatalf("FindTypeDef(%s) error = %v", typeName, err)
	}
	_, method, err := md.FindMethodDef(typeRID, methodName)
	if err != nil {
		t.Fatalf("FindMethodDef(%s.%s) error = %v", typeName, methodName, err)
	}

	body, err := f.RVA(method.RVA)
	if err != nil {
		t.Fatalf("f.RVA(%#x) error = %v", method.RVA, err)
	}
	header, code, err := ReadMethodBody(body)
	if err != nil {
		t.Fatalf("ReadMethodBody() error = %v", err)
	}
	instrs, err := Decode(code)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	return header, instrs
}

func hasOp(instrs []Instruction, name string) bool {
	for _, i := range instrs {
		if i.OpCode.Name() == name {
			return true
		}
	}
	return false
}

func TestDecode_SimpleMathAdd(t *testing.T) {
	_, instrs := loadFixtureMethod(t, "SimpleMath", "Add")

	if len(instrs) == 0 {
		t.Fatal("Decode() returned no instructions")
	}
	if !hasOp(instrs, "add") {
		t.Errorf("SimpleMath.Add: expected an `add` instruction, got %+v", instrs)
	}
	last := instrs[len(instrs)-1]
	if last.OpCode.Name() != "ret" {
		t.Errorf("SimpleMath.Add: last instruction = %s, want ret", last.OpCode.Name())
	}
}

func TestDecode_StringsHello(t *testing.T) {
	_, instrs := loadFixtureMethod(t, "Strings", "Hello")

	if !hasOp(instrs, "ldstr") {
		t.Errorf("Strings.Hello: expected an `ldstr` instruction, got %+v", instrs)
	}
	if !hasOp(instrs, "call") {
		t.Errorf("Strings.Hello: expected a `call` instruction (String.Concat), got %+v", instrs)
	}
}

func TestDecode_LoopsSum(t *testing.T) {
	header, instrs := loadFixtureMethod(t, "Loops", "Sum")

	if !header.Fat {
		t.Errorf("Loops.Sum: header.Fat = false, want true (method has locals)")
	}
	if header.LocalVarSigToken == 0 {
		t.Errorf("Loops.Sum: header.LocalVarSigToken = 0, want a StandAloneSig token")
	}

	sawBranch := false
	for _, instr := range instrs {
		switch instr.OpCode.Name() {
		case "br.s", "blt.s", "ble.s", "br", "blt", "ble":
			sawBranch = true
			if target := instr.Operand.(int); target < 0 {
				t.Errorf("branch target %d is negative", target)
			}
		}
	}
	if !sawBranch {
		t.Errorf("Loops.Sum: expected at least one branch instruction, got %+v", instrs)
	}
	if !hasOp(instrs, "ret") {
		t.Errorf("Loops.Sum: expected a `ret` instruction")
	}
}
