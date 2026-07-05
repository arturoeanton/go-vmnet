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
// Repurposed to a `calli` (indirect call through a C# 9+ function
// pointer) once exception filter clauses — its previous "still
// unsupported" example — gained real support in Fase 3.51.
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
				if f.Method != "Vmnet.Fixtures.Unsupported::FunctionPointerCall" {
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
// indirect call through a function pointer (`delegate*<...>`, compiling
// to a real `calli` — internal/ir/builder.go's opcode switch has no case
// for it at all, the same "no raw function-pointer indirection" boundary
// Reflection.Emit/P-Invoke already sit outside of) shows up as a
// concrete, located finding, not a silent skip or a crash.
func TestAnalyze_UnsupportedOpcodeIsReported(t *testing.T) {
	r := analyzeFixture(t, ProfileNetStandardLite)

	var found *Finding
	for i := range r.Findings {
		if r.Findings[i].Method == "Vmnet.Fixtures.Unsupported::FunctionPointerCall" {
			found = &r.Findings[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected a finding for Unsupported::FunctionPointerCall, got: %+v", r.Findings)
	}
	if found.Kind != KindUnsupportedOpcode {
		t.Errorf("finding.Kind = %s, want %s", found.Kind, KindUnsupportedOpcode)
	}
	if r.Status == StatusCompatible {
		t.Error("Status = compatible, want partial (Unsupported.FunctionPointerCall should have blocked it)")
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

// TestCategorize is the spec §28.6 "reflection detection"/"async
// detection" golden test: categorize (analyzer.go) turns an unresolved
// call target's full name into the right human-meaningful reason bucket,
// mirroring spec §23.3's grouped output.
func TestCategorize(t *testing.T) {
	tests := []struct {
		name string
		want FindingKind
	}{
		{"System.Reflection.Emit.DynamicMethod::CreateDelegate", KindReflection},
		{"System.Reflection.Assembly::GetExecutingAssembly", KindReflection},
		{"System.Threading.Tasks.TaskCompletionSource`1::SetResult", KindAsync},
		{"System.Threading.Tasks.Parallel::For", KindAsync},
		{"Some.Totally.Unknown::Method", KindUnsupportedMethod},
		{"System.IO.Ports.SerialPort::Open", KindUnsupportedMethod},
	}
	for _, tt := range tests {
		if got := categorize(tt.name); got != tt.want {
			t.Errorf("categorize(%q) = %s, want %s", tt.name, got, tt.want)
		}
	}
}

// TestAnalyze_PInvokeIsReported is the spec §28.6 "P/Invoke detection"
// golden test: a real [DllImport] extern method compiles to a real
// ImplMap table row — the checker must flag the whole assembly as
// unsupported (P/Invoke has no pure-Go equivalent), not silently accept
// or crash on it. Deliberately a separate fixture assembly (tests/
// fixtures/csharp-pinvoke) — see that fixture's own doc comment for why.
func TestAnalyze_PInvokeIsReported(t *testing.T) {
	const pinvokeFixtureRelPath = "../../tests/fixtures/csharp-pinvoke/bin/Release/netstandard2.0/Vmnet.Fixtures.PInvoke.dll"
	data, err := os.ReadFile(filepath.FromSlash(pinvokeFixtureRelPath))
	if err != nil {
		t.Skipf("fixture assembly not built: %v (run `dotnet build tests/fixtures/csharp-pinvoke/PInvokeFixture.csproj -c Release`)", err)
	}
	f, err := pe.Parse(data)
	if err != nil {
		t.Fatalf("pe.Parse() error = %v", err)
	}
	md, err := metadata.Parse(f.Metadata)
	if err != nil {
		t.Fatalf("metadata.Parse() error = %v", err)
	}
	r := Analyze(f, md, ProfileNetStandardLite)

	if r.Status != StatusUnsupported {
		t.Errorf("Status = %s, want %s", r.Status, StatusUnsupported)
	}
	var found bool
	for _, finding := range r.Findings {
		if finding.Kind == KindPInvoke {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a KindPInvoke finding, got: %+v", r.Findings)
	}
}
