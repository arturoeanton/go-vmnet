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

### Una puerta `Permissions` real, deny-by-default (Fase 3.59)

Desde la Fase 3.59, `vmnet.VM` tiene una puerta de capacidad `Permissions` (`permissions.go`,
`vm.Permissions()`), y cada método nativo de BCL que llega a I/O de disco real se chequea contra
ella **antes de correr siquiera** — una capacidad denegada nunca ejecuta ni un `stat(2)`:

```go
vm := vmnet.New()
// vm.Permissions() arranca en el valor cero: todo lo de abajo está denegado.
vm.Permissions().AllowFileRead = true
vm.Permissions().AllowFileWrite = true
asm, _ := vm.LoadPackage("NPOI@2.8.0")
```

Dos campos independientes, ambos `false` por defecto:

- **`AllowFileRead`** protege `System.IO.File.Exists`/`OpenRead`/`ReadAllText`/`ReadAllBytes`,
  `System.IO.Directory.Exists`, los miembros de solo lectura de `FileInfo`/`DirectoryInfo`, y abrir
  un `FileStream`/`FileInfo.Open` en `FileMode.Open`.
- **`AllowFileWrite`** protege `System.IO.File.Create`/`WriteAllText`/`WriteAllBytes`/`Delete`/
  `SetAttributes`, `System.IO.Directory.CreateDirectory`, `FileInfo`/`DirectoryInfo.Create`/
  `Delete`, y abrir un `FileStream` en cualquier modo que no sea `Open` (`Copy` necesita ambos, ya
  que lee el origen y escribe el destino).

Una llamada denegada tira un `System.UnauthorizedAccessException` real — atrapable desde C#
interpretado exactamente como cualquier otra excepción (`catch (UnauthorizedAccessException)`, o
`catch (Exception)`; ver `examples/permissions-demo`, que corre el mismo C# compilado tres veces
contra tres configuraciones distintas de `Permissions` y muestra los tres resultados, incluyendo
una relectura independiente desde Go que confirma que el caso permitido tocó un archivo real en
disco, no una ilusión en memoria).

Dos nativos **preexistentes** que ya hacían I/O de archivo real completamente sin puerta antes de
esta Fase se retrofitearon bajo la misma puerta en vez de dejarlos inconsistentes:

- Abrir una `Microsoft.Data.Sqlite.SqliteConnection` real (Fase 3.53, `internal/bcl/
  system_data_sqlite.go`) ahora requiere `AllowFileRead` y `AllowFileWrite` a la vez.
- `System.IO.Path.GetTempFileName` (crea un archivo real y vacío en disco, no solo un string de
  ruta) ahora requiere `AllowFileWrite`.

Los campos `AllowConsole` y `AllowNetwork` también existen en `Permissions` hoy, por compatibilidad
a futuro con la promesa de roadmap ya existente de este documento — **ninguno de los dos se hace
cumplir todavía**: `System.Console.Write`/`WriteLine` sigue siempre permitido (comportamiento
previo a Permissions, sin cambios), y no existe ningún nativo que toque la red. Ver "Lo que vmnet
NO hace cumplir hoy" más abajo para exactamente qué queda abierto.

Un `*interpreter.Machine` construido sin `Permissions` configurado (nil) se trata exactamente igual
que un `Permissions{}` explícito con todo denegado — una puerta faltante nunca puede significar en
silencio "permitir todo".

## Lo que vmnet NO hace cumplir hoy

Esta es la sección para leer de verdad antes de decidir si correr el C# de otra persona a través
de vmnet.

### La salida por consola y todo lo demás que no sea I/O de disco siguen corriendo con privilegio total del host

La puerta `Permissions` de arriba cubre I/O de disco real específicamente — todavía no cubre la
salida por consola (`AllowConsole` está definido pero no se hace cumplir) ni nada más que un método
nativo de BCL pueda hacer a futuro fuera de la memoria administrada de vmnet. Cada método nativo de
BCL que no esté listado arriba corre con exactamente los mismos privilegios que el propio proceso
host de Go.

### Todavía no hay superficie de red ni de generación de procesos — deliberadamente

