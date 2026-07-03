package nuget

import (
	"fmt"
	"strings"
)

// ResolvedPackage is one node in a resolved dependency graph.
type ResolvedPackage struct {
	ID            string
	Version       string
	SelectedAsset string // zip entry path, "" if nothing was selectable
	TFM           string
	Unselectable  string // non-empty: why no asset could be selected (native-only, no compatible TFM, ...)
	Dependencies  []string
}

// Resolver walks a package's dependency graph, downloading (via cache)
// each package it discovers and picking a TFM-compatible asset for it.
//
// Version conflicts resolve by highest-version-wins: if two paths in the
// graph request different versions of the same package, the higher one
// is kept and re-resolved. This is a deliberate simplification of NuGet's
// real version-range negotiation (spec §22.3 calls for "dependencias
// transitivas simples", not full range solving) — documented, not
// accidental.
type Resolver struct {
	Client *Client
	Cache  *Cache
	Opts   SelectOptions
}

func NewResolver(client *Client, cache *Cache, opts SelectOptions) *Resolver {
	return &Resolver{Client: client, Cache: cache, Opts: opts}
}

// Resolve returns every package reachable from direct, keyed by
// lowercased id.
func (r *Resolver) Resolve(direct []Dependency) (map[string]*ResolvedPackage, error) {
	resolved := map[string]*ResolvedPackage{}
	versions := map[string]string{}

	var visit func(id, version string, chain []string) error
	visit = func(id, version string, chain []string) error {
		version = ParseMinVersion(version)
		key := strings.ToLower(id)
		for _, c := range chain {
			if strings.EqualFold(c, id) {
				return fmt.Errorf("nuget: dependency cycle: %s -> %s", strings.Join(chain, " -> "), id)
			}
		}

		if existing, ok := versions[key]; ok && CompareVersions(existing, version) >= 0 {
			return nil // already resolved at an equal-or-newer version
		}
		versions[key] = version

		data, err := r.Cache.Fetch(r.Client, id, version)
		if err != nil {
			return fmt.Errorf("nuget: resolving %s@%s (via %s): %w", id, version, dependencyPath(chain, id), err)
		}
		pkg, err := OpenPackage(data)
		if err != nil {
			return fmt.Errorf("nuget: %s@%s: %w", id, version, err)
		}

		rp := &ResolvedPackage{ID: pkg.Spec.Metadata.ID, Version: pkg.Spec.Metadata.Version}
		asset, ok, reason := pkg.SelectLibAsset(r.Opts)
		depTFM := TFM{}
		if ok {
			rp.SelectedAsset = asset.Path
			rp.TFM = asset.TFM.Raw
			depTFM = asset.TFM
		} else {
			rp.Unselectable = reason
		}

		deps := pkg.Spec.DependenciesFor(depTFM)
		for _, d := range deps {
			rp.Dependencies = append(rp.Dependencies, d.ID)
		}
		resolved[key] = rp

		for _, d := range deps {
			if err := visit(d.ID, d.Version, append(append([]string{}, chain...), id)); err != nil {
				return err
			}
		}
		return nil
	}

	for _, d := range direct {
		if err := visit(d.ID, d.Version, nil); err != nil {
			return nil, err
		}
	}
	return resolved, nil
}

func dependencyPath(chain []string, id string) string {
	if len(chain) == 0 {
		return id
	}
	return strings.Join(chain, " -> ") + " -> " + id
}
