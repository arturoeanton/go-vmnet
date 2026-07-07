package bcl

import (
	"fmt"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// System.Linq.Expressions support (Fase 3.41 narrow beginning, Fase 3.64
// widened for a REAL property-access-chain Expression<T>.Compile(), Fase
// 3.65 widened again into a genuinely general — if still not exhaustive —
// expression-tree evaluator). This still isn't a JIT: nothing here ever
// generates or runs new machine code or IL. Every node below is a plain
// Go struct recording exactly what its real Expression.XXX(...) factory
// call was given; internal/interpreter/exprcompile.go's own evalExprNode
// walks that data recursively at invocation time, dispatching each real
// operation (a property read, a method call, a constructor call, an
// assignment into a Block-scoped variable, ...) through the SAME real
// Machine.call/newObj machinery an ordinary compiled call site already
// uses — a tree-walking interpreter for a small, real subset of
// Expression node kinds, not a general one. Found via two real, deep
// consumers: Microsoft.Extensions.DependencyInjection's own
// ExpressionResolverBuilder (its compiled-service-resolution fast path)
// and AutoMapper's own mapping-plan generation.
//
// Every node type below carries a typeName string for Expression.Type —
// derived from data already available at construction time (the same
// information the real .NET caller already had), not inferred centrally;
// see expressionGetType's own doc comment.

// nativeParameterExpression backs ParameterExpression — used for BOTH a
// lambda's own formal parameters (Expression.Parameter) and a Block's
// locals (Expression.Variable, a real, exact alias of Parameter in
// .NET). Identity matters here in a way it didn't before Fase 3.65: the
// evaluator's environment is keyed by this Value's own *runtime.Object
// pointer, so the SAME ParameterExpression object appearing at multiple
// points in a tree (the common case: a lambda parameter referenced many
// times in its own body) always resolves to the same environment slot.
type nativeParameterExpression struct {
	typeName string
	name     string
}

// nativeMemberExpression carries the property name and, since Fase 3.64,
// the Expression it was accessed off of. typeName (Fase 3.65) is the
// member's own declared type — known when constructed via the
// PropertyInfo overload (PropertyInfoParts already carries it); "" (falls
// back to System.Object for Expression.Type) for the MethodInfo overload,
// which carries no property-type information at all.
type nativeMemberExpression struct {
	propertyName string
	expression   runtime.Value
	typeName     string
}

// nativeMemberInfo backs MemberExpression.Member's real return type
// (System.Reflection.MemberInfo) — exposes only .Name, the one member
// AddAttribute (Fase 3.41) reads.
type nativeMemberInfo struct {
	name string
}

// nativeLambdaExpression backs Expression<TDelegate> — body plus (Fase
// 3.65) ALL of its own parameters, in order, needed both for
// LambdaExpression.Parameters (a real, plural property several real
// consumers read) and to seed the evaluator's environment with every
// parameter, not just the first, when the compiled delegate is actually
// invoked.
type nativeLambdaExpression struct {
	body       runtime.Value
	parameters []runtime.Value
}

// nativeConstantExpression backs ConstantExpression — a literal value
// baked into the tree at construction time.
type nativeConstantExpression struct {
	value    runtime.Value
	typeName string
}

// nativeCallExpression backs MethodCallExpression — instance.Kind ==
// KindNull for a static method call.
type nativeCallExpression struct {
	instance   runtime.Value
	typeName   string
	methodName string
	args       []runtime.Value
}

// nativeNewExpression backs NewExpression.
type nativeNewExpression struct {
	typeName string
	args     []runtime.Value
}

// nativeNewArrayExpression backs NewArrayExpression (Expression.
// NewArrayInit — NewArrayBounds, a different real factory for an
// uninitialized array of a given length, isn't modeled: no real corpus
// caller found needs it).
type nativeNewArrayExpression struct {
	elemTypeName string
	elements     []runtime.Value
}

// nativeConvertExpression backs UnaryExpression with NodeType Convert/
// ConvertChecked — evalConvert (exprcompile.go) does the real coercion.
type nativeConvertExpression struct {
	operand  runtime.Value
	typeName string
}

// nativeAssignExpression backs BinaryExpression with NodeType Assign —
// real .NET models Assign as a BinaryExpression (Left/Right), not its
// own subclass; modeled as its own narrow Go type here since Assign is
// the only BinaryExpression kind this subsystem evaluates (no general
// arithmetic/comparison operators — Add/Subtract/Equal/etc. — yet; no
// real corpus caller confirmed needing one).
type nativeAssignExpression struct {
	left, right runtime.Value
}

// nativeBlockExpression backs BlockExpression — variables are this
// block's own locally-scoped ParameterExpressions (Expression.Variable),
// seeded to a real default(T) in the evaluator's environment before body
// runs; body is evaluated in order, and the block's own value is its
// LAST expression's value (real Block semantics).
type nativeBlockExpression struct {
	variables []runtime.Value
	body      []runtime.Value
}

// nativeDefaultExpression backs DefaultExpression (Expression.Default).
type nativeDefaultExpression struct {
	typeName string
}

// nativeConditionalExpression backs ConditionalExpression — covers all
// three real factories that produce one: IfThen (ifFalse.Kind ==
// KindNull, a statement — no useful value), IfThenElse, and Condition
// (both branches given, a real ternary-shaped value).
type nativeConditionalExpression struct {
	test, ifTrue, ifFalse runtime.Value
}

// nativeInvokeExpression backs InvocationExpression (Expression.Invoke)
// — invoking a nested lambda/delegate-valued expression, distinct from a
// MethodCallExpression (a real named method).
type nativeInvokeExpression struct {
	expr runtime.Value
	args []runtime.Value
}

func init() {
	register("System.Linq.Expressions.Expression::Parameter", true, expressionParameter)
	register("System.Linq.Expressions.Expression::Variable", true, expressionParameter)
	register("System.Linq.Expressions.Expression::Property", true, expressionProperty)
	// MakeMemberAccess(Expression, MemberInfo) is Property's own general
	// factory (Fase 3.81, found via CsvHelper's own ExpressionManager
	// building `Expression.Assign(Expression.MakeMemberAccess(instance,
	// b.Member), b.Expression)` for each mapped member) — expressionProperty
	// already handles a PropertyInfo 2nd argument (the only MemberInfo
	// kind CsvHelper's own real member/reference maps ever carry here,
	// since Person-shaped record types are mapped by public property, not
	// field), so the exact same implementation applies unchanged. A
	// FieldInfo 2nd argument isn't modeled (falls through to
	// expressionProperty's own silent-empty degenerate case) — no real
	// corpus caller needs it yet.
	register("System.Linq.Expressions.Expression::MakeMemberAccess", true, expressionProperty)
	register("System.Linq.Expressions.Expression::Lambda", true, expressionLambda)
	register("System.Linq.Expressions.LambdaExpression::get_Body", true, lambdaExpressionGetBody)
	register("System.Linq.Expressions.LambdaExpression::get_Parameters", true, lambdaExpressionGetParameters)
	register("System.Linq.Expressions.MemberExpression::get_Member", true, memberExpressionGetMember)
	register("System.Linq.Expressions.MemberExpression::get_Expression", true, memberExpressionGetExpression)
	register("System.Linq.Expressions.Expression::get_NodeType", true, expressionGetNodeType)
	register("System.Linq.Expressions.Expression::get_Type", true, expressionGetType)
	register("System.Linq.Expressions.ParameterExpression::get_Name", true, parameterExpressionGetName)
	register("System.Linq.Expressions.ParameterExpression::get_Type", true, expressionGetType)
	register("System.Linq.Expressions.Expression::Constant", true, expressionConstant)
	register("System.Linq.Expressions.Expression::Call", true, expressionCall)
	register("System.Linq.Expressions.Expression::New", true, expressionNew)
	register("System.Linq.Expressions.Expression::NewArrayInit", true, expressionNewArrayInit)
	register("System.Linq.Expressions.Expression::ArrayIndex", true, expressionArrayIndex)
	register("System.Linq.Expressions.Expression::Bind", true, expressionBind)
	// Member is declared (and never overridden) on the abstract MemberBinding
	// base, not MemberAssignment itself — real IL callvirts target
	// MemberBinding::get_Member directly, same reasoning as MemberInfo.
	// DeclaringType's own registration under the base MemberInfo name
	// (reflection.go).
	register("System.Linq.Expressions.MemberBinding::get_Member", true, memberAssignmentGetMember)
	register("System.Linq.Expressions.MemberAssignment::get_Expression", true, memberAssignmentGetExpression)
	register("System.Linq.Expressions.Expression::Convert", true, expressionConvert)
	register("System.Linq.Expressions.Expression::ConvertChecked", true, expressionConvert)
	register("System.Linq.Expressions.Expression::Assign", true, expressionAssign)
	register("System.Linq.Expressions.Expression::Block", true, expressionBlock)
	register("System.Linq.Expressions.Expression::Default", true, expressionDefault)
	register("System.Linq.Expressions.Expression::Empty", true, expressionEmpty)
	register("System.Linq.Expressions.Expression::Throw", true, expressionThrow)
	register("System.Linq.Expressions.Expression::Coalesce", true, expressionCoalesce)
	register("System.Linq.Expressions.Expression::Catch", true, expressionCatch)
	register("System.Linq.Expressions.Expression::TryCatch", true, expressionTryCatch)
	register("System.Linq.Expressions.Expression::TryFinally", true, expressionTryFinally)
	register("System.Linq.Expressions.Expression::TryCatchFinally", true, expressionTryCatchFinally)
	register("System.Linq.Expressions.TryExpression::get_Body", true, tryExpressionGetBody)
	register("System.Linq.Expressions.TryExpression::get_Handlers", true, tryExpressionGetHandlers)
	register("System.Linq.Expressions.TryExpression::get_Finally", true, tryExpressionGetFinally)
	register("System.Linq.Expressions.CatchBlock::get_Test", true, catchBlockGetTest)
	register("System.Linq.Expressions.CatchBlock::get_Variable", true, catchBlockGetVariable)
	register("System.Linq.Expressions.CatchBlock::get_Body", true, catchBlockGetBody)
	register("System.Linq.Expressions.Expression::IfThen", true, expressionIfThen)
	register("System.Linq.Expressions.Expression::IfThenElse", true, expressionIfThenElse)
	register("System.Linq.Expressions.Expression::Condition", true, expressionIfThenElse)
	register("System.Linq.Expressions.Expression::Invoke", true, expressionInvoke)
	register("System.Linq.Expressions.Expression::ReferenceEqual", true, expressionReferenceEqual)
	register("System.Linq.Expressions.Expression::ReferenceNotEqual", true, expressionReferenceNotEqual)
	register("System.Linq.Expressions.Expression::PreIncrementAssign", true, expressionPreIncrementAssign)
	register("System.Linq.Expressions.Expression::PostIncrementAssign", true, expressionPostIncrementAssign)
	register("System.Linq.Expressions.Expression::PreDecrementAssign", true, expressionPreDecrementAssign)
	register("System.Linq.Expressions.Expression::PostDecrementAssign", true, expressionPostDecrementAssign)
	register("System.Linq.Expressions.NewArrayExpression::get_Expressions", true, newArrayExpressionGetExpressions)
	register("System.Linq.Expressions.MethodCallExpression::get_Object", true, callExpressionGetObject)
	register("System.Linq.Expressions.MethodCallExpression::get_Arguments", true, callExpressionGetArguments)
	register("System.Linq.Expressions.MethodCallExpression::get_Method", true, callExpressionGetMethod)
	register("System.Linq.Expressions.UnaryExpression::get_Operand", true, convertExpressionGetOperand)
	register("System.Linq.Expressions.BinaryExpression::get_Left", true, assignExpressionGetLeft)
	register("System.Linq.Expressions.BinaryExpression::get_Right", true, assignExpressionGetRight)
	register("System.Linq.Expressions.BlockExpression::get_Variables", true, blockExpressionGetVariables)
	register("System.Linq.Expressions.BlockExpression::get_Expressions", true, blockExpressionGetExpressions)
	register("System.Linq.Expressions.ConditionalExpression::get_Test", true, conditionalExpressionGetTest)
	register("System.Linq.Expressions.ConditionalExpression::get_IfTrue", true, conditionalExpressionGetIfTrue)
	register("System.Linq.Expressions.ConditionalExpression::get_IfFalse", true, conditionalExpressionGetIfFalse)
	register("System.Linq.Expressions.InvocationExpression::get_Expression", true, invokeExpressionGetExpression)
	register("System.Linq.Expressions.InvocationExpression::get_Arguments", true, invokeExpressionGetArguments)
	// "System.Reflection.MemberInfo::get_Name" is registered once, in
	// system_type.go's own init() (typeGetName) — that native now checks
	// for this file's *nativeMemberInfo receiver shape too. Registering
	// it a second time here used to silently lose to system_type.go's
	// entry (register() always overwrites; Go runs init()s in
	// alphabetical-by-filename order within a package), breaking every
	// real MemberExpression.Member.Name lookup with "System.Type method
	// receiver is not a Type" (Fase 3.41 bug, found running the real
	// openxml-demo).
	// MethodBase.GetMethodFromHandle is an identity passthrough over
	// whatever LoadMethodToken already produced (see that IR
	// instruction's own doc comment) — the CastClass to MethodInfo
	// right after it in real IL is a no-op for vmnet's Value model
	// (nothing to actually narrow).
	register("System.Reflection.MethodBase::GetMethodFromHandle", true, methodBaseGetMethodFromHandle)
}

// exprArgsFrom collects an IEnumerable<Expression>-shaped argument's own
// elements — either a real CLI array (the overwhelmingly common case,
// `params Expression[]`) or a *nativeList (a caller-built
// List<Expression> passed as IEnumerable<Expression>, e.g.
// Expression.Block(IEnumerable<ParameterExpression>, ...)).
func exprArgsFrom(v runtime.Value) []runtime.Value {
	switch v.Kind {
	case runtime.KindArray:
		if v.Arr == nil {
			return nil
		}
		return v.Arr.Elems
	case runtime.KindObject:
		if l, ok := nativeOf[*nativeList](v); ok {
			return l.items
		}
	}
	return nil
}

func expressionParameter(args []runtime.Value) (runtime.Value, error) {
	p := &nativeParameterExpression{}
	if len(args) > 0 {
		if name, ok := TypeFullNameOf(args[0]); ok {
			p.typeName = name
		}
	}
	if len(args) > 1 && args[1].Kind == runtime.KindString {
		p.name = args[1].Str
	}
	return runtime.ObjRef(&runtime.Object{Native: p}), nil
}

func parameterExpressionGetName(args []runtime.Value) (runtime.Value, error) {
	p, ok := nativeOf[*nativeParameterExpression](firstArg(args))
	if !ok {
		return runtime.Null(), nil
	}
	if p.name == "" {
		return runtime.Null(), nil
	}
	return runtime.String(p.name), nil
}

func methodBaseGetMethodFromHandle(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 {
		return runtime.Null(), nil
	}
	return args[0], nil
}

// expressionProperty backs BOTH real Expression.Property overloads:
// (Expression, MethodInfo) — a property accessor method handle, the
// shape DocumentFormat.OpenXml's own ConfigureMetadata always uses — and
// (Expression, PropertyInfo) — a real PropertyInfo directly, which also
// carries the property's own declared type (PropertyInfoParts), unlike
// the MethodInfo overload.
func expressionProperty(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, nil
	}
	if _, propName, _, _, ok := PropertyInfoParts(args[1]); ok {
		propTypeName, _ := propertyInfoTypeNameOf(args[1])
		return runtime.ObjRef(&runtime.Object{Native: &nativeMemberExpression{propertyName: propName, expression: args[0], typeName: propTypeName}}), nil
	}
	_, methodName, ok := MethodInfoParts(args[1])
	if !ok {
		return runtime.Value{}, nil
	}
	name := methodName
	if p, ok := strings.CutPrefix(name, "get_"); ok {
		name = p
	} else if p, ok := strings.CutPrefix(name, "set_"); ok {
		name = p
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeMemberExpression{propertyName: name, expression: args[0]}}), nil
}

