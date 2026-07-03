# Arquitectura

Ver `docs/es/spec.md` (especificación completa) y `docs/es/ROADMAP.md` (plan de
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
`docs/es/adr/0002-package-layout.md`.

## Por qué pure-Go (sin CoreCLR en el núcleo)

Ver `docs/es/adr/0001-pure-go-core.md`.

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
dispatch, `try/catch/finally`, generics más allá de List/Dictionary, y
`DateTime`/`Guid` quedan para fases siguientes (`docs/es/ROADMAP.md`) — el IR
builder reporta cualquier opcode no soportado explícitamente en vez de
ejecutarlo mal. (`System.Array` se agregó en Fase 3.5 — ver más abajo.)

Además (Fase 2.5): el intérprete recupera cualquier panic en el borde
público (`Machine.Invoke`) en vez de crashear el proceso host, aplica
`MaxStackDepth` de verdad, `*vmnet.Assembly` es seguro para llamar desde
múltiples goroutines (`sync.RWMutex` sobre los caches de métodos/tipos,
verificado con `-race`), y `internal/pe`, `internal/metadata` e
`internal/il` tienen fuzz tests nativos de Go (corridos manualmente por
~16.8M ejecuciones combinadas sin panics).

Fase 3 (checker + NuGet) completa. `internal/checker` reutiliza el pipeline
real (no una reimplementación heurística aparte) para decidir si un
assembly es `compatible`/`partial`/`unsupported` por perfil
(`minimal`/`rules`/`netstandard-lite`). `internal/nuget` lee `.nupkg`/
`.nuspec` reales (forma corta y larga de TFM), resuelve dependencias
transitivas contra `api.nuget.org` (highest-version-wins, documentado como
simplificación), cachea en `.vmnet/packages/` y expone
`vm.NuGet().Add/Restore/Packages()` + `vm.LoadPackage(id)`. Certificado
contra 7 paquetes NuGet reales y populares (ver `docs/es/ROADMAP.md` para la
tabla completa); 3 de ellos tienen una función real ejecutando
correctamente a través de vmnet. El proceso de certificación encontró y
corrigió dos gaps reales: resolución de `MethodSpec` (llamadas a métodos
genéricos) y un bug de comparaciones sin signo (`.un` opcodes) que daba
resultados silenciosamente incorrectos, no solo "no soportado".

Fase 3.5 (endurecimiento + compatibilidad real de DLLs) completa. El motor
ahora soporta `System.Array` (`newarr`/`ldlen`/`ldelem.*`/`stelem.*`, solo
SZARRAY, con `Limits.MaxArrayLength`), punteros administrados para `ref`/
`out` (`ldarga`/`ldloca`/`ldelema`/`ldflda` + `ldind.*`/`stind.*` —
modelados como un `*runtime.Value` de Go apuntando dentro de un slice de
tamaño fijo, sin ningún caso especial en `Call`/`NewObj`) y campos
estáticos con `.cctor` perezoso (`ldsfld`/`stsfld`, `sync.Once` por
`Type`). Re-certificado contra los mismos 7 paquetes de Fase 3: el
promedio de métodos limpios subió de ~45.5% a ~56.8% (`docs/es/ROADMAP.md`
tiene la tabla completa por paquete). El proceso encontró y corrigió tres
bugs reales de concurrencia/correctitud que no existían como riesgo antes
de que `runtime.Type` empezara a cargar estado mutable: un deadlock de
reentrancia cuando un `.cctor` escribe su propio campo estático, una race
condition en el cache de tipos de `Assembly` que podía duplicar un `Type`
bajo acceso concurrente, y un `default(T)` incorrecto para campos value-type
nunca asignados explícitamente (ahora resuelto parseando la firma real del
campo — `metadata.ParseFieldSig`, nuevo). También detectó y corrigió dos
casos de "drift" en `internal/checker` (el perfil `minimal` no excluía
arrays/static fields como debía, y `sigShapeFindings` seguía marcando
`ref`/`out` como no soportado después de que sí se implementó) — ambos
atrapados por el propio test de dogfood del checker.

Fase 3.6 (primera sub-fase del camino a 85% de compatibilidad, ver
`docs/es/ROADMAP.md`) completa: opcode `switch` (ya decodificado desde Fase 1
pero nunca bajado a IR) y una tanda de nativos BCL de alto alcance
(`StringBuilder`, `String.Format`/`Substring`/indexador/`Equals`,
`Array.Empty`, `Double.IsNaN`, stubs de `CultureInfo`/`Environment`).
Expuso el primer caso concreto del límite ya documentado de "callvirt sin
vtable real": el compilador de C# emite `StringBuilder.ToString()` como
`callvirt Object::ToString`, confiando en el despacho virtual real del
CLR — vmnet lo resuelve estáticamente por el `MemberRef` declarado, así
que sin un parche dirigido en `objectToString` siempre corría el
`ToString` genérico. El despacho virtual real (jerarquía de tipos +
`isinst`/`castclass`) es Fase 3.8. Certificación (7 paquetes de Fase 3 +
Jint, el motor de JavaScript completo para .NET usado como target del
demo de "lenguaje dinámico" planeado): promedio de los 7 paquetes sube de
~56.8% a ~59.8%; con Jint incluido, ~60.3%.

Fase 3.7 (value types) completa: el motor ahora modela structs de verdad
(`runtime.KindStruct`, copiados por valor vía `Value.Clone()` en cada
punto donde un valor entra a un slot persistente, no compartidos por
referencia como `Object`) — `initobj`/`ldobj`/`stobj`/`constrained.`,
`newobj` empujando el valor en vez de una referencia para un value type,
y `System.Nullable`1`. Encontró y arregló dos bugs reales expuestos
apenas se probó contra un fixture con structs: locals de tipo struct
arrancaban sin inicializar (el compilador de C# confía en la garantía
`InitLocals` de la CLI y omite `initobj` cuando puede probar que el
struct se sobreescribe completo antes de usarse — ahora
`runtime.Method.LocalDefaults` los prezera igual que ya se hacía para
campos), y un deadlock de recursión en el lock de resolución de tipos de
Fase 3.5 (asumía que construir un tipo nunca necesita resolver otro,
falso en cuanto un campo/local de struct referencia otro tipo — rediseño
verificado con `TestStructsConcurrentResolve`). Certificación: promedio
de los 7 paquetes sube de ~59.8% a ~63.2%; con Jint, ~63.6%.

Fase 3.8 (jerarquía de tipos real) completa: `runtime.Type` ahora sabe su
`BaseTypeFullName` y sus `Interfaces` (spec §II.22.23, tabla
`InterfaceImpl`, sin usar hasta ahora), y `isinst`/`castclass` despachan
contra ese árbol real en vez de no existir en absoluto. Dos bugs reales
expuestos por el primer fixture con herencia: comparar una referencia
contra `null` (`<valor> ldnull cgt.un`/`ceq`, la forma compilada más común
de `is`/`!= null`/`== null`) fallaba con "mismatched value kinds" — ningún
fixture anterior había comparado explícitamente una referencia contra
`null` vía IL; y los campos declarados en una clase base simplemente no
existían en las instancias de sus subclases (`runtime.Type` nunca había
necesitado mirar más allá de su propio `TypeDef`) — ahora el tipo base se
resuelve recursivamente y sus campos se anteponen, igual que el layout de
memoria real de la CLR. Certificación (7 paquetes + Jint): promedio de los
7 sube de ~63.2% a ~64.2%; Jint da el salto grande, ~66.1% a ~74.4%
(despacho por tipo/casteos constantes en un motor de JS), ~63.6% a ~65.5%
con Jint incluido en el promedio de 8.

Fase 3.9 (delegates/closures) completa: `runtime.KindFunc` representa un
delegate como el nombre completo de su método target más un receptor
opcional, detectado **estructuralmente** (no por nombre de tipo) tanto en
`newobj` (`ldftn` + receptor + `.ctor(object, native int)`, la misma forma
para cualquier tipo delegate) como en el despacho de `Invoke` (por Kind
del receptor). Las closures no necesitaron ningún trabajo adicional: el
compilador de C# ya las baja a una clase real con las variables
capturadas como campos, que el modelo de objetos existente desde Fase 2
maneja sin casos especiales — verificado incluso con una closure que muta
un local capturado. El propio test de dogfood del checker atrapó de
inmediato el drift esperado (el checker no sabía que `Func`2::Invoke`
ahora resuelve, ya que nunca se registra en `bcl.Lookup`) — se agregó
`isDelegateType` reconociendo prefijos BCL conocidos más delegates
declarados localmente vía su `TypeDef` real. Certificación: promedio de
los 7 paquetes sube de ~64.2% a ~67.6% (~65.5% a ~68.8% con Jint);
`FluentValidation` (una librería de predicados/callbacks) da el salto más
grande medido en todo el camino a 85%, +13.4 puntos.

