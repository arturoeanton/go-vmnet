package bcl

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// Real XmlNodeType ordinal values (System.Xml.XmlNodeType).
const (
	xmlNodeNone                  = 0
	xmlNodeElement               = 1
	xmlNodeText                  = 3
	xmlNodeProcessingInstruction = 7
	xmlNodeComment               = 8
	xmlNodeDocumentType          = 10
	xmlNodeEndElement            = 15
	xmlNodeXmlDeclaration        = 17
)

// nativeXmlReader backs System.Xml.XmlReader (via the static XmlReader.Create
// factory) — needed because DocumentFormat.OpenXml.Framework's real,
// NuGet-cached ZipPackage/OpenXmlPartReader parse every OPC part's XML
// through it (Fase 3.40: found reading ClosedXML's real .xlsx, itself
// going through OpenXml's package layer). Wraps Go's encoding/xml.Decoder,
// which is namespace-URI-aware (like XmlReader) but discards raw prefix
// text — this reconstructs a best-effort Prefix by reverse-mapping a URI
// back to whichever prefix most recently declared it (a cumulative,
// never-popped map is safe here: real OOXML parts declare each prefix
// once, consistently, document-wide).
type nativeXmlReader struct {
	dec *xml.Decoder

	lookahead    xml.Token
	hasLookahead bool

	eof    bool
	closed bool

	nodeType int32
	depth    int32
	isEmpty  bool

	localName    string
	prefix       string
	namespaceURI string
	value        string

	attrs   []xmlReaderAttr
	attrIdx int // -1 = positioned on the node itself, not an attribute

	stack []xml.Name

	uriToPrefix map[string]string
	prefixToURI map[string]string
}

type xmlReaderAttr struct {
	prefix       string
	localName    string
	namespaceURI string
	value        string
}

func init() {
	register("System.Xml.XmlReader::Create", true, xmlReaderCreate)
	// A real subclass of the abstract XmlReader (e.g. DocumentFormat.
	// OpenXml's own XmlConvertingReader, which wraps a real BaseReader
	// field rather than needing our own nativeXmlReader backing at all)
	// chains `: base()` to this as a plain (non-newobj) call on its own
	// already-allocated receiver — a genuine no-op here, unlike Uri/
	// MemoryStream's ctor-in-place pattern, since such a subclass never
	// needs Obj.Native set to a *nativeXmlReader at all.
	register("System.Xml.XmlReader::.ctor", false, xmlReaderCtorNoop)

	registerCtor("System.Xml.XmlReaderSettings", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeXmlReaderSettings{}}, nil
	})
	for _, prop := range []string{"CloseInput", "IgnoreWhitespace", "DtdProcessing", "MaxCharactersInDocument", "IgnoreComments", "IgnoreProcessingInstructions", "CheckCharacters", "ConformanceLevel"} {
		register("System.Xml.XmlReaderSettings::set_"+prop, false, xrSettingsNoop)
	}

	register("System.Xml.XmlReader::get_NodeType", true, xmlReaderGetNodeType)
	register("System.Xml.XmlReader::get_Depth", true, xmlReaderGetDepth)
	register("System.Xml.XmlReader::get_EOF", true, xmlReaderGetEOF)
	register("System.Xml.XmlReader::get_LocalName", true, xmlReaderGetLocalName)
	register("System.Xml.XmlReader::get_Prefix", true, xmlReaderGetPrefix)
	register("System.Xml.XmlReader::get_NamespaceURI", true, xmlReaderGetNamespaceURI)
	register("System.Xml.XmlReader::get_Name", true, xmlReaderGetName)
	register("System.Xml.XmlReader::get_Value", true, xmlReaderGetValue)
	register("System.Xml.XmlReader::get_IsEmptyElement", true, xmlReaderGetIsEmptyElement)
	register("System.Xml.XmlReader::get_HasAttributes", true, xmlReaderGetHasAttributes)
	register("System.Xml.XmlReader::get_AttributeCount", true, xmlReaderGetAttributeCount)
	register("System.Xml.XmlReader::get_Item", true, xmlReaderGetAttribute)
	register("System.Xml.XmlReader::GetAttribute", true, xmlReaderGetAttribute)
	register("System.Xml.XmlReader::MoveToNextAttribute", true, xmlReaderMoveToNextAttribute)
	register("System.Xml.XmlReader::MoveToFirstAttribute", true, xmlReaderMoveToFirstAttribute)
	register("System.Xml.XmlReader::MoveToElement", true, xmlReaderMoveToElement)
	register("System.Xml.XmlReader::IsStartElement", true, xmlReaderIsStartElement)
	register("System.Xml.XmlReader::Read", true, xmlReaderRead)
	register("System.Xml.XmlReader::Skip", false, xmlReaderSkip)
	register("System.Xml.XmlReader::MoveToContent", true, xmlReaderMoveToContent)
	register("System.Xml.XmlReader::LookupNamespace", true, xmlReaderLookupNamespace)
	register("System.Xml.XmlReader::ReadInnerXml", true, xmlReaderReadInnerXml)
	register("System.Xml.XmlReader::ReadOuterXml", true, xmlReaderReadOuterXml)
	register("System.Xml.XmlReader::Close", false, xmlReaderClose)
	register("System.Xml.XmlReader::Dispose", false, xmlReaderClose)
	register("System.Xml.XmlReader::get_NameTable", true, xmlReaderGetNameTable)
}

