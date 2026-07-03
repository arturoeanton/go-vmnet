# npoi-demo (work in progress)

Reads a real legacy `.xls` file (OLE2/CFBF binary format, not the zip-based `.xlsx`) through the
real, unmodified NPOI 2.8.0 NuGet package, via `Assembly.New`/`Instance.Call` — no compiled C#
wrapper, no dotnet SDK needed at runtime, same no-wrapper pattern as
[`examples/jint-nowrapper`](../jint-nowrapper).

```bash
go run .
```

## Current status: blocked on a real POIFS/OLE2 parsing bug, not yet passing

Constructing `new HSSFWorkbook(stream)` against `testdata/sample.xls` currently throws
`NPOI.POIFS.FileSystem.NotOLE2FileException` — NPOI's own file-magic detector
(`FileMagicContainer.ValueOf`) is misclassifying a genuinely well-formed OLE2 file (confirmed via
`file`/`xxd`: starts with the correct `D0 CF 11 E0 A1 B1 1A E1` signature, written by the real NPOI
2.8.0 `HSSFWorkbook.Write()` — see `generate/`).

Getting this far already found and fixed several real, general interpreter bugs (not NPOI-specific
workarounds — see `docs/en/ROADMAP.md` Fase 3.39 for the full writeup):

- `assembly.go`'s RVA-backed-field reader only recognized a custom `ClassLayout`-sized struct
  field, not a plain `int`/`long`-typed field relying on its own natural size (the shape a short
  ≤8-byte array literal compiles to) — `NPOI.POIFS.Common.POIFSConstants.OOXML_FILE_HEADER`
  (a 4-byte array literal) hit exactly this.
- Shift operations (`shl`/`shr`) require same-Kind operands in vmnet's binary-op evaluator, but
  real ECMA-335 semantics allow the shift amount to be `int32` regardless of the shifted value's
  own width — POIFS's own block-offset arithmetic shifts a `long` by a plain `int` bit count with
  no `conv.i8` in between.
- Overload resolution couldn't recognize a native-backed value (no `TypeDef`, e.g.
  `System.IO.MemoryStream`) as assignable to a real BCL base type (`System.IO.Stream`) — a
  same-arity constructor set over unrelated types (`FileInfo`/`FileStream`/`Stream`) tied and
  silently picked the wrong (file-based) one by declaration order.
- `Dictionary<K,V>` only supported string keys — `NPOI.SS.Formula.Eval.ErrorEval` keys one by
  `FormulaError`'s own static singleton instances (object identity), a real "smart enum" pattern.
- `System.Text.Encoding.GetString`/`GetBytes` only accepted the `CallBytes`/`CallJSON`-only
  `KindBytes` shape, not a real interpreted `byte[]` (`KindArray`) — the shape real code (including
  NPOI's own internal string decoding) actually produces and consumes.

**The remaining, still-open bug** is somewhere in `NPOI.Util.IOUtils.PeekFirstNBytes`'s own
stream-peeking machinery — a straight port of a Java `InputStream.mark()/reset()` pattern layered
through `NPOI.Util.ByteArrayInputStream`/`BoundedInputStream`/`ByteArrayOutputStream` and
`IOUtils.Copy`, none of which is `System.IO`-native code at all (it's NPOI's own C#, interpreted
like anything else) — meaning the bug is most likely in vmnet's execution of *that* chain, not
`System.IO.MemoryStream` itself (which was independently verified correct via a direct probe:
constructing one, writing to it, seeking, and reading back the exact bytes written all worked).
Not yet isolated to a single root cause.

## Why this is committed anyway

The fixture (`testdata/sample.xls`, written by real NPOI via `generate/`) and the demo's own
`main.go` are real, reviewable progress even with the underlying bug still open — per this
project's own stated principle (`docs/en/ROADMAP.md`), a real gap gets documented explicitly, not
left as a silent TODO or hidden until it's fully solved. This example is **not** yet linked from
the top-level `README.md`/`README.es.md` example table — it'll move there once
`new HSSFWorkbook(stream)` succeeds and real cell data comes back out.

## Regenerating `testdata/sample.xls`

Dev-only (needs the real .NET SDK + network access to nuget.org) — never a dependency of the demo
itself, which only needs `go run .`:

```bash
dotnet run --project generate
cp generate/sample.xls testdata/
```
