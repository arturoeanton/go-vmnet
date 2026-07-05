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
  de checker alto hacia "completamente verificado".

## Paquetes con demo real y funcionando (la señal más fuerte)

| Paquete | % de checker | Demo | Confianza |
|---|---|---|---|
| `DocumentFormat.OpenXml@3.1.1` | 100.0% (67.234 métodos, 7 marcados) | [`examples/openxml-demo`](../../examples/openxml-demo) | **Verificado.** Genera un `.docx` real desde cero; la salida se verifica abriéndola de vuelta con el SDK de .NET real, sin modificar — no solo que vmnet produjo *algunos* bytes. |
| `NPOI@2.8.0` | 97.9% (14.202 métodos, 292 marcados) | [`examples/npoi-demo`](../../examples/npoi-demo) | **Verificado.** Lee un archivo `.xls` legacy real de punta a punta (strings, números, una celda con fórmula `SUM`); queda una brecha cosmética conocida y documentada (el texto del rango de celdas de la fórmula renderiza puntos de código numéricos en vez de letras de columna — los *valores* de celda son correctos). |
| `Dapper@2.1.79` | 94.5% (1.047 métodos, 58 marcados) | [`examples/dapper-demo`](../../examples/dapper-demo), [`examples/sqlite-demo`](../../examples/sqlite-demo) | **Verificado, de dos formas.** `dapper-demo` corre el propio `SqlMapper.Query`/`Execute` real de Dapper contra un proveedor ADO.NET fake en memoria; `sqlite-demo` corre el mismo código real de Dapper contra el propio proveedor `Microsoft.Data.Sqlite` real y nativo en Go de vmnet, y después reabre de forma independiente el archivo `.db` resultante con el CLI real `sqlite3` y corre `PRAGMA integrity_check`. Quedan dos brechas arquitectónicas reales, permanentes y documentadas (una limitación de `typeof(T)` en métodos genéricos, y una feature de regex de Dapper que el motor RE2 de Go nunca puede compilar) — ver `docs/en/ROADMAP.md` Fase 3.52/3.53. |
| `ClosedXML@0.105.0` | 96.7% (10.444 métodos, 340 marcados) | [`examples/closedxml-demo`](../../examples/closedxml-demo) | **Verificado**, con una salvedad honesta: un pequeño wrapper de C# compilado provee un `IXLGraphicEngine` mínimo, porque el propio motor de métricas de fuente por defecto de ClosedXML choca contra una limitación arquitectónica real y profunda (sustitución de parámetro de tipo genérico dentro de los propios inicializadores de campo estático de una clase genérica) sin relación con leer datos de celda en sí. Lee un `.xlsx` real correctamente; también fue el sujeto de un cuelgue no determinista real y arreglado (Fase 3.44) — ahora estable a través de corridas repetidas. |
| `System.Text.Json@8.0.5` | 96.5% (3.577 métodos, 124 marcados) | [`examples/system-text-json-demo`](../../examples/system-text-json-demo) | **Verificado.** Parsea JSON real a través de la propia API real de `JsonDocument`, confirmado contra la salida real de .NET. |
| `Jint@3.1.3` | 95.8% (5.414 métodos, 228 marcados) | [`examples/jint-demo`](../../examples/jint-demo), [`examples/jint-nowrapper`](../../examples/jint-nowrapper) | **Verificado.** Corre un motor de JavaScript real de punta a punta — parsea código JS real, construye un AST real, lo evalúa, y devuelve un resultado real — tanto a través de un wrapper compilado como con cero pegamento de C#. La evidencia más fuerte de que vmnet maneja código real genuinamente no trivial y profundamente orientado a objetos, no solo bibliotecas pequeñas de métodos estáticos. |
| `Newtonsoft.Json@13.0.3` | 85.6% (4.064 métodos, 585 marcados) | [`examples/newtonsoft-json-demo`](../../examples/newtonsoft-json-demo) | **Verificado para el camino demostrado** (parseo real del DOM "LINQ to JSON" y acceso por indexador), pero el % de checker más bajo de cualquier paquete con demo — su superficie de tipado dinámico basada en `Dynamic`/`ExpandoObject` (`JValue+JValueDynamicProxy`) es una brecha real y no implementada que el demo no ejercita. No leas que el demo pase como "todo este paquete funciona". |
| `Microsoft.Extensions.DependencyInjection@8.0.0` | 94.1% (437 métodos, 26 marcados) | [`examples/di-demo`](../../examples/di-demo) | **Verificado para inyección de constructor real** — el propio contenedor de DI oficial de Microsoft resuelve un servicio cuyo constructor depende de otro servicio registrado, a través de su propia API real `ServiceCollection`/`ServiceProvider`/`GetRequiredService<T>()`, sin modificar. Llegar acá requirió tres fixes reales del intérprete (Fase 3.60): un tie-break de resolución de overloads de método causando una auto-recursión infinita, `typeof(T)` nunca resolviendo sobre el propio parámetro de tipo abierto de un método genérico, y una brecha de reflection entre paquetes. **Todavía no verificable en la práctica**: el propio camino rápido de árbol de expresión compilado de `DependencyInjection` (`ExpressionResolverBuilder`) — la Fase 3.65 construyó el evaluador general de árbol de expresión que esto necesita, pero leer el propio IL real muestra que el camino rápido es una optimización de mejor esfuerzo en segundo plano (encolada vía `ThreadPool` después de la 2ª resolución de un servicio, con cualquier fallo de compilación tragado silenciosamente) que se comporta idénticamente para un llamador real sea que tenga éxito o no; `di-demo` ejercita el OTRO camino de resolución, siempre activo (`CallSiteRuntimeResolver`), que no necesita `Expression.Compile()` en absoluto. |
| `FluentValidation@11.9.2` | 98.3% (1.289 métodos, 22 marcados) | [`examples/fluentvalidation-demo`](../../examples/fluentvalidation-demo) | **Verificado para validación de objetos real** — un validador real (`RuleFor`/`NotEmpty`/`WithMessage`) tanto acepta un objeto válido como rechaza uno inválido con el mensaje de error correcto. Llegar acá necesitó que `Expression<TDelegate>.Compile()` funcione de verdad para una clase real (aunque angosta) de árboles de expresión (Fase 3.64) — FluentValidation compila e invoca el lambda de acceso a propiedad para leer el valor que se está validando, no solo inspecciona su forma. **Limitación real y separada conocida**: los validadores de rango numérico (`GreaterThanOrEqualTo`, etc.) chocan con un bug de genéricos más profundo (la instancia cacheada de `Comparer<T>.Default` no se mantiene separada por instanciación genérica cerrada) — el demo deliberadamente solo ejercita los validadores de string que ya funcionan correctamente. |

