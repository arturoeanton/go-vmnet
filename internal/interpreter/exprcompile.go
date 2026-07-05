package interpreter

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// Real Expression<TDelegate>.Compile() (Fase 3.64, widened Fase 3.65) —
// found via FluentValidation's own real PropertyRule construction (Fase
// 3.64: a simple, non-branching property-access chain) and, more deeply,
// Microsoft.Extensions.DependencyInjection's own ExpressionResolverBuilder
// compiled-resolution fast path and AutoMapper's own mapping-plan
// generation (Fase 3.65: real method calls, object construction, local
// variables, assignment, conditionals — a genuinely general, if still not
// exhaustive, tree-walking expression evaluator).
//
// This is deliberately NOT a general expression-tree-to-IL JIT compiler:
// nothing here ever generates or runs new machine code. Compile() returns
// a delegate whose own invocation walks the ALREADY-BUILT tree
// (internal/bcl/system_linq_expressions.go's own native node types) node
// by node, dispatching each real operation (a property read, a method
// call, a constructor call, an assignment into a Block-scoped variable,
// ...) through the SAME real Machine.call/newObj machinery an ordinary
// compiled call site already uses. Real, correct evaluation for the node
// kinds this subsystem models, without ever needing a codegen backend at
// all — vmnet has none.
func init() {
	machineRegistry["System.Linq.Expressions.Expression`1::Compile"] = expressionCompile
	machineRegistry["System.Linq.Expressions.LambdaExpression::Compile"] = expressionCompile
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
// args[0] is the lambda tree (smuggled through via Func.Receiver, see
// expressionCompile above), args[1:] are the real arguments the compiled
// delegate was actually called with — bound, in order, to the lambda's
// own ParameterExpressions before evaluating its body.
func compiledExpressionInvoke(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("interpreter: compiled expression invoked without a tree")
	}
	tree := args[0]
	realArgs := args[1:]
	env := map[*runtime.Object]runtime.Value{}
	if params, ok := bcl.LambdaExpressionParameters(tree); ok {
		for i, p := range params {
			if obj, ok := bcl.ExprNodeIdentity(p); ok && i < len(realArgs) {
				env[obj] = realArgs[i]
			}
		}
	}
	body, ok := bcl.LambdaExpressionBody(tree)
	if !ok {
		// Tolerate being handed a non-lambda node directly (e.g. a
		// re-entrant eval of an already-unwrapped body) rather than
		// erroring outright.
		body = tree
	}
	return evalExprNode(m, body, env, depth, instrCount, 0)
}

// maxExprEvalDepth bounds evalExprNode's own recursive tree walk (Fase
// 3.66, found via AutoMapper's own mapping-plan generation for a real
// object graph: unlike an ordinary interpreted method's CIL loop, which
// grows m.Limits.MaxInstructions without growing the Go call stack, this
// recursion adds one real Go stack frame per node — a genuinely large
// (or accidentally self-referential) tree can overflow the actual OS
// stack long before any of the interpreter's existing resource limits
// would trip, crashing the whole process rather than surfacing a
// graceful error). 4096 is comfortably below Go's default stack growth
// ceiling for this function's own frame size, while still generous for
// any real expression tree found so far (a few dozen levels deep at
// most).
const maxExprEvalDepth = 4096

