package metadata

import (
	"encoding/binary"
	"errors"
	"unicode/utf16"
)

// decodeCompressed reads an ECMA-335 §II.23.2 compressed unsigned integer
// from the start of b, returning its value and how many bytes it occupied.
func decodeCompressed(b []byte) (value uint32, size int, err error) {
	if len(b) == 0 {
		return 0, 0, errors.New("metadata: empty compressed integer")
	}
	first := b[0]
	switch {
	case first&0x80 == 0:
		return uint32(first), 1, nil
	case first&0xC0 == 0x80:
		if len(b) < 2 {
			return 0, 0, errors.New("metadata: truncated compressed integer")
		}
		return (uint32(first&0x3F) << 8) | uint32(b[1]), 2, nil
	case first&0xE0 == 0xC0:
		if len(b) < 4 {
			return 0, 0, errors.New("metadata: truncated compressed integer")
		}
		return (uint32(first&0x1F) << 24) | (uint32(b[1]) << 16) | (uint32(b[2]) << 8) | uint32(b[3]), 4, nil
	default:
		return 0, 0, errors.New("metadata: invalid compressed integer prefix")
	}
}

// stringHeap is the #Strings heap: UTF-8, null-terminated, byte-indexed.
type stringHeap struct{ data []byte }

func newStringHeap(data []byte) *stringHeap { return &stringHeap{data: data} }

func (h *stringHeap) String(index uint32) (string, error) {
	if h == nil || index == 0 {
		return "", nil
	}
	if uint64(index) >= uint64(len(h.data)) {
		return "", ErrOutOfRange
	}
	end := index
	for end < uint32(len(h.data)) && h.data[end] != 0 {
		end++
	}
	return string(h.data[index:end]), nil
}

// guidHeap is the #GUID heap: a 1-based array of 16-byte GUIDs.
type guidHeap struct{ data []byte }

func newGUIDHeap(data []byte) *guidHeap { return &guidHeap{data: data} }

func (h *guidHeap) GUID(index uint32) ([16]byte, error) {
	var g [16]byte
	if h == nil || index == 0 {
		return g, nil
	}
	off := uint64(index-1) * 16
	if off+16 > uint64(len(h.data)) {
		return g, ErrOutOfRange
	}
	copy(g[:], h.data[off:off+16])
	return g, nil
}

// blobHeap is the #Blob heap: length-prefixed (compressed integer) byte
// blobs, byte-indexed — signatures, constant values, custom attributes.
type blobHeap struct{ data []byte }

func newBlobHeap(data []byte) *blobHeap { return &blobHeap{data: data} }

func (h *blobHeap) Blob(index uint32) ([]byte, error) {
	if h == nil || index == 0 {
		return nil, nil
	}
	if uint64(index) >= uint64(len(h.data)) {
		return nil, ErrOutOfRange
	}
	n, sz, err := decodeCompressed(h.data[index:])
	if err != nil {
		return nil, err
	}
	start := index + uint32(sz)
	end := uint64(start) + uint64(n)
	if end > uint64(len(h.data)) {
		return nil, ErrOutOfRange
	}
	return h.data[start:end], nil
}

// usHeap is the #US ("user strings") heap: length-prefixed UTF-16LE
// strings used as ldstr operands, byte-indexed.
type usHeap struct{ data []byte }

func newUSHeap(data []byte) *usHeap { return &usHeap{data: data} }

func (h *usHeap) String(index uint32) (string, error) {
	if h == nil || index == 0 {
		return "", nil
	}
	if uint64(index) >= uint64(len(h.data)) {
		return "", ErrOutOfRange
	}
	n, sz, err := decodeCompressed(h.data[index:])
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", nil
	}
	// The compressed length covers the UTF-16 bytes plus one trailing
	// "has non-ASCII char" flag byte that isn't part of the string.
	charBytes := n - 1
	start := index + uint32(sz)
	end := uint64(start) + uint64(charBytes)
	if end > uint64(len(h.data)) {
		return "", ErrOutOfRange
	}
	raw := h.data[start:uint32(end)]

	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(raw[i*2 : i*2+2])
	}
	return string(utf16.Decode(units)), nil
}
