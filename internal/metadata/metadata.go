// Package metadata parses ECMA-335 CLI metadata: the #~, #Strings, #US,
// #Blob and #GUID streams, the metadata tables (TypeDef, MethodDef,
// MemberRef, ...), tokens, coded indexes and method/field/local signatures.
// It is the layer that turns raw metadata bytes from internal/pe into typed
// records the rest of the runtime can resolve. See docs/ROADMAP.md, Fase 1,
// module "/metadata".
package metadata

import (
	"encoding/binary"
	"errors"
	"fmt"
)

var (
	ErrInvalidMetadataRoot = errors.New("metadata: invalid metadata root")
	ErrMissingStream       = errors.New("metadata: missing required stream")
	ErrUnsupportedTable    = errors.New("metadata: unsupported table layout")
	ErrOutOfRange          = errors.New("metadata: index out of range")
)

const metadataSignature = 0x424A5342 // "BSJB", ECMA-335 §II.24.2.1

type streamHeader struct {
	Offset uint32
	Size   uint32
}

// Metadata is the parsed CLI metadata root: heaps and tables, ready for
// higher layers (internal/il, internal/runtime) to resolve tokens against.
type Metadata struct {
	strings *stringHeap
	us      *usHeap
	blob    *blobHeap
	guid    *guidHeap

	tables map[TableID]*Table
}

// Parse reads a CLI metadata root (ECMA-335 §II.24.2.1) — the bytes
// pe.File.Metadata points to.
func Parse(root []byte) (*Metadata, error) {
	if len(root) < 16 || binary.LittleEndian.Uint32(root[0:4]) != metadataSignature {
		return nil, ErrInvalidMetadataRoot
	}

	versionLength := binary.LittleEndian.Uint32(root[12:16])
	pos := uint32(16) + versionLength
	if uint64(pos)+4 > uint64(len(root)) {
		return nil, ErrInvalidMetadataRoot
	}

	// Flags (2 bytes, reserved) immediately precede NumberOfStreams.
	numStreams := binary.LittleEndian.Uint16(root[pos+2 : pos+4])
	pos += 4

	streams := make(map[string]streamHeader, numStreams)
	for i := uint16(0); i < numStreams; i++ {
		if uint64(pos)+8 > uint64(len(root)) {
			return nil, ErrInvalidMetadataRoot
		}
		offset := binary.LittleEndian.Uint32(root[pos : pos+4])
		size := binary.LittleEndian.Uint32(root[pos+4 : pos+8])
		pos += 8

		nameStart := pos
		nameEnd := nameStart
		for nameEnd < uint32(len(root)) && root[nameEnd] != 0 {
			nameEnd++
		}
		if nameEnd >= uint32(len(root)) {
			return nil, ErrInvalidMetadataRoot
		}
		name := string(root[nameStart:nameEnd])

		consumed := nameEnd - nameStart + 1
		pad := (4 - consumed%4) % 4
		pos = nameEnd + 1 + pad

		streams[name] = streamHeader{Offset: offset, Size: size}
	}

	md := &Metadata{tables: map[TableID]*Table{}}

	if s, ok := streams["#Strings"]; ok {
		md.strings = newStringHeap(sliceStream(root, s))
	}
	if s, ok := streams["#US"]; ok {
		md.us = newUSHeap(sliceStream(root, s))
	}
	if s, ok := streams["#Blob"]; ok {
		md.blob = newBlobHeap(sliceStream(root, s))
	}
	if s, ok := streams["#GUID"]; ok {
		md.guid = newGUIDHeap(sliceStream(root, s))
	}

	tablesStream, ok := streams["#~"]
	if !ok {
		tablesStream, ok = streams["#-"]
	}
	if !ok {
		return nil, fmt.Errorf("%w: #~", ErrMissingStream)
	}

	if err := md.parseTables(sliceStream(root, tablesStream)); err != nil {
		return nil, err
	}

	return md, nil
}

