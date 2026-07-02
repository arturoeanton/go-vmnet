# Arquitectura

Ver `docs/spec.md` (especificación completa) y `docs/ROADMAP.md` (plan de
entrega en 4 fases). Este documento es el mapa rápido de "qué vive dónde" en
el repo tal como está implementado hoy — se amplía a medida que cada fase
agrega comportamiento real.

## Pipeline (spec §8)

```txt
.dll (PE/CLI)
  → internal/pe          lee headers PE/COFF, ubica CLI header + metadata root
  → internal/metadata    parsea streams (#~ #Strings #US #Blob #GUID) y tablas
  → internal/il          decodifica method bodies IL a instrucciones tipadas
  → internal/ir          normaliza IL a la IR propia de vmnet
  → internal/interpreter evalúa la IR sobre un frame/stack, con límites
  → internal/runtime     modelo de objetos managed (Type/Method/Field/Heap)
  → internal/bcl         System.* implementado nativamente en Go
```

`internal/nuget` y `internal/checker` son transversales: el primero resuelve
paquetes `.nupkg` hacia assemblies que entran por el mismo pipeline; el
segundo analiza metadata/IR antes de ejecutar para reportar compatibilidad
(spec §23).

## Layout de paquetes

```txt
/                     package vmnet — API pública (spec §6)
/internal/pe          PE/CLI loader (spec §9)
/internal/metadata    metadata loader + signatures (spec §10)
/internal/il          IL decoder (spec §11)
/internal/ir          IR intermedia (spec §12)
/internal/interpreter stack-based interpreter (spec §13)
/internal/runtime     modelo de objetos managed (spec §14-15, 17-18, 20)
/internal/bcl         BCL parcial (spec §16)
/internal/nuget       .nupkg/.nuspec/TFM/resolver (spec §22)
/internal/checker     compatibility checker (spec §23-24)
/cmd/vmnet            CLI (spec §27)
/examples             hello, rules, calculator, nuget-basic
/tests/fixtures       fixtures C# usadas como golden input
/tests/golden         salidas esperadas para tests table-driven
```

Por qué `/internal` en vez del layout plano de la spec: ver
`docs/adr/0002-package-layout.md`.

## Por qué pure-Go (sin CoreCLR en el núcleo)

Ver `docs/adr/0001-pure-go-core.md`.

## Estado actual

Fase 0 (bootstrap), Fase 1 (núcleo IL), Fase 2 (motor de reglas) y Fase 2.5
(endurecimiento) completas. El pipeline `.dll → internal/pe →
internal/metadata → internal/il → internal/ir → internal/interpreter →
internal/bcl` corre de punta a punta contra un assembly real compilado con
el SDK de .NET (`tests/fixtures/csharp`), expuesto por la API pública
(`vmnet.New()`, `Assembly.Call`/`CallBytes`/`CallJSON`) y por el CLI
(`vmnet inspect` / `vmnet il` / `vmnet run`). Alcance actual: métodos
static e instancia, `newobj`/`callvirt`/fields (sin vtable — resolución
directa), `List<T>` / `Dictionary<string,V>` con backing nativo Go,
`throw` no manejado (propagado como error Go tipado,
`vmnet.ManagedException`), y el bridge `byte[]`/JSON. Interface/vtable
dispatch, `try/catch/finally`, `System.Array`, generics más allá de
List/Dictionary, y `DateTime`/`Guid` quedan para fases siguientes
(`docs/ROADMAP.md`) — el IR builder reporta cualquier opcode no soportado
explícitamente en vez de ejecutarlo mal.

Además (Fase 2.5): el intérprete recupera cualquier panic en el borde
público (`Machine.Invoke`) en vez de crashear el proceso host, aplica
`MaxStackDepth` de verdad, `*vmnet.Assembly` es seguro para llamar desde
múltiples goroutines (`sync.RWMutex` sobre los caches de métodos/tipos,
verificado con `-race`), y `internal/pe`, `internal/metadata` e
`internal/il` tienen fuzz tests nativos de Go (corridos manualmente por
~16.8M ejecuciones combinadas sin panics).