// expressionLambda backs BOTH real Lambda factories: the generic
// Lambda<TDelegate>(Expression body, params ParameterExpression[]
// parameters) — TDelegate is never consulted (see exprcompile.go's own
// doc comment for why Compile() doesn't need it either) — and the
// non-generic Lambda(Type delegateType, Expression body, params
// ParameterExpression[] parameters) (Fase 3.81, found via CsvHelper's own
// ObjectRecordCreator building `Expression.Lambda(typeof(Func<>).
// MakeGenericType(recordType), body)`, needed since the record type is
// only known at runtime — no TDelegate to close over at compile time).
// Disambiguated by whether args[0] is itself a recognized Expression node
// at all: a real Type value (delegateType) never is, so it can never be
// confused with a real body expression.
func expressionLambda(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 {
		return runtime.Value{}, nil
	}
	bodyIdx := 0
	if KindOfExprNode(args[0]) == ExprNodeNone {
		if len(args) < 2 {
			return runtime.Value{}, fmt.Errorf("bcl: Expression.Lambda(Type, ...) expects a body expression")
		}
		bodyIdx = 1
	}
	var params []runtime.Value
	if len(args) > bodyIdx+1 {
		params = exprArgsFrom(args[bodyIdx+1])
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeLambdaExpression{body: args[bodyIdx], parameters: params}}), nil
}

