package bcl

import (
	"fmt"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// xmlWriterFrame tracks one open element on nativeXmlWriter's stack:
// whether its opening tag's '>' has been emitted yet (still open for a
// same-tag WriteAttributeString) or whether any content forced it closed
// (a child element, WriteString, WriteRaw) — needed to decide, at
// WriteEndElement time, between a self-closing "/>" (nothing written
// since the start tag) and a real "</name>".
type xmlWriterFrame struct {
	name      string
	tagClosed bool
}

// nativeXmlWriter backs System.Xml.XmlWriter — abstract in real .NET,
// always obtained via the static XmlWriter.Create(...) factory (never
// newobj'd directly), writing incrementally into a destination
// MemoryStream exactly like the real writer streams into whatever
// Stream/TextWriter it was created against. Only a MemoryStream (or a
// package's own subclass of it, via the same native-base-chaining
// pattern system_io.go/system_exception.go established) is supported as
// a destination — the only kind of Stream this loop's target packages
// (NPOI/ClosedXML) ever hand it.
type nativeXmlWriter struct {
	dest    *nativeMemoryStream
	stack   []xmlWriterFrame
	inAttr  bool
	attrBuf strings.Builder
	attrTag string
}

func init() {
	register("System.Xml.XmlWriter::Create", true, xmlWriterCreate)
	register("System.Xml.XmlWriter::WriteStartElement", false, xwWriteStartElement)
	register("System.Xml.XmlWriter::WriteEndElement", false, xwWriteEndElement)
	register("System.Xml.XmlWriter::WriteFullEndElement", false, xwWriteEndElement)
	register("System.Xml.XmlWriter::WriteAttributeString", false, xwWriteAttributeString)
	register("System.Xml.XmlWriter::WriteStartAttribute", false, xwWriteStartAttribute)
	register("System.Xml.XmlWriter::WriteEndAttribute", false, xwWriteEndAttribute)
	register("System.Xml.XmlWriter::WriteString", false, xwWriteString)
	register("System.Xml.XmlWriter::WriteRaw", false, xwWriteRaw)
	register("System.Xml.XmlWriter::WriteElementString", false, xwWriteElementString)
	register("System.Xml.XmlWriter::WriteCData", false, xwWriteCData)
	register("System.Xml.XmlWriter::Flush", false, xwNoop)
	register("System.Xml.XmlWriter::Close", false, xwClose)
	register("System.Xml.XmlWriter::Dispose", false, xwClose)

	registerCtor("System.Xml.XmlWriterSettings", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeXmlWriterSettings{}}, nil
	})
	for _, prop := range []string{"CloseOutput", "Encoding", "Indent", "OmitXmlDeclaration", "NewLineChars", "ConformanceLevel"} {
		register("System.Xml.XmlWriterSettings::set_"+prop, false, xwSettingsNoop)
	}
}

// nativeXmlWriterSettings carries no real state: none of its properties
// change nativeXmlWriter's actual output shape (no target package's real
// IL in this loop was found relying on indentation/encoding-declaration
// specifics), so every setter is a no-op — same pragmatic posture as
// MemoryStream's Flush/Close.
type nativeXmlWriterSettings struct{}

func xwSettingsNoop(args []runtime.Value) (runtime.Value, error) {
	return runtime.Value{}, nil
}

func xmlWriterCreate(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: XmlWriter.Create expects at least a destination")
	}
	dest, err := asMemoryStream(args[0:1])
	if err != nil {
		return runtime.Value{}, fmt.Errorf("bcl: XmlWriter.Create: only a MemoryStream-backed destination is supported")
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeXmlWriter{dest: dest}}), nil
}

func asXmlWriter(args []runtime.Value) (*nativeXmlWriter, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return nil, fmt.Errorf("bcl: XmlWriter method called without a receiver")
	}
	w, ok := args[0].Obj.Native.(*nativeXmlWriter)
	if !ok {
		return nil, fmt.Errorf("bcl: receiver is not an XmlWriter")
	}
	return w, nil
}

func (w *nativeXmlWriter) emit(s string) {
	w.dest.writeAt([]byte(s))
}

// closeStartTag emits the pending '>' for the currently open element's
// start tag, if not already closed — must happen before writing any real
// content (a child element, text, raw markup) so the output is well
// formed either way (self-closing if nothing follows, or content
// followed by a real end tag).
func (w *nativeXmlWriter) closeStartTag() {
	if len(w.stack) == 0 {
		return
	}
	top := &w.stack[len(w.stack)-1]
	if !top.tagClosed {
		w.emit(">")
		top.tagClosed = true
	}
}

func xwWriteStartElement(args []runtime.Value) (runtime.Value, error) {
	w, err := asXmlWriter(args)
	if err != nil {
		return runtime.Value{}, err
	}
	params := args[1:]
	var name string
	switch len(params) {
	case 1:
		name = params[0].Str
	case 2:
		name = params[0].Str // (localName, ns) — namespace URI dropped, see doc comment
	case 3:
		if params[0].Kind == runtime.KindString && params[0].Str != "" {
			name = params[0].Str + ":" + params[1].Str
		} else {
			name = params[1].Str
		}
	default:
		return runtime.Value{}, fmt.Errorf("bcl: XmlWriter.WriteStartElement: unsupported argument shape")
	}
	w.closeStartTag()
	w.emit("<" + name)
	w.stack = append(w.stack, xmlWriterFrame{name: name})
	return runtime.Value{}, nil
}

