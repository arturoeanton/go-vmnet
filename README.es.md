# vmnet

Un intérprete de IL/CIL puro en Go para correr plugins C# — y un conjunto
creciente de paquetes NuGet reales — dentro de un programa Go, sin
necesidad de tener el runtime de .NET instalado en el host.

## Esto corre un motor de JavaScript real. Dentro de un binario Go. Sin CGo.

```go
vm := vmnet.New()
vm.NuGet().Add("Jint", "3.1.3")
vm.NuGet().Restore()
jintAsm, _ := vm.LoadPackage("Jint")

engine, _ := jintAsm.New("Jint.Engine")
result, _ := engine.Call("Evaluate", vmnet.String("1 + 2"), vmnet.String(""))
str, _ := result.(*vmnet.Instance).Call("ToString")
fmt.Println(str.Native())
```

```txt
$ go run .
3
```

Eso es [Jint](https://github.com/sebastienros/jint) 3.1.3 — un motor de
JavaScript en C# real, popular y **sin modificar**, bajado directo de
nuget.org junto con toda su cadena de dependencias transitivas (Esprima,
System.Memory, System.Buffers, ...) — parseando JavaScript de verdad,
construyendo un AST real, despachando métodos virtuales a través de su
jerarquía de clases real, y evaluando el resultado. Sin subproceso, sin
`dotnet` instalado en el host, sin un shim escrito a mano simulando la
librería real. `vmnet` está ejecutando el IL compilado real de Jint, byte
por byte.

Probalo vos mismo: [`examples/jint-nowrapper`](examples/jint-nowrapper)
(Go puro, sin ningún paso de compilación más allá de `go run`) y
[`examples/jint-demo`](examples/jint-demo) (lo mismo manejado a través de
un wrapper compilado en C# chiquito, para APIs que dependen de azúcar
sintáctico exclusivo de C#).

```txt
Estado: Fase 3 completa (Fase 3.39) — checker + NuGet + despacho virtual
real + resolución multi-ensamblado + una API de instancias de objetos
(Assembly.New / Instance.Call) + System.Reflection real
(ConstructorInfo/MethodInfo/FieldInfo con Invoke) + un demo real de
`.xls` legacy vía NPOI. Sigue la Fase 4 (listo para producción:
benchmarks, sandbox completo, docs finales). Ver docs/es/ROADMAP.md.
```

*[Read it in English →](README.md)*

## Qué es y qué no es

`vmnet` **no** es .NET reimplementado en Go, y no promete correr cualquier
DLL .NET que exista. Es un intérprete de un subconjunto real y creciente
de CIL (ECMA-335) más una Base Class Library parcial (`System.*`), pensado
para:

- Plugins C# embebidos en una aplicación Go (reglas de pricing,
  validaciones, lógica de scoring — lógica de negocio que el equipo ya
  escribe en C#)
- Migración incremental .NET → Go, un assembly a la vez
- Reusar paquetes NuGet "puros" ya publicados (sin P/Invoke, sin
  reflection pesada, sin ASP.NET Core/EF Core/WPF) sin depender de
  CoreCLR — Jint de arriba es la prueba de que esto escala a código real
  genuinamente no trivial y orientado a objetos, no solo librerías chicas
  de métodos estáticos

Antes de cargar un assembly de terceros, `vmnet check` dice exactamente
qué métodos van a correr y cuáles no —con una razón concreta para cada
falta— en vez de fallar a mitad de la ejecución. Chequeado hoy contra 9
paquetes NuGet reales y populares más Jint: los 7 originales
(Ardalis.GuardClauses, FluentValidation, System.Text.Json,
Newtonsoft.Json, Semver, SimpleBase, Humanizer.Core) más Jint promedian
~89% limpio bajo el perfil `netstandard-lite` de vmnet, y los dos
agregados más recientemente — NPOI (el demo de `.xls` legacy de abajo) y
ClosedXML — están en 97.3% y 93.9% respectivamente (ver
[`docs/es/ROADMAP.md`](docs/es/ROADMAP.md) para el desglose por paquete y
la metodología).

La especificación técnica completa está en [`docs/es/spec.md`](docs/es/spec.md).

## Qué funciona hoy de verdad

- **Ejecución de IL**: métodos static e instancia, aritmética (con y sin
  signo — los opcodes `.un` tienen semántica correcta y distinta),
  branches, loops/`switch`, `try`/`catch`/`finally` real, value types
  (`initobj`/`constrained.`/`Nullable<T>`), despacho virtual real
  (`callvirt` resuelve a través del tipo concreto real del receptor y
  toda su cadena de herencia, no solo el tipo declarado), `isinst`/
  `castclass` contra jerarquías reales de clases/interfaces, delegates/
  closures (`ldftn`/`Action`/`Func`/multicast), `System.Array` (`SZARRAY`
  — `newarr`/`ldelem`/`stelem`/`ldlen`, correctamente inicializado en
  cero para elementos de value type), punteros administrados para
  parámetros `ref`/`out`, campos estáticos con `.cctor` perezoso, y
  `throw` no manejado propagado como error Go tipado
  (`vmnet.ManagedException`).
- **Construcción de objetos y llamadas de instancia desde Go**:
  `Assembly.New` + `Instance.Call` construyen un objeto real y manejan su
  API de instancia directamente desde Go — sin necesidad de un ensamblado
  glue en C# compilado para el caso común (ver
  [`examples/jint-nowrapper`](examples/jint-nowrapper)).
- **Resolución multi-ensamblado**: `vm.LoadPackage` carga automáticamente
  el grafo completo de dependencias transitivas de un paquete NuGet, con
  resolución de símbolos con ámbito de ensamblado por método (sin
  colisiones de nombres entre ensamblados).
- **LINQ, `async`/`await`** (modelado de forma síncrona), `System.
  Reflection` real (`Type.GetConstructor`/`GetMethod`/`GetField` más el
  propio `Invoke`/`GetValue` de `ConstructorInfo`/`MethodInfo`/
  `FieldInfo` — no `Reflection.Emit`, sin generación de código, cada
  target es un método/campo real que vmnet ya sabe correr),
  `Enum.GetValues`/`HasFlag`, `DateTime`/`Span<T>`/`ReadOnlySpan<T>`,
  `System.Text.RegularExpressions`, tanto las colecciones genéricas
  (`HashSet<T>`/`Stack<T>`/`ConcurrentDictionary`) como las legacy no
  genéricas (`ArrayList`/`Hashtable`/`SortedList`/`Stack`), y una porción
  amplia y en crecimiento constante de `System.String`/`System.Math`/
  `System.Text.Encoding`/`StringBuilder`.
- **Bridge Go↔C#**: llamar un método directamente con argumentos tipados
  (`Assembly.Call`), construir y manejar un grafo de objetos
  (`Assembly.New`/`Instance.Call`), o pasar/devolver `byte[]`/JSON crudo
  (`CallBytes`/`CallJSON`) para formas arbitrarias.
- **Checker de compatibilidad**: `vmnet check <dll>` reutiliza el pipeline
  de ejecución *real* para reportar, método por método, qué corre y qué no
  bajo un perfil dado (`minimal`/`rules`/`netstandard-lite`) — no es una
  heurística separada adivinando.
- **NuGet**: `vmnet add`/`restore`/`packages` resuelven y descargan
  paquetes reales desde `api.nuget.org` (incluidas las dependencias
  transitivas), los cachean localmente, y se cargan con
  `vm.LoadPackage`.
- **Sandbox**: límites de instrucciones/profundidad de llamadas/
  profundidad de stack/longitud de arrays, y cualquier panic dentro del
  código interpretado se recupera en el borde de la API — un plugin roto
  o adversarial no puede tirar abajo el proceso host.

Ver [`docs/es/ROADMAP.md`](docs/es/ROADMAP.md) para el historial completo fase
por fase — incluido cada bug de correctitud real encontrado y arreglado en
el camino (comparación con/sin signo, un deadlock de reentrancia en un
`.cctor`, un bug de aliasing en el default de un campo struct que hacía
que `1 + 2` evaluara a `2` dentro de Jint real, y más), nada escondido
bajo la alfombra.

## Empezar rápido

```bash
go get github.com/arturoeanton/go-vmnet
```

```go
package main

import (
	"fmt"
	"log"

	vmnet "github.com/arturoeanton/go-vmnet"
)

func main() {
	vm := vmnet.New()

	asm, err := vm.LoadFile("MyPlugin.dll")
	if err != nil {
		log.Fatal(err)
	}

	result, err := asm.Call("MyNamespace.MyClass", "Add", vmnet.Int32(3), vmnet.Int32(4))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.Native()) // 7
}
```

`MyPlugin.dll` es un assembly normal compilado con el SDK oficial de .NET
(`dotnet build`) — el SDK es una dependencia de **build**, para producir
el plugin, nunca una dependencia en tiempo de ejecución del programa Go
que lo carga.

Para una API orientada a objetos (construir una instancia, llamar sus
métodos, usar lo que devuelven), `Assembly.New`/`Instance.Call` funcionan
igual sin necesidad de ningún wrapper de método estático — así es
exactamente como funciona el demo de Jint de arriba:

```go
engine, _ := jintAsm.New("Jint.Engine")
result, _ := engine.Call("Evaluate", vmnet.String("1 + 2"), vmnet.String(""))
str, _ := result.(*vmnet.Instance).Call("ToString")
fmt.Println(str.Native()) // "3"
```

Ejemplos corribles y documentados en [`examples/`](examples/):

| Ejemplo | Muestra |
|---|---|
| [`examples/hello`](examples/hello) | El `LoadFile` + `Call` más simple posible |
| [`examples/rules`](examples/rules) | Objetos, `List`/`Dictionary`, bridge JSON, excepciones managed, el sandbox de instrucciones frenando un plugin descontrolado |
| [`examples/nuget-basic`](examples/nuget-basic) | Agregar y restaurar un paquete NuGet real publicado, y llamar una función real de ese paquete |
| [`examples/jint-demo`](examples/jint-demo) | Ejecución de JavaScript real vía el paquete NuGet Jint real + toda su cadena de dependencias, manejado a través de un pequeño wrapper compilado en C# |
| [`examples/jint-nowrapper`](examples/jint-nowrapper) | El mismo demo de Jint sin ningún wrapper de C# — `Assembly.New`/`Instance.Call` manejando `Jint.Engine` directamente desde Go |
| [`examples/npoi-demo`](examples/npoi-demo) | Leer un archivo `.xls` legacy real (strings, números, una celda con fórmula) vía el paquete NuGet NPOI real, sin wrapper de C# |
| [`examples/system-text-json-demo`](examples/system-text-json-demo) | Parsear JSON real vía el paquete System.Text.Json real, sin wrapper de C# |
| [`examples/newtonsoft-json-demo`](examples/newtonsoft-json-demo) | Parsear JSON real vía el DOM "LINQ to JSON" de Newtonsoft.Json real, sin wrapper de C# |
| [`examples/openxml-demo`](examples/openxml-demo) | Generar un `.docx` real desde cero vía el paquete DocumentFormat.OpenXml real, verificado abriéndolo con el SDK de .NET real |
| [`examples/closedxml-demo`](examples/closedxml-demo) | Leer un archivo `.xlsx` real vía el paquete ClosedXML real, con un pequeño wrapper de C# compilado para una limitación de métricas de fuentes |
| [`examples/calculator`](examples/calculator) | Una carga de aritmética/loop corrida a través de vmnet, Go nativo y (opcionalmente) CoreCLR real, lado a lado, para una comparación de corrección y velocidad |
| [`examples/dapper-demo`](examples/dapper-demo) | El propio `SqlMapper.Query`/`Execute` del paquete NuGet Dapper real, corrido contra un proveedor ADO.NET fake mínimo en memoria — sin base de datos real, sin necesitar el SDK de .NET en tiempo de ejecución |

## CLI

```txt
vmnet inspect <dll>                                    # resumen de metadata
vmnet il <dll> <Type.Method>                            # IL decodificado de un método
vmnet run <dll> <Type.Method> '<json-array-of-args>'    # ejecutarlo
vmnet check [--profile=minimal|rules|netstandard-lite] <dll>
vmnet check package [--profile=...] <id>@<version>       # chequear un paquete NuGet sin agregarlo
vmnet add <id>[@<version>]
vmnet restore
vmnet packages
```

## Arquitectura

```txt
.dll → internal/pe → internal/metadata → internal/il → internal/ir → internal/interpreter → internal/bcl
```

La API pública y el CLI viven en la raíz del repo; todo lo demás es
detalle de implementación bajo `internal/`. Ver
[`docs/es/architecture.md`](docs/es/architecture.md) para el pipeline completo,
el layout de paquetes, y notas del estado actual, y
[`docs/es/adr/`](docs/es/adr) para las decisiones de diseño ya tomadas (por qué
Go puro, por qué el layout de paquetes se desvía de la spec original,
...).

## Desarrollo

```bash
go build ./...
go vet ./...
go test ./... -race
```

Los tests de integración cargan DLLs C# reales compiladas desde
`tests/fixtures/csharp`. El SDK de .NET es una dependencia **solo de
desarrollo**, necesaria para regenerar esos fixtures — nunca una
dependencia del runtime de `vmnet`:

```bash
dotnet build tests/fixtures/csharp/Fixtures.csproj -c Release
```

Ver [`CONTRIBUTING.md`](CONTRIBUTING.md) antes de mandar un PR.

## Licencia

Apache License 2.0 — ver [`LICENSE`](LICENSE).
