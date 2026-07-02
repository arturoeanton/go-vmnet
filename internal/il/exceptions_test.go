package il

import "testing"

func TestReadExceptionHandlers_Small(t *testing.T) {
	// codeEnd=8 (not 4-byte aligned) -> section starts at 8.
	data := make([]byte, 8)
	section := []byte{
		0x01, 0x10, 0x00, 0x00, // small header: Kind=EHTable, DataSize=16
		0x02, 0x00, // Flags=Finally
		0x00, 0x00, // TryOffset=0
		0x05,       // TryLength=5
		0x05, 0x00, // HandlerOffset=5
		0x03,                   // HandlerLength=3
		0x00, 0x00, 0x00, 0x00, // ClassToken/FilterOffset=0
	}
	data = append(data, section...)

	got, err := ReadExceptionHandlers(data, MethodHeader{MoreSections: true}, 8)
	if err != nil {
		t.Fatalf("ReadExceptionHandlers() error = %v", err)
	}
	want := []ExceptionHandler{{Kind: HandlerFinally, TryOffset: 0, TryLength: 5, HandlerOffset: 5, HandlerLength: 3}}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("ReadExceptionHandlers() = %+v, want %+v", got, want)
	}
}

func TestReadExceptionHandlers_Fat(t *testing.T) {
	data := make([]byte, 4) // codeEnd=4, already aligned
	header := make([]byte, 4)
	header[0] = sectFatFormat | sectEHTable
	dataSize := fatSectHdrLen + fatClauseSize
	header[1] = byte(dataSize)
	header[2] = byte(dataSize >> 8)
	header[3] = byte(dataSize >> 16)

	clause := make([]byte, fatClauseSize)
	putU32 := func(b []byte, off int, v uint32) {
		b[off] = byte(v)
		b[off+1] = byte(v >> 8)
		b[off+2] = byte(v >> 16)
		b[off+3] = byte(v >> 24)
	}
	putU32(clause, 0, 0)           // Flags=Catch
	putU32(clause, 4, 10)          // TryOffset
	putU32(clause, 8, 20)          // TryLength
	putU32(clause, 12, 30)         // HandlerOffset
	putU32(clause, 16, 8)          // HandlerLength
	putU32(clause, 20, 0x02000005) // ClassToken

	data = append(data, header...)
	data = append(data, clause...)

	got, err := ReadExceptionHandlers(data, MethodHeader{MoreSections: true}, 4)
	if err != nil {
		t.Fatalf("ReadExceptionHandlers() error = %v", err)
	}
	want := ExceptionHandler{Kind: HandlerCatch, TryOffset: 10, TryLength: 20, HandlerOffset: 30, HandlerLength: 8, ClassToken: 0x02000005}
	if len(got) != 1 || got[0] != want {
		t.Errorf("ReadExceptionHandlers() = %+v, want %+v", got, want)
	}
}

func TestReadExceptionHandlers_NoMoreSections(t *testing.T) {
	got, err := ReadExceptionHandlers([]byte{0, 0, 0, 0}, MethodHeader{MoreSections: false}, 4)
	if err != nil || got != nil {
		t.Errorf("ReadExceptionHandlers() = %v, %v; want nil, nil", got, err)
	}
}
