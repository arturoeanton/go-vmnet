package metadata

import (
	"os"
	"testing"

	"github.com/arturoeanton/go-vmnet/internal/pe"
)

// FuzzParse proves the metadata table/stream parser can't be made to
// panic by a malformed metadata root — see internal/pe's FuzzParse for
// why this matters for vmnet specifically.
func FuzzParse(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x42, 0x53, 0x4A, 0x42}) // "BSJB" signature, nothing else
	f.Add(make([]byte, 64))

	if data, err := os.ReadFile(fixtureRelPath); err == nil {
		if pf, err := pe.Parse(data); err == nil {
			f.Add(pf.Metadata)
		}
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		md, err := Parse(data)
		if err != nil || md == nil {
			return
		}
		// A successful parse must leave every row accessor safe to call,
		// including with out-of-range RIDs.
		for id := TableID(0); id <= TableGenericParamConstraint; id++ {
			count := md.RowCount(id)
			if count > 10_000 {
				count = 10_000 // don't let a crafted huge row count blow up the fuzz run itself
			}
			for rid := uint32(1); rid <= count; rid++ {
				switch id {
				case TableTypeDef:
					_, _ = md.TypeDef(rid)
				case TableMethodDef:
					_, _ = md.MethodDef(rid)
				case TableField:
					_, _ = md.Field(rid)
				}
			}
		}
		_, _ = md.TypeDef(0)
		_, _ = md.MethodDef(0)
		_, _ = md.String(0xFFFFFFFF)
		_, _ = md.Blob(0xFFFFFFFF)
	})
}