func lambdaExpressionGetBody(args []runtime.Value) (runtime.Value, error) {
	le, ok := nativeOf[*nativeLambdaExpression](firstArg(args))
	if !ok {
		return runtime.Null(), nil
	}
	return le.body, nil
}

func lambdaExpressionGetParameters(args []runtime.Value) (runtime.Value, error) {
	le, ok := nativeOf[*nativeLambdaExpression](firstArg(args))
	if !ok {
		return runtime.ArrRef(runtime.NewArray(0)), nil
	}
	return runtime.ArrRef(&runtime.Array{Elems: append([]runtime.Value(nil), le.parameters...)}), nil
}

func memberExpressionGetMember(args []runtime.Value) (runtime.Value, error) {
	me, ok := nativeOf[*nativeMemberExpression](firstArg(args))
	if !ok {
		return runtime.Null(), nil
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeMemberInfo{name: me.propertyName}}), nil
}

func memberExpressionGetExpression(args []runtime.Value) (runtime.Value, error) {
	me, ok := nativeOf[*nativeMemberExpression](firstArg(args))
	if !ok {
		return runtime.Null(), nil
	}
	return me.expression, nil
}

// Real System.Linq.Expressions.ExpressionType values (confirmed against
// a real .NET 10 Enum.GetValues(typeof(ExpressionType)) run, not
// recalled from memory — a wrong constant here would be a silent
// mismatch, not a crash).
const (
	exprTypeArrayIndex          = 2
	exprTypeCall                = 6
	exprTypeConstant            = 9
	exprTypeConvert             = 10
	exprTypeConvertChecked      = 11
	exprTypeInvoke              = 17
	exprTypeLambda              = 18
	exprTypeMemberAccess        = 23
	exprTypeNew                 = 31
	exprTypeNewArrayInit        = 32
	exprTypeParameter           = 38
	exprTypeAssign              = 46
	exprTypeBlock               = 47
	exprTypeDefault             = 51
	exprTypeConditional         = 8
	exprTypeEqual               = 13
	exprTypeNotEqual            = 35
	exprTypePreIncrementAssign  = 77
	exprTypePreDecrementAssign  = 78
	exprTypePostIncrementAssign = 79
	exprTypePostDecrementAssign = 80
	exprTypeThrow               = 60
	exprTypeCoalesce            = 7
	exprTypeTry                 = 61
)

// Exported mirrors of the exprTypeXxx values above, needed by
// internal/interpreter/exprcompile.go to tell which real
// increment/decrement/reference-compare variant an
// IncDecExpressionParts/BinaryCompareExpressionParts opType holds —
// everything else about a node's shape is already exposed through its
// own typed accessor, but opType itself is a bare int since it doubles
// as the real NodeType value returned by expressionGetNodeType.
const (
	ExprTypeEqual               = exprTypeEqual
	ExprTypeNotEqual            = exprTypeNotEqual
	ExprTypePreIncrementAssign  = exprTypePreIncrementAssign
	ExprTypePreDecrementAssign  = exprTypePreDecrementAssign
	ExprTypePostIncrementAssign = exprTypePostIncrementAssign
	ExprTypePostDecrementAssign = exprTypePostDecrementAssign
)

// expressionGetNodeType backs Expression.NodeType — real consumers
// (FluentValidation's own PropertyRule/MemberAccessor construction,
// AutoMapper's own mapping-plan generation) use it to tell which
// concrete Expression subclass a body/parent actually is before casting.
func expressionGetNodeType(args []runtime.Value) (runtime.Value, error) {
	v := firstArg(args)
	if v.Kind == runtime.KindObject && v.Obj != nil {
		switch n := v.Obj.Native.(type) {
		case *nativeMemberExpression:
			return runtime.Int32(exprTypeMemberAccess), nil
		case *nativeParameterExpression:
			return runtime.Int32(exprTypeParameter), nil
		case *nativeLambdaExpression:
			return runtime.Int32(exprTypeLambda), nil
		case *nativeConstantExpression:
			return runtime.Int32(exprTypeConstant), nil
		case *nativeCallExpression:
			return runtime.Int32(exprTypeCall), nil
		case *nativeNewExpression:
			return runtime.Int32(exprTypeNew), nil
		case *nativeNewArrayExpression:
			return runtime.Int32(exprTypeNewArrayInit), nil
		case *nativeConvertExpression:
			return runtime.Int32(exprTypeConvert), nil
		case *nativeAssignExpression:
			return runtime.Int32(exprTypeAssign), nil
		case *nativeBlockExpression:
			return runtime.Int32(exprTypeBlock), nil
		case *nativeDefaultExpression:
			return runtime.Int32(exprTypeDefault), nil
		case *nativeConditionalExpression:
			return runtime.Int32(exprTypeConditional), nil
		case *nativeInvokeExpression:
			return runtime.Int32(exprTypeInvoke), nil
		case *nativeIncDecExpression:
			return runtime.Int32(int32(n.opType)), nil
		case *nativeBinaryCompareExpression:
			return runtime.Int32(int32(n.opType)), nil
		case *nativeThrowExpression:
			return runtime.Int32(exprTypeThrow), nil
		case *nativeCoalesceExpression:
			return runtime.Int32(exprTypeCoalesce), nil
		case *nativeTryExpression:
			return runtime.Int32(exprTypeTry), nil
		case *nativeArrayIndexExpression:
			return runtime.Int32(exprTypeArrayIndex), nil
		}
	}
	return runtime.Value{}, fmt.Errorf("bcl: Expression.NodeType: receiver isn't one of the Expression shapes this subsystem models (see system_linq_expressions.go's own doc comment)")
}

// expressionGetType backs Expression.Type — every node's own typeName
// field, populated at construction time from data the real .NET caller
// already had (the explicit Type argument to Constant/Parameter/Convert/
// Default/New, or PropertyInfoParts' own propertyTypeFullName for
// Expression.Property's PropertyInfo overload). Falls back to
// "System.Object" when a node's own typeName is genuinely unavailable
// (the MethodInfo-based Property overload, a Call node's own return
// type, ...) rather than erroring — a real caller reading .Type mainly
// to decide whether/how to emit a Convert node still gets a usable,
// if imprecise, answer instead of a hard failure.
func expressionGetType(args []runtime.Value) (runtime.Value, error) {
	v := firstArg(args)
	name := "System.Object"
	if v.Kind == runtime.KindObject && v.Obj != nil {
		switch n := v.Obj.Native.(type) {
		case *nativeParameterExpression:
			if n.typeName != "" {
				name = n.typeName
			}
		case *nativeMemberExpression:
			if n.typeName != "" {
				name = n.typeName
			}
		case *nativeConstantExpression:
			if n.typeName != "" {
				name = n.typeName
			}
		case *nativeConvertExpression:
			if n.typeName != "" {
				name = n.typeName
			}
		case *nativeDefaultExpression:
			if n.typeName != "" {
				name = n.typeName
			}
		case *nativeNewExpression:
			if n.typeName != "" {
				name = n.typeName
			}
		case *nativeNewArrayExpression:
			if n.elemTypeName != "" {
				name = n.elemTypeName + "[]"
			}
		case *nativeAssignExpression:
			return expressionGetType([]runtime.Value{n.left})
		case *nativeBlockExpression:
			if len(n.body) > 0 {
				return expressionGetType(n.body[len(n.body)-1:])
			}
		case *nativeConditionalExpression:
			return expressionGetType([]runtime.Value{n.ifTrue})
		case *nativeIncDecExpression:
			return expressionGetType([]runtime.Value{n.operand})
		case *nativeBinaryCompareExpression:
			name = "System.Boolean"
		case *nativeThrowExpression:
			if n.typeName != "" {
				name = n.typeName
			}
		case *nativeCoalesceExpression:
			return expressionGetType([]runtime.Value{n.left})
		case *nativeTryExpression:
			return expressionGetType([]runtime.Value{n.body})
		case *nativeArrayIndexExpression:
			// Real .NET reports array.Type.GetElementType() — approximated
			// by stripping a trailing "[]" off the array sub-expression's
			// own .Type when it's array-shaped, else the "System.Object"
			// default above (every real caller found so far immediately
			// wraps this in an explicit Expression.Convert anyway, so this
			// answer is rarely, if ever, load-bearing on its own).
			arrType, err := expressionGetType([]runtime.Value{n.array})
			if err == nil {
				if arrName, ok := TypeFullNameOf(arrType); ok && strings.HasSuffix(arrName, "[]") {
					name = strings.TrimSuffix(arrName, "[]")
				}
			}
		}
	}
	return NewTypeValue(name), nil
}

