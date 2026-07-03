<!--
  Especificación técnica original de vmnet/gocil, tal como fue provista para
  arrancar el proyecto (2026-07-02). Es la referencia canónica que
  docs/es/ROADMAP.md cita como "spec §N". Las decisiones que se apartan de este
  documento (p. ej. layout de paquetes con /internal) están documentadas por
  separado en docs/es/adr/.
-->

# Especificación técnica — `vmnet` / `gocil`

## 1. Resumen

Construir una librería Go llamada provisionalmente **`vmnet`** o **`gocil`** que permita ejecutar assemblies `.NET` / `C#` dentro de una aplicación Go usando un **intérprete de IL/CIL escrito en Go puro**.

El objetivo de producto es que el usuario final pueda usar DLLs C# o paquetes NuGet compatibles con una experiencia parecida a `goja`:

```go
vm := vmnet.New()

asm, err := vm.LoadFile("./Rules.dll")
if err != nil {
    panic(err)
}

out, err := asm.CallJSON("Rules.Engine", "Eval", map[string]any{
    "country": "AR",
    "amount":  1000,
})
if err != nil {
    panic(err)
}

fmt.Println(out)
```

La idea **no** es hostear CoreCLR con `hostfxr` como primera opción. Microsoft sí documenta el hosting nativo de .NET mediante `nethost` y `hostfxr`, pero ese enfoque requiere runtime instalado, `runtimeconfig.json`, resolución de runtime y complejidad externa. Ese modo puede existir como fallback futuro, no como núcleo del producto.

El núcleo debe ser:

```txt
Go app
  ↓
vmnet pure Go
  ↓
PE/CLI metadata loader
  ↓
IL decoder
  ↓
VM interpreter
  ↓
BCL parcial en Go
  ↓
DLL C# compatible / NuGet compatible
```

La especificación base de CLI/CIL está definida por ECMA-335, que cubre Common Language Infrastructure, metadata, execution model e instruction set. Esta especificación debe usarse como referencia primaria para metadata e IL.

---

## 2. Objetivo de producto

### 2.1 Tagline

```txt
Run C# plugins and selected NuGet packages inside Go, without requiring .NET at runtime.
```

En español:

```txt
Ejecutá plugins C# y paquetes NuGet seleccionados dentro de Go, sin requerir .NET instalado.
```

### 2.2 Posicionamiento

`vmnet` no debe venderse como ".NET completo en Go". El posicionamiento correcto es:

```txt
Un runtime IL embebible, escrito en Go puro, para reglas, plugins y lógica de negocio C# compatible.
```

### 2.3 Casos de uso

1. **Plugins C# para aplicaciones Go**
   Un producto escrito en Go permite que clientes enterprise escriban extensiones en C#.

2. **Migración incremental .NET → Go**
   El sistema nuevo está en Go, pero algunas reglas o módulos legacy siguen en C#.

3. **Ejecución de reglas de negocio**
   C# se usa como lenguaje de reglas, pricing, validaciones, cálculo fiscal, workflows simples.

4. **Reutilización de librerías NuGet compatibles**
   Ejecutar paquetes NuGet puros que no dependan de P/Invoke, threading complejo, ASP.NET, EF Core, WPF, etc.

5. **Sandbox controlado**
   Ejecutar lógica externa en un entorno más restringido que CoreCLR completo.

6. **Herramienta educativa / debugging de IL**
   Inspeccionar metadata, instrucciones IL, stack frames, llamadas y objetos.

---

## 3. No objetivos

Estos puntos deben quedar explícitos para evitar scope creep.

### 3.1 No implementar .NET completo en v1

No soportar inicialmente:

```txt
- ASP.NET Core
- Entity Framework Core
- WPF
- WinForms
- Reflection.Emit
- dynamic avanzado
- P/Invoke
- unsafe
- threading real completo
- async/await completo
- arbitrary NuGet packages
- cualquier DLL .NET existente sin restricciones
```

### 3.2 No usar CoreCLR en el núcleo

El modo principal debe ser Go puro:

```txt
Sin cgo.
Sin hostfxr.
Sin runtime .NET instalado.
Sin dependencia obligatoria de dotnet.
```

CoreCLR puede existir como backend opcional futuro:

```go
vm := vmnet.New(vmnet.Options{
    Backend: vmnet.Auto,
})
```

Pero no debe contaminar el diseño del intérprete puro.

### 3.3 No prometer compatibilidad binaria total

Mensaje correcto:

```txt
Compatible con assemblies C# compilados para el perfil vmnet.
```

Mensaje incorrecto:

```txt
Corre cualquier DLL .NET.
```

---

## 4. Referencias técnicas obligatorias

El developer debe tomar como referencia:

1. **ECMA-335**
   Define Common Language Infrastructure, metadata, CIL e execution model.

2. **Target Framework Monikers / TFMs**
   NuGet y SDK-style projects usan TFMs para identificar frameworks destino, por ejemplo `netstandard2.0`, `net8.0`, `net10.0`, etc.

3. **NuGet `.nuspec`**
   El `.nuspec` es el manifiesto XML del paquete y contiene metadata, dependencias y grupos por framework.

4. **.NET Standard**
   Para v1, el target recomendado es un subconjunto de `netstandard2.0`, porque expone una API común y ampliamente usada por librerías NuGet.

5. **CoreCLR hosting**
   Solo como referencia para backend futuro `coreclr`, usando `nethost` y `hostfxr`.

---

## 5. Nombre del proyecto

Opciones:

```txt
vmnet
gocil
goil
cilgo
dotgo
```

Recomendación:

```txt
Nombre de librería: vmnet
Nombre técnico interno: gocil
```

Motivo:

```txt
vmnet = nombre de producto.
gocil = describe el motor: CIL runtime in Go.
```

