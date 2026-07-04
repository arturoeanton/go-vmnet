package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// nativeLinkedList backs System.Collections.Generic.LinkedList`1 — a real
// doubly-linked list, needed (Fase 3.40) because its node-identity-based
// Remove(LinkedListNode<T>) is exactly what System.IO.Packaging's own
// OrderedDictionary<TKey,TValue> relies on for O(1) removal (a
// Dictionary<TKey,LinkedListNode<TValue>> pairs each key with the node
// holding its value in a separate insertion-ordered LinkedList<TValue>).
// Kept as a plain doubly-linked chain of *linkedListNode (not a slice)
// specifically so a node reference stays valid across insertions/removals
// elsewhere in the list — a slice index would silently go stale.
type nativeLinkedList struct {
	head, tail *linkedListNode
	count      int
}

// linkedListNode backs System.Collections.Generic.LinkedListNode`1 — only
// ever constructed by nativeLinkedList's own AddFirst/AddLast (real
// LinkedList<T> also allows constructing a detached node and adding it
// later, a shape no real caller in this loop's target packages uses).
type linkedListNode struct {
	value      runtime.Value
	list       *nativeLinkedList
	prev, next *linkedListNode
}

func init() {
	registerCtor("System.Collections.Generic.LinkedList`1", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeLinkedList{}}, nil
	})
	register("System.Collections.Generic.LinkedList`1::AddLast", true, linkedListAddLast)
	register("System.Collections.Generic.LinkedList`1::AddFirst", true, linkedListAddFirst)
	register("System.Collections.Generic.LinkedList`1::Remove", true, linkedListRemove)
	register("System.Collections.Generic.LinkedList`1::RemoveFirst", false, linkedListRemoveFirst)
	register("System.Collections.Generic.LinkedList`1::RemoveLast", false, linkedListRemoveLast)
	register("System.Collections.Generic.LinkedList`1::Clear", false, linkedListClear)
	register("System.Collections.Generic.LinkedList`1::Contains", true, linkedListContains)
	register("System.Collections.Generic.LinkedList`1::get_Count", true, linkedListGetCount)
	register("System.Collections.Generic.LinkedList`1::get_First", true, linkedListGetFirst)
	register("System.Collections.Generic.LinkedList`1::get_Last", true, linkedListGetLast)
	register("System.Collections.Generic.LinkedList`1::GetEnumerator", true, linkedListGetEnumerator)

	register("System.Collections.Generic.LinkedListNode`1::get_Value", true, linkedListNodeGetValue)
	register("System.Collections.Generic.LinkedListNode`1::get_Next", true, linkedListNodeGetNext)
	register("System.Collections.Generic.LinkedListNode`1::get_Previous", true, linkedListNodeGetPrevious)
}

func asLinkedList(args []runtime.Value) (*nativeLinkedList, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return nil, fmt.Errorf("bcl: LinkedList method called without a receiver")
	}
	ll, ok := args[0].Obj.Native.(*nativeLinkedList)
	if !ok {
		return nil, fmt.Errorf("bcl: receiver is not a LinkedList")
	}
	return ll, nil
}

func asLinkedListNode(v runtime.Value) (*linkedListNode, bool) {
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	if v.Kind != runtime.KindObject || v.Obj == nil {
		return nil, false
	}
	n, ok := v.Obj.Native.(*linkedListNode)
	return n, ok
}

func linkedListAddLast(args []runtime.Value) (runtime.Value, error) {
	ll, err := asLinkedList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: LinkedList.AddLast expects a value")
	}
	n := &linkedListNode{value: args[1], list: ll, prev: ll.tail}
	if ll.tail != nil {
		ll.tail.next = n
	} else {
		ll.head = n
	}
	ll.tail = n
	ll.count++
	return runtime.ObjRef(&runtime.Object{Native: n}), nil
}

func linkedListAddFirst(args []runtime.Value) (runtime.Value, error) {
	ll, err := asLinkedList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: LinkedList.AddFirst expects a value")
	}
	n := &linkedListNode{value: args[1], list: ll, next: ll.head}
	if ll.head != nil {
		ll.head.prev = n
	} else {
		ll.tail = n
	}
	ll.head = n
	ll.count++
	return runtime.ObjRef(&runtime.Object{Native: n}), nil
}

