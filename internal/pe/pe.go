// Package pe reads Windows PE/COFF binaries and locates the CLI (CLR)
// header and metadata root within them, per ECMA-335 partition II. It
// resolves RVAs to file offsets so higher layers (internal/metadata) can
// read metadata streams and method bodies. See docs/en/ROADMAP.md, Fase 1,
// module "/pe".
package pe

import "errors"

var (
	ErrInvalidPE           = errors.New("pe: invalid PE file")
	ErrMissingCLIHeader    = errors.New("pe: missing CLI header (not a .NET assembly)")
	ErrInvalidRVA          = errors.New("pe: invalid RVA")
	ErrInvalidMetadataRoot = errors.New("pe: invalid metadata root")
)

// File is a parsed PE/CLI assembly: its section table, CLI header, and the
// raw CLI metadata root bytes (ECMA-335 partition II, §24.2.1).
type File struct {
	data     []byte
	Sections []Section
	CLI      *CLIHeader
	Metadata []byte
}

// Parse reads a PE/CLI (.NET) assembly from data.
func Parse(data []byte) (*File, error) {
	peOffset, err := readDOSHeader(data)
	if err != nil {
		return nil, err
	}

	coff, optHeaderOffset, err := readCOFFHeader(data, peOffset)
	if err != nil {
		return nil, err
	}

	dirs, err := readOptionalHeader(data, optHeaderOffset, coff.SizeOfOptionalHeader)
	if err != nil {
		return nil, err
	}

	sectionsOffset := optHeaderOffset + uint32(coff.SizeOfOptionalHeader)
	sections, err := readSections(data, sectionsOffset, coff.NumberOfSections)
	if err != nil {
		return nil, err
	}

	comDir := dirs[imageDirectoryEntryComDescriptor]
	if comDir.RVA == 0 || comDir.Size == 0 {
		return nil, ErrMissingCLIHeader
	}
	cliOffset, err := rvaToOffset(sections, comDir.RVA)
	if err != nil {
		return nil, ErrMissingCLIHeader
	}
	cli, err := readCLIHeader(data, cliOffset)
	if err != nil {
		return nil, err
	}

	if cli.MetaDataRVA == 0 || cli.MetaDataSize == 0 {
		return nil, ErrInvalidMetadataRoot
	}
	metaOffset, err := rvaToOffset(sections, cli.MetaDataRVA)
	if err != nil {
		return nil, ErrInvalidMetadataRoot
	}
	end := uint64(metaOffset) + uint64(cli.MetaDataSize)
	if end > uint64(len(data)) {
		return nil, ErrInvalidMetadataRoot
	}

	return &File{
		data:     data,
		Sections: sections,
		CLI:      cli,
		Metadata: data[metaOffset:end],
	}, nil
}