func expressionConstant(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 {
		return runtime.Value{}, fmt.Errorf("bcl: Expression.Constant expects a value")
	}
	c := &nativeConstantExpression{value: args[0]}
	if len(args) > 1 {
		if name, ok := TypeFullNameOf(args[1]); ok {
			c.typeName = name
		}
	}
	return runtime.ObjRef(&runtime.Object{Native: c}), nil
}

// expressionCall backs both real Call overloads: (MethodInfo, Expression[])
// static — always exactly 2 arguments — and (Expression, MethodInfo,
// Expression[]) instance — always exactly 3. Arity alone disambiguates
// them (see this file's own top doc comment).
func expressionCall(args []runtime.Value) (runtime.Value, error) {
	// Call(Expression instance, string methodName, Type[] typeArguments,
	// params Expression[] arguments) — the string-name overload (Fase
	// 3.81, found via CsvHelper's own ExpressionManager/
	// PrimitiveRecordCreator/PrimitiveRecordWriter/ObjectRecordWriter, all
	// of which build `Expression.Call(Expression.Constant(converterOrWriter),
	// "ConvertFromString"/"WriteField"/..., null, ...)`). Unlike every
	// other shape below, there's no MethodInfo here to name a declaring
	// type up front — the method is resolved against instance's own real
	// runtime type at EVALUATION time instead (exprcompile.go's
	// ExprNodeCall case, triggered by typeName == "", something a real
	// MethodInfo's declaring type can never be). A bare string in
	// position 1 can never be confused with a MethodInfo/Expression node,
	// so this check runs before the arity-based disambiguation below.
	//
	// `params Expression[] arguments` always compiles down to exactly ONE
	// real 4th argument — a genuine `Expression[]` array the C# compiler
	// builds at the call site, regardless of how many individual
	// expressions the caller's own source passed (one, several, or zero)
	// — never several separate trailing arguments. args[3] (when present)
	// is unwrapped via exprArgsFrom accordingly, not sliced directly.
	if len(args) >= 3 && args[1].Kind == runtime.KindString {
		var callArgs []runtime.Value
		if len(args) > 3 {
			callArgs = exprArgsFrom(args[3])
		}
		return runtime.ObjRef(&runtime.Object{Native: &nativeCallExpression{
			instance:   args[0],
			methodName: args[1].Str,
			args:       callArgs,
		}}), nil
	}
	var instance, methodArg, argsArg runtime.Value
	switch len(args) {
	case 2:
		// Three real, distinct overloads share this arity: Call(MethodInfo
		// method, params Expression[] arguments) and Call(MethodInfo
		// method, Expression arg0) — both STATIC, no instance — and
		// Call(Expression instance, MethodInfo method) — an instance call
		// with zero extra arguments (found via AutoMapper's own
		// ExpressionBuilder static constructor, which uses all three:
		// `Expression.Call(CheckContextMethod, ContextParameter)` (single
		// bare Expression, not an array) and `Expression.Call(disposable,
		// DisposeMethod)` (instance call, no args)). Disambiguated by
		// which position actually holds a MethodInfo, and — for that
		// case — whether the OTHER argument is itself a real Expression
		// node (a bare single argument) rather than an array/list of them.
		if _, _, ok := MethodInfoParts(args[0]); ok {
			instance = runtime.Null()
			methodArg = args[0]
			if KindOfExprNode(args[1]) != ExprNodeNone {
				argsArg = runtime.ArrRef(&runtime.Array{Elems: []runtime.Value{args[1]}})
			} else {
				argsArg = args[1]
			}
		} else {
			instance, methodArg, argsArg = args[0], args[1], runtime.Value{}
		}
	case 3:
		// Two more real, distinct overloads share THIS arity: Call(
		// MethodInfo method, Expression arg0, Expression arg1) — STATIC,
		// two bare args, no array — and Call(Expression instance,
		// MethodInfo method, Expression[]/IEnumerable<Expression>
		// arguments) — instance + method + an actual array/list. Same
		// disambiguation posture as the 2-arg case above: whichever
		// position actually holds a MethodInfo wins.
		if _, _, ok := MethodInfoParts(args[0]); ok {
			instance = runtime.Null()
			methodArg = args[0]
			argsArg = runtime.ArrRef(&runtime.Array{Elems: []runtime.Value{args[1], args[2]}})
		} else {
			instance, methodArg, argsArg = args[0], args[1], args[2]
		}
	case 4:
		// Same disambiguation posture, one arg wider (Fase 3.81): Call(
		// MethodInfo method, Expression arg0, Expression arg1, Expression
		// arg2) — STATIC, three bare args, no array — vs Call(Expression
		// instance, MethodInfo method, Expression arg0, Expression arg1) —
		// instance + method + two bare args. The far more common 4-arg
		// shape this project has actually hit (CsvHelper's own
		// TypeConverter.ConvertFromString/WriteField calls) is the
		// string-name overload intercepted above before this switch ever
		// runs; this MethodInfo-based case only fires for a genuine
		// MethodInfo-carrying 4-arg call.
		if _, _, ok := MethodInfoParts(args[0]); ok {
			instance = runtime.Null()
			methodArg = args[0]
			argsArg = runtime.ArrRef(&runtime.Array{Elems: []runtime.Value{args[1], args[2], args[3]}})
		} else {
			instance = args[0]
			methodArg = args[1]
			argsArg = runtime.ArrRef(&runtime.Array{Elems: []runtime.Value{args[2], args[3]}})
		}
	default:
		return runtime.Value{}, fmt.Errorf("bcl: Expression.Call: unsupported argument shape (%d args)", len(args))
	}
	typeName, methodName, ok := MethodInfoParts(methodArg)
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: Expression.Call: 2nd argument is not a MethodInfo (nargs=%d)", len(args))
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeCallExpression{
		instance:   instance,
		typeName:   typeName,
		methodName: methodName,
		args:       exprArgsFrom(argsArg),
	}}), nil
}

func callExpressionGetObject(args []runtime.Value) (runtime.Value, error) {
	c, ok := nativeOf[*nativeCallExpression](firstArg(args))
	if !ok {
		return runtime.Null(), nil
	}
	return c.instance, nil
}

func callExpressionGetArguments(args []runtime.Value) (runtime.Value, error) {
	c, ok := nativeOf[*nativeCallExpression](firstArg(args))
	if !ok {
		return runtime.ArrRef(runtime.NewArray(0)), nil
	}
	return runtime.ArrRef(&runtime.Array{Elems: append([]runtime.Value(nil), c.args...)}), nil
}

func callExpressionGetMethod(args []runtime.Value) (runtime.Value, error) {
	c, ok := nativeOf[*nativeCallExpression](firstArg(args))
	if !ok {
		return runtime.Null(), nil
	}
	return NewMethodInfoValue(c.typeName, c.methodName), nil
}

