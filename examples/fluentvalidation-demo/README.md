# fluentvalidation-demo

Runs the real, unmodified `FluentValidation` 11.9.2 NuGet package end to end:
a real validator built with the exact `RuleFor`/`NotEmpty`/`WithMessage` API
any real .NET app uses, both rejecting an invalid object with the right
error message and accepting a valid one.

`RuleFor(c => c.Name)` compiles to a real `System.Linq.Expressions` tree that
FluentValidation itself **compiles and invokes**
(`Expression<Func<T,TProperty>>.Compile()`) to read the actual property value
being validated — a genuinely deeper use of expression trees than
`examples/openxml-demo`'s own `DocumentFormat.OpenXml` dependency, which only
ever inspects a tree's shape, never compiles or runs it.

```bash
dotnet build FvDemoWrapper.csproj -c Release
go run .
```

Expected output:

```txt
Validate("Ada") = valid
Validate("") = invalid: Name is required
```

See `docs/en/ROADMAP.md`, Fase 3.64, for the interpreter work this needed
(walking and compiling a narrow class of real expression trees) and for a
known, real, separate limitation: FluentValidation's own numeric range
validators (`GreaterThanOrEqualTo`, etc.) hit a distinct, deeper generics
limitation (`Comparer<T>.Default`'s cached instance not being kept separate
per closed generic instantiation) not fixed by this Fase — this demo only
exercises the string validators that already work correctly.
