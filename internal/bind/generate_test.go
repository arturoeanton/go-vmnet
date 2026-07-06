package bind

import (
	"go/format"
	"os"
	"strings"
	"testing"

	"github.com/arturoeanton/go-vmnet/internal/metadata"
	"github.com/arturoeanton/go-vmnet/internal/pe"
)

const fixtureRelPath = "../../tests/fixtures/csharp/bin/Release/netstandard2.0/Vmnet.Fixtures.dll"

func parseFixture(t *testing.T) *metadata.Metadata {
	t.Helper()
	data, err := os.ReadFile(fixtureRelPath)
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
	return md
}

// TestBuildModel_RealFixture proves BuildModel finds real, known public
// types in the shared fixture assembly (SimpleMath is about as simple a
// real target as exists: one public static class, one public static
// method, Add(int,int) int).
func TestBuildModel_RealFixture(t *testing.T) {
	md := parseFixture(t)
	m, err := BuildModel(md, "fixtures", "Vmnet.Fixtures.dll")
	if err != nil {
		t.Fatalf("BuildModel() error = %v", err)
	}
	if len(m.Types) == 0 {
		t.Fatal("BuildModel() found zero public types in a fixture assembly with many")
	}

	var simpleMath *BoundType
	for _, bt := range m.Types {
		if bt.FullName == "Vmnet.Fixtures.SimpleMath" {
			simpleMath = bt
		}
	}
	if simpleMath == nil {
		t.Fatal("BuildModel() didn't find Vmnet.Fixtures.SimpleMath")
	}
	var add *BoundMethod
	for _, bm := range simpleMath.Static {
		if bm.Name == "Add" {
			add = bm
		}
	}
	if add == nil {
		t.Fatal("BuildModel() didn't find SimpleMath.Add as a static method")
	}
	if add.Overloaded {
		t.Error("SimpleMath.Add has exactly one real overload, want Overloaded=false")
	}
	if len(add.Params) != 2 || add.Params[0].Type.Kind != "int32" || add.Params[1].Type.Kind != "int32" {
		t.Errorf("SimpleMath.Add params = %+v, want two int32 params", add.Params)
	}
	if add.Return == nil || add.Return.Kind != "int32" {
		t.Errorf("SimpleMath.Add return = %+v, want int32", add.Return)
	}
}

// TestBuildModel_PropertyAccessorNaming proves a real C# property's own
// get_X/set_X compiler-generated accessor names ("get_Name") become
// idiomatic GetX/SetX Go names ("GetName"), not a literal,
// underscore-preserving sanitizeIdent("get_Name") ("Get_Name").
func TestBuildModel_PropertyAccessorNaming(t *testing.T) {
	md := parseFixture(t)
	m, err := BuildModel(md, "fixtures", "Vmnet.Fixtures.dll")
	if err != nil {
		t.Fatalf("BuildModel() error = %v", err)
	}
	var customer *BoundType
	for _, bt := range m.Types {
		if bt.FullName == "Vmnet.Fixtures.Customer" {
			customer = bt
		}
	}
	if customer == nil {
		t.Fatal("BuildModel() didn't find Vmnet.Fixtures.Customer")
	}
	names := map[string]bool{}
	for _, bm := range customer.Instance {
		names[bm.GoName] = true
	}
	for _, want := range []string{"GetName", "SetName"} {
		if !names[want] {
			t.Errorf("Customer instance methods = %v, want %q among them", names, want)
		}
	}
	for _, unwanted := range []string{"Get_Name", "Set_Name"} {
		if names[unwanted] {
			t.Errorf("Customer instance methods contains %q, want the get_/set_ prefix collapsed instead", unwanted)
		}
	}
}

// TestGenerate_RealFixtureCompiles proves the generated Go source for
// the real, full fixture assembly is genuinely valid Go — go/format
// itself is the check inside Generate, but this test additionally
// confirms the output contains real, expected content (not just that
// SOME valid-but-empty Go file was produced).
func TestGenerate_RealFixtureCompiles(t *testing.T) {
	md := parseFixture(t)
	m, err := BuildModel(md, "fixtures", "Vmnet.Fixtures@test")
	if err != nil {
		t.Fatalf("BuildModel() error = %v", err)
	}
	src, err := m.Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if _, err := format.Source([]byte(src)); err != nil {
		t.Fatalf("Generate() produced invalid Go: %v", err)
	}
	if !strings.Contains(src, "package fixtures") {
		t.Error("Generate() output missing the expected package clause")
	}
	if !strings.Contains(src, "func NewSimpleMath") && !strings.Contains(src, "SimpleMathStatic_Add") {
		t.Error("Generate() output missing SimpleMath's own generated entry points")
	}
	if !strings.Contains(src, "DO NOT EDIT") {
		t.Error("Generate() output missing the generated-file header")
	}
}

// TestGenerate_EmptyModel proves a model with no bound types still
// produces valid (if minimal) Go — never a broken/partial file.
func TestGenerate_EmptyModel(t *testing.T) {
	m := &Model{GoPackage: "empty", SourceName: "Empty.dll", boundByFull: map[string]*BoundType{}}
	src, err := m.Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if _, err := format.Source([]byte(src)); err != nil {
		t.Fatalf("Generate() produced invalid Go for an empty model: %v", err)
	}
}
