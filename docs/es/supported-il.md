# IL soportado

Este documento existe porque decir "vmnet soporta CIL" no dice nada por sí solo — un decoder puede
reconocer cada opcode de ECMA-335 jamás definido y aun así no *ejecutar* ni la mitad de ellos. Esta
página traza esa línea con precisión, apoyada en tres archivos reales en vez de en conocimiento
genérico de CIL: `internal/il/opcode.go` (la tabla de opcodes del decoder — lo que vmnet puede
siquiera parsear), `internal/ir/builder.go` (el único switch que convierte IL decodificado en el IR
propio de vmnet — la verdad última de lo que vmnet *ejecuta*) e `internal/interpreter/eval.go` (el
loop que corre ese IR — donde viven la semántica real de runtime y sus brechas genuinas).

Como la spec §33.3 exige que este proyecto diga en todas partes, sin adornos:

> vmnet no es una implementación completa de .NET. vmnet ejecuta un subconjunto soportado de CIL y
> APIs BCL seleccionadas. Usá `vmnet check` antes de cargar assemblies de terceros.

Nada de lo que sigue cambia eso. Este documento es un mapa del subconjunto, no una promesa de que
el subconjunto sea "básicamente todo".

## El enfoque de vmnet: decodificar, bajar a IR, ejecutar — o rechazar con honestidad

vmnet lee un PE real y sus tablas de metadata reales de ECMA-335 (sin reescribir IL, sin un paso de
"traducción" de bytecode externo a vmnet mismo) y corre cada cuerpo de método en tres etapas:

1. **Decodificar** (`internal/il`) — `il.ReadMethodBody` parsea el header tiny/fat del método (spec
   §II.25.4) y `il.Decode` convierte los bytes crudos en una lista plana de instrucciones,
   resolviendo cada target de branch/switch a un offset absoluto en el camino. La tabla de opcodes
   en `opcode.go` lista **cada** opcode documentado de ECMA-335, de uno y de dos bytes por igual —
   decodificar un método nunca falla solo porque contiene un opcode que vmnet todavía no ejecuta.
   Esa línea se traza una etapa más adelante.
