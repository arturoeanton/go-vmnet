package metadata

// Token is an ECMA-335 metadata token: a table id in the top byte and a
// 1-based row id (RID) in the low 3 bytes (§II.22.2). RID 0 means "null".
type Token uint32

// NewToken builds a token from a table id and a 1-based row id.
func NewToken(table TableID, rid uint32) Token {
	return Token(uint32(table)<<24 | (rid & 0x00FFFFFF))
}

// Table returns the token's table id.
func (t Token) Table() TableID { return TableID(t >> 24) }

// RID returns the token's 1-based row id (0 means null).
func (t Token) RID() uint32 { return uint32(t) & 0x00FFFFFF }

// IsNil reports whether the token has a null (zero) RID.
func (t Token) IsNil() bool { return t.RID() == 0 }
