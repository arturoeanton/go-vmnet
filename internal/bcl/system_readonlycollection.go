package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// System.Collections.ObjectModel.ReadOnlyCollection<T> (Fase 3.43, found
// reading a real .xlsx through ClosedXML 0.105.0's `new XLWorkbook(stream)`:
// DocumentFormat.OpenXml's own OpenXmlPartReader.Attributes property —
// real decompiled source, /tmp/openxmlfw_ns20/DocumentFormat.OpenXml/
// OpenXmlPartReader.cs:69-81 — returns `new ReadOnlyCollection
// <OpenXmlAttribute>(_attributeList)`, and ClosedXML's own worksheet
// reader consumes it via Count + the indexer (ClosedXML.Extensions/
// OpenXmlPartReaderExtensions.cs:28-41)).
//
// Two real construction shapes reach the ctor here:
//
//   - wrapping a live List<T> (OpenXmlPartReader._attributeList): the SAME
//     *nativeList backing is shared into the new wrapper Object — a real,
//     live view (real ReadOnlyCollection<T> is a wrapper over the live
//     IList<T>, not a snapshot), with every member (get_Count/get_Item/
//     GetEnumerator) resolving through the existing List`1 natives — both
//     via the receiver-concrete-type dispatch walk (NativeTypeName still
//     reports the shared backing's own List`1 name) and via the aliases
//     registered below for the declared-name fallback path.
//   - wrapping a T[] (Cached.ReadOnlyCollectionCache<T>.Value's own
//     `new ReadOnlyCollection<T>(Array<T>())`, DocumentFormat.OpenXml.
//     Framework/Cached.cs — always Array.Empty<T>()): the array's own
//     element slice is wrapped directly; a CLI array is fixed-size, so
//     element writes (the only mutation possible) stay visible through
//     the shared slice exactly like the real live view.
//
// Anything else (an arbitrary interpreted IList<T> implementation) has no
// real caller in this loop's target packages and errors loudly rather
// than silently snapshotting wrong.
func init() {
	registerCtor("System.Collections.ObjectModel.ReadOnlyCollection`1", func(args []runtime.Value) (*runtime.Object, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("bcl: ReadOnlyCollection<T> constructor expects a list argument")
		}
		switch {
		case args[0].Kind == runtime.KindObject && args[0].Obj != nil:
			if l, ok := args[0].Obj.Native.(*nativeList); ok {
				return &runtime.Object{Native: l}, nil
			}
		case args[0].Kind == runtime.KindArray && args[0].Arr != nil:
			return &runtime.Object{Native: &nativeList{items: args[0].Arr.Elems, typeName: "System.Collections.Generic.List`1"}}, nil
		}
		return nil, fmt.Errorf("bcl: ReadOnlyCollection<T>: unsupported backing kind %d", args[0].Kind)
	})
	register("System.Collections.ObjectModel.ReadOnlyCollection`1::get_Count", true, listCount)
	register("System.Collections.ObjectModel.ReadOnlyCollection`1::get_Item", true, listGetItem)
	register("System.Collections.ObjectModel.ReadOnlyCollection`1::GetEnumerator", true, listGetEnumerator)
}
