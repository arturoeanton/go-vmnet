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
| 3 | Checker + ecosistema NuGet | 6–9 sem | Adopción de bajo riesgo + reuso de librerías existentes | `vmnet check` sobre DLL real + `vmnet add/restore` de un NuGet real |
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
- [ ] DOS header, PE header, COFF header, optional header
- [ ] Section headers + conversión RVA → file offset
- [ ] Localización de CLI header y metadata root
- [ ] Errores: `ErrInvalidPE`, `ErrMissingCLIHeader`, `ErrInvalidRVA`, `ErrInvalidMetadataRoot`
- [ ] Tests: PE válido/inválido, sin CLI header, RVA inválido, múltiples secciones

**`/metadata` — metadata loader**
- [ ] Streams: `#~`, `#Strings`, `#US`, `#Blob`, `#GUID`
- [ ] Tablas core: Module, TypeRef, TypeDef, Field, MethodDef, Param, MemberRef, Constant,
      StandAloneSig, Assembly, AssemblyRef (resto de tablas de §10.2 deben parsear sin fallar
      aunque no se usen todavía)
- [ ] Modelo de tokens + resolución de coded indexes
- [ ] Parser de signatures: primitivos, `SZARRAY`, `CLASS`, `VALUETYPE` (generics → Fase 2/3)
- [ ] Tests por tabla + decodificación de signatures

**`/il` — decoder**
- [ ] Tabla de opcodes + decoder del set v0.1 (spec §11.2 completo)
- [ ] `Instruction{Offset, OpCode, Operand}` con tracking de offsets
- [ ] Reconocer (sin ejecutar) opcodes v0.2+ como "unsupported" en vez de crashear

**`/ir`**
- [ ] Set de instrucciones IR (`LoadArg`, `LoadLocal`, `StoreLocal`, `LoadConstI4`, `Add`,
      `Call`, `Branch`, `Return`, ...)
- [ ] Builder IL → IR

