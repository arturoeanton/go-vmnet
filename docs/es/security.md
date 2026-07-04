# Modelo de seguridad y modelo de amenazas

Este documento describe qué es lo que vmnet realmente hace cumplir hoy, qué no, y qué debería (y
no debería) asumir una aplicación host que embebe vmnet. Está escrito para leerse antes de correr
cualquier ensamblado C# que no hayas escrito vos mismo — la respuesta honesta, al momento de
escribir esto, es que el sandbox de vmnet es un **límite de estabilidad**, todavía no un **límite
de confianza** completo. Seguí leyendo para entender exactamente qué significa esa distinción en la
práctica.

## Lo que vmnet hace cumplir hoy (un sandbox real y funcional)

Cada invocación de `Assembly.Call`/`Instance.Call`/`Assembly.New` corre bajo los propios límites
de recursos de `internal/interpreter` (`Limits`, `internal/interpreter/limits.go`), actualmente
fijos (todavía no configurables por el llamador — ver la sección de Roadmap más abajo):

- **Cantidad de instrucciones** (`MaxInstructions`, 10.000.000 por defecto por llamada de nivel
  superior): un presupuesto de pasos duro sobre el propio loop de despacho de bytecode del
  intérprete. Un loop infinito, un método recursivo descontrolado, o un busy-loop deliberadamente
  adversarial choca contra esto y devuelve `interpreter.ErrInstructionLimitExceeded` en vez de
  colgar el proceso host para siempre. Verificado contra un fixture real `while(true)`
  (`Runaway()` de `tests/fixtures/csharp/Loops.cs`) — el sandbox salta de forma confiable, bastante
  por debajo de 5 segundos en hardware ordinario.
- **Profundidad de llamadas** (`MaxCallDepth`, 256 por defecto) y **profundidad de pila**
  (`MaxStackDepth`, 10.000 por defecto): acotan la recursión sin límite y el crecimiento
  patológico de la pila de expresiones de la misma forma en que la propia pila de un CLR real
  eventualmente fallaría, pero de forma determinista y recuperable en vez de un desborde de pila
  real a nivel de SO.
- **Longitud de array** (`MaxArrayLength`, 16 MiB de elementos por defecto): acota que un solo
  `newarr` pida una asignación irrazonablemente grande.
- **Recuperación de pánico en el límite de la API**: cualquier pánico a nivel de Go dentro del
  intérprete (un bug del propio vmnet, no solo código interpretado comportándose de forma
  inesperada) se recupera y se expone como un `error` de Go desde `Assembly.Call`/etc., nunca como
  un crash del proceso host. Un plugin roto o activamente adversarial no puede tirar abajo el
  programa Go que lo embebe por este camino.

Estos cuatro límites son reales, con peso real, y ya previenen la forma más común en que un plugin
no confiable-pero-no-malicioso se porta mal: corre para siempre, recursa para siempre, o asigna una
cantidad irrazonable de memoria. Ese es el "límite de estabilidad" al que se refiere el párrafo de
apertura de este documento.

## Lo que vmnet NO hace cumplir hoy

Esta es la sección para leer de verdad antes de decidir si correr el C# de otra persona a través
de vmnet.

### Todavía no hay ningún modelo de capacidad/permisos

El Roadmap (`docs/en/ROADMAP.md`, Fase 4) siempre planeó un modelo `Permissions`
(`AllowConsole`/`AllowFileRead`/`AllowNetwork`, deny-by-default) conectado a cada método nativo de
BCL que toca el mundo exterior. **Todavía no existe.** Cada método nativo de BCL implementado
hasta ahora corre con exactamente los mismos privilegios que el propio proceso host de Go — no hay
ninguna puerta por ensamblado, por llamada, ni por capacidad, de ningún tipo.

### Hoy existe acceso de escritura real al sistema de archivos, sin ninguna restricción

Desde la Fase 3.53 (el proveedor `Microsoft.Data.Sqlite`, `internal/bcl/system_data_sqlite.go`),
código C# interpretado puede hacer esto:

```csharp
using Microsoft.Data.Sqlite;
var conn = new SqliteConnection("Data Source=/cualquier/ruta/que/el/proceso/host/pueda/escribir");
conn.Open();
// acá pasa I/O de archivo real, exactamente en la ruta que eligió el código interpretado
```

El propio parseo de connection string de `SqliteConnection` (`parseSqliteConnectionString`) no
hace ninguna validación, ninguna lista blanca, ni ninguna restricción a un directorio en
particular — el string que provee el código interpretado se convierte en la ruta literal que se
pasa al propio `sql.Open` de Go. **Cualquier código C# corriendo dentro de vmnet puede crear, leer,
o escribir un archivo real en cualquier lugar donde el proceso del SO host tenga permiso para
tocar**, sujeto solo a lo que el formato de archivo real de SQLite tolere en esa ruta (una ruta
arbitraria, si es escribible, obtiene un archivo de base de datos SQLite nuevo creado ahí; un
archivo existente que no sea una base de datos generalmente va a fallar en la primera consulta
real, no en el propio `Open()`). Esta es una capacidad real y funcional hoy, no hipotética — es la
primera capacidad genuina de almacenamiento persistente que tuvo jamás el código interpretado en
este proyecto, y no tiene ninguna puerta de ningún tipo.

