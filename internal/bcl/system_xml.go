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
//
// nsDecls records this frame's own namespace declarations (prefix -> URI,
// ""-keyed for the default "xmlns" declaration) — needed for
// XmlWriter.LookupPrefix (Fase 3.41, found via a real, load-bearing case:
// DocumentFormat.OpenXml.Framework's own OpenXmlPartRootElement.WriteTo
// calls xmlWriter.LookupPrefix(NamespaceUri) as one of its fallbacks when
// writing a real .docx's root element). Populated from WriteStartElement's
// 3-arg (prefix, localName, ns) overload — the real CLR auto-declares that
// binding if not already in scope — and from an explicit
// WriteAttributeString("xmlns"/"xmlns:prefix", ...) call.
type xmlWriterFrame struct {
	name      string
	tagClosed bool
	nsDecls   map[string]string
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
	register("System.Xml.XmlWriter::LookupPrefix", true, xwLookupPrefix)
	register("System.Xml.XmlWriter::WriteString", false, xwWriteString)
	register("System.Xml.XmlWriter::WriteRaw", false, xwWriteRaw)
	register("System.Xml.XmlWriter::WriteElementString", false, xwWriteElementString)
	register("System.Xml.XmlWriter::WriteCData", false, xwWriteCData)
	register("System.Xml.XmlWriter::Flush", false, xwNoop)
	register("System.Xml.XmlWriter::Close", false, xwClose)
	register("System.Xml.XmlWriter::Dispose", false, xwClose)
	register("System.Xml.XmlWriter::WriteStartDocument", false, xwWriteStartDocument)
	register("System.Xml.XmlWriter::WriteEndDocument", false, xwWriteEndDocument)

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
	var prefix, ns string
	haveNs := false
	switch len(params) {
	case 1:
		name = params[0].Str
	case 2:
		// (localName, ns) — no explicit prefix; real WriteStartElement
		// still auto-declares ns as the DEFAULT namespace (Fase 3.41; see
		// the 3-arg case's own doc comment for why this must actually be
		// written, not just tracked).
		name = params[0].Str
		ns, haveNs = params[1].Str, true
	case 3:
		prefix, ns, haveNs = params[0].Str, params[2].Str, true
		if prefix != "" {
			name = prefix + ":" + params[1].Str
		} else {
			name = params[1].Str
		}
	default:
		return runtime.Value{}, fmt.Errorf("bcl: XmlWriter.WriteStartElement: unsupported argument shape")
	}
	w.closeStartTag()
	w.emit("<" + name)
	frame := xmlWriterFrame{name: name}
	// The real CLR's namespace-carrying WriteStartElement overloads
	// auto-declare/emit the binding as an xmlns attribute on this exact
	// element, unless an ancestor already binds the same prefix to the
	// same URI — found via a real, load-bearing bug (Fase 3.41): the
	// namespace argument used to be silently dropped entirely (never
	// written to the output at all), so every OOXML part vmnet itself
	// generated — including [Content_Types].xml's own required default
	// namespace — came out with NO namespace at all. That's invisible to
	// vmnet's own lenient XmlReader (which never checks namespaces), but
	// the real .NET SDK/Word reject it outright ("Required Types tag not
	// found") — confirmed by round-tripping examples/openxml-demo's own
	// output through the real, unmodified OpenXml SDK.
	if haveNs && ns != "" && !nsAlreadyInScope(w, prefix, ns) {
		if prefix == "" {
			w.emit(` xmlns="` + xmlEscapeAttr(ns) + `"`)
		} else {
			w.emit(` xmlns:` + prefix + `="` + xmlEscapeAttr(ns) + `"`)
		}
		frame.nsDecls = map[string]string{prefix: ns}
	}
	w.stack = append(w.stack, frame)
	return runtime.Value{}, nil
}

// nsAlreadyInScope reports whether prefix is already bound to ns by some
// still-open ancestor element — used to decide whether WriteStartElement
// needs to (re-)emit the xmlns declaration on this exact element.
func nsAlreadyInScope(w *nativeXmlWriter, prefix, ns string) bool {
	for i := len(w.stack) - 1; i >= 0; i-- {
		if uri, ok := w.stack[i].nsDecls[prefix]; ok {
			return uri == ns
		}
	}
	return false
}

