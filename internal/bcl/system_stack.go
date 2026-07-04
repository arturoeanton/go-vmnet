package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// nativeStack backs Stack<T> — items[len-1] is the top, matching Push/
// Pop/Peek's LIFO order directly off a Go slice append/truncate. Also
// backs the legacy, non-generic System.Collections.Stack (Fase 3.39,
// same reasoning as nativeList/nativeDict's own typeName field: found
// via a real, load-bearing case, NPOI's own formula rendering needing a
// real Stack once the AreaPtg overload-resolution bug — see
// valueIsAssignableToTypeName's doc comment — stopped masking it).
type nativeStack struct {
	items    []runtime.Value
	typeName string
}

// stackEnumeratorType/legacyStackEnumeratorType mirror queueEnumeratorType
// exactly (see its own doc comment for why the exact real struct name
// matters — a direct `foreach (var x in stack)` calls this concrete
// type's MoveNext/get_Current non-virtually), one per real BCL type this
// same nativeStack backs.
var stackEnumeratorType = runtime.NewValueType(
	"System.Collections.Generic", "Stack`1+Enumerator",
	[]string{"stack", "index"},
	[]runtime.Value{runtime.Null(), runtime.Int32(-1)},
)

var legacyStackEnumeratorType = runtime.NewValueType(
	"System.Collections", "Stack+StackEnumerator",
	[]string{"stack", "index"},
	[]runtime.Value{runtime.Null(), runtime.Int32(-1)},
)

func init() {
	registerCtor("System.Collections.Generic.Stack`1", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeStack{typeName: "System.Collections.Generic.Stack`1"}}, nil
	})
	register("System.Collections.Generic.Stack`1::Push", false, stackPush)
	register("System.Collections.Generic.Stack`1::Pop", true, stackPop)
	register("System.Collections.Generic.Stack`1::Peek", true, stackPeek)
	register("System.Collections.Generic.Stack`1::get_Count", true, stackCount)
	register("System.Collections.Generic.Stack`1::Clear", false, stackClear)
	register("System.Collections.Generic.Stack`1::Contains", true, stackContains)
	register("System.Collections.Generic.Stack`1::TryPop", true, stackTryPop)
	register("System.Collections.Generic.Stack`1::TryPeek", true, stackTryPeek)
	register("System.Collections.Generic.Stack`1::GetEnumerator", true, stackGetEnumerator)
	register("System.Collections.Generic.Stack`1+Enumerator::MoveNext", true, stackEnumeratorMoveNext)
	register("System.Collections.Generic.Stack`1+Enumerator::get_Current", true, stackEnumeratorGetCurrent)

	registerCtor("System.Collections.Stack", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeStack{typeName: "System.Collections.Stack"}}, nil
	})
	register("System.Collections.Stack::Push", false, stackPush)
	register("System.Collections.Stack::Pop", true, stackPop)
	register("System.Collections.Stack::Peek", true, stackPeek)
	register("System.Collections.Stack::get_Count", true, stackCount)
	register("System.Collections.Stack::Clear", false, stackClear)
	register("System.Collections.Stack::Contains", true, stackContains)
	register("System.Collections.Stack::ToArray", true, stackToArray)
	register("System.Collections.Stack::GetEnumerator", true, stackGetEnumerator)
	register("System.Collections.Stack+StackEnumerator::MoveNext", true, stackEnumeratorMoveNext)
	register("System.Collections.Stack+StackEnumerator::get_Current", true, stackEnumeratorGetCurrent)
}

// stackToArray returns items top-to-bottom, matching real Stack.ToArray
// semantics (index 0 is the top, unlike the internal items slice which
// keeps the top at the end for O(1) push/pop).
func stackToArray(args []runtime.Value) (runtime.Value, error) {
	s, err := asStack(args)
	if err != nil {
		return runtime.Value{}, err
	}
	n := len(s.items)
	out := make([]runtime.Value, n)
	for i, v := range s.items {
		out[n-1-i] = v
	}
	return runtime.ArrRef(&runtime.Array{Elems: out}), nil
}

func stackClear(args []runtime.Value) (runtime.Value, error) {
	s, err := asStack(args)
	if err != nil {
		return runtime.Value{}, err
	}
	s.items = nil
	return runtime.Value{}, nil
}

func stackContains(args []runtime.Value) (runtime.Value, error) {
	s, err := asStack(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: Stack.Contains expects 1 argument")
	}
	for _, item := range s.items {
		if valuesEqual(item, args[1]) {
			return runtime.Bool(true), nil
		}
	}
	return runtime.Bool(false), nil
}

func asStack(args []runtime.Value) (*nativeStack, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return nil, fmt.Errorf("bcl: Stack method called without a receiver")
	}
	s, ok := args[0].Obj.Native.(*nativeStack)
	if !ok {
		return nil, fmt.Errorf("bcl: receiver is not a Stack")
	}
	return s, nil
}

func stackPush(args []runtime.Value) (runtime.Value, error) {
	s, err := asStack(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: Stack.Push expects 1 argument")
	}
	s.items = append(s.items, args[1])
	return runtime.Value{}, nil
}

