package bcl

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// nativeXElement is a parsed LINQ-to-XML element (System.Xml.Linq.
// XElement/XDocument share this same tree — XDocument just wraps a root
// XElement, matching real XDocument.Root). Namespace URIs are dropped
// throughout this file, matched only by local name — the same
// simplification system_xml.go's XmlWriter already makes for its own
// namespace arguments; a package reading its own previously-written XML
// (ClosedXML's VML comment/shape parts) never actually needs namespace
// disambiguation to find the elements it's looking for by local name
// alone.
type nativeXElement struct {
	name     string
	attrs    []*nativeXAttribute
	children []*nativeXElement
	text     string
}

type nativeXAttribute struct {
	name  string
	value string
}

type nativeXDocument struct {
	root *nativeXElement
}

func init() {
	register("System.Xml.Linq.XDocument::Load", true, xDocumentLoad)
	register("System.Xml.Linq.XDocument::get_Root", true, xDocumentGetRoot)

	register("System.Xml.Linq.XContainer::Elements", true, xContainerElements)
	register("System.Xml.Linq.XContainer::Element", true, xContainerElement)

	register("System.Xml.Linq.XElement::Attribute", true, xElementAttribute)
	register("System.Xml.Linq.XElement::get_Value", true, xElementGetValue)
	register("System.Xml.Linq.XElement::get_Name", true, xElementGetName)
	register("System.Xml.Linq.XElement::get_HasElements", true, xElementGetHasElements)
	// XElement is itself a valid XContainer (real inheritance), so
	// Elements()/Element() must also resolve when the declared call site
	// names XElement directly rather than the XContainer base.
	register("System.Xml.Linq.XElement::Elements", true, xContainerElements)
	register("System.Xml.Linq.XElement::Element", true, xContainerElement)

	register("System.Xml.Linq.XAttribute::get_Value", true, xAttributeGetValue)

	// XName is modeled as a plain System.String (Fase 3.35): every
	// consumer here only ever needs a local name to match against, and
	// nothing round-trips an XName through a real BCL API that would
	// notice the difference — op_Implicit is the identity function on
	// the string itself.
	register("System.Xml.Linq.XName::op_Implicit", true, xNameIdentity)
	register("System.Xml.Linq.XName::get_LocalName", true, xNameIdentity)
	register("System.Xml.Linq.XName::Get", true, xNameGet)
}

func xDocumentLoad(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: XDocument.Load expects a source")
	}
	if args[0].Kind == runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: XDocument.Load(string) (a file path/URI) is not supported — only Load(Stream) is")
	}
	ms, err := asMemoryStream(args[:1])
	if err != nil {
		return runtime.Value{}, fmt.Errorf("bcl: XDocument.Load: only a MemoryStream-backed source is supported")
	}
	root, err := parseXMLTree(ms.buf[ms.pos:])
	if err != nil {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.Xml.XmlException", Message: err.Error()}
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeXDocument{root: root}}), nil
}

func parseXMLTree(data []byte) (*nativeXElement, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var stack []*nativeXElement
	var root *nativeXElement
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			el := &nativeXElement{name: t.Name.Local}
			for _, a := range t.Attr {
				el.attrs = append(el.attrs, &nativeXAttribute{name: a.Name.Local, value: a.Value})
			}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, el)
			} else {
				root = el
			}
			stack = append(stack, el)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].text += string(t)
			}
		}
	}
	if root == nil {
		return nil, fmt.Errorf("no root element found")
	}
	return root, nil
}

func asXDocument(args []runtime.Value) (*nativeXDocument, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return nil, &runtime.ManagedException{TypeName: "System.NullReferenceException", Message: "XDocument method called on a null receiver"}
	}
	d, ok := args[0].Obj.Native.(*nativeXDocument)
	if !ok {
		return nil, fmt.Errorf("bcl: receiver is not an XDocument")
	}
	return d, nil
}

