package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// Real Expression<TDelegate>.Compile() (Fase 3.64) — found via
// FluentValidation's own real PropertyRule construction, which calls
// RuleFor(x => x.Name) and then genuinely INVOKES the compiled getter
// against every object it validates (not just inspecting the tree's own
// shape the way DocumentFormat.OpenXml's ConfigureMetadata does — see
// system_linq_expressions.go's own top-of-file doc comment for that
// narrower, non-invoking use this same subsystem was originally built
// for).
//
// This does NOT implement a general expression-tree-to-IL JIT compiler
// (a much larger undertaking, still out of scope — see docs/en/
// ROADMAP.md's own "found, not fixed" note on AutoMapper's heavy
// Expression.Compile()-based mapping-plan generation, which needs
// exactly that and remains unaddressed). It works ONLY because every
// expression tree this narrow subsystem can actually construct
// (system_linq_expressions.go's own Expression.Parameter/Property/Lambda)
// is already restricted to a simple, non-branching property-access chain
// — `x => x.Prop`, `x => x.Prop1.Prop2`, never a method call, arithmetic,
// or conditional. Compile() exploits that: instead of generating and
// running real code, it returns a delegate whose invocation walks the
// SAME already-built tree node by node and reads each property via an
// ordinary property-getter call (Machine.call), the same real machinery
// backing any other property access.
func init() {
	machineRegistry["System.Linq.Expressions.Expression`1::Compile"] = expressionCompile
	machineRegistry["VmnetInternal.CompiledExpression::Invoke"] = compiledExpressionInvoke
}

// expressionCompile returns a real, invokable delegate — its FullName
// names compiledExpressionInvoke below (a sentinel, never a real BCL
// method: no real assembly ever declares "VmnetInternal.
// CompiledExpression"), and its Receiver carries the actual expression
// tree to evaluate. invokeFuncTarget (calls.go) already prepends
// Receiver as the delegate's own first call argument for any bound Func,
// which is exactly the mechanism this reuses to smuggle the tree through
// to compiledExpressionInvoke without needing any new Func field at all.
func expressionCompile(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: Expression.Compile expects a receiver")
	}
	tree := args[0]
	return runtime.FuncVal(&runtime.Func{
		FullName: "VmnetInternal.CompiledExpression::Invoke",
		Receiver: &tree,
	}), nil
}

// compiledExpressionInvoke is the compiled delegate's own real body:
// args[0] is the expression tree (smuggled through via Func.Receiver, see
// expressionCompile above), args[1] is the real target object the
// compiled getter was actually called with (`compiledGetter(customer)`).
func compiledExpressionInvoke(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("interpreter: compiled expression invoked without (tree, target)")
	}
	return evalCompiledExpression(m, args[0], args[1], depth, instrCount)
}

// evalCompiledExpression walks tree node by node against target — see
// this file's own top doc comment for why only these three node shapes
// (Lambda/Parameter/MemberAccess) ever need handling here.
func evalCompiledExpression(m *Machine, tree, target runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if body, ok := bcl.LambdaExpressionBody(tree); ok {
		return evalCompiledExpression(m, body, target, depth, instrCount)
	}
	if bcl.IsParameterExpression(tree) {
		return target, nil
	}
	if propName, inner, ok := bcl.MemberExpressionParts(tree); ok {
		receiver, err := evalCompiledExpression(m, inner, target, depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		typeName, ok := receiverTypeName(receiver)
		if !ok {
			return runtime.Value{}, fmt.Errorf("interpreter: compiled expression: can't determine %q's own receiver type", propName)
		}
		v, _, err := m.call(typeName+"::get_"+propName, []runtime.Value{receiver}, true, depth, instrCount, nil, nil)
		return v, err
	}
	return runtime.Value{}, fmt.Errorf("interpreter: compiled expression: unsupported node shape — this subsystem only evaluates a simple, non-branching property-access chain (see this file's own doc comment)")
}
