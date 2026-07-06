# system-text-json-demo

Parses a real JSON document through the real, unmodified `System.Text.Json`
8.0.5 NuGet package — no dotnet SDK installed, no compiled C# wrapper. It
drives `JsonDocument`'s real static `Parse` factory plus `JsonElement`'s
instance API directly from Go via `Assembly.Call`/`Instance.Call` (Fase
3.28), the same no-wrapper pattern `examples/jint-nowrapper` uses.

```bash
go run .
```

Expected output:

```txt
vmnet:true
```

## What this closed (Fase 3.41)

`JsonDocument.Parse(string, JsonDocumentOptions)` — real UTF-16→UTF-8
transcoding and byte-level marshaling — needed several real, general
interpreter fixes, not JSON-specific patches (see `docs/en/ROADMAP.md`):

- Overload resolution collapsed a closed generic argument
  (`ReadOnlyMemory<byte>` vs `ReadOnlyMemory<char>`) to its open name too
  early, so `Parse`'s two same-arity overloads looked identical and the tie
  broke toward the wrong one — feeding raw UTF-16 straight into the UTF-8
  reader with zero transcoding.
- No native existed for the pointer-taking `Encoding.GetByteCount`/
  `GetBytes` overloads the netstandard2.0 build of System.Text.Json (the
  one vmnet actually loads) uses — a real `fixed (char* p = span)`
  pointer-pinning shape, not the simpler `ReadOnlySpan<char>`-only overload
  a net8.0 build would take.
- `localloc` (`stackalloc T[n]`) had no IR instruction at all —
  `JsonReaderHelper`'s own scratch-buffer sizing stack-allocates this way.
- Real byte-level struct marshaling (`MemoryMarshal.Read<T>`/`Write<T>`,
  backed by `Unsafe.ReadUnaligned`/`WriteUnaligned`) had no implementation —
  `JsonDocument`'s own `MetadataDb` packs each parsed token as a 12-byte
  struct directly into a rented `byte[]`.

`JsonElement.GetBoolean()`'s result also isn't a Go `bool` at the call
site: vmnet has no distinct `bool` Value kind (every CIL i4-shaped value —
int, bool, char, an enum's underlying storage — is the same `KindI4`), so
`main.go` reads it back as `int32(0)`/`int32(1)` and compares explicitly.

See `docs/en/COMPATIBILITY.md` for the full measured/verified state across
every package this project tracks.
