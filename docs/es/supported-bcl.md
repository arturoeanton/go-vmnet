# Superficie de BCL soportada

vmnet no es una implementación completa de .NET. vmnet ejecuta un subconjunto soportado de CIL y
de APIs de BCL seleccionadas (spec §33.3). Este documento describe ese subconjunto tal como existe
hoy en el código — `internal/bcl` (nativos planos en Go) más los nativos conscientes de la Machine
en `internal/interpreter` (`linq.go`, `async.go`, `reflection.go`, `activator.go`, y otros por el
estilo). Es una foto del momento, no un contrato: la superficie crece en cada Fase (ver
`docs/es/ROADMAP.md`), y un método que hoy falta puede existir para cuando estés leyendo esto.
**La única respuesta autoritativa a "¿va a andar mi código?" es `vmnet check` corrido contra tu
propio assembly** — este documento te cuenta qué hay en general; el checker te dice qué hay para
*tu* IL específico, método por método, y dónde están las brechas. `docs/es/compatibility-profile.md`
cubre los tres profiles (`minimal`/`rules`/`netstandard-lite`) que filtran esta superficie a nivel
checker; este documento habla de lo que el *runtime* puede ejecutar en absoluto, independientemente
del profile.

```bash
go build -o vmnet ./cmd/vmnet
./vmnet check --profile=netstandard-lite <tu.dll>
./vmnet check package --profile=netstandard-lite <PackageId>@<Version>
```

## Cómo leer este documento

Cada sección de abajo lista la amplitud real de lo que está implementado para ese namespace/área,
con algunos métodos nombrados como anclas concretas — no es un índice exhaustivo método por método
(eso sería enorme y quedaría desactualizado en una semana). Para la lista literal y actual,
`grep -rn 'register(' internal/bcl/*.go` y `grep -rn 'machineRegistry\[\|genericMachineRegistry\['
internal/interpreter/*.go` son la fuente de verdad; este documento es el recorrido curado de eso.

## System (tipos centrales)

- **Primitivos y boxing**: `Int16`/`Int32`/`Int64`/`UInt16`/`UInt32`/`UInt64`/`Byte`/`SByte`/
  `Single`/`Double`/`Decimal`/`Boolean`/`Char` — parseo (`TryParse`/`Parse`), formateo,
  `CompareTo`, miembros adyacentes a la aritmética. `System.Convert` cubre las conversiones
  cruzadas comunes (`ToInt32`, `ToString`, `ToBoolean`, `ToDateTime`, ...) más
  `Convert.FromBase64String`/`ToBase64String` (`system_convert_base64.go`).
- **`System.String`**: el archivo individual más profundo (`system_string.go` +
  `system_string_statics.go`, ~33 miembros registrados) — `Concat`, `Format`, `Substring`,
  `Split`, `Replace`, la familia `IndexOf`/`LastIndexOf` (tanto los overloads planos como los
  sufijados con `StringComparison` — comparación ordinal únicamente, sin soporte de cultura, igual
  que `StartsWith`/`Equals`; un bug real de despacho donde el valor crudo del enum en el overload
  `StringComparison` se leía mal como índice de inicio, a veces tirando un
  `ArgumentOutOfRangeException` espurio, se encontró y arregló en la Fase 3.76 vía una tabla
  `stringComparisonSensitiveNatives` en `internal/interpreter/calls.go`), la familia `Trim`,
  `PadLeft`/`PadRight`, `Equals`/`op_Equality`, `Join`, `Contains`, `StartsWith`/`EndsWith`,
  `ToUpper`/`ToLower` (sensible a la cultura vía `CultureInfo`), `get_Chars`/`get_Length`.
  `System.MemoryExtensions` suma las variantes con sabor a `Span<char>` (`AsSpan`, `IndexOf`/`Trim`
  sobre span) que el código real usa cada vez más.
