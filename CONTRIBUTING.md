# Contribuir a vmnet

## Antes de nada

Lee `docs/es/spec.md` (qué construimos y por qué) y `docs/es/ROADMAP.md` (en qué
fase estamos y qué tareas quedan). Un PR que agrega algo fuera del alcance
de la fase activa probablemente deba esperar — ver "No objetivos" en la
spec (§3) antes de proponer soporte para reflection, async, P/Invoke, etc.

## Reglas duras

- **Sin cgo en el núcleo.** `go build`/`go vet`/`go test` deben pasar con
  `CGO_ENABLED=0`. Esto es un compromiso de producto (ADR 0001), no solo un
  flag de CI.
- **`internal/` es internal.** Los paquetes bajo `internal/pe`,
  `internal/metadata`, etc. no son API pública (ADR 0002). Cualquier cosa
  que el usuario final deba poder llamar se expone deliberadamente desde el
  paquete raíz `vmnet`.
- **`vmnet check` antes de prometer compatibilidad.** Si agregás soporte
  para un opcode o método BCL nuevo, el analyzer de `internal/checker` tiene
  que dejar de marcarlo como unsupported — no alcanza con que el intérprete
  no crashee.

## Build y test

```bash
go build ./...
go vet ./...
go test ./...
```

## Fixtures C#

Los tests de PE/metadata/IL/interpreter usan DLLs reales compiladas desde
`tests/fixtures/csharp`. Necesitás el SDK de .NET instalado **solo para
esto** — nunca para correr `vmnet` en sí:

```bash
dotnet build tests/fixtures/csharp/Fixtures.csproj -c Release
```

Si agregás un caso nuevo (un opcode, un patrón de clase, una llamada BCL),
agregá primero la fixture C# correspondiente en
`tests/fixtures/csharp/README.md` con una fila describiendo qué ejercita.

## Commits y PRs

- Un PR por tarea de `docs/es/ROADMAP.md` cuando sea posible; referenciá la
  fase y el módulo en la descripción (ej. "Fase 1 · /pe: RVA→offset").
- Agregá tests junto con el código, no en un PR aparte.
- Si el PR introduce una decisión de arquitectura no trivial, documentala
  como ADR en `docs/es/adr/`.