// nativeXmlReaderSettings carries no real state: DTD processing, whitespace
// handling and character limits don't change how vmnet's own reader walks
// real, compact, machine-generated OOXML part XML — same pragmatic posture
// nativeXmlWriterSettings documents for the write side.
type nativeXmlReaderSettings struct{}

func xrSettingsNoop(args []runtime.Value) (runtime.Value, error) {
	return runtime.Value{}, nil
}

func xmlReaderCtorNoop(args []runtime.Value) (runtime.Value, error) {
	return runtime.Value{}, nil
}

func xmlReaderCreate(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 {
		return runtime.Value{}, fmt.Errorf("bcl: XmlReader.Create expects at least a source")
	}
	ms, err := valueAsMemoryStream(args[0])
	if err != nil {
		return runtime.Value{}, fmt.Errorf("bcl: XmlReader.Create only supports a Stream source (directly or through a real wrapper Stream)")
	}
	data := ms.buf
	if ms.pos > 0 && ms.pos <= len(data) {
		data = data[ms.pos:]
	}
	// Real XmlReader.Create auto-detects and strips a leading byte-order
	// mark; Go's xml.Decoder does not, and would otherwise surface it as
	// a bogus leading CharData token — found via a real, load-bearing
	// case: every OPC part ClosedXML/OpenXml write out (e.g.
	// [Content_Types].xml) is saved UTF-8-BOM-prefixed, and that stray
	// text node made MoveToContent() stop short of the real root element
	// (Fase 3.40).
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	r := &nativeXmlReader{
		dec:         dec,
		attrIdx:     -1,
		uriToPrefix: map[string]string{},
		prefixToURI: map[string]string{},
	}
	return runtime.ObjRef(&runtime.Object{Native: r}), nil
}

