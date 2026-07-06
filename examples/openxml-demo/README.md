# openxml-demo

Generates a real `.docx` file (OOXML/zip binary format) through the real,
unmodified `DocumentFormat.OpenXml` 3.1.1 NuGet package — no dotnet SDK
installed at runtime, no third-party `.docx` writer built for vmnet. It
drives `WordprocessingDocument`'s real static `Create` factory plus the
`Document`/`Body`/`Paragraph`/`Run`/`Text` element tree directly from Go via
`Assembly.Call`/`Assembly.New`/`Instance.Call` (Fase 3.28) — the same
no-wrapper pattern `examples/jint-nowrapper` and `examples/npoi-demo` use.
Unlike `examples/closedxml-demo`, generating a document never touches
ClosedXML's `IXLGraphicEngine` abstraction, so no compiled C# wrapper is
needed here at all.

```bash
go run .
```

Expected output:

```txt
wrote report.docx (907 bytes)
```

## Verified against the real .NET SDK, not just vmnet's own reader (Fase 3.42)

During development, the resulting `report.docx` was opened back with the
real, unmodified .NET OpenXml SDK — not merely round-tripped through
vmnet's own lenient `XmlReader` — and confirmed to contain the correct
paragraph text (`docs/en/ROADMAP.md`). That check caught a real,
load-bearing bug this phase fixed: `XmlWriter`'s namespace-URI arguments
were silently dropped, so every OOXML part vmnet generated — including
`[Content_Types].xml`'s own required default namespace — came out with no
namespace at all. vmnet's own reader never noticed (it doesn't check
namespaces), but the real .NET SDK rejected the file outright
(`"Required Types tag not found"`) until `WriteStartElement`'s
namespace-carrying overloads actually started emitting the `xmlns`/
`xmlns:prefix` declaration.

Getting here also needed a real `System.Linq.Expressions` subset and
`ldtoken`-on-a-method support: every OpenXml element's own
`ConfigureMetadata` registers each real attribute via
`Expression<Func<TElement,TValue>>` (`a => a.Space`), a pattern the real
SDK repeats roughly 1,859 times. The only real consumer just pattern-matches
`expression.Body is MemberExpression` and reads `.Member.Name`, so vmnet
only needed enough shape for that one inspection, not a fully walkable
expression-tree compiler.

See `docs/en/COMPATIBILITY.md` for the full measured/verified state across
every package this project tracks — `DocumentFormat.OpenXml@3.1.1` currently
clears 100.0% of its analyzed methods (67,234 analyzed, 7 flagged), the
highest of any package tracked.