// evalExprNode walks node against env (the current lambda parameter/
// Block-local bindings, keyed by each ParameterExpression's own identity
// — internal/bcl/system_linq_expressions.go's own ExprNodeIdentity) and
// returns its real, evaluated value. exprDepth is THIS function's own
// recursion depth (distinct from depth, the interpreter's own CIL call
// depth) — see maxExprEvalDepth's own doc comment for why it exists as
// its own, separately-tracked counter.
func evalExprNode(m *Machine, node runtime.Value, env map[*runtime.Object]runtime.Value, depth int, instrCount *int64, exprDepth int) (runtime.Value, error) {
	if exprDepth > maxExprEvalDepth {
		return runtime.Value{}, ErrStackOverflow
	}
	switch bcl.KindOfExprNode(node) {
	case bcl.ExprNodeLambda:
		// A nested lambda evaluated directly (rare outside Invoke, but
		// tolerated) — its own value IS its body's value, same as the
		// top-level compile entry point above.
		body, _ := bcl.LambdaExpressionBody(node)
		return evalExprNode(m, body, env, depth, instrCount, exprDepth+1)

	case bcl.ExprNodeParameter:
		obj, ok := bcl.ExprNodeIdentity(node)
		if !ok {
			return runtime.Value{}, fmt.Errorf("interpreter: compiled expression: parameter node has no identity")
		}
		if v, found := env[obj]; found {
			return v, nil
		}
		// An unbound variable (a Block-scoped local never assigned
		// before being read, or a parameter this specific invocation
		// didn't bind) — real default(T).
		typeName, _ := bcl.ParameterExpressionTypeName(node)
		return defaultValueForExprType(typeName), nil

	case bcl.ExprNodeConstant:
		v, _ := bcl.ConstantExpressionValue(node)
		return v, nil

	case bcl.ExprNodeMember:
		propName, inner, _ := bcl.MemberExpressionParts(node)
		receiver, err := evalExprNode(m, inner, env, depth, instrCount, exprDepth+1)
		if err != nil {
			return runtime.Value{}, err
		}
		typeName, ok := receiverTypeName(receiver)
		if !ok {
			return runtime.Value{}, fmt.Errorf("interpreter: compiled expression: can't determine %q's own receiver type", propName)
		}
		v, _, err := m.call(typeName+"::get_"+propName, []runtime.Value{receiver}, true, depth, instrCount, nil, nil)
		return v, err

	case bcl.ExprNodeCall:
		instance, typeName, methodName, argNodes, _ := bcl.CallExpressionParts(node)
		evaluatedArgs, err := evalExprList(m, argNodes, env, depth, instrCount, exprDepth+1)
		if err != nil {
			return runtime.Value{}, err
		}
		if instance.Kind == runtime.KindNull {
			v, _, err := m.call(typeName+"::"+methodName, evaluatedArgs, false, depth, instrCount, nil, nil)
			return v, err
		}
		receiver, err := evalExprNode(m, instance, env, depth, instrCount, exprDepth+1)
		if err != nil {
			return runtime.Value{}, err
		}
		callArgs := append([]runtime.Value{receiver}, evaluatedArgs...)
		// virtual=true: matches ordinary compiled callvirt sites
		// elsewhere in this project — typeName is the statically-known
		// declaring type (from the tree's own MethodInfo), and
		// Machine.call's own ancestor-walk redirects to the receiver's
		// real concrete override when there is one, exactly like any
		// other callvirt already does.
		v, _, err := m.call(typeName+"::"+methodName, callArgs, true, depth, instrCount, nil, nil)
		return v, err

	case bcl.ExprNodeNew:
		typeName, argNodes, _ := bcl.NewExpressionParts(node)
		evaluatedArgs, err := evalExprList(m, argNodes, env, depth, instrCount, exprDepth+1)
		if err != nil {
			return runtime.Value{}, err
		}
		// typeName came from a real ConstructorInfo/Type value (Expression.
		// New's own constructor argument) — already fully closed, exactly
		// like Machine.New's own reflection-based construction entry
		// point (Fase 3.66, found via CsvHelper's own ObjectCreator
		// caching a compiled `Expression.New(ctor).Compile()` delegate
		// per closed type): parsed directly off typeName's own "[[...]]"
		// suffix, no forwarding-resolution needed (this evaluator has no
		// notion of an enclosing generic method's own open type
		// parameter at all).
		return m.newObj(newObjArgs{TypeFullName: typeName, CtorFullName: typeName + "::.ctor", Args: evaluatedArgs, ClassGenericArgs: bcl.ClosedGenericArgs(typeName)}, depth, instrCount)

	case bcl.ExprNodeNewArrayInit:
		_, elemNodes, _ := bcl.NewArrayExpressionParts(node)
		evaluated, err := evalExprList(m, elemNodes, env, depth, instrCount, exprDepth+1)
		if err != nil {
			return runtime.Value{}, err
		}
		return runtime.ArrRef(&runtime.Array{Elems: evaluated}), nil

	case bcl.ExprNodeConvert:
		operand, typeName, _ := bcl.ConvertExpressionParts(node)
		v, err := evalExprNode(m, operand, env, depth, instrCount, exprDepth+1)
		if err != nil {
			return runtime.Value{}, err
		}
		return convertExprValue(v, typeName), nil

	case bcl.ExprNodeAssign:
		left, right, _ := bcl.AssignExpressionParts(node)
		v, err := evalExprNode(m, right, env, depth, instrCount, exprDepth+1)
		if err != nil {
			return runtime.Value{}, err
		}
		switch bcl.KindOfExprNode(left) {
		case bcl.ExprNodeParameter:
			// A Block-scoped local (or, more rarely, a lambda parameter
			// itself being reassigned) — store into the environment.
			obj, ok := bcl.ExprNodeIdentity(left)
			if !ok {
				return runtime.Value{}, fmt.Errorf("interpreter: compiled expression: Assign's left side has no identity")
			}
			env[obj] = v
			return v, nil
		case bcl.ExprNodeMember:
			// A real property/field assignment (`dest.Name = ...`) — the
			// overwhelmingly common shape in a real mapping-plan's own
			// generated code (AutoMapper) — dispatches to the property's
			// real setter, the same way an ordinary compiled `stfld`/
			// `callvirt set_Xxx` site already would.
			propName, inner, _ := bcl.MemberExpressionParts(left)
			receiver, err := evalExprNode(m, inner, env, depth, instrCount, exprDepth+1)
			if err != nil {
				return runtime.Value{}, err
			}
			typeName, ok := receiverTypeName(receiver)
			if !ok {
				return runtime.Value{}, fmt.Errorf("interpreter: compiled expression: Assign: can't determine %q's own receiver type", propName)
			}
			if _, _, err := m.call(typeName+"::set_"+propName, []runtime.Value{receiver, v}, true, depth, instrCount, nil, nil); err != nil {
				return runtime.Value{}, err
			}
			return v, nil
		default:
			return runtime.Value{}, fmt.Errorf("interpreter: compiled expression: Assign's left side must be a variable/parameter or a property/field access")
		}

	case bcl.ExprNodeBlock:
		variables, body, _ := bcl.BlockExpressionParts(node)
		for _, v := range variables {
			obj, ok := bcl.ExprNodeIdentity(v)
			if !ok {
				continue
			}
			if _, exists := env[obj]; !exists {
				typeName, _ := bcl.ParameterExpressionTypeName(v)
				env[obj] = defaultValueForExprType(typeName)
			}
		}
		var result runtime.Value
		for _, expr := range body {
			v, err := evalExprNode(m, expr, env, depth, instrCount, exprDepth+1)
			if err != nil {
				return runtime.Value{}, err
			}
			result = v
		}
		return result, nil

	case bcl.ExprNodeDefault:
		typeName, _ := bcl.DefaultExpressionTypeName(node)
		return defaultValueForExprType(typeName), nil

	case bcl.ExprNodeConditional:
		test, ifTrue, ifFalse, _ := bcl.ConditionalExpressionParts(node)
		t, err := evalExprNode(m, test, env, depth, instrCount, exprDepth+1)
		if err != nil {
			return runtime.Value{}, err
		}
		if t.Truthy() {
			return evalExprNode(m, ifTrue, env, depth, instrCount, exprDepth+1)
		}
		if ifFalse.Kind == runtime.KindNull {
			// A real IfThen (no else branch) with a false test — void,
			// same as real IfThen's own documented return-nothing shape.
			return runtime.Value{}, nil
		}
		return evalExprNode(m, ifFalse, env, depth, instrCount, exprDepth+1)

	case bcl.ExprNodeInvoke:
		exprToInvoke, argNodes, _ := bcl.InvokeExpressionParts(node)
		evaluatedArgs, err := evalExprList(m, argNodes, env, depth, instrCount, exprDepth+1)
		if err != nil {
			return runtime.Value{}, err
		}
		if bcl.KindOfExprNode(exprToInvoke) == bcl.ExprNodeLambda {
			// The overwhelmingly common real shape: Expression.Invoke on
			// a LITERAL nested LambdaExpression node (a sub-expression
			// built once and spliced into several call sites — real
			// AutoMapper mapping-plan generation does exactly this for
			// per-member sub-mappings), not a compiled delegate VALUE.
			// Evaluating exprToInvoke directly (like any other node)
			// would run its body immediately with whatever's already in
			// env for its own parameters — wrong; this binds the
			// ACTUALLY EVALUATED arguments to the lambda's own
			// parameters in a fresh scope first, then evaluates its
			// body, exactly like compiledExpressionInvoke's own
			// top-level entry point does for the outer Compile()'d
			// lambda.
			params, _ := bcl.LambdaExpressionParameters(exprToInvoke)
			body, _ := bcl.LambdaExpressionBody(exprToInvoke)
			inner := make(map[*runtime.Object]runtime.Value, len(env)+len(params))
			for k, v := range env {
				inner[k] = v
			}
			for i, p := range params {
				if obj, ok := bcl.ExprNodeIdentity(p); ok && i < len(evaluatedArgs) {
					inner[obj] = evaluatedArgs[i]
				}
			}
			return evalExprNode(m, body, inner, depth, instrCount, exprDepth+1)
		}
		fnVal, err := evalExprNode(m, exprToInvoke, env, depth, instrCount, exprDepth+1)
		if err != nil {
			return runtime.Value{}, err
		}
		if fnVal.Kind != runtime.KindFunc || fnVal.Func == nil {
			return runtime.Value{}, fmt.Errorf("interpreter: compiled expression: Invoke target is not a delegate")
		}
		v, _, err := m.invokeFunc(fnVal.Func, evaluatedArgs, depth, instrCount)
		return v, err

	case bcl.ExprNodeTry:
		// Real evaluation — found via AutoMapper's own mapping-plan
		// generation wrapping a property-mapping expression in a real
		// try/catch/finally template. A finally block always runs on
		// the way out, on EITHER path (matching real .NET semantics),
		// via a deferred-style closure rather than a Go `defer` (this
		// isn't a Go function boundary, just one branch of a larger
		// expression evaluation).
		body, catches, finallyExpr, _ := bcl.TryExpressionParts(node)
		result, err := evalExprNode(m, body, env, depth, instrCount, exprDepth+1)
		if err != nil {
			if mex, ok := err.(*runtime.ManagedException); ok {
				for _, cb := range catches {
					testType, variable, catchBody, _ := bcl.CatchBlockParts(cb)
					if !m.exceptionMatchesCatch(mex, testType) {
						continue
					}
					if obj, ok := bcl.ExprNodeIdentity(variable); ok {
						exObj := mex.Object
						if exObj == nil {
							exObj = &runtime.Object{Native: mex}
						}
						env[obj] = runtime.ObjRef(exObj)
					}
					result, err = evalExprNode(m, catchBody, env, depth, instrCount, exprDepth+1)
					break
				}
			}
		}
		if finallyExpr.Kind != runtime.KindNull {
			if _, ferr := evalExprNode(m, finallyExpr, env, depth, instrCount, exprDepth+1); ferr != nil {
				return runtime.Value{}, ferr
			}
		}
		return result, err

	case bcl.ExprNodeThrow:
		// Real evaluation, not just tree-shape modeling — found via
		// AutoMapper's own null-source-guard template
		// (`throw new ArgumentNullException(...)`). Evaluating the
		// thrown-value expression runs its own `newobj` (via
		// ExprNodeNew), producing a real exception instance that
		// valueAsThrowable (eval.go) turns into the same Go error a
		// real `throw` IL instruction propagates.
		valueExpr, _, _ := bcl.ThrowExpressionParts(node)
		v, err := evalExprNode(m, valueExpr, env, depth, instrCount, exprDepth+1)
		if err != nil {
			return runtime.Value{}, err
		}
		if throwErr := valueAsThrowable(v); throwErr != nil {
			return runtime.Value{}, throwErr
		}
		return runtime.Value{}, fmt.Errorf("interpreter: compiled expression: Throw's value isn't a recognized exception instance")

	case bcl.ExprNodeCoalesce:
		left, right, _ := bcl.CoalesceExpressionParts(node)
		lv, err := evalExprNode(m, left, env, depth, instrCount, exprDepth+1)
		if err != nil {
			return runtime.Value{}, err
		}
		if lv.Kind != runtime.KindNull && !(lv.Kind == runtime.KindObject && lv.Obj == nil) {
			return lv, nil
		}
		return evalExprNode(m, right, env, depth, instrCount, exprDepth+1)

	case bcl.ExprNodeBinaryCompare:
		opType, left, right, _ := bcl.BinaryCompareExpressionParts(node)
		lv, err := evalExprNode(m, left, env, depth, instrCount, exprDepth+1)
		if err != nil {
			return runtime.Value{}, err
		}
		rv, err := evalExprNode(m, right, env, depth, instrCount, exprDepth+1)
		if err != nil {
			return runtime.Value{}, err
		}
		equal := referenceOrValueEqual(lv, rv)
		if opType == bcl.ExprTypeNotEqual {
			return runtime.Bool(!equal), nil
		}
		return runtime.Bool(equal), nil

	case bcl.ExprNodeIncDec:
		opType, operand, _ := bcl.IncDecExpressionParts(node)
		if bcl.KindOfExprNode(operand) != bcl.ExprNodeParameter {
			return runtime.Value{}, fmt.Errorf("interpreter: compiled expression: increment/decrement operand must be a variable")
		}
		obj, ok := bcl.ExprNodeIdentity(operand)
		if !ok {
			return runtime.Value{}, fmt.Errorf("interpreter: compiled expression: increment/decrement operand has no identity")
		}
		old, err := evalExprNode(m, operand, env, depth, instrCount, exprDepth+1)
		if err != nil {
			return runtime.Value{}, err
		}
		delta := int64(1)
		if opType == bcl.ExprTypePreDecrementAssign || opType == bcl.ExprTypePostDecrementAssign {
			delta = -1
		}
		updated := addIntDelta(old, delta)
		env[obj] = updated
		if opType == bcl.ExprTypePreIncrementAssign || opType == bcl.ExprTypePreDecrementAssign {
			return updated, nil
		}
		return old, nil

	default:
		return runtime.Value{}, fmt.Errorf("interpreter: compiled expression: unsupported node shape — this subsystem doesn't model every real Expression node type yet (see internal/bcl/system_linq_expressions.go's own doc comment)")
	}
}

