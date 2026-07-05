package runtime

// Object is a heap-allocated instance. Plain vmnet classes (Type != nil)
// use Fields; BCL-native-backed instances (List<T>, Dictionary<K,V>,
// exceptions, ...) use Native instead — see internal/bcl.
type Object struct {
	Type   *Type
	Fields []Value
	Native any

	// ClassGenericArgs holds the real, closed generic type argument
	// names this object's own declaring generic CLASS was instantiated
	// with at its `newobj` site — e.g. ["Vmnet.Fixtures.Source",
	// "Vmnet.Fixtures.Dest"] for a `new MappingExpression<Source, Dest>
	// (...)`. nil for a non-generic class, or a generic class whose
	// closed args weren't resolvable at the newobj site (see
	// ir.NewObj.ClassGenericArgs's own doc comment) — mirrors
	// Frame.MethodGenericArgs (Fase 3.60) one level up: a generic
	// method's own open type parameter lives on the CALL, a generic
	// class's own lives on the OBJECT, for as long as it exists (Fase
	// 3.66, found via AutoMapper's own MappingExpressionBase`3<TSource,
	// TDestination,...> reading `typeof(TSource)`/`typeof(TDestination)`
	// — class-level MVAR generic parameters, `!N` not `!!N` — inside its
	// own constructor to build the real TypePair a TypeMap gets
	// registered under; these always used to resolve to "" since vmnet's
	// type-erased generics had no per-instance answer for them at all).
	ClassGenericArgs []string
}
