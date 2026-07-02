package nuget

import (
	"encoding/xml"
	"fmt"
)

// NuSpec is the parsed .nuspec manifest (spec §22.3): package identity and
// per-framework dependency groups. Only the fields vmnet's resolver needs
// are modeled — a real .nuspec has many more (authors, license, icon,
// ...) that are irrelevant to compatibility/dependency resolution.
type NuSpec struct {
	Metadata NuSpecMetadata `xml:"metadata"`
}

type NuSpecMetadata struct {
	ID           string             `xml:"id"`
	Version      string             `xml:"version"`
	Dependencies nuSpecDependencies `xml:"dependencies"`
}

type nuSpecDependencies struct {
	// Legacy/simple form: <dependencies><dependency .../></dependencies>,
	// applies to every target framework.
	Flat []Dependency `xml:"dependency"`
	// Modern form: one <group targetFramework="..."> per TFM.
	Groups []DependencyGroup `xml:"group"`
}

type DependencyGroup struct {
	TargetFramework string       `xml:"targetFramework,attr"`
	Dependencies    []Dependency `xml:"dependency"`
}

type Dependency struct {
	ID      string `xml:"id,attr"`
	Version string `xml:"version,attr"`
}

// ParseNuSpec parses a .nuspec file's raw XML bytes.
func ParseNuSpec(data []byte) (*NuSpec, error) {
	var spec NuSpec
	if err := xml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("nuget: parsing .nuspec: %w", err)
	}
	if spec.Metadata.ID == "" {
		return nil, fmt.Errorf("nuget: .nuspec is missing <id>")
	}
	return &spec, nil
}

// DependenciesFor returns the dependency list that applies when consuming
// this package under target TFM (spec §22.3: "el .nuspec contiene
// metadata y dependencias del paquete, agrupadas por framework").
//
// If the package has no groups (legacy form), the flat list applies
// unconditionally. If it has groups, target is assumed to already be a
// concrete, chosen TFM (typically the one SelectLibAsset picked) — this
// just finds the group with that exact TFM, it does not re-run vmnet's
// own selection tiering against it.
func (s *NuSpec) DependenciesFor(target TFM) []Dependency {
	if len(s.Metadata.Dependencies.Groups) == 0 {
		return s.Metadata.Dependencies.Flat
	}

	for i := range s.Metadata.Dependencies.Groups {
		g := &s.Metadata.Dependencies.Groups[i]
		if g.TargetFramework == "" {
			continue // "any framework" group with no TFM restriction, handled as fallback below
		}
		t := ParseTFM(g.TargetFramework)
		if t.Family == target.Family && t.Major == target.Major && t.Minor == target.Minor {
			return g.Dependencies
		}
	}
	// Fallback: an explicit empty-TFM group means "applies to everything".
	for i := range s.Metadata.Dependencies.Groups {
		if s.Metadata.Dependencies.Groups[i].TargetFramework == "" {
			return s.Metadata.Dependencies.Groups[i].Dependencies
		}
	}
	return nil
}
