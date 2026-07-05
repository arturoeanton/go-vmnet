package bcl

import (
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// System.Runtime.CompilerServices.ReadOnlyCollectionBuilder<T> (Fase
// 3.65, found via AutoMapper's own AutoMapper.Internal.PrimitiveHelper.
// ToReadOnly<T> helper: `new ReadOnlyCollectionBuilder<T>(); .Add(item);
// .ToReadOnlyCollection()`, used by ExpressionBuilder's own static
// constructor to build a one-element ReadOnlyCollection<ParameterExpression>
// for a real, working expression-tree Block).
//
// Backed by the exact same *nativeList real List<T> already uses — a
// builder is functionally just a growable buffer — with its own distinct
// typeName so NativeTypeName/dispatch still report "ReadOnlyCollectionBuilder`1"
// while it's being built. ToReadOnlyCollection() below hands back a NEW
// *nativeList wrapping the same backing array under the
// "ReadOnlyCollection`1" name, matching real .NET's own "snapshot the
// buffer into an immutable-looking wrapper" semantics closely enough for
// every real caller found so far (nothing mutates the builder after
// calling ToReadOnlyCollection()).
func init() {
	registerCtor("System.Runtime.CompilerServices.ReadOnlyCollectionBuilder`1", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeList{typeName: "System.Runtime.CompilerServices.ReadOnlyCollectionBuilder`1"}}, nil
	})
	register("System.Runtime.CompilerServices.ReadOnlyCollectionBuilder`1::Add", false, listAdd)
	register("System.Runtime.CompilerServices.ReadOnlyCollectionBuilder`1::get_Count", true, listCount)
	register("System.Runtime.CompilerServices.ReadOnlyCollectionBuilder`1::get_Item", true, listGetItem)
	register("System.Runtime.CompilerServices.ReadOnlyCollectionBuilder`1::set_Item", false, listSetItem)
	register("System.Runtime.CompilerServices.ReadOnlyCollectionBuilder`1::ToReadOnlyCollection", true, readOnlyCollectionBuilderToReadOnlyCollection)
}

func readOnlyCollectionBuilderToReadOnlyCollection(args []runtime.Value) (runtime.Value, error) {
	l, err := asList(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeList{items: l.items, typeName: "System.Collections.ObjectModel.ReadOnlyCollection`1"}}), nil
}