// asXmlReader resolves a receiver to the nativeXmlReader backing it,
// walking through any real managed wrapper reader first — the exact same
// need and technique valueAsMemoryStream documents (system_io_compression
// _zip.go): DocumentFormat.OpenXml's own XmlConvertingReader wraps a real
// inner XmlReader in a plain BaseReader field, and not every one of its
// members is actually overridden to forward there (whichever aren't fall
// through vmnet's virtual dispatch to this native with the WRAPPER
// itself as the receiver, not its inner reader) — Fase 3.40.
func asXmlReader(args []runtime.Value) (*nativeXmlReader, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("bcl: XmlReader method called without a receiver")
	}
	seen := map[*runtime.Object]bool{}
	var walk func(v runtime.Value) *nativeXmlReader
	walk = func(v runtime.Value) *nativeXmlReader {
		if v.Kind == runtime.KindRef && v.Ref != nil {
			v = *v.Ref
		}
		if v.Kind != runtime.KindObject || v.Obj == nil || seen[v.Obj] {
			return nil
		}
		seen[v.Obj] = true
		if r, ok := v.Obj.Native.(*nativeXmlReader); ok {
			return r
		}
		for _, f := range v.Obj.Fields {
			if r := walk(f); r != nil {
				return r
			}
		}
		return nil
	}
	if r := walk(args[0]); r != nil {
		return r, nil
	}
	return nil, fmt.Errorf("bcl: receiver is not an XmlReader, directly or through a real wrapper reader")
}

func (r *nativeXmlReader) nextToken() (xml.Token, error) {
	if r.hasLookahead {
		r.hasLookahead = false
		return r.lookahead, nil
	}
	return r.dec.Token()
}

func (r *nativeXmlReader) pushBack(t xml.Token) {
	r.lookahead = t
	r.hasLookahead = true
}

func (r *nativeXmlReader) lookupPrefix(uri string) string {
	if uri == "" {
		return ""
	}
	return r.uriToPrefix[uri]
}

// processStart resolves the element itself and every attribute's Prefix
// (two passes: first record any xmlns declarations carried on this same
// tag, then resolve — needed for the extremely common
// `<x:elem xmlns:x="...">` declare-and-use-on-the-same-tag OOXML pattern).
func (r *nativeXmlReader) processStart(t xml.StartElement) {
	for _, a := range t.Attr {
		switch {
		case a.Name.Space == "xmlns":
			r.uriToPrefix[a.Value] = a.Name.Local
			r.prefixToURI[a.Name.Local] = a.Value
		case a.Name.Space == "" && a.Name.Local == "xmlns":
			r.uriToPrefix[a.Value] = ""
			r.prefixToURI[""] = a.Value
		}
	}
	r.localName = t.Name.Local
	r.namespaceURI = t.Name.Space
	r.prefix = r.lookupPrefix(t.Name.Space)
	r.attrs = r.attrs[:0]
	for _, a := range t.Attr {
		switch {
		case a.Name.Space == "xmlns":
			r.attrs = append(r.attrs, xmlReaderAttr{prefix: "xmlns", localName: a.Name.Local, namespaceURI: "http://www.w3.org/2000/xmlns/", value: a.Value})
		case a.Name.Space == "" && a.Name.Local == "xmlns":
			r.attrs = append(r.attrs, xmlReaderAttr{prefix: "", localName: "xmlns", namespaceURI: "http://www.w3.org/2000/xmlns/", value: a.Value})
		default:
			r.attrs = append(r.attrs, xmlReaderAttr{prefix: r.lookupPrefix(a.Name.Space), localName: a.Name.Local, namespaceURI: a.Name.Space, value: a.Value})
		}
	}
	r.attrIdx = -1
	r.value = ""
}

func parsePseudoAttrs(inst string) []xmlReaderAttr {
	// XML declaration pseudo-attributes ("version=\"1.0\" encoding=\"utf-8\"")
	// are a fixed, simple shape — a hand-rolled scan is enough, no need for
	// a real attribute grammar (no namespaces or entities ever appear here).
	var attrs []xmlReaderAttr
	s := inst
	for {
		s = strings.TrimSpace(s)
		if s == "" {
			break
		}
		eq := strings.IndexByte(s, '=')
		if eq < 0 {
			break
		}
		name := strings.TrimSpace(s[:eq])
		rest := strings.TrimSpace(s[eq+1:])
		if rest == "" || (rest[0] != '"' && rest[0] != '\'') {
			break
		}
		quote := rest[0]
		end := strings.IndexByte(rest[1:], quote)
		if end < 0 {
			break
		}
		value := rest[1 : end+1]
		attrs = append(attrs, xmlReaderAttr{localName: name, value: value})
		s = rest[end+2:]
	}
	return attrs
}