- **`System.Math`**: la superficie común completa (`Pow`, `Round`, `Log`/`Log2`/`Log10`, `Sqrt`,
  las funciones trigonométricas, `Ceiling`/`Floor`/`Truncate`, `Abs`, `Min`/`Max`, `Clamp`) —
  cerrada desde la Fase 3.31.
- **`System.DateTime`/`DateTimeOffset`/`TimeSpan`/`TimeZoneInfo`**: construcción, aritmética
  (`Add*`, resta que produce un `TimeSpan`), comparación, `ToString` con format strings,
  `Now`/`UtcNow`/`Today`, `Parse`/`TryParse`. Solo `System.DateTime` registra ~32 miembros.
  `TimeZoneInfo` cubre las consultas comunes de zona local/UTC.
- **`System.Guid`**: `NewGuid`, `Parse`/`TryParse`, `ToString` (todos los format specifiers
  estándar), igualdad.
- **`System.Random`**: `Next`/`NextDouble`/`NextBytes` con comportamiento de PRNG real,
  determinístico cuando se siembra.
- **La jerarquía `System.Exception`**: construcción, `Message`/`InnerException`/`StackTrace`, y un
  roster amplio de tipos concretos con relaciones de tipo base reales para el matching de `catch`
  — `ArgumentException`/`ArgumentNullException`/`ArgumentOutOfRangeException`,
  `InvalidOperationException`, `NotSupportedException`, `NotImplementedException`,
  `NullReferenceException`, `IndexOutOfRangeException`, `InvalidCastException`,
  `FormatException`, `OverflowException`, `ObjectDisposedException`, `ApplicationException`,
  `AggregateException` (con `InnerExceptions` real), más las de `System.IO`:
  `IOException`/`FileNotFoundException`/`DirectoryNotFoundException`/`EndOfStreamException`, y
  `System.UnauthorizedAccessException` (Fase 3.59) y `System.Data.DataException`.
  `ExceptionDispatchInfo.Capture`/`Throw` (Fase 3.57) preservan un stack capturado.
- **`System.Uri`/`UriParser`**: parseo, accesores de componentes, `IsWellFormedUriString`.
- **`System.Object`/`System.Delegate`/`System.Array`**: `Equals`/`GetHashCode`/`ToString`,
  combine/remove de delegado multicast, y una superficie amplia de `System.Array` — `Sort`/
  `BinarySearch` (consciente de la Machine, `internal/interpreter/array_sort.go`), `Reverse`/
  `Fill`/`Find`/`FindLast`/`FindIndex`/`FindAll`/`Exists`/`ForEach`/`TrueForAll`/`ConvertAll`/
  `LastIndexOf` (`internal/interpreter/array_ops.go`), más los nativos planos `Copy`/`Clone`/
  `Resize`/`IndexOf`/`CreateInstance`/`GetValue`/`SetValue`/`get_Length`/`GetLength`/`Empty`/
  `GetEnumerator`.
- **`System.Nullable<T>`**: semántica real de value type (`HasValue`/`Value`/
  `GetValueOrDefault`), `Nullable.GetUnderlyingType`.
- **`System.ValueTuple`, `System.IntPtr`, `System.BitConverter`, `System.GC`,
  `System.Environment`** (`get_NewLine`, `GetEnvironmentVariable`, `get_ProcessorCount`,
  `get_CurrentManagedThreadId`), `System.AppContext`, `System.WeakReference`/`WeakReference<T>`,
  `System.Lazy<T>` (`get_Value` es consciente de la Machine para invocar el delegado factory en el
  primer acceso), `System.DBNull`.
- **`System.Drawing.Color`/`Point`**: un subconjunto chico y autocontenido — suficiente para
  paquetes que llevan value types adyacentes a dibujo básico sin una dependencia real de
  `System.Drawing`.

## System.Collections / System.Collections.Generic / System.Collections.Concurrent

