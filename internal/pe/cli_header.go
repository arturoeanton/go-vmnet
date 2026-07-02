package pe

import "encoding/binary"

const cliHeaderSize = 72

// CLIHeader is the IMAGE_COR20_HEADER (ECMA-335 partition II, §25.3.3):
// the entry point vmnet uses to find the metadata root. Fields beyond
// metadata and the managed entry point aren't modeled yet — nothing in
// the Fase 1-2 pipeline needs them.
type CLIHeader struct {
	MajorRuntimeVersion uint16
	MinorRuntimeVersion uint16
	MetaDataRVA         uint32
	MetaDataSize        uint32
	Flags               uint32
	EntryPointToken     uint32
	ResourcesRVA        uint32
	ResourcesSize       uint32
}

func readCLIHeader(data []byte, offset uint32) (*CLIHeader, error) {
	if uint64(offset)+cliHeaderSize > uint64(len(data)) {
		return nil, ErrMissingCLIHeader
	}
	raw := data[offset : offset+cliHeaderSize]

	cb := binary.LittleEndian.Uint32(raw[0:4])
	if cb < cliHeaderSize {
		return nil, ErrMissingCLIHeader
	}

	return &CLIHeader{
		MajorRuntimeVersion: binary.LittleEndian.Uint16(raw[4:6]),
		MinorRuntimeVersion: binary.LittleEndian.Uint16(raw[6:8]),
		MetaDataRVA:         binary.LittleEndian.Uint32(raw[8:12]),
		MetaDataSize:        binary.LittleEndian.Uint32(raw[12:16]),
		Flags:               binary.LittleEndian.Uint32(raw[16:20]),
		EntryPointToken:     binary.LittleEndian.Uint32(raw[20:24]),
		ResourcesRVA:        binary.LittleEndian.Uint32(raw[24:28]),
		ResourcesSize:       binary.LittleEndian.Uint32(raw[28:32]),
	}, nil
}
