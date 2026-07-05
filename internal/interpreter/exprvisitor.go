package interpreter

import (
	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// System.Linq.Expressions.ExpressionVisitor (Fase 3.65) — found via
// AutoMapper's own AutoMapper.Execution.ReplaceVisitorBase/ReplaceVisitor/
// ParameterReplaceVisitor: real C# subclasses (real TypeDefs, real IL)
// that override ONE OR TWO of ExpressionVisitor's own virtual methods
// (typically just Visit or VisitParameter) and otherwise rely entirely
// on the BASE class's own default "recurse into children, rebuild if
// anything changed" behavior for every other node kind in the tree —
// the standard real-.NET pattern for a targeted tree rewrite (here:
// swapping one cached template's ParameterExpression for the caller's
// own, so a per-property mapping expression built once can be reused
// against many different outer lambdas).
//
// ExpressionVisitor itself ships as compiled IL in System.Private.CoreLib/
// System.Linq.Expressions — vmnet has no bytecode for it at all (it's
// pure BCL, not something any real caller's own assembly declares), so
// every method below is a native Go implementation standing in for that
// real default virtual-dispatch behavior. Subclass overrides still work
// exactly like any other virtual call: m.call's own ancestor walk always
// tries the receiver's actual concrete type first (see calls.go's own
// doc comment on this), so a subclass overriding just VisitParameter (or
// just Visit itself, as ReplaceVisitor does) still gets every OTHER node
// kind's default behavior from the native VisitXxx entries registered
// here.
func init() {
	machineRegistry["System.Linq.Expressions.ExpressionVisitor::.ctor"] = expressionVisitorCtor
	machineRegistry["System.Linq.Expressions.ExpressionVisitor::Visit"] = expressionVisitorVisit
	machineRegistry["System.Linq.Expressions.ExpressionVisitor::VisitParameter"] = expressionVisitorVisitLeaf
	machineRegistry["System.Linq.Expressions.ExpressionVisitor::VisitConstant"] = expressionVisitorVisitLeaf
	machineRegistry["System.Linq.Expressions.ExpressionVisitor::VisitDefault"] = expressionVisitorVisitLeaf
	machineRegistry["System.Linq.Expressions.ExpressionVisitor::VisitMember"] = expressionVisitorVisitMember
	machineRegistry["System.Linq.Expressions.ExpressionVisitor::VisitMethodCall"] = expressionVisitorVisitMethodCall
	machineRegistry["System.Linq.Expressions.ExpressionVisitor::VisitNew"] = expressionVisitorVisitNew
	machineRegistry["System.Linq.Expressions.ExpressionVisitor::VisitNewArray"] = expressionVisitorVisitNewArray
	machineRegistry["System.Linq.Expressions.ExpressionVisitor::VisitUnary"] = expressionVisitorVisitUnary
	machineRegistry["System.Linq.Expressions.ExpressionVisitor::VisitBinary"] = expressionVisitorVisitBinary
	machineRegistry["System.Linq.Expressions.ExpressionVisitor::VisitBlock"] = expressionVisitorVisitBlock
	machineRegistry["System.Linq.Expressions.ExpressionVisitor::VisitConditional"] = expressionVisitorVisitConditional
	machineRegistry["System.Linq.Expressions.ExpressionVisitor::VisitInvocation"] = expressionVisitorVisitInvocation
	machineRegistry["System.Linq.Expressions.ExpressionVisitor::VisitLambda"] = expressionVisitorVisitLambda
}

// expressionVisitorCtor backs ExpressionVisitor's own base .ctor —
// ExpressionVisitor carries no state of its own (every real subclass's
// own fields, e.g. ReplaceVisitorBase's _oldNode/_newNode, are already
// allocated on `this` by the time this chained base call runs), so this
// is a real no-op, matching real .NET's own empty ExpressionVisitor()
// constructor body.
func expressionVisitorCtor(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	return runtime.Value{}, nil
}

// expressionVisitorVisit is Visit(Expression)'s own real default body:
// null in, null out; otherwise dispatch — VIRTUALLY, so an override on
// any individual VisitXxx still applies — to the one VisitXxx bucket
// matching node's real node kind. Every real ExpressionType maps to
// exactly one of these buckets in actual .NET (see each case's own
// comment for which ones share a bucket).
func expressionVisitorVisit(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Null(), nil
	}
	receiver, node := args[0], args[1]
	if node.Kind == runtime.KindNull {
		return runtime.Null(), nil
	}
	visitMethod := ""
	switch bcl.KindOfExprNode(node) {
	case bcl.ExprNodeParameter:
		visitMethod = "VisitParameter"
	case bcl.ExprNodeConstant:
		visitMethod = "VisitConstant"
	case bcl.ExprNodeDefault:
		visitMethod = "VisitDefault"
	case bcl.ExprNodeMember:
		visitMethod = "VisitMember"
	case bcl.ExprNodeCall:
		visitMethod = "VisitMethodCall"
	case bcl.ExprNodeNew:
		visitMethod = "VisitNew"
	case bcl.ExprNodeNewArrayInit:
		visitMethod = "VisitNewArray"
	case bcl.ExprNodeConvert, bcl.ExprNodeIncDec:
		// Both are real UnaryExpression shapes — see nativeConvertExpression/
		// nativeIncDecExpression's own doc comments.
		visitMethod = "VisitUnary"
	case bcl.ExprNodeAssign, bcl.ExprNodeBinaryCompare:
		// Both are real BinaryExpression shapes.
		visitMethod = "VisitBinary"
	case bcl.ExprNodeBlock:
		visitMethod = "VisitBlock"
	case bcl.ExprNodeConditional:
		visitMethod = "VisitConditional"
	case bcl.ExprNodeInvoke:
		visitMethod = "VisitInvocation"
	case bcl.ExprNodeLambda:
		visitMethod = "VisitLambda"
	default:
		// A node shape this subsystem doesn't model at all — passed
		// through unchanged rather than erroring, since a visitor that
		// never actually reaches this specific shape (the overwhelmingly
		// common case: most real trees only ever touch a handful of node
		// kinds) shouldn't fail just because SOME other kind has no
		// native default here yet.
		return node, nil
	}
	v, _, err := m.call("System.Linq.Expressions.ExpressionVisitor::"+visitMethod, []runtime.Value{receiver, node}, true, depth, instrCount, nil, nil)
	if err != nil {
		return runtime.Value{}, err
	}
	return v, nil
}