Toda colección genérica mainstream tiene un backing store nativo real y constructor propio:
`List<T>` (también `Sort`/`RemoveAll`/`ForEach` vía el camino consciente de la Machine, para
soportar delegados), `Dictionary<K,V>` (más `KeyCollection`/`ValueCollection` y sus
enumeradores), `HashSet<T>`, `Stack<T>`, `Queue<T>`, `LinkedList<T>`/`LinkedListNode<T>`,
`SortedDictionary<K,V>`, `SortedSet<T>`, `KeyValuePair<K,V>`, `EqualityComparer<T>`/`Comparer<T>`
(`Comparer<T>.Create` es consciente de la Machine para invocar un delegado de comparación).
`System.Collections.ObjectModel.Collection<T>`/`ReadOnlyCollection<T>` y
`System.Runtime.CompilerServices.ReadOnlyCollectionBuilder<T>` cubren los tipos wrapper que las
bibliotecas reales exponen en sus APIs públicas. Las colecciones legacy no genéricas (`ArrayList`,
`Hashtable`, `System.Collections.Stack`, `SortedList`) también están soportadas — código real,
especialmente cualquier cosa apuntada a APIs de la era .NET Framework vieja (NPOI, por ejemplo),
todavía las usa. `System.Collections.Concurrent.ConcurrentDictionary<K,V>` (incluyendo el overload
consciente de la Machine `GetOrAdd` que toma un delegado factory) y `ConcurrentQueue<T>` (Fase
3.61) están soportadas para el caso de uso común de colección thread-safe, aunque vmnet no tiene
un scheduler concurrente real debajo — ver la nota sobre async más abajo.

Cada uno de estos enumeradores implementa el protocolo real `GetEnumerator`/`MoveNext`/
`get_Current`, así que un `foreach` común sobre cualquiera de ellos — o sobre una referencia
tipada por interfaz (`IEnumerable<T>`, `IList<T>`, `IDictionary<K,V>`, `ICollection<T>`) —
despacha correctamente (Fase 3.11/3.13).

## System.Linq (`Enumerable`)

Toda la superficie común de LINQ-to-Objects está implementada como nativos conscientes de la
Machine (`internal/interpreter/linq.go`, `linq_orderby.go`, `linq_groupby.go`, `linq_range.go`)
porque cada uno necesita invocar un delegado argumento y/o recorrer una fuente `IEnumerable<T>`
arbitraria a través del protocolo real de enumerador — algo que un `bcl.Native` plano no puede
hacer (el propio descubrimiento arquitectónico de la Fase 3.15). Cubierto: `Select`, `Where`,
`SelectMany`, `Any`, `All`, `Count`, `ToList`, `ToArray`, `ToDictionary`, `ToHashSet`,
`First`/`FirstOrDefault`, `Single`/`SingleOrDefault`, `Last`/`LastOrDefault`, `ElementAt`,
`Take`/`Skip`/`TakeWhile`/`SkipWhile`, `Contains`, `Distinct`,
`Concat`/`Union`/`Except`/`Intersect`, `Zip`, `Aggregate`, `Sum`/`Average`/`Min`/`Max`, `Reverse`,
`Cast`/`OfType`/`AsEnumerable`, `Empty`, `Range`,
`OrderBy`/`OrderByDescending`/`ThenBy`/`ThenByDescending` (con un tipo resultado `Ordered` real,
materializado de forma perezosa), y `GroupBy` (con un `Grouping` resultado real que expone `.Key`
y enumeración). Un call site tipado contra las interfaces reales `IGrouping<K,T>`/
`IOrderedEnumerable<T>` despacha a los mismos nativos vía redirección de tipo de receptor por
despacho virtual.

## System.Text / System.Text.RegularExpressions

- **`System.Text.StringBuilder`**: `Append` (todos los overloads comunes), `Insert`, `Remove`,
  `Replace`, `ToString`, `Clear`, `get_Length`/`Capacity` — el caballo de batalla de string
  mutable que el código real prefiere frente a concatenar `String` en un loop.
