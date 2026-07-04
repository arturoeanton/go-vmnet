package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// System.Xml.XmlQualifiedName is a real, simple BCL reference type (an
// XML Schema "QName": a local name plus its namespace URI) — found via a
// real, load-bearing case (Fase 3.41): DocumentFormat.OpenXml.Framework's
// own NumberValidator/StringValidator (part of the schema-validation
// metadata every ConfigureMetadata/AddAttribute<T> call builds — see
// system_linq_expressions.go's own doc comment) each cache a handful of
// these as static readonly fields, e.g. `new XmlQualifiedName
// ("positiveInteger", "http://www.w3.org/2001/XMLSchema")`.
type nativeXmlQualifiedName struct {
	name string
	ns   string
}

// xmlQualifiedNameStaticsType backs XmlQualifiedName.Empty (`ldsfld
// System.Xml.XmlQualifiedName::Empty`) — a real static readonly field,
// not a property, found via a real, load-bearing case: DocumentFormat.
// OpenXml.Framework's own OpenXmlSimpleTypeExtensions falls back to it
// whenever a simple type has no explicit schema QName. Needs the same
// static-field-host registration System.IntPtr::Zero uses (system_
// intptr.go) — a plain registerCtor (this file's own ctor below) has no
// bearing on ldsfld/`m.ResolveType` resolution at all, a distinct gap
// that surfaced only once real code reached this specific static field.
var xmlQualifiedNameStaticsType = runtime.NewType(
	"System.Xml", "XmlQualifiedName", nil,
	[]string{"Empty"}, nil,
	[]runtime.Value{runtime.ObjRef(&runtime.Object{Native: &nativeXmlQualifiedName{}})},
)

func init() {
	registerStaticFieldHost(xmlQualifiedNameStaticsType)
	registerCtor("System.Xml.XmlQualifiedName", xmlQualifiedNameCtor)
	register("System.Xml.XmlQualifiedName::get_Name", true, xmlQualifiedNameGetName)
	register("System.Xml.XmlQualifiedName::get_Namespace", true, xmlQualifiedNameGetNamespace)
	register("System.Xml.XmlQualifiedName::get_IsEmpty", true, xmlQualifiedNameGetIsEmpty)
	register("System.Xml.XmlQualifiedName::ToString", true, xmlQualifiedNameToString)
	register("System.Xml.XmlQualifiedName::Equals", true, xmlQualifiedNameEquals)
	register("System.Xml.XmlQualifiedName::GetHashCode", true, xmlQualifiedNameGetHashCode)
}

func xmlQualifiedNameCtor(args []runtime.Value) (*runtime.Object, error) {
	q := &nativeXmlQualifiedName{}
	if len(args) > 0 && args[0].Kind == runtime.KindString {
		q.name = args[0].Str
	}
	if len(args) > 1 && args[1].Kind == runtime.KindString {
		q.ns = args[1].Str
	}
	return &runtime.Object{Native: q}, nil
}

func asXmlQualifiedName(args []runtime.Value) (*nativeXmlQualifiedName, error) {
	q, ok := nativeOf[*nativeXmlQualifiedName](firstArg(args))
	if !ok {
		return nil, fmt.Errorf("bcl: XmlQualifiedName method called on a non-XmlQualifiedName receiver")
	}
	return q, nil
}

func xmlQualifiedNameGetName(args []runtime.Value) (runtime.Value, error) {
	q, err := asXmlQualifiedName(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.String(q.name), nil
}

func xmlQualifiedNameGetNamespace(args []runtime.Value) (runtime.Value, error) {
	q, err := asXmlQualifiedName(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.String(q.ns), nil
}

func xmlQualifiedNameGetIsEmpty(args []runtime.Value) (runtime.Value, error) {
	q, err := asXmlQualifiedName(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(q.name == "" && q.ns == ""), nil
}

// xmlQualifiedNameToString matches real XmlQualifiedName.ToString():
// "ns:name" when a namespace is set, otherwise just "name".
func xmlQualifiedNameToString(args []runtime.Value) (runtime.Value, error) {
	q, err := asXmlQualifiedName(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if q.ns == "" {
		return runtime.String(q.name), nil
	}
	return runtime.String(q.ns + ":" + q.name), nil
}

func xmlQualifiedNameEquals(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return runtime.Value{}, fmt.Errorf("bcl: XmlQualifiedName.Equals expects 2 arguments")
	}
	a, aok := nativeOf[*nativeXmlQualifiedName](args[0])
	b, bok := nativeOf[*nativeXmlQualifiedName](args[1])
	if !aok || !bok {
		return runtime.Bool(false), nil
	}
	return runtime.Bool(a.name == b.name && a.ns == b.ns), nil
}

func xmlQualifiedNameGetHashCode(args []runtime.Value) (runtime.Value, error) {
	q, err := asXmlQualifiedName(args)
	if err != nil {
		return runtime.Value{}, err
	}
	h := int32(17)
	for i := 0; i < len(q.name); i++ {
		h = h*31 + int32(q.name[i])
	}
	for i := 0; i < len(q.ns); i++ {
		h = h*31 + int32(q.ns[i])
	}
	return runtime.Int32(h), nil
}
