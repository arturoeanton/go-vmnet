package nuget

import (
	"strconv"
	"strings"
)

// CompareVersions orders two NuGet version strings. NuGet versions are
// usually semver (Major.Minor.Patch[-prerelease]) but sometimes carry a
// 4th "revision" component inherited from .NET's System.Version; both are
// handled, comparing numeric components left to right and treating a
// prerelease suffix as lower than the same numeric release (matching
// semver precedence, e.g. 1.0.0-beta < 1.0.0). This is deliberately
// simpler than NuGet's full version-range negotiation — vmnet's resolver
// just wants "which of these two is newer", not range satisfaction.
func CompareVersions(a, b string) int {
	coreA, preA := splitVersion(a)
	coreB, preB := splitVersion(b)

	for i := 0; i < 4; i++ {
		va, vb := versionPart(coreA, i), versionPart(coreB, i)
		if va != vb {
			if va < vb {
				return -1
			}
			return 1
		}
	}

	switch {
	case preA == "" && preB == "":
		return 0
	case preA == "" && preB != "":
		return 1
	case preA != "" && preB == "":
		return -1
	case preA < preB:
		return -1
	case preA > preB:
		return 1
	default:
		return 0
	}
}

// ParseMinVersion extracts the concrete version the resolver actually
// fetches from a NuGet dependency version string, which may be a plain
// version ("5.0.0", meaning ">= 5.0.0") or a full NuGet version range
// ("[3.1.1, 4.0.0)", "[3.1.1]", "(1.0.0, )", ...) — real .nuspec files
// use ranges routinely (ClosedXML@0.105.0's own dependency on
// DocumentFormat.OpenXml is declared as "[3.1.1, 4.0.0)", not a plain
// pin). vmnet always resolves to the range's lower bound: the same
// "lowest applicable version" NuGet itself defaults to for a plain
// PackageReference with no floating notation, and deterministic without
// an extra round-trip to enumerate every available version and pick the
// highest one satisfying the range. A malformed or open-ended-minimum
// range ("(, 4.0.0)") falls back to the original string unchanged —
// Cache.Fetch will then fail with a clear "not found", which is more
// honest than silently guessing a version.
func ParseMinVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || (v[0] != '[' && v[0] != '(') {
		return v
	}
	inner := v
	if len(inner) >= 2 {
		inner = inner[1 : len(inner)-1]
	}
	if comma := strings.IndexByte(inner, ','); comma >= 0 {
		inner = inner[:comma]
	}
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return v
	}
	return inner
}

func splitVersion(v string) (core []string, prerelease string) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if plus := strings.IndexByte(v, '+'); plus >= 0 {
		v = v[:plus] // build metadata never affects precedence
	}
	if dash := strings.IndexByte(v, '-'); dash >= 0 {
		return strings.Split(v[:dash], "."), v[dash+1:]
	}
	return strings.Split(v, "."), ""
}

func versionPart(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	n, err := strconv.Atoi(parts[i])
	if err != nil {
		return 0
	}
	return n
}