// expressionVisitorVisitLeaf backs the default VisitParameter/
// VisitConstant/VisitDefault — all three are real leaf nodes with no
// child Expressions to recurse into, so real .NET's own default
// behavior is simply "return node unchanged".
func expressionVisitorVisitLeaf(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Null(), nil
	}
	return args[1], nil
}

func visitChild(m *Machine, receiver, child runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if child.Kind == runtime.KindNull {
		return child, nil
	}
	v, _, err := m.call("System.Linq.Expressions.ExpressionVisitor::Visit", []runtime.Value{receiver, child}, true, depth, instrCount, nil, nil)
	return v, err
}

func visitChildren(m *Machine, receiver runtime.Value, children []runtime.Value, depth int, instrCount *int64) ([]runtime.Value, error) {
	out := make([]runtime.Value, len(children))
	for i, c := range children {
		v, err := visitChild(m, receiver, c, depth, instrCount)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

func expressionVisitorVisitMember(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	node := args[1]
	propName, inner, _ := bcl.MemberExpressionParts(node)
	newInner, err := visitChild(m, args[0], inner, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	typeName, _ := bcl.MemberExpressionTypeName(node)
	return bcl.NewMemberExpressionValue(propName, newInner, typeName), nil
}

func expressionVisitorVisitMethodCall(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	node := args[1]
	instance, typeName, methodName, argNodes, _ := bcl.CallExpressionParts(node)
	newInstance, err := visitChild(m, args[0], instance, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	newArgs, err := visitChildren(m, args[0], argNodes, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	return bcl.NewCallExpressionValue(newInstance, typeName, methodName, newArgs), nil
}

func expressionVisitorVisitNew(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	node := args[1]
	typeName, argNodes, _ := bcl.NewExpressionParts(node)
	newArgs, err := visitChildren(m, args[0], argNodes, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	return bcl.NewNewExpressionValue(typeName, newArgs), nil
}

func expressionVisitorVisitNewArray(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	node := args[1]
	elemTypeName, elemNodes, _ := bcl.NewArrayExpressionParts(node)
	newElems, err := visitChildren(m, args[0], elemNodes, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	return bcl.NewNewArrayExpressionValue(elemTypeName, newElems), nil
}

func expressionVisitorVisitUnary(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	node := args[1]
	if bcl.KindOfExprNode(node) == bcl.ExprNodeIncDec {
		opType, operand, _ := bcl.IncDecExpressionParts(node)
		newOperand, err := visitChild(m, args[0], operand, depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		return bcl.NewIncDecExpressionValue(opType, newOperand), nil
	}
	operand, typeName, _ := bcl.ConvertExpressionParts(node)
	newOperand, err := visitChild(m, args[0], operand, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	return bcl.NewConvertExpressionValue(newOperand, typeName), nil
}

func expressionVisitorVisitBinary(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	node := args[1]
	if bcl.KindOfExprNode(node) == bcl.ExprNodeBinaryCompare {
		opType, left, right, _ := bcl.BinaryCompareExpressionParts(node)
		newLeft, err := visitChild(m, args[0], left, depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		newRight, err := visitChild(m, args[0], right, depth, instrCount)
		if err != nil {
			return runtime.Value{}, err
		}
		return bcl.NewBinaryCompareExpressionValue(opType, newLeft, newRight), nil
	}
	left, right, _ := bcl.AssignExpressionParts(node)
	newLeft, err := visitChild(m, args[0], left, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	newRight, err := visitChild(m, args[0], right, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	return bcl.NewAssignExpressionValue(newLeft, newRight), nil
}

func expressionVisitorVisitBlock(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	node := args[1]
	variables, body, _ := bcl.BlockExpressionParts(node)
	// Real default VisitBlock does call VisitAndConvert on each variable
	// too, but a ParameterExpression is a leaf (VisitParameter's own
	// default is identity) — visiting them here would just copy the same
	// slice for no observable difference, so this skips straight to the
	// body for simplicity.
	newBody, err := visitChildren(m, args[0], body, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	return bcl.NewBlockExpressionValue(variables, newBody), nil
}

func expressionVisitorVisitConditional(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	node := args[1]
	test, ifTrue, ifFalse, _ := bcl.ConditionalExpressionParts(node)
	newTest, err := visitChild(m, args[0], test, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	newIfTrue, err := visitChild(m, args[0], ifTrue, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	newIfFalse, err := visitChild(m, args[0], ifFalse, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	return bcl.NewConditionalExpressionValue(newTest, newIfTrue, newIfFalse), nil
}

func expressionVisitorVisitInvocation(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	node := args[1]
	expr, argNodes, _ := bcl.InvokeExpressionParts(node)
	newExpr, err := visitChild(m, args[0], expr, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	newArgs, err := visitChildren(m, args[0], argNodes, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	return bcl.NewInvokeExpressionValue(newExpr, newArgs), nil
}

func expressionVisitorVisitLambda(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	node := args[1]
	body, ok := bcl.LambdaExpressionBody(node)
	if !ok {
		return node, nil
	}
	params, _ := bcl.LambdaExpressionParameters(node)
	newBody, err := visitChild(m, args[0], body, depth, instrCount)
	if err != nil {
		return runtime.Value{}, err
	}
	return bcl.NewLambdaExpressionValue(newBody, params), nil
}
