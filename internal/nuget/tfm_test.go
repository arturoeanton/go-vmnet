package nuget

import "testing"

func TestParseTFM(t *testing.T) {
	tests := []struct {
		raw    string
		family Family
		major  int
		minor  int
		patch  int
	}{
		{"netstandard2.0", FamilyNetStandard, 2, 0, 0},
		{"netstandard2.1", FamilyNetStandard, 2, 1, 0},
		{"net8.0", FamilyNetModern, 8, 0, 0},
		{"net6.0-windows", FamilyNetModern, 6, 0, 0},
		{"net472", FamilyNetFramework, 4, 7, 2},
		{"net48", FamilyNetFramework, 4, 8, 0},
		{"netcoreapp3.1", FamilyNetCoreApp, 3, 1, 0},
		// Long forms real .nuspec files still carry.
		{".NETStandard2.0", FamilyNetStandard, 2, 0, 0},
		{".NETFramework4.7.2", FamilyNetFramework, 4, 7, 2},
		{".NETFramework4.5", FamilyNetFramework, 4, 5, 0},
		{".NETCoreApp3.1", FamilyNetCoreApp, 3, 1, 0},
		{"garbage-not-a-tfm", FamilyUnknown, 0, 0, 0},
		{"", FamilyUnknown, 0, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got := ParseTFM(tt.raw)
			if got.Family != tt.family || got.Major != tt.major || got.Minor != tt.minor || got.Patch != tt.patch {
				t.Errorf("ParseTFM(%q) = %+v, want family=%v major=%d minor=%d patch=%d",
					tt.raw, got, tt.family, tt.major, tt.minor, tt.patch)
			}
		})
	}
}

func TestParseTFM_PlatformSpecific(t *testing.T) {
	got := ParseTFM("net6.0-windows")
	if !got.IsPlatformSpecific() {
		t.Error("IsPlatformSpecific() = false, want true for net6.0-windows")
	}
	if got.Platform != "windows" {
		t.Errorf("Platform = %q, want %q", got.Platform, "windows")
	}
}

func TestSelectTFM_PriorityOrder(t *testing.T) {
	// spec §22.5: netstandard2.0 > netstandard2.1 > net5.0+ (opt-in) >
	// nothing else, regardless of what order the candidates are listed in.
	candidates := []string{"net45", "netstandard2.1", "net8.0", "netstandard1.3", "netstandard2.0", "net6.0-windows"}

	best, ok := SelectTFM(candidates, SelectOptions{})
	if !ok || best.Raw != "netstandard2.0" {
		t.Fatalf("SelectTFM() = %+v, ok=%v, want netstandard2.0", best, ok)
	}
}

func TestSelectTFM_FallsBackToNetStandard21(t *testing.T) {
	candidates := []string{"net45", "netstandard2.1", "netstandard1.3"}
	best, ok := SelectTFM(candidates, SelectOptions{})
	if !ok || best.Raw != "netstandard2.1" {
		t.Fatalf("SelectTFM() = %+v, ok=%v, want netstandard2.1", best, ok)
	}
}

func TestSelectTFM_ModernNetRequiresOptIn(t *testing.T) {
	candidates := []string{"net8.0"}

	if _, ok := SelectTFM(candidates, SelectOptions{}); ok {
		t.Error("SelectTFM() selected net8.0 without AllowModernNet, want ok=false")
	}
	best, ok := SelectTFM(candidates, SelectOptions{AllowModernNet: true})
	if !ok || best.Raw != "net8.0" {
		t.Errorf("SelectTFM(AllowModernNet) = %+v, ok=%v, want net8.0", best, ok)
	}
}

func TestSelectTFM_NoCandidates(t *testing.T) {
	if _, ok := SelectTFM(nil, SelectOptions{}); ok {
		t.Error("SelectTFM(nil) ok = true, want false")
	}
	if _, ok := SelectTFM([]string{"net45", "net6.0-windows"}, SelectOptions{}); ok {
		t.Error("SelectTFM(only unselectable) ok = true, want false")
	}
}

func TestSelectable_Reasons(t *testing.T) {
	tests := []struct {
		raw  string
		opts SelectOptions
		ok   bool
	}{
		{"netstandard2.0", SelectOptions{}, true},
		{"net8.0", SelectOptions{}, false},
		{"net8.0", SelectOptions{AllowModernNet: true}, true},
		{"net472", SelectOptions{}, false},
		{"net6.0-windows", SelectOptions{AllowModernNet: true}, false},
	}
	for _, tt := range tests {
		ok, reason := Selectable(ParseTFM(tt.raw), tt.opts)
		if ok != tt.ok {
			t.Errorf("Selectable(%q, %+v) = %v (%q), want %v", tt.raw, tt.opts, ok, reason, tt.ok)
		}
		if !ok && reason == "" {
			t.Errorf("Selectable(%q) = false with empty reason, want an explanation", tt.raw)
		}
	}
}
