# nuget-basic

Adds a real, published NuGet package (`SimpleBase@4.0.0`, zero
dependencies of its own) as a dependency, restores it for real from
nuget.org (resolving its own transitive dependencies too), and calls one
of its functions from Go via `vm.LoadPackage`. This is the Fase 3 demo —
see `docs/ROADMAP.md`.

Needs network access to nuget.org.

```bash
go run .
```

Expected output (the exact selected TFM/dependency versions may drift as
the package is updated upstream):

```txt
added SimpleBase@4.0.0 to vmnet.json
restored dependencies into vmnet.lock.json
  SimpleBase@4.0.0 -> lib/netstandard2.1/SimpleBase.dll
  System.Buffers@4.5.1 -> lib/netstandard2.0/System.Buffers.dll
  ...
SimpleBase.Base32.getAllocationByteCountForDecoding(0) = 0
SimpleBase.Base32.getAllocationByteCountForDecoding(1000) = 625
```

Running it writes `vmnet.json`, `vmnet.lock.json` and `.vmnet/packages/`
(the package cache) into this directory — the first two are meant to be
committed in a real project (see `docs/ROADMAP.md`), the cache is not
(`.gitignore`).