## Paquetes medidos solo por el checker (todavía sin demo)

Que todavía no exista un demo no es una señal de alarma por sí sola — cada uno de los paquetes de
arriba empezó acá también. Sí significa que todavía nadie corrió el código real de este paquete
específico de punta a punta y comparó la salida contra .NET real; tratá el porcentaje como una
estimación de cobertura de lo que *probablemente* funcionaría, no como confirmación de que
funciona.

| Paquete | % de checker | Confianza |
|---|---|---|
| `Humanizer.Core@2.14.1` | 97.9% (1.597 métodos, 34 marcados) | Estimación de cobertura alta; no verificado por una corrida real. |
| `Ardalis.GuardClauses@5.0.0` | 97.5% (285 métodos, 7 marcados) | Estimación de cobertura alta; no verificado por una corrida real. |
| `Polly@8.7.0` | 95.5% (2.049 métodos, 92 marcados) | Estimación de cobertura alta; no verificado por una corrida real. |
| `NodaTime@3.3.2` | 94.3% (3.098 métodos, 176 marcados) | Estimación de cobertura alta; no verificado por una corrida real. |
| `YamlDotNet@18.1.0` | 94.9% (2.182 métodos, 112 marcados) | Buena estimación de cobertura; no verificado por una corrida real. |
| `Semver@2.3.0` | 92.9% (423 métodos, 30 marcados) | Buena estimación de cobertura; no verificado por una corrida real. |
| `SimpleBase@4.0.0` | 92.2% (258 métodos, 20 marcados) | Buena estimación de cobertura; no verificado por una corrida real. |
| `Serilog@4.3.1` | 92.1% (1.115 métodos, 88 marcados) | Buena estimación de cobertura; no verificado por una corrida real. |
| `CsvHelper@33.1.0` | 95.8% (1.393 métodos, 59 marcados) | La Fase 3.64 intentó una lectura real de CSV impulsada por el atributo `[Name]` y encontró una limitación genuina, distinta y más profunda: el propio caché interno de conversión de tipos de CsvHelper usa un `Dictionary` con clave de un struct con un campo array, y el propio hashing de clave de `Dictionary` de vmnet no tiene soporte para un componente de clave con forma de array. Una brecha real y específica, no arreglada — no solo una estimación sin verificar. |
| `MediatR@14.2.0` | 93.0% (441 métodos, 31 marcados) | Estimación de cobertura moderada; no verificado por una corrida real. |
| `AutoMapper@16.2.0` | 93.4% (2.319 métodos, 152 marcados) | La Fase 3.65 construyó el evaluador general de árbol-de-expresión-a-ejecutable (más un subsistema real de `System.Linq.Expressions.ExpressionVisitor`) que la propia generación de plan de mapeo de este paquete necesita, y lo usó para llevar un `AutoMapper` real y sin modificar a través de toda su inicialización estática, su capa de reflexión y su maquinaria de selección de constructor — doce fixes reales del intérprete encontrados y arreglados en el camino. Su propia llamada real `Mapper.Map<T>(source)` todavía lanza una `NullReferenceException` en las profundidades de la propia contabilidad de `TypeDetails` (una brecha genuinamente separada y más profunda, todavía sin causa raíz encontrada) — ver la propia sección "Encontrado, no arreglado" de la Fase 3.65 en `docs/es/ROADMAP.md`. Todavía no es un demo funcionando. |

