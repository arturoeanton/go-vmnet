package bcl

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// Real System.UriComponents/System.UriFormat ordinal values (stable BCL
// constants, unchanged since .NET Framework 1.1) — needed because these
// enums have no TypeDef vmnet can resolve (they live in the runtime's own
// System.Private.Uri, never a loaded NuGet assembly), so a call site's
// compiled-in literal int is all that reaches us.
const (
	uriComponentsPath                    = 0x10
	uriComponentsQuery                   = 0x20
	uriComponentsFragment                = 0x40
	uriComponentsScheme                  = 0x1
	uriComponentsUserInfo                = 0x2
	uriComponentsHost                    = 0x4
	uriComponentsPort                    = 0x8
	uriComponentsKeepDelimiter           = 0x40000000
	uriComponentsHttpRequestUrl          = uriComponentsScheme | uriComponentsHost | uriComponentsPort | uriComponentsPath | uriComponentsQuery
	uriComponentsPathAndQuery            = uriComponentsPath | uriComponentsQuery
	uriComponentsAbsoluteUri             = uriComponentsScheme | uriComponentsUserInfo | uriComponentsHost | uriComponentsPort | uriComponentsPath | uriComponentsQuery | uriComponentsFragment
	uriComponentsSerializationInfoString = int32(-2147483648) // 0x80000000 as a 32-bit signed int
)

// nativeUri backs System.Uri — needed since Fase 3.40: the real,
// NuGet-cached System.IO.Packaging.PackUriHelper identifies every OPC part
// ("/xl/worksheets/sheet1.xml") and every relationship target by Uri value
// equality/resolution, not by string. Backed by Go's net/url.URL, which
// implements the same RFC 3986 resolution family .NET's Uri targets —
// close enough for the simple absolute/relative path URIs real OPC
// packages use (no userinfo, no exotic schemes).
type nativeUri struct {
	original string
	u        *url.URL
}

func init() {
	registerCtor("System.Uri", uriCtor)
	// A real subclass of Uri (e.g. System.IO.Packaging.PackUriHelper's own
	// internal ValidatedPartUri) chains `: base(...)` to this same ctor
	// logic as a plain (non-newobj) call on its own already-allocated
	// receiver — same "Type xor Native" narrow exception documented by
	// memoryStreamCtorInPlace/baseExceptionCtorInPlace.
	register("System.Uri::.ctor", false, uriCtorInPlace)
	register("System.Uri::get_AbsolutePath", true, uriGetAbsolutePath)
	register("System.Uri::get_AbsoluteUri", true, uriGetAbsoluteUri)
	register("System.Uri::get_IsAbsoluteUri", true, uriGetIsAbsoluteUri)
	register("System.Uri::get_OriginalString", true, uriGetOriginalString)
	register("System.Uri::get_Fragment", true, uriGetFragment)
	register("System.Uri::get_Query", true, uriGetQuery)
	register("System.Uri::get_Scheme", true, uriGetScheme)
	register("System.Uri::get_Host", true, uriGetHost)
	register("System.Uri::ToString", true, uriToString)
	register("System.Uri::GetComponents", true, uriGetComponents)
	register("System.Uri::Equals", true, uriEquals)
	register("System.Uri::op_Equality", true, uriOpEquality)
	register("System.Uri::op_Inequality", true, uriOpInequality)
	register("System.Uri::GetHashCode", true, uriGetHashCode)

	register("System.Uri::TryCreate", true, uriTryCreate)
	register("System.Uri::HexEscape", true, uriHexEscape)
	register("System.Uri::UnescapeDataString", true, uriUnescapeDataString)
	register("System.Uri::EscapeDataString", true, uriEscapeDataString)
	register("System.Uri::Compare", true, uriCompare)
	register("System.Uri::get_SchemeDelimiter", true, uriGetSchemeDelimiter)

	// System.UriParser backs .NET's scheme-registration mechanism
	// (UriParser.Register lets code teach Uri how to parse a custom
	// scheme, e.g. PackUriHelper's own "pack" scheme). Go's net/url.Parse
	// already parses any scheme generically with no such registration
	// step, so IsKnownScheme always reporting true is enough to make
	// every real caller's own "if not already registered, register it"
	// guard skip straight past — Register itself is never reached.
	register("System.UriParser::IsKnownScheme", true, uriParserIsKnownScheme)
}