- **`System.Text.Encoding`/`UTF8Encoding`/`ASCIIEncoding`/`UnicodeEncoding`**: `GetBytes`/
  `GetString`, los accesores estáticos `Encoding.UTF8`/`ASCII`/`Unicode` — transcodificación real
  UTF-16↔UTF-8 a nivel de byte (ejercitada de punta a punta por
  `examples/system-text-json-demo`, Fase 3.41), no un stub.
- **`System.Text.RegularExpressions.Regex`**: `IsMatch`, `Match`, `Matches` (`MatchCollection`),
  `Group`/`GroupCollection`/`Capture`, y `Regex.Replace` (consciente de la Machine,
  `internal/interpreter/regexreplace.go`, para soportar un delegado callback de reemplazo, no
  solo un string de reemplazo). Construido sobre el propio motor `regexp` de Go (RE2) — ver
  Brechas conocidas más abajo por lo que eso cuesta.

## System.Threading / System.Threading.Tasks

`System.Threading.Interlocked`, `Volatile`, `Monitor`, `SpinLock`, `ThreadLocal<T>`/
`AsyncLocal<T>` (Fase 3.61), `CancellationToken`/`CancellationTokenSource`/
`CancellationTokenRegistration` están todos implementados con semántica real de un solo hilo
(vmnet no tiene concurrencia real a nivel de SO debajo, así que estos dan resultados correctos
para código que los usa como primitivas de sincronización/cancelación sin necesitar de verdad
ejecución paralela real).

`async`/`await` (Fase 3.22, "el salto más grande de la secuencia") funciona bajo una
simplificación de diseño deliberada: **cada `Task`/`Task<T>` se completa por construcción** — no
hay scheduler ni thread pool real. `Task.FromResult`, `AsyncTaskMethodBuilder.SetResult`/
`SetException`, `Task.Run`, `Task.Factory.StartNew` producen todos una tarea ya completada, así
que el chequeo `awaiter.IsCompleted` de la state machine `MoveNext()` que genera el compilador
siempre es verdadero, y cada `await` procede de forma sincrónica, en orden de programa, en la
misma goroutine. El control de flujo `async` real y secuencial (incluyendo `try`/`catch`/`finally`
alrededor de un `await`) funciona correctamente bajo este modelo; la ejecución concurrente
genuina, las races, o el comportamiento dependiente de timing de `Task.WhenAll`/`WhenAny` no — ver
Brechas conocidas.

## System.Reflection / System.Linq.Expressions

La reflection se fue construyendo de forma incremental (Fase 3.14 `typeof(T)`/`ldtoken`, 3.16
`IsAssignableFrom`, 3.25 introspección profunda de `Type`, 3.26 `Enum`, pasadas de hardening
3.51/3.56/3.58, 3.62 `IsSubclassOf`/`RuntimeTypeHandle`, 3.63 `CustomAttributeData`) y hoy es lo
bastante amplia para que contenedores reales de inyección de dependencias y ORMs recorran sus
propios tipos objetivo en runtime:

- **`System.Type`**: `GetType()`/`typeof(T)` (empuja un `System.Type` real, sin necesitar un Kind
  separado para `RuntimeTypeHandle`), `IsAssignableFrom`/`IsInstanceOfType`/`IsSubclassOf`,
  `get_IsValueType`/`IsEnum`/`IsInterface`/`IsClass`/`IsAbstract`/`IsPrimitive`, `GetInterfaces`,
  `get_BaseType`, `GetConstructor(s)`, `GetMethod(s)`, `GetField(s)`, `GetProperty/GetProperties`,
  `GetMember`.
