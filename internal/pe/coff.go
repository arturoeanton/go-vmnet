package pe

import "encoding/binary"

const (
	dosSignature = 0x5A4D     // "MZ"
	peSignature  = 0x00004550 // "PE\0\0" as a little-endian uint32
	peOffsetPtr  = 0x3C       // e_lfanew: offset of the PE header offset field

	peMagicPE32     = 0x10b
	peMagicPE32Plus = 0x20b

	imageDirectoryEntryComDescriptor = 14
	imageNumberOfDirectoryEntries    = 16
)

type coffHeader struct {
	Machine              uint16
	NumberOfSections     uint16
	TimeDateStamp        uint32
	PointerToSymbolTable uint32
	NumberOfSymbols      uint32
	SizeOfOptionalHeader uint16
	Characteristics      uint16
}

// readDOSHeader validates the MS-DOS stub signature and returns the file
// offset of the PE header (the e_lfanew field).
func readDOSHeader(data []byte) (peHeaderOffset uint32, err error) {
	if len(data) < 0x40 {
		return 0, ErrInvalidPE
	}
	if binary.LittleEndian.Uint16(data[0:2]) != dosSignature {
		return 0, ErrInvalidPE
	}
	off := binary.LittleEndian.Uint32(data[peOffsetPtr : peOffsetPtr+4])
	if uint64(off)+4 > uint64(len(data)) {
		return 0, ErrInvalidPE
	}
	return off, nil
}

// readCOFFHeader validates the PE signature and parses the COFF header that
// follows it, returning the file offset where the optional header starts.
func readCOFFHeader(data []byte, peOffset uint32) (h coffHeader, optionalHeaderOffset uint32, err error) {
	if uint64(peOffset)+24 > uint64(len(data)) {
		return coffHeader{}, 0, ErrInvalidPE
	}
	if binary.LittleEndian.Uint32(data[peOffset:peOffset+4]) != peSignature {
		return coffHeader{}, 0, ErrInvalidPE
	}

	base := peOffset + 4
	h = coffHeader{
		Machine:              binary.LittleEndian.Uint16(data[base : base+2]),
		NumberOfSections:     binary.LittleEndian.Uint16(data[base+2 : base+4]),
		TimeDateStamp:        binary.LittleEndian.Uint32(data[base+4 : base+8]),
		PointerToSymbolTable: binary.LittleEndian.Uint32(data[base+8 : base+12]),
		NumberOfSymbols:      binary.LittleEndian.Uint32(data[base+12 : base+16]),
		SizeOfOptionalHeader: binary.LittleEndian.Uint16(data[base+16 : base+18]),
		Characteristics:      binary.LittleEndian.Uint16(data[base+18 : base+20]),
	}
	return h, base + 20, nil
}

type dataDirectory struct {
	RVA  uint32
	Size uint32
}

// readOptionalHeader parses just enough of the PE32/PE32+ optional header
// to recover its 16 data directories; vmnet only needs the COM Descriptor
// (CLI header) directory, so the rest of the optional header fields are
// intentionally not modeled.
func readOptionalHeader(data []byte, offset uint32, size uint16) ([imageNumberOfDirectoryEntries]dataDirectory, error) {
	var dirs [imageNumberOfDirectoryEntries]dataDirectory

	if size == 0 || uint64(offset)+uint64(size) > uint64(len(data)) {
		return dirs, ErrInvalidPE
	}

	magic := binary.LittleEndian.Uint16(data[offset : offset+2])

	// Offset of the DataDirectory array from the start of the optional
	// header: 96 bytes of standard+Windows-specific fields for PE32 (no
	// BaseOfData field and an 8-byte ImageBase for PE32+ shift this to 112).
	var dirOffset uint32
	switch magic {
	case peMagicPE32:
		dirOffset = offset + 96
	case peMagicPE32Plus:
		dirOffset = offset + 112
	default:
		return dirs, ErrInvalidPE
	}

	end := offset + uint32(size)
	for i := 0; i < imageNumberOfDirectoryEntries; i++ {
		entryOffset := dirOffset + uint32(i*8)
		if uint64(entryOffset)+8 > uint64(end) || uint64(entryOffset)+8 > uint64(len(data)) {
			break // SizeOfOptionalHeader may declare fewer than 16 directories
		}
		dirs[i] = dataDirectory{
			RVA:  binary.LittleEndian.Uint32(data[entryOffset : entryOffset+4]),
			Size: binary.LittleEndian.Uint32(data[entryOffset+4 : entryOffset+8]),
		}
	}
	return dirs, nil
}
