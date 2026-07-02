// Package bcl implements the partial Base Class Library (System.*) vmnet
// ships natively in Go: types and methods that are not interpreted from IL
// but registered as native implementations (e.g. System.Math, System.String,
// System.Collections.Generic.List[T]). Coverage grows by profile — see
// docs/ROADMAP.md Fase 1-3 and docs/spec.md section 16.
package bcl

import "github.com/arturoeanton/go-vmnet/internal/runtime"

// Native is a BCL method implemented directly in Go. args holds exactly
// the arguments the IL call site pushed (including an implicit `this` as
// args[0] for instance calls) — the interpreter does the popping.
type Native func(args []runtime.Value) (runtime.Value, error)

type entry struct {
	fn        Native
	hasReturn bool
}

var registry = map[string]entry{}

func register(fullName string, hasReturn bool, fn Native) {
	registry[fullName] = entry{fn: fn, hasReturn: hasReturn}
}

// Lookup returns the native registered for fullName ("Namespace.Type::Method").
func Lookup(fullName string) (fn Native, hasReturn bool, ok bool) {
	e, ok := registry[fullName]
	return e.fn, e.hasReturn, ok
}
