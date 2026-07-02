package il

import (
	"encoding/binary"
	"fmt"
)

// HandlerKind is one EH clause's kind (ECMA-335 §II.25.4.6's Flags column).
type HandlerKind byte

const (
	HandlerCatch HandlerKind = iota
	HandlerFilter
	HandlerFinally
	HandlerFault
)

// ExceptionHandler is one parsed EH clause. TryOffset/TryLength/
// HandlerOffset/HandlerLength/FilterOffset are method-body-relative IL
// byte offsets, matching Instruction.Offset — ready for the same
// offset-to-IR-index resolution ir.Build already does for branch targets.
type ExceptionHandler struct {
	Kind          HandlerKind
	TryOffset     int
	TryLength     int
	HandlerOffset int
	HandlerLength int
	ClassToken    uint32 // Kind == HandlerCatch: the caught type's token
	FilterOffset  int    // Kind == HandlerFilter: where the filter expression starts
}

const (
	sectEHTable    = 0x01
	sectFatFormat  = 0x40
	sectMoreSects  = 0x80
	fatClauseSize  = 24
	smallClauseSz  = 12
	fatSectHdrLen  = 4
	smallSectHdrLn = 4
)

// ReadExceptionHandlers parses the "extra sections" following a
// fat-format method body's code (spec §II.25.4.5, small and fat clause
// formats both handled) — the EH clause table that `try`/`catch`/
// `finally`/`fault` compile down to. data is the same slice passed to
// ReadMethodBody; codeEnd is the byte offset (within data) where the code
// ends, i.e. 12+header.CodeSize for a fat header. Returns nil if the
// method has no MoreSections (tiny-format methods never do — the CLR
// verifier disallows EH clauses without a fat header).
func ReadExceptionHandlers(data []byte, header MethodHeader, codeEnd int) ([]ExceptionHandler, error) {
	if !header.MoreSections {
		return nil, nil
	}
	pos := (codeEnd + 3) &^ 3 // sections start 4-byte-aligned
	var out []ExceptionHandler
	for {
		if pos+fatSectHdrLen > len(data) {
			return nil, fmt.Errorf("il: truncated method data section header")
		}
		kind := data[pos]
		isFat := kind&sectFatFormat != 0
		more := kind&sectMoreSects != 0

		var dataSize int
		var clauseSize int
		if isFat {
			dataSize = int(data[pos+1]) | int(data[pos+2])<<8 | int(data[pos+3])<<16
			clauseSize = fatClauseSize
		} else {
			dataSize = int(data[pos+1])
			clauseSize = smallClauseSz
		}
		if dataSize < fatSectHdrLen || pos+dataSize > len(data) {
			return nil, fmt.Errorf("il: truncated method data section")
		}

		if kind&sectEHTable != 0 {
			clauses := data[pos+fatSectHdrLen : pos+dataSize]
			for i := 0; i+clauseSize <= len(clauses); i += clauseSize {
				h, err := parseExceptionClause(clauses[i:i+clauseSize], isFat)
				if err != nil {
					return nil, err
				}
				out = append(out, h)
			}
		}

		pos += dataSize
		if !more {
			break
		}
	}
	return out, nil
}

func parseExceptionClause(c []byte, isFat bool) (ExceptionHandler, error) {
	var flags uint32
	var h ExceptionHandler
	var classOrFilter uint32
	if isFat {
		flags = binary.LittleEndian.Uint32(c[0:4])
		h.TryOffset = int(binary.LittleEndian.Uint32(c[4:8]))
		h.TryLength = int(binary.LittleEndian.Uint32(c[8:12]))
		h.HandlerOffset = int(binary.LittleEndian.Uint32(c[12:16]))
		h.HandlerLength = int(binary.LittleEndian.Uint32(c[16:20]))
		classOrFilter = binary.LittleEndian.Uint32(c[20:24])
	} else {
		flags = uint32(binary.LittleEndian.Uint16(c[0:2]))
		h.TryOffset = int(binary.LittleEndian.Uint16(c[2:4]))
		h.TryLength = int(c[4])
		h.HandlerOffset = int(binary.LittleEndian.Uint16(c[5:7]))
		h.HandlerLength = int(c[7])
		classOrFilter = binary.LittleEndian.Uint32(c[8:12])
	}
	switch flags {
	case 0x0001:
		h.Kind = HandlerFilter
		h.FilterOffset = int(classOrFilter)
	case 0x0002:
		h.Kind = HandlerFinally
	case 0x0004:
		h.Kind = HandlerFault
	case 0x0000:
		h.Kind = HandlerCatch
		h.ClassToken = classOrFilter
	default:
		return ExceptionHandler{}, fmt.Errorf("il: unsupported exception clause flags %#x", flags)
	}
	return h, nil
}
