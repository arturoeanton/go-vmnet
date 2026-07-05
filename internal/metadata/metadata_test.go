package metadata

import (
	"fmt"
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

// TestFindMethodDefCandidates_Cached is the Fase 3.73 regression test:
// repeat calls for the same (typeRID, name) must return consistent
// results (a cache bug returning stale/wrong data would be far worse
// than no cache at all), and a genuine miss must keep failing on every
// repeat call too, not get incorrectly cached as a false hit.
func TestFindMethodDefCandidates_Cached(t *testing.T) {
	md := parseFixture(t)

	rid, _, err := md.FindTypeDef("Vmnet.Fixtures", "SimpleMath")
	if err != nil {
		t.Fatalf("FindTypeDef(SimpleMath) error = %v", err)
	}

	for i := 0; i < 3; i++ {
		rids, rows, err := md.FindMethodDefCandidates(rid, "Add")
		if err != nil {
			t.Fatalf("FindMethodDefCandidates(Add) call %d error = %v", i, err)
		}
		if len(rids) != 1 || len(rows) != 1 {
			t.Fatalf("FindMethodDefCandidates(Add) call %d = %d candidates, want 1", i, len(rids))
		}
		if rows[0].Name != "Add" {
			t.Errorf("FindMethodDefCandidates(Add) call %d: rows[0].Name = %q, want Add", i, rows[0].Name)
		}
	}

	for i := 0; i < 3; i++ {
		if _, _, err := md.FindMethodDefCandidates(rid, "DoesNotExist"); err == nil {
			t.Fatalf("FindMethodDefCandidates(DoesNotExist) call %d: error = nil, want a not-found error", i)
		}
	}
}

// TestFindMethodDefCandidates_ConcurrentAccess races many goroutines
// resolving the same and different (typeRID, name) pairs — run with
// -race, this must never report a data race, and every goroutine must
// still get a correct result.
func TestFindMethodDefCandidates_ConcurrentAccess(t *testing.T) {
	md := parseFixture(t)

	rid, _, err := md.FindTypeDef("Vmnet.Fixtures", "SimpleMath")
	if err != nil {
		t.Fatalf("FindTypeDef(SimpleMath) error = %v", err)
	}

	const goroutines = 32
	errCh := make(chan error, goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			for i := 0; i < 50; i++ {
				rids, rows, err := md.FindMethodDefCandidates(rid, "Add")
				if err != nil {
					errCh <- err
					return
				}
				if len(rids) != 1 || rows[0].Name != "Add" {
					errCh <- fmt.Errorf("FindMethodDefCandidates(Add) = %+v/%+v, want 1 candidate named Add", rids, rows)
					return
				}
				if _, err := md.ParseMethodSigCached(rows[0].Signature); err != nil {
					errCh <- err
					return
				}
			}
			errCh <- nil
		}()
	}
	for g := 0; g < goroutines; g++ {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}
}

