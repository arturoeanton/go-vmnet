# jint-demo

Runs real JavaScript inside a Go process, no CGo, no subprocess: loads
the real, unmodified [Jint](https://github.com/sebastienros/jint) 3.1.3
NuGet package (a full C# JavaScript engine) plus its whole transitive
dependency chain (Esprima, System.Memory, System.Buffers,
System.Numerics.Vectors, System.Runtime.CompilerServices.Unsafe) via
`vm.LoadPackage`, then calls into a small compiled C# wrapper
(`JintWrapper.cs` in this directory) that drives Jint's own
`Engine.Evaluate`/`Engine.SetValue` API directly.

This is the Fase 3.27 demo — see `docs/ROADMAP.md` for the architecture
work it needed (multi-assembly resolution across a real NuGet dependency
graph, real virtual dispatch across inheritance chains, generic-method
and struct-vs-class overload disambiguation, all found and fixed by
running this exact package).

Needs network access to nuget.org (to restore Jint) and a local `dotnet`
SDK (to compile the wrapper — vmnet only runs already-compiled IL, it
doesn't compile C#).

```bash
dotnet build -c Release
go run .
```

Expected output:

```txt
RunJs("1 + 2") = 3
AddNumbers(3, 4) = 7
```

`JintWrapper.cs`/`JintWrapper.csproj` are committed (they're the actual
source being demonstrated); `bin/`, `obj/`, and the `dotnet restore`/
`vm.NuGet().Restore()` caches they produce are not.

See `examples/jint-nowrapper` for the same demo driven directly from Go
(`Assembly.New`/`Instance.Call`, Fase 3.28) with no C# wrapper and no
`dotnet` SDK dependency at all. A compiled wrapper like this one is still
the better choice when the real C# API leans on compile-time-only sugar
(optional parameters, extension methods, implicit conversions) that
`Instance.Call` can't reconstruct — see that example's README for the
concrete cases Jint hits.