- **`System.Reflection.ConstructorInfo`/`MethodInfo`/`FieldInfo`/`PropertyInfo`/`MemberInfo`/
  `ParameterInfo`**: `Invoke`, `GetValue`/`SetValue`, `GetParameters`, `get_Name`/
  `get_DeclaringType`, la familia de accesores `IsPublic`/`IsPrivate`/`IsFamily`/`IsAssembly`/
  `IsStatic`/`IsVirtual`/`IsAbstract`/`IsFinal`, y la identidad de miembro `op_Equality`/
  `op_Inequality`.
- **`System.Reflection.CustomAttributeData`/`CustomAttributeTypedArgument`** (Fase 3.63): lee los
  argumentos de constructor y los argumentos nombrados de un atributo real sin instanciar el
  atributo, el camino de lectura diferida de metadata que necesita `GetCustomAttributesData`.
- **`System.Activator.CreateInstance`** (consciente de la Machine,
  `internal/interpreter/activator.go`): construcción real basada en reflection, incluso a través
  de un argumento de tipo genérico.
- **`System.Enum`**: `GetValues`/`GetNames`/`IsDefined`/`ToObject`/`Parse`/`TryParse`/`HasFlag`/
  `GetUnderlyingType`.
- **`System.Linq.Expressions`**: un evaluador de árboles de expresión genuino (Fase 3.65,
  generalizado a partir de una rebanada más angosta de la Fase 3.64) — `Expression`/
  `Expression<T>`, `ParameterExpression`, `ConstantExpression`, `MemberExpression`,
  `MethodCallExpression`, `NewExpression`/`NewArrayExpression`, `UnaryExpression`/
  `BinaryExpression`, `BlockExpression`, `ConditionalExpression`, `InvocationExpression`,
  `TryExpression`/`CatchBlock`, y un `ExpressionVisitor` base real con todos los overrides
  estándar `Visit*`. `LambdaExpression.Compile()`/`Expression<TDelegate>.Compile()` (consciente de
  la Machine, `internal/interpreter/exprcompile.go`) producen un delegado real e invocable —
  suficiente para la compilación de la lambda de acceso a propiedad de `FluentValidation` y para
  el camino `CallSiteRuntimeResolver` de `Microsoft.Extensions.DependencyInjection`. El tracking
  de parámetros de tipo genérico a nivel de clase (Fase 3.66) hace que esto funcione
  correctamente incluso dentro de los métodos de instancia de una clase genérica.

## System.IO

`System.IO.MemoryStream`/`Stream`/`StringReader`/`StringWriter`/`TextReader`/`TextWriter` (Fase
3.30/3.57) están disponibles sin condición. El I/O de disco real —
`System.IO.File`/`Directory`/`FileStream`/`FileInfo`/`DirectoryInfo`/`Path` (Fase 3.59) — está
implementado con operaciones de archivo reales e incondicionales a nivel de Go (`ReadAllText`,
`WriteAllBytes`, `Create`, `Copy`, `Delete`, cada `FileMode`, ...), filtrado con
**denegar-por-defecto** detrás de un modelo de capacidades `Permissions`
(`AllowFileRead`/`AllowFileWrite`, chequeado por `internal/interpreter/permissions.go` antes de
que el nativo corra) — ver `docs/es/security.md`. Una llamada denegada tira un
`System.UnauthorizedAccessException` real, no un no-op silencioso.
`System.IO.Compression.ZipArchive`/`ZipArchiveEntry` cubre lectura/escritura de formatos basados
en zip (usado por el propio formato de paquete `.docx`/`.xlsx` de `DocumentFormat.OpenXml` por
debajo).

## System.Globalization

`CultureInfo`/`TextInfo` cubren los caminos comunes de casing y comparación sensibles a cultura a
los que delegan los propios miembros sensibles a cultura de `String` (`ToUpper`/`ToLower`,
comparación de strings).

## System.Xml / System.Xml.Linq

