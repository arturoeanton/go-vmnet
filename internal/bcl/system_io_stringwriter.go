package bcl

import (
	"fmt"
	"strings"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// System.IO.StringWriter/System.IO.TextWriter — the write-side counterpart
// of system_io_stringreader.go's StringReader/TextReader (that file's own
// doc comment is the closer template this one follows). Found via a
// corpus-wide static-checker scan across the 19 tracked NuGet packages:
// TextWriter.Write alone is the single highest real-call-site count of
// anything the scan turned up (218 sites across CsvHelper, Serilog,
// YamlDotNet and three others), yet neither type existed here at all.
//
// Real call shapes confirmed by disassembling the actual real DLLs (not
// guessed): YamlDotNet.Serialization.Serializer.Serialize does `using (var
// writer = new StringWriter()) { Serialize(writer, ...); return writer.
// ToString(); }` — parameterless ctor, then a receiver-typed ToString()
// bound directly to StringWriter's own override, then Dispose() in the
// generated `finally`. CsvHelper.CsvWriter.FlushBuffer calls Write(char[]
// buffer, int index, int count) on its constructor-injected TextWriter.
// Serilog.Rendering.ReusableStringWriter (a real StringWriter subclass,
// its own thread-local reusable-writer pool) chains `: base(formatProvider
// ?? CultureInfo.CurrentCulture)` to StringWriter's own (IFormatProvider)
// overload — the base-ctor-chaining shape baseExceptionCtorInPlace
// (system_exception.go) / dictCtorInPlace (system_collections.go) already
// handle for Exception/Dictionary`2 — and separately reads back
// TextWriter.FormatProvider/Encoding.
//
// Scope, matching what the scan's real call sites actually use and no
// more: Write/WriteLine's string/char/object/numeric and char[]([,int,
// int]) overloads, ToString, Flush/Dispose/Close (no-ops — see
// textWriterNoop), NewLine, FormatProvider, Encoding. Deliberately NOT
// covered: WriteAsync/FlushAsync/DisposeAsync (CsvHelper's own async
// extension methods call these, but this project models async
// synchronously throughout — internal/interpreter/async.go — so the
// handful of real async TextWriter call sites found were left as a
// separate, still-open gap rather than folded in here) and a StringWriter
// ctor overload seeded from a caller-supplied StringBuilder (no real
// corpus call site constructs one that way; see newStringWriterNative's
// own doc comment for how that overload still degrades gracefully rather
// than erroring outright).
type nativeStringWriter struct {
	buf strings.Builder
	// formatProvider is whatever the ctor was given (real .NET: almost
	// always a CultureInfo), defaulting to CurrentCulture like real
	// StringWriter()'s own parameterless overload. Round-tripped verbatim
	// through get_FormatProvider — vmnet has no locale-aware formatting to
	// actually apply to it (same posture CultureInfo's own stub already
	// takes, system_misc.go's cultureInfoInvariant), so nothing here ever
	// inspects its contents beyond handing it back to whoever asks.
	formatProvider runtime.Value
}

func init() {
	registerCtor("System.IO.StringWriter", stringWriterCtor)
	// StringWriter::.ctor as a plain (non-newobj) call: the shape a real
	// subclass's own constructor uses to chain `: base(...)` — see
	// baseExceptionCtorInPlace's doc comment (system_exception.go) for why
	// this is a second, separate registration from registerCtor above
	// rather than the same one firing twice. Confirmed load-bearing by a
	// real case, not a hypothetical: Serilog.Rendering.ReusableStringWriter
	// extends StringWriter and does exactly this.
	register("System.IO.StringWriter::.ctor", false, stringWriterCtorInPlace)
	register("System.IO.StringWriter::Write", false, textWriterWrite)
	register("System.IO.StringWriter::WriteLine", false, textWriterWriteLine)
	register("System.IO.StringWriter::ToString", true, textWriterToString)
	register("System.IO.StringWriter::Flush", false, textWriterNoop)
	register("System.IO.StringWriter::Close", false, textWriterNoop)
	register("System.IO.StringWriter::Dispose", false, textWriterNoop)
	register("System.IO.StringWriter::get_NewLine", true, textWriterGetNewLine)
	register("System.IO.StringWriter::get_FormatProvider", true, textWriterGetFormatProvider)
	register("System.IO.StringWriter::get_Encoding", true, textWriterGetEncoding)
	// System.IO.TextWriter is StringWriter's real abstract base — see
	// system_io_stringreader.go's identical StringReader/TextReader
	// registrations for why a callvirt compiled against the declared
	// TextWriter type needs its own registration too, beyond what virtual
	// dispatch off the receiver's concrete StringWriter type already
	// reaches (Machine.call's ancestor walk, internal/interpreter/
	// calls.go): a call site with no such receiver to walk from (a plain
	// `TextWriter w = ...; w.Write(x);` where the compiler bound directly
	// to the base MemberRef) needs the name registered under TextWriter
	// itself too. CsvHelper's constructor-injected `TextWriter writer`
	// field is exactly this: every Write/Flush/Dispose call on it resolves
	// through these, not the StringWriter-declared entries above.
	register("System.IO.TextWriter::Write", false, textWriterWrite)
	register("System.IO.TextWriter::WriteLine", false, textWriterWriteLine)
	register("System.IO.TextWriter::ToString", true, textWriterToString)
	register("System.IO.TextWriter::Flush", false, textWriterNoop)
	register("System.IO.TextWriter::Close", false, textWriterNoop)
	register("System.IO.TextWriter::Dispose", false, textWriterNoop)
	register("System.IO.TextWriter::get_NewLine", true, textWriterGetNewLine)
	register("System.IO.TextWriter::get_FormatProvider", true, textWriterGetFormatProvider)
	register("System.IO.TextWriter::get_Encoding", true, textWriterGetEncoding)
}

// newStringWriterNative builds the native backing shared by both
// registerCtor's newobj path (constructing a StringWriter directly) and
// stringWriterCtorInPlace's base-chaining path (args here already
// excludes the receiver in that case — see both call sites). Covers every
// real StringWriter ctor overload's argument SHAPE by scanning for it
// rather than trusting a fixed position, same technique innerExceptionArg
// (system_exception.go) uses to tell an exception ctor's (message,
// paramName) apart from (message, innerException):
//
//   - StringWriter() — args empty, both fields stay at their defaults.
//   - StringWriter(IFormatProvider) — the one KindObject argument that
//     isn't a StringBuilder.
//   - StringWriter(StringBuilder[, IFormatProvider]) — no real corpus call
//     site found using this overload (every one seen constructs a fresh,
//     private buffer), so rather than the real live-aliasing semantics
//     (later Write calls on the StringWriter would need to append into the
//     SAME external StringBuilder, which nativeStringBuilder's own
//     string-rebuilding representation can't share efficiently with
//     strings.Builder here), this seeds the initial text as a one-time
//     copy: correct for the common "pre-seed then write" pattern, just not
//     for a caller that keeps reading the original StringBuilder
//     afterward and expects to see the StringWriter's later writes too.
func newStringWriterNative(args []runtime.Value) *nativeStringWriter {
	w := &nativeStringWriter{}
	w.formatProvider, _ = cultureInfoInvariant(nil)
	for _, a := range args {
		if a.Kind != runtime.KindObject || a.Obj == nil {
			continue
		}
		if sb, ok := a.Obj.Native.(*nativeStringBuilder); ok {
			w.buf.WriteString(sb.buf)
			continue
		}
		w.formatProvider = a
	}
	return w
}

func stringWriterCtor(args []runtime.Value) (*runtime.Object, error) {
	return &runtime.Object{Native: newStringWriterNative(args)}, nil
}

func stringWriterCtorInPlace(args []runtime.Value) (runtime.Value, error) {
	if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return runtime.Value{}, fmt.Errorf("bcl: StringWriter constructor called without a receiver")
	}
	args[0].Obj.Native = newStringWriterNative(args[1:])
	return runtime.Value{}, nil
}