## Números agregados, y por qué el número por paquete importa más

- **Promedio simple entre los 19 paquetes: 94.45%** (subiendo del 93.9% antes del barrido de
  prioridad de todo el corpus de la Fase 3.54-3.58 — ver `docs/en/ROADMAP.md` para la propia
  metodología de ese barrido: agregar los hallazgos del checker en TODO el corpus por callee real,
  no por paquete, así un callee marcado en muchos paquetes a la vez sale a la luz como lo de mayor
  apalancamiento para arreglar a continuación).
- **Promedio ponderado por métodos: ~97.8%** — pero está dominado por los propios 67.234 métodos
  analizados de `DocumentFormat.OpenXml` (55% de cada método analizado entre los 19 paquetes
  combinados) sentados en 100%. Un promedio ponderado responde "qué fracción de todas las llamadas
  a métodos analizadas en todo este corpus resuelve", que es un número real pero no el que predice
  si *tu* paquete específico va a funcionar — el **número por paquete de arriba es el que
  importa** para eso.
- El objetivo de trabajo para cada paquete acá es **97%+, individualmente** — no un promedio de
  todo el corpus. Un promedio puede esconder un paquete mal cubierto que se rompe en el instante en
  que alguien realmente depende de él, aunque otros paquetes lo compensen en la media. Al momento
  de escribir esto, 5 de 19 paquetes están en o por arriba de esa vara (`DocumentFormat.OpenXml`
  100.0%, `Humanizer.Core` 97.9%, `NPOI` 97.9%, `Ardalis.GuardClauses` 97.5%, `FluentValidation`
  97.0%); el resto son objetivos activos de endurecimiento, priorizados por cuánto están por
  debajo del 97% y por cuánto uso real del mundo representan.

## La familia `Microsoft.Extensions.*` — frameworks oficiales de Microsoft, una medición separada y en curso

Distinto del corpus de 19 paquetes de arriba (el propio objetivo de compatibilidad de largo plazo
de este proyecto), la Fase 3.60 empezó a medir específicamente paquetes oficiales de Microsoft
`Microsoft.Extensions.*` — los building blocks del .NET moderno (inyección de dependencias,
configuración, logging, options, caché) sobre los que se construye cada app de ASP.NET
Core/worker-service. % de checker, profile `netstandard-lite`, con todas las dependencias
transitivas, a la Fase 3.60:

| Paquete | % de checker |
|---|---|
| `Microsoft.Extensions.Configuration.Abstractions@8.0.0` | 100.0% |
| `Microsoft.Extensions.Options.ConfigurationExtensions@8.0.0` | 100.0% |
| `Microsoft.Extensions.Options@8.0.0` | 99.7% |
| `Microsoft.Extensions.Configuration.Json@8.0.0` | 98.8% |
| `Microsoft.Extensions.Logging@8.0.0` | 98.1% |
| `Microsoft.Extensions.Configuration.EnvironmentVariables@8.0.0` | 98.0% |
| `Microsoft.Extensions.Logging.Abstractions@8.0.0` | 97.8% |
| `Microsoft.Extensions.Configuration@8.0.0` | 97.2% |
| `Microsoft.Extensions.Primitives@8.0.0` | 96.9% |
| `Microsoft.Extensions.Configuration.FileExtensions@8.0.0` | 95.9% |
| `Microsoft.Extensions.Caching.Abstractions@8.0.0` | 95.9% |
| `Microsoft.Extensions.DependencyInjection.Abstractions@8.0.0` | 94.0% |
| `System.ComponentModel.Annotations@5.0.0` | 94.1% |
| `Microsoft.Extensions.Logging.Console@8.0.0` | 90.6% |
| `Microsoft.Extensions.Configuration.Binder@8.0.0` | 89.4% |
| `Microsoft.Extensions.DependencyInjection@8.0.0` | 89.5% (**verificado con un demo real**, arriba) |
| `Microsoft.Extensions.Caching.Memory@8.0.0` | 87.3% |

Promedio simple: 95.50%. El propio demo real de punta a punta de `DependencyInjection` (ver arriba)
es la prueba más fuerte hasta ahora: un paquete oficial real, sin modificar, corriendo su propia
lógica real de inyección de constructor, no solo una estimación estática. El resto de esta familia
es lo próximo en la fila para el mismo tratamiento de corrida real.

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