Una superficie genuinamente amplia, impulsada por uso real dentro de las propias partes XML del
paquete `.docx`/`.xlsx` de `DocumentFormat.OpenXml`: `System.Xml.XmlReader`/`XmlReaderSettings`
(~27+2 miembros), `XmlWriter`/`XmlWriterSettings` (~17+1), `XmlConvert`, `XmlNameTable`,
`XmlQualifiedName`, y la superficie LINQ-to-XML `System.Xml.Linq.XDocument`/`XContainer`/
`XElement`/`XAttribute`/`XName`.

## System.Runtime.CompilerServices / System.Runtime.InteropServices / adyacente a unsafe

`RuntimeHelpers.InitializeArray` (el patrón `ldtoken Field` para literales de array) y
`IsReferenceOrContainsReferences`/`EnsureSufficientExecutionStack`, `Unsafe` (un subconjunto
chico y real — suficiente para código que lo toca incidentalmente, no un modelo general de
punteros unsafe), `MemoryMarshal.Read`/`Write` (consciente de la Machine),
`ConditionalWeakTable<TKey,TValue>`, `AsyncTaskMethodBuilder`/`AsyncTaskMethodBuilder<T>`,
`TaskAwaiter`/`TaskAwaiter<T>`, `ConfiguredTaskAwaitable` y su `ConfiguredTaskAwaiter` anidado —
el plumbing de la state machine async generada por el compilador, algo que el código de usuario
no llama directamente pero que se necesita para que cualquier método `async` corra de verdad.

## System.Data / ADO.NET / Microsoft.Data.Sqlite

`System.Data.IDbConnection`/`IDbCommand`/`IDbDataParameter`/`IDataReader`/`IDataRecord` y las
clases abstractas `System.Data.Common.Db*` resuelven como objetivos reales de despacho por
interfaz/clase base — no hace falta un nativo por miembro porque un micro-ORM real basado en
ADO.NET (el `SqlMapper` de Dapper, el caso más notable) llama a través de estas abstracciones de
forma polimórfica. Detrás de ellas, vmnet trae un proveedor concreto real y nativo en Go:
`Microsoft.Data.Sqlite` (Fase 3.53) — `SqliteConnection`/`SqliteCommand`/`SqliteParameter`/
`SqliteParameterCollection`/`SqliteDataReader`/`SqliteTransaction`, respaldado por un motor
SQLite real, verificado escribiendo un archivo `.db` y reabriéndolo con el CLI real `sqlite3`
(`examples/sqlite-demo`).

## También presente: DocumentFormat.OpenXml y hooks de hosting adyacentes a Jint

Varios archivos de `internal/interpreter` (`elementfactory.go`, `elements.go`,
`getattribute.go`, `getelement.go`, `partcontainer.go`, `loaddomtree.go`, `cloneimp.go`,
`attribute_createnew.go`, `features.go`) registran nativos conscientes de la Machine dirigidos
específicamente a tipos `DocumentFormat.OpenXml.*`. Esto no es BCL genérica — son nativos
puntuales para la maquinaria interna de un paquete NuGet específico y muy usado (lo que hace que
el puntaje de checker de `DocumentFormat.OpenXml` sea 100% y que su demo produzca un `.docx` real
verificado de ida y vuelta con el SDK). Se incluyen acá porque la "superficie soportada" de un
paquete real incluye este tipo de plumbing específico de paquete, no solo `System.*`.

## Cómo se registran los nativos (para quien quiera contribuir)