func uriParserIsKnownScheme(args []runtime.Value) (runtime.Value, error) {
	return runtime.Bool(true), nil
}

func uriCtor(args []runtime.Value) (*runtime.Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("bcl: Uri constructor expects at least a string argument")
	}
	if args[0].Kind == runtime.KindString {
		raw := args[0].Str
		parsed, err := url.Parse(raw)
		if err != nil {
			return nil, &runtime.ManagedException{TypeName: "System.UriFormatException", Message: err.Error()}
		}
		return &runtime.Object{Native: &nativeUri{original: raw, u: parsed}}, nil
	}
	base, err := asUriValue(args[0])
	if err != nil {
		return nil, fmt.Errorf("bcl: Uri constructor: unsupported argument shape")
	}
	var relRaw string
	if len(args) > 1 {
		switch args[1].Kind {
		case runtime.KindString:
			relRaw = args[1].Str
		case runtime.KindObject:
			rel, rerr := asUriValue(args[1])
			if rerr != nil {
				return nil, rerr
			}
			relRaw = rel.original
		}
	}
	resolved, err := base.u.Parse(relRaw)
	if err != nil {
		return nil, &runtime.ManagedException{TypeName: "System.UriFormatException", Message: err.Error()}
	}
	return &runtime.Object{Native: &nativeUri{original: resolved.String(), u: resolved}}, nil
}

func uriCtorInPlace(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return runtime.Value{}, fmt.Errorf("bcl: Uri constructor called without a receiver")
	}
	obj, err := uriCtor(args[1:])
	if err != nil {
		return runtime.Value{}, err
	}
	args[0].Obj.Native = obj.Native
	return runtime.Value{}, nil
}

func asUriValue(v runtime.Value) (*nativeUri, error) {
	if v.Kind == runtime.KindRef && v.Ref != nil {
		v = *v.Ref
	}
	if v.Kind != runtime.KindObject || v.Obj == nil {
		return nil, fmt.Errorf("bcl: expected a Uri value")
	}
	u, ok := v.Obj.Native.(*nativeUri)
	if !ok {
		return nil, fmt.Errorf("bcl: receiver is not a Uri")
	}
	return u, nil
}

func asUri(args []runtime.Value) (*nativeUri, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("bcl: Uri method called without a receiver")
	}
	return asUriValue(args[0])
}

func (u *nativeUri) getComponents(components int32) string {
	if components == uriComponentsSerializationInfoString {
		return u.u.String()
	}
	masked := components &^ uriComponentsKeepDelimiter
	switch masked {
	case uriComponentsPath:
		return u.u.EscapedPath()
	case uriComponentsPathAndQuery:
		s := u.u.EscapedPath()
		if u.u.RawQuery != "" {
			s += "?" + u.u.RawQuery
		}
		return s
	case uriComponentsAbsoluteUri, uriComponentsHttpRequestUrl, uriComponentsHttpRequestUrl | uriComponentsUserInfo:
		return u.u.String()
	default:
		return u.u.String()
	}
}

