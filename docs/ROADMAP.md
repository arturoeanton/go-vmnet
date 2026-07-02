# vmnet — Plan de ejecución en 4 fases

> Estado: propuesta inicial · Fecha: 2026-07-02 · Repo en estado greenfield (sin código aún)

Este documento traduce la especificación técnica completa de `vmnet` (intérprete IL/CIL puro
Go para correr plugins C#/NuGet embebidos en Go) en **4 fases ejecutables**, cada una cerrando
con una **demo concreta** pensada para conseguir aprobación/continuidad de presupuesto. Cada
fase es un stage-gate: se demuestra valor incremental y se decide si se financia la siguiente.

Supuesto de staffing por defecto: 1–2 ingenieros Go senior dedicados. Las duraciones son
estimaciones en semanas-persona; ajustar según equipo real.

Fuera de alcance hasta v1.0 (recordatorio, ver spec §3): ASP.NET Core, EF Core, WPF/WinForms,
Reflection.Emit, `dynamic` avanzado, P/Invoke, `unsafe`, threading real, async/await completo,
NuGet arbitrario, backend CoreCLR. Estos quedan como roadmap post-v1.0 (v1.5 "hybrid backend").

---

## Resumen ejecutivo (para stakeholders)

| Fase | Nombre | Duración est. | Qué prueba | Demo en 1 línea |
|---|---|---|---|---|
| 1 | Núcleo IL funcional | 5–7 sem | Viabilidad técnica: Go puede parsear y ejecutar IL real | `vmnet run SimpleMath.dll Add 3 4` → `7`, sin .NET instalado |
| 2 | Motor de reglas de negocio | 6–8 sem | Es un producto usable, no solo un parser | Rule engine C# real llamado desde Go vía JSON, con sandbox |
| 2.5 | Endurecimiento *(gate interno, sin demo de venta)* | 2–3 días | El intérprete no crashea el host bajo input adversarial ni concurrencia | `go test ./... -race` + fuzzing (~16.8M ejecuciones, 0 panics) |
| 3 | Checker + ecosistema NuGet | 6–9 sem | Adopción de bajo riesgo + reuso de librerías existentes | 7 paquetes NuGet reales chequeados, 3 con funciones ejecutando de verdad |
| 4 | v1.0 producción | 5–7 sem | Listo para pilotos reales | Benchmarks, seguridad, docs, CI multiplataforma, 5 min a "hello world" |

**Riesgo mayor del proyecto**: no es el parser IL, es la BCL (`System.*`). Por eso las 4 fases
están ordenadas para exponer ese riesgo lo antes posible (Fase 2) y mitigarlo con un checker
fuerte (Fase 3) antes de prometer compatibilidad amplia.

---

## Fase 0 — Bootstrap (previo a Fase 1, ~3–5 días)

No es una fase "vendible" pero es prerequisito técnico.

- [ ] `go mod init` — decidir nombre de módulo definitivo (`github.com/arturoeanton/go-vmnet`,
      paquete público `vmnet`, codename interno `gocil`)
- [ ] Scaffolding de carpetas según arquitectura (`/pe /metadata /il /ir /interpreter /runtime
      /bcl /nuget /checker /cmd/vmnet /examples /tests`)
- [ ] CI mínima (GitHub Actions): build + test en Linux/macOS/Windows, `CGO_ENABLED=0`
- [ ] `/tests/fixtures/csharp`: proyecto .NET SDK con las fixtures de la spec §29
      (`SimpleMath`, `Strings`, `Loops`, `Objects`, `CollectionsTest`, `ExceptionTest`) +
      script de build (`Makefile`/`justfile`) que compila los `.dll` de prueba.
      **Nota importante**: el SDK de .NET es una dependencia *solo de desarrollo* (para generar
      los binarios de test), nunca del runtime de `vmnet` — esto hay que comunicarlo claro para
      no confundir a stakeholders.
- [ ] `docs/architecture.md` esqueleto (referencia a esta spec), `CONTRIBUTING.md`
- [ ] ADR corto documentando la decisión de "pure Go, no cgo, no hostfxr" como núcleo

---

## Fase 1 — Núcleo IL funcional ("Proof of Concept")

**Objetivo:** demostrar que Go puede leer un ensamblado `.NET` real (compilado con el compilador
oficial de Microsoft, sin modificar) y ejecutar un subconjunto de IL correctamente, sin ningún
runtime .NET instalado. Este es el mayor riesgo técnico del proyecto y se prueba primero.

### Tareas

**`/pe` — PE/CLI loader**
- [x] DOS header, PE header, COFF header, optional header
- [x] Section headers + conversión RVA → file offset
- [x] Localización de CLI header y metadata root
- [x] Errores: `ErrInvalidPE`, `ErrMissingCLIHeader`, `ErrInvalidRVA`, `ErrInvalidMetadataRoot`
- [x] Tests: PE válido/inválido, sin CLI header, RVA inválido, múltiples secciones

**`/metadata` — metadata loader**
- [x] Streams: `#~`, `#Strings`, `#US`, `#Blob`, `#GUID`
- [x] Tablas core: Module, TypeRef, TypeDef, Field, MethodDef, Param, MemberRef, Constant,
      StandAloneSig, Assembly, AssemblyRef (resto de tablas de §10.2 parsean sin fallar vía
      esquema genérico, aunque no se usen todavía)
- [x] Modelo de tokens + resolución de coded indexes
- [x] Parser de signatures: primitivos, `SZARRAY`, `CLASS`, `VALUETYPE`, `MethodDefSig`,
      `LocalVarSig` (generics/`GENERICINST` se parsean para no romper alineación, pero se
      exponen como `SigUnknown` — resolución real en Fase 2/3)
- [x] Tests por tabla + decodificación de signatures (contra el DLL real de fixtures)

**`/il` — decoder**
- [x] Tabla de opcodes completa (set v0.1 de spec §11.2 + opcodes v0.2+ de §11.3, todos
      reconocidos por el decoder — ver nota de alcance más abajo)
- [x] `Instruction{Offset, OpCode, Operand}` con tracking de offsets
- [x] Method header (tiny/fat) + reconocimiento de opcodes no soportados sin crashear

**`/ir`**
- [x] Set de instrucciones IR (`LoadArg`, `LoadLocal`, `StoreLocal`, `LoadConstI4`, `BinOp`,
      `Call`, `Branch`, `Return`, ...)
- [x] Builder IL → IR, con error explícito y localizado (offset IL) para cualquier opcode que
      la IR todavía no baja (callvirt, newobj, ldfld, arrays, excepciones — Fase 2)

**`/interpreter` + `/runtime` (mínimo viable)**
- [x] Frame/stack model, loop `eval`, dispatch
- [x] Aritmética + branches + loops
- [x] Resolución e invocación de métodos static (incluye llamadas a BCL nativo y a otros
      métodos static del mismo assembly, con límite de profundidad de recursión)
- [x] Modelo runtime mínimo de `Value`/`Method`
- [x] Límites: `MaxCallDepth`, `MaxInstructions` (`ErrCallDepthExceeded`,
      `ErrInstructionLimitExceeded`)

**`/bcl` (subset v0.1)**
- [x] `System.Math.Abs`, `System.String.Concat`/`get_Length`, `System.Console.WriteLine`
- [x] Mecanismo de registro de nativos (`bcl.Lookup`/`register`)

**`/cmd/vmnet` CLI**
- [x] `vmnet inspect <dll>` — lista tipos/métodos
- [x] `vmnet il <dll> <Type.Method>` — vuelca IL decodificado
- [x] `vmnet run <dll> <Type.Method> '<json-array>'` — ejecuta método static

**API pública Go (subset de §6.1)**
- [x] `vmnet.New()`, `VM.LoadFile/LoadBytes`, `Assembly.Call`, tipos `Value` (Int32/Int64/
      Float32/Float64/String)

**Tests / aceptación**
- [x] Golden tests: `SimpleMath.Add`, `Strings.Hello`, `Loops.Sum` (Go API + CLI, contra el DLL
      real compilado con el SDK de .NET)
- [x] Criterios de aceptación MVP §35 #1–5, #9, #10, #11, #12

> **Ajuste de alcance vs. spec original:** la tabla de tareas original de esta fase incluía
> "allocación de objetos básica + lectura/escritura de fields" citando los criterios #6–8 del
> MVP (spec §35: crear objetos, leer/escribir fields, `call`/`callvirt` básicos). Al implementar,
> esos tres puntos se movieron a Fase 2 junto con el resto del modelo de objetos
> (`newobj`/`callvirt`/fields de instancia), porque ninguno de los tres métodos del demo de
> Fase 1 (`SimpleMath.Add`, `Strings.Hello`, `Loops.Sum`) los necesita, y separarlos evita
> duplicar trabajo cuando el modelo de objetos completo llegue en Fase 2. El decoder de IL sí
> reconoce `newobj`/`callvirt`/`ldfld`/etc. sin crashear; el IR builder los reporta como
> "unsupported opcode" explícito (verificado con test) hasta Fase 2.

### Demo de cierre de Fase 1 — "Esto es real" (~10 min)

1. Mostrar un `.cs` plano compilado con `dotnet build` sin modificaciones — remarcar
   "esto es exactamente lo que produce el compilador oficial de Microsoft".
2. `vmnet inspect SimpleMath.dll` → tipos/métodos leídos de metadata real.
3. `vmnet il SimpleMath.dll SimpleMath.Add` → IL decodificado, comparable a `ildasm`.
4. `vmnet run SimpleMath.dll SimpleMath.Add '[3,4]'` → `7`; luego `Loops.Sum(1000)` para
   probar branches/loops.
5. El mismo ejemplo desde un programa Go (`vmnet.New()` / `asm.Call(...)`), corriendo en una
   máquina/contenedor **sin .NET instalado** — para que el punto quede grabado a fuego.

**Mensaje de venta:** "Construimos desde cero, en Go puro, un parser PE/CLI/IL y un intérprete
que ejecutan código C# real. Esto era el 20% más riesgoso y ya funciona — todo lo demás se
construye sobre esta base."

---

## Fase 2 — Motor de reglas de negocio ("Producto usable")

**Objetivo:** soportar los patrones OO de C# que aparecen en código real (clases, dispatch
virtual, colecciones, excepciones) y cerrar el JSON bridge que convierte esto en un producto
usable de verdad, con el primer nivel de sandboxing.

