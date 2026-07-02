package il

import (
	"os"
	"testing"

	"github.com/arturoeanton/go-vmnet/internal/metadata"
	"github.com/arturoeanton/go-vmnet/internal/pe"
)

// FuzzReadMethodBody proves the method-header parser (tiny vs fat format)
// can't be made to panic by a malformed method body — the header's own
// byte controls how many following bytes get sliced as IL.
func FuzzReadMethodBody(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x02})                                                       // tiny, 0 bytes of code
	f.Add([]byte{0x13, 0x30, 0x01, 0x00, 0x04, 0x00, 0x00, 0x00, 0, 0, 0, 0}) // fat header, no code

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = ReadMethodBody(data)
	})
}

// FuzzDecode proves the opcode decoder can't be made to panic by
// truncated operands or unknown opcodes.
func FuzzDecode(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x00, 0x00, 0x2A})       // nop, nop, ret
	f.Add([]byte{0xFE})                   // truncated two-byte opcode
	f.Add([]byte{0x28, 0x01, 0x00, 0x00}) // call with a truncated token

	if data, err := os.ReadFile(fixtureRelPath); err == nil {
		if pf, err := pe.Parse(data); err == nil {
			if md, err := metadata.Parse(pf.Metadata); err == nil {
				if rid, _, err := md.FindTypeDef("Vmnet.Fixtures", "Loops"); err == nil {
					if _, m, err := md.FindMethodDef(rid, "Sum"); err == nil && m.RVA != 0 {
						if body, err := pf.RVA(m.RVA); err == nil {
							if _, code, err := ReadMethodBody(body); err == nil {
								f.Add(code)
							}
						}
					}
				}
			}
		}
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Decode(data)
	})
}

// FuzzReadExceptionHandlers proves the EH-clause-table parser (Fase
// 3.10 — small and fat section/clause formats, chained MoreSects
// sections) can't be made to panic or hang on malformed data: a crafted
// DataSize/MoreSects combination could otherwise loop or read out of
// bounds.
func FuzzReadExceptionHandlers(f *testing.F) {
	f.Add([]byte{}, 0)
	f.Add([]byte{0, 0, 0, 0}, 4)                                                       // codeEnd==len(data), no section bytes
	f.Add([]byte{0, 0, 0, 0, 0x01, 0x10, 0, 0, 2, 0, 0, 0, 5, 5, 0, 3, 0, 0, 0, 0}, 4) // small, one Finally clause
	f.Add([]byte{0, 0, 0, 0, 0x41, 28, 0, 0}, 4)                                       // fat header claiming more data than present
	f.Add([]byte{0, 0, 0, 0, 0x81, 0x10, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, 4) // MoreSects set, no next section

	f.Fuzz(func(t *testing.T, data []byte, codeEnd int) {
		if codeEnd < 0 || codeEnd > len(data) {
			return
		}
		_, _ = ReadExceptionHandlers(data, MethodHeader{MoreSections: true}, codeEnd)
	})
}
