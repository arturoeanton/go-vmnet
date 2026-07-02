package nuget

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Cache is a local on-disk store of downloaded .nupkg files, keyed by
// id@version, so `vmnet restore` doesn't re-download on every run (spec
// §22.3 "cache local").
type Cache struct {
	Dir string
}

func NewCache(dir string) *Cache {
	return &Cache{Dir: dir}
}

func (c *Cache) path(id, version string) string {
	idl, vl := strings.ToLower(id), strings.ToLower(version)
	return filepath.Join(c.Dir, idl, vl, idl+"."+vl+".nupkg")
}

// Has reports whether id@version is already cached.
func (c *Cache) Has(id, version string) bool {
	_, err := os.Stat(c.path(id, version))
	return err == nil
}

// Load reads a cached .nupkg's bytes.
func (c *Cache) Load(id, version string) ([]byte, error) {
	return os.ReadFile(c.path(id, version))
}

// Store saves data as id@version's cached .nupkg, writing to a temp file
// first so a crash mid-write can never leave a corrupt cache entry that
// looks valid.
func (c *Cache) Store(id, version string, data []byte) error {
	p := c.path(id, version)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("nuget: creating cache directory: %w", err)
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("nuget: writing cache file: %w", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		return fmt.Errorf("nuget: finalizing cache file: %w", err)
	}
	return nil
}

// Fetch returns id@version's .nupkg bytes, preferring the cache and
// falling back to client.Download. A cache-write failure (e.g. a full
// disk) doesn't fail the fetch — the caller already has the bytes it
// asked for, caching is a best-effort optimization, not a correctness
// requirement.
func (c *Cache) Fetch(client *Client, id, version string) ([]byte, error) {
	if data, err := c.Load(id, version); err == nil {
		return data, nil
	}
	data, err := client.Download(id, version)
	if err != nil {
		return nil, err
	}
	_ = c.Store(id, version, data)
	return data, nil
}