// referenceOrValueEqual backs Expression.ReferenceEqual/ReferenceNotEqual's
// real evaluation — a true reference-identity comparison for objects
// (never a user-defined == operator), falling back to a plain value
// comparison for the primitive Kinds where "reference" doesn't apply
// (real .NET boxes these before comparing, which is observably a value
// comparison for identical boxed values).
func referenceOrValueEqual(a, b runtime.Value) bool {
	if a.Kind == runtime.KindNull || b.Kind == runtime.KindNull {
		return a.Kind == b.Kind
	}
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case runtime.KindObject:
		return a.Obj == b.Obj
	case runtime.KindI4:
		return a.I4 == b.I4
	case runtime.KindI8:
		return a.I8 == b.I8
	case runtime.KindR4:
		return a.R4 == b.R4
	case runtime.KindR8:
		return a.R8 == b.R8
	case runtime.KindString:
		return a.Str == b.Str
	default:
		return false
	}
}

// addIntDelta implements the increment/decrement's own +1/-1 step for
// every integer Kind a Block-scoped loop-index variable realistically
// holds; anything else is returned unchanged rather than guessed at.
func addIntDelta(v runtime.Value, delta int64) runtime.Value {
	switch v.Kind {
	case runtime.KindI4:
		return runtime.Int32(v.I4 + int32(delta))
	case runtime.KindI8:
		return runtime.Int64(v.I8 + delta)
	default:
		return v
	}
}