---

## 6. API pública esperada

### 6.1 API mínima v0.1

```go
package vmnet

type VM struct{}

type Options struct {
    Profile       Profile
    Debug         bool
    MaxStackDepth int
    MaxHeapBytes  int64
}

type Profile string

const (
    ProfileMinimal     Profile = "minimal"
    ProfileRules       Profile = "rules"
    ProfileNetStandard Profile = "netstandard-lite"
)

func New(opts ...Options) *VM

func (vm *VM) LoadFile(path string) (*Assembly, error)
func (vm *VM) LoadBytes(name string, data []byte) (*Assembly, error)

type Assembly struct{}

func (asm *Assembly) Call(typeName string, methodName string, args ...Value) (Value, error)
func (asm *Assembly) CallJSON(typeName string, methodName string, input any) (any, error)
func (asm *Assembly) CallBytes(typeName string, methodName string, input []byte) ([]byte, error)
```

### 6.2 API de uso simple

```go
vm := vmnet.New()

asm, err := vm.LoadFile("./plugins/Rules.dll")
if err != nil {
    panic(err)
}

result, err := asm.CallJSON("Rules.Engine", "Eval", map[string]any{
    "amount":  1500,
    "country": "AR",
})
if err != nil {
    panic(err)
}

fmt.Printf("%#v\n", result)
```

### 6.3 API de bajo nivel

```go
method, err := asm.ResolveMethod("Rules.Engine", "Eval")
if err != nil {
    return err
}

frame := vm.NewFrame(method)

frame.Push(vmnet.String("AR"))
frame.Push(vmnet.Int32(1500))

ret, err := vm.Invoke(frame)
```

### 6.4 API para NuGet futura

```go
vm := vmnet.New()

err := vm.NuGet().Add("NodaTime", "3.2.0")
if err != nil {
    panic(err)
}

err = vm.NuGet().Restore()
if err != nil {
    panic(err)
}

asm, err := vm.LoadPackage("NodaTime")
```

### 6.5 API con backend automático futuro

```go
vm := vmnet.New(vmnet.Options{
    Backend: vmnet.BackendAuto,
})
```

### 6.6 API de instancias (Fase 3.28)

`Call`/`CallBytes`/`CallJSON` (§6.1) solo invocan métodos **estáticos**.
Una API real orientada a objetos (`new Engine()`, `engine.Evaluate(...)`,
encadenar sobre el resultado) necesita construir instancias y llamar
métodos de instancia directamente desde Go — sin un ensamblado glue en
C# intermedio (ver `examples/jint-nowrapper` para el caso real).

```go
type Instance struct{}

func (in *Instance) Native() any
func (in *Instance) TypeName() string
func (in *Instance) Call(methodName string, args ...Value) (Value, error)

func (asm *Assembly) New(typeName string, args ...Value) (*Instance, error)
```

`New` construye una instancia vía su `.ctor` real, resuelto por
aridad/Kind de `args` (mismo mecanismo que `Call` para overloads
estáticos). `Instance.Call` invoca un método de instancia por nombre,
despachado como una llamada virtual real: primero el tipo concreto del
receptor, subiendo por toda su cadena de herencia si hace falta (ver
`internal/interpreter/calls.go`, `Machine.call`). Cualquier `Call`/
`Instance.Call` cuyo resultado sea un objeto o un value type ahora
devuelve un `*Instance` (antes: `nil` en silencio) para poder seguir
encadenando.

**Límite explícito:** esta API refleja el modelo de objetos real de CIL
(`newobj`/`callvirt`/campos), no el AZÚCAR SINTÁCTICO de C# que el
compilador resuelve en tiempo de compilación — parámetros opcionales
con valor por defecto, métodos de extensión, conversiones implícitas
definidas por el usuario. Código C# real que dependa de esas
conveniencias necesita el argumento explícito (parámetros opcionales) o
seguir usando un wrapper compilado (extensiones/conversiones implícitas)
— ver `examples/jint-nowrapper/README.md` para el caso concreto
encontrado con Jint (`Evaluate(string, string = null)`,
`JsValueExtensions.AsNumber`).

Backends:

```go
const (
    BackendPureGo Backend = "pure-go"
    BackendCoreCLR Backend = "coreclr"
    BackendWorker Backend = "worker"
    BackendAuto Backend = "auto"
)
```

En v1 solo es obligatorio `BackendPureGo`.

---

## 7. Arquitectura general

Estructura de carpetas recomendada:

```txt
/vmnet
  vm.go
  options.go
  assembly.go
  call.go
  value.go
  errors.go
  profile.go

/pe
  pe.go
  coff.go
  cli_header.go
  sections.go
  rva.go

/metadata
  metadata.go
  streams.go
  tables.go
  tokens.go
  signatures.go
  blobs.go
  strings.go
  userstrings.go
  coded_index.go
  resolver.go

/il
  opcode.go
  decoder.go
  instruction.go
  verifier.go

/ir
  ir.go
  builder.go
  normalize.go

/interpreter
  frame.go
  stack.go
  eval.go
  dispatch.go
  arithmetic.go
  branches.go
  calls.go
  exceptions.go

/runtime
  object.go
  class.go
  method.go
  field.go
  heap.go
  array.go
  string.go
  delegate.go
  exception.go
  generics.go
  interface.go

/bcl
  system_object.go
  system_string.go
  system_array.go
  system_math.go
  system_console.go
  system_exception.go
  system_datetime.go
  system_guid.go
  system_collections.go
  system_linq.go
  system_text.go
  system_io.go

/nuget
  nupkg.go
  nuspec.go
  restore.go
  resolver.go
  tfm.go
  cache.go
  lockfile.go

/checker
  analyzer.go
  unsupported.go
  report.go
  profile_rules.go

/cmd/vmnet
  main.go
  check.go
  run.go
  inspect.go
  restore.go
  repl.go

/examples
  hello
  rules
  calculator
  nuget-basic

/tests
  fixtures
  golden
```