Fase 3.10 (`try`/`catch`/`finally` real) completa — la pieza
arquitectónicamente más grande del camino a 85%. `internal/il` gana un
parser para la tabla de cláusulas de manejo de excepciones (spec
§II.25.4.5-6, formas *small*/*fat*, nunca antes leída) y
`internal/interpreter` un motor de despacho completo: un
`*runtime.ManagedException` que sale de la ejecución de un método (por
`throw`, `rethrow`, o propagado desde cualquier llamada anidada) se busca
contra los handlers del método del más interno al más externo, un
`catch` matchea reusando el mismo walk de jerarquía real de Fase 3.8 (no
solo comparación exacta de tipo), y cualquier `finally`/`fault` en el
camino corre siempre antes de que la excepción siga su curso. El
refactor fue deliberadamente de bajo riesgo: el `switch` gigante de ~40
casos existente se extrajo intacto a su propia función (`runFrame`), sin
tocar la lógica interna de ningún caso previo — todo el riesgo nuevo
quedó concentrado en el mecanismo de despacho, no esparcido por el
archivo. Certificación: promedio de los 7 paquetes sube apenas de
~67.6% a ~67.7% (~68.8% a ~69.0% con Jint) — movimiento chico y
esperado, ya que excepciones solo "limpia" un método si era el único
obstáculo; el valor de esta fase es arquitectónico, no un salto grande
en el número.

Fase 3.11 (`foreach`/enumeradores) completa — re-priorizada con datos:
el plan original apuntaba a DateTime/Span, pero el mismo probe de
findings-por-target de siempre mostró que `foreach` sobre
`List<T>`/`Dictionary<K,V>` **no funcionaba en absoluto** (Fase 2 solo
daba acceso indexado) y que eso era mucho más ancho (7-8/8 paquetes)
que DateTime/Span (2-5/8). `List<T>.Enumerator`/`Dictionary<K,V>.
Enumerator`/`KeyValuePair<K,V>` se modelan como value types sintéticos
(mismo patrón que `Nullable`1` de Fase 3.7), confirmado contra IL real
antes de escribir el native. Encontró y arregló un riesgo real antes de
que causara daño: `List`1.Enumerator::MoveNext` resolvía a
`"Enumerator"` sin calificar (`resolveTypeToken` nunca había necesitado
caminar `ResolutionScope` para un `TypeRef` anidado), lo que habría
secuestrado silenciosamente cualquier otro tipo `Enumerator` en
cualquier ensamblado cargado (Jint tiene los suyos propios) —
`qualifyTypeRefName` arma `Tipo1+Tipo2` igual que `Type.FullName` real.
También agregó `IDisposable::Dispose` (no-op), `EqualityComparer`1.
Default` (reusa `valuesEqual`/`valueHash` de Fase 3.7), `Math.Min`/`Max`
y `String.Join`. Certificación: promedio de los 7 paquetes sube de
~67.7% a ~68.8% (~69.0% a ~70.3% con Jint). DateTime/Span/Memory quedan
documentados como Fase 3.12, no descartados.

Fase 3.12 (`System.DateTime`, `Span<T>`/`ReadOnlySpan<T>`/`Memory<T>`/
`ReadOnlyMemory<T>`) completa — el plan pospuesto desde 3.11.
`DateTime` se modela como un value type sintético de un campo (`ticks
int64`, la misma representación que usa la CLR); los cuatro tipos Span
comparten un solo shape de 3 campos (`backing`, `start`, `length`), una
vista defensiva sobre un `runtime.Array` o los caracteres de un string
— vmnet no tiene punteros sin gestionar para modelar la semántica real.
Tres bugs reales encontrados y arreglados: el indexador de `Span<T>`
está declarado `ref T`, no `T` — tanto la lectura como la escritura
compilan al mismo `call get_Item` seguido de `ldind`/`stind` (no existe
un `set_Item` separado), así que devolver el valor en vez de una
referencia rompía todo escritor; `ReadOnlySpan<char>.ToString()`
despacha vía `constrained.`+`callvirt Object::ToString` (mismo patrón
que `StringBuilder` en Fase 3.6), no una llamada directa; y la
conversión ticks↔`time.Time` desbordaba silenciosamente `time.Duration`
(un `int64` de nanosegundos, válido solo ~292 años) al puentear el
epoch de .NET (año 1) con una fecha moderna — arreglado anclando en
aritmética de segundos Unix en vez de una duración. Certificación:
promedio de los 7 paquetes sube de ~68.8% a ~76.3% (~70.3% a ~76.9% con
Jint) — el salto más grande de toda la secuencia 3.6-3.12, dominado casi
por completo por `Humanizer.Core` (+34.4 puntos: es una librería de
"humanizar" fechas, DateTime era su único bloqueador real). Con 76.9%
el criterio de cierre firme de 85% todavía no se alcanza; queda al
menos una Fase 3.x más antes de Fase 4.

Fase 3.13 (`foreach` sobre colección tipada como interfaz + paquete de
wins baratos) completa. El probe post-3.12 mostró que
`IEnumerable`1::GetEnumerator`/`IEnumerator`1::get_Current`/
`IEnumerator::MoveNext` eran el hallazgo más ancho del proyecto entero
(7/8 targets) — exactamente lo que Fase 3.11 había dejado afuera por
necesitar despacho virtual real. `Machine.call` gana un fallback: cuando
el nombre declarado en el sitio de `callvirt` (baked in en tiempo de
compilación, p.ej. `IEnumerable`1::GetEnumerator`, ya que vmnet no tiene
vtable) no resuelve, reintenta una vez contra el tipo concreto real del
receptor (`receiverTypeName` — `Struct.Type`/`Obj.Type` para la mayoría
de valores, `bcl.NativeTypeName` para colecciones nativas sin
`runtime.Type` propio como `List<T>`), cubriendo uniformemente tanto
colecciones BCL accedidas por interfaz como clases del plugin que
implementan una interfaz. Un iterador `yield return` necesitó una pieza
más: su `GetEnumerator`/`Current` compila como implementación *explícita*
de interfaz (nombre mangled tipo
`"IEnumerable<System.Int32>.GetEnumerator"`, confirmado con `strings`
sobre el DLL antes de asumir nada) — `ExplicitImplResolver` camina la
tabla `MethodImpl` (spec §II.22.27, mismo patrón que `InterfaceImpl` de
Fase 3.8) para encontrarlo.

El fallback aplicado sin más expuso una recursión infinita real: un
constructor de excepción propia encadenando `: base(message)` (un `call`
plano, no `newobj` — solo el tipo exacto se `newobj`ea) se redirigía a sí
mismo, agotando la pila. Causa raíz: el fallback nunca debía aplicar a un
`call` no-virtual, solo a `callvirt` — arreglado propagando el flag
`Virtual` (ya existente en la IR, nunca antes threaded hasta
`Machine.call`) y condicionando el fallback a él. Arreglar esto de fondo
reveló que `System.Exception::.ctor` nunca había resuelto para una
subclase propia del plugin en absoluto (mismo patrón "solo newobj estaba
cubierto" que ya había mordido a DateTime/Nullable`1`), y que una vez
resuelto, el nombre de tipo quedaba pegado al tipo *base* en vez del
derivado real, y que el matching de `catch` no caminaba la jerarquía real
del plugin — los tres arreglados en cadena (base-ctor chaining registrado
también como `call` plano; `TypeName` tomado del `Obj.Type` real del
receptor; `nativeMatches` — ahora método de `Machine` — alternando entre
el mapa fijo de excepciones BCL y el `BaseTypeFullName` real del plugin
en la misma caminata).

El paquete de wins baratos (medido, no adivinado) suma `String`
(`IsNullOrEmpty`/`Split`/`StartsWith`/`IndexOf`/`Replace`/`Trim`/...),
`Char` (`IsUpper`/`IsDigit`/`ToString`/...), `Int32.ToString`, extras de
`List<T>`/`Dictionary<K,V>` (`set_Item`/`ToArray`/`AddRange`/`Contains`/
`TryGetValue`), y confirma que `Nullable`1::.ctor` necesitaba el mismo
fix de "asignación directa a un local" (`ldloca`+`call .ctor` sin
`newobj`) que `DateTime` en Fase 3.12. Certificación: promedio de los 7
paquetes sube de 76.3% a 79.0% (76.9% a 79.4% con Jint) — movimiento
sólido y repartido, sin un salto dominante único.
Con 79.4% el criterio de cierre firme de 85% todavía no se alcanza; el
hallazgo más ancho restante es reflection-lite (`ldtoken`/`GetType`/
`Type`, 5-6/8), candidato natural para la próxima sub-fase.

Fase 3.14 (reflection-lite: `ldtoken`/`typeof(T)`, `Object.GetType()`,
`System.Type`) completa — exactamente el hallazgo anotado arriba.
`typeof(T)` compila siempre `ldtoken T` + `call Type::
GetTypeFromHandle(RuntimeTypeHandle)`, confirmado contra IL real; vmnet
no modela `RuntimeTypeHandle` como Kind propio — `ir.LoadTypeToken`
empuja directamente el `System.Type` real, y `GetTypeFromHandle` es la
función identidad, así el par se comporta como el CLR sin una
representación intermedia. `System.Type` es un objeto native-backed
mínimo (`nativeTypeInfo{FullName string}`) sin identidad de referencia
real — cada comparación (`op_Equality`, `Equals`) es por el string
`FullName`, nunca por puntero Go, lo único observable desde la API
pública de `Type` de todos modos. `Object.GetType()` reusa la misma
inspección de "forma real en runtime" que `isAssignableTo` (Fase 3.8) ya
hace para `isinst`/`castclass`. Certificación: promedio de los 7
paquetes sube de 79.0% a 80.1% (79.4% a 80.5% con Jint) — movimiento
más chico que Fase 3.13 (reflection es más disperso que el despacho por
interfaz), pero limpio: `Semver`/`SimpleBase` no se mueven en absoluto
(sin reflection en su superficie), los cuatro paquetes que sí usan
`GetType()`/`typeof` con volumen real (`FluentValidation`,
`System.Text.Json`, `Newtonsoft.Json`, `Jint`) sí suben. Con 80.5% el
85% todavía no se alcanza; LINQ es ahora el hallazgo más ancho
no-async/no-regex restante (~174 casos en 4-5/8, Select/Any/ToList/
Where/ToArray), viable desde que existen delegates (3.9), enumeradores
reales (3.11) y despacho por interfaz (3.13) — candidato natural para
la siguiente sub-fase.

Fase 3.15 (LINQ: `System.Linq.Enumerable`) completa. Descubrimiento
central: los métodos de `Enumerable` no pueden ser `bcl.Native` planos —
cada uno necesita invocar el delegate argumento (`m.invokeFunc`) y/o
recorrer una fuente `IEnumerable<T>` arbitraria vía el protocolo real
`GetEnumerator`/`MoveNext`/`get_Current` (`m.call`, reusando el fallback
de despacho por interfaz de 3.13), ninguno de los dos disponible a una
función `func(args) (Value, error)` sin `Machine`. Se agregó un registro
paralelo (`linqRegistry`, `internal/interpreter/linq.go`) de nativos
"Machine-aware", mismo tipo de plumbing nuevo que `ExplicitImplResolver`
ya había necesitado en 3.13. `Select`/`Where`/`Any`/`All`/`ToList`/
`ToArray`/`FirstOrDefault` son eager (materializan de inmediato), no los
iteradores perezosos reales de la CLR — una llamada encadenada
(`xs.Where(...).Select(...).ToList()`) igual se comporta idéntica desde
el punto de vista del llamador, porque cada resultado de LINQ se envuelve
como un `List<T>` real vía `bcl.NewListValue` (mismo patrón que
`bcl.NewTypeValue` de 3.14). `enumerateAll` unifica la fuente: camino
rápido para array/`List<T>` nativo (ya son un slice de Go), protocolo
real de iteración para cualquier otra cosa — el mismo mecanismo que
`foreach` ya usa, no una segunda implementación de iteración. Certifi-
cación: promedio de los 7 paquetes sube de 80.1% a 80.5% (80.5% a 80.9%
con Jint) — movimiento más chico que el volumen crudo de hallazgos
(~174 casos) sugería, mismo patrón ya visto en Fase 3.10: LINQ solo
"limpia" un método si era el único obstáculo, y varios métodos que usan
LINQ en estos paquetes también tocan reflection profunda o regex, que
siguen sin soporte. Con 80.9% el 85% todavía no se alcanza.

Fase 3.16 (`Type::IsAssignableFrom`) completa — el segundo hallazgo más
ancho de reflection dejado explícitamente afuera de 3.14, ahora mecánico
gracias al registro Machine-aware que 3.15 ya había generalizado
(`linqRegistry` renombrado a `machineRegistry`, sin cambio de
comportamiento). `typeIsAssignableFrom` re-deriva `isAssignableTo` (Fase
3.8) partiendo de un nombre de tipo en vez de un `Value`/`Kind` ya
conocido, resolviendo el `TypeDef` real del candidato y caminando con
`m.typeMatches`. Certificación: 80.5% a 80.6% (80.9% a 81.0% con Jint) —
movimiento mínimo, mismo patrón de "no era el único obstáculo" ya visto
en LINQ. Con 81.0% el 85% todavía no se alcanza.

Fase 3.17 (bug crítico: colisión de nombres de tipos anidados propios
del plugin + `System.Lazy<T>`) completa — el salto más grande de la
secuencia 3.6-3.17 después de 3.12, y no de una feature nueva sino de un
arreglo de corrección. El compilador de C# emite una clase cache de
lambdas no-capturadoras (literalmente llamada `<>c`) POR CADA tipo
contenedor que tiene alguna — un ensamblado con lambdas en dos clases
distintas termina con dos TypeDefs separados, ambos llamados `<>c`
(mismo Name, Namespace vacío, ya que un tipo anidado siempre lo tiene).
Todo el código que resolvía un token TypeDef a nombre completo
colapsaba directo a `Namespace.Name` sin caminar la tabla `NestedClass`
(spec §II.22.32) — la MISMA clase de bug que Fase 3.11 ya había
arreglado para TypeRef (tipos anidados foráneos) pero había dejado
explícitamente documentado como riesgo preexistente para TypeDef (tipos
del propio plugin). El riesgo se volvió real: al agregar un segundo
archivo con lambdas y correr la suite con `-count=3` (no solo una vez),
`ldsfld` empezó a resolver contra el `<>c` equivocado. Arreglado con
`metadata.EnclosingClass` (nuevo, lee `NestedClass`, sin lector previo),
`qualifyTypeDefName`/`QualifyTypeDefName` (nuevo, camina la tabla
recursivamente igual que `qualifyTypeRefName` ya hace con
`ResolutionScope`, reemplazando el `Qualify` directo en 8 sitios reales
across `internal/ir/builder.go`, `assembly.go` e
`internal/checker/analyzer.go`), `metadata.FindTypeDef` extendido para
aceptar nombres `"+"`-calificados en el round-trip, y
`runtime.Type.QualifiedName` (nuevo campo) para que `fullTypeName`
(despacho por interfaz de 3.13, catch-matching de excepciones) no
reconstruya y pierda la calificación de nuevo. Impacto medido: 80.6% a
82.8% (81.0% a 83.0% con Jint) — `SimpleBase` solo saltó +14.7 puntos,
confirmando que cualquier paquete real con más de una clase usando
lambdas (patrón extremadamente común) ya estaba silenciosamente
resolviendo contra el `<>c` equivocado en algún punto. De paso, se
agregó `System.Lazy<T>` (factory `Func<T>` invocado exactamente una vez,
cacheado, con el lock de la instancia sostenido durante todo el cómputo
para serializar correctamente accesos concurrentes al mismo campo
estático — el uso dominante real de `Lazy<T>`), cuyo fixture (agregado
junto al de LINQ) fue lo que expuso el bug en primer lugar. Con 83.0% el
85% todavía no se alcanza, pero el margen se cerró considerablemente.

Fase 3.18 (segundo paquete de wins baratos + `IDictionary<K,V>` por
interfaz) completa. `System.String::.ctor` necesitó su propio camino en
`newObj` en vez del registro `bcl.LookupCtor` normal: un string en vmnet
es un `KindString` plano, no un `KindObject` — envolverlo en
`runtime.ObjRef` como cualquier otro ctor nativo habría sido incorrecto.
`Interlocked.CompareExchange` implementa la semántica real de
comparar-e-intercambiar (no un stub que siempre asigna), aunque vmnet no
tenga un modelo de memoria multi-core real contra el cual ser atómico.
`IDictionary<K,V>::set_Item`/`get_Item`/`TryGetValue`/`ContainsKey` se
agregan al allowlist de despacho por interfaz de Fase 3.13 sin código
nuevo — el runtime ya los resolvía gratis reusando los natives de
`Dictionary`2` existentes. `System.Convert::` se promueve de
`netstandard-lite` a `rules` (mismo tratamiento que `System.Type::` en
Fase 3.14), así que `netstandard-lite` queda como copia explícita de
`rules` en vez de una lista adicional. Certificación: 82.8% a 83.3%
(83.0% a 83.5% con Jint). Con 83.5% el 85% todavía no se alcanza, pero
el margen restante es chico — lo que queda con volumen real es async
(fuera de alcance permanente), regex (decisión de diseño pendiente), y
reflection más profunda sobre genéricos/enums.

Fase 3.19 (`HashSet<T>`, `Stack<T>`, `TimeSpan`) completa — tres
superficies nuevas con volumen moderado (4/8), no extensiones de algo
existente. `HashSet<T>` deduplica/busca por barrido lineal con
`valuesEqual`, no un `map` real de Go (`runtime.Value` no es
intrínsecamente hasheable en el sentido de clave de mapa), misma
simplificación pragmática que `List<T>.Contains`. `TimeSpan` repite el
diseño de `DateTime` (Fase 3.12): value type de un campo `ticks int64`,
registrado también como `call` plano para la asignación directa a un
local — esta vez anticipado por el patrón ya conocido, no descubierto
por sorpresa. Certificación: 83.3% a 83.5% (83.5% a 83.7% con Jint).
Falta ~1.3-1.5 puntos para el 85%.

Fase 3.20 (`System.Text.RegularExpressions`) completa. Compila patrones
con el motor RE2 de Go, no el motor real de .NET — coinciden en la
enorme mayoría de uso real, pero RE2 no tiene backreferences ni
lookaround; un patrón que los use falla al compilar con un error claro,
no un resultado plausible-pero-incorrecto. Bug real encontrado al correr
el fixture: la jerarquía real es `Capture -> Group -> Match`, y `Match`
hereda `Success`/`Value` de `Group`/`Capture` sin sobreescribirlos —
`m.Success`/`m.Value` compilan a `callvirt Group::get_Success`/`callvirt
Capture::get_Value`, nunca contra `Match::` directamente. La primera
versión registró `Match::get_Success`/`get_Value` y nunca se llamaban en
absoluto; arreglado con un único accesor compartido que lee
`(Success, Value)` tanto de un grupo de captura como del match completo
(Grupo 0), registrado bajo los nombres reales. `Match.Groups[i]` usa
`FindStringSubmatchIndex` (pares de índices), no `FindStringSubmatch`
(strings planos), para distinguir un grupo opcional que no participó
(`Success = false`) de uno que capturó una cadena vacía. Certificación:
83.5% a 83.7% (83.7% a 83.9% con Jint) — regex casi nunca es el único
obstáculo de un método real, mismo patrón que LINQ. Falta ~1.1-1.3
puntos para el 85%.

Fase 3.21 (tercer paquete de wins baratos) completa — **cruza el 85%**.
`DateTime.Kind` necesitó agregar un segundo campo (`kind`,
`DateTimeKind` como `int32`) al value type sintético de un campo que
existía desde Fase 3.12. `IList<T>`/`IReadOnlyList<T>`/
`IReadOnlyCollection<T>`/`IEqualityComparer<T>` se agregan al allowlist
de despacho por interfaz de Fase 3.13 sin código nuevo. Certificación:
83.7% a **85.1%** (83.9% a **85.3%** con Jint) — se cruza el criterio de
cierre firme original de la Fase 3.6+. `Semver` salta +5.9 puntos por sí
solo (`Int32.Parse`/`TryParse` son su superficie central). Con el
objetivo ya revisado a ~97% ("100% puede ser 97%", BCL endurecido,
documentación al día), la secuencia de sub-fases continúa más allá de
este cruce.

Fase 3.22 (`async`/`await`, modelo síncrono) completa — el salto más
grande de toda la secuencia 3.6-3.22. Un análisis de techo (arreglar
TODO lo no-async) dio un techo de 89.6%/89.3% con Jint, por debajo del
objetivo de ~97% — con async explicando la mayoría de lo que quedaba en
`Newtonsoft.Json`/`System.Text.Json`/`SimpleBase`, se revisó la decisión
de dejarlo permanentemente afuera. Decisión de diseño: cada `Task` que
cualquier native produce está completado desde que se crea (vmnet no
tiene scheduler real) — consecuencia clave: el `MoveNext()` que el
compilador genera para cualquier `async` revisa `awaiter.IsCompleted` en
cada `await`, y como siempre da `true` en este modelo, el branch que
suspende nunca se toma en la práctica. Una sola llamada a `MoveNext()`
corre el método completo de punta a punta, sin importar cuántos
`await`s tenga. No hizo falta tocar el intérprete: el cuerpo de
`MoveNext()` es IL común (campos, branches, un try/catch/finally real),
ya soportado desde Fase 1/3.10 — todo el trabajo fue superficie de BCL
(`AsyncTaskMethodBuilder`, `Task`/`TaskAwaiter`, `Task.FromResult`/
`Run`). Los cuatro casos del fixture (awaits secuenciales, excepción
tras un await, método void, cadena anidada) funcionaron de punta a
punta en el primer intento contra IL real, sin ningún bug encontrado
durante la verificación — a diferencia de casi todas las fases
anteriores. Certificación: 85.1% a **88.1%** (85.3% a **88.0%** con
Jint) — `SimpleBase` (+8.5) y `Newtonsoft.Json` (+6.2) confirman la
hipótesis del análisis de techo. Con 88.0% el objetivo de ~97% todavía
no se alcanza.

Fase 3.23 (cuarto paquete de wins baratos) completa, con dos bugs
reales de corrección encontrados verificando contra IL real, no solo
superficie nueva. Primero: el despacho por interfaz de Fase 3.13 podía
dejar la pila corta cuando la firma real del método concreto difiere de
la interfaz declarada (`IList.Add` devuelve `int`, redirige a
`List`1::Add`, que es `void`) — causaba un panic real
(`index out of range`), arreglado usando la firma declarada en el sitio
de llamada (`in.HasReturn`) como autoridad para decidir si empujar un
resultado, no lo que reporte el callee finalmente resuelto. Segundo:
`fieldSlot` nunca manejaba un receptor struct pasado por valor directo
(sin puntero administrado) — hasta ahora todo acceso a campo de struct
visto usaba `ldloca`+`ldfld`, pero `ValueTuple`'s `t.Item1 + t.Item2`
reveló que el compilador real a veces emite `ldloc`+`ldfld` plano para
el segundo acceso en la misma expresión, legal per spec §III.4.10 pero
nunca antes ejercitado. También se descubrió que ningún value type
nativo de BCL había necesitado un campo *estático* real hasta
`TimeSpan.Zero` (un campo público, no propiedad) — `runtime.
NewValueType` no lo soporta en absoluto, así que `timeSpanType` se
reconstruyó vía `runtime.NewType` + `SetStaticField`, con un fallback
nuevo en `resolveTypeByFullName` para que tipos BCL nativos sin
`TypeDef` en el ensamblado del plugin puedan resolverse igual.
Certificación: 88.1% a 88.7% (88.0% a 88.5% con Jint) — movimiento chico
esperado para wins dispersos, pero el valor real son los dos bugs de
corrección (uno de ellos era un riesgo silencioso desde Fase 3.13 en
cualquier despacho por interfaz con firma incompatible). Con 88.5% el
objetivo de ~97% todavía no se alcanza.

Fase 3.24 (quinto paquete de wins baratos) completa:
`ConcurrentDictionary`1` (mutex real + `GetOrAdd` con overload de valor
literal y de factory delegado, resuelto por el registro Machine-aware ya
que invocar el factory necesita `Machine`), `Regex.Replace` (mismo motor
RE2 y desambiguación por `Kind` que `IsMatch`/`Match`), y el primer
soporte real de delegado multicast del proyecto —
`Delegate.Combine`/`Remove`, respaldado por un campo `Chain []*Func`
nuevo en `runtime.Func` que `invokeFunc` recorre después del target
propio, descartando resultados intermedios igual que
`MulticastDelegate.Invoke` real. También `System.Array::GetEnumerator` +
un enumerador de referencia real (a diferencia del struct inlineado de
`List<T>`), y un bug real encontrado verificando `(Action)Delegate.
Combine(...)` contra IL real: `isAssignableTo` no tenía ningún caso para
`KindFunc` — un delegado nunca había pasado por un `castclass` real
hasta ahora — arreglado aceptando cualquier cast/isinst delegado-a-
delegado sobre un valor de delegado real, ya que `runtime.Func` no lleva
un tipo de delegado declarado propio contra el cual chequear (se detecta
estructuralmente, no por tipo, desde Fase 3.9). Certificación: 88.7% a
**88.9%** (88.5% a **88.7%** con Jint) — el movimiento más chico de la
secuencia de wins baratos, esperado: son superficies reales pero
angostas. El probe post-3.24 confirma que la cola de superficie barata
de volumen se agotó: los hallazgos con más ancho (4-5/8 paquetes) están
ahora concentrados casi enteramente en reflexión profunda
(`Type.MakeGenericType`/`GetInterfaces`/`get_IsGenericType`/
`GetMethod(s)`/`GetProperties`/`GetConstructors`, `System.Reflection.
MethodInfo`/`PropertyInfo`/`ParameterInfo`/`MemberInfo`/`Assembly`,
`MethodBase.Invoke`, `Activator.CreateInstance`, `System.Enum.*`) — el
candidato natural para la próxima fase, pero un bloque
arquitectónicamente más grande (introspección respaldada por metadata
real + invocación dinámica) que cualquier cosa atacada hasta ahora. Con
88.7% el objetivo de ~97% todavía no se alcanza.

Fase 3.25 (reflexión profunda, primera porción: introspección de
`System.Type`) completa. Cambio de raíz en `internal/metadata/
signatures.go`: `SigType` retiene ahora sus argumentos genéricos (`Args
[]SigType`, antes descartados al parsear una instanciación genérica) —
aditivo puro, todo consumidor existente sigue ignorándolos. Nuevo
`resolveClosedTypeSpecName`/`sigTypeFullName` en `internal/ir/builder.go`,
usado solo por `ldtoken` (`typeof(T)`): `typeof(List<int>)` retiene ahora
sus argumentos como `"List\`1[[System.Int32]]"`, mientras que
`initobj`/`ldobj`/`stobj`/resolución de `MemberRef` siguen sin
necesitarlos. Sobre esa base: `Type.IsGenericType`/
`GetGenericTypeDefinition`/`GetGenericArguments`/`MakeGenericType`
(parseo puro de corchetes, sin acceso a `Machine`),
`Nullable.GetUnderlyingType`. `runtime.Type` ganó `IsEnum`/`IsInterface`
(antes solo `IsValueType`, que colapsaba struct y enum juntos) —
`assembly.go` los puebla desde el `TypeDef` real (`IsInterface` lee el
bit `TypeAttributes.Interface` directo de `Flags`, el único de los tres
que no se deriva de `Extends`). Con eso: `Type.IsValueType`/`IsEnum`/
`IsInterface`/`BaseType`/`GetInterfaces()`/`GetType(string)` — mapa fijo
de primitivos/interfaces BCL conocidos primero, `TypeDef` real de plugin
después (Machine-aware, `internal/interpreter/reflection.go`). Bug real
encontrado con el primer `enum` de plugin de todo el proyecto
(`TrafficLight`, fixture nuevo): `buildType` entraba en recursión
infinita, porque un miembro de enum es un campo `static literal`
autorreferenciado en IL real (`static literal valuetype TrafficLight Red
= int32(0)`, no `int32`) — `fieldOrLocalDefault` intentaba calcular su
default recursando en `resolveTypeByFullName` sobre el mismo tipo que
todavía no había terminado de construirse. Arreglado saltando el cálculo
de default para cualquier campo `FieldAttributes.Literal` (su valor real
vive en la tabla `Constant`, que vmnet todavía no lee). Certificación:
88.7% a **89.0%** (con Jint) — `System.Text.Json`/`Newtonsoft.Json`
concentran la mayor parte del movimiento. El resto del bloque de
reflexión (`System.Reflection.MethodInfo`/`PropertyInfo`/
`ConstructorInfo` como objetos reales, `MethodBase.Invoke`/
`Activator.CreateInstance`, `Type.GetMethod(s)`/`GetProperties`/
`GetConstructors`/`GetFields`, `Enum.GetValues`/`GetNames`/`IsDefined`)
queda confirmado como la superficie de mayor volumen restante — un
diseño más grande (jerarquía de objetos respaldada por metadata real más
invocación dinámica genuina), candidato para Fase 3.26. Con 89.0% el
objetivo de ~97% todavía no se alcanza.

Fase 3.26 (`System.Enum.GetValues`/`GetNames`/`IsDefined`/`ToObject`)
completa. Necesitaba un dato que vmnet nunca había leído: el valor real
de cada miembro de un enum vive en la tabla `Constant` de metadata,
sin parser hasta ahora. `internal/metadata/constant.go` (nuevo):
`constantForField` (búsqueda lineal — sin índice directo de RID de campo
a fila `Constant` en el formato mismo), `decodeConstantInt64`,
`EnumMembers(typeRID)`. `ConstantRow`/`md.Constant(rid)` ya existían en
`resolver.go` de una fase anterior, nunca conectados a nada — esta fase
los usa por primera vez. Nuevo `EnumResolver` en la cadena `Machine`
(mismo patrón que `ExplicitImplResolver`, Fase 3.13), conectado vía
`asm.resolveEnumMembers` — solo resuelve un enum declarado por el propio
plugin (un `TypeDef` real); un enum solo-BCL como `System.DayOfWeek`
sigue sin funcionar (vmnet no tiene base de datos de miembros de enums
BCL). `Enum.GetValues`/`GetNames` no necesitaron ningún cambio en el
intérprete — el array resultante fluye directo a través de
`System.Array::GetEnumerator` (Fase 3.24). Certificación: 89.2%/89.0%
sin cambio a nivel de tabla, pero real bajo el capó — el conteo de
*findings* individuales bajó en cada paquete tocado (`Enum.*` dejó de
aparecer como hallazgo), pero la métrica es por-método y esos mismos
métodos casi siempre llaman también algo del bloque grande de reflexión
todavía pendiente, así que siguen contando como "método con hallazgos".
Confirma con más fuerza que el único camino real hacia ~97% pasa ahora
por `MethodInfo`/`PropertyInfo`/invocación dinámica.

Fase 3.27 (resolución multi-ensamblado + demo real de
`Jint.Engine.Evaluate()`) completa — el cambio de arquitectura más grande
desde Fase 3. `Call()` solo invocaba métodos estáticos de un único
ensamblado; correr Jint de verdad necesita resolver símbolos a través de
su propia cadena de dependencias NuGet (Jint → Esprima → System.Memory →
...). `Assembly.deps []*Assembly` + `WithDependencies`, `vm.LoadPackage`
cargando el grafo transitivo completo automáticamente, y
`runtime.Resolvers` (5 resolvers agrupados por `*runtime.Method`,
intercambiados en `Machine.invoke` durante cada llamada) para que cada
método resuelva contra el ensamblado real que lo produjo — no el punto
de entrada original — evitando colisiones de nombre entre ensamblados
(`<PrivateImplementationDetails>` existe por separado en `Jint.dll` y
`Esprima.dll`). Además: resolución real de overloads por aridad + Kind +
nombre de tipo exacto/subtipo (`pickMethodOverload`/`scoreParamMatch`,
`assembly.go`) con un descalificador duro para combinaciones de forma
imposibles en CIL real (`hasHardShapeMismatch`: un `KindObject` nunca
puede ser un `SigValueType` sin una conversión visible en el IL);
despacho virtual real que prueba el tipo concreto del receptor primero y
sube por toda la cadena de herencia, no solo como fallback tras un "no
resuelto"; `newarr` sembrando arrays de value type con su default real en
vez de `Null()` ciego; y un bug de aliasing real en `newObj`/
`runtime.NewStruct` (copiaban los defaults de campo con `copy()` —
superficial para un default `KindStruct`, cuyo `Value.Struct` es un
puntero compartido entre toda instancia del tipo hasta la primera
escritura). Cada uno de estos fue encontrado ejecutando el pipeline
completo contra los DLLs reales de Jint/Esprima, no en un fixture propio.
Resultado: `examples/jint-demo/` corre JavaScript real de punta a punta
(`Engine.Evaluate("1 + 2")` → `"3"`, `Engine.SetValue` + variables → `7`)
a través del motor Jint 3.1.3 sin modificar — ver `docs/es/ROADMAP.md` para
el desglose completo bug por bug.

Fase 3.28 (API de instancias: `Assembly.New`/`Instance.Call`) completa.
`Call`/`CallBytes`/`CallJSON` solo invocan métodos estáticos; `New`/`Call`
exponen al host Go el mismo mecanismo interno de `newobj`/`callvirt` que
Fase 3.27 hizo real de punta a punta — `Machine.New`/`Machine.CallInstance`
(`internal/interpreter/eval.go`) son wrappers exportados finos sobre
`Machine.newObj`/`Machine.call`, y `*Instance` (`instance.go`) implementa
`Value` para poder encadenar (`engine.Call("Evaluate", ...)` → `*Instance`
→ `.Call("ToString")`). Resultado: `examples/jint-nowrapper/` corre el
mismo `Engine.Evaluate`/`SetValue` que `examples/jint-demo` sin ningún
ensamblado glue en C# — solo dos límites reales (no bugs) quedaron
documentados: parámetros opcionales con valor por defecto y métodos de
extensión son azúcar sintáctico que el compilador de C# resuelve en
tiempo de compilación, no algo que el CLR/CIL modele en runtime, así que
`Instance.Call` no puede reconstruirlos automáticamente (ver
`examples/jint-nowrapper/README.md`).

Fase 3.29 (checker: resolución consciente de dependencias) completa.
`checker.Analyze` solo decodificaba el único DLL que se le pasaba, sin
ninguna noción de "esta llamada resuelve contra el IL de OTRO ensamblado
real" — un hueco real una vez que la carga del grafo de dependencias de
`vm.LoadPackage` (Fase 3.27) es el comportamiento real de runtime contra el
que se está certificando. `checker.AnalyzeWithDeps(f, md, deps
[]*metadata.Metadata, profile)` — `Analyze` ahora es un wrapper delgado que
la llama con `deps=nil` — reintenta un target que falla contra `md` contra
la metadata de cada dependencia antes de marcarlo, tratando un target
resuelto vía dependencia como compatible directamente (misma postura que
`isLocalMethod` ya toma para una llamada dentro de `md` mismo: lo que corre
es el cuerpo del callee, no el call site). `vmnet check package` resuelve
el grafo transitivo completo del target vía `nuget.NewResolver` (el mismo
resolver que usa `NuGetManager.Restore`) y lo pasa. Primer caso real:
el `.nuspec` de `NPOI@2.8.0` lista dependencias NuGet genuinas (`ZString`,
`SkiaSharp`, `BouncyCastle.Cryptography`, `ExtendedNumerics.BigDecimal`) —
sin `AnalyzeWithDeps`, ~400 findings eran falsos negativos de llamadas que
ya corren correctamente en runtime a través del mecanismo multi-ensamblado
existente.
