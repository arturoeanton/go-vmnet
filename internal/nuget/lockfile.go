package nuget

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// LockFile is vmnet's own resolved-dependency-graph format (spec §22.6) —
// not compatible with NuGet's real packages.lock.json, deliberately
// simpler, since vmnet's resolver itself is simpler (see resolver.go).
type LockFile struct {
	Version  int             `json:"version"`
	Target   string          `json:"target"`
	Packages []LockedPackage `json:"packages"`
}

type LockedPackage struct {
	ID            string   `json:"id"`
	Version       string   `json:"version"`
	SelectedAsset string   `json:"selectedAsset"`
	Unselectable  string   `json:"unselectable,omitempty"`
	Dependencies  []string `json:"dependencies"`
}

// BuildLockFile turns a resolver's output into a deterministic (sorted by
// id) LockFile.
func BuildLockFile(target string, resolved map[string]*ResolvedPackage) *LockFile {
	lf := &LockFile{Version: 1, Target: target}
	keys := make([]string, 0, len(resolved))
	for k := range resolved {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		rp := resolved[k]
		deps := append([]string{}, rp.Dependencies...)
		sort.Strings(deps)
		lf.Packages = append(lf.Packages, LockedPackage{
			ID:            rp.ID,
			Version:       rp.Version,
			SelectedAsset: rp.SelectedAsset,
			Unselectable:  rp.Unselectable,
			Dependencies:  deps,
		})
	}
	return lf
}

func WriteLockFile(path string, lf *LockFile) error {
	data, err := json.MarshalIndent(lf, "", "  ")
	if err != nil {
		return fmt.Errorf("nuget: encoding lockfile: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("nuget: writing lockfile: %w", err)
	}
	return nil
}

func ReadLockFile(path string) (*LockFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lf LockFile
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("nuget: parsing lockfile: %w", err)
	}
	return &lf, nil
}
