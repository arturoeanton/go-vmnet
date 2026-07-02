package runtime

// Object is a heap-allocated instance. Plain vmnet classes (Type != nil)
// use Fields; BCL-native-backed instances (List<T>, Dictionary<K,V>,
// exceptions, ...) use Native instead — see internal/bcl.
type Object struct {
	Type   *Type
	Fields []Value
	Native any
}