2. **Bajar a IR** (`internal/ir`) — `ir.Build` recorre las instrucciones decodificadas con un único
   `switch` grande sobre el mnemónico del opcode y produce el IR propio de vmnet (valores
   `ir.Instr`: `LoadArg`, `BinOp`, `Call`, `Branch`, etc.). Este es el archivo que decide de verdad
   qué está soportado:
   - Un opcode con un `case` que agrega un nodo IR real y distinto está soportado y se ejecuta con
     semántica real (aritmética, llamadas, acceso a campos/arrays, branches, excepciones, ...).
   - Un puñado de opcodes tiene un `case` que agrega deliberadamente `ir.Nop` — no porque se hayan
     olvidado, sino porque la representación `runtime.Value` de vmnet los vuelve no-ops genuinos
     (ver "No-ops de identidad" más abajo). `box`/`unbox.any` son el único caso acá con una
     advertencia de corrección real y documentada, cubierta también más abajo.
   - Todo lo demás cae al `default` del `switch` y devuelve un
     `*ir.UnsupportedOpcodeError{OpCode, Offset}` — un tipo de error propio y estructurado (no un
     string suelto) específicamente para que `internal/checker` lo reporte como un finding
     `KindUnsupportedOpcode` en vez de una falla genérica (spec §23.5, el "debe reconocer pero puede
     marcar como no soportado" de §11.3).
3. **Ejecutar** (`internal/interpreter`) — `Machine.runFrame` interpreta el IR nodo por nodo. Acá es
   donde se construye de verdad la semántica de runtime que algunos opcodes de CIL solo *insinúan*:
   despacho virtual real recorriendo la jerarquía de tipos, `try`/`catch`/`finally`/filter real,
   chequeos de límites reales en `ldelem`/`stelem` que levantan una `IndexOutOfRangeException`
   gestionada en vez de un panic de Go, etc.

Como la etapa 2 es donde se decide "soportado", este documento está organizado alrededor del propio
switch de `ir.Build`, no de la tabla cruda de opcodes.

## No-ops de identidad: opcodes que son CIL real pero no hacen nada en vmnet

`ir.Build` convierte un conjunto chico y deliberado de opcodes en `ir.Nop` en vez de un nodo IR
distinto, porque el `runtime.Value` de vmnet ya modela lo que de otro modo necesitarían hacer:

- **`box`, `unbox.any`** — `runtime.Value` ya es una unión etiquetada uniforme (un campo `Kind` más
  el payload), así que "boxear" un value type nunca necesita un cambio de representación: un
  `int32` ya se autodescribe antes y después de un `box`. **El costo real y documentado de esta
  simplificación**: descarta la única información que un par `box`/`unbox.any` de otro modo
  preservaría — que un valor `KindI4` particular era específicamente un `bool` o un miembro de
  `enum`, no un `int` común. Esta es una brecha *conocida y hoy sin arreglar* (ver "Brechas
  conocidas" más abajo), no una teórica — se encontró corriendo código real.
- **`constrained.`, `volatile.`, `readonly.`** — prefijos puros que solo importan para un modelo de
  memoria real o una elección real de despacho por vtable. `constrained.` solo importa para elegir
  entre boxear o la propia sobrescritura de un value type en un `callvirt` siguiente; como
  `runtime.Value` ya lleva su `Kind` real, un `callvirt` a, por ejemplo,
  `System.Object::ToString`/`Equals`/`GetHashCode` ya despacha sobre el valor real sin necesitar la
  pista (ver `internal/bcl/system_object.go`). `volatile.`/`readonly.` son pistas de
  ordenamiento de memoria/aliasing sin sentido para un intérprete basado en `Value` sin modelo de
  memoria cruda.
- **`unaligned.`** (Fase 3.40, encontrado vía `WriteUnaligned`/`ReadUnaligned` de
  `System.Runtime.CompilerServices.Unsafe`) — una preocupación real en hardware que falla ante
  lecturas desalineadas, sin sentido para el storage basado en `Value` de vmnet, que no tiene
  noción de alineación de memoria en absoluto.
- **`nop`, `break`** — no-ops genuinos también en CIL real (`break` es una pista de breakpoint de
  debugger).

## Qué está soportado, por categoría

Cada entrada de abajo tiene un `case` real en el switch de `ir.Build` (`internal/ir/builder.go`) y
semántica de ejecución real en `Machine.runFrame` (`internal/interpreter/eval.go`), salvo que se
indique lo contrario.

### Aritmética y comparaciones
`add`/`sub`/`mul`/`div`/`div.un`/`rem`/`rem.un`/`and`/`or`/`xor`/`shl`/`shr`/`shr.un`/`neg`/`not`,
`ceq`/`cgt`/`cgt.un`/`clt`/`clt.un`, y toda la familia de conversión numérica
`conv.*`/`conv.ovf.*`/`conv.ovf.*.un` — todos colapsan a un conjunto chico de nodos IR (`BinOp`,
`Neg`, `Not`, `Conv`) desde la Fase 1. Las variantes `.ovf` con chequeo de overflow se aceptan pero
hoy ejecutan con la *misma* semántica sin chequeo que sus contrapartes planas — vmnet todavía no
levanta `OverflowException` en un `add.ovf`/`mul.ovf`/etc. que desborda (una brecha real, más
angosta que las de abajo, ya que la aritmética del día a día no se ve afectada).

### Branches, loops, switch
Las formas corta y larga de cada branch condicional/incondicional (`br`/`br.s`,
`brtrue`/`brfalse` y sus formas `.s`, `beq`/`bge`/`bgt`/`ble`/`blt` y sus variantes con signo/`.s`)
resuelven a IR `Branch`/`BranchIfTrue`/`BranchIfFalse`/`BranchCompare` con los targets ya resueltos
a índices de IR en tiempo de build — los loops son solo branches hacia atrás, nada especial.
`switch` (spec §III.3.68, una tabla de saltos real, un índice fuera de rango cae a la siguiente
instrucción) está soportado desde la **Fase 3.6** — se decodificaba desde la Fase 1 pero no se
bajaba a IR hasta entonces, junto con una primera tanda de wins baratos de BCL de alto alcance
medidos contra el corpus de compatibilidad de 7 paquetes + Jint.

### Llamadas a métodos
`call` (despacho estático, o un target de instancia no virtual conocido) y `callvirt` (despacho
virtual) resuelven su token `MethodDef`/`MemberRef`/`MethodSpec` a un nombre completo
`Namespace.Tipo::Método` en tiempo de build del IR. **La ejecución de `callvirt` es despacho
virtual real, no una búsqueda de slot de vtable** (vmnet no tiene vtable en absoluto):
`Machine.call` primero intenta el tipo runtime *concreto* del receptor, y después trepa
`BaseTypeFullName` un ancestro a la vez hasta encontrar una sobrescritura — construido en las
**Fase 3.7/3.8** (jerarquía de tipos real, `isinst`/`castclass`) y endurecido significativamente en
la **Fase 3.27** (resolución multi-assembly, una caminata completa de la cadena de herencia en vez
de "tipo concreto o nada", resolución de overloads real por `Kind`/puntaje de subtipo del
parámetro). La **Fase 3.13** agregó el mismo mecanismo para despacho por interfaz
(`IEnumerable<T>`, `IComparable<T>`, ...), ya que tampoco hay un slot de vtable derivado de
`InterfaceImpl` por el cual despachar. Un `callvirt` sobre un receptor null levanta una
`System.NullReferenceException` real, no un panic de puntero nil de Go. `newobj` construye tanto
tipos referencia (empuja una referencia real a objeto) como value types (spec §III.4.21: construye
en un slot temporal, llama al `.ctor` con un puntero gestionado a ese slot, empuja el *valor*) desde
la Fase 3.7. Las llamadas a métodos genéricos (`MethodSpec`, p. ej. `Guard.Against.Null<string>`)
se desenvuelven a una llamada común — la erasure de tipos de vmnet implica que los argumentos de
tipo normalmente no se necesitan para ejecutar la llamada, con una excepción real y angosta: un
`typeof(T)` dentro del propio cuerpo de ese método genérico, resuelto en el call site vía
`Frame.MethodGenericArgs` (Fase 3.60).

### Campos estáticos y de instancia
`ldfld`/`stfld`/`ldflda` (instancia) y `ldsfld`/`stsfld`/`ldsflda` (estático) funcionan desde la
Fase 1/2. El acceso a campos acepta tres formas de receptor de manera uniforme según spec
§III.4.10/4.28: una instancia de clase (`KindObject`), un puntero gestionado a un struct
(`KindRef → KindStruct`, cómo un struct recibe su propio `this` en sus métodos de instancia), y un
valor de struct entregado directamente (`KindStruct`, la forma que puede tomar una lectura de campo
de struct una vez que su dirección ya se tomó antes en la misma expresión — Fase 3.23). Los campos
estáticos disparan el `.cctor` del tipo dueño en el primer acceso, incluyendo manejo re-entrante
seguro para un `.cctor` que lee las estáticas de su propio tipo.

### Construcción de instancias/tipos: boxing, structs, initobj
`initobj` (zero-init real a través de una dirección) y `newobj` sobre un value type construyen un
struct genuino con semántica de copia (`Value.Clone()` hace deep-copy de `KindStruct`, conectado en
cada punto donde un `Value` entra a un slot persistente — `stloc`/`starg`/`stfld`/`stsfld`/
`stelem`/`stind`), no una referencia compartida — desde la **Fase 3.7**. Ver "No-ops de identidad"
arriba para `box`/`unbox.any`.

### Arrays
`newarr`, `ldlen`, cada variante tipada `ldelem.*`/`stelem.*` más las formas genéricas por token
`ldelem`/`stelem`, y `ldelema` están todos soportados, con chequeo de límites real: un índice fuera
de rango levanta una `System.IndexOutOfRangeException` gestionada, una referencia de array null
levanta `System.NullReferenceException` — nunca un panic de Go. Los elementos de un array de value
type se siembran con un valor por defecto real (no un `null` genérico), calzando con la semántica
real del CLR de que un array de value type nunca es realmente null-valuado (Fase 3.27). `localloc`
(`stackalloc`) está soportado como un `runtime.Array` real con forma de bytes puestos en cero (era
de la Fase 3.7).

### Strings
`ldstr` resuelve el token del heap `#US` a un string real de Go en tiempo de build del IR — los
*opcodes* de strings son así de delgados; la superficie real de métodos de `System.String`
(`Concat`, `Substring`, formateo, ...) es una cuestión de BCL, no de IL (ver
`docs/es/supported-bcl.md`).

### Excepciones: try/catch/finally/fault/filter
Despacho de excepciones real, no solo un `throw` sin manejar — la pieza arquitectónicamente más
grande de la capa de IL, construida en la **Fase 3.10**: `il.ReadExceptionHandlers` parsea la
tabla de cláusulas de manejo de excepciones en su forma chica/fat (spec §II.25.4.5-6), e
`ir.Build` resuelve los offsets de bytes de IL de cada cláusula a índices de IR exactamente igual
que un target de branch. En runtime, una `*runtime.ManagedException` que sale de un frame se
compara contra los handlers de ese método de adentro hacia afuera; un `catch` matchea con la misma
caminata real de jerarquía de tipos que usan `isinst`/`castclass`; `finally`/`fault` siempre
corren, sea que la excepción se capture o siga propagándose; `leave`/`leave.s` correctamente
enhebran cualquier `finally` pendiente entre el punto de salida y el target antes de saltar de
verdad; `rethrow` (el `throw;` de C#) preserva la excepción original. **Las cláusulas de filtro de
excepción `catch (Foo) when (cond)`** fueron la única forma que quedó sin soportar en la Fase 3.10
(una cláusula `HandlerFilter` hacía fallar el método entero) — cerrada en la **Fase 3.51**:
`FilterOffset` baja a un `ir.HandlerFilter` real con su propio `FilterStart`, y `endfilter` (opcode
`0xFE11`, distinto del `0xDC` de `endfinally`) corre el cuerpo del filtro inline para decidir si
entrar al handler o seguir buscando. Un `throw` sobre un objeto de excepción gestionada real y
reconocido se propaga como un error real de Go que el frame del propio llamador puede capturar;
tirar algo que no es un objeto de excepción reconocido es en sí mismo un error reportado, no algo
aceptado en silencio.

### Generics
El soporte de generics de vmnet es **erasure de tipos con dos excepciones reales y puntuales**, no
un runtime genérico en el sentido del CLR — las instanciaciones `MethodSpec`/`TypeSpec` se
desenvuelven a su `MethodDef`/`TypeDef` abierto en tiempo de build del IR, y el `Value` de vmnet por
regla no lleva los argumentos de tipo genérico cerrados. Los dos lugares donde esa erasure de
verdad rompe código real, ambos arreglados:
- **A nivel método**: `typeof(T)` dentro del propio cuerpo de un método genérico, resuelto vía
  `Frame.MethodGenericArgs` — llevado en el call site desde la **Fase 3.60** (el caso real y de
  peso que forzó esto fue `ServiceDescriptor.Singleton<TService,TImplementation>()` de
  `Microsoft.Extensions.DependencyInjection` llamando a `typeof(TImplementation)` sobre su propio
  parámetro abierto).
- **A nivel clase**: `typeof(T)` sobre el propio parámetro genérico de la *clase contenedora*,
  resuelto desde el `ClassGenericArgs` de la propia instancia de objeto actual (poblado en su
  propio sitio de `newobj`) — agregado en la **Fase 3.66** (root-caused vía bugs reales de registro
  de `TypeMap` en `AutoMapper`/`CsvHelper`).

Un método genérico que reenvía su propio parámetro de tipo todavía abierto a otra llamada genérica
(p. ej. `Method2<T>() { Method1<T>(); }`) se maneja con un centinela `"!!N"` resuelto de nuevo en
cada llamada (`resolveForwardedGenericArgs`), el mismo mecanismo que comparten los dos arreglos de
arriba.

### Despacho virtual y despacho por interfaz
Cubierto arriba en "Llamadas a métodos" — despacho real, primero por el tipo del receptor, con una
caminata completa de la cadena de herencia (Fase 3.7/3.8/3.27), extendido a interfaces sin una
vtable real (Fase 3.13). No hay ningún slot de vtable en vmnet en ninguna parte; toda llamada
virtual/de interfaz se resuelve por nombre contra el tipo runtime real del receptor en el momento
de la llamada.

### Delegates y closures
`ldftn`/`ldvirtftn` (puntero a método sin bindear/virtual) más `newobj` sobre un tipo delegate
compilan a la misma forma exacta sin importar el nombre del delegate (`Action`, `Func`2`, un
`delegate` propio del usuario) — soportado desde la **Fase 3.9** vía `runtime.KindFunc`/
`runtime.Func` (`FullName` más un receptor bindeado opcional), deliberadamente sin modelar
`System.Delegate`/`MulticastDelegate` como tipos BCL reales en absoluto. El `Invoke` de un delegate
se intercepta por el `Kind` del receptor, no por un nombre registrado
`"AlgúnTipoDelegate::Invoke"` (el nombre del tipo delegate no tiene límite).

## Brechas conocidas

Estas son brechas reales, actuales, verificadas leyendo el código — no una cobertura genérica. Cada
una o bien no tiene ningún `case` en el switch de `ir.Build` (cae directo a
`UnsupportedOpcodeError`) o es una advertencia de corrección más angosta y documentada sobre un
opcode que por lo demás funciona.

**Fuera de alcance de forma permanente:**
- **`calli` — llamadas indirectas a través de un puntero a función** (`delegate*<...>` de C# 9+).
  No hay ningún `case "calli"` en el switch de `ir.Build`, así que cae directo al
  `UnsupportedOpcodeError` por defecto. Esta no es una brecha de "todavía no implementado" — es un
  límite arquitectónico: vmnet no tiene generación de código nativo ni indirección de puntero a
  función cruda por la cual despachar, el mismo límite fuera del cual ya quedan `Reflection.Emit` y
  P/Invoke para este intérprete. Ver `tests/fixtures/csharp/Unsupported.cs`, el fixture propio y
  reproducible del checker exactamente para este caso (`Unsupported.FunctionPointerCall`).

**Sin implementar hoy (sin ningún `case` en `ir.Build` — un assembly real que use uno de estos
opcodes recibe un `UnsupportedOpcodeError`, no un comportamiento silenciosamente incorrecto):**
- `jmp` (spec §III.3.32, salto de cola a otro método con los mismos argumentos — raro en la salida
  real de Roslyn).
- `cpobj` (copiar un value type a través de dos punteros gestionados, distinto de `ldobj`/`stobj`,
  que sí *están* soportados).
- El `unbox` plano (distinto de `unbox.any`, que sí *está* soportado como no-op) — `unbox` produce
  un puntero gestionado hacia el valor boxeado mismo (usado para mutar un struct boxeado in place),
  una forma para la que el modelo de boxing con passthrough de identidad de vmnet no tiene caso.
- `sizeof`, `cpblk`/`initblk` (operaciones crudas de bloque de memoria no gestionada — sin sentido
  para un intérprete basado en `Value` sin un modelo de memoria plana que direccionar),
  `arglist`/`refanyval`/`refanytype`/`mkrefany` (las features raras de C# `__arglist` varargs y
  `TypedReference`), `ckfinite`.
- Los prefijos de opcode `tail.` y `no.` (a diferencia de los cuatro prefijos cubiertos en "No-ops
  de identidad" arriba, que sí están manejados) — un método cuyo IL use cualquiera de los dos falla
  igual que cualquier otro opcode sin manejar.

**Advertencias de corrección reales y más angostas (el opcode se ejecuta, pero no con fidelidad
completa):**
- **`box`/`unbox.any` borran si un valor `KindI4` boxeado era un `bool`/miembro de `enum` versus un
  `int32` común.** Encontrado corriendo código real, no algo teórico: un valor `bool`/enum boxeado
  que llega a `string.Format`/un string interpolado imprime su valor numérico crudo (`"1"`/`"0"`, o
  el entero subyacente de un enum) en vez de `"True"`/`"False"` o el nombre del miembro, porque para
  cuando llega al código de formateo todo `KindI4` se ve idéntico (`docs/en/ROADMAP.md`, sección
  "Encontrado, no arreglado" de la Fase 3.51, y un caso relacionado de la Fase 3.68 en
  FluentValidation: un argumento de value type boxeado igual al cero de su tipo — p. ej. un `0`
  boxeado — es indistinguible de un `null` real, así que los chequeos estilo `x?.ToString()`
  null-condicionales sobre ese valor son incorrectos).
- **La aritmética `.ovf` no chequea overflow.** `add.ovf`/`mul.ovf`/`sub.ovf` y sus variantes `.un`
  ejecutan idéntico a sus contrapartes sin chequeo — no hay `OverflowException` en un overflow.
- **Los filtros de excepción tienen un caso límite**: `rethrow` rastrea solo la excepción del catch
  entrado más recientemente en un único slot, no una pila — un `rethrow` dentro de un handler catch
  que a su vez contiene un `try`/`catch` anidado ve la excepción interna en vez de restaurar la
  externa (documentado desde la Fase 3.10, sigue siendo así).

## No te memorices este documento — corré `vmnet check`

Esta página te dice qué es cierto de la capa de IL *en general*. Si un assembly real
**específico** que te importa de verdad va a correr es una pregunta más angosta y más útil, y
vmnet tiene una herramienta que la responde directamente en vez de pedirte que razones sobre
tablas de opcodes:

```bash
go build -o vmnet ./cmd/vmnet
./vmnet check ./TuAssembly.dll
./vmnet check package --profile=netstandard-lite <IdDelPaquete>@<Versión>
```

El analizador estático de `internal/checker` recorre cada método del target (y, para
`check package`, todo su grafo de dependencias transitivas, resuelto de la misma forma que
`vm.LoadPackage` lo resuelve en runtime) y reporta, método por método, exactamente qué opcode o
llamada BCL no resuelve — los mismos findings `KindUnsupportedOpcode`/`KindUnsupportedBCL` que
describe la sección "Brechas conocidas" de este documento, pero contra tu código real en vez de uno
hipotético. Ver `docs/es/COMPATIBILITY.md` para qué prueba y qué no prueba un porcentaje de checker
(es una estimación de cobertura, no una prueba de corrección — un método con cero findings todavía
puede comportarse mal si una implementación nativa tiene un bug que el checker no puede ver), y
`docs/es/ROADMAP.md` para la historia completa de cada brecha real encontrada y arreglada para
llegar la capa de IL a donde está hoy.
