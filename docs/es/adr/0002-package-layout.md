# ADR 0002 — Paquete público en la raíz, implementación bajo `/internal`

- Estado: aceptado
- Fecha: 2026-07-02

## Contexto

La spec original (spec §7) propone una carpeta `/vmnet` (API pública) junto
a carpetas de implementación en la raíz del repo: `/pe`, `/metadata`, `/il`,
`/ir`, `/interpreter`, `/runtime`, `/bcl`, `/nuget`, `/checker`. Puestas así,
todas serían paquetes Go públicos e importables desde fuera del módulo desde
el primer commit.

## Decisión

1. El paquete público vive en la raíz del módulo: `package vmnet` en
   `github.com/arturoeanton/go-vmnet`, sin subcarpeta `/vmnet` — así
   `go get github.com/arturoeanton/go-vmnet` resuelve directo al paquete
   que el usuario final importa (spec §6).
2. Todo lo que la spec listaba como `/pe /metadata /il /ir /interpreter
   /runtime /bcl /nuget /checker` se mueve bajo `/internal/...`. `cmd/vmnet`
   y los `examples/` siguen pudiendo importarlos porque están dentro del
   mismo módulo — `internal/` sólo bloquea imports desde *fuera* del
   repositorio.

## Motivo

Durante las Fases 1-3 el diseño interno (representación de `Value`, IR,
modelo de objetos) va a cambiar con cada fase. Publicar esos paquetes desde
el día uno como API estable de Go obligaría a mantener compatibilidad hacia
atrás antes de tener ni siquiera el intérprete funcionando. `internal/`
deja la única superficie pública comprometida en v1.0 (spec §6, congelada en
Fase 4) sin restar nada a la arquitectura ni a la separación de
responsabilidades que la spec define.

## Consecuencias

- Cualquier necesidad real de exponer algo de bajo nivel (p. ej. la API de
  bajo nivel de spec §6.3, `ResolveMethod`/`NewFrame`/`Invoke`) se agrega
  deliberadamente al paquete raíz `vmnet`, no reexportando `internal/*`
  directamente.
- `docs/es/spec.md` mantiene el layout original como referencia; esta ADR es
  la nota de la única desviación deliberada.
