# Compatibilidad: 19 paquetes reales, medidos de tres formas separadas

Este documento existe porque un solo número — "X% compatible" — esconde más de lo que revela. Un
puntaje de checker estático, un demo real corriendo, y confianza real en la corrección son tres
cosas distintas, y confundirlas es exactamente cómo un proyecto termina lanzando algo que "parece"
97% listo pero se rompe en el instante en que un usuario real lo corre. Esta página mantiene las
tres separadas, a propósito, para cada paquete contra el que se mide vmnet.

## Las tres columnas, y qué significa realmente cada una

- **% de checker** — el analizador estático de `internal/checker` recorre cada método del paquete
  (más todo su grafo de dependencias transitivas, resuelto exactamente de la misma forma que lo
  hace `vm.LoadPackage` en tiempo de ejecución) y reporta, método por método, si cada llamada
  BCL/opcode que usa resuelve contra algo que vmnet realmente implementa, bajo el profile
  `netstandard-lite`. El porcentaje es `(métodos sin ningún finding) / (métodos analizados)`.
  **Esto es una estimación de cobertura, no una prueba de corrección** — un método puede tener
  cero findings y aun así comportarse sutilmente mal si una implementación nativa tiene un bug que
  el checker no tiene forma de ver (para eso están los demos reales, más abajo). Reproducí
  cualquier número acá vos mismo: `vmnet check package --profile=netstandard-lite <id>@<versión>`.
- **Demo real** — si `examples/` tiene un programa real y corrible que carga el paquete real, sin
  modificar, desde nuget.org, y ejercita su código real de punta a punta, con la salida comparada
  contra `dotnet run` real/el SDK de .NET real cuando aplica. Esta es la señal más fuerte que tiene
  vmnet: significa que alguien realmente corrió la lógica real de este paquete específico y
  confirmó que la salida coincide con .NET real, no solo que el checker no marcó nada.
  Reproducilo vos mismo: `cd examples/<nombre> && go run .`.
- **Confianza** — una nota en lenguaje simple sobre qué deberías concluir realmente de las primeras
  dos columnas para este paquete específico, escrita para resistir la tentación de redondear un %
  de checker alto hacia "completamente verificado". La nota de cada paquete vive en la subsección
  "Notas de confianza" justo después de su tabla, no embutida en la tabla misma — suficientemente
  larga como para necesitar saltos de línea y encabezados reales, no una sola celda sin cortes.

## Paquetes con demo real y funcionando (la señal más fuerte)

| Paquete | % de checker | Demo |
|---|---|---|
| `DocumentFormat.OpenXml@3.1.1` | 100.0% (67.234 métodos, 7 marcados) | [`examples/openxml-demo`](../../examples/openxml-demo) |
| `NPOI@2.8.0` | 98.2% (14.202 métodos, 249 marcados) | [`examples/npoi-demo`](../../examples/npoi-demo) |
| `System.Text.Json@8.0.5` | 98.2% (3.577 métodos, 66 marcados) | [`examples/system-text-json-demo`](../../examples/system-text-json-demo) |
| `FluentValidation@11.9.2` | 98.2% (1.289 métodos, 23 marcados) | [`examples/fluentvalidation-demo`](../../examples/fluentvalidation-demo) |
| `ClosedXML@0.105.0` | 97.5% (10.444 métodos, 257 marcados) | [`examples/closedxml-demo`](../../examples/closedxml-demo) |
| `Jint@3.1.3` | 96.7% (5.414 métodos, 178 marcados) | [`examples/jint-demo`](../../examples/jint-demo), [`examples/jint-nowrapper`](../../examples/jint-nowrapper), [`examples/jint-advanced-demo`](../../examples/jint-advanced-demo) |
| `Dapper@2.1.79` | 95.5% (1.047 métodos, 47 marcados) | [`examples/dapper-demo`](../../examples/dapper-demo), [`examples/sqlite-demo`](../../examples/sqlite-demo) |
| `CsvHelper@33.1.0` | 94.5% (1.393 métodos, 76 marcados) | [`examples/csvhelper-demo`](../../examples/csvhelper-demo) |
| `Microsoft.Extensions.DependencyInjection@8.0.0` | 94.1% (437 métodos, 26 marcados) | [`examples/di-demo`](../../examples/di-demo) |
| `Newtonsoft.Json@13.0.3` | 89.2% (4.064 métodos, 440 marcados) | [`examples/newtonsoft-json-demo`](../../examples/newtonsoft-json-demo) |

