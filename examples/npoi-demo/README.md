# npoi-demo

Reads a real legacy `.xls` file (OLE2/CFBF binary format, not the zip-based `.xlsx`) through the
real, unmodified NPOI 2.8.0 NuGet package, via `Assembly.New`/`Instance.Call` — no compiled C#
wrapper, no dotnet SDK needed at runtime, same no-wrapper pattern as
[`examples/jint-nowrapper`](../jint-nowrapper).

```bash
go run .
```

Opens `testdata/sample.xls`, constructs a real `HSSFWorkbook`, and prints its real cell data —
strings, numbers, and a `SUM` formula cell — read straight out of the binary BIFF8 format.

## Getting here found 18 real, general interpreter/overload bugs

Not NPOI-specific workarounds — see `docs/en/ROADMAP.md` Fase 3.39 for the full writeup of each.
The two most notable:

- **`Dictionary`/`Hashtable` enumeration was non-deterministic.** `nativeDict` had no memory of
  insertion order, so every `GetEnumerator`/`.Values`/`.Keys` call got Go's own intentionally-
  randomized map iteration order. NPOI's `FileMagicContainer.ValueOf` relies on real .NET's
  Dictionary enumerating in insertion order to check `OLE2` before its own `UNKNOWN` catch-all —
  without that, opening this exact file threw `NotOLE2FileException` on roughly half of all runs.
- **Overload resolution couldn't recognize interface implementation**, only class hierarchy —
  `AreaPtg(ILittleEndianInput)` vs `AreaPtg(AreaReference)`, a same-arity constructor pair, picked
  the wrong one for a stream argument, silently constructing a broken object.

**Known remaining limitation**: the `SUM(...)` formula's cell-range text renders column letters as
their numeric code points (e.g. `SUM(662:664)` instead of `SUM(B2:B4)`) — vmnet has no distinct
`char` value kind (chars are stored as plain `int32`, and IL's `conv.u2` for a `(char)` cast is
bytecode-identical to a `ushort` conversion), so `StringBuilder.Insert`/`Append` can't tell a
`char` argument from a plain integer. A real fix needs a new `KindChar`, an architecture change
out of scope for this phase. Cell values themselves are unaffected — only formula-text rendering.

## Regenerating `testdata/sample.xls`

Dev-only (needs the real .NET SDK + network access to nuget.org) — never a dependency of the demo
itself, which only needs `go run .`:

```bash
dotnet run --project generate
cp generate/sample.xls testdata/
```