// xwLookupPrefix backs XmlWriter.LookupPrefix(string ns) — walks the open
// element stack from innermost to outermost looking for a namespace
// declaration bound to ns, matching real in-scope-namespace resolution.
// Returns Null() (real LookupPrefix returns null, not "") when nothing in
// scope binds ns at all.
func xwLookupPrefix(args []runtime.Value) (runtime.Value, error) {
	w, err := asXmlWriter(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 || args[1].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: XmlWriter.LookupPrefix expects a namespace URI")
	}
	ns := args[1].Str
	for i := len(w.stack) - 1; i >= 0; i-- {
		for prefix, uri := range w.stack[i].nsDecls {
			if uri == ns {
				return runtime.String(prefix), nil
			}
		}
	}
	return runtime.Null(), nil
}

// xwWriteStartDocument backs both real overloads — WriteStartDocument()
// and WriteStartDocument(bool standalone) — writing the real XML
// declaration every OOXML part starts with (Fase 3.41, found via a real,
// load-bearing case: DocumentFormat.OpenXml.Framework's own
// OpenXmlPartRootElement.Save calls this before WriteTo). Real .NET's
// default XmlWriterSettings.Encoding here is UTF-8 without a BOM
// (OpenXmlPartRootElement.Save constructs `new UTF8Encoding
// (encoderShouldEmitUTF8Identifier: false)` explicitly) — matching that
// exactly rather than guessing at the encoding name real code would want.
func xwWriteStartDocument(args []runtime.Value) (runtime.Value, error) {
	w, err := asXmlWriter(args)
	if err != nil {
		return runtime.Value{}, err
	}
	w.emit(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	return runtime.Value{}, nil
}

// xwWriteEndDocument backs XmlWriter.WriteEndDocument() — real semantics
// close every still-open element as a safety net (the writer's own
// caller is expected to have already closed everything itself; this is
// belt-and-suspenders in real .NET too). In a real, well-formed
// document-writing pipeline the stack is already empty by the time this
// runs, so the loop below is normally a no-op.
func xwWriteEndDocument(args []runtime.Value) (runtime.Value, error) {
	w, err := asXmlWriter(args)
	if err != nil {
		return runtime.Value{}, err
	}
	for len(w.stack) > 0 {
		top := w.stack[len(w.stack)-1]
		w.stack = w.stack[:len(w.stack)-1]
		if !top.tagClosed {
			w.emit("/>")
		} else {
			w.emit("</" + top.name + ">")
		}
	}
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
	declPrefix, declURI, isDecl := "", "", false
	switch len(params) {
	case 2:
		name, value = params[0].Str, params[1].Str
		// A same-name-shape namespace declaration written the plain
		// (name, value) way: "xmlns" (default) or "xmlns:foo" (prefixed).
		if name == "xmlns" {
			declPrefix, declURI, isDecl = "", value, true
		} else if p, ok := strings.CutPrefix(name, "xmlns:"); ok {
			declPrefix, declURI, isDecl = p, value, true
		}
	case 3:
		name, value = params[0].Str, params[2].Str // (localName, ns, value)
	case 4:
		prefix, local, _, v := params[0].Str, params[1].Str, params[2].Str, params[3].Str
		name, value = local, v
		if prefix != "" {
			name = prefix + ":" + local
		}
		// WriteAttributeString("xmlns", prefix, ns, uri) — the (prefix,
		// localName, ns, value) shape real OpenXml namespace-attribute
		// writing uses (Fase 3.41, xmlWriterFrame's own doc comment).
		if prefix == "xmlns" {
			declPrefix, declURI, isDecl = local, v, true
		}
	default:
		return runtime.Value{}, fmt.Errorf("bcl: XmlWriter.WriteAttributeString: unsupported argument shape")
	}
	if len(w.stack) == 0 {
		return runtime.Value{}, fmt.Errorf("bcl: XmlWriter.WriteAttributeString: no open element")
	}
	if isDecl {
		top := &w.stack[len(w.stack)-1]
		if top.nsDecls == nil {
			top.nsDecls = map[string]string{}
		}
		top.nsDecls[declPrefix] = declURI
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