func (ll *nativeLinkedList) unlink(n *linkedListNode) {
	if n.prev != nil {
		n.prev.next = n.next
	} else {
		ll.head = n.next
	}
	if n.next != nil {
		n.next.prev = n.prev
	} else {
		ll.tail = n.prev
	}
	n.prev, n.next, n.list = nil, nil, nil
	ll.count--
}

// linkedListRemove backs both real overloads — Remove(T value) (a linear
// value search, returns bool) and Remove(LinkedListNode<T> node) (O(1)
// node-identity removal, void) — dispatched by args[1]'s own shape, same
// convention every other multi-overload native in this package uses.
func linkedListRemove(args []runtime.Value) (runtime.Value, error) {
	ll, err := asLinkedList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: LinkedList.Remove expects a value or node")
	}
	if n, ok := asLinkedListNode(args[1]); ok && n.list == ll {
		ll.unlink(n)
		return runtime.Value{}, nil
	}
	for n := ll.head; n != nil; n = n.next {
		if valuesEqual(n.value, args[1]) {
			ll.unlink(n)
			return runtime.Bool(true), nil
		}
	}
	return runtime.Bool(false), nil
}

func linkedListRemoveFirst(args []runtime.Value) (runtime.Value, error) {
	ll, err := asLinkedList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if ll.head != nil {
		ll.unlink(ll.head)
	}
	return runtime.Value{}, nil
}

func linkedListRemoveLast(args []runtime.Value) (runtime.Value, error) {
	ll, err := asLinkedList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if ll.tail != nil {
		ll.unlink(ll.tail)
	}
	return runtime.Value{}, nil
}

func linkedListClear(args []runtime.Value) (runtime.Value, error) {
	ll, err := asLinkedList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	ll.head, ll.tail, ll.count = nil, nil, 0
	return runtime.Value{}, nil
}

func linkedListContains(args []runtime.Value) (runtime.Value, error) {
	ll, err := asLinkedList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 {
		return runtime.Bool(false), nil
	}
	for n := ll.head; n != nil; n = n.next {
		if valuesEqual(n.value, args[1]) {
			return runtime.Bool(true), nil
		}
	}
	return runtime.Bool(false), nil
}

func linkedListGetCount(args []runtime.Value) (runtime.Value, error) {
	ll, err := asLinkedList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(int32(ll.count)), nil
}

func linkedListGetFirst(args []runtime.Value) (runtime.Value, error) {
	ll, err := asLinkedList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if ll.head == nil {
		return runtime.Null(), nil
	}
	return runtime.ObjRef(&runtime.Object{Native: ll.head}), nil
}

func linkedListGetLast(args []runtime.Value) (runtime.Value, error) {
	ll, err := asLinkedList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if ll.tail == nil {
		return runtime.Null(), nil
	}
	return runtime.ObjRef(&runtime.Object{Native: ll.tail}), nil
}

// linkedListGetEnumerator materializes a snapshot List<T> and reuses its
// own enumerator machinery — simpler and no less correct than a bespoke
// LinkedList enumerator, since real code here never mutates a list while
// actively enumerating it.
func linkedListGetEnumerator(args []runtime.Value) (runtime.Value, error) {
	ll, err := asLinkedList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	items := make([]runtime.Value, 0, ll.count)
	for n := ll.head; n != nil; n = n.next {
		items = append(items, n.value)
	}
	snapshot := runtime.ObjRef(&runtime.Object{Native: &nativeList{items: items, typeName: "System.Collections.Generic.List`1"}})
	return listGetEnumerator([]runtime.Value{snapshot})
}

func linkedListNodeGetValue(args []runtime.Value) (runtime.Value, error) {
	n, ok := asLinkedListNode(args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: receiver is not a LinkedListNode")
	}
	return n.value, nil
}

func linkedListNodeGetNext(args []runtime.Value) (runtime.Value, error) {
	n, ok := asLinkedListNode(args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: receiver is not a LinkedListNode")
	}
	if n.next == nil {
		return runtime.Null(), nil
	}
	return runtime.ObjRef(&runtime.Object{Native: n.next}), nil
}

func linkedListNodeGetPrevious(args []runtime.Value) (runtime.Value, error) {
	n, ok := asLinkedListNode(args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: receiver is not a LinkedListNode")
	}
	if n.prev == nil {
		return runtime.Null(), nil
	}
	return runtime.ObjRef(&runtime.Object{Native: n.prev}), nil
}
