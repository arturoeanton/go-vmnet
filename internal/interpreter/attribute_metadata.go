package interpreter

import (
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// nativeCctorOverrides substitutes a Go-native static constructor for a
// real type whose real IL .cctor can't be interpreted at all — checked by
// statics.go's runCctor before it ever tries to resolve/run the real
// ::.cctor body.
var nativeCctorOverrides = map[string]func(m *Machine, t *runtime.Type, depth int, instrCount *int64) error{}

// Populated from init() rather than nativeCctorOverrides's own map
// literal: attributeMetadataBuilderCctor's body reaches m.call, which
// (through runCctor) refers back to nativeCctorOverrides itself — the Go
// compiler treats that as a real package-level initialization cycle when
// the map literal names the function directly, even though nothing
// actually runs until well after package init.
func init() {
	nativeCctorOverrides["DocumentFormat.OpenXml.Framework.Metadata.AttributeMetadata+Builder`1"] = attributeMetadataBuilderCctor
}

// DocumentFormat.OpenXml.Framework.Metadata.AttributeMetadata.Builder<
// TSimpleType>'s real static field initializer (Fase 3.41, found running
// the real openxml-demo's Document.Save()):
//
//	private static readonly IValidator _defaultValidator = GetDefaultValidator();
//	private static IValidator GetDefaultValidator() {
//	    TSimpleType val = new TSimpleType();
//	    if (val is StringValue) return StringValidator.Instance;
//	    if (val.IsEnum || val is OnOffValue || val is TrueFalseBlankValue) return EnumValidator.Instance;
//	    if (val is IEnumerable) return ListValidator.Instance;
//	    return NumberValidator.Instance;
//	}
//
// (real decompiled source: /tmp/openxmlfw_ns20/DocumentFormat.OpenXml.
// Framework.Metadata/AttributeMetadata.cs:36,61-77). `new TSimpleType()`
// compiles to `call !!0 System.Activator::CreateInstance<!0>()` — its
// MethodSpec instantiation argument is `!0`, a CLASS-level generic
// parameter (Builder<TSimpleType>'s own TSimpleType), not a method-level
// one. ir.Call.MethodGenericArgs (see activator.go's own doc comment)
// only ever resolves a call site's OWN MethodSpec instantiation, which is
// exactly what a generic METHOD parameter needs (Fase 3.40) — a generic
// CLASS parameter has no such thing to read at IR-build time at all: the
// same IR body runs once for every different closed TSimpleType
// instantiation of Builder<TSimpleType>, and vmnet's runtime.Type/static-
// field model (internal/runtime/class.go) tracks no notion of "closed
// generic class instantiation identity" to tell them apart. Reifying that
// properly is a real, separate undertaking (out of scope here); this is a
// narrow, targeted workaround for this one static field instead.
//
// _defaultValidator only ever feeds OpenXmlValidator's real schema
// validation (AttributeMetadata.Build() -> AddValidator(_defaultValidator)
// -> Validators, consulted by ValidationStack/SchemaTypeValidator — never
// by the XML-writing path: grepping the whole decompiled SDK for any file
// that references both `.Validators` and `WriteAttribute`/`XmlWriter`
// turns up nothing). So which concrete IValidator answer ends up seeded
// here doesn't affect real document generation at all, only the accuracy
// of validation diagnostics nobody is running in this path — seeding the
// same NumberValidator.Instance singleton GetDefaultValidator() itself
// falls back to for every TSimpleType it can't otherwise classify is a
// safe, honest default rather than a silent correctness compromise.
func attributeMetadataBuilderCctor(m *Machine, t *runtime.Type, depth int, instrCount *int64) error {
	idx := t.StaticFieldIndex("_defaultValidator")
	if idx < 0 {
		// The real TypeDef's shape changed (a different OpenXml SDK
		// version); nothing to seed, and definitely not worth a hard
		// error over a validation-only field.
		return nil
	}
	v, _, err := m.call("DocumentFormat.OpenXml.Framework.NumberValidator::get_Instance", nil, false, depth, instrCount, nil, nil)
	if err != nil {
		return err
	}
	t.SetStaticField(idx, v)
	return nil
}
