# Estabilidad de la API y compromiso de semver

Este documento es el entregable de la Fase 4 "congelar la API pública de Go, compromiso de semver"
(`docs/en/ROADMAP.md`). Dice claramente qué está congelado, qué no, y qué significa exactamente un
bump de versión de acá en adelante — así un programa Go que embebe vmnet puede decidir qué tan
ajustado fijar su dependencia.

## Qué cubre

**Solo el paquete raíz, `github.com/arturoeanton/go-vmnet` (paquete `vmnet`) — todo lo importable
sin un segmento de path `internal/`.** Cada paquete `internal/*` (`internal/il`, `internal/ir`,
`internal/metadata`, `internal/pe`, `internal/runtime`, `internal/interpreter`, `internal/bcl`,
`internal/checker`, `internal/nuget`) es exactamente lo que la propia convención `internal/` de Go
significa: detalle de implementación, libre de cambiar de forma en cualquier momento, en cualquier
release, sin aviso. Nada en este documento los restringe. `cmd/vmnet` (el CLI) se cubre por
separado, de forma informal, por su propia superficie de comandos/flags — ver
`docs/es/compatibility-profile.md` para los flags actuales de los subcomandos `check`/`check
package`; el CLI no se rastrea con semver de la forma en que la API de Go sí, ya que se consume
como binario, no como paquete importado.

## La superficie congelada, a partir de esta instantánea

Cada símbolo exportado en el paquete raíz, vigente a partir de la Fase 3.70 (verificable en
cualquier momento con `go doc -all .` — la salida de ese comando es la verdadera fuente de verdad;
esta lista es una instantánea de ella):

**Punto de entrada**
- `func New() *VM`
- `type VM struct{ ... }` (campos no exportados) con métodos:
  `LoadFile(path string) (*Assembly, error)`,
  `LoadBytes(name string, data []byte) (*Assembly, error)`,
  `LoadPackage(id string) (*Assembly, error)`,
  `NuGet() *NuGetManager`,
  `Permissions() *Permissions`

**Llamar hacia código cargado**
- `type Assembly struct{ ... }` (campos no exportados) con métodos:
  `Call(typeName, methodName string, args ...Value) (Value, error)`,
  `CallBytes(typeName, methodName string, input []byte) ([]byte, error)`,
  `CallJSON(typeName, methodName string, input any) (any, error)`,
  `New(typeName string, args ...Value) (*Instance, error)`,
  `WithDependencies(deps ...*Assembly) *Assembly`,
  `Name() string`
- `type Instance struct{ ... }` (campos no exportados) con métodos:
  `Call(methodName string, args ...Value) (Value, error)`, `Native() any`, `TypeName() string`
- `type Value interface{ ... }` — el tipo de argumento/retorno para `Call`/`CallJSON`/`New`/
  `Instance.Call`; implementado por cada constructor de abajo y por `*Instance` mismo (así un
  objeto vivo se puede pasar de vuelta como argumento)
- Constructores de Value: `func Int32(v int32) Value`, `func Int64(v int64) Value`,
  `func Float32(v float32) Value`, `func Float64(v float64) Value`, `func String(v string) Value`,
  `func ByteArray(data []byte) Value`

**NuGet**
- `type NuGetManager struct{ ... }` (campos no exportados) con métodos:
  `Add(id, version string) error`, `Restore() error`, `Packages() ([]Package, error)`
- `type Package struct { ID, Version, SelectedAsset, Unselectable string; Dependencies []string }`
- Constantes: `NuGetManifestFile = "vmnet.json"`, `NuGetLockFile = "vmnet.lock.json"`,
  `NuGetCacheDir = ".vmnet/packages"`

**Errores (spec §30, lograda en la Fase 3.67)**
- `type Code string` y sus 14 constantes definidas por la spec (`CodeInvalidPE`,
  `CodeMissingCLIHeader`, `CodeInvalidMetadata`, `CodeUnsupportedOpcode`,
  `CodeUnsupportedBCLMethod`, `CodeTypeNotFound`, `CodeMethodNotFound`, `CodeFieldNotFound`,
  `CodeStackOverflow`, `CodeCallDepthExceeded`, `CodeManagedException`, `CodeNuGetResolveFailed`,
  `CodeUnsupportedPackage`, `CodePermissionDenied`) más el único agregado deliberado más allá de la
  propia lista de la spec, `CodeInternal` (un catch-all — ver su propio comentario de doc para qué
  significa y qué no significa)
- `type Error struct { Code Code; Message, Details string; Cause error }` con
  `Error() string`/`Unwrap() error`
- `type ManagedException = runtime.ManagedException` (un alias de tipo — usar `errors.As` para
  inspeccionar una excepción CIL lanzada y no manejada que un `Call`/`CallBytes`/`CallJSON` sacó a
  la superficie)

**Permisos (el modelo de seguridad de la spec, logrado en la Fase 3.59)**
- `type Permissions = runtime.Permissions` (un alias de tipo, no un wrapper — ver su propio
  comentario de doc para el porqué) con los campos `AllowFileRead`, `AllowFileWrite` (ambos
  aplicados hoy), `AllowConsole`, `AllowNetwork` (definidos por compatibilidad hacia adelante, no
  aplicados por nada todavía — ver `docs/es/security.md`)