// expressionNew backs Expression.New(ConstructorInfo, params Expression[])
// and Expression.New(Type) (a parameterless constructor, given just the
// type — no ConstructorInfo at all).
func expressionNew(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 {
		return runtime.Value{}, fmt.Errorf("bcl: Expression.New expects at least 1 argument")
	}
	if typeName, ok := TypeFullNameOf(args[0]); ok {
		return runtime.ObjRef(&runtime.Object{Native: &nativeNewExpression{typeName: typeName}}), nil
	}
	// The CLOSED name (Fase 3.81), not ConstructorInfoParts's own open
	// one — this nativeNewExpression eventually feeds Machine.New/newObj
	// the same way ConstructorInfo.Invoke does (Expression.New(ctor).
	// Compile()/Invoke() is exactly the pattern CsvHelper's own
	// AutoMap()-based construction uses), and only a still-closed name
	// has any ClassGenericArgs for Machine.New to find. See
	// nativeConstructorInfo.closedTypeFullName's own doc comment.
	typeName, ok := ConstructorInfoConstructTypeFullName(args[0])
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: Expression.New: 1st argument is not a Type or ConstructorInfo")
	}
	var ctorArgs []runtime.Value
	if len(args) > 1 {
		ctorArgs = exprArgsFrom(args[1])
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeNewExpression{typeName: typeName, args: ctorArgs}}), nil
}

func expressionNewArrayInit(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: Expression.NewArrayInit expects a Type argument")
	}
	elemTypeName, _ := TypeFullNameOf(args[0])
	var elems []runtime.Value
	if len(args) > 1 {
		elems = exprArgsFrom(args[1])
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeNewArrayExpression{elemTypeName: elemTypeName, elements: elems}}), nil
}

func newArrayExpressionGetExpressions(args []runtime.Value) (runtime.Value, error) {
	n, ok := nativeOf[*nativeNewArrayExpression](firstArg(args))
	if !ok {
		return runtime.ArrRef(runtime.NewArray(0)), nil
	}
	return runtime.ArrRef(&runtime.Array{Elems: append([]runtime.Value(nil), n.elements...)}), nil
}

func expressionConvert(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: Expression.Convert expects (Expression, Type)")
	}
	typeName, _ := TypeFullNameOf(args[1])
	return runtime.ObjRef(&runtime.Object{Native: &nativeConvertExpression{operand: args[0], typeName: typeName}}), nil
}

func convertExpressionGetOperand(args []runtime.Value) (runtime.Value, error) {
	if c, ok := nativeOf[*nativeConvertExpression](firstArg(args)); ok {
		return c.operand, nil
	}
	if i, ok := nativeOf[*nativeIncDecExpression](firstArg(args)); ok {
		return i.operand, nil
	}
	if t, ok := nativeOf[*nativeThrowExpression](firstArg(args)); ok {
		return t.value, nil
	}
	return runtime.Null(), nil
}

// nativeIncDecExpression backs Expression.PreIncrementAssign/
// PostIncrementAssign/PreDecrementAssign/PostDecrementAssign (Fase 3.65,
// found via AutoMapper's own ExpressionBuilder static constructor
// building a real array-copy loop template: `Expression.PostIncrementAssign
// (indexVariable)`). Reported as a real UnaryExpression (matching .NET's
// own concrete type for these four factory methods), with opType holding
// the real ExpressionType so exprTypeXxx-based NodeType/evaluation both
// see the right one.
type nativeIncDecExpression struct {
	opType  int
	operand runtime.Value
}

func expressionPreIncrementAssign(args []runtime.Value) (runtime.Value, error) {
	return newIncDecExpression(exprTypePreIncrementAssign, args)
}

func expressionPostIncrementAssign(args []runtime.Value) (runtime.Value, error) {
	return newIncDecExpression(exprTypePostIncrementAssign, args)
}

func expressionPreDecrementAssign(args []runtime.Value) (runtime.Value, error) {
	return newIncDecExpression(exprTypePreDecrementAssign, args)
}

func expressionPostDecrementAssign(args []runtime.Value) (runtime.Value, error) {
	return newIncDecExpression(exprTypePostDecrementAssign, args)
}

func newIncDecExpression(opType int, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: Expression increment/decrement factory expects (Expression operand)")
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeIncDecExpression{opType: opType, operand: args[0]}}), nil
}

// IncDecExpressionParts exposes opType/operand to
// internal/interpreter/exprcompile.go — opType is one of the real
// exprTypeXxx ExpressionType values above, letting the evaluator pick
// increment vs. decrement and pre- vs. post- semantics.
func IncDecExpressionParts(v runtime.Value) (opType int, operand runtime.Value, ok bool) {
	i, ok := nativeOf[*nativeIncDecExpression](v)
	if !ok {
		return 0, runtime.Value{}, false
	}
	return i.opType, i.operand, true
}

// nativeBinaryCompareExpression backs Expression.ReferenceEqual/
// ReferenceNotEqual (Fase 3.65, found via the same AutoMapper
// ExpressionBuilder static constructor: `Expression.ReferenceNotEqual
// (disposable, Null)` guarding a real IDisposable.Dispose() call).
// Reported as a real BinaryExpression; comparison is always by reference
// identity (never a user-defined == operator), matching what
// ReferenceEqual/ReferenceNotEqual actually mean in real .NET.
type nativeBinaryCompareExpression struct {
	opType      int
	left, right runtime.Value
}

func expressionReferenceEqual(args []runtime.Value) (runtime.Value, error) {
	return newBinaryCompareExpression(exprTypeEqual, args)
}

func expressionReferenceNotEqual(args []runtime.Value) (runtime.Value, error) {
	return newBinaryCompareExpression(exprTypeNotEqual, args)
}

func newBinaryCompareExpression(opType int, args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: Expression reference-compare factory expects (Expression left, Expression right)")
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeBinaryCompareExpression{opType: opType, left: args[0], right: args[1]}}), nil
}

// BinaryCompareExpressionParts exposes opType/left/right to
// internal/interpreter/exprcompile.go, mirroring IncDecExpressionParts.
func BinaryCompareExpressionParts(v runtime.Value) (opType int, left, right runtime.Value, ok bool) {
	b, ok := nativeOf[*nativeBinaryCompareExpression](v)
	if !ok {
		return 0, runtime.Value{}, runtime.Value{}, false
	}
	return b.opType, b.left, b.right, true
}

// nativeArrayIndexExpression backs Expression.ArrayIndex(array, index)
// (Fase 3.81, found via CsvHelper's own ObjectCreator.CreateInstanceFunc —
// `Expression.Convert(Expression.ArrayIndex(parameterExpression,
// Expression.Constant(j)), paramType)`, building `(ParamType)args[j]` for
// each constructor argument off a real `object[] args` parameter). Only
// ever evaluated, never rebuilt/visited (unlike MemberExpression's own
// exprvisitor.go handling) — no real caller found so far needs to walk
// into one.
type nativeArrayIndexExpression struct {
	array, index runtime.Value
}

func expressionArrayIndex(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: Expression.ArrayIndex expects (Expression array, Expression index)")
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeArrayIndexExpression{array: args[0], index: args[1]}}), nil
}

// ArrayIndexExpressionParts exposes array/index to exprcompile.go's own
// evaluator, mirroring BinaryCompareExpressionParts/CoalesceExpressionParts.
func ArrayIndexExpressionParts(v runtime.Value) (array, index runtime.Value, ok bool) {
	a, ok := nativeOf[*nativeArrayIndexExpression](v)
	if !ok {
		return runtime.Value{}, runtime.Value{}, false
	}
	return a.array, a.index, true
}

// nativeMemberAssignment backs System.Linq.Expressions.MemberAssignment,
// returned by Expression.Bind(MemberInfo, Expression) (Fase 3.81, found
// via CsvHelper's own ExpressionManager building a member/reference map's
// MemberAssignment list). Unlike every other native type in this file,
// this is NOT itself an Expression subtype in real .NET (MemberBinding is
// a separate base hierarchy, only ever nested inside a
// MemberInitExpression/ListInitExpression) — deliberately never wired
// into KindOfExprNode/expressionGetNodeType/expressionGetType, same
// posture as nativeCatchBlock below: real callers found so far only ever
// read a MemberAssignment's own .Member/.Expression properties back
// (CreateInstanceAndAssignMembers's own `assignments.Select((MemberAssignment
// b) => Expression.Assign(Expression.MakeMemberAccess(instance, b.Member),
// b.Expression))`), never evaluate it as a tree node in its own right.
type nativeMemberAssignment struct {
	member, expr runtime.Value
}

func expressionBind(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: Expression.Bind expects (MemberInfo member, Expression expression)")
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeMemberAssignment{member: args[0], expr: args[1]}}), nil
}

func memberAssignmentGetMember(args []runtime.Value) (runtime.Value, error) {
	ma, ok := nativeOf[*nativeMemberAssignment](firstArg(args))
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: MemberAssignment.Member: receiver is not a MemberAssignment")
	}
	return ma.member, nil
}

