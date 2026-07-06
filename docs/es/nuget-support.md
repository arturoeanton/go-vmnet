# Soporte de NuGet

vmnet consume paquetes reales, sin modificar, del mismo registro que usa cualquier proyecto .NET —
nuget.org — enteramente en Go puro. No hay CLI de `dotnet`, no hay `NuGet.exe`, y no hay runtime de
.NET involucrado en ningún punto de bajar, parsear, resolver o leer un paquete. Todo lo que se
documenta acá vive en `internal/nuget/` (el motor de parseo/resolución/lockfile) y en el `nuget.go`
de la raíz (la API pública `NuGetManager`/`VM.LoadPackage` que efectivamente llama un host en Go).
Esto es la implementación del §22 del spec.

La versión corta: `vm.NuGet().Add("NodaTime", "3.2.0")` seguido de `vm.NuGet().Restore()` baja el
`.nupkg` real desde nuget.org (o desde el cache local propio de vmnet), parsea su `.nuspec` real,
resuelve su grafo real de dependencias transitivas, y escribe `vmnet.lock.json` — la respuesta
propia de vmnet a `packages.lock.json`. `vm.LoadPackage("NodaTime")` después carga exactamente los
bytes del DLL que Restore fijó, conecta el grafo de dependencias resuelto, y devuelve un
`*vmnet.Assembly` listo para `Call`/`New`.

## Qué se parsea realmente

Un `.nupkg` es un archivo zip real; `nuget.OpenPackage` (`internal/nuget/nupkg.go`) lo abre con el
`archive/zip` estándar de Go y se queda solo con las entradas que el resolver podría necesitar
alguna vez — `lib/`, `ref/`, `runtimes/`, y el `.nuspec` de la raíz — descartando el ruido de
empaquetado OPC (`_rels/`, `package/`, `[Content_Types].xml`) que las herramientas reales de NuGet
emiten pero que vmnet no usa para nada. Cada entrada retenida se lee bajo un límite de 256MB, para
que un `.nupkg` hostil o corrupto no pueda hacer OOM al resolver.

El `.nuspec` (`internal/nuget/nuspec.go`) se parsea como XML plano hacia `NuSpec` — deliberadamente
un modelo angosto del schema real: identidad del paquete (`id`, `version`) y grupos de
dependencias. Un `.nuspec` real trae autores, licencia, ícono, notas de versión, y más; nada de eso
afecta si el IL de un paquete puede correr dentro de vmnet, así que nada de eso se modela. Se
manejan dos formas de dependencias, porque ambas aparecen en paquetes reales según qué herramienta
generó el `.nuspec`:

- **Legacy/plana**: `<dependencies><dependency id="..." version="..."/></dependencies>` — aplica
  incondicionalmente, sin distinción por framework.
- **Moderna/agrupada**: un `<group targetFramework="...">` por TFM, cada uno con su propia lista de
  `<dependency>` — esta es la forma que exige resolución consciente de TFM (ver abajo).

## Selección de TFM

El resolver de vmnet entiende las dos notaciones que usan los `.nuspec` reales para un target
framework moniker: la forma corta de nombre de carpeta (`netstandard2.0`, `net8.0`, `net472`) y la
forma larga que todavía emiten algunas herramientas (`.NETStandard,Version=v2.0` y la variante
abreviada con puntos, `.NETFramework4.7.2`). `ParseTFM` (`internal/nuget/tfm.go`) normaliza ambas a
un único struct `TFM` con una `Family` (`FamilyNetStandard`, `FamilyNetCoreApp`, `FamilyNetModern`
para net5.0+, `FamilyNetFramework`) más números de major/minor/patch. Un TFM que trae un
calificador de sistema operativo (`net6.0-windows`) queda marcado `IsPlatformSpecific` y nunca es
seleccionable — vmnet es Go puro y cross-platform por construcción, así que un asset específico de
plataforma es un no rotundo sin importar el tier.

