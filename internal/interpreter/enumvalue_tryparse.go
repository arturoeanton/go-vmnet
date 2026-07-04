package interpreter

import (
	"fmt"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// DocumentFormat.OpenXml.EnumValue<T>.TryParse(string?, out T) (Fase 3.44,
// found running a real .xlsx through ClosedXML 0.105.0's own `new
// XLWorkbook(stream)` — one call further into the same investigation
// attribute_createnew.go's AttributeInfo.CreateNew fix already reached).
//
// Real decompiled source (DocumentFormat.OpenXml.Framework 9.0.0,
// /tmp/openxmlfw_ns20/DocumentFormat.OpenXml/EnumValue.cs):
//
//	private protected override bool TryParse(string? input, out T value)
//	{
//	    if (input != null)
//	    {
//	        T val = default(T).Create(input);
//	        if (val.IsValid) { value = val; return true; }
//	    }
//	    value = default(T);
//	    return false;
//	}
//
// (IL, /tmp/openxmlfw.il ~offset 32200: `initobj !T` on a fresh local,
// then `constrained. !T` + `callvirt instance !0 class DocumentFormat.
// OpenXml.IEnumValueFactory\`1<!T>::Create(string)`.)
//
// `default(T)` here is the whole problem: T is EnumValue<T>'s own CLASS
// generic parameter (a plain `!0`/VAR, not a method `!!0`/MVAR). vmnet
// erases every class-level generic instantiation to one shared Type per
// OPEN generic definition — no closed-instantiation identity at all, the
// same limitation attribute_createnew.go/getattribute.go both already
// document for this exact SDK. `initobj !T` at IR-build time
// (internal/ir/builder.go's resolveTypeTokenOrGeneric) has no way to
// resolve a class-level VAR either (see ir.InitObj's own doc comment), so
// it emits TypeFullName="" — at runtime that becomes plain KindNull
// (defaultValueFor, structs.go), not a real SpaceProcessingModeValues-
// shaped struct. A `constrained.`-prefixed callvirt through a KindNull
// receiver has no concrete type for Machine.call's virtual-dispatch/
// explicit-impl ancestor walk (calls.go) to redirect on, so it falls all
// the way through to the bare, bodyless interface declaration itself:
// "DocumentFormat.OpenXml.IEnumValueFactory`1::Create: method has no
// body (abstract/extern methods are unsupported)" (assembly.go's
// buildMethod, its RVA==0 guard). This is the SAME dispatch mechanism
// calls.go's own explicit-impl-first fix already handles correctly for a
// struct receiver with a real concrete Type (e.g. SpaceProcessingModeValues
// explicitly implementing IEnumValueFactory<SpaceProcessingModeValues>.
// Create) — the only thing missing here is ever constructing that real
// receiver in the first place.
//
// Fixed the same way attribute_createnew.go's own CreateNew() fix already
// had to: relay T across the one real call site that DOES still know it.
// AttributeInfo.CreateNew constructs this exact EnumValue<T> instance via
// `new TSimpleType()` with TSimpleType genuinely closed (that fix, same
// investigation) — attributeInfoCreateNew now also stashes that closed
// T's name directly on the resulting *runtime.Object (Native, a plain Go
// side channel already used the same way elsewhere for objects that need
// extra state vmnet's plain Type+Fields model has no slot for — see
// elementfactory.go's nativeKnownChildBuilder). TryParse is intercepted
// here to read that stash back and build a REAL default(T) via
// m.defaultValueFor (a genuine *runtime.Type-backed KindStruct, not
// InitObj's erased KindNull) before calling through to
// IEnumValueFactory`1::Create / IEnumValue::get_IsValid — both already
// dispatch correctly against a real struct receiver via Machine.call's
// existing explicit-impl-first logic, left completely unmodified here.
//
// A receiver with no recorded T (any EnumValue<T> instance NOT built
// through AttributeInfo.CreateNew — e.g. one constructed some other real
// way this investigation hasn't hit yet) falls through to the real
// interpreted TryParse body untouched, exactly as honestly as before
// this fix.
func init() {
	machineRegistry["DocumentFormat.OpenXml.EnumValue`1::TryParse"] = enumValueTryParse
}

// enumValueGenericArg tags a *runtime.Object as "some EnumValue<T> whose
// concrete T we independently learned" — stashed on Native by
// attributeInfoCreateNew (attribute_createnew.go) at construction time.
type enumValueGenericArg struct {
	typeFullName string
}

// firstClosedGenericArg extracts a closed arity-1 generic instantiation's
// single type argument — e.g. "DocumentFormat.OpenXml.EnumValue`1
// [[DocumentFormat.OpenXml.SpaceProcessingModeValues]]" ->
// "DocumentFormat.OpenXml.SpaceProcessingModeValues" (SigTypeFullName's
// own "Open`N[[Arg1],[Arg2]]" format, internal/ir/builder.go). EnumValue`1
// always has exactly one type parameter, so — unlike bcl's own
// splitGenericArgs (system_type.go), which has to handle arbitrary arity
// and nested brackets for System.Type reflection — this only ever needs
// the single span between the outer "[[" and the trailing "]]".
func firstClosedGenericArg(closedName string) (string, bool) {
	start := strings.Index(closedName, "[[")
	if start < 0 || !strings.HasSuffix(closedName, "]]") {
		return "", false
	}
	arg := closedName[start+2 : len(closedName)-2]
	if arg == "" {
		return "", false
	}
	return arg, true
}

func enumValueTryParse(m *Machine, args []runtime.Value, depth int, instrCount *int64) (runtime.Value, error) {
	if len(args) >= 3 && args[0].Kind == runtime.KindObject && args[0].Obj != nil {
		if tag, ok := args[0].Obj.Native.(*enumValueGenericArg); ok && tag.typeFullName != "" {
			if args[2].Kind != runtime.KindRef || args[2].Ref == nil {
				return runtime.Value{}, fmt.Errorf("interpreter: EnumValue<T>.TryParse expects an `out T value` third argument")
			}
			input := args[1]
			if input.Kind == runtime.KindString {
				def := m.defaultValueFor(tag.typeFullName)
				created, _, err := m.call("DocumentFormat.OpenXml.IEnumValueFactory`1::Create", []runtime.Value{def, input}, true, depth, instrCount, nil, nil)
				if err != nil {
					return runtime.Value{}, err
				}
				valid, _, err := m.call("DocumentFormat.OpenXml.IEnumValue::get_IsValid", []runtime.Value{created}, true, depth, instrCount, nil, nil)
				if err != nil {
					return runtime.Value{}, err
				}
				if valid.Truthy() {
					*args[2].Ref = created
					return runtime.Bool(true), nil
				}
			}
			*args[2].Ref = m.defaultValueFor(tag.typeFullName)
			return runtime.Bool(false), nil
		}
	}
	// No recorded T for this instance — fall through to the real
	// interpreted TryParse body, bypassing only this one machineRegistry
	// entry (same pattern as attribute_createnew.go's own fallback), so
	// an uncovered construction path fails exactly as honestly as it did
	// before this fix.
	const fullName = "DocumentFormat.OpenXml.EnumValue`1::TryParse"
	if m.Resolve == nil {
		return runtime.Value{}, fmt.Errorf("interpreter: EnumValue<T>.TryParse: no Resolver attached")
	}
	method, err := m.Resolve(fullName, args, nil, 0)
	if err != nil {
		return runtime.Value{}, err
	}
	return m.invoke(method, args, depth+1, instrCount)
}
