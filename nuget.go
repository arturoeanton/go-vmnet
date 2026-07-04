package vmnet

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/nuget"
)

// Default on-disk locations for the NuGet manifest, lockfile and package
// cache — the same paths the `vmnet` CLI's add/restore/packages commands
// use, so a Go program and the CLI can interoperate on one project.
const (
	NuGetManifestFile = "vmnet.json"
	NuGetLockFile     = "vmnet.lock.json"
	NuGetCacheDir     = ".vmnet/packages"
)

// NuGetManager is the Go equivalent of `vmnet add`/`restore`/`packages`
// (spec §6.4, §22.7).
type NuGetManager struct {
	client   *nuget.Client
	cache    *nuget.Cache
	manifest string
	lockfile string
}

// NuGet returns vm's package manager, reading/writing the manifest,
// lockfile and cache at their default locations relative to the current
// directory.
func (vm *VM) NuGet() *NuGetManager {
	return &NuGetManager{
		client:   nuget.NewClient(),
		cache:    nuget.NewCache(NuGetCacheDir),
		manifest: NuGetManifestFile,
		lockfile: NuGetLockFile,
	}
}

// Add records id as a direct dependency. If version is empty, the latest
// published version is looked up and pinned. Add does not download or
// resolve anything by itself — call Restore afterward (spec §22.7 keeps
// these as two separate steps).
func (n *NuGetManager) Add(id, version string) error {
	if version == "" {
		v, err := n.client.LatestVersion(id)
		if err != nil {
			return fmt.Errorf("vmnet: %w", err)
		}
		version = v
	}
	m, err := nuget.ReadManifest(n.manifest)
	if err != nil {
		return fmt.Errorf("vmnet: %w", err)
	}
	m.Add(id, version)
	if err := nuget.WriteManifest(n.manifest, m); err != nil {
		return fmt.Errorf("vmnet: %w", err)
	}
	return nil
}

// Restore resolves every direct dependency in the manifest (transitively,
// highest-version-wins on conflicts — see internal/nuget/resolver.go),
// downloading each package into the local cache, and writes the lockfile.
func (n *NuGetManager) Restore() error {
	m, err := nuget.ReadManifest(n.manifest)
	if err != nil {
		return fmt.Errorf("vmnet: %w", err)
	}
	resolver := nuget.NewResolver(n.client, n.cache, nuget.SelectOptions{})
	resolved, err := resolver.Resolve(m.Packages)
	if err != nil {
		return fmt.Errorf("vmnet: %w", err)
	}
	lf := nuget.BuildLockFile("netstandard2.0", resolved)
	if err := nuget.WriteLockFile(n.lockfile, lf); err != nil {
		return fmt.Errorf("vmnet: %w", err)
	}
	return nil
}

// Package is one resolved package's public-facing summary (spec §22.6),
// wrapping internal/nuget.LockedPackage so callers never need to import
// an internal package to use NuGetManager.Packages/VM.LoadPackage.
type Package struct {
	ID            string
	Version       string
	SelectedAsset string
	Unselectable  string // non-empty explains why no asset could be loaded
	Dependencies  []string
}

// Packages returns every package recorded in the lockfile — run Restore
// first if it doesn't exist yet.
func (n *NuGetManager) Packages() ([]Package, error) {
	lf, err := nuget.ReadLockFile(n.lockfile)
	if err != nil {
		return nil, fmt.Errorf("vmnet: %w (run Restore first)", err)
	}
	out := make([]Package, len(lf.Packages))
	for i, p := range lf.Packages {
		out[i] = Package{ID: p.ID, Version: p.Version, SelectedAsset: p.SelectedAsset, Unselectable: p.Unselectable, Dependencies: p.Dependencies}
	}
	return out, nil
}