Los tiers de selección, de `tier()` en `tfm.go`, calzan exactamente con la lista de prioridad del
§22.5 del spec:

1. **`netstandard2.0`** — tier 1, el target preferido.
2. **`netstandard2.1`** — tier 2.
3. **`net5.0`+ (.NET moderno)** — tier 3, pero *solo* si quien llama lo habilita explícitamente vía
   `SelectOptions.AllowModernNet` (apagado por defecto). Esto es deliberado: el perfil IL/BCL de
   vmnet está pensado para código con forma de netstandard2.0, no para la superficie moderna de la
   BCL que un paquete net5.0+ puede asumir presente.
4. **`netstandard1.x`** — tier 5 (más viejo, todavía usualmente compatible en el código fuente,
   pero rankeado por debajo del `net5.0` moderno aun cuando lo moderno no está habilitado, ya que
   es un fallback legacy y no un target real).
5. Cualquier otra cosa (`net472`-estilo .NET Framework, `netcoreapp3.1`-estilo netcoreapp directo,
   un moniker no reconocido) — tier 0, no seleccionable, con una razón legible en `Selectable()`
   (p. ej. `"net472 targets .NET Framework, not netstandard2.0-compatible"`).

`SelectTFM` elige el tier más bajo (mejor) entre todas las carpetas `lib/<tfm>/` que un paquete
ofrece. Consecuencia práctica, confirmada por el propio corpus de 19 paquetes de
`docs/es/COMPATIBILITY.md`: la mayoría de los paquetes reales que exponen "lógica pura"
(parsers, validadores, serializadores, bibliotecas de fecha/hora) siguen publicando un asset
`netstandard2.0` incluso años después de que salió net5.0+, justamente porque `netstandard2.0`
sigue siendo la forma en que un autor de biblioteca llega a la audiencia más amplia (consumidores
de .NET Framework viejo incluidos). `vmnet check package` sobre ese corpus casi siempre reporta
`netstandard2.0` como el target seleccionado — `AllowModernNet` existe para los paquetes que no
cumplen esto, pero no es la postura por defecto de vmnet.

## Resolución de dependencias

`NuSpec.DependenciesFor(target TFM)` elige la lista de dependencias que aplica una vez que ya se
eligió un TFM concreto (típicamente el mismo que eligió `SelectLibAsset`): busca el `<group>` cuyo
propio `targetFramework` coincide con la familia/major/minor de `target`, cayendo hacia un grupo
explícito de TFM vacío ("aplica a todo") si existe uno, o hacia la lista plana legacy si el
`.nuspec` no tiene grupos en absoluto.

`Resolver.Resolve` (`internal/nuget/resolver.go`) recorre la clausura transitiva completa
empezando desde las dependencias directas de un proyecto: para cada paquete, lo trae (vía
`Cache.Fetch`, primero el cache, cayendo hacia `Client.Download` desde nuget.org), selecciona su
mejor asset, lee el grupo de dependencias de ese asset, y recurre sobre *esas* dependencias —
exactamente el mismo grafo que recorrería un `dotnet restore` real, solo que resuelto con el
algoritmo propio, más simple, de vmnet. Dos simplificaciones son explícitas, no accidentales:

- **Los conflictos de versión se resuelven por "gana la versión más alta".** Si dos caminos del
  grafo piden versiones distintas del mismo paquete, el resolver se queda con la más alta y
  vuelve a resolver desde ahí. NuGet real hace negociación completa de rangos de versión; el §22.3
  del spec pide deliberadamente "dependencias transitivas simples" en su lugar.
