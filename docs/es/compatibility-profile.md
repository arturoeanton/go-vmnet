# Perfiles de compatibilidad

Este documento es requerido por la spec §33.2 (`/docs/compatibility-profile.md`, junto con
`architecture.md`, `supported-il.md`, `supported-bcl.md`, `nuget-support.md`, `security.md`,
`roadmap.md`). Explica, con precisión y contra el código actual (no contra una aspiración de
diseño), qué permite y qué prohíbe cada uno de los tres perfiles de compatibilidad de vmnet, cómo
corre realmente el checker (`internal/checker`), y qué significa su reporte.

## 1. Por qué existen los perfiles

Un checker que solo responde "compatible" o "no" está contestando la pregunta equivocada, porque
"compatible con qué" depende enteramente de qué es lo que quien llama planea hacer con el
assembly. Cargar una función de regla de negocio bien acotada, que solo toca primitivos y métodos
static, es una apuesta fundamentalmente distinta a cargar un paquete NuGet entero, orientado a
objetos, cargado de genéricos, bajado directamente de nuget.org. El checker de vmnet no produce un
solo veredicto — produce un veredicto *relativo a un perfil con nombre*.
`internal/checker/profile.go` lo dice directamente en su propio comentario:

> "El veredicto del checker siempre es relativo a uno: lo que es 'compatible' bajo minimal puede
> ser 'fuera de perfil' bajo el mismo runtime, porque el runtime en sí soporta más de lo que
> minimal promete."

Esa es también la razón detrás del mensaje requerido por la spec §33.3, que aparece textual en la
documentación de este proyecto:

> vmnet is not a full .NET implementation.
> vmnet executes a supported subset of CIL and selected BCL APIs.
> Use vmnet check before loading third-party assemblies.

"Before loading" (antes de cargar) es la frase clave. El checker está pensado para correr antes de
`vm.LoadPackage`/`Assembly.Call`, contra un assembly o paquete en el que todavía no confiás, para
que quien lo llama pueda decidir — antes de que se ejecute una sola instrucción IL — si el
comportamiento real del target entra dentro de un perfil que está dispuesto a aceptar, y cuál.

## 2. Los tres perfiles, con precisión

`internal/checker/profile.go` define exactamente tres:

```go
const (
	ProfileMinimal         Profile = "minimal"
	ProfileRules           Profile = "rules"
	ProfileNetStandardLite Profile = "netstandard-lite"
)
```

Dos ejes independientes deciden si algo está "en perfil":

- **La compuerta del modelo de objetos** (`objectOpcodesAllowed`) — si clases, campos, `callvirt`,
  `throw`, arrays y campos static están permitidos *en absoluto*, sin importar lo que el runtime
  pueda ejecutar técnicamente. Solo `minimal` falla esta compuerta.
- **La lista permitida de BCL** (`bclPrefixes[profile]`, chequeada vía `inProfile`) — el nombre
  completo (`Namespace.Tipo::Miembro`) de un target de llamada o constructor resuelto tiene que
  matchear con uno de los prefijos propios del perfil. Fundamental: un target que el runtime
  realmente puede correr — resuelve vía `bcl.Lookup`/`bcl.LookupCtor`, un registro Machine-aware, o
  un método local — pero que no está en la lista del perfil, *igual queda marcado*, como
  `out-of-profile` en vez de `unsupported`, porque hoy correría de verdad pero no forma parte de lo
  que ese perfil promete a quien lo llama.

### 2.1 `minimal` — solo métodos static y primitivos

La intención de diseño de la spec §24.1: "para testing básico," soportando métodos static,
`int`/`bool`/`string`, aritmética, branches, `return`. En código, `objectOpcodesAllowed(ProfileMinimal)`
devuelve `false`, y `instrIsObjectModel` rechaza el método entero apenas usa `newobj`, `callvirt`,
carga/escritura de campo, `throw`, arrays (`newarr`/`ldlen`/`ldelem`/`stelem`), o campos static — sin
importar cuán chico sea el resto del método. Un solo finding cubre el método completo, no uno por
instrucción, porque bajo `minimal` el método no puede correr en absoluto una vez que toca
cualquiera de estos, sin importar cuál instrucción específica del modelo de objetos lo disparó.

Deliberadamente **no** excluidos: `ldarga`/`ldloca`/`ldind`/`stind` (dirección-de/carga-escritura
indirecta sobre un local o argumento). Un parámetro `ref`/`out` *primitivo* nunca toca el heap ni el
layout de campos de un tipo, así que se mantiene dentro de la promesa de `minimal` aunque
estructuralmente se vea raro al lado de "métodos static y primitivos".