func asXElement(args []runtime.Value) (*nativeXElement, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return nil, &runtime.ManagedException{TypeName: "System.NullReferenceException", Message: "XElement method called on a null receiver"}
	}
	e, ok := args[0].Obj.Native.(*nativeXElement)
	if !ok {
		return nil, fmt.Errorf("bcl: receiver is not an XElement")
	}
	return e, nil
}

func asXAttribute(args []runtime.Value) (*nativeXAttribute, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return nil, &runtime.ManagedException{TypeName: "System.NullReferenceException", Message: "XAttribute method called on a null receiver"}
	}
	a, ok := args[0].Obj.Native.(*nativeXAttribute)
	if !ok {
		return nil, fmt.Errorf("bcl: receiver is not an XAttribute")
	}
	return a, nil
}

func xDocumentGetRoot(args []runtime.Value) (runtime.Value, error) {
	d, err := asXDocument(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if d.root == nil {
		return runtime.Null(), nil
	}
	return runtime.ObjRef(&runtime.Object{Native: d.root}), nil
}

// xContainerElements backs both Elements() and Elements(XName) — real
// XContainer.Elements() (no filter) vs. Elements(name) (filtered by
// local name), disambiguated by argument count; also registered for
// XElement directly (see init's doc comment).
func xContainerElements(args []runtime.Value) (runtime.Value, error) {
	e, err := asXElement(args)
	if err != nil {
		return runtime.Value{}, err
	}
	var filter string
	hasFilter := len(args) >= 2 && args[1].Kind == runtime.KindString
	if hasFilter {
		filter = args[1].Str
	}
	out := make([]runtime.Value, 0, len(e.children))
	for _, c := range e.children {
		if hasFilter && c.name != filter {
			continue
		}
		out = append(out, runtime.ObjRef(&runtime.Object{Native: c}))
	}
	return NewListValue(out), nil
}

func xContainerElement(args []runtime.Value) (runtime.Value, error) {
	e, err := asXElement(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 || args[1].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: XContainer.Element expects a name argument")
	}
	for _, c := range e.children {
		if c.name == args[1].Str {
			return runtime.ObjRef(&runtime.Object{Native: c}), nil
		}
	}
	return runtime.Null(), nil
}

func xElementAttribute(args []runtime.Value) (runtime.Value, error) {
	e, err := asXElement(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 || args[1].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: XElement.Attribute expects a name argument")
	}
	for _, a := range e.attrs {
		if a.name == args[1].Str {
			return runtime.ObjRef(&runtime.Object{Native: a}), nil
		}
	}
	return runtime.Null(), nil
}

// xElementGetValue concatenates all descendant text content in document
// order, matching real XElement.Value — most real usage here (ClosedXML
// reading its own previously-written VML parts) hits leaf elements with
// no children, where this is just the element's own text, but the
// recursive walk is correct for the general case too.
func xElementGetValue(args []runtime.Value) (runtime.Value, error) {
	e, err := asXElement(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.String(xElementText(e)), nil
}

func xElementText(e *nativeXElement) string {
	if len(e.children) == 0 {
		return e.text
	}
	var buf bytes.Buffer
	buf.WriteString(e.text)
	for _, c := range e.children {
		buf.WriteString(xElementText(c))
	}
	return buf.String()
}

func xElementGetName(args []runtime.Value) (runtime.Value, error) {
	e, err := asXElement(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.String(e.name), nil
}

func xElementGetHasElements(args []runtime.Value) (runtime.Value, error) {
	e, err := asXElement(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(len(e.children) > 0), nil
}

func xAttributeGetValue(args []runtime.Value) (runtime.Value, error) {
	a, err := asXAttribute(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.String(a.value), nil
}

func xNameIdentity(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 {
		return runtime.Value{}, fmt.Errorf("bcl: XName method expects an argument/receiver")
	}
	return args[0], nil
}

func xNameGet(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: XName.Get expects a local name string")
	}
	return args[0], nil
}
