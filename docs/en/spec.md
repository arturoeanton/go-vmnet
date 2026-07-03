<!--
  Original technical specification for vmnet/gocil, as provided to
  bootstrap the project (2026-07-02). It is the canonical reference that
  docs/en/ROADMAP.md cites as "spec §N". Decisions that diverge from this
  document (e.g. package layout with /internal) are documented separately
  in docs/en/adr/.
-->

# Technical specification — `vmnet` / `gocil`

## 1. Summary

Build a Go library provisionally named **`vmnet`** or **`gocil`** that allows running `.NET` / `C#` assemblies inside a Go application using an **IL/CIL interpreter written in pure Go**.

The product goal is for the end user to be able to use C# DLLs or compatible NuGet packages with an experience similar to `goja`:

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

The idea is **not** to host CoreCLR via `hostfxr` as the first option. Microsoft does document native .NET hosting through `nethost` and `hostfxr`, but that approach requires an installed runtime, `runtimeconfig.json`, runtime resolution, and external complexity. That mode can exist as a future fallback, not as the core of the product.

The core must be:

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

The base CLI/CIL specification is defined by ECMA-335, which covers the Common Language Infrastructure, metadata, execution model, and instruction set. This specification should be used as the primary reference for metadata and IL.

---

## 2. Product goal

### 2.1 Tagline

```txt
Run C# plugins and selected NuGet packages inside Go, without requiring .NET at runtime.
```

In Spanish:

```txt
Ejecutá plugins C# y paquetes NuGet seleccionados dentro de Go, sin requerir .NET instalado.
```

### 2.2 Positioning

`vmnet` should not be marketed as "full .NET in Go". The correct positioning is:

```txt
Un runtime IL embebible, escrito en Go puro, para reglas, plugins y lógica de negocio C# compatible.
```

### 2.3 Use cases

1. **C# plugins for Go applications**
   A product written in Go allows enterprise customers to write extensions in C#.

2. **Incremental .NET → Go migration**
   The new system is in Go, but some legacy rules or modules remain in C#.

3. **Business rule execution**
   C# is used as a rules language for pricing, validations, tax calculation, simple workflows.

4. **Reuse of compatible NuGet libraries**
   Run pure NuGet packages that don't depend on P/Invoke, complex threading, ASP.NET, EF Core, WPF, etc.

5. **Controlled sandbox**
   Run external logic in an environment more restricted than full CoreCLR.

6. **Educational / IL debugging tool**
   Inspect metadata, IL instructions, stack frames, calls, and objects.

---

## 3. Non-goals

These points must be stated explicitly to avoid scope creep.

### 3.1 Do not implement full .NET in v1

Do not initially support:

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

### 3.2 Do not use CoreCLR in the core

The primary mode must be pure Go:

```txt
Sin cgo.
Sin hostfxr.
Sin runtime .NET instalado.
Sin dependencia obligatoria de dotnet.
```

CoreCLR can exist as a future optional backend:

```go
vm := vmnet.New(vmnet.Options{
    Backend: vmnet.Auto,
})
```

But it must not contaminate the design of the pure interpreter.

### 3.3 Do not promise full binary compatibility

Correct message:

```txt
Compatible con assemblies C# compilados para el perfil vmnet.
```

Incorrect message:

```txt
Corre cualquier DLL .NET.
```

---

## 4. Mandatory technical references

The developer must use as a reference:

1. **ECMA-335**
   Defines the Common Language Infrastructure, metadata, CIL, and execution model.

2. **Target Framework Monikers / TFMs**
   NuGet and SDK-style projects use TFMs to identify target frameworks, for example `netstandard2.0`, `net8.0`, `net10.0`, etc.

3. **NuGet `.nuspec`**
   The `.nuspec` is the package's XML manifest and contains metadata, dependencies, and framework groups.

4. **.NET Standard**
   For v1, the recommended target is a subset of `netstandard2.0`, because it exposes a common API surface that is widely used by NuGet libraries.

5. **CoreCLR hosting**
   Only as a reference for a future `coreclr` backend, using `nethost` and `hostfxr`.

---

## 5. Project name

Options:

```txt
vmnet
gocil
goil
cilgo
dotgo
```

Recommendation:

```txt
Nombre de librería: vmnet
Nombre técnico interno: gocil
```

Rationale:

```txt
vmnet = nombre de producto.
gocil = describe el motor: CIL runtime in Go.
```

---

## 6. Expected public API