Su lista de BCL permitida es angosta y explícita: `System.Math`, `System.BitConverter`, un puñado
de miembros de `RuntimeHelpers`/`MemoryMarshal`, `System.Console`, la mayoría de los miembros
comunes de `System.String` (`Concat`, `Format`, `Substring`, `get_Chars`, `Equals`, `op_Equality`,
`Join`, `get_Length`), `System.Double::IsNaN`, `System.Activator::CreateInstance`,
`System.Xml.XmlQualifiedName`, y `System.Object::.ctor` — este último necesario estructuralmente,
porque el constructor base implícito de todo value type pasa por ahí incluso bajo un perfil "sin
modelo de objetos".

**Ejemplo concreto.** `internal/checker/analyzer_test.go`'s `TestAnalyze_MinimalProfileFlagsObjectModel`
fija este comportamiento contra un assembly de fixture real:

- `Vmnet.Fixtures.Customer::get_Name`, `Vmnet.Fixtures.Arrays::SumArray`,
  `Vmnet.Fixtures.Statics::GetInitValue`, y `Vmnet.Fixtures.Statics::IncrementAndGet` — se espera
  que las cuatro produzcan un finding `KindOutOfProfile` bajo `minimal`: un getter de propiedad
  necesita un campo/`callvirt`, `SumArray` necesita `newarr`/`ldelem`, y ambos métodos de `Statics`
  tocan campos static.
- `Vmnet.Fixtures.ByRef::CallIncrementTwice` — un método que solo usa parámetros `ref`/`out` de
  tipo `int` — se espera que produzca **cero** findings bajo `minimal`, confirmando que los
  primitivos por `ref`/`out` se mantienen en perfil incluso acá.

### 2.2 `rules` — reglas de negocio: objetos reales, colecciones, LINQ, excepciones

La intención de diseño de la spec §24.2: "para reglas de negocio" — clases, objetos, strings,
arrays, `List<T>`, `Dictionary<string, object>`, excepciones, `DateTime`, `Guid`, helpers de JSON.
En código esto es `bclPrefixes[ProfileMinimal]` más todo lo que se agrega en el `init()` de
`profile.go` — la superficie completa de objetos/colecciones/excepciones/texto construida a lo
largo de muchas fases del roadmap: `List<T>`, `Dictionary<K,V>`, `HashSet<T>`, `SortedSet<T>`,
`Stack<T>`, `Queue<T>`, `LinkedList<T>`, las familias de interfaces
`IEnumerable`/`IEnumerator`/`ICollection`/`IList`/`IDictionary`, métodos de los structs primitivos
(`Int32`, `Char`, `Boolean`, `Single`, `Double`, ...), una superficie amplia de
`System.Linq.Enumerable` más `System.Linq.Expressions.*` (árboles de expresión), `Regex`,
maquinaria de `Task`/async (`AsyncTaskMethodBuilder`, `TaskAwaiter`, ...), abstracciones ADO.NET de
`System.Data`/`System.Data.Common` (la superficie contra la que corren los micro-ORMs estilo
Dapper), `System.IO` (`File`, `Directory`, `FileStream`, `MemoryStream`, `Stream`, con permisos
según `internal/interpreter/permissions.go`), reflection (`Type`, `MethodInfo`, `PropertyInfo`,
`ParameterInfo`, `ConstructorInfo`, `CustomAttributeData`, ...), `System.Xml`/`System.Xml.Linq`,
`System.Uri`, `System.Guid`, `System.DateTime`/`DateTimeOffset`, y más. `objectOpcodesAllowed`
devuelve `true` para `rules`, así que el modelo de objetos en sí está completamente permitido.

### 2.3 `netstandard-lite` — paquetes NuGet puros

La intención de diseño de la spec §24.3: "para NuGet puro" — una BCL ampliada, colecciones, un
subconjunto de LINQ, `Text.Encoding`, `MemoryStream`, `CultureInfo` básico, reflection-lite. En el
código *actual*, sin embargo, la lista permitida de `netstandard-lite` se define como exactamente
la misma lista de `rules`, copiada entera. El propio comentario de `profile.go` es explícito sobre
esto:

> "netstandard-lite hoy promete exactamente la misma superficie de BCL que rules (System.Type se
> movió a `rules` en la Fase 3.14, System.Convert en la Fase 3.18, apenas cada uno tuvo un native
> real detrás) — se mantiene como su propio perfil/slice en vez de colapsarlo en uno solo, para que
> un futuro agregado exclusivo de rules no tenga que reconsiderarse para ambos niveles por
> construcción."