// TestParseMethodSigCached_MatchesUncached proves the cached path
// (Fase 3.73) parses identically to the uncached ParseMethodSig it
// wraps — a cache is only a win if it never changes the answer.
func TestParseMethodSigCached_MatchesUncached(t *testing.T) {
	md := parseFixture(t)

	rid, _, err := md.FindTypeDef("Vmnet.Fixtures", "SimpleMath")
	if err != nil {
		t.Fatalf("FindTypeDef(SimpleMath) error = %v", err)
	}
	_, method, err := md.FindMethodDef(rid, "Add")
	if err != nil {
		t.Fatalf("FindMethodDef(Add) error = %v", err)
	}

	want, err := ParseMethodSig(method.Signature)
	if err != nil {
		t.Fatalf("ParseMethodSig error = %v", err)
	}
	for i := 0; i < 3; i++ {
		got, err := md.ParseMethodSigCached(method.Signature)
		if err != nil {
			t.Fatalf("ParseMethodSigCached call %d error = %v", i, err)
		}
		if got.HasThis != want.HasThis || len(got.Params) != len(want.Params) {
			t.Errorf("ParseMethodSigCached call %d = %+v, want %+v", i, got, want)
		}
	}
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

// TestParse_RealAssembly_TypeRef is the spec §28.2 "TypeRef parsing"
// golden test — every real assembly that touches the BCL at all (which
// this fixture does constantly) references System.Object as a TypeRef,
// but no test asserted TypeRef(rid) directly before Fase 3.69 (only
// exercised incidentally through end-to-end BCL calls).
func TestParse_RealAssembly_TypeRef(t *testing.T) {
	md := parseFixture(t)

	count := md.RowCount(TableTypeRef)
	if count == 0 {
		t.Fatal("RowCount(TypeRef) = 0, want > 0")
	}
	var foundObject bool
	for rid := uint32(1); rid <= count; rid++ {
		row, err := md.TypeRef(rid)
		if err != nil {
			t.Fatalf("TypeRef(%d) error = %v", rid, err)
		}
		if row.Namespace == "System" && row.Name == "Object" {
			foundObject = true
		}
	}
	if !foundObject {
		t.Error("expected a TypeRef for System.Object, not found")
	}
}

// TestParse_RealAssembly_MemberRef is the spec §28.2 "MemberRef parsing"
// golden test — SimpleMath.Add's own signature is entirely self-
// contained, but Strings.Hello calls the real System.String::Concat via
// a MemberRef; no test asserted MemberRef(rid) directly before Fase
// 3.69.
func TestParse_RealAssembly_MemberRef(t *testing.T) {
	md := parseFixture(t)

	count := md.RowCount(TableMemberRef)
	if count == 0 {
		t.Fatal("RowCount(MemberRef) = 0, want > 0")
	}
	var found *MemberRefRow
	for rid := uint32(1); rid <= count; rid++ {
		row, err := md.MemberRef(rid)
		if err != nil {
			t.Fatalf("MemberRef(%d) error = %v", rid, err)
		}
		if row.Name == "Concat" {
			found = &row
			break
		}
	}
	if found == nil {
		t.Fatal("expected a MemberRef named Concat (String.Concat), not found")
	}
	if len(found.Signature) == 0 {
		t.Error("MemberRef(Concat).Signature is empty, want a signature blob")
	}
}

// TestParse_RealAssembly_GenericSignature is the spec §28.2 "generic
// signatures" golden test: a real TypeSpec's own GENERICINST blob
// (ECMA-335 §II.23.2.15) — the shape List<int>/Dictionary<K,V> compile
// to wherever a closed generic instantiation is named directly (e.g. a
// `newobj`/`call` operand's Class token) — decodes into SigGenericInst
// with the right open type and argument count.
func TestParse_RealAssembly_GenericSignature(t *testing.T) {
	md := parseFixture(t)

	count := md.RowCount(TableTypeSpec)
	if count == 0 {
		t.Skip("fixture assembly has no TypeSpec rows to test against")
	}
	var foundGenericInst bool
	for rid := uint32(1); rid <= count; rid++ {
		blob, err := md.TypeSpecSignature(rid)
		if err != nil {
			t.Fatalf("TypeSpecSignature(%d) error = %v", rid, err)
		}
		sig, err := ParseTypeSpec(blob)
		if err != nil {
			// Not every TypeSpec is a GENERICINST (e.g. SZARRAY) — only
			// fail on a genuine parse error, which ParseTypeSpec doesn't
			// return for a recognized non-generic shape.
			t.Fatalf("ParseTypeSpec(%d) error = %v", rid, err)
		}
		if sig.Kind == SigGenericInst {
			foundGenericInst = true
			if len(sig.Args) == 0 {
				t.Errorf("TypeSpec(%d): SigGenericInst with 0 Args, want >= 1", rid)
			}
		}
	}
	if !foundGenericInst {
		t.Error("expected at least one TypeSpec row to decode as SigGenericInst (e.g. List<int>), found none")
	}
}

func TestParse_InvalidMetadataRoot(t *testing.T) {
	_, err := Parse([]byte{0, 0, 0, 0})
	if err != ErrInvalidMetadataRoot {
		t.Fatalf("Parse(invalid) error = %v, want ErrInvalidMetadataRoot", err)
	}
}
