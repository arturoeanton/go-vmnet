# Arquitectura

Ver `docs/spec.md` (especificación completa) y `docs/ROADMAP.md` (plan de
entrega en 4 fases). Este documento es el mapa rápido de "qué vive dónde" en
el repo tal como está implementado hoy — se amplía a medida que cada fase
agrega comportamiento real.

## Pipeline (spec §8)

```txt
.dll (PE/CLI)
  → internal/pe          lee headers PE/COFF, ubica CLI header + metadata root
  → internal/metadata    parsea streams (#~ #Strings #US #Blob #GUID) y tablas
  → internal/il          decodifica method bodies IL a instrucciones tipadas
  → internal/ir          normaliza IL a la IR propia de vmnet
  → internal/interpreter evalúa la IR sobre un frame/stack, con límites
  → internal/runtime     modelo de objetos managed (Type/Method/Field/Heap)
  → internal/bcl         System.* implementado nativamente en Go
```

`internal/nuget` y `internal/checker` son transversales: el primero resuelve
paquetes `.nupkg` hacia assemblies que entran por el mismo pipeline; el
segundo analiza metadata/IR antes de ejecutar para reportar compatibilidad
(spec §23).

## Layout de paquetes

```txt
/                     package vmnet — API pública (spec §6)
/internal/pe          PE/CLI loader (spec §9)
/internal/metadata    metadata loader + signatures (spec §10)
/internal/il          IL decoder (spec §11)
/internal/ir          IR intermedia (spec §12)
/internal/interpreter stack-based interpreter (spec §13)
/internal/runtime     modelo de objetos managed (spec §14-15, 17-18, 20)
/internal/bcl         BCL parcial (spec §16)
/internal/nuget       .nupkg/.nuspec/TFM/resolver (spec §22)
/internal/checker     compatibility checker (spec §23-24)
/cmd/vmnet            CLI (spec §27)
/examples             hello, rules, calculator, nuget-basic
/tests/fixtures       fixtures C# usadas como golden input
/tests/golden         salidas esperadas para tests table-driven
```

Por qué `/internal` en vez del layout plano de la spec: ver
`docs/adr/0002-package-layout.md`.

## Por qué pure-Go (sin CoreCLR en el núcleo)

Ver `docs/adr/0001-pure-go-core.md`.

## Estado actual

Fase 0 (bootstrap), Fase 1 (núcleo IL), Fase 2 (motor de reglas) y Fase 2.5
(endurecimiento) completas. El pipeline `.dll → internal/pe →
internal/metadata → internal/il → internal/ir → internal/interpreter →
internal/bcl` corre de punta a punta contra un assembly real compilado con
el SDK de .NET (`tests/fixtures/csharp`), expuesto por la API pública
(`vmnet.New()`, `Assembly.Call`/`CallBytes`/`CallJSON`) y por el CLI
(`vmnet inspect` / `vmnet il` / `vmnet run`). Alcance actual: métodos
static e instancia, `newobj`/`callvirt`/fields (sin vtable — resolución
directa), `List<T>` / `Dictionary<string,V>` con backing nativo Go,
`throw` no manejado (propagado como error Go tipado,
`vmnet.ManagedException`), y el bridge `byte[]`/JSON. Interface/vtable
dispatch, `try/catch/finally`, generics más allá de List/Dictionary, y
`DateTime`/`Guid` quedan para fases siguientes (`docs/ROADMAP.md`) — el IR
builder reporta cualquier opcode no soportado explícitamente en vez de
ejecutarlo mal. (`System.Array` se agregó en Fase 3.5 — ver más abajo.)

Además (Fase 2.5): el intérprete recupera cualquier panic en el borde
público (`Machine.Invoke`) en vez de crashear el proceso host, aplica
`MaxStackDepth` de verdad, `*vmnet.Assembly` es seguro para llamar desde
múltiples goroutines (`sync.RWMutex` sobre los caches de métodos/tipos,
verificado con `-race`), y `internal/pe`, `internal/metadata` e
`internal/il` tienen fuzz tests nativos de Go (corridos manualmente por
~16.8M ejecuciones combinadas sin panics).

Fase 3 (checker + NuGet) completa. `internal/checker` reutiliza el pipeline
real (no una reimplementación heurística aparte) para decidir si un
assembly es `compatible`/`partial`/`unsupported` por perfil
(`minimal`/`rules`/`netstandard-lite`). `internal/nuget` lee `.nupkg`/
`.nuspec` reales (forma corta y larga de TFM), resuelve dependencias
transitivas contra `api.nuget.org` (highest-version-wins, documentado como
simplificación), cachea en `.vmnet/packages/` y expone
`vm.NuGet().Add/Restore/Packages()` + `vm.LoadPackage(id)`. Certificado
contra 7 paquetes NuGet reales y populares (ver `docs/ROADMAP.md` para la
tabla completa); 3 de ellos tienen una función real ejecutando
correctamente a través de vmnet. El proceso de certificación encontró y
corrigió dos gaps reales: resolución de `MethodSpec` (llamadas a métodos
genéricos) y un bug de comparaciones sin signo (`.un` opcodes) que daba
resultados silenciosamente incorrectos, no solo "no soportado".