func memberAssignmentGetExpression(args []runtime.Value) (runtime.Value, error) {
	ma, ok := nativeOf[*nativeMemberAssignment](firstArg(args))
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: MemberAssignment.Expression: receiver is not a MemberAssignment")
	}
	return ma.expr, nil
}

// nativeCoalesceExpression backs Expression.Coalesce(left, right) — the
// `??` null-coalescing operator, a real BinaryExpression with its own
// value-producing semantics (unlike ReferenceEqual/NotEqual's boolean
// result): evaluates left; if it isn't null, THAT's the result; otherwise
// right is evaluated and returned. Found via AutoMapper's own
// mapping-plan generation guarding a possibly-null source member before
// mapping it. Approximates the node's own .Type as left's declared
// type (real .NET computes the least-derived common type of both
// branches; every real caller found so far has both sides share one
// type already).
type nativeCoalesceExpression struct {
	left, right runtime.Value
}

func expressionCoalesce(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: Expression.Coalesce expects (Expression left, Expression right)")
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeCoalesceExpression{left: args[0], right: args[1]}}), nil
}

// CoalesceExpressionParts exposes left/right to exprcompile.go's own
// evaluator and exprvisitor.go's own rebuild logic.
func CoalesceExpressionParts(v runtime.Value) (left, right runtime.Value, ok bool) {
	c, ok := nativeOf[*nativeCoalesceExpression](v)
	if !ok {
		return runtime.Value{}, runtime.Value{}, false
	}
	return c.left, c.right, true
}

func NewCoalesceExpressionValue(left, right runtime.Value) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeCoalesceExpression{left: left, right: right}})
}

// nativeCatchBlock backs System.Linq.Expressions.CatchBlock — unlike
// every other node type in this file, a real CatchBlock is NOT itself an
// Expression subtype (it has no NodeType/Type of its own), so it's
// deliberately never wired into KindOfExprNode/expressionGetNodeType/
// expressionGetType — only TryExpression (below) ever holds one, and
// only exprcompile.go's own Try evaluation ever reads it. Found via
// AutoMapper's own mapping-plan generation wrapping a property-mapping
// expression in a real `try { ... } catch (Exception ex) { ... }`
// template for its own contextual exception messages.
type nativeCatchBlock struct {
	testType string
	variable runtime.Value // KindNull for the Catch(Type, Expression) overload (no bound variable)
	body     runtime.Value
}

func expressionCatch(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: Expression.Catch expects (Type|ParameterExpression, Expression body)")
	}
	cb := &nativeCatchBlock{body: args[1], variable: runtime.Null()}
	if IsParameterExpression(args[0]) {
		cb.variable = args[0]
		cb.testType, _ = ParameterExpressionTypeName(args[0])
	} else if name, ok := TypeFullNameOf(args[0]); ok {
		cb.testType = name
	} else {
		return runtime.Value{}, fmt.Errorf("bcl: Expression.Catch: 1st argument is not a Type or ParameterExpression")
	}
	return runtime.ObjRef(&runtime.Object{Native: cb}), nil
}

// CatchBlockParts exposes a CatchBlock's own test type, bound exception
// variable (KindNull if none), and handler body to exprcompile.go's own
// Try evaluator.
func CatchBlockParts(v runtime.Value) (testType string, variable, body runtime.Value, ok bool) {
	cb, ok := nativeOf[*nativeCatchBlock](v)
	if !ok {
		return "", runtime.Value{}, runtime.Value{}, false
	}
	return cb.testType, cb.variable, cb.body, true
}

// nativeTryExpression backs Expression.TryCatch/TryFinally/
// TryCatchFinally — a real TryExpression, evaluated for real (not just
// modeled) by exprcompile.go's own ExprNodeTry case: runs body, and on a
// real thrown exception, dispatches to the first matching catch (by
// real exception-hierarchy assignability, internal/interpreter/
// exceptions.go's own exceptionMatchesCatch), then always runs finally
// (if any) on the way out, matching real .NET's own try/catch/finally
// semantics.
type nativeTryExpression struct {
	body    runtime.Value
	catches []runtime.Value // each a *nativeCatchBlock
	finally runtime.Value   // KindNull if no finally block
}

func expressionTryCatch(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: Expression.TryCatch expects (Expression body, params CatchBlock[] handlers)")
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeTryExpression{body: args[0], catches: exprArgsFrom(argsOrEmpty(args, 1)), finally: runtime.Null()}}), nil
}

func expressionTryFinally(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: Expression.TryFinally expects (Expression body, Expression finallyBlock)")
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeTryExpression{body: args[0], finally: args[1]}}), nil
}

func expressionTryCatchFinally(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 3 {
		return runtime.Value{}, fmt.Errorf("bcl: Expression.TryCatchFinally expects (Expression body, Expression finallyBlock, params CatchBlock[] handlers)")
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeTryExpression{body: args[0], finally: args[1], catches: exprArgsFrom(argsOrEmpty(args, 2))}}), nil
}

// argsOrEmpty returns args[i] if present, a zero Value (KindNull-ish,
// exprArgsFrom's own default-to-nil path) otherwise — a `params
// CatchBlock[]` argument omitted entirely case-2's own arity already
// guards against isn't reachable through the registered overloads above
// but this keeps index access safe regardless.
func argsOrEmpty(args []runtime.Value, i int) runtime.Value {
	if i < len(args) {
		return args[i]
	}
	return runtime.Value{}
}

// TryExpressionParts exposes body/catches/finally to exprcompile.go's
// own evaluator and exprvisitor.go's own rebuild logic.
func TryExpressionParts(v runtime.Value) (body runtime.Value, catches []runtime.Value, finallyExpr runtime.Value, ok bool) {
	t, ok := nativeOf[*nativeTryExpression](v)
	if !ok {
		return runtime.Value{}, nil, runtime.Value{}, false
	}
	return t.body, t.catches, t.finally, true
}

func NewTryExpressionValue(body runtime.Value, catches []runtime.Value, finallyExpr runtime.Value) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeTryExpression{body: body, catches: catches, finally: finallyExpr}})
}

func NewCatchBlockValue(testType string, variable, body runtime.Value) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeCatchBlock{testType: testType, variable: variable, body: body}})
}

func tryExpressionGetBody(args []runtime.Value) (runtime.Value, error) {
	t, ok := nativeOf[*nativeTryExpression](firstArg(args))
	if !ok {
		return runtime.Null(), nil
	}
	return t.body, nil
}

func tryExpressionGetHandlers(args []runtime.Value) (runtime.Value, error) {
	t, ok := nativeOf[*nativeTryExpression](firstArg(args))
	if !ok {
		return runtime.ArrRef(runtime.NewArray(0)), nil
	}
	return runtime.ArrRef(&runtime.Array{Elems: append([]runtime.Value(nil), t.catches...)}), nil
}

func tryExpressionGetFinally(args []runtime.Value) (runtime.Value, error) {
	t, ok := nativeOf[*nativeTryExpression](firstArg(args))
	if !ok {
		return runtime.Null(), nil
	}
	return t.finally, nil
}

func catchBlockGetTest(args []runtime.Value) (runtime.Value, error) {
	cb, ok := nativeOf[*nativeCatchBlock](firstArg(args))
	if !ok {
		return runtime.Null(), nil
	}
	return NewTypeValue(cb.testType), nil
}

func catchBlockGetVariable(args []runtime.Value) (runtime.Value, error) {
	cb, ok := nativeOf[*nativeCatchBlock](firstArg(args))
	if !ok {
		return runtime.Null(), nil
	}
	return cb.variable, nil
}

func catchBlockGetBody(args []runtime.Value) (runtime.Value, error) {
	cb, ok := nativeOf[*nativeCatchBlock](firstArg(args))
	if !ok {
		return runtime.Null(), nil
	}
	return cb.body, nil
}

func expressionAssign(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: Expression.Assign expects (Expression left, Expression right)")
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeAssignExpression{left: args[0], right: args[1]}}), nil
}

func assignExpressionGetLeft(args []runtime.Value) (runtime.Value, error) {
	if a, ok := nativeOf[*nativeAssignExpression](firstArg(args)); ok {
		return a.left, nil
	}
	if b, ok := nativeOf[*nativeBinaryCompareExpression](firstArg(args)); ok {
		return b.left, nil
	}
	if c, ok := nativeOf[*nativeCoalesceExpression](firstArg(args)); ok {
		return c.left, nil
	}
	return runtime.Null(), nil
}