### Tareas

**`/runtime`**
- [x] `newobj` + ejecución de constructores (incluye `System.Object::.ctor` como no-op nativo)
- [x] Lectura/escritura de fields de instancia (`ldfld`/`stfld`, resueltos por nombre)
- [x] `callvirt` resuelto directamente (sin vtable) + null check → `NullReferenceException`
      managed si el receptor es `null`
- [x] Boxing/unboxing (`box`/`unbox.any`) como no-op, dado que `runtime.Value` ya es un tagged
      union uniforme
- [ ] ~~Jerarquía de clases (BaseType, Interfaces) + vtable real~~ — diferido: ningún fixture
      necesita polimorfismo (override de un método virtual por una subclase). `callvirt` hoy
      resuelve el método exacto del token, no el override en tiempo de ejecución.
- [ ] ~~Interface dispatch~~ — diferido, mismo motivo

**`/bcl` (subset v0.2)**
- [x] `System.Collections.Generic.List`1` (backing nativo Go): `Add`, `get_Count`, `get_Item`
- [x] `System.Collections.Generic.Dictionary`2` (backing nativo Go, **solo claves string** —
      cubre `Dictionary<string,string>`/`Dictionary<string,object>` de spec §17.1): `Add`,
      `get_Item`, `set_Item`, `ContainsKey`, `get_Count`
