package nuget

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLockFile_RoundTrip(t *testing.T) {
	resolved := map[string]*ResolvedPackage{
		"b.dep": {ID: "B.Dep", Version: "2.0.0", SelectedAsset: "lib/netstandard2.0/B.Dep.dll", Dependencies: nil},
		"a": {
			ID: "A", Version: "1.0.0", SelectedAsset: "lib/netstandard2.0/A.dll",
			Dependencies: []string{"B.Dep"},
		},
	}
	lf := BuildLockFile("netstandard2.0", resolved)

	if lf.Version != 1 || lf.Target != "netstandard2.0" {
		t.Fatalf("BuildLockFile() = %+v", lf)
	}
	if len(lf.Packages) != 2 || lf.Packages[0].ID != "A" || lf.Packages[1].ID != "B.Dep" {
		t.Fatalf("Packages = %+v, want sorted [A, B.Dep]", lf.Packages)
	}

	path := filepath.Join(t.TempDir(), "vmnet.lock.json")
	if err := WriteLockFile(path, lf); err != nil {
		t.Fatalf("WriteLockFile() error = %v", err)
	}

	got, err := ReadLockFile(path)
	if err != nil {
		t.Fatalf("ReadLockFile() error = %v", err)
	}
	if len(got.Packages) != 2 || got.Packages[0].ID != "A" || got.Packages[0].Dependencies[0] != "B.Dep" {
		t.Errorf("round-tripped lockfile = %+v", got)
	}
}

func TestReadLockFile_Malformed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vmnet.lock.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadLockFile(path); err == nil {
		t.Fatal("ReadLockFile(malformed) error = nil, want an error")
	}
}

func TestReadLockFile_Missing(t *testing.T) {
	if _, err := ReadLockFile(filepath.Join(t.TempDir(), "does-not-exist.json")); err == nil {
		t.Fatal("ReadLockFile(missing) error = nil, want an error")
	}
}
