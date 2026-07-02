package nuget

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
)

// Package is a parsed .nupkg: its manifest plus the raw zip entries, ready
// for asset selection (spec §22.4: lib/, ref/, runtimes/).
type Package struct {
	Spec    *NuSpec
	entries map[string][]byte
}

// OpenPackage reads a .nupkg (a zip archive) from data.
func OpenPackage(data []byte) (*Package, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("nuget: opening .nupkg: %w", err)
	}

	entries := make(map[string][]byte, len(zr.File))
	var nuspecName string
	for _, zf := range zr.File {
		name := zf.Name
		if strings.HasSuffix(name, "/") {
			continue // directory entry
		}
		if !nameIsRelevant(name) {
			continue // skip package internals we never need (_rels/, package/, [Content_Types].xml, ...)
		}
		if strings.HasSuffix(strings.ToLower(name), ".nuspec") && !strings.Contains(name, "/") {
			nuspecName = name
		}
		rc, err := zf.Open()
		if err != nil {
			return nil, fmt.Errorf("nuget: reading %s: %w", name, err)
		}
		content, err := readLimited(rc, 256<<20) // 256MB cap: a hostile .nupkg shouldn't OOM the resolver
		closeErr := rc.Close()
		if err != nil {
			return nil, fmt.Errorf("nuget: reading %s: %w", name, err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("nuget: closing %s: %w", name, closeErr)
		}
		entries[name] = content
	}

	if nuspecName == "" {
		return nil, fmt.Errorf("nuget: .nupkg has no root .nuspec file")
	}
	spec, err := ParseNuSpec(entries[nuspecName])
	if err != nil {
		return nil, err
	}

	return &Package{Spec: spec, entries: entries}, nil
}

func nameIsRelevant(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".nuspec") && !strings.Contains(name, "/") {
		return true
	}
	return strings.HasPrefix(lower, "lib/") || strings.HasPrefix(lower, "ref/") || strings.HasPrefix(lower, "runtimes/")
}

func readLimited(r io.Reader, limit int64) ([]byte, error) {
	lr := io.LimitReader(r, limit+1)
	data, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("entry exceeds %d bytes", limit)
	}
	return data, nil
}

// Entry returns one zip entry's raw bytes by its exact path (e.g.
// "lib/netstandard2.0/Foo.dll"), as recorded in a LockedPackage's
// SelectedAsset.
func (p *Package) Entry(entryPath string) ([]byte, bool) {
	data, ok := p.entries[entryPath]
	return data, ok
}

// LibTFMs returns every distinct TFM folder name under lib/.
func (p *Package) LibTFMs() []string {
	return p.tfmFoldersUnder("lib/")
}

// RefTFMs returns every distinct TFM folder name under ref/ (analysis-only
// reference assemblies — spec §22.5 tier 4: never selected for execution).
func (p *Package) RefTFMs() []string {
	return p.tfmFoldersUnder("ref/")
}

func (p *Package) tfmFoldersUnder(prefix string) []string {
	seen := map[string]bool{}
	for name := range p.entries {
		if !strings.HasPrefix(strings.ToLower(name), prefix) {
			continue
		}
		rest := name[len(prefix):]
		if slash := strings.Index(rest, "/"); slash > 0 {
			seen[rest[:slash]] = true
		}
	}
	out := make([]string, 0, len(seen))
	for tfm := range seen {
		out = append(out, tfm)
	}
	sort.Strings(out)
	return out
}

// HasNativeAssets reports whether the package ships runtimes/*/native
// content — unsupported in vmnet's pure-Go mode (spec §22.5 tier 5).
func (p *Package) HasNativeAssets() []string {
	var natives []string
	for name := range p.entries {
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, "runtimes/") && strings.Contains(lower, "/native/") {
			natives = append(natives, name)
		}
	}
	sort.Strings(natives)
	return natives
}

// SelectedAsset is one DLL vmnet picked to load, plus how it got picked.
type SelectedAsset struct {
	TFM           TFM
	Path          string // zip entry path, e.g. "lib/netstandard2.0/Foo.dll"
	Data          []byte
	ReferenceOnly bool // came from ref/, not lib/ — spec §22.5 tier 4
}

// SelectLibAsset picks the best lib/ (or, failing that, ref/) DLL for
// vmnet's target, per spec §22.5's priority order. If the package has no
// selectable managed asset at all, ok is false and reason explains why —
// including native-asset packages, which are correctly "unsupported", not
// a bug.
func (p *Package) SelectLibAsset(opts SelectOptions) (asset SelectedAsset, ok bool, reason string) {
	if tfm, found := SelectTFM(p.LibTFMs(), opts); found {
		if data, dllPath, ok := p.firstDLLUnder("lib/" + tfm.Raw + "/"); ok {
			return SelectedAsset{TFM: tfm, Path: dllPath, Data: data}, true, ""
		}
	}
	if tfm, found := SelectTFM(p.RefTFMs(), opts); found {
		if data, dllPath, ok := p.firstDLLUnder("ref/" + tfm.Raw + "/"); ok {
			return SelectedAsset{TFM: tfm, Path: dllPath, Data: data, ReferenceOnly: true}, true,
				"selected from ref/ (compile-time reference only, spec §22.5 tier 4) — cannot be executed, only inspected"
		}
	}
	if natives := p.HasNativeAssets(); len(natives) > 0 {
		return SelectedAsset{}, false, fmt.Sprintf("package only ships native assets (%s) — unsupported in pure-Go mode", natives[0])
	}
	return SelectedAsset{}, false, "no lib/ or ref/ asset compatible with vmnet's target (netstandard2.0, or netstandard2.1/net5.0+ if allowed)"
}

func (p *Package) firstDLLUnder(prefix string) (data []byte, entryPath string, ok bool) {
	var names []string
	for name := range p.entries {
		if strings.HasPrefix(strings.ToLower(name), prefix) && strings.EqualFold(path.Ext(name), ".dll") {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil, "", false
	}
	sort.Strings(names) // deterministic when a TFM folder ships more than one assembly
	return p.entries[names[0]], names[0], true
}
