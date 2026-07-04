package bcl

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// System.Xml.XmlConvert.VerifyNCName validates a real XML "NCName"
// production (a Name without a colon, ECMA/W3C XML Namespaces §NCName) —
// needed since Fase 3.40: System.IO.Packaging validates every
// relationship id this way before writing it into a .rels part. A
// full-fidelity NCName check (the real grammar allows a wide set of
// Unicode letter/combining/extender categories) isn't needed here — every
// real id in this loop's target packages is ASCII ("rId1"), so this
// checks the common-case shape (starts with a letter or underscore,
// followed by letters/digits/'.'/'-'/'_') and is lenient rather than
// wrong for anything Unicode-adjacent, since it isn't the thing being
// tested.
func init() {
	register("System.Xml.XmlConvert::VerifyNCName", true, xmlConvertVerifyNCName)
	// UInt32Value.Parse (Fase 3.42, found reading a real .xlsx through
	// ClosedXML 0.105.0's own `new XLWorkbook(stream)`, parsing a real
	// sheetId="1"/count="N" attribute) is the real, unmodified
	// `private protected override uint Parse(string input) => XmlConvert.
	// ToUInt32(input);` (DocumentFormat.OpenXml/UInt32Value.cs) — the
	// same real per-value-type Parse() OpenXmlSimpleValue<T>.Value's own
	// getter calls lazily on first read (see OpenXmlSimpleValue.cs's own
	// Value getter, cached in InnerValue after).
	register("System.Xml.XmlConvert::ToUInt32", true, xmlConvertToUInt32)
	// XmlConvert.ToBoolean (Fase 3.42, found reading the same real .xlsx
	// through ClosedXML — a real xsd:boolean attribute, parsed via
	// BooleanValue's own real Parse override the same way UInt32Value.
	// Parse reaches ToUInt32 above).
	register("System.Xml.XmlConvert::ToBoolean", true, xmlConvertToBoolean)
	// XmlConvert.ToInt32/ToInt64/ToDouble (Fase 3.43, found reading the
	// same real .xlsx once its worksheet cells started parsing: Int32Value.
	// Parse (DocumentFormat.OpenXml/Int32Value.cs:45), Int64Value/
	// IntegerValue.Parse (Int64Value.cs:45, IntegerValue.cs:49), and
	// DoubleValue.Parse (DoubleValue.cs:49) are all one-line `=>
	// XmlConvert.ToX(input)` overrides on the identical
	// OpenXmlSimpleValue<T>.Value lazy-parse path ToUInt32 above already
	// documents).
	register("System.Xml.XmlConvert::ToInt32", true, xmlConvertToInt32)
	register("System.Xml.XmlConvert::ToInt64", true, xmlConvertToInt64)
	register("System.Xml.XmlConvert::ToDouble", true, xmlConvertToDouble)
	// XmlConvert.DecodeName (Fase 3.43, found reading the same real .xlsx:
	// ClosedXML.Utils.XmlEncoder.DecodeString — decompiled ClosedXML.Utils/
	// XmlEncoder.cs:45 — decodes every shared-string/sheet-name it reads
	// through it).
	register("System.Xml.XmlConvert::DecodeName", true, xmlConvertDecodeName)
}

// xmlConvertDecodeName implements real XmlConvert.DecodeName's escape
// grammar: every `_xHHHH_` (4 hex digits, lowercase 'x') and `_xHHHHHHHH_`
// (8 hex digits, for a non-BMP code point) sequence decodes to the
// corresponding code point; anything not matching passes through
// unchanged, exactly like the real method (which also returns null/empty
// input unchanged).
func xmlConvertDecodeName(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Value{}, fmt.Errorf("bcl: XmlConvert.DecodeName expects a string")
	}
	if args[0].Kind != runtime.KindString {
		// Real DecodeName returns a null/empty name unchanged.
		return args[0], nil
	}
	s := args[0].Str
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '_' {
			if r, n, ok := decodeXmlNameEscape(s[i:]); ok {
				b.WriteRune(r)
				i += n
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return runtime.String(b.String()), nil
}

// decodeXmlNameEscape parses one `_xHHHH_`/`_xHHHHHHHH_` escape at the
// start of s, returning the decoded rune and the escape's byte length.
func decodeXmlNameEscape(s string) (rune, int, bool) {
	for _, hexLen := range []int{8, 4} {
		total := hexLen + 3 // '_' 'x' HEX… '_'
		if len(s) < total || s[1] != 'x' || s[total-1] != '_' {
			continue
		}
		v, err := strconv.ParseUint(s[2:2+hexLen], 16, 32)
		if err != nil {
			continue
		}
		return rune(v), total, true
	}
	return 0, 0, false
}

// xmlConvertToInt32 matches real XmlConvert.ToInt32(string)'s lexical
// rules for real OOXML attribute values (xsd:int: optional surrounding
// whitespace, optional leading sign, decimal digits only — no hex, no
// thousands separators, no exponent), throwing the same real exception
// types real .NET does on bad input, same posture as xmlConvertToUInt32.
func xmlConvertToInt32(args []runtime.Value) (runtime.Value, error) {
	n, err := xmlConvertParseInt(args, "Int32", 32)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int32(int32(n)), nil
}

// xmlConvertToInt64 — see xmlConvertToInt32; identical rules at 64-bit
// range (xsd:long / xsd:integer via IntegerValue).
func xmlConvertToInt64(args []runtime.Value) (runtime.Value, error) {
	n, err := xmlConvertParseInt(args, "Int64", 64)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.Int64(n), nil
}

func xmlConvertParseInt(args []runtime.Value, typeName string, bits int) (int64, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return 0, fmt.Errorf("bcl: XmlConvert.To%s expects a string", typeName)
	}
	s := strings.TrimSpace(args[0].Str)
	s = strings.TrimPrefix(s, "+")
	if s == "" {
		return 0, &runtime.ManagedException{TypeName: "System.FormatException", Message: "Input string was not in a correct format."}
	}
	v, err := strconv.ParseInt(s, 10, bits)
	if err != nil {
		if numErr, ok := err.(*strconv.NumError); ok && numErr.Err == strconv.ErrRange {
			return 0, &runtime.ManagedException{TypeName: "System.OverflowException", Message: fmt.Sprintf("Value was either too large or too small for an %s.", typeName)}
		}
		return 0, &runtime.ManagedException{TypeName: "System.FormatException", Message: "Input string was not in a correct format."}
	}
	return v, nil
}

