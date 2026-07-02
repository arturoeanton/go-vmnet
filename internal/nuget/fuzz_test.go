package nuget

import "testing"

// FuzzParseNuSpec proves malformed .nuspec XML can't panic the parser —
// a .nuspec ultimately comes from a downloaded third-party package, so
// this is the same "untrusted input" concern as internal/pe's FuzzParse.
func FuzzParseNuSpec(f *testing.F) {
	f.Add([]byte(""))
	f.Add([]byte(sampleNuSpecGrouped))
	f.Add([]byte(sampleNuSpecFlat))
	f.Add([]byte("<package><metadata><id/></metadata></package>"))
	f.Add([]byte("<not-a-package/>"))

	f.Fuzz(func(t *testing.T, data []byte) {
		spec, err := ParseNuSpec(data)
		if err != nil || spec == nil {
			return
		}
		// A successful parse must leave DependenciesFor safe to call for
		// any TFM, including ones with no matching group.
		for _, tfm := range []string{"netstandard2.0", "net8.0", "", "garbage"} {
			_ = spec.DependenciesFor(ParseTFM(tfm))
		}
	})
}

// FuzzOpenPackage proves a malformed .nupkg (a zip file, but an
// adversarial one — bad central directory, a .nuspec with impossible
// content, ...) can't panic the resolver.
func FuzzOpenPackage(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("not a zip"))
	f.Add(buildNupkg("Sample", "1.0.0", simpleNuSpec("Sample", "1.0.0"),
		fakeEntry{"lib/netstandard2.0/Sample.dll", []byte("dummy")}))
	f.Add(buildNupkg("Native", "1.0.0", simpleNuSpec("Native", "1.0.0"),
		fakeEntry{"runtimes/linux-x64/native/libfoo.so", []byte("elf")}))

	f.Fuzz(func(t *testing.T, data []byte) {
		pkg, err := OpenPackage(data)
		if err != nil || pkg == nil {
			return
		}
		_ = pkg.LibTFMs()
		_ = pkg.RefTFMs()
		_ = pkg.HasNativeAssets()
		_, _, _ = pkg.SelectLibAsset(SelectOptions{})
		_, _, _ = pkg.SelectLibAsset(SelectOptions{AllowModernNet: true})
	})
}
