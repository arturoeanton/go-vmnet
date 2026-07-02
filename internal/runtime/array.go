package runtime

// Array is a heap-allocated, fixed-length CLI array (SZARRAY only —
// vmnet doesn't model multi-dimensional arrays). Elems is an ordinary Go
// slice; CLI reference semantics (two variables that alias the same
// array see each other's writes) fall out for free since copying a Value
// that wraps *Array only copies the pointer, not the backing storage.
type Array struct {
	Elems []Value
}

func NewArray(length int) *Array {
	return &Array{Elems: make([]Value, length)}
}
