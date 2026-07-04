package bcl

import "github.com/arturoeanton/go-vmnet/internal/runtime"

// System.Xml.NameTable backs a real (if narrow) BCL type: a string-
// interning table XmlReaderSettings.NameTable/XmlReader.NameTable expose
// so multiple readers can share one pool of atomized strings (real
// callers compare interned names by reference for speed, e.g. System.IO.
// Packaging's own XmlCompatibilityReader). vmnet's strings are already
// plain Go strings compared by value everywhere real code in this loop's
// target packages actually uses one (`==`, never ReferenceEquals), so
// Add/Get are both simple identity passthroughs — no real interning table
// needed for that to behave correctly (Fase 3.40, found via
// System.IO.Packaging.PackageXmlStringTable's own static NameTable).
func init() {
	registerCtor("System.Xml.NameTable", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeNameTable{}}, nil
	})
	register("System.Xml.NameTable::.ctor", false, nameTableCtorInPlace)
	register("System.Xml.NameTable::Add", true, nameTableAdd)
	register("System.Xml.NameTable::Get", true, nameTableAdd)
	register("System.Xml.XmlReaderSettings::set_NameTable", false, xrSettingsNoop)
}

type nativeNameTable struct{}

func nameTableCtorInPlace(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return runtime.Value{}, nil
	}
	args[0].Obj.Native = &nativeNameTable{}
	return runtime.Value{}, nil
}

func nameTableAdd(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[1].Kind != runtime.KindString {
		return runtime.Null(), nil
	}
	return args[1], nil
}