- **Un string de versión de dependencia puede ser un rango** (`"[3.1.1, 4.0.0)"`, `"[3.1.1]"`,
  `"(1.0.0, )"` — los `.nuspec` reales los usan de forma rutinaria; la propia dependencia de
  `ClosedXML@0.105.0` sobre `DocumentFormat.OpenXml` está declarada exactamente así).
  `ParseMinVersion` (`internal/nuget/version.go`) siempre resuelve hacia el límite inferior del
  rango — el mismo comportamiento de "versión mínima aplicable" que usa NuGet real por defecto
  para un `PackageReference` plano sin notación flotante — en vez de volver a consultar nuget.org
  para encontrar la versión más alta real que satisface el rango.
- Un ciclo de dependencias (muy poco probable en un grafo real, pero no imposible) se detecta y se
  reporta como error en vez de recurrir para siempre.

Cada nodo resuelto se vuelve un `ResolvedPackage`: su asset seleccionado y su TFM (o una razón
`Unselectable` si no se pudo seleccionar nada — ver más abajo), más sus propios IDs de dependencia
directa. `NuGetManager.Restore()` alimenta la salida de `Resolver.Resolve` directamente a
`BuildLockFile`.

### Conectar el grafo resuelto al intérprete

Resolver el grafo en disco es solo la mitad de la historia — `VM.LoadPackage` (`nuget.go`) es lo
que hace que las llamadas entre paquetes efectivamente se ejecuten. Desde la Fase 3.27 (la fase que
hizo correr de verdad `Jint.Engine.Evaluate()`), `Assembly` trae un campo `deps []*Assembly` y un
método `WithDependencies(...*Assembly)`; cada uno de los resolvers de método/campo del intérprete
cae hacia `asm.deps` cuando no encuentra un símbolo en el propio assembly local. `LoadPackage`
arma esto automáticamente: `loadLockedPackage` recorre recursivamente la lista `Dependencies` ya
calculada del lockfile para el ID de paquete pedido, carga el asset seleccionado de cada
dependencia (saltando cualquier dependencia sin asset gestionado seleccionable — ver abajo — no
como error, ya que puede legítimamente no tener nada propio para cargar), y conecta cada
dependencia cargada vía `WithDependencies`. Las dependencias en diamante (un paquete alcanzable por
más de un camino en el grafo) se cargan una sola vez y se cachean dentro de una misma llamada a
`LoadPackage`, y un ciclo de dependencias termina limpiamente en vez de recurrir para siempre.

Esto es lo que hace que la cadena real de dependencias de Jint — `Jint` → `Esprima` →
`System.Memory` → `System.Buffers`/`System.Numerics.Vectors`/
`System.Runtime.CompilerServices.Unsafe` — se resuelva de punta a punta en tiempo de ejecución:
una llamada desde el propio IL de Jint directamente hacia un tipo propio de Esprima no es un caso
especial, es el mismo fallback de resolvers que ya usa cualquier llamada local, solo que extendido
a través de un límite de assembly. Desde la Fase 3.40, `LoadPackage` también arma un índice de
tipos compartido entre paquetes (`globalTypeIndex`) como fallback de último recurso para que el
`typeof(T)` de un método genérico resuelva contra un tipo declarado en uno de sus propios
dependientes — de mejor esfuerzo, nunca requerido para que un paquete cargue, y se salta en
silencio cualquier fila `TypeDef` que no pueda decodificar.

## El lockfile (`vmnet.lock.json`)

`vmnet.lock.json` es el formato propio de vmnet para el grafo de dependencias resuelto
(`internal/nuget/lockfile.go`) — documentado explícitamente como *no* compatible con el
`packages.lock.json` real de NuGet, con el mismo espíritu (restores reproducibles que no derivan
en silencio entre máquinas/corridas) pero deliberadamente más simple, en línea con las propias
simplificaciones deliberadas del resolver de arriba. Su forma, según el §22.6 del spec:

```json
{
  "version": 1,
  "target": "netstandard2.0",
  "packages": [
    {
      "id": "NodaTime",
      "version": "3.2.0",
      "selectedAsset": "lib/netstandard2.0/NodaTime.dll",
      "unselectable": "",
      "dependencies": []
    }
  ]
}
```

