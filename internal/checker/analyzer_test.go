package checker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/arturoeanton/go-vmnet/internal/metadata"
	"github.com/arturoeanton/go-vmnet/internal/pe"
)

const fixtureRelPath = "../../tests/fixtures/csharp/bin/Release/netstandard2.0/Vmnet.Fixtures.dll"

func analyzeFixture(t *testing.T, profile Profile) *Report {
	t.Helper()
	data, err := os.ReadFile(filepath.FromSlash(fixtureRelPath))
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
	return Analyze(f, md, profile)
}

// TestAnalyze_OwnAssemblyIsCompatible is the checker's own dogfood test:
// vmnet's thoroughly-exercised fixture assembly (Fase 1-2) must self-
// certify clean under the profiles it was built against, with the single
// expected exception being Unsupported.cs (which exists specifically to
// test the checker itself — see TestAnalyze_UnsupportedOpcodeIsReported).
// Any OTHER finding here means the checker or the interpreter has drifted
// from what it actually supports.
func TestAnalyze_OwnAssemblyIsCompatible(t *testing.T) {
	for _, profile := range []Profile{ProfileRules, ProfileNetStandardLite} {
		t.Run(string(profile), func(t *testing.T) {
			r := analyzeFixture(t, profile)
			if r.MethodsAnalyzed == 0 {
				t.Fatal("MethodsAnalyzed = 0, want > 0 (did the fixture assembly fail to load?)")
			}
			for _, f := range r.Findings {
				if f.Method != "Vmnet.Fixtures.Unsupported::FilterClause" {
					t.Errorf("unexpected finding outside Unsupported.cs: %+v", f)
				}
			}
		})
	}
}

// TestAnalyze_MinimalProfileFlagsObjectModel proves the `minimal` profile
// (spec §24.1: static methods and primitives only) rejects the same
// assembly's object-model methods (Customer, CollectionsTest, arrays,
// static fields, ...) even though the runtime can execute them under
// `rules`. Arrays and static fields were added in Fase 3.5 alongside
// `ref`/`out` primitive parameters — this also locks in that the latter
// stay allowed under `minimal` (they never touch the heap or a type's
// field layout), so a future change can't silently regress either side.
func TestAnalyze_MinimalProfileFlagsObjectModel(t *testing.T) {
	r := analyzeFixture(t, ProfileMinimal)
	if r.Status == StatusCompatible {
		t.Fatal("Status = compatible under minimal, want partial/unsupported (Customer/CollectionsTest use the object model)")
	}

	wantOutOfProfile := map[string]bool{
		"Vmnet.Fixtures.Customer::get_Name":       false,
		"Vmnet.Fixtures.Arrays::SumArray":         false,
		"Vmnet.Fixtures.Statics::GetInitValue":    false,
		"Vmnet.Fixtures.Statics::IncrementAndGet": false,
	}
	for _, f := range r.Findings {
		if f.Kind == KindOutOfProfile {
			if _, ok := wantOutOfProfile[f.Method]; ok {
				wantOutOfProfile[f.Method] = true
			}
		}
		if f.Method == "Vmnet.Fixtures.ByRef::CallIncrementTwice" {
			t.Errorf("ByRef::CallIncrementTwice (ref/out primitives only) unexpectedly flagged under minimal: %+v", f)
		}
	}
	for method, found := range wantOutOfProfile {
		if !found {
			t.Errorf("expected an out-of-profile finding for %s, got: %+v", method, r.Findings)
		}
	}
}

// TestAnalyze_UnsupportedOpcodeIsReported proves a method using an
// exception filter clause (`catch (Foo) when (cond)` — the one exception-
// handling shape Fase 3.10 doesn't lower, see ir/builder.go's
// buildHandlers) shows up as a concrete, located finding, not a silent
// skip or a crash.
func TestAnalyze_UnsupportedOpcodeIsReported(t *testing.T) {
	r := analyzeFixture(t, ProfileNetStandardLite)

	var found *Finding
	for i := range r.Findings {
		if r.Findings[i].Method == "Vmnet.Fixtures.Unsupported::FilterClause" {
			found = &r.Findings[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected a finding for Unsupported::FilterClause, got: %+v", r.Findings)
	}
	if found.Kind != KindUnsupportedOpcode {
		t.Errorf("finding.Kind = %s, want %s", found.Kind, KindUnsupportedOpcode)
	}
	if r.Status == StatusCompatible {
		t.Error("Status = compatible, want partial (Unsupported.FilterClause should have blocked it)")
	}
}

// TestAnalyze_AllMethodsFailingIsUnsupported proves the compatible/
// partial/unsupported boundary: an assembly where every single analyzed
// method is flagged must report "unsupported", not "partial".
func TestAnalyze_AllMethodsFailingIsUnsupported(t *testing.T) {
	r := &Report{MethodsAnalyzed: 3, MethodsFlagged: 3, Findings: []Finding{{Kind: KindUnsupportedOpcode}}}
	r.finalize()
	if r.Status != StatusUnsupported {
		t.Errorf("Status = %s, want %s", r.Status, StatusUnsupported)
	}
}

func TestAnalyze_NoMethodsAnalyzedButHasFindingsIsUnsupported(t *testing.T) {
	// e.g. a P/Invoke-only assembly: nothing has a body to analyze, but
	// the assembly-wide P/Invoke finding still means "can't run this".
	r := &Report{MethodsAnalyzed: 0, Findings: []Finding{{Kind: KindPInvoke}}}
	r.finalize()
	if r.Status != StatusUnsupported {
		t.Errorf("Status = %s, want %s", r.Status, StatusUnsupported)
	}
}

func TestAnalyze_SomeMethodsFailingIsPartial(t *testing.T) {
	r := &Report{MethodsAnalyzed: 10, MethodsFlagged: 3, Findings: []Finding{{Kind: KindUnsupportedOpcode}}}
	r.finalize()
	if r.Status != StatusPartial {
		t.Errorf("Status = %s, want %s", r.Status, StatusPartial)
	}
}
