package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// System.Collections.ObjectModel.Collection<T>'s public mutators
// (Add/Insert/RemoveAt/Clear/set_Item/Remove), Fase 3.50 — split out of
// internal/bcl/system_collection_objectmodel.go (Fase 3.44) because real
// virtual dispatch to a subclass's protected InsertItem/RemoveItem/
// SetItem/ClearItems override needs Machine.call, which a plain
// bcl.Native (no Machine access) can't do. See that file's own doc
// comment for the full real-world bug (Newtonsoft.Json's
// JPropertyKeyedCollection) this fixes.
//
// Each wrapper below mirrors real Collection<T>'s own reference-source
// shape exactly: Add/Insert compute the affected index and virtually
// call InsertItem(index, item); RemoveAt/Remove call RemoveItem(index);
// Clear calls ClearItems(); the indexer setter calls SetItem(index,
// item). Because these are virtual calls (m.call's 3rd arg), the
// receiver's OWN concrete type is tried first (internal/interpreter/
// calls.go's ancestor walk) — a real subclass override (interpreted IL,
// e.g. JPropertyKeyedCollection.InsertItem) runs before ever falling
// back to bcl's own base-case native (InsertItem/RemoveItem/SetItem/
// ClearItems, registered in system_collection_objectmodel.go), exactly
// matching real Collection<T>.Add calling `this.InsertItem(...)`.
func init() {
	machineRegistry["System.Collections.ObjectModel.Collection`1::Add"] = collectionAdd
	machineRegistry["System.Collections.ObjectModel.Collection`1::Insert"] = collectionInsert
	machineRegistry["System.Collections.ObjectModel.Collection`1::RemoveAt"] = collectionRemoveAt
	machineRegistry["System.Collections.ObjectModel.Collection`1::Clear"] = collectionClear
	machineRegistry["System.Collections.ObjectModel.Collection`1::set_Item"] = collectionSetItem
	machineRegistry["System.Collections.ObjectModel.Collection`1::Remove"] = collectionRemove
}

// collectionHookNames are the 4 fullNames a virtual dispatch below
// targets — always named on Collection`1 itself (real Collection<T>'s
// own declaring type for these 4 protected virtuals), regardless of the
// receiver's actual concrete type: Machine.call's own virtual-dispatch
// ancestor walk (calls.go) is what finds a more-derived override, not
// the name passed in here.
const (
	collectionInsertItemName  = "System.Collections.ObjectModel.Collection`1::InsertItem"
	collectionRemoveItemName  = "System.Collections.ObjectModel.Collection`1::RemoveItem"
	collectionSetItemHookName = "System.Collections.ObjectModel.Collection`1::SetItem"
	collectionClearItemsName  = "System.Collections.ObjectModel.Collection`1::ClearItems"
)

// collectionReceiverLen reads a Collection<T>-shaped receiver's current
// item count directly off its nativeList backing (bcl.NativeListItems) —
// needed by Add (real Collection<T>.Add inserts at Count) and Remove
// (IndexOf's search range) without duplicating nativeList's definition
// here (it's unexported to bcl on purpose, same reasoning as every other
// cross-package native-backing access in this file's siblings).
func collectionReceiverLen(receiver runtime.Value) (int, error) {
	if receiver.Kind != runtime.KindObject || receiver.Obj == nil {
		return 0, fmt.Errorf("interpreter: Collection<T> method called without a receiver")
	}
	items, ok := bcl.NativeListItems(receiver.Obj.Native)
	if !ok {
		return 0, fmt.Errorf("interpreter: receiver is not a Collection<T>")
	}
	return len(items), nil
}

// collectionAdd backs Collection<T>.Add(T item): real reference source is
// `Insert(Count, item)` phrased directly as `InsertItem(Count, item)`
// (Collection<T> has no separate virtual Add hook — Add and Insert both
// terminate at InsertItem).
func collectionAdd(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: Collection<T>.Add expects 1 argument, got %d", len(args)-1)
	}
	count, err := collectionReceiverLen(args[0])
	if err != nil {
		return runtime.Value{}, err
	}
	_, _, err = m.call(collectionInsertItemName, []runtime.Value{args[0], runtime.Int32(int32(count)), args[1]}, true, depth, instrCount, nil, nil)
	return runtime.Value{}, err
}

// collectionInsert backs Collection<T>.Insert(int index, T item).
func collectionInsert(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 3 || args[1].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("interpreter: Collection<T>.Insert expects an int32 index")
	}
	_, _, err := m.call(collectionInsertItemName, []runtime.Value{args[0], args[1], args[2]}, true, depth, instrCount, nil, nil)
	return runtime.Value{}, err
}

// collectionRemoveAt backs Collection<T>.RemoveAt(int index).
func collectionRemoveAt(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 || args[1].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("interpreter: Collection<T>.RemoveAt expects an int32 index")
	}
	_, _, err := m.call(collectionRemoveItemName, []runtime.Value{args[0], args[1]}, true, depth, instrCount, nil, nil)
	return runtime.Value{}, err
}

// collectionClear backs Collection<T>.Clear().
func collectionClear(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: Collection<T>.Clear expects no arguments")
	}
	_, _, err := m.call(collectionClearItemsName, []runtime.Value{args[0]}, true, depth, instrCount, nil, nil)
	return runtime.Value{}, err
}

// collectionSetItem backs Collection<T>'s indexer setter (this[index] =
// item).
func collectionSetItem(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 3 || args[1].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("interpreter: Collection<T> indexer setter expects an int32 index")
	}
	_, _, err := m.call(collectionSetItemHookName, []runtime.Value{args[0], args[1], args[2]}, true, depth, instrCount, nil, nil)
	return runtime.Value{}, err
}

// collectionRemove backs Collection<T>.Remove(T item): real reference
// source is `int index = IndexOf(item); if (index<0) return false;
// RemoveItem(index); return true;` — IndexOf here uses the same
// reference/value equality every other Remove overload in this codebase
// shares (bcl.ValuesEqual), since Collection<T>'s own real IndexOf just
// delegates to the backing list's default equality comparer.
func collectionRemove(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: Collection<T>.Remove expects 1 argument, got %d", len(args)-1)
	}
	if args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: Collection<T>.Remove called without a receiver")
	}
	items, ok := bcl.NativeListItems(args[0].Obj.Native)
	if !ok {
		return runtime.Value{}, fmt.Errorf("interpreter: receiver is not a Collection<T>")
	}
	index := -1
	for i, item := range items {
		if bcl.ValuesEqual(item, args[1]) {
			index = i
			break
		}
	}
	if index < 0 {
		return runtime.Bool(false), nil
	}
	if _, _, err := m.call(collectionRemoveItemName, []runtime.Value{args[0], runtime.Int32(int32(index))}, true, depth, instrCount, nil, nil); err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(true), nil
}