func asTextWriter(args []runtime.Value) (*nativeStringWriter, error) {
	w, ok := nativeOf[*nativeStringWriter](firstArg(args))
	if !ok {
		return nil, fmt.Errorf("bcl: TextWriter method called on a non-StringWriter receiver")
	}
	return w, nil
}

func textWriterWrite(args []runtime.Value) (runtime.Value, error) {
	w, err := asTextWriter(args)
	if err != nil {
		return runtime.Value{}, err
	}
	appendTextWriterArgs(w, args[1:])
	return runtime.Value{}, nil
}

// textWriterWriteLine matches real WriteLine semantics: whatever Write
// would have produced for the same arguments, then one NewLine. "\n", not
// "\r\n" — same reasoning environmentNewLine (system_misc.go) and
// StringBuilder.AppendLine already give: vmnet has no host-OS concept to
// match a real platform-dependent Environment.NewLine against, and "\n"
// is what every other vmnet-produced multi-line string already uses.
func textWriterWriteLine(args []runtime.Value) (runtime.Value, error) {
	w, err := asTextWriter(args)
	if err != nil {
		return runtime.Value{}, err
	}
	appendTextWriterArgs(w, args[1:])
	w.buf.WriteString("\n")
	return runtime.Value{}, nil
}

// appendTextWriterArgs covers every Write/WriteLine overload this loop's
// real target packages (CsvHelper, Serilog, YamlDotNet) actually call —
// confirmed by disassembling their real DLLs rather than guessed. args
// excludes the receiver.
func appendTextWriterArgs(w *nativeStringWriter, args []runtime.Value) {
	if len(args) == 0 {
		// The real parameterless overload — WriteLine() is just the
		// newline (added by the caller above); Write() is a no-op.
		return
	}
	if args[0].Kind == runtime.KindArray {
		// Write(char[] buffer, int index, int count) — CsvHelper.CsvWriter.
		// FlushBuffer's own real call shape, flushing its internal field
		// buffer directly rather than building an intermediate string
		// first. Write(char[] buffer) (no index/count) is the same thing
		// over the whole array.
		if args[0].Arr == nil {
			return
		}
		elems := args[0].Arr.Elems
		start, end := 0, len(elems)
		if len(args) >= 3 {
			start = int(args[1].I4)
			end = start + int(args[2].I4)
			if end > len(elems) {
				end = len(elems)
			}
		}
		for i := start; i < end; i++ {
			w.buf.WriteRune(rune(elems[i].I4))
		}
		return
	}
	// Write(string)/Write(object) (its own ToString(), via displayString —
	// the same fallback System.Console.Write and StringBuilder.Append
	// already share)/Write(bool|int|long|float|double|...) (numeric
	// literal rendering). Write(char) arrives here already converted to a
	// single-rune KindString by calls.go's charSensitiveNatives entry for
	// this exact native (added alongside this file — see that map's own
	// doc comment), not as the numeric code point StringBuilder.Append
	// stringifies an unconverted char argument as.
	w.buf.WriteString(displayString(args[0]))
}