**`/interpreter` + `/runtime` (mínimo viable)**
- [ ] Frame/stack model, loop `eval`, dispatch
- [ ] Aritmética + branches + loops
- [ ] Resolución e invocación de métodos static
- [ ] Modelo runtime mínimo de Type/Method/Field
- [ ] Allocación de objetos básica + lectura/escritura de fields (necesario para criterio de
      aceptación #6-7 del MVP)
- [ ] Límites de stack/call depth: `ErrStackOverflow`, `ErrCallDepthExceeded`

**`/bcl` (subset v0.1)**
- [ ] `System.Object`, `System.String` (Concat/Length), `System.Math` (Abs, etc.),
      `System.Console.WriteLine`, tipos primitivos con boxing básico
- [ ] Mecanismo `NativeMethod` de registro

**`/cmd/vmnet` CLI**
- [ ] `vmnet inspect <dll>` — lista tipos/métodos
- [ ] `vmnet il <dll> <Type.Method>` — vuelca IL decodificado
- [ ] `vmnet run <dll> <Type.Method> <args>` — ejecuta método static

**API pública Go (subset de §6.1)**
- [ ] `vmnet.New()`, `VM.LoadFile/LoadBytes`, `Assembly.Call`, tipos `Value` mínimos

**Tests / aceptación**
- [ ] Golden tests: `SimpleMath.Add`, `Strings.Hello`, `Loops.Sum`
- [ ] Criterios de aceptación MVP §35 #1–5, #9, #10 (parcial), #11, #12

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
- [ ] Jerarquía de clases (BaseType, Interfaces), `callvirt` con vtable + null check
- [ ] `newobj` + ejecución de constructores
- [ ] Lectura/escritura de fields de instancia y estáticos
- [ ] Boxing/unboxing de value types
- [ ] Interface dispatch básico

**`/bcl` (subset v0.2)**
- [ ] `System.String` ampliado (Substring, Equals, ToUpper/Lower, Split, Format básico)
- [ ] `System.Array` + soporte runtime de `SZARRAY`
- [ ] `System.Collections.Generic.List<T>` (backing nativo Go)
- [ ] `System.Collections.Generic.Dictionary<K,V>` (backing nativo Go)
- [ ] `System.DateTime`, `System.TimeSpan`, `System.Guid` básicos
- [ ] `System.Text.Encoding` (UTF8 GetBytes/GetString) — necesario para el bridge `CallBytes`
- [ ] Jerarquía `System.Exception` + throw/catch/finally

**Generics (mínimo, spec §17.1)**
- [ ] `GenericInstance` + resolución de `MethodSpec`/`TypeSpec` limitada a `List<T>`/`Dictionary<K,V>`

**Excepciones**
- [ ] `ManagedException` + captura de stack trace (formato spec §18.3)
- [ ] Separación `VMError` vs `ManagedExceptionError`
- [ ] Soporte IL de try/catch/finally (`leave`, `leave.s`, `endfinally`)

**JSON bridge + API pública**
- [ ] `Assembly.CallBytes`, `Assembly.CallJSON`
- [ ] Set completo de `Value` + marshaling Go ↔ managed para objetos/maps

**Sandbox v1**
- [ ] `Limits{MaxInstructions, MaxHeapBytes, MaxCallDepth, MaxStackDepth}` conectado al eval loop
- [ ] `ErrInstructionLimitExceeded`, `ErrOutOfMemory`
- [ ] `Permissions` stub (solo `AllowConsole`, resto deny-by-default) — modelo completo en Fase 4

**Tests**
- [ ] Fixtures `Objects`, `CollectionsTest`, `ExceptionTest` (spec §29.4–29.6)
- [ ] Test de plugin con loop infinito muerto por `MaxInstructions`

### Demo de cierre de Fase 2 — "Esto es el producto" (~10–15 min)

1. `Rules.dll` realista: `Rules.Engine.Eval(byte[]) -> byte[]` con una clase `Customer`, una
   `List<LineItem>`, un `Dictionary<string,decimal>` de impuestos, y una excepción lanzada ante
   input inválido.
2. Desde un host Go (un mini checkout service), `asm.CallJSON("Rules.Engine", "Eval", cartJSON)`
   devuelve totales calculados — JSON in/out sin código de serialización manual.
3. Input inválido a propósito → excepción managed capturada como error Go legible, con stack
   trace.
4. Cargar un segundo DLL "buggy" con loop infinito → `MaxInstructions` lo mata en milisegundos
   en vez de colgar el proceso host.
5. Reemplazar `Rules.dll` por `Rules_v2.dll` en caliente, sin recompilar ni reiniciar el
   binario Go — remarcar "lógica de negocio hot-swappable".

**Mensaje de venta:** "Esto es lo que un cliente compra: reglas de negocio en C# embebidas de
forma segura en un servicio Go, con aislamiento de fallas y un one-liner de JSON in/out."

---

## Fase 3 — Checker de compatibilidad + ecosistema NuGet ("Confianza + reuso")

**Objetivo:** bajar el riesgo de adopción diciendo exactamente qué corre y qué no, y abrir la
puerta a reusar paquetes NuGet ya publicados en vez de depender solo de DLLs propias.

### Tareas

**`/checker`**
- [ ] Analyzer: uso de opcodes, grafo de llamadas, assemblies referenciados, uso de generics,
      exception handlers, custom attributes, detección de P/Invoke, punteros unsafe, uso de
      reflection, detección de async state machines
- [ ] Modelo de reporte (compatible / partial / unsupported) con razones y sugerencias (spec
      §23.2–23.4)
- [ ] Perfiles `minimal`, `rules`, `netstandard-lite` (spec §24) — validación contra la IR
- [ ] `vmnet check <dll>` CLI

**`/nuget`**
- [ ] Lector de `.nupkg` (zip)
- [ ] Parser de `.nuspec` (metadata + dependency groups)
- [ ] Parser de TFM + selección (prioridad `netstandard2.0`, spec §22.5)
- [ ] Resolver de dependencias transitivo (versión simple)
- [ ] Cache local de paquetes
- [ ] Formato de lockfile + generación (spec §22.6)
- [ ] CLI: `vmnet add`, `vmnet restore`, `vmnet packages`
- [ ] Detección de native assets (`runtimes/*`) → marcados unsupported en modo pure-go

**`/bcl` (subset v0.3, solo lo necesario para certificar paquetes elegidos)**
- [ ] `System.Linq.Enumerable` subset (Where/Select/ToList/Count/Any)
- [ ] `System.Nullable<T>`
- [ ] `System.Convert`, `System.Globalization.CultureInfo` básico

**Certificación de paquetes**
- [ ] Elegir y certificar 2–3 paquetes NuGet reales de lógica pura contra el perfil
      `netstandard-lite` (candidatos: librería de validación liviana, utilidades, evaluar
      subset de NodaTime) → esto produce el activo de marketing "lista de paquetes certificados"

**Reflection-lite (mínimo spec §19.2, solo lo que pida el checker/BCL)**
- [ ] `typeof(T)`, `object.GetType()`, `Type.FullName/Name/Namespace`

**Tests**
- [ ] Checker: unsupported opcode, unsupported BCL call, P/Invoke, async, reflection, native
      asset (spec §28.6)
- [ ] NuGet: parseo `.nupkg`/`.nuspec`, selección de TFM, dependencias transitivas, lockfile
      (spec §28.7)

### Demo de cierre de Fase 3 — "Sabemos qué funciona, y reusamos el ecosistema" (~10 min)

1. `vmnet check` sobre una DLL real compleja (algo con reflection pesada o async) → reporte
   claro de "unsupported" con razones concretas, no un stack trace crudo.
2. `vmnet check` sobre uno de los paquetes certificados → reporte "compatible" o "partial".
3. `vmnet add SomePackage@x.y.z` + `vmnet restore` en vivo, mostrando resolución de
   dependencias + lockfile generado.
4. Llamar una función de ese paquete NuGet real desde Go vía `vmnet.LoadPackage(...)`.

**Mensaje de venta:** "No prometemos el mundo — probamos, de forma transparente, exactamente
qué código C# corre, y ya estamos reusando paquetes NuGet reales publicados, no solo DLLs de
juguete propias."

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
