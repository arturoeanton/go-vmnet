package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// nativeCustomAttributeData backs System.Reflection.CustomAttributeData
// (Fase 3.63, real attribute reading — deliberately deferred until now,
// see docs/en/ROADMAP.md's own long-standing "genuinely new subsystem"
// note): a real custom attribute application, already fully decoded by
// assembly.go's own resolveCustomAttributes/decodeCustomAttribute (the
// Machine-aware side, internal/interpreter/customattributes.go, only
// re-resolves by (typeFullName, memberKind, memberName) — same "wrapper
// carries just enough identity, the resolver does the real work" shape
// nativeConstructorInfo/nativeMethodInfo/nativePropertyInfo already use).
type nativeCustomAttributeData struct {
	typeFullName string
	ctorArgs     []runtime.Value
}

// customAttributeTypedArgumentType backs System.Reflection.
// CustomAttributeTypedArgument — a REAL struct in .NET (unlike
// ConstructorInfo/MethodInfo/PropertyInfo, all classes), so this is
// modeled as a genuine value type with two fields, the same pattern
// queueEnumeratorType (system_queue.go) already uses for a real BCL
// struct vmnet doesn't otherwise have a native Go type for.
var customAttributeTypedArgumentType = runtime.NewValueType(
	"System.Reflection", "CustomAttributeTypedArgument",
	[]string{"ArgumentType", "Value"},
	[]runtime.Value{runtime.Null(), runtime.Null()},
)

func init() {
	register("System.Reflection.CustomAttributeData::get_AttributeType", true, customAttributeDataGetAttributeType)
	register("System.Reflection.CustomAttributeData::get_ConstructorArguments", true, customAttributeDataGetConstructorArguments)
	register("System.Reflection.CustomAttributeTypedArgument::get_ArgumentType", true, customAttributeTypedArgumentGetArgumentType)
	register("System.Reflection.CustomAttributeTypedArgument::get_Value", true, customAttributeTypedArgumentGetValue)
}

// NewCustomAttributeDataValue wraps a fully-decoded attribute application
// as a real CustomAttributeData value — used by internal/interpreter/
// customattributes.go's own Machine-aware GetCustomAttributesData/
// GetCustomAttribute<T>, which resolve the real data via
// Machine.ResolveCustomAttributes and hand it back through this
// unexported wrapper.
func NewCustomAttributeDataValue(typeFullName string, ctorArgs []runtime.Value) runtime.Value {
	return runtime.ObjRef(&runtime.Object{Native: &nativeCustomAttributeData{typeFullName: typeFullName, ctorArgs: ctorArgs}})
}

// CustomAttributeDataParts exposes a CustomAttributeData value's own
// decoded (typeFullName, ctorArgs) — used by internal/interpreter/
// customattributes.go to actually CONSTRUCT a real attribute instance
// (CustomAttributeExtensions.GetCustomAttribute<T>, Attribute.
// GetCustomAttribute), which needs Machine.New/newObj access this
// package doesn't have.
func CustomAttributeDataParts(v runtime.Value) (typeFullName string, ctorArgs []runtime.Value, ok bool) {
	cad, ok := nativeOf[*nativeCustomAttributeData](v)
	if !ok {
		return "", nil, false
	}
	return cad.typeFullName, cad.ctorArgs, true
}

func asCustomAttributeData(args []runtime.Value) (*nativeCustomAttributeData, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("bcl: CustomAttributeData method called without a receiver")
	}
	cad, ok := nativeOf[*nativeCustomAttributeData](args[0])
	if !ok {
		return nil, fmt.Errorf("bcl: receiver is not a CustomAttributeData")
	}
	return cad, nil
}

func customAttributeDataGetAttributeType(args []runtime.Value) (runtime.Value, error) {
	cad, err := asCustomAttributeData(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return NewTypeValue(cad.typeFullName), nil
}

func customAttributeDataGetConstructorArguments(args []runtime.Value) (runtime.Value, error) {
	cad, err := asCustomAttributeData(args)
	if err != nil {
		return runtime.Value{}, err
	}
	elems := make([]runtime.Value, len(cad.ctorArgs))
	for i, arg := range cad.ctorArgs {
		s := runtime.NewStruct(customAttributeTypedArgumentType)
		s.Fields[0] = NewTypeValue(runtimeValueBclTypeName(arg))
		s.Fields[1] = arg
		elems[i] = runtime.StructVal(s)
	}
	return runtime.ArrRef(&runtime.Array{Elems: elems}), nil
}

// runtimeValueBclTypeName gives CustomAttributeTypedArgument.ArgumentType
// a plausible real BCL type name for the decoded value's own runtime
// Kind — best-effort (vmnet's Value model can't distinguish e.g. a plain
// Int32 constructor argument from an enum-typed one any more precisely
// than this once decoded), matching the same coarse-Kind-based naming
// isAssignableTo/runtimeValueTypeName (assembly.go) already use elsewhere
// for the identical reason.
func runtimeValueBclTypeName(v runtime.Value) string {
	switch v.Kind {
	case runtime.KindString:
		return "System.String"
	case runtime.KindI4:
		return "System.Int32"
	case runtime.KindI8:
		return "System.Int64"
	case runtime.KindR4:
		return "System.Single"
	case runtime.KindR8:
		return "System.Double"
	case runtime.KindObject:
		if name, ok := TypeFullNameOf(v); ok {
			return name
		}
		return "System.Object"
	default:
		return "System.Object"
	}
}

func customAttributeTypedArgumentGetArgumentType(args []runtime.Value) (runtime.Value, error) {
	s, err := derefStructReceiver(args, "CustomAttributeTypedArgument", "CustomAttributeTypedArgument.ArgumentType")
	if err != nil {
		return runtime.Value{}, err
	}
	return s.Fields[0], nil
}

func customAttributeTypedArgumentGetValue(args []runtime.Value) (runtime.Value, error) {
	s, err := derefStructReceiver(args, "CustomAttributeTypedArgument", "CustomAttributeTypedArgument.Value")
	if err != nil {
		return runtime.Value{}, err
	}
	return s.Fields[1], nil
}