En otras palabras: hoy, `rules` y `netstandard-lite` son listas permitidas idénticas en su
comportamiento. La separación existe para que los dos puedan divergir en el futuro sin una
reescritura estructural, no porque uno sea hoy más estricto que el otro. Vale la pena decirlo
explícitamente en vez de insinuar una diferencia que el código en producción no tiene.

Aun así, `netstandard-lite` es el perfil que este proyecto usa realmente para medir paquetes NuGet
del mundo real (§5–6 más abajo), y es el default propio de `vmnet check package` (§3).

## 3. Cómo correr el checker

Dos puntos de entrada, ambos en `cmd/vmnet/main.go`:

```
vmnet check [--profile=minimal|rules|netstandard-lite] <dll>
vmnet check package [--profile=...] <id>@<version>
```

- **`vmnet check <dll>`** usa `--profile=rules` por defecto cuando se omite el flag (`profile :=
  checker.ProfileRules` en `runCheck`), y llama a `checker.Analyze(f, md, profile)` — un solo
  assembly, sin ningún grafo de dependencias adjunto.
- **`vmnet check package <id>@<version>`** usa `--profile=netstandard-lite` por defecto cuando se
  omite el flag (`profile := checker.ProfileNetStandardLite` en `runCheckPackage`). Resuelve el
  asset objetivo propio del paquete, resuelve su **grafo de dependencias transitivas completo** vía
  `nuget.NewResolver(...).Resolve(...)`, descarga y parsea los metadatos propios de cada
  dependencia, imprime `Dependencies resolved: N`, y llama a
  `checker.AnalyzeWithDeps(f, md, deps, profile)` — no el `Analyze` simple.
- Ambos caminos validan el string de perfil mediante `validateProfile`: solo se aceptan `minimal`,
  `rules` y `netstandard-lite`. Cualquier otro valor es un error duro
  (`unknown profile %q (want minimal, rules or netstandard-lite)`), nunca un fallback silencioso.
- Cualquiera de los dos comandos termina con status 1 si el `Status` del reporte resultante no es
  `compatible`.

**Por qué importa `AnalyzeWithDeps`, con precisión.** El propio comentario de documentación de
`analyzer.go` dice el objetivo de diseño directamente:

> "Analyze recorre cada método que el pipeline de vmnet plausiblemente podría ejecutar e intenta
> exactamente los mismos pasos que Assembly.Call haría (decodificar IL, construir IR, resolver el
> target de la llamada) — así que un veredicto 'compatible' significa 'esto va a correr de
> verdad', no la conjetura de una heurística separada."

`AnalyzeWithDeps` extiende esa misma garantía a través de los límites de paquete. Cuando un target
de llamada o constructor no resuelve contra los metadatos propios del paquete, `checkTarget` lo
intenta contra los metadatos de cada dependencia transitiva antes de marcarlo. El IL de un paquete
real frecuentemente llama directo a los tipos propios de una dependencia — los propios ejemplos del
comentario son Jint llamando a Esprima, y NPOI llamando a ZString, SkiaSharp y
BouncyCastle.Cryptography — y esas llamadas corren de verdad una vez que `vm.LoadPackage` conecta
la cadena de dependencias resuelta en tiempo de ejecución, reflejando lo que hace
`Assembly.WithDependencies`. Marcar una llamada así como no soportada sería un falso negativo, no
una brecha real. `deps` está pensado para ser el grafo de dependencias transitivas **completo** del
paquete (p. ej. vía `internal/nuget.Resolver`), no solo sus dependencias directas — ese es
justamente el punto del mecanismo.

## 4. La forma del Report

`internal/checker/report.go` define:

```go
type Report struct {
	AssemblyName    string
	Profile         Profile
	MethodsAnalyzed int
	MethodsFlagged  int
	Findings        []Finding
	Status          Status
}

type Finding struct {
	Kind       FindingKind
	Method     string // "Namespace.Tipo::Método" donde se encontró ("" para findings a nivel assembly)
	Detail     string // el opcode, el target de llamada no resuelto, ...
	Suggestion string
}
```

### 4.1 Status — la regla exacta de `finalize()`

```go
func (r *Report) finalize() {
	switch {
	case len(r.Findings) == 0:
		r.Status = StatusCompatible
	case r.MethodsAnalyzed == 0 || r.MethodsFlagged >= r.MethodsAnalyzed:
		r.Status = StatusUnsupported
	default:
		r.Status = StatusPartial
	}
}
```

Leída en orden, es exactamente:

1. **`compatible`** — si y solo si `len(r.Findings) == 0`. No es "cero métodos marcados": es
   literalmente ningún `Finding` en absoluto, incluyendo los que son a nivel assembly y no tienen
   método asociado (por ejemplo, un finding `KindPInvoke` por una tabla `ImplMap` presente).
2. **`unsupported`** — si `MethodsAnalyzed == 0` (no existió ningún cuerpo de método analizable —
   p. ej. un assembly stub) **o** `MethodsFlagged >= MethodsAnalyzed` (cada uno de los métodos
   analizados fue marcado al menos una vez). Notá el `>=`, no el `==`: es una cota de seguridad
   contra que `MethodsFlagged` de alguna forma supere a `MethodsAnalyzed`, no un chequeo de
   igualdad estricta.
3. **`partial`** — el caso por defecto: existen findings, pero no todos los métodos analizados
   quedaron marcados.

No hay ningún campo de porcentaje en `Report`, y `printReport` (`cmd/vmnet/main.go`) nunca calcula
ni imprime uno — imprime `MethodsAnalyzed`/`MethodsFlagged` como enteros crudos. "X% compatible" es
un número que este proyecto calcula y expresa en prosa (`(MethodsAnalyzed - MethodsFlagged) /
MethodsAnalyzed`, ver §6), no algo que la herramienta misma emita.

### 4.2 FindingKind — las 7 categorías, y su texto real de sugerencia

```go
const (
	KindUnsupportedOpcode FindingKind = "unsupported-opcode"
	KindUnsupportedMethod FindingKind = "unsupported-bcl-method"
	KindReflection        FindingKind = "reflection"
	KindAsync             FindingKind = "async"
	KindPInvoke           FindingKind = "p-invoke"
	KindUnsafePointer     FindingKind = "unsafe-pointer"
	KindOutOfProfile      FindingKind = "out-of-profile"
)
```

- **`unsupported-opcode`** — falla al decodificar IL o al construir IR: un opcode para el que
  `ir.Build` no tiene traducción a IR (`ir.UnsupportedOpcodeError`), una firma de método
  imposible de parsear, o una falla leyendo el cuerpo/header/manejadores de excepción. La
  sugerencia es específica del opcode cuando se conoce: para `ldtoken`, "los inicializadores de
  array literal (RuntimeHelpers.InitializeArray) todavía no están soportados — asigná los
  elementos individualmente en su lugar"; para una cláusula de filtro `catch (T) when (cond)`,
  "las cláusulas de filtro de excepción (catch (T) when (cond)) todavía no están soportadas —
  catch (T) sin el filtro sí lo está"; si no, un fallback genérico: "not yet implemented — see
  docs/en/ROADMAP.md".
- **`out-of-profile`** — dos formas: (a) el rechazo de método entero por modelo de objetos bajo
  `minimal` ("uses the object model (classes/fields/callvirt/throw), not part of this profile" /
  `suggestion: use profile "rules" or "netstandard-lite"`), o (b) un finding por llamada donde el
  target resuelve (el runtime *sí puede* correrlo) pero no está en la `bclPrefixes` del perfil
  activo y no es un método local. El caso por llamada no lleva sugerencia enlatada — `Detail` es
  directamente el nombre completo del target no listado pero ejecutable.
- **`unsupported-bcl-method`** — la categoría por defecto (rama `default` de `categorize`): un
  target de llamada o constructor que no resuelve ni contra los metadatos propios del paquete ni
  contra ninguna dependencia en `deps`, y que por nombre no tiene forma de reflection ni de Task.
  Sugerencia: "this BCL method has no native implementation yet."
- **`reflection`** — un target no resuelto bajo el namespace `System.Reflection.*`. Sugerencia:
  "avoid reflection-heavy code paths; only typeof/GetType/Type.Name are supported." Este texto es
  anterior a la superficie real de reflection actual, mucho más amplia
  (`GetConstructor`/`GetMethod`/`GetField`/`GetMember`, `PropertyInfo`, `ParameterInfo`,
  `CustomAttributeData`, y más, todo resuelto vía el registro Machine-aware
  `reflectionMachineTargets`) — solo se dispara para la llamada de reflection específica que
  realmente no resuelve, así que hay que leerlo como preciso sobre esa llamada puntual, no sobre el
  soporte de reflection en general.
- **`async`** — un target no resuelto bajo `System.Threading.Tasks.*`. Sugerencia: "avoid
  async/Task — vmnet has no async runtime yet." Misma salvedad que `reflection`: hoy existe
  maquinaria real de `Task`, `AsyncTaskMethodBuilder` y awaiters que funciona bajo
  `rules`/`netstandard-lite`; esto solo se dispara para la llamada específica con forma de Task
  que realmente falla en resolver.