### 6.1 Minimal v0.1 API

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

### 6.2 Simple usage API

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

### 6.3 Low-level API

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

### 6.4 Future NuGet API

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

### 6.5 Future automatic-backend API

```go
vm := vmnet.New(vmnet.Options{
    Backend: vmnet.BackendAuto,
})
```

### 6.6 Instance API (Fase 3.28)

`Call`/`CallBytes`/`CallJSON` (§6.1) only invoke **static** methods.
A real object-oriented API (`new Engine()`, `engine.Evaluate(...)`,
chaining on the result) needs to construct instances and call
instance methods directly from Go — without an intermediate glue
assembly in C# (see `examples/jint-nowrapper` for the real case).

```go
type Instance struct{}

func (in *Instance) Native() any
func (in *Instance) TypeName() string
func (in *Instance) Call(methodName string, args ...Value) (Value, error)

func (asm *Assembly) New(typeName string, args ...Value) (*Instance, error)
```

`New` constructs an instance via its real `.ctor`, resolved by
arity/Kind of `args` (the same mechanism `Call` uses for static
overloads). `Instance.Call` invokes an instance method by name,
dispatched as a real virtual call: first the receiver's concrete
type, walking the inheritance chain if needed (see
`internal/interpreter/calls.go`, `Machine.call`). Any `Call`/
`Instance.Call` whose result is an object or a value type now
returns an `*Instance` (previously: silent `nil`) so it can keep
being chained.