Seis de estos nueve números subieron en la Fase 3.79 — no porque esa Fase apuntara a estos
paquetes directamente (estaba persiguiendo las brechas propias que le quedaban a Jint), sino porque
varios de sus arreglos son correcciones generales de CIL/BCL (una llamada a interfaz genérica con
prefijo `constrained.` que nunca se desreferenciaba, `conv.u8` extendiendo con signo en vez de con
ceros, los operadores de comparación de `TimeSpan`, `StringBuilder.set_Capacity`/
`ToString(inicio,largo)`, y media docena de nativos de `Regex`/`Span<T>`) que los propios caminos
de código real de otros paquetes también terminan tocando. Reproducido con la misma invocación de
`vmnet check package` documentada más abajo, justo después de que aterrizara la Fase 3.79.

### Notas de confianza

#### `DocumentFormat.OpenXml@3.1.1`

**Verificado.** Genera un `.docx` real desde cero; la salida se verifica abriéndola de vuelta con
el SDK de .NET real, sin modificar — no solo que vmnet produjo *algunos* bytes.

#### `NPOI@2.8.0`

**Verificado.** Lee un archivo `.xls` legacy real de punta a punta (strings, números, una celda con
fórmula `SUM`); queda una brecha cosmética conocida y documentada (el texto del rango de celdas de
la fórmula renderiza puntos de código numéricos en vez de letras de columna — los *valores* de
celda son correctos).

% de checker subió en la Fase 3.74 (arreglos de perfil de `IReadOnlyDictionary\`2`/
`CancellationToken` — ver las notas de `ClosedXML`/`System.Text.Json` abajo para qué fueron esos
arreglos).

#### `System.Text.Json@8.0.5`

**Verificado.** Parsea JSON real a través de la propia API real de `JsonDocument`, confirmado
contra la salida real de .NET.

Cruzó la barrera del 97% en la Fase 3.74: nativos nuevos para `ArraySegment\`1`, `Array.CopyTo`,
`Exception.Source`, `KeyNotFoundException`, y `ICollection\`1.IsReadOnly`, más los mismos arreglos
de `IReadOnlyDictionary\`2`/perfil que `ClosedXML` abajo.