Esa es toda la superficie. Es deliberadamente chica: tres tipos "verbo" (`VM`, `Assembly`,
`Instance`), un tipo de error, un struct de permisos, un manager de NuGet, y un puñado de
constructores de `Value`.

## El compromiso de semver

**vmnet está pre-1.0** (`go.mod` no declara versión; los tags de git hoy están numerados por Fase,
todavía no son semver — ver "Sobre el versionado hoy" abajo). Según la propia regla de semver para
`0.y.z`, cualquier cosa puede cambiar en cualquier release. Este proyecto acota eso de todas formas
a una promesa concreta y útil, porque "pre-1.0" no debería significar "ninguna promesa en
absoluto" para quien realmente dependa de esto:

- **Un release equivalente a PATCH** (una Fase nueva que solo agrega — un nativo nuevo, un opcode
  soportado nuevo, un paquete empujado por encima de la propia vara del 97% del checker, un arreglo
  de bug) nunca cambia una firma exportada existente, remueve un símbolo exportado, o cambia el
  significado de una constante `Code` existente. Seguro de traer sin leer un changelog.
- **Un release equivalente a MINOR** puede agregar símbolos exportados nuevos (un método nuevo, un
  `Code` nuevo, un parámetro opcional nuevo vía variádico) — aditivo, pero vale la pena leer por
  encima la entrada de Fase más nueva de `docs/es/ROADMAP.md` antes de actualizar, por si una
  capacidad nueva cambia un default en el que confiabas implícitamente (p. ej. un campo de
  `Permissions` recién aplicado).
- **Un cambio de firma, un símbolo exportado removido, o un cambio de significado de una constante
  `Code` siempre se trata como breaking** — aunque la propia regla `0.y.z` de semver técnicamente
  lo permitiría en un bump de minor. Cuando uno es genuinamente necesario, se señala explícitamente,
  en negrita, en la propia entrada de esa Fase en `docs/es/ROADMAP.md`, no escondido en la prosa.
- **Una vez que este proyecto llegue a v1.0.0** (el checklist completo de la Fase 4 — ver la propia
  sección "v1.0 listo para producción" de `docs/es/ROADMAP.md` para exactamente qué queda pendiente
  a partir de esta instantánea), empieza el semver real: un cambio breaking requiere un bump de
  versión mayor, que según la propia convención de módulos de Go (`golang.org/x/mod`, la referencia
  de módulos de go.dev) significa un path de módulo nuevo con sufijo `/v2` — `github.com/
  arturoeanton/go-vmnet` mismo nunca rompe bajo los pies de un importador existente sin que ese
  importador decida explícitamente pasarse al path nuevo.

## Sobre el versionado hoy

A partir de esta Fase, los tags de git de este proyecto siguen un patrón
`v0.0.3.<n>.faseNNN-<slug>` (p. ej. `v0.0.3.70.fase370-docs-and-benchmark-suite`) — útil para el
propio rastreo interno de este proyecto, un tag por fase de desarrollo, pero **no es un tag semver
válido de módulo Go** (demasiados componentes numéricos, sin el separador `-prerelease`/`+build`
que el propio resolvedor de módulos de Go reconoce). Un programa Go corriendo `go get
github.com/arturoeanton/go-vmnet@latest` hoy resuelve a una pseudo-versión del último commit en
`main`, no a un release fijado. Los tags numerados por Fase se siguen creando junto a tags semver
reales de acá en adelante (ambos pueden apuntar al mismo commit); el propio commit de esta Fase
también está tageado `v0.1.0` — el primer tag que un consumidor de módulo Go realmente puede
`go get` por número de versión, marcando la propia instantánea de superficie-congelada de este
documento como su punto de partida.

## Qué este documento deliberadamente NO cubre

- El propio boceto de API de la spec §6.1 de `docs/en/spec.md` (un struct `Options{Profile, Debug,
  MaxStackDepth, MaxHeapBytes}` pasado a `New`, una API de bajo nivel `ResolveMethod`/`NewFrame`/
  `Invoke`, `BackendAuto`) fue la propia visión de diseño inicial del proyecto, escrita antes de que
  empezara la implementación real — la API que realmente se construyó y está congelada arriba
  divergió de ella en varios lugares (`New()` no toma ninguna opción en absoluto; `Permissions()`
  es su propio accesor separado, mutable in situ, en vez de un campo de struct en tiempo de
  construcción; no hay ninguna API de bajo nivel Frame/Invoke expuesta públicamente, solo los tres
  métodos `Call*`). **Este documento, no la spec §6.1, es la descripción autoritativa de la API
  actual, real y congelada** — spec.md sigue siendo la visión de diseño original, mantenida por
  contexto histórico, no una promesa sobre lo que se envió.
- Los propios flags/subcomandos del CLI de `cmd/vmnet` (informal, ver
  `docs/es/compatibility-profile.md`).
- Cualquier cosa bajo `internal/` (la propia convención de Go ya hace la promesa acá: ninguna).
