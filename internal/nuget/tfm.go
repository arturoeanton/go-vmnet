// Package nuget reads .nupkg/.nuspec packages, selects the best-matching
// target framework moniker (favoring netstandard2.0), resolves transitive
// dependencies and writes vmnet's own lockfile. See docs/en/ROADMAP.md, Fase 3,
// module "/nuget".
package nuget

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Family is the broad category a TFM belongs to.
type Family byte

const (
	FamilyUnknown Family = iota
	FamilyNetStandard
	FamilyNetCoreApp
	FamilyNetModern // net5.0+ (unified .NET, not .NET Framework)
	FamilyNetFramework
)

// TFM is a parsed target framework moniker, in either its short folder-name
// form ("netstandard2.0", "net472") or long form (".NETStandard,Version=v2.0"),
// both of which appear in real .nuspec files depending on the tool that
// generated them.
type TFM struct {
	Raw      string
	Family   Family
	Major    int
	Minor    int
	Patch    int
	Platform string // e.g. "windows" in "net6.0-windows" — always excluded from selection
}

var (
	reModern       = regexp.MustCompile(`^net(\d+)\.(\d+)(?:-([a-z0-9.]+))?$`)
	reNetStandard  = regexp.MustCompile(`^netstandard(\d+)\.(\d+)$`)
	reNetCoreApp   = regexp.MustCompile(`^netcoreapp(\d+)\.(\d+)$`)
	reNetFramework = regexp.MustCompile(`^net(\d)(\d)(\d)?$`) // dotless: net45, net472, net48

	// The long form real .nuspec files still carry for older packages,
	// e.g. ".NETStandard,Version=v2.0" (rare) or, far more commonly in
	// practice, the abbreviated ".NETStandard2.0" / ".NETFramework4.7.2"
	// dotted-with-a-leading-dot form msbuild/nuget.exe also emit.
	reLongForm = regexp.MustCompile(`^\.net(standard|coreapp|framework)(?:,version=v)?(\d+)\.(\d+)(?:\.(\d+))?$`)
)

// normalizeLongForm rewrites a long-form TFM to the short folder-name form
// ParseTFM's other patterns understand, or returns ok=false if s isn't in
// a long form it recognizes.
func normalizeLongForm(s string) (short string, ok bool) {
	m := reLongForm.FindStringSubmatch(s)
	if m == nil {
		return "", false
	}
	family, major, minor, patch := m[1], m[2], m[3], m[4]
	switch family {
	case "standard":
		return fmt.Sprintf("netstandard%s.%s", major, minor), true
	case "coreapp":
		return fmt.Sprintf("netcoreapp%s.%s", major, minor), true
	case "framework":
		return "net" + major + minor + patch, true // dotless short form: 4,7,2 -> "net472"
	}
	return "", false
}

// ParseTFM parses a target framework moniker in either notation. An
// unrecognized string is not an error — it comes back as FamilyUnknown so
// callers can decide whether that's fatal (e.g. an empty/legacy
// "any framework" dependency group is common and not an error).
func ParseTFM(s string) TFM {
	raw := s
	norm := strings.ToLower(strings.TrimSpace(s))
	if short, ok := normalizeLongForm(norm); ok {
		norm = short
	}

	if m := reNetStandard.FindStringSubmatch(norm); m != nil {
		major, _ := strconv.Atoi(m[1])
		minor, _ := strconv.Atoi(m[2])
		return TFM{Raw: raw, Family: FamilyNetStandard, Major: major, Minor: minor}
	}
	if m := reNetCoreApp.FindStringSubmatch(norm); m != nil {
		major, _ := strconv.Atoi(m[1])
		minor, _ := strconv.Atoi(m[2])
		return TFM{Raw: raw, Family: FamilyNetCoreApp, Major: major, Minor: minor}
	}
	if m := reModern.FindStringSubmatch(norm); m != nil {
		major, _ := strconv.Atoi(m[1])
		minor, _ := strconv.Atoi(m[2])
		return TFM{Raw: raw, Family: FamilyNetModern, Major: major, Minor: minor, Platform: m[3]}
	}
	if m := reNetFramework.FindStringSubmatch(norm); m != nil {
		major, _ := strconv.Atoi(m[1])
		minor, _ := strconv.Atoi(m[2])
		patch := 0
		if m[3] != "" {
			patch, _ = strconv.Atoi(m[3])
		}
		return TFM{Raw: raw, Family: FamilyNetFramework, Major: major, Minor: minor, Patch: patch}
	}
	return TFM{Raw: raw, Family: FamilyUnknown}
}

func (t TFM) String() string {
	if t.Raw == "" {
		return "(any)"
	}
	return t.Raw
}

// IsPlatformSpecific reports whether t targets a specific OS (e.g.
// "net6.0-windows") — vmnet's pure-Go, cross-platform runtime never
// selects these (spec §22.5 has no platform-specific tier).
func (t TFM) IsPlatformSpecific() bool { return t.Platform != "" }

// SelectOptions configures asset/dependency-group selection.
type SelectOptions struct {
	// AllowModernNet opts into selecting a net5.0+ asset when no
	// netstandard asset is available (spec §22.5, tier 3: "solo si el
	// perfil lo permite" — off by default because vmnet's IL/BCL profile
	// targets netstandard2.0-shaped code, not the modern BCL surface).
	AllowModernNet bool
}

// tier ranks a candidate TFM for vmnet's fixed target (netstandard2.0),
// per spec §22.5's priority list. Lower is better; 0 means "not selectable".
func tier(t TFM, opts SelectOptions) int {
	if t.IsPlatformSpecific() {
		return 0
	}
	switch t.Family {
	case FamilyNetStandard:
		switch {
		case t.Major == 2 && t.Minor == 0:
			return 1
		case t.Major == 2 && t.Minor == 1:
			return 2
		case t.Major <= 2:
			return 5 // netstandard1.x: older but still usually source-compatible
		}
	case FamilyNetModern:
		if opts.AllowModernNet {
			return 3
		}
	}
	return 0
}

// SelectTFM picks the best candidate for vmnet's target out of candidates
// (e.g. the TFM segment of each `lib/<tfm>/` folder in a .nupkg). ok is
// false if none are selectable.
func SelectTFM(candidates []string, opts SelectOptions) (best TFM, ok bool) {
	bestTier := 0
	for _, c := range candidates {
		t := ParseTFM(c)
		tr := tier(t, opts)
		if tr == 0 {
			continue
		}
		if !ok || tr < bestTier {
			best, bestTier, ok = t, tr, true
		}
	}
	return best, ok
}

// Selectable reports whether t is usable at all for vmnet's target, with a
// human reason when it isn't (spec §22.5/§23: explain, don't just fail).
func Selectable(t TFM, opts SelectOptions) (ok bool, reason string) {
	if t.IsPlatformSpecific() {
		return false, fmt.Sprintf("%s targets a specific platform (%s); vmnet is cross-platform pure Go", t.Raw, t.Platform)
	}
	if tier(t, opts) > 0 {
		return true, ""
	}
	switch t.Family {
	case FamilyNetModern:
		return false, fmt.Sprintf("%s requires AllowModernNet (spec §22.5 tier 3)", t.Raw)
	case FamilyNetFramework:
		return false, fmt.Sprintf("%s targets .NET Framework, not netstandard2.0-compatible", t.Raw)
	case FamilyNetCoreApp:
		return false, fmt.Sprintf("%s targets netcoreapp directly, not a library-compatible TFM", t.Raw)
	default:
		return false, fmt.Sprintf("%q is not a recognized or supported target framework moniker", t.Raw)
	}
}
