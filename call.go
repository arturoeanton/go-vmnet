package vmnet

import (
	"encoding/json"
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/interpreter"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

func (asm *Assembly) machine() *interpreter.Machine {
	return interpreter.New(asm.resolveByFullName, asm.resolveTypeByFullName, interpreter.DefaultLimits()).
		WithExplicitImplResolver(asm.resolveExplicitImpl).
		WithEnumResolver(asm.resolveEnumMembers)
}

// Call resolves typeName.methodName (e.g. "Rules.Engine", "Eval") and
// invokes it with args, returning its result. Call only calls static
// methods with primitive/string arguments directly — for byte[]/JSON
// payloads (e.g. an instance method or a class you don't want to model
// argument-by-argument in Go) use CallBytes/CallJSON instead.
func (asm *Assembly) Call(typeName, methodName string, args ...Value) (Value, error) {
	rtArgs := make([]runtime.Value, len(args))
	for i, a := range args {
		rtArgs[i] = a.toRuntime()
	}

	method, err := asm.resolveMethod(typeName, methodName, rtArgs)
	if err != nil {
		return nil, fmt.Errorf("vmnet: %w", err)
	}
	if method.HasThis {
		return nil, fmt.Errorf("vmnet: %s.%s is an instance method; Call only invokes static methods", typeName, methodName)
	}
	if len(args) != method.ParamCount {
		return nil, fmt.Errorf("vmnet: %s.%s expects %d argument(s), got %d", typeName, methodName, method.ParamCount, len(args))
	}

	result, err := asm.machine().Invoke(method, rtArgs)
	if err != nil {
		return nil, fmt.Errorf("vmnet: %s.%s: %w", typeName, methodName, err)
	}
	if !method.HasReturn {
		return nil, nil
	}
	return fromRuntime(result), nil
}

// CallBytes resolves typeName.methodName as a static `byte[] X(byte[])`
// method and invokes it with input, returning its raw result (spec §25.3).
func (asm *Assembly) CallBytes(typeName, methodName string, input []byte) ([]byte, error) {
	method, err := asm.resolveMethod(typeName, methodName, []runtime.Value{runtime.Bytes(input)})
	if err != nil {
		return nil, fmt.Errorf("vmnet: %w", err)
	}
	if method.HasThis {
		return nil, fmt.Errorf("vmnet: %s.%s is an instance method; CallBytes only invokes static methods", typeName, methodName)
	}
	if method.ParamCount != 1 {
		return nil, fmt.Errorf("vmnet: %s.%s must take exactly one byte[] argument for CallBytes", typeName, methodName)
	}

	result, err := asm.machine().Invoke(method, []runtime.Value{runtime.Bytes(input)})
	if err != nil {
		return nil, fmt.Errorf("vmnet: %s.%s: %w", typeName, methodName, err)
	}
	if !method.HasReturn {
		return nil, nil
	}
	if result.Kind != runtime.KindBytes {
		return nil, fmt.Errorf("vmnet: %s.%s did not return byte[]", typeName, methodName)
	}
	return result.Bytes, nil
}

// CallJSON is CallBytes with JSON marshaling on both sides (spec §25.4):
// input is marshaled to JSON, passed as UTF-8 bytes, and the method's
// returned bytes are unmarshaled back into a Go value.
func (asm *Assembly) CallJSON(typeName, methodName string, input any) (any, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("vmnet: marshaling CallJSON input: %w", err)
	}
	out, err := asm.CallBytes(typeName, methodName, data)
	if err != nil {
		return nil, err
	}
	var result any
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("vmnet: unmarshaling CallJSON result: %w", err)
	}
	return result, nil
}