// xmlConvertToDouble matches real XmlConvert.ToDouble(string)'s lexical
// rules closely enough for real OOXML attribute/cell values: xsd:double
// is culture-invariant decimal notation with optional exponent, plus the
// XML-specific spellings "INF"/"-INF"/"NaN" (which real XmlConvert maps
// to the IEEE specials — .NET's own "Infinity" spelling is NOT valid
// xsd:double, and Go's ParseFloat accepts spellings like "inf"/"infinity"
// xsd:double forbids, so the specials are matched exactly first and any
// remaining alphabetic spelling rejected before ParseFloat runs).
func xmlConvertToDouble(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: XmlConvert.ToDouble expects a string")
	}
	s := strings.TrimSpace(args[0].Str)
	switch s {
	case "INF":
		return runtime.Float64(math.Inf(1)), nil
	case "-INF":
		return runtime.Float64(math.Inf(-1)), nil
	case "NaN":
		return runtime.Float64(math.NaN()), nil
	}
	for _, r := range s {
		if (r < '0' || r > '9') && r != '.' && r != '+' && r != '-' && r != 'e' && r != 'E' {
			return runtime.Value{}, &runtime.ManagedException{TypeName: "System.FormatException", Message: "Input string was not in a correct format."}
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.FormatException", Message: "Input string was not in a correct format."}
	}
	return runtime.Float64(v), nil
}

// xmlConvertToBoolean matches real XmlConvert.ToBoolean(string)'s lexical
// rules: xsd:boolean accepts "true"/"1" and "false"/"0" (case-sensitive,
// no other spellings), trimmed of surrounding whitespace, throwing
// FormatException on anything else — the same real exception real .NET
// throws on malformed input, not a silent default.
func xmlConvertToBoolean(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: XmlConvert.ToBoolean expects a string")
	}
	switch strings.TrimSpace(args[0].Str) {
	case "true", "1":
		return runtime.Bool(true), nil
	case "false", "0":
		return runtime.Bool(false), nil
	default:
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.FormatException", Message: "String was not recognized as a valid Boolean."}
	}
}

// xmlConvertToUInt32 matches real XmlConvert.ToUInt32(string)'s lexical
// rules closely enough for real OOXML attribute values (ECMA-376's own
// xsd:unsignedInt attributes, e.g. sheetId/count/uniqueCount): optional
// surrounding whitespace, an optional leading '+', decimal digits only,
// range-checked against uint32 — throwing the same real exception types
// (FormatException/OverflowException) real .NET does on bad input,
// rather than silently truncating/wrapping a malformed value.
func xmlConvertToUInt32(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: XmlConvert.ToUInt32 expects a string")
	}
	s := strings.TrimSpace(args[0].Str)
	s = strings.TrimPrefix(s, "+")
	if s == "" {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.FormatException", Message: "Input string was not in a correct format."}
	}
	v, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		if numErr, ok := err.(*strconv.NumError); ok && numErr.Err == strconv.ErrRange {
			return runtime.Value{}, &runtime.ManagedException{TypeName: "System.OverflowException", Message: "Value was either too large or too small for a UInt32."}
		}
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.FormatException", Message: "Input string was not in a correct format."}
	}
	return runtime.Int32(int32(uint32(v))), nil
}

func xmlConvertVerifyNCName(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return runtime.Value{}, fmt.Errorf("bcl: XmlConvert.VerifyNCName expects a string")
	}
	s := args[0].Str
	runes := []rune(s)
	valid := len(runes) > 0 && (unicode.IsLetter(runes[0]) || runes[0] == '_')
	for i := 1; valid && i < len(runes); i++ {
		r := runes[i]
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_') {
			valid = false
		}
	}
	if !valid {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.Xml.XmlException", Message: fmt.Sprintf("'%s' is not a valid NCName.", s)}
	}
	return args[0], nil
}
