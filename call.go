package vmnet

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/interpreter"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// Call resolves typeName.methodName (e.g. "Rules.Engine", "Eval") and
// invokes it with args, returning its result. Fase 1 only supports static
// methods — instance methods, callvirt and object construction land in
// Fase 2 (see docs/ROADMAP.md).
func (asm *Assembly) Call(typeName, methodName string, args ...Value) (Value, error) {
	method, err := asm.resolveMethod(typeName, methodName)
	if err != nil {
		return nil, fmt.Errorf("vmnet: %w", err)
	}
	if method.HasThis {
		return nil, fmt.Errorf("vmnet: %s.%s is an instance method; Fase 1 only calls static methods", typeName, methodName)
	}
	if len(args) != method.ParamCount {
		return nil, fmt.Errorf("vmnet: %s.%s expects %d argument(s), got %d", typeName, methodName, method.ParamCount, len(args))
	}

	rtArgs := make([]runtime.Value, len(args))
	for i, a := range args {
		rtArgs[i] = a.toRuntime()
	}

	machine := interpreter.New(asm.resolveByFullName, interpreter.DefaultLimits())
	result, err := machine.Invoke(method, rtArgs)
	if err != nil {
		return nil, fmt.Errorf("vmnet: %s.%s: %w", typeName, methodName, err)
	}
	if !method.HasReturn {
		return nil, nil
	}
	return fromRuntime(result), nil
}
