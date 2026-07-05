// Package metadata parses ECMA-335 CLI metadata: the #~, #Strings, #US,
// #Blob and #GUID streams, the metadata tables (TypeDef, MethodDef,
// MemberRef, ...), tokens, coded indexes and method/field/local signatures.
// It is the layer that turns raw metadata bytes from internal/pe into typed
// records the rest of the runtime can resolve. See docs/en/ROADMAP.md, Fase 1,
// module "/metadata".
package metadata

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
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

	// typeDefCacheMu/typeDefCache memoize FindTypeDef (resolver.go) —
	// otherwise-plain-O(n) linear scans of the TypeDef table, each row
	// decoded fresh off the string heap. FindTypeDef is a pure function
	// of the metadata (which never changes after Parse returns), and a
	// real resolution chain looks the SAME (namespace, name) up over and
	// over: DocumentFormat.OpenXml.dll alone has thousands of TypeDefs,
	// and opening even a small .xlsx/.docx re-resolves the same handful
	// of feature/element types on every part loaded — Fase 3.49, found
	// via a reproducible closedxml-demo hang (deep, heavily-nested
	// resolveByFullName/resolveByFullNameCrossPackage recursion during
	// real package opening; a goroutine dump showed the main goroutine
	// [runnable], not deadlocked — just doing this scan over and over).
	// A mutex (not the caller's own single-threaded assumption) because
	// a *Metadata is shared read-only across every Assembly/Machine that
	// loads the same file, and nothing here guarantees one goroutine.
	typeDefCacheMu sync.RWMutex
	typeDefCache   map[string]typeDefCacheEntry

	// methodCandidatesCacheMu/methodCandidatesCache memoize
	// FindMethodDefCandidates (resolver.go) the same way typeDefCache
	// memoizes FindTypeDef above, and for the same reason: a real call
	// chain resolves the SAME (typeRID, methodName) pair over and over
	// (every single repeat call to a method — a tight loop calling the
	// same method thousands of times re-scans that type's own MethodDef
	// row range, decoding every row's Name off the string heap, on
	// EVERY iteration) — Fase 3.73, found profiling assembly.go's own
	// pickMethodOverload, which had no cache at all between it and this
	// table scan (unlike the type-lookup step just above it, already
	// cached since Fase 3.49).
	methodCandidatesCacheMu sync.RWMutex
	methodCandidatesCache   map[methodCandidatesCacheKey]methodCandidatesCacheEntry

	// methodSigCacheMu/methodSigCache memoize ParseMethodSig (signatures.go)
	// via ParseMethodSigCached — the other half of the same real, repeat-
	// call cost methodCandidatesCache closes (Fase 3.73): a signature blob
	// is small, but re-decoding it (compressed-integer parsing, a fresh
	// Params slice allocation) on every single call to the same method is
	// exactly the same "redo pure, input-only-dependent work every time"
	// waste, just smaller in absolute cost per call. Keyed by the blob's
	// own bytes (as a string) rather than a method RID, since not every
	// caller has one at hand (MemberRef/StandAloneSig signatures have no
	// MethodDef RID at all) — a real method signature blob is a handful
	// of bytes, cheap to use as a map key directly.
	methodSigCacheMu sync.RWMutex
	methodSigCache   map[string]methodSigCacheEntry
}

// methodSigCacheEntry caches one ParseMethodSig outcome, hit or miss.
type methodSigCacheEntry struct {
	sig   MethodSig
	found bool
}

// typeDefCacheEntry caches one FindTypeDef outcome, hit or miss — a miss
// re-scans the whole table exactly as expensively as a hit (nothing about
// scanning to the end is cheaper than stopping partway), so an uncached
// miss is just as capable of compounding into the same hang.
type typeDefCacheEntry struct {
	rid   uint32
	row   TypeDefRow
	found bool
}

// methodCandidatesCacheKey is (typeRID, methodName) — the two inputs
// FindMethodDefCandidates' own result depends on and nothing else (a
// TypeDef's own MethodDef row range never changes after Parse returns).
type methodCandidatesCacheKey struct {
	typeRID uint32
	name    string
}

// methodCandidatesCacheEntry mirrors typeDefCacheEntry: caches a miss
// (found=false) just as eagerly as a hit, since a miss scans exactly as
// much of the table as a hit that happens to be the last candidate.
type methodCandidatesCacheEntry struct {
	rids  []uint32
	rows  []MethodDefRow
	found bool
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