> **Nota de implementación (ver `docs/es/adr/0002-package-layout.md`):** el repo
> implementa esta misma separación de responsabilidades, pero pone `/vmnet`
> en la raíz del módulo (paquete público `vmnet`, `import "github.com/arturoeanton/go-vmnet"`)
> y mueve `/pe /metadata /il /ir /interpreter /runtime /bcl /nuget /checker`
> bajo `/internal`, para no comprometer una API pública de bajo nivel mientras
> el diseño interno todavía cambia rápido en las Fases 1-3.

---

## 8. Pipeline interno

Carga y ejecución:

```txt
1. Leer archivo PE.
2. Encontrar CLI header.
3. Leer metadata root.
4. Leer streams:
   - #~
   - #Strings
   - #US
   - #Blob
   - #GUID
5. Parsear metadata tables.
6. Resolver TypeDef, TypeRef, MemberRef, MethodDef, FieldDef.
7. Decodificar firmas.
8. Decodificar cuerpos IL.
9. Convertir IL a IR propia.
10. Validar contra perfil soportado.
11. Crear runtime types.
12. Ejecutar método solicitado.
```

---

## 9. PE/CLI loader

### 9.1 Responsabilidades

El módulo `/pe` debe:

```txt
- Leer DOS header.
- Leer PE header.
- Leer COFF header.
- Leer optional header.
- Leer section headers.
- Convertir RVA a file offset.
- Encontrar CLI header.
- Encontrar metadata root.
```

### 9.2 API interna

```go
package pe

type File struct {
    Sections []Section
    CLI      *CLIHeader
    Metadata []byte
}

func Parse(data []byte) (*File, error)
func (f *File) RVA(rva uint32) ([]byte, error)
func (f *File) OffsetFromRVA(rva uint32) (uint32, error)
```

### 9.3 Validaciones

Debe rechazar:

```txt
- PE corrupto.
- Assembly sin CLI header.
- Metadata inexistente.
- RVA inválido.
- Method body fuera de rango.
```

Errores:

```go
ErrInvalidPE
ErrMissingCLIHeader
ErrInvalidRVA
ErrInvalidMetadataRoot
```

---

## 10. Metadata loader

### 10.1 Streams obligatorios

Soportar:

```txt
#~
#Strings
#US
#Blob
#GUID
```

### 10.2 Tablas iniciales

Implementar como mínimo:

```txt
Module
TypeRef
TypeDef
Field
MethodDef
Param
InterfaceImpl
MemberRef
Constant
CustomAttribute
StandAloneSig
PropertyMap
Property
MethodSemantics
MethodImpl
TypeSpec
Assembly
AssemblyRef
File
ManifestResource
NestedClass
GenericParam
MethodSpec
GenericParamConstraint
```

No todas se usan en v0.1, pero deben poder parsearse para no fallar con assemblies reales.

### 10.3 Tokens

Representar tokens:

```go
type Token uint32

const (
    TokenTypeDef     = 0x02000000
    TokenTypeRef     = 0x01000000
    TokenMethodDef   = 0x06000000
    TokenMemberRef   = 0x0A000000
    TokenFieldDef    = 0x04000000
    TokenTypeSpec    = 0x1B000000
    TokenMethodSpec  = 0x2B000000
    TokenString      = 0x70000000
)
```

### 10.4 Signature parser

Debe parsear signatures de:

```txt
- methods
- fields
- locals
- generic instantiations
- arrays
- SZARRAY
- classes
- valuetypes
- primitive types
```

Tipos iniciales:

```txt
ELEMENT_TYPE_VOID
ELEMENT_TYPE_BOOLEAN
ELEMENT_TYPE_CHAR
ELEMENT_TYPE_I1
ELEMENT_TYPE_U1
ELEMENT_TYPE_I2
ELEMENT_TYPE_U2
ELEMENT_TYPE_I4
ELEMENT_TYPE_U4
ELEMENT_TYPE_I8
ELEMENT_TYPE_U8
ELEMENT_TYPE_R4
ELEMENT_TYPE_R8
ELEMENT_TYPE_STRING
ELEMENT_TYPE_OBJECT
ELEMENT_TYPE_CLASS
ELEMENT_TYPE_VALUETYPE
ELEMENT_TYPE_SZARRAY
ELEMENT_TYPE_ARRAY
ELEMENT_TYPE_GENERICINST
ELEMENT_TYPE_VAR
ELEMENT_TYPE_MVAR
```

---

## 11. IL decoder

### 11.1 Responsabilidad

El módulo `/il` debe decodificar bytes IL en instrucciones estructuradas.

```go
type Instruction struct {
    Offset  int
    OpCode  OpCode
    Operand any
}
```

### 11.2 OpCodes v0.1

Soporte mínimo:

```txt
nop
break

ldarg.0
ldarg.1
ldarg.2
ldarg.3
ldarg.s
ldarg

starg.s
starg

ldloc.0
ldloc.1
ldloc.2
ldloc.3
ldloc.s
ldloc

stloc.0
stloc.1
stloc.2
stloc.3
stloc.s
stloc

ldloca.s
ldloca

ldnull
ldc.i4.m1
ldc.i4.0
ldc.i4.1
ldc.i4.2
ldc.i4.3
ldc.i4.4
ldc.i4.5
ldc.i4.6
ldc.i4.7
ldc.i4.8
ldc.i4.s
ldc.i4
ldc.i8
ldc.r4
ldc.r8

ldstr

add
sub
mul
div
div.un
rem
rem.un
and
or
xor
shl
shr
shr.un
neg
not

conv.i1
conv.i2
conv.i4
conv.i8
conv.u1
conv.u2
conv.u4
conv.u8
conv.r4
conv.r8

br.s
br
brfalse.s
brfalse
brtrue.s
brtrue
beq.s
beq
bge.s
bge
bgt.s
bgt
ble.s
ble
blt.s
blt
bne.un.s
bne.un

switch

call
callvirt
newobj
ret

ldfld
stfld
ldsfld
stsfld
ldflda
ldsflda

newarr
ldlen
ldelem.i4
ldelem.ref
stelem.i4
stelem.ref

box
unbox.any
isinst
castclass

throw
leave
leave.s
endfinally

dup
pop
```

### 11.3 OpCodes v0.2+

Agregar:

```txt
constrained.
readonly.
volatile.
tail.
initobj
cpobj
ldobj
stobj
mkrefany
refanyval
refanytype
sizeof
localloc
```

Muchos pueden marcarse como unsupported inicialmente, pero el decoder debe poder reconocerlos.

---

## 12. IR intermedia

No interpretar IL crudo directamente como arquitectura definitiva.

Convertir IL a una IR interna:

```go
type Instr interface{}

type LoadArg struct { Index int }
type LoadLocal struct { Index int }
type StoreLocal struct { Index int }
type LoadConstI4 struct { Value int32 }
type Add struct { Kind NumericKind }
type Call struct { Method *runtime.Method }
type Branch struct { Target int }
type Return struct{}
```

Ventajas:

```txt
- simplifica el interpreter
- permite validación previa
- facilita optimizaciones futuras
- permite codegen futuro IL → Go
- permite reportes claros de unsupported features
```

---

## 13. Interpreter

### 13.1 Modelo de ejecución

Usar stack-based interpreter:

```go
type Frame struct {
    Method *runtime.Method
    Args   []Value
    Locals []Value
    Stack  []Value
    IP     int
}
```

### 13.2 Loop principal

```go
func (vm *VM) eval(frame *Frame) (Value, error) {
    for frame.IP < len(frame.Method.IR) {
        instr := frame.Method.IR[frame.IP]
        err := vm.exec(frame, instr)
        if err != nil {
            return nil, err
        }
    }
    return frame.ReturnValue, nil
}
```

### 13.3 Stack safety

Configurable:

```go
type Limits struct {
    MaxStackDepth int
    MaxCallDepth  int
    MaxHeapBytes  int64
    MaxInstructions int64
}
```

Errores:

```go
ErrStackOverflow
ErrCallDepthExceeded
ErrInstructionLimitExceeded
ErrOutOfMemory
```

### 13.4 Determinismo

Debe haber opción para ejecución limitada:

```go
vm := vmnet.New(vmnet.Options{
    Limits: vmnet.Limits{
        MaxInstructions: 1_000_000,
        MaxHeapBytes: 64 << 20,
    },
})
```

Esto es clave para plugins y sandbox.

---

## 14. Runtime object model

### 14.1 Value

```go
type Value interface {
    Type() *Type
}

type Int32Value int32
type Int64Value int64
type Float64Value float64
type BoolValue bool
type StringValue struct {
    Obj *Object
}
type ObjectRef struct {
    Obj *Object
}
type NullValue struct{}
```

### 14.2 Object

```go
type Object struct {
    Type   *Type
    Fields []Value
}
```

### 14.3 Type

```go
type Type struct {
    Namespace string
    Name      string
    Kind      TypeKind

    BaseType   *Type
    Interfaces []*Type

    Fields  []*Field
    Methods []*Method

    GenericParams []GenericParam
    GenericArgs   []*Type
}
```

### 14.4 Method

```go
type Method struct {
    Name       string
    Owner      *Type
    Signature  MethodSig
    IsStatic   bool
    IsVirtual  bool
    IsCtor     bool
    IL         []il.Instruction
    IR         []ir.Instr
    NativeImpl NativeMethod
}
```

### 14.5 Virtual dispatch

Soportar:

```txt
- call directo
- callvirt con null check
- override por vtable
- interface dispatch básico
```

No optimizar al principio.

---

## 15. Heap y memoria

### 15.1 Opción inicial

Go GC puede administrar objetos del runtime:

```go
type Heap struct {
    objects []*Object
    bytes   int64
}
```

Aunque Go GC maneje memoria real, `vmnet` debe contar memoria lógica para límites.

### 15.2 API interna

```go
func (h *Heap) NewObject(t *Type) (*Object, error)
func (h *Heap) NewArray(elem *Type, length int) (*Object, error)
func (h *Heap) NewString(s string) (*Object, error)
```

### 15.3 String interning

Implementar string intern opcional:

```go
type StringPool struct {
    values map[string]*Object
}
```

`ldstr` debe usar user string heap `#US` y crear `System.String`.

---

## 16. BCL mínima en Go

La BCL parcial es el verdadero corazón del proyecto.

### 16.1 Objetivo

Implementar suficientes tipos `System.*` para correr código C# realista de reglas y librerías puras.

### 16.2 Fase BCL v0.1

```txt
System.Object
System.ValueType
System.Enum
System.String
System.Array
System.Boolean
System.Char
System.Byte
System.SByte
System.Int16
System.UInt16
System.Int32
System.UInt32
System.Int64
System.UInt64
System.Single
System.Double
System.Void
System.Math
System.Console mínimo
System.Exception
```

### 16.3 Fase BCL v0.2

```txt
System.DateTime
System.TimeSpan
System.Guid
System.Decimal básico
System.Convert
System.Text.Encoding
System.Text.StringBuilder
System.Collections.Generic.List<T>
System.Collections.Generic.Dictionary<K,V>
System.Collections.Generic.IEnumerable<T>
System.Collections.Generic.IEnumerator<T>
System.Nullable<T>
```

