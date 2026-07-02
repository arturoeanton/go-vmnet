package nuget

import "testing"

func TestOpenPackage_SelectsBestAsset(t *testing.T) {
	data := buildNupkg("Sample.Package", "1.0.0", simpleNuSpec("Sample.Package", "1.0.0"),
		fakeEntry{"lib/net45/Sample.Package.dll", []byte("net45 build")},
		fakeEntry{"lib/netstandard2.0/Sample.Package.dll", []byte("netstandard2.0 build")},
		fakeEntry{"lib/netstandard2.1/Sample.Package.dll", []byte("netstandard2.1 build")},
	)

	pkg, err := OpenPackage(data)
	if err != nil {
		t.Fatalf("OpenPackage() error = %v", err)
	}
	if pkg.Spec.Metadata.ID != "Sample.Package" {
		t.Errorf("Spec.Metadata.ID = %q", pkg.Spec.Metadata.ID)
	}

	asset, ok, reason := pkg.SelectLibAsset(SelectOptions{})
	if !ok {
		t.Fatalf("SelectLibAsset() ok = false: %s", reason)
	}
	if asset.TFM.Raw != "netstandard2.0" {
		t.Errorf("selected TFM = %q, want netstandard2.0 (spec §22.5 priority)", asset.TFM.Raw)
	}
	if string(asset.Data) != "netstandard2.0 build" {
		t.Errorf("selected asset content = %q", asset.Data)
	}
}

func TestOpenPackage_RefOnlyFallback(t *testing.T) {
	data := buildNupkg("Ref.Only", "1.0.0", simpleNuSpec("Ref.Only", "1.0.0"),
		fakeEntry{"ref/netstandard2.0/Ref.Only.dll", []byte("reference assembly")},
	)
	pkg, err := OpenPackage(data)
	if err != nil {
		t.Fatal(err)
	}
	asset, ok, reason := pkg.SelectLibAsset(SelectOptions{})
	if !ok {
		t.Fatalf("SelectLibAsset() ok = false: %s", reason)
	}
	if !asset.ReferenceOnly {
		t.Error("ReferenceOnly = false, want true (asset only exists under ref/)")
	}
}

func TestOpenPackage_NativeOnlyIsUnselectable(t *testing.T) {
	data := buildNupkg("Native.Only", "1.0.0", simpleNuSpec("Native.Only", "1.0.0"),
		fakeEntry{"runtimes/linux-x64/native/libfoo.so", []byte("elf")},
	)
	pkg, err := OpenPackage(data)
	if err != nil {
		t.Fatal(err)
	}
	_, ok, reason := pkg.SelectLibAsset(SelectOptions{})
	if ok {
		t.Fatal("SelectLibAsset() ok = true, want false for a native-only package")
	}
	if reason == "" {
		t.Error("reason is empty, want an explanation mentioning native assets")
	}
	natives := pkg.HasNativeAssets()
	if len(natives) != 1 || natives[0] != "runtimes/linux-x64/native/libfoo.so" {
		t.Errorf("HasNativeAssets() = %v", natives)
	}
}

func TestOpenPackage_NoNuspec(t *testing.T) {
	_, err := OpenPackage([]byte("not a zip at all"))
	if err == nil {
		t.Fatal("OpenPackage(garbage) error = nil, want an error")
	}
}

func TestOpenPackage_NoSelectableAsset(t *testing.T) {
	data := buildNupkg("Empty.Package", "1.0.0", simpleNuSpec("Empty.Package", "1.0.0"))
	pkg, err := OpenPackage(data)
	if err != nil {
		t.Fatal(err)
	}
	_, ok, reason := pkg.SelectLibAsset(SelectOptions{})
	if ok {
		t.Fatal("SelectLibAsset() ok = true for a package with no lib/ref assets at all")
	}
	if reason == "" {
		t.Error("reason is empty, want an explanation")
	}
}