func textWriterToString(args []runtime.Value) (runtime.Value, error) {
	w, err := asTextWriter(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.String(w.buf.String()), nil
}

// textWriterNoop backs Flush/Close/Dispose: real StringWriter.Flush() has
// nothing to flush (the "stream" already IS the in-memory buffer, no
// separate OS-level write to force out), and Dispose/Close on any
// TextWriter has nothing to release in a pure-Go interpreter with no
// unmanaged handles — the same documented posture System.IDisposable::
// Dispose already takes project-wide (disposeNoop, system_collections.go).
// Real .NET marks the writer closed and would throw ObjectDisposedException
// on a further Write after this; no real call site in this loop's target
// packages writes after disposing, so that edge case is left unmodeled
// rather than adding state nothing here exercises.
func textWriterNoop(args []runtime.Value) (runtime.Value, error) {
	return runtime.Value{}, nil
}

func textWriterGetNewLine(args []runtime.Value) (runtime.Value, error) {
	return runtime.String("\n"), nil
}

func textWriterGetFormatProvider(args []runtime.Value) (runtime.Value, error) {
	w, err := asTextWriter(args)
	if err != nil {
		return runtime.Value{}, err
	}
	return w.formatProvider, nil
}

// textWriterGetEncoding matches real StringWriter.Encoding's own actual
// default (`new UnicodeEncoding(false, false)`, i.e. UTF-16 little-endian
// with no byte-order mark) — a StringWriter writes into an in-memory
// System.String/StringBuilder, never a real byte stream, and .NET strings
// are UTF-16 internally, so this is what a real caller reading .Encoding
// back actually sees, not an arbitrary placeholder. encodingGetUnicodeLE
// (system_text.go) already builds exactly this Encoding shape for System.
// Text.Encoding.Unicode itself; reused verbatim rather than duplicating it.
func textWriterGetEncoding(args []runtime.Value) (runtime.Value, error) {
	return encodingGetUnicodeLE(nil)
}