### 16.4 Fase BCL v0.3

```txt
System.Linq.Enumerable subset
System.IO.Stream
System.IO.MemoryStream
System.IO.TextReader
System.IO.TextWriter
System.Globalization.CultureInfo básico
System.Threading.CancellationToken stub
System.Threading.Tasks.Task subset o unsupported limpio
```

### 16.5 Implementación de métodos nativos

Algunos métodos BCL no vienen de IL, sino como implementación nativa Go:

```go
type NativeMethod func(ctx *Context, args []Value) (Value, error)
```

Ejemplo:

```go
bcl.Register("System.Math", "Abs", nativeMathAbs)
bcl.Register("System.String", "Concat", nativeStringConcat)
bcl.Register("System.Console", "WriteLine", nativeConsoleWriteLine)
```

### 16.6 Unsupported explícito

Cuando una llamada no está implementada:

```txt
Unsupported BCL method:
System.Reflection.MethodInfo.Invoke
Required by:
FluentValidation.Internal.ReflectionHelper
```

No devolver errores genéricos.

---

## 17. Generics

### 17.1 Scope inicial

Soportar generics por interpretación, no por especialización JIT compleja.

Casos iniciales:

```txt
List<int>
List<string>
List<object>
Dictionary<string,string>
Dictionary<string,object>
Nullable<T>
```

### 17.2 Representación

```go
type GenericInstance struct {
    GenericType *Type
    Args        []*Type
}
```

### 17.3 MethodSpec

Resolver `MethodSpec` y `TypeSpec`.

### 17.4 Limitaciones iniciales

No soportar al inicio:

```txt
- generic variance avanzada
- constraints complejos
- generic virtual methods complejos
- reflection sobre generics completa
```

---

## 18. Exceptions

### 18.1 Soporte v0.1

```txt
throw
try/catch
leave
finally básico
```

### 18.2 Estructura

```go
type ManagedException struct {
    Object     *Object
    Message    string
    StackTrace []ManagedFrame
}
```

### 18.3 Stack trace

Formato:

```txt
System.InvalidOperationException: invalid rule
   at Rules.Engine.Eval(input)
   at Host.Invoke()
```

### 18.4 Errores Go vs managed

Separar:

```go
type VMError struct {
    Code    string
    Message string
    Details string
}

type ManagedExceptionError struct {
    Exception *ManagedException
}
```

---

## 19. Reflection

### 19.1 v0.1

No soportar reflection completa.

Soportar stubs mínimos:

```txt
typeof(T)
object.GetType()
Type.FullName
Type.Name
Type.Namespace
```

### 19.2 v0.2

Agregar:

```txt
Type.GetProperty
Type.GetField
Type.GetMethod
Attribute lectura básica
CustomAttributeData
```

### 19.3 No soportar inicialmente

```txt
MethodInfo.Invoke
Activator.CreateInstance genérico avanzado
Reflection.Emit
DynamicMethod
Expression.Compile
```

Este límite impacta muchos paquetes NuGet; el checker debe detectarlo.

---

## 20. Delegates y lambdas

### 20.1 Soporte inicial

```txt
- delegate simple
- multicast no requerido al inicio
- lambda compilada a método estático o instance
```

### 20.2 Representación

```go
type DelegateObject struct {
    Target *Object
    Method *Method
}
```

### 20.3 Casos

Soportar:

```csharp
Func<int, int> f = x => x + 1;
return f(10);
```

Más adelante:

```txt
- events
- multicast delegates
- closure classes
```

---

## 21. Async / Task

### 21.1 v1

No soportar async completo.

El checker debe reportar:

```txt
Unsupported:
System.Threading.Tasks.Task
async state machine detected
```

### 21.2 Futuro

Se puede implementar un `Task` cooperativo:

```txt
- Task.FromResult
- Task.CompletedTask
- await sobre Task completado
```

Pero no debe bloquear el MVP.

---

## 22. NuGet support

### 22.1 Objetivo largo

Permitir:

```go
vm.NuGet().Add("NodaTime", "3.2.0")
vm.NuGet().Restore()
```

NuGet usa TFMs para aislar componentes compatibles dentro de un paquete; el resolver debe entenderlos.

### 22.2 Target inicial recomendado

```txt
netstandard2.0
```

Motivo:

```txt
Muchos paquetes de lógica pura publican assets netstandard2.0.
El objetivo es soportar un subconjunto netstandard-lite, no net8 completo.
```

Microsoft documenta que .NET Standard sirve para compartir APIs entre implementaciones .NET y que `netstandard2.0` ofrece una superficie amplia usada por muchas librerías.

### 22.3 Resolver de paquetes

Implementar:

```txt
- leer .nupkg como zip
- leer .nuspec
- parsear package metadata
- parsear dependency groups
- seleccionar mejor TFM
- resolver dependencias transitivas
- cache local
- lockfile
```

El `.nuspec` contiene metadata y dependencias del paquete, por eso el resolver debe parsearlo correctamente.

### 22.4 Layout NuGet a soportar

```txt
lib/netstandard2.0/Foo.dll
lib/net8.0/Foo.dll
ref/netstandard2.0/Foo.dll
runtimes/win-x64/native/foo.dll
runtimes/linux-x64/native/foo.so
```

### 22.5 Reglas de selección

Prioridad inicial:

```txt
1. lib/netstandard2.0
2. lib/netstandard2.1
3. lib/net8.0 solo si perfil lo permite
4. ref/* solo para análisis, no ejecución
5. runtimes/*/native => unsupported en pure-go
```

### 22.6 Lockfile propio

Crear:

```json
{
  "version": 1,
  "target": "netstandard2.0",
  "packages": [
    {
      "id": "NodaTime",
      "version": "3.2.0",
      "selectedAsset": "lib/netstandard2.0/NodaTime.dll",
      "dependencies": []
    }
  ]
}
```

### 22.7 Comandos CLI

```bash
vmnet add NodaTime@3.2.0
vmnet restore
vmnet check
vmnet run Rules.dll Rules.Engine.Eval '{"amount":100}'
```

---

## 23. Compatibility checker

El checker es obligatorio. Sin checker, el usuario final sufrirá.

### 23.1 Comando

```bash
vmnet check Rules.dll
vmnet check package NodaTime@3.2.0
```

### 23.2 Salida OK

```txt
Rules.dll
Status: compatible
Profile: rules
Methods analyzed: 42
Unsupported opcodes: none
Unsupported BCL calls: none
```

### 23.3 Salida parcial

```txt
FluentValidation 12.0.0
Status: partial
Selected target: netstandard2.0

Unsupported:
- System.Reflection.MethodInfo.Invoke
- System.Linq.Expressions.Expression.Compile
- System.Threading.Tasks.Task

Suggested:
- Use vmnet reflection-lite profile
- Avoid dynamic validators
```

### 23.4 Salida unsupported

```txt
Microsoft.EntityFrameworkCore
Status: unsupported

Reasons:
- heavy reflection
- async/Task usage
- expression trees
- database provider abstractions
- unsupported System.Data APIs
```

### 23.5 Analyzer interno

Debe analizar:

```txt
- opcodes usados
- method calls
- referenced assemblies
- generic usage
- exception handlers
- custom attributes
- P/Invoke
- unsafe pointers
- reflection usage
- async state machines
- native assets en NuGet
```

---

## 24. Perfiles de compatibilidad

### 24.1 `minimal`

Para pruebas básicas.

Soporta:

```txt
- métodos static
- int/bool/string
- operaciones aritméticas
- branches
- return
```

### 24.2 `rules`

Para reglas de negocio.

Soporta:

```txt
- clases simples
- objetos
- strings
- arrays
- List<T>
- Dictionary<string, object>
- exceptions
- DateTime
- Guid
- JSON helpers
```

### 24.3 `netstandard-lite`

Para NuGet puro.

Soporta:

```txt
- BCL ampliada
- collections
- LINQ subset
- Text.Encoding
- MemoryStream
- CultureInfo básico
- reflection-lite
```

---

## 25. JSON bridge

### 25.1 Motivo

El usuario final quiere simplicidad. La forma más simple es:

```go
CallJSON(typeName, methodName, input)
```

### 25.2 C# esperado

```csharp
namespace Rules;

public static class Engine
{
    public static object Eval(object input)
    {
        return new {
            ok = true,
            tax = 21
        };
    }
}
```

### 25.3 Alternativa recomendada para v0.1

Usar `byte[]`:

```csharp
public static byte[] Invoke(byte[] input)
{
    return input;
}
```

Y Go hace JSON antes/después.

### 25.4 Helpers

```go
func (asm *Assembly) CallJSON(typeName, methodName string, input any) (any, error) {
    data := json.Marshal(input)
    out := asm.CallBytes(typeName, methodName, data)
    return json.Unmarshal(out)
}
```

---

## 26. Seguridad y sandbox

### 26.1 Límites

```go
type Limits struct {
    MaxInstructions int64
    MaxHeapBytes    int64
    MaxCallDepth    int
    MaxArrayLength  int
    MaxStringBytes  int
}
```

### 26.2 Bloquear APIs peligrosas

En pure-Go mode:

```txt
- File IO real deshabilitado por default
- Network IO deshabilitado por default
- Environment access deshabilitado por default
- Reflection limitada
- P/Invoke deshabilitado
- Threading deshabilitado
```

### 26.3 Permisos explícitos

```go
vm := vmnet.New(vmnet.Options{
    Permissions: vmnet.Permissions{
        AllowConsole: true,
        AllowFileRead: false,
        AllowNetwork: false,
    },
})
```

---

## 27. CLI

Crear binario:

```txt
vmnet
```

Comandos:

```bash
vmnet inspect Rules.dll
vmnet il Rules.dll Rules.Engine.Eval
vmnet check Rules.dll
vmnet run Rules.dll Rules.Engine.Eval '{"x":1}'
vmnet add NodaTime@3.2.0
vmnet restore
vmnet packages
```

### 27.1 `inspect`

```bash
vmnet inspect Rules.dll
```

Salida:

```txt
Assembly: Rules
Version: 1.0.0.0
Types:
- Rules.Engine
- Rules.Customer
Methods:
- Rules.Engine.Eval(byte[]) byte[]
```

### 27.2 `il`

```bash
vmnet il Rules.dll Rules.Engine.Add
```

Salida:

```txt
IL_0000: ldarg.0
IL_0001: ldarg.1
IL_0002: add
IL_0003: ret
```

### 27.3 `run`

```bash
vmnet run Rules.dll Rules.Engine.Eval '{"amount":100}'
```

---

## 28. Test suite

### 28.1 Tests de PE

```txt
- valid PE
- invalid PE
- missing CLI header
- invalid RVA
- multiple sections
```

### 28.2 Tests de metadata

```txt
- TypeDef parsing
- MethodDef parsing
- MemberRef parsing
- TypeRef parsing
- signatures
- blobs
- user strings
- generic signatures
```

### 28.3 Tests de IL

```txt
- arithmetic
- branches
- loops
- switch
- method calls
- static fields
- instance fields
- arrays
- strings
- exceptions
```

### 28.4 Tests de runtime