Fase 3.5 (endurecimiento + compatibilidad real de DLLs) completa. El motor
ahora soporta `System.Array` (`newarr`/`ldlen`/`ldelem.*`/`stelem.*`, solo
SZARRAY, con `Limits.MaxArrayLength`), punteros administrados para `ref`/
`out` (`ldarga`/`ldloca`/`ldelema`/`ldflda` + `ldind.*`/`stind.*` —
modelados como un `*runtime.Value` de Go apuntando dentro de un slice de
tamaño fijo, sin ningún caso especial en `Call`/`NewObj`) y campos
estáticos con `.cctor` perezoso (`ldsfld`/`stsfld`, `sync.Once` por
`Type`). Re-certificado contra los mismos 7 paquetes de Fase 3: el
promedio de métodos limpios subió de ~45.5% a ~56.8% (`docs/ROADMAP.md`
tiene la tabla completa por paquete). El proceso encontró y corrigió tres
bugs reales de concurrencia/correctitud que no existían como riesgo antes
de que `runtime.Type` empezara a cargar estado mutable: un deadlock de
reentrancia cuando un `.cctor` escribe su propio campo estático, una race
condition en el cache de tipos de `Assembly` que podía duplicar un `Type`
bajo acceso concurrente, y un `default(T)` incorrecto para campos value-type
nunca asignados explícitamente (ahora resuelto parseando la firma real del
campo — `metadata.ParseFieldSig`, nuevo). También detectó y corrigió dos
casos de "drift" en `internal/checker` (el perfil `minimal` no excluía
arrays/static fields como debía, y `sigShapeFindings` seguía marcando
`ref`/`out` como no soportado después de que sí se implementó) — ambos
atrapados por el propio test de dogfood del checker.

Fase 3.6 (primera sub-fase del camino a 85% de compatibilidad, ver
`docs/ROADMAP.md`) completa: opcode `switch` (ya decodificado desde Fase 1
pero nunca bajado a IR) y una tanda de nativos BCL de alto alcance
(`StringBuilder`, `String.Format`/`Substring`/indexador/`Equals`,
`Array.Empty`, `Double.IsNaN`, stubs de `CultureInfo`/`Environment`).
Expuso el primer caso concreto del límite ya documentado de "callvirt sin
vtable real": el compilador de C# emite `StringBuilder.ToString()` como
`callvirt Object::ToString`, confiando en el despacho virtual real del
CLR — vmnet lo resuelve estáticamente por el `MemberRef` declarado, así
que sin un parche dirigido en `objectToString` siempre corría el
`ToString` genérico. El despacho virtual real (jerarquía de tipos +
`isinst`/`castclass`) es Fase 3.8. Certificación (7 paquetes de Fase 3 +
Jint, el motor de JavaScript completo para .NET usado como target del
demo de "lenguaje dinámico" planeado): promedio de los 7 paquetes sube de
~56.8% a ~59.8%; con Jint incluido, ~60.3%.

Fase 3.7 (value types) completa: el motor ahora modela structs de verdad
(`runtime.KindStruct`, copiados por valor vía `Value.Clone()` en cada
punto donde un valor entra a un slot persistente, no compartidos por
referencia como `Object`) — `initobj`/`ldobj`/`stobj`/`constrained.`,
`newobj` empujando el valor en vez de una referencia para un value type,
y `System.Nullable`1`. Encontró y arregló dos bugs reales expuestos
apenas se probó contra un fixture con structs: locals de tipo struct
arrancaban sin inicializar (el compilador de C# confía en la garantía
`InitLocals` de la CLI y omite `initobj` cuando puede probar que el
struct se sobreescribe completo antes de usarse — ahora
`runtime.Method.LocalDefaults` los prezera igual que ya se hacía para
campos), y un deadlock de recursión en el lock de resolución de tipos de
Fase 3.5 (asumía que construir un tipo nunca necesita resolver otro,
falso en cuanto un campo/local de struct referencia otro tipo — rediseño
verificado con `TestStructsConcurrentResolve`). Certificación: promedio
de los 7 paquetes sube de ~59.8% a ~63.2%; con Jint, ~63.6%.