Existen dos caminos de registro distintos, y la diferencia importa si estás decidiendo dónde va
un método que falta. `internal/bcl` registra nativos planos — `func(args []runtime.Value)
(runtime.Value, error)` vía `register(fullName, hasReturn, fn)` para métodos, o `func(args
[]runtime.Value) (*runtime.Object, error)` vía `registerCtor`/`registerValueTypeCtor` para
constructores — buscados puramente por clave de string `"Namespace.Type::Method"`, **sin acceso a
la `Machine` del intérprete**. Eso alcanza para la gran mayoría de la BCL: funciones puras sobre
sus argumentos (`Math.Pow`), o métodos que solo tocan el estado nativo de su propio receptor
(`StringBuilder.Append`). *No* alcanza para nada que necesite invocar un argumento delegado (un
predicado de `Select` de LINQ), recorrer un `IEnumerable<T>` arbitrario a través del protocolo
real `GetEnumerator`/`MoveNext`, resolver los propios argumentos de tipo de un método genérico, o
consultar un gate de capacidades — esos viven en los mapas `machineRegistry`/
`genericMachineRegistry` de `internal/interpreter` (`func(m *Machine, args []runtime.Value, depth
int, instrCount *int64) (runtime.Value, error)`, más `methodGenericArgs` para la variante
genérica), consultados por `Machine.tryCall` en `calls.go` después de `bcl.Lookup` y antes de caer
al fallback de invocar un cuerpo de método interpretado. Si estás agregando un nativo y este solo
toca sus propios argumentos, va en `internal/bcl`; si necesita volver a llamar a código
interpretado o inspeccionar los genéricos resueltos del call site, va en `internal/interpreter`.

## Brechas conocidas

Estas son las brechas reales y documentadas — no son adivinanzas. Citadas de
`docs/es/COMPATIBILITY.md` y `docs/es/ROADMAP.md`; acá no se inventó nada.

- **`dynamic`/`ExpandoObject`**: no implementado. La propia superficie de tipado `Dynamic` de
  `Newtonsoft.Json` (`JValue+JValueDynamicProxy`) es el ejemplo concreto — el puntaje de checker
  más bajo de cualquier paquete con demo real (85.6%) específicamente por esta brecha, y la demo
  evita a propósito ejercitarla. No leas que una demo pasando significa "todo el paquete
  funciona" cuando tiene una superficie de tipado dinámico conocida.
- **Sustitución de parámetro de tipo genérico dentro de los propios inicializadores de campo
  estático de una clase genérica**: una limitación arquitectónica real y profunda — el propio
  motor de métricas de fuente por defecto de `ClosedXML` choca contra esto, necesitando un
  pequeño wrapper de C# compilado que provee un `IXLGraphicEngine` mínimo para esquivarlo en la
  demo. El tracking de parámetros de tipo genérico a nivel de clase para métodos de *instancia*
  se arregló (Fase 3.66, desbloqueando el registro de `TypeMap` de `AutoMapper` y la cadena
  `ClassMap.GetGenericType` de `CsvHelper`), pero la forma de inicializador estático de arriba
  sigue abierta. `Type.GetConstructor()` también pierde la identidad de generic cerrado en el
  límite de reflection (el camino de construcción `AutoMap()` de `CsvHelper`) — una
  simplificación separada, deliberada y preexistente.
- **El borde de null-condicional con boxed-cero** (encontrado y con causa raíz identificada en la
  Fase 3.68, todavía una limitación abierta y angosta): un value type boxeado cuyo valor es igual
  al cero de su tipo (un `int` boxeado con valor `0`) es indistinguible de un null real para el
  `box` de paso de identidad de vmnet, así que un chequeo null-condicional del estilo
  `x?.ToString()` sobre ese valor lo trata incorrectamente como null. Se dispara en el formateo de
  mensajes de `InclusiveBetween` de `FluentValidation` únicamente cuando un límite es exactamente
  `0`.
- **El regex es el RE2 de Go, no el motor de regex real de .NET** (Fase 3.20): los dos dialectos
  concuerdan en la enorme mayoría del uso real (clases de caracteres, cuantificadores, anclas,
  grupos, alternancia), pero RE2 **no tiene backreferences ni lookaround**
  (`(?=...)`/`(?<=...)`/`(?!...)`) en absoluto — no es una implementación parcial, es una
  limitación dura del motor que no se puede esquivar sin reemplazar el motor de regex entero. El
  propio regex de parámetro SQL de Dapper (`(?<![\p{L}\p{N}_])\{=([\p{L}\p{N}_]+)\}`, un
  lookbehind negativo) es un ejemplo real, permanente y documentado de un patrón que nunca puede
  compilar bajo vmnet.
