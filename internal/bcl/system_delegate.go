package bcl

import (
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

func init() {
	register("System.Delegate::Combine", true, delegateCombine)
	register("System.Delegate::Remove", true, delegateRemove)
	register("System.Delegate::op_Equality", true, delegateEquality)
	register("System.Delegate::op_Inequality", true, delegateInequality)
}

// delegateFuncs flattens a runtime.Func (plus its Chain, Fase 3.24) into
// its real invocation list, in order — used by both Combine (concatenate
// two lists) and Remove (delete the last matching run from one list),
// mirroring real MulticastDelegate semantics.
func delegateFuncs(v runtime.Value) []*runtime.Func {
	if v.Kind != runtime.KindFunc || v.Func == nil {
		return nil
	}
	list := make([]*runtime.Func, 0, 1+len(v.Func.Chain))
	head := *v.Func
	head.Chain = nil
	list = append(list, &head)
	list = append(list, v.Func.Chain...)
	return list
}

func funcsToValue(list []*runtime.Func) runtime.Value {
	if len(list) == 0 {
		return runtime.Null()
	}
	head := *list[0]
	head.Chain = list[1:]
	return runtime.FuncVal(&head)
}

// delegateCombine backs Delegate.Combine(Delegate, Delegate): either
// operand can be a real .NET null (no combined delegates yet), matching
// the common `handler = (EventHandler)Delegate.Combine(handler, value)`
// pattern used when a compiler can't statically prove the target isn't
// already null.
func delegateCombine(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, nil
	}
	a, b := delegateFuncs(args[0]), delegateFuncs(args[1])
	if len(a) == 0 {
		return funcsToValue(b), nil
	}
	if len(b) == 0 {
		return funcsToValue(a), nil
	}
	return funcsToValue(append(append([]*runtime.Func{}, a...), b...)), nil
}

// delegateRemove backs Delegate.Remove(Delegate, Delegate): removes the
// last occurrence of value's invocation list from source's, matching real
// semantics for the overwhelmingly common case (value is a single
// non-combined delegate, e.g. `handler -= OnClick`).
func delegateRemove(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, nil
	}
	source, value := delegateFuncs(args[0]), delegateFuncs(args[1])
	if len(source) == 0 || len(value) == 0 {
		return funcsToValue(source), nil
	}
	for start := len(source) - len(value); start >= 0; start-- {
		if funcsEqualRun(source[start:start+len(value)], value) {
			out := append([]*runtime.Func{}, source[:start]...)
			out = append(out, source[start+len(value):]...)
			return funcsToValue(out), nil
		}
	}
	return funcsToValue(source), nil
}

// delegateEquality backs Delegate.op_Equality(Delegate, Delegate) —
// real semantics: both null, or both non-null with the exact same
// invocation list (same length, same targets in the same order), unlike
// delegateRemove's funcsEqualRun use which only needs a matching sub-run.
func delegateEquality(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Bool(false), nil
	}
	a, b := delegateFuncs(args[0]), delegateFuncs(args[1])
	if len(a) != len(b) {
		return runtime.Bool(false), nil
	}
	return runtime.Bool(funcsEqualRun(a, b)), nil
}

func delegateInequality(args []runtime.Value) (runtime.Value, error) {
	v, err := delegateEquality(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(!v.Truthy()), nil
}

func funcsEqualRun(a, b []*runtime.Func) bool {
	for i := range a {
		if a[i].FullName != b[i].FullName {
			return false
		}
		if (a[i].Receiver == nil) != (b[i].Receiver == nil) {
			return false
		}
		if a[i].Receiver != nil && !valuesEqual(*a[i].Receiver, *b[i].Receiver) {
			return false
		}
	}
	return true
}