func xwWriteEndElement(args []runtime.Value) (runtime.Value, error) {
	w, err := asXmlWriter(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(w.stack) == 0 {
		return runtime.Value{}, fmt.Errorf("bcl: XmlWriter.WriteEndElement: no open element")
	}
	top := w.stack[len(w.stack)-1]
	w.stack = w.stack[:len(w.stack)-1]
	if !top.tagClosed {
		w.emit("/>")
	} else {
		w.emit("</" + top.name + ">")
	}
	return runtime.Value{}, nil
}

// xwWriteAttributeString covers both WriteAttributeString(name, value)
// and WriteAttributeString(prefix, localName, ns, value) — disambiguated
// by argument count, the namespace-carrying arguments dropped the same
// way WriteStartElement drops them.
func xwWriteAttributeString(args []runtime.Value) (runtime.Value, error) {
	w, err := asXmlWriter(args)
	if err != nil {
		return runtime.Value{}, err
	}
	params := args[1:]
	var name, value string
	switch len(params) {
	case 2:
		name, value = params[0].Str, params[1].Str
	case 3:
		name, value = params[0].Str, params[2].Str // (localName, ns, value)
	case 4:
		prefix, local, _, v := params[0].Str, params[1].Str, params[2].Str, params[3].Str
		name, value = local, v
		if prefix != "" {
			name = prefix + ":" + local
		}
	default:
		return runtime.Value{}, fmt.Errorf("bcl: XmlWriter.WriteAttributeString: unsupported argument shape")
	}
	if len(w.stack) == 0 {
		return runtime.Value{}, fmt.Errorf("bcl: XmlWriter.WriteAttributeString: no open element")
	}
	w.emit(" " + name + `="` + xmlEscapeAttr(value) + `"`)
	return runtime.Value{}, nil
}

func xwWriteStartAttribute(args []runtime.Value) (runtime.Value, error) {
	w, err := asXmlWriter(args)
	if err != nil {
		return runtime.Value{}, err
	}
	params := args[1:]
	if len(params) == 0 {
		return runtime.Value{}, fmt.Errorf("bcl: XmlWriter.WriteStartAttribute expects a name")
	}
	name := params[0].Str
	if len(params) == 3 {
		if prefix := params[0].Str; prefix != "" {
			name = prefix + ":" + params[1].Str
		} else {
			name = params[1].Str
		}
	}
	w.inAttr = true
	w.attrTag = name
	w.attrBuf.Reset()
	return runtime.Value{}, nil
}

func xwWriteEndAttribute(args []runtime.Value) (runtime.Value, error) {
	w, err := asXmlWriter(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if !w.inAttr {
		return runtime.Value{}, fmt.Errorf("bcl: XmlWriter.WriteEndAttribute: no open attribute")
	}
	w.emit(" " + w.attrTag + `="` + w.attrBuf.String() + `"`)
	w.inAttr = false
	return runtime.Value{}, nil
}

// xwWriteString routes to the in-progress attribute value buffer between
// WriteStartAttribute/WriteEndAttribute, or to the element's text content
// otherwise — real XmlWriter dispatches identically based on writer
// state.
func xwWriteString(args []runtime.Value) (runtime.Value, error) {
	w, err := asXmlWriter(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 || args[1].Kind != runtime.KindString {
		return runtime.Value{}, nil // WriteString(null) is a real no-op
	}
	if w.inAttr {
		w.attrBuf.WriteString(xmlEscapeAttr(args[1].Str))
		return runtime.Value{}, nil
	}
	w.closeStartTag()
	w.emit(xmlEscapeText(args[1].Str))
	return runtime.Value{}, nil
}

func xwWriteRaw(args []runtime.Value) (runtime.Value, error) {
	w, err := asXmlWriter(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 || args[1].Kind != runtime.KindString {
		return runtime.Value{}, nil
	}
	w.closeStartTag()
	w.emit(args[1].Str)
	return runtime.Value{}, nil
}

// xwWriteElementString covers WriteElementString(name, value) and
// WriteElementString(name, ns, value) — a start tag, escaped text, and
// an end tag in one call.
func xwWriteElementString(args []runtime.Value) (runtime.Value, error) {
	w, err := asXmlWriter(args)
	if err != nil {
		return runtime.Value{}, err
	}
	params := args[1:]
	var name, value string
	switch len(params) {
	case 2:
		name, value = params[0].Str, params[1].Str
	case 3:
		name, value = params[0].Str, params[2].Str
	default:
		return runtime.Value{}, fmt.Errorf("bcl: XmlWriter.WriteElementString: unsupported argument shape")
	}
	w.closeStartTag()
	w.emit("<" + name + ">" + xmlEscapeText(value) + "</" + name + ">")
	return runtime.Value{}, nil
}

func xwWriteCData(args []runtime.Value) (runtime.Value, error) {
	w, err := asXmlWriter(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 || args[1].Kind != runtime.KindString {
		return runtime.Value{}, nil
	}
	w.closeStartTag()
	w.emit("<![CDATA[" + args[1].Str + "]]>")
	return runtime.Value{}, nil
}

func xwNoop(args []runtime.Value) (runtime.Value, error) {
	if _, err := asXmlWriter(args); err != nil {
		return runtime.Value{}, err
	}
	return runtime.Value{}, nil
}

// xwClose closes every still-open element (real XmlWriter does the same
// on Close/Dispose — an unbalanced Write*Element sequence still produces
// well-formed output).
func xwClose(args []runtime.Value) (runtime.Value, error) {
	w, err := asXmlWriter(args)
	if err != nil {
		return runtime.Value{}, err
	}
	for len(w.stack) > 0 {
		if _, err := xwWriteEndElement(args); err != nil {
			return runtime.Value{}, err
		}
	}
	return runtime.Value{}, nil
}

func xmlEscapeText(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

func xmlEscapeAttr(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}
