package pe

import (
	"os"
	"testing"
)

// FuzzParse proves Parse (and the RVA lookups a caller would naturally do
// afterward) can't be made to panic by malformed or adversarial bytes — a
// vmnet plugin is untrusted input, so a crafted .dll must produce an
// error, never crash the host process. `go test` runs the seed corpus
// below as regular cases; `go test -fuzz=FuzzParse` mutates it further.
func FuzzParse(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x4D, 0x5A})
	f.Add(make([]byte, 128))

	if data, err := os.ReadFile(fixtureRelPath); err == nil {
		f.Add(data)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		file, err := Parse(data)
		if err != nil || file == nil {
			return
		}
		// A successful parse must leave File usable without panicking,
		// including on out-of-range RVAs a corrupted CLI header could carry.
		_, _ = file.OffsetFromRVA(0)
		_, _ = file.OffsetFromRVA(0xFFFFFFFF)
		if file.CLI != nil {
			_, _ = file.RVA(file.CLI.MetaDataRVA)
		}
	})
}
