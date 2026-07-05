# di-demo

Runs the real, unmodified `Microsoft.Extensions.DependencyInjection` 8.0.0 NuGet
package — Microsoft's own official dependency-injection container, the one
every ASP.NET Core and worker-service `Program.cs` builds on — inside vmnet,
with no .NET runtime installed.

`DiDemoWrapper.cs` registers two real services (`IClock`/`FixedClock`,
`IGreeter`/`Greeter`) and resolves `IGreeter` through the container. `Greeter`'s
own constructor takes an `IClock` — the container discovers and supplies it on
its own via real constructor injection, not a trivial parameterless-type
special case.

```bash
dotnet build DiDemoWrapper.csproj -c Release
go run .
```

Expected output:

```txt
Program.Run("vmnet") = Hello, vmnet! (at 2026-01-01T00:00:00Z)
```

Getting a real DI container this far required three real interpreter fixes
(Fase 3.60, see `docs/en/ROADMAP.md`):

- A method-overload-resolution tie-break that mis-picked `ServiceDescriptor`'s
  own private constructor, causing an infinite self-recursion.
- `typeof(T)` never resolving on a generic method's own still-open type
  parameter — `AddSingleton<TService, TImplementation>()`'s real body does
  exactly this.
- Reflection over a service implementation type (`Greeter`) declared in the
  wrapper assembly, reached from code running *inside* the framework assembly
  with no declared dependency edge back to it at all.

See `examples/permissions-demo` for vmnet's deny-by-default capability gate,
and `docs/en/COMPATIBILITY.md` for the current measured/verified state across
every package this project tracks.
