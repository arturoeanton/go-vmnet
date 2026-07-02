# vmnet

Un intérprete de IL/CIL puro en Go para correr plugins C# — y un conjunto
creciente de paquetes NuGet reales — dentro de un programa Go, sin
necesidad de tener el runtime de .NET instalado en el host.

```txt
Estado: Fase 3.5 completa (checker + NuGet + endurecimiento). Sigue la
Fase 4 (listo para producción: benchmarks, sandbox completo, docs
finales). Ver docs/ROADMAP.md.
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
  reflection pesada, sin ASP.NET Core/EF Core/WPF) sin depender de CoreCLR

Antes de cargar un assembly de terceros, `vmnet check` dice exactamente
qué métodos van a correr y cuáles no —con una razón concreta para cada
falta— en vez de fallar a mitad de la ejecución.

La especificación técnica completa está en [`docs/spec.md`](docs/spec.md).

## Qué funciona hoy de verdad

- **Ejecución de IL**: métodos static e instancia, aritmética (con y sin
  signo — los opcodes `.un` tienen semántica correcta y distinta),
  branches, loops, `newobj`/`callvirt`/campos de instancia (resolución
  directa, todavía sin vtable), `System.Array` (`SZARRAY` —
  `newarr`/`ldelem`/`stelem`/`ldlen`), punteros administrados para
  parámetros `ref`/`out`, campos estáticos con `.cctor` perezoso, y
  `throw` no manejado propagado como error Go tipado
  (`vmnet.ManagedException`).
- **BCL parcial**: `List<T>`, `Dictionary<string,V>`, lo básico de
  `System.String`/`System.Math`/`System.Text.Encoding`, constructores de
  excepciones.
- **Bridge Go↔C#**: llamar un método directamente con argumentos tipados
  (`Assembly.Call`), o pasar/devolver `byte[]`/JSON crudo
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

Ver [`docs/ROADMAP.md`](docs/ROADMAP.md) para el historial completo fase
por fase — incluidos dos bugs de correctitud reales (comparación con/sin
signo, un deadlock de reentrancia en un `.cctor`) y un par de bugs de
"drift" en el checker que se encontraron y arreglaron en el camino, no se
escondieron.

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

Ejemplos corribles y documentados en [`examples/`](examples/):

| Ejemplo | Muestra |
|---|---|
| [`examples/hello`](examples/hello) | El `LoadFile` + `Call` más simple posible |
| [`examples/rules`](examples/rules) | Objetos, `List`/`Dictionary`, bridge JSON, excepciones managed, el sandbox de instrucciones frenando un plugin descontrolado |
| [`examples/nuget-basic`](examples/nuget-basic) | Agregar y restaurar un paquete NuGet real publicado, y llamar una función real de ese paquete |

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
[`docs/architecture.md`](docs/architecture.md) para el pipeline completo,
el layout de paquetes, y notas del estado actual, y
[`docs/adr/`](docs/adr) para las decisiones de diseño ya tomadas (por qué
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