```txt
- object allocation
- method dispatch
- virtual call
- interface call
- boxing/unboxing
- generics basic
- List<T>
- Dictionary<K,V>
```

### 28.5 Tests de BCL

```txt
- System.String.Concat
- System.String.Length
- System.Math.Abs
- System.DateTime
- System.Guid
- List<T>.Add
- Dictionary<K,V>.TryGetValue
```

### 28.6 Tests de checker

```txt
- unsupported opcode
- unsupported BCL call
- P/Invoke detection
- async detection
- reflection detection
- native NuGet asset detection
```

### 28.7 Tests de NuGet

```txt
- parse .nupkg
- parse .nuspec
- choose TFM
- dependency group
- transitive dependency
- lockfile generation
```

---

## 29. Fixtures C# para tests

Crear proyecto `/tests/fixtures/csharp`.

### 29.1 `SimpleMath`

```csharp
public static class SimpleMath
{
    public static int Add(int a, int b) => a + b;
}
```

### 29.2 `Strings`

```csharp
public static class Strings
{
    public static string Hello(string name) => "Hello " + name;
}
```

### 29.3 `Loops`

```csharp
public static class Loops
{
    public static int Sum(int n)
    {
        var total = 0;
        for (var i = 0; i <= n; i++)
            total += i;
        return total;
    }
}
```

### 29.4 `Objects`

```csharp
public class Customer
{
    public string Name { get; set; }
    public int Age { get; set; }
}
```

### 29.5 `Collections`

```csharp
public static class CollectionsTest
{
    public static int Count()
    {
        var xs = new List<int>();
        xs.Add(1);
        xs.Add(2);
        return xs.Count;
    }
}
```

### 29.6 `Exceptions`

```csharp
public static class ExceptionTest
{
    public static void Fail()
    {
        throw new InvalidOperationException("boom");
    }
}
```

> Estas seis fixtures ya están implementadas en `tests/fixtures/csharp/` y
> compilan contra `netstandard2.0` (ver `tests/fixtures/csharp/README.md`).

---

## 30. Error model

### 30.1 Go errors

```go
type Error struct {
    Code    string
    Message string
    Details string
    Cause   error
}
```

### 30.2 Codes

```txt
VMNET_INVALID_PE
VMNET_MISSING_CLI_HEADER
VMNET_INVALID_METADATA
VMNET_UNSUPPORTED_OPCODE
VMNET_UNSUPPORTED_BCL_METHOD
VMNET_TYPE_NOT_FOUND
VMNET_METHOD_NOT_FOUND
VMNET_FIELD_NOT_FOUND
VMNET_STACK_OVERFLOW
VMNET_CALL_DEPTH_EXCEEDED
VMNET_MANAGED_EXCEPTION
VMNET_NUGET_RESOLVE_FAILED
VMNET_UNSUPPORTED_PACKAGE
VMNET_PERMISSION_DENIED
```

### 30.3 Error example

```txt
VMNET_UNSUPPORTED_BCL_METHOD

Method:
System.Reflection.MethodInfo.Invoke

Required by:
Rules.DynamicInvoker.Run

Suggestion:
Avoid reflection invoke or enable coreclr fallback.
```

---

## 31. Roadmap

> El roadmap original (v0.1 → v1.5, incremental por versión) fue reorganizado
> en 4 fases con demo de cierre para conseguir aprobación por etapas — ver
> `docs/es/ROADMAP.md`. El mapeo es: v0.1 → Fase 1; v0.2+v0.3 → Fase 2;
> v0.4+v0.5+v0.6 → Fase 3; v1.0 → Fase 4; v1.5 queda fuera de las 4 fases
> como roadmap posterior. El contenido de cada versión se conserva abajo
> como referencia de alcance.

### 31.1 v0.1 — IL mínimo

Objetivo:

```txt
Correr DLLs C# simples con métodos static.
```

Incluye:

```txt
- PE loader
- metadata parser básico
- IL decoder
- interpreter stack
- int/bool/string básico
- branches
- call
- ret
- CLI inspect
- vmnet run
```

No incluye:

```txt
- NuGet
- generics
- exceptions completas
- reflection
```

### 31.2 v0.2 — Objetos y strings

```txt
- class
- fields
- newobj
- callvirt
- System.String
- arrays
- simple heap
```

### 31.3 v0.3 — BCL rules profile

```txt
- List<T>
- Dictionary<string, object>
- DateTime
- Guid
- exceptions
- JSON helpers
```

### 31.4 v0.4 — Compatibility checker

```txt
- vmnet check
- unsupported opcode report
- unsupported BCL report
- profile validation
```

### 31.5 v0.5 — NuGet local

```txt
- .nupkg parser
- .nuspec parser
- TFM selection
- local package cache
```

### 31.6 v0.6 — NuGet restore

```txt
- dependency resolver
- transitive dependencies
- lockfile
- package compatibility report
```

### 31.7 v1.0 — Public plugin runtime

```txt
- stable Go API
- stable CLI
- documented supported profile
- examples
- test suite
- benchmark suite
- selected NuGet package compatibility list
```

### 31.8 v1.5 — Hybrid backend

```txt
- pure-go backend
- coreclr fallback backend
- worker process backend
- same API
```

---

## 32. Benchmarks

### 32.1 Comparar

```txt
- vmnet interpreter
- native Go equivalent
- CoreCLR native execution
- goja equivalent where possible
```

### 32.2 Tests

```txt
- arithmetic loop
- string concat
- JSON in/out
- object allocation
- List<T>.Add
- Dictionary lookup
- rule engine call 10k times
```

### 32.3 Métricas

```txt
- cold load time
- method invoke overhead
- instructions/sec
- allocations/op
- heap logical bytes
- package restore time
```

---

## 33. Documentación requerida

### 33.1 README

