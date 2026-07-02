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
