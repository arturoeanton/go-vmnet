package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// System.Collections.ObjectModel.Collection<T> (Fase 3.44, general IL/
// BCL hardening pass — found via a real, load-bearing case: Newtonsoft.
// Json's own JObject/JContainer property storage is a real Collection<T>
// subclass, `JPropertyKeyedCollection : KeyedCollection<string,JToken>`,
// itself `: Collection<TItem>`) is the single most common base class real
// .NET code reaches for when it wants "a List<T> with customization
// hooks" — reused here via nativeList, the same backing every other
// concrete list-shaped type in this package already shares (List`1,
// ArrayList).
//
// Fase 3.44's own doc comment (see git history) flagged, but deliberately
// deferred, Collection<T>'s 4 protected virtual hooks (ClearItems/
// InsertItem/RemoveItem/SetItem) that real subclasses override to keep a
// side index in sync. Fase 3.50 closes that gap — found the hard way via
// examples/newtonsoft-json-demo: Newtonsoft's real `internal class
// JPropertyKeyedCollection : Collection<JToken>`
// (/tmp/nj_ns20/Newtonsoft.Json.Linq/JPropertyKeyedCollection.cs) overrides
// `protected override void InsertItem(int index, JToken item)` to also
// populate a private `Dictionary<string,JToken> _dictionary` (AddKey), the
// only thing TryGetValue/this[string] ever consult. JContainer's own
// `InsertItem(int, JToken, bool, bool)` (unrelated 4-arg internal method,
// /tmp/nj_ns20/Newtonsoft.Json.Linq/JContainer.cs:524-556) adds a property
// via `childrenTokens.Insert(index, item)` — a plain IList<T>.Insert call,
// which resolved (via Machine.call's virtual ancestor walk,
// internal/interpreter/calls.go) straight to this package's own
// bcl-native, non-virtual Collection`1::Insert, silently skipping
// JPropertyKeyedCollection's InsertItem override entirely: the property
// landed in the backing list (Count/enumeration/JToken[i] all looked
// correct) but `_dictionary` stayed nil forever, so every by-key lookup
// (JObject.this[string] -> Property(name) ->
// _properties.TryGetValue(name, ...)) silently returned null instead of
// throwing — a bare wrong answer, not a crash.
//
// The general fix: the public mutators (Add/Insert/RemoveAt/Clear/
// set_Item/Remove) no longer touch the backing list directly here — they
// now live in internal/interpreter/collection_objectmodel.go as
// Machine-aware natives that virtually dispatch to InsertItem/RemoveItem/
// SetItem/ClearItems on the RECEIVER's concrete type (exactly like real
// Collection<T>.Add calling `this.InsertItem(...)`, a virtual call), so a
// real IL override such as JPropertyKeyedCollection.InsertItem actually
// runs. That override's own `base.InsertItem(index, item)` is a plain,
// non-virtual call, which resolves right back to the 4 hook natives
// registered below (InsertItem/RemoveItem/SetItem/ClearItems) — these do
// the actual nativeList mutation Add/Insert/RemoveAt/Clear/set_Item used
// to do directly, unchanged in behavior for the (overwhelmingly common)
// case where nothing overrides the hooks at all.
func init() {
	registerCtor("System.Collections.ObjectModel.Collection`1", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeList{typeName: "System.Collections.ObjectModel.Collection`1"}}, nil
	})
	// A plugin/BCL-package class subclassing Collection<T> directly
	// (`class Foo : Collection<T> { public Foo() : base() {} }`, the
	// overwhelmingly common real-world shape — KeyedCollection<TKey,
	// TItem> itself does exactly this) chains to its base via a plain,
	// non-virtual `call Collection\`1::.ctor(this[, list])` — not
	// `newobj` (only the exact leaf type gets newobj'd; the base call
	// runs on the already-allocated derived object). Same established
	// pattern as system_collections.go's dictCtorInPlace (Fase 3.42) /
	// system_exception.go's baseExceptionCtorInPlace (Fase 3.13).
	register("System.Collections.ObjectModel.Collection`1::.ctor", false, collectionCtorInPlace)
	register("System.Collections.ObjectModel.Collection`1::get_Count", true, listCount)
	register("System.Collections.ObjectModel.Collection`1::get_Item", true, listGetItem)
	register("System.Collections.ObjectModel.Collection`1::Contains", true, listContains)
	register("System.Collections.ObjectModel.Collection`1::CopyTo", false, listCopyTo)
	register("System.Collections.ObjectModel.Collection`1::GetEnumerator", true, listGetEnumerator)
	// The 4 protected virtual hooks themselves (Fase 3.50) — real
	// Collection<T>'s own base implementation, reusing the exact same
	// raw-mutation logic Add/Insert/RemoveAt/Clear/set_Item used to run
	// directly before this fix. Add/Insert/RemoveAt/Clear/set_Item/Remove
	// are deliberately NOT registered here anymore: they're now
	// Machine-aware virtual-dispatch wrappers in internal/interpreter/
	// collection_objectmodel.go, since bcl natives (this package) have no
	// Machine access to perform a virtual call with.
	register("System.Collections.ObjectModel.Collection`1::InsertItem", false, listInsert)
	register("System.Collections.ObjectModel.Collection`1::RemoveItem", false, listRemoveAt)
	register("System.Collections.ObjectModel.Collection`1::SetItem", false, listSetItem)
	register("System.Collections.ObjectModel.Collection`1::ClearItems", false, listClear)
}

// collectionCtorInPlace covers both Collection() (empty) and Collection
// (IList<T> list) — the second copies the given list's items upfront,
// matching real Collection<T>(IList<T>)'s "wrap this exact list" vs.
// "start empty" distinction closely enough for every real caller found
// so far (none mutate the original list afterward and expect the
// wrapper to observe it live).
func collectionCtorInPlace(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return runtime.Value{}, fmt.Errorf("bcl: Collection`1 constructor called without a receiver")
	}
	typeName := "System.Collections.ObjectModel.Collection`1"
	if t := args[0].Obj.Type; t != nil {
		if t.Namespace != "" {
			typeName = t.Namespace + "." + t.Name
		} else {
			typeName = t.Name
		}
	}
	nl := &nativeList{typeName: typeName}
	if len(args) >= 2 && args[1].Kind == runtime.KindObject && args[1].Obj != nil {
		if items, ok := NativeListItems(args[1].Obj.Native); ok {
			nl.items = append(nl.items, items...)
		}
	}
	args[0].Obj.Native = nl
	return runtime.Value{}, nil
}