func evalExprList(m *Machine, nodes []runtime.Value, env map[*runtime.Object]runtime.Value, depth int, instrCount *int64, exprDepth int) ([]runtime.Value, error) {
	out := make([]runtime.Value, len(nodes))
	for i, n := range nodes {
		v, err := evalExprNode(m, n, env, depth, instrCount, exprDepth+1)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// defaultValueForExprType returns real .NET's own default(T) for typeName
// — a small, deliberately incomplete set covering the primitive/string
// cases a Block-scoped local or an unbound parameter realistically has;
// anything else (a struct, an unresolved type name) falls back to Null(),
// which is wrong for a real value-typed default but unobservable for
// every real corpus caller found so far (a variable is always assigned
// before being read in the generated code this subsystem has been run
// against).
func defaultValueForExprType(typeName string) runtime.Value {
	switch typeName {
	case "System.Int32", "System.Boolean", "System.Byte", "System.SByte",
		"System.Int16", "System.UInt16", "System.UInt32", "System.Char":
		return runtime.Int32(0)
	case "System.Int64", "System.UInt64":
		return runtime.Int64(0)
	case "System.Single":
		return runtime.Float32(0)
	case "System.Double":
		return runtime.Float64(0)
	default:
		return runtime.Null()
	}
}

// convertExprValue implements Convert/ConvertChecked's real numeric
// coercions (widening/narrowing between int32/int64/float32/float64) —
// vmnet's own overflow-checking posture for ConvertChecked isn't
// modeled separately (same simplification the rest of this project's
// arithmetic already accepts). A reference-type target (or any target
// this switch doesn't recognize) is an identity passthrough: vmnet's
// uniform Value representation already IS the "boxed" shape a real
// reference-type conversion/box would produce, the same reasoning
// ir/builder.go's own box/unbox.any handling already documents as a
// pure Nop.
func convertExprValue(v runtime.Value, targetTypeName string) runtime.Value {
	switch targetTypeName {
	case "System.Int32", "System.Boolean", "System.Byte", "System.SByte",
		"System.Int16", "System.UInt16", "System.UInt32", "System.Char":
		switch v.Kind {
		case runtime.KindI8:
			return runtime.Int32(int32(v.I8))
		case runtime.KindR4:
			return runtime.Int32(int32(v.R4))
		case runtime.KindR8:
			return runtime.Int32(int32(v.R8))
		}
	case "System.Int64", "System.UInt64":
		switch v.Kind {
		case runtime.KindI4:
			return runtime.Int64(int64(v.I4))
		case runtime.KindR4:
			return runtime.Int64(int64(v.R4))
		case runtime.KindR8:
			return runtime.Int64(int64(v.R8))
		}
	case "System.Single":
		switch v.Kind {
		case runtime.KindI4:
			return runtime.Float32(float32(v.I4))
		case runtime.KindI8:
			return runtime.Float32(float32(v.I8))
		case runtime.KindR8:
			return runtime.Float32(float32(v.R8))
		}
	case "System.Double":
		switch v.Kind {
		case runtime.KindI4:
			return runtime.Float64(float64(v.I4))
		case runtime.KindI8:
			return runtime.Float64(float64(v.I8))
		case runtime.KindR4:
			return runtime.Float64(float64(v.R4))
		}
	}
	return v
}
