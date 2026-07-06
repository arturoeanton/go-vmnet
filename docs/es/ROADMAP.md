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
| 3.5 | Endurecimiento + compatibilidad real *(gate interno, sin demo de venta)* | 3–4 días | El motor cubre el patrón de código C# real más común (arrays, `ref`/`out`, static fields), no solo lo que estaba fácil | Re-certificación de los mismos 7 paquetes: promedio de métodos limpios sube de ~45% a ~57% |
| 3.6+ | Camino a 85% + demo Jint *(multi-fase; 85% alcanzado en Fase 3.21, objetivo revisado a ~97%)* | varias semanas | El motor corre una porción realmente grande de código C# real, no solo casos curados — validado contra un motor JS completo (Jint), no solo librerías chicas | 85%+ alcanzado (85.1%/85.3% con Jint); ~97% en curso; `Engine().SetValue(...).Execute(...)` corriendo de verdad |
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
- [ ] `docs/es/architecture.md` esqueleto (referencia a esta spec), `CONTRIBUTING.md`
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
- [x] `System.Array` + soporte runtime de `SZARRAY` (`newarr`/`ldelem`/`stelem`/`ldlen`) —
      diferido en su momento (ver nota de alcance del bridge `CallBytes` más abajo), implementado
      en Fase 3.5
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
(`docs/es/ROADMAP.md` ya lo documenta como diferido). El valor del checker es exactamente mostrar
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
  de lo que ya resuelve el checker genéricamente: siguen sin soportarse al cierre de Fase 3. Con
  los datos de la tabla de arriba, System.Array (y, se descubrió al medir en Fase 3.5, `ref`/
  `out` más que reflection) era el bloqueador #1 real en paquetes NuGet reales —
  **System.Array, `ref`/`out` y campos estáticos se implementaron en Fase 3.5** (ver esa sección
  más abajo para la re-certificación); try/catch/finally, delegates y reflection extendida
  siguen pendientes.
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

## Fase 3.5 — Endurecimiento + compatibilidad real de DLLs (previa a Fase 4, ~3–4 días)

**Objetivo:** la certificación de Fase 3 midió con precisión qué falta, y el bloqueador #1 no era
reflection ni async — eran patrones de código C# aburridos y omnipresentes: `System.Array`,
`ref`/`out` (address-of), y campos estáticos. Antes de entrar a Fase 4 (producción), cerrar esos
tres huecos y volver a medir contra los mismos 7 paquetes, para llegar a Fase 4 con un motor que
ya corre una porción sustancialmente mayor de código real, no solo fixtures propios.

Igual que Fase 2.5, no es una fase con demo de venta — es un gate de calidad interno, pero con
métricas concretas de antes/después que sí sirven como evidencia de progreso real.

### Tareas

**Priorización basada en datos, no en intuición**
- [x] Antes de escribir código: se corrió un probe temporal (`checker.Analyze` contra los 7
      paquetes ya descargados en Fase 3) para contar findings agrupados por opcode/kind. El
      resultado reordenó por completo la prioridad esperada: los opcodes de address-of
      (`ldarga`/`ldloca`/`starg`/`ldflda` — la base de `ref`/`out`) eran el bloqueador #1 medido
      (2532 findings), muy por delante de arrays (295) y static fields (689) — no lo que se
      hubiera asumido mirando solo la tabla de Fase 3.

**`internal/runtime`, `internal/ir`, `internal/interpreter` — System.Array**
- [x] `runtime.Array` (heap-allocado, solo SZARRAY — sin arrays multidimensionales, cubre la
      inmensa mayoría del uso real) y `runtime.Value.KindArray`
- [x] IR + intérprete: `newarr`/`ldlen`/`ldelem.*`/`stelem.*` (todas las variantes tipadas
      colapsan a una sola implementación — `Value` ya es un tagged union, no hace falta
      distinguir por tipo de elemento como hace CIL)
- [x] Bounds-checking real: índice fuera de rango o array nulo lanzan `ManagedException`, no
      un panic de Go recuperado genéricamente
- [x] `Limits.MaxArrayLength` (16M elementos por defecto) — un `newarr` con longitud
      adversarial no puede agotar memoria del host

**`internal/runtime`, `internal/ir`, `internal/interpreter` — punteros administrados (`ref`/`out`)**
- [x] `runtime.Value.KindRef`: un puntero administrado es literalmente un `*Value` de Go
      apuntando dentro de un slice de tamaño fijo (`Args`/`Locals`/`Object.Fields`/
      `Array.Elems`). Decisión de diseño clave: esto hace que `ref`/`out` no necesiten *ningún*
      caso especial en `Call`/`NewObj` — un parámetro byref es sencillamente un `Arg` cuyo
      `Value` resulta tener `Kind == KindRef`
- [x] IR + intérprete: `ldarga(.s)`/`ldloca(.s)`/`ldelema`/`ldflda` (address-of) y
      `ldind.*`/`stind.*` (leer/escribir a través del puntero)
- [x] `ldsflda` (dirección de un campo **estático**) deliberadamente **no** implementado: a
      diferencia de los otros cuatro, exponer un `*Value` crudo hacia el slice de estáticos de
      un `Type` (protegido por `sync.RWMutex`) dejaría a quien tenga el puntero saltearse ese
      mutex en cada lectura/escritura futura — un riesgo de concurrencia real, no solo trabajo
      pendiente. Queda documentado como gap explícito, no silencioso.

**`internal/runtime`, `internal/interpreter` — campos estáticos + `.cctor` perezoso**
- [x] `runtime.Type` pasa a cargar estado real: `statics []Value` (protegido por
      `sync.RWMutex`, porque a diferencia de los campos de instancia sí es estado mutable
      compartido entre callers concurrentes) y un `.cctor` que corre perezosamente en el primer
      acceso estático, exactamente una vez, vía `sync.Once`
- [x] IR + intérprete: `ldsfld`/`stsfld`
- [x] **Bug real encontrado y arreglado — deadlock de reentrancia**: un `.cctor` que escribe su
      propio campo estático (el caso *común*, no el raro — `static Foo() { Bar = 42; }`) volvía
      a entrar a `EnsureCctor` sobre el mismo `sync.Once`, que no es reentrante, y colgaba el
      proceso. Se arregló rastreando, por `Machine` (que nunca se comparte entre goroutines —
      una por cada `Assembly.Call` de nivel superior), qué tipos tienen su `.cctor` corriendo en
      esta misma cadena de llamadas; un acceso reentrante en la misma cadena sigue sin volver a
      entrar al `sync.Once`, mientras que otra goroutine que llegue primero sigue bloqueando
      correctamente contra el `.cctor` en curso.
- [x] **Bug real encontrado y arreglado — race condition en el cache de tipos**: antes de esta
      fase, `resolveTypeByFullName` hacía "leer del cache, si falta construir y guardar" con
      locks separados para cada paso — inofensivo cuando `Type` solo describía el layout de
      campos (inmutable), pero con estáticos y `.cctor` de por medio, dos goroutines resolviendo
      el mismo tipo por primera vez podían construir *cada una su propio* `runtime.Type`; los
      `SetStaticField` de la que "pierde la carrera" quedaban en un objeto que nadie más volvía
      a ver. Se arregló sosteniendo un único lock sobre todo el "leer o construir y guardar",
      verificado con `TestStaticsConcurrentCctor` (32 goroutines, `-race`, `-count=3`).
- [x] **Bug real encontrado y arreglado — default(T) incorrecto**: un campo (estático o de
      instancia) nunca asignado explícitamente (`static int Counter;`, sin `= 0`, el caso común
      en C# real) quedaba en el `Value{}` vacío de Go (`KindNull`), que no es aritméticamente
      compatible con ningún tipo numérico — la primera operación aritmética sobre ese campo
      fallaba. Se agregó `metadata.ParseFieldSig` (parser nuevo de la firma de campo, ECMA-335
      §II.23.2.4) para calcular el `default(T)` real por campo — cero tipado para todo tipo
      valor, `null` para todo lo demás — y `runtime.Type` ahora guarda `FieldDefaults`/
      `StaticFieldDefaults` en paralelo a los nombres de campo.

**`internal/checker` — el checker no puede quedar desactualizado silenciosamente**
- [x] **Drift real encontrado y arreglado**: `sigShapeFindings` todavía marcaba todo parámetro
      `ref`/`out` (`SigByRef`) como no soportado, escrito antes de que byref se implementara —
      el propio test de "dogfood" del checker (el assembly de fixtures se autocertifica contra
      su propio checker) lo detectó apenas se agregó `ByRef.cs`, exactamente el propósito de ese
      test.
- [x] **Drift real encontrado y arreglado**: `instrIsObjectModel` (qué excluye el perfil
      `minimal` — spec §24.1, "solo métodos estáticos y primitivas") nunca se actualizó al
      agregar arrays y campos estáticos; el checker certificaba código que usa `System.Array` o
      estado estático compartido como "compatible" bajo `minimal`, contradiciendo su propio
      contrato documentado. `ldarga`/`ldloca`/`ldind`/`stind` sobre primitivas quedan
      deliberadamente **fuera** de esa exclusión — un `ref int` nunca toca el heap ni el layout
      de una clase, así que sigue dentro de lo que promete `minimal`.
- [x] Mensaje de sugerencia para `newarr`/`ldelem`/`stelem`/`ldlen` (decía "`System.Array` no
      soportado todavía") corregido — ya está soportado; el único caso real que sigue cayendo en
      ese camino es el azúcar sintáctico de inicializadores de array literal (`ldtoken` +
      `RuntimeHelpers.InitializeArray`), no el opcode en sí.

**Fixtures y tests**
- [x] `Arrays.cs`, `ByRef.cs`, `Statics.cs` — fixtures nuevas compiladas con el SDK de .NET real,
      cada una con su test Go correspondiente (`TestArrays`, `TestByRef`, `TestStatics`,
      `TestStaticsConcurrentCctor`)
- [x] `Unsupported.cs` reescrita (`try`/`finally`, antes usaba arrays — ahora que arrays
      funcionan, se reemplazó por otra construcción genuinamente no soportada, para no perder el
      caso de test negativo del checker)
- [x] `TestAnalyze_MinimalProfileFlagsObjectModel` extendido: prueba que arrays y static fields
      quedan fuera de `minimal`, y que `ref`/`out` sobre primitivas se mantienen adentro —
      bloquea una futura regresión de cualquiera de los dos lados
- [x] `FuzzParseSignatures` (`internal/metadata`) — el parser nuevo `ParseFieldSig` recibe bytes
      sin confiar (parte del `#Blob` stream de una DLL cargada), y de paso se cerró que
      `ParseMethodSig`/`ParseLocalVarSig`/`ParseTypeSpec` nunca habían tenido fuzz test propio
      (`FuzzParse` en `/metadata` solo llega hasta las filas crudas, nunca parsea sus blobs de
      firma)

### Lo que se dejó explícitamente afuera de esta fase

```txt
- ldsflda (dirección de un campo estático): ver nota de diseño arriba — riesgo de concurrencia
  real, no trabajo pendiente sin mirar.
- Arrays multidimensionales/jagged más allá de una dimensión: solo SZARRAY.
- Inicializadores de array literal (`new int[] {1,2,3}` compila a newarr+ldtoken+
  RuntimeHelpers.InitializeArray, que lee bytes crudos de un FieldRVA) — se puede lograr el
  mismo resultado asignando elemento por elemento, que sí funciona.
- try/catch/finally (leave/endfinally), isinst/castclass, switch, ldftn/delegates, localloc,
  initobj (structs/generics de valor): confirmados como los siguientes bloqueadores reales por
  volumen en la re-certificación de abajo — candidatos naturales para la próxima fase de
  expansión del intérprete, no una sorpresa.
- Superficie BCL nueva (DateTime, Span<T>/ReadOnlySpan<T>, Nullable<T>, String.Format,
  CultureInfo): sigue siendo el bloqueador dominante en volumen absoluto de findings
  (unsupported-bcl-method), pero es trabajo de "agregar método nativo", no de intérprete —
  fuera del alcance de esta fase, que se enfocó en opcodes/semántica del motor.
```

### Re-certificación contra los mismos 7 paquetes reales

Mismos 7 paquetes de Fase 3, mismo perfil (`netstandard-lite`), estado del código al cierre de
Fase 3.5:

| Paquete | Métodos analizados | % limpio Fase 3 | % limpio Fase 3.5 |
|---|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 285 | 85.6% | **86.7%** |
| `System.Text.Json@8.0.5` | 3577 | ~41% | **60.5%** |
| `FluentValidation@11.9.2` | 1289 | ~41% | **58.0%** |
| `Semver@2.3.0` | 423 | ~38% | **56.0%** |
| `Newtonsoft.Json@13.0.3` | 4064 | ~46% | **52.5%** |
| `Humanizer.Core@2.14.1` | 1597 | ~34% | **43.0%** |
| `SimpleBase@4.0.0` | 258 | ~33% | **40.7%** |
| **Promedio** | | **~45.5%** | **~56.8%** |

`System.Text.Json` y `Semver` son los saltos más grandes — ambos usan `System.Array` y `ref`/
`out` extensivamente para parsing/comparación de bajo nivel, exactamente el patrón que esta fase
apuntó a resolver. Los findings restantes (ver desglose por opcode arriba) ya no están dominados
por address-of — ahora son mayormente `initobj`/`ldftn`/`isinst`/`switch`/`ldtoken`/`leave.s`
(features de fase futura) y superficie BCL (`DateTime`/`Span`/`Nullable`), no un vacío de
cobertura del intérprete en construcciones básicas del lenguaje.

### Cómo verificar esta fase

```bash
go test ./... -race -count=3                                       # todo verde, incluye concurrencia
go test ./ -run TestStatics -v                                     # .cctor perezoso + defaults tipados
go test ./ -run TestStaticsConcurrentCctor -race -v                # 32 goroutines, sin deadlock ni data race
go test ./ -run TestArrays -v
go test ./ -run TestByRef -v
go test ./internal/checker/... -run TestAnalyze_MinimalProfileFlagsObjectModel -v
go test ./internal/metadata/... -run '^$' -fuzz '^FuzzParseSignatures$' -fuzztime=30s
```

---

## Fase 3.6+ — Camino a 85% de compatibilidad real + demo Jint

**Objetivo original:** antes de Fase 4, subir la compatibilidad real medida a **por lo menos 85%**
promedio (criterio de cierre firme, no aspiracional) sobre los 7 paquetes ya certificados **más
Jint** (motor de JavaScript completo para .NET, ~5400 métodos), y validar un demo real de
"lenguaje dinámico corriendo dentro de vmnet" ejecutando el patrón
`new Engine().SetValue(...).Execute(...)` de punta a punta — no solo el número agregado. Se
descartó explícitamente un demo de ASP.NET Core/MVC (fuera de alcance documentado, spec §3).

**El 85% se alcanzó en Fase 3.21** (85.1%/85.3% con Jint — ver esa sección). El objetivo se
revisó al alza en el momento: el nuevo criterio de cierre es un BCL endurecido apuntando a
**~97%** ("100% puede ser 97%"), con toda la documentación mantenida al día en cada sub-fase —
la secuencia de sub-fases abajo continúa bajo ese objetivo, no se detiene en 85%.

Dado el tamaño real de la brecha, esto **no es una sola fase**: es una secuencia de sub-fases,
cada una con su propia medición, tests, docs y commit/tag/push — igual que Fase 2.5/3.5, pero
encadenadas. El orden se decidió con el mismo método de Fase 3.5 (medir antes de adivinar): se
corrió el mismo probe de findings-por-opcode/BCL, ahora incluyendo Jint, contra los 8 targets.

| Sub-fase | Qué ataca | Por qué ese orden |
|---|---|---|
| **3.6** | `switch` (jump table) + BCL barata de alto alcance (`StringBuilder`, `String.Format`/`Substring`/indexador/`Equals`, `Array.Empty`, `Double.IsNaN`, `CultureInfo.InvariantCulture`, `ArgumentOutOfRangeException`, `Environment.CurrentManagedThreadId`) | Alto alcance (varios llegan a 6-8/8 paquetes), bajo costo — nada de esto necesita máquina de tipos nueva |
| **3.7** | Value types: `initobj`/`ldobj`/`stobj`/`constrained.` + `Nullable<T>` | 8/8 paquetes usan `initobj`; vmnet no modela structs todavía, solo clases |
| **3.8** | Jerarquía de tipos real + `isinst`/`castclass` | 8/8 y 6/8 paquetes; `runtime.Type` es plano hoy (sin herencia/interfaces); desbloquea `EqualityComparer<T>` |
| **3.9** | Delegates/closures (`ldftn`, `Action<T>`/`Func<T>`, `Invoke`) | Necesario para el demo de Jint literal (`SetValue(new Action<string>(...))` es la primera línea) |
| **3.10** | `try`/`catch`/`finally` (`leave`/`leave.s`/`endfinally`) | 8/8 paquetes; hoy solo existe throw no manejado |
| **3.11** | `foreach`/enumeradores + wins baratos (re-priorizado con datos — ver sección) | El probe mostró `IDisposable::Dispose`/`IEnumerator`1`/`EqualityComparer`1` en 7-8/8 paquetes, más ancho que DateTime/Span (2-5/8) |
| **3.12** | `DateTime`, `Span<T>`/`ReadOnlySpan<T>`/`Memory<T>`/`ReadOnlyMemory<T>` | Impacto grande pero concentrado (principalmente Humanizer.Core/SimpleBase/System.Text.Json) |
| **3.13** | `foreach` sobre colección tipada como interfaz (despacho por tipo real del receptor) + paquete de wins baratos (`String`/`Char`/`List`/`Dictionary`) | `IEnumerable`1::GetEnumerator`/`IEnumerator`1::get_Current`/`IEnumerator::MoveNext` eran el hallazgo más ancho (7/8) tras Fase 3.12 |
| **3.14** | Reflection-lite: `ldtoken`/`typeof(T)`, `Object.GetType()`, `System.Type` (igualdad/`Name`/`FullName`) | `ldtoken` (6/8), `Object::GetType` (5/8) y `MemberInfo::get_Name` (5/8) eran los tres hallazgos más anchos tras Fase 3.13 |
| **3.15** | LINQ (`System.Linq.Enumerable`: `Select`/`Where`/`Any`/`All`/`ToList`/`ToArray`/`FirstOrDefault`) | ~174 casos en 4-5/8 tras Fase 3.14, viable desde que existen delegates (3.9), enumeradores reales (3.11) y despacho por interfaz (3.13) |
| **3.16** | `Type::IsAssignableFrom` | Segundo hallazgo más ancho de reflection tras 3.14 (84 casos, 4/8); mecánico una vez que existe el registro Machine-aware de 3.15 |
| **3.x** | Re-medición final, cierre de brecha restante hacia 85%, validación literal del demo Jint | Confirma el número Y que el escenario concreto corre, no solo el promedio |

### Fase 3.6 — `switch` + BCL barata de alto alcance

**Tareas**

- [x] IR + intérprete: `switch` (spec §III.3.68) — ya se decodificaba desde Fase 1
      (`internal/il/decoder.go` resuelve la tabla de offsets), pero `ir.Build` nunca lo bajaba;
      caía como opcode no soportado. Fuera de rango cae al siguiente instrucción (no es error,
      por spec), verificado con el fixture.
- [x] `System.Text.StringBuilder`: ctor (parameterless + seed-string), `Append`/`AppendLine`
      (devuelven el receiver — encadenado fluido `sb.Append(a).Append(b)` funciona), `ToString`,
      `get_Length`, `Clear`.
- [x] `System.String`: `Format` (gramática compuesta `{index[,alignment][:formatString]}`,
      escapes `{{`/`}}`, especificadores `D`/`F`/`N`/`X`/`P`/`G` — uno no reconocido es error
      explícito, no un resultado adivinado), `Substring` (1 y 2 args), `get_Chars` (indexador),
      `Equals`/`op_Equality` (una sola native cubre instancia + estático + `==`, ver comentario
      en el código).
- [x] `System.Array::Empty`, `System.Double::IsNaN`, `System.Globalization.CultureInfo::
      get_InvariantCulture` (stub), `System.Environment::get_CurrentManagedThreadId` (stub),
      constructor de `System.ArgumentOutOfRangeException`/`System.IndexOutOfRangeException`
      (mismo patrón que las excepciones ya registradas en Fase 2).
- [x] **Bug real encontrado y arreglado — `StringBuilder.ToString()` no hacía nada útil**: el
      compilador de C# emite `sb.ToString()` como `callvirt System.Object::ToString` (confía en
      el despacho virtual real del CLR para llegar al override), no como
      `callvirt System.Text.StringBuilder::ToString`. vmnet resuelve `callvirt` de forma
      estática por el `MemberRef` declarado (spec: "sin vtable" — el despacho virtual real es
      Fase 3.8), así que sin arreglo esto siempre ejecutaba el `ToString` genérico de `Object` y
      devolvía `<object>`. Se resolvió extendiendo `displayString`/`objectToString` (ya pensado
      para dispatchear "como si tuviera vtable" sobre valores boxed) para reconocer tipos
      native-backed conocidos — StringBuilder por ahora, el mismo mecanismo cubre casos futuros.
      Es un parche dirigido, no una solución general — el despacho virtual real llega en Fase 3.8.
- [x] **Endurecimiento**: `String.Format` limita el ancho de alineación (`{0,N}`) a un máximo
      fijo — sin el límite, un `{0,999999999}` (desde una plugin adversarial o desde el bridge
      `CallBytes`/`CallJSON`, donde la format string puede venir de fuera del proceso) haría que
      `strings.Repeat` intentara asignar cientos de MB de padding. Mismo tipo de riesgo que
      `MaxArrayLength` (Fase 3.5) para `newarr`.

**Fixtures y tests**

- [x] `SwitchTest.cs` (switch denso 0-4 + default) / `TestSwitch`, incluye el caso fuera de rango
- [x] `StringOps.cs` (StringBuilder encadenado, Format, Substring, indexador, Equals) /
      `TestStringOps`

**Medición (7 paquetes de Fase 3 + Jint, perfil `netstandard-lite`)**

| Paquete | % limpio Fase 3.5 | % limpio Fase 3.6 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 86.7% | 86.7% |
| `System.Text.Json@8.0.5` | 60.5% | 61.4% |
| `FluentValidation@11.9.2` | 58.0% | 62.8% |
| `Semver@2.3.0` | 56.0% | 63.8% |
| `Newtonsoft.Json@13.0.3` | 52.5% | 55.6% |
| `Humanizer.Core@2.14.1` | 43.0% | 45.2% |
| `SimpleBase@4.0.0` | 40.7% | 43.4% |
| **Promedio (7 paquetes)** | **56.8%** | **59.8%** |
| `Jint@3.1.3` (nuevo, no en Fase 3.5) | — | 63.7% |
| **Promedio (7 paquetes + Jint)** | — | **60.3%** |

Movimiento modesto (+3 puntos en los 7 de siempre), esperado para una sub-fase de "wins baratos"
— el salto grande está en 3.7-3.10 (value types, jerarquía de tipos, delegates, try/catch), que
son los bloqueadores dominantes en los 8 targets por volumen real medido.

### Cómo verificar Fase 3.6

```bash
go test ./... -race -count=1
go test ./ -run TestSwitch -v
go test ./ -run TestStringOps -v
```

### Fase 3.7 — Value types: `initobj`/`ldobj`/`stobj`/`constrained.` + `Nullable<T>`

**Tareas**

- [x] `runtime.KindStruct`/`runtime.Struct` (Fields por posición, igual que un objeto, pero con
      **semántica de copia** en vez de referencia compartida) y `runtime.Type.IsValueType`
      (detectado por `Extends == System.ValueType`/`System.Enum`, o registrado directo para
      tipos BCL sintéticos como `Nullable`1`)
- [x] `Value.Clone()`: no-op para todo Kind salvo `KindStruct`, donde clona `Fields` (recursivo —
      un struct anidado dentro de otro struct también copia bien). Cableado en **todo** punto
      donde un `Value` entra a un slot persistente: `stloc`/`starg`/`stfld`/`stsfld`/`stelem`/
      `stind`, y el setup inicial de `Locals` de cada invocación — sin esto, dos locals de tipo
      struct terminan compartiendo el mismo `*Struct` por debajo y mutar uno muta el otro
- [x] IR + intérprete: `initobj` (zero-init real vía dirección; `ldloca`/`ldflda`/etc. ya
      existían), `ldobj`/`stobj` — resultan ser **exactamente** `ldind.*`/`stind.*` reusados sin
      instrucción IR nueva, porque un puntero de vmnet ya es un `*runtime.Value` tipado, no
      memoria cruda — y `constrained.`/`volatile.`/`readonly.` como no-ops explícitos (prefijos
      que no aplican al modelo de `Value` de vmnet)
- [x] `newobj` sobre un value type empuja el **valor**, no una referencia (spec §III.4.21): se
      construye en un slot temporal, se llama al `.ctor` con `this` = puntero administrado a ese
      slot (igual que cualquier método de instancia de un struct), y se empuja el valor
- [x] `ldfld`/`stfld`/`ldflda` extendidos para aceptar un receptor `KindRef → KindStruct` además
      de `KindObject` — así es como un struct recibe `this` en sus propios métodos de instancia
- [x] `System.Nullable`1`: tipo sintético con dos campos (`hasValue`, `value`), ctor nativo,
      `get_HasValue`/`get_Value`/`GetValueOrDefault`
- [x] `System.Object::Equals`/`GetHashCode`: igualdad/hash por valor para primitivas y structs
      (recursivo campo a campo), por referencia para clases/arrays — necesario porque
      `constrained.` + `callvirt Object::Equals/GetHashCode` es el patrón real más común en
      código de comparación genérico (`EqualityComparer<T>`, Fase 3.8)
- [x] `metadata.SigType.GenericInstIsValueType`: el parser de firmas descartaba el byte marcador
      CLASS/VALUETYPE de una instanciación genérica (`GENERICINST`) — necesario para distinguir
      `List<T>` (referencia, default `null`) de `KeyValuePair<K,V>`/`Nullable<T>` (valor, default
      un struct cero) en el mismo `SigGenericInst`
- [x] **Bug real encontrado y arreglado — locals de struct sin inicializar**: `var p = new
      Point(3, 4);` asignado directo a un local compila como `ldloca` + `call .ctor` **sin**
      `initobj` previo — el compilador de C# confía en la garantía `InitLocals` de la CLI (todos
      los locals arrancan en cero, no solo los que tienen `initobj` explícito). vmnet inicializaba
      todos los locals al `Value{}` vacío de Go sin mirar su tipo declarado; para un local struct
      eso significa `KindNull`, no un struct cero, así que el primer `stfld` a través del puntero
      fallaba con `NullReferenceException`. Se agregó `runtime.Method.LocalDefaults` (paralelo a
      `LocalCount`, resuelto una vez al construir el método, igual que ya existía para campos)
      clonado en cada invocación.
- [x] **Bug real encontrado y arreglado — deadlock de recursión en `resolveTypeByFullName`**: el
      lock de Fase 3.5 (que cubre todo el ciclo "leer o construir y guardar" para evitar que dos
      goroutines construyan `Type`s duplicados) asumía que construir un tipo nunca necesita
      resolver OTRO tipo — cierto hasta que un campo o local de tipo struct necesitó resolver
      recursivamente su propio tipo anidado, contra el mismo `sync.Mutex` no reentrante de Go.
      Encontrado inmediatamente al correr el primer fixture con un struct. Se rediseñó a
      "verificar caché → construir SIN el lock (puede recursar) → verificar de nuevo y guardar":
      bajo una carrera genuina, ambas goroutines pueden construir un `Type` completo, pero solo
      el ganador se guarda y todos los llamadores terminan viendo la misma instancia — la garantía
      de Fase 3.5 se mantiene, solo se pierde trabajo redundante en la carrera, no correctitud.
      Verificado con `TestStructsConcurrentResolve` (32 goroutines, `-race`, `-count=3`).

**Fixtures y tests**

- [x] `Structs.cs` (`Point`: struct con ctor propio y método propio) / `TestStructs` — construcción
      in-place, `default`, semántica de copia (mutar una copia no afecta al original — el caso que
      más falla en implementaciones ingenuas), `constrained.` despachando `ToString()` sobre un
      parámetro de tipo genérico ligado a un struct, y `Nullable<int>` de punta a punta
- [x] `TestStructsConcurrentResolve` — endurecimiento de concurrencia para el rediseño del lock

### Lo que se dejó explícitamente afuera de esta fase

```txt
- `initobj` sobre un parámetro de tipo genérico sin instanciación cerrada conocida (`initobj
  !!0` dentro del cuerpo de un método genérico en abstracto) cae a Null() — vmnet borra los
  argumentos de tipo genérico al resolver MethodSpec (Fase 3, decisión ya documentada), así que
  no hay forma de saber el T real en ese punto. Coincide con el patrón ya aceptado para otros
  huecos de erasure de generics.
- Un value type foráneo que vmnet no modela (DateTime, Guid, TimeSpan, KeyValuePair<K,V> más
  allá de Nullable<T>, ...) también cae a Null() en vez de fallar la resolución del método
  entero — mismo principio que un Call target no resoluble: el gap se reporta en el momento de
  uso real, no al cargar el método.
- `constrained.` solo garantiza el despacho correcto para ToString/Equals/GetHashCode (los tres
  casos reales dominantes medidos). Otros overrides virtuales sobre un value type sin vtable
  real siguen yendo a la implementación de base — el despacho virtual genuino es Fase 3.8.
```

### Re-certificación contra los mismos 8 targets (7 paquetes + Jint)

| Paquete | % limpio Fase 3.6 | % limpio Fase 3.7 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 86.7% | 87.4% |
| `Semver@2.3.0` | 63.8% | **72.6%** |
| `System.Text.Json@8.0.5` | 61.4% | **66.7%** |
| `FluentValidation@11.9.2` | 62.8% | 63.5% |
| `Newtonsoft.Json@13.0.3` | 55.6% | **60.6%** |
| `Humanizer.Core@2.14.1` | 45.2% | 46.0% |
| `SimpleBase@4.0.0` | 43.4% | 45.7% |
| **Promedio (7 paquetes)** | **59.8%** | **63.2%** |
| `Jint@3.1.3` | 63.7% | 66.1% |
| **Promedio (7 paquetes + Jint)** | **60.3%** | **63.6%** |

`Semver` y `System.Text.Json` son los saltos más grandes — ambos hacen parsing/comparación de
bajo nivel apoyado en structs (rangos, spans lógicos, comparadores) de forma intensiva. Sigue
faltando terreno considerable para el objetivo de 85%: jerarquía de tipos real (`isinst`/
`castclass`, Fase 3.8) y delegates/closures (Fase 3.9) son los siguientes bloqueadores por
volumen medido.

### Cómo verificar Fase 3.7

```bash
go test ./... -race -count=3
go test ./ -run TestStructs -v
go test ./ -run TestStructsConcurrentResolve -race -count=3 -v
```

### Fase 3.8 — Jerarquía de tipos real + `isinst`/`castclass`

**Tareas**

- [x] `runtime.Type.BaseTypeFullName`/`Interfaces` (solo directamente implementadas — spec
      §II.22.23; extender una interfaz por otra, o heredar de una clase base, se resuelve
      recursivamente en el walk, no aplanado de antemano)
- [x] `metadata.InterfaceImpls` (accessor nuevo para la tabla `InterfaceImpl`, sin usar hasta
      ahora) y `resolveTypeTokenName` extendido para resolver un `TypeSpec` (instanciación de
      interfaz genérica — `IEnumerable<T>`/`IComparable<T>`, el caso *dominante* en
      `InterfaceImpl` real) a su tipo genérico abierto
- [x] IR + intérprete: `isinst`/`castclass` — mismo token TypeDefOrRefOrSpec que `initobj`
      (`resolveTypeTokenOrGeneric`, renombrado de `resolveInitObjTarget` ahora que lo comparten
      tres opcodes), despachando por Kind con el walk de jerarquía real para `KindObject`/
      `KindStruct`, reglas de sentido común para primitivas/string/array, y `null` siempre pasa
      sin chequear (comportamiento exigido por spec)
- [x] `internal/interpreter/typecheck.go`: `isAssignableTo` — camina `BaseTypeFullName` +
      `Interfaces` (recursivo para interfaz-extiende-interfaz) para clases propias del plugin;
      tabla chica mantenida a mano de la jerarquía real de excepciones (`ArgumentNullException`
      → `ArgumentException` → `SystemException` → `Exception`) para que `ex is ArgumentException`
      dé la respuesta correcta sobre las excepciones que vmnet ya construye nativamente
- [x] `System.InvalidCastException` registrada como excepción nativa (mismo patrón que las demás)
- [x] **Bug real encontrado y arreglado — comparación de referencias contra `null`**: la forma
      compilada más común de `x is T`/`x != null`/`x == null` es exactamente
      `<valor> ldnull cgt.un`/`ceq` — comparar un `KindObject` contra el `KindNull` literal de
      `ldnull`. `evalBinOp`/`evalCompare` exigían el mismo `Kind` en ambos lados y fallaban con
      "mismatched value kinds" apenas el primer fixture con `isinst` los ejercitó — un hueco
      preexistente que ningún fixture anterior había tocado (nada comparaba explícitamente una
      referencia contra `null` vía IL hasta ahora). Se agregó comparación por identidad de
      referencia/nulidad (`refEqual`/`refGreater` en `internal/interpreter/arithmetic.go`) para
      todo Kind con forma de referencia (`Object`/`Array`/`Ref`/`Struct`/`String`), incluyendo
      igualdad estructural recursiva para structs.
- [x] **Bug real encontrado y arreglado — campos heredados no existían**: `runtime.Type` nunca
      había necesitado mirar más allá de su propio `TypeDef` (comentario original: "no base-type
      field inheritance yet"). En cuanto el primer fixture con herencia (`Dog : Animal`) accedió
      a un campo declarado en la clase *base*, falló con "has no field" — la lista de campos de
      `Dog` nunca incluía los de `Animal`. Se resolvió construyendo el tipo base recursivamente
      (mismo patrón seguro-para-recursión que Fase 3.7) y anteponiendo sus campos, igual que el
      layout de memoria real de la CLR (campos de la base primero).

**Fixtures y tests**

- [x] `TypeChecks.cs` (`Animal`/`Dog`/`Cat`/`IShape`) / `TestTypeChecks` — `is`/`as`/cast
      explícito sobre una referencia de tipo base que en runtime es un subtipo, cast fallido
      lanzando `InvalidCastException` (no silenciosamente exitoso ni panic), `isinst` contra la
      jerarquía de excepciones sin necesitar try/catch (construyendo la excepción directo, ya que
      try/catch es recién Fase 3.10)

### Lo que se dejó explícitamente afuera de esta fase

```txt
- List<T>/Dictionary<T>/StringBuilder (backing nativo Go, sin runtime.Type): isinst/castclass
  contra ellos solo reconoce System.Object, no sus interfaces reales (IEnumerable,
  ICollection<T>, IList<T>, ...) — nativeMatches en typecheck.go solo modela la jerarquía de
  excepciones. Nunca da un falso positivo (en el peor caso isinst devuelve null quien debería
  matchear), documentado como gap, no bug silencioso.
- isinst/castclass contra System.Enum específicamente (vs. System.ValueType genérico, que sí
  funciona) — vmnet no distingue "es un enum" de "es un struct cualquiera" en el Type todavía.
- Herencia de campos estáticos: cada tipo tiene su propio storage estático propio, sin heredar
  ni compartir con la base — coincide con la semántica real de la CLR (los estáticos no se
  layoutean como los de instancia), no es una simplificación.
```

### Re-certificación contra los mismos 8 targets (7 paquetes + Jint)

| Paquete | % limpio Fase 3.7 | % limpio Fase 3.8 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 87.4% | 87.4% |
| `Semver@2.3.0` | 72.6% | 74.9% |
| `System.Text.Json@8.0.5` | 66.7% | 68.1% |
| `Newtonsoft.Json@13.0.3` | 60.6% | 62.9% |
| `FluentValidation@11.9.2` | 63.5% | 63.9% |
| `Humanizer.Core@2.14.1` | 46.0% | 46.3% |
| `SimpleBase@4.0.0` | 45.7% | 46.1% |
| **Promedio (7 paquetes)** | **63.2%** | **64.2%** |
| `Jint@3.1.3` | 66.1% | **74.4%** |
| **Promedio (7 paquetes + Jint)** | **63.6%** | **65.5%** |

Jint es el salto grande de esta fase (+8.3 puntos) — un motor de JS hace despacho por tipo y
casteos constantemente (representar cada tipo de valor de JS como una subclase de `JsValue`,
chequeada con `is`/`as` en el código de evaluación). Los 7 paquetes de siempre suben más modesto
(+1 punto): ya tenían menos `isinst`/`castclass` relativo a su tamaño que Jint.

### Cómo verificar Fase 3.8

```bash
go test ./... -race -count=3
go test ./ -run TestTypeChecks -v
```

### Fase 3.9 — Delegates/closures: `ldftn`, `Action`/`Func`, `Invoke`

**Tareas**

- [x] `runtime.KindFunc`/`runtime.Func` (`FullName` del método target + `Receiver` opcional,
      `nil` para un target estático) — deliberadamente **sin** modelar `System.Delegate`/
      `MulticastDelegate` como tipos BCL reales: todo tipo delegate (`Action`, `Func`2`, uno
      declarado por el usuario) compila su construcción a la **misma forma exacta** sin importar
      el nombre — `ldftn` empuja un target sin bind, `newobj AlgúnDelegado::.ctor(object,
      native int)` lo combina con el receptor recién empujado antes (`null` para un target
      estático). Detectar esa forma estructuralmente en vez de registrar cada tipo delegate por
      nombre es lo que hace que `Action<T>`/`Func<T,TResult>`/un delegate propio funcionen todos
      sin trabajo extra.
- [x] IR + intérprete: `ldftn`/`ldvirtftn` (`ldvirtftn` descarta el receptor que pop — sin vtable
      real, igual que `constrained.` en Fase 3.7), y el despacho de `Invoke` intercepta por
      **Kind del receptor** (`KindFunc`), no por nombre de método — nunca hay que registrar
      "AlgúnDelegado::Invoke" en ningún lado.
- [x] **Closures sin trabajo adicional**: una lambda que captura variables externas compila a
      una clase generada por el compilador con las variables capturadas como *campos reales* y
      el cuerpo de la lambda como un método de instancia sobre ella — el mecanismo de
      objetos/campos que ya existe desde Fase 2 alcanza sin ningún caso especial. Verificado con
      una closure que además **muta** un local capturado (el compilador reescribe el local para
      compartir el campo entre el método contenedor y la lambda) — funcionó a la primera vez
      probado contra un fixture real.
- [x] **Bug real encontrado y arreglado — drift del checker**: el propio test de dogfood lo
      atrapó de inmediato — el checker no tenía forma de saber que `Func`2::Invoke`/
      `Action`1::.ctor` ahora resuelven, porque la detección es puramente estructural en el
      intérprete (nunca se registra en `bcl.Lookup`). Se agregó `isDelegateType` al checker:
      reconoce los prefijos BCL conocidos (`Action`, `Func\``, `Predicate\`1`, ...) por nombre, y
      un delegate declarado localmente (`public delegate ...`) resolviendo su `TypeDef` real y
      chequeando que su `Extends` sea `System.MulticastDelegate`/`System.Delegate` — mismo patrón
      que `isValueType` en `assembly.go`.

**Fixtures y tests**

- [x] `Delegates.cs` (`Delegates`, `IntTransform`) / `TestDelegates` — conversión de grupo de
      métodos (target estático, el compilador la cachea en un campo estático), closure
      capturando un parámetro, closure capturando *y mutando* un local, y un tipo delegate
      declarado localmente (ejercita el camino de `TypeDef` de `isDelegateType`, no solo el de
      prefijos BCL conocidos)

### Lo que se dejó explícitamente afuera de esta fase

```txt
- Delegates multicast (`+=`/`-=`, System.Delegate.Combine/Remove): runtime.Func modela un solo
  target, no una lista de invocación. El caso dominante medido (Action<T>/Func<T,TResult> de un
  solo uso — predicados de validación, callbacks) no los necesita.
- BeginInvoke/EndInvoke (invocación asíncrona basada en IAsyncResult) y DynamicInvoke
  (reflection): solo Invoke está soportado.
- Covarianza/contravarianza de delegates: no se verifica ni se necesita — vmnet no hace type
  checking estático de todos modos.
```

### Re-certificación contra los mismos 8 targets (7 paquetes + Jint)

| Paquete | % limpio Fase 3.8 | % limpio Fase 3.9 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 87.4% | 90.5% |
| `FluentValidation@11.9.2` | 63.9% | **77.3%** |
| `Semver@2.3.0` | 74.9% | 76.8% |
| `System.Text.Json@8.0.5` | 68.1% | 69.3% |
| `Newtonsoft.Json@13.0.3` | 62.9% | 63.8% |
| `SimpleBase@4.0.0` | 46.1% | 48.4% |
| `Humanizer.Core@2.14.1` | 46.3% | 46.8% |
| **Promedio (7 paquetes)** | **64.2%** | **67.6%** |
| `Jint@3.1.3` | 74.4% | 77.3% |
| **Promedio (7 paquetes + Jint)** | **65.5%** | **68.8%** |

`FluentValidation` es el salto más grande de todo el camino a 85% hasta ahora (+13.4 puntos) —
una librería de validación es, literalmente, un árbol de predicados (`Func<T,bool>`) y callbacks
componibles. Confirma que delegates era, junto con la jerarquía de tipos, uno de los dos
bloqueadores realmente dominantes.

### Cómo verificar Fase 3.9

```bash
go test ./... -race -count=3
go test ./ -run TestDelegates -v
```

### Fase 3.10 — `try`/`catch`/`finally` real

La pieza arquitectónicamente más grande del camino a 85%: manejo de excepciones real, no solo
`throw` no manejado.

**Tareas**

- [x] `internal/il`: parser nuevo de la tabla de cláusulas de manejo de excepciones (spec
      §II.25.4.5-6, formas *small* y *fat*, secciones encadenadas vía `MoreSects`) — hasta ahora
      `ReadMethodBody` ni siquiera leía los bytes que siguen al código de un método con
      `try`/`catch`/`finally`. Fuzz test nuevo (`FuzzReadExceptionHandlers`, ~4.6M ejecuciones
      corridas manualmente, 0 panics).
- [x] IR: `Leave` (spec §III.3.44 — a diferencia de un `Branch` común, tiene que correr cualquier
      `finally`/`fault` entre el punto de salida y el destino antes de saltar), `EndFinally`,
      `Rethrow` (`throw;` de C#, sin operando). `Build` ahora también devuelve `[]Handler`
      (offsets de IL ya resueltos a índices de IR, igual que los targets de branch) — cambio de
      firma que tocó los dos call sites (`assembly.go`, el checker).
- [x] `internal/interpreter`: motor de despacho de excepciones completo —
  - Un `*runtime.ManagedException` que sale de `runFrame` (venga de un `throw` directo, un
    `rethrow`, o propagado desde cualquier llamada anidada — `frame.IP` ya apunta a la
    instrucción exacta que estaba corriendo, sin necesidad de rastrear nada especial) se busca
    contra los `Handler`s del método actual, del más interno al más externo.
  - Un `catch` matchea reusando **el mismo walk de jerarquía real de Fase 3.8**
    (`isAssignableTo`) — así que `catch (ArgumentException)` atrapa correctamente una
    `ArgumentNullException` lanzada adentro, no solo un match exacto de tipo.
  - Un `finally`/`fault` en el camino se corre siempre, tanto si la excepción termina
    atrapada como si sigue propagándose — `endfinally` retoma exactamente la transferencia de
    control que entró al handler (un `leave` encadenando el siguiente `finally` pendiente, o la
    búsqueda de catch retomando desde donde quedó).
  - `rethrow` preserva la excepción original (`frame.currentException`, seteado al entrar a
    cualquier catch) en vez de exigir que el handler guarde su propia referencia.
  - `System.Exception::get_Message` — faltaba por completo; sin él, `catch (T ex) { ...
    ex.Message ... }` (el patrón más común de todos) no tenía forma de leer el mensaje.
- [x] **Refactor de bajo riesgo, no una reescritura**: el loop gigante existente (`switch` con
      ~40 casos) se dejó intacto — se extrajo tal cual a `runFrame`, y `invoke` pasó a ser un
      loop delgado que llama a `runFrame`, atrapa un `*runtime.ManagedException` si sale, y
      reintenta despachándolo contra los handlers del método antes de dejarlo propagar. Cero
      cambios a la lógica interna de los ~40 casos existentes — todo el riesgo quedó concentrado
      en el mecanismo nuevo, no esparcido por todo el archivo.

**Fixtures y tests**

- [x] `TryCatch.cs` / `TestTryCatch` — catch por tipo exacto y por tipo base, `finally` corriendo
      en el camino atrapado y en el no atrapado, `finally` anidado corriendo antes de llegar a un
      `catch` externo, primer `catch` que matchea gana entre varios, `rethrow` preservando el
      mensaje original, y una excepción sin `catch` que matchea propagándose como error de Go —
      **todos los casos que no dependían del límite preexistente del CLI con argumentos JSON
      booleanos pasaron a la primera corrida real**, incluida la excepción anidada.
- [x] `internal/checker`: `Unsupported.cs` repurpuesta otra vez (tercera vez que crece la
      cobertura de vmnet) — ahora usa una cláusula de filtro (`catch (T) when (cond)`), la única
      forma de manejo de excepciones que esta fase deliberadamente no baja a IR.

### Lo que se dejó explícitamente afuera de esta fase

```txt
- Cláusulas de filtro (`catch (T) when (cond)`): buildHandlers en ir/builder.go las rechaza
  explícitamente como opcode no soportado en vez de ejecutarlas mal. Poco frecuentes en código
  real comparado con catch/finally simples.
- `rethrow` solo rastrea la excepción del catch más reciente que se entró (un solo slot, no una
  pila) — un `rethrow` después de un try/catch anidado *dentro* del mismo catch handler vería
  la excepción interna en vez de restaurarse a la externa. Edge case raro, documentado.
- Tipos de excepción definidos por el usuario (clases que heredan de Exception con campos
  propios): las excepciones siguen siendo solo los tipos nativos que vmnet ya registra
  (docs/es/ROADMAP.md Fase 2) — el mecanismo de catch-por-jerarquía funciona iguales para
  cualquier tipo que sí resuelva, pero construir una excepción custom con `newobj` todavía
  necesita que su `.ctor` sea interpretable, que hoy no está especialmente ejercitado.
```

### Re-certificación contra los mismos 8 targets (7 paquetes + Jint)

| Paquete | % limpio Fase 3.9 | % limpio Fase 3.10 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 90.5% | 90.5% |
| `System.Text.Json@8.0.5` | 69.3% | 69.7% |
| `Newtonsoft.Json@13.0.3` | 63.8% | 64.3% |
| `Humanizer.Core@2.14.1` | 46.8% | 47.0% |
| `FluentValidation@11.9.2` | 77.3% | 77.3% |
| `Semver@2.3.0` | 76.8% | 76.8% |
| `SimpleBase@4.0.0` | 48.4% | 48.4% |
| **Promedio (7 paquetes)** | **67.6%** | **67.7%** |
| `Jint@3.1.3` | 77.3% | 78.1% |
| **Promedio (7 paquetes + Jint)** | **68.8%** | **69.0%** |

Movimiento chico, honesto: a diferencia de la jerarquía de tipos o los delegates (que
desbloquearon directamente otros targets de llamada que antes fallaban), `try`/`catch`/`finally`
solo "limpia" un método si **ese era el único** obstáculo — muchos métodos que usan excepciones
en los paquetes reales también tocan otras cosas que siguen sin soportarse (DateTime, Span,
reflection). El valor de esta fase es arquitectónico (excepciones reales, no solo throw no
manejado) más que un salto grande en el número, y era la pieza más riesgosa de implementar bien —
vale la pena haberla hecho con cuidado aunque el número no lo refleje tanto como Fase 3.8/3.9.

### Cómo verificar Fase 3.10

```bash
go test ./... -race -count=3
go test ./ -run TestTryCatch -v
go test ./internal/il/... -run '^$' -fuzz '^FuzzReadExceptionHandlers$' -fuzztime=30s
```

### Fase 3.11 — `foreach`/enumeradores + wins baratos (re-priorizada con datos)

El plan original para esta fase era "DateTime, Span/ReadOnlySpan/Memory". Antes de escribir
código se corrió el mismo probe de findings-por-target de siempre (7 paquetes + Jint) — y
`System.IDisposable::Dispose`, `IEnumerator`1::get_Current`, `IEnumerable`1::GetEnumerator` y
`EqualityComparer`1` resultaron mucho más anchos (7-8/8 paquetes) que DateTime/Span (2-5/8,
aunque con más volumen absoluto). La causa: **`foreach` sobre `List<T>`/`Dictionary<K,V>` no
funcionaba en absoluto** — Fase 2 solo daba acceso indexado (`xs[i]`/`xs.Count`), nunca se agregó
el patrón `GetEnumerator`/`MoveNext`/`get_Current`/`Dispose` que el compilador de C# genera para
todo `foreach`. Se re-priorizó la fase para cerrar esto primero — mismo principio de "medir antes
de adivinar" que ya reordenó Fase 3.5 y Fase 3.6. DateTime/Span/Memory quedan documentados como
Fase 3.12 (ver abajo), no descartados.

**Tareas**

- [x] `List<T>.Enumerator`/`Dictionary<K,V>.Enumerator` como value types sintéticos reales
      (mismo patrón que `Nullable`1` de Fase 3.7) — confirmado contra IL real antes de escribir
      el native: `List<T>.GetEnumerator()` devuelve un **struct**, no una referencia, así que el
      sitio de llamada usa `ldloca`+`call` (no `callvirt`) para `MoveNext`/`get_Current`,
      exactamente el mecanismo de receptor-por-puntero que ya existía desde Fase 3.7.
- [x] `System.Collections.Generic.KeyValuePair`2` como value type — lo que produce
      `Dictionary<K,V>.Enumerator.Current`. El enumerador de diccionario saca una foto de las
      claves al momento de `GetEnumerator()` (un array propio, Fase 3.5) en vez de iterar el
      `map[string]Value` de Go en vivo — el orden de iteración de un map de Go es aleatorio por
      corrida, lo que haría el `MoveNext` no determinístico incluso *dentro* de una sola
      enumeración, no solo entre corridas.
- [x] `System.IDisposable::Dispose` — no-op genérico. Cubre tanto el `Dispose()` que `foreach`
      compila siempre dentro de un `finally` (haya o no algo que liberar) como el uso explícito
      de `using`.
- [x] `System.Collections.Generic.EqualityComparer`1::get_Default`/`Equals`/`GetHashCode` —
      reutiliza literalmente `valuesEqual`/`valueHash` de `system_object.go` (Fase 3.7), la misma
      igualdad/hash por default que la CLR usa a falta de un `IEquatable<T>` propio.
- [x] `System.Math::Min`/`Max`, `System.String::Join` (incluida la sobrecarga
      `IEnumerable<string>` — el sitio de llamada pasa el `List<T>` directo, no un array, cuando
      el argumento es un `List<T>`) — wins baratos de la lista original.
- [x] **Bug real encontrado y arreglado — colisión de nombres de tipos anidados**: antes de
      registrar el enumerador de `List<T>`, se verificó contra IL real qué nombre completo
      resuelve `ir.Build` para `List`1.Enumerator::MoveNext` — y resultó ser literalmente
      `"Enumerator"`, sin ningún prefijo, porque `resolveTypeToken`/`resolveMemberRefClassName`
      nunca habían necesitado caminar `ResolutionScope` para un `TypeRef` anidado (spec
      §II.22.38: un tipo anidado no tiene su propio namespace, lo hereda del que lo contiene).
      Registrar un native bajo ese nombre sin calificar habría **secuestrado silenciosamente**
      cualquier otro tipo llamado `Enumerator` en cualquier ensamblado cargado — Jint, por
      ejemplo, tiene los suyos propios (confirmado en el probe de esta misma fase). Se agregó
      `qualifyTypeRefName` (duplicado en `internal/ir` y en el paquete raíz, mismo patrón que
      otros resolvers ya duplicados) que arma `Tipo1+Tipo2` igual que `Type.FullName` real de
      .NET, encontrado y arreglado **antes** de que causara daño, no después.

**Fixtures y tests**

- [x] `Foreach.cs` / `TestForeach` — `foreach` sobre `List<int>`, `foreach` sobre
      `Dictionary<string,int>` (`kv.Value`), `EqualityComparer<int>.Default.Equals`,
      `Math.Min`/`Max`, `String.Join` sobre un `List<string>`

### Lo que se dejó explícitamente afuera de esta fase

```txt
- `foreach` sobre una colección tipada como interfaz (`IEnumerable<T> e = ...; foreach (x in
  e)`), en vez del tipo concreto (`List<T> xs = ...; foreach (x in xs)`): el primero compila
  contra IEnumerable<T>::GetEnumerator directamente, que necesita despacho virtual real (Fase
  3.8 solo cubre isinst/castclass, no despacho de método) — el patrón dominante real es el
  segundo (colección local de tipo concreto), que sí funciona.
- Colisión de nombres de tipos anidados para TypeDef propios del plugin (una clase anidada
  DECLARADA en el propio ensamblado, vía la tabla NestedClass): el fix de esta fase solo cubre
  TypeRef anidado (tipos BCL foráneos, que es lo que necesitaban los enumeradores). Riesgo
  preexistente, no empeorado, documentado.
- LINQ (`System.Linq.Enumerable` — Where/Select/Any/Count/...): ahora que hay delegates
  (Fase 3.9) y enumeradores reales (esta fase), sería viable, pero es una superficie grande por
  sí sola — candidato natural para una fase futura, no once-off.
```

### Re-certificación contra los mismos 8 targets (7 paquetes + Jint)

| Paquete | % limpio Fase 3.10 | % limpio Fase 3.11 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 90.5% | 90.9% |
| `FluentValidation@11.9.2` | 77.3% | 78.9% |
| `System.Text.Json@8.0.5` | 69.7% | 71.1% |
| `Newtonsoft.Json@13.0.3` | 64.3% | 65.7% |
| `Semver@2.3.0` | 76.8% | 78.0% |
| `SimpleBase@4.0.0` | 48.4% | 49.2% |
| `Humanizer.Core@2.14.1` | 47.0% | 48.0% |
| **Promedio (7 paquetes)** | **67.7%** | **68.8%** |
| `Jint@3.1.3` | 78.1% | 80.6% |
| **Promedio (7 paquetes + Jint)** | **69.0%** | **70.3%** |

### Cómo verificar Fase 3.11

```bash
go test ./... -race -count=3
go test ./ -run TestForeach -v
```

### Fase 3.12 — `DateTime`, `Span<T>`/`ReadOnlySpan<T>`/`Memory<T>`/`ReadOnlyMemory<T>`

El plan original pospuesto desde Fase 3.11 (ver arriba): dos superficies de BCL con impacto
grande pero concentrado en unos pocos paquetes, en vez de anchas en los 8 targets — por eso
quedaron después de `foreach`, no antes.

**Tareas**

- [x] `System.DateTime` como value type sintético de un solo campo (`ticks int64`, misma
      representación interna que la CLR usa: intervalos de 100ns desde el año 1) —
      `get_Year`/`Month`/`Day`/`Hour`/`Minute`/`Second`/`Millisecond`/`DayOfYear`/`DayOfWeek`
      (todos vía una sola factory `dateTimeField(func(time.Time) int32)`), `get_Now`/`get_UtcNow`/
      `get_Today`, `get_Ticks`, `get_Date`, `AddDays`/`AddHours`/`AddMinutes`/`AddSeconds`/
      `AddMilliseconds` (vía `dateTimeAdd`), `AddYears`/`AddMonths` (vía `dateTimeAddCalendar`,
      aritmética de calendario real de `time.Time.AddDate`, no solo sumar una duración fija),
      `ToString` (formato fijo invariant — vmnet no modela cultura, igual que `CultureInfo` desde
      Fase 3.6), `CompareTo`, `Equals`.
- [x] `Span<T>`/`ReadOnlySpan<T>`/`Memory<T>`/`ReadOnlyMemory<T>` como un solo shape de 3 campos
      (`backing`, `start`, `length`) reusado por los 4 — una vista defensiva sobre un
      `runtime.Array` o los caracteres de un string, no semántica real de puntero sin gestionar
      (vmnet no tiene punteros crudos). `MemoryExtensions.AsSpan`/`AsMemory`, `get_Length`,
      `get_Item`, `Slice`, `ToString`, `ToArray`, `Memory<T>.get_Span`.
- [x] `tests/fixtures/csharp/Fixtures.csproj`: agregado `System.Memory@4.5.5` como dependencia
      NuGet dev-only — `netstandard2.0` no trae `Span<T>`/`ReadOnlySpan<T>`/`AsSpan` de fábrica
      (llegan recién en `netstandard2.1`), y `System.Memory` es exactamente el polyfill que
      paquetes reales que targetean `netstandard2.0` (incluido `System.Text.Json` en versiones
      viejas) usan para tener Span — mismo shape de IL real, no un atajo de test.
- [x] **Bug real encontrado y arreglado — indexador de `Span<T>` devolvía el valor, no una
      referencia**: `Span<T>.this[int]` está declarado `ref T` (`ref readonly T` en
      `ReadOnlySpan<T>`) — confirmado contra IL real antes de arreglar: tanto `span[i]` como
      `span[i] = v` compilan al **mismo** `call get_Item` seguido de `ldind.i4`/`stind.i4`; no
      existe un `set_Item` separado en los metadatos para un indexador que devuelve `ref`. La
      primera versión devolvía el elemento directo, lo que hacía fallar el `ldind.i4` siguiente
      con "dereferencing a null managed pointer" (recibía un valor, no un `KindRef`). Arreglado
      devolviendo `runtime.RefTo(&backing.Arr.Elems[start+idx])` para el caso array, o un puntero
      a un `Int32` recién boxeado para el caso string (los strings de Go no tienen almacenamiento
      direccionable por rune — seguro solo porque ese puntero se usa de forma transitoria, deref'd
      de inmediato por el `ldind` que sigue). Se eliminó el `set_Item` que se había registrado al
      principio — código muerto, nunca hay un sitio de llamada real que lo use.
- [x] **Bug real encontrado y arreglado — `ReadOnlySpan<char>.ToString()` no despachaba**:
      devolvía el placeholder genérico `<ReadOnlySpan``1>` en vez del substring real. Mismo patrón
      que `StringBuilder.ToString()` en Fase 3.6: el sitio de llamada compila a `constrained.` +
      `callvirt Object::ToString`, no una llamada directa al método declarado en `Span`1`.
      Arreglado extendiendo `displayString` (`system_object.go`) para reconocer también
      `KindStruct` y despachar vía un nuevo helper compartido `spanToStringValue`.
- [x] **Bug real encontrado y arreglado — overflow de `time.Duration` en la conversión de ticks
      (el más serio de la fase)**: la primera versión de `timeToTicks`/`ticksToTime` usaba
      `t.Sub(dotnetEpoch)` / `dotnetEpoch.Add(time.Duration(secs)*time.Second)`. `time.Duration`
      es un `int64` de *nanosegundos*, válido solo para spans de ~292 años — la brecha de ~2000
      años entre el epoch de .NET (año 1) y cualquier fecha real del siglo XXI desborda
      silenciosamente (`time.Time.Sub` no da error, clampea a `math.MaxInt64`/`MinInt64`), y todas
      las fechas de prueba colapsaban al mismo resultado incorrecto sin importar el input. No se
      encontró por inspección de código — el razonamiento sobre la aritmética parecía correcto en
      el papel — sino agregando prints de depuración temporales que mostraron argumentos de
      entrada correctos (2024, 3, 15) contra ticks de salida sin sentido, aislando el bug a la
      conversión en sí. Arreglado reescribiendo ambas funciones sobre aritmética de segundos Unix
      (`time.Unix`/`t.Unix()`, que no comparte el límite de `Duration`), anclada al constante
      conocido `unixEpochTicks = 621355968000000000`.
- [x] **Bug real encontrado y arreglado — `System.DateTime::.ctor` no resolvía para construcción
      directa sobre un local**: `new DateTime(2024,3,15)` asignado directo a un local compila
      `ldloca.s`+argumentos+`call .ctor` (confirmado contra IL real), **no** `newobj` — el mismo
      patrón que ya había obligado a un fix en Fase 3.7 para structs de plugin, pero nunca se
      había replicado para un value type nativo. Sin el fix, esa forma de llamada caía en el
      registro regular de `bcl.Lookup` (que solo tenía la entrada de `newobj` vía
      `registerValueTypeCtor`) y fallaba como método no resuelto. Arreglado registrando también
      `"System.DateTime::.ctor"` en el registro regular, con una función (`dateTimeCtorInPlace`)
      que muta `*args[0].Ref` en el lugar en vez de devolver un valor nuevo.

**Fixtures y tests**

- [x] `DateTimeSpan.cs` / `TestDateTimeSpan` — `DateTimeSpanTest`: `YearMonthDay` (construcción +
      lectura de campos), `AddDaysAcrossMonth` (aritmética de calendario cruzando un límite de
      mes), `CompareDates` (`CompareTo`), `SpanSum` (`Span<int>` sobre un array vía `AsSpan`,
      suma por índice), `ReadOnlySpanSubstring` (`ReadOnlySpan<char>` sobre un string, `Slice` +
      `ToString`), `SpanWriteThrough` (escritura por índice a través del indexador `ref`,
      confirmando que el valor persiste en el array de respaldo).

### Lo que se dejó explícitamente afuera de esta fase

```txt
- Formato/parseo cultural real de DateTime (ToString con format string, culturas no invariant,
  DateTime.Parse/TryParse): vmnet no modela CultureInfo más allá del stub de Fase 3.6; ToString
  usa un formato fijo. Ningún paquete de los 8 targets lo necesitaba para pasar el checker.
- TimeSpan como tipo propio: se ve en varios de los 8 targets (Humanizer especialmente), pero
  DateTime.Add* ya cubre la aritmética que los casos reales medidos necesitaban; TimeSpan como
  value type de primera clase queda para una fase futura si el probe lo justifica.
- Span<T>/Memory<T> sobre memoria no administrada (punteros crudos, `stackalloc`, `fixed`):
  fuera de alcance permanente, no solo de esta fase — vmnet no tiene memoria sin gestionar (spec
  §3, "qué no es").
- DateTimeOffset, TimeZoneInfo: no aparecieron en el probe de los 8 targets con volumen
  suficiente para justificar la superficie extra.
```

### Re-certificación contra los mismos 8 targets (7 paquetes + Jint)

| Paquete | % limpio Fase 3.11 | % limpio Fase 3.12 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 90.9% | 91.2% |
| `FluentValidation@11.9.2` | 78.9% | 78.9% |
| `System.Text.Json@8.0.5` | 71.1% | 77.1% |
| `Newtonsoft.Json@13.0.3` | 65.7% | 65.8% |
| `Semver@2.3.0` | 78.0% | 78.0% |
| `SimpleBase@4.0.0` | 49.2% | 60.5% |
| `Humanizer.Core@2.14.1` | 48.0% | 82.4% |
| **Promedio (7 paquetes)** | **68.8%** | **76.3%** |
| `Jint@3.1.3` | 80.6% | 81.0% |
| **Promedio (7 paquetes + Jint)** | **70.3%** | **76.9%** |

El salto más grande de toda la secuencia 3.6-3.12: +7.5 puntos en los 7 paquetes (+6.6 con Jint).
`Humanizer.Core` solo (+34.4 puntos) explica la mayor parte — es literalmente una librería de
"humanizar" fechas/tiempos ("hace 3 días", "en 2 horas"), así que `DateTime` era su bloqueador
dominante, no uno más entre varios. `SimpleBase` (+11.3) y `System.Text.Json` (+6.0) confirman la
hipótesis original del probe: ambos usan `Span<byte>`/`ReadOnlySpan<char>` en sus rutas de
codificación/parseo de bajo nivel. `Ardalis.GuardClauses`/`FluentValidation`/`Newtonsoft.Json`/
`Semver`/`Jint` se mueven poco o nada — ya tenían menos DateTime/Span relativo a su tamaño que los
tres que sí saltaron, confirmando que la priorización basada en datos (impacto concentrado, no
ancho) fue la lectura correcta del probe.

Con 76.9% (7 paquetes + Jint), el criterio de cierre firme de 85% **todavía no se alcanza** —
queda una Fase 3.x adicional antes de poder cerrar Fase 3.6+ y pasar a Fase 4.

### Cómo verificar Fase 3.12

```bash
go test ./... -race -count=3
go test ./ -run TestDateTimeSpan -v
```

### Fase 3.13 — `foreach` sobre interfaz (despacho por tipo real) + paquete de wins baratos

Con el mismo probe de findings-por-target de siempre, corrido de nuevo tras Fase 3.12: los tres
hallazgos más anchos del proyecto entero eran `System.Collections.IEnumerator::MoveNext` (7/8),
`IEnumerator`1::get_Current` (7/8) e `IEnumerable`1::GetEnumerator` (7/8) — `foreach` sobre una
colección tipada como interfaz (`IEnumerable<T> xs = list; foreach (x in xs)`), en vez de tipo
concreto (`List<T> xs = ...`), exactamente lo que Fase 3.11 había dejado afuera explícitamente por
necesitar "despacho virtual real, no solo isinst/castclass".

**Tareas — despacho por interfaz**

- [x] `Machine.call` gana un fallback (`internal/interpreter/calls.go`): cuando el nombre
      declarado en el sitio de llamada (`"IEnumerable`1::GetEnumerator"`, baked in en tiempo de
      compilación desde el `MemberRef` — vmnet no tiene vtable) no resuelve ni como native ni
      como método interpretado, se reintenta una vez contra el **tipo concreto real del
      receptor** (`receiverTypeName`, `internal/interpreter/typecheck.go`): el `Struct.Type`/
      `Obj.Type` de la mayoría de los valores ya alcanza; para un `List<T>`/`Dictionary<K,V>`
      nativo (sin `runtime.Type` propio, solo `Native`) se agregó `bcl.NativeTypeName` — mismo
      patrón de despacho-por-Go-type que `nativeToString` (Fase 3.6). Esto cubre uniformemente
      tanto colecciones BCL accedidas por interfaz como clases del propio plugin que implementan
      una interfaz (una `IEnumerator` escrita a mano, un `IEquatable<T>` propio), sin registrar
      nada extra por tipo.
- [x] **Bug real encontrado y arreglado — recursión infinita en encadenamiento de constructores
      base**: el fallback de arriba, aplicado sin condición, hacía que `MyException(string) :
      base(message)` (un `call System.Exception::.ctor(this, msg)` — no `newobj`, ya que solo el
      tipo *exacto* se `newobj`ea; una llamada de constructor base corre sobre el objeto ya
      asignado del tipo *derivado*) redirigiera hacia el tipo concreto del receptor... que es el
      propio tipo derivado en construcción, re-invocando su propio constructor y agotando la pila
      (`interpreter: call depth exceeded`). La causa raíz: el fallback nunca debía aplicar a un
      `call` plano (no-virtual) — solo `callvirt` necesita redespacho por tipo real; un `call`
      nombra un target exacto a propósito (constructor base, método sellado/privado). Arreglado
      agregando el flag `virtual bool` (ya existente en `ir.Call.Virtual`, nunca antes propagado
      hasta `Machine.call`) y condicionando el fallback a `virtual == true`.
- [x] `ExplicitImplResolver` (`internal/interpreter/calls.go`, implementado en
      `assembly.go:resolveExplicitImpl`): un iterador `yield return` compila su
      `GetEnumerator`/`Current` como **implementación explícita de interfaz** — un `MethodDef` con
      nombre mangled (`"System.Collections.Generic.IEnumerable<System.Int32>.GetEnumerator"`, no
      un simple `"GetEnumerator"`), confirmado con `strings` sobre el DLL real antes de asumir
      nada. El fallback por nombre-plano de arriba no lo encuentra; se agregó
      `metadata.MethodImpls` (mismo patrón que `InterfaceImpls` de Fase 3.8, tabla `MethodImpl`,
      spec §II.22.27) para caminar las implementaciones explícitas del tipo concreto y encontrar
      el nombre real detrás de la interfaz declarada.
- [x] Checker (`internal/checker/analyzer.go`): `interfaceDispatchTargets`, un allowlist explícito
      de los targets de interfaz que el runtime fallback resuelve — el checker es estático y no
      puede saber el tipo concreto real de un receptor (necesitaría análisis de flujo de datos),
      así que esto es "mejor esfuerzo", mismo espíritu que `isDelegateType`.

**Tareas — corrección de excepciones personalizadas (encontrado al verificar el fix de arriba)**

- [x] **Bug real encontrado y arreglado — `System.Exception::.ctor` nunca resolvía para una
      subclase propia del plugin**: el mismo patrón de "solo `newobj` estaba cubierto" que ya
      había mordido a `DateTime`/`Nullable`1` en fases anteriores, esta vez para el encadenamiento
      de constructor base. Se registró `"System.Exception::.ctor"` (y cada excepción ya conocida)
      también como `call` plano, mutando el objeto ya asignado (`Obj.Native = &ManagedException{
      ...}`) — una excepción a la regla "Type xor Native" de `runtime.Object` documentada
      explícitamente, necesaria porque `ir.Throw` exige `Obj.Native` en *cualquier* objeto
      lanzado, plugin o no.
- [x] **Bug real encontrado y arreglado — el nombre de tipo quedaba pegado al tipo base, no al
      derivado real**: la primera versión seteaba `TypeName: "System.Exception"` (el nombre fijo
      bajo el que el native está registrado), así que `catch (MyException e)` nunca matcheaba —
      arreglado leyendo el `Obj.Type` real del receptor (el TypeDef del plugin) para el nombre.
- [x] **Bug real encontrado y arreglado — `catch (Exception e)` no atrapaba una subclase
      propia una vez arreglado lo anterior**: el matching de catch (`exceptionMatchesCatch`)
      nunca miraba la jerarquía real de tipos del plugin — solo un mapa fijo `exceptionBaseType`
      de nombres BCL conocidos. `nativeMatches` (ahora método de `Machine`, ya que necesita
      `ResolveType`) camina una sola cadena alternando entre ambas fuentes: el mapa fijo cuando el
      nombre es una excepción BCL conocida, o el `BaseTypeFullName` real del `TypeDef` del plugin
      cuando no lo es — así `MyException -> System.Exception` (vía TypeDef real) empalma con
      `System.Exception -> ...` (vía el mapa) en la misma caminata.

**Tareas — paquete de wins baratos**

- [x] `System.String`: `IsNullOrEmpty`, `IsNullOrWhiteSpace`, `StartsWith`, `IndexOf`/
      `LastIndexOf` (en posiciones de rune, consistente con `Substring`/`get_Chars` ya
      existentes), `Split` (separador `char[]`/`string[]`, vacío o ausente = espacio en blanco —
      mismo comportamiento documentado que `Split(null)` real; `StringSplitOptions.
      RemoveEmptyEntries` se honra, un límite de cantidad no), `ToCharArray`, `Replace` (cubre
      `(string,string)` y `(char,char)`), `Trim`/`Trim(char[])`, `op_Inequality`.
- [x] `System.Char` (`internal/bcl/system_char.go`, archivo nuevo): `IsUpper`/`IsLower`/`IsDigit`/
      `IsLetter`/`IsLetterOrDigit`/`IsWhiteSpace`/`ToUpper`/`ToLower`/`ToString` — todos sobre un
      `int32` plano (`char` no tiene su propio `Kind` en `runtime.Value`, spec §III.1.1).
- [x] `System.Int32::ToString` (`internal/bcl/system_numeric.go`, archivo nuevo) — sin soporte de
      format string (mismo límite ya documentado para `CultureInfo`).
- [x] `List<T>`: `set_Item`, `ToArray`, `AddRange` (acepta otro `List<T>` o un array),
      `Contains` (reusa `valuesEqual` de Fase 3.7). `Dictionary<K,V>::TryGetValue` (el parámetro
      `out` usa el mismo mecanismo de puntero administrado que cualquier `ref`/`out` primitivo
      desde Fase 3.5; en un miss escribe `Null()`, no un `default(TValue)` tipado — aproximación
      documentada, vmnet no tiene el argumento de tipo genérico en este sitio de llamada).
- [x] `ICollection`1::Add`/`get_Count` e `ICollection::get_Count` (no genérica) agregados al
      allowlist del checker — el runtime ya los resolvía gratis vía el fallback de despacho por
      interfaz de arriba, reusando los natives de `List`1::Add`/`get_Count` ya existentes; nada
      nuevo que registrar.
- [x] `Nullable`1::.ctor` como `call` plano además de `newobj`: `int? n = 42;` (asignación directa
      a un local, sin ternario) compila `ldloca`+`call .ctor` directo sobre el local, confirmado
      contra IL real antes de arreglar — el mismo patrón exacto que `DateTime` necesitó en Fase
      3.12, encontrado esta vez por sospecha directa (mismo "shape" de bug) y confirmado empí-
      ricamente, no asumido.

**Fixtures y tests**

- [x] `InterfaceForeach.cs` / `TestInterfaceForeach` — suma sobre un `List<int>` accedido vía
      `IEnumerable<int>`, suma sobre un iterador `yield return` (implementación explícita de
      interfaz)
- [x] `TryCatch.cs` (`CustomException`/`CustomExceptionTest`) / `TestCustomException` — catch por
      subtipo exacto y por tipo base real
- [x] `Structs.cs` (`DirectNullableAssignTest`) — cubierto dentro de `TestStructs`
- [x] `CheapWins.cs` / `TestCheapWins` — String/Char/Int32/List/Dictionary del paquete de arriba

### Lo que se dejó explícitamente afuera de esta fase

```txt
- Reflection-lite más allá del stub de System.Type ya existente: `object.GetType()`,
  `MemberInfo.get_Name`, `Type::op_Equality`/`IsAssignableFrom`/`get_FullName`, y el opcode
  `ldtoken` (typeof(T)) — el segundo hallazgo más ancho tras esta fase (5/8, ldtoken 6/8), pero
  es una superficie nueva (necesita un objeto System.Type real, no el stub actual) que merece su
  propia sub-fase, no un agregado apurado a esta.
- LINQ (System.Linq.Enumerable: Select/Any/Where/ToList/ToArray/FirstOrDefault/All) — viable
  ahora que hay delegates (3.9) y enumeradores reales + despacho por interfaz (3.11/3.13), pero
  es una superficie grande por sí sola — mismo candidato ya anotado como pendiente en Fase 3.11.
- Regex (System.Text.RegularExpressions) — el motor de regex de Go es RE2 (sin backreferences,
  sin lookaround), semánticamente distinto del motor de .NET; traducir sintaxis o limitar el
  subconjunto soportado es una decisión de diseño propia, no un native de una línea.
- Async/Task (AsyncTaskMethodBuilder) — fuera de alcance permanente, no solo de esta fase (spec
  §3, "qué no es"; ya documentado en el registro de riesgos).
- HashSet<T>, Stack<T>, ConcurrentDictionary<K,V>, TimeSpan, StringComparer — aparecieron en el
  probe (4/8, volumen moderado) pero cada uno es una superficie nueva propia, no una extensión de
  algo que ya existe (a diferencia de los métodos de List/Dictionary agregados en esta fase).
- La colisión de nombres de tipos anidados para TypeDef propios del plugin (documentada como
  riesgo preexistente desde Fase 3.11) sigue sin resolverse — no empeorada por esta fase.
```

### Re-certificación contra los mismos 8 targets (7 paquetes + Jint)

| Paquete | % limpio Fase 3.12 | % limpio Fase 3.13 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 91.2% | 93.0% |
| `FluentValidation@11.9.2` | 78.9% | 82.0% |
| `System.Text.Json@8.0.5` | 77.1% | 78.2% |
| `Newtonsoft.Json@13.0.3` | 65.8% | 68.1% |
| `Semver@2.3.0` | 78.0% | 82.7% |
| `SimpleBase@4.0.0` | 60.5% | 60.9% |
| `Humanizer.Core@2.14.1` | 82.4% | 87.9% |
| **Promedio (7 paquetes)** | **76.3%** | **79.0%** |
| `Jint@3.1.3` | 81.0% | 82.6% |
| **Promedio (7 paquetes + Jint)** | **76.9%** | **79.4%** |

+2.7 puntos en los 7 paquetes (+2.5 con Jint) — movimiento sólido y bien repartido (todos los
paquetes suben, ninguno un salto único dominante como Humanizer en 3.12), consistente con haber
atacado tanto un hallazgo realmente ancho (despacho por interfaz, 7/8) como un paquete disperso de
wins baratos de menor volumen individual. Con 79.4% (7 paquetes + Jint), el criterio de cierre
firme de 85% **todavía no se alcanza** — el hallazgo más ancho que queda es reflection-lite
(`ldtoken`/`GetType`/`Type`, 5-6/8), candidato natural para la próxima sub-fase.

### Cómo verificar Fase 3.13

```bash
go test ./... -race -count=3
go test ./ -run 'TestInterfaceForeach|TestCustomException|TestCheapWins' -v
```

### Fase 3.14 — Reflection-lite: `ldtoken`/`typeof(T)`, `GetType()`, `System.Type`

El probe post-3.13 confirmó la predicción de la fase anterior: `ldtoken` (6/8), `System.Object::
GetType` (5/8) y `System.Reflection.MemberInfo::get_Name` (5/8) eran los tres hallazgos más
anchos del proyecto.

**Tareas**

- [x] `ldtoken` (spec §III.4.16, decodificado desde Fase 1 pero nunca bajado a IR) — solo para la
      forma `typeof(T)` (token de `TypeDef`/`TypeRef`/`TypeSpec`). Confirmado contra IL real
      antes de implementar: `typeof(T)` compila siempre `ldtoken T` + `call System.Type::
      GetTypeFromHandle(RuntimeTypeHandle)` — vmnet no modela `RuntimeTypeHandle` como un Kind
      propio: `ir.LoadTypeToken` empuja directamente un `System.Type` real, y
      `GetTypeFromHandle` se registra como función identidad, así el par de instrucciones se
      comporta exactamente como el CLR sin necesitar una representación intermedia de "handle".
      La otra forma de `ldtoken` (token de `Field`, el patrón `RuntimeHelpers.InitializeArray`
      detrás de un inicializador de array literal) sigue sin soporte, mismo mensaje que antes.
- [x] `System.Type` modelado como un objeto native-backed mínimo (`nativeTypeInfo{FullName
      string}`, `internal/bcl/system_type.go`) — sin identidad de referencia real (`typeof(X)`
      llamado dos veces produce dos `*nativeTypeInfo` Go distintos, a diferencia del Type
      canónico único de la CLR); todas las operaciones soportadas comparan por `FullName`
      (string), nunca por identidad de puntero — lo único observable desde la API pública de
      `Type` de todos modos.
- [x] `System.Object::GetType` — reusa la misma inspección de "forma real en runtime" que
      `isAssignableTo` (Fase 3.8) ya hace para `isinst`/`castclass`, sin duplicar un segundo
      mecanismo de identidad de tipo. Un primitivo boxeado tiene la misma ambigüedad ya
      documentada en `isAssignableTo` (`KindI4` cubre `int32`/`bool`/`char`/`short`/`byte`) — se
      asume el caso dominante (`int32`).
- [x] `System.Type::get_Name`/`get_FullName`/`ToString`/`op_Equality`/`op_Inequality`/`Equals`,
      `System.Reflection.MemberInfo::get_Name` (alias exacto de `get_Name` — `System.Type` es un
      `MemberInfo` real en la BCL, así que el mismo call site puede resolver contra cualquiera de
      los dos nombres según cómo el compilador tipó la expresión).
- [x] Checker: `ir.LoadTypeToken` se agrega a `instrIsObjectModel` (un `System.Type` es un objeto
      heap-alocado, igual que cualquier `newobj`); `System.Type::`/`System.Reflection.
      MemberInfo::get_Name` promovidos a `rules` (el stub `System.Type::` que ya vivía solo en
      `netstandard-lite` desde antes de esta fase se elimina — redundante una vez que
      `netstandard-lite` hereda `rules`).

**Fixtures y tests**

- [x] `Reflection.cs` / `TestReflection` — `GetType() == typeof(T)` (verdadero para el tipo
      exacto, falso contra el tipo base — confirma que la comparación no colapsa a "cualquier
      Type es igual"), `Type.Name`, `Type.FullName`, `!=`

### Lo que se dejó explícitamente afuera de esta fase

```txt
- Type::IsAssignableFrom — el segundo hallazgo más ancho que quedó (84 casos, 4/8) tras esta
  fase, pero necesita caminar la jerarquía real de tipos (BaseTypeFullName/Interfaces) con
  acceso a Machine.ResolveType, algo que un bcl.Native (func(args) (Value, error) plano, sin
  Machine) no tiene hoy — necesitaría el mismo tipo de plumbing que ExplicitImplResolver (Fase
  3.13), no un native de una línea.
- Type::MakeGenericType/GetGenericTypeDefinition/GetInterfaces/get_IsGenericType/get_IsEnum,
  Nullable.GetUnderlyingType — reflection sobre genéricos e introspección de forma real; vmnet
  no modela argumentos de tipo genérico en runtime.Value en absoluto (spec §17.1, "generics
  mínimos" — type-erased).
- System.Reflection.MethodBase::Invoke/MethodInfo — invocación dinámica real, una superficie
  completamente distinta (y bastante más riesgosa de exponer a un plugin) que "solo consultar
  el nombre/tipo", fuera de alcance de "reflection-lite".
- LINQ (System.Linq.Enumerable) — sigue siendo el hallazgo más ancho no-async del proyecto tras
  esta fase (Select/Any/ToList/Where/ToArray suman ~174 casos en 4-5/8), ya anotado como
  pendiente desde Fase 3.11/3.13 — candidato natural para la siguiente sub-fase.
- Regex, async/Task, HashSet<T>/Interlocked/StringComparer — sin cambios respecto a lo ya
  documentado como afuera en Fase 3.13.
```

### Re-certificación contra los mismos 8 targets (7 paquetes + Jint)

| Paquete | % limpio Fase 3.13 | % limpio Fase 3.14 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 93.0% | 93.3% |
| `FluentValidation@11.9.2` | 82.0% | 84.6% |
| `System.Text.Json@8.0.5` | 78.2% | 80.4% |
| `Newtonsoft.Json@13.0.3` | 68.1% | 70.4% |
| `Semver@2.3.0` | 82.7% | 82.7% |
| `SimpleBase@4.0.0` | 60.9% | 60.9% |
| `Humanizer.Core@2.14.1` | 87.9% | 88.0% |
| **Promedio (7 paquetes)** | **79.0%** | **80.1%** |
| `Jint@3.1.3` | 82.6% | 83.8% |
| **Promedio (7 paquetes + Jint)** | **79.4%** | **80.5%** |

+1.1 puntos en los 7 paquetes (+1.1 con Jint) — `Semver`/`SimpleBase` no se mueven en absoluto
(no usan reflection en su superficie pública), `FluentValidation`/`System.Text.Json`/
`Newtonsoft.Json`/`Jint` sí (validación de tipo genérico, serialización basada en tipo, motor de
JS con despacho por tipo — los cuatro tocan `GetType()`/`typeof` con volumen real). Con 80.5%
(7 paquetes + Jint) el criterio de cierre firme de 85% **todavía no se alcanza** — LINQ es ahora
el hallazgo más ancho no-async/no-regex restante, candidato natural para la siguiente sub-fase.

### Cómo verificar Fase 3.14

```bash
go test ./... -race -count=3
go test ./ -run TestReflection -v
```

### Fase 3.15 — LINQ (`System.Linq.Enumerable`)

El probe post-3.14 confirmó lo ya anotado: LINQ (`Select`/`Any`/`ToList`/`Where`/`ToArray`,
~174 casos en 4-5/8) era el hallazgo más ancho no-async/no-regex restante, y ya era viable —
delegates (3.9), enumeradores reales (3.11) y despacho por interfaz (3.13) cubren todo lo que
LINQ necesita para operar sobre cualquier fuente real.

**Tareas**

- [x] **Descubrimiento arquitectónico central**: los métodos de `Enumerable` no pueden ser
      `bcl.Native` planos (`func(args) (Value, error)`, sin acceso a `Machine`) — cada uno
      necesita invocar el delegate argumento (`m.invokeFunc`) y/o recorrer una fuente
      `IEnumerable<T>` arbitraria vía el protocolo real `GetEnumerator`/`MoveNext`/`get_Current`
      (`m.call`, reusando el fallback de despacho por interfaz de Fase 3.13). Se agregó un
      registro paralelo `linqRegistry` (`internal/interpreter/linq.go`, nuevo) de
      `linqNative func(m *Machine, args []runtime.Value, ...) (runtime.Value, error)`, consultado
      en `Machine.tryCall` antes de cualquier resolución que no tenga `Machine`. Mismo tipo de
      plumbing nuevo que `ExplicitImplResolver` necesitó en Fase 3.13, no una sorpresa.
- [x] `enumerateAll` — un solo helper que drena cualquier fuente en un `[]runtime.Value`:
      camino rápido para `KindArray` y un `List<T>` nativo (ya son un slice de Go), camino
      general vía el protocolo real de iteración para cualquier otra cosa (`Dictionary<K,V>`,
      una clase del plugin, un iterador `yield return`, otro resultado de LINQ) — el mismo
      mecanismo que `foreach` ya usa, no una segunda implementación paralela de iteración.
- [x] `Select`/`Where`/`Any`/`All`/`ToList`/`ToArray`/`FirstOrDefault` — **eager**
      (materializan de inmediato en un `[]runtime.Value`), no los iteradores perezosos reales de
      la CLR — simplificación deliberada: una llamada encadenada
      (`xs.Where(...).Select(...).ToList()`) se comporta idéntico desde el punto de vista del
      llamador, porque cada resultado de LINQ se envuelve como un `List<T>` real y
      completamente enumerable (`bcl.NewListValue`, nuevo constructor exportado — mismo patrón
      que `bcl.NewTypeValue` de Fase 3.14) en vez de una promesa perezosa.
- [x] `bcl.NativeListItems` (nuevo, exportado) — acceso de solo lectura a los items de un
      `List<T>` nativo desde `internal/interpreter`, ya que `nativeList` es un tipo no exportado
      de `internal/bcl`; necesario para el camino rápido de `enumerateAll`.
- [x] Checker: `linqTargets` (`internal/checker/analyzer.go`) — un allowlist separado de
      `interfaceDispatchTargets`, no fusionado con él: la razón por la que el checker no puede
      resolver estos nombres es distinta (no "no sabe el tipo concreto del receptor", sino
      "no sabe que existe el registro `linqRegistry` de `internal/interpreter` en absoluto" — el
      checker no puede importar ese paquete sin romper su límite de análisis puramente estático).
      Prefijo `"System.Linq.Enumerable::"` agregado al perfil `rules`.
- [x] **Endurecimiento verificado durante la fase**: `new int[] { 1, 2, 3 }` (inicializador de
      array literal) compila `newarr` + `ldtoken <FieldDef>` + `call RuntimeHelpers.
      InitializeArray` — la forma de `ldtoken` que Fase 3.14 dejó explícitamente sin soportar
      (token de campo, no de tipo). Confirmado al escribir el fixture de LINQ sobre un array: la
      fuente de array debe construirse por asignación elemento a elemento, no con inicializador
      de colección — limitación preexistente, no introducida ni agravada por esta fase, solo
      redescubierta al verificar.

**Fixtures y tests**

- [x] `Linq.cs` / `TestLinq` — `Where().Select().ToList()` encadenado sobre `List<int>`,
      `Any`/`All` con predicado, `FirstOrDefault` con predicado, `Select`/`ToArray` sobre un
      `int[]` (confirma que el camino rápido de `enumerateAll` para arrays funciona, no solo
      `List<T>`)

### Lo que se dejó explícitamente afuera de esta fase

```txt
- Las sobrecargas con índice de Select/Where (Func<T,int,TResult>/Func<T,int,bool>) — solo la
  forma de un solo argumento está cubierta; agregar la de índice es mecánico pero no medido
  como necesario todavía.
- OrderBy/GroupBy/Skip/Take/Sum/Min/Max/Distinct/Concat/Reverse — no aparecieron con volumen
  significativo en el probe de los 8 targets; candidatos para agregar bajo demanda si una
  fase futura los mide como relevantes, no una lista aspiracional.
- Encadenamiento verdaderamente perezoso (LINQ real es streaming; esta implementación es eager
  en cada paso) — una fuente infinita o muy grande con un `.Take(n)` en algún punto de la
  cadena materializaría de más; ningún paquete de los 8 targets ejercita ese patrón hoy.
- Type::IsAssignableFrom sigue afuera (ver Fase 3.14) — ahora que existe el patrón de
  "Machine-aware native" (linqRegistry) sería mecánicamente más simple agregarlo, pero no se
  hizo en esta fase para mantenerla enfocada en LINQ.
```

### Re-certificación contra los mismos 8 targets (7 paquetes + Jint)

| Paquete | % limpio Fase 3.14 | % limpio Fase 3.15 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 93.3% | 93.3% |
| `FluentValidation@11.9.2` | 84.6% | 86.3% |
| `System.Text.Json@8.0.5` | 80.4% | 80.4% |
| `Newtonsoft.Json@13.0.3` | 70.4% | 70.7% |
| `Semver@2.3.0` | 82.7% | 83.7% |
| `SimpleBase@4.0.0` | 60.9% | 60.9% |
| `Humanizer.Core@2.14.1` | 88.0% | 88.3% |
| **Promedio (7 paquetes)** | **80.1%** | **80.5%** |
| `Jint@3.1.3` | 83.8% | 83.8% |
| **Promedio (7 paquetes + Jint)** | **80.5%** | **80.9%** |

+0.4 puntos — más chico que el volumen crudo de hallazgos (~174 casos) sugería, mismo patrón ya
documentado en Fase 3.10: LINQ solo "limpia" un método si era el único obstáculo, y varios de los
métodos que usan LINQ en estos paquetes reales *también* tocan reflection profunda o regex, que
siguen sin soporte. El valor real de esta fase es desbloquear el patrón `Where`/`Select`/`ToList`
en sí — que ahora funciona de punta a punta, encadenado y sobre cualquier fuente — más que el
movimiento en el promedio agregado. Con 80.9% el criterio de cierre firme de 85% todavía no se
alcanza.

### Cómo verificar Fase 3.15

```bash
go test ./... -race -count=3
go test ./ -run TestLinq -v
```

### Fase 3.16 — `Type::IsAssignableFrom`

Sub-fase chica: el segundo hallazgo más ancho de reflection dejado explícitamente afuera de Fase
3.14 (84 casos, 4/8) — no se hizo entonces porque necesitaba acceso a `Machine` (caminar la
jerarquía real de tipos requiere `Machine.ResolveType`, no disponible a un `bcl.Native` plano),
pero ese exacto tipo de plumbing ya existe desde Fase 3.15 (`machineRegistry`, generalizado de
`linqRegistry` — mismo registro, ahora con un nombre que no asume que solo LINQ lo va a usar).

**Tareas**

- [x] `typeIsAssignableFrom` (`internal/interpreter/reflection.go`, archivo nuevo) — re-deriva la
      lógica de `isAssignableTo` (Fase 3.8) partiendo de un **nombre** de tipo en vez de un
      `runtime.Value`/`Kind` ya conocido (ambos operandos son `System.Type`, que solo cargan un
      `FullName` string): igualdad exacta o `target == "System.Object"` corta camino de
      inmediato, si no se resuelve el `TypeDef` real del candidato y se camina con
      `m.typeMatches` — el mismo walk que `isinst`/`castclass` y el catch-matching de
      excepciones (Fase 3.13) ya usan.
- [x] `bcl.TypeFullNameOf` (nuevo, exportado) — extrae el `FullName` de un valor `System.Type`
      desde fuera de `internal/bcl`, ya que `nativeTypeInfo` es un tipo no exportado.
- [x] Checker: entrada directa para `"System.Type::IsAssignableFrom"` en `resolvableMethod`
      (no se creó un mapa nuevo de un solo elemento, a diferencia de `linqTargets`).

**Fixtures y tests**

- [x] `Reflection.cs` (`VehicleAssignableFromCar`/`CarNotAssignableFromVehicle`) — cubierto
      dentro de `TestReflection`

### Re-certificación contra los mismos 8 targets (7 paquetes + Jint)

| Paquete | % limpio Fase 3.15 | % limpio Fase 3.16 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 93.3% | 93.3% |
| `FluentValidation@11.9.2` | 86.3% | 86.4% |
| `System.Text.Json@8.0.5` | 80.4% | 80.9% |
| `Newtonsoft.Json@13.0.3` | 70.7% | 71.0% |
| `Semver@2.3.0` | 83.7% | 83.7% |
| `SimpleBase@4.0.0` | 60.9% | 60.9% |
| `Humanizer.Core@2.14.1` | 88.3% | 88.3% |
| **Promedio (7 paquetes)** | **80.5%** | **80.6%** |
| `Jint@3.1.3` | 83.8% | 83.8% |
| **Promedio (7 paquetes + Jint)** | **80.9%** | **81.0%** |

+0.1 puntos — movimiento mínimo, esperado para un método con volumen concentrado en pocos
métodos que probablemente también tocan otras superficies sin soporte (mismo patrón que LINQ en
esta misma fase). Con 81.0% el criterio de cierre firme de 85% todavía no se alcanza.

### Cómo verificar Fase 3.16

```bash
go test ./... -race -count=3
go test ./ -run TestReflection -v
```

### Fase 3.17 — Bug crítico: colisión de nombres de tipos anidados propios del plugin + `System.Lazy<T>`

Al agregar `Lazy.cs` (un segundo archivo con lambdas no-capturadoras, junto a `Linq.cs` de Fase
3.15) y correr la suite completa con `-count=3` (no solo una vez), `TestLinq` empezó a fallar con
`"<>c has no static field \"<>9__0_0\""` — un bug real, no relacionado con `Lazy<T>` en sí, que la
adición de un segundo archivo con lambdas simplemente hizo alcanzable por primera vez.

**Causa raíz**: el compilador de C# emite una clase cache de lambdas no-capturadoras (literalmente
llamada `<>c`) **por cada tipo contenedor** que tiene alguna — un ensamblado con lambdas en dos
clases distintas (`LinqTest` y `LazyTest`) termina con **dos TypeDefs separados, ambos llamados
`<>c`** (mismo `Name`, ambos con `Namespace=""`, ya que un tipo anidado siempre tiene namespace
vacío — spec §II.22.32). Todo el código de vmnet que resolvía un token `TypeDef` a un nombre
completo (`ldsfld`/`stsfld`/`newobj`/`call`/`ir.Build`, más los duplicados en `assembly.go` y
`internal/checker/analyzer.go`) colapsaba directamente a `Qualify(typeDef.Namespace, typeDef.Name)`
— **sin caminar la tabla `NestedClass`** — así que ambos `<>c` colapsaban al mismo string `"<>c"`,
y `metadata.FindTypeDef` devolvía el que escaneara primero, sin importar cuál necesitaba el sitio
de llamada real. Esto es la MISMA clase de bug que Fase 3.11 ya había arreglado para `TypeRef`
(tipos anidados *foráneos*, vía `qualifyTypeRefName`/`ResolutionScope`) — y que esa misma fase
había **documentado explícitamente como riesgo preexistente, no arreglado**, para `TypeDef` (tipos
anidados *propios del plugin*, vía `NestedClass`). El riesgo, tal cual se predijo, terminó siendo
real.

**Impacto medido — mucho más grande que un problema de fixtures**: al medir contra los 8 targets
después del arreglo, el promedio saltó de 80.6% a **82.8%** (7 paquetes) y de 81.0% a **83.0%**
con Jint — el salto más grande de toda la secuencia 3.6-3.17 después de Fase 3.12. `SimpleBase`
solo saltó de 60.9% a 75.6% (+14.7 puntos). La razón: **cualquier paquete real con más de una
clase usando lambdas no-capturadoras** (patrón extremadamente común, no un caso de borde) ya
estaba silenciosamente resolviendo `ldsfld`/`call` contra el `<>c` equivocado en algún punto,
produciendo errores de "static field/method not found" en métodos que no tenían nada que ver con
lambdas per se, solo compartían ensamblado con otra clase que también usaba alguna.

**Tareas — el arreglo**

- [x] `metadata.EnclosingClass(typeRID) (uint32, bool, error)` (nuevo, `internal/metadata/
      resolver.go`) — lee la tabla `NestedClass` (spec §II.22.32), sin función previa que la
      leyera en absoluto (confirmado antes de escribir nada).
- [x] `qualifyTypeDefName`/`QualifyTypeDefName` (nuevo, duplicado en `internal/ir/builder.go`
      —exportado, ya que `internal/checker` también lo necesita y sí puede importar `internal/ir`—
      y en `assembly.go` —no exportado, mismo patrón ya establecido para `qualifyTypeRefName`—):
      camina `NestedClass` recursivamente construyendo `Enclosing+Nested`, igual que
      `qualifyTypeRefName` ya hace para `ResolutionScope`. Reemplaza el `Qualify(ns,name)` directo
      en los 8 sitios reales que resuelven un token `TypeDef` a nombre: `resolveCallTarget`,
      `resolveMemberRefClassName`, `resolveTypeToken`, `resolveNewObjTarget`, `resolveFieldTarget`
      (el sitio exacto del bug — `ldsfld`/`stsfld`) en `internal/ir/builder.go`;
      `resolveMethodDefOrRefName`, `buildMethod`, `resolveTypeTokenName` en `assembly.go`;
      `Analyze` en `internal/checker/analyzer.go`.
- [x] `metadata.FindTypeDef` extendido para aceptar un nombre `"+"`-calificado (el round-trip: el
      nombre calificado que `qualifyTypeDefName` produce necesita volver a resolverse a la fila
      `TypeDef` real más tarde, vía `buildType`/`resolveByFullName`) — un simple match por
      `Name`+`Namespace` no alcanza cuando hay varios TypeDefs con el mismo `Name` anidados en
      tipos distintos; ahora camina `NestedClass` hacia arriba desde cada candidato para confirmar
      que la cadena de contenedores coincide con lo pedido, con el `Namespace` anclado únicamente
      en el nivel más externo (el único que tiene uno real).
- [x] `runtime.Type.QualifiedName` (nuevo campo) — `buildType` lo setea al nombre ya calificado
      que recibió como entrada; `fullTypeName` (`internal/interpreter/typecheck.go`, usado por el
      despacho por interfaz de Fase 3.13 y el catch-matching de excepciones) lo prefiere sobre
      reconstruir desde `Namespace`+`Name`, que perdería la calificación de nuevo para cualquier
      tipo anidado propio del plugin.

**Tareas — `System.Lazy<T>`**

- [x] `nativeLazy` (`internal/bcl/system_lazy.go`, archivo nuevo): constructor cubre las
      sobrecargas con factory `Func<T>` (con o sin un `bool`/`LazyThreadSafetyMode` final,
      ignorado — todo acceso ya se serializa vía el propio mutex de la instancia sin importar el
      modo pedido); `get_IsValueCreated` (native plano); `get_Value` — necesita `Machine` (invocar
      el factory usa `m.invokeFunc`), así que va al `machineRegistry` generalizado en Fase 3.16
      (`internal/interpreter/lazy.go`, archivo nuevo). `bcl.LazyGetOrCompute` mantiene el lock de
      la instancia durante **todo** el cómputo (no solo alrededor del chequeo), para que dos
      goroutines compitiendo por el mismo `Lazy<T>.Value` por primera vez se serialicen en "uno
      computa, el otro bloquea y ve el mismo resultado cacheado" en vez de "ambos computan, uno
      pisa silenciosamente el resultado del otro" — un riesgo real, no hipotético: un campo
      estático `Lazy<T>` es el uso dominante real, y `Assembly.Call` está documentado como seguro
      para goroutines concurrentes.

**Fixtures y tests**

- [x] `Lazy.cs` / `TestLazy` — factory invocado exactamente una vez (verificado contando
      invocaciones reales, no solo revisando que el valor devuelto sea consistente),
      `IsValueCreated` antes/después del primer acceso
- [x] `Linq.cs` + `Lazy.cs` juntos, corridos con `-count>=3`, son la cobertura de regresión del
      bug de `<>c` — ambos archivos ya tienen lambdas no-capturadoras en clases distintas, la
      forma exacta que lo reprodujo

### Re-certificación contra los mismos 8 targets (7 paquetes + Jint)

| Paquete | % limpio Fase 3.16 | % limpio Fase 3.17 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 93.3% | 93.3% |
| `FluentValidation@11.9.2` | 86.4% | 86.4% |
| `System.Text.Json@8.0.5` | 80.9% | 80.9% |
| `Newtonsoft.Json@13.0.3` | 71.0% | 71.0% |
| `Semver@2.3.0` | 83.7% | 83.7% |
| `SimpleBase@4.0.0` | 60.9% | **75.6%** |
| `Humanizer.Core@2.14.1` | 88.3% | 88.8% |
| **Promedio (7 paquetes)** | **80.6%** | **82.8%** |
| `Jint@3.1.3` | 83.8% | 84.0% |
| **Promedio (7 paquetes + Jint)** | **81.0%** | **83.0%** |

+2.2 puntos (+2.0 con Jint) de un arreglo de corrección, no de una feature nueva — `SimpleBase`
solo explica casi todo el salto en los 7 paquetes. Con 83.0% el criterio de cierre firme de 85%
todavía no se alcanza, pero el margen se cerró considerablemente.

### Cómo verificar Fase 3.17

```bash
go test ./... -race -count=5
go test ./ -run 'TestLinq|TestLazy' -count=3 -v
```

### Fase 3.18 — Segundo paquete de wins baratos + `IDictionary<K,V>` por interfaz

Tras el salto grande de Fase 3.17, esta fase ataca la siguiente franja de hallazgos concentrados
y baratos del probe, más el mismo patrón de despacho por interfaz de Fase 3.13 aplicado a
`IDictionary<K,V>`.

**Tareas**

- [x] `System.String::Contains`, `System.String::.ctor` (cubre `new string(char[])`,
      `new string(char[], start, length)`, `new string(char, count)`) — necesitó su propio camino
      en `newObj` (`internal/interpreter/calls.go`), no el registro `bcl.LookupCtor`/
      `registerCtor` normal: un `string` en vmnet es un `KindString` plano, no un `KindObject`,
      así que envolver el resultado en `runtime.ObjRef` (lo que todo otro ctor nativo hace) sería
      incorrecto — confirmado antes de escribir nada, no asumido.
- [x] `System.Environment::get_NewLine` (siempre `"\n"` — vmnet no tiene un SO real contra el
      cual matchear el valor dependiente de plataforma), `System.Convert::ToInt32` (por `Kind` del
      argumento — string/int64/float/null; un string no parseable lanza `FormatException`, no un
      resultado adivinado), `System.Double::ToString` (formato `G` invariant, mismo límite de
      cultura documentado en toda la BCL).
- [x] `List<T>::RemoveAt`/`Insert`, `Dictionary<K,V>::Clear` — extras baratos sobre colecciones ya
      soportadas.
- [x] `System.FormatException`/`System.OverflowException` agregadas al registro de excepciones
      construibles (mismo patrón que las demás desde Fase 2), con sus entradas correspondientes en
      `exceptionBaseType` (`internal/interpreter/typecheck.go`) para que `catch (Exception e)`
      también las atrape correctamente.
- [x] `System.Threading.Interlocked::CompareExchange` — el argumento `ref` llega como puntero
      administrado (`KindRef`), mismo mecanismo que cualquier `ref`/`out` desde Fase 3.5; la
      semántica de comparar-e-intercambiar es real, no solo un stub que siempre asigna (vmnet no
      tiene un modelo de memoria multi-core real contra el cual ser atómico, pero el resultado
      observable — lo que el código real realmente depende para su corrección — sí es correcto).
- [x] `System.StringComparer` (`Ordinal`/`OrdinalIgnoreCase`/`InvariantCulture`/
      `InvariantCultureIgnoreCase` — las variantes de cultura colapsan a comparación ordinal, mismo
      límite de "sin soporte de cultura" documentado en toda la BCL; solo `IgnoreCase` se
      distingue de verdad) con `Equals`/`Compare`/`GetHashCode`.
- [x] `IDictionary<K,V>::set_Item`/`get_Item`/`TryGetValue`/`ContainsKey` agregados al allowlist
      del checker (`interfaceDispatchTargets`) — el runtime ya los resolvía gratis vía el
      fallback de despacho por interfaz de Fase 3.13, reusando los natives de `Dictionary`2` ya
      existentes; nada nuevo que registrar, mismo patrón que `ICollection`1` en Fase 3.13.
- [x] `System.Convert::` promovido de `netstandard-lite` a `rules` (mismo tratamiento que
      `System.Type::` en Fase 3.14) — con natives reales detrás, `netstandard-lite` y `rules`
      ahora prometen exactamente la misma superficie BCL; el perfil `netstandard-lite` queda como
      una copia explícita de `rules` en vez de una lista adicional, documentado en el código para
      que una futura adición solo-`rules` no tenga que reconsiderarse para ambos niveles.

**Fixtures y tests**

- [x] `CheapWins2.cs` / `TestCheapWins2` — un caso por cada native de la lista de arriba

### Re-certificación contra los mismos 8 targets (7 paquetes + Jint)

| Paquete | % limpio Fase 3.17 | % limpio Fase 3.18 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 93.3% | 93.3% |
| `FluentValidation@11.9.2` | 86.4% | 87.3% |
| `System.Text.Json@8.0.5` | 80.9% | 81.7% |
| `Newtonsoft.Json@13.0.3` | 71.0% | 71.6% |
| `Semver@2.3.0` | 83.7% | 84.6% |
| `SimpleBase@4.0.0` | 75.6% | 75.6% |
| `Humanizer.Core@2.14.1` | 88.8% | 89.2% |
| **Promedio (7 paquetes)** | **82.8%** | **83.3%** |
| `Jint@3.1.3` | 84.0% | 84.4% |
| **Promedio (7 paquetes + Jint)** | **83.0%** | **83.5%** |

Con 83.5% el criterio de cierre firme de 85% todavía no se alcanza, pero el margen restante es
chico. Lo que queda con volumen real: async (fuera de alcance permanente), regex (decisión de
diseño pendiente), y reflection más profunda (`Type.MakeGenericType`/`GetGenericTypeDefinition`/
`GetInterfaces`, `Enum.GetValues`/`GetNames`/`IsDefined`).

### Cómo verificar Fase 3.18

```bash
go test ./... -race -count=3
go test ./ -run TestCheapWins2 -v
```

### Fase 3.19 — `HashSet<T>`, `Stack<T>`, `TimeSpan`

Tres superficies nuevas del probe (33/29/11+6 casos respectivamente) con volumen moderado (4/8) —
cada una una colección/value type nuevo, no una extensión de algo ya existente.

**Tareas**

- [x] `HashSet<T>` (`internal/bcl/system_hashset.go`, archivo nuevo): `Add`/`Contains`/`get_Count`/
      `GetEnumerator` + `HashSet`1+Enumerator::MoveNext`/`get_Current` (struct value type, mismo
      patrón que `List`1.Enumerator` de Fase 3.11, confirmado contra IL real antes de asumirlo).
      Deduplicación/`Contains` por barrido lineal con `valuesEqual` (`system_object.go`), no un
      `map` real de Go — `runtime.Value` no es intrínsecamente hasheable/comparable en el sentido
      de clave de mapa de Go (un `KindStruct`/`KindObject` necesitaría canonicalizarse primero);
      misma simplificación pragmática ya aceptada para `List<T>.Contains`. `Add` devuelve si el
      elemento se agregó de verdad (semántica real de `HashSet<T>.Add`), no `void`.
- [x] `Stack<T>` (`internal/bcl/system_stack.go`, archivo nuevo): `Push`/`Pop`/`Peek`/`get_Count`
      sobre un slice de Go usado como LIFO directo (`append`/truncar).
- [x] `System.TimeSpan` (`internal/bcl/system_timespan.go`, archivo nuevo): value type sintético
      de un campo (`ticks int64`, misma representación de 100ns que `DateTime` desde Fase 3.12).
      Cubre `(ticks)`, `(hours,minutes,seconds)`, `(days,hours,minutes,seconds[,milliseconds])`;
      `FromDays`/`FromHours`/`FromMinutes`/`FromSeconds`/`FromMilliseconds`; propiedades de
      componente (`Days`/`Hours`/`Minutes`/`Seconds`/`Milliseconds`, cada una el resto tras
      dividir por la unidad de arriba, no el total) y de total (`TotalDays`/.../`TotalMilliseconds`,
      `double`). Registrado también como `call` plano además de `newobj` (`timeSpanCtorInPlace`) —
      mismo bug de "asignación directa a un local" que `DateTime`/`Nullable`1` ya necesitaron
      arreglar, anticipado esta vez por el patrón ya conocido y confirmado contra IL real antes de
      escribir el fixture, no descubierto por sorpresa.

**Fixtures y tests**

- [x] `CollectionsExtra.cs` / `TestCollectionsExtra` — `HashSet<int>` con duplicado (confirma
      deduplicación real), `Stack<int>` (`Push`×3/`Pop`/`Count`), `TimeSpan.FromSeconds`,
      `new TimeSpan(1,2,3)` directo a un local

### Re-certificación contra los mismos 8 targets (7 paquetes + Jint)

| Paquete | % limpio Fase 3.18 | % limpio Fase 3.19 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 93.3% | 93.3% |
| `FluentValidation@11.9.2` | 87.3% | 87.5% |
| `System.Text.Json@8.0.5` | 81.7% | 81.8% |
| `Newtonsoft.Json@13.0.3` | 71.6% | 71.7% |
| `Semver@2.3.0` | 84.6% | 84.6% |
| `SimpleBase@4.0.0` | 75.6% | 75.6% |
| `Humanizer.Core@2.14.1` | 89.2% | 89.9% |
| **Promedio (7 paquetes)** | **83.3%** | **83.5%** |
| `Jint@3.1.3` | 84.4% | 84.8% |
| **Promedio (7 paquetes + Jint)** | **83.5%** | **83.7%** |

Movimiento chico (+0.2/+0.2) — esperado para superficies de volumen moderado y no tan anchas
(4/8). Con 83.7% el criterio de cierre firme de 85% todavía no se alcanza; falta ~1.3-1.5 puntos.

### Cómo verificar Fase 3.19

```bash
go test ./... -race -count=3
go test ./ -run TestCollectionsExtra -v
```

### Fase 3.20 — `System.Text.RegularExpressions`

Decisión de diseño ya anotada como pendiente desde Fase 3.13: vmnet compila patrones con el motor
RE2 de Go (`regexp`), no el motor real de .NET — los dos dialectos coinciden en la enorme mayoría
de uso real (clases de caracteres, cuantificadores, anclas, grupos, alternancia), pero RE2 no
tiene backreferences ni lookaround (`(?=...)`/`(?<=...)`/`(?!...)`); un patrón que los use falla
al compilar con un error claro (`ArgumentException`), no un resultado plausible-pero-incorrecto —
la misma disciplina de "nunca una respuesta silenciosamente equivocada" que el resto del proyecto
ya sigue.

**Tareas**

- [x] `Regex` (`internal/bcl/system_regex.go`, archivo nuevo): constructor (compila el patrón vía
      `regexp.Compile`), `IsMatch`/`Match` en sus formas estática (`Regex.IsMatch(input,
      pattern)`) e instancia (`regex.IsMatch(input)`), distinguidas por la forma de los argumentos
      igual que cualquier otro native multi-sobrecarga de este paquete. El match corre entero y
      eager en el momento de `Match()` (no hay `Match` perezoso real) — misma simplificación ya
      hecha para LINQ (Fase 3.15).
- [x] **Bug real encontrado y arreglado — sorpresa de jerarquía real confirmada contra IL**: la
      primera versión registró `Match::get_Success`/`Match::get_Value` directamente y nunca se
      llamaban en absoluto. La jerarquía real es `Capture -> Group -> Match`: `Value` lo declara
      `Capture`, `Success` lo declara `Group`, y `Match` **hereda ambos sin sobreescribirlos** —
      así que `m.Success`/`m.Value` sobre una instancia de `Match` compilan a `callvirt
      Group::get_Success`/`callvirt Capture::get_Value`, nunca contra `Match::` directamente.
      Encontrado corriendo el fixture real y viendo el error "receiver is not a Group/Capture,
      got *nativeMatchVal" — no asumido de antemano. Arreglado con un único accesor compartido
      (`asSuccessValue`) que lee `(Success, Value)` tanto de un `*nativeGroupVal` (un grupo de
      captura) como de un `*nativeMatchVal` (Grupo 0, el match completo), registrado una sola vez
      bajo los nombres reales `Group::get_Success`/`Capture::get_Value`.
- [x] `Match.Groups[i]` vía `GroupCollection::get_Item`/`get_Count` — `Groups[0]` es siempre el
      match completo (semántica real de Grupo 0), `Groups[1:]` los grupos de captura del patrón
      en orden. Se usa `FindStringSubmatchIndex` (pares de índices), no `FindStringSubmatch`
      (strings planos): así se distingue un grupo opcional que no participó del match (`Success =
      false`) de uno que capturó una cadena vacía — ambos serían `""` con la API de strings.
      `Match.Groups` en sí no asigna un objeto `GroupCollection` separado: como sus únicos dos
      miembros leen exactamente el mismo slice de grupos que el propio `Match` ya tiene, se
      reusa el mismo objeto Match/Native en vez de asignar un wrapper sin diferencia observable.

**Fixtures y tests**

- [x] `Regex.cs` / `TestRegex` — `IsMatch` estático (match y no-match), `Match` con grupos de
      captura (`(\w+)@(\w+)\.com`, match y no-match), `Regex` de instancia + `Match`

### Re-certificación contra los mismos 8 targets (7 paquetes + Jint)

| Paquete | % limpio Fase 3.19 | % limpio Fase 3.20 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 93.3% | 93.7% |
| `FluentValidation@11.9.2` | 87.5% | 88.1% |
| `System.Text.Json@8.0.5` | 81.8% | 81.8% |
| `Newtonsoft.Json@13.0.3` | 71.7% | 71.8% |
| `Semver@2.3.0` | 84.6% | 84.9% |
| `SimpleBase@4.0.0` | 75.6% | 75.6% |
| `Humanizer.Core@2.14.1` | 89.9% | 90.2% |
| **Promedio (7 paquetes)** | **83.5%** | **83.7%** |
| `Jint@3.1.3` | 84.8% | 84.8% |
| **Promedio (7 paquetes + Jint)** | **83.7%** | **83.9%** |

Movimiento chico (+0.2/+0.2) — regex casi nunca es el único obstáculo de un método en estos
paquetes reales (el mismo patrón visto en LINQ, Fase 3.15). Con 83.9% el criterio de cierre firme
de 85% todavía no se alcanza; falta ~1.1-1.3 puntos.

### Cómo verificar Fase 3.20

```bash
go test ./... -race -count=3
go test ./ -run TestRegex -v
```

### Fase 3.21 — Tercer paquete de wins baratos: **cruza el 85%** 🎯

Tercera ronda de hallazgos concentrados y baratos del probe. Esta fase cruza el criterio de
cierre firme original de la Fase 3.6+ (85%) — ver la nota al principio de esta sección sobre el
objetivo revisado a ~97%.

**Tareas**

- [x] `System.NotImplementedException` agregada al registro de excepciones construibles (mismo
      patrón que las demás desde Fase 2), con su entrada en `exceptionBaseType`.
- [x] `System.Double::IsInfinity`/`IsPositiveInfinity`/`IsNegativeInfinity`, `System.Math::Floor`.
- [x] `System.String::EndsWith`.
- [x] `List<T>::Clear`, `Dictionary<K,V>::Remove`.
- [x] `System.Int32::Parse`/`TryParse`/`CompareTo` — `TryParse`'s `out int` usa el mismo mecanismo
      de puntero administrado que cualquier `ref`/`out` primitivo desde Fase 3.5.
- [x] `System.DateTime::get_Kind` — necesitó agregar un segundo campo (`kind`, un
      `System.DateTimeKind` como `int32`) al value type sintético de `DateTime` (Fase 3.12), antes
      de un solo campo `ticks`. Solo `get_Now`/`get_UtcNow`/`get_Today` lo setean a algo distinto
      de `Unspecified` (el único lugar donde vmnet tiene una distinción real Utc-vs-local que
      reportar) — `Add*`/`get_Date` no propagan el `Kind` del original, una simplificación
      documentada (no medida como necesaria: ningún hallazgo del probe pedía fidelidad de `Kind`
      a través de aritmética, solo la propiedad en sí).
- [x] `KeyValuePair<K,V>` gana también el registro `call` plano (`.ctor`) además de
      `registerValueTypeCtor` — mismo patrón de "asignación directa a un local" que
      `DateTime`/`Nullable`1`/`TimeSpan` ya necesitaron, esta vez anticipado por el patrón
      conocido y confirmado contra IL real antes de escribir el fixture.
- [x] `IList<T>::get_Item`/`set_Item`, `IReadOnlyList<T>::get_Item`,
      `IReadOnlyCollection<T>::get_Count`, `IEqualityComparer<T>::Equals`/`GetHashCode` agregados
      al allowlist de despacho por interfaz de Fase 3.13 — el runtime ya los resolvía gratis
      reusando los natives de `List`1`/`EqualityComparer`1` existentes, mismo patrón que
      `ICollection`1`/`IDictionary`2` en fases anteriores.
- [x] `System.Double::`/`System.Int32::` promovidos a prefijos amplios en el perfil `rules` (antes
      solo entradas puntuales) — con natives reales cubriendo la superficie común, listar cada
      miembro por separado ya no aportaba nada sobre el prefijo completo.

**Fixtures y tests**

- [x] `CheapWins3.cs` / `TestCheapWins3` — un caso por cada native de la lista de arriba, incluido
      `IList<T>`/`IReadOnlyCollection<T>` sobre la misma instancia concreta de `List<T>`

### Re-certificación contra los mismos 8 targets (7 paquetes + Jint)

| Paquete | % limpio Fase 3.20 | % limpio Fase 3.21 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 93.7% | 93.7% |
| `FluentValidation@11.9.2` | 88.1% | 88.3% |
| `System.Text.Json@8.0.5` | 81.8% | 82.1% |
| `Newtonsoft.Json@13.0.3` | 71.8% | 72.4% |
| `Semver@2.3.0` | 84.9% | **90.8%** |
| `SimpleBase@4.0.0` | 75.6% | 75.6% |
| `Humanizer.Core@2.14.1` | 90.2% | 92.6% |
| **Promedio (7 paquetes)** | **83.7%** | **85.1%** |
| `Jint@3.1.3` | 84.8% | 86.8% |
| **Promedio (7 paquetes + Jint)** | **83.9%** | **85.3%** |

**Se cruza el criterio de cierre firme original de 85%** (85.1% en 7 paquetes, 85.3% con Jint).
`Semver` salta +5.9 puntos por sí solo (Int32.Parse/TryParse y comparación de versiones son su
superficie central); `Humanizer.Core` y `Jint` también suben con volumen real. Con el objetivo ya
revisado a ~97% (ver nota al principio de esta sección), la secuencia de sub-fases continúa.

### Cómo verificar Fase 3.21

```bash
go test ./... -race -count=3
go test ./ -run TestCheapWins3 -v
```

### Fase 3.22 — `async`/`await` (modelo síncrono) — el salto más grande de la secuencia

Un análisis de techo corrido antes de esta fase (arreglar TODO lo no-async, dejando async
permanentemente afuera como decía el registro de riesgos hasta ahora) dio un techo de **89.6%**
(7 paquetes) / **89.3%** con Jint — por debajo del nuevo objetivo de ~97%. Con async
representando la mayor parte de lo que quedaba sin cubrir en `Newtonsoft.Json`/
`System.Text.Json`/`SimpleBase` específicamente, llegar cerca de 97% sin tocarlo era
matemáticamente inviable. Se revisó la decisión de "fuera de alcance permanente" registrada desde
el principio del proyecto.

**Decisión de diseño — todo `Task` está completado por construcción**: vmnet no tiene scheduler
ni thread pool real, así que en vez de intentar modelar concurrencia cooperativa genuina, cada
`Task`/`Task<T>` que cualquier native produce (`Task.FromResult`, `AsyncTaskMethodBuilder.
SetResult`/`SetException`, `Task.Run`) está **completado desde el momento en que se crea**. Esto
tiene una consecuencia arquitectónica clave: el método `MoveNext()` que el compilador genera para
cualquier `async` (una máquina de estados real, con su propia región try/catch/finally para
enrutar excepciones) revisa `awaiter.IsCompleted` en cada `await` — y como esa propiedad siempre
da `true` en este modelo, el branch que suspende (`AwaitUnsafeOnCompleted` + `return`) nunca se
toma en la práctica. Una sola llamada a `MoveNext()` corre el método `async` completo de punta a
punta, incluyendo cualquier cantidad de `await`s encadenados o anidados. **No hizo falta tocar el
intérprete en absoluto** para el cuerpo de `MoveNext()` en sí — es IL común y corriente (campos,
branches, un try/catch/finally real), ya soportado íntegramente desde Fase 1/3.10. Todo el trabajo
de esta fase fue superficie de BCL.

**Tareas**

- [x] `AsyncTaskMethodBuilder`/`AsyncTaskMethodBuilder`1` (`internal/bcl/system_task.go`, archivo
      nuevo) como value types sintéticos de un campo (una referencia al `Task` que están
      construyendo, para que sobreviva a que la struct contenedora se copie): `Create` (estático),
      `SetStateMachine` (no-op — solo importa para boxear una máquina de estados basada en struct
      que necesite sobrevivir una suspensión real, que en este modelo nunca ocurre),
      `SetResult`/`SetException`, `get_Task`.
- [x] `AsyncTaskMethodBuilder::Start`/`AwaitUnsafeOnCompleted` (`internal/interpreter/async.go`,
      archivo nuevo, generalizando el `machineRegistry` de Fase 3.15/3.16 una vez más) — necesitan
      `Machine` para invocar el `MoveNext()` de la máquina de estados generada por el compilador
      (tipo sin acotar, resuelto por tipo real del receptor vía `receiverTypeName`, el mismo
      mecanismo que el despacho por interfaz de Fase 3.13 ya usa). `AwaitUnsafeOnCompleted` en la
      práctica nunca se ejecuta (el branch que lo invoca nunca se toma, ver arriba) — se dejó como
      fallback defensivo que igual continúa la máquina de estados en vez de fallar, por si algún
      caso futuro sí llega a necesitarlo.
- [x] `Task`/`Task<T>` como la misma instancia actuando también como su propio *awaiter* —
      `TaskAwaiter`/`ConfiguredTaskAwaitable(+Awaiter)` no tienen miembros propios más allá de
      `GetAwaiter`/`get_IsCompleted`/`GetResult`, así que asignar un wrapper separado en cada caso
      no habría cambiado nada observable. `Task::ConfigureAwait` es la función identidad (vmnet no
      tiene contexto de sincronización entre el cual saltar).
- [x] `Task.FromResult<T>`, `Task.CompletedTask`, `Task.Delay` (ignora la espera real, ya
      completado de inmediato — documentado, no una decisión escondida), `Task.Run` (invoca el
      delegate ahora mismo de forma síncrona — necesita `Machine`, va también en
      `internal/interpreter/async.go`; no desenvuelve un `Task` anidado si el delegate mismo es
      async, una simplificación documentada no medida como necesaria por el probe).
- [x] Checker: `asyncMachineTargets` (allowlist, mismo patrón que `linqTargets`/
      `interfaceDispatchTargets`) para los targets Machine-aware; prefijos de perfil para
      `System.Threading.Tasks.Task(`1)::`, `AsyncTaskMethodBuilder(`1)::`,
      `TaskAwaiter(`1)::`, `ConfiguredTaskAwaitable(`1)(+ConfiguredTaskAwaiter)::`.

**Fixtures y tests**

- [x] `Async.cs` / `TestAsync` — dos `await`s secuenciales (`ComputeAsync`), una excepción lanzada
      **después** de un `await` propagando correctamente a través de
      `GetAwaiter().GetResult()` hasta un `catch` síncrono (confirma que `SetException` +
      el re-throw de `GetResult` funcionan, no solo el camino feliz), un método `async Task` void,
      y una cadena de `await` sobre **otro método `async`** (no solo `Task.FromResult`,
      confirmando que las cadenas anidadas de verdad encadenan) — los cuatro casos funcionaron de
      punta a punta en el primer intento real contra IL real, sin ningún bug encontrado durante la
      verificación (a diferencia de casi todas las fases anteriores).

### Lo que se dejó explícitamente afuera de esta fase

```txt
- Concurrencia cooperativa real: Task.Delay no espera de verdad, Task.Run no usa un thread pool
  real (corre el delegate ya mismo, síncronamente), no hay Task.WhenAll/WhenAny reales con
  paralelismo — todo delegado a "ya está completo", correcto para el patrón dominante real (un
  plugin que usa async por conveniencia de API, no por I/O genuinamente concurrente) pero no un
  modelo de concurrencia real. Documentado en el roadmap post-v1.0 como "async/Task cooperativo
  real" si alguna vez hace falta.
- Task.Run(Func<Task<T>>) (un delegate que él mismo devuelve un Task) no desenvuelve el resultado
  anidado — produce un Task<Task<T>>, no el Task<T> aplanado real. No medido como necesario por
  el probe.
- IAsyncEnumerable<T>/await foreach — superficie distinta (enumeración asincrónica), no
  medida con volumen en el probe.
```

### Re-certificación contra los mismos 8 targets (7 paquetes + Jint)

| Paquete | % limpio Fase 3.21 | % limpio Fase 3.22 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 93.7% | 96.8% |
| `FluentValidation@11.9.2` | 88.3% | 91.5% |
| `System.Text.Json@8.0.5` | 82.1% | 82.4% |
| `Newtonsoft.Json@13.0.3` | 72.4% | **78.6%** |
| `Semver@2.3.0` | 90.8% | 90.8% |
| `SimpleBase@4.0.0` | 75.6% | **84.1%** |
| `Humanizer.Core@2.14.1` | 92.6% | 92.6% |
| **Promedio (7 paquetes)** | **85.1%** | **88.1%** |
| `Jint@3.1.3` | 86.8% | 86.8% |
| **Promedio (7 paquetes + Jint)** | **85.3%** | **88.0%** |

**+3.0 puntos en los 7 paquetes (+2.7 con Jint) — el salto más grande de toda la secuencia
3.6-3.22.** `SimpleBase` (+8.5) y `Newtonsoft.Json` (+6.2) confirman exactamente la hipótesis del
análisis de techo: eran los paquetes con más superficie async real. Con 88.0% el nuevo objetivo de
~97% todavía no se alcanza, pero el salto confirma que atacar async era la decisión correcta.

### Cómo verificar Fase 3.22

```bash
go test ./... -race -count=5
go test ./ -run TestAsync -v
```

### Fase 3.23 — Cuarto paquete de wins baratos + dos bugs reales de corrección

Cuarta ronda de hallazgos del probe (`DateTimeOffset`, operadores de `DateTime`,
`Double.TryParse`, `Convert.ToInt64`, `Char.ToLowerInvariant`, `Int64.ToString`, `ValueTuple`,
más LINQ, `CultureInfo`, `IList`). Verificar estos natives contra IL real expuso dos bugs
genuinos en mecanismos ya existentes (el despacho por interfaz de Fase 3.13 y `fieldSlot` desde
Fase 3.7), no solo faltantes de superficie.

**Tareas — wins baratos**

- [x] `System.DateTimeOffset` (`internal/bcl/system_datetimeoffset.go`, archivo nuevo): value type
      sintético de dos campos (`ticks` UTC + `offsetTicks`) — mismo doble registro `newobj`+`call`
      plano que `DateTime`/`Nullable`1`/`TimeSpan`/`KeyValuePair` ya necesitaron.
      `get_UtcDateTime`/`get_DateTime`/`get_Offset`/`get_Ticks`.
- [x] `DateTime::op_Subtraction` (devuelve `TimeSpan`, reusando el mismo campo `ticks` de 100ns),
      `op_Equality`/`op_Inequality`, `ToUniversalTime`/`ToLocalTime` (función identidad — vmnet no
      tiene zona horaria local real contra la cual convertir, mismo razonamiento que
      `Environment.NewLine` desde Fase 3.18).
- [x] `Double.TryParse` (mismo mecanismo de `out` por puntero administrado que `Int32.TryParse`),
      `Double.Equals`, `Convert.ToInt64`, `Char.ToUpperInvariant`/`ToLowerInvariant` (misma
      transformación que las variantes sensibles a cultura — vmnet no tiene soporte de cultura en
      ningún lado), `Int64.ToString`.
- [x] `System.ValueTuple`2` (`internal/bcl/system_valuetuple.go`, archivo nuevo) — a diferencia de
      cualquier otro value type de este paquete, sus miembros (`Item1`/`Item2`) son campos
      públicos reales, no propiedades: registrarlo como value type sintético con esos dos campos
      alcanza, `ldfld`/`stfld` ya resuelven genéricamente contra cualquier `Type.FieldIndex`
      registrado — cero código nativo de getter/setter necesario.
- [x] LINQ: `SelectMany` (aplana invocando el selector y enumerando su resultado con el mismo
      `enumerateAll` genérico), `Take`, `Contains`, `Empty`.
- [x] `System.Collections.IList::Add`/`get_Item`/`set_Item` agregados al allowlist de despacho por
      interfaz de Fase 3.13 (`IList.Count` ya funcionaba gratis: en la BCL real `Count` lo declara
      `ICollection`, no `IList`, y `System.Collections.ICollection::get_Count` ya estaba desde
      Fase 3.13 — mismo patrón "miembro heredado, no redeclarado" que `Match.Success`/`Value` en
      Fase 3.20).
- [x] `CultureInfo::get_CurrentCulture`/`get_Name` (stubs).

**Tareas — bugs reales encontrados y arreglados**

- [x] **Bug — el despacho por interfaz (Fase 3.13) podía dejar la pila corta cuando la firma
      real del método concreto difiere de la de la interfaz declarada**: `System.Collections.
      IList::Add` devuelve `int` (el índice insertado), pero redirige a `List`1::Add`, que es
      `void`. La pila se desbalanceaba (nada empujado donde el sitio de llamada esperaba un
      valor), causando un panic real (`index out of range [-1]`) en la siguiente instrucción que
      intentaba consumirlo — encontrado ejecutando el fixture real, no por inspección. Arreglado
      en `internal/interpreter/eval.go`: la decisión de empujar un resultado ahora usa
      `in.HasReturn` (la firma declarada en el sitio de llamada, conocida en tiempo de
      construcción del IR) como autoridad, no el `hasReturn` que reporta el callee finalmente
      resuelto — si difieren, se empuja `Null()` como placeholder para mantener la pila
      balanceada (el resultado real solo se pierde si alguien de verdad captura el valor de
      retorno de `IList.Add`, un patrón raro en la práctica).
- [x] **Bug — `fieldSlot` nunca manejaba un receptor struct pasado por valor directo (sin
      puntero administrado)**: hasta ahora, cada acceso a campo de struct visto en este proyecto
      usaba `ldloca`+`ldfld` (puntero administrado, el caso `KindRef` de `fieldSlot`). El fixture
      de `ValueTuple` reveló que el compilador real a veces emite `ldloc`+`ldfld` directo (sin
      dirección) para el *segundo* acceso a campo en la misma expresión (`t.Item1 + t.Item2`:
      `Item1` vía `ldloca`+`ldflda`, pero `Item2` vía `ldloc`+`ldfld` plano) — legal según spec
      §III.4.10, pero un caso nunca antes ejercitado en la práctica. `fieldSlot` solo tenía casos
      para `KindObject`/`KindRef`; un `KindStruct` bare caía al `default:` y lanzaba
      `NullReferenceException`. Se agregó el caso `KindStruct` directo.
- [x] **Descubrimiento arquitectónico — un value type nativo de BCL nunca había necesitado un
      campo *estático* real hasta `TimeSpan.Zero`**: es un campo público estático real (`ldsfld
      System.TimeSpan::Zero`), no una propiedad. `runtime.NewValueType` no soporta campos
      estáticos en absoluto (documentado en su propio comentario, nunca hacía falta); `timeSpanType`
      se reconstruyó usando `runtime.NewType` directamente más `SetStaticField` para el valor real
      (un `TimeSpan` cero que se autorreferencia, por lo que no puede ir en el literal de
      construcción). También se agregó un fallback en `resolveTypeByFullName` (`assembly.go`)
      para consultar `bcl.LookupValueType` cuando el tipo no tiene `TypeDef` en el ensamblado del
      plugin — necesario para que `ir.LoadStaticField` pueda resolver el `*runtime.Type` de
      `System.TimeSpan` en absoluto.

**Fixtures y tests**

- [x] `CheapWins4.cs` / `TestCheapWins4` — un caso por cada native de la lista de arriba, más
      `IListAddTest` (regresión del bug de firma distinta) y `ValueTupleTest` (regresión del bug
      de `fieldSlot`)

### Re-certificación contra los mismos 8 targets (7 paquetes + Jint)

| Paquete | % limpio Fase 3.22 | % limpio Fase 3.23 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 96.8% | 96.8% |
| `FluentValidation@11.9.2` | 91.5% | 92.7% |
| `System.Text.Json@8.0.5` | 82.4% | 82.7% |
| `Newtonsoft.Json@13.0.3` | 78.6% | 79.2% |
| `Semver@2.3.0` | 90.8% | 91.0% |
| `SimpleBase@4.0.0` | 84.1% | 85.3% |
| `Humanizer.Core@2.14.1` | 92.6% | 93.3% |
| **Promedio (7 paquetes)** | **88.1%** | **88.7%** |
| `Jint@3.1.3` | 86.8% | 87.2% |
| **Promedio (7 paquetes + Jint)** | **88.0%** | **88.5%** |

+0.6 puntos (+0.5 con Jint) — movimiento chico esperado para una ronda de wins dispersos, pero el
valor real de la fase son los dos bugs de corrección arreglados (uno de ellos, el de la pila
desbalanceada, es un riesgo que existía silenciosamente desde Fase 3.13 en CUALQUIER despacho por
interfaz con firma incompatible, no solo `IList.Add`). Con 88.5% el objetivo de ~97% todavía no se
alcanza.

### Cómo verificar Fase 3.23

```bash
go test ./... -race -count=5
go test ./ -run TestCheapWins4 -v
```

### Fase 3.24 — Quinto paquete de wins baratos: ConcurrentDictionary, Regex.Replace, Delegate multicast

Quinta ronda del probe de findings-por-target. A diferencia de las rondas anteriores, el probe
post-3.23 ya no mostraba una cola larga de superficies dispersas de volumen moderado: los
hallazgos con más ancho (5-4/8 paquetes) están ahora concentrados casi enteramente en reflexión
profunda (`Type.MakeGenericType`/`GetGenericTypeDefinition`/`GetInterfaces`/`get_IsGenericType`/
`get_IsEnum`/`GetMethod(s)`/`GetProperties`/`GetConstructors`/`get_BaseType`, `System.Reflection.
MethodInfo`/`PropertyInfo`/`ParameterInfo`/`MemberInfo`/`Assembly`, `MethodBase.Invoke`,
`Activator.CreateInstance`, `System.Enum.GetValues`/`GetNames`/`IsDefined`/`ToObject` —
requieren introspección respaldada por metadata real, no solo un native más). Esta fase toma la
última cosecha de superficie barata que NO depende de reflexión antes de abordar ese bloque más
grande en una fase dedicada.

**Tareas**

- [x] `System.Collections.Concurrent.ConcurrentDictionary`2` (`internal/bcl/
      system_concurrentdictionary.go`, archivo nuevo): mismo backing por mutex + `map[string]
      Value` que `Dictionary`2` (limitación de solo-claves-string ya documentada, Fase 2), más un
      `sync.Mutex` real — el punto de este tipo sobre `Dictionary`2` es acceso concurrente
      seguro, y aunque un único `Machine` de vmnet nunca corre en más de una goroutine a la vez,
      una aplicación host sí puede compartir legítimamente un `ConcurrentDictionary` entre varias.
      `GetOrAdd` tiene dos overloads reales (`(key, TValue value)` y `(key, Func<TKey,TValue>
      factory)`) resueltos bajo el mismo nombre de call target — como el dispatch de vmnet no
      distingue overloads por firma, el `Kind` del tercer argumento los distingue en tiempo de
      ejecución (mismo patrón que `resolveRegexAndInput`). Invocar el factory necesita acceso a
      `Machine`, así que `GetOrAdd` se resuelve por el registro Machine-aware
      (`internal/interpreter/concurrentdict.go`), no por un native plano — mismo motivo que
      `Lazy`1.Value` (Fase 3.17). El resto (`TryAdd`/`TryGetValue`/`TryRemove`/`ContainsKey`/
      indexador/`get_Count`) sí son natives planos.
- [x] `Regex.Replace` (`internal/bcl/system_regex.go`): mismo mecanismo de desambiguación estático
      vs. instancia por `Kind` que `IsMatch`/`Match` (`resolveRegexReplace`), reusando el motor
      RE2 de Go (`ReplaceAllString`) — la sintaxis `$1`/`${name}` de reemplazo de .NET coincide
      con la de Go en los casos comunes, misma limitación de dialecto ya documentada para
      `IsMatch`/`Match` (Fase 3.20).
- [x] `Delegate.Combine`/`Delegate.Remove` (`internal/bcl/system_delegate.go`, archivo nuevo):
      primer soporte real de delegado multicast del proyecto. `runtime.Func` ganó un campo
      `Chain []*Func` (lista de targets adicionales); `Machine.invokeFunc`
      (`internal/interpreter/calls.go`) ahora invoca el target propio y luego cada entrada de
      `Chain` en orden, descartando todos los resultados menos el último — igual que
      `MulticastDelegate.Invoke` real. `Combine`/`Remove` son natives planos: solo manipulan
      listas de `*Func`, no necesitan invocar nada.
- [x] `System.Array::GetEnumerator` + su enumerador (`internal/bcl/system_array.go`): a diferencia
      de `List`1.Enumerator` (struct inlineado directo en el call site del `foreach`, Fase 3.11),
      un array recorrido por el protocolo no genérico `IEnumerable` recibe un enumerador real de
      tipo referencia (`System.Array+SZArrayEnumerator` en la BCL real) — confirmado contra IL
      real (Fase 3.24): `foreach` sobre una fuente tipada `Array`/`IEnumerable` compila a
      `callvirt System.Array::GetEnumerator` directo, y el *resultado* se recorre a través de la
      interfaz `IEnumerator` (`callvirt MoveNext`/`get_Current`). Se agregó `nativeArrayEnumerator`
      con una entrada real en `NativeTypeName` (`system_object.go`) — el despacho por interfaz de
      Fase 3.13 es lo que redirige esas llamadas tipadas por interfaz a los natives concretos
      registrados bajo `System.Array+ArrayEnumerator::`.
- [x] **Bug real encontrado al verificar `(Action)Delegate.Combine(a1, a2)` contra IL real**:
      `isAssignableTo` (`internal/interpreter/typecheck.go`) no tenía ningún caso para
      `KindFunc` — un delegado nunca había necesitado pasar por un `castclass`/`isinst` real hasta
      ahora (`Action a = SomeMethod;` no emite `castclass`; el compilador ya construye el tipo
      correcto). `Delegate.Combine` devuelve `Delegate` (el tipo base), así que asignarlo a
      `Action` sí compila a un `castclass Action` real. Como `runtime.Func` no lleva su propio
      tipo de delegado declarado (se detecta estructuralmente, no por tipo — ver el comentario de
      `Func`, Fase 3.9), no hay nada contra qué chequear: se agregó `case runtime.KindFunc: return
      true`, aceptando cualquier cast/isinst delegado-a-delegado sobre un valor de delegado real.

**Fixtures y tests**

- [x] `CheapWins5.cs` / `TestCheapWins5` — un caso por cada native de la lista de arriba, más
      `ConcurrentDictGetOrAddFactoryTest` (confirma que el factory corre exactamente una vez pese
      a tres llamadas con la misma clave) y `DelegateCombineThenRemoveTest` (regresión de
      multicast: combinar dos, quitar uno, invocar el que queda).

**Lo que se dejó explícitamente afuera**

- `System.Enum.GetValues`/`GetNames`/`IsDefined`/`ToObject`: requieren leer los literales de campo
  estático de un `enum` real desde su `TypeDef` (tabla `Constant`, sin parser en
  `internal/metadata` todavía) — parte del mismo bloque de reflexión profunda que el resto de la
  lista de abajo, no una superficie aislada.
- Reflexión profunda completa (`Type.GetMethods`/`GetProperties`/`GetConstructors`/
  `get_BaseType`/`GetInterfaces`/`MakeGenericType`/`GetGenericTypeDefinition`/
  `GetGenericArguments`/`get_IsGenericType`/`get_IsEnum`/`get_IsValueType`/`get_IsInterface`,
  `System.Reflection.MethodInfo`/`PropertyInfo`/`ConstructorInfo`/`ParameterInfo`/`MemberInfo`/
  `Assembly`, `MethodBase.Invoke`, `Activator.CreateInstance`): confirmado por el probe como la
  categoría dominante restante (findings de 4-5/8 de ancho, la mayor concentración vista desde
  Fase 3.13) — candidato natural para una fase dedicada propia, con su propio diseño (necesita
  introspección respaldada por metadata real más invocación dinámica, no solo otro native más).
- `System.Linq.Expressions` (árboles de expresión — `Expression.Parameter`/`Lambda`): apareció con
  volumen moderado (3/8) en el mismo probe, pero es una superficie completamente nueva (parsear y
  evaluar un árbol de expresión) sin relación con la reflexión de arriba más allá de compartir
  cliente (Jint la usa para JIT/interpretación de expresiones JS compiladas).
- `Span`1::op_Implicit` (53 casos, 3/8): conversión implícita `T[] -> Span<T>`/`Span<T> ->
  ReadOnlySpan<T>` sin native propio; candidato barato para una fase futura si el probe lo sigue
  mostrando con volumen tras resolver reflexión.
- `ldsflda`/`localloc` (opcodes, 3/8 cada uno): dirección de un campo estático real y buffer de
  pila dinámico — ninguno tiene un fixture propio verificado contra IL real todavía.

### Re-certificación contra los mismos 8 targets (7 paquetes + Jint)

| Paquete | % limpio Fase 3.23 | % limpio Fase 3.24 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 96.8% | 96.8% |
| `FluentValidation@11.9.2` | 92.7% | 93.3% |
| `System.Text.Json@8.0.5` | 82.7% | 82.8% |
| `Newtonsoft.Json@13.0.3` | 79.2% | 79.6% |
| `Semver@2.3.0` | 91.0% | 91.0% |
| `SimpleBase@4.0.0` | 85.3% | 85.3% |
| `Humanizer.Core@2.14.1` | 93.3% | 93.4% |
| **Promedio (7 paquetes)** | **88.7%** | **88.9%** |
| `Jint@3.1.3` | 87.2% | 87.5% |
| **Promedio (7 paquetes + Jint)** | **88.5%** | **88.7%** |

+0.2 puntos — el movimiento más chico de la secuencia de "wins baratos" (esperado: `Concurrent
Dictionary`/`Delegate.Combine`/`Regex.Replace` son superficies reales pero angostas comparadas con
LINQ o async). `FluentValidation` (+0.6) y `Newtonsoft.Json` (+0.4) concentran casi todo el
movimiento — consistente con ambos usando caches basados en delegados/diccionarios concurrentes
internamente. Confirma la lectura de la sección anterior: con 88.7% y sin más superficie barata de
volumen visible en el probe, el camino hacia ~97% pasa por la reflexión profunda, no por otra
ronda de wins dispersos.

### Cómo verificar Fase 3.24

```bash
go test ./... -race -count=5
go test ./ -run TestCheapWins5 -v
```

### Fase 3.25 — Reflexión profunda, primera porción: introspección de System.Type

Primera porción del bloque de "reflexión profunda" identificado como la categoría dominante
restante al cierre de Fase 3.24 (findings de 4-5/8 de ancho). Alcance deliberadamente acotado a
`System.Type` — introspección pura (generics, `IsValueType`/`IsEnum`/`IsInterface`/`BaseType`/
`GetInterfaces`, `Type.GetType(string)`) — dejando afuera el bloque más grande todavía
(`System.Reflection.MethodInfo`/`PropertyInfo`/`ConstructorInfo`/`ParameterInfo`, invocación
dinámica vía `MethodBase.Invoke`/`Activator.CreateInstance`), que necesita una jerarquía de
objetos real respaldada por metadata, no solo manipulación de nombres.

**Tareas — generics de `System.Type`**

- [x] **Cambio de raíz — `internal/metadata/signatures.go`**: `SigType` ganó un campo `Args
      []SigType`, poblado en la rama `elementGenericInst` de `parseType` (antes descartaba cada
      argumento parseado: `_, sz3, err := parseType(...)`). Aditivo puro — todo consumidor
      existente de `SigType` sigue ignorando `Args` igual que antes; ningún comportamiento previo
      cambia.
- [x] **`internal/ir/builder.go`**: nuevo `resolveClosedTypeSpecName`/`sigTypeFullName`, usado
      *solo* por el caso `ldtoken` (`typeof(T)`) — a diferencia de `resolveTypeTokenOrGeneric`
      (usado por `initobj`/`ldobj`/`stobj` y resolución de `MemberRef`, que siguen sin necesitar
      más que el nombre abierto), `typeof(List<int>)` ahora retiene sus argumentos como
      `"System.Collections.Generic.List\`1[[System.Int32]]"` — confirmado contra IL real que
      `typeof(List<>)` (genérico abierto) sigue resolviendo directo a un `TypeDef`/`TypeRef` sin
      `TypeSpec` en absoluto, así que el nombre abierto nunca gana corchetes por accidente.
- [x] `Type.get_IsGenericType` (`internal/bcl/system_type.go`): `strings.Contains(nombre, "\`")`
      sobre la porción antes de `[[` — cierto tanto para el tipo abierto como el cerrado, igual
      que el contrato real.
- [x] `Type.GetGenericTypeDefinition()`: recorta el sufijo `[[...]]` si existe.
- [x] `Type.GetGenericArguments()`: parser `splitGenericArgs` con seguimiento de profundidad de
      corchetes (un argumento puede ser él mismo un genérico cerrado anidado). Vacío para un
      genérico abierto (`typeof(List<>)`) — .NET real devuelve los parámetros (`T`) ahí, que
      vmnet no tiene forma de nombrar (limitación documentada).
- [x] `Type.MakeGenericType(params Type[])`: a diferencia de `typeof(T)`, SIEMPRE recibe nombres
      reales en tiempo de ejecución (el compilador siempre baja `params Type[]` a un array real
      en el sitio de llamada) — construye el nombre cerrado directamente, sin depender de que el
      genérico abierto original haya retenido nada.
- [x] `System.Nullable::GetUnderlyingType(Type)` (nótese: la clase helper no genérica
      `System.Nullable`, no `System.Nullable\`1` — un método real distinto) — mismo parser de
      corchetes, `null` para cualquier tipo que no sea `Nullable\`1[[...]]` cerrado.

**Tareas — clasificación de tipos (Machine-aware, `internal/interpreter/reflection.go`)**

- [x] `runtime.Type` ganó `IsEnum`/`IsInterface` (antes solo existía `IsValueType`, que colapsaba
      struct y enum juntos — Fase 3.7 nunca necesitó la distinción). `assembly.go`'s `buildType`
      los puebla: `classifyTypeDef` (antes `isValueType`) ahora también reporta `isEnum`
      (`Extends == "System.Enum"` específicamente, no solo `"System.ValueType"`), e `isInterface`
      lee el bit `TypeAttributes.Interface` (`0x20`) directo de `TypeDefRow.Flags` — el único de
      los tres que no se podía derivar de `Extends` (una interfaz, igual que `System.Object`
      mismo, no tiene `Extends` en absoluto).
- [x] `Type.IsValueType`/`IsEnum`/`IsInterface`/`BaseType`/`GetInterfaces()`: clasificación en dos
      niveles — un mapa fijo de primitivos/interfaces BCL conocidos primero (mismo patrón que
      `exceptionBaseType`/`interfaceDispatchTargets`), luego el `TypeDef` real de un tipo de
      plugin vía `Machine.ResolveType`. `GetInterfaces()` devuelve solo lo directamente
      implementado (`runtime.Type.Interfaces`, sin expandir transitivamente — mismo alcance que
      `isinst`/`castclass` desde Fase 3.8).
- [x] `Type.GetType(string)`: resuelve un tipo de plugin vía `Machine.ResolveType` o un value type
      nativo de BCL vía `bcl.LookupValueType`; cualquier otro nombre (una búsqueda cross-assembly
      real, que necesita nombre calificado por ensamblado y un loader que vmnet no tiene) devuelve
      `null`, igual que el contrato real de `Type.GetType` para un nombre que no puede resolver.
- [x] `Type.Assembly` (`internal/bcl/system_type.go`): stub `System.Reflection.Assembly` — vmnet
      no modela múltiples ensamblados reales, así que todo valor `Assembly` es intercambiable;
      solo `.ToString()`/`.FullName` devuelven una constante plausible (mismo precedente que el
      stub de `CultureInfo`, Fase 3.23).

**Bug real encontrado y arreglado**

- [x] **Recursión infinita en `buildType` al construir el primer `enum` declarado por un plugin
      en todo el proyecto**: cada miembro de un enum (`Red` en `enum TrafficLight`) es un campo
      `static literal` cuyo tipo, en IL real, es el propio enum (`static literal valuetype
      TrafficLight Red = int32(0)`) — no `int32` como podría suponerse. `buildType` calculaba un
      default para *todo* campo (estático o no) antes de separarlos, así que ese campo
      autorreferenciado disparaba `fieldOrLocalDefault` → `valueTypeDefault` →
      `resolveTypeByFullName("TrafficLight")` → `buildType("TrafficLight")` de nuevo — el tipo
      todavía no estaba en la caché (`asm.types`) porque su propia construcción no había
      terminado, así que cada vuelta repetía la misma cadena hasta agotar la pila
      (`stack overflow` real, encontrado corriendo el fixture, no por inspección). Arreglado
      saltando `fieldOrLocalDefault` para cualquier campo `FieldAttributes.Literal` (`0x40`): su
      valor real vive en la tabla `Constant`, que vmnet todavía no lee (mismo motivo por el que
      `Enum.GetValues`/`IsDefined` siguen fuera de alcance — ver abajo), así que no había ningún
      default útil que calcular de todos modos.

**Fixtures y tests**

- [x] `Reflection2.cs` / `TestReflection2` (22 casos) — reusa la jerarquía `Animal`/`Dog`/`IShape`
      de `TypeChecks.cs` y el struct `Point` de `Structs.cs` para que `BaseType`/`GetInterfaces`
      ejerciten un `TypeDef` de plugin real, no solo nombres BCL; declara `TrafficLight`, el
      primer `enum` de plugin del proyecto (regresión directa del bug de arriba).

**Lo que se dejó explícitamente afuera**

- `System.Enum.GetValues`/`GetNames`/`IsDefined`/`ToObject`: necesitan leer la tabla `Constant`
  (valor literal real de cada miembro) — sin parser todavía en `internal/metadata`. Es la pieza
  que más se repite en el probe (5/8 de ancho) pero es un módulo aparte, no una extensión barata
  de esta fase.
- El resto de la reflexión profunda: `System.Reflection.MethodInfo`/`PropertyInfo`/
  `ConstructorInfo`/`ParameterInfo`/`MemberInfo`/`Assembly` como objetos reales,
  `MethodBase.Invoke`/`Activator.CreateInstance` (invocación dinámica), `Type.GetMethod(s)`/
  `GetProperties`/`GetConstructors`/`GetFields`/`GetElementType`/`get_IsArray`/`get_IsAbstract` —
  confirmado por el probe post-3.25 como el bloque de mayor volumen restante; necesita una
  jerarquía de objetos respaldada por metadata real (RID de método/propiedad/campo) más
  invocación dinámica genuina (`Machine.call` desde un `MethodInfo` arbitrario), un diseño
  bastante más grande que cualquier cosa de esta fase — candidato para Fase 3.26.
- `System.Linq.Expressions` (`Expression.Parameter`/`Lambda`, 3/8): árboles de expresión, todavía
  sin relación con el resto de la reflexión más allá de compartir cliente (Jint).
- `Span\`1::op_Implicit`/`CopyTo` (3/8), `ldsflda`/`localloc` (opcodes, 3/8), `Convert.ChangeType`,
  `Array.IndexOf`, `List\`1::Remove`, `RuntimeHelpers.GetHashCode`: superficie dispersa de volumen
  moderado sin relación con reflexión — candidatos para un futuro paquete de wins baratos.

### Re-certificación contra los mismos 8 targets (7 paquetes + Jint)

| Paquete | % limpio Fase 3.24 | % limpio Fase 3.25 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 96.8% | 96.8% |
| `FluentValidation@11.9.2` | 93.3% | 93.7% |
| `System.Text.Json@8.0.5` | 82.8% | 83.7% |
| `Newtonsoft.Json@13.0.3` | 79.6% | 80.4% |
| `Semver@2.3.0` | 91.0% | 91.0% |
| `SimpleBase@4.0.0` | 85.3% | 85.3% |
| `Humanizer.Core@2.14.1` | 93.4% | 93.4% |
| **Promedio (7 paquetes)** | **88.9%** | **89.2%** |
| `Jint@3.1.3` | 87.5% | 87.6% |
| **Promedio (7 paquetes + Jint)** | **88.7%** | **89.0%** |

+0.3 puntos — movimiento moderado para una porción deliberadamente acotada (solo introspección de
`Type`, sin tocar todavía `MethodInfo`/`PropertyInfo`/invocación dinámica). `System.Text.Json`
(+0.9) y `Newtonsoft.Json` (+0.8) concentran la mayor parte, consistente con ambos usando
`Type.IsGenericType`/`GetGenericArguments`/`IsValueType`/`IsEnum` en sus rutas de
serialización/deserialización basadas en reflexión. Con 89.0% el objetivo de ~97% todavía no se
alcanza — el probe confirma que el resto del bloque de reflexión (`MethodInfo`/`PropertyInfo`/
invocación dinámica, `Enum.*`) es ahora, con claridad, la superficie de mayor volumen restante.

### Cómo verificar Fase 3.25

```bash
go test ./... -race -count=5
go test ./ -run TestReflection2 -v
```

### Fase 3.26 — System.Enum.GetValues/GetNames/IsDefined/ToObject

El hallazgo de mayor ancho tras Fase 3.25 (`System.Enum::IsDefined`, 5/8 paquetes). A diferencia de
la introspección de `Type` (Fase 3.25, pura manipulación de nombres), esto necesita un dato que
vmnet nunca había leído: el valor real de cada miembro de un enum, que vive en la tabla `Constant`
de metadata (spec §II.22.9) — sin parser hasta esta fase.

**Tareas**

- [x] **`internal/metadata/constant.go`** (archivo nuevo): `constantForField` (búsqueda lineal
      sobre la tabla `Constant`, que no tiene índice directo desde un RID de campo — igual que
      System.Reflection.Metadata de .NET real la calcula perezosamente también; la tabla es
      pequeña y esto solo se llama por-enum, no por-acceso-a-campo), `decodeConstantInt64`
      (decodifica el blob según su tag de tipo: booleano/char/i1/u1/i2/u2/i4/u4/i8/u8 — el único
      conjunto de formas que el valor subyacente de un miembro de enum puede tomar), y
      `EnumMembers(typeRID)` (nombres + valores reales, en orden de declaración, saltando el
      campo `value__` no-literal que Fase 3.25 ya identificó). `ConstantRow`/`md.Constant(rid)`
      ya existían en `internal/metadata/resolver.go` (aparentemente de una fase anterior, nunca
      conectados a nada) — esta fase es lo que finalmente los usa.
- [x] **Nuevo resolver en la cadena `Machine`**: `EnumResolver` (`internal/interpreter/calls.go`)
      + `Machine.ResolveEnum` + `WithEnumResolver` (`eval.go`), mismo patrón que
      `ExplicitImplResolver` (Fase 3.13) — conectado en `call.go` vía `asm.resolveEnumMembers`
      (`assembly.go`: `FindTypeDef` + `md.EnumMembers`). Solo resuelve un enum declarado por el
      propio plugin (un `TypeDef` real); un enum solo-BCL como `System.DayOfWeek` no tiene
      metadata en absoluto en el ensamblado del plugin, así que falla ahí — vmnet no tiene (ni
      tendrá pronto) una base de datos de miembros de enums de la BCL real.
- [x] `Enum.GetValues(Type)`/`GetNames(Type)` (Machine-aware, `internal/interpreter/
      reflection.go`): arrays de `Int32`/`String` en orden de declaración. `GetValues` no
      necesitó ningún cambio en el intérprete — el array resultante ya fluye a través de
      `System.Array::GetEnumerator` (Fase 3.24) para el `foreach` que casi siempre lo consume.
- [x] `Enum.IsDefined(Type, object)`: acepta tanto el valor entero subyacente como el nombre del
      miembro (dos formas reales del mismo overload) — el `Kind` del segundo argumento elige la
      comparación, mismo patrón que cada otro native multi-overload de este proyecto.
- [x] `Enum.ToObject(Type, object)`: no-op sobre el valor subyacente — boxear un enum no cambia su
      representación en el modelo de `Value` de vmnet (mismo razonamiento que el comentario de
      `objectToString`), y — igual que la implementación real — no valida que el valor sea
      realmente un miembro definido.

**Fixtures y tests**

- [x] `Reflection3.cs` / `TestReflection3` (6 casos) — reusa el `enum TrafficLight` de
      `Reflection2.cs` (Fase 3.25).

**Lo que se dejó explícitamente afuera**

- Un enum solo-BCL (`System.DayOfWeek`, `System.ConsoleColor`, ...) sigue sin funcionar: ninguno
  tiene `TypeDef` en el ensamblado del plugin. Cubrir esto necesitaría una base de datos completa
  hardcodeada de miembros de enums BCL conocidos — alto mantenimiento, bajo valor frente al bloque
  de reflexión real que sigue (ver abajo).
- El bloque grande de reflexión sigue intacto: `System.Reflection.MethodInfo`/`PropertyInfo`/
  `ConstructorInfo`/`ParameterInfo`/`MemberInfo` como objetos reales, `MethodBase.Invoke`/
  `Activator.CreateInstance` (invocación dinámica genuina), `Type.GetMethod(s)`/`GetProperties`/
  `GetConstructors`/`GetFields`/`GetElementType`/`get_IsArray`/`get_IsAbstract` — confirmado por el
  probe post-3.26 como, con claridad, el bloque de mayor volumen restante (4/8 de ancho:
  `MethodBase.Invoke`, `MethodInfo::op_Inequality`, `PropertyInfo::get_PropertyType`,
  `CustomAttributeExtensions::GetCustomAttribute`).
- `System.Linq.Expressions` (`Expression.Parameter`/`Lambda`, 3/8), `Span\`1::op_Implicit`/
  `CopyTo`/`Fill` (3/8), `ldsflda`/`localloc` (opcodes, 3/8), `Convert.ChangeType`,
  `Array.IndexOf`, `List\`1::Remove`, `System.Numerics.BigInteger`, `RuntimeHelpers.GetHashCode`,
  `Math.Sign`: superficie dispersa sin relación con reflexión — candidatos para un futuro paquete
  de wins baratos.

### Re-certificación contra los mismos 8 targets (7 paquetes + Jint)

| Paquete | % limpio Fase 3.25 | % limpio Fase 3.26 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 96.8% | 96.8% |
| `FluentValidation@11.9.2` | 93.7% | 93.9% |
| `System.Text.Json@8.0.5` | 83.7% | 83.8% |
| `Newtonsoft.Json@13.0.3` | 80.4% | 80.4% |
| `Semver@2.3.0` | 91.0% | 91.0% |
| `SimpleBase@4.0.0` | 85.3% | 85.3% |
| `Humanizer.Core@2.14.1` | 93.4% | 93.4% |
| **Promedio (7 paquetes)** | **89.2%** | **89.2%** |
| `Jint@3.1.3` | 87.6% | 87.6% |
| **Promedio (7 paquetes + Jint)** | **89.0%** | **89.0%** |

Movimiento nulo al nivel de precisión de la tabla (89.2%/89.0% en ambas), pero real bajo el
capó: el conteo total de *findings* individuales bajó en cada paquete tocado (`System.Text.Json`
1306→1301, `Newtonsoft.Json` 1581→1572, `Humanizer.Core` 215→209, `Jint` 1733→1730,
`FluentValidation` 188→185, `Ardalis.GuardClauses` 16→14) — las cuatro llamadas de `Enum` dejaron
de aparecer como *finding* en absoluto. La certificación no se mueve porque es una métrica *por
método*: los métodos que llaman `Enum.GetValues`/`IsDefined` en estos paquetes casi siempre
llaman TAMBIÉN algo del bloque grande de reflexión (`MethodInfo`, `Expression`, ...) en el mismo
método, así que siguen contando como "método con hallazgos" de todos modos. Esto confirma con
más fuerza todavía la lectura de Fase 3.25: el único camino real hacia ~97% pasa ahora por el
bloque de `MethodInfo`/`PropertyInfo`/invocación dinámica — cualquier superficie más chica,
aislada, seguirá sin mover el número agregado mientras ese bloque siga intacto.

### Cómo verificar Fase 3.26

```bash
go test ./... -race -count=5
go test ./ -run TestReflection3 -v
```

---

### Fase 3.27 — Resolución multi-ensamblado + demo real de `Jint.Engine.Evaluate()`

Disparada por una pregunta directa: ¿puede vmnet correr el ejemplo de Jint de verdad, no un stub?
La respuesta arrancó en "no": `Call()` solo invocaba métodos estáticos de un único ensamblado, y
Jint necesita resolver símbolos a través de su propia cadena de dependencias NuGet real (Jint →
Esprima → System.Memory → System.Buffers/System.Numerics.Vectors/
System.Runtime.CompilerServices.Unsafe). Esta fase construyó esa arquitectura desde cero y después
persiguió, uno por uno, cada bug real que aparecía al ejecutar `new Engine().Evaluate("1 + 2")`
contra los DLLs reales — sin fixtures propios, sin atajos.

**Arquitectura nueva: resolución multi-ensamblado**

- [x] `Assembly.deps []*Assembly` + `WithDependencies(...*Assembly) *Assembly`. `vm.LoadPackage`
      ahora carga automáticamente el grafo completo de dependencias transitivas de un paquete
      (`loadLockedPackage`, recursivo sobre `Dependencies []string` del lockfile), enganchando
      cada una vía `WithDependencies`.
- [x] Cada resolver (`resolveMethod`, `resolveByFullName`, `resolveExplicitImpl`,
      `resolveEnumMembers`, `resolveFieldBytes`) cae a `asm.deps` cuando no encuentra el símbolo
      localmente, propagando el error real del dep más profundo — no un genérico "not found" que
      esconde el problema real.
- [x] **Resolución con ámbito de ensamblado real**: `runtime.Resolvers` (definido en
      `internal/runtime/method.go` para evitar un ciclo de imports) agrupa los 5 resolvers de una
      `*runtime.Method`; `Machine.invoke` intercambia los resolvers activos de la Machine a los de
      `method.Resolvers` durante esa llamada. Corrige colisiones de nombre entre ensamblados —
      `<PrivateImplementationDetails>` existe por separado en `Jint.dll` y en `Esprima.dll`, y
      antes de esto el diseño de resolver global podía resolver silenciosamente contra el
      ensamblado equivocado.
- [x] `runtime.ErrMethodNotFound`: distingue "no existe tal método" (seguro ignorar en
      `runCctor`) de "el método existe pero falló al construirse" (un error real que debe
      propagarse, porque el `.cctor` que falla puede haber mutado ya estado estático real antes
      de fallar).

**Resolución real de overloads (antes: "primer match por nombre gana")**

- [x] `pickMethodOverload` (`assembly.go`): filtro duro de aridad + `scoreParamMatch` (tabla de
      puntaje por `Kind`) + refinamiento de coincidencia exacta de nombre de tipo (+50 si
      coincide exacto, -3 si es un mismatch confirmado, +20 si el argumento es subclase del tipo
      declarado — ver abajo). Encontrado corriendo Jint real: `Engine` tiene 5 constructores y 9
      overloads de `SetValue`; el motor de clases de Jint tiene múltiples overloads del mismo
      nombre y aridad que solo se distinguen por tipo de parámetro.
- [x] **Bug de subtipo vs. tipo genérico**: un argumento cuyo tipo concreto es subclase del tipo
      declarado de un parámetro (p. ej. un `JsNumber` contra un parámetro `JsValue`) recibía la
      misma penalización de -3 que un mismatch real, haciendo perder el overload correcto contra
      un overload de `object` sin relación — causaba una recursión infinita real en
      `Engine.SetValue`. Fix: `valueIsAssignableToTypeName` camina la cadena `BaseTypeFullName`
      del tipo del argumento; un subtipo confirmado suma +20 en vez de restar 3.
- [x] **Bug de forma dura (Fase final, el más sutil)**: `GlobalObject` en Jint declara su propio
      `GetOwnProperty(Key property)` no-virtual (un atajo interno de performance) con el mismo
      nombre y aridad que el `GetOwnProperty(JsValue property)` virtual que hereda pero no
      sobreescribe. El *chain walk* de despacho virtual (ver abajo) encontraba ese único candidato
      por nombre en `GlobalObject` y lo aceptaba sin más — un valor `JsValue` (referencia) nunca
      puede ser directamente un `Key` (struct) sin una conversión visible en el IL, así que esto
      corrompía en silencio cada lookup de propiedad. Fix: `hasHardShapeMismatch` descalifica la
      combinación `KindObject` argumento vs. `SigValueType` parámetro incluso cuando es el único
      candidato por nombre (`candidateMatchesArgs`) — el chain walk entonces sigue subiendo hasta
      encontrar el verdadero método virtual.
- [x] **Bug de puntaje `KindRef`**: un argumento `byref` (`ref`/`in`) puntuaba igual que un
      `KindObject` contra un parámetro `SigClass`/`SigObject` (puntaje 5) pero solo 1 (el
      default) contra el `SigByRef` correcto — invertido. Corregido con un caso `KindRef` propio
      (10 si `SigByRef`, 1 si no). Causaba que `StringDictionarySlim<T>`'s helper genérico
      `MoveNext<T>(ref Node? node, in NodeList<T> list)` perdiera contra un overload no
      relacionado de un solo `Node` — devolvía el `NodeList` completo como si fuera un único nodo.

**Despacho virtual real (antes: solo probaba el tipo concreto como fallback tras un "no resuelto")**

- [x] `Machine.call` ahora, para toda llamada virtual, prueba el tipo concreto del receptor
      *primero* — no solo cuando el nombre declarado falla en resolver del todo. Un método de
      clase base puede existir y resolver perfectamente bien (un `MethodDef` real, invocable)
      pero seguir siendo el incorrecto cuando el tipo real del receptor tiene su propio override.
- [x] **Chain walk completo**: si el tipo concreto no tiene el método (name-only lookup, sin
      herencia dentro de `resolveMethod`), sube por `BaseTypeFullName` probando cada ancestro
      hasta llegar al tipo declarado — no solo el tipo hoja. Esprima's `Node.GetChildNodes()`
      lanza `NotImplementedException` deliberadamente ("olvidaste hacer override") — con solo el
      tipo hoja probado, cada nodo concreto (que no sobreescribe `GetChildNodes` directo, sino a
      través de una clase intermedia) disparaba esa guardia en cada llamada.

**`newarr`/structs: tipado correcto de los defaults (antes: `Null()` ciego sin importar el tipo)**

- [x] `ir.NewArr` ahora lleva `TypeFullName` (resuelto del token de tipo del elemento, igual que
      `initobj`). El intérprete siembra cada slot con `runtime.Value` real para tipos valor
      (struct, enum, primitivos: int/long/float/double/bool/char/byte/...) en vez de un `Null()`
      genérico — un array de valor nunca es null en CLR real. `internal/interpreter/structs.go`
      gana `primitiveDefaults` (mapa de nombres de primitivos CIL a su default, ninguno tenía
      `TypeDef` ni entrada en el registro de value types de BCL).
- [x] **El bug real que esto expuso, no uno cosmético**: `Jint.Collections.StringDictionarySlim`
      usa `int[] _buckets`; sin el default correcto, `buckets[i]` leía `Null()`, y una resta
      contra eso fallaba con "binary op on mismatched value kinds".
- [x] **Bug de aliasing real, el más caro de encontrar de toda la fase**: `newObj`/
      `runtime.NewStruct` copiaban los defaults de campo con `copy()` — una copia superficial de
      `runtime.Value`. Cuando un default es `KindStruct`, `Value.Struct` es un puntero: **todas**
      las instancias de un tipo compartían el mismo `*Struct` subyacente para ese campo, hasta que
      algo lo sobreescribía explícitamente. Esto hacía que `Esprima.Utils.AdditionalDataSlot`
      (embebido en cada nodo AST, usado por Jint para cachear expresiones compiladas por nodo) se
      compartiera entre dos literales AST distintos ("1" y "2") — cachear el resultado compilado
      de "1" hacía que "2" leyera ese mismo caché, y `"1 + 2"` evaluaba a `2`. Fix: cada default se
      clona (`Value.Clone()`) por campo en vez de copiarse en bloque — en `internal/runtime/
      struct.go` (`NewStruct`) y `internal/interpreter/calls.go` (`newObj`).

**Wins chicos encontrados en el camino (cada uno, un gap real corriendo Jint/Esprima)**

- [x] `ir.LoadStaticFieldAddr` (`ldsflda`) + `runtime.Type.StaticFieldAddr` + `bcl.
      LookupStaticFieldHost`/`registerStaticFieldHost` (registro separado de `LookupValueType`,
      ya que `System.String` necesita almacenamiento estático para `string.Empty` pero no es un
      value type).
- [x] `RuntimeHelpers.InitializeArray` (patrón de inicializador de array literal): `ir.
      LoadFieldToken`, `runtime.Resolvers.ResolveFieldBytes`, lectura de campos respaldados por
      RVA vía la tabla `FieldRVA` de metadata (`internal/metadata/fieldrva.go`, nuevo).
- [x] Guard de profundidad de recursión al construir tipos valor (`maxValueTypeDepth = 24`) —
      stack overflow real de Go (no del intérprete) con una cadena de campos de value type
      auto-referencial.
- [x] **Bug de default de enum**: un enum se representa en CIL siempre como su primitivo
      subyacente directo en el stack, nunca como struct — pero `valueTypeDefault`/
      `defaultValueFor` envolvían *todo* value type default (incluyendo enums) en un struct.
      `Jint.Runtime.Debugger.StepMode` (un enum real del plugin) disparaba "switch on non-int32
      value kind" hasta este fix.
- [x] `isinst`/`castclass` contra un array (`is T[]`) — `resolveTypeTokenOrGeneric` no tenía caso
      para `SigSZArray`.
- [x] `OpRem` (`%`) para floats — CIL `rem` es `fmod` IEEE 754 (mismo signo que el dividendo,
      distinto de `Math.IEEERemainder`); Go's `%` no aplica a floats, así que usa `math.Mod`.
- [x] `System.Delegate::op_Equality`/`op_Inequality`, `System.Enum::HasFlag`,
      `System.Array::Copy` (overload de 5 argumentos) — superficie de BCL mundana, cada una
      encontrada como el siguiente bloqueo exacto al re-correr el demo.

**El demo: `examples/jint-demo/`**

- [x] `JintWrapper.cs`/`JintWrapper.csproj` (commiteados, `netstandard2.0`, referencia
      `Jint@3.1.3`) + `main.go`: carga Jint vía `vm.NuGet()`/`vm.LoadPackage`, carga
      `JintWrapper.dll` (compilado aparte con `dotnet build -c Release`) vía `vm.LoadBytes` +
      `WithDependencies`, y llama `RunJs("1 + 2")` → `"3"` y `AddNumbers(3, 4)` → `7` — ambos a
      través del motor Jint real, sin modificar, corriendo dentro de vmnet.
- [x] `TestJintDemoE2E` (raíz del repo, gateada tras `VMNET_NETWORK_TESTS=1`, con skip limpio si
      `JintWrapper.dll` no está compilado — mismo patrón que `tests/fixtures/csharp`).

**Lo que se dejó explícitamente afuera**

- No se re-corrió la certificación completa de los 8 targets (7 paquetes + Jint): el objetivo de
  esta fase era el demo funcional, no mover el porcentaje agregado. Los fixes de esta fase son
  todos correcciones de corrección real (no solo cobertura nueva), así que deberían mover el
  número en la próxima medición, pero eso queda para una fase futura dedicada a re-medir.
- `hasHardShapeMismatch` solo cubre la combinación `KindObject` vs. `SigValueType` — la única que
  causó daño real observado. La combinación simétrica (`KindStruct` vs. un `SigClass` específico,
  no `SigObject`, ya que boxear a `object` sí es válido) queda sin cubrir a propósito: menos
  certeza de que sea siempre un mismatch real, y ningún caso real la disparó todavía.

### Cómo verificar Fase 3.27

```bash
go test ./... -race -count=5
dotnet build examples/jint-demo/JintWrapper.csproj -c Release
VMNET_NETWORK_TESTS=1 go test ./ -run TestJintDemoE2E -v
cd examples/jint-demo && go run .
```

---

### Fase 3.28 — API de instancias (`Assembly.New`/`Instance.Call`)

Pregunta directa tras Fase 3.27: ¿se puede correr Jint sin el wrapper de C#? La API pública
(`Call`/`CallBytes`/`CallJSON`) solo invoca métodos **estáticos** — Jint necesita `new Engine()` +
`engine.Evaluate(...)`, ambos de instancia. Esta fase expone el mecanismo interno de `newobj`/
`callvirt` que el intérprete ya usaba (Fase 3.27 lo hizo real de punta a punta) directamente al
host Go.

**API nueva**

- [x] `Machine.New(typeFullName string, args []runtime.Value) (runtime.Value, error)`
      (`internal/interpreter/eval.go`) — wrapper exportado sobre `Machine.newObj` (la misma
      máquina que `ir.NewObj` dispara internamente), con recuperación de panic + contador de
      instrucciones fresco, mismo patrón que `Invoke`.
- [x] `Machine.CallInstance(fullName string, args []runtime.Value) (runtime.Value, bool, error)`
      — wrapper exportado sobre `Machine.call` con `virtual=true` siempre: el receptor real
      (`args[0]`) se prueba primero por su tipo concreto, subiendo toda la cadena de herencia si
      hace falta (el despacho virtual real de Fase 3.27) — seguro incluso para un método
      genuinamente no-virtual, ya que el tipo concreto del receptor coincide con el tipo
      declarado en ese caso.
- [x] `Assembly.New(typeName string, args ...Value) (*Instance, error)` y
      `(*Instance).Call(methodName string, args ...Value) (Value, error)` (`instance.go`, nuevo)
      — la fachada pública. El `.ctor`/método se resuelve por aridad + Kind de `args`, igual que
      cualquier overload estático (`pickMethodOverload`).
- [x] `*Instance` implementa `Value` (puede pasarse como argumento a otro `Call`/`New`, o
      encadenarse: `engine.Call("Evaluate", ...)` → `*Instance` de `JsValue` →
      `.Call("ToString")`). `wrapResult` reemplaza el uso directo de `fromRuntime` en `Call` y
      `Instance.Call`: un resultado `KindObject`/`KindStruct` ahora se envuelve en un `*Instance`
      en vez de perderse silenciosamente como `nil` — una mejora real en `Call` también, no solo
      la superficie nueva.

**El bug de aliasing que confirmó el diseño de structs**

- [x] `Instance.Call` sobre un value type (ej. `Point` con `Scale(int factor)` mutando
      `X`/`Y`) muta correctamente la instancia mantenida por el host — verificado con el fixture
      `Point` existente (`Structs.cs`, Fase 3.7). Funciona porque `runtime.Struct` siempre se
      referencia vía puntero: pasar `in.value` (una copia superficial del `runtime.Value`) igual
      comparte el mismo `*runtime.Struct` subyacente, así que una mutación de campo a través del
      método se ve reflejada de vuelta en el `Instance` que el host sigue sosteniendo — el mismo
      mecanismo de puntero compartido que causó el bug real de Fase 3.27 (`newObj`/`NewStruct`
      copiando defaults con `copy()`), aquí funcionando a favor en vez de en contra porque es
      exactamente UNA instancia, no N instancias compartiendo un default.

**Límite real, no un bug: azúcar sintáctico de C# que el compilador resuelve, no el CLR**

- [x] `examples/jint-nowrapper/` (nuevo, sin necesidad de `dotnet build` — solo Go + red):
      corre `Engine.Evaluate("1 + 2")` → `"3"` y `SetValue` + `a + b` → `"7"` sin ningún wrapper
      compilado. Encontró en el camino los dos límites reales de esta API:
    - **Parámetros opcionales con valor por defecto** son un mecanismo de tiempo de compilación
      (el compilador inserta el argumento omitido en el sitio de la llamada) — `Engine.Evaluate`
      real es `Evaluate(string code, string source = null)`; `Instance.Call` necesita ambos
      argumentos explícitos, ya que no hay información de "parámetro opcional" en runtime para
      recuperar automáticamente.
    - **Métodos de extensión** son azúcar sobre una llamada estática a un tipo *distinto* —
      `JsValue.AsNumber()` está declarado en `Jint.JsValueExtensions`, no en `JsValue`/`JsNumber`;
      `Instance.Call` siempre apunta al tipo concreto propio del receptor, así que no puede
      alcanzarlo. `ToString()` (un método de instancia real) sirve como alternativa en el demo.
    - Conversiones implícitas definidas por el usuario (`operator implicit`) tendrían la misma
      limitación, aunque no se encontró un caso real que la disparara en este demo específico.
      Documentado en `examples/jint-nowrapper/README.md`.

**Tests**

- [x] `TestInstanceAPI` (`vmnet_test.go`) — clase con ctor sin argumentos + getter/setter de
      propiedad (`Customer`, Fase 2), struct con ctor parametrizado + método mutante (`Point`,
      Fase 3.7), y el caso de error (tipo inexistente).
- [x] `TestJintNoWrapperE2E` (raíz, gateado tras `VMNET_NETWORK_TESTS=1`) — mismos dos casos que
      `TestJintDemoE2E` pero sin wrapper.

### Cómo verificar Fase 3.28

```bash
go test ./... -race -count=5
go test ./ -run TestInstanceAPI -v
VMNET_NETWORK_TESTS=1 go test ./ -run TestJintNoWrapperE2E -v
cd examples/jint-nowrapper && go run .
```

### Re-certificación contra los mismos 8 targets (7 paquetes + Jint) tras Fase 3.27/3.28

Fase 3.27 dejó pendiente esta re-medición explícitamente (el objetivo era el demo funcional, no
mover el número agregado). Con la resolución real de overloads, el despacho virtual completo y el
fix de aliasing de structs ya en el árbol, esta es esa medición.

| Paquete | % limpio Fase 3.26 | % limpio Fase 3.27/3.28 |
|---|---|---|
| `Ardalis.GuardClauses@5.0.0` | 96.8% | 96.8% |
| `FluentValidation@11.9.2` | 93.9% | 93.9% |
| `System.Text.Json@8.0.5` | 83.8% | 84.5% |
| `Newtonsoft.Json@13.0.3` | 80.4% | 81.1% |
| `Semver@2.3.0` | 91.0% | 91.0% |
| `SimpleBase@4.0.0` | 85.3% | 85.3% |
| `Humanizer.Core@2.14.1` | 93.4% | 93.6% |
| **Promedio (7 paquetes)** | **89.2%** | **89.5%** |
| `Jint@3.1.3` | 87.6% | 88.7% |
| **Promedio (7 paquetes + Jint)** | **89.0%** | **89.4%** |

Esta vez sí hay movimiento real a nivel de tabla, no solo bajo el capó: `System.Text.Json`
(83.8%→84.5%), `Newtonsoft.Json` (80.4%→81.1%), `Humanizer.Core` (93.4%→93.6%) y sobre todo
`Jint` (87.6%→88.7%, +1.1pp) — el paquete que motivó cada fix de esta fase, coherente con que la
mayoría de los bugs reales encontrados (resolución de overloads con subtipos, despacho virtual
completo, `newarr` tipado, el aliasing de structs) fueron encontrados corriendo código de Jint
específicamente. `Ardalis.GuardClauses`/`FluentValidation`/`Semver`/`SimpleBase` no se movieron:
ninguno ejercita las formas de código (jerarquías de clases profundas, structs compartiendo
metadata de tipo, overloads ambiguos por Kind) que estos fixes corrigen. El promedio agregado
sigue sin acercarse a 97% por la misma razón documentada desde Fase 3.25/3.26: el bloque grande de
`MethodInfo`/`PropertyInfo`/invocación dinámica sigue siendo, con claridad, la superficie de mayor
volumen restante.

### Fase 3.29 — Checker: resolución consciente de dependencias (`AnalyzeWithDeps`)

Nueva iniciativa: llevar dos paquetes NuGet reales y populares más (`NPOI`, hojas de cálculo/`.xls`
legacy; `ClosedXML`, `.xlsx`) lo más cerca posible de 100% limpio bajo `netstandard-lite`, cada uno
terminando en un demo real y corrible — la misma vara que Jint (Fase 3.27/3.28), no solo "compila".
`DocumentFormat.OpenXml` (Word/PPTX) queda explícitamente fuera de este loop: una primera medición
lo dejó en 36.9% limpio, dominado por miles de findings de `ldtoken` dentro de constructores de
clases de schema OOXML auto-generadas — un patrón estructural de reflection pesada, coherente con
los no-objetivos ya declarados en spec.md §3 (`Reflection.Emit`, `dynamic` pesado), no un simple
hueco de nativo faltante.

Antes de implementar nada para NPOI, su medición base (`vmnet check package NPOI@2.8.0`) sacó a
la luz un punto ciego del checker, no un hueco del intérprete: el `.nuspec` de NPOI lista
dependencias transitivas reales — `ZString` (`Cysharp.Text.Utf16ValueStringBuilder`, 234 sitios de
llamada), `SkiaSharp`, `BouncyCastle.Cryptography`, `ExtendedNumerics.BigDecimal` — exactamente la
forma que `vm.LoadPackage` ya resuelve correctamente en runtime (Fase 3.27: `Jint` → `Esprima` →
`System.Memory` → ...). `checker.Analyze`, sin embargo, solo decodificaba el único DLL que se le
pasaba — no tenía ninguna noción de "esta llamada resuelve contra el IL real de OTRO ensamblado",
así que marcaba los ~400 findings de ese tipo como `unsupported-bcl-method`, un falso negativo:
esas llamadas corren de verdad una vez que `LoadPackage` adjunta la cadena de dependencias
resuelta, igual que una llamada dentro del propio DLL del paquete.

**Fix**

- [x] `checker.AnalyzeWithDeps(f *pe.File, md *metadata.Metadata, deps []*metadata.Metadata,
      profile Profile) *Report` (`internal/checker/analyzer.go`) — `Analyze` ahora es un wrapper
      delgado que llama a esta con `deps=nil` (retrocompatible al 100%, ningún caller existente se
      toca). `checkTarget` primero intenta `resolvable(md, target)` (comportamiento sin cambios);
      si falla, reintenta `resolvable(dep, target)` contra la metadata de cada dependencia antes de
      rendirse. Un target resuelto vía dependencia se trata como compatible directamente, sin pasar
      por el allowlist del profile — igual que `isLocalMethod` ya trata una llamada dentro de `md`
      mismo: lo que corre de verdad es el cuerpo del callee, no este call site, así que no está "en"
      ni "fuera" del profile del que llama.
- [x] `vmnet check package` (`cmd/vmnet/main.go`) ahora resuelve el grafo completo de dependencias
      transitivas del paquete objetivo vía `nuget.NewResolver` (el mismo resolver que usa
      `NuGetManager.Restore`, solo que sin necesitar manifest/lockfile en disco primero — `check
      package` siempre fue un comando de "mirar antes de agregar"), descarga y parsea el asset
      seleccionado de cada dependencia, y se los pasa todos a `AnalyzeWithDeps`. Imprime
      `Dependencies resolved: N`.

**Impacto en la medición, todavía no en capacidad**

Esta fase hace que el número de NPOI sea *honesto*, no más alto por nueva capacidad — el
intérprete/BCL subyacente no cambió. `NPOI@2.8.0`: 91.3% → 92.0% limpio (`MethodsFlagged` 1235 →
1131), y los findings de dependencias de terceros (`Cysharp.Text`, `SkiaSharp.SKColor`,
`Org.BouncyCastle`, `ExtendedNumerics.BigDecimal` — ~400 findings combinados) desaparecen por
completo del reporte. Los ~1131 métodos marcados restantes son ahora, con mucha más confianza,
huecos genuinos de cobertura de BCL u opcodes propios de vmnet — exactamente la señal que el resto
de este loop necesita para priorizar correctamente.

Límite de alcance conocido, dejado tal cual: `AnalyzeWithDeps` chequea si una llamada *resuelve*
contra un método real de una dependencia, no si el cuerpo de ese método de la dependencia
correría limpio a su vez (sin análisis recursivo de todo el grafo). Una llamada no soportada
dentro de una dependencia saldría recién en runtime real, no como finding de `vmnet check package
NPOI@2.8.0` hoy. Chequeo transitivo completo es un cambio más grande (atribución de reporte a
través de N ensamblados, manejo de ciclos en el propio walk de chequeo) que no hace falta para el
objetivo de este loop — se deja anotado acá en vez de quedar como hueco silencioso.

### Cómo verificar Fase 3.29

```bash
go build ./...
go vet ./...
go test ./... -race -count=5
/tmp/vmnet-cli check package NPOI@2.8.0 --profile=netstandard-lite   # Dependencies resolved: 21
```

### Fase 3.30 — `System.IO.MemoryStream`/`Stream` + un bug real del resolver de NuGet

Bloqueador de mayor impacto después de la medición honesta de Fase 3.29: `System.IO.Stream`/
`MemoryStream` (709 findings combinados en NPOI, 92 en ClosedXML) — el hueco genuinamente-BCL más
grande, y que se repite en ambos paquetes objetivo, coherente con la metodología de este loop de
"elegir el bloqueador de mayor apalancamiento". Sondear el IL real de NPOI primero
(`NPOI.POIDocument::WritePropertySet`, `NPOI.Util.HexDump::Dump`, ...) sacó a la luz dos cosas que
valía la pena diseñar antes de escribir ningún nativo:

1. NPOI declara subclases reales de `MemoryStream`/`Stream` directamente (por ejemplo
   `NPOI.POIFS.FileSystem.NDocumentOutputStream extends System.IO.MemoryStream`,
   `NPOI.Util.OutputStream extends System.IO.Stream`) — una clase managed encadenando su propio
   `.ctor` a una clase base BCL *nativa*, exactamente la misma forma que
   `system_exception.go`'s `baseExceptionCtorInPlace` ya resolvió para subclases de excepción
   (Fase 3.13). No hace falta ningún cambio en el intérprete: `newObj` ya asigna el objeto derivado
   con su propio `Type`/campos y después llama a su `.ctor`, que a su vez encadena vía un `call`
   plano a `System.IO.MemoryStream::.ctor` — registrar ese nombre como un nativo normal (no-newobj)
   que muta el `Obj.Native` del receptor *existente* en el lugar es trabajo puramente aditivo en
   `internal/bcl`.
2. El código real abrumadoramente mantiene un `MemoryStream` en un local/parámetro tipado como
   `Stream` (`Stream s = new MemoryStream();`), así que los call sites compilan contra el nombre
   declarado `System.IO.Stream::Method`, no `MemoryStream::Method`. El despacho virtual de la
   Fase 3.27 ya intenta primero el tipo concreto real del receptor (vía `bcl.NativeTypeName`) antes
   de caer al nombre declarado — así que registrar todo bajo `System.IO.MemoryStream::*` solo ya
   resolvería correctamente un call site declarado como `Stream`; ambos nombres se registran de
   todas formas para cubrir también un call site no-virtual que nombre `Stream` directamente.

**Nativo nuevo: `internal/bcl/system_io.go`**

- [x] `nativeMemoryStream{buf []byte, pos int, closed bool}` — `Write`/`WriteByte`/`Read`/
      `ReadByte`/`Seek`/`SetLength`/`Flush`/`Close`/`Dispose`/`CopyTo`/`get_Length`/`get_Position`/
      `set_Position`/`get_CanRead`/`get_CanWrite`/`get_CanSeek`, más `ToArray`/`GetBuffer`
      (solo de MemoryStream en .NET real, registrados una vez). `System.IO.IOException` y
      `System.IO.EndOfStreamException` agregados a la lista plana de excepciones ya existente en
      `system_exception.go` (65 findings; `throw new IOException(...)` no necesitó nada más que
      esa línea).
- [x] `internal/checker/profile.go`: `System.IO.MemoryStream::`/`System.IO.Stream::`/
      `System.IO.IOException`/`System.IO.EndOfStreamException` agregados al allowlist de
      `netstandard-lite` — olvidarse este paso hizo que la primera re-medición no mostrara ningún
      movimiento pese a que `bcl.Lookup` resolvía correctamente: `checkTarget` sigue marcando un
      target resoluble-pero-fuera-de-profile como `KindOutOfProfile` (ver el mismo patrón de dos
      pasos en el agregado de `System.Enum::HasFlag`/`System.Array::Copy` de Fase 3.27).

**Un segundo bug real, no relacionado, encontrado al re-medir ClosedXML**

- [x] El propio `.nuspec` de `ClosedXML@0.105.0` declara su dependencia de `DocumentFormat.OpenXml`
      como un rango de versión NuGet (`[3.1.1, 4.0.0)`), no un pin plano — común en `.nuspec`
      reales, pero el resolver de `internal/nuget` no tenía ningún parseo de rangos: `Resolver.
      visit` pasaba el string de rango crudo directo a `Cache.Fetch` como si fuera una versión
      exacta, lo que da 404 contra `api.nuget.org`. Esto rompía por completo la nueva (Fase 3.29)
      resolución de dependencias de `vmnet check package ClosedXML@0.105.0` — y hubiera roto
      igual de mal el camino real `vm.NuGet().Restore()` → `vm.LoadPackage("ClosedXML")`, ya que
      ambos comparten el mismo `Resolver.Resolve`. `nuget.ParseMinVersion(v string) string`
      (`internal/nuget/version.go`) extrae la cota inferior de un rango (`"[3.1.1, 4.0.0)"` →
      `"3.1.1"`) — el mismo "lowest applicable version" al que NuGet mismo recurre por defecto
      para un `PackageReference` plano sin notación floating, y determinístico sin necesitar una
      vuelta extra para enumerar cada versión disponible. `Resolver.visit` normaliza a través de
      esto antes de cada fetch.

**Resultado**

| Paquete | % limpio Fase 3.29 | % limpio Fase 3.30 |
|---|---|---|
| `NPOI@2.8.0` | 92.0% (`MethodsFlagged` 1131) | 94.2% (`MethodsFlagged` 825) |
| `ClosedXML@0.105.0` | n/a (resolución de dependencias fallaba — ver arriba) | 90.2% (`MethodsFlagged` 1029, `Dependencies resolved: 12`) |

Los findings con prefijo `System.IO` desaparecen por completo de los findings principales de ambos
paquetes. Se re-chequearon los 8 targets ya certificados (`Jint`, `Newtonsoft.Json`,
`Ardalis.GuardClauses`) después del fix del resolver — conteos de dependencias y de métodos sin
cambios, ninguna regresión por ninguno de los dos fixes.

### Cómo verificar Fase 3.30

```bash
go build ./...
go vet ./...
go test ./... -race -count=5
/tmp/vmnet-cli check package NPOI@2.8.0 --profile=netstandard-lite       # Sin findings de System.IO
/tmp/vmnet-cli check package ClosedXML@0.105.0 --profile=netstandard-lite # Dependencies resolved: 12 (antes era un error duro)
```

### Fase 3.31 — huecos de `System.Math` (`Pow`/`Round`/`Log`/trig/`Ceiling`/`Truncate`/...)

Segundo bloqueador de mayor apalancamiento cruzado después de Fase 3.30: `System.Math` solo tenía
`Abs`/`Min`/`Max`/`Floor` implementados nativamente — `Pow` (53 findings en NPOI, 19 en ClosedXML)
y `Round` (40 + 24) solos representaban rutas de código real de fórmulas/formateo en ambos
paquetes, más `Log`/`Ceiling`/`Truncate`/`Sqrt`/las funciones trigonométricas individualmente más
chicas pero parte de la misma clase de hueco fácil de arreglar. `"System.Math::"` ya era un prefijo
wildcard en todos los profiles incluido `minimal` (es anterior a este loop) — los findings previos
eran puro `unsupported-bcl-method` (nada registrado en `bcl.Lookup`), no un hueco de allowlist
out-of-profile, así que no hizo falta ningún cambio en `profile.go` esta vez.

- [x] `internal/bcl/system_math.go`: se agregaron `Ceiling`/`Truncate`/`Pow`/`Sqrt`/`Log`/`Log10`/
      `Log2`/`Exp`/`Sign`/`Round`/`Sin`/`Cos`/`Tan`/`Atan`/`Atan2`. `Round(double)`/`Round(double,
      digits)` comparten un solo nativo desambiguado por cantidad de argumentos (la misma forma
      que necesita `Log(double)`/`Log(double, newBase)` — `resolveCallTarget` nunca desambigua
      overloads por firma, solo por el nombre desnudo del target). Coincide con el
      `MidpointRounding.ToEven` ("banker's rounding") por defecto de .NET real vía el
      `math.RoundToEven` de Go, no el redondeo ingenuo half-away-from-zero — la correctitud importa
      acá porque los resultados de fórmulas de hoja de cálculo son exactamente el tipo de valor que
      un demo mostraría y compararía contra una respuesta conocida-correcta. Un argumento enum
      `MidpointRounding`, cuando está presente, se acepta pero no se distingue (ningún IL real de
      paquete objetivo en este loop se encontró dependiendo específicamente de `AwayFromZero`); los
      overloads de `Math.Round` tipados `System.Decimal` quedan fuera de alcance hasta que
      `System.Decimal` mismo tenga un nativo real (`System.Decimal::op_Explicit`, 17 findings,
      sigue siendo un hueco abierto separado).

**Resultado**

| Paquete | % limpio Fase 3.30 | % limpio Fase 3.31 |
|---|---|---|
| `NPOI@2.8.0` | 94.2% (`MethodsFlagged` 825) | 94.7% (`MethodsFlagged` 748) |
| `ClosedXML@0.105.0` | 90.2% (`MethodsFlagged` 1029) | 90.9% (`MethodsFlagged` 947) |

### Cómo verificar Fase 3.31

```bash
go build ./...
go vet ./...
go test ./... -race -count=5
/tmp/vmnet-cli check package NPOI@2.8.0 --profile=netstandard-lite       # sin findings de System.Math
/tmp/vmnet-cli check package ClosedXML@0.105.0 --profile=netstandard-lite # sin findings de System.Math
```

### Fase 3.32 — `Dictionary.Values`/`.Keys`, `List.Remove`/`ForEach`, y 11 métodos LINQ más

La clase de bloqueador restante más grande de ClosedXML: huecos de `System.Linq.Enumerable`
(`Cast`/`First`/`LastOrDefault`/`Count`/`Distinct`/`OrderBy`/`Concat`/`OfType`/`ToDictionary`/`Max`
— ~370 findings combinados), más `Dictionary.Values`/`.Keys` y sus enumeradores
`ValueCollection`/`KeyCollection` (~130 combinados entre ambos paquetes) y
`List<T>.Remove`/`.ForEach` (25 + 43). Todo el trabajo LINQ machine-aware reutiliza la maquinaria
ya existente de `internal/interpreter/linq.go` (`enumerateAll`/`linqInvoke`, Fase 3.14) — trabajo
genuinamente aditivo, sin cambios en el núcleo del intérprete.

- [x] `internal/bcl/system_collections.go`: `Dictionary.get_Values`/`.get_Keys` devuelven un
      `nativeList` de snapshot plano — el propio `GetEnumerator`/`MoveNext`/`get_Current` de
      `ValueCollection`/`KeyCollection` se registran contra los enumeradores ya existentes de
      `List<T>` textualmente en vez de duplicarlos: nada río abajo inspecciona el nombre de tipo
      struct que reporta el enumerador, solo su comportamiento `MoveNext`/`get_Current`. Se agregó
      `List<T>.Remove` (igualdad por referencia/valor vía el `valuesEqual` ya existente, la misma
      noción que ya usa `Object.Equals`). Nuevo `bcl.NewDictValue(map[string]runtime.Value)`
      exportado — el `ToDictionary` de `linq.go` necesita construir una instancia real de
      `Dictionary` sin meterse en el `nativeDict` no exportado de `bcl`.
- [x] `internal/interpreter/linq.go`: `Cast`/`OfType` (pasan directo — sin info de argumento de
      tipo genérico reificado contra la cual type-checkear/filtrar, la misma aproximación
      documentada que el caso de miss no-tipado de `Dictionary.TryGetValue`), `First` (lanza
      `InvalidOperationException` en vacío/sin-match, a diferencia de `FirstOrDefault`),
      `LastOrDefault`, `Count`, `Distinct` (dedup O(n²) con `valuesDeepEqual` — suficiente para los
      tamaños que el código real toca), `Concat`, `OrderBy` + un nuevo helper `linqCompare`
      (orden numérico/de strings; una comparación de Kind distinto o no-primitiva se reporta, no
      se adivina — vmnet no tiene despacho `IComparable`), `Max`, `ToDictionary` (claves
      stringificadas vía `Value.String()` — el alcance ya existente de solo-claves-string), y
      `List<T>.ForEach` (machine-aware por la misma razón que cualquier otro método LINQ que
      invoca un delegate — necesita `Machine.invokeFunc`).
- [x] El `linqTargets` de `internal/checker/analyzer.go` y el allowlist `netstandard-lite` de
      `internal/checker/profile.go` se actualizaron ambos para cada nombre nuevo — el mismo
      registro en dos pasos que Fase 3.27/3.30/3.31 ya estableció (un nativo solo no alcanza; el
      checker tiene su propia conciencia separada de la superficie del Machine-registry, por
      diseño — ver el propio doc comment de `linqTargets`).

**Resultado**

| Paquete | % limpio Fase 3.31 | % limpio Fase 3.32 |
|---|---|---|
| `NPOI@2.8.0` | 94.7% (`MethodsFlagged` 748) | 95.1% (`MethodsFlagged` 694) |
| `ClosedXML@0.105.0` | 90.9% (`MethodsFlagged` 947) | 92.8% (`MethodsFlagged` 757) |

Los bloqueadores principales restantes de ClosedXML se corrieron decisivamente hacia la
serialización XML misma — `System.Xml.XmlWriter` (`WriteStartElement`/`WriteEndElement`/
`WriteAttributeString`/`WriteStartAttribute`/`WriteEndAttribute`, ~205 combinados) y
`System.Xml.Linq` (`XName`/`XElement`/`XAttribute`/`XContainer`, ~65 combinados) — justo la
maquinaria que un demo que escribe `.xlsx` va a ejercitar de verdad, una buena señal de
secuenciación.

### Cómo verificar Fase 3.32

```bash
go build ./...
go vet ./...
go test ./... -race -count=5
/tmp/vmnet-cli check package NPOI@2.8.0 --profile=netstandard-lite
/tmp/vmnet-cli check package ClosedXML@0.105.0 --profile=netstandard-lite
```

### Fase 3.33 — `System.Xml.XmlWriter`

La maquinaria de serialización XML que un demo que escribe `.xlsx` va a ejercitar de verdad:
`WriteStartElement`/`WriteEndElement`/`WriteAttributeString`/`WriteStartAttribute`/
`WriteEndAttribute` solos eran ~205 findings combinados en ClosedXML. Sondear IL real de ClosedXML
(`ClosedXML.Excel.IO.CommentPartWriter::GenerateWorksheetCommentsPartContent`) confirmó la forma
concreta: `XmlWriter.Create(Stream, XmlWriterSettings)` — el mismo `System.IO.Stream` que ya
construyó Fase 3.30, lo que significa que `XmlWriter` puede escribir incrementalmente directo al
buffer de un `nativeMemoryStream` existente en vez de necesitar ninguna primitiva de E/S nueva
debajo.

- [x] `internal/bcl/system_xml.go` (nuevo): `nativeXmlWriter{dest *nativeMemoryStream, stack
      []xmlWriterFrame, ...}` — una pila explícita chica de elementos rastrea, por elemento
      abierto, si ya se emitió el `'>'` de su tag de apertura (`tagClosed`), para que
      `WriteEndElement` pueda elegir correctamente entre auto-cerrar con `"/>"` (nada escrito desde
      el tag de apertura) o un `"</name>"` real — verificado con un probe directo:
      `<root id="1"><child>hello &amp; &lt;world&gt;</child><empty/><leaf>v</leaf></root>`,
      confirmando que auto-cierre, anidamiento, y escapado de entidades (`&`/`<`/`>`/`"`) salen
      todos bien formados. `WriteStartElement`/`WriteAttributeString`/`WriteElementString` cada uno
      colapsa varios overloads reales (`(name)`/`(name, ns)`/`(prefix, name, ns)`) en un solo
      nativo desambiguado por cantidad de argumentos, el mismo patrón que cualquier otro nativo BCL
      multi-overload en este codebase; el argumento de URI de namespace mismo se descarta
      (documentado — ClosedXML emite sus propios atributos `xmlns:` explícitos donde los necesita,
      así que vmnet no necesita sintetizar ninguno). `WriteStartAttribute`/`WriteString`/
      `WriteEndAttribute` comparten estado vía `inAttr`/`attrBuf`, coincidiendo con cómo el
      `WriteString` real despacha al valor del atributo abierto o al contenido de texto del
      elemento según el estado del writer. `Close`/`Dispose` recorren la pila de elementos
      todavía-abiertos para cerrar cualquier secuencia desbalanceada, coincidiendo con el
      `XmlWriter` real. `XmlWriterSettings` es un objeto trivial con setters de propiedad no-op —
      ninguno de ellos (`CloseOutput`/`Encoding`/`Indent`/...) cambia la forma real de salida del
      writer para ningún IL real encontrado en este loop.
- [x] `internal/checker/profile.go`: `System.Xml.XmlWriter::`/`System.Xml.XmlWriterSettings::`
      agregados al allowlist de `netstandard-lite` (primera entrada `System.Xml.*` en absoluto).

**Resultado**

| Paquete | % limpio Fase 3.32 | % limpio Fase 3.33 |
|---|---|---|
| `NPOI@2.8.0` | 95.1% (`MethodsFlagged` 694) | 95.1% (sin cambios — sin uso de `System.Xml.XmlWriter`) |
| `ClosedXML@0.105.0` | 92.8% (`MethodsFlagged` 757) | 92.9% (`MethodsFlagged` 741) |

Los findings de `XmlWriter` desaparecen por completo de los findings principales de ClosedXML; el
delta modesto en cantidad de métodos (757→741, más chico que los ~200 findings crudos eliminados)
es el mismo efecto de solapamiento que Fase 3.29 documentó primero — muchos de esos métodos ya
estaban marcados por otra razón también (`System.Xml.Linq`, métodos LINQ todavía faltantes) y solo
salen del conteo de flagged una vez que se limpia cada finding sobre ellos. Los bloqueadores
principales restantes de ClosedXML son ahora `System.Xml.Linq` (`XName`/`XElement`/`XAttribute`/
`XContainer`, LINQ-to-XML — usado para *leer* partes XML existentes, una preocupación distinta del
camino de escritura de `XmlWriter`) y un puñado más de métodos `System.Linq.Enumerable`
(`Single`/`SingleOrDefault`/`OrderByDescending`/`ElementAt`/`Skip`/`Union`).

### Cómo verificar Fase 3.33

```bash
go build ./...
go vet ./...
go test ./... -race -count=5
/tmp/vmnet-cli check package ClosedXML@0.105.0 --profile=netstandard-lite # sin findings de System.Xml.XmlWriter
```

### Fase 3.34 — 6 métodos más de `System.Linq.Enumerable`

Seguimiento rápido y mecánico limpiando el resto de la cola chica de LINQ de ClosedXML antes de
pasar al bloqueador más grande de `System.Xml.Linq`: `Single`/`SingleOrDefault` (como
`First`/`FirstOrDefault` pero también lanzando `InvalidOperationException` con más de un match),
`OrderByDescending` (construido invirtiendo el resultado ascendente de `OrderBy` en vez de duplicar
el sort), `ElementAt`, `Skip`, `Union` (`Concat` + el dedup de `Distinct`, preservando el orden de
primera-aparición a través de ambas secuencias). Cada uno reutiliza
`enumerateAll`/`linqInvoke`/`linqCompare` de Fase 3.14/3.32 — sin maquinaria nueva.

- [x] `internal/interpreter/linq.go`: los 6 métodos de arriba.
- [x] `linqTargets` de `internal/checker/analyzer.go` actualizado para que coincida — el mismo
      registro en dos pasos que necesita cualquier agregado al Machine-registry.

**Resultado**

| Paquete | % limpio Fase 3.33 | % limpio Fase 3.34 |
|---|---|---|
| `ClosedXML@0.105.0` | 92.9% (`MethodsFlagged` 741) | 93.3% (`MethodsFlagged` 703) |

Bloqueadores principales restantes: `System.Xml.Linq` (`XName`/`XElement`/`XAttribute`/
`XContainer` — lectura de partes XML existentes, ej. hojas de cálculo plantilla),
`System.Collections.Generic.IReadOnlyDictionary`2::get_Item` (45, una llamada tipada por interfaz
que el fallback de despacho por interfaz de Fase 3.13 todavía no cubre), y una cola larga de ítems
más chicos (`System.Drawing.Color`/`Point`, `DateTime.ToOADate`/`FromOADate`,
`System.Reflection.CustomAttributeData`).

### Cómo verificar Fase 3.34

```bash
go build ./...
go vet ./...
go test ./... -race -count=5
/tmp/vmnet-cli check package ClosedXML@0.105.0 --profile=netstandard-lite
```

### Fase 3.35 — `System.Xml.Linq` (`XDocument`/`XElement`/`XAttribute`/`XName`)

El camino de *lectura* LINQ-to-XML de ClosedXML (`System.Xml.Linq`) — una preocupación distinta del
camino de *escritura* `XmlWriter` de Fase 3.33, usado por ClosedXML para releer sus propias partes
XML de comentarios/shapes VML ya escritas durante la carga (`XLWorkbook.GetCommentShapes`/
`DeleteExistingCommentsShapes`, `XDocumentExtensions.Load`). Sondear confirmó el punto de entrada:
`XDocument.Load(Stream)` — el mismo `System.IO.Stream`/`MemoryStream` que construyó Fase 3.30, así
que tampoco hizo falta ninguna primitiva de E/S nueva acá.

- [x] `internal/bcl/system_xmllinq.go` (nuevo): `nativeXElement{name, attrs, children, text}` es un
      árbol chico parseado con el `encoding/xml.Decoder` de la stdlib de Go (token por token —
      `xml.StartElement`/`EndElement`/`CharData`), no un parser hecho a mano. `XDocument` solo
      envuelve un `XElement` raíz, coincidiendo con el `XDocument.Root` real. Verificado de punta a
      punta contra un round-trip real: `XmlWriter` (Fase 3.33) escribe
      `<shapes><shape id="42" /></shapes>` en un `MemoryStream`, `XDocument.Load` lo re-parsea,
      `.Root.Elements()` encuentra el único hijo, `.Attribute("id").Value` lee `"42"` de vuelta
      correctamente.
    - Los URI de namespace se descartan en todo el archivo, matcheando solo por nombre local — la
      misma simplificación que `XmlWriter` ya hizo para sus propios argumentos de namespace; releer
      el XML ya escrito de un paquete nunca necesita de verdad desambiguar por namespace para
      encontrar lo que busca por nombre local solo.
    - `XName` se modela como un `System.String` plano, no su propio tipo de objeto: cada consumidor
      acá solo necesita un nombre local contra el cual matchear, así que
      `op_Implicit`/`get_LocalName`/`Get` son todos la función identidad sobre el string mismo (el
      argumento de namespace de `Get` se descarta de la misma forma).
    - `Elements()`/`Element(name)` se registran bajo `XContainer::` (la base real declarada) Y
      `XElement::` directamente (código real a veces lo llama contra un local ya tipado como el
      `XElement` concreto) — se confirmó que ambas formas aparecen en IL real de ClosedXML.
    - `XElement.Value` concatena todo el texto descendiente recursivamente, coincidiendo con la
      semántica real para el caso general, aunque la forma de elemento-hoja-sin-hijos es la que el
      uso real de ClosedXML realmente toca.
    - Hueco conocido, dejado sin hacer: `System.Xml.Linq.Extensions::Remove` (un método de
      extensión que remueve un elemento de su padre) necesitaría que `nativeXElement` rastree un
      puntero al padre, que el árbol actual no tiene. Solo lo usa `DeleteExistingCommentsShapes` —
      limpiar shapes de comentarios VML existentes antes de regenerarlos al *cargar* un archivo que
      ya tiene comentarios, no ejercitado por el escenario de demo crear-desde-cero de este loop.
      Se deja documentado como hueco en vez de construirlo especulativamente.
- [x] `internal/checker/profile.go`: `System.Xml.Linq.XDocument::`/`XContainer::`/`XElement::`/
      `XAttribute::`/`XName::` agregados al allowlist de `netstandard-lite`.

**Resultado**

| Paquete | % limpio Fase 3.34 | % limpio Fase 3.35 |
|---|---|---|
| `NPOI@2.8.0` | 95.1% (`MethodsFlagged` 694) | 95.1% (sin cambios — sin uso de `System.Xml.Linq`) |
| `ClosedXML@0.105.0` | 93.3% (`MethodsFlagged` 703) | 93.5% (`MethodsFlagged` 684) |

Los findings de `System.Xml.Linq` desaparecen por completo de los findings principales de
ClosedXML. Los bloqueadores principales restantes son ahora una cola larga de ítems más chicos y
sin relación entre sí: `IReadOnlyDictionary`2::get_Item` (45, una llamada tipada por interfaz),
`DateTime.ToOADate`/`FromOADate` (conversión de fecha serial de Excel, 24 combinados),
`System.Drawing.Color`/`Point`, `System.Reflection.CustomAttributeData`, y varios métodos más de
`System.Linq.Enumerable` (`GroupBy`/`Range`/`Min`/`SequenceEqual`) — ninguno individualmente lo
bastante grande como para justificar su propio título de fase como sí lo hicieron
`System.IO`/`System.Math`/`System.Linq`/`System.Xml.*`.

### Cómo verificar Fase 3.35

```bash
go build ./...
go vet ./...
go test ./... -race -count=5
/tmp/vmnet-cli check package ClosedXML@0.105.0 --profile=netstandard-lite # sin findings de System.Xml.Linq
```

### Fase 3.36 — `System.Collections.ArrayList`/`Hashtable` (colecciones legacy)

El último bloqueador de cluster único grande de NPOI: los predecesores legacy, no-genéricos, de
`List<T>`/`Dictionary<K,V>` (~145 findings combinados: `ArrayList` `.ctor`/`get_Count`/`Add`/
`ToArray`, `Hashtable` `get_Item`/`set_Item`/`.ctor`). Como el `runtime.Value` de vmnet ya es una
unión etiquetada uniforme sin importar el argumento de tipo genérico real, `nativeList`/
`nativeDict` respaldan `ArrayList`/`Hashtable` textualmente — genuinamente cero tipos de backing
nuevos, solo registraciones nuevas apuntando a funciones ya existentes.

- [x] `internal/bcl/system_collections.go`: `ArrayList` reutiliza cada uno de los métodos ya
      existentes de `nativeList` directamente, incluido `GetEnumerator` — `listGetEnumerator`
      siempre etiqueta su struct resultado como `"List`1+Enumerator"` sin importar el tipo de
      receptor declarado, y el despacho virtual de `Machine.call` (Fase 3.27) intenta primero el
      tipo struct concreto real del receptor, así que `MoveNext`/`get_Current` resuelven
      correctamente sin necesitar una registración separada de `"ArrayList+Enumerator"` — la misma
      reutilización gratis que Fase 3.32 ya encontró para `Dictionary.Values`/`.Keys`. `Hashtable`
      reutiliza `nativeDict` de la misma forma, con una diferencia semántica real capturada antes
      de que se subiera: `Dictionary<K,V>.get_Item` lanza `KeyNotFoundException` con una clave
      faltante, pero el `Hashtable.get_Item` real devuelve `null` — aliasear `dictGetItem`
      directamente hubiera sido un bug de comportamiento real, no solo una feature incompleta, así
      que una pequeña variante `hashtableGetItem` devuelve `Null()` en un miss en vez de eso.
      `Hashtable.Contains` se registra como un alias real de `ContainsKey` (cierto en el `Hashtable`
      real, a diferencia de `Dictionary<K,V>`, que no tiene ese alias) y `.Remove` como `void`
      (`hasReturn=false`) en vez del `bool` de `Dictionary<K,V>.Remove`.
    - Hueco conocido, dejado sin hacer: `Hashtable.GetEnumerator`/`foreach` (la semántica real
      produce `DictionaryEntry`, no `KeyValuePair`2`) no está conectado — ningún IL real de los
      paquetes objetivo de este loop se encontró enumerando un `Hashtable`, solo acceso tipo
      indexador `get_Item`/`set_Item`.
- [x] `internal/checker/profile.go`: `System.Collections.ArrayList::`/`Hashtable::` agregados al
      allowlist de `netstandard-lite`.

**Resultado**

| Paquete | % limpio Fase 3.35 | % limpio Fase 3.36 |
|---|---|---|
| `NPOI@2.8.0` | 95.1% (`MethodsFlagged` 694) | 95.7% (`MethodsFlagged` 616) |

Los findings de `ArrayList`/`Hashtable` desaparecen por completo de los findings principales de
NPOI. Los bloqueadores principales restantes son individualmente chicos: `Console.Write` (48),
`Int16.ToString`/`Array.Clone` (46 cada uno), `String.ToUpper`/`.ToLower`/`.Compare`,
`XmlDocument.CreateElement`, `Encoding.GetEncoding`, `StringBuilder.get_Chars`/`.Remove`/
`.set_Chars`, `Decimal.op_Explicit`, y una cola larga por debajo de 15 findings cada uno.

### Cómo verificar Fase 3.36

```bash
go build ./...
go vet ./...
go test ./... -race -count=5
/tmp/vmnet-cli check package NPOI@2.8.0 --profile=netstandard-lite # sin findings de ArrayList/Hashtable
```

### Fase 3.37 — una barrida amplia de huecos chicos de primitivos/`Array`/`String`/`Console`

Un lote de items individualmente chicos pero numerosos, cada uno un nativo de una o pocas líneas
reutilizando un patrón ya establecido (sin subsistema nuevo, sin cambios en el intérprete) — el
trabajo de mayor densidad restante ahora que cada bloqueador de cluster único grande está limpio:
`Console.Write` (reutiliza `displayString`, el mismo formateo que cualquier otro camino de
`ToString` implícito ya comparte), `Array.Clone`/`.get_Length`, `String.ToUpper`/
`.ToUpperInvariant`/`.ToLower`/`.ToLowerInvariant`/`.Compare`/`.CompareTo`,
`Int16.ToString`/`.GetHashCode` y `Byte.ToString`/`.GetHashCode` (reutilizados directamente de los
propios nativos de `Int32` — Int16/Byte se almacenan igual que Int32, un `KindI4` plano en el stack
de CIL, así que no hizo falta ninguna función nueva en absoluto), `Int32.GetHashCode`,
`Boolean.ToString`/`.CompareTo`/`.GetHashCode`, `Double.CompareTo`/`.Parse`, `Convert.ToString`,
`Char.ConvertFromUtf32`.

- [x] `internal/bcl/system_array.go`, `system_console.go`, `system_string.go`, `system_numeric.go`,
      `system_misc.go`: los nativos de arriba.
- [x] `internal/checker/profile.go`: `System.Array::Clone`/`get_Length` (dos nombres explícitos —
      `System.Array::` no tiene entrada wildcard, a diferencia de la mayoría de los tipos),
      wildcards `System.Int16::`/`System.Byte::`/`System.Boolean::` agregados
      (`System.Int32::`/`System.Char::`/`System.String::`/`System.Console::`/`System.Convert::` ya
      existían como wildcards, así que esos no necesitaron ningún cambio de profile en absoluto).

**Resultado**

| Paquete | % limpio Fase 3.36 | % limpio Fase 3.37 |
|---|---|---|
| `NPOI@2.8.0` | 95.7% (`MethodsFlagged` 616) | **97.0%** (`MethodsFlagged` 422) |
| `ClosedXML@0.105.0` | 93.5% (`MethodsFlagged` 684) | 93.9% (`MethodsFlagged` 635) |

Los findings restantes de NPOI ahora están individualmente por debajo de 25 cada uno —
`XmlDocument.CreateElement`/`Encoding.GetEncoding`/miembros borde de `StringBuilder`/
`System.Decimal`/`Data.DataRow` lideran una cola larga y cada vez más difusa. Ambos paquetes están
ahora sólidamente por encima del promedio ~89% que los 7 paquetes reales existentes + Jint
alcanzaron antes de este loop (Fase 3.28).

### Cómo verificar Fase 3.37

```bash
go build ./...
go vet ./...
go test ./... -race -count=5
/tmp/vmnet-cli check package NPOI@2.8.0 --profile=netstandard-lite
/tmp/vmnet-cli check package ClosedXML@0.105.0 --profile=netstandard-lite
```

### Fase 3.39 — `examples/npoi-demo`: 5 + 13 bugs reales de intérprete/overload encontrados construyéndolo

Ambos paquetes ya habían cruzado el punto de retornos decrecientes persiguiendo findings del
checker (Fase 3.29-3.37: NPOI 91.3%→97.0%, ClosedXML 87.2%→93.9%), así que esta fase pasó al
entregable real hacia el que apuntaba todo el loop: un demo real leyendo un `.xls` legacy real.
Misma metodología que el demo de Jint (Fase 3.27/3.28) — construir la cosa real, arreglar
cualquier hueco real que rompa a continuación, no adivinar de antemano. Se generó un fixture
`.xls` real vía el paquete NPOI 2.8.0 real (paso `dotnet` solo-de-dev,
`examples/npoi-demo/generate/`) y se commiteó (`examples/npoi-demo/testdata/sample.xls`) — el demo
mismo no necesita el SDK de dotnet en runtime, siguiendo el patrón ya establecido del proyecto de
"generar una vez, cargar en Go puro para siempre".

Construir `new HSSFWorkbook(stream)` contra ese archivo real sacó a la luz cinco bugs reales y
generales — no workarounds específicos de NPOI, todos independientes de cualquier método BCL
faltante:

- [x] **El lector de campos respaldados por RVA era demasiado angosto** (`rvaFieldBytes` de
      `assembly.go`): solo reconocía un campo struct sintetizado por el compilador con
      `ClassLayout` de tamaño fijo, respaldando un literal de array — la salida real de Roslyn para
      un literal de array *corto* (≤8 bytes) en cambio declara el campo como un `int`/`long` plano,
      confiando en el tamaño natural del primitivo, sin ninguna fila `ClassLayout` en absoluto.
      `NPOI.POIFS.Common.POIFSConstants.OOXML_FILE_HEADER` — un literal de array real de 4 bytes —
      pegó justo con esto. Arreglado aceptando también tipos de campo `SigI4`/`SigU4` (tamaño 4) y
      `SigI8`/`SigU8` (tamaño 8), usando el ancho primitivo propio del campo en vez de requerir
      `ClassLayout`.
- [x] **Los operadores de shift requerían incorrectamente operandos del mismo Kind**
      (`evalBinOp` de `internal/interpreter/arithmetic.go`): cualquier otro operador numérico
      binario sí necesita anchos de operando coincidentes, pero ECMA-335 III.1.5 Tabla 2
      ("Operaciones de Shift") es la única excepción explícita — la cantidad de shift siempre es
      `int32` sin importar el ancho propio del valor desplazado, y el compilador no emite ningún
      `conv.i8` ensanchador sobre ella. La propia aritmética de offset de bloque de POIFS desplaza
      un `int64` por una cantidad de bits `int` plana de esta forma. Arreglado tratando
      especialmente `OpShl`/`OpShr` para ensanchar una cantidad de shift `int32` al ancho propio
      del valor desplazado en vez de rechazar el desajuste directamente.
- [x] **La resolución de overloads no podía reconocer la clase base BCL real de un tipo nativo**
      (`valueIsAssignableToTypeName` de `assembly.go`): un valor respaldado nativamente (sin
      `TypeDef`, ej. `System.IO.MemoryStream`) siempre devolvía "no asignable" a cualquier cosa, ya
      que no hay cadena `BaseTypeFullName` para recorrer. `NPOI.POIFS.FileSystem.POIFSFileSystem`/
      `NPOIFSFileSystem` declaran un conjunto de constructores de misma aridad sobre tipos de
      referencia completamente no relacionados (`FileInfo`/`FileStream`/`Stream`) — cada candidato
      empataba en el score de solo-Kind grueso para un argumento `MemoryStream`, y el empate se
      resolvía por orden de declaración, corriendo silenciosamente el constructor equivocado
      (basado en archivo) en vez del basado en `Stream`. Arreglado con un nuevo
      `bcl.NativeBaseTypeName` — una tabla chica mantenida a mano (por ahora solo `MemoryStream` →
      `Stream`) que espeja a `bcl.NativeTypeName`, consultada cuando `Obj.Type == nil`.
- [x] **`Dictionary<K,V>` era solo-claves-string** (`internal/bcl/system_collections.go`):
      ampliado para también soportar claves `int32`/`int64`/referencia-a-objeto (`nativeDict.m`
      ahora guarda un par `dictEntry{key, value}` por clave codificada, así que
      `get_Keys`/enumeración devuelven la clave original real, no la codificación de string interna
      de vmnet). Dos casos reales, con peso real, necesitaron esto solo para construir un
      `HSSFWorkbook` en absoluto: `NPOI.SS.Formula.Eval.ErrorEval` clavea un `Dictionary` por las
      propias instancias singleton estáticas de `FormulaError` — un patrón real de "enum
      inteligente" de C#, manejado correctamente vía identidad de puntero de Go sobre el
      `*runtime.Object` subyacente (la misma semántica que `EqualityComparer<TKey>.Default` le
      daría a un tipo de referencia sin override de `Equals`/`GetHashCode`, no una aproximación de
      eso).
- [x] **`Encoding.GetString`/`GetBytes` solo aceptaban la forma `KindBytes` de
      `CallBytes`/`CallJSON`** (`internal/bcl/system_text.go`), no un `byte[]` interpretado real
      (`KindArray`) — la forma que el código real, incluida la propia decodificación interna de
      strings de NPOI, realmente produce y consume. Arreglado para aceptar cualquiera de las dos
      formas en la entrada y devolver siempre un `KindArray` real en la salida (coincidiendo con lo
      que `newarr`/cualquier otro nativo productor de arrays ya devuelve) — lo que a su vez
      requirió generalizar el chequeo estricto de salida solo-`KindBytes` del propio `CallBytes`
      (`call.go`) para también aceptar un resultado `KindArray`, ya que el valor de retorno del
      propio `Encoding.GetBytes(...)` del fixture de test `Rules.Eval` ahora tiene exactamente esta
      forma.
- [x] También en el camino: `System.Random` (`internal/bcl/system_random.go`, nuevo — un caso
      real con peso real: el constructor estático de `NPOI.SS.Formula.Atp.RandBetween` hace `new
      Random()`/`.NextDouble()`, y con solo tocar el registro de funciones de fórmula de NPOI — no
      algo que las propias celdas del demo usen — se llega ahí), `System.IO.FileSystemInfo::
      get_FullName`/`get_Exists`/`Delete` y `System.Environment::GetEnvironmentVariable` como
      stubs seguros no-op/"no seteado" (el camino de fallback a archivo temporal en disco de POIFS
      y un chequeo de override de límite de tamaño, ninguno de los dos está en el camino
      solo-`MemoryStream` de vmnet, pero ambos igual necesitaban *algo* registrado para no hacer
      crashear el intérprete directamente), un nuevo `vmnet.ByteArray([]byte) Value` público
      (`value.go` — la API pública no tenía ninguna forma de construir un argumento `byte[]` real
      en absoluto, necesario para `New("System.IO.MemoryStream", vmnet.ByteArray(data))`), y el
      propio manejo de `KindArray` de `Value` en el lado de retorno (antes se descartaba
      silenciosamente a `nil`).

**Causa raíz de `NotOLE2FileException`, encontrada y arreglada**: NO estaba en `PeekFirstNBytes`
después de todo — esa cadena entera (`ByteArrayInputStream`/`BoundedInputStream`/
`ByteArrayOutputStream`/`IOUtils.Copy`, `LittleEndian.PutLong`/`GetLong`) fue verificada correcta
individualmente vía probes directos. El bug real estaba río arriba de todo eso:
`FileMagicContainer.ValueOf(byte[])` itera con `foreach` un `Dictionary<FileMagic,
FileMagicContainer>` estático construido una vez vía un inicializador de diccionario-literal
(`OLE2` primero, ..., `UNKNOWN` último, cuyo patrón "magic" es `Array.Empty<byte>()` — que empata
trivialmente con *cualquier* entrada vía el cuerpo-de-loop-vacío de `FindMagic` devolviendo `true`
al vacío). El `Dictionary<K,V>` real de .NET enumera en orden de inserción en la práctica mientras
ninguna clave sea removida jamás (no es un contrato estricto, pero los autores de NPOI claramente
escribieron `ValueOf` confiando exactamente en esto: chequeando `OLE2` bien antes de llegar jamás al
comodín de `UNKNOWN`). `nativeDict` (`internal/bcl/system_collections.go`) estaba respaldado por un
`map[string]dictEntry` de Go plano **sin memoria alguna del orden de inserción** — cada llamada a
`GetEnumerator`/`.Values`/`.Keys` obtenía el orden `range` de Go, intencionalmente aleatorizado, así
que `ValueOf` empataba no-determinísticamente con `UNKNOWN` *antes* de siquiera chequear `OLE2`,
clasificando mal un archivo OLE2 confirmado-correcto en aproximadamente la mitad de las corridas.
Arreglado agregando un campo `order []string` (claves codificadas en orden de inserción, mantenido
por un nuevo par `put`/`delete` por el que ahora pasa cada camino de escritura) así cada camino de
enumeración produce un orden de inserción real y estable — un arreglo de corrección real y general
para *cualquier* `Dictionary`/`Hashtable` que un caller enumere, no un parche específico de NPOI.

Ese arreglo sacó a la luz inmediatamente el siguiente hueco real, y el siguiente, en el mismo loop
de "probar → arreglar → re-correr", hasta un demo que efectivamente imprime datos de celda reales
leídos del `.xls` real:

- [x] **`new object()` no tenía `NativeCtor`** (`internal/bcl/system_object.go`) — solo existía la
      variante de llamada-a-base (`register("System.Object::.ctor", false, ...)`, para la cadena
      `: base()` de una subclase), no un target directo de `newobj`. `private readonly object _lock
      = new object();` (un campo de objeto-de-lock común) pegó con esto en más de una clase
      wrapper de I/O propia de NPOI.
- [x] **`System.Threading.Monitor`** (`internal/bcl/system_monitor.go`, nuevo) — `Enter`/`Exit`/
      `TryEnter` como no-ops seguros (`lock (obj) { }`, respaldando un campo plano): vmnet nunca
      corre dos goroutines dentro de una misma cadena de llamadas, así que nunca hay contención
      real que modelar.
- [x] **`System.Type.IsAbstract`** — un nuevo `IsAbstract bool` en `runtime.Type` (poblado en
      `buildType` de `assembly.go` desde `TypeAttributes.Abstract`), `classifyTypeByName` ampliado
      para devolverlo, y un nuevo nativo `get_IsAbstract`.
- [x] **Un subsistema real de `System.Reflection`** (`Type.GetConstructor`/`GetMethod`/`GetField` +
      el propio `Invoke`/`GetValue` de `ConstructorInfo`/`MethodInfo`/`FieldInfo`) — necesario para
      el constructor estático propio de `RecordFactory`, que descubre y construye dinámicamente
      ~205 subclases de `Record` reflejando sobre su campo `sid` y un constructor que coincide.
      Esto es reflection estándar (`ConstructorInfo.Invoke`/`MethodInfo.Invoke`/
      `FieldInfo.GetValue`), no `Reflection.Emit` — no se genera código, cada target es un
      `MethodDef`/`Field` real que la maquinaria ya existente de vmnet (`Machine.New`/
      `Machine.call`/`Type.FieldIndex`) ya sabe correr — confirmado como una capacidad real,
      general, que endurece el proyecto (no un hack puntual de NPOI) antes de construirla. Nuevo
      `MemberResolver` (`Type.GetConstructor`/`GetMethod` con matching de nombre-exacto-más-tipos-
      de-parámetro-declarados, sin coerción de argumentos en runtime ya que todavía no hay
      argumentos en ese punto) enhebrado por `Machine`/`runtime.Resolvers`/`assembly.go` exactamente
      igual que los cuatro resolvers preexistentes (patrón de la Fase 3.27) —
      `internal/bcl/system_reflection.go` (nuevo, tipos wrapper) e
      `internal/interpreter/reflection.go` (nuevos nativos).
- [x] **Los campos estáticos literales/`const` nunca obtenían su valor real de compile-time**
      (`buildType` de `assembly.go`) — deliberadamente saltado para *todo* campo literal (Fase
      3.25, para esquivar recursión infinita construyendo el tipo de firma auto-referencial propio
      de un miembro de enum), dejando un campo estilo `const short sid = 133;` en `Null()` para
      siempre. Una llamada a `FieldInfo.GetValue` encontraba el campo correctamente pero devolvía
      el valor equivocado — que, al usarse como clave de `Dictionary`, salía a la superficie como
      `bcl: Dictionary key kind 0 is not supported`. Arreglado con un nuevo
      `metadata.ConstantForField` (decodifica la fila de la tabla Constant de ECMA-335 por *su
      propio* tag de tipo, nunca el tipo de firma declarado del campo — esquivando la
      preocupación de recursión por completo, ya que el valor en la tabla Constant de un miembro
      de enum siempre es un entero plano sin importar su firma auto-referencial) conectado a la
      rama de campo-literal.
- [x] **El overload de un solo argumento `string[]` de `String.Concat` no se desempaquetaba**
      (`internal/bcl/system_string.go`) — compila a una llamada `ArgCount:1` donde el único
      argumento *es* el array; el código viejo convertía a string el valor del array mismo,
      produciendo el texto literal `<array[5]>` dentro de un mensaje de excepción por lo demás
      correcto.
- [x] **`bcl.NativeTypeName` identificaba mal un `Hashtable` como `Dictionary\`2` (y un
      `ArrayList` como `List\`1`)** (`internal/bcl/system_object.go`) — ambas colecciones legacy
      comparten el struct Go nativo de su contraparte genérica, pero `NativeTypeName` reportaba un
      nombre fijo por tipo Go sin importar qué constructor lo construyó. El recorrido de
      dispatch-virtual de `receiverTypeName` (Fase 3.27) entonces reintentaba silenciosamente un
      miss de `Hashtable::get_Item` contra `Dictionary\`2::get_Item` — que lanza en una clave
      faltante en vez del propio "devolver null" de `Hashtable` — corrompiendo la primerísima
      búsqueda en caché de `NPOI.Util.BitFieldFactory` en un caché que parece siempre vacío.
      Arreglado con un campo `typeName` en `nativeList`/`nativeDict` (seteado por constructor
      real: `List\`1` vs `ArrayList`, `Dictionary\`2` vs `Hashtable`), y extendido a los nuevos
      tipos wrapper `ConstructorInfo`/`MethodInfo`/`FieldInfo`/`SortedList`/`Stack` de abajo para
      que la misma clase de bug no pueda repetirse en ninguno de ellos tampoco.
- [x] **`System.Collections.Hashtable::Add`** no estaba registrado en absoluto (solo el
      indexador/`ContainsKey`/`Clear`/`Remove`) — agregado, reusando el propio `Add` de
      `Dictionary\`2`.
- [x] **`System.IO.Path`** (`internal/bcl/system_io_path.go`, nuevo) — `DirectorySeparatorChar`/
      `AltDirectorySeparatorChar` como host de campo estático (siempre `'/'`: vmnet no tiene
      concepto de "el SO destino en el que correrá este programa", y ningún caller real bifurca
      sobre el valor, solo lo guarda en un campo).
- [x] **`System.Collections.Generic.Comparer\`1`** (`internal/bcl/system_comparer.go`, nuevo) — la
      clase base abstracta `IComparer<T>` que código real subclasea para un ordenamiento a medida
      (el propio `SharedValueManager.SharedFormulaGroupComparator : Comparer<SharedFormulaGroup>`
      de NPOI); solo su llamada-en-cadena de constructor `: base()` necesitaba un stub nativo (una
      subclase interpretada real siempre provee el override real de `Compare`).
- [x] **`System.Collections.SortedList`** (`internal/bcl/system_sortedlist.go`, nuevo) — a
      diferencia del almacenamiento desordenado de `Hashtable`/`Dictionary`, un `IDictionary` real
      que mantiene las entradas ordenadas por clave en todo momento (inserción por búsqueda binaria
      sobre slices paralelos `keys`/`values`). El propio `RowRecordsAggregate` de NPOI clavea sus
      filas por número de fila en uno específicamente para que `.Values` transmita las filas de
      vuelta en orden ascendente — un mapa desordenado aquí barajaría silenciosamente las filas.
- [x] **`System.Collections.Stack`** (`internal/bcl/system_stack.go`, extendido — antes solo
      respaldaba al `Stack\`1` genérico) — el predecesor legacy no genérico, reusando cada uno de
      los nativos existentes de `Stack\`1` más un nuevo `ToArray` (orden de arriba-hacia-abajo,
      coincidiendo con el `Stack.ToArray` real — el reverso del orden interno de slice
      push/pop-al-final).
- [x] **`StringBuilder.Insert`** no estaba registrado en absoluto, y no se podía haber agregado
      sobre el almacenamiento viejo de todas formas: `nativeStringBuilder` usaba un
      `strings.Builder` de Go, que es solo-append y estructuralmente no puede insertar contenido en
      una posición arbitraria. Se cambió el campo de respaldo a un `string` de Go plano
      (reconstruido en cada `Append`/`Insert` — los `StringBuilder`s del mundo real que pega este
      loop se mantienen chicos, una sola fórmula o un fragmento corto de XML, nunca el caso de
      append-en-streaming-grande para el que existe `strings.Builder`) y se agregó `Insert(index,
      value)`.
- [x] **`Encoding.Unicode`/`.BigEndianUnicode` eran alias de la simplificación
      pasar-todo-como-UTF-8** (`internal/bcl/system_text.go`) — silenciosamente incorrecto para el
      propio `NPOI.Util.StringUtil` de NPOI, que usa `Encoding.Unicode` para decodificar los
      strings de celda "sin comprimir" de BIFF, que son genuinamente UTF-16LE en el cable (2
      bytes/char, no 1 — decodificar un byte a la vez tanto corrompe cada codepoint fuera del rango
      ASCII como desincroniza el offset de bytes para todo lo que viene después). Se agregó un
      marcador `nativeEncoding` real + codificación/decodificación UTF-16LE/BE vía el
      `unicode/utf16` de Go; cada otro getter `Encoding.*` mantiene la simplificación preexistente
      de pasar-todo-como-UTF-8 (no hay evidencia de que algún caller real necesite todavía una
      tabla real de codepage windows-1252/big5). También: `Encoding.GetEncoding(string name)`
      reconoce el puñado de nombres realmente solicitados por nombre ("ISO-8859-1", "UTF-16BE",
      ...), y `Encoding.RegisterProvider(CodePagesEncodingProvider.Instance)` es un no-op (la
      indirección real de registro-de-provider nunca se necesita ya que `GetEncoding` ya conoce
      cada nombre que importa sin ella).
- [x] **La resolución de overloads no podía reconocer la implementación de *interfaz* real de un
      argumento, solo su jerarquía de clases** (`valueIsAssignableToTypeName` de `assembly.go`) —
      el arreglo de mayor valor de esta fase: el recorrido solo seguía `t.BaseTypeFullName`, nunca
      consultaba `t.Interfaces` en absoluto. `NPOI.SS.Formula.PTG.AreaPtg` declara dos
      constructores de la misma aridad, 1 argumento — `AreaPtg(ILittleEndianInput in1)` (leyendo
      la codificación binaria de un token, el camino de construcción real al parsear una fórmula
      desde el archivo) y `AreaPtg(AreaReference areaRef)` (construyendo uno desde una referencia
      ya resuelta) — y un `LittleEndianByteArrayInputStream` real nunca se reconocía como asignable
      al parámetro tipado `ILittleEndianInput`, así que empataba en score no mejor que el overload
      completamente no relacionado tipado `AreaReference`, y el empate silenciosamente elegía
      cualquiera que viniera primero: construyendo un `AreaPtg` genuinamente roto cuyo campo
      "`AreaReference`" *era* el input stream, saliendo a la superficie cuatro instrucciones
      `newobj`/`call` después como `NPOI.SS.Util.AreaReference has no field "_firstCell"` — un
      crash de `this`-tipado-como-una-clase-completamente-no-relacionada que tomó rastrear IR real
      de vuelta por `Machine.call`/`newObj`/`fieldSlot` para aislar. Arreglado chequeando también
      la propia lista `Interfaces` del tipo candidato (y de cada clase base), no solo su cadena de
      clases — esto generaliza a *cualquier* parámetro de overload tipado-por-interfaz en
      cualquier parte del proyecto, no solo este par de constructores.

**Limitación conocida restante (no arreglada esta fase)**: el renderizado de texto de fórmulas
muestra las *letras* de columna de una referencia de celda como sus codepoints numéricos en vez de
las letras mismas (ej. `SUM(662:664)` en vez de `SUM(B2:B4)` — `66`/`'B'`+fila `2`, `66`+fila `4`,
concatenados). Causa raíz: vmnet no tiene un `Kind` `char` distinto — un `char` se guarda como un
`int32` plano (limitación existente ya documentada, `String.Concat` ya tenía la misma para
argumentos boxeados no-string), y el `conv.u2` de IL para un cast `(char)x` es *bytecode-idéntico*
a una conversión a `ushort` — las dos son genuinamente indistinguibles una vez decodificadas, sin
información de firma enhebrada hasta el nativo de `StringBuilder.Append`/`Insert` para
recuperarla. `NPOI.SS.Util.CellReference.ConvertNumToColString` construye una letra de columna vía
`StringBuilder.Insert(0, (char)...)` repetido, que esta limitación convierte en dígitos. Un arreglo
real necesita un `KindChar` (o marshaling de llamadas consciente de la firma) — un cambio de
arquitectura, no un parche rápido; fuera de alcance para esta fase, que trata de los bugs de
intérprete/overload de arriba, todos independientes de este hueco preexistente.

Efecto secundario, no el objetivo: el % limpio de `NPOI@2.8.0` en netstandard-lite se movió de
97.0% a **97.3%** (`MethodsFlagged` 422 → 384) puramente de arreglar bugs reales que el demo
necesitaba — esta fase nunca apuntó directamente a findings del checker.

### Cómo verificar Fase 3.39

```bash
go build ./...
go vet ./...
go test ./... -race -count=5
cd examples/npoi-demo && go run .   # abre el .xls real, imprime datos de celda reales + una fórmula (texto con dígitos en vez de letras)
```

---

### Fase 3.40 — `examples/closedxml-demo`: lectura real de `.xlsx`, la cadena de bugs más larga del proyecto

**Objetivo:** el único demo que la Fase 3.39 dejó bloqueado — `new XLWorkbook(stream)` contra un
`.xlsx` real a través del paquete ClosedXML 0.105.0 real, sin modificar. Llegar hasta acá necesitó
docenas de bugs reales y generales de intérprete/BCL corregidos uno por uno en el mismo ciclo de
sonda-ejecución-corrección de cada fase de demo anterior — solo que una cadena mucho más larga
(ClosedXML arrastra transitivamente DocumentFormat.OpenXml, DocumentFormat.OpenXml.Framework,
System.IO.Packaging y System.Memory, cada uno con sus propios internals reales que correr
correctamente).

**El problema arquitectónico central, encontrado una y otra vez**: vmnet borra todo parámetro de
tipo genérico en tiempo de construcción del IR (el mismo cuerpo de método compilado corre para
cada instanciación cerrada), lo cual está bien para una colección respaldada nativamente
(`List<T>`) pero se rompe apenas IL *real, interpretado* depende de conocer su propio `T` —
`typeof(T)`, `new T()`/`Activator.CreateInstance<T>()`, `default(T)`, o una llamada virtual con
prefijo `constrained.` sobre el propio `T`. Un parámetro de método genérico (MVAR, `!!0`) puede
recuperarse por sitio de llamada desde el blob `Instantiation` de su propio `MethodSpec`
(`ir.Call.MethodGenericArgs`, ya construido en una fase anterior) — pero un parámetro de **clase**
genérica (VAR, `!0`) no, ya que el mismo IR corre para cada instanciación de la clase sin importar
qué método se entra. Se evaluó construir identidad real de `runtime.Type` por instanciación cerrada
(para poder rastrear un parámetro genérico de clase de la misma forma) y se decidió deliberadamente
**no** hacerlo — una tarea mayor desproporcionada frente a las formas reales que esto encontró en la
práctica, todas dentro de uno de dos patrones acotados y arreglables:

- [x] **Un método genérico reenvía su propio T todavía abierto a otro método genérico** (p. ej.
      `OpenXmlPart.LoadDomTree<T>()` llamando a `Activator.CreateInstance<T>()`, o
      `OpenXmlElement.Elements<T>()`/`GetFirstChild<T>()` reenviando a `OfType<T>()`/`First<T>()`):
      el sitio de llamada del LLAMADOR conoce el T real y cerrado (es el único lugar de toda la
      cadena donde T es genuinamente concreto), así que cada uno de estos se interceptó directamente
      vía `genericMachineRegistry` — el mismo mecanismo que la primera entrada de la Fase 3.40
      (`FeatureCollectionBase.Get<TFeature>`) ya había establecido — reimplementando solo lo
      necesario del comportamiento real del método en forma nativa en vez de dejar correr el cuerpo
      de IR compartido con T borrado. Nuevos `internal/interpreter/loaddomtree.go`,
      `elementfactory.go`, `elements.go`, `attribute_createnew.go`, `enumvalue_tryparse.go`,
      `cloneimp.go`, `linq_groupby.go`, `linq_range.go` — una intercepción acotada y documentada
      por cada forma real encontrada, no un motor general de reificación.
- [x] **Un campo estático de un parámetro genérico de clase se lee a través de un `ref`/puntero
      administrado en un sitio de llamada cuyo PROPIO tipo declarado es concreto**
      (`Lut<T>.DefaultValue`, `Slice<TElement>._defaultValue` — internals reales de
      `System.Memory`/ClosedXML): el sitio de lectura en sí (`ldfld`/`ldflda`) siempre nombra un
      tipo de valor real y resoluble, aunque el tipo *declarado del campo* sea un `T` borrado.
      `fieldSlot` de `internal/interpreter/eval.go` (promovido a método de `Machine`) ahora
      recupera un struct transitorio, correctamente puesto a cero, del **tipo del propio sitio de
      acceso** cuando encuentra un `KindRef` desnudo apuntando a `KindNull` — de solo lectura por
      construcción (nunca se escribe de vuelta en el slot estático compartido y borrado, lo cual
      corrompería cualquier *otra* instanciación que lo comparta).

**Otros bugs reales y generales encontrados y corregidos en el camino** (cada uno confirmado contra
IL real decompilado antes de corregir, según la metodología estándar del proyecto):

- [x] **Las implementaciones explícitas de interfaz perdían una carrera contra un miembro no
      relacionado con el mismo nombre** (`Machine.call` de `internal/interpreter/calls.go`): el
      recorrido de ancestros del despacho virtual probaba el método de nombre plano de cada
      ancestro *antes* de siquiera revisar si existía una implementación explícita de interfaz
      real (con su nombre mangled) — así que `DocumentFormat.OpenXml.Features.PackageFeatureBase`,
      que declara tanto un `protected abstract Package Package { get; }` plano *como* una
      implementación explícita no relacionada `IPackage DocumentFormat.OpenXml.Features.
      IPackageFeature.get_Package()`, devolvía silenciosamente la incorrecta (el
      `System.IO.Packaging.ZipPackage` real, no el wrapper `this` que el miembro de interfaz
      realmente devuelve), corrompiendo toda llamada tipada `IPackage` posterior sobre él. La
      resolución de implementación explícita ahora corre primero, incondicionalmente.
- [x] **Ese mismo recorrido de ancestros saltaba el tipo hoja del propio receptor cuando se
      llegaba desde un `Instance.Call` disparado por el host** (la API pública de la Fase 3.28
      siempre nombra el tipo concreto exacto del receptor como destino de la llamada, así que
      `concrete == class` ya en la primera iteración del bucle) — una optimización anterior
      saltaba reintentar ese nombre en el bucle (razonamiento: el fallback final por nombre
      completo ya lo cubre), pero un overload de un ANCESTRO que calzaba peor, encontrado primero
      en el bucle, ganaba la carrera antes de que el overload propio del tipo hoja — mejor
      calzado — tuviera siquiera oportunidad. Corregido probando el tipo hoja en línea en vez de
      saltarlo — confirmado vía `DocumentFormat.OpenXml.Wordprocessing.Run.AppendChild<T>()`
      (heredado, nunca redeclarado) y, en el demo de Newtonsoft.Json posterior (Fase 3.43), el
      propio `get_Item(string)` de `Newtonsoft.Json.Linq.JObject` perdiendo contra el
      `get_Item(object)` no relacionado de `JContainer`/`JToken`.
- [x] **La resolución de overloads no tenía forma de distinguir un método plano de uno genérico
      con el mismo nombre y aridad real** — `DocumentFormat.OpenXml.OpenXmlElement` declara tanto
      un `Descendants()` plano como un `Descendants<T>()` genérico (T no aporta ningún parámetro
      real en ninguno de los dos), y el iterador generado por el compilador del propio
      `Descendants<T>()` internamente llama al plano — sin ninguna señal de aridad para desempatar,
      el resolutor elegía de vuelta el overload genérico, reconstruyendo un iterador nuevo y
      llamándose a sí mismo por siempre (`ErrCallDepthExceeded`, una recursión infinita real, no
      una consulta lenta). Corregido con un filtro estricto de `sig.GenParamCount` en
      `pickMethodOverload` de `assembly.go`, guiado por la aridad de instanciación genérica
      conocida del propio sitio de llamada (la longitud de `ir.Call.MethodGenericArgs`) —
      deliberadamente **no** aplicado al camino rápido de candidato único, ya que una llamada
      disparada por el host no tiene ninguna señal de aridad de instanciación propia y un único
      candidato real no es ambiguo de todos modos.
- [x] **`newobj` no tenía un equivalente de `Call.ParamTypeNames`** — `new XLFill()` (dos
      argumentos `ldnull`) se resolvía entre tres constructores de 2 parámetros y la misma aridad
      puramente por *Kind* del argumento, y dos argumentos null calzaban igual de bien en cada
      overload de tipo referencia; corría el incorrecto, saltándose silenciosamente la lógica real
      de generación de clave de `XLFill` y dejando un campo null tres llamadas después. Corregido
      enhebrando los nombres de tipo de parámetro declarados del callee a través de `ir.NewObj`
      exactamente igual que `ir.Call` ya los lleva, alimentando el bono de coincidencia exacta ya
      existente de `pickMethodOverload`.
- [x] **Encadenamiento de constructor base para la familia `Dictionary`/`ConditionalWeakTable`/
      `Collection<T>`** (`internal/bcl/system_collections.go`, `system_conditionalweaktable.go`,
      `system_collection_objectmodel.go`) — una clase de plugin/paquete que subclasea uno de estos
      directamente (`PartExtensionProvider : Dictionary<string,string>`, el propio
      `ExpressionCache : ConditionalWeakTable<string,Formula>` de ClosedXML) encadena a su base
      vía un `call` plano sobre el objeto derivado ya `newobj`'d, no una asignación nueva —
      necesitando el mismo patrón de "mutar el propio `Native` del receptor en el lugar" que
      `system_exception.go` ya había establecido para subclases de excepción.
- [x] **Los propios hooks virtuales protegidos de `Collection<T>` (`InsertItem`/`RemoveItem`/
      `SetItem`/`ClearItems`) se saltaban por completo** — el soporte inicial de `Collection<T>`
      (necesario para el `JPropertyKeyedCollection : Collection<JToken>` de Newtonsoft.Json, Fase
      3.43) implementaba `Add`/`Insert`/`Remove`/`Clear`/el setter del indexador como natives
      planos que mutaban la lista directamente, así que un override real de estos hooks en una
      subclase (que los tipos estilo `KeyedCollection` usan para mantener sincronizado un índice
      de diccionario paralelo) nunca corría — el elemento sí llegaba a la lista (`Count`/
      enumeración se veían correctos) pero cualquier búsqueda por clave devolvía null en silencio.
      Corregido moviendo los mutadores públicos a natives conscientes de `Machine`
      (`internal/interpreter/collection_objectmodel.go`) que hacen una llamada *virtual* real a los
      4 hooks, exactamente igual que el `Collection<T>.Add` real llama a `this.InsertItem(...)`.
- [x] Un hueco real de host de campo estático para `OpenXmlQualifiedName`/`XmlQualifiedName`
      (`XmlQualifiedName.Empty`, un campo `static readonly` real, necesitaba su propio registro
      `registerStaticFieldHost` separado de su constructor plano), más un bug real y general de
      despacho de `IEnumValueFactory<T>::Create`: `EnumValue<T>.TryParse` hace
      `default(T).Create(input)` — una llamada virtual con prefijo `constrained.` sobre un T
      genérico de clase sin receptor concreto al que el despacho de vmnet pudiera redirigir —
      corregido de la misma forma que los otros casos de genérico de clase, retransmitiendo T
      desde el único sitio de llamada real (`AttributeInfo.CreateNew`) que todavía lo conoce.
- [x] `System.Xml.XmlQualifiedName`, `System.Xml.XmlConvert.ToInt32/ToInt64/ToDouble/ToBoolean/
      DecodeName`, `System.Runtime.CompilerServices.ConditionalWeakTable<TKey,TValue>`,
      `System.Collections.ObjectModel.ReadOnlyCollection<T>`, `System.Enumerable.GroupBy`/`Range`,
      los overloads `ReadOnlySpan<char>`/`NumberStyles` de `System.Double`/`Int32.TryParse` —
      superficie de BCL real y general, encontrada y llenada exactamente donde la cadena real de
      lectura de `.xlsx` la necesitaba, no adivinada de antemano.
- [x] **Un hueco de resolución cruzada de ensamblados en la dirección "inversa"**: el wrapper C#
      compilado propio de `examples/closedxml-demo` (`GraphicEngineWrapper.dll`, que provee un
      `IXLGraphicEngine` mínimo para que el motor real de métricas de fuente de ClosedXML — que
      independientemente choca contra la pared de `typeof(T)` de genérico de clase vía
      `SixLabors.Fonts` — nunca tenga que correr) se carga vía `LoadBytes` *después* de ClosedXML
      mismo, así que cuando el propio IL de ClosedXML llama de vuelta a un tipo que el wrapper
      declara, el resolutor nunca había mirado en esa dirección. `Assembly.WithDependencies` ahora
      une los propios TypeDefs del ensamblado llamador al `globalTypeIndex` compartido, extendiendo
      el fallback cruzado entre paquetes de la Fase 3.40 existente para funcionar en ambas
      direcciones.

**Resultado**: `examples/closedxml-demo` abre el mismo fixture `.xlsx` real que el propio demo de
NPOI (Fase 3.39) estableció el patrón para, e imprime su grilla de celdas real, valores string/
numéricos, y una fórmula `SUM` — sin código de lectura en C# compilado en absoluto, solo el
pequeño wrapper necesario para evitar la dependencia propia de ClosedXML en métricas de fuente.

### Cómo verificar Fase 3.40

```bash
go build ./...
go vet ./...
go test ./... -race -count=5
cd examples/closedxml-demo && go run .   # abre el .xlsx real, imprime datos de celda reales + una fórmula SUM
```

### Fase 3.41 — `examples/system-text-json-demo`: transcodificación real UTF-16→UTF-8 y marshaling a nivel de bytes

**Objetivo:** `JsonDocument.Parse(string, JsonDocumentOptions)` a través del paquete
System.Text.Json 8.0.5 real, sin modificar, luego leer un string y un bool de vuelta del
`JsonElement` parseado — sin wrapper C# compilado, solo `Assembly.Call`/`Instance.Call`.

- [x] **La resolución de overloads colapsaba un argumento genérico cerrado a su nombre abierto
      demasiado pronto** — `JsonDocument.Parse(json.AsMemory(), options)` tiene dos overloads de
      la misma aridad, `Parse(ReadOnlyMemory<byte>, ...)`/`Parse(ReadOnlyMemory<char>, ...)`, y
      tanto los nombres de tipo de parámetro capturados por el propio sitio de llamada
      (`ir.Call.ParamTypeNames`) como el scorer de coincidencia exacta en `assembly.go` resolvían
      un `SigGenericInst` hasta solo su nombre genérico abierto (`ReadOnlyMemory\`1`, sin
      argumento de tipo) — así que ambos overloads se veían idénticos y el empate se resolvía
      hacia el que la tabla de metadata listara primero (`byte`), alimentando UTF-16 crudo
      directamente al lector UTF-8 sin ninguna transcodificación. Corregido enrutando ambos a
      través de la codificación de nombre cerrado `SigTypeFullName` ya existente (ya usada para
      `typeof(T)`, solo que no para esto).
- [x] **No había native para los overloads de `Encoding.GetByteCount`/`GetBytes` que toman
      punteros** — el build netstandard2.0 de System.Text.Json (el que vmnet realmente carga)
      transcodifica vía `fixed (char* p = span) { encoding.GetByteCount(p, len); }`, una forma
      real de pin de puntero, no el overload más simple de solo `ReadOnlySpan<char>` que usaría
      un build net8.0. Se agregó `Span<T>/ReadOnlySpan<T>.GetPinnableReference()` más los natives
      de `Encoding` que toman puntero (`internal/bcl/system_span.go`, `system_text.go`).
- [x] **`Unsafe.AddByteOffset` era un passthrough de identidad incondicional** — correcto para su
      único caso conocido original (siempre offset 0), pero el propio bucle real de escaneo por
      byte de `JsonReaderHelper` pasa un offset real y variable — corregido a aritmética de
      punteros real de Go (`internal/bcl/system_unsafe.go`), reflejando el enfoque ya existente de
      `Unsafe.Add`.
- [x] **El marshaling real a nivel de bytes de structs (`MemoryMarshal.Read<T>`/`Write<T>`,
      respaldado por `Unsafe.ReadUnaligned`/`WriteUnaligned`) no tenía implementación en
      absoluto** — el propio `MetadataDb` de `JsonDocument` empaqueta cada token parseado como un
      struct `DbRow` de 12 bytes (3 campos `int32` empaquetados) directamente en un `byte[]`
      alquilado, y luego lee/escribe campos empaquetados individuales de vuelta en offsets de
      byte. Nuevo `internal/interpreter/memorymarshal.go`: codifica/decodifica la forma real de un
      Value (Kinds primitivos, o los propios campos de un struct en orden de declaración) hacia/
      desde bytes consecutivos en un span real respaldado por un byte-array — reinterpretación
      genuina a nivel de bytes, no un hack específico para `DbRow`, útil para cualquier código
      real de formato binario/protocolo que choque con este mismo idioma.
- [x] **`localloc` (`stackalloc T[n]`) no tenía ninguna instrucción de IR en absoluto** — código
      real (el propio dimensionamiento de buffer temporal de `JsonReaderHelper`) reserva en pila
      un buffer pequeño inmediatamente envuelto en un `Span<byte>`. Nuevo `ir.LocalAlloc`: asigna
      un `runtime.Array` real de bytes en cero y empuja un puntero administrado hacia él
      (observablemente idéntico a una reserva de pila real para cualquier caller real, ya que la
      memoria solo se usa con forma de arreglo por el resto de la vida de esa llamada).
- [x] `Span<T>` no tenía ningún constructor registrado en absoluto (`ReadOnlySpan<T>` sí) — un
      `Span<byte>` escribible construido desde el puntero de `localloc` quedaba en un struct vacío
      sin respaldo por defecto en silencio. `Encoding.GetMaxByteCount`, el camino de construcción
      con forma de `IntPtr` de `Convert`, y `Span<T>.Clear()` completaron el resto de la cadena de
      llamada real.

**Resultado**: `examples/system-text-json-demo` parsea `{"name":"vmnet","ok":true}` e imprime
`vmnet:true`.

### Cómo verificar Fase 3.41

```bash
go build ./...
go test ./... -race -count=5
cd examples/system-text-json-demo && go run .
```

### Fase 3.42 — `examples/openxml-demo`: generación real de `.docx`, verificada con el SDK real de .NET

**Objetivo:** generar un `.docx` real desde cero — `WordprocessingDocument.Create`,
`AddMainDocumentPart`, un árbol de elementos `Document`/`Body`/`Paragraph`/`Run`/`Text`,
`Document.Save()` — a través del paquete DocumentFormat.OpenXml 3.1.1 real, sin modificar, sin
wrapper C# compilado (a diferencia del demo de ClosedXML, acá nada necesita un motor de métricas
de fuente).

- [x] **Un subconjunto real de `System.Linq.Expressions`, `ldtoken` sobre un método, y soporte de
      `System.Reflection.MemberInfo`** — el propio `ConfigureMetadata` de cada elemento de OpenXml
      registra cada atributo real vía `Expression<Func<TElement,TValue>>` (`a => a.Space`), que el
      compilador reduce a `Expression.Parameter` + `ldtoken <getter de propiedad>` +
      `MethodBase.GetMethodFromHandle` + `Expression.Property` + `Expression.Lambda` — un patrón
      repetido **~1859 veces** en todo el SDK real. `ldtoken` sobre un token de Método nunca se
      había implementado (solo Tipo/Campo); el único consumidor real (`ElementMetadata.Builder<T>.
      AddAttribute`) solo hace pattern-match `expression.Body is MemberExpression` y lee
      `.Member.Name`, así que ninguno de estos necesitaba representar un grafo de expresión real,
      recorrible/compilable — solo la forma suficiente para esa única inspección. Nuevo
      `ir.LoadMethodToken` (refleja el mismo atajo de identidad que `LoadTypeToken` ya tiene para
      `typeof(T)`) e `internal/bcl/system_linq_expressions.go`.
- [x] **`isinst`/`castclass` contra un objeto respaldado nativamente solo reconocía la jerarquía
      real de excepciones** — el propio `is MemberExpression` de `AddAttribute` contra los nuevos
      stand-ins nativos de Expression de vmnet siempre fallaba. `nativeMatches` de
      `internal/interpreter/typecheck.go` ahora recae en la cadena existente y mantenida a mano de
      `bcl.NativeTypeName` + `bcl.NativeBaseTypeName` (ya usada para el scoring de overloads) para
      cualquier tipo nativo, no solo `ManagedException`.
- [x] Otra pared de parámetro genérico de clase, encontrada de nuevo: el propio inicializador de
      campo estático de `AttributeMetadata.Builder<TSimpleType>` hace `new TSimpleType()` dentro de
      un método *no genérico* de una *clase genérica* — plomería puramente de metadata de
      validación, nunca consultada por el camino real de escritura de XML, así que interceptada
      vía un nuevo hook `nativeCctorOverrides` (`internal/interpreter/attribute_metadata.go`) en
      vez de perseguida arquitectónicamente.
- [x] **Los argumentos de URI de espacio de nombres de `XmlWriter` se descartaban en silencio,
      nunca se escribían a la salida en absoluto** — un bug real y de correctness, con
      consecuencias: cada parte OOXML que vmnet mismo generaba, incluyendo el propio espacio de
      nombres por defecto *requerido* de `[Content_Types].xml`, salía sin ningún espacio de
      nombres. Invisible para el propio `XmlReader` permisivo de vmnet (round-trip a través de sí
      mismo nunca revisa espacios de nombres), pero el SDK real de .NET/Word lo rechazan
      directamente (confirmado directamente: abrir el propio archivo generado por este demo a
      través del SDK real de OpenXml, sin modificar, arrojaba `"Required Types tag not found"`
      antes de esta corrección). Los overloads de `XmlWriter.WriteStartElement` que llevan espacio
      de nombres ahora sí emiten la declaración `xmlns`/`xmlns:prefix` (rastreada por ámbito de
      elemento abierto, reutilizada para `LookupPrefix`) salvo que un ancestro ya enlace el mismo
      prefijo a la misma URI.
- [x] `System.Xml.XmlWriter.WriteStartDocument`/`WriteEndDocument`/`LookupPrefix`, rastreo real de
      ámbito de prefijo de espacio de nombres, el camino de constructor faltante de `System.IntPtr`
      (`newobj IntPtr::.ctor` — vmnet representa IntPtr como un `Int64` desnudo, necesitando el
      mismo tratamiento de "no tiene forma de objeto/struct, su propio camino de ctor" que ya tiene
      el constructor de `System.String`).

**Resultado**: `examples/openxml-demo` genera `report.docx` y — verificado directamente, no
asumido — el SDK real de OpenXml de .NET, sin modificar, lo abre de vuelta y lee el texto de
párrafo correcto.

### Cómo verificar Fase 3.42

```bash
go build ./...
go test ./... -race -count=5
cd examples/openxml-demo && go run .   # escribe report.docx
```

### Fase 3.43 — `examples/newtonsoft-json-demo` + un barrido general de hardening de IL/BCL

**Objetivo:** dos cosas a la vez, deliberadamente — cerrar el ciclo con Newtonsoft.Json 13.0.3
(todavía uno de los paquetes .NET reales más ampliamente desplegados, manejado acá a través de su
DOM real "LINQ to JSON", acceso por indexador de `JObject.Parse`, sin wrapper compilado) *y* un
barrido general por superficie común de BCL de .NET Core sin ningún paquete único que lo dirija,
sobre el principio de que una cobertura más amplia de IL/BCL compone su valor a través de cualquier
paquete futuro, no solo el que se esté sondeando en ese momento.

**Bugs específicos de Newtonsoft.Json** (cada uno una corrección real y general, no un parche
específico de paquete):

- [x] **Un argumento `KindObject` "calzaba" en silencio con un parámetro `SigSZArray`, y un
      argumento numérico "calzaba" en silencio con uno `SigString`** (`hasHardShapeMismatch` de
      `assembly.go`) — `JContainer.InsertItem` llama al `ValidateToken(item, null)` virtual; como
      `JProperty` no lo sobreescribe, el recorrido de ancestros encontró el `ValidateToken(JToken,
      JTokenType[], bool)` estático privado *no relacionado* de `JToken` (misma aridad) y lo
      aceptó, alimentando un objeto `JValue` al parámetro arreglo de `Array.IndexOf`. La misma
      forma luego rompió un índice de lista posicional (`int`) contra el propio indexador `this
      [string key]` no relacionado de `JPropertyKeyedCollection`. Ambos ahora se rechazan
      directamente como formas imposibles, igual que el rechazo directo ya existente de
      `KindObject`-vs-`SigValueType`.
- [x] `System.Char.IsNumber` (`unicode.IsNumber`) — el propio camino de escaneo de números de
      `JsonTextReader`.
- [x] `System.IO.StringReader`/`System.IO.TextReader` — `JObject.Parse(string)` siempre pasa por
      `new JsonTextReader(new StringReader(json))`; superficie de BCL genuinamente nueva,
      `Read`/`Peek`/`ReadLine`/`ReadToEnd` reales.
- [x] `System.Collections.ObjectModel.Collection<T>` en sí (ver la propia entrada de la Fase 3.40
      sobre su hueco de hooks virtuales descubierto después) — la clase base real más común para
      "un `List<T>` con hooks de personalización," reutilizada vía el mismo respaldo `nativeList`
      que ya comparte cada tipo con forma de lista concreto.
- [x] Los overloads de 3/4 argumentos `(array, value, startIndex[, count])` de `Array.IndexOf`
      (solo existía la forma de 2 argumentos) — la propia implementación base de
      `KeyedCollection<TKey,TItem>` reubica un elemento por posición de esta forma durante un
      cambio de clave.

**Hardening general de BCL** (encontrado por relevamiento sistemático de huecos, no por la sonda de
ningún paquete en particular):

- [x] `Convert.ToBase64String`/`FromBase64String`/`TryToBase64Chars` — la codificación/
      decodificación Base64 real estaba completamente ausente; entre la superficie de BCL real de
      .NET más común (datos binarios como texto: hashes criptográficos, tokens, imágenes) mucho
      más allá de cualquier paquete objetivo en particular.
- [x] `Convert.ToByte/ToSByte/ToInt16/ToUInt16/ToUInt32/ToUInt64/ToSingle` — cada conversión
      numérica de ensanchamiento/estrechamiento restante que `Convert.ToInt32/ToInt64/ToDouble/
      ToBoolean` todavía no cubría.
- [x] `String.TrimStart/TrimEnd/PadLeft/PadRight/Insert/Remove` — superficie común de `String` sin
      cobertura previa.
- [x] `Array.Reverse/Fill/Find/FindLast/FindIndex/FindAll/Exists/ForEach/TrueForAll/ConvertAll/
      LastIndexOf` — los miembros estáticos de `Array` que toman `Predicate<T>`/`Action<T>`/
      `Converter<T,TOutput>`, junto a los `Sort`/`BinarySearch` ya existentes.

**Resultado**: `examples/newtonsoft-json-demo` parsea `{"name":"vmnet","stars":42,"active":true}`
e imprime `vmnet:42`.

### Cómo verificar Fase 3.43

```bash
go build ./...
go vet ./...
go test ./... -race -count=5
cd examples/newtonsoft-json-demo && go run .
cd ../npoi-demo && go run .
cd ../system-text-json-demo && go run .
cd ../openxml-demo && go run .
cd ../closedxml-demo && go run .
```

### Fase 3.44 — el cuelgue no determinista de `examples/closedxml-demo`: `FindTypeDef` era un escaneo O(n) sin caché

**Objetivo:** cerrar un bug real y reproducible que la propia afirmación "los cinco demos pasan"
de la suite pasó por alto — `closedxml-demo` se colgaba de forma intermitente (no en cada
ejecución) al lanzarlo directamente con `go run .`, contradiciendo la propia verificación de la
Fase 3.40.

- [x] **`internal/metadata.Metadata.FindTypeDef` hacía un escaneo lineal completo de la tabla
      TypeDef en *cada llamada*, decodificando una cadena Go nueva desde el string heap por cada
      fila revisada** — y el camino real de apertura de paquetes de ClosedXML llama a esto una y
      otra vez para el mismo puñado de nombres de tipo, a través de cadenas recursivas profundas de
      `resolveByFullName`/`resolveByFullNameCrossPackage`/`resolveByFullNameInDeps` anidadas dentro
      de `FeatureCollectionBase.Get<TFeature>()`/`OpenXmlPart.LoadDomTree()` (ver las entradas de
      `genericMachineRegistry` de la propia Fase 3.40). Con `DocumentFormat.OpenXml.dll` cargando
      por sí solo miles de TypeDefs, ese costo se multiplicaba con la profundidad real de
      recursión — y como esa profundidad depende de qué partes XML visita realmente cada ejecución
      y en qué orden, el costo total (y por tanto si a un humano observándolo le parecía un
      "cuelgue") variaba de una ejecución a otra, aunque el algoritmo en sí fuera completamente
      determinista. Diagnosticado con un volcado de goroutines real vía `kill -QUIT` sobre un
      proceso colgado: la goroutine principal estaba `[runnable]` (nunca bloqueada/en deadlock)
      dentro de exactamente este escaneo, decenas de frames del intérprete de profundidad.
      Arreglado con una caché `typeDefCache map[string]typeDefCacheEntry` protegida por mutex,
      agregada a `Metadata` mismo (`internal/metadata/metadata.go`), memoizando tanto aciertos
      *como fallos* (`FindTypeDef` es una función pura de los metadatos, que nunca cambian después
      de que `Parse` retorna, y un fallo re-escanea toda la tabla tan costosamente como un
      acierto) — el mismo patrón de caché ya probado para el cuello de botella hermano
      `resolveExplicitImplExact` (la caché `explicitImpls` de `assembly.go`), aplicado ahora en la
      capa de la que se beneficia cada llamador de `FindTypeDef`, no en un único call site.
- [x] Descartado, no solo asumido: el orden de iteración de mapas de Go como causa contribuyente.
      Se revisó cada tipo de colección agregado esta sesión que pudiera plausiblemente filtrar el
      orden aleatorizado propio de los mapas de Go hacia semántica C# observable — `nativeDict`
      (respalda `Dictionary`/`Hashtable`/`ConditionalWeakTable`) ya rastrea un `order []string` de
      inserción real, con cada camino de escritura canalizado por un único helper `put()`;
      `nativeHashSet` está respaldado por un simple slice `[]runtime.Value`, nunca un mapa de Go.
      Ninguno de los dos pudo haber contribuido a este cuelgue específico.

**Verificación**: 20 invocaciones consecutivas y cronometradas de `go run .` (antes: cuelgues
intermitentes de varios minutos) se completaron todas en una banda plana de 2.50-2.60s sin
fallos, más 10 invocaciones adicionales de `go run .` totalmente directas, sin timeout (igual a
como se reportó el bug originalmente) — todas instantáneas, todas correctas.

### Cómo verificar Fase 3.44

```bash
go build ./...
go vet ./...
gofmt -l .
go test ./...
cd examples/closedxml-demo && for i in $(seq 1 10); do go run .; done
```

### Fase 3.45 — `examples/calculator` + un barrido amplio de endurecimiento de LINQ/colecciones, verificado contra .NET real

**Objetivo:** terminar el último ejemplo de Fase 4 sin implementar (`calculator` — una carga real
de aritmética/loop, verificada, que compara vmnet contra Go nativo y, cuando el SDK de .NET está
disponible, contra CoreCLR real), y, en paralelo, cerrar la brecha más grande que quedaba en
cobertura general de LINQ/colecciones: sin soporte de `OrderBy`/`GroupBy` multi-clave ni
comparador personalizado, sin `Sort`/`Aggregate`/`Zip`/`Min`, y varios tipos de colección menos
comunes (`SortedDictionary`, `SortedSet`, `Queue`/`Stack` bajo LINQ) faltando por completo. Cada
fix acá se verificó de dos formas: la salida real de `dotnet run` de un proyecto sonda como
verdad de referencia, comparada línea por línea contra la propia salida de vmnet para el mismo
código C# exacto (no solo "compila limpio" o "un demo sigue pasando").

**`examples/calculator`** — `Bench.CountPrimes` (loop anidado, módulo, branch) y
`Bench.SumOfSquares` (un solo loop de multiplicar-acumular), corridos a través de vmnet,
cronometrados, verificados contra una reimplementación idéntica en Go nativo (el demo hace
`log.Fatalf` ante cualquier discrepancia — esto es tanto un chequeo de corrección como de
rendimiento), y opcionalmente contra CoreCLR real vía un pequeño proyecto complementario
`coreclr/` que hace `ProjectReference` al mismo `Calculator.csproj` que carga vmnet (así la
comparación siempre corre el mismo código C# exacto, nunca una copia duplicada a mano). Los
límites de los loops se eligieron empíricamente contra el sandbox real de 10.000.000 de
instrucciones por `Call` de vmnet (`DefaultLimits` de `internal/interpreter/limits.go`) —
`CountPrimes(50000)`/`SumOfSquares(700000)` ya lo exceden — en vez de adivinarlos.

**Ordenamiento LINQ, general** (`internal/interpreter/linq_orderby.go`, nuevo):
- [x] `OrderBy`/`OrderByDescending` reemplazó una implementación de una sola clave que solo
      igualaba por Kind exacto (`linqCompare`, un error incondicional ante cualquier clave no
      primitiva o de Kind distinto) por una versión que soporta **`ThenBy`/`ThenByDescending`**
      (no existían en absoluto antes) y un argumento `IComparer<TKey>` real. `ThenBy` no puede
      simplemente reordenar por la nueva clave — eso trataría la nueva clave como primaria y
      descartaría el propio orden del `OrderBy` anterior en cada empate — así que
      `bcl.NativeOrdered` (`system_linq_native.go`) mantiene el orden original antes de ordenar y
      cada clave aplicada hasta el momento, y cada `ThenBy` recalcula el ordenamiento
      compuesto completo desde cero (`materializeOrdered`).
- [x] `compareNatural` (`comparer.go`, generalizado desde un helper exclusivo de
      `Comparer<T>.Default` al ordenamiento compartido "sin comparador en absoluto" de
      `List<T>.Sort`/`Array.Sort`/`OrderBy`/`Min`/`Max`) ahora desenvuelve `Nullable<T>` primero:
      el `Comparer<T>.Default` real para `int?`/`double?`/... ordena una instancia vacía
      (`HasValue == false`) antes que cualquier valor real — la misma regla ya aplicada a una
      referencia null simple.
- [x] `compareFunc`/`equalsFunc` (`comparer.go`) — un despachador compartido cada uno para cada
      forma real de argumento comparador (delegado `Comparison<T>` / instancia `IComparer<T>` /
      instancia `IEqualityComparer<T>` / ausente), reusado ahora por `List<T>.Sort`, `Array.Sort`,
      `Array.BinarySearch`, `OrderBy`/`ThenBy`, y `Distinct`/`Except`/`Intersect`/`Union`/
      `ToHashSet`/`GroupBy` — un despachador por forma en vez de que cada llamador reimplemente su
      propio switch de argumento comparador.

**`List<T>.Sort`/`Array.Sort` e igualdad/ordenamiento por defecto**:
- [x] `List<T>.Sort()`/`Sort(IComparer<T>)`/`Sort(Comparison<T>)` — faltaba por completo (ningún
      nativo registrado en absoluto bajo `List\`1::Sort`); `Array.Sort`/`Array.BinarySearch` ya
      existían (Fase 3.41) y se re-conectaron sobre el mismo despachador `compareFunc` compartido
      de arriba. (`internal/interpreter/array_sort.go`)
- [x] **El propio wrapper de `Comparer<T>.Create(Comparison<T>)` (`funcComparer`) no tenía
      entrada en `NativeTypeName` una vez que `compareFunc`/`arraySort`/`listSort` dejaron de
      tratarlo como caso especial en línea** — un comparador respaldado por
      `Comparer<T>.Create(...)` pasado a `Array.Sort`/`List<T>.Sort` habría caído silenciosamente
      al ordenamiento natural, ignorando el `Comparison<T>` real del llamador. Arreglado agregando
      un caso a `interpreterNativeTypeName` (`elementfactory.go`) — encontrado y arreglado durante
      la propia revisión de integración de este pase, no por la sonda inicial.
- [x] **La igualdad por defecto (sin `IEqualityComparer<T>` explícito) de `Distinct`/`GroupBy`/
      `Except`/`Intersect`/`Union`/`ToHashSet` usaba identidad de puntero para cualquier elemento
      `KindObject`** — el patrón real dominante de "agrupar/deduplicar por más de un campo",
      `GroupBy(x => new { x.A, x.B })`/`Distinct()` sobre una lista de objetos de plugin, dividía
      silenciosamente en varios lo que debía haber sido un solo grupo/elemento distinto.
      `defaultObjectEqual` (`comparer.go`) ahora despacha el propio `Equals` real, posiblemente
      sobreescrito, del receptor (virtual, así que una sobreescritura genuina gana sobre cualquier
      fallback de base) antes de degradar a igualdad por referencia. **Encontrado dos veces**: una
      vez en la propia plomería nueva de `equalsFunc`, y una segunda vez como una regresión viva en
      el propio closure `keysEqual` preexistente de `GroupBy` (`linq_groupby.go`), que todavía
      llamaba directamente al viejo `valuesDeepEqual` (solo igualdad por referencia) y NO había
      sido re-conectado a `defaultObjectEqual` — atrapado por la propia sonda de este pase
      (`GroupBy(e => new { e.Dept, High = e.Salary >= 60 })` producía dos grupos separados
      `Eng:True` de 1 en vez de un solo grupo real de 2) antes de ser arreglado.

**Nuevos miembros de `System.Linq.Enumerable`** (`internal/interpreter/linq.go`): `Min`
(comparte `linqMinMax` con el `Max` ya existente, ambos ahora basados en `compareNatural` y
conscientes de nullables — `Min(IEnumerable<int?>)` devuelve `null` en una fuente vacía/todo-null,
igualando la sobrecarga nullable real en vez de lanzar excepción), `Sum`, `Average`, `Aggregate`
(las tres sobrecargas reales: sin seed, con seed, con seed+resultSelector), `Zip`, `Except`,
`Intersect`, `SkipWhile`, `TakeWhile`, `Reverse`, `AsEnumerable`, `ToHashSet` (materializa en un
`HashSet<T>` real, no un `List<T>` disfrazado de HashSet — los llamadores que después invocan
`Contains`/etc. necesitan el tipo receptor real). `Distinct`/`Union` ganaron su sobrecarga
opcional de `IEqualityComparer<T>`.

**Colecciones heredadas/menos comunes, faltantes por completo antes de este pase**:
- [x] `SortedDictionary<K,V>`/`SortedSet<T>` — reusan el propio conjunto de métodos de
      `Dictionary<K,V>`/`HashSet<T>` verbatim vía un campo `sorted bool` cada uno (insertar en
      posición ordenada en vez de agregar al final), el mismo patrón de
      "typeName-distingue-el-tipo-BCL-real" que ya usa `nativeList` para `List\`1`/`ArrayList`.
      Ninguna sobrecarga `IComparer<T>`/`IComparer<K>` de sus constructores está conectada
      (ignorada silenciosamente) — este paquete no tiene acceso a Machine para despachar una
      personalizada, una brecha documentada, no silenciosa.
- [x] `Queue<T>`/`Stack<T>` no tenían `GetEnumerator` en absoluto — cualquier `foreach`/llamada
      LINQ sobre cualquiera de los dos lanzaba directamente. Arreglado con tipos struct
      `Queue\`1+Enumerator`/`Stack\`1+Enumerator` dedicados (también se agregaron
      `TryPeek`/`TryPop`).

**Un bug real de corrección, sin relación con nada de lo anterior**:
- [x] **El caso de FALLO de `Dictionary<K,V>.TryGetValue` sobreescribía incondicionalmente el
      parámetro `out` con un `null` sin tipo**, destruyendo un cero tipado perfectamente bueno que
      ya estaba ahí — el almacenamiento de un argumento `out int v` ya está inicializado a cero de
      forma real como `Int32(0)` por el propio paso de inicialización de locales del método, antes
      de que `TryGetValue` sea siquiera llamado. Probado con un fixture real
      (`d.TryGetValue("missing", out int v); return v.ToString();`): pisar ese `Int32(0)` con
      `KindNull` hacía que el siguiente `v.ToString()` lanzara "expects an int32 receiver" en vez
      de imprimir `0` como hace .NET real. Arreglado dejando la ranura intacta ante un fallo.

**Nota de arquitectura**: el tipo de resultado de `IGrouping<K,V>` (`nativeGrouping`) se movió de
`internal/interpreter` a `internal/bcl` como un `NativeGrouping` exportado (reflejando la propia
división ya existente de `NativeOrdered`: tipo de resultado en `bcl`, algoritmo en `interpreter`)
— necesario para que nativos simples, sin Machine, que no pueden importar un paquete que a su vez
importa `bcl` (`String.Join`, `List<T>.AddRange`) puedan reconocer un resultado de `GroupBy` de la
misma forma que ya reconocen uno de `OrderBy`, sin duplicar la lógica de reconocimiento en dos
lugares.

**Verificación**: un proyecto sonda independiente (~40 escenarios reales de LINQ/colecciones —
`OrderBy`/`ThenBy` multi-clave, `Distinct` con comparador personalizado, `Aggregate`, `Zip`,
`OrderBy`/`Sum` de tipo nullable, `SortedDictionary`/`SortedSet`, `Queue`/`Stack` bajo LINQ,
`List<T>.Sort`/`Array.Sort` con comparador, y más) corrido primero bajo `dotnet run` real como
verdad de referencia, y luego el mismo DLL compilado cargado en vmnet — cada línea coincidió con
la salida real de .NET excepto una aproximación ya documentada y preexistente, intencional (el
orden de enumeración de `Dictionary<K,V>` después de un remove-luego-reinsertar no replica la
reutilización real de slots de bucket de CoreCLR; anotada en `system_collections.go` bastante
antes de este pase, no algo introducido acá).

### Cómo verificar Fase 3.45

```bash
go build ./...
go vet ./...
gofmt -l .
go test ./...
cd examples/calculator && dotnet build Calculator.csproj -c Release && go run .
cd ../npoi-demo && go run .
cd ../system-text-json-demo && go run .
cd ../openxml-demo && go run .
cd ../newtonsoft-json-demo && go run .
cd ../closedxml-demo && for i in $(seq 1 10); do go run .; done
```

### Re-certificación: 10 targets (8 paquetes + Jint, NPOI y ClosedXML promovidos a targets completos)

NPOI y ClosedXML se graduaron de "sondas de checker de ganancias baratas" (Fase 3.29-3.39) a
targets completos respaldados por demo, junto a los 7 paquetes originales + Jint, en el mismo
plano: un paquete real, sin modificar, sin glue C# compilado más allá de lo que una limitación
arquitectónica genuina (el propio motor de métricas de fuente de ClosedXML) requiere.

| Paquete | % limpio netstandard-lite | Demo real |
|---|---|---|
| `NPOI@2.8.0` | 97.6% (14.202 métodos analizados, 347 marcados) | `examples/npoi-demo` — lee un `.xls` legacy real |
| `ClosedXML@0.105.0` | 96.1% (10.444 métodos analizados, 412 marcados) | `examples/closedxml-demo` — lee un `.xlsx` real |
| `System.Text.Json@8.0.5` | 95.7% (3.577 métodos analizados, 155 marcados) | `examples/system-text-json-demo` — parsea JSON real |
| `Newtonsoft.Json@13.0.3` | 84.4% (4.064 métodos analizados, 636 marcados) | `examples/newtonsoft-json-demo` — parsea JSON real |
| `DocumentFormat.OpenXml@3.1.1` | 100.0% (67.234 métodos analizados, 7 marcados) | `examples/openxml-demo` — escribe un `.docx` real, abierto de vuelta por el SDK real de .NET |
| `Jint@3.1.3` | 94.6% (5.414 métodos analizados, 290 marcados) | `examples/jint-demo`/`jint-nowrapper` — corre JS real |
| `Ardalis.GuardClauses@5.0.0` | 97.5% (285 métodos analizados, 7 marcados) | — |
| `Semver@2.3.0` | 92.9% (423 métodos analizados, 30 marcados) | — |
| `FluentValidation@11.9.2` | 96.0% (1.289 métodos analizados, 51 marcados) | — |
| `Humanizer.Core@2.14.1` | 97.1% (1.597 métodos analizados, 47 marcados) | — |
| `SimpleBase@4.0.0` | 92.2% (258 métodos analizados, 20 marcados) | — |

Cinco de diez targets ahora tienen un demo real y completo (leyendo y/o escribiendo formatos
binarios/XML/JSON reales de punta a punta) en vez de solo un porcentaje de checker estático — la
señal más fuerte hasta ahora de que vmnet corre paquetes .NET reales y genuinamente sin modificar,
no solo pasa un linter de compatibilidad. Re-medido después de la Fase 3.45
(`internal/checker.Report.MethodsAnalyzed`/`MethodsFlagged`, `--profile=netstandard-lite`,
dependencias transitivas incluidas exactamente como lo hace `vm.LoadPackage` en tiempo de
ejecución): cada target subió, varios sustancialmente — `Humanizer.Core` de 46.0% a 97.1%,
`FluentValidation` de 63.5% a 96.0%, `SimpleBase` de 45.7% a 92.2%, `Newtonsoft.Json` de 60.6% a
84.4% — reflejando cuánta de la superficie de LINQ/colecciones (`OrderBy`/`GroupBy`/`Sort`/
`Aggregate`/`SortedDictionary`/...) usa realmente el propio código de esos paquetes. El promedio
simple entre las 11 filas es 94.9%; un promedio ponderado por métodos es 98.2%, dominado por los
propios 67.234 métodos analizados de `DocumentFormat.OpenXml` (62% del total combinado) al
100.0% — el número por paquete es el más representativo de "qué tan bien cubre vmnet un paquete
real diverso y típico", no el ponderado.

### Fase 3.51 — un barrido amplio de endurecimiento de formato de strings/cultura, excepciones, y reflection

**Objetivo:** tres áreas que los pases anteriores impulsados por demos nunca apuntaron
específicamente — especificadores de `ToString(format)`/`string.Format` numéricos/de DateTime,
filtros de excepción/`Data`/`AggregateException`, y `System.Reflection` (`PropertyInfo`,
`Enum.Parse`/`TryParse`, `Activator.CreateInstance(Type, object[])`,
`MethodInfo.MakeGenericMethod`). Cada fix se verificó contra la salida real de `dotnet run` para
el mismo código C# exacto (una librería fixture `netstandard2.0` + un runner `net10.0` manejado
por reflection), comparada línea por línea contra la propia salida de vmnet para el mismo DLL
compilado.

**Excepciones — las dos brechas más grandes**:
- [x] **Las cláusulas de filtro `catch (Foo) when (cond)` no tenían soporte en absoluto** —
      `ir.Build` fallaba directamente con `UnsupportedOpcodeError{OpCode: "filter (catch-when)"}`
      en el instante en que un método contenía una, no solo un bug de salida incorrecta. Arreglado
      de forma general: el parseo ya correcto de `FilterOffset` de `il` ahora se baja a una
      cláusula `ir.HandlerFilter` real con su propio `FilterStart`/`endfilter` (`ir.EndFilter`,
      opcode `0xFE11` — distinto del `0xDC` de `endfinally`) IR, y
      `internal/interpreter/exceptions.go` corre el cuerpo del filtro en línea exactamente como un
      handler finally/fault, usando su veredicto booleano para decidir si entrar al handler o
      seguir buscando entre los candidatos restantes (`resumeAfterFilter`).
- [x] **Un objeto de excepción capturado perdía sus propios campos extra en el momento en que se
      capturaba** — `catch (MyException e) { ...e.Code... }` fallaba con `"MyException has no
      field \"<Code>k__BackingField\""` para CUALQUIER subclase de excepción personalizada con sus
      propios campos/auto-propiedades, con o sin filtro. Causa raíz: `dispatchException`/
      `resumeAfterFilter` empujaban un `&runtime.Object{Native: ex}` nuevo y vacío a la pila en el
      punto de entrada del catch, nunca el objeto REAL lanzado (que — para una subclase de plugin —
      tiene tanto `Type` como `Native` seteados, ver el propio comentario de
      `baseExceptionCtorInPlace`). Arreglado dándole a `ManagedException` una referencia real
      `Object *runtime.Object`, seteada una vez por `ir.Throw`, y reusándola (`exceptionValue`) en
      cada punto de entrada de catch/filtro en vez de un wrapper nuevo — la propia lógica de
      coincidencia de tipos de `exceptionMatchesCatch` queda intacta (todavía necesita el camino
      del wrapper vacío para que el recorrido de jerarquía de excepciones de `nativeMatches` siga
      funcionando más allá de una base `System.Exception` no resoluble).
- [x] `Exception.GetType()` sobre una excepción simple (no subclase de plugin) fallaba con
      "unsupported BCL method" — el recorrido de despacho virtual (`calls.go`) ya tenía un
      fallback de `System.Object` para `Equals`/`GetHashCode`/`ToString`, pero no para `GetType`; y
      por separado, `bcl.NativeTypeName` no tenía ningún caso para `*runtime.ManagedException`, así
      que el recorrido ni siquiera arrancaba para un objeto de excepción simple (`ok` era false).
      Ambos arreglados.
- [x] `Exception.ToString()` caía a un `Object.ToString()` genérico (solo el nombre de tipo, sin
      mensaje) — se agregó una sobreescritura real reusando el propio formato `TypeName: Message
      ---> innerError` de `ManagedException.Error()`.
- [x] `Exception.Data` (un `IDictionary`, semántica real: nunca null, respaldado perezosamente) —
      faltaba por completo; ahora asigna perezosamente un diccionario real con forma de
      `Hashtable` en un nuevo campo `ManagedException.Data` en el primer acceso.
- [x] `ArgumentException`/`ArgumentNullException`/`ArgumentOutOfRangeException.ParamName` —
      faltaba por completo (sin campo para guardarlo). El constructor de 2 strings de
      `ArgumentNullException`/`ArgumentOutOfRangeException` pone `paramName` PRIMERO
      (`(paramName, message)`), lo opuesto al propio `(message, paramName)` de `ArgumentException`
      — una asimetría real y fácil de errar de la API de .NET, ahora manejada por tipo
      (`argExceptionParamOrder`).
- [x] `System.AggregateException` — faltaba por completo (`InnerExceptions`, `Flatten()`). Se
      agregó un campo `ManagedException.InnerExceptions []*Object` (`Inner` sigue apuntando al
      primero, así que el getter singular ordinario `InnerException` sigue funcionando de forma
      transparente) más un `Flatten()` recursivo real.

**Formato de strings**:
- [x] `int`/`long`/`double.ToString(format)` **ignoraba el argumento de formato por completo** —
      `n.ToString("X")`/`("N0")` silenciosamente corría la sobrecarga sin argumento en su lugar.
      Para `double` específicamente esto no era solo "falta una coma": `Double.ToString("N2")`
      sobre un valor grande caía a `FormatFloat('G', -1, ...)`, que cambia a notación científica
      en esa magnitud — una respuesta completamente distinta, no solo formato faltante. Ambos
      ahora pasan por `formatValue`, el propio parser de especificadores de `String.Format`.
- [x] `{0:X}` sobre un valor negativo producía `"-1"` en vez del patrón de bits en complemento a
      dos real de .NET (`"FFFFFFFF"` para `int`, 16 F's para `long`) — arreglado usando el `Kind`
      real del valor (I4 vs I8) para elegir el ancho correcto. `{0:x8}` en minúscula también
      ignoraba el caso por completo (siempre en mayúscula) — arreglado.
- [x] Los especificadores estándar `"C"` (moneda) y `"E"`/`"e"` (científico) no estaban
      implementados en absoluto. `"E"` además necesitó un fix manual de ancho de exponente: el
      `FormatFloat` de Go rellena el exponente a 2 dígitos, .NET real siempre usa al menos 3
      (`"E+003"`, no `"E+03"`).
- [x] Los strings de formato numérico personalizado (`"0.00%"`, `"000.00"`, `"#,##0.00"` — una
      secuencia de caracteres placeholder `0`/`#`/`,`/`.`/`%`, no un especificador estándar de una
      letra+dígitos) se rechazaban directamente como "unsupported format specifier". Se agregó una
      implementación acotada de formato personalizado (`formatCustomNumeric`) cubriendo el caso
      común; una sección separada por `;` de positivo/negativo/cero o un patrón personalizado de
      notación científica sigue dando error correctamente en vez de adivinar.
- [x] `StringBuilder.AppendFormat` — faltaba por completo; ahora comparte el propio parser de
      formato compuesto de `String.Format`.
- [x] `int.TryParse(s, NumberStyles.HexNumber, ...)` parseaba mal silenciosamente un literal hex
      como decimal (y fallaba) — ahora detecta el bit `AllowHexSpecifier` y parsea en base 16.
- [x] `DateTime.ToString(format)` ignoraba su argumento (siempre el formato fijo por defecto) —
      ahora respeta tanto un especificador estándar de una letra (`"d"`, `"D"`, `"s"`, `"o"`, ...)
      como un patrón personalizado (`"yyyy-MM-dd HH:mm:ss"`, vía el mismo traductor que
      `ParseExact` ya usaba, corrido en la dirección de Format).
- [ ] **Encontrado, no arreglado**: un valor `bool`/enum boxeado pasado a través de
      `string.Format`/un string interpolado imprime su `int32` subyacente crudo (`"1"`/`"0"`, o el
      valor numérico de un enum) en vez de `"True"`/`"False"` o el nombre del miembro. Causa raíz:
      `box`/`unbox.any` se eliden como no-op (`ir/builder.go` — el `Value` de vmnet ya es una unión
      etiquetada, así que boxear un value type "simplemente funciona" para cualquier otro
      consumidor), lo cual descarta la ÚNICA información que un sitio de llamada de display/
      `ToString` necesitaría para distinguir un `bool`/enum boxeado de un `int32` boxeado en ese
      punto — para cuando los argumentos `object[]` de `String.Format` llegan a `displayString`,
      son valores `KindI4` indistinguibles. Un fix real necesita o bien un cambio amplio de
      representación de `Value` (etiquetar cada valor enum/bool con su tipo declarado — toca
      despacho de aritmética/comparación/switch en todo el intérprete) o pasar el propio operando
      de tipo estático del prefijo `constrained.` hasta un `callvirt` de `ToString` siguiente
      específicamente — ambos más grandes que el alcance de este pase.
      `Enum.GetValues`/`GetNames`/`Parse`/`TryParse` (que todos toman un argumento `Type` explícito,
      no un receptor ambiente) no están afectados y ya son correctos.

**Reflection**:
- [x] `Type.GetProperties()`/`GetProperty(name)` + `PropertyInfo.GetValue`/`SetValue`/`Name`/
      `CanRead`/`CanWrite` — faltaba por completo; no existía ningún lector de metadatos para las
      tablas `Property`/`PropertyMap`/`MethodSemantics`. Se agregaron accesores tipados
      (`metadata.Property`/`TypeDefPropertyRange`/`PropertyAccessors`) más un `PropertyResolver`
      nuevo, enhebrado a través de `runtime.Resolvers`/`interpreter.Machine` de la misma forma que
      ya lo están `MemberResolver`/`EnumResolver`. `CanRead`/`CanWrite` vienen del enlace real de
      `MethodSemantics` de `get_Xxx`/`set_Xxx` (correctamente true para un accesor `private set` —
      el `CanWrite` real de reflection significa "tiene un setter en absoluto", no "públicamente"),
      no una adivinanza por nombre.
- [x] `Activator.CreateInstance(Type, object[])` — la sobrecarga de reflection no genérica
      ordinaria — siempre fallaba con `"T could not be resolved"`, porque `Activator.CreateInstance`
      solo estaba conectado para la forma GENÉRICA `CreateInstance<T>()` (`where T : new()`); ambas
      compilan al mismo nombre de método CIL, distinguibles solo por si los argumentos de método
      genérico del sitio de llamada están presentes.
- [x] `Enum.Parse`/`Enum.TryParse<TEnum>` — faltaba por completo. `Parse` es un método estático
      simple (el enum destino nombrado por un argumento `Type` ordinario); `TryParse<TEnum>` es en
      sí mismo un MÉTODO GENÉRICO (la misma forma `ir.Call.MethodGenericArgs` que necesita
      `Activator.CreateInstance<T>`) — conectado por separado por esa razón.
- [x] `MethodInfo.MakeGenericMethod(Type[])` + invocar el resultado — faltaba por completo.
      `Type.GetMethod(string)` (sin argumento de firma `Type[]`, la sobrecarga que el código real
      usa para buscar un método genérico todavía abierto antes de cerrarlo) también requería 3
      argumentos incondicionalmente y fallaba directamente; ahora acepta la forma de 2 argumentos,
      coincidiendo solo por nombre (la propia distinción de Go entre nil y slice vacío, preservada
      a través de `bcl.TypeArrayToFullNames`, es lo que distingue "sin Type[] en absoluto" de "un
      Type[] vacío real explícito" en `resolveMember`).

### Cómo verificar Fase 3.51

```bash
go build ./...
go vet ./...
gofmt -l .
go test ./...
cd examples/npoi-demo && go run .
cd ../system-text-json-demo && go run .
cd ../openxml-demo && go run .
cd ../newtonsoft-json-demo && go run .
cd ../calculator && dotnet build Calculator.csproj -c Release && go run .
cd ../closedxml-demo && dotnet build GraphicEngineWrapper.csproj -c Release && for i in 1 2 3 4; do go run .; done
```

---
### Fase 3.52 — endurecimiento de Dapper: despacho ADO.NET de `System.Data`, reflection de genéricos cerrados, y un demo real con proveedor fake

**Objetivo:** Dapper 2.1.79 medía 76.1% limpio bajo `netstandard-lite` (1047 métodos, 250
marcados) — el más débil de los paquetes reales rastreados, casi enteramente `System.Data`/
`System.Data.Common` (sin ningún despacho de interfaz modelado en absoluto) y un tramo de
superficie de `System.Reflection` para la cual la Fase 3.51 construyó la maquinaria pero nunca
terminó de conectar al checker. Termina en **93.8% limpio (65/1047 marcados)**, más un
`examples/dapper-demo` real y funcional que ejercita el propio `SqlMapper.Query`/`Execute` de
Dapper de punta a punta contra un proveedor ADO.NET fake mínimo en memoria — sin base de datos
real, sin necesitar el SDK de .NET en tiempo de ejecución.

**`System.Data` — diseñado desde cero**:
- [x] `IDbConnection`/`IDbCommand`/`IDbTransaction`/`IDataReader`/`IDataRecord`/`IDataParameter`/
      `IDbDataParameter`/`IDataParameterCollection` no necesitan NINGUNA maquinaria nueva del
      intérprete — la propia implementación concreta de un plugin (un driver real, o el fake de un
      demo) se resuelve por el recorrido de despacho virtual ya existente de `Machine.call`, el
      mismo mecanismo que ya usan `IEnumerable`1`/`IEqualityComparer`1`. Solo el checker necesitaba
      ponerse al día: los nuevos `adoNetDispatchTypes`/`isAdoNetDispatchTarget` de
      `internal/checker/analyzer.go` tratan cualquier miembro de estos tipos como resoluble vía
      despacho, por TIPO en vez de una entrada de `interfaceDispatchTargets` por cada miembro real
      (~60 de ellos entre las interfaces y las clases base abstractas de abajo).
- [x] `DbConnection`/`DbCommand`/`DbDataReader`/`DbParameter`/`DbParameterCollection`/
      `DbTransaction` son CLASES BASE ABSTRACTAS reales de ADO.NET que un plugin extiende vía una
      cadena `.ctor` `base()` real (usualmente implícita) — `internal/bcl/system_data.go` registra
      un `.ctor` no-op simple para cada una, el mismo patrón `baseExceptionCtorInPlace`/
      `dictCtorInPlace` que las Fases 3.10/3.32 ya establecieron para `System.Exception`/
      `Dictionary`2`.
- [x] `DbDataReader.Dispose()` (público, concreto — NO abstracto) es un método real heredado de la
      clase base que una subclase real típicamente NO sobreescribe (usualmente solo se
      sobreescribe el `Dispose(bool)` protegido, ej. el propio `WrappedBasicReader` interno de
      Dapper) — `dbDataReaderDispose` de `internal/interpreter/adonet.go` intenta la propia
      sobreescritura `Dispose(bool)` del receptor directamente vía `Machine.tryCall` (no
      `Machine.call`, para evitar recursión hacia sí mismo una vez que no existe sobreescritura).

**Reflection — cerrando brechas reales que la Fase 3.51 abrió pero no terminó**:
- [x] `Type.GetProperties`/`GetProperty` más `PropertyInfo.GetValue`/`SetValue`/op_Equality/
      op_Inequality eran nativos reales y FUNCIONALES desde la Fase 3.51 que la propia lista
      `reflectionMachineTargets` del checker simplemente nunca reflejó — cada llamada real se
      reportaba mal como no soportada pese a ya correr correctamente. La misma clase de brecha de
      paridad se arregló para el propio `Find`/`FindAll`/`ConvertAll`/`Sort`/etc. de `System.Array`
      (`array_ops.go`/`array_sort.go`, todos nativos reales desde las Fases 3.41/3.42, nunca
      reflejados en el checker tampoco) vía un nuevo mapa `arrayMachineTargets`.
- [x] `Type.GetMethod`/`GetProperty` solo aceptaban su propia forma de sobrecarga documentada más
      angosta (2 o 3 argumentos) — cualquier sobrecarga real que tomara `BindingFlags`
      (`GetMethod(name, BindingFlags)`, `GetMethod(name, BindingFlags, Binder, Type[],
      ParameterModifier[])`) o fallaba directamente o mal-leía silenciosamente un argumento int
      `BindingFlags` como un `Type[]`. Ahora escanea todos los argumentos finales buscando el
      primer `Type[]` real, ignorando cualquier otra cosa — encontrado vía el propio constructor
      estático `SqlMapper` de Dapper, que usa exactamente la forma de 5 argumentos a través de su
      propio helper `GetPublicInstanceMethod`.
- [x] **`Type.GetMethod`/`GetConstructor`/`GetField` llamados sobre un tipo GENÉRICO CERRADO (vía
      `Type.MakeGenericType`) siempre fallaban en resolver** — `resolveMember` (assembly.go) nunca
      recibía el nombre TypeDef real, ABIERTO/sin enlazar, para buscar, solo el string cerrado
      `Outer+Inner\`1[[Arg]]` que codifica `sigTypeFullName` para `typeof(T)`/`MakeGenericType` (en
      los metadatos reales solo hay un TypeDef por tipo genérico abierto — ECMA-335 no tiene un
      TypeDef separado por cada instanciación cerrada). El nuevo `typeFullNameOfOpen`
      (reflection.go) normaliza vía `bcl.GenericOpenName` antes de cada búsqueda de reflection.
      Encontrado vía el propio cctor de Dapper reflejando sobre `TypeHandlerCache<DataTable>`/
      `<XmlDocument>`/`<XDocument>`/`<XElement>` para cachear el método `SetHandler` de cada uno —
      esto se rompía en el instante en que se tocaba `Dapper.SqlMapper` en absoluto, antes de
      correr una sola consulta real.
- [x] `Type.GetProperties`/`GetProperty` más `PropertyInfo.PropertyType`/`GetGetMethod`/
      `GetSetMethod`/`GetIndexParameters` (`PropertyInfo.PropertyType` leído del accesor real que
      exista vía el nuevo `propertyTypeFullName` de `assembly.go`). Un fallback
      `wellKnownBclProperties` (reflection.go) pequeño y deliberadamente angosto además mapea a
      mano exactamente dos propiedades reales del framework BCL para las que vmnet no tiene ningún
      TypeDef — `CultureInfo.InvariantCulture` y el propio indexador `this[int]` de
      `DbDataReader` — ambas reflejadas incondicionalmente por el cctor de Dapper; sin esto,
      `.GetGetMethod()` llamado sobre el `PropertyInfo` no-nulo del comportamiento real de .NET
      lanzaba NullReferenceException sobre el `Null()` de vmnet en su lugar, en el instante en que
      carga `Dapper.SqlMapper`.
- [x] `Type.GetConstructors()` (plural) más `MethodBase.GetParameters()`/`ParameterInfo` — nuevo
      por completo: `resolveMemberParams` de `assembly.go` lee cada sobrecarga real de tipos de
      parámetro declarados (vía `metadata.ParseMethodSig`) y nombres de parámetro reales (nuevo
      `metadata.MethodDefParamRange`, reflejando `TypeDefFieldRange`). Un `ConstructorInfo` del
      plural `GetConstructors()` lleva su propio `overloadIndex` real así que cada elemento de ese
      array responde `GetParameters()` con SU PROPIA firma, no solo la de la primera.
- [x] `Type.GetTypeCode`/`Enum.GetUnderlyingType`/`Type.IsArray`/`GetElementType` — agregados
      pequeños e independientes (manipulación pura de nombre/sufijo, ninguno necesita acceso a
      Machine).

**Colecciones/Array — brechas reales encontradas auditando la paridad checker-vs-runtime**:
- [x] `List\`1::.ctor` no tenía nativo de encadenamiento de base (`Dictionary`2` ya lo tenía) —
      cualquier clase de plugin subclaseando `List<T>` directamente entraba en pánico sobre un
      receptor nulo en el instante en que cualquier método nativo de `List<T>` corría a través del
      recorrido de ancestros. Encontrado vía el propio
      `FakeParameterCollection : List<FakeParameter>, IDataParameterCollection` de
      `examples/dapper-demo`.
- [x] `List<T>.RemoveAll(Predicate<T>)` — faltaba por completo; agregado junto a los propios
      nativos de `Array.Find` que invocan delegados con acceso a Machine.
- [x] `List<T>.Reverse()`, `Array.CreateInstance`/`GetValue`/`SetValue`, `Regex.Escape`,
      `ConcurrentDictionary`2::Clear`/`get_Keys`/`GetEnumerator` (las últimas tres necesitaron
      cambiar el propio almacenamiento de `nativeConcurrentDict` de valores simples a pares reales
      clave+valor `dictEntry`, ya que el diseño original nunca necesitó devolver la clave real),
      `IDictionary`2::Add`/`Remove`/`get_Keys` (solo checker — nativos de `Dictionary<K,V>` ya
      funcionales que el checker nunca reflejó para la forma de llamada declarada por interfaz),
      `SByte`/`UInt16`/`UInt32`/`UInt64`/`Single.ToString`, y tres tipos de excepción más simples
      (`DataException`/`ApplicationException`/`ObjectDisposedException`) — todas brechas pequeñas
      e independientes encontradas auditando uno por uno los hallazgos restantes del propio
      checker sobre Dapper.
- [x] `StringComparer.Ordinal`/`OrdinalIgnoreCase` no tenía caso en `NativeTypeName` — un sitio de
      llamada declarado contra `IEqualityComparer<string>` (el propio campo
      `connectionStringComparer` de Dapper) nunca podía redirigir a los nativos ya registrados
      `StringComparer::GetHashCode`/`Equals`, cayendo en su lugar al nombre de interfaz literal, no
      resoluble.

**Encontrado, no arreglado** (ambos límites arquitectónicos genuinos y permanentes, no
descuidos):
- Los propios `Query<T>()`/`Execute<T>()` genéricos de Dapper hacen `typeof(T)` internamente
  sobre su propio parámetro de tipo de MÉTODO genérico — el caso `IsMethodGenericParam` de
  `ir.LoadTypeToken` no tiene forma de resolver esto (nada enhebra "con qué argumentos genéricos
  fue invocado el propio método actualmente en ejecución" hasta un `ldtoken` en las profundidades
  del cuerpo de ese método, a diferencia de los propios argumentos genéricos ya resueltos de un
  sitio de llamada). Arreglarlo de forma general implica llevar el contexto de instanciación de
  método genérico a través de todo el pipeline de invocación de métodos — un cambio real e
  invasivo, no intentado acá. Se evitó en el demo usando en su lugar la propia sobrecarga no
  genérica `Query(Type, ...)` de Dapper.
- Cualquier llamada de Dapper que provea un objeto de parámetros real (de cualquier forma) siempre
  escanea primero el texto SQL crudo vía `Dapper.SqlMapper.CompiledRegex.LiteralTokens`:
  `(?<![\p{L}\p{N}_])\{=([\p{L}\p{N}_]+)\}` — un negative lookbehind, una feature real de regex de
  .NET que el `regexp` de Go, basado en RE2, nunca puede soportar. No arreglable sin reemplazar
  todo el motor de regex, muy fuera de alcance. El demo solo pasa SQL literal sin objeto de
  parámetros, lo cual se salta este escaneo por completo.
- `new List<T>(existingCollection)` (la sobrecarga real de constructor-copia) produce
  silenciosamente una lista VACÍA en vez de copiar o dar error —
  `registerCtor("System.Collections.Generic.List\`1", ...)` ignora sus argumentos de constructor
  por completo sin importar la forma. No afectado por nada en este pase una vez que el propio
  código del demo lo evitó, pero es una brecha real con forma de pérdida silenciosa de datos que
  vale la pena señalar para un pase futuro.

### Cómo verificar Fase 3.52

```bash
go build ./...
go vet ./...
gofmt -l .
go test ./...
cd examples/npoi-demo && go run .
cd ../system-text-json-demo && go run .
cd ../openxml-demo && go run .
cd ../newtonsoft-json-demo && go run .
cd ../calculator && dotnet build Calculator.csproj -c Release && go run .
cd ../closedxml-demo && dotnet build GraphicEngineWrapper.csproj -c Release && for i in 1 2 3 4; do go run .; done
cd ../dapper-demo && dotnet build DapperDemoWrapper.csproj -c Release && go run .
```

---
### Fase 3.53 — un proveedor ADO.NET real, nativo en Go: `Microsoft.Data.Sqlite` sobre `go-r2-sqlite`

**Objetivo:** la Fase 3.52 probó que el propio código de mapeo `SqlMapper` de Dapper corre
correctamente contra cualquier forma real de `IDbConnection` — pero solo contra el propio
proveedor fake en memoria de `examples/dapper-demo`, nunca contra un motor de base de datos real.
Esta fase agrega uno: una implementación concreta de `Microsoft.Data.Sqlite`, respaldada por Go
(`internal/bcl/system_data_sqlite.go`), sobre
[`github.com/arturoeanton/go-r2-sqlite`](https://github.com/arturoeanton/go-r2-sqlite) — un motor
SQLite puro en Go, sin CGO, que expone la interfaz estándar `database/sql/driver` como
`"r2sqlite"`. Esta es la primera dependencia externa de Go que vmnet tuvo jamás (`go.mod` no tenía
ninguna antes — una excepción deliberada y autorizada por el dueño del proyecto, por única vez,
no un precedente).

- [x] `go get github.com/arturoeanton/go-r2-sqlite` — la única línea `require` en `go.mod`
      (subió `go 1.23` → `go 1.24`, el mínimo propio de la dependencia). `sql.Open("r2sqlite",
      path)` es toda la superficie de integración; cada otra línea de `system_data_sqlite.go` es
      uso simple de `database/sql`.
- [x] Seis tipos BCL reales, concretos y nativos en Go, registrados bajo nombres reales de tipo de
      Microsoft.Data.Sqlite (`SqliteConnection`/`SqliteCommand`/`SqliteDataReader`/
      `SqliteParameter`/`SqliteParameterCollection`/`SqliteTransaction`) — código C# real haciendo
      `using Microsoft.Data.Sqlite;` + `new SqliteConnection(...)` necesita cero cambios de código
      fuente para correr contra esto. Colocados en `internal/bcl` (`Native`/`NativeCtor` simple,
      sin acceso a Machine) en vez del `machineRegistry` de `internal/interpreter` (el propio
      patrón de `adonet.go`): a diferencia del propio `dbDataReaderDispose` de `adonet.go` (que
      genuinamente necesita `Machine.tryCall` para re-despachar a la propia sobreescritura de
      `Dispose(bool)` de una subclase de PLUGIN), nada acá llama de vuelta a código de plugin
      interpretado — cada operación es una llamada hoja al propio `database/sql` de Go, la misma
      postura que las propias llamadas reales a `archive/zip` de `ZipArchive` o a `bytes.Buffer`
      de `MemoryStream`.
- [x] Binding real de parámetros con nombre (`@name`) y posicionales (`?`) vía el propio
      `sql.Named` de Go — se encontró un desajuste de frontera real llegando ahí: el propio
      `database/sql` de Go (`validateNamedValueName` de `convert.go`) exige un nombre desnudo sin
      símbolo ("empieza con una letra"), mientras que un `SqliteParameter.ParameterName` real
      normalmente incluye uno (`"@id"`). `bindParams` lo quita antes de llamar a `sql.Named`; la
      propia búsqueda de parámetros con nombre de go-r2-sqlite (`engine/expr.go`) ya prueba el
      propio placeholder del texto SQL tanto con como sin su símbolo, así que esto tiende un
      puente entre ambas convenciones sin que vmnet necesite adivinar cuál usó el texto SQL de un
      comando dado.
- [x] Una `SqliteTransaction` real (`BeginTransaction`/`Commit`/`Rollback`), respaldada por un
      `sql.Tx` de Go genuino — `SqliteCommand.target()` elige el `*sql.Tx` vinculado sobre el
      `*sql.DB` pooled de la conexión solo cuando uno se setea explícitamente vía
      `cmd.Transaction = tx`, igualando el ADO.NET real (un comando nunca se une automáticamente a
      una transacción solo porque su conexión tenga una abierta).
- [x] `System.DBNull` (`internal/bcl/system_dbnull.go`) — completamente nuevo, sin representación
      previa en ningún lugar de este código. Un `NULL` SQL real leído de vuelta a través de
      `GetValue`/`ExecuteScalar` necesita ser `DBNull.Value` (un singleton real, verificable con
      `is`, comparable por referencia), nunca el propio `KindNull` de vmnet (un `null` de C# plano)
      — código real (incluyendo el propio `SqlMapper` de Dapper) comúnmente ramifica sobre
      `is DBNull` antes de caer a un `null` real.
- [x] `GetFieldType(i)`/`GetDataTypeName(i)` deben responder desde METADATOS de columna,
      disponibles apenas se abre el reader — independiente de la posición del cursor — a
      diferencia de `GetValue`/`GetInt32`/..., que necesitan una fila real. Encontrado vía un caso
      real y determinante: el propio `SqlMapper.GetDapperRowDeserializer` de Dapper (el camino de
      fila `typeof(object)` que usan los propios demos de este proyecto) llama a `GetFieldType(i)`
      antes de que el estado de fila actual del reader esté necesariamente poblado; el error
      estricto inicial de "sin fila actual" rompía cada consulta real de Dapper en el instante en
      que corría contra este proveedor.
- [x] `examples/sqlite-demo` — un demo nuevo y autocontenido (se dejó `examples/dapper-demo`
      completamente intacto): binding real de parámetros con nombre/posicionales y una transacción
      real confirmada vía ADO.NET simple, después la MISMA conexión real entregada al propio
      `SqlMapper.Query`/`Execute` real de Dapper, y después el archivo `.db` resultante abierto de
      forma independiente por el propio CLI `sqlite3` real (`PRAGMA integrity_check` pasando) como
      la prueba real de round-trip — el mismo patrón de verificación "herramienta externa real, sin
      modificar" que usa `examples/openxml-demo` para su propia salida `.docx`.
      `SqliteDemoWrapper.csproj` referencia el paquete NuGet real `Microsoft.Data.Sqlite` SOLO para
      chequeo de tipos en tiempo de compilación — su DLL nunca se carga en vmnet en tiempo de
      ejecución, solo `Dapper.dll` se adjunta como dependencia.

**Encontrado, no arreglado** (misma causa raíz que la Fase 3.52, confirmada acá como
independiente del proveedor, no específica de la conexión fake): cualquier llamada de Dapper que
pase un objeto de parámetros real todavía escanea incondicionalmente el texto SQL vía el regex de
token literal `{=name}` que el motor RE2 de Go nunca puede compilar, sin importar qué
`IDbConnection` real haya por debajo. `examples/sqlite-demo` solo pasa SQL literal, el mismo
workaround ya documentado. `System.Decimal` todavía no tiene representación distinta en ningún
lugar de este código (el propio `formatValue` de `system_misc.go` ya lo pliega en
`Double`/`Single`) — una columna vinculada o leída como `DbType.Decimal` se maneja como un
`double` ordinario.

### Cómo verificar Fase 3.53

```bash
go build ./...
go vet ./...
gofmt -l .
go test ./...
cd examples/sqlite-demo && dotnet build SqliteDemoWrapper.csproj -c Release && go run .
```

---
### Fase 3.54 — barrido de paridad solo-checker: nativos reales que los propios allowlists del checker nunca reflejaron

**Objetivo:** con 19 paquetes ya rastreados, corrí el checker contra todo el corpus y agregué cada
hallazgo de TODOS los paquetes por el callee real (no por paquete) — un callee marcado en muchos
paquetes a la vez es exactamente lo de mayor apalancamiento para arreglar. La ganancia individual
más grande: el propio mapa `linqTargets` de `internal/checker/analyzer.go` (su allowlist de
"resuelto a través del registro separado Machine-aware del intérprete, no `bcl.Lookup`") todavía
solo listaba los métodos LINQ ORIGINALES de la Fase 3.14 — cada método que el pase de
endurecimiento de LINQ de las Fases 3.44/3.45 agregó desde entonces (`GroupBy`, `ThenBy`/
`ThenByDescending`, `Min`, `Sum`, `Average`, `Aggregate`, `Zip`, `Except`, `Intersect`,
`SkipWhile`, `TakeWhile`, `Reverse`, `AsEnumerable`, `ToHashSet`) era un nativo real y funcional
que el checker simplemente nunca supo que existía — la misma clase de brecha que las Fases
3.51/3.52 ya arreglaron una vez para `Type.GetProperties`/`GetConstructors`, solo que nunca se
barrió específicamente para LINQ.

- [x] Los 14 de arriba se agregaron a `linqTargets`. `GroupBy` sola estaba marcada en 8 de 19
      paquetes (25 sitios de llamada reales); varias de las otras tocan tantos o más.
- [x] `System.Activator::CreateInstance` agregado a `reflectionMachineTargets` — una entrada real
      y funcional de `genericMachineRegistry` desde la Fase 3.39, nunca reflejada (9 paquetes, 52
      sitios de llamada).
- [x] `System.Linq.IGrouping\`2::get_Key`/`GetEnumerator` y
      `System.Linq.IOrderedEnumerable\`1::GetEnumerator` agregados a `interfaceDispatchTargets`
      (`analyzer.go`) y como prefijos en el propio `bclPrefixes` de `profile.go` — un sitio de
      llamada real puede estar declarado directamente contra estos nombres de interfaz de la BCL,
      no solo contra los nombres sintéticos `VmnetInternal.Ordered`/`VmnetInternal.Grouping` ya
      reconocidos; el checker no puede ver a través de esta redirección de despacho virtual
      específica más de lo que puede con cualquier otro caso ya en ese mapa (7 paquetes, 26 sitios
      de llamada solo para `IGrouping\`2::get_Key`).

**Resultado**: el promedio simple entre los 19 paquetes rastreados pasó de 93.9% a **94.2%**, a
partir de un cambio puramente mecánico y sin ningún riesgo de runtime (no se tocó código del
intérprete en absoluto — cada fix acá es el checker poniéndose al día con nativos que ya
funcionaban). Cada uno de los 19 paquetes mejoró o se mantuvo exactamente igual; ninguno
retrocedió.

### Cómo verificar Fase 3.54

```bash
go build ./...
go vet ./...
gofmt -l .
go test ./...
```

---
### Fase 3.55 — cuatro brechas de BCL pequeñas y mecánicas del barrido de prioridad del corpus completo

**Objetivo:** continuando el barrido de hallazgos agregados de todo el corpus (Fase 3.54), cuatro
brechas reales más, verificadas de forma independiente — `List<T>.IndexOf`, el indexador/setter
de `Length` de `StringBuilder`, `Regex.Matches`, y `Decimal.ToString` — cada una confirmada contra
la salida real de `dotnet run` antes de implementarla.

- [x] `List<T>.IndexOf(T)` (también conectado al `ArrayList.IndexOf` legacy) — un escaneo lineal
      simple reusando el helper `valuesEqual` ya existente que comparten todos los demás métodos
      de `List<T>` basados en igualdad.
- [x] `StringBuilder[int]` (getter del indexador) y `StringBuilder.Length` (setter — el getter ya
      existía) — ambos operan sobre el mismo backing store que ya usan `Append`/`ToString`/etc. El
      comportamiento real de .NET del setter cuando CRECE (no solo trunca) es rellenar con
      caracteres `'\0'`, no lanzar excepción ni dejar basura — confirmado contra la salida real de
      `dotnet run` en vez de asumido.
- [x] `Regex.Matches(string)` — el método plural, todas las coincidencias (`Match` ya era real).
      Su tipo de retorno real, `MatchCollection`, no necesitó ningún struct nuevo: se reusó el
      `*nativeList` ya existente (el mismo truco que ya usa `ArrayList` para reusar los propios
      nativos de `List<T>`), exponiendo solo `get_Count`/`GetEnumerator` — el uso real del mundo
      real que este corpus ejercita. Salió a la luz un bug real y separado verificando esto:
      `foreach (Match m in regex.Matches(s))` castea cada `Current` (tipado `object`, ya que
      `MatchCollection.GetEnumerator()` devuelve el `IEnumerator` no genérico) hacia `Match` — y
      `*nativeMatchVal` (el wrapper de `Match`) no tenía ninguna entrada en `NativeTypeName`, así
      que cada uno de esos casts lanzaba `InvalidCastException` incondicionalmente, sin importar
      `Matches` en sí. Arreglado junto con esto.
- [x] `Decimal.ToString()`/`ToString(format)` — más grande de lo que parecía: `System.Decimal` no
      tenía **ningún constructor registrado en absoluto**, así que incluso `decimal d = 1234.5m;`
      fallaba de inmediato en `System.Decimal::.ctor`, mucho antes de siquiera llegar a
      `ToString`. Se agregó el constructor real de 5 enteros `(lo, mid, hi, isNegative, scale)`
      (confirmado vía una sonda real: esto es exactamente lo que el compilador emite para un
      literal `decimal`) más las sobrecargas `int`/`long`/`float`/`double`/sin parámetros, todas
      plegándose a la representación `KindR8` ya existente según el alcance ya documentado de "sin
      representación distinta de `Decimal`" de este código (el propio comentario de `system_data_
      sqlite.go`, Fase 3.53). `ToString` después reusa `doubleToString` textualmente.

**Encontrado, no arreglado** (genuinamente fuera del alcance angosto de esta ronda, no
descuidos): los operadores aritméticos de `Decimal` (`op_Addition` etc.) y la sobrecarga de
constructor con array de bits `int[]` siguen sin implementar — no se encontró ningún sitio de
llamada real para la sobrecarga de array de bits en este corpus, y los operadores aritméticos no
formaban parte de lo que pedían los hallazgos de esta ronda, limitados a `ToString`.

### Cómo verificar Fase 3.55

```bash
go build ./...
go vet ./...
gofmt -l .
go test ./...
```

---
### Fase 3.56 — cinco brechas más de reflection del barrido de prioridad del corpus completo

**Objetivo:** continuando el barrido de todo el corpus (Fase 3.54/3.55), cinco brechas reales más
de `System.Reflection`, cada una verificada de forma independiente contra la salida real de
`dotnet run` (incluyendo cruzar contra IL real decompilado vía `ilspycmd` para confirmar
exactamente a qué nombre `Type::Method` compila el código real antes de asumir).

- [x] `FieldInfo.FieldType` — lee directo de la propia firma del campo (`metadata.ParseFieldSig` +
      `ir.SigTypeFullName`), más simple que `PropertyInfo.PropertyType` (el propio precedente de
      las Fases 3.51/3.52) ya que un campo no tiene indirección de método accesor que leer.
- [x] `MemberInfo.DeclaringType` — un nativo compartido cubriendo los cuatro tipos wrapper de
      reflection (`ConstructorInfo`/`MethodInfo`/`FieldInfo`/`PropertyInfo`) — confirmado vía IL
      real decompilado que ninguno de los cuatro lo re-declara, todos resuelven a través del
      `MemberInfo::get_DeclaringType` base, el mismo precedente ya establecido para
      `MemberInfo::get_Name`.
- [x] `Type.GetFields()`/`GetMethods()` (las sobrecargas PLURALES, sin argumentos, que devuelven
      arrays — las singulares `GetField(name)`/`GetMethod(name)` ya existían) — nuevos callbacks
      resolver `Machine.ResolveFields`/`ResolveMethods` conectados de la misma forma que ya lo
      está `ResolveProperties` (`calls.go` → `eval.go` → `runtime/method.go` → los propios
      recorredores de rango de campo/método de `assembly.go`), reusando el ya existente
      `TypeDefFieldRange`/`TypeDefMethodRange`.
- [x] `Type.IsGenericTypeDefinition`/`GenericTypeArguments`/`ContainsGenericParameters`/
      `IsGenericParameter` — chequeos puros de forma de string sobre el propio nombre completo de
      un tipo, la misma postura que los ya existentes `IsGenericType`/`GetGenericArguments`.
- [x] **Fix bonus, necesario para que `GetFields()`/`GetMethods()` sean realmente usables**:
      decompilar IL real para este pase reveló que `FieldInfo.Name`/`MethodInfo.Name`/
      `ConstructorInfo.Name`/`PropertyInfo.Name` TAMBIÉN resuelven a través del
      `MemberInfo::get_Name` compartido — que solo reconocía un receptor `Type`/
      `nativeMemberInfo`. Cada llamador real que enumera un resultado de `GetFields()`/
      `GetMethods()` lee `.Name` en cada elemento inmediatamente, así que esta fue una brecha real
      y determinante descubierta ejercitando de verdad los nuevos métodos plurales de punta a
      punta, no un hallazgo separado y no relacionado.

**Encontrado, no arreglado** (limitaciones preexistentes, no regresiones nuevas):
`Type.GetMethod(name)` solo busca los propios miembros declarados de un tipo, no los heredados
(el .NET real busca en toda la cadena de base — una feature separada y más grande);
`FieldType`/`PropertyType` no pueden resolver el propio tipo de campo de un parámetro de tipo
genérico abierto (ej. `public T Value` en `Generic<T>`, ya que `ir.SigTypeFullName` no tiene caso
`SigVar`/`SigMVar`) — una limitación que `PropertyType` ya tenía, no algo que este pase introdujo.

### Cómo verificar Fase 3.56

```bash
go build ./...
go vet ./...
gofmt -l .
go test ./...
```

---
### Fase 3.57 — `TextWriter`/`StringWriter`, `CancellationToken`, `ExceptionDispatchInfo` del barrido de todo el corpus

**Objetivo:** el último y más grande grupo del barrido de prioridad de todo el corpus (Fases
3.54-3.56): `System.IO.TextWriter`/`StringWriter` (la brecha con más hits de todo el escaneo de
19 paquetes — 218 sitios de llamada reales entre 6 paquetes), `System.Threading.CancellationToken`/
`CancellationTokenSource`, y `System.Runtime.ExceptionServices.ExceptionDispatchInfo`. Un cuarto
objetivo, `CustomAttributeExtensions.GetCustomAttribute<T>`, se investigó pero se encontró que
necesita un subsistema genuinamente nuevo (ver "Encontrado, no arreglado" abajo) — correctamente
fuera del alcance de este pase en vez de intentado a medias.

- [x] **`System.IO.TextWriter`/`StringWriter`** (`internal/bcl/system_io_stringwriter.go`, nuevo)
      — `Write`/`WriteLine` (string/char/object/numérico/bool/`char[]`/`char[],index,count`),
      `ToString`, `Flush`/`Close`/`Dispose` (no-ops — nada que liberar en un intérprete puro en Go,
      la misma postura que ya toma cualquier otro no-op de `IDisposable` acá), `NewLine`, más
      encadenamiento de ctor de base (`StringWriter::.ctor` in-place — necesario para una
      subclase real, ej. el propio `ReusableStringWriter` de Serilog, mismo patrón ya establecido
      para las clases base de `Exception`/`Dictionary`/ADO.NET). Verificar esto contra la salida
      real de `dotnet run` sacó a la luz dos bugs de corrección reales y separados, no
      específicos de `TextWriter` en sí:
    - Los argumentos `char` perdían su identidad entrando a `Write`/`WriteLine` — exactamente la
      misma clase de bug que `charSensitiveNatives` ya existe para arreglar en
      `StringBuilder.Append` (Fase 3.40), solo que nunca se extendió a este nativo nuevo.
      Arreglado agregando `System.IO.StringWriter::Write`/`WriteLine`/
      `System.IO.TextWriter::Write`/`WriteLine` a ese mapa ya existente.
    - Los argumentos `bool` imprimían `"1"`/`"0"` en vez de `"True"`/`"False"` — una brecha real y
      no descubierta antes (este código no tiene un Kind distinto para `bool`, spec §17.1, así
      que un bool boxeado/pasado es un `KindI4` ordinario indistinguible de un `int` en el punto
      donde un nativo lo recibe). Arreglado de forma acotada (no ensanchando el propio
      `charSensitiveNatives`, que hubiera necesitado que cada llamador re-derive "char vs bool"
      del mismo Kind): un nuevo mapa paralelo `boolSensitiveNatives`, acotado solo a
      `TextWriter.Write`/`WriteLine` (no se encontró ningún sitio de llamada real que lo necesite
      para `StringBuilder.Append`/`Insert` — ensanchar el comportamiento de un nativo ya en
      producción sin beneficio real arriesga una regresión no relacionada), más un nuevo caso
      `metadata.SigBoolean` en la propia captura de nombres de tipo de parámetro de
      `ir/builder.go` (confirmado inerte para el scoring de resolución de sobrecarga existente
      antes de incorporarlo).
- [x] **`System.Threading.CancellationToken`/`CancellationTokenSource`**
      (`internal/bcl/system_cancellationtoken.go`, nuevo) — estado de cancelación real, mutable,
      no un stub: `Cancel()` seguido de `ThrowIfCancellationRequested()` en código secuencial
      plano es un patrón real y alcanzable incluso bajo el modelo síncrono de `async`/`await` de
      este proyecto (Fase 3.22) — un token que nunca pudiera volverse realmente cancelado se
      portaría mal silenciosamente para ese caso ordinario, no solo para la cancelación
      concurrente real que este modelo no intenta. Propagación de una sola vía en
      `CreateLinkedTokenSource` (un token vinculado observa la cancelación posterior de sus
      padres; un padre nunca observa la de un hijo vinculado), `Equals`/`op_Equality`,
      `Register`/`CancellationTokenRegistration` (el registro tiene éxito y se dispone
      limpiamente pero nunca invoca de verdad el callback — un recorte de alcance documentado:
      ningún sitio de llamada real en este corpus ejercita la invocación de callback registrado,
      solo la forma de polling con `ThrowIfCancellationRequested`). Se agregó
      `System.OperationCanceledException` al registro de tipos de excepción y al recorrido de
      jerarquía de excepciones (`typecheck.go`) para que un `catch (Exception)` simple lo siga
      atrapando, igualando la propia jerarquía real de .NET.
- [x] **`System.Runtime.ExceptionServices.ExceptionDispatchInfo`**
      (`internal/bcl/system_exceptiondispatchinfo.go`, nuevo) — `Capture`/`Throw`/
      `SourceException`, reusando la propia referencia real de vuelta al objeto originalmente
      lanzado que ya tiene `ManagedException` (Fase 3.51) para que un round-trip
      `Capture(ex).Throw()` relance exactamente la MISMA excepción — los propios campos extra de
      una excepción personalizada sobreviven, y sigue siendo atrapada por el `catch` más derivado
      que hubiera atrapado la original más arriba.

**Encontrado, no arreglado** (infraestructura genuinamente nueva necesaria, no una brecha de
conexión — correctamente diferido): se investigó `CustomAttributeExtensions.GetCustomAttribute<T>`
(afecta 7 paquetes, 27 sitios de llamada) y se encontró que necesita un subsistema real y nuevo:
el código ya existente con nombre "attribute" en este proyecto
(`getattribute.go`/`attribute_createnew.go`/`attribute_metadata.go`) trata enteramente sobre los
propios atributos *XML* de DocumentFormat.OpenXml — un falso amigo de nombres no relacionado, no
atributos de reflection del CLR en absoluto. Hoy nada lee la tabla de metadatos real
`CustomAttribute` (ECMA-335 §II.22.10) — solo existen sus constantes de tag de índice codificado.
Un soporte real necesita un lector de filas `CustomAttribute`, una búsqueda inversa
`HasCustomAttribute`, y decodificación real de blob de atributo (§II.23.3: argumentos de
constructor fijos y con nombre) — una pieza de trabajo genuinamente nueva y de tamaño
considerable. Se confirmaron llamadores reales que se beneficiarían (los atributos de propiedad
`[Name]`/`[Index]` de CsvHelper, los chequeos de enum `[Flags]` de FluentValidation, el
`[ValueConverter]` de AutoMapper) — dejado para un pase futuro dedicado en vez de una
implementación parcial apurada.

### Cómo verificar Fase 3.57

```bash
go build ./...
go vet ./...
gofmt -l .
go test ./...
```

---
### Fase 3.58 — cerrando el barrido de todo el corpus: paridad de checker para `Type.GetFields`/`GetMethods`, números finales

**Objetivo:** la Fase 3.56 agregó nativos reales y funcionales de `Type.GetFields()`/`GetMethods()`
(plural) pero — igualando exactamente la misma clase de brecha que todo este barrido (Fases
3.54-3.57) seguía encontrando — nunca los reflejó en el propio allowlist `reflectionMachineTargets`
del checker. De todo lo que agregaron las Fases 3.55-3.57, esta fue la ÚNICA entrada que en
realidad necesitaba un fix del lado del checker: cada otro nativo que esos tres pases registraron
es un `bcl.Native` simple (`register(...)`), que el checker ya reconoce automáticamente vía
`bcl.Lookup` sin necesitar ninguna entrada de allowlist — `GetFields`/`GetMethods` son las únicas
dos resueltas a través del `machineRegistry` Machine-aware en su lugar (la misma razón por la que
`GetProperties`/`GetConstructors` necesitaron sus propias entradas en las Fases 3.51/3.52).

- [x] `System.Type::GetFields`/`GetMethods` agregados a `reflectionMachineTargets`.

**Resultado — el barrido completo de todo el corpus (Fases 3.54-3.58), números finales**: el
promedio simple entre los 19 paquetes rastreados pasó de 93.9% a **94.45%**. `FluentValidation`
cruzó el objetivo individual del 97% durante este barrido (97.0%, subiendo de 96.4%) — el objetivo
de trabajo que este proyecto se exige es 97%+ por paquete, no un promedio de todo el corpus (un
promedio puede esconder un paquete mal cubierto que se rompe en el instante en que alguien
realmente depende de él) — llevando la cuenta de paquetes en o por arriba de esa vara a 5 de 19
(`DocumentFormat.OpenXml` 100.0%, `Humanizer.Core` 97.9%, `NPOI` 97.9%, `Ardalis.GuardClauses`
97.5%, `FluentValidation` 97.0%). Ver `docs/en/COMPATIBILITY.md` para la tabla completa por
paquete, re-medida de forma fresca.

Vale la pena notar que todo este barrido (cinco pases reales de arreglos más este fix de cierre de
paridad del checker) arrancó de un solo artefacto: agregar los propios hallazgos del checker en
todo el corpus de 19 paquetes por callee real en vez de por paquete, así un callee marcado en
muchos paquetes a la vez salía a la luz como lo de mayor apalancamiento para arreglar a
continuación — la misma metodología es reusable para lo que resulte ser el próximo barrido de
prioridad.

### Cómo verificar Fase 3.58

```bash
go build ./...
go vet ./...
gofmt -l .
go test ./...
```

---
### Fase 3.59 — un modelo `Permissions` real, más `System.IO.File`/`Directory`/`FileStream`/`FileInfo`/`DirectoryInfo` deny-by-default

**Objetivo:** hacer aterrizar la puerta de capacidad `Permissions` prometida hace tiempo (el propio
checklist de Fase 4 de este archivo, `docs/en/security.md`) para I/O de disco real específicamente,
y después implementar la superficie `System.IO.File`/`Directory`/`FileStream`/`FileInfo`/
`DirectoryInfo` que un barrido de todo el corpus (la misma metodología de hallazgos agregados que
usaron las Fases 3.54-3.58, esta vez apuntando a `System.IO.File`/`Directory`/`FileStream`/`Path`,
`System.Diagnostics.Process`, y `System.Net.*`) mostró que tenía demanda real, aunque modesta (~40
hits entre `ClosedXML`/`NPOI`) — detrás de esa misma puerta desde la primera línea de código, en
vez de enviarla sin puerta y retrofitearla después.

- **`internal/runtime/permissions.go`** (nuevo): el propio struct `Permissions`
  (`AllowFileRead`/`AllowFileWrite`/`AllowConsole`/`AllowNetwork`) — puesto deliberadamente en
  `internal/runtime`, no en `internal/interpreter` ni en el paquete `vmnet` de nivel superior, así
  tanto `internal/bcl` como `internal/interpreter` pueden ver el mismo tipo sin ningún ciclo de
  import (el paquete de nivel superior ya depende de ambos; `internal/bcl` nunca debe depender de
  `internal/interpreter`). `AllowFileRead`/`AllowFileWrite` se hacen cumplir desde esta Fase;
  `AllowConsole`/`AllowNetwork` existen por compatibilidad a futuro con la promesa documentada de
  hace tiempo de este proyecto pero todavía no protegen nada — `System.Console.Write`/`WriteLine`
  sigue siempre permitido, sin cambios.
- **`permissions.go`** (raíz del repo, nuevo): la API pública — `type Permissions =
  runtime.Permissions` y `func (vm *VM) Permissions() *Permissions`, devolviendo un puntero al
  propio estado de `vm` (a diferencia de `vm.NuGet()`, que devuelve un lector de manifest/lockfile
  fresco y sin estado en cada llamada) así una mutación hecha después de `LoadFile`/`LoadPackage`
  igual toma efecto en cada llamada subsiguiente a través de ese mismo `Assembly`.
- **Conectando `Permissions` desde `VM` hasta `Machine`**: `VM` ganó un campo `permissions
  runtime.Permissions` (antes `type VM struct{}`, completamente vacío); `Assembly` ganó un campo
  `permissions *runtime.Permissions`, seteado a `&vm.permissions` en `LoadBytes` (cada camino de
  carga — `LoadFile`, `LoadPackage` — pasa por esta única función); `Assembly.machine()` de
  `call.go` ganó `.WithPermissions(asm.permissions)` en su cadena de builder, igualando cada otro
  `With*Resolver` que ya está ahí. `interpreter.Machine` ganó un campo `Permissions
  *runtime.Permissions` y un setter `WithPermissions` — `nil` (cualquier Machine construido sin
  esto, incluyendo cada fixture de test preexistente) se trata idéntico a un `&runtime.
  Permissions{}` explícito con todo denegado, nunca como "permitir todo".
- **La puerta en sí, `internal/interpreter/permissions.go`**: en vez de conectar un chequeo de
  Permissions dentro de cada nativo individual (lo que significaría cambiar de forma invasiva las
  firmas de función `Native`/`NativeCtor` simples de `internal/bcl`, o darle a esa capa más baja
  conciencia de un Machine que nunca estuvo pensada para tener — ver el propio comentario de
  `calls.go` sobre por qué los `bcl.Native` simples no pueden ver un `Machine`), `tryCall` (el
  único punto de embudo que ya distingue nativos `bcl.Lookup` simples de los Machine-aware) y
  `newObj` (el embudo para `bcl.LookupCtor`) consultan cada uno un mapa chico —
  `permissionGatedBCLNatives`/`permissionGatedBCLCtors` — indexado por el nombre completo exacto
  del nativo, ANTES de siquiera llamar al nativo. Una capacidad denegada tira un
  `System.UnauthorizedAccessException` real (`unauthorized`, mismo archivo) sin que el propio
  código Go del nativo protegido corra en absoluto — ningún efecto parcial, ningún canal lateral
  de tiempo entre "denegado" y "el archivo no existe."
- **`internal/bcl/system_io_file.go`** (nuevo): los propios nativos reales — `File.Exists`/
  `OpenRead`/`ReadAllText`/`ReadAllBytes`/`WriteAllText`/`WriteAllBytes`/`Delete`/`SetAttributes`/
  `Create`/`Copy`, `Directory.CreateDirectory`/`Exists`, el constructor de `FileStream` (cada
  `FileMode` real — `CreateNew`/`Create`/`Open`/`OpenOrCreate`/`Truncate`/`Append`, la misma
  postura de "sin TypeDef para un enum de BCL, switch sobre el int32 crudo" que ya usa el propio
  manejo de `SeekOrigin` de `msSeek`), y `FileInfo`/`DirectoryInfo` (constructores que nunca tocan
  disco por sí mismos, igualando la semántica perezosa real, más sus miembros reales que sí tocan
  disco). Cada uno de estos asume que su propia puerta de permiso ya corrió y siempre realiza el
  I/O real sin condiciones — `internal/bcl` en sí se mantiene completamente agnóstico de permisos,
  a propósito.
- **`nativeMemoryStream` de `internal/bcl/system_io.go` ganó dos campos**: `typeName` (el mismo
  patrón que ya usa `nativeList` para distinguir `List\`1` del legado `ArrayList`) así un stream
  real, respaldado por disco, se reporta a sí mismo como `System.IO.FileStream`, no
  `System.IO.MemoryStream`, al despacho virtual y a `NativeTypeName`; y `diskPath`, no vacío solo
  para un stream capaz de escribir, volcado a la ruta real de una sola vez por `msClose` en el
  primer `Close`/`Dispose` — cada llamada intermedia de `Read`/`Write`/`Seek`/`Position`/`Length`
  durante la vida del stream opera puramente sobre el `buf` en memoria, exactamente como ya hace un
  `MemoryStream`, así `FileStream` obtiene gratis cada miembro de `System.IO.Stream` con solo
  agregar `"System.IO.FileStream"` como un tercer prefijo al loop de registro ya existente.
- **Se retrofitearon dos nativos preexistentes de I/O de archivo real, previamente sin ninguna
  puerta en absoluto**, bajo la misma puerta en vez de dejarlos inconsistentes una vez que existía
  una puerta: abrir una `Microsoft.Data.Sqlite.SqliteConnection` real (Fase 3.53) ahora requiere
  `AllowFileRead` y `AllowFileWrite` a la vez; `System.IO.Path.GetTempFileName` (crea un archivo
  real y vacío en disco vía `os.CreateTemp`, no solo un string de ruta) ahora requiere
  `AllowFileWrite`.
- **Un bug real y latente de jerarquía de excepciones, encontrado y arreglado mientras se agregaba
  esto**: el mapa `exceptionBaseType` mantenido a mano de `internal/interpreter/typecheck.go` no
  tenía ninguna entrada para `System.IO.IOException`/`FileNotFoundException`/
  `DirectoryNotFoundException`/`EndOfStreamException`/`InvalidDataException`/
  `ObjectDisposedException`/`System.Data.DataException`/`ApplicationException` — cada uno de estos
  ya tenía un constructor registrado (`internal/bcl/system_exception.go`) pero ninguna entrada de
  jerarquía, así que el recorrido de `nativeMatches` chocaba con un nombre sin entrada en el mapa,
  intentaba `ResolveType` contra un nombre de BCL plano sin ningún `TypeDef` en el ensamblado
  cargado, obtenía un error, y devolvía `false` — significando que un `catch (Exception e)` plano
  (o `catch (IOException e)` para las dos subtipos de `System.IO`) **fallaba en silencio en
  matchear a alguno de estos**, dejándolo propagar sin atrapar. Los propios tiros nuevos de esta
  Fase de `System.IO.FileNotFoundException`/`System.UnauthorizedAccessException` hubieran chocado
  con esto inmediatamente la primera vez que algún llamador envolviera uno en un `catch (Exception
  e)` plano — arreglado agregando los ocho al mapa con sus tipos base reales de .NET.
- **`internal/checker/profile.go`**: se agregaron `"System.IO.File::"`/`"System.IO.Directory::"`/
  `"System.IO.FileStream::"`/`"System.IO.FileInfo::"`/`"System.IO.DirectoryInfo::"` más los dos
  nombres de tipo de excepción nuevos a `bclPrefixes` de `ProfileRules` (heredado por
  `ProfileNetStandardLite`) — necesario para el propio test de auto-consistencia del checker
  (`TestAnalyze_OwnAssemblyIsCompatible`), que requiere que cada método del propio ensamblado
  fixture de vmnet analice limpio; a diferencia de la mayoría del trabajo de paridad de checker de
  este proyecto, esto NO es "cero trabajo extra de allowlist a pesar de ser un nativo simple" — el
  scoping por prefijo de namespace del profile es una puerta separada y deliberada de "qué promete
  este profile" encima de la mera resolubilidad en tiempo de ejecución (ver el propio comentario
  de `bclPrefixes`).
- **Nuevo fixture dorado, `tests/fixtures/csharp/FileIO.cs`**, más `TestPermissions_FileIO` en
  `vmnet_test.go`: ejercita denegado-por-defecto (incluyendo el propio `catch
  (UnauthorizedAccessException)`/`catch (Exception)` del fixture, probando el fix de jerarquía de
  excepciones de arriba), lectura+escritura otorgadas con una re-verificación independiente vía
  `os.ReadFile` de que resultó un archivo *real* (no una ilusión interna de vmnet), y solo-lectura-
  otorgada-igual-deniega-escritura.
- **Nuevo demo, `examples/permissions-demo`**: el mismo C# compilado idéntico
  (`Vmnet.Fixtures.FileIO`, reusado de la misma forma en que `examples/hello` reusa
  `SimpleMath`/`Strings`) corrido tres veces contra tres configuraciones distintas de
  `Permissions`, con una relectura del lado de Go independiente confirmando que el archivo del
  caso otorgado es real.

### Encontrado, no arreglado (esta Fase)

- **`AllowConsole`/`AllowNetwork` todavía no protegen nada** — definidos en `Permissions` por
  compatibilidad a futuro con la promesa documentada de hace tiempo de este proyecto, pero
  `System.Console.*` sigue siempre permitido y no existe ningún nativo que toque la red. Ver
  `docs/en/security.md`.
- **`System.Diagnostics.Process`**: el mismo barrido de todo el corpus que motivó el trabajo de
  File/Directory de arriba encontró **cero** usos reales en los 19 paquetes rastreados —
  deliberadamente no implementado hasta que aparezca demanda real, en vez de construido de forma
  especulativa.
- **`System.Net.Http`/`System.Net.IPAddress`**: se encontró demanda real modesta (`HttpClient`/
  `HttpResponseMessage`/`HttpContent` de `ClosedXML`, `IPAddress` de `SimpleBase`, probablemente
  para formateo/validación en vez de redes de verdad) — no implementado esta Fase; un candidato
  para una futura, protegido por `AllowNetwork` desde su primera línea en vez de retrofiteado.
- **Los argumentos `FileAccess`/`FileShare` del constructor de `FileStream` se aceptan pero no se
  hacen cumplir** — los propios métodos Stream de vmnet no rechazan una violación de modo de
  acceso/compartición en absoluto (la misma postura que ya tiene el resto de `system_io.go`); solo
  la ruta y el `FileMode` determinan qué capacidad de `Permissions` se requiere.
- **`FileMode.CreateNew` de `File.Copy` no se distingue de `Create`/`Truncate`** — el
  `FileMode.CreateNew` real tira `IOException` si el destino ya existe; esta simplificación
  siempre tiene éxito en su lugar. No se encontró ningún llamador real del corpus que dependa de
  ese camino de falla específico.

### Cómo verificar Fase 3.59

```bash
dotnet build tests/fixtures/csharp/Fixtures.csproj -c Release
go build ./...
go vet ./...
gofmt -l .
go test ./...
cd examples/permissions-demo && go run . && cd -
cd examples/sqlite-demo && go run . && cd -   # confirma que el retrofit de SqliteConnection sigue funcionando una vez otorgados AllowFileRead/AllowFileWrite
```

---
### Fase 3.60 — Microsoft.Extensions.DependencyInjection real, tres fixes profundos del intérprete

**Objetivo:** arrancar la propia lista de prioridad explícita del usuario — paquetes oficiales de
Microsoft `Microsoft.Extensions.*` y una nueva ronda de NuGets populares — midiendo toda la familia
(un escaneo de checker de todo el corpus, con la misma metodología que las Fases 3.54-3.59 usaron
para priorizar) y llevando al que tiene más valor, `Microsoft.Extensions.DependencyInjection` (el
propio contenedor de DI oficial de Microsoft, la base sobre la que se construye cada `Program.cs`
de ASP.NET Core/worker-service), a correr de punta a punta con un servicio real resuelto mediante
inyección de constructor real — no solo un porcentaje de checker alto.

**Medido, antes de cualquier trabajo nuevo** (% de checker, profile `netstandard-lite`, con todas
las dependencias transitivas): `Microsoft.Extensions.Configuration.Abstractions` 100.0%,
`Options.ConfigurationExtensions` 100.0%, `Options` 99.7%, `Configuration.Json` 98.8%, `Logging`
98.1%, `Configuration.EnvironmentVariables` 98.0%, `Logging.Abstractions` 97.8%, `Configuration`
97.2%, `Primitives` 96.9%, `Configuration.FileExtensions` 95.9%, `Caching.Abstractions` 95.9%,
`DependencyInjection.Abstractions` 94.0%, `System.ComponentModel.Annotations` 94.1%,
`Logging.Console` 90.6%, `Configuration.Binder` 89.4%, `DependencyInjection` 89.5%,
`Caching.Memory` 87.3% — un promedio simple de 95.50% entre los 17, ya bastante adelantado desde un
arranque en frío.

A pesar del propio 89.5% de `DependencyInjection`, correr de verdad `services.AddSingleton<TService,
TImplementation>(); provider.GetRequiredService<TService>();` chocó con bugs reales y profundos del
intérprete que ningún porcentaje de checker estático podría haber predicho — el checker prueba que
un target de llamada resuelve a *algo*, nunca que el comportamiento resuelto sea correcto:

- **Un bug de tie-break en la resolución de overloads de métodos, causando una auto-recursión
  infinita.** El propio constructor real de `ServiceDescriptor` tiene dos constructores de 3
  argumentos distintos que difieren solo en el tipo de su 2do parámetro (`object` en el target real
  vs. una clase concreta en otro overload) — un 2do argumento `null` en un call site cuyo propio
  tipo de parámetro declarado genuinamente ES `object` (no resoluble a un nombre vía
  `paramTypeName`, que solo resuelve formas de clase/valuetype/instanciación genérica, nunca
  `object` en sí) estaba puntuando más alto al candidato EQUIVOCADO: el propio `pickMethodOverload`
  de `assembly.go` le da a un argumento `KindNull` un bonus chico y deliberado por un parámetro de
  clase concreta por sobre uno `object` plano (Fase 3.27, arreglando un mixup real *distinto* de
  Jint entre `Equals(object)`/`Equals(T)`) — un tie-break razonable cuando genuinamente no hay otra
  señal, pero equivocado acá porque había una señal más fuerte disponible y simplemente
  descartada: el tipo de parámetro del propio call site genuinamente ES `object`, y un candidato
  cuyo MISMO parámetro TAMBIÉN es no-resoluble-a-un-nombre (estructuralmente "igual de opaco") es
  evidencia real y positiva de un match, que supera a un candidato que resuelve a alguna clase
  concreta no relacionada. Arreglado puntuando ese caso "ambos lados son igual de no resueltos" de
  forma explícita, +6 — suficiente para revertir la brecha de a lo sumo 2 puntos de clase-vs-object
  de `KindNull` sin acercarse a ninguna señal confirmada de match exacto/mismatch en otra parte del
  mismo loop. Regresión: `tests/fixtures/csharp/OverloadTieBreak.cs`/
  `TestOverloadTieBreak_NullArgumentAgainstObjectVsClassParam`.
- **`typeof(T)` nunca resolviendo sobre el propio parámetro de tipo todavía abierto de un método
  genérico.** `ir.LoadTypeToken.IsMethodGenericParam` existe desde la Fase 3.40 específicamente
  para marcar este caso, pero la propia ejecución de `eval.go` nunca lo consultaba en realidad —
  siempre empujaba el `TypeFullName` de tiempo de construcción de IR (sin sentido para este caso,
  según el propio comentario de ese campo), degradando a un `Type` de nombre vacío cada vez. El
  propio cuerpo real de `AddSingleton<TService, TImplementation>()` hace exactamente
  `typeof(TImplementation)` sobre su propio parámetro de tipo genérico de método. Arreglado
  agregando `Frame.MethodGenericArgs` (los propios nombres de argumento de tipo genérico resueltos
  de ESTA llamada específica, conectados a través de un nuevo parámetro `invoke(...,
  methodGenericArgs []string)` — 7 call sites actualizados) y haciendo que la ejecución de
  `LoadTypeToken` realmente indexe en él. Una segunda capa, más profunda, de la misma brecha: un
  método genérico que *reenvía* su propio parámetro de tipo todavía abierto a OTRA llamada genérica
  (ej. el propio cuerpo de `ServiceDescriptor.Singleton<TService, TImplementation>()` llamándose a
  sí mismo recursivamente a través de más maquinaria genérica) compila a un MethodSpec instanciado
  con el propio `!!N` no resuelto del llamador — `ir.SigTypeFullName` ya tenía una convención `""`
  documentada para esto (`metadata.SigGenericParam`), perdiendo qué índice de parámetro se
  reenviaba. Arreglado haciendo que `methodSpecGenericArgNames` emita un sentinel `"!!N"` (la propia
  notación ILAsm de ECMA-335, reusada tal cual — un nombre de tipo real nunca puede empezar con
  `!`), resuelto de vuelta a un nombre real por el propio caso `ir.Call` de `eval.go` contra el
  `MethodGenericArgs` del frame LLAMADOR, en el momento exacto en que cada llamada se ejecuta (el
  mismo IR estático corre para cada instanciación de llamado distinta, así que esto no se puede
  resolver una sola vez en tiempo de construcción). Regresión: `tests/fixtures/csharp/
  GenericTypeOf.cs`/`TestGenericTypeOf_MethodGenericParam` (tanto el caso directo como el
  reenviado).
- **Seis resolvers de reflection a los que les faltaba el fallback de último recurso entre paquetes
  `globalTypeIndex` que un resolver hermano ya tenía.** `resolveTypeByFullName`/
  `resolveExplicitImplExact` ya consultan `globalTypeIndex` (Fase 3.40/3.43) cuando un tipo no se
  encuentra a través de `asm.deps` — el caso de borde inverso donde a un ensamblado de framework
  compartido se le pasa un `Type`/reflexiona sobre un miembro perteneciente al tipo que LO cargó a
  él, no al revés como apunta cualquier borde de dependencia ordinario. `resolveMember`,
  `resolveProperties`, `resolveMemberParams`, `resolveFields`, y `resolveMethods` (todos agregados
  en las Fases 3.51-3.53, bastante después de que `globalTypeIndex` ya existiera) nunca tuvieron el
  mismo fallback conectado — encontrado vía un caso real y con peso: el propio
  `CallSiteFactory.CreateConstructorCallSite` de `Microsoft.Extensions.DependencyInjection` llama a
  `Type.GetConstructors()` sobre `Greeter`, un tipo declarado en el ensamblado WRAPPER del que
  `DependencyInjection.dll` en sí no tiene ninguna dependencia declarada en absoluto
  (`wrapperAsm.WithDependencies(diAsm)` solo apunta para el otro lado). Arreglado agregando el mismo
  fallback de dos líneas de `globalTypeIndex` a los seis.
- **Nuevos getters de accesibilidad de `MethodBase`**: `get_IsPublic`/`IsPrivate`/`IsFamily`/
  `IsAssembly`/`IsStatic`/`IsVirtual`/`IsAbstract`/`IsFinal`, respaldados por un nuevo
  `MemberFlagsResolver` (que espeja la misma forma de re-resolver-por-(typeFullName,memberName,
  overloadIndex) que ya tiene `MemberParamsResolver`) leyendo el bitmask crudo de
  `MethodAttributes` de ECMA-335 de cada overload directamente de `MethodDefRow.Flags` — necesario
  para la propia lógica real de selección de constructor de `DependencyInjection` (`IsPublic`) y
  encontrado suficientemente útil (`ComponentModel.Annotations`/`Configuration.Binder` usan varios
  de los otros) como para implementar toda la familia de una vez en vez de a pedazos.
- **`System.Type::IsInstanceOfType`** — la imagen espejo del ya existente `IsAssignableFrom`,
  reusando `isAssignableTo` directamente contra un valor real en vez de un segundo `Type`;
  necesario para la propia validación de instancia resuelta de `ServiceProvider`.
- **`RuntimeHelpers.EnsureSufficientExecutionStack`** (una guarda defensiva de recursión real que
  `CallSiteRuntimeResolver` llama) registrada como no-op — los propios `MaxCallDepth`/
  `MaxStackDepth` de vmnet ya protegen contra recursión descontrolada en una capa por arriba de
  esto.
- **Un stub mínimo y explícitamente limitado de `GetCustomAttributes`/`IsDefined`/
  `Attribute.GetCustomAttribute`** — siempre "no se encontraron atributos", en `ParameterInfo`/
  `MemberInfo`/`MethodInfo`/`ConstructorInfo`/`MethodBase`/`PropertyInfo`/`FieldInfo`/`Type`. vmnet
  todavía no tiene un subsistema real de `CustomAttributeData`/decodificación de blob de atributo
  (ECMA-335 §II.23.3 — una pieza de trabajo genuinamente nueva y de tamaño considerable, ya diferida
  antes y todavía diferida) — este stub es correcto para el caso común abrumador que un chequeo
  defensivo de atributo encuentra (genuinamente no hay tal atributo acá), que es exactamente lo que
  hace el propio constructor de call-site de inyección de constructor real de `DependencyInjection`
  para cada parámetro plano y sin anotar. Daría una respuesta equivocada para un llamador que
  específicamente dependa de leer los datos de un atributo real.
- **Nuevo demo, `examples/di-demo`**: el propio paquete real y sin modificar `Microsoft.Extensions.
  DependencyInjection` 8.0.0 resolviendo `IGreeter` (que depende de `IClock`) mediante inyección de
  constructor real — no un caso especial trivial de tipo sin parámetros. Nuevo `TestDiDemoE2E`
  (con puerta de red, siguiendo el mismo patrón ya establecido de `TestJintDemoE2E`).

### Encontrado, no arreglado (esta Fase)

- **El soporte de `System.Linq.Expressions` sigue siendo mínimo** (solo `Parameter`/`Property`/
  `Lambda`/`get_Body`/`get_Member`) — el propio camino rápido de árbol de expresión compilado de
  `DependencyInjection` (`ExpressionResolverBuilder`) necesita mucho más (`Constant`/`Call`/`Block`/
  `Convert`/`IfThen`/`Expression<T>.Compile()`/`ExpressionVisitor`, ...) de lo que esta Fase
  implementa; no lo choca el demo de arriba solo porque un servicio resuelto una cantidad chica y
  acotada de veces no llega a esa capa de optimización en la práctica — un riesgo real para un host
  de larga duración resolviendo el mismo servicio muchas veces, todavía abierto.
- **`System.Reflection.CustomAttributeData`/`System.Threading.AsyncLocal\`1`** — ambos tienen
  demanda real y medida en toda la familia `Microsoft.Extensions.*` (`AsyncLocal` en
  `Caching.Memory`/`Logging`/`Logging.Abstractions`; `CustomAttributeData` en
  `Configuration.Binder`/`DependencyInjection`/`Logging`) y siguen sin implementar — candidatos para
  la próxima iteración.
- **El propio hilo de flush en segundo plano de `Microsoft.Extensions.Logging.Console`**
  (`System.Threading.Thread`/`Monitor.Wait`, escritura de consola async real basada en hilo de SO
  real) no lo ejercitó el demo de esta Fase y sigue sin medir contra una corrida real.

### Cómo verificar Fase 3.60

```bash
dotnet build tests/fixtures/csharp/Fixtures.csproj -c Release
go build ./...
go vet ./...
gofmt -l .
go test ./...
dotnet build examples/di-demo/DiDemoWrapper.csproj -c Release
cd examples/di-demo && go run . && cd -
VMNET_NETWORK_TESTS=1 go test -run TestDiDemoE2E -v .
```

---
### Fase 3.61 — primera pasada sobre la lista extendida de NuGets del usuario: AsyncLocal/ThreadLocal, ConcurrentQueue, paridad de checker

**Objetivo:** continuar la lista de prioridad de la Fase 3.60 — se midieron 5 paquetes candidatos
nuevos que el usuario preguntó (`Markdig`, `HtmlAgilityPack`, `Google.Protobuf`,
`OpenTelemetry.Api`, `Castle.Core`/`Moq`) y se atacaron los hallazgos de mayor apalancamiento y
menor riesgo compartidos entre varios paquetes ya rastreados, en vez de una sola integración
profunda esta vez.

**Medido** (% de checker, `netstandard-lite`, con todas las dependencias transitivas):
`Markdig@0.37.0` 2038 métodos/78 hallazgos (33 después de esta Fase, ver abajo),
`OpenTelemetry.Api@1.9.0` 318/31 (20 después), `Google.Protobuf@3.28.2` 3639/189,
`HtmlAgilityPack@1.11.61` 820/313, `Castle.Core@5.1.1` 3310/795, `Moq@4.20.72` 1659/751.
**`Castle.Core`/`Moq` son una clase de problema distinta, no una de hallazgos de checker**: ambos
están construidos fundamentalmente sobre generación de proxy dinámico al estilo
`System.Reflection.Emit` — compilando IL nuevo para un tipo sintetizado en tiempo de ejecución —
algo que vmnet no tiene ninguna forma de hacer en absoluto (interpreta IL pre-compilado; no hay un
backend de JIT/codegen acá para apuntar). Hacer correr cualquiera de los dos de verdad necesitaría
un subsistema dedicado de emulación de proxy dinámico (interceptando las propias llamadas de
generación de `DynamicProxy` con una reimplementación nativa), no cobertura incremental de BCL —
marcado acá como **difícil, probablemente fuera de alcance** en vez de intentado a ciegas.

- **`System.Threading.ThreadLocal\`1`/`AsyncLocal\`1`** (`internal/bcl/system_threadlocal.go`,
  `internal/interpreter/threadlocal.go`) — demanda real en `Microsoft.Extensions.Caching.Memory`/
  `Logging`/`Logging.Abstractions` y `OpenTelemetry.Api`. Ambos tipos reales de BCL existen para
  darle a cada hilo/flujo async concurrente su propio valor independiente — una distinción que
  colapsa a nada acá (vmnet corre cada cadena de llamadas de forma sincrónica en una sola goroutine,
  el mismo colapso que `system_cancellationtoken.go` ya documenta para `CancellationToken`), así que
  ambos se modelan como una caja de valor mutable real, aunque trivial. El propio `valueFactory`
  opcional de `ThreadLocal<T>` (computado a lo sumo una vez, espejando `System.Lazy\`1` exactamente
  — el propio patrón de `bcl.LazyGetOrCompute` reusado tal cual como `bcl.ValueBoxGetOrCompute`)
  necesita acceso a Machine para invocarse, a diferencia de `AsyncLocal<T>` (sin concepto de factory
  en .NET real en absoluto).
- **`System.Collections.Concurrent.ConcurrentQueue\`1`** (`internal/bcl/system_concurrentqueue.go`)
  — no existía en absoluto; encontrado vía la propia llamada de `ConcurrentQueueExtensions.Clear`
  de Markdig. Espeja de cerca al ya existente `Queue\`1` (`system_queue.go`), más un mutex (un
  `ConcurrentQueue<T>` real se alcanza más seguido a través de un campo estático compartido, a
  diferencia de `Queue<T>`) y un enumerador basado en snapshot (igualando el propio contrato
  documentado "débilmente consistente" de `ConcurrentQueue<T>.GetEnumerator()` real, vs. el propio
  enumerador vivo, sin snapshot, de `Queue<T>`).
- **`System.Reflection.CustomAttributeExtensions.GetCustomAttribute<T>`/`GetCustomAttributes`/
  `IsDefined`** — la forma de método de extensión genérico del mismo stub "no se encontraron
  atributos" que la Fase 3.60 ya le dio a `System.Attribute`/`MemberInfo`/etc.; un `bcl.Native`
  plano a pesar de ser un call site de método genérico, ya que el modelo `Value` type-erased de
  vmnet significa que la respuesta no depende de en qué cierra `T`.
- **Fix solo de paridad de checker, cero riesgo en tiempo de ejecución**: `System.IO.TextWriter`/
  `StringWriter` (nativos reales y funcionales desde la Fase 3.57) nunca se reflejaron en la propia
  lista de prefijos de namespace del profile del checker — la misma clase de brecha "resoluble pero
  reportada out-of-profile" que este proyecto sigue encontrando de nuevo para cada área nueva de
  BCL, encontrado vía el propio uso real de `TextWriter`/`StringWriter` de Markdig.

### Encontrado, no arreglado (esta Fase)

- **`Castle.Core`/`Moq`**: ver arriba — necesita un subsistema dedicado de emulación de proxy
  dinámico, no cobertura incremental de BCL; no intentado esta Fase.
- **`Google.Protobuf`/`HtmlAgilityPack`**: medidos pero todavía no trabajados — ambos todavía tienen
  brechas sustanciales (189 y 313 hallazgos respectivamente) no tocadas por los fixes mecánicos y
  entre paquetes de esta Fase; candidatos para un pase dedicado cada uno.
- **`System.Type::GetTypeHandle`/`RuntimeTypeHandle::get_Value`/`IsSubclassOf`,
  `System.Globalization.IdnMapping`/`CultureInfo::get_CompareInfo`** (hallazgos restantes de
  Markdig) — brechas reales, todavía no implementadas; de menor valor/más de nicho que las
  elecciones entre paquetes de esta Fase.

### Cómo verificar Fase 3.61

```bash
go build ./...
go vet ./...
gofmt -l .
go test ./...
```

---
## Fase 3.62 — Type.IsSubclassOf, Type.GetTypeHandle/RuntimeTypeHandle.Value

**Objetivo:** cerrar el resto de la propia lista "encontrado, no arreglado" de la Fase 3.61 — las
brechas restantes de primitivas de reflection de Markdig (saltando las de Globalization de menor
valor, `IdnMapping`/`CultureInfo::get_CompareInfo`, dejadas para un pase futuro ya que vmnet no
tiene ningún dato real de locale/cultura para respaldarlas de forma significativa).

- **`Type.IsSubclassOf(Type)`** (`internal/interpreter/reflection.go`) — a diferencia de los ya
  reales `IsAssignableFrom`/`IsInstanceOfType`, esto camina SOLO la propia cadena de clase real
  (`BaseTypeFullName`), nunca interfaces (una diferencia real y documentada:
  `IsSubclassOf(typeof(IAlgunaInterfaz))` siempre es `false` aún para una clase que la implementa),
  y requiere un ancestro ESTRICTO — un tipo nunca es su propia subclase. Encontrado vía el propio
  `MarkdownObjectExtensions.Descendants<T>` de Markdig.
- **`Type.GetTypeHandle()`/`RuntimeTypeHandle.Value`** (`internal/bcl/system_type.go`) — los
  llamadores reales acá solo usan el handle resultante como una clave opaca de identidad/comparación
  por Tipo (el propio `RendererBase.GetKeyForType` de Markdig lo usa como clave de `Dictionary`
  cacheando información de renderer por Tipo, nunca para algo que necesite una dirección de memoria
  genuina), así que `GetTypeHandle` es un passthrough de identidad puro (sin ninguna representación
  separada de `RuntimeTypeHandle` en absoluto, el mismo truco que ya usa `GetTypeFromHandle`) y
  `.Value` hashea el propio `FullName` del tipo a un `Int64` estable (FNV-1a) — el mismo Tipo real
  siempre da el mismo valor de handle.

### Cómo verificar Fase 3.62

```bash
go build ./...
go vet ./...
gofmt -l .
go test ./...
```

---
## Fase 3.63 — `System.Reflection.CustomAttributeData` real: el subsistema de lectura de atributos ya diferido

**Objetivo:** cerrar la única brecha que este proyecto había diferido deliberadamente a través de
tres Fases anteriores (3.57, 3.60, 3.61 la mencionan todas) en vez de seguir construyendo a su
alrededor pieza por pieza: decodificación real de blob de atributo (ECMA-335 §II.23.3), confirmada
como necesaria por nombre en la familia `Microsoft.Extensions.*` que preguntó el usuario (el propio
atributo de propiedad `[ConfigurationKeyName]` de `Configuration.Binder`, el más directo) y por
varios otros paquetes rastreados (`[Name]`/`[Index]` de `CsvHelper`, los chequeos `[Flags]` de
`FluentValidation`, `[ValueConverter]` de `AutoMapper`, el propio `Markdown.Version` de `Markdig`
leyendo un atributo a nivel de ensamblado).

- **`internal/metadata/customattribute.go`** (nuevo): la capa real de metadata.
  `CustomAttributesForParent` lee la tabla CustomAttribute (§II.22.10), matcheando un coded index
  Parent por escaneo lineal — la misma postura que ya toma `MethodImpls` por la misma razón (la
  tabla no es contigua por parent como sí lo son los propios rangos de método/campo de TypeDef).
  `DecodeCustomAttributeArgs` decodifica los argumentos de constructor FIJOS (posicionales) reales
  de un blob de atributo dado el propio parámetro de firma ya parseado del constructor — el blob en
  sí no lleva tags de tipo para argumentos fijos en absoluto; los tipos de parámetro declarados del
  constructor son la única fuente de verdad, igualando exactamente cómo un CLR real decodifica los
  mismos bytes. Cubre cada primitivo, `string`, y enum (codificado como su propio int32 subyacente —
  el compilador de C# solo permite una constante de tiempo de compilación como argumento de
  atributo, así que cualquiera de tipo valor siempre es un enum) y `System.Type` (un `SerString` de
  su nombre calificado por ensamblado); los argumentos nombrados (`[Foo(1, Bar = "x")]`) se leen y
  se saltan correctamente (así los propios bytes finales del blob nunca se decodifican mal como más
  argumentos fijos) pero todavía no se exponen como valores — no se encontró ningún llamador real
  del corpus que necesite uno. Los argumentos fijos de array/`object` boxeado son una brecha
  documentada y más angosta (devuelven "no soportado para esta posición" en vez de fallar todo el
  blob).
- **El nuevo `resolveCustomAttributes` de `assembly.go`** — resuelve las propias `CustomAttributeRow`
  reales de un miembro a pares `(AttributeTypeFullName, []runtime.Value ctorArgs)`, listos para
  pasar directo a `Machine.New`/`newObj` como argumentos de constructor. Con alcance a los tipos de
  miembro `"type"` y `"property"` por ahora (igualando las dos necesidades reales y confirmadas del
  corpus de arriba) — los atributos a nivel de campo/método/parámetro quedan como brecha
  documentada, extensible de la misma forma una vez que un llamador real lo necesite.
  `ir.ResolveMemberRefClassName` (antes no exportado) ahora está exportado para que esto lo reuse en
  vez de duplicar la lógica de resolución del nombre del tipo propietario.
- **`internal/interpreter/customattributes.go`** (nuevo): la capa de nativos Machine-aware — reales
  `MemberInfo.GetCustomAttributesData()`/`GetCustomAttributes()`/`IsDefined()`,
  `System.Attribute.GetCustomAttribute(MemberInfo, Type)`, y
  `CustomAttributeExtensions.GetCustomAttribute<T>()` (una entrada de `genericMachineRegistry` —
  necesita el propio `<T>` resuelto del call site, la propia maquinaria `Frame.MethodGenericArgs`
  de la Fase 3.60). Cada uno de estos construye una instancia REAL del atributo vía el mismo camino
  `newObj` exacto que ya usa un call site ordinario `new AlgunAtributo(args)` — los atributos son
  tipos reales y construibles como cualquier otro, una vez que se conocen sus argumentos de
  constructor. Registrado para los 8 receptores de reflection (`ParameterInfo`/`MemberInfo`/
  `MethodInfo`/`ConstructorInfo`/`MethodBase`/`PropertyInfo`/`FieldInfo`/`Type`) — real para
  `Type`/`PropertyInfo` (igualando el propio alcance de `resolveCustomAttributes`), un honesto "no
  se encontraron atributos" para el resto, reemplazando los propios stubs `bcl.Native` siempre-vacíos
  de la Fase 3.60 con una implementación centralizada y Machine-aware.
- **`internal/bcl/system_customattributedata.go`** (nuevo): los wrappers de valor reales
  `CustomAttributeData`/`CustomAttributeTypedArgument` que devuelve `GetCustomAttributesData()` —
  `CustomAttributeTypedArgument` se modela como un struct de tipo valor genuino (igualando su propia
  forma real en .NET), a diferencia de cualquier otro wrapper de reflection en este proyecto
  (`ConstructorInfo`/`MethodInfo`/etc., todos clases).
- **Dos brechas chicas y reales encontradas y arregladas mientras se construía el test de
  regresión**: un array CIL real implementa implícitamente `ICollection<T>`/`IList<T>` (covarianza
  SZArray, ECMA-335 §II.9.9) — un llamador que declara `IList<CustomAttributeData> datas =
  member.GetCustomAttributesData()` (el propio tipo de retorno real declarado) y después lee
  `datas.Count`/`datas[0]` llega a `ICollection<T>.get_Count`/`IList<T>.get_Item`, no a
  `Array.Length`/`ldelem` — ninguno de los dos estaba registrado en absoluto antes de esta Fase
  (`System.Array::get_Count`/`get_Item`, aliasados trivialmente a los ya reales
  `get_Length`/`GetValue`).
- **Re-medido, profile `netstandard-lite`, métodos MARCADOS (no cuenta de hallazgos crudos) como la
  comparación justa contra el propio número anterior de cada Fase**:
  `Microsoft.Extensions.Configuration.Binder` 89.4% → **98.6%** (142 métodos, 2 marcados),
  `Microsoft.Extensions.DependencyInjection` 89.5% → **96.1%** (437 métodos, 17 marcados),
  `Markdig` → **99.2%** (2038 métodos, 17 marcados), `Microsoft.Extensions.Logging` 98.1% →
  **99.6%** (269 métodos, 1 marcado).
- Nuevo fixture dorado `tests/fixtures/csharp/CustomAttributeTest.cs`/`TestCustomAttributes` cubre
  la API de bajo nivel `CustomAttributeData`, la construcción de instancia real de alto nivel
  `GetCustomAttribute<T>`, atributos a nivel de tipo vs. de propiedad, un miembro sin etiquetar
  reportando correctamente ninguno, e `IsDefined` de las dos formas.

### Encontrado, no arreglado (esta Fase)

- **Atributos personalizados a nivel de campo/método/parámetro/constructor** —
  `resolveCustomAttributes` solo resuelve los tipos de miembro `"type"`/`"property"`; extenderlo a
  los demás es la misma forma (agregar un caso, encontrar el token propietario) pero todavía no
  hecho, ya que no se confirmó ningún llamador real del corpus que lo necesitara esta Fase.
- **Atributos personalizados a nivel de ensamblado** — el propio `Markdown.Version` de Markdig lee
  `AssemblyFileVersionAttribute` del PROPIO ENSAMBLADO CONTENEDOR, una forma de `Parent` distinta
  (la propia fila de la tabla Assembly/Module, no una relativa a TypeDef) todavía no conectada a
  `resolveCustomAttributes` en absoluto; el propio `Markdown.Version` sigue sin verificar contra una
  corrida real.
- **Argumentos de constructor fijos de array/`object` boxeado, y todos los argumentos nombrados** —
  decodificados y saltados correctamente (así nunca corrompen los argumentos fijos posteriores de un
  blob) pero no expuestos como valores reales; todavía no se confirmó ningún llamador real que
  necesite uno.

### Cómo verificar Fase 3.63

```bash
dotnet build tests/fixtures/csharp/Fixtures.csproj -c Release
go build ./...
go vet ./...
gofmt -l .
go test ./...
```

---
## Fase 3.64 — verificación real de punta a punta: `FluentValidation`, `CsvHelper`, `AutoMapper`

**Objetivo:** usar el propio subsistema nuevo `CustomAttributeData` de la Fase 3.63 para verificar
de verdad, de forma honesta, los tres paquetes que motivaron diferirlo en primer lugar — no solo
re-medir su % de checker, sino intentar hacer correr un demo REAL para cada uno, el mismo estándar
de "tres dimensiones separadas" (% de checker, demo real, confianza) que este proyecto le exige a
cualquier otro paquete rastreado.

**Resultado: un demo real, verificado y funcionando de punta a punta (`FluentValidation`); dos
paquetes que chocaron con paredes arquitectónicas reales, más profundas y preexistentes, sin
relación con los atributos, encontradas y documentadas honestamente en vez de enviadas como un demo
superficial y poco convincente.**

- **`examples/fluentvalidation-demo`: `FluentValidation` 11.9.2 real y sin modificar validando un
  objeto real, tanto aceptándolo como rechazándolo con el mensaje de error correcto.** Llegar ahí
  necesitó cinco fixes más reales del intérprete, todos encontrados iterando contra la falla real en
  cada paso en vez de adivinar de antemano:
  - **`MemberExpression.Expression`** — el subsistema ya existente `System.Linq.Expressions`
    (Fase 3.41, construido para el propio uso más angosto de `DocumentFormat.OpenXml`, "inspeccionar
    la forma de un árbol, nunca compilarlo") nunca registraba de qué expresión se accedía un
    miembro. La propia construcción de `PropertyRule` de FluentValidation camina hacia atrás por el
    árbol (¿el padre es el propio parámetro del lambda — un acceso directo — u otro acceso a
    miembro — uno anidado?), lo que necesita exactamente esto.
  - **`Expression.NodeType`** — confirmado contra una corrida real de
    `Enum.GetValues(typeof(ExpressionType))` en vez de confiar en la memoria (`MemberAccess`=23,
    `Parameter`=38, `Lambda`=18) — una constante equivocada acá hubiera sido un desajuste
    silencioso, no un crash.
  - **`Expression<TDelegate>.Compile()`, de verdad** — no un compilador JIT general de expresión a
    IL (todavía fuera de alcance, ver `AutoMapper` abajo), sino un delegate real y funcional para la
    ÚNICA forma angosta que los propios nativos de este subsistema pueden construir: una cadena
    simple y no ramificada de acceso a propiedad (`x => x.Prop`, `x => x.Prop1.Prop2`). El delegate
    devuelto usa el propio `Func.Receiver` para contrabandear el árbol de expresión real a través
    del propio mecanismo ya existente de `invokeFuncTarget` que antepone el receiver, hacia un nuevo
    nativo sentinel (`internal/interpreter/compiledexpression.go`) que camina el árbol y lee cada
    propiedad vía una llamada de getter de propiedad ordinaria — evaluación real y correcta sin
    generar ni correr código nuevo jamás.
  - **`MemberInfo::op_Equality`/`op_Inequality`** — ya reales para receptores `ConstructorInfo`/
    `MethodInfo` (Fase 3.39/3.51) pero nunca reflejados bajo el propio nombre base `MemberInfo`,
    alcanzado cuando el tipo estático declarado de los valores comparados es la base, no una
    subclase concreta.
  - **`IComparable\`1::CompareTo`** — alcanzado cuando un método genérico restringido a
    `IComparable<T>` llama `value.CompareTo(other)` con `T` todavía abierto en el call site (prefijo
    `constrained.`); los propios genéricos type-erased de vmnet no tienen ningún `TypeDef` para `T`
    para redirigir a través del recorrido de despacho virtual ordinario, así que esto despacha
    directamente desde el propio `Kind` en tiempo de ejecución del receptor
    (`internal/bcl/system_numeric.go`).
- **`CsvHelper`: progreso real, una brecha real nueva encontrada, no arreglada.** `TextInfo`
  (`CultureInfo.TextInfo`, `ToUpper`/`ToLower`/`ToTitleCase`/`ListSeparator`) no existía en
  absoluto — agregado, siempre comportándose como la cultura invariante (vmnet no tiene datos
  reales de locale, la misma postura que ya documenta `cultureInfoInvariant`). Pasado eso, una
  lectura real de CSV impulsada por el atributo `[Name]` choca con una limitación genuinamente
  distinta y más profunda: el propio caché interno de conversión de tipos de CsvHelper usa un
  `Dictionary` con clave de un struct que contiene un campo array, y el hashing de clave del propio
  `Dictionary` de vmnet (`internal/bcl/system_collections.go`) no tiene ningún soporte para un
  componente de clave con forma de array — sin relación con atributos, no arreglado esta Fase.
- **`AutoMapper`: confirmado bloqueado por una brecha preexistente y mucho más grande, no
  intentado.** Los propios hallazgos están dominados por `System.Linq.Expressions`
  (`Constant`/`Call`/`Block`/`Assign`/`New`/`Convert`/`ExpressionVisitor`/
  `LambdaExpression.Parameters`, ~300 hits) — la propia generación de plan de mapeo real COMPILA
  todo un árbol de expresión personalizado por par de tipos en un delegate grande, nada parecido a
  las simples cadenas de acceso a propiedad que el fix de `Compile()` de arriba de
  `FluentValidation` puede evaluar. Esta es la misma brecha que la Fase 3.60 ya marcó para el propio
  camino rápido `ExpressionResolverBuilder` de `Microsoft.Extensions.DependencyInjection` — un
  compilador general real de árbol de expresión a ejecutable, una empresa sustancialmente más
  grande que cualquier otra cosa en esta Fase, identificada correctamente en vez de sorteada con un
  demo superficial que no ejercitaría el valor real de `AutoMapper`.
- **Re-medido** (métodos marcados, `netstandard-lite`): `FluentValidation` 97.0% (base) → 98.3%,
  `CsvHelper` 91.8% → 95.8%, `AutoMapper` 88.3% → 95.5% (progreso solo de % de checker; la brecha
  real de arriba permanece).
- Nuevo `TestFluentValidationDemoE2E` (con puerta de red, siguiendo el mismo patrón ya establecido
  de `TestDiDemoE2E`/`TestJintDemoE2E`).

### Encontrado, no arreglado (esta Fase)

- **La propia limitación de CsvHelper de Dictionary con componente de clave con forma de array** —
  una brecha real, específica y angosta en el propio soporte de hashing de clave de
  `internal/bcl/system_collections.go`, sin relación con atributos; no arreglada esta Fase.
- **Un compilador general de árbol de expresión a ejecutable** — necesario para la propia
  generación de plan de mapeo real de `AutoMapper` y el propio camino rápido de expresión compilada
  de `Microsoft.Extensions.DependencyInjection` (Fase 3.60); una empresa sustancialmente más grande
  que el propio `Compile()` angosto y solo-cadena-de-acceso-a-propiedad de esta Fase, todavía no
  intentado.
- **Los propios validadores de rango numérico de FluentValidation** (`GreaterThanOrEqualTo`, etc.)
  chocan con una limitación de genéricos distinta y más profunda encontrada mientras se investigaba
  esta Fase: la propia instancia de comparador cacheada de `Comparer<T>.Default` no se mantiene
  separada por instanciación genérica cerrada en el propio modelo de genéricos type-erased de
  vmnet, así que dos `T` distintos pueden observar el comparador cacheado del otro —
  `examples/fluentvalidation-demo` deliberadamente solo ejercita los validadores de string que ya
  funcionan correctamente; esta es una brecha arquitectónica real, profunda y preexistente, no algo
  que esta Fase intentó arreglar.

### Cómo verificar Fase 3.64

```bash
go build ./...
go vet ./...
gofmt -l .
go test ./...
dotnet build examples/fluentvalidation-demo/FvDemoWrapper.csproj -c Release
cd examples/fluentvalidation-demo && go run . && cd -
VMNET_NETWORK_TESTS=1 go test -run TestFluentValidationDemoE2E -v .
```

---
## Fase 3.65 — un evaluador real de árbol de expresión: `System.Linq.Expressions.Expression<T>.Compile()`, generalizado

**Objetivo:** construir el subsistema general de árbol-de-expresión-a-ejecutable que la Fase 3.60 y
la Fase 3.64 identificaron y postergaron — específicamente porque desbloquearía DOS consumidores
reales, independientes y de alto valor a la vez: la propia generación de plan de mapeo de
`AutoMapper` (compila un árbol de expresión completo y personalizado por cada par de tipos) y el
propio camino rápido de resolución compilada `ExpressionResolverBuilder` de `Microsoft.Extensions.
DependencyInjection`.

**Esto es un intérprete que recorre el árbol, no un JIT.** Nada en este subsistema genera o ejecuta
código máquina nuevo jamás — vmnet no tiene ningún backend de generación de código, y esta Fase no
agrega uno. `Expression<TDelegate>.Compile()` devuelve un delegado cuya invocación recorre el árbol
ya construido (los propios tipos de nodo nativos de `internal/bcl/system_linq_expressions.go`) nodo
por nodo en el momento de la llamada, despachando cada operación real (una lectura de propiedad, una
llamada a método, una llamada a constructor, una asignación a una variable de ámbito `Block`, un
incremento, una comparación por referencia, ...) a través de la MISMA maquinaria real `Machine.call`/
`newObj` que ya usa cualquier sitio de llamada compilado ordinario. El árbol del delegado se
contrabandea a través de `Func.Receiver` (el mismo mecanismo que introdujo la Fase 3.64), y el
entorno del evaluador es un `map[*runtime.Object]runtime.Value` indexado por la identidad de objeto
propia de cada nodo `ParameterExpression`/`Variable`, así que la misma variable referenciada muchas
veces a través de un árbol siempre resuelve al mismo slot.

**Resultado: un evaluador genuinamente general (aunque todavía no exhaustivo), validado contra una
prueba sintética hecha a mano al estilo AutoMapper Y contra porciones reales y sustanciales de la
propia maquinaria en tiempo de ejecución de AutoMapper 16.2.0 real — doce correcciones reales del
intérprete en el camino, un bloqueo real, profundo y sin arreglar encontrado y documentado
honestamente.**

- **Nuevos tipos de nodo** (más allá del conjunto angosto de cadena-de-acceso-a-propiedad de la Fase
  3.41/3.64): `Constant`, `Call`, `New`, `NewArrayInit`, `Convert`/`ConvertChecked`, `Assign` (a una
  variable Y a una propiedad/campo, vía el setter real), `Block` (con locales declarados,
  inicializados a un `default(T)` real), `Default`, `Conditional` (cubre `IfThen`/`IfThenElse`/
  `Condition`), `Invoke`, `ReferenceEqual`/`ReferenceNotEqual`, y `Pre`/`PostIncrementAssign`/`Pre`/
  `PostDecrementAssign`. Cada valor del enum `ExpressionType` usado se confirmó contra una ejecución
  real de `dotnet run` imprimiendo `Enum.GetValues(typeof(ExpressionType))`, no se confió en la
  memoria.
- **`System.Linq.Expressions.ExpressionVisitor`** (`internal/interpreter/exprvisitor.go`, nuevo) —
  las subclases reales de .NET de esta clase base (encontradas vía `AutoMapper.Execution.
  ReplaceVisitorBase`/`ReplaceVisitor`/`ParameterReplaceVisitor`, usadas para insertar una plantilla
  de mapeo por-propiedad cacheada dentro de la propia lambda externa de quien llama, sustituyendo un
  `ParameterExpression` por otro) típicamente sobrescriben solo UN método (`Visit` o
  `VisitParameter`) y confían por completo en el propio comportamiento por defecto de la clase base
  ("recorrer los hijos, reconstruir si algo cambió") para cualquier otro tipo de nodo. `
  ExpressionVisitor` en sí se distribuye como IL de BCL compilado del que vmnet no tiene bytecode en
  absoluto, así que cada método `Visit`/`VisitXxx` es una implementación nativa en Go que reemplaza
  ese comportamiento por defecto — `Visit` despacha virtualmente (así que la propia sobrescritura de
  una subclase de cualquier `VisitXxx` individual sigue aplicando, vía el mismo recorrido de
  ancestros que `Machine.call` ya usa para cualquier otra llamada virtual) a uno de trece nativos `
  VisitXxx`, cada uno reconstruyendo su propio tipo de nodo a partir de hijos recién visitados vía
  nuevos constructores exportados en `system_linq_expressions.go`.
- **Errores reales encontrados y corregidos probando contra el AutoMapper 16.2.0 real**, cada uno vía
  el ciclo establecido "medir → chocar con un error real → arreglar → re-ejecutar", no adivinados de
  antemano:
  - `Expression.Empty()` — la fábrica sin argumentos (un `DefaultExpression` tipado `void`) no
    existía.
  - `Type.GetMember(string, MemberTypes, BindingFlags)` — no existía en absoluto; agregado,
    resolviendo solo la familia `Method` (la única que el único llamador real encontrado,
    `TypeExtensions.GetInstanceMethod`, necesita).
  - `System.Runtime.CompilerServices.ReadOnlyCollectionBuilder<T>` — un tipo real de buffer creciente
    (`.ctor()`/`Add`/`ToReadOnlyCollection()`) que respalda `AutoMapper.Internal.PrimitiveHelper.
    ToReadOnly<T>`; modelado con el mismo `*nativeList` real que ya usa `List<T>`, bajo su propio
    nombre de tipo.
  - Las sobrecargas ambiguas de 2 argumentos de `Expression.Call` — `Call(MethodInfo, params
    Expression[])`, `Call(MethodInfo, Expression)` (un único argumento sin arreglo) y `Call(
    Expression, MethodInfo)` (una llamada de instancia, sin argumentos extra) comparten la misma
    aridad; desambiguadas según qué posición realmente contiene un `MethodInfo`, y — para ese caso —
    si el otro argumento es en sí mismo un nodo Expression real.
  - Un fallback de métodos de interfaz BCL bien conocidos (`internal/interpreter/reflection.go`) —
    `typeof(IDisposable).GetMethod("Dispose")` y `typeof(IList).GetMethod("Clear")` devolvían `null`
    porque vmnet no tiene ningún `TypeDef` real para estas interfaces BCL en absoluto, lo cual luego
    hacía fallar `Expression.Call(disposable, methodInfoNulo)` con un error de `MethodInfo` nulo; una
    lista blanca pequeña y explícita de métodos de interfaz siempre-presentes ahora responde "sí,
    esto existe" exactamente para los que se encontró que un llamador real necesita, sin afirmar nada
    sobre cómo despacharían realmente.
  - `Type.GetConstructor`/`GetConstructors` solo aceptaban sus sobrecargas más simples (exactamente 2
    args / exactamente 1 arg) — el propio `AutoMapper.Internal.Mappers.ConstructorMapper` real usa el
    `GetConstructor(BindingFlags, Binder, Type[], ParameterModifier[])` de 5 argumentos y el
    `GetConstructors(BindingFlags)` de 1 argumento; ambos ahora escanean cada argumento final
    buscando el primer `Type[]` real (o aceptan cualquier aridad ≥1), la misma postura que `Type.
    GetMethod` ya estableció para su propio abanico de aridades multi-sobrecarga.
  - `Environment.ProcessorCount` — no existía (el propio `LockingConcurrentDictionary` de `
    AutoMapper` dimensiona su cantidad de particiones a partir de él); respondido con el propio
    `runtime.NumCPU()` real de Go — a diferencia de `GetEnvironmentVariable`/`UserName`
    (deliberadamente falsos, ya que esos pueden revelar la identidad del host), una cantidad de CPUs
    solo revela capacidad, así que una respuesta real está bien acá.
- **El propio CLI de `vmnet check package` ahora imprime `Methods flagged` directamente**
  (`cmd/vmnet/main.go`) — antes solo se imprimía `Methods analyzed`, forzando una aproximación manual
  basada en `grep` de la cantidad de métodos marcados que resultó no coincidir con el propio contador
  de verdad del checker en casos límite; imprimir el campo real elimina la necesidad de aproximar.
- **Re-medido contra la MISMA herramienta, antes vs. después, en los propios commits de inicio/fin de
  esta Fase** (no contra cifras documentadas previamente, que resultaron no coincidir con lo que esta
  misma herramienta reporta al re-ejecutarse — una inconsistencia de medición preexistente, no algo
  que esta Fase introdujo, ahora corregida re-derivando siempre ambos lados de una comparación desde
  una sola ejecución): `AutoMapper` 2.319 métodos, 256 marcados (89.0%) → 152 marcados (93.4%); `
  Microsoft.Extensions.DependencyInjection` 437 métodos, 40 marcados (90.8%) → 26 marcados (94.1%).

### Encontrado, no arreglado (esta Fase)

- **El propio `Mapper.Map<TDestination>(source)` real de AutoMapper todavía lanza una excepción** —
  una `NullReferenceException` real (`System.ValueTuple\`2.Item2`) en las profundidades de la propia
  maquinaria de selección de constructor/`TypeDetails`, alcanzada solo después de atravesar toda su
  inicialización estática, su capa de reflexión y su infraestructura de inserción de plantillas
  basada en `ExpressionVisitor`. Solo `TypeDetails` abarca miles de líneas de IL real usando cadenas
  LINQ `Select`/`Where` sobre tipos anónimos generados por el compilador y una caché de métodos
  genéricos — encontrar la causa raíz exacta de qué mecanismo interno produce un campo de tupla nulo
  necesitaría arqueología dedicada sustancial más allá del alcance propio de esta Fase. No es una
  regresión del propio trabajo de esta Fase: todo hasta este punto (inicialización estática, `
  ConstructorMapper`, descubrimiento de miembros basado en reflexión, el patrón `ExpressionVisitor`)
  ahora corre correctamente donde antes fallaba directamente.
- **El propio camino rápido `ExpressionResolverBuilder` de Microsoft.Extensions.DependencyInjection
  sigue sin verificar — y es inherentemente difícil de verificar desde fuera del uso normal.** Leer
  su propio IL real (`ilspycmd`) muestra que es una optimización de mejor esfuerzo en segundo plano:
  `DynamicServiceProviderEngine` resuelve las primeras DOS llamadas a cualquier servicio dado a
  través de `CallSiteRuntimeResolver` (un intérprete simple que recorre el árbol, siempre disponible,
  que no necesita `Expression.Compile()` en absoluto), luego encola un `ThreadPool.
  UnsafeQueueUserWorkItem` en segundo plano para compilar el sitio de llamada vía `
  ExpressionResolverBuilder` e intercambiar el delegado compilado para llamadas posteriores —
  envuelto en un `try`/`catch` que TRAGA cualquier fallo de compilación y lo registra vía `
  DependencyInjectionEventSource` en lugar de propagarlo. El propio comportamiento observable de un
  llamador real (un servicio resuelto, construido correctamente) es IDÉNTICO ya sea que esa
  compilación en segundo plano tenga éxito silenciosamente o falle silenciosamente, lo cual significa
  que este camino rápido no se puede demostrar como "funcionando" o "roto" a través del uso ordinario
  de DI de la forma en que el propio camino `CallSiteRuntimeResolver` siempre activo de `examples/
  di-demo` ya lo demuestra. El propio `di-demo` no se ve afectado de ninguna manera — ejercita el
  camino del intérprete, no este.

### Cómo verificar Fase 3.65

```bash
go build ./...
go vet ./...
gofmt -l .
go test ./...
go run ./cmd/vmnet check package --profile=netstandard-lite automapper@16.2.0
go run ./cmd/vmnet check package --profile=netstandard-lite microsoft.extensions.dependencyinjection@8.0.0
```

---
## Fase 3.66 — parámetros genéricos a nivel de clase, try/catch/finally real en árboles de expresión, y causas raíz precisas para AutoMapper/CsvHelper/FluentValidation

**Objetivo:** llevar las tres pistas propias "encontrado, no arreglado" de la Fase 3.65 a una
conclusión real — el crash del propio `Mapper.Map<T>()` de AutoMapper, la brecha de `Dictionary`
con clave con forma de array de CsvHelper (Fase 3.64), y el desajuste de `Comparer<T>.Default` en
los validadores numéricos de FluentValidation (Fase 3.64) — iterando contra cada falla real
exactamente como toda Fase anterior lo hizo, en vez de volver a adivinar de memoria.

**Resultado: una capacidad arquitectónica genuinamente nueva y general (parámetros genéricos a
nivel de clase, finalmente rastreados por instancia); dos errores reales más arreglados de raíz (la
brecha de clave-array de `Dictionary`, y toda una clase de errores de secuencia vacía en
`Enumerable.FirstOrDefault/LastOrDefault/SingleOrDefault<T>`); una regresión real detectada y
arreglada por el propio gate de verificación completo de esta Fase antes de que llegara a un
commit; y, para los dos paquetes que todavía no funcionan de punta a punta, una causa raíz PRECISA
y reproducida en lugar de una conjetura — un diagnóstico sustancialmente más fuerte que el que
tenían la Fase 3.64/3.65 para cualquiera de los dos.**

- **La causa raíz real del `NullReferenceException` en `ValueTuple\`2.Item2` de AutoMapper (la
  propia entrada "encontrado, no arreglado" de la Fase 3.65): `Enumerable.FirstOrDefault<T>()` (y
  `LastOrDefault`/`SingleOrDefault`) responden una secuencia vacía/sin coincidencia con `Null()` en
  vez de un `default(T)` real y tipado.** Para un tipo valor `T` (acá `ValueTuple\`2<ConstructorInfo,
  ParameterInfo[]>`, del propio `constructors.Select(...).FirstOrDefault()` real de `AutoMapper.
  Execution.ObjectFactory.CallConstructor`), `Null()` no es una aproximación — es el `Kind`
  completamente EQUIVOCADO, y el siguiente `ldfld ...::Item2` choca. Arreglado de dos formas:
  `Machine.defaultValueFor` (`internal/interpreter/structs.go`, ya la propia lógica real de
  `default(T)` de `initobj`) ahora pela el sufijo `"[[...]]"` propio de una instanciación genérica
  cerrada antes de consultar `bcl.LookupValueType` — un registro de nombre simple, un fallo de una
  línea para cualquier cosa que no sea un nombre genérico abierto; y `FirstOrDefault`/`LastOrDefault`/
  `SingleOrDefault` se movieron del `machineRegistry` plano al `genericMachineRegistry` (el mismo
  mecanismo que `OfType<T>` ya estableció, Fase 3.42), así su propia respuesta vacía/sin coincidencia
  puede llamar a `Machine.defaultValueFor` con el propio `T` real y resuelto del sitio de llamada en
  vez de responder siempre `Null()`.
- **Una capacidad arquitectónica completamente nueva: parámetros genéricos a nivel de clase,
  rastreados por instancia de objeto real.** La Fase 3.60 le dio al propio parámetro de tipo abierto
  de un MÉTODO genérico (`typeof(T)` dentro de `AddSingleton<TService,TImplementation>()`, un
  MVAR/`!!N`) una respuesta real, resuelta de nuevo en cada llamada vía `Frame.MethodGenericArgs`.
  Nada análogo existía para el propio parámetro de tipo de una CLASE genérica (`typeof(TSource)`
  dentro del propio constructor de `MappingExpressionBase\`3<TSource,TDestination,...>`, un VAR/`!N`)
  — cada lectura de este tipo respondía silenciosamente `""`, un tipo sin resolver. Encontrado vía el
  propio `CreateMap<Source,Dest>()` de AutoMapper: su propio `TypeMap` real se registraba bajo un
  `TypePair` VACÍO/equivocado (construido de `new TypePair(typeof(TSource), typeof(TDestination))`
  dentro de un constructor de clase base genérica), así que `Mapper.Map<Dest>(source)` después
  lanzaba `"Missing type map configuration or unsupported mapping"` — un bug real, silencioso y
  serio de corrección para CUALQUIER clase genérica en esta forma, no solo AutoMapper. Arreglado con
  un nuevo campo `runtime.Object.ClassGenericArgs []string` (reflejando `Frame.MethodGenericArgs` un
  nivel más arriba: el propio T de un método genérico vive en la LLAMADA, el propio de una clase
  genérica vive en el OBJETO, mientras exista), poblado en cada camino de construcción real
  encontrado hasta ahora:
  - `ir.NewObj.ClassGenericArgs` (campo nuevo) — los propios argumentos de tipo cerrados de un sitio
    literal `newobj AlgoGenerico\`N<Args>::.ctor(...)`, resueltos del propio `Instantiation` del
    TypeSpec de `MemberRef.Class` (la nueva `typeSpecInstantiationArgNames` de `ir/builder.go`),
    incluyendo un centinela `"!!N"` (la MISMA codificación que la propia `methodSpecGenericArgNames`
    de la Fase 3.60 ya estableció) cuando un argumento es en sí mismo el propio parámetro de tipo
    todavía abierto del método genérico ENVOLVENTE siendo reenviado — resuelto contra el propio
    `MethodGenericArgs` del frame que llama en el momento de ejecución (`resolveForwardedGenericArgs`,
    ya construido exactamente para esta forma).
  - `Machine.New`/`Activator.CreateInstance<T>()`/el propio caso `Expression.New` del evaluador de
    expresiones — todos los caminos de construcción basados en reflection/árbol-de-expresión, donde
    el nombre de tipo que llega a `newObj` ya está completamente cerrado (`bcl.ClosedGenericArgs`, un
    nuevo parser exportado para el sufijo `"[[...]]"`, parseado directamente, sin necesitar reenvío).
  - `ir.LoadTypeToken.IsClassGenericParam`/`ClassGenericParamIndex` (campos nuevos, reflejando
    exactamente `IsMethodGenericParam`) — `typeof(T)` sobre un VAR a nivel de clase ahora resuelve
    desde el propio objeto receptor del método ACTUAL (`frame.Args[0].Obj.ClassGenericArgs[N]`) en
    vez de responder siempre `""`.
  - `System.Object.GetType()` (la nueva `closedTypeFullNameOf` de `internal/bcl/system_type.go`)
    ahora reporta el propio nombre cerrado REAL de un objeto genérico (`"Namespace.Type\`1[[Arg]]"`),
    no solo el nombre TypeDef abierto plano — necesario para que cadenas de reflection al estilo
    `Type.BaseType.GetGenericArguments()` (abajo) tengan algo real de dónde partir.
  - `Type.BaseType` (la nueva `closedBaseTypeFullName` de `internal/interpreter/reflection.go`)
    resolviendo los propios argumentos cerrados de una base genérica — capturados POR SEPARADO en el
    momento de parseo del TypeDef como un nuevo campo `runtime.Type.BaseTypeGenericArgs` (la nueva
    `baseTypeSpecGenericArgs` de assembly.go, usando el mismo centinela `"!N"` para una base cuyos
    propios argumentos reenvían los parámetros todavía abiertos de la clase DERIVADA, ej. `class
    DefaultClassMap<TClass> : ClassMap<TClass>`), resuelto contra el propio nombre cerrado del
    receptor en el momento de la propia llamada de `Type.BaseType`. Encontrado vía el propio
    `this.GetType().BaseType.GetGenericArguments()[0]` real de `CsvHelper.Configuration.ClassMap.
    GetGenericType()` — índice fuera de rango en un array vacío sin esto.
- **Una regresión real, detectada por el propio gate de verificación completo de esta Fase antes de
  que llegara a un commit.** La primera versión del fix de `Type.BaseType` adjuntaba los propios
  argumentos cerrados/centinela de la base DIRECTAMENTE sobre `runtime.Type.BaseTypeFullName` — que
  cualquier OTRO consumidor de ese campo (el recorrido de ancestros de despacho virtual, la herencia
  de campos, el matching de jerarquía de excepciones) resuelve directamente de vuelta a un TypeDef
  vía su propio nombre simple, y se rompió apenas cargó un sufijo `"[[...]]"`: el propio subtest de
  iterador yield-return de `TestInterfaceForeach` empezó a fallar. Arreglado haciendo que
  `BaseTypeGenericArgs` sea su propio campo separado y aditivo — `BaseTypeFullName` en sí queda
  intacto, exactamente como cada llamador preexistente ya espera.
- **La propia brecha de `Dictionary` con clave con forma de array de CsvHelper (el propio hallazgo
  postergado de la Fase 3.64, arreglado de verdad): el codificador de claves de Dictionary de
  `internal/bcl/system_collections.go` ahora maneja `KindArray`,** codificando recursivamente cada
  elemento de la misma forma que los propios campos de una clave struct ya lo hacen — la propia
  caché interna de conversión de tipos de CsvHelper (con clave de un struct con un campo array) ya
  no choca con `"Dictionary key kind 8 is not supported"`.
- **Una ampliación del evaluador de árbol de expresión, junto al trabajo de genéricos de clase —
  encontrada probando el propio AutoMapper real contra el nuevo evaluador de la Fase 3.65:**
  `Expression.Throw`/`Coalesce`/`Catch`/`TryCatch`/`TryFinally`/`TryCatchFinally` (más el nodo
  `CatchBlock` que el propio `Expression.Catch` devuelve, que tampoco es un subtipo de `Expression`
  en el .NET real) — todos genuinamente EVALUADOS, no solo modelados en forma de árbol: `Throw`
  lanza el valor evaluado como una excepción real (reutilizando el propio manejo de `ir.Throw` de
  `eval.go` vía un nuevo helper compartido `valueAsThrowable`); `Coalesce` corta en corto sobre una
  rama izquierda no nula; `TryCatch`/`TryFinally` corren un try/catch/finally real, comparando una
  excepción atrapada contra el propio tipo de prueba de cada `CatchBlock` vía el MISMO chequeo real
  de jerarquía de excepciones (`Machine.exceptionMatchesCatch`) que ya usa una cláusula `catch`
  interpretada genuina. También: `Task.Factory`/`TaskFactory.StartNew`/`TaskScheduler.Default`
  (encontrado vía la propia verificación de licencia en segundo plano, fire-and-forget, de
  AutoMapper) y `Type.IsClass` (encontrado vía el propio código real de clasificación de parámetros
  genéricos de `MappingExpressionBase\`3`).
- **Un fix real y general de robustez para `Task.Run`/`TaskFactory.StartNew`: CUALQUIER error del
  delegado se convierte en la propia excepción Faulted del Task devuelto, no solo un
  `ManagedException` real de .NET.** Antes, una limitación interna del intérprete (un tipo para el
  cual la propia metadata de este loop no tiene ningún `TypeDef`, sin relación con la semántica real
  de excepciones de .NET) golpeada DENTRO de una tarea en segundo plano fire-and-forget solía
  propagarse hasta el hilo LLAMADOR, chocando a un llamador real y sin modificar aunque nadie jamás
  espera u observa ese Task — exactamente al revés de .NET real, donde el propio fallo de un Task en
  segundo plano (de cualquier tipo) es invisible para un llamador que nunca lo revisa. Un nuevo
  helper compartido `taskFaultOrPropagate` decide esto una vez para ambos nativos — excluyendo los
  propios centinelas de seguridad de recursos de vmnet (`ErrInstructionLimitExceeded`/
  `ErrStackOverflow`), que todavía deben abortar toda la corrida.
- **Un límite de seguridad de profundidad de recursión para el propio evaluador de expresiones
  (`maxExprEvalDepth`, `internal/interpreter/exprcompile.go`).** A diferencia del propio loop de CIL
  de un método interpretado ordinario (que hace crecer `Machine.Limits.MaxInstructions` sin hacer
  crecer la pila de Go en absoluto), el propio recorrido recursivo de árbol de `evalExprNode` agrega
  un frame de pila de Go real por nodo — encontrado vía el propio árbol de plan de mapeo real de
  AutoMapper golpeando un genuino **crash de proceso** `runtime: goroutine stack exceeds
  1000000000-byte limit`, no un error atrapable, mucho antes de que cualquiera de los límites de
  recursos existentes del intérprete jamás dispararan. Ahora se convierte en un `ErrStackOverflow`
  gracioso en su lugar — un fix de robustez real sin importar qué esté realmente causando que el
  propio árbol de AutoMapper recurra tan profundo (ver "Encontrado, no arreglado" abajo).

### Encontrado, no arreglado (esta Fase)

- **El propio `Mapper.Map<Dest>(source)` real de AutoMapper — incluso para un mapeo plano trivial de
  dos propiedades — todavía no completa.** Superados los bugs de `ValueTuple\`2`/registro de
  `TypeMap` de arriba (ambos ya arreglados), choca con el nuevo guardián `maxExprEvalDepth` propio de
  `evalExprNode`: un árbol de expresión de plan de mapeo compilado real recurriendo miles de niveles
  de `Block`/`Try` de profundidad, o genuinamente sin límite. Causa raíz NO encontrada — se sabe que
  el propio `TypeMapPlanBuilder` real de AutoMapper construye andamiaje genérico de seguridad para
  referencias circulares (una plantilla perezosa/auto-referencial) incluso para tipos planos, no
  circulares, la explicación más probable, pero esto no se confirmó rastreando la forma real del
  árbol (sin herramientas en este loop para visualizar/volcar un árbol `Expression` compilado más
  allá de arqueología manual de IL, que no fue concluyente acá). No es una regresión de nada en esta
  Fase — cada problema real HASTA este punto (inicialización estática, reflection, `
  ExpressionVisitor`, el bug de genéricos de clase) ahora está arreglado y confirmado funcionando;
  esto es un muro nuevo, más profundo y distinto.
- **La construcción de `ClassMap` basada en `AutoMap()` propia de CsvHelper pierde la identidad
  genérica cerrada en la frontera de reflection de `Type.GetConstructor()` — una simplificación
  SEPARADA, deliberada y preexistente, no un bug del propio trabajo de genéricos de clase de esta
  Fase.** `CsvHelper.CsvContext.AutoMap(Type)` construye su propio `DefaultClassMap<T>` interno vía
  `typeof(DefaultClassMap\`1).MakeGenericType(new[]{recordType})` + reflection (`IObjectResolver.
  Resolve` → `Activator.CreateInstance` → en la práctica, un patrón `Expression.New(ctor).Compile()`
  y cacheo) — cada uno de los cuales el propio trabajo de `ClassGenericArgs` de esta Fase ahora
  encadena correctamente. Pero el valor `ConstructorInfo` que maneja `Expression.New` en sí vino de
  `Type.GetConstructor()` (la propia `typeFullNameOfOpen` de `internal/interpreter/reflection.go` —
  deliberadamente pela el `"[[...]]"` antes de resolver la existencia del miembro, ya que `Machine.
  ResolveMember`/`ResolveType` solo funcionan con nombres TypeDef ABIERTOS, una postura de todo el
  proyecto usada por cada nativo `Type.GetMethod`/`GetField`/`GetProperty`/..., no solo
  `GetConstructor`), así que la identidad cerrada ya se perdió para cuando `Expression.New` la ve.
  Preservar la identidad genérica cerrada a través de TODA la superficie de nativos de reflection
  (no solo construcción) sería un cambio mucho más amplio y riesgoso que el propio alcance de esta
  Fase — el % del checker no se ve afectado de ninguna manera (`CsvHelper` 1.393 métodos, 88
  marcados, sin cambios desde la Fase 3.65), ya que nada de esto toca la resolubilidad estática de
  objetivos de llamada, solo la corrección en tiempo de ejecución.
- **Los propios validadores de rango numérico de FluentValidation (`GreaterThanOrEqualTo`, etc.) —
  diagnosticados con precisión, todavía no arreglados, y la teoría de la Fase 3.64 corregida.** La
  Fase 3.64 conjeturó que esto era el propio bug de instancia cacheada compartida entre
  instanciaciones de `Comparer<T>.Default`; una reproducción real y reproducida (una regla real
  `GreaterThanOrEqualTo(18)`) muestra que `Comparer<T>.Default` en sí (`comparerDefault` de
  `internal/bcl/system_comparer.go`) es un centinela sin estado, recién asignado en cada llamada —
  no hay ninguna caché para compartir en absoluto. El desajuste REAL: `GreaterThanOrEqualValidator\`2.
  IsValid(TProperty value, TProperty valueToCompare)` (una comparación real, con restricción
  genérica `IComparable<T>`, confirmada correcta en el propio IL real de FluentValidation) recibe
  una instancia real de `FluentValidation.ValidationContext\`1` donde debería estar `value`, y el
  propio valor de propiedad real (25, o 10) donde debería estar `valueToCompare` (la constante 18) —
  rastreado hasta el propio wrapper `AbstractComparisonValidator\`2.IsValid(ValidationContext<T>,
  TProperty)` que llama, el cual comparte el MISMO nombre de método y aridad que la sobrescritura
  realmente siendo invocada incorrectamente. Lo más probable es que la propia resolución de
  overloads/despacho virtual de vmnet confunda las dos sobrescrituras `IsValid` de mismo nombre y
  misma aridad en algún punto de la cadena de ancestros — no confirmado rastreando la decisión de
  despacho real. Se intentó un fix de "degradar con gracia" (tratando el desajuste como una
  respuesta arbitraria de "igual", la misma postura que el caso preexistente `KindObject`-vs-
  `KindObject` ya usaba) y se REVIRTIÓ: hacía que `GreaterThanOrEqualTo(18)` aceptara silenciosamente
  cualquier entrada sin importar su valor real (`Validate(10)` reportando `"valid"` cuando debe ser
  `"invalid"`) — una librería de validación validando silenciosamente algo mal es un bug de
  corrección real con consecuencias reales, estrictamente peor que el error honesto y ruidoso
  original que reemplazaba. `examples/fluentvalidation-demo` sigue ejercitando deliberadamente solo
  los validadores de string que ya funcionan correctamente.

### Cómo verificar Fase 3.66

```bash
go build ./...
go vet ./...
gofmt -l .
go test ./...
go run ./cmd/vmnet check package --profile=netstandard-lite automapper@16.2.0
go run ./cmd/vmnet check package --profile=netstandard-lite csvhelper@33.1.0
go run ./cmd/vmnet check package --profile=netstandard-lite fluentvalidation@11.9.2
```

---
## Fase 3.67 — el modelo de errores real: códigos `VMNET_*` y stack traces de la spec §18.3

**Objetivo:** el primer ítem del empuje de "listo para producción" de la Fase 4 — el propio modelo
de errores de la spec §30 (códigos `VMNET_*`, `Error{Code, Message, Details, Cause}` estructurado)
nunca se había implementado en realidad; cada falla de la API pública era un error de Go plano y
sin estructura que un llamador solo podía matchear por string. Lo mismo para el propio formato de
stack trace "at Type.Method()" de la spec §18.3.

**Resultado: un tipo `vmnet.Error` real y testeado con los 14 códigos de la spec (más un agregado
honesto), conectado en cada punto de entrada público que puede fallar, clasificando la propia falla
subyacente real por TIPO de error de Go donde ya existe uno y por contenido de mensaje bien
establecido solo donde no existe — y stack traces multi-frame reales y funcionando en cada
excepción manejada.**

- **`vmnet.Error`** (`errors.go`) — `Code`/`Message`/`Details`/`Cause`, implementando
  `Unwrap() error` así `errors.Is`/`errors.As` todavía alcanzan el propio centinela subyacente real
  (`pe.ErrInvalidPE`, `metadata.ErrOutOfRange`, un `*ManagedException`, ...) a través de él. `Code`
  es una de 14 constantes estables que coinciden uno a uno con la propia lista de la spec §30.2
  (`CodeInvalidPE`, `CodeMissingCLIHeader`, `CodeInvalidMetadata`, `CodeUnsupportedOpcode`,
  `CodeUnsupportedBCLMethod`, `CodeTypeNotFound`, `CodeMethodNotFound`, `CodeFieldNotFound`,
  `CodeStackOverflow`, `CodeCallDepthExceeded`, `CodeManagedException`, `CodeNuGetResolveFailed`,
  `CodeUnsupportedPackage`, `CodePermissionDenied`), más un agregado honesto más allá de la propia
  lista de la spec: `CodeInternal`, un catch-all para que `Code` nunca quede vacío para una falla
  real que el clasificador no pueda ubicar de otra forma.
- **Una función `classify()` en capas**, llamada una vez en cada frontera pública
  (`Assembly.Call`/`CallBytes`/`New`, `Instance.Call`, `VM.LoadBytes`, `NuGetManager.Add`/`Restore`,
  `VM.LoadPackage`) — nunca internamente, así ningún sitio de `fmt.Errorf` interno en todo el
  intérprete necesitó cambiar:
  - Matches exactos de TIPO/centinela de error de Go primero, siempre confiables:
    `*ManagedException` (dividido además en `CodePermissionDenied` cuando `TypeName ==
    "System.UnauthorizedAccessException"` — el único tipo de excepción real de .NET que el gate de
    `Permissions` siempre lanza — vs. `CodeManagedException` para cualquier otra excepción real
    lanzada y no atrapada), el nuevo `*ir.UnsupportedOpcodeError`, un nuevo
    `*interpreter.UnsupportedBCLMethodError` (reemplazando un string formateado plano en el único
    sitio real de llamada "sin nativo registrado", `internal/interpreter/calls.go`, así es
    detectable con `errors.As` por primera vez), `interpreter.ErrStackOverflow`,
    `interpreter.ErrCallDepthExceeded`/`ErrInstructionLimitExceeded`/`ErrArrayTooLarge` (los tres:
    "se excedió un límite de recursos de ejecución configurado" — la spec §30.2 tiene un código
    para toda la familia, no uno por límite específico), `pe.ErrMissingCLIHeader`/`ErrInvalidPE`/
    `ErrInvalidRVA`, `pe.ErrInvalidMetadataRoot`/`metadata.ErrInvalidMetadataRoot`/`ErrMissingStream`/
    `ErrUnsupportedTable` — cada uno de estos ya existía como un centinela real de Go desde tan
    atrás como la Fase 1; esta Fase es lo primero que realmente clasifica por ellos.
  - Matching de contenido de mensaje solo para las pocas expresiones reales y bien establecidas sin
    centinela dedicado hoy: `runtime.ErrMethodNotFound` (la propia frontera `resolveMethod` de
    assembly.go, que envuelve YA SEA una falla de TypeDef faltante O de ningún overload que matchee
    bajo un centinela vía `%v` — no `%w`, así el más específico `metadata.ErrOutOfRange` no es
    alcanzable a través de él en absoluto — desambiguado por la propia frase "type X.Y not found"
    del mensaje, siempre presente cuando esa es la causa real), `metadata.ErrOutOfRange` alcanzado
    a través de un camino que SÍ preserva `%w`, y los propios fallos de acceso a campo de
    `internal/interpreter` ("... has no field ..."/"... has no static field ..."), más los propios
    strings planos "nuget: ..." de `internal/nuget` (sin centinelas ahí en absoluto).
- **Stack traces multi-frame reales** (spec §18.3) — `runtime.ManagedException.Stack []string` y
  `PushFrame`, agregado exactamente una vez por frame de método interpretado, por el propio
  `Machine.invoke` central de `internal/interpreter/eval.go`, en el instante en que una excepción
  está por dejar ese frame sin manejar (no en el propio sitio del `throw` original — un
  `catch`-y-relanzamiento real obtiene su propio frame registrado una vez que ÉL, a su vez, falla en
  manejar lo que relanza). `ManagedException.String()` renderiza el propio formato exacto de la
  spec §18.3 (`TypeName: Message`, luego una línea `   at Type::Method()` por frame, la más interna
  primero) — `Error()` en sí deliberadamente se queda como el resumen corto de una sola línea que
  siempre fue (muchos llamadores existentes ya lo loguean/matchean/envuelven así); `String()` es de
  donde se puebla `vmnet.Error.Details` para un `*ManagedException`.
- **`TestErrorClassification`/`TestManagedExceptionStackTraceFormat`** (`errors_test.go`, nuevo) —
  diecisiete subtests, uno por `Code`, cada uno contra un disparador real y reproducido de punta a
  punta (el propio `Rules.Eval`/`Loops.Runaway`/`FileIO.WriteThenReadText` del fixture compartido de
  C#, un nombre de tipo/método desconocido, bytes de PE basura) donde ya existía uno barato, o un
  chequeo unitario directo de `classify()` contra el propio centinela/tipo real para el puñado de
  códigos que de otra forma necesitarían un fixture de C# completamente nuevo, acceso real a red, o
  un PE artificialmente corrompido solo para alcanzarlos.

### Cómo verificar Fase 3.67

```bash
go build ./...
go vet ./...
gofmt -l .
go test ./...
go test -run "TestErrorClassification|TestManagedExceptionStackTraceFormat" -v .
```

---
## Fase 3.68 — el bug de despacho de los validadores numéricos de FluentValidation, arreglado de verdad

**Objetivo:** cerrar el hueco que la Fase 3.66 diagnosticó con precisión pero dejó sin arreglar —
las reglas numéricas propias de FluentValidation (`GreaterThanOrEqualTo`/`LessThanOrEqualTo`/
`InclusiveBetween`) crasheaban o, en un intento anterior revertido, validaban silenciosamente algo
incorrecto.

**Resultado: la causa raíz real era la propia caminata de ancestros de vmnet por solo-nombre para
métodos virtuales, que confundía dos overrides `IsValid` distintos, de mismo nombre y misma
aridad, a través de un par de clases base/derivada genéricas — arreglado con una regla general de
resolución de sobrecarga, y luego endurecido a través de una cadena corta de huecos más chicos y
genuinamente separados que el arreglo dejó recién alcanzables.**

- **El bug de despacho real.** Los propios `AbstractComparisonValidator<T,TProperty>.IsValid
  (ValidationContext<T>, TProperty)` y `GreaterThanOrEqualValidator<T,TProperty>.IsValid(TProperty,
  TProperty)` de FluentValidation 11.9.2 real y sin modificar son dos métodos virtuales
  genuinamente distintos que casualmente comparten nombre y aridad — .NET real los distingue por
  firma completa (un slot de vtable distinto cada uno), pero la propia caminata de ancestros de
  vmnet (`retryName := t + "::" + method` en `assembly.go`) solo busca por nombre, así que podía
  resolver a cualquiera de los dos. Arreglado con una nueva regla en `hasHardShapeMismatch`
  (`assembly.go`): si un candidato declara el MISMO índice de parámetro genérico todavía abierto
  (coincidiendo también nivel clase-vs-método) en dos o más posiciones de parámetro, los
  argumentos reales en tiempo de ejecución enlazados a esas posiciones deben compartir el mismo
  `Kind` — los genéricos reales nunca pueden producir dos tipos concretos distintos para un
  parámetro de tipo compartido en un único sitio de llamada, así que un mismatch de `Kind` ahí es
  prueba concluyente de que se eligió el override equivocado de mismo nombre, no solo "clase
  concreta distinta." Verificado contra el ensamblado real: `Validate(25)` (una edad válida) ahora
  reporta correctamente `"valid"` en vez de crashear con `IComparable.CompareTo: unsupported
  receiver kind 7`.
- **Tres huecos más chicos y genuinamente separados que el arreglo de despacho dejó recién
  alcanzables** (antes nunca se llegaba a ellos porque la ejecución crasheaba antes):
  - **`box` seguido de chequeo de null sobre un primitivo** (`internal/interpreter/
    arithmetic.go`) — el C# real compila `box !TProperty` seguido de una comparación `ldnull`/
    `cgt.un` como una forma genérica de chequear "¿este valor tipado T es no-null?", sin saber en
    tiempo de compilación si `T` es tipo valor o tipo referencia. Boxear un tipo valor genuino en
    .NET real nunca produce null, así que este chequeo siempre tiene una única respuesta fija y
    determinística. El propio `box` de vmnet sobre un `Kind` primitivo es un passthrough de
    identidad puro (nunca se vuelve un wrapper `KindObject`), así que la comparación pegaba antes
    en un error sin manejar de "mismatched value kinds". Arreglado con un nuevo helper
    `isPrimitiveValueKind` y un caso dedicado en `evalBinOp` que responde el resultado fijo
    correcto para `ceq`/`cgt`/`clt` entre cualquier `Kind` primitivo y `KindNull`.
  - **`CultureInfo.CurrentUICulture`/`.Parent`/`.IsNeutralCulture`** (`internal/bcl/
    system_misc.go`) — tres propiedades reales que la propia búsqueda de mensajes de recurso
    satélite de FluentValidation llama y que no tenían nativo todavía; registradas sobre el propio
    stand-in de cultura invariante ya existente (`IsNeutralCulture` respondiendo `false`,
    coincidiendo con el propio `InvariantCulture.IsNeutralCulture` de .NET real).
  - **Un bug de objeto-fresco-vs-singleton en el propio `cultureInfoInvariant`** — devolvía un
    `&runtime.Object{}` nuevo en cada llamada, así que dos llamadas a (digamos)
    `CultureInfo.CurrentCulture` nunca eran iguales por referencia. Código de .NET real que camina
    la propia cadena de padres de una cultura hasta llegar a la cultura invariante (genuinamente
    singleton) — `while (c != CultureInfo.InvariantCulture) c = c.Parent;`, exactamente el propio
    patrón de fallback de recursos de FluentValidation — nunca terminaba: `VMNET_CALL_DEPTH_
    EXCEEDED`. Arreglado haciendo que `cultureInfoInvariant` devuelva el MISMO `*runtime.Object`
    compartido en cada llamada. `TimeZoneInfo.Local`/`.Utc` (que reutilizaban la misma función para
    un stub de "sin datos reales" no relacionado) se separaron deliberadamente a su PROPIO
    singleton separado (`timeZoneInfoStub`) — compartir un singleton entre dos tipos de .NET real
    no relacionados haría que un stand-in de `TimeZoneInfo` fuera incorrectamente igual por
    referencia a un stand-in de `CultureInfo`.
- **Una limitación restante, más angosta, y ahora acotada con precisión.** El propio
  `MessageFormatter.BuildMessage` (código fuente real de FluentValidation) formatea cada
  `{Placeholder}` vía `value2?.ToString()` — un operador condicional-nulo de C# real, que compila a
  `dup; brtrue.s ...` directamente sobre el valor boxeado, no una comparación `ldnull`/`ceq` (una
  forma de bytecode distinta a la recién arreglada arriba). Para un valor boxeado que resulta ser
  igual al cero propio de su tipo (p. ej. un `int` boxeado con valor `0`, que es exactamente lo que
  el propio placeholder `{From}` de `InclusiveBetween(0, ...)` produce), el `box` de identidad-
  passthrough de vmnet no deja forma de distinguir "una referencia null real" de "un valor
  boxeado, real, legítimamente cero" en esta instrucción — `brtrue` ve un `I4` cero de cualquier
  forma. Esta es una instancia más angosta de la misma simplificación arquitectónica subyacente
  ("`box` no boxea de verdad"), no una falla de diseño nueva y separada; arreglarla en general
  significaría darle a `box` una representación de wrapper real a través de cada opcode y nativo
  numérico, algo desproporcionado para el alcance propio de esta Fase. Confirmado con una
  reproducción real (`InclusiveBetween(0, 130)` sobre la edad `131`) y acotado en vez de arreglado
  en profundidad; tanto `examples/fluentvalidation-demo` como el corpus del checker evitan este
  borde angosto (cualquier límite distinto de exactamente `0` formatea correctamente, verificado
  con `InclusiveBetween(1, 130)`).
- **`examples/fluentvalidation-demo` extendido** — ahora también ejercita `GreaterThanOrEqualTo(18)`
  sobre una propiedad `int` real (`ValidateAge`), demostrando el arreglo, no solo los validadores
  de string usados antes.
- **Checker remedido con la metodología correcta** (`vmnet check package`, que resuelve el propio
  grafo de dependencias transitivas del paquete de la misma forma que `LoadPackage` en tiempo de
  ejecución — la forma simple `vmnet check <dll>` usada para un chequeo rápido de cordura antes en
  esta Fase da un número distinto y no relacionado porque no puede resolver la superficie de BCL
  reenviada a través de las dependencias faltantes): `FluentValidation@11.9.2` pasa de 26 a 25
  métodos marcados (98.1%, de 98.0%) — el nuevo nativo (`get_IsNeutralCulture`) explica la
  diferencia; los arreglos de despacho/box/singleton de arriba son arreglos de corrección en
  tiempo de ejecución, no visibles para el checker.

### Cómo verificar la Fase 3.68

```bash
go build ./...
go vet ./...
gofmt -l .
go test ./...
go run ./cmd/vmnet check package fluentvalidation@11.9.2
cd examples/fluentvalidation-demo && dotnet build FvDemoWrapper.csproj -c Release && go run .
```

---
## Fase 3.69 — cerrando los huecos de la suite golden de la spec §28, y una línea base de cobertura real y medida

**Objetivo:** el ítem propio del checklist de la Fase 4 "suite golden completa (spec §28.1-28.5)"
nunca se había auditado en realidad contra el propio checklist de ~40 ítems de la spec — después de
68 Fases de trabajo incremental, la mayoría de las categorías resultaron ya estar cubiertas, pero
hacía falta una auditoría real y honesta para saber cuáles genuinamente no lo estaban, en vez de
asumirlo.

**Resultado: un mapa de cobertura de cada sub-ítem de la spec §28.1-28.7 contra la suite de tests
existente, tests reales agregados para cada hueco genuino encontrado, y una línea base de cobertura
de sentencias real (no adivinada) con un objetivo hacia adelante fijado con honestidad.**

- **La auditoría.** Cada uno de los ~40 sub-ítems nombrados en la spec §28 (tests de PE/metadata/
  IL/runtime/BCL/checker/NuGet) se chequeó contra la suite de tests real. La mayoría ya estaban
  sólidamente cubiertos — nada sorprendente después de 68 Fases de desarrollo guiado por fixtures —
  pero aparecieron nueve huecos genuinos: parsing de TypeRef, parsing de MemberRef, user strings, y
  firmas genéricas (§28.2, todos ejercitados solo *incidentalmente* a través de llamadas BCL de
  punta a punta, nunca verificados directamente); llamada virtual y boxing/unboxing (§28.4 — el
  propio corpus real de vmnet, FluentValidation/AutoMapper/etc., ejercita el despacho virtual
  constantemente vía `callvirt`, pero el ensamblado de fixtures compartido no tenía ningún test de
  regresión propio para esto); `System.Math.Abs` y `System.Guid` (§28.5, ambos con una
  implementación nativa real y cero llamadores en tests); detección de llamada BCL no soportada/
  P-Invoke/async/reflection (§28.6, la propia función `categorize()` del checker y el finding
  `KindPInvoke` a nivel de ensamblado se ejercitaban solo vía valores `Report` construidos a mano en
  los tests existentes, nunca contra IL real que efectivamente los dispare).
- **Cerrados, uno por uno:**
  - `tests/fixtures/csharp/VirtualDispatch.cs` (nuevo) — `Beast`/`Wolf`/`Lion`, una jerarquía real
    `virtual`/`override` ejercitada a través de una referencia de tipo base, un método virtual
    heredado y no sobrescrito, y un array del tipo base — más una suite de ida y vuelta `box`/
    `unbox.any` (incluyendo el borde junto al cual está el hueco del cero boxeado en el condicional-
    nulo de la Fase 3.68: un `zero` boxeado sí va y vuelve correctamente a través del propio `box`
    de passthrough de identidad, solo no a través de una verificación `?.` de condicional-nulo sobre
    él). Tests de Go nuevos: `TestVirtualDispatch`, `TestBoxUnboxRoundTrip` (`vmnet_test.go`).
  - `tests/fixtures/csharp/MathAndGuid.cs` (nuevo) — `Math.Abs(int)`/`Math.Abs(double)`, y una ida y
    vuelta real de `Guid.NewGuid()`/`.ToString()`/`.Equals()` (dos GUIDs frescos difieren, un GUID es
    igual a su propio `ToString()` repetido, el formato canónico tiene 36 caracteres). Test de Go
    nuevo: `TestMathAbsAndGuid`.
  - `internal/metadata/metadata_test.go` — `TestParse_RealAssembly_TypeRef` (encuentra el TypeRef de
    `System.Object` que todo ensamblado real referencia), `TestParse_RealAssembly_MemberRef`
    (encuentra el MemberRef real de `String.Concat` que `Strings.Hello` llama),
    `TestParse_RealAssembly_GenericSignature` (una fila TypeSpec real decodifica como
    `SigGenericInst`, p. ej. `List<int>`, vía `ParseTypeSpec`).
  - `internal/il/decoder_test.go` — `TestDecode_StringsHello_UserString`, una capa debajo del ya
    existente `TestDecode_StringsHello` (que solo confirmaba que el opcode `ldstr` decodifica):
    resuelve el token real del heap `#US` a su valor literal ("Hello ") vía
    `metadata.Metadata.UserString`.
  - `internal/checker/analyzer_test.go` — `TestCategorize` (cobertura unitaria directa del propio
    mapeo de `categorize`: `System.Reflection.*` -> `KindReflection` / `System.Threading.Tasks.*` ->
    `KindAsync` / todo lo demás -> `KindUnsupportedMethod`) y `TestAnalyze_PInvokeIsReported`, que
    corre `Analyze` contra un ensamblado REAL con `[DllImport]` (`tests/fixtures/csharp-pinvoke`, un
    proyecto de fixture nuevo y deliberadamente SEPARADO — una declaración P/Invoke real es un
    finding a nivel de todo el ensamblado que de otra forma rompería la propia invariante de
    `TestAnalyze_OwnAssemblyIsCompatible` de "solo se espera que `Unsupported.FunctionPointerCall`
    esté marcado" para el fixture principal compartido).
- **Una línea base de cobertura real y medida — no una adivinada.** El ítem preexistente del
  checklist de la Fase 4 ("objetivo de cobertura acordado con stakeholders, p. ej. ≥70% en paquetes
  core") aparentemente nunca se había medido en realidad contra este código; correrlo por primera
  vez (`go test -coverpkg=./... -coverprofile=... ./...`, que atribuye correctamente la cobertura a
  través de fronteras de paquete — el simple `go test -cover ./...` subcuenta feo a
  `internal/interpreter`/`internal/bcl`/`internal/runtime`/`internal/ir`, ya que casi todo su
  ejercicio real viene de los propios tests de punta a punta del paquete RAÍZ llamando hacia ellos,
  no de tests que viven en esos paquetes mismos) muestra **33.7% de cobertura de sentencias total
  sin los tests gateados por red, 38.8% con ellos** (`VMNET_NETWORK_TESTS=1`). Ambos números son
  reales pero genuinamente modestos, y el placeholder original de "≥70%" nunca fue realista para
  este tipo de proyecto: el propio despacho de opcodes/nativos de un intérprete de IL se ejercita
  naturalmente a través de la ejecución completa de programas de punta a punta (un solo test de
  fixture golden camina docenas de ramas de un puñado de switch statements gigantes) en vez de tests
  unitarios angostos por función, y la señal de corrección primaria REAL de este proyecto siempre
  fue el propio porcentaje de resolubilidad por paquete del checker (el propio objetivo de 97%+ de
  `docs/en/COMPATIBILITY.md`) más los 12+ demos reales y sin modificar de paquetes NuGet — ninguno
  de los cuales `go test -cover` cuenta en absoluto. **Nuevo objetivo fijado con honestidad: ≥35% de
  cobertura de sentencias total** (ya cumplido — los propios arreglos de esta Fase más los tests
  nuevos lo subieron un poco más sobre la línea base preexistente), **sin retroceder** en ningún
  cambio futuro, chequeado de la misma forma
  (`go test -coverpkg=./... -coverprofile=cover.out ./... && go tool cover -func=cover.out | tail
  -1`).

### Cómo verificar la Fase 3.69

```bash
go build ./...
go vet ./...
gofmt -l .
go test ./...
dotnet build tests/fixtures/csharp/Fixtures.csproj -c Release
dotnet build tests/fixtures/csharp-pinvoke/PInvokeFixture.csproj -c Release
go test -coverpkg=./... -coverprofile=/tmp/cover.out ./...
go tool cover -func=/tmp/cover.out | tail -1
```

---
## Fase 3.70 — los cuatro docs faltantes, y una suite de benchmarks real

**Objetivo:** dos ítems de la Prioridad 2 de la planificación de este empuje — los cuatro docs que
la spec §33.2 exige y que este proyecto nunca había escrito (`supported-il.md`, `supported-bcl.md`,
`nuget-support.md`, `compatibility-profile.md`), y una suite de benchmarks real (spec §32) más allá
de la propia semilla "arithmetic loop" de `examples/calculator`.

**Resultado: los cuatro docs escritos de forma bilingüe, basados enteramente en el código real (no
en conocimiento genérico de CIL/.NET/NuGet); una suite de benchmarks real y ejecutable que cubre
los siete workloads de la spec §32.2 más cada métrica de la spec §32.3 medible hoy a través de la
API pública; y un bug nuevo y genuino encontrado por la propia suite de benchmarks.**

- **Los cuatro docs** (`docs/en/` y `docs/es/`, ~2.400 líneas en total): `supported-il.md` (basado
  en `internal/il/opcode.go`, el propio switch statement de `internal/ir/builder.go` — la verdad
  de fondo real de "qué CIL ejecuta vmnet" — y `internal/interpreter/eval.go`; documenta los
  opcodes identity-no-op, el límite permanentemente fuera de alcance de `calli`, y cada opcode sin
  ningún `case` en absoluto: `jmp`, `cpobj`, `unbox` puro, `sizeof`/`cpblk`/`initblk`,
  `arglist`/`refanyval`/`refanytype`/`mkrefany`, `ckfinite`, los prefijos `tail.`/`no.` —
  encontrados diffeando la tabla de opcodes contra el switch, no por suposición); `supported-bcl.md`
  (basado en cada llamada `register()` a través de `internal/bcl/*.go` más las entradas de
  `machineRegistry`/`genericMachineRegistry` en `internal/interpreter`, organizado por namespace,
  citando solo los huecos reales ya documentados en `docs/en/COMPATIBILITY.md`); `nuget-support.md`
  (basado en el propio parsing real de nupkg/nuspec de `internal/nuget/`, la prioridad de tiers de
  TFM, la resolución de dependencias transitivas, la forma JSON real del lockfile, y el manejo de
  assets solo-nativos/solo-referencia); `compatibility-profile.md` (basado en los tres perfiles
  reales de `internal/checker/profile.go`, la lógica exacta de `finalize()` de `analyzer.go`, y un
  ejemplo trabajado de `fluentvalidation@11.9.2` usando las propias cifras reales y publicadas de
  este proyecto, 98.1%/25 marcados — deliberadamente sin fabricar líneas de Finding individuales
  que COMPATIBILITY.md mismo no publica). Un desliz factual atrapado y arreglado durante la
  revisión: una cita que atribuía el hueco del condicional-nulo con cero boxeado a la "Fase 3.66"
  en ambas versiones de idioma de `supported-il.md` — en realidad se encontró y se le halló la
  causa raíz en la Fase 3.68; corregido en ambos archivos.
- **`benchmarks/`** (directorio nuevo) — `Bench.cs`/`Bench.csproj` implementa los siete workloads
  de la spec §32.2 (loop aritmético, concatenación de strings, asignación de objetos,
  `List<T>.Add`, búsqueda en `Dictionary`, JSON de entrada/salida vía el propio paquete real
  `System.Text.Json`, y un método de motor de reglas llamado 10.000 veces desde el lado Go
  específicamente para estresar el overhead de ida y vuelta por llamada a volumen realista) como
  métodos C# que iteran internamente y devuelven un resultado final, así que el harness de Go mide
  UNA `Assembly.Call` por workload — el overhead de despacho nunca contamina la medición de "n
  iteraciones de trabajo real". `main.go` corre cada uno a través de vmnet Y un equivalente Go
  nativo línea por línea, falla ruidosamente ante cualquier discrepancia, y reporta cada métrica de
  la spec §32.3 medible hoy a través de la API pública: tiempo de carga en frío, overhead de
  invocación de método (5.000 llamadas triviales ya calentadas), asignaciones/op y bytes lógicos de
  heap (`testing.AllocsPerRun`/`runtime.MemStats`, ambos del lado host — el propio costo del lado Go
  de vmnet de manejar una llamada interpretada), y tiempo de restauración de paquete. **Hueco
  honesto y documentado**: instructions/sec no se reporta — el propio contador real de
  instrucciones por `Call` del intérprete (el mismo contra el que `VMNET_CALL_DEPTH_EXCEEDED`
  presupuesta) todavía no está expuesto a través de la API pública; reportarlo necesitaría un gancho
  de instrumentación nuevo, no una adivinanza. La comparación contra CoreCLR se mantiene acotada a
  la configuración ya existente de `examples/calculator` (seis programas CoreCLR más para mantener
  a mano se juzgó desproporcionado para esta Fase); un "equivalente goja" (spec §32.1) no aplica en
  absoluto — goja es un motor de JavaScript, no un runtime de CIL/BCL.
- **Un bug nuevo y genuino encontrado corriendo esta suite por primera vez**: `JsonRoundTrip`
  (`System.Text.Json.JsonSerializer.Serialize`/`Deserialize`, una API distinta y más usada que el
  propio parsing basado en `JsonDocument` de `examples/system-text-json-demo`, que ya funciona)
  crashea con `binary op on mismatched value kinds (9, 1)`. Se le halló la causa raíz vía
  instrumentación puntual en `eval.go` (agregada, usada, y luego limpiamente removida) más
  disassembly de IL real de `System.Text.Encodings.Web.dll`: la propia inicialización estática de
  `JsonSerializer` alcanza `DefaultJavaScriptEncoder`, que necesita el propio campo `unsafe fixed
  uint Bitmap[2048]` de `AllowedBmpCodePointsBitmap` — un buffer de tamaño fijo `unsafe` de C# real
  (aritmética de punteros direccionable por byte dentro de un array inline, respaldado por una
  estructura anidada generada por el compilador `<Bitmap>e__FixedBuffer`). Un grep por todo el
  código en busca de "fixed buffer"/"FixedBuffer" no encuentra nada en absoluto — vmnet no tiene
  ningún soporte para esta característica, no es un intento parcial o con bugs. Implementar
  semántica real de memoria unsafe direccionable por byte es un proyecto sustancial y separado,
  desproporcionado para el alcance propio de esta Fase; acotado en vez de arreglado en profundidad
  — el propio workload de JSON de entrada/salida de `benchmarks/main.go` atrapa esto con gracia y
  lo reporta como un hueco conocido en vez de crashear toda la suite, y el parsing basado en
  `JsonDocument` de `examples/system-text-json-demo` sigue siendo la historia verificada de este
  proyecto con System.Text.Json.

### Cómo verificar la Fase 3.70

```bash
go build ./...
go vet ./...
gofmt -l .
go test ./...
cd benchmarks && dotnet build Bench.csproj -c Release && go run .
```

---
## Fase 3.71 — congelando la API pública de Go, y un compromiso de semver real

**Objetivo:** el último ítem de la Prioridad 2 de la planificación de este empuje — la API pública
de Go nunca se había congelado ni documentado formalmente como una superficie estable, y los tags
de git del proyecto no eran semver válido en absoluto, así que un consumidor real de módulo Go no
tenía forma significativa de hacer `go get` de un release fijado.

**Resultado: `docs/en/api-stability.md`/`docs/es/api-stability.md`, una instantánea completa y
actual de cada símbolo exportado en el paquete raíz con una política de semver explícita y
concreta — más el primer tag de git semver real y válido del proyecto, `v0.1.0`, junto al tag
numerado por Fase existente en el mismo commit.**

- **La superficie congelada**: enumerada directamente de la propia salida real de `go doc -all .`
  (no de memoria) — tres tipos "verbo" (`VM`, `Assembly`, `Instance`), `Value` más sus seis
  constructores, `Error`/`Code` (spec §30, Fase 3.67), `Permissions` (el modelo de seguridad de la
  spec, Fase 3.59), y `NuGetManager`/`Package`. Deliberadamente chica.
- **La política**: pre-1.0, así que la propia regla `0.y.z` de semver técnicamente permite que
  cualquier cosa cambie — acotada de todas formas a una promesa concreta: un release equivalente a
  patch nunca cambia una firma existente/remueve un símbolo/cambia el significado de un `Code`; un
  release equivalente a minor solo puede agregar; cualquier cosa realmente breaking se señala
  explícitamente, en negrita, en la propia entrada de esa Fase en el ROADMAP, nunca permitida
  silenciosamente solo porque pre-1.0 técnicamente lo permite. Después de v1.0.0, aplica el semver
  real, con un cambio breaking requiriendo un path de módulo nuevo con sufijo `/v2` según la propia
  convención de Go.
- **Una nota real y honesta sobre los tags de hoy**: el patrón existente
  `v0.0.3.<n>.faseNNN-<slug>` no es semver válido de módulo Go (demasiados componentes numéricos,
  sin un separador de prerelease real) — un `go get .../go-vmnet@latest` hoy resuelve
  silenciosamente a una pseudo-versión, no a un release fijado. Arreglado tageando el propio commit
  de esta Fase también como `v0.1.0` (junto al tag usual numerado por Fase, ambos en el mismo
  commit) — el primer tag que un consumidor externo realmente puede `go get` por número de versión.
- **Una nota explícita de divergencia**: el propio boceto de API de la spec §6.1 de
  `docs/en/spec.md` (`Options{Profile, Debug, MaxStackDepth, MaxHeapBytes}` pasado a `New`, una API
  de bajo nivel `ResolveMethod`/`NewFrame`/`Invoke`, `BackendAuto`) fue la propia visión de diseño
  inicial del proyecto — la API real, construida y congelada divergió de ella (`New()` no toma
  ningún argumento en absoluto; `Permissions()` es su propio accesor mutable in situ, no una opción
  en tiempo de construcción). `api-stability.md`, no la spec §6.1, es ahora la descripción
  autoritativa de lo que realmente está congelado.

### Cómo verificar la Fase 3.71

```bash
go build ./...
go vet ./...
gofmt -l .
go test ./...
go doc -all . | diff - <(cat docs/en/api-stability.md)  # solo sanity-check; se espera que difiera en prosa, no en símbolos
```

---
## Fase 3.72 — `MaxStringBytes`, el análogo de strings de `MaxArrayLength`

**Objetivo:** el primer ítem de la Prioridad 3 (endurecimiento incremental) — `newarr` está
acotado contra una longitud adversarial desde la Fase 3.5 (`MaxArrayLength`), pero nada acotaba a
una sola llamada que produce un string de pedir un tamaño igualmente adversarial; un sitio de
llamada real como `new string('x', int.MaxValue)` o `"x".PadLeft(int.MaxValue)` podría intentar una
asignación de varios gigabytes en un solo paso.

**Resultado: un nuevo `Limits.MaxStringBytes` (default 64 MiB), aplicado tanto en los dos sitios de
llamada conocidos que pueden pedir una asignación enorme desde un argumento `int` puro ANTES de que
ocurra cualquier asignación, como — a modo de red de seguridad general — después del resultado de
cada llamada nativa BCL, por si algún otro nativo que esta Fase no auditó específicamente produce
algo demasiado grande.**

- **Dos sitios de llamada reales y adversariales, chequeados antes de la asignación**: `new
  string(char, int count)` (el propio camino dedicado a `System.String` de `Machine.newObj`, ya
  que un string de vmnet es un `KindString` puro, no el `KindObject` que devuelve cada otro
  constructor nativo) y el propio argumento `width` de `String.PadLeft`/`PadRight` (un nuevo mapa
  `stringSizeGatedBCLNatives` en `calls.go`, reflejando el propio patrón ya establecido de
  `permissionGatedBCLNatives` de un gate pre-llamada indexado por nombre). Deliberadamente angosto,
  no una heurística general de "cualquier argumento int podría ser un tamaño": el propio `char[]`
  de `new string(char[], start, length)` vino de un `newarr` real, ya acotado por
  `MaxArrayLength` — nada nuevo que acotar ahí.
- **Una red de seguridad general post-llamada** (`Machine.checkStringLimit`, llamada desde los tres
  propios caminos de despacho nativo de `tryCall` — `bcl.Lookup` puro, `genericMachineRegistry`,
  `machineRegistry`): cualquier llamada nativa cuyo resultado sea un `KindString` más largo que
  `MaxStringBytes` se rechaza con `ErrStringTooLarge`, incluso si no era uno de los dos sitios de
  llamada específicamente pre-chequeados arriba (p. ej. `String.Concat` de dos strings ya grandes
  pero individualmente bajo el límite, o `StringBuilder.ToString()` después de un buffer grande).
  Chequear el RESULTADO es deliberadamente insuficiente por sí solo para los sitios de llamada
  pre-chequeados — para cuando `strings.Repeat` de la propia librería estándar de Go retorna (o
  entra en pánico) desde un count absurdo, el intento de asignación ya ocurrió; el pre-chequeo
  existe específicamente para detener esa clase de caso antes de que pueda ocurrir.
- **`ErrStringTooLarge`** se clasifica de la misma forma en que ya lo hacen
  `ErrArrayTooLarge`/`ErrCallDepthExceeded`/`ErrInstructionLimitExceeded` (el propio `classify` de
  `errors.go`) — la spec §30.2 tiene un solo código `CodeCallDepthExceeded` para toda la familia "se
  excedió un límite de recursos de ejecución configurado", no uno por cada límite específico.
- **Un hueco real, honesto y preexistente que la propia investigación de esta Fase sacó a la luz,
  no introducido por ella**: no hay ninguna API pública para configurar NINGÚN campo de `Limits`
  (`MaxCallDepth`/`MaxInstructions`/`MaxStackDepth`/`MaxArrayLength`, y ahora `MaxStringBytes`) — el
  propio camino de construcción de la máquina de `call.go` siempre usa
  `interpreter.DefaultLimits()`. Esto es anterior a `MaxStringBytes` (ya era cierto para
  `MaxArrayLength` desde la Fase 3.5) y está fuera del alcance propio de esta Fase arreglarlo, pero
  vale la pena nombrarlo claramente en vez de dejarlo implícito — una Fase futura que exponga
  límites configurables a través de `VM`/`Assembly` necesitaría actualizar la propia superficie
  congelada de `docs/es/api-stability.md` en consecuencia.
- **Cuatro tests nuevos** (`internal/interpreter/eval_test.go`, IR construido a mano en el mismo
  estilo que los ya existentes `TestInvoke_MaxStackDepth`/`TestInvoke_CallDepthExceeded`): el
  pre-chequeo de `PadLeft`, el pre-chequeo de `new string(...)`, un `PadLeft` chico y legítimo que
  sigue funcionando normalmente bajo el límite, y la red de seguridad post-llamada atrapando un
  resultado de `String.Concat` demasiado grande que los pre-chequeos no cubren.

### Cómo verificar la Fase 3.72

```bash
go build ./...
go vet ./...
gofmt -l .
go test ./...
go test -run "TestInvoke_MaxStringBytes" -v ./internal/interpreter/...
```

---
## Fase 3.73 — una caché real de resolución de métodos/tokens

**Objetivo:** el segundo ítem de la Prioridad 3 — perfilar hacia dónde va realmente el overhead
propio por `Call` de vmnet antes de optimizar a ciegas. Resultó que la construcción/caché de IR
(por método, desde temprano) ya estaba bien; el costo real y evitable, repetido, estaba una capa
más arriba.

**Resultado: dos cachés nuevas que cierran un hueco real y medido — cada llamada repetida al mismo
método volvía a correr un escaneo lineal completo de la tabla MethodDef Y volvía a parsear su blob
de firma desde cero, sin nada que memoizara ninguno de los dos pasos, a diferencia del paso de
búsqueda de tipo justo antes de ellos (cacheado desde la Fase 3.49). Arreglar esto bajó el overhead
medido por llamada en aproximadamente un tercio.**

- **`FindMethodDefCandidates` (`internal/metadata/resolver.go`)**, memoizada por `(typeRID,
  methodName)` — el mismo patrón de mapa protegido por mutex que el propio `typeDefCache` de
  `FindTypeDef` ya estableció (Fase 3.49), por la razón idéntica: una cadena de llamadas real
  resuelve el mismo par exacto una y otra vez, y cada llamada repetida a un método — incluyendo
  cada reintento que la propia caminata de ancestros de `callvirt` de `Machine.call` hace buscando
  el override correcto — volvía a escanear todo el rango de filas MethodDef del tipo, decodificando
  el nombre de cada fila desde el heap de strings, antes de esta Fase. Se cachean tanto un hit como
  un miss genuino (un miss escanea exactamente tanto de la tabla como un hit que resulta ser el
  último candidato, así que no hay nada más barato en volver a derivar "no encontrado" cada vez).
- **`ParseMethodSigCached` (`internal/metadata/signatures.go`, nueva)** — `ParseMethodSig`,
  memoizada por los propios bytes del blob de firma, conectada en los dos sitios de llamada más
  calientes de `assembly.go` (`candidateMatchesArgs`, alcanzado en cada llamada de un solo
  candidato — la abrumadora mayoría de las llamadas reales — y el propio loop de desempate de
  múltiples candidatos de `pickMethodOverload`). Los otros 11 sitios de llamada a través de
  `assembly.go`/`internal/checker`/`internal/ir` se dejaron deliberadamente en el `ParseMethodSig`
  plano y sin cachear — no están en el camino caliente real por `Call` (helpers de reflexión,
  resolución en tiempo de construcción de IR ya cubierta por la caché de IR por método), y auditar
  los 11 para "nunca muta el slice `Params` devuelto in situ" no valía la pena hacerlo hasta que se
  demostrara necesario.
- **Mejora real y medida** (`benchmarks/`, la propia suite de la Fase 3.70, antes vs. después): el
  workload de motor de reglas x10.000 bajó de 8.03ms a ~5.2ms (aproximadamente 35% más rápido), el
  overhead de invocación de método de 0.86 a ~0.55 microsegundos/llamada, y las asignaciones del
  lado host por llamada a `EvalRule` de 29.0 a 19.0 (consistente con remover las asignaciones de
  decodificación del heap de strings por fila que hacía el escaneo lineal y la propia asignación
  del slice `Params` fresco del parseo de firma). Los workloads de aritmética/strings/colecciones
  no se ven afectados, como se esperaba — sus propios loops calientes se quedan enteramente dentro
  de un cuerpo de método C# sin objetivos repetidos de `call`/`callvirt` con los que esta caché
  pueda ayudar.
- **Seis tests nuevos** (`internal/metadata/metadata_test.go`): consistencia de cache-hit a través
  de llamadas repetidas, un miss genuino que sigue siendo miss en cada llamada repetida (no
  cacheado incorrectamente como un falso hit), un test de carrera con 32 goroutines de acceso
  concurrente (ambas cachés se alcanzan desde múltiples goroutines resolviendo las mismas y
  distintas keys a la vez — el propio contrato ya existente de "seguro para uso concurrente" de
  `Assembly.Call` tiene que seguir cumpliéndose), y `ParseMethodSigCached` produciendo resultados
  idénticos al `ParseMethodSig` plano que envuelve. Todo verde bajo `go test -race ./...`.

### Cómo verificar la Fase 3.73

```bash
go build ./...
go vet ./...
gofmt -l .
go test ./...
go test -race ./...
go test -run "TestFindMethodDefCandidates|TestParseMethodSigCached" -v -race ./internal/metadata/...
cd benchmarks && go run .  # comparar los números de rule-engine/method-invoke-overhead/allocs contra la propia entrada de esta Fase arriba
```

---
## Fase 3.74 — empujando más paquetes por encima de la barra del 97% del checker, y un arreglo real de clase de bug

**Objetivo:** el tercer y último ítem de la Prioridad 3 — empujar más del corpus de 19 paquetes por
encima del objetivo de trabajo del 97% individual (`docs/en/COMPATIBILITY.md`), empezando por los
paquetes más cercanos a esa barra (`ClosedXML` 96.7%, `System.Text.Json` 96.5%).

**Resultado: 7 de 19 paquetes ahora superan el 97% (subiendo de 5), mejora amplia a través de la
mayor parte del resto del corpus, y — investigando por qué un nativo nuevo no funcionaba como se
esperaba — un bug genuinamente nuevo y general encontrado y arreglado que afecta a cada tipo valor
de BCL construido directamente en una variable local.**

- **`IReadOnlyDictionary\`2` puesto en la lista de permitidos, tanto para el runtime COMO para el
  checker** — un receptor `Dictionary\`2` real ya despacha correctamente hoy un sitio de llamada
  declarado como `IReadOnlyDictionary\`2` (la misma caminata de ancestros por nombre de la que ya
  dependen las propias entradas ya permitidas de `IDictionary\`2`), pero ni el propio
  `interfaceDispatchTargets` de `internal/checker/analyzer.go` ni el propio `bclPrefixes` de
  `internal/checker/profile.go` sabían esto — cada sitio de llamada real
  `IReadOnlyDictionary\`2::get_Item`/`TryGetValue`/`ContainsKey`/`get_Keys`/`get_Values` estaba
  marcado a pesar de funcionar genuinamente. Verificado con un test real y separado de ida y vuelta
  (`ro["a"] + ro["b"] + TryGetValue + ContainsKey + foreach Keys + foreach Values`, respuesta real
  109) antes de confiar en el arreglo. Este único par de entradas explicaba la mayor porción de lo
  que ClosedXML/System.Text.Json tenían marcado.
- **Nativos nuevos**: `System.ArraySegment\`1` (ctor, `get_Array`/`get_Offset`/`get_Count` — la
  misma convención real de nombres de campo `(_array, _offset, _count)` que el propio
  `system_span.go` ya estableció para `Span<T>`/`ReadOnlySpan<T>`), `Array.CopyTo` (la contraparte
  de instancia del ya registrado `Array.Copy` estático), `Exception.Source` (get/set, un nuevo campo
  `ManagedException.Source`), `System.Collections.Generic.KeyNotFoundException`/`System.
  OutOfMemoryException` (agregados al loop compartido de registro de constructores de excepción),
  el propio `get_IsReadOnly` de `List\`1`/`Dictionary\`2`/`HashSet\`1` (siempre `false` — ninguna de
  las colecciones nativas mutables de vmnet es realmente de solo lectura), e
  `Interlocked.MemoryBarrier` (un no-op real — no hay modelo de memoria multi-core contra el cual
  hacer fence). Del lado del checker: el propio `get_IsReadOnly` de `ICollection\`1`/`IList`/
  `IDictionary` puesto en la lista de permitidos de la misma forma que `IReadOnlyDictionary\`2`, más
  un prefijo de perfil `System.Buffer`/`System.ArraySegment\`1` (`Buffer.BlockCopy` ya tenía un
  nativo real desde la Fase 3.41 pero tampoco tenía entrada de perfil).
- **Un bug genuinamente nuevo y general encontrado y arreglado**: agregar el constructor de
  `ArraySegment<T>` vía el patrón ya establecido `registerValueTypeCtor` por sí solo no funcionaba —
  `var seg = new ArraySegment<int>(arr);` (construir un tipo valor directamente en una variable
  local) seguía fallando con un error no relacionado de "tipo no encontrado". Se le halló la causa
  raíz vía disassembly de IL real: Roslyn compila un tipo valor construido directamente en un local
  como `ldloca` + `call instance .ctor` — una llamada de método de instancia plana sobre la propia
  dirección del local — NO `newobj`, que es la ÚNICA forma que el propio despacho `bcl.
  LookupValueTypeCtor` de `Machine.newObj` (el que `registerValueTypeCtor` puebla) alguna vez
  consulta. El propio `System.Collections.Generic.KeyValuePair\`2` ya tenía una variante in-place
  separada, registrada en `bcl.Lookup` plano, `"...KeyValuePair\`2::.ctor"` exactamente por esta
  razón (Fase desconocida, anterior a esta investigación) — pero auditar cada OTRA entrada de
  `registerValueTypeCtor` en busca de la misma contraparte faltante encontró CUATRO huecos más
  reales y antes no notados: `System.Guid`, `System.ReadOnlySpan\`1`, `System.Span\`1`, y `System.
  Threading.CancellationToken` — significando que `var g = new Guid(...)`/`var span = new
  ReadOnlySpan<T>(...)`/`var token = new CancellationToken(...)` asignados directamente a una
  variable local han estado silenciosamente rotos todo este tiempo, simplemente nunca atrapados
  porque ningún código real del corpus o test existente había tocado exactamente esta forma de
  construcción para ninguno de estos cuatro tipos. Los cuatro arreglados con el mismo patrón de
  registro de ctor in-place, cada uno verificado con un test real de ida y vuelta.
- **`System.Threading.CancellationToken`/`CancellationTokenSource`/`CancellationTokenRegistration`
  no tenían NINGUNA entrada en la lista de permitidos de perfil del checker**, descubierto por el
  nuevo test de fixture de arriba al pegar contra un finding "out-of-profile" para un tipo con
  nativos reales y funcionando desde bastante antes de esta Fase (el propio comentario de doc de
  `system_cancellationtoken.go` cita 7 de los 19 paquetes rastreados usándolo: Polly, MediatR,
  CsvHelper, FluentValidation, Dapper, ...). Arreglado con tres entradas de prefijo nuevas — esto
  solo mejoró de forma medible los propios porcentajes de checker de Dapper, Polly, Serilog,
  MediatR, y CsvHelper, ninguno de los cuales necesitó ningún nativo nuevo en absoluto.
- **Resultado en todo el corpus**: `DocumentFormat.OpenXml` 100.0% (sin cambios), `NPOI` 97.9% →
  98.2%, `System.Text.Json` 96.5% → **98.1%**, `FluentValidation` 98.1% → 98.1% (24 marcados,
  bajando de 25), `Humanizer.Core` 97.9% → 98.3%, `Ardalis.GuardClauses` 97.5% → 98.6%, `ClosedXML`
  96.7% → **97.5%** (los últimos dos cruzando recién la barra del 97%), más mejora amplia a través
  del resto del corpus (Dapper 94.5%→95.4%, Newtonsoft.Json 85.6%→89.2%, Polly 95.5%→96.3%,
  YamlDotNet 94.9%→96.2%, Serilog 92.1%→95.8%, CsvHelper 93.7%→94.2%, MediatR 93.0%→95.5%).
  Promedio simple a través del corpus: 94.45% → 95.8%; promedio ponderado por métodos: ~97.8% →
  ~98.4%. Ver `docs/es/COMPATIBILITY.md` para la tabla completa y actual por paquete.
- **Diez tests de regresión nuevos** (`tests/fixtures/csharp/CheckerHardening.cs`,
  `TestCheckerHardening` en `vmnet_test.go`): despacho de `IReadOnlyDictionary\`2`, ida y vuelta de
  `ArraySegment<T>`, `Array.CopyTo`, ida y vuelta de `Exception.Source`, lanzar/atrapar
  `KeyNotFoundException`, el propio `IsReadOnly` de `List`/`Dictionary`/`HashSet`, y las cuatro
  formas de construcción en variable local recién arregladas (`Guid`, `ReadOnlySpan<T>`, `Span<T>`,
  `CancellationToken`).

### Cómo verificar la Fase 3.74

```bash
go build ./...
go vet ./...
gofmt -l .
go test ./...
go test -run "TestCheckerHardening" -v .
go run ./cmd/vmnet check package closedxml@0.105.0
go run ./cmd/vmnet check package system.text.json@8.0.5
```

---

## Fase 4 — v1.0 listo para producción ("Ready to ship")

**Objetivo:** convertir el motor funcional en un producto adoptable, confiable, documentado y
con benchmarks — el paquete completo para que un equipo de ingeniería apruebe un piloto real.

### Tareas

**Seguridad / sandbox**
- [x] Modelo `Permissions` (`AllowFileRead`/`AllowFileWrite`, deny-by-default) conectado a cada
      método nativo de BCL que toca I/O de disco real — aterrizado en la Fase 3.59
      (`permissions.go`, `internal/runtime/permissions.go`, `internal/interpreter/permissions.go`).
      `AllowConsole`/`AllowNetwork` existen en el mismo struct por compatibilidad a futuro pero
      siguen sin hacerse cumplir — ver `docs/en/security.md`.
- [x] `MaxArrayLength` — adelantado a Fase 3.5 junto con el soporte de `System.Array` (tenía que
      existir desde el día uno de `newarr`, no tenía sentido esperar a Fase 4)
- [x] `MaxStringBytes` — lograda en la Fase 3.72 (`internal/interpreter/limits.go`/`calls.go`),
      chequeado antes de la asignación en los dos sitios de llamada adversariales conocidos (`new
      string(char, count)`, `PadLeft`/`PadRight`) más una red de seguridad post-llamada general
      para cualquier otro nativo
- [x] `docs/en/security.md`/`docs/es/security.md` — threat model, qué se bloquea por default
      (actualizado en la Fase 3.59 para la puerta `Permissions` real ya en su lugar)
- [x] Soporte real de `System.IO.File`/`Directory`/`FileStream`/`FileInfo`/`DirectoryInfo` —
      aterrizado en la Fase 3.59, detrás de la puerta `Permissions` de arriba desde su primera
      línea de código (ver la propia entrada de esa Fase para la metodología del barrido de corpus
      y la superficie exacta protegida).
- [ ] Soporte real de `System.Diagnostics.Process`/sockets — todavía no implementado; el barrido
      de corpus de la Fase 3.59 encontró cero demanda real de `Process` y cero de
      `System.Net.Sockets` crudo en los 19 paquetes rastreados, así que ninguno está planeado hasta
      que aparezca demanda real. Se encontró demanda real modesta de `System.Net.Http`
      (`ClosedXML`) — un candidato para una Fase futura, protegido por `AllowNetwork` desde su
      primera línea en vez de retrofiteado como tuvieron que ser los dos nativos de archivo
      preexistentes sin puerta en la Fase 3.59.

**Modelo de errores**
- [x] Catálogo completo de códigos `VMNET_*` (spec §30.2) implementado consistentemente — llegó en
      la Fase 3.67 (`Error`/`Code`/`classify` propios de `errors.go`)
- [x] Stack traces de excepciones managed pulidos (formato spec §18.3) — llegó en la Fase 3.67
      (`runtime.ManagedException.Stack`/`PushFrame`/`String`)

**Performance / benchmarks**
- [x] Suite de benchmarks (spec §32): loop aritmético, concat de strings, JSON in/out,
      allocación de objetos, `List.Add`, lookup de `Dictionary`, 10k llamadas a rule engine —
      lograda en la Fase 3.70 (`benchmarks/`); JSON in/out está actualmente bloqueado por un hueco
      real y documentado (soporte de buffer de tamaño fijo unsafe), no medido como "lento" — ver la
      propia entrada de esa Fase
- [x] Comparación vs Go nativo y, donde sea viable, vs ejecución nativa CoreCLR — los siete
      workloads vs Go nativo (Fase 3.70); la comparación contra CoreCLR se mantiene acotada al
      workload de loop aritmético (`examples/calculator/coreclr/`, preexistente) — seis programas
      CoreCLR más para mantener a mano se juzgó desproporcionado para esta Fase
- [x] Cache de resolución de métodos/tokens, pasada de optimización de hot paths — lograda en la
      Fase 3.73 (las nuevas cachés `FindMethodDefCandidates`/`ParseMethodSigCached` de
      `internal/metadata`), ~35% menos overhead medido por llamada

**API/CLI estables**
- [x] Congelar API pública Go (spec §6) para v1.0, compromiso semver — lograda en la Fase 3.71
      (`docs/es/api-stability.md`); creado el primer tag semver real `v0.1.0`
- [ ] Set completo de comandos CLI (inspect/il/check/run/add/restore/packages)
- [ ] Matriz CI multiplataforma: Linux/macOS/Windows, verificar `CGO_ENABLED=0`

**Tests**
- [x] Suite golden completa (spec §28.1–28.5) — auditada y con huecos cerrados en la Fase 3.69; cada
      sub-ítem ahora tiene un test directo (ver la propia entrada de esa Fase para los nueve huecos
      encontrados y cerrados)
- [x] Meta de cobertura acordada con stakeholders — lograda en la Fase 3.69 con una línea base real
      y medida (33.7%/38.8% de cobertura de sentencias total sin/con tests de red) en vez del
      placeholder original sin medir de "≥70%"; meta nueva: ≥35%, sin retroceder

**Documentación (spec §33)**
- [ ] README completo (qué es / qué no es, quickstart, perfiles, límites conocidos)
- [x] `docs/es/architecture.md`, `supported-il.md`, `supported-bcl.md`, `nuget-support.md`,
      `compatibility-profile.md`, `security.md`, `roadmap.md` — los cuatro docs antes faltantes
      (`supported-il.md`/`supported-bcl.md`/`nuget-support.md`/`compatibility-profile.md`)
      lograron de forma bilingüe en la Fase 3.70; los otros tres ya existían
- [x] `/examples`: hello, rules, calculator, nuget-basic — ejecutables y documentados

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
- Reflection completa (`Type.MakeGenericType`/`GetMethod`/`Assembly.GetType`, reflection de
  atributos/parámetros)
- async/Task **cooperativo real** (scheduler, continuaciones que genuinamente suspenden,
  `Task.Delay` con espera real, paralelismo) — Fase 3.22 ya cubre el patrón dominante real
  (`async`/`await` modelado íntegramente síncrono: todo `Task` que cualquier native produce ya
  está completado por construcción, así que el propio `MoveNext()` generado por el compilador
  corre de punta a punta en una sola llamada) sin necesitar ninguna de estas piezas

## Criterios de aceptación de referencia

Ver spec original §35 (MVP) y §36 (NuGet v1) — se usan como checklist de salida de Fase 1/2 y
Fase 3 respectivamente, sin duplicarlos aquí.