`BuildLockFile` ordena los paquetes por ID y la lista de dependencias de cada paquete
alfabéticamente, así que dos restores del mismo grafo de dependencias siempre producen una salida
idéntica byte a byte — un diff del lockfile en control de versiones solo muestra un cambio real,
nunca no-determinismo del resolver o de la iteración de un map. `WriteLockFile`/`ReadLockFile` van
y vuelven a través de `encoding/json` sin ningún campo con pérdida: todo campo que escribe
`BuildLockFile` sobrevive intacto a una lectura de vuelta (cubierto directamente por
`internal/nuget/lockfile_test.go`), que es justo lo que le permite a `LoadPackage` confiar en el
propio `SelectedAsset`/`Dependencies` del lockfile al cargar, sin tener que volver a correr la
resolución.

`NuGetManager.Packages()` lee el lockfile de vuelta y devuelve cada entrada como un
`vmnet.Package` público — los mismos campos, solo que sin exigirle a quien llama importar el
paquete `internal/nuget` para inspeccionar qué se resolvió.

## Qué NO está soportado, explícitamente

vmnet es honesto sobre dos categorías de contenido de paquete que no puede ejecutar — las dos se
detectan y se reportan con una razón, nunca se descartan en silencio ni se informan erróneamente
como "compatibles":

**Assets solo-nativos.** `Package.HasNativeAssets()` detecta contenido `runtimes/*/native/*` —
binarios nativos reales (un `.dll`/`.so`/`.dylib`) que un paquete trae en vez de, o además de, IL
gestionado. vmnet es Go puro sin ningún camino de CGo o carga de nativos, así que un paquete cuyo
*único* contenido seleccionable es nativo queda marcado `Unselectable` con una razón explícita
(`"package only ships native assets (...) — unsupported in pure-Go mode"`), tanto en el propio
valor de retorno de `SelectLibAsset` como, de punta a punta, en el campo `unselectable` del
lockfile y en la salida `Status: unsupported` de `vmnet check package`. Esta es una limitación
deliberada y permanente (tier 5 del §22.5 del spec), no un bug para tragarse en silencio — un host
en Go que llama `LoadPackage` sobre un paquete así recibe un error claro que apunta a `Packages()`
para la razón, y una dependencia alcanzable solo en forma nativa se salta al conectar
`WithDependencies`, exactamente igual que cualquier otro paquete sin nada propio para cargar.

**Ensamblados solo-referencia (`ref/`).** Un `ref/<tfm>/Foo.dll` es una convención real de .NET: un
ensamblado de referencia solo para compilación cuyos cuerpos de método están recortados (firmas
reales, sin IL real), usado por la cadena de build de .NET puramente para resolver la forma de la
API en tiempo de compilación mientras un asset separado en tiempo de ejecución provee la
implementación real. `SelectLibAsset` solo cae hacia `ref/` cuando no hay ningún asset `lib/`
seleccionable para el TFM target en absoluto, y marca el resultado `ReferenceOnly: true` con una
nota explícita: `"selected from ref/ (compile-time reference only) — cannot be executed, only
inspected"`. La implicación práctica: el checker y el lector de metadata de vmnet pueden abrir un
asset solo-`ref/` y decirte cuál es su superficie de API real — nombres de tipo, firmas de método —
pero no hay cuerpo de método para interpretar, así que nada de eso puede efectivamente correr.
`vmnet check package` lo muestra directamente (`Note: reference-only asset (ref/) — inspected, but
cannot be executed`); un paquete que solo ofrece un asset `ref/` para el TFM target de vmnet es
inspeccionable pero no es candidato para `LoadPackage`-y-correr. Cuando un paquete trae *tanto*
`lib/` como `ref/` para TFMs compatibles, vmnet siempre prefiere la implementación real de `lib/`
— `ref/` es estrictamente el último recurso, nunca se elige por sobre un asset real y ejecutable.

