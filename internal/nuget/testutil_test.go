package nuget

import (
	"archive/zip"
	"bytes"
	"fmt"
)

// fakeEntry is one file to embed in a synthetic .nupkg built by buildNupkg.
type fakeEntry struct {
	name string
	data []byte
}

// buildNupkg constructs an in-memory .nupkg (a zip file) for tests, so
// resolver/selection logic can be exercised deterministically without
// hitting the network or vendoring real third-party binaries.
func buildNupkg(id, version, nuspecXML string, extra ...fakeEntry) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	nuspecName := id + ".nuspec"
	w, err := zw.Create(nuspecName)
	if err != nil {
		panic(err)
	}
	if _, err := w.Write([]byte(nuspecXML)); err != nil {
		panic(err)
	}

	for _, e := range extra {
		w, err := zw.Create(e.name)
		if err != nil {
			panic(err)
		}
		if _, err := w.Write(e.data); err != nil {
			panic(err)
		}
	}

	if err := zw.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// simpleNuSpec renders a minimal single-group .nuspec for id@version with
// the given dependencies under netstandard2.0.
func simpleNuSpec(id, version string, deps ...Dependency) string {
	depXML := ""
	for _, d := range deps {
		depXML += fmt.Sprintf(`<dependency id="%s" version="%s" />`, d.ID, d.Version)
	}
	return fmt.Sprintf(`<?xml version="1.0"?>
<package><metadata><id>%s</id><version>%s</version>
<dependencies><group targetFramework="netstandard2.0">%s</group></dependencies>
</metadata></package>`, id, version, depXML)
}