func assignExpressionGetRight(args []runtime.Value) (runtime.Value, error) {
	if a, ok := nativeOf[*nativeAssignExpression](firstArg(args)); ok {
		return a.right, nil
	}
	if b, ok := nativeOf[*nativeBinaryCompareExpression](firstArg(args)); ok {
		return b.right, nil
	}
	if c, ok := nativeOf[*nativeCoalesceExpression](firstArg(args)); ok {
		return c.right, nil
	}
	return runtime.Null(), nil
}

// expressionBlock backs Block(IEnumerable<ParameterExpression>,
// Expression[]), Block(Expression[]) (no locals), and Block(Type,
// Expression[]) (an explicit result type — accepted and ignored, same
// "best-effort .Type" posture the rest of this file takes) — 2 args
// where args[0] is a Type means the 3rd shape; 2 args where args[0] is a
// variables collection means the 1st; 1 arg is always the 2nd.
func expressionBlock(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 {
		return runtime.Value{}, fmt.Errorf("bcl: Expression.Block expects at least 1 argument")
	}
	if len(args) == 1 {
		return runtime.ObjRef(&runtime.Object{Native: &nativeBlockExpression{body: exprArgsFrom(args[0])}}), nil
	}
	// 2 arguments: (variables, expressions) or (Type, expressions) — a
	// Type argument is never itself an Expression-shaped object, so
	// checking for that shape tells the two apart.
	if _, ok := TypeFullNameOf(args[0]); ok {
		return runtime.ObjRef(&runtime.Object{Native: &nativeBlockExpression{body: exprArgsFrom(args[1])}}), nil
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeBlockExpression{
		variables: exprArgsFrom(args[0]),
		body:      exprArgsFrom(args[1]),
	}}), nil
}

func blockExpressionGetVariables(args []runtime.Value) (runtime.Value, error) {
	b, ok := nativeOf[*nativeBlockExpression](firstArg(args))
	if !ok {
		return runtime.ArrRef(runtime.NewArray(0)), nil
	}
	return runtime.ArrRef(&runtime.Array{Elems: append([]runtime.Value(nil), b.variables...)}), nil
}

func blockExpressionGetExpressions(args []runtime.Value) (runtime.Value, error) {
	b, ok := nativeOf[*nativeBlockExpression](firstArg(args))
	if !ok {
		return runtime.ArrRef(runtime.NewArray(0)), nil
	}
	return runtime.ArrRef(&runtime.Array{Elems: append([]runtime.Value(nil), b.body...)}), nil
}

func expressionDefault(args []runtime.Value) (runtime.Value, error) {
	typeName := ""
	if len(args) > 0 {
		typeName, _ = TypeFullNameOf(args[0])
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeDefaultExpression{typeName: typeName}}), nil
}

func expressionEmpty(args []runtime.Value) (runtime.Value, error) {
	return runtime.ObjRef(&runtime.Object{Native: &nativeDefaultExpression{typeName: "System.Void"}}), nil
}

// nativeThrowExpression backs Expression.Throw(Expression) and
// Expression.Throw(Expression, Type) — a real UnaryExpression (matching
// .NET's own concrete type for this factory) whose evaluation
// (internal/interpreter/exprcompile.go) actually raises the evaluated
// value as a real exception, not just models the tree's shape. Found via
// AutoMapper's own mapping-plan generation, which builds a real
// `throw new ArgumentNullException(...)` template for a null-source
// guard.
type nativeThrowExpression struct {
	value    runtime.Value
	typeName string
}

func expressionThrow(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: Expression.Throw expects (Expression value, ...)")
	}
	typeName := "System.Void"
	if len(args) > 1 {
		if name, ok := TypeFullNameOf(args[1]); ok {
			typeName = name
		}
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeThrowExpression{value: args[0], typeName: typeName}}), nil
}

// ThrowExpressionParts exposes the thrown value expression and the
// node's own declared .Type to exprcompile.go's own evaluator and
// exprvisitor.go's own rebuild logic.
func ThrowExpressionParts(v runtime.Value) (value runtime.Value, typeName string, ok bool) {
	t, ok := nativeOf[*nativeThrowExpression](v)
	if !ok {
		return runtime.Value{}, "", false
	}
	return t.value, t.typeName, true
}

func expressionIfThen(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: Expression.IfThen expects (test, ifTrue)")
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeConditionalExpression{test: args[0], ifTrue: args[1], ifFalse: runtime.Null()}}), nil
}

func expressionIfThenElse(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 3 {
		return runtime.Value{}, fmt.Errorf("bcl: Expression.IfThenElse/Condition expects (test, ifTrue, ifFalse)")
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeConditionalExpression{test: args[0], ifTrue: args[1], ifFalse: args[2]}}), nil
}

func conditionalExpressionGetTest(args []runtime.Value) (runtime.Value, error) {
	c, ok := nativeOf[*nativeConditionalExpression](firstArg(args))
	if !ok {
		return runtime.Null(), nil
	}
	return c.test, nil
}

func conditionalExpressionGetIfTrue(args []runtime.Value) (runtime.Value, error) {
	c, ok := nativeOf[*nativeConditionalExpression](firstArg(args))
	if !ok {
		return runtime.Null(), nil
	}
	return c.ifTrue, nil
}

func conditionalExpressionGetIfFalse(args []runtime.Value) (runtime.Value, error) {
	c, ok := nativeOf[*nativeConditionalExpression](firstArg(args))
	if !ok {
		return runtime.Null(), nil
	}
	return c.ifFalse, nil
}

func expressionInvoke(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: Expression.Invoke expects an Expression")
	}
	var invokeArgs []runtime.Value
	if len(args) > 1 {
		invokeArgs = exprArgsFrom(args[1])
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeInvokeExpression{expr: args[0], args: invokeArgs}}), nil
}

func invokeExpressionGetExpression(args []runtime.Value) (runtime.Value, error) {
	i, ok := nativeOf[*nativeInvokeExpression](firstArg(args))
	if !ok {
		return runtime.Null(), nil
	}
	return i.expr, nil
}

func invokeExpressionGetArguments(args []runtime.Value) (runtime.Value, error) {
	i, ok := nativeOf[*nativeInvokeExpression](firstArg(args))
	if !ok {
		return runtime.ArrRef(runtime.NewArray(0)), nil
	}
	return runtime.ArrRef(&runtime.Array{Elems: append([]runtime.Value(nil), i.args...)}), nil
}

func firstArg(args []runtime.Value) runtime.Value {
	if len(args) == 0 {
		return runtime.Null()
	}
	return args[0]
}

// LambdaExpressionBody/IsParameterExpression/MemberExpressionParts and
// the rest of this section expose this file's own unexported native
// shapes to internal/interpreter/exprcompile.go (Fase 3.64/3.65), which
// needs to walk a real expression tree node by node to actually EVALUATE
// it. ExprNodeKind identifies which one a given Value holds, so the
// evaluator can dispatch with one type switch on the exported kind
// rather than needing a type assertion against each unexported Go type
// individually from outside this package.
type ExprNodeKind int

const (
	ExprNodeNone ExprNodeKind = iota
	ExprNodeLambda
	ExprNodeParameter
	ExprNodeMember
	ExprNodeConstant
	ExprNodeCall
	ExprNodeNew
	ExprNodeNewArrayInit
	ExprNodeConvert
	ExprNodeAssign
	ExprNodeBlock
	ExprNodeDefault
	ExprNodeConditional
	ExprNodeInvoke
	ExprNodeIncDec
	ExprNodeBinaryCompare
	ExprNodeThrow
	ExprNodeCoalesce
	ExprNodeTry
	ExprNodeArrayIndex
)

