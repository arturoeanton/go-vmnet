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
ValidateAge(25) = valid
ValidateAge(10) = invalid: 'Age' must be greater than or equal to '18'.
```

See `docs/en/ROADMAP.md`, Fase 3.64, for the interpreter work the string
validators needed (walking and compiling a narrow class of real expression
trees). The `ValidateAge` calls exercise `GreaterThanOrEqualTo`, a numeric
range validator that used to fail: FluentValidation dispatches it through
`AbstractComparisonValidator<T,TProperty>`, a generic base class with two
same-named, same-arity `IsValid` overrides down its own hierarchy that only
differ by full signature — real .NET tells them apart by vtable slot, which
vmnet's own by-name ancestor walk used to conflate. Fixed in Fase 3.68 (see
`docs/en/ROADMAP.md`); Fase 3.64's original guess (a `Comparer<T>.Default`
caching issue) turned out to be wrong.