- [x] `System.Text.Encoding.UTF8.GetString`/`GetBytes` — necesario para el bridge `CallBytes`
- [x] `System.String.Concat` ampliado para aceptar argumentos boxeados (no solo string), como
      hace el compilador de C# en concatenaciones mixtas
- [x] `System.Object.ToString()` (despacha por `Kind` del valor boxeado)
- [ ] `System.String` ampliado (Substring, Equals, ToUpper/Lower, Split, Format) — diferido, no
      lo pide ningún fixture de Fase 2
- [ ] `System.Array` + soporte runtime de `SZARRAY` (`newarr`/`ldelem`/`stelem`/`ldlen`) —
      diferido; ver nota de alcance del bridge `CallBytes` más abajo
- [ ] `System.DateTime`, `System.TimeSpan`, `System.Guid` — diferido

**Generics (mínimo, spec §17.1)**
- [x] Resolución de `TypeSpec`/`GENERICINST` al nombre del tipo genérico abierto (p. ej.
      `List`1`), ignorando los argumentos de tipo — suficiente porque el backing nativo de
      List/Dictionary no necesita saber `T`

**Excepciones**
- [x] `throw` (`runtime.ManagedException`, reexportado como `vmnet.ManagedException`),
      propagación como error Go normal via `%w` (`errors.As` funciona)
- [x] Constructores nativos para `System.Exception`/`InvalidOperationException`/
      `ArgumentException`/`ArgumentNullException`/`NotSupportedException`
- [ ] ~~`try`/`catch`/`finally` (`leave`, `leave.s`, `endfinally`)~~ — diferido explícitamente:
      el demo de Fase 2 solo necesita que una excepción **no manejada** llegue a Go como error
      claro, no que C# la capture. El decoder de IL ya reconoce `leave`/`endfinally`; el IR
      builder los reporta como opcode no soportado si aparecen.
- [ ] Formato de stack trace multi-frame (spec §18.3) — hoy el error es de un solo frame
      (`Tipo.Método: Excepción: mensaje`), sin la cadena `at ...` completa

**JSON bridge + API pública**
- [x] `Assembly.CallBytes`, `Assembly.CallJSON`

**Sandbox**
- [x] `MaxInstructions`/`MaxCallDepth` conectados al eval loop desde Fase 1, verificados ahora
      con un fixture de loop infinito real
- [ ] `MaxHeapBytes`, `MaxStackDepth`, `Permissions` (`AllowConsole` stub) — diferido a Fase 4
      (spec ya los agrupa ahí como parte del modelo de seguridad completo)

**Tests**
- [x] Fixtures `Objects` (Customer), `CollectionsTest`, `ExceptionTest` — ya existían desde
      Fase 0, ahora ejecutables de verdad
- [x] Fixture nueva `Rules.cs`: objetos + `List<int>` + `Dictionary<string,int>` + `Encoding` +
      throw, todo en un solo método `Eval(byte[]) -> byte[]`
- [x] Fixture `Loops.Runaway()`: loop infinito real, matado por `MaxInstructions`
- [x] Tests golden: `TestFase2Demo` (CallBytes, CallJSON, excepción managed tipada via
      `errors.As`, sandbox), `TestObjectsAndCollections`

> **Ajuste de alcance vs. spec original:** igual que en Fase 1, se recortó lo que el demo
> concreto no necesita. Sin vtable/interface dispatch (nada usa polimorfismo real), sin
> `try/catch/finally` (el demo es "excepción no manejada llega a Go", no "C# la atrapa"), sin
> `System.Array`/`SZARRAY` (el bridge `CallBytes` pasa `byte[]` de un lado a otro sin que el C#
> lo indexe — ver comentario en `tests/fixtures/csharp/Rules.cs`), sin `DateTime`/`Guid`. Todo
> lo diferido queda documentado acá en vez de fallar en silencio: el `IR builder` reporta un
> error explícito con el nombre del opcode si un assembly de terceros necesita algo de esta
> lista.

### Demo de cierre de Fase 2 — "Esto es el producto" (~10–15 min)

Corrible hoy con `examples/rules` (`go run .` después de compilar las fixtures):

1. `Rules.Eval` real: una clase `Customer` con propiedades (`callvirt` sobre los accessors
   generados por el compilador), un `List<int>`, un `Dictionary<string,int>` de impuestos.
2. Desde un host Go, `asm.CallJSON("Vmnet.Fixtures.Rules", "Eval", "checkout request")` →
   `map[customer:acme corp ok:true]` — JSON in/out sin código de serialización manual.
3. Input inválido (`CallBytes` con `[]byte("")`) → excepción managed capturada como error Go
   tipado (`errors.As(err, &vmnet.ManagedException{})`): `System.InvalidOperationException:
   empty input`.
4. `Loops.Runaway()` (loop infinito real) → `MaxInstructions` lo mata en ~100ms en vez de
   colgar el proceso host.
5. Reemplazar `Rules.dll` por `Rules_v2.dll` en caliente, sin recompilar ni reiniciar el
   binario Go — remarcar "lógica de negocio hot-swappable" (coreografía de demo, no requiere
   código nuevo: `vm.LoadFile` ya soporta cargar múltiples assemblies).

**Mensaje de venta:** "Esto es lo que un cliente compra: reglas de negocio en C# embebidas de
forma segura en un servicio Go, con aislamiento de fallas y un one-liner de JSON in/out."

---

## Fase 2.5 — Endurecimiento (previa a Fase 3, ~2–3 días)

**Objetivo:** Fase 3 (checker + NuGet) agrega superficie nueva sobre un intérprete que hasta
ahora solo corrió assemblies propios y confiables. Antes de eso, cerrar los huecos de robustez
que quedaron documentados como deuda durante Fase 1/2 — sobre todo los que rompen la promesa
central de "un plugin no puede tirar abajo el host". No es una fase con demo de venta; es un
gate de calidad interno, pero deja evidencia concreta (fuzzing, `-race`) para respaldar el
argumento de seguridad más adelante en Fase 4.

### Tareas

**`internal/interpreter` — el intérprete no puede crashear el proceso host**
- [x] `recover()` en el borde público (`Machine.Invoke`): cualquier panic en todo el árbol de
      invocación (bounds check faltante, type assertion, IR malformada) se convierte en un
      `error` normal en vez de propagarse al goroutine del caller
- [x] `Limits.MaxStackDepth` real — existía en el `Limits` struct desde Fase 1 pero nunca se
      aplicaba; un plugin que hace `push` sin `pop` (bug o adversarial) podía crecer el stack
      sin límite hasta chocar con `MaxInstructions` (potencialmente cientos de MB antes de
      fallar). Ahora se corta con `ErrStackOverflow` mucho antes.
- [x] Tests directos con IR armada a mano (`internal/interpreter/eval_test.go`): panic
      recuperado, `MaxStackDepth` disparado, `MaxCallDepth` disparado por recursión infinita

**`vmnet` (paquete raíz) — seguridad de concurrencia**
- [x] `*Assembly` ahora es seguro para llamar desde múltiples goroutines: los caches
      `methods`/`types` se pueblan de forma lazy en el primer uso, y sin lock dos goroutines
      escribiendo el mismo map al mismo tiempo panickean (`fatal error: concurrent map writes`,
      no recuperable con `recover()`). Se agregó `sync.RWMutex`.
- [x] `TestConcurrentCalls`: 32 goroutines llamando al mismo `*Assembly` en paralelo, corrido
      con `-race`

**Fuzzing nativo de Go (`internal/pe`, `internal/metadata`, `internal/il`)**
- [x] `FuzzParse` en `/pe` y `/metadata`, `FuzzDecode` y `FuzzReadMethodBody` en `/il` — bytes
      arbitrarios nunca deben panickear, solo devolver error. El corpus semilla (incluye el DLL
      real de fixtures) corre como tests normales en `go test`, sin costo de CI
- [x] Corridas de fuzzing real localmente: ~16.8M ejecuciones combinadas (pe + metadata + il),
      0 panics encontrados

**CI**
- [x] `go test ./... -race` en Linux/macOS (Windows-hosted runners de GitHub Actions no tienen
      confiablemente un toolchain de C para el race detector, que necesita cgo — se corre sin
      `-race` ahí, igual cubierto por el resto de la matriz)
- [x] El paso de `Build` sigue forzando `CGO_ENABLED=0` explícitamente para no perder la
      garantía de "pure Go" solo por habilitar `-race` en Test

### Lo que se dejó explícitamente afuera de este gate

No es un endurecimiento completo — sigue habiendo deuda documentada que no bloquea Fase 3:

```txt
- MaxHeapBytes / conteo de memoria lógica: sigue diferido a Fase 4 (Permissions/sandbox
  completo), igual que en el plan original.
- Frame.pop() sigue sin bounds-check explícito (confía en recover() como red de seguridad
  en vez de devolver un error más específico ahí mismo). Suficiente para "no crashea el
  host"; un mensaje de error más preciso es una mejora futura, no un requisito de seguridad.
- No se auditó exhaustivamente cada slice `data[a:b]` de /pe y /metadata — el fuzzing corrido
  hasta ahora (segundos, no horas) es evidencia de robustez, no una garantía formal. Vale la
  pena correr fuzzing más largo (`-fuzztime=1h`+) periódicamente, no solo una vez en Fase 2.5.
```

### Cómo verificar esta fase

```bash
go test ./... -race                                              # todo verde, sin warnings de race
go test ./internal/interpreter/... -run TestInvoke -v             # recover / MaxStackDepth / MaxCallDepth
go test ./ -run TestConcurrentCalls -race -v                      # concurrencia en Assembly
go test ./internal/pe/... -run '^$' -fuzz '^FuzzParse$' -fuzztime=30s
go test ./internal/metadata/... -run '^$' -fuzz '^FuzzParse$' -fuzztime=30s
go test ./internal/il/... -run '^$' -fuzz '^FuzzDecode$' -fuzztime=30s
```

---

## Fase 3 — Checker de compatibilidad + ecosistema NuGet ("Confianza + reuso")

**Objetivo:** bajar el riesgo de adopción diciendo exactamente qué corre y qué no, y abrir la
puerta a reusar paquetes NuGet ya publicados en vez de depender solo de DLLs propias.

### Tareas

**`/checker`**
- [x] Analyzer que reutiliza el pipeline real (`il.Decode` → `ir.Build` → resolución de cada
      `Call`/`NewObj`/`CallVirt` contra `bcl.Lookup`/`bcl.LookupCtor`/métodos locales) en vez de
      reimplementar heurísticas aparte — si el checker dice "compatible", `Assembly.Call`
      efectivamente va a correrlo, porque es literalmente el mismo código de resolución
- [x] Detección de P/Invoke (tabla `ImplMap`), punteros unsafe (`SigPointer`, tipado real
      agregado en Fase 3 — antes colapsaba junto con `byref`/generics en `SigUnknown`), y
      parámetros `ref`/`out` (`SigByRef`, no ejecutables aún — hallazgo propio, no solo "no
      soportado")
- [x] Modelo de reporte con `FindingKind` categorizado (`unsupported-opcode`,
      `unsupported-bcl-method`, `reflection`, `async`, `p-invoke`, `unsafe-pointer`,
      `byref-parameter`, `out-of-profile`) y sugerencia por finding (spec §23.2–23.4)
- [x] Perfiles `minimal` (rechaza *todo* el modelo de objetos, no solo opcodes puntuales — spec
      §24.1), `rules`, `netstandard-lite` — implementados como allowlist real de prefijos BCL +
      instrucciones IR permitidas, no como metadata decorativa
- [x] `vmnet check <dll> [--profile=...]` y `vmnet check package <id>@<version> [--profile=...]`

**`/nuget`**
- [x] Lector de `.nupkg` (zip, `archive/zip` de la stdlib, límite de 256MB por entrada contra
      zip bombs)
- [x] Parser de `.nuspec`: forma agrupada por TFM y forma plana legacy, **forma larga real**
      (`.NETStandard2.0`, `.NETFramework4.7.2`, ...) además de la corta — verificado contra
      `.nuspec` reales, no solo la spec
- [x] Parser de TFM con regex general para ambas notaciones + prioridad exacta de spec §22.5
      (`netstandard2.0` > `netstandard2.1` > `net5.0+` solo con opt-in `AllowModernNet` > `ref/`
      solo análisis > `runtimes/*/native` unsupported)
- [x] Resolver de dependencias transitivo real (cierre completo, ciclos detectados,
      highest-version-wins documentado como simplificación vs. NuGet real)
- [x] Cache local de paquetes (`.vmnet/packages/`, escritura atómica vía archivo temporal +
      rename)
- [x] Lockfile JSON (spec §22.6) + manifest propio (`vmnet.json`, equivalente a
      `<PackageReference>` ya que vmnet no tiene project file)
- [x] CLI: `vmnet add <id>[@version]`, `vmnet restore`, `vmnet packages`
- [x] Cliente NuGet v3 real (`api.nuget.org/v3-flatcontainer`, endpoint hardcodeado — ver nota
      de alcance)
- [x] API Go pública: `vm.NuGet().Add/Restore/Packages()`, `vm.LoadPackage(id)`

**Generics — hallazgo no planeado, más valioso que lo que reemplazó**
- [x] Resolución de `MethodSpec` (tabla `0x2B`, instanciación de métodos genéricos: p. ej.
      `Guard.Against.Null<T>`) — se descubrió DURANTE la certificación de paquetes reales que
      esta era la causa de la mayoría de los "unsupported call target" en librerías reales, no
      falta de métodos BCL puntuales. Se resuelve desenrollando al `MethodDef`/`MemberRef` de
      base, ignorando los argumentos de tipo (igual que ya se hacía para `TypeSpec`)

**Corrección de comparaciones con/sin signo — bug real encontrado por testing, no solo deuda**
- [x] `div.un`/`rem.un`/`shr.un`/`cgt.un`/`clt.un` y las ramas `bge.un`/`bgt.un`/`ble.un`/
      `blt.un`/`bne.un` ahora tienen semántica **sin signo** real, distinta de sus contrapartes
      con signo. Antes ambas colapsaban a la misma operación con signo — funcionaba para los
      fixtures propios (enteros no negativos) pero daba **resultados silenciosamente
      incorrectos** en el patrón idiomático de C# `(uint)(c - low) <= high` (chequeo de rango
      muy común en código real). Se encontró ejecutando `System.HexConverter.IsHexUpperChar`
      de `System.Text.Json` contra el caracter `' '` y viendo `true` en vez de `false`.

**BCL v0.3 — reemplazado por lo anterior**
- [ ] ~~`System.Linq.Enumerable` subset~~ — diferido: requiere delegates/lambdas (spec §20,
      nunca implementado en ninguna fase), que es una feature nueva grande, no un método BCL
      suelto. Sin esto, LINQ no es viable aunque se agreguen `Where`/`Select` como nombres.
- [ ] ~~`System.Nullable<T>`, `System.Convert`, reflection-lite (`typeof`/`GetType`)~~ —
      evaluado contra los 7 paquetes reales probados (ver certificación abajo): el impacto
      medido era bajo comparado con `MethodSpec` y las comparaciones sin signo, que
      **sí** se implementaron. Ajuste de prioridad, no trabajo pendiente sin mirar.

**Tests**
- [x] Checker: assembly propio se autocertifica compatible bajo `rules`/`netstandard-lite`
      (`TestAnalyze_OwnAssemblyIsCompatible`), perfil `minimal` rechaza el modelo de objetos,
      fixture nueva `Unsupported.cs` (usa `System.Array`, deliberadamente no soportado) prueba
      el finding exacto, límites compatible/partial/unsupported probados con datos sintéticos
- [x] NuGet: TFM (formas corta y larga, prioridad, `net6.0-windows` excluido, opt-in
      `AllowModernNet`), `.nuspec` (agrupado + legacy + XML malformado), resolver (cadena
      transitiva, diamante con conflicto de versión resuelto a la mayor, detección de ciclos,
      dependencia sin asset seleccionable no aborta la resolución), lockfile y manifest
      round-trip — todo con paquetes `.nupkg` sintéticos en memoria, sin red
- [x] Fuzz tests nativos de Go: `FuzzParseNuSpec`, `FuzzOpenPackage` (además de los de Fase 2.5
      en pe/metadata/il) — corridas manuales combinadas ~5.3M ejecuciones, 0 panics

### Certificación de paquetes NuGet reales

Se probaron **7 paquetes reales y populares** descargados en vivo de `api.nuget.org` contra
`vmnet check package --profile=netstandard-lite` (métricas con el estado del código al cierre
de Fase 3, incluye las correcciones de `MethodSpec` y sin-signo):

| Paquete | Métodos analizados | Métodos limpios | Motivo principal de lo que falta |
|---|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 285 | 85.6% | `ldsfld`/`ldarga.s` en overloads con mensaje custom |
| `Newtonsoft.Json@13.0.3` | 4064 | ~46% | `System.Array`, static fields, algo de reflection |
| `System.Text.Json@8.0.5` | 3577 | ~41% | `System.Array`, `byref`, intrínsecos de bajo nivel |
| `FluentValidation@11.9.2` | 1289 | ~41% | reflection pesada — coincide con el ejemplo de la spec §23.4 |
| `Semver@2.3.0` | 423 | ~38% | `byref`, `System.Array` |
| `Humanizer.Core@2.14.1` | 1597 | ~34% | `System.Array`, BCL de formateo de texto |
| `SimpleBase@4.0.0` | 258 | ~33% | `byref`, `System.Array` (algoritmos de codificación) |

Ninguno certifica "compatible" al 100% — esperado y honesto: son librerías reales que usan
arrays, campos estáticos y reflection extensivamente, nada de lo cual está en el alcance actual
(`docs/ROADMAP.md` ya lo documenta como diferido). El valor del checker es exactamente mostrar
esto con precisión método por método, no inflar el resultado.

**Pero además, `vmnet` ejecuta funciones reales de 3 de esos paquetes**, sin modificar el
`.dll` ni el código fuente — spec §36 pide certificar paquetes NuGet "puros" con ejecución real:

- `Newtonsoft.Json.Utilities.MathUtils.ApproxEquals(double, double)` — comparación de punto
  flotante con épsilon, incluye casos borde reales
- `System.HexConverter.IsHexUpperChar(int)` — el mismo método que expuso el bug de
  comparaciones sin signo; ahora pasa, incluyendo el caso `' '` que antes fallaba
- `SimpleBase.Base32.getAllocationByteCountForDecoding(int)` — aritmética entera

Verificable con `VMNET_NETWORK_TESTS=1 go test ./ -run TestCertifiedNuGetPackages -v` (baja los
tres paquetes en vivo) o corriendo `examples/nuget-basic` (agrega + restaura + ejecuta
`SimpleBase` real vía la API pública, incluida la resolución de sus 4 dependencias
transitivas).

### Notas de alcance

```txt
- Cliente NuGet: endpoint de flat-container hardcodeado (api.nuget.org), no hay descubrimiento
  vía v3/index.json — no soporta feeds privados/mirrors todavía. Documentado, no accidental.
- Resolución de versiones: highest-version-wins, no el algoritmo real de rangos de NuGet.
- DependenciesFor no re-valida el TFM contra las reglas de selección de vmnet — asume que el
  caller (el resolver) ya eligió un TFM válido. Se encontró y corrigió durante el testing: la
  primera versión mezclaba "qué grupo de dependencias corresponde a este TFM" con "es este TFM
  válido para vmnet", que son preguntas distintas.
- System.Array, try/catch/finally, delegates/lambdas (y por lo tanto LINQ), reflection más allá
  de lo que ya resuelve el checker genéricamente: siguen sin soportarse. Con los datos de la
  tabla de arriba, System.Array es el bloqueador #1 real en paquetes NuGet reales — candidato
  natural para la próxima fase de expansión de BCL, más que reflection-lite.
```

### Demo de cierre de Fase 3 — "Sabemos qué funciona, y reusamos el ecosistema" (~10 min)

1. `vmnet check package FluentValidation@11.9.2 --profile=netstandard-lite` → reporte
   "partial" con razones concretas agrupadas (reflection, opcodes), coincide con el ejemplo de
   la spec §23.4 casi al pie de la letra.
2. `vmnet check package SimpleBase@4.0.0` → también "partial", pero mostrar que es honesto:
   258 métodos analizados, no un "no funciona" genérico.
3. `examples/nuget-basic`: `vmnet add SimpleBase@4.0.0` + restore en vivo (resuelve 4
   dependencias transitivas reales) + `vm.LoadPackage("SimpleBase")` + llamar
   `Base32.getAllocationByteCountForDecoding` de verdad, con resultados correctos.
4. Bonus técnico (para audiencia de ingeniería): contar cómo se encontró el bug de
   comparaciones sin signo probando `System.Text.Json` real — el checker y la certificación no
   son solo demos, encontraron y motivaron una corrección de correctitud real.

**Mensaje de venta:** "No prometemos el mundo — probamos, de forma transparente, exactamente
qué código C# corre, con números reales sobre 7 librerías populares. Y ya ejecutamos funciones
reales de paquetes NuGet publicados, no solo DLLs de juguete propias — el proceso de probarlo
contra código real nos hizo encontrar y arreglar un bug de correctitud que ningún test propio
hubiera detectado."

---

## Fase 4 — v1.0 listo para producción ("Ready to ship")

**Objetivo:** convertir el motor funcional en un producto adoptable, confiable, documentado y
con benchmarks — el paquete completo para que un equipo de ingeniería apruebe un piloto real.

### Tareas

**Seguridad / sandbox**
- [ ] Modelo `Permissions` completo (`AllowConsole/AllowFileRead/AllowNetwork`, deny-by-default)
      conectado a todos los métodos nativos de BCL
- [ ] `MaxArrayLength`/`MaxStringBytes`
- [ ] `docs/security.md` — threat model, qué se bloquea por default

**Modelo de errores**
- [ ] Catálogo completo de códigos `VMNET_*` (spec §30.2) implementado consistentemente
- [ ] Stack traces de excepciones managed pulidos (formato spec §18.3)

**Performance / benchmarks**
- [ ] Suite de benchmarks (spec §32): loop aritmético, concat de strings, JSON in/out,
      allocación de objetos, `List.Add`, lookup de `Dictionary`, 10k llamadas a rule engine
- [ ] Comparación vs Go nativo y, donde sea viable, vs ejecución nativa CoreCLR
- [ ] Cache de resolución de métodos/tokens, pasada de optimización de hot paths

**API/CLI estables**
- [ ] Congelar API pública Go (spec §6) para v1.0, compromiso semver
- [ ] Set completo de comandos CLI (inspect/il/check/run/add/restore/packages)
- [ ] Matriz CI multiplataforma: Linux/macOS/Windows, verificar `CGO_ENABLED=0`

**Tests**
- [ ] Suite golden completa (spec §28.1–28.5)
- [ ] Meta de cobertura acordada con stakeholders (ej. ≥70% en paquetes core)

**Documentación (spec §33)**
- [ ] README completo (qué es / qué no es, quickstart, perfiles, límites conocidos)
- [ ] `docs/architecture.md`, `supported-il.md`, `supported-bcl.md`, `nuget-support.md`,
      `compatibility-profile.md`, `security.md`, `roadmap.md`
- [ ] `/examples`: hello, rules, calculator, nuget-basic — ejecutables y documentados

### Demo de cierre de Fase 4 — "Listo para producción" (~15 min, foco ejecutivo)

1. Cero-a-corriendo en menos de 5 minutos en una máquina limpia: `go get`, `dotnet build` de
   un plugin, `vmnet run` — cronometrado en pantalla.
2. Gráfico de benchmarks en pantalla: vmnet vs CoreCLR vs Go plano para el workload de rule
   engine — números honestos, mostrando que alcanza para el caso de uso objetivo.
3. Demo de seguridad: un plugin que intenta leer un archivo o hacer una llamada HTTP es
   bloqueado por los permisos por default, con log claro.
4. Recorrido de docs/README — tablas de IL/BCL soportado, perfiles de compatibilidad, lista de
   paquetes NuGet certificados.
5. CI en verde en Linux/macOS/Windows sin SDK de .NET instalado en los runners (solo se usa en
   un paso de dev separado para compilar las fixtures de test).

**Mensaje de venta:** "Ya no es un prototipo — está versionado, documentado, benchmarkeado,
securizado y es multiplataforma. Está listo para un piloto de integración real."

---

## Registro de riesgos (mapeado a fases)

| Riesgo | Fase donde se expone | Mitigación |
|---|---|---|
| BCL (`System.*`) es más difícil que el parser IL | 2–3 | Empezar mínima, implementar por demanda, checker fuerte, certificar paquetes concretos |
| NuGet arbitrario tiene demasiada variedad | 3 | Solo `netstandard2.0` inicialmente, bloquear native assets/P-Invoke/reflection pesada, catálogo curado |
| Expectativa de "corre cualquier DLL .NET" | Todas (comunicación) | Naming claro, `vmnet check` obligatorio antes de cargar terceros, docs explícitas de qué no es |
| Performance de intérprete vs CoreCLR | 4 | IR propia, cache de tokens/métodos, benchmarks honestos, roadmap futuro de codegen IL→Go |
| Dependencia de .NET SDK para generar fixtures de test | 0–1 | Documentar que es solo dev-dependency, nunca runtime; considerar DLLs de fixture pre-compiladas versionadas en el repo |

## Fuera de las 4 fases (roadmap post-v1.0)

- v1.5 — backend híbrido (`pure-go` / `coreclr` fallback / `worker` process) — spec §39
- `vmnet transpile` — codegen IL → Go source (migración C# → Go) — spec §38
- Ampliación de perfil `netstandard-lite` más allá de los paquetes certificados iniciales
- Reflection completa, async/Task cooperativo más allá de `Task.FromResult`/`CompletedTask`

## Criterios de aceptación de referencia

Ver spec original §35 (MVP) y §36 (NuGet v1) — se usan como checklist de salida de Fase 1/2 y
Fase 3 respectivamente, sin duplicarlos aquí.