// KindOfExprNode identifies v's own real Expression node kind, or
// ExprNodeNone if v isn't one of this subsystem's own native shapes at
// all.
func KindOfExprNode(v runtime.Value) ExprNodeKind {
	if v.Kind != runtime.KindObject || v.Obj == nil {
		return ExprNodeNone
	}
	switch v.Obj.Native.(type) {
	case *nativeLambdaExpression:
		return ExprNodeLambda
	case *nativeParameterExpression:
		return ExprNodeParameter
	case *nativeMemberExpression:
		return ExprNodeMember
	case *nativeConstantExpression:
		return ExprNodeConstant
	case *nativeCallExpression:
		return ExprNodeCall
	case *nativeNewExpression:
		return ExprNodeNew
	case *nativeNewArrayExpression:
		return ExprNodeNewArrayInit
	case *nativeConvertExpression:
		return ExprNodeConvert
	case *nativeAssignExpression:
		return ExprNodeAssign
	case *nativeBlockExpression:
		return ExprNodeBlock
	case *nativeDefaultExpression:
		return ExprNodeDefault
	case *nativeConditionalExpression:
		return ExprNodeConditional
	case *nativeInvokeExpression:
		return ExprNodeInvoke
	case *nativeIncDecExpression:
		return ExprNodeIncDec
	case *nativeBinaryCompareExpression:
		return ExprNodeBinaryCompare
	case *nativeThrowExpression:
		return ExprNodeThrow
	case *nativeCoalesceExpression:
		return ExprNodeCoalesce
	case *nativeTryExpression:
		return ExprNodeTry
	case *nativeArrayIndexExpression:
		return ExprNodeArrayIndex
	default:
		return ExprNodeNone
	}
}

func LambdaExpressionBody(v runtime.Value) (runtime.Value, bool) {
	le, ok := nativeOf[*nativeLambdaExpression](v)
	if !ok {
		return runtime.Value{}, false
	}
	return le.body, true
}

func LambdaExpressionParameters(v runtime.Value) ([]runtime.Value, bool) {
	le, ok := nativeOf[*nativeLambdaExpression](v)
	if !ok {
		return nil, false
	}
	return le.parameters, true
}

// ExprNodeIdentity returns v's own *runtime.Object pointer as an opaque
// comparable key — used by exprcompile.go's own environment map to give
// every distinct ParameterExpression/variable a stable slot regardless
// of how many times it's referenced across a tree.
func ExprNodeIdentity(v runtime.Value) (*runtime.Object, bool) {
	if v.Kind != runtime.KindObject || v.Obj == nil {
		return nil, false
	}
	return v.Obj, true
}

func IsParameterExpression(v runtime.Value) bool {
	_, ok := nativeOf[*nativeParameterExpression](v)
	return ok
}

func ParameterExpressionTypeName(v runtime.Value) (string, bool) {
	p, ok := nativeOf[*nativeParameterExpression](v)
	if !ok {
		return "", false
	}
	return p.typeName, true
}

// MemberExpressionParts returns a MemberExpression node's own property
// name and the Expression it was accessed off of (Expression.Property's
// own first argument — see nativeMemberExpression's own doc comment).
func MemberExpressionParts(v runtime.Value) (propertyName string, inner runtime.Value, ok bool) {
	me, ok := nativeOf[*nativeMemberExpression](v)
	if !ok {
		return "", runtime.Value{}, false
	}
	return me.propertyName, me.expression, true
}

// MemberExpressionTypeName exposes a MemberExpression's own declared
// .Type name — separate from MemberExpressionParts (whose two return
// values are already used positionally by several call sites) since only
// exprvisitor.go's own VisitMember rebuild actually needs it.
func MemberExpressionTypeName(v runtime.Value) (string, bool) {
	me, ok := nativeOf[*nativeMemberExpression](v)
	if !ok {
		return "", false
	}
	return me.typeName, true
}

func ConstantExpressionValue(v runtime.Value) (runtime.Value, bool) {
	c, ok := nativeOf[*nativeConstantExpression](v)
	if !ok {
		return runtime.Value{}, false
	}
	return c.value, true
}

func CallExpressionParts(v runtime.Value) (instance runtime.Value, typeName, methodName string, args []runtime.Value, ok bool) {
	c, ok := nativeOf[*nativeCallExpression](v)
	if !ok {
		return runtime.Value{}, "", "", nil, false
	}
	return c.instance, c.typeName, c.methodName, c.args, true
}

func NewExpressionParts(v runtime.Value) (typeName string, args []runtime.Value, ok bool) {
	n, ok := nativeOf[*nativeNewExpression](v)
	if !ok {
		return "", nil, false
	}
	return n.typeName, n.args, true
}

func NewArrayExpressionParts(v runtime.Value) (elemTypeName string, elements []runtime.Value, ok bool) {
	n, ok := nativeOf[*nativeNewArrayExpression](v)
	if !ok {
		return "", nil, false
	}
	return n.elemTypeName, n.elements, true
}

func ConvertExpressionParts(v runtime.Value) (operand runtime.Value, typeName string, ok bool) {
	c, ok := nativeOf[*nativeConvertExpression](v)
	if !ok {
		return runtime.Value{}, "", false
	}
	return c.operand, c.typeName, true
}

func AssignExpressionParts(v runtime.Value) (left, right runtime.Value, ok bool) {
	a, ok := nativeOf[*nativeAssignExpression](v)
	if !ok {
		return runtime.Value{}, runtime.Value{}, false
	}
	return a.left, a.right, true
}

func BlockExpressionParts(v runtime.Value) (variables, body []runtime.Value, ok bool) {
	b, ok := nativeOf[*nativeBlockExpression](v)
	if !ok {
		return nil, nil, false
	}
	return b.variables, b.body, true
}

func DefaultExpressionTypeName(v runtime.Value) (string, bool) {
	d, ok := nativeOf[*nativeDefaultExpression](v)
	if !ok {
		return "", false
	}
	return d.typeName, true
}

func ConditionalExpressionParts(v runtime.Value) (test, ifTrue, ifFalse runtime.Value, ok bool) {
	c, ok := nativeOf[*nativeConditionalExpression](v)
	if !ok {
		return runtime.Value{}, runtime.Value{}, runtime.Value{}, false
	}
	return c.test, c.ifTrue, c.ifFalse, true
}

func InvokeExpressionParts(v runtime.Value) (expr runtime.Value, args []runtime.Value, ok bool) {
	i, ok := nativeOf[*nativeInvokeExpression](v)
	if !ok {
		return runtime.Value{}, nil, false
	}
	return i.expr, i.args, true
}

// Rebuild constructors (Fase 3.65, ExpressionVisitor support) — exported
// so internal/interpreter/exprvisitor.go's own default Visit/VisitXxx
// implementations can build a NEW node of the same shape after
// recursively visiting its children, exactly like real .NET's own
// default ExpressionVisitor behavior (Update-if-changed). Unlike real
// .NET, these always allocate a fresh node rather than returning the
// original when nothing changed — object-identity preservation is a
// real-.NET optimization this subsystem's own evaluator never depends
// on, so it isn't reproduced here.
func NewMemberExpressionValue(propertyName string, inner runtime.Value, typeName string) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeMemberExpression{propertyName: propertyName, expression: inner, typeName: typeName}})
}

func NewCallExpressionValue(instance runtime.Value, typeName, methodName string, args []runtime.Value) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeCallExpression{instance: instance, typeName: typeName, methodName: methodName, args: args}})
}

func NewNewExpressionValue(typeName string, args []runtime.Value) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeNewExpression{typeName: typeName, args: args}})
}

func NewNewArrayExpressionValue(elemTypeName string, elements []runtime.Value) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeNewArrayExpression{elemTypeName: elemTypeName, elements: elements}})
}

func NewConvertExpressionValue(operand runtime.Value, typeName string) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeConvertExpression{operand: operand, typeName: typeName}})
}

func NewIncDecExpressionValue(opType int, operand runtime.Value) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeIncDecExpression{opType: opType, operand: operand}})
}

func NewThrowExpressionValue(value runtime.Value, typeName string) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeThrowExpression{value: value, typeName: typeName}})
}

func NewAssignExpressionValue(left, right runtime.Value) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeAssignExpression{left: left, right: right}})
}

func NewBinaryCompareExpressionValue(opType int, left, right runtime.Value) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeBinaryCompareExpression{opType: opType, left: left, right: right}})
}

func NewBlockExpressionValue(variables, body []runtime.Value) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeBlockExpression{variables: variables, body: body}})
}

func NewConditionalExpressionValue(test, ifTrue, ifFalse runtime.Value) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeConditionalExpression{test: test, ifTrue: ifTrue, ifFalse: ifFalse}})
}

func NewInvokeExpressionValue(expr runtime.Value, args []runtime.Value) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeInvokeExpression{expr: expr, args: args}})
}

func NewLambdaExpressionValue(body runtime.Value, parameters []runtime.Value) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeLambdaExpression{body: body, parameters: parameters}})
}