Ninguna de las dos es silenciosa: cada razón `Unselectable` y cada flag `ReferenceOnly` sobrevive
todo el camino desde `Package.SelectLibAsset`, pasando por el `Resolver`, hacia el lockfile, y
hasta la salida tanto de `NuGetManager.Packages()` como de la consola de `vmnet check package` — la
misma postura de "explicar, no solo fallar" que el §23 del spec exige del checker de
compatibilidad aplica exactamente igual acá.

## `vmnet check package` — evaluar un paquete antes de escribir código Go

La forma recomendada de saber si un paquete real de NuGet va a funcionar en vmnet no es agregarlo
al proyecto y ver qué se rompe — es `vmnet check package`, que corre todo el pipeline de
fetch/select/resolve/analyze de solo lectura, sin tocar el manifest ni el lockfile del proyecto:

```bash
vmnet check package NodaTime@3.2.0
vmnet check package --profile=netstandard-lite Jint@3.1.3
vmnet check package Newtonsoft.Json          # sin @versión: resuelve la última de nuget.org
```

De punta a punta (`runCheckPackage` en `cmd/vmnet/main.go`), esto: baja (o reusa del cache) el
`.nupkg` directo de nuget.org, llama a `SelectLibAsset` e imprime el TFM y el path del asset
seleccionado (o la nota `Unselectable`/`ReferenceOnly` de arriba si aplica), resuelve el grafo
*completo* de dependencias transitivas del paquete exactamente de la misma forma que lo hace
`vm.LoadPackage` en tiempo de ejecución, decodifica la metadata propia de cada dependencia, y
finalmente corre `checker.AnalyzeWithDeps` — el mismo analizador estático del que salen los números
por paquete de `docs/es/COMPATIBILITY.md`, con ese mismo contexto real de dependencias para que una
llamada desde el propio IL del paquete directamente hacia un tipo de una dependencia resuelta
(Jint → Esprima, NPOI → ZString, ClosedXML → DocumentFormat.OpenXml) se verifique contra los
métodos reales de esa dependencia en vez de reportarse erróneamente como no resuelta solo porque se
decodificó únicamente el DLL de nivel superior (Fase 3.29).

La salida reporta un status (`compatible`/`partial`/`unsupported`), el profile contra el que se
verificó, cuántos métodos se analizaron contra cuántos se marcaron y — este es el porcentaje que
reporta `COMPATIBILITY.md` por paquete — `(métodos analizados − métodos marcados) / métodos
analizados`. Es una estimación de cobertura de lo que el checker estático pudo confirmar que
resuelve contra algo que vmnet realmente implementa, no una prueba de corrección; una
implementación nativa sutilmente equivocada que el checker no tiene forma de ver es justamente por
qué `COMPATIBILITY.md` también sigue por separado los demos reales que efectivamente corren.
`docs/es/compatibility-profile.md` (un documento hermano) cubre en profundidad qué permite
realmente cada profile (`minimal`/`rules`/`netstandard-lite`) y el formato completo de
findings/reporte — este documento solo cubre la mitad específica de NuGet del pipeline que
alimenta a eso.

`vmnet bind package <id>@<versión>` (`internal/bind`, Fase 3.75) es el otro comando del CLI que
resuelve un paquete real directo desde nuget.org: `runBindPackage` (`cmd/vmnet/main.go`) reusa
exactamente los mismos bloques `nuget.Client`/`nuget.Cache`/`OpenPackage`/`SelectLibAsset` que usa
`check package` para bajar el `.nupkg` y elegir su mejor asset, pero se detiene ahí en vez de
recorrer el grafo completo de dependencias transitivas — generar un wrapper Go idiomático solo
necesita la metadata propia y ya decodificada del paquete target, no la de sus dependientes. El
§3.2 de `docs/es/compatibility-profile.md` cubre qué genera y cómo usar el resultado; el trabajo de
este documento es solo señalar que se apoya en la misma maquinaria de fetch/select documentada
arriba.
