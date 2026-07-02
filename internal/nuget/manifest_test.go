package nuget

import (
	"path/filepath"
	"testing"
)

func TestManifest_ReadMissingIsEmpty(t *testing.T) {
	m, err := ReadManifest(filepath.Join(t.TempDir(), "vmnet.json"))
	if err != nil {
		t.Fatalf("ReadManifest(missing) error = %v, want nil (an empty manifest)", err)
	}
	if len(m.Packages) != 0 {
		t.Errorf("Packages = %+v, want empty", m.Packages)
	}
}

func TestManifest_AddIsIdempotentAndCaseInsensitive(t *testing.T) {
	m := &Manifest{}
	m.Add("NodaTime", "3.1.0")
	m.Add("Newtonsoft.Json", "13.0.3")
	m.Add("nodatime", "3.2.0") // re-add, different case, newer version

	if len(m.Packages) != 2 {
		t.Fatalf("Packages = %+v, want 2 entries", m.Packages)
	}
	for _, p := range m.Packages {
		if p.ID == "NodaTime" && p.Version != "3.2.0" {
			t.Errorf("NodaTime version = %q, want updated to 3.2.0", p.Version)
		}
	}
}

func TestManifest_WriteReadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vmnet.json")
	m := &Manifest{}
	m.Add("A", "1.0.0")
	if err := WriteManifest(path, m); err != nil {
		t.Fatalf("WriteManifest() error = %v", err)
	}

	got, err := ReadManifest(path)
	if err != nil {
		t.Fatalf("ReadManifest() error = %v", err)
	}
	if len(got.Packages) != 1 || got.Packages[0].ID != "A" || got.Packages[0].Version != "1.0.0" {
		t.Errorf("round-tripped manifest = %+v", got.Packages)
	}
}
