package bcl

import (
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// System.Linq.Expressions support here is deliberately narrow, not a
// general expression-tree engine: every real call site found across
// DocumentFormat.OpenXml/.Framework (Fase 3.41, ~1859 occurrences) uses
// the exact same shape — every element's own ConfigureMetadata builds
// an Expression<Func<TElement,TValue>> for each attribute accessor
// (`a => a.Space`), which the compiler lowers to `Expression.Parameter`
// + `ldtoken <property getter>`/`MethodBase.GetMethodFromHandle`/
// `Expression.Property` + `Expression.Lambda`, and the only real
// consumer (ElementMetadata.Builder<T>.AddAttribute, real interpreted
// IL) does nothing but `expression.Body is MemberExpression m` then
// reads `m.Member.Name` — never compiles or invokes the tree. So none of
// these natives need to represent a real, walkable/compilable
// expression graph — just enough shape for that one inspection to work.

// nativeParameterExpression is completely opaque: nothing downstream
// ever reads anything off it besides passing it back into
// Expression.Lambda's own parameter array, itself unused.
type nativeParameterExpression struct{}

// nativeMemberExpression carries just the property name AddAttribute
// ultimately reads via .Member.Name — derived from the property
// accessor's own method name (get_Space -> "Space") right when
// Expression.Property is called, rather than modeling a real
// PropertyInfo/MemberInfo graph.
type nativeMemberExpression struct {
	propertyName string
}

// nativeMemberInfo backs MemberExpression.Member's real return type
// (System.Reflection.MemberInfo) — exposes only .Name, the one member
// AddAttribute reads.
type nativeMemberInfo struct {
	name string
}

// nativeLambdaExpression backs Expression<TDelegate> — exposes only
// .Body, the one member AddAttribute reads.
type nativeLambdaExpression struct {
	body runtime.Value
}

func init() {
	register("System.Linq.Expressions.Expression::Parameter", true, expressionParameter)
	register("System.Linq.Expressions.Expression::Property", true, expressionProperty)
	register("System.Linq.Expressions.Expression::Lambda", true, expressionLambda)
	register("System.Linq.Expressions.LambdaExpression::get_Body", true, lambdaExpressionGetBody)
	register("System.Linq.Expressions.MemberExpression::get_Member", true, memberExpressionGetMember)
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

func expressionParameter(args []runtime.Value) (runtime.Value, error) {
	return runtime.ObjRef(&runtime.Object{Native: &nativeParameterExpression{}}), nil
}

func methodBaseGetMethodFromHandle(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 {
		return runtime.Null(), nil
	}
	return args[0], nil
}

// expressionProperty backs Expression.Property(Expression, MethodInfo) —
// the overload real ConfigureMetadata code always uses (a property
// accessor method handle, never a bare PropertyInfo or string name).
func expressionProperty(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, nil
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
	return runtime.ObjRef(&runtime.Object{Native: &nativeMemberExpression{propertyName: name}}), nil
}

// expressionLambda backs Expression.Lambda<TDelegate>(Expression body,
// ParameterExpression[] parameters) — a generic method, but TDelegate is
// never consulted (nothing here compiles or invokes the tree), so this
// is registered as a plain native rather than needing
// genericMachineRegistry.
func expressionLambda(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 {
		return runtime.Value{}, nil
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeLambdaExpression{body: args[0]}}), nil
}

func lambdaExpressionGetBody(args []runtime.Value) (runtime.Value, error) {
	le, ok := nativeOf[*nativeLambdaExpression](firstArg(args))
	if !ok {
		return runtime.Null(), nil
	}
	return le.body, nil
}

func memberExpressionGetMember(args []runtime.Value) (runtime.Value, error) {
	me, ok := nativeOf[*nativeMemberExpression](firstArg(args))
	if !ok {
		return runtime.Null(), nil
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeMemberInfo{name: me.propertyName}}), nil
}

func firstArg(args []runtime.Value) runtime.Value {
	if len(args) == 0 {
		return runtime.Null()
	}
	return args[0]
}
