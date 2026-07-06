# closedxml-demo

Reads a real `.xlsx` file (OOXML/zip binary format) through the real,
unmodified `ClosedXML` 0.105.0 NuGet package — no dotnet SDK installed at
runtime, no third-party `.xlsx` parser built for vmnet specifically. A tiny
compiled C# wrapper, `GraphicEngineWrapper.dll` (built from
`GraphicEngineWrapper.cs` in this directory), supplies a minimal
`IXLGraphicEngine` implementation and constructs the real `XLWorkbook` —
needed only because ClosedXML's own `DefaultGraphicEngine` depends on
`SixLabors.Fonts`/`System.Memory` internals that hit a real, deep vmnet
limitation unrelated to reading cell data at all (generic type-parameter
substitution for `typeof(T)` inside a generic class's own static field
initializers — Fase 3.40, `docs/en/ROADMAP.md`). Once the workbook is
constructed, its own instance API is driven directly from Go via
`Assembly.New`/`Instance.Call` (Fase 3.28), the same no-wrapper pattern
`examples/jint-nowrapper` uses for everything past that one construction
step.

```bash
dotnet build GraphicEngineWrapper.csproj -c Release
go run .
```

Expected output:

```txt
Sheet has 5 rows, 3 columns

Product	Units	Price	
Widget	12	9.99	
Gadget	7	19.5	
Gizmo	25	3.25	
Total	=SUM(B2:B4)		
```

## Why a compiled wrapper here, but not in openxml-demo/npoi-demo

`IXLGraphicEngine` is a real interface — satisfying it needs a concrete
backing type with real methods, which vmnet has no way to fabricate purely
from Go (see `examples/npoi-demo` and `examples/openxml-demo` for the same
package family read/written with no wrapper at all, because neither one's
own path touches an interface vmnet would have to implement). The wrapper's
`NullGraphicEngine` is a minimal, real implementation that sidesteps the
font-metrics limitation entirely: real cell *values* never depend on font
metrics, only auto-column-width calculation does. `WorkbookOpener.
OpenWorkbook` sets it once as ClosedXML's process-wide `LoadOptions.
DefaultGraphicEngine` before `new XLWorkbook(stream)` runs, then hands the
constructed workbook back to Go.

## Fase 3.40: the longest bug chain in the project, plus a later hang fix

Getting `new XLWorkbook(stream)` to open a real `.xlsx` needed dozens of
real, general interpreter fixes — not ClosedXML-specific patches — the
single largest single-phase entry in `docs/en/ROADMAP.md`. A later phase
(Fase 3.44) also found and fixed a real non-deterministic hang: `go run .`
intermittently hung because `internal/metadata.Metadata.FindTypeDef` did an
uncached, full linear scan of the TypeDef table on every single call,
decoding a fresh Go string off the string heap each time — a cost that
compounded multiplicatively with the real, data-dependent recursion depth
ClosedXML's own DOM-loading code takes. `ClosedXML@0.105.0` currently
clears 97.5% of its analyzed methods (10,444 analyzed, 266 flagged) under
`vmnet check` — see `docs/en/COMPATIBILITY.md` for the full measured/
verified state across every package this project tracks.

## Regenerating `testdata/sample.xlsx`

Dev-only (needs the real .NET SDK) — never a dependency of the demo itself,
which only needs the two commands above:

```bash
dotnet run --project generate
cp generate/sample.xlsx testdata/
```
