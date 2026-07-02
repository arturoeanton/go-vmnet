package pe

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const fixtureRelPath = "../../tests/fixtures/csharp/bin/Release/netstandard2.0/Vmnet.Fixtures.dll"

func readFixture(t *testing.T) []byte {
	t.Helper()
	path := filepath.FromSlash(fixtureRelPath)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("fixture assembly not built: %v (run `dotnet build tests/fixtures/csharp/Fixtures.csproj -c Release`)", err)
	}
	return data
}

func TestParse_RealAssembly(t *testing.T) {
	data := readFixture(t)

	f, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	if f.CLI == nil {
		t.Fatal("Parse() f.CLI = nil, want a CLI header")
	}
	if f.CLI.MajorRuntimeVersion == 0 {
		t.Errorf("f.CLI.MajorRuntimeVersion = 0, want > 0")
	}

	if len(f.Sections) < 2 {
		t.Errorf("len(f.Sections) = %d, want >= 2 (a real assembly has at least .text and .reloc)", len(f.Sections))
	}

	// Metadata root signature per ECMA-335 §24.2.1: 4-byte "BSJB" magic.
	wantSig := []byte("BSJB")
	if len(f.Metadata) < 4 || !bytes.Equal(f.Metadata[0:4], wantSig) {
		t.Errorf("f.Metadata[0:4] = %q, want %q", f.Metadata[:min(4, len(f.Metadata))], wantSig)
	}
}

func TestParse_InvalidPE(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"too short", []byte{0x4D, 0x5A}},
		{"bad DOS signature", bytes.Repeat([]byte{0x00}, 128)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.data)
			if !errors.Is(err, ErrInvalidPE) {
				t.Fatalf("Parse() error = %v, want ErrInvalidPE", err)
			}
		})
	}
}

func TestParse_MissingCLIHeader(t *testing.T) {
	data := readFixture(t)

	peOffset, err := readDOSHeader(data)
	if err != nil {
		t.Fatalf("readDOSHeader() error = %v", err)
	}
	_, optOffset, err := readCOFFHeader(data, peOffset)
	if err != nil {
		t.Fatalf("readCOFFHeader() error = %v", err)
	}
	magic := binary.LittleEndian.Uint16(data[optOffset : optOffset+2])

	var dirOffset uint32
	switch magic {
	case peMagicPE32:
		dirOffset = optOffset + 96
	case peMagicPE32Plus:
		dirOffset = optOffset + 112
	default:
		t.Fatalf("unexpected optional header magic %#x", magic)
	}

	comEntryOffset := dirOffset + imageDirectoryEntryComDescriptor*8
	mutated := append([]byte(nil), data...)
	for i := uint32(0); i < 8; i++ {
		mutated[comEntryOffset+i] = 0
	}

	_, err = Parse(mutated)
	if !errors.Is(err, ErrMissingCLIHeader) {
		t.Fatalf("Parse() error = %v, want ErrMissingCLIHeader", err)
	}
}

func TestParse_InvalidMetadataRoot(t *testing.T) {
	data := readFixture(t)

	f, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	peOffset, _ := readDOSHeader(data)
	_, optOffset, _ := readCOFFHeader(data, peOffset)
	magic := binary.LittleEndian.Uint16(data[optOffset : optOffset+2])
	var dirOffset uint32
	switch magic {
	case peMagicPE32:
		dirOffset = optOffset + 96
	case peMagicPE32Plus:
		dirOffset = optOffset + 112
	}
	comEntryOffset := dirOffset + imageDirectoryEntryComDescriptor*8
	comRVA := binary.LittleEndian.Uint32(data[comEntryOffset : comEntryOffset+4])
	cliOffset, err := f.OffsetFromRVA(comRVA)
	if err != nil {
		t.Fatalf("OffsetFromRVA(comRVA) error = %v", err)
	}

	// MetaDataRVA lives at offset 8 within IMAGE_COR20_HEADER.
	mutated := append([]byte(nil), data...)
	binary.LittleEndian.PutUint32(mutated[cliOffset+8:cliOffset+12], 0xFFFFFFF0)

	_, err = Parse(mutated)
	if !errors.Is(err, ErrInvalidMetadataRoot) {
		t.Fatalf("Parse() error = %v, want ErrInvalidMetadataRoot", err)
	}
}

func TestFile_RVA_InvalidRVA(t *testing.T) {
	data := readFixture(t)
	f, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if _, err := f.OffsetFromRVA(0xFFFFFFFF); !errors.Is(err, ErrInvalidRVA) {
		t.Fatalf("OffsetFromRVA(huge) error = %v, want ErrInvalidRVA", err)
	}
}