`JsonSerializer.Serialize`/`Deserialize` en sí sigue bloqueado por un hueco separado y más
profundo encontrado en la Fase 3.70 (un campo de buffer de tamaño fijo unsafe) — el propio parseo
basado en `JsonDocument` de este demo es una superficie de API distinta que ya funciona. Rastreado
como el [issue #4](https://github.com/arturoeanton/go-vmnet/issues/4) — ver la Fase 3.70 de
`docs/es/ROADMAP.md` para el relato completo de la causa raíz.

#### `FluentValidation@11.9.2`

**Verificado para validación de objetos real, incluyendo validadores de rango numérico.** Un
validador real (`RuleFor`/`NotEmpty`/`WithMessage`/`GreaterThanOrEqualTo`) acepta un objeto válido
y rechaza uno inválido con el mensaje de error correcto.

Llegar acá necesitó que `Expression<TDelegate>.Compile()` funcione de verdad para una clase real
(aunque angosta) de árboles de expresión (Fase 3.64) — FluentValidation compila e invoca el lambda
de acceso a propiedad para leer el valor que se está validando, no solo inspecciona su forma.

La Fase 3.66 diagnosticó correctamente (pero todavía no arregló) el bug de despacho de los
validadores numéricos: dos sobrescrituras `IsValid` de mismo nombre y misma aridad a través de un
par de clases base/derivada genéricas (`AbstractComparisonValidator<T,TProperty>` y
`GreaterThanOrEqualValidator<T,TProperty>`), distinguibles en .NET real solo por firma
completa/slot de vtable, estaban siendo confundidas por la caminata de ancestros de vmnet, que solo
mira por nombre. **Arreglado en la Fase 3.68** con una regla general de resolución de sobrecarga
(dos posiciones que declaran el mismo parámetro genérico todavía abierto deben enlazar al mismo
`Kind` en tiempo de ejecución).

**Queda una limitación más angosta y documentada**: un argumento de tipo valor boxeado cuyo valor
es igual al cero de su tipo (p. ej. un `int` boxeado con valor `0`) es indistinguible de un null
real para el `box` de vmnet, que es un passthrough de identidad — por eso las verificaciones estilo
`x?.ToString()` sobre ese valor lo tratan incorrectamente como null. Esto se manifiesta en el
formateo de mensajes multi-placeholder de `InclusiveBetween` solo cuando un límite es exactamente
`0`; el demo evita este caso angosto.

#### `ClosedXML@0.105.0`

**Verificado**, con una salvedad honesta: un pequeño wrapper de C# compilado provee un
`IXLGraphicEngine` mínimo, porque el propio motor de métricas de fuente por defecto de ClosedXML
choca contra una limitación arquitectónica real y profunda (sustitución de parámetro de tipo
genérico dentro de los propios inicializadores de campo estático de una clase genérica) sin
relación con leer datos de celda en sí. Lee un `.xlsx` real correctamente; también fue el sujeto de
un cuelgue no determinista real y arreglado (Fase 3.44) — ahora estable a través de corridas
repetidas.

**Cruzó la barrera del 97% en la Fase 3.74**: `IReadOnlyDictionary\`2` (un receptor `Dictionary\`2`
real despacha hacia él idénticamente a `IDictionary\`2`, verificado con un test real de ida y
vuelta) explicaba el mayor pedazo individual de lo que estaba marcado.

#### `Jint@3.1.3`

**Verificado.** Corre un motor de JavaScript real de punta a punta — parsea código JS real,
construye un AST real, lo evalúa, y devuelve un resultado real — tanto a través de un wrapper
compilado como con cero pegamento de C#. La evidencia más fuerte de que vmnet maneja código real
genuinamente no trivial y profundamente orientado a objetos, no solo bibliotecas pequeñas de
métodos estáticos.

Las Fases 3.77-3.79 llevaron esto de tres clases enteras de JavaScript real documentadas como rotas
(declaraciones de función, crecimiento de arrays/métodos de string, clases ES6/`.concat`/`.map`/
`JSON.stringify`/regex) a una sola brecha más angosta que queda (grupos entre paréntesis de regex y
clases abreviadas `\d`/`\w`/`\s`) — `examples/jint-advanced-demo` es la prueba corriendo, ejercitando
closures, recursión, funciones flecha, métodos de orden superior de arrays, herencia ES6 con
`super`, `.test`/`.exec`/`.match`/`.replace` de regex, `JSON.stringify` sobre datos anidados reales
de varios dígitos, y template literals, todo en un solo script. Ver las entradas de las Fases
3.77/3.78/3.79 en `docs/es/ROADMAP.md` para el relato completo y citable.

#### `Dapper@2.1.79`

**Verificado, de dos formas.** `dapper-demo` corre el propio `SqlMapper.Query`/`Execute` real de
Dapper contra un proveedor ADO.NET fake en memoria; `sqlite-demo` corre el mismo código real de
Dapper contra el propio proveedor `Microsoft.Data.Sqlite` real y nativo en Go de vmnet, y después
reabre de forma independiente el archivo `.db` resultante con el CLI real `sqlite3` y corre
`PRAGMA integrity_check`.

Quedan dos brechas arquitectónicas reales, permanentes y documentadas (una limitación de
`typeof(T)` en métodos genéricos, y una feature de regex de Dapper que el motor RE2 de Go nunca
puede compilar) — ver `docs/en/ROADMAP.md` Fase 3.52/3.53.

#### `CsvHelper@33.1.0`

**Verificado.** `csvhelper-demo` corre `CsvReader.GetRecords<T>()` **sin ningún `ClassMap`
registrado**, forzando a que el propio camino de `AutoMap()` de CsvHelper, basado solo en reflexión
(`Type.GetConstructor(s)`, `Expression.New`/`Lambda`/`Compile`), construya el tipo de registro y
cada mapa de miembro puramente en tiempo de ejecución — exactamente la brecha que este documento
había marcado antes como "todavía no es un demo funcionando", arreglada de verdad en la Fase 3.81
(ocho bugs distintos: identidad de genérico cerrado a través de
`Type.GetConstructor()`/`ConstructorInfo.Invoke()`, la misma identidad perdida de forma inversa en
tiempo de construcción, el parámetro genérico a nivel de clase de un iterador generado por el
compilador que no sobrevivía a una llamada a método genérico reenviada, el mismo centinela que no
sobrevivía a quedar anidado un nivel dentro de un tipo genérico cerrado, `System.String.Join`
recibiendo un `IEnumerable<string>` real de un plugin, y varias fábricas de
`System.Linq.Expressions` faltantes). Queda una brecha conocida y más estrecha:
`new List<T>(algúnIEnumerablePlugin)` construye silenciosamente una lista vacía en lugar de
conducir el protocolo de enumeración real de la fuente — ver el propio relato de la Fase 3.81 en
`docs/es/ROADMAP.md` y `examples/csvhelper-demo/README.md`.

#### `Microsoft.Extensions.DependencyInjection@8.0.0`

**Verificado para inyección de constructor real** — el propio contenedor de DI oficial de
Microsoft resuelve un servicio cuyo constructor depende de otro servicio registrado, a través de
su propia API real `ServiceCollection`/`ServiceProvider`/`GetRequiredService<T>()`, sin modificar.
Llegar acá requirió tres fixes reales del intérprete (Fase 3.60): un tie-break de resolución de
overloads de método causando una auto-recursión infinita, `typeof(T)` nunca resolviendo sobre el
propio parámetro de tipo abierto de un método genérico, y una brecha de reflection entre paquetes.

**Todavía no verificable en la práctica**: el propio camino rápido de árbol de expresión compilado
de `DependencyInjection` (`ExpressionResolverBuilder`) — la Fase 3.65 construyó el evaluador
general de árbol de expresión que esto necesita, pero leer el propio IL real muestra que el camino
rápido es una optimización de mejor esfuerzo en segundo plano (encolada vía `ThreadPool` después de
la 2ª resolución de un servicio, con cualquier fallo de compilación tragado silenciosamente) que se
comporta idénticamente para un llamador real sea que tenga éxito o no; `di-demo` ejercita el OTRO
camino de resolución, siempre activo (`CallSiteRuntimeResolver`), que no necesita
`Expression.Compile()` en absoluto.

#### `Newtonsoft.Json@13.0.3`

**Verificado para el camino demostrado** (parseo real del DOM "LINQ to JSON" y acceso por
indexador), pero el % de checker más bajo de cualquier paquete con demo — su superficie de tipado
dinámico basada en `Dynamic`/`ExpandoObject` (`JValue+JValueDynamicProxy`) es una brecha real y no
implementada que el demo no ejercita. No leas que el demo pase como "todo este paquete funciona".
Rastreado como el [issue #3](https://github.com/arturoeanton/go-vmnet/issues/3).

## Paquetes medidos solo por el checker (todavía sin demo)

Que todavía no exista un demo no es una señal de alarma por sí sola — cada uno de los paquetes de
arriba empezó acá también. Sí significa que todavía nadie corrió el código real de este paquete
específico de punta a punta y comparó la salida contra .NET real; tratá el porcentaje como una
estimación de cobertura de lo que *probablemente* funcionaría, no como confirmación de que
funciona.

| Paquete | % de checker |
|---|---|
| `Ardalis.GuardClauses@5.0.0` | 98.6% (285 métodos, 4 marcados) |
| `Humanizer.Core@2.14.1` | 98.4% (1.597 métodos, 25 marcados) |
| `Polly@8.7.0` | 97.0% (2.049 métodos, 61 marcados) |
| `YamlDotNet@18.1.0` | 96.2% (2.182 métodos, 82 marcados) |
| `Serilog@4.3.1` | 96.1% (1.115 métodos, 43 marcados) |
| `MediatR@14.2.0` | 95.5% (441 métodos, 20 marcados) |
| `NodaTime@3.3.2` | 94.8% (3.098 métodos, 162 marcados) |
| `AutoMapper@16.2.0` | 94.2% (2.319 métodos, 135 marcados) |
| `SimpleBase@4.0.0` | 92.6% (258 métodos, 19 marcados) |
| `Semver@2.3.0` | 92.9% (423 métodos, 30 marcados) |

### Notas de confianza

#### `Ardalis.GuardClauses@5.0.0`, `Humanizer.Core@2.14.1`

Estimación de cobertura alta; no verificado por una corrida real.

#### `Polly@8.7.0`

Estimación de cobertura alta; no verificado por una corrida real. Subió en la Fase 3.74 —
`CancellationToken` tenía nativos reales desde bastante antes de esta Fase pero ninguna entrada en
la lista de perfil del checker. **Cruzó la barrera del 97% en la Fase 3.79** con los mismos
arreglos generales de `TimeSpan`/`Regex`/`conv.u8`/`constrained.` que movieron a varios otros
paquetes de este documento — la propia lógica de reintentos/circuit-breaker de Polly se apoya en
comparaciones de `TimeSpan`.

#### `YamlDotNet@18.1.0`

Buena estimación de cobertura; no verificado por una corrida real.

#### `Serilog@4.3.1`

Buena estimación de cobertura; no verificado por una corrida real. Subió en la Fase 3.74 (arreglo
de perfil de `CancellationToken`); subió de nuevo en la Fase 3.79 (los mismos arreglos generales de
`TimeSpan`/`Regex`/`Span<T>` que `Polly` arriba).

#### `MediatR@14.2.0`

Estimación de cobertura moderada; no verificado por una corrida real. Subió en la Fase 3.74
(arreglo de perfil de `CancellationToken`).

#### `NodaTime@3.3.2`, `SimpleBase@4.0.0`, `Semver@2.3.0`

Estimación de cobertura buena-a-alta; no verificado por una corrida real. El propio número de
`NodaTime` se movió levemente en la Fase 3.79 (`TimeSpan`/`conv.u8`).

#### `AutoMapper@16.2.0`

La Fase 3.66 encontró la causa raíz y arregló de verdad el NRE de `ValueTuple` de la Fase 3.65 (una
brecha general de default tipado en `Enumerable.FirstOrDefault/LastOrDefault/SingleOrDefault<T>`) Y
arregló un bug real y profundo de registro de TypeMap (`typeof(TSource)`/`typeof(TDestination)`
nunca resolviendo dentro de los propios métodos de instancia de una clase genérica — una capacidad
genuinamente nueva y general, rastreo de parámetros de tipo genérico a nivel de clase).

Un `AutoMapper` real y sin modificar ahora supera su propia inicialización estática, capa de
reflexión, maquinaria de selección de constructor, Y el paso de registro de TypeMap — pero su
propia llamada real `Mapper.Map<T>(source)` choca con un muro nuevo y más profundo: su propio árbol
de expresión de plan de mapeo compilado recurre mucho más allá de un límite de seguridad agregado
en esta Fase específicamente para convertir lo que solía ser un crash de proceso crudo en un error
gracioso — ver la propia sección "Encontrado, no arreglado" de la Fase 3.66 en
`docs/es/ROADMAP.md`. Rastreado como el [issue #1](https://github.com/arturoeanton/go-vmnet/issues/1).

Todavía no es un demo funcionando. % de checker subió levemente en la Fase 3.79 (los mismos
arreglos generales de `conv.u8`/`constrained.` que varios otros paquetes de este documento).

## Números agregados, y por qué el número por paquete importa más

- **Promedio simple entre los 19 paquetes: 95.9%** (subiendo del 95.8% antes de los propios
  arreglos generales de CIL/BCL de la Fase 3.79 — desreferenciar el receptor de `constrained.`,
  extensión con ceros de `conv.u8`, los operadores de comparación de `TimeSpan`, y media docena de
  nativos de `Regex`/`StringBuilder`/`Span<T>` — que movieron a doce de los diecinueve; subiendo del
  94.45% antes del propio barrido de todo el corpus de la Fase 3.74 antes de eso. Ver
  `docs/es/ROADMAP.md` para la propia metodología de cada Fase).
- **Promedio ponderado por métodos: ~98.4%** — pero está dominado por los propios 67.234 métodos
  analizados de `DocumentFormat.OpenXml` (55% de cada método analizado entre los 19 paquetes
  combinados) sentados en 100%. Un promedio ponderado responde "qué fracción de todas las llamadas
  a métodos analizadas en todo este corpus resuelve", que es un número real pero no el que predice
  si *tu* paquete específico va a funcionar — el **número por paquete de arriba es el que
  importa** para eso.
- El objetivo de trabajo para cada paquete acá es **97%+, individualmente** — no un promedio de
  todo el corpus. Un promedio puede esconder un paquete mal cubierto que se rompe en el instante en
  que alguien realmente depende de él, aunque otros paquetes lo compensen en la media.

Al momento de escribir esto, 8 de 19 paquetes están en o por arriba de esa vara:

| Paquete | % de checker |
|---|---|
| `DocumentFormat.OpenXml` | 100.0% |
| `Ardalis.GuardClauses` | 98.6% |
| `Humanizer.Core` | 98.4% |
| `NPOI` | 98.2% |
| `System.Text.Json` | 98.2% |
| `FluentValidation` | 98.2% |
| `ClosedXML` | 97.5% (cruzó la vara en la Fase 3.74) |
| `Polly` | 97.0% (cruzó la vara en la Fase 3.79) |

El resto son objetivos activos de endurecimiento, priorizados por cuánto están por debajo del 97%
y por cuánto uso real del mundo representan. `Jint` (96.7%) y `YamlDotNet`/`Serilog`
(96.2%/96.1%) son los más cercanos de los once restantes.

## La familia `Microsoft.Extensions.*` — frameworks oficiales de Microsoft, una medición separada y en curso

Distinto del corpus de 19 paquetes de arriba (el propio objetivo de compatibilidad de largo plazo
de este proyecto), la Fase 3.60 empezó a medir específicamente paquetes oficiales de Microsoft
`Microsoft.Extensions.*` — los building blocks del .NET moderno (inyección de dependencias,
configuración, logging, options, caché) sobre los que se construye cada app de ASP.NET
Core/worker-service. % de checker, profile `netstandard-lite`, con todas las dependencias
transitivas, actualizado después de la Fase 3.79 (la mayoría de esta familia se movió — los mismos
arreglos generales de `constrained.`/`conv.u8`/`TimeSpan`/`Regex`/`StringBuilder`/`Span<T>` que el
corpus principal de 19 paquetes de arriba):

| Paquete | % de checker |
|---|---|
| `Microsoft.Extensions.Configuration.Abstractions@8.0.0` | 100.0% |
| `Microsoft.Extensions.Options.ConfigurationExtensions@8.0.0` | 100.0% |
| `Microsoft.Extensions.Options@8.0.0` | 99.7% |
| `Microsoft.Extensions.Caching.Abstractions@8.0.0` | 99.2% (subió del 95.9%) |
| `Microsoft.Extensions.Logging@8.0.0` | 99.6% (subió del 98.1%) |
| `Microsoft.Extensions.Configuration.Json@8.0.0` | 98.8% |
| `Microsoft.Extensions.Logging.Abstractions@8.0.0` | 98.8% (subió del 97.8%) |
| `Microsoft.Extensions.Configuration@8.0.0` | 98.8% (subió del 97.2%) |
| `Microsoft.Extensions.Primitives@8.0.0` | 98.3% (subió del 96.9%) |
| `Microsoft.Extensions.Configuration.EnvironmentVariables@8.0.0` | 96.1% (bajó del 98.0% — una
  brecha preexistente y no relacionada de enumeración de `System.Environment.
  GetEnvironmentVariables`/`IDictionary`; no tocada por la Fase 3.79, y no una regresión que haya
  causado) |
| `Microsoft.Extensions.Configuration.FileExtensions@8.0.0` | 95.9% |
| `System.ComponentModel.Annotations@5.0.0` | 95.8% (subió del 94.1%) |
| `Microsoft.Extensions.DependencyInjection.Abstractions@8.0.0` | 95.5% (subió del 94.0%) |
| `Microsoft.Extensions.DependencyInjection@8.0.0` | 94.1% (**verificado con un demo real**, arriba) |
| `Microsoft.Extensions.Logging.Console@8.0.0` | 93.6% (subió del 90.6%) |
| `Microsoft.Extensions.Caching.Memory@8.0.0` | 92.6% (subió del 87.3%) |
| `Microsoft.Extensions.Configuration.Binder@8.0.0` | 90.1% (subió del 89.4%) |

Promedio simple: 96.9% (subiendo del 95.5%). El propio demo real de punta a punta de
`DependencyInjection` (ver arriba) es la prueba más fuerte hasta ahora: un paquete oficial real, sin
modificar, corriendo su propia lógica real de inyección de constructor, no solo una estimación
estática. El resto de esta familia es lo próximo en la fila para el mismo tratamiento de corrida
real.

## Metodología y reproducibilidad

Cada porcentaje de checker de arriba se midió de forma fresca contra el paquete/versión exacto
listado, incluyendo el propio grafo de dependencias transitivas de ese paquete (resuelto de la
misma forma en que `vm.LoadPackage` lo resuelve en tiempo de ejecución — el propio código real de
una dependencia no se reporta mal como no soportado solo porque se decodificó el DLL del paquete
de nivel superior únicamente). Reproducí cualquier número:

```bash
go build -o vmnet ./cmd/vmnet
./vmnet check package --profile=netstandard-lite <PackageId>@<Versión>
```

Cada demo real listado arriba es corrible directamente: `cd examples/<nombre> && go run .` — la
mayoría no necesita el SDK de .NET instalado en absoluto; unos pocos (donde interviene un pequeño
wrapper de C# compilado, solo en tiempo de desarrollo, anotado en el propio `README.md` de cada
demo) necesitan correr `dotnet build` una vez primero. Ver `docs/en/ROADMAP.md` para la historia
completa, fase por fase, de cada bug encontrado y arreglado para llevar cada uno de estos números
adonde está hoy — nada acá se esconde debajo de la alfombra.