Debe explicar:

```txt
- qué es vmnet
- qué no es
- ejemplo Go mínimo
- ejemplo C# mínimo
- perfiles soportados
- cómo compilar DLL compatible
- cómo correr vmnet check
- límites conocidos
```

### 33.2 Docs

```txt
/docs
  architecture.md
  supported-il.md
  supported-bcl.md
  nuget-support.md
  compatibility-profile.md
  security.md
  roadmap.md
```

### 33.3 Mensaje explícito

Incluir:

```txt
vmnet is not a full .NET implementation.
vmnet executes a supported subset of CIL and selected BCL APIs.
Use vmnet check before loading third-party assemblies.
```

---

## 34. Ejemplo completo esperado

### 34.1 C#

```csharp
namespace Rules;

public static class Engine
{
    public static byte[] Eval(byte[] input)
    {
        var text = System.Text.Encoding.UTF8.GetString(input);
        var output = "{\"ok\":true,\"source\":\"csharp\"}";
        return System.Text.Encoding.UTF8.GetBytes(output);
    }
}
```

### 34.2 Go

```go
package main

import (
    "fmt"

    "github.com/acme/vmnet"
)

func main() {
    vm := vmnet.New()

    asm, err := vm.LoadFile("./Rules.dll")
    if err != nil {
        panic(err)
    }

    out, err := asm.CallBytes("Rules.Engine", "Eval", []byte(`{"amount":100}`))
    if err != nil {
        panic(err)
    }

    fmt.Println(string(out))
}
```

Resultado:

```txt
{"ok":true,"source":"csharp"}
```

---

## 35. Criterios de aceptación del MVP

El MVP se considera completo cuando:

```txt
1. Puede cargar un assembly C# simple.
2. Puede listar tipos y métodos.
3. Puede ejecutar método static int Add(int,int).
4. Puede ejecutar método static string Hello(string).
5. Puede ejecutar loops y branches.
6. Puede crear objetos simples.
7. Puede leer y escribir fields.
8. Puede ejecutar call y callvirt básicos.
9. Puede reportar unsupported opcode claramente.
10. Tiene CLI vmnet inspect/check/run.
11. Tiene tests automatizados.
12. Corre en Linux, macOS y Windows sin cgo.
```

---

## 36. Criterios de aceptación para NuGet v1

NuGet support se considera útil cuando:

```txt
1. Puede leer .nupkg local.
2. Puede parsear .nuspec.
3. Puede seleccionar lib/netstandard2.0.
4. Puede resolver dependencias transitivas simples.
5. Puede generar lockfile.
6. Puede correr vmnet check sobre paquete.
7. Puede explicar por qué un paquete no es compatible.
8. Puede ejecutar al menos 3 paquetes NuGet puros certificados.
```

---

## 37. Riesgos técnicos

### 37.1 Riesgo mayor: BCL

Implementar IL no es lo más difícil. Lo más difícil es `System.*`.

Mitigación:

```txt
- empezar con BCL mínima
- implementar métodos por demanda
- tener checker fuerte
- certificar paquetes concretos
```

### 37.2 Riesgo: NuGet arbitrario

NuGet tiene demasiada variedad.

Mitigación:

```txt
- solo netstandard2.0 al principio
- bloquear native assets
- bloquear P/Invoke
- bloquear reflection-heavy
- catálogo de paquetes compatibles
```

### 37.3 Riesgo: expectativas

Usuarios pueden esperar .NET completo.

Mitigación:

```txt
- naming claro
- docs claras
- checker obligatorio
- profiles explícitos
```

### 37.4 Riesgo: performance

Un intérprete será más lento que CoreCLR.

Mitigación:

```txt
- IR propia
- inline BCL native methods
- method cache
- resolved tokens cache
- futuro IL → Go codegen
```

---

## 38. Futuro: IL → Go codegen

Una evolución potente:

```txt
C# DLL
  ↓
IL metadata
  ↓
IR
  ↓
Go codegen
  ↓
Go package
```

Producto futuro:

```bash
vmnet transpile Rules.dll -o ./rulesgo
```

Esto permitiría migración:

```txt
C# business rules → Go source code
```

No debe estar en el MVP, pero la IR debe diseñarse pensando en esta posibilidad.

---

## 39. Futuro: backend híbrido

Más adelante:

```go
vm := vmnet.New(vmnet.Options{
    Backend: vmnet.BackendAuto,
})
```

Comportamiento:

```txt
1. Intentar pure-go.
2. Si checker falla y CoreCLR está disponible, usar backend coreclr.
3. Si necesita aislamiento, usar worker process.
```

Backends:

```txt
pure-go:
  portable, sandbox, sin .NET instalado.

coreclr:
  más compatible, requiere runtime .NET.

worker:
  más aislado, usa proceso externo .NET/gRPC/stdin.
```

---

## 40. Conclusión técnica

El producto debe construirse con esta filosofía:

```txt
Primero: ejecutar C# simple.
Después: ejecutar reglas reales.
Después: ejecutar librerías NuGet puras seleccionadas.
Después: resolver más compatibilidad.
Nunca prometer .NET completo antes de tenerlo.
```

La versión viable del producto no es:

```txt
.NET completo reimplementado en Go.
```

La versión viable es:

```txt
Un intérprete IL puro Go, embebible, con BCL parcial, checker de compatibilidad y soporte progresivo de NuGet.
```

Ese enfoque tiene valor real para:

```txt
- migraciones enterprise
- plugins C#
- reglas de negocio
- integración Go/.NET
- reutilización parcial de NuGet
```

Nombre recomendado:

```txt
vmnet
```

Subtítulo recomendado:

```txt
A pure-Go IL interpreter for embeddable C# plugins and selected NuGet packages.
```
