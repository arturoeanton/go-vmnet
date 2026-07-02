package pe

import "encoding/binary"

const sectionHeaderSize = 40

// Section is one IMAGE_SECTION_HEADER entry: the mapping vmnet needs
// between an RVA range and the corresponding file-offset range.
type Section struct {
	Name             string
	VirtualSize      uint32
	VirtualAddress   uint32
	SizeOfRawData    uint32
	PointerToRawData uint32
	Characteristics  uint32
}

func readSections(data []byte, offset uint32, count uint16) ([]Section, error) {
	sections := make([]Section, 0, count)
	for i := uint16(0); i < count; i++ {
		start := offset + uint32(i)*sectionHeaderSize
		if uint64(start)+sectionHeaderSize > uint64(len(data)) {
			return nil, ErrInvalidPE
		}
		raw := data[start : start+sectionHeaderSize]

		nameEnd := 8
		for j, b := range raw[0:8] {
			if b == 0 {
				nameEnd = j
				break
			}
		}

		sections = append(sections, Section{
			Name:             string(raw[0:nameEnd]),
			VirtualSize:      binary.LittleEndian.Uint32(raw[8:12]),
			VirtualAddress:   binary.LittleEndian.Uint32(raw[12:16]),
			SizeOfRawData:    binary.LittleEndian.Uint32(raw[16:20]),
			PointerToRawData: binary.LittleEndian.Uint32(raw[20:24]),
			Characteristics:  binary.LittleEndian.Uint32(raw[36:40]),
		})
	}
	return sections, nil
}
