package nuget

import "testing"

const sampleNuSpecGrouped = `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://schemas.microsoft.com/packaging/2013/05/nuspec.xsd">
  <metadata>
    <id>Sample.Package</id>
    <version>1.2.3</version>
    <dependencies>
      <group targetFramework=".NETStandard2.0">
        <dependency id="Newtonsoft.Json" version="13.0.3" exclude="Build,Analyzers" />
        <dependency id="System.Buffers" version="4.5.1" />
      </group>
      <group targetFramework="net6.0">
        <dependency id="System.Buffers" version="4.5.1" />
      </group>
    </dependencies>
  </metadata>
</package>`

const sampleNuSpecFlat = `<?xml version="1.0" encoding="utf-8"?>
<package>
  <metadata>
    <id>Legacy.Package</id>
    <version>0.9.0</version>
    <dependencies>
      <dependency id="Old.Dep" version="1.0.0" />
    </dependencies>
  </metadata>
</package>`

func TestParseNuSpec_Grouped(t *testing.T) {
	spec, err := ParseNuSpec([]byte(sampleNuSpecGrouped))
	if err != nil {
		t.Fatalf("ParseNuSpec() error = %v", err)
	}
	if spec.Metadata.ID != "Sample.Package" || spec.Metadata.Version != "1.2.3" {
		t.Errorf("Metadata = %+v", spec.Metadata)
	}

	deps := spec.DependenciesFor(ParseTFM("netstandard2.0"))
	if len(deps) != 2 {
		t.Fatalf("DependenciesFor(netstandard2.0) = %+v, want 2 entries", deps)
	}

	deps = spec.DependenciesFor(ParseTFM("net6.0"))
	if len(deps) != 1 || deps[0].ID != "System.Buffers" {
		t.Fatalf("DependenciesFor(net6.0) = %+v, want [System.Buffers]", deps)
	}

	// A TFM with no matching group and no "any framework" fallback group
	// has no dependencies under that target, not an error.
	deps = spec.DependenciesFor(ParseTFM("net472"))
	if len(deps) != 0 {
		t.Errorf("DependenciesFor(net472) = %+v, want none", deps)
	}
}

func TestParseNuSpec_FlatLegacyForm(t *testing.T) {
	spec, err := ParseNuSpec([]byte(sampleNuSpecFlat))
	if err != nil {
		t.Fatalf("ParseNuSpec() error = %v", err)
	}
	// A flat (ungrouped) dependency list applies to every target framework.
	for _, tfm := range []string{"netstandard2.0", "net472", "net8.0"} {
		deps := spec.DependenciesFor(ParseTFM(tfm))
		if len(deps) != 1 || deps[0].ID != "Old.Dep" {
			t.Errorf("DependenciesFor(%s) = %+v, want [Old.Dep]", tfm, deps)
		}
	}
}

func TestParseNuSpec_MissingID(t *testing.T) {
	_, err := ParseNuSpec([]byte(`<package><metadata><version>1.0.0</version></metadata></package>`))
	if err == nil {
		t.Fatal("ParseNuSpec() with no <id>: error = nil, want an error")
	}
}

func TestParseNuSpec_MalformedXML(t *testing.T) {
	_, err := ParseNuSpec([]byte(`<package><metadata><id>X</id`))
	if err == nil {
		t.Fatal("ParseNuSpec() with truncated XML: error = nil, want an error")
	}
}