**Explicit limitation:** this API reflects CIL's real object model
(`newobj`/`callvirt`/fields), not C#'s SYNTACTIC SUGAR that the
compiler resolves at compile time — optional parameters with a
default value, extension methods, user-defined implicit
conversions. Real C# code that relies on those conveniences needs
the explicit argument (optional parameters) or must keep using a
compiled wrapper (extensions/implicit conversions)
— see `examples/jint-nowrapper/README.md` for the concrete case
found with Jint (`Evaluate(string, string = null)`,
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

In v1 only `BackendPureGo` is mandatory.

---

## 7. General architecture

Recommended folder structure:

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

> **Implementation note (see `docs/en/adr/0002-package-layout.md`):** the repo
> implements this same separation of responsibilities, but places `/vmnet`
> at the module root (public package `vmnet`, `import "github.com/arturoeanton/go-vmnet"`)
> and moves `/pe /metadata /il /ir /interpreter /runtime /bcl /nuget /checker`
> under `/internal`, so as not to commit to a low-level public API while
> the internal design is still changing rapidly during Fases 1-3.

---

## 8. Internal pipeline

Loading and execution:

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

### 9.1 Responsibilities

The `/pe` module must:

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

### 9.2 Internal API

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

### 9.3 Validations

Must reject:

```txt
- PE corrupto.
- Assembly sin CLI header.
- Metadata inexistente.
- RVA inválido.
- Method body fuera de rango.
```

Errors:

```go
ErrInvalidPE
ErrMissingCLIHeader
ErrInvalidRVA
ErrInvalidMetadataRoot
```

---

## 10. Metadata loader

### 10.1 Mandatory streams

Support:

```txt
#~
#Strings
#US
#Blob
#GUID
```

### 10.2 Initial tables

Implement at minimum:

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

Not all of them are used in v0.1, but they must be parseable so as not to fail on real assemblies.

### 10.3 Tokens

Represent tokens:

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

Must parse signatures for:

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

Initial types:

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

### 11.1 Responsibility

The `/il` module must decode IL bytes into structured instructions.

```go
type Instruction struct {
    Offset  int
    OpCode  OpCode
    Operand any
}
```

### 11.2 OpCodes v0.1

Minimum support:

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

Add:

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

Many can initially be marked as unsupported, but the decoder must be able to recognize them.

---

## 12. Intermediate IR

Do not interpret raw IL directly as the definitive architecture.

Convert IL to an internal IR:

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

Advantages:

```txt
- simplifica el interpreter
- permite validación previa
- facilita optimizaciones futuras
- permite codegen futuro IL → Go
- permite reportes claros de unsupported features
```

---

## 13. Interpreter

### 13.1 Execution model

Use a stack-based interpreter:

```go
type Frame struct {
    Method *runtime.Method
    Args   []Value
    Locals []Value
    Stack  []Value
    IP     int
}
```

### 13.2 Main loop

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

Errors:

```go
ErrStackOverflow
ErrCallDepthExceeded
ErrInstructionLimitExceeded
ErrOutOfMemory
```

### 13.4 Determinism

There must be an option for bounded execution:

```go
vm := vmnet.New(vmnet.Options{
    Limits: vmnet.Limits{
        MaxInstructions: 1_000_000,
        MaxHeapBytes: 64 << 20,
    },
})
```

This is key for plugins and sandboxing.

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

Support:

```txt
- call directo
- callvirt con null check
- override por vtable
- interface dispatch básico
```

Do not optimize initially.

---

## 15. Heap and memory

### 15.1 Initial option

Go's GC can manage runtime objects:

```go
type Heap struct {
    objects []*Object
    bytes   int64
}
```

Even though Go's GC handles real memory, `vmnet` must track logical memory for limits.

### 15.2 Internal API

```go
func (h *Heap) NewObject(t *Type) (*Object, error)
func (h *Heap) NewArray(elem *Type, length int) (*Object, error)
func (h *Heap) NewString(s string) (*Object, error)
```

### 15.3 String interning

Implement optional string interning:

```go
type StringPool struct {
    values map[string]*Object
}
```

`ldstr` must use the `#US` user string heap and create a `System.String`.

---

## 16. Minimal BCL in Go

The partial BCL is the true heart of the project.

### 16.1 Goal

Implement enough `System.*` types to run realistic C# rules code and pure libraries.

### 16.2 BCL Phase v0.1

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

### 16.3 BCL Phase v0.2

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

### 16.4 BCL Phase v0.3

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

### 16.5 Native method implementation

Some BCL methods don't come from IL, but as a native Go implementation:

```go
type NativeMethod func(ctx *Context, args []Value) (Value, error)
```

Example:

```go
bcl.Register("System.Math", "Abs", nativeMathAbs)
bcl.Register("System.String", "Concat", nativeStringConcat)
bcl.Register("System.Console", "WriteLine", nativeConsoleWriteLine)
```

### 16.6 Explicit unsupported

When a call is not implemented:

```txt
Unsupported BCL method:
System.Reflection.MethodInfo.Invoke
Required by:
FluentValidation.Internal.ReflectionHelper
```

Do not return generic errors.

---

## 17. Generics

### 17.1 Initial scope

Support generics through interpretation, not through complex JIT specialization.

Initial cases:

```txt
List<int>
List<string>
List<object>
Dictionary<string,string>
Dictionary<string,object>
Nullable<T>
```

### 17.2 Representation

```go
type GenericInstance struct {
    GenericType *Type
    Args        []*Type
}
```

### 17.3 MethodSpec

Resolve `MethodSpec` and `TypeSpec`.

### 17.4 Initial limitations

Do not initially support:

```txt
- generic variance avanzada
- constraints complejos
- generic virtual methods complejos
- reflection sobre generics completa
```

---

## 18. Exceptions

### 18.1 v0.1 support

```txt
throw
try/catch
leave
finally básico
```

### 18.2 Structure

```go
type ManagedException struct {
    Object     *Object
    Message    string
    StackTrace []ManagedFrame
}
```

### 18.3 Stack trace

Format:

```txt
System.InvalidOperationException: invalid rule
   at Rules.Engine.Eval(input)
   at Host.Invoke()
```

### 18.4 Go errors vs managed errors

Separate:

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

Do not support full reflection.

Support minimal stubs:

```txt
typeof(T)
object.GetType()
Type.FullName
Type.Name
Type.Namespace
```

### 19.2 v0.2

Add:

```txt
Type.GetProperty
Type.GetField
Type.GetMethod
Attribute lectura básica
CustomAttributeData
```

### 19.3 Do not initially support

```txt
MethodInfo.Invoke
Activator.CreateInstance genérico avanzado
Reflection.Emit
DynamicMethod
Expression.Compile
```

This limitation impacts many NuGet packages; the checker must detect it.

---

## 20. Delegates and lambdas

### 20.1 Initial support

```txt
- delegate simple
- multicast no requerido al inicio
- lambda compilada a método estático o instance
```

### 20.2 Representation

```go
type DelegateObject struct {
    Target *Object
    Method *Method
}
```

### 20.3 Cases

Support:

```csharp
Func<int, int> f = x => x + 1;
return f(10);
```

Later:

```txt
- events
- multicast delegates
- closure classes
```

---

## 21. Async / Task

### 21.1 v1

Do not support full async.

The checker must report:

```txt
Unsupported:
System.Threading.Tasks.Task
async state machine detected
```

### 21.2 Future

A cooperative `Task` can be implemented:

```txt
- Task.FromResult
- Task.CompletedTask
- await sobre Task completado
```

But it must not block the MVP.

---

## 22. NuGet support

### 22.1 Long-term goal

Enable:

```go
vm.NuGet().Add("NodaTime", "3.2.0")
vm.NuGet().Restore()
```

NuGet uses TFMs to isolate compatible components within a package; the resolver must understand them.

### 22.2 Recommended initial target

```txt
netstandard2.0
```

Rationale:

```txt
Muchos paquetes de lógica pura publican assets netstandard2.0.
El objetivo es soportar un subconjunto netstandard-lite, no net8 completo.
```

Microsoft documents that .NET Standard exists to share APIs across .NET implementations and that `netstandard2.0` offers a wide surface used by many libraries.

### 22.3 Package resolver

Implement:

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

The `.nuspec` contains the package's metadata and dependencies, which is why the resolver must parse it correctly.

### 22.4 NuGet layout to support

```txt
lib/netstandard2.0/Foo.dll
lib/net8.0/Foo.dll
ref/netstandard2.0/Foo.dll
runtimes/win-x64/native/foo.dll
runtimes/linux-x64/native/foo.so
```

### 22.5 Selection rules

Initial priority:

```txt
1. lib/netstandard2.0
2. lib/netstandard2.1
3. lib/net8.0 solo si perfil lo permite
4. ref/* solo para análisis, no ejecución
5. runtimes/*/native => unsupported en pure-go
```

### 22.6 Own lockfile

Create:

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

### 22.7 CLI commands

```bash
vmnet add NodaTime@3.2.0
vmnet restore
vmnet check
vmnet run Rules.dll Rules.Engine.Eval '{"amount":100}'
```

---

## 23. Compatibility checker

The checker is mandatory. Without a checker, the end user will suffer.

### 23.1 Command

```bash
vmnet check Rules.dll
vmnet check package NodaTime@3.2.0
```

### 23.2 OK output

```txt
Rules.dll
Status: compatible
Profile: rules
Methods analyzed: 42
Unsupported opcodes: none
Unsupported BCL calls: none
```

### 23.3 Partial output

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

### 23.4 Unsupported output

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

### 23.5 Internal analyzer

Must analyze:

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

## 24. Compatibility profiles

### 24.1 `minimal`

For basic testing.

Supports:

```txt
- métodos static
- int/bool/string
- operaciones aritméticas
- branches
- return
```

### 24.2 `rules`

For business rules.

Supports:

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

For pure NuGet.

Supports:

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

### 25.1 Rationale

The end user wants simplicity. The simplest form is:

```go
CallJSON(typeName, methodName, input)
```

### 25.2 Expected C#

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

### 25.3 Recommended alternative for v0.1

Use `byte[]`:

```csharp
public static byte[] Invoke(byte[] input)
{
    return input;
}
```

And Go does the JSON handling before/after.

### 25.4 Helpers

```go
func (asm *Assembly) CallJSON(typeName, methodName string, input any) (any, error) {
    data := json.Marshal(input)
    out := asm.CallBytes(typeName, methodName, data)
    return json.Unmarshal(out)
}
```

---

## 26. Security and sandbox

### 26.1 Limits

```go
type Limits struct {
    MaxInstructions int64
    MaxHeapBytes    int64
    MaxCallDepth    int
    MaxArrayLength  int
    MaxStringBytes  int
}
```

### 26.2 Block dangerous APIs

In pure-Go mode:

```txt
- File IO real deshabilitado por default
- Network IO deshabilitado por default
- Environment access deshabilitado por default
- Reflection limitada
- P/Invoke deshabilitado
- Threading deshabilitado
```

### 26.3 Explicit permissions

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

Create binary:

```txt
vmnet
```

Commands:

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

Output:

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

Output:

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

### 28.1 PE tests

```txt
- valid PE
- invalid PE
- missing CLI header
- invalid RVA
- multiple sections
```

### 28.2 Metadata tests

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

### 28.3 IL tests

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

### 28.4 Runtime tests

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

### 28.5 BCL tests

```txt
- System.String.Concat
- System.String.Length
- System.Math.Abs
- System.DateTime
- System.Guid
- List<T>.Add
- Dictionary<K,V>.TryGetValue
```

### 28.6 Checker tests

```txt
- unsupported opcode
- unsupported BCL call
- P/Invoke detection
- async detection
- reflection detection
- native NuGet asset detection
```

### 28.7 NuGet tests

```txt
- parse .nupkg
- parse .nuspec
- choose TFM
- dependency group
- transitive dependency
- lockfile generation
```

---

## 29. C# fixtures for tests

Create project `/tests/fixtures/csharp`.

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

> These six fixtures are already implemented in `tests/fixtures/csharp/` and
> compile against `netstandard2.0` (see `tests/fixtures/csharp/README.md`).

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

> The original roadmap (v0.1 → v1.5, incremental by version) was reorganized
> into 4 phases with a closing demo to secure stage-by-stage approval — see
> `docs/en/ROADMAP.md`. The mapping is: v0.1 → Fase 1; v0.2+v0.3 → Fase 2;
> v0.4+v0.5+v0.6 → Fase 3; v1.0 → Fase 4; v1.5 falls outside the 4 phases
> as a later roadmap. Each version's content is preserved below
> as a reference for scope.

### 31.1 v0.1 — Minimal IL

Goal:

```txt
Correr DLLs C# simples con métodos static.
```

Includes:

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

Does not include:

```txt
- NuGet
- generics
- exceptions completas
- reflection
```

### 31.2 v0.2 — Objects and strings

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

### 31.5 v0.5 — Local NuGet

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

### 32.1 Compare

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

### 32.3 Metrics

```txt
- cold load time
- method invoke overhead
- instructions/sec
- allocations/op
- heap logical bytes
- package restore time
```

---

## 33. Required documentation

### 33.1 README

Must explain:

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

### 33.3 Explicit message

Include:

```txt
vmnet is not a full .NET implementation.
vmnet executes a supported subset of CIL and selected BCL APIs.
Use vmnet check before loading third-party assemblies.
```

---

## 34. Full expected example

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

Result:

```txt
{"ok":true,"source":"csharp"}
```

---

## 35. MVP acceptance criteria

The MVP is considered complete when:

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

## 36. Acceptance criteria for NuGet v1

NuGet support is considered useful when:

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

## 37. Technical risks

### 37.1 Major risk: BCL

Implementing IL is not the hardest part. The hardest part is `System.*`.

Mitigation:

```txt
- empezar con BCL mínima
- implementar métodos por demanda
- tener checker fuerte
- certificar paquetes concretos
```

### 37.2 Risk: arbitrary NuGet

NuGet has too much variety.

Mitigation:

```txt
- solo netstandard2.0 al principio
- bloquear native assets
- bloquear P/Invoke
- bloquear reflection-heavy
- catálogo de paquetes compatibles
```

### 37.3 Risk: expectations

Users may expect full .NET.

Mitigation:

```txt
- naming claro
- docs claras
- checker obligatorio
- profiles explícitos
```

### 37.4 Risk: performance

An interpreter will be slower than CoreCLR.

Mitigation:

```txt
- IR propia
- inline BCL native methods
- method cache
- resolved tokens cache
- futuro IL → Go codegen
```

---

## 38. Future: IL → Go codegen

A powerful evolution:

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

Future product:

```bash
vmnet transpile Rules.dll -o ./rulesgo
```

This would enable migration:

```txt
C# business rules → Go source code
```

This must not be in the MVP, but the IR must be designed with this possibility in mind.

---

## 39. Future: hybrid backend

Later:

```go
vm := vmnet.New(vmnet.Options{
    Backend: vmnet.BackendAuto,
})
```

Behavior:

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

## 40. Technical conclusion

The product must be built with this philosophy:

```txt
Primero: ejecutar C# simple.
Después: ejecutar reglas reales.
Después: ejecutar librerías NuGet puras seleccionadas.
Después: resolver más compatibilidad.
Nunca prometer .NET completo antes de tenerlo.
```

The viable version of the product is not:

```txt
.NET completo reimplementado en Go.
```

The viable version is:

```txt
Un intérprete IL puro Go, embebible, con BCL parcial, checker de compatibilidad y soporte progresivo de NuGet.
```

This approach has real value for:

```txt
- migraciones enterprise
- plugins C#
- reglas de negocio
- integración Go/.NET
- reutilización parcial de NuGet
```

Recommended name:

```txt
vmnet
```

Recommended subtitle:

```txt
A pure-Go IL interpreter for embeddable C# plugins and selected NuGet packages.
```
