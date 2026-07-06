package migrate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/arturoeanton/go-vmnet/internal/checker"
)

const (
	fixtureDLL        = "../../tests/fixtures/csharp/bin/Release/netstandard2.0/Vmnet.Fixtures.dll"
	pinvokeFixtureDLL = "../../tests/fixtures/csharp-pinvoke/bin/Release/netstandard2.0/Vmnet.Fixtures.PInvoke.dll"
)

// copyFixturesInto builds a real temp directory containing both real
// fixture assemblies (skipping the test if either hasn't been built —
// same convention every other fixture-dependent test in this repo
// uses) plus one bogus, non-DLL file, so AnalyzeDirectory's own
// skip-and-keep-going behavior gets exercised against something real.
func copyFixturesInto(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, src := range []string{fixtureDLL, pinvokeFixtureDLL} {
		data, err := os.ReadFile(filepath.FromSlash(src))
		if err != nil {
			t.Skipf("fixture assembly not built: %v (run the two `dotnet build` commands in tests/fixtures/csharp*/README.md)", err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(src)), data, 0o644); err != nil {
			t.Fatalf("writing fixture copy: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "not-a-dll.dll"), []byte("this is not a real PE file"), 0o644); err != nil {
		t.Fatalf("writing bogus file: %v", err)
	}
	return dir
}

func TestAnalyzeDirectory_RealFixtures(t *testing.T) {
	dir := copyFixturesInto(t)

	summary, err := AnalyzeDirectory(dir, checker.ProfileNetStandardLite)
	if err != nil {
		t.Fatalf("AnalyzeDirectory() error = %v", err)
	}

	if summary.TotalAssemblies != 2 {
		t.Errorf("TotalAssemblies = %d, want 2 (the bogus file must be skipped, not analyzed)", summary.TotalAssemblies)
	}
	if len(summary.Skipped) != 1 {
		t.Fatalf("len(Skipped) = %d, want 1 (the bogus not-a-dll.dll)", len(summary.Skipped))
	}
	if summary.Skipped[0].Reason == "" {
		t.Error("Skipped[0].Reason is empty, want a real explanation")
	}

	if summary.TotalMethods == 0 {
		t.Error("TotalMethods = 0, want > 0 (both fixtures have real method bodies)")
	}
	if summary.TotalRunnable != summary.TotalMethods-summary.TotalFlagged {
		t.Errorf("TotalRunnable = %d, want TotalMethods-TotalFlagged = %d", summary.TotalRunnable, summary.TotalMethods-summary.TotalFlagged)
	}

	// The P/Invoke fixture must show up with its own real, assembly-wide
	// KindPInvoke finding (internal/checker's own ImplMap-table check) —
	// proving this directory scan reaches AnalyzeWithDeps the same way
	// `vmnet check package` does, not some simplified stand-in.
	var sawPInvoke bool
	for _, nr := range summary.Reports {
		for _, f := range nr.Report.Findings {
			if f.Kind == checker.KindPInvoke {
				sawPInvoke = true
			}
		}
	}
	if !sawPInvoke {
		t.Error("expected a KindPInvoke finding from the P/Invoke fixture assembly, found none")
	}

	// Candidates must be real, non-trivial (>= minCandidateMethods) types
	// from the actual fixture, sorted best-first.
	if len(summary.Candidates) == 0 {
		t.Fatal("Candidates is empty, want at least one real type with >= 3 analyzed methods")
	}
	for i := 1; i < len(summary.Candidates); i++ {
		prev, cur := summary.Candidates[i-1], summary.Candidates[i]
		prevRatio := float64(prev.MethodsAnalyzed-prev.MethodsFlagged) / float64(prev.MethodsAnalyzed)
		curRatio := float64(cur.MethodsAnalyzed-cur.MethodsFlagged) / float64(cur.MethodsAnalyzed)
		if curRatio > prevRatio {
			t.Errorf("Candidates not sorted best-first at index %d: %v (%.2f) before %v (%.2f)", i, prev.Type, prevRatio, cur.Type, curRatio)
		}
	}
	for _, c := range summary.Candidates {
		if c.MethodsAnalyzed < minCandidateMethods {
			t.Errorf("Candidate %s has only %d analyzed methods, want >= %d", c.Type, c.MethodsAnalyzed, minCandidateMethods)
		}
	}
}

func TestAnalyzeDirectory_NoDLLs(t *testing.T) {
	dir := t.TempDir()
	if _, err := AnalyzeDirectory(dir, checker.ProfileNetStandardLite); err == nil {
		t.Fatal("AnalyzeDirectory() on an empty directory: error = nil, want a real error")
	}
}

func TestNamespaceBucket(t *testing.T) {
	tests := []struct {
		target string
		want   string
	}{
		{"System.Data.SqlClient.SqlConnection::Open", "System.Data"},
		{"System.Reflection.Emit.DynamicMethod::CreateDelegate", "System.Reflection"},
		{"System.Threading.Tasks.Task::Run", "System.Threading"},
		{"Newtonsoft.Json.Linq.JObject::Parse", "Newtonsoft.Json"},
		{"NoNamespaceType::Method", "NoNamespaceType"},
	}
	for _, tt := range tests {
		if got := namespaceBucket(tt.target); got != tt.want {
			t.Errorf("namespaceBucket(%q) = %q, want %q", tt.target, got, tt.want)
		}
	}
}

func TestBlockedByCategory(t *testing.T) {
	r := &checker.Report{Findings: []checker.Finding{
		{Kind: checker.KindUnsupportedMethod, Detail: "System.Data.SqlClient.SqlConnection::Open"},
		{Kind: checker.KindUnsupportedMethod, Detail: "System.Data.SqlClient.SqlCommand::ExecuteReader"},
		{Kind: checker.KindReflection, Detail: "System.Reflection.Emit.DynamicMethod::CreateDelegate"},
		{Kind: checker.KindPInvoke, Detail: "assembly declares P/Invoke method(s)"},
	}}
	summary := &Summary{Reports: []checker.NamedReport{{Name: "A.dll", Report: r}}}

	categories := BlockedByCategory(summary)
	if len(categories) != 3 {
		t.Fatalf("BlockedByCategory() = %d categories, want 3 (System.Data x2, Reflection x1, P/Invoke x1)", len(categories))
	}
	// Highest count first: "System.Data" (2) must lead.
	if categories[0].Label != "System.Data" || categories[0].Count != 2 {
		t.Errorf("categories[0] = %+v, want {System.Data 2}", categories[0])
	}
}
