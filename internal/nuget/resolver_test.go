package nuget

import (
	"testing"
)

// seedCache pre-populates a Cache with synthetic packages so Resolver
// never needs the network — resolver_test.go is entirely about the graph
// algorithm (transitive closure, version conflicts, cycles), not I/O.
func seedCache(t *testing.T, pkgs ...struct {
	id, version string
	deps        []Dependency
}) *Cache {
	t.Helper()
	cache := NewCache(t.TempDir())
	for _, p := range pkgs {
		// Every synthetic package here ships a (dummy) netstandard2.0
		// asset: dependency resolution only knows which group to use once
		// an asset's TFM has been selected (see resolver.go) — a package
		// with no asset at all is exactly TestResolver_UnselectableDependencyIsRecordedNotFatal's
		// scenario, tested separately.
		data := buildNupkg(p.id, p.version, simpleNuSpec(p.id, p.version, p.deps...),
			fakeEntry{"lib/netstandard2.0/" + p.id + ".dll", []byte("dummy")})
		if err := cache.Store(p.id, p.version, data); err != nil {
			t.Fatalf("seeding cache for %s@%s: %v", p.id, p.version, err)
		}
	}
	return cache
}

func TestResolver_TransitiveClosure(t *testing.T) {
	// A -> B -> C (a simple chain).
	cache := seedCache(t,
		struct {
			id, version string
			deps        []Dependency
		}{"A", "1.0.0", []Dependency{{ID: "B", Version: "1.0.0"}}},
		struct {
			id, version string
			deps        []Dependency
		}{"B", "1.0.0", []Dependency{{ID: "C", Version: "1.0.0"}}},
		struct {
			id, version string
			deps        []Dependency
		}{"C", "1.0.0", nil},
	)

	r := NewResolver(&Client{BaseURL: "http://127.0.0.1:1"}, cache, SelectOptions{})
	resolved, err := r.Resolve([]Dependency{{ID: "A", Version: "1.0.0"}})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	for _, id := range []string{"a", "b", "c"} {
		if _, ok := resolved[id]; !ok {
			t.Errorf("resolved is missing %q; got %v", id, keysOf(resolved))
		}
	}
}

func TestResolver_DiamondHighestVersionWins(t *testing.T) {
	// A depends on B and C; B wants D@1.0.0, C wants D@2.0.0 — the
	// resolver should keep the higher one (documented simplification vs.
	// NuGet's real version-range negotiation, see resolver.go).
	cache := seedCache(t,
		struct {
			id, version string
			deps        []Dependency
		}{"A", "1.0.0", []Dependency{{ID: "B", Version: "1.0.0"}, {ID: "C", Version: "1.0.0"}}},
		struct {
			id, version string
			deps        []Dependency
		}{"B", "1.0.0", []Dependency{{ID: "D", Version: "1.0.0"}}},
		struct {
			id, version string
			deps        []Dependency
		}{"C", "1.0.0", []Dependency{{ID: "D", Version: "2.0.0"}}},
		struct {
			id, version string
			deps        []Dependency
		}{"D", "1.0.0", nil},
		struct {
			id, version string
			deps        []Dependency
		}{"D", "2.0.0", nil},
	)

	r := NewResolver(&Client{BaseURL: "http://127.0.0.1:1"}, cache, SelectOptions{})
	resolved, err := r.Resolve([]Dependency{{ID: "A", Version: "1.0.0"}})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	got := resolved["d"]
	if got == nil || got.Version != "2.0.0" {
		t.Errorf("resolved[d] = %+v, want version 2.0.0", got)
	}
}

func TestResolver_CycleDetected(t *testing.T) {
	cache := seedCache(t,
		struct {
			id, version string
			deps        []Dependency
		}{"A", "1.0.0", []Dependency{{ID: "B", Version: "1.0.0"}}},
		struct {
			id, version string
			deps        []Dependency
		}{"B", "1.0.0", []Dependency{{ID: "A", Version: "1.0.0"}}},
	)

	r := NewResolver(&Client{BaseURL: "http://127.0.0.1:1"}, cache, SelectOptions{})
	_, err := r.Resolve([]Dependency{{ID: "A", Version: "1.0.0"}})
	if err == nil {
		t.Fatal("Resolve() with a dependency cycle: error = nil, want an error")
	}
}

func TestResolver_UnselectableDependencyIsRecordedNotFatal(t *testing.T) {
	// A depends on a native-only package: resolution should still
	// succeed, recording *why* that one package has no usable asset —
	// spec §23: explain, don't just fail.
	cache := NewCache(t.TempDir())
	aData := buildNupkg("A", "1.0.0", simpleNuSpec("A", "1.0.0", Dependency{ID: "Native", Version: "1.0.0"}),
		fakeEntry{"lib/netstandard2.0/A.dll", []byte("dummy")})
	nativeData := buildNupkg("Native", "1.0.0", simpleNuSpec("Native", "1.0.0"),
		fakeEntry{"runtimes/linux-x64/native/libfoo.so", []byte("elf")})
	if err := cache.Store("A", "1.0.0", aData); err != nil {
		t.Fatal(err)
	}
	if err := cache.Store("Native", "1.0.0", nativeData); err != nil {
		t.Fatal(err)
	}

	r := NewResolver(&Client{BaseURL: "http://127.0.0.1:1"}, cache, SelectOptions{})
	resolved, err := r.Resolve([]Dependency{{ID: "A", Version: "1.0.0"}})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	native := resolved["native"]
	if native == nil {
		t.Fatal("resolved is missing \"native\"")
	}
	if native.SelectedAsset != "" {
		t.Errorf("native.SelectedAsset = %q, want empty", native.SelectedAsset)
	}
	if native.Unselectable == "" {
		t.Error("native.Unselectable is empty, want an explanation")
	}
}

func keysOf(m map[string]*ResolvedPackage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
