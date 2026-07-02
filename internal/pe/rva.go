package pe

func rvaToOffset(sections []Section, rva uint32) (uint32, error) {
	for _, s := range sections {
		size := s.VirtualSize
		if s.SizeOfRawData > size {
			size = s.SizeOfRawData
		}
		if rva >= s.VirtualAddress && rva < s.VirtualAddress+size {
			return s.PointerToRawData + (rva - s.VirtualAddress), nil
		}
	}
	return 0, ErrInvalidRVA
}

// OffsetFromRVA converts a relative virtual address into a file offset.
func (f *File) OffsetFromRVA(rva uint32) (uint32, error) {
	return rvaToOffset(f.Sections, rva)
}

// RVA returns the file bytes starting at rva and extending to the end of
// the containing section's raw data. Callers slice further using a known
// length (e.g. a method body size or a metadata stream size).
func (f *File) RVA(rva uint32) ([]byte, error) {
	off, err := f.OffsetFromRVA(rva)
	if err != nil {
		return nil, err
	}
	if uint64(off) > uint64(len(f.data)) {
		return nil, ErrInvalidRVA
	}
	return f.data[off:], nil
}