func stackPop(args []runtime.Value) (runtime.Value, error) {
	s, err := asStack(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(s.items) == 0 {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.InvalidOperationException", Message: "Stack empty."}
	}
	top := s.items[len(s.items)-1]
	s.items = s.items[:len(s.items)-1]
	return top, nil
}

func stackPeek(args []runtime.Value) (runtime.Value, error) {
	s, err := asStack(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(s.items) == 0 {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.InvalidOperationException", Message: "Stack empty."}
	}
	return s.items[len(s.items)-1], nil
}

func stackCount(args []runtime.Value) (runtime.Value, error) {
	s, err := asStack(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(int32(len(s.items))), nil
}

// stackTryPop/stackTryPeek are Pop/Peek's non-throwing counterparts (same
// `out`-by-managed-pointer mechanism as Queue<T>.TryDequeue/TryPeek) —
// missing entirely until probed against a hand-written fixture.
func stackTryPop(args []runtime.Value) (runtime.Value, error) {
	s, err := asStack(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: Stack.TryPop expects an out param")
	}
	if len(s.items) == 0 {
		return runtime.Bool(false), nil
	}
	top := s.items[len(s.items)-1]
	s.items = s.items[:len(s.items)-1]
	if args[1].Kind == runtime.KindRef && args[1].Ref != nil {
		*args[1].Ref = top
	}
	return runtime.Bool(true), nil
}

func stackTryPeek(args []runtime.Value) (runtime.Value, error) {
	s, err := asStack(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: Stack.TryPeek expects an out param")
	}
	if len(s.items) == 0 {
		return runtime.Bool(false), nil
	}
	if args[1].Kind == runtime.KindRef && args[1].Ref != nil {
		*args[1].Ref = s.items[len(s.items)-1]
	}
	return runtime.Bool(true), nil
}

// stackGetEnumerator backs both a direct `foreach (var x in stack)` and
// LINQ over a Stack<T> reached through IEnumerable`1. Missing entirely
// until probed against a hand-written fixture. Stack<T> enumerates
// LIFO — most-recently-pushed first, the OPPOSITE of the items slice's
// own storage order (items[len-1] is the top, kept there for O(1) Push/
// Pop) — so, unlike queueGetEnumerator's plain copy, this snapshot must
// reverse the slice before handing it to List<T>'s own enumerator
// machinery (listGetEnumerator); getting this backwards would silently
// enumerate a Stack<T> oldest-pushed-first, the opposite of every real
// `foreach (var x in stack)`.
func stackGetEnumerator(args []runtime.Value) (runtime.Value, error) {
	s, err := asStack(args)
	if err != nil {
		return runtime.Value{}, err
	}
	// Real (as opposed to List<T>'s borrowed) Stack`1+Enumerator/
	// Stack+StackEnumerator struct — see queueEnumeratorType's own doc
	// comment for why the exact type name matters (a direct `foreach
	// (var x in stack)` calls it non-virtually by name). typeName picks
	// between the two real BCL types this same nativeStack backs.
	t := stackEnumeratorType
	if s.typeName == "System.Collections.Stack" {
		t = legacyStackEnumeratorType
	}
	st := runtime.NewStruct(t)
	st.Fields[0] = args[0]
	return runtime.StructVal(st), nil
}

// stackEnumeratorMoveNext/stackEnumeratorGetCurrent back both real stack
// enumerator struct types (registered separately, under each one's own
// concrete name, but sharing this same implementation — the field
// layout is identical either way). Stack<T> enumerates LIFO — top
// first — the OPPOSITE of the items slice's own storage order (items
// [len-1] is the top, kept there for O(1) Push/Pop), so index i (0 at
// the top) maps to items[len-1-i], live off the receiver exactly like
// List<T>/HashSet<T>/Queue<T>'s own enumerators (not a snapshot).
func stackEnumeratorMoveNext(args []runtime.Value) (runtime.Value, error) {
	st, err := derefStructReceiver(args, "Stack.Enumerator", "Stack.Enumerator.MoveNext")
	if err != nil {
		return runtime.Value{}, err
	}
	s, err := asStack([]runtime.Value{st.Fields[0]})
	if err != nil {
		return runtime.Value{}, err
	}
	next := st.Fields[1].I4 + 1
	st.Fields[1] = runtime.Int32(next)
	return runtime.Bool(int(next) < len(s.items)), nil
}

func stackEnumeratorGetCurrent(args []runtime.Value) (runtime.Value, error) {
	st, err := derefStructReceiver(args, "Stack.Enumerator", "Stack.Enumerator.Current")
	if err != nil {
		return runtime.Value{}, err
	}
	s, err := asStack([]runtime.Value{st.Fields[0]})
	if err != nil {
		return runtime.Value{}, err
	}
	idx := int(st.Fields[1].I4)
	if idx < 0 || idx >= len(s.items) {
		return runtime.Value{}, fmt.Errorf("bcl: Stack.Enumerator.Current: index %d out of range", idx)
	}
	return s.items[len(s.items)-1-idx], nil
}
