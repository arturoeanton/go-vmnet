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
  (docs/ROADMAP.md Fase 2) — el mecanismo de catch-por-jerarquía funciona iguales para
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

---

## Fase 4 — v1.0 listo para producción ("Ready to ship")

**Objetivo:** convertir el motor funcional en un producto adoptable, confiable, documentado y
con benchmarks — el paquete completo para que un equipo de ingeniería apruebe un piloto real.

### Tareas

**Seguridad / sandbox**
- [ ] Modelo `Permissions` completo (`AllowConsole/AllowFileRead/AllowNetwork`, deny-by-default)
      conectado a todos los métodos nativos de BCL
- [x] `MaxArrayLength` — adelantado a Fase 3.5 junto con el soporte de `System.Array` (tenía que
      existir desde el día uno de `newarr`, no tenía sentido esperar a Fase 4)
- [ ] `MaxStringBytes`
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