func (r *nativeXmlReader) read() (bool, error) {
	if r.eof {
		return false, nil
	}
	tok, err := r.nextToken()
	if err != nil {
		if err == io.EOF {
			r.eof = true
			r.nodeType = xmlNodeNone
			return false, nil
		}
		return false, err
	}
	switch t := tok.(type) {
	case xml.StartElement:
		r.depth = int32(len(r.stack))
		r.stack = append(r.stack, t.Name)
		r.processStart(t)
		next, nerr := r.dec.Token()
		switch {
		case nerr == io.EOF:
			r.isEmpty = false
		case nerr != nil:
			return false, nerr
		default:
			if end, ok := next.(xml.EndElement); ok && end.Name == t.Name {
				r.isEmpty = true
				// A self-closing element has no separate EndElement read()
				// ever surfaces (matching real XmlReader — see the
				// StartElement case's own doc comment on isEmpty), so
				// nothing will pop this stack entry the normal way; pop it
				// now instead, or every later sibling's Depth would keep
				// growing by one per empty element that came before it
				// (found the hard way: ZipPackage's own ContentTypeHelper
				// require exact Depth==1 for every <Default>/<Override>
				// child, Fase 3.40).
				r.stack = r.stack[:len(r.stack)-1]
			} else {
				r.isEmpty = false
				r.pushBack(next)
			}
		}
		r.nodeType = xmlNodeElement
	case xml.EndElement:
		if len(r.stack) > 0 {
			r.stack = r.stack[:len(r.stack)-1]
		}
		r.depth = int32(len(r.stack))
		r.localName = t.Name.Local
		r.namespaceURI = t.Name.Space
		r.prefix = r.lookupPrefix(t.Name.Space)
		r.attrs = nil
		r.attrIdx = -1
		r.value = ""
		r.isEmpty = false
		r.nodeType = xmlNodeEndElement
	case xml.CharData:
		r.depth = int32(len(r.stack))
		r.value = string(t)
		r.attrs = nil
		r.attrIdx = -1
		r.isEmpty = false
		r.nodeType = xmlNodeText
	case xml.Comment:
		r.depth = int32(len(r.stack))
		r.value = string(t)
		r.attrs = nil
		r.attrIdx = -1
		r.isEmpty = false
		r.nodeType = xmlNodeComment
	case xml.ProcInst:
		r.depth = int32(len(r.stack))
		inst := string(t.Inst)
		r.localName = t.Target
		r.value = strings.TrimSpace(inst)
		r.attrIdx = -1
		r.isEmpty = false
		if t.Target == "xml" {
			r.attrs = parsePseudoAttrs(inst)
			r.nodeType = xmlNodeXmlDeclaration
		} else {
			r.attrs = nil
			r.nodeType = xmlNodeProcessingInstruction
		}
	case xml.Directive:
		r.depth = int32(len(r.stack))
		r.value = string(t)
		r.attrs = nil
		r.attrIdx = -1
		r.isEmpty = false
		r.nodeType = xmlNodeDocumentType
	default:
		return r.read()
	}
	return true, nil
}

// skip mirrors real XmlReader.Skip(): on a non-empty element, it consumes
// the entire subtree plus its matching end tag and lands one node past it
// (matching OpenXmlPartReader's own InnerSkip, which relies on exactly
// this to move to a sibling); on anything else it behaves like Read().
func (r *nativeXmlReader) skip() (bool, error) {
	if r.nodeType != xmlNodeElement || r.isEmpty {
		return r.read()
	}
	targetDepth := r.depth
	for {
		ok, err := r.read()
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
		if r.nodeType == xmlNodeEndElement && r.depth == targetDepth {
			return r.read()
		}
	}
}