Al momento de escribir esto, vmnet no tiene ninguna implementación nativa de
`System.Diagnostics.Process` ni ningún tipo de cliente de socket/HTTP en absoluto
(`System.Net.Sockets`/`System.Net.Http`). El código interpretado no puede generar un subproceso ni
hacer una conexión de red — no porque nada de esto esté bloqueado por un control de seguridad, sino
porque la superficie de BCL simplemente no está implementada. Un barrido de todo el corpus a través
de los 19 paquetes que sigue este proyecto (`docs/en/COMPATIBILITY.md`) encontró **cero usos reales
de `System.Diagnostics.Process`** y **cero usos reales de `System.Net.Sockets` crudo** — solo una
cantidad modesta pero real de `System.Net.Http` (`ClosedXML`) y `System.Net.IPAddress`
(`SimpleBase`, probablemente para formateo/validación, no para redes de verdad). Agregar
cualquiera de los dos es una feature planeada y querida para cuando la demanda real lo justifique,
protegida por `AllowNetwork` desde el primer día en vez de retrofiteada como los dos nativos de
archivo de arriba tuvieron que ser.

## Qué significa esto en la práctica, hoy

- **Tratá el sandbox actual de vmnet como un límite de estabilidad más I/O de archivo, no un límite
  de confianza completo.** Frena de forma confiable a un plugin con bugs o accidentalmente
  adversarial de colgar o tirar abajo tu proceso host, y (desde la Fase 3.59) frena de forma
  confiable cualquier lectura o escritura de archivo que no hayas otorgado explícitamente vía
  `vm.Permissions()`. **No** frena a un ensamblado *deliberadamente* malicioso de consumir
  CPU/memoria hasta los propios límites del sandbox (que son lo suficientemente generosos como
  para hacer trabajo real, aunque acotado), ni — si ya otorgaste `AllowFileRead`/`AllowFileWrite`
  — de hacer cualquier otra cosa que un programa .NET real podría hacer con ese mismo acceso a
  archivos.
- **Otorgá solo las capacidades de archivo que un paquete específico realmente necesita.**
  `AllowFileRead` y `AllowFileWrite` son independientes — un host que solo necesita abrir su propio
  caché de paquetes en modo solo lectura nunca tiene que otorgar acceso de escritura.
- **Corré solo C# en el que confíes** para todo lo que esta puerta Permissions todavía no cubre
  (salida por consola, y lo que un futuro método nativo de BCL pueda hacer antes de que
  `AllowConsole`/`AllowNetwork` se hagan cumplir de verdad) — el propio código de tu equipo, o un
  paquete NuGet real y publicado que realmente hayas revisado (o del que este mismo proyecto, vía
  `vmnet check`/`docs/en/COMPATIBILITY.md`, ya te dé un panorama concreto).
- **Si necesitás correr código menos confiable hoy**, poné tu propio límite a nivel de SO
  alrededor de todo el proceso host (un contenedor con sistema de archivos de solo lectura o
  mínimamente escribible, un usuario de SO restringido, un perfil `seccomp`/jail, un proceso
  worker dedicado que estés dispuesto a matar y reiniciar) — los propios límites y la puerta
  Permissions de vmnet no son un sustituto para eso, solo una segunda capa.

## I/O del lado del host vs. capacidad del código interpretado — una distinción que vale la pena hacer explícita

El propio código Go de vmnet — la biblioteca y el CLI que importás/corrés, no el C# que
interpreta — hace I/O de archivo real (cargando un `.dll` al que lo apuntás) e I/O de red real
(`vm.NuGet().Restore()` bajando paquetes de `api.nuget.org`) como parte ordinaria de su propia
operación. Ese es el mismo nivel de confianza que cualquier otra dependencia de Go que
importarías, y no es de lo que trata el modelo de amenazas de este documento. De lo que trata este
documento es específicamente: qué puede hacer el **propio código C# interpretado**, corriendo
dentro de la VM — independientemente de las capacidades que ya tenga la aplicación que lo embebe.

## Roadmap

- Hacer cumplir `AllowConsole`/`AllowNetwork` — los campos ya existen en `Permissions` hoy pero no
  protegen nada todavía (la Fase 3.59 solo conecta `AllowFileRead`/`AllowFileWrite`); soporte real
  de `System.Net.Http`/`System.Net.Sockets` (ver "todavía no hay superficie de red" arriba)
  llegaría protegido por `AllowNetwork` desde el primer día.
- `MaxStringBytes` — un límite al tamaño de asignación de strings individuales, junto al ya
  existente `MaxArrayLength` — todavía no implementado.
- Soporte real de `System.Diagnostics.Process` — no planeado a menos que aparezca demanda real del
  corpus (el barrido de la Fase 3.59 no encontró ninguna en los 19 paquetes que se siguen).
- `Limits` configurables (los valores actuales de instrucciones/profundidad de
  llamada/profundidad de pila/longitud de array son constantes fijas, todavía no expuestas para
  que un llamador las ajuste según su caso de uso).