// LoadPackage loads the assembly Restore selected for id (spec §6.4:
// `vm.LoadPackage("NodaTime")`), reading its .nupkg from the local cache
// — and, since Fase 3.27, also loads id's full transitive dependency
// closure (walking the lockfile's own already-resolved Dependencies
// graph, computed once by Restore) and attaches each as an
// Assembly.WithDependencies dep, so a package whose own IL directly
// calls into another package's types (not just the BCL) resolves
// end-to-end — e.g. Jint's real dependency chain (Jint -> Esprima ->
// System.Memory -> ...). A dependency with no selectable managed asset
// (a pure reference/native/compile-only package) is silently skipped,
// not an error: it may still be a legitimate link in the chain with
// simply nothing of its own to load.
func (vm *VM) LoadPackage(id string) (*Assembly, error) {
	n := vm.NuGet()
	lf, err := nuget.ReadLockFile(n.lockfile)
	if err != nil {
		return nil, fmt.Errorf("vmnet: %w (run NuGet().Restore() first)", err)
	}
	loaded := map[string]*Assembly{}
	asm, err := vm.loadLockedPackage(n, lf, id, loaded)
	if err != nil {
		return nil, err
	}
	if asm == nil {
		return nil, fmt.Errorf("vmnet: package %q has no usable assembly (check NuGet().Packages() for the reason)", id)
	}
	// Build the shared cross-package type index (Fase 3.40) — see
	// Assembly.globalTypeIndex's own doc comment for why this exists at
	// all (a shared dependency's own generic method resolving typeof(T)
	// for a T declared in one of ITS OWN dependents, not the other way
	// around). Best-effort: a TypeDef this loop can't name (a decode
	// error on some row) is just skipped, not a hard failure — the index
	// is a last-resort fallback, never required for a package to load.
	index := map[string]*Assembly{}
	for _, a := range loaded {
		if a != nil {
			a.indexOwnTypesInto(index)
		}
	}
	for _, a := range loaded {
		if a != nil {
			a.globalTypeIndex = index
		}
	}
	return asm, nil
}

// loadLockedPackage loads one locked package's own selected asset (if
// any) and recursively attaches its dependencies — see LoadPackage's
// doc comment. loaded caches by package ID within one LoadPackage call,
// both to avoid reloading a package reachable through more than one
// path in the dependency graph (diamond dependencies are common) and to
// terminate on any dependency cycle, however unlikely in a real NuGet
// graph.
func (vm *VM) loadLockedPackage(n *NuGetManager, lf *nuget.LockFile, id string, loaded map[string]*Assembly) (*Assembly, error) {
	if asm, ok := loaded[id]; ok {
		return asm, nil
	}
	var found *nuget.LockedPackage
	for i := range lf.Packages {
		if lf.Packages[i].ID == id {
			found = &lf.Packages[i]
			break
		}
	}
	if found == nil {
		return nil, fmt.Errorf("vmnet: package %q is not in %s (add + restore it first)", id, n.lockfile)
	}
	if found.SelectedAsset == "" {
		loaded[id] = nil
		return nil, nil
	}

	data, err := n.cache.Load(found.ID, found.Version)
	if err != nil {
		return nil, fmt.Errorf("vmnet: %s@%s not in local cache (run NuGet().Restore() again): %w", found.ID, found.Version, err)
	}
	pkg, err := nuget.OpenPackage(data)
	if err != nil {
		return nil, fmt.Errorf("vmnet: %w", err)
	}
	// Load the exact asset Restore locked, not a freshly re-selected one —
	// what runs should always match what was inspected/promised.
	assetData, ok := pkg.Entry(found.SelectedAsset)
	if !ok {
		return nil, fmt.Errorf("vmnet: %s@%s: locked asset %q is missing from the cached .nupkg (re-run Restore)", found.ID, found.Version, found.SelectedAsset)
	}
	asm, err := vm.LoadBytes(found.ID+"@"+found.Version, assetData)
	if err != nil {
		return nil, err
	}
	loaded[id] = asm

	for _, depID := range found.Dependencies {
		depAsm, err := vm.loadLockedPackage(n, lf, depID, loaded)
		if err != nil {
			return nil, fmt.Errorf("vmnet: loading %s's dependency %q: %w", id, depID, err)
		}
		if depAsm != nil {
			asm.WithDependencies(depAsm)
		}
	}
	return asm, nil
}