func xmlReaderGetNodeType(args []runtime.Value) (runtime.Value, error) {
	r, err := asXmlReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(r.nodeType), nil
}

func xmlReaderGetDepth(args []runtime.Value) (runtime.Value, error) {
	r, err := asXmlReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(r.depth), nil
}

func xmlReaderGetEOF(args []runtime.Value) (runtime.Value, error) {
	r, err := asXmlReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(r.eof), nil
}

func xmlReaderGetLocalName(args []runtime.Value) (runtime.Value, error) {
	r, err := asXmlReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if r.attrIdx >= 0 && r.attrIdx < len(r.attrs) {
		return runtime.String(r.attrs[r.attrIdx].localName), nil
	}
	return runtime.String(r.localName), nil
}

func xmlReaderGetPrefix(args []runtime.Value) (runtime.Value, error) {
	r, err := asXmlReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if r.attrIdx >= 0 && r.attrIdx < len(r.attrs) {
		return runtime.String(r.attrs[r.attrIdx].prefix), nil
	}
	return runtime.String(r.prefix), nil
}

func xmlReaderGetNamespaceURI(args []runtime.Value) (runtime.Value, error) {
	r, err := asXmlReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if r.attrIdx >= 0 && r.attrIdx < len(r.attrs) {
		return runtime.String(r.attrs[r.attrIdx].namespaceURI), nil
	}
	return runtime.String(r.namespaceURI), nil
}

func xmlReaderGetName(args []runtime.Value) (runtime.Value, error) {
	r, err := asXmlReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	prefix, local := r.prefix, r.localName
	if r.attrIdx >= 0 && r.attrIdx < len(r.attrs) {
		prefix, local = r.attrs[r.attrIdx].prefix, r.attrs[r.attrIdx].localName
	}
	if prefix != "" {
		return runtime.String(prefix + ":" + local), nil
	}
	return runtime.String(local), nil
}

func xmlReaderGetValue(args []runtime.Value) (runtime.Value, error) {
	r, err := asXmlReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if r.attrIdx >= 0 && r.attrIdx < len(r.attrs) {
		return runtime.String(r.attrs[r.attrIdx].value), nil
	}
	return runtime.String(r.value), nil
}

func xmlReaderGetIsEmptyElement(args []runtime.Value) (runtime.Value, error) {
	r, err := asXmlReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(r.isEmpty), nil
}

func xmlReaderGetHasAttributes(args []runtime.Value) (runtime.Value, error) {
	r, err := asXmlReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(len(r.attrs) > 0), nil
}

func xmlReaderGetAttributeCount(args []runtime.Value) (runtime.Value, error) {
	r, err := asXmlReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(int32(len(r.attrs))), nil
}

// xmlReaderGetAttribute backs GetAttribute(int)/GetAttribute(string)/
// GetAttribute(string,string) and the this[...] indexer overloads alike —
// vmnet's native registry has no arity-based overload dispatch (a single
// Go function backs one fully-qualified name regardless of arg shape,
// same convention every other multi-overload native in this package
// follows), so this switches on args[1]'s own Kind instead.
func xmlReaderGetAttribute(args []runtime.Value) (runtime.Value, error) {
	r, err := asXmlReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 {
		return runtime.Value{}, fmt.Errorf("bcl: XmlReader.GetAttribute expects an argument")
	}
	if args[1].Kind == runtime.KindI4 {
		idx := int(args[1].I4)
		if idx < 0 || idx >= len(r.attrs) {
			return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentOutOfRangeException", Message: "index"}
		}
		return runtime.String(r.attrs[idx].value), nil
	}
	if args[1].Kind != runtime.KindString {
		return runtime.Null(), nil
	}
	name := args[1].Str
	if len(args) >= 3 && args[2].Kind == runtime.KindString {
		ns := args[2].Str
		for _, a := range r.attrs {
			if a.localName == name && a.namespaceURI == ns {
				return runtime.String(a.value), nil
			}
		}
		return runtime.Null(), nil
	}
	for _, a := range r.attrs {
		qn := a.localName
		if a.prefix != "" {
			qn = a.prefix + ":" + a.localName
		}
		if qn == name {
			return runtime.String(a.value), nil
		}
	}
	return runtime.Null(), nil
}

