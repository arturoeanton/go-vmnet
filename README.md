# vmnet

A pure-Go IL interpreter for embeddable C# plugins and selected NuGet
packages — run C# code inside a Go program without installing .NET.

```txt
Status: early development (Fase 0 — bootstrap). No commands work yet.
```

## Qué es y qué no es

`vmnet` **no** es .NET completo reimplementado en Go, y no promete correr
cualquier DLL .NET existente. Es un intérprete de un subconjunto soportado
de CIL (ECMA-335) más una Base Class Library parcial (`System.*`), pensado
para:

- Plugins C# embebidos en aplicaciones Go
- Reglas de negocio (pricing, validaciones, cálculo fiscal) escritas en C#
- Migración incremental .NET → Go
- Reuso de paquetes NuGet puros (sin P/Invoke, sin reflection pesada, sin
  ASP.NET/EF Core/WPF)

Antes de cargar un assembly de terceros, `vmnet check` (Fase 3) va a decir
explícitamente qué corre y qué no — en vez de fallar a mitad de ejecución.

Ver la especificación técnica completa en [`docs/spec.md`](docs/spec.md).

## Plan de entrega

El proyecto se construye en 4 fases, cada una cerrando con una demo
concreta: núcleo IL funcional → motor de reglas de negocio → checker de
compatibilidad + NuGet → v1.0 lista para producción. Ver
[`docs/ROADMAP.md`](docs/ROADMAP.md) para el detalle completo de tareas y
criterios de aceptación por fase.

## Arquitectura

Ver [`docs/architecture.md`](docs/architecture.md) para el pipeline
PE → metadata → IL → IR → interpreter → runtime → BCL y el layout de
paquetes.

## Desarrollo

```bash
go build ./...
go vet ./...
go test ./...
```

Los tests de integración cargan DLLs C# reales compiladas desde
`tests/fixtures/csharp`. El SDK de .NET es una dependencia **solo de
desarrollo**, para generar esos DLLs — nunca del runtime de `vmnet`:

```bash
dotnet build tests/fixtures/csharp/Fixtures.csproj -c Release
```

Ver [`CONTRIBUTING.md`](CONTRIBUTING.md) antes de mandar un PR.

## Licencia

Apache License 2.0 — ver [`LICENSE`](LICENSE).