### Todavía no hay superficie de red ni de generación de procesos — deliberadamente

Al momento de escribir esto, vmnet no tiene ninguna implementación nativa de `System.IO.File`
(más allá de la ruta de SQLite de arriba), `System.Diagnostics.Process`, ni ningún tipo de cliente
de socket/HTTP en absoluto (`System.Net.Sockets`/`System.Net.Http`). El código interpretado no
puede abrir un archivo arbitrario a través de `File.*`, no puede generar un subproceso, y no puede
hacer una conexión de red — no porque nada de esto esté bloqueado por un control de seguridad, sino
porque la superficie de BCL simplemente no está implementada.

Esto explícitamente **no** es un descuido para arreglar de forma casual. Agregar soporte real de
`System.IO.File`/`System.Diagnostics.Process`/sockets es una feature planeada y querida — pero se
difiere deliberadamente hasta que el modelo `Permissions` de arriba llegue primero, o junto con
esto. Enviar capacidad de archivo/proceso/red sin restricción al código interpretado antes de que
exista alguna puerta deny-by-default repetiría, a mucha mayor escala, exactamente la brecha que ya
ilustra el proveedor SQLite de arriba sobre una superficie más angosta. Este orden es una decisión
explícita del proyecto, no un accidente de cronograma.

## Qué significa esto en la práctica, hoy

- **Tratá el sandbox actual de vmnet como un límite de estabilidad, no un límite de confianza.**
  Frena de forma confiable a un plugin con bugs o accidentalmente adversarial de colgar o tirar
  abajo tu proceso host. **No** frena a un ensamblado *deliberadamente* malicioso de leer/escribir
  archivos que el proceso del SO host pueda alcanzar (vía la ruta de SQLite de arriba) ni de
  consumir CPU/memoria hasta los propios límites del sandbox (que son lo suficientemente generosos
  como para hacer trabajo real, aunque acotado).
- **Corré solo C# en el que confíes** — el propio código de tu equipo, o un paquete NuGet real y
  publicado que realmente hayas revisado (o del que este mismo proyecto, vía `vmnet check`/
  `docs/en/COMPATIBILITY.md`, ya te dé un panorama concreto) — hasta que el modelo `Permissions`
  esté listo. No trates a vmnet hoy como seguro para correr C# arbitrario, adversarial, y no
  confiable, enviado por un tercero (ej. plugins subidos por usuarios en un servicio
  multi-tenant) sin tu propio aislamiento adicional.
- **Si necesitás correr código menos confiable hoy**, poné tu propio límite a nivel de SO
  alrededor de todo el proceso host (un contenedor con sistema de archivos de solo lectura o
  mínimamente escribible, un usuario de SO restringido, un perfil `seccomp`/jail, un proceso
  worker dedicado que estés dispuesto a matar y reiniciar) — vmnet todavía no provee esto
  internamente, y los límites de instrucciones/profundidad/pila/array de arriba no son un
  sustituto para eso.

## I/O del lado del host vs. capacidad del código interpretado — una distinción que vale la pena hacer explícita

El propio código Go de vmnet — la biblioteca y el CLI que importás/corrés, no el C# que
interpreta — hace I/O de archivo real (cargando un `.dll` al que lo apuntás) e I/O de red real
(`vm.NuGet().Restore()` bajando paquetes de `api.nuget.org`) como parte ordinaria de su propia
operación. Ese es el mismo nivel de confianza que cualquier otra dependencia de Go que
importarías, y no es de lo que trata el modelo de amenazas de este documento. De lo que trata este
documento es específicamente: qué puede hacer el **propio código C# interpretado**, corriendo
dentro de la VM — independientemente de las capacidades que ya tenga la aplicación que lo embebe.

## Roadmap

- Modelo `Permissions` (`AllowConsole`/`AllowFileRead`/`AllowNetwork`, deny-by-default), conectado
  a cada método nativo de BCL que toca el mundo exterior — todavía no implementado (Fase 4,
  `docs/en/ROADMAP.md`).
- `MaxStringBytes` — un límite al tamaño de asignación de strings individuales, junto al ya
  existente `MaxArrayLength` — todavía no implementado.
- Soporte real de `System.IO.File`/`System.Diagnostics.Process`/sockets — planeado, pero
  deliberadamente retenido hasta que el modelo `Permissions` de arriba llegue primero o junto con
  esto, por el razonamiento de arriba.
- `Limits` configurables (los valores actuales de instrucciones/profundidad de
  llamada/profundidad de pila/longitud de array son constantes fijas, todavía no expuestas para
  que un llamador las ajuste según su caso de uso).