- **`p-invoke`** — a nivel assembly, no por método: se emite una sola vez si la tabla de metadatos
  `ImplMap` del assembly tiene alguna fila (es decir, declara algún método P/Invoke). Sugerencia:
  "P/Invoke is not supported in pure-Go mode."
- **`unsafe-pointer`** — el tipo de retorno o algún parámetro de un método es un puntero no
  administrado (`SigPointer` — código `unsafe` real de C#). No se setea texto de sugerencia para
  este tipo.

## 5. Ejemplo concreto: `fluentvalidation@11.9.2`

El número medido y publicado hoy por `docs/en/COMPATIBILITY.md`: `FluentValidation@11.9.2` →
**98.1% (1.289 métodos, 25 marcados)**, medido bajo `--profile=netstandard-lite`. Reproducilo vos
mismo:

```bash
go build -o vmnet ./cmd/vmnet
./vmnet check package --profile=netstandard-lite fluentvalidation@11.9.2
```

Con el formato exacto y actual de `printReport`, y estos números reales y publicados, el
encabezado de la salida es exactamente:

```
FluentValidation
Status: partial
Profile: netstandard-lite
Methods analyzed: 1289
Methods flagged: 25

Findings:
...
```

Cada campo y etiqueta de arriba (`Status:`, `Profile:`, `Methods analyzed:`, `Methods flagged:`,
`Findings:`) es el texto real y exacto que emite `printReport`, y `1289`/`25` son los conteos
reales y actuales de COMPATIBILITY.md. El `Status` es `partial`, no `unsupported`, porque la regla
de `finalize()` solo escala a `unsupported` cuando `MethodsFlagged >= MethodsAnalyzed` — 25 sobre
1.289 está lejos de esa cota. COMPATIBILITY.md no publica los nombres individuales de método/target
de los 25 findings de forma textual, solo sus causas raíz documentadas: dos sobrecargas `IsValid`
de mismo nombre y misma aridad, a través de un par de clases base/derivada genéricas de
validadores, que la caminata de ancestros por nombre de vmnet solía confundir (arreglado en la
Fase 3.68 con una regla general de resolución de sobrecarga), y una brecha más angosta que queda:
un argumento de tipo valor boxeado cuyo valor es igual al cero de su tipo (p. ej. un `0` boxeado)
es indistinguible de un `null` real para el `box` de vmnet, que es un passthrough de identidad, así
que una verificación estilo `x?.ToString()` sobre ese valor se evalúa mal. Cada una de esas causas
se vería como uno o más findings `out-of-profile`/`unsupported-bcl-method` en la lista real de
`Findings:` — las dos líneas mostradas arriba (`Status`/`Profile`/conteos) son la parte de esta
transcripción que es exacta; las líneas de finding individuales no se reproducen acá porque no
están publicadas de forma textual en ningún lado de la documentación de este proyecto.

## 6. Cómo leer el porcentaje, honestamente

La posición ya establecida por este proyecto, expresada en `docs/en/COMPATIBILITY.md`, aplica
también acá — el porcentaje del checker es `(métodos sin ningún finding) / (métodos analizados)`,
y es **una estimación de cobertura, no una prueba de corrección**: un método puede tener cero
findings y aun así comportarse sutilmente mal si una implementación nativa tiene un bug real que el
checker no tiene forma de ver (para eso están los demos de punta a punta, no algo que chequear
perfiles pueda reemplazar).

El mismo documento también argumenta, en sus propias palabras, que **el número por paquete importa
más que cualquier promedio de todo el corpus**: un promedio simple o ponderado por métodos, a
través de muchos paquetes, puede esconder un paquete mal cubierto que se rompe en el instante en
que alguien realmente depende de él, incluso mientras otros paquetes lo compensan en el promedio.
Su propio objetivo declarado es 97%+ **por paquete**, no como agregado — al momento de ese
documento, 5 de 19 paquetes medidos cumplen esa marca individualmente
(`DocumentFormat.OpenXml` 100.0%, `Humanizer.Core` 97.9%, `NPOI` 97.9%, `Ardalis.GuardClauses`
97.5%, `FluentValidation` 98.1%). Leer un único porcentaje de checker para el paquete específico
que pensás cargar — siempre bajo `netstandard-lite`, siempre incluyendo su grafo completo de
dependencias transitivas — es el número que predice si *ese* paquete va a correr para *vos*; un
promedio de todo el corpus no predice nada sobre ningún paquete en particular.
