# ADR 0001 — Núcleo pure-Go, sin CoreCLR

- Estado: aceptado
- Fecha: 2026-07-02

## Contexto

`vmnet` necesita ejecutar código C#/IL desde Go. La alternativa obvia es
hostear CoreCLR vía `nethost`/`hostfxr` (hosting nativo documentado por
Microsoft). Eso da compatibilidad casi total, pero exige runtime .NET
instalado, `runtimeconfig.json`, cgo y resolución de runtime — es decir,
deja de ser una librería Go embebible y pasa a ser un integrador de proceso
externo.

## Decisión

El núcleo de `vmnet` es un intérprete de IL/CIL escrito en Go puro:
`CGO_ENABLED=0`, sin `hostfxr`, sin dependencia obligatoria de `dotnet`
instalado en el host que ejecuta la aplicación Go. CoreCLR sólo puede
existir como backend *opcional* futuro (`vmnet.BackendCoreCLR`, ver spec
§39), nunca como requisito del modo por defecto.

## Consecuencias

- vmnet nunca correrá "cualquier DLL .NET" — solo el subconjunto de IL/BCL
  que el intérprete soporta. Esto se comunica explícitamente (spec §3.3,
  §33.3) y se hace verificable con `vmnet check` (Fase 3).
- El mayor riesgo del proyecto pasa a ser la BCL (`System.*`), no el hosting
  de runtime — ver registro de riesgos en `docs/ROADMAP.md`.
- CI puede validarse en Linux/macOS/Windows sin instalar el SDK de .NET en
  los runners que construyen `vmnet` (el SDK de .NET solo hace falta para
  generar los DLL de fixtures de test, un paso de desarrollo aparte).