- **Sin concurrencia real bajo `async`/`Task`**: cada `Task` se completa de forma sincrónica en el
  momento de su creación (la decisión de diseño deliberada de la Fase 3.22) — el control de flujo
  `async` secuencial real funciona, pero el paralelismo genuino, las races, y la semántica
  dependiente de timing de `Task.WhenAll`/`WhenAny` no existen.
- **Sin `System.Diagnostics.Process`, sin `System.Net.Sockets` crudo, sin P/Invoke, sin
  aritmética de punteros `unsafe`, sin `Reflection.Emit`**: `Process`/sockets crudos se dejaron
  deliberadamente sin implementar después de que un escaneo de todo el corpus (Fase 3.59) no
  encontrara demanda real de ninguno de los dos entre 19 paquetes rastreados — no es una pared
  técnica, es una decisión de "se construye cuando aparece demanda real".
  `System.Net.Http`/`System.Net.IPAddress` sí mostraron demanda real modesta (el uso de
  `HttpClient` de `ClosedXML`, el `IPAddress` de `SimpleBase`) pero todavía no están
  implementados; cualquier nativo de red futuro quedaría filtrado por la misma capacidad
  `Permissions.AllowNetwork` que el modelo ya reserva para eso (definida hoy pero sin hacerse
  cumplir en ningún lado, porque todavía no existe nada que toque la red).
- **`AllowConsole`/`AllowNetwork` no filtran nada hoy**: `Permissions` define ambos campos por
  compatibilidad futura, pero `System.Console.Write`/`WriteLine` sigue siempre permitido y no
  existe ningún nativo que toque la red para filtrar.
- **Los argumentos de constructor `FileAccess`/`FileShare` de `FileStream` se aceptan pero no se
  hacen cumplir**, y **el `FileMode.CreateNew` de `File.Copy` no se distingue de
  `Create`/`Truncate`** (el .NET real tira `IOException` si el destino ya existe; vmnet siempre
  tiene éxito) — ambas son simplificaciones documentadas y angostas de la Fase 3.59, sin ningún
  caller real conocido del corpus que dependa de ninguno de los dos caminos de falla específicos.
- **Los caminos rápidos de árbol de expresión de inyección de dependencias no son verificables,
  no necesariamente están rotos**: el propio camino rápido compilado `ExpressionResolverBuilder`
  de `Microsoft.Extensions.DependencyInjection` es una optimización de background, best-effort,
  que cae en silencio a un fallback ante cualquier falla de compilación — la cobertura de demo
  real ejercita en cambio el camino siempre activo `CallSiteRuntimeResolver`, así que el
  comportamiento real del camino rápido bajo vmnet no está confirmado de forma independiente en
  ningún sentido.

## La respuesta autoritativa para tu assembly

Todo lo de arriba describe la forma general de la superficie soportada. Para cualquier assembly o
paquete NuGet real, corré el checker — recorre cada método que tu código (y todo su grafo de
dependencias transitivas) realmente llama y reporta, objetivo resoluble por objetivo resoluble,
exactamente qué andaría y qué no, bajo el profile que elijas:

```bash
./vmnet check --profile=netstandard-lite ruta/a/TuAssembly.dll
./vmnet check package --profile=netstandard-lite AlgunPaquete@1.2.3
```

Ver `docs/es/COMPATIBILITY.md` para 19 paquetes reales medidos de esta forma (más, donde existe,
una demo real corriendo que confirma el comportamiento real contra la salida real de .NET — el
porcentaje del checker solo es una estimación de cobertura, no una prueba de corrección).
