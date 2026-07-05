# permissions-demo

Shows vmnet's deny-by-default `Permissions` capability gate (Fase 3.59,
`docs/en/security.md`) end to end against real disk I/O. The exact same
compiled C# code (`Vmnet.Fixtures.FileIO`, reused from vmnet's own test
fixtures) behaves completely differently across three runs, purely based on
how the embedding Go program configures `vm.Permissions()` before loading
it — nothing in the C# source changes.

```bash
dotnet build ../../tests/fixtures/csharp/Fixtures.csproj -c Release
go run .
```

Expected output (abridged):

```txt
--- 1. A fresh VM denies real file I/O by default ---
File.WriteAllText/ReadAllText with no Permissions granted: vmnet: ... System.UnauthorizedAccessException: ...
The C# code itself can catch it too: File.ReadAllText -> caught "DENIED"
Confirmed: no file exists on disk at all — the gate ran before any real syscall.

--- 2. Granting AllowFileRead/AllowFileWrite makes the exact same C# code do real I/O ---
File.WriteAllText/ReadAllText, now granted -> "top secret"
Independently re-read from Go (not through vmnet at all): "top secret" — a real file, not an illusion.

--- 3. Granting only AllowFileRead still denies a write ---
File.WriteAllText with only AllowFileRead granted: vmnet: ... System.UnauthorizedAccessException: ...
```

Every capability starts denied (the zero `Permissions` value) — see
`permissions.go` at the repo root and `docs/en/security.md` for the full
gated native list (`System.IO.File`/`Directory`/`FileStream`/`FileInfo`/
`DirectoryInfo`, plus two retrofitted pre-existing natives that did real,
ungated file I/O before this Fase: `System.IO.Path.GetTempFileName` and
opening a real `Microsoft.Data.Sqlite.SqliteConnection`, see
`examples/sqlite-demo`).
