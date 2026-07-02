package nuget

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Manifest is the small "what direct packages does this project want"
// file `vmnet add` writes and `vmnet restore` reads — the vmnet
// equivalent of a .csproj's <PackageReference> list, since vmnet has no
// project file of its own.
type Manifest struct {
	Packages []Dependency `json:"packages"`
}

// ReadManifest loads path, returning an empty Manifest (not an error) if
// it doesn't exist yet — `vmnet add` creates it on first use.
func ReadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Manifest{}, nil
	}
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("nuget: parsing %s: %w", path, err)
	}
	return &m, nil
}

func WriteManifest(path string, m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("nuget: encoding %s: %w", path, err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("nuget: writing %s: %w", path, err)
	}
	return nil
}

// Add records id@version as a direct dependency, replacing any existing
// entry for the same id (case-insensitive, matching NuGet package IDs).
func (m *Manifest) Add(id, version string) {
	for i, p := range m.Packages {
		if strings.EqualFold(p.ID, id) {
			m.Packages[i].Version = version
			return
		}
	}
	m.Packages = append(m.Packages, Dependency{ID: id, Version: version})
}
