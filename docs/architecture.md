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

Fase 0 (bootstrap) completa: scaffolding de módulos, CI, fixtures C#
compilables. Ningún paquete tiene todavía lógica real — eso arranca en
Fase 1 (`docs/ROADMAP.md`).
