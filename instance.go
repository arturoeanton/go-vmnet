package vmnet

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// Instance is a live handle to a constructed object (a class instance or
// a value type), returned by Assembly.New or by any Call/Instance.Call
// whose result is itself an object — e.g. Jint's `Engine.Evaluate`
// returning a `JsValue`. It implements Value, so it can be passed back
// into another Call/New as an argument, or chained straight into another
// Instance.Call.
//
// This is what lets host code drive an instance-heavy API (construct an
// object, call methods on it, use what those methods return) directly
// from Go — see examples/jint-demo for the same Jint.Engine driven both
// this way and via a compiled C# wrapper.
type Instance struct {
	asm      *Assembly
	typeName string
	value    runtime.Value
}

// Native returns nil: an Instance has no meaningful Go-native
// representation (unlike a primitive Value) — call one of its own
// methods (e.g. ToString/AsNumber for a Jint JsValue) to extract one.
func (in *Instance) Native() any              { return nil }
func (in *Instance) toRuntime() runtime.Value { return in.value }

// TypeName returns the instance's concrete type's full name
// ("Namespace.Type", or "Namespace.Type+Nested" for a nested type) —
// what New named it, or what the returning call's own receiver resolved
// to for a chained result.
func (in *Instance) TypeName() string { return in.typeName }

// New constructs an instance of typeName (e.g. "Jint.Engine") via a real
// newobj + its resolved .ctor overload — picked by arity and argument
// Kind against args, exactly like Call picks a static method overload
// (see assembly.go's pickMethodOverload's doc comment). typeName can
// name a type in this Assembly or in any of its attached dependencies
// (WithDependencies/LoadPackage, Fase 3.27).
func (asm *Assembly) New(typeName string, args ...Value) (*Instance, error) {
	rtArgs := make([]runtime.Value, len(args))
	for i, a := range args {
		rtArgs[i] = a.toRuntime()
	}
	v, err := asm.machine().New(typeName, rtArgs)
	if err != nil {
		return nil, fmt.Errorf("vmnet: new %s: %w", typeName, err)
	}
	return &Instance{asm: asm, typeName: typeName, value: v}, nil
}

// Call invokes methodName as an instance method on the receiver,
// resolved by arity and argument Kind (like Assembly.Call), dispatched
// as a real virtual call: the receiver's actual concrete type is tried
// first, walking its full inheritance chain, before falling back to
// in's own declared type name — see Machine.call's doc comment
// (internal/interpreter/calls.go, Fase 3.27) for why that matters (a
// base class's own same-named method can resolve successfully while
// still being the wrong one to run).
func (in *Instance) Call(methodName string, args ...Value) (Value, error) {
	rtArgs := make([]runtime.Value, 0, len(args)+1)
	rtArgs = append(rtArgs, in.value)
	for _, a := range args {
		rtArgs = append(rtArgs, a.toRuntime())
	}
	result, hasReturn, err := in.asm.machine().CallInstance(in.typeName+"::"+methodName, rtArgs)
	if err != nil {
		return nil, fmt.Errorf("vmnet: %s.%s: %w", in.typeName, methodName, err)
	}
	if !hasReturn {
		return nil, nil
	}
	return wrapResult(in.asm, result), nil
}

// wrapResult turns a raw runtime.Value into the Value a host caller can
// actually use: fromRuntime already handles every primitive Kind: an
// object or a struct — the two shapes a live method call can return
// that fromRuntime doesn't — becomes an *Instance, so the caller can
// keep chaining Call on it (in.Call("Evaluate", ...) -> a JsValue ->
// .Call("ToString")). Every other Kind (Null, Array, Func, ...) has no
// host-side representation yet and falls back to nil, same as
// fromRuntime's own default — silently losing that value is no worse
// than what Call already did before Instance existed.
func wrapResult(asm *Assembly, v runtime.Value) Value {
	if simple := fromRuntime(v); simple != nil {
		return simple
	}
	switch v.Kind {
	case runtime.KindObject:
		if v.Obj == nil {
			return nil
		}
		return &Instance{asm: asm, typeName: objectTypeName(v.Obj), value: v}
	case runtime.KindStruct:
		if v.Struct == nil || v.Struct.Type == nil {
			return nil
		}
		return &Instance{asm: asm, typeName: qualifiedOrPlain(v.Struct.Type), value: v}
	default:
		return nil
	}
}

// objectTypeName names obj's real type — a plugin/dependency class
// (obj.Type, a real TypeDef) or a BCL-native-backed one (obj.Native,
// e.g. List<T>/Dictionary<K,V> — the same hand-maintained name table
// their own register() calls use, bcl.NativeTypeName).
func objectTypeName(obj *runtime.Object) string {
	if obj.Type != nil {
		return qualifiedOrPlain(obj.Type)
	}
	if name, ok := bcl.NativeTypeName(obj.Native); ok {
		return name
	}
	return ""
}

// qualifiedOrPlain mirrors assembly.go's qualifiedOrPlainName: prefers
// t.QualifiedName (the real "+"-nested full name — needed since two
// different nested types can share the same bare Name/Namespace, Fase
// 3.17) and falls back to Namespace+"."+Name.
func qualifiedOrPlain(t *runtime.Type) string {
	if t.QualifiedName != "" {
		return t.QualifiedName
	}
	if t.Namespace == "" {
		return t.Name
	}
	return t.Namespace + "." + t.Name
}
