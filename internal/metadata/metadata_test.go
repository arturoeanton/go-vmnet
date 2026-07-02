package metadata

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/arturoeanton/go-vmnet/internal/pe"
)

const fixtureRelPath = "../../tests/fixtures/csharp/bin/Release/netstandard2.0/Vmnet.Fixtures.dll"

func parseFixture(t *testing.T) *Metadata {
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
	md, err := Parse(f.Metadata)
	if err != nil {
		t.Fatalf("metadata.Parse() error = %v", err)
	}
	return md
}

func TestParse_RealAssembly_Assembly(t *testing.T) {
	md := parseFixture(t)

	if got := md.RowCount(TableAssembly); got != 1 {
		t.Fatalf("RowCount(Assembly) = %d, want 1", got)
	}
	asm, err := md.Assembly(1)
	if err != nil {
		t.Fatalf("Assembly(1) error = %v", err)
	}
	if asm.Name != "Vmnet.Fixtures" {
		t.Errorf("Assembly(1).Name = %q, want %q", asm.Name, "Vmnet.Fixtures")
	}
}

func TestParse_RealAssembly_TypeDefs(t *testing.T) {
	md := parseFixture(t)

	want := map[string]bool{
		"SimpleMath":      false,
		"Strings":         false,
		"Loops":           false,
		"Customer":        false,
		"CollectionsTest": false,
		"ExceptionTest":   false,
	}

	count := md.RowCount(TableTypeDef)
	if count == 0 {
		t.Fatal("RowCount(TypeDef) = 0, want > 0")
	}

	var got []string
	for rid := uint32(1); rid <= count; rid++ {
		row, err := md.TypeDef(rid)
		if err != nil {
			t.Fatalf("TypeDef(%d) error = %v", rid, err)
		}
		got = append(got, row.Namespace+"."+row.Name)
		if row.Namespace == "Vmnet.Fixtures" {
			if _, ok := want[row.Name]; ok {
				want[row.Name] = true
			}
		}
	}

	for name, found := range want {
		if !found {
			sort.Strings(got)
			t.Errorf("TypeDef %q not found in Vmnet.Fixtures; got types: %v", name, got)
		}
	}
}

func TestParse_RealAssembly_MethodDefsAndSignature(t *testing.T) {
	md := parseFixture(t)

	rid, typeDef, err := md.FindTypeDef("Vmnet.Fixtures", "SimpleMath")
	if err != nil {
		t.Fatalf("FindTypeDef(SimpleMath) error = %v", err)
	}
	if typeDef.Name != "SimpleMath" {
		t.Fatalf("typeDef.Name = %q, want SimpleMath", typeDef.Name)
	}

	methodRID, method, err := md.FindMethodDef(rid, "Add")
	if err != nil {
		t.Fatalf("FindMethodDef(Add) error = %v", err)
	}
	if method.RVA == 0 {
		t.Errorf("MethodDef(Add).RVA = 0, want > 0 (method has a body)")
	}
	if len(method.Signature) == 0 {
		t.Errorf("MethodDef(Add).Signature is empty, want a signature blob")
	}
	_ = methodRID
}

func TestParse_RealAssembly_Loops(t *testing.T) {
	md := parseFixture(t)

	rid, _, err := md.FindTypeDef("Vmnet.Fixtures", "Loops")
	if err != nil {
		t.Fatalf("FindTypeDef(Loops) error = %v", err)
	}
	_, method, err := md.FindMethodDef(rid, "Sum")
	if err != nil {
		t.Fatalf("FindMethodDef(Sum) error = %v", err)
	}
	if method.RVA == 0 {
		t.Errorf("MethodDef(Sum).RVA = 0, want > 0")
	}
}

func TestParse_InvalidMetadataRoot(t *testing.T) {
	_, err := Parse([]byte{0, 0, 0, 0})
	if err != ErrInvalidMetadataRoot {
		t.Fatalf("Parse(invalid) error = %v, want ErrInvalidMetadataRoot", err)
	}
}
