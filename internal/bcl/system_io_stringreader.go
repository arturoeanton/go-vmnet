package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// System.IO.StringReader/System.IO.TextReader (Fase 3.44, general IL/BCL
// hardening pass — found via a real, load-bearing case: Newtonsoft.
// Json's own JObject.Parse(string) always goes through `new JsonTextReader
// (new StringReader(json))`, and TextReader-based parsing this way is a
// pervasive real-world pattern well beyond just this one caller). Reads
// by rune (not byte), matching real TextReader's char-granular contract;
// runes is pre-decoded once at construction rather than re-decoding the
// remaining string on every Read call.
type nativeStringReader struct {
	runes []rune
	pos   int
}

func init() {
	registerCtor("System.IO.StringReader", stringReaderCtor)
	register("System.IO.StringReader::Read", true, stringReaderRead)
	register("System.IO.StringReader::Peek", true, stringReaderPeek)
	register("System.IO.StringReader::ReadLine", true, stringReaderReadLine)
	register("System.IO.StringReader::ReadToEnd", true, stringReaderReadToEnd)
	register("System.IO.StringReader::Close", false, stringReaderNoop)
	register("System.IO.StringReader::Dispose", false, stringReaderNoop)
	// System.IO.TextReader is StringReader's real abstract base — a
	// callvirt compiled against the declared TextReader type (e.g. a
	// method parameter typed `TextReader reader`) reaches these same
	// natives via the virtual-dispatch ancestor walk (Machine.call,
	// internal/interpreter/calls.go) trying the receiver's concrete type
	// (StringReader) first; registering under TextReader too covers a
	// direct static-type call site with no such receiver to walk from.
	register("System.IO.TextReader::Read", true, stringReaderRead)
	register("System.IO.TextReader::Peek", true, stringReaderPeek)
	register("System.IO.TextReader::ReadLine", true, stringReaderReadLine)
	register("System.IO.TextReader::ReadToEnd", true, stringReaderReadToEnd)
	register("System.IO.TextReader::Close", false, stringReaderNoop)
	register("System.IO.TextReader::Dispose", false, stringReaderNoop)
}

func stringReaderCtor(args []runtime.Value) (*runtime.Object, error) {
	if len(args) < 1 || args[0].Kind != runtime.KindString {
		return nil, fmt.Errorf("bcl: StringReader constructor expects a string")
	}
	return &runtime.Object{Native: &nativeStringReader{runes: []rune(args[0].Str)}}, nil
}

func asStringReader(args []runtime.Value) (*nativeStringReader, error) {
	r, ok := nativeOf[*nativeStringReader](firstArg(args))
	if !ok {
		return nil, fmt.Errorf("bcl: StringReader method called on a non-StringReader receiver")
	}
	return r, nil
}

// stringReaderRead covers both Read() -> int (next char code point, or
// -1 at EOF) and Read(char[] buffer, int index, int count) -> int
// (number of chars actually read, 0 at EOF) — disambiguated by argument
// count like every other multi-overload native in this package.
func stringReaderRead(args []runtime.Value) (runtime.Value, error) {
	r, err := asStringReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if len(args) >= 4 && args[1].Kind == runtime.KindArray && args[1].Arr != nil {
		index, count := int(args[2].I4), int(args[3].I4)
		n := 0
		for n < count && r.pos < len(r.runes) {
			args[1].Arr.Elems[index+n] = runtime.Int32(r.runes[r.pos])
			r.pos++
			n++
		}
		return runtime.Int32(int32(n)), nil
	}
	if r.pos >= len(r.runes) {
		return runtime.Int32(-1), nil
	}
	c := r.runes[r.pos]
	r.pos++
	return runtime.Int32(c), nil
}

func stringReaderPeek(args []runtime.Value) (runtime.Value, error) {
	r, err := asStringReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if r.pos >= len(r.runes) {
		return runtime.Int32(-1), nil
	}
	return runtime.Int32(r.runes[r.pos]), nil
}

// stringReaderReadLine matches real TextReader.ReadLine() semantics:
// consumes up to (and past) the next \r, \n, or \r\n, returning the line
// WITHOUT the terminator, or null (not "") once nothing is left at all.
func stringReaderReadLine(args []runtime.Value) (runtime.Value, error) {
	r, err := asStringReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	if r.pos >= len(r.runes) {
		return runtime.Null(), nil
	}
	start := r.pos
	for r.pos < len(r.runes) {
		c := r.runes[r.pos]
		if c == '\n' {
			line := string(r.runes[start:r.pos])
			r.pos++
			return runtime.String(line), nil
		}
		if c == '\r' {
			line := string(r.runes[start:r.pos])
			r.pos++
			if r.pos < len(r.runes) && r.runes[r.pos] == '\n' {
				r.pos++
			}
			return runtime.String(line), nil
		}
		r.pos++
	}
	return runtime.String(string(r.runes[start:])), nil
}

func stringReaderReadToEnd(args []runtime.Value) (runtime.Value, error) {
	r, err := asStringReader(args)
	if err != nil {
		return runtime.Value{}, err
	}
	rest := string(r.runes[r.pos:])
	r.pos = len(r.runes)
	return runtime.String(rest), nil
}

func stringReaderNoop(args []runtime.Value) (runtime.Value, error) {
	return runtime.Value{}, nil
}