func xmlReaderMoveToNextAttribute(args []runtime.Value) (runtime.Value, error) {
	r, err := asXmlReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if r.attrIdx+1 < len(r.attrs) {
		r.attrIdx++
		return runtime.Bool(true), nil
	}
	return runtime.Bool(false), nil
}

func xmlReaderMoveToFirstAttribute(args []runtime.Value) (runtime.Value, error) {
	r, err := asXmlReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(r.attrs) == 0 {
		return runtime.Bool(false), nil
	}
	r.attrIdx = 0
	return runtime.Bool(true), nil
}

func xmlReaderMoveToElement(args []runtime.Value) (runtime.Value, error) {
	r, err := asXmlReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if r.attrIdx == -1 {
		return runtime.Bool(false), nil
	}
	r.attrIdx = -1
	return runtime.Bool(true), nil
}

func xmlReaderIsStartElement(args []runtime.Value) (runtime.Value, error) {
	r, err := asXmlReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if r.nodeType != xmlNodeElement {
		return runtime.Bool(false), nil
	}
	if len(args) >= 2 && args[1].Kind == runtime.KindString {
		name := args[1].Str
		qn := r.localName
		if r.prefix != "" {
			qn = r.prefix + ":" + r.localName
		}
		return runtime.Bool(qn == name), nil
	}
	return runtime.Bool(true), nil
}

func xmlReaderRead(args []runtime.Value) (runtime.Value, error) {
	r, err := asXmlReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	ok, rerr := r.read()
	if rerr != nil {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.Xml.XmlException", Message: rerr.Error()}
	}
	return runtime.Bool(ok), nil
}

func xmlReaderSkip(args []runtime.Value) (runtime.Value, error) {
	r, err := asXmlReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if _, rerr := r.skip(); rerr != nil {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.Xml.XmlException", Message: rerr.Error()}
	}
	return runtime.Value{}, nil
}

func xmlIsNonContentNode(nt int32) bool {
	switch nt {
	case xmlNodeNone, xmlNodeProcessingInstruction, xmlNodeDocumentType, xmlNodeComment, xmlNodeXmlDeclaration:
		return true
	}
	return false
}

func xmlReaderMoveToContent(args []runtime.Value) (runtime.Value, error) {
	r, err := asXmlReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	for !r.eof && xmlIsNonContentNode(r.nodeType) {
		if _, rerr := r.read(); rerr != nil {
			return runtime.Value{}, &runtime.ManagedException{TypeName: "System.Xml.XmlException", Message: rerr.Error()}
		}
	}
	return runtime.Int32(r.nodeType), nil
}

func xmlReaderLookupNamespace(args []runtime.Value) (runtime.Value, error) {
	r, err := asXmlReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 || args[1].Kind != runtime.KindString {
		return runtime.Null(), nil
	}
	if uri, ok := r.prefixToURI[args[1].Str]; ok {
		return runtime.String(uri), nil
	}
	return runtime.Null(), nil
}

func (r *nativeXmlReader) elementTagText(selfClose bool) string {
	var b strings.Builder
	b.WriteByte('<')
	if r.prefix != "" {
		b.WriteString(r.prefix)
		b.WriteByte(':')
	}
	b.WriteString(r.localName)
	for _, a := range r.attrs {
		b.WriteByte(' ')
		if a.prefix != "" {
			b.WriteString(a.prefix)
			b.WriteByte(':')
		}
		b.WriteString(a.localName)
		b.WriteString(`="`)
		b.WriteString(xmlEscapeAttr(a.value))
		b.WriteByte('"')
	}
	if selfClose {
		b.WriteString("/>")
	} else {
		b.WriteByte('>')
	}
	return b.String()
}