func sliceStream(root []byte, h streamHeader) []byte {
	end := uint64(h.Offset) + uint64(h.Size)
	if end > uint64(len(root)) {
		end = uint64(len(root))
	}
	if uint64(h.Offset) > end {
		return nil
	}
	return root[h.Offset:end]
}

type heapSizes struct {
	wideStrings bool
	wideGUID    bool
	wideBlob    bool
}

// parseTables reads the #~ tables-stream header (ECMA-335 §II.24.2.6) and
// slices out each present table's raw row data.
func (md *Metadata) parseTables(data []byte) error {
	if len(data) < 24 {
		return ErrInvalidMetadataRoot
	}

	heapFlags := data[6]
	hs := heapSizes{
		wideStrings: heapFlags&0x01 != 0,
		wideGUID:    heapFlags&0x02 != 0,
		wideBlob:    heapFlags&0x04 != 0,
	}
	valid := binary.LittleEndian.Uint64(data[8:16])

	reservedShift := uint(TableGenericParamConstraint) + 1
	reservedMask := ^uint64(0) << reservedShift
	if valid&reservedMask != 0 {
		return fmt.Errorf("%w: reserved table bit set", ErrUnsupportedTable)
	}

	pos := uint32(24)

	var order []TableID
	rowCounts := make(map[TableID]uint32)
	for id := TableID(0); id <= TableGenericParamConstraint; id++ {
		if valid&(1<<uint(id)) == 0 {
			continue
		}
		if uint64(pos)+4 > uint64(len(data)) {
			return ErrInvalidMetadataRoot
		}
		rowCounts[id] = binary.LittleEndian.Uint32(data[pos : pos+4])
		pos += 4
		order = append(order, id)
	}

	for _, id := range order {
		schema, ok := tableSchemas[id]
		if !ok {
			return fmt.Errorf("%w: table %#x", ErrUnsupportedTable, byte(id))
		}

		colWidths := make([]uint32, len(schema.columns))
		colOffsets := make([]uint32, len(schema.columns))
		var rowSize uint32
		for i, c := range schema.columns {
			w := columnWidth(c, hs, rowCounts)
			colWidths[i] = w
			colOffsets[i] = rowSize
			rowSize += w
		}

		count := rowCounts[id]
		size := uint64(rowSize) * uint64(count)
		if uint64(pos)+size > uint64(len(data)) {
			return ErrInvalidMetadataRoot
		}

		md.tables[id] = &Table{
			id:         id,
			schema:     schema,
			rowCount:   count,
			rowSize:    rowSize,
			data:       data[pos : uint64(pos)+size],
			colOffsets: colOffsets,
			colWidths:  colWidths,
		}
		pos += uint32(size)
	}

	return nil
}

// RowCount returns how many rows table id has (0 if the table is absent).
func (md *Metadata) RowCount(id TableID) uint32 {
	t := md.tables[id]
	if t == nil {
		return 0
	}
	return t.rowCount
}

// UserString resolves a ldstr token's #US heap offset (the low 3 bytes of
// a token whose table byte is 0x70) to its string value.
func (md *Metadata) UserString(heapOffset uint32) (string, error) {
	if md.us == nil {
		return "", fmt.Errorf("%w: no #US stream", ErrMissingStream)
	}
	return md.us.String(heapOffset)
}

// Blob resolves a raw #Blob heap index (as stored in a metadata column) to
// its bytes — e.g. for a MethodDef/Field/MemberRef/StandAloneSig signature.
func (md *Metadata) Blob(index uint32) ([]byte, error) {
	return md.blob.Blob(index)
}

// String resolves a raw #Strings heap index to its value.
func (md *Metadata) String(index uint32) (string, error) {
	return md.strings.String(index)
}

// GUID resolves a raw #GUID heap index (1-based) to its 16 bytes.
func (md *Metadata) GUID(index uint32) ([16]byte, error) {
	return md.guid.GUID(index)
}