func uriGetAbsolutePath(args []runtime.Value) (runtime.Value, error) {
	u, err := asUri(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.String(u.u.EscapedPath()), nil
}

func uriGetAbsoluteUri(args []runtime.Value) (runtime.Value, error) {
	u, err := asUri(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.String(u.u.String()), nil
}

func uriGetIsAbsoluteUri(args []runtime.Value) (runtime.Value, error) {
	u, err := asUri(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(u.u.IsAbs()), nil
}

func uriGetOriginalString(args []runtime.Value) (runtime.Value, error) {
	u, err := asUri(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.String(u.original), nil
}

func uriGetFragment(args []runtime.Value) (runtime.Value, error) {
	u, err := asUri(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if u.u.Fragment == "" {
		return runtime.String(""), nil
	}
	return runtime.String("#" + u.u.EscapedFragment()), nil
}

func uriGetQuery(args []runtime.Value) (runtime.Value, error) {
	u, err := asUri(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if u.u.RawQuery == "" {
		return runtime.String(""), nil
	}
	return runtime.String("?" + u.u.RawQuery), nil
}

func uriGetScheme(args []runtime.Value) (runtime.Value, error) {
	u, err := asUri(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.String(u.u.Scheme), nil
}

func uriGetHost(args []runtime.Value) (runtime.Value, error) {
	u, err := asUri(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.String(u.u.Hostname()), nil
}

func uriToString(args []runtime.Value) (runtime.Value, error) {
	u, err := asUri(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.String(u.u.String()), nil
}

func uriGetComponents(args []runtime.Value) (runtime.Value, error) {
	u, err := asUri(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 || args[1].Kind != runtime.KindI4 {
		return runtime.Value{}, fmt.Errorf("bcl: Uri.GetComponents expects (UriComponents, UriFormat)")
	}
	return runtime.String(u.getComponents(args[1].I4)), nil
}

func uriEquals(args []runtime.Value) (runtime.Value, error) {
	u, err := asUri(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) < 2 {
		return runtime.Bool(false), nil
	}
	other, oerr := asUriValue(args[1])
	if oerr != nil {
		return runtime.Bool(false), nil
	}
	return runtime.Bool(u.u.String() == other.u.String()), nil
}

func uriOpEquality(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[0].Kind == runtime.KindNull || args[1].Kind == runtime.KindNull {
		return runtime.Bool(args[0].Kind == runtime.KindNull && (len(args) < 2 || args[1].Kind == runtime.KindNull)), nil
	}
	return uriEquals(args)
}

func uriOpInequality(args []runtime.Value) (runtime.Value, error) {
	eq, err := uriOpEquality(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Bool(!(eq.Kind == runtime.KindI4 && eq.I4 != 0)), nil
}

func uriGetHashCode(args []runtime.Value) (runtime.Value, error) {
	u, err := asUri(args)
	if err != nil {
		return runtime.Value{}, err
	}
	s := u.u.String()
	var h int32
	for i := 0; i < len(s); i++ {
		h = h*31 + int32(s[i])
	}
	return runtime.Int32(h), nil
}

func uriTryCreate(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 3 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: Uri.TryCreate expects (string, UriKind, out Uri)")
	}
	if args[2].Kind != runtime.KindRef || args[2].Ref == nil {
		return runtime.Value{}, fmt.Errorf("bcl: Uri.TryCreate expects an out parameter")
	}
	parsed, err := url.Parse(args[0].Str)
	if err != nil {
		*args[2].Ref = runtime.Null()
		return runtime.Bool(false), nil
	}
	*args[2].Ref = runtime.ObjRef(&runtime.Object{Native: &nativeUri{original: args[0].Str, u: parsed}})
	return runtime.Bool(true), nil
}

func uriHexEscape(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: Uri.HexEscape expects a char argument")
	}
	return runtime.String(fmt.Sprintf("%%%02X", byte(args[0].I4))), nil
}

func uriUnescapeDataString(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: Uri.UnescapeDataString expects a string")
	}
	s, err := url.PathUnescape(args[0].Str)
	if err != nil {
		return runtime.String(args[0].Str), nil
	}
	return runtime.String(s), nil
}

func uriEscapeDataString(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: Uri.EscapeDataString expects a string")
	}
	return runtime.String(url.QueryEscape(args[0].Str)), nil
}

func uriCompare(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 4 {
		return runtime.Value{}, fmt.Errorf("bcl: Uri.Compare expects (Uri, Uri, UriComponents, UriFormat, StringComparison)")
	}
	var s1, s2 string
	if args[0].Kind != runtime.KindNull {
		u1, err := asUriValue(args[0])
		if err != nil {
			return runtime.Value{}, err
		}
		s1 = u1.getComponents(args[2].I4)
	}
	if args[1].Kind != runtime.KindNull {
		u2, err := asUriValue(args[1])
		if err != nil {
			return runtime.Value{}, err
		}
		s2 = u2.getComponents(args[2].I4)
	}
	return runtime.Int32(int32(strings.Compare(s1, s2))), nil
}

func uriGetSchemeDelimiter(args []runtime.Value) (runtime.Value, error) {
	return runtime.String("://"), nil
}