func (r *nativeXmlReader) endTagText() string {
	if r.prefix != "" {
		return "</" + r.prefix + ":" + r.localName + ">"
	}
	return "</" + r.localName + ">"
}

func (r *nativeXmlReader) nodeMarkup() string {
	switch r.nodeType {
	case xmlNodeElement:
		return r.elementTagText(r.isEmpty)
	case xmlNodeEndElement:
		return r.endTagText()
	case xmlNodeText:
		return xmlEscapeText(r.value)
	case xmlNodeComment:
		return "<!--" + r.value + "-->"
	default:
		return ""
	}
}

// readOuterXML backs ReadOuterXml(): re-serializes the current element
// (start tag through matching end tag) from vmnet's own token walk rather
// than replaying original source bytes — real callers here (OpenXmlElement
// falling back to raw storage for an unrecognized element) only need
// structurally equivalent XML, not a byte-identical copy.
func (r *nativeXmlReader) readOuterXML() (string, error) {
	if r.nodeType != xmlNodeElement {
		return "", nil
	}
	var b strings.Builder
	b.WriteString(r.elementTagText(r.isEmpty))
	if r.isEmpty {
		if _, err := r.read(); err != nil {
			return "", err
		}
		return b.String(), nil
	}
	targetDepth := r.depth
	for {
		ok, err := r.read()
		if err != nil {
			return "", err
		}
		if !ok {
			break
		}
		if r.nodeType == xmlNodeEndElement && r.depth == targetDepth {
			b.WriteString(r.endTagText())
			break
		}
		b.WriteString(r.nodeMarkup())
	}
	if _, err := r.read(); err != nil {
		return "", err
	}
	return b.String(), nil
}

// readInnerXML backs ReadInnerXml(): unlike ReadOuterXml, real XmlReader
// leaves the reader positioned ON the current element's own EndElement
// afterward, rather than moving past it.
func (r *nativeXmlReader) readInnerXML() (string, error) {
	if r.nodeType != xmlNodeElement || r.isEmpty {
		return "", nil
	}
	targetDepth := r.depth
	var b strings.Builder
	for {
		ok, err := r.read()
		if err != nil {
			return "", err
		}
		if !ok {
			break
		}
		if r.nodeType == xmlNodeEndElement && r.depth == targetDepth {
			break
		}
		b.WriteString(r.nodeMarkup())
	}
	return b.String(), nil
}

func xmlReaderReadOuterXml(args []runtime.Value) (runtime.Value, error) {
	r, err := asXmlReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	s, rerr := r.readOuterXML()
	if rerr != nil {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.Xml.XmlException", Message: rerr.Error()}
	}
	return runtime.String(s), nil
}

func xmlReaderReadInnerXml(args []runtime.Value) (runtime.Value, error) {
	r, err := asXmlReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	s, rerr := r.readInnerXML()
	if rerr != nil {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.Xml.XmlException", Message: rerr.Error()}
	}
	return runtime.String(s), nil
}

func xmlReaderClose(args []runtime.Value) (runtime.Value, error) {
	r, err := asXmlReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	r.closed = true
	return runtime.Value{}, nil
}

// xmlReaderGetNameTable backs XmlReader.NameTable — a fresh stateless
// sentinel each call is fine, matching nativeNameTable's own doc comment
// (system_xmlnametable.go): every real caller in this loop's target
// packages only ever calls .Add(string) on it and compares by value.
func xmlReaderGetNameTable(args []runtime.Value) (runtime.Value, error) {
	if _, err := asXmlReader(args); err != nil {
		return runtime.Value{}, err
	}
	return runtime.ObjRef(&runtime.Object{Native: &nativeNameTable{}}), nil
}
