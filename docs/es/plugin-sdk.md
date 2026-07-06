# El SDK de plugins: `dotnet new vmnet-plugin`

`Assembly.CallBytes`/`Assembly.CallJSON` (la API pública congelada de
`docs/es/api-stability.md`, spec §25.3-25.4) ya permiten que un programa Go
llame a cualquier método static `byte[] X(byte[])` de cualquier assembly
cargado. `templates/vmnet-plugin` es un template real de `dotnet new` que
scaffoldea un proyecto con exactamente esa forma — el punto no es una
capacidad de runtime nueva, es convertir "el lado Go ya sabe cómo llamar
esta forma" en "acá hay una forma de un solo comando para empezar a
escribir esa forma."

## Instalar y generar

```bash
dotnet new install ./templates/vmnet-plugin
dotnet new vmnet-plugin -n BillingRules
```

Esto produce:

```
BillingRules/
  BillingRules.csproj   # netstandard2.0, sin dependencias
  Entry.cs              # public static class Entry { public static byte[] Invoke(byte[] input) { ... } }
  README.md             # instrucciones de build + sitio de llamada Go, con nombre sustituido
```

`-n BillingRules` no es solo un nombre de directorio — la sustitución propia
de `sourceName` de `dotnet new` (`.template.config/template.json`) reescribe
el namespace, el nombre del archivo `.csproj`, `RootNamespace`/
`AssemblyName`, y cada referencia a `PluginName` dentro de
`Entry.cs`/`README.md` a `BillingRules`, de la misma forma que cualquier
template real de Microsoft (`dotnet new classlib -n Foo`).

## Compilarlo, y llamarlo desde Go

```bash
cd BillingRules
dotnet build -c Release
```

```go
vm := vmnet.New()
plugin, err := vm.LoadFile("BillingRules/bin/Release/netstandard2.0/BillingRules.dll")
if err != nil {
    log.Fatal(err)
}
out, err := plugin.CallBytes("BillingRules.Entry", "Invoke", []byte(`{"name":"Ada"}`))
```

o, dejando que vmnet maneje el marshaling de JSON en ambos lados:

```go
result, err := plugin.CallJSON("BillingRules.Entry", "Invoke", map[string]any{"name": "Ada"})
```

`vm.LoadFile` y `Assembly.CallBytes`/`CallJSON` no son nuevos — son la misma
API congelada que ya usa cada otro ejemplo en `examples/` (ver
`examples/rules` para el mismo patrón `CallJSON`/`CallBytes` contra el
propio assembly de fixtures compartido de este proyecto). Un plugin
compilado desde este template también obtiene el sandbox `Permissions`
deny-by-default habitual de vmnet (`docs/es/security.md`) — nada por ser
"un plugin" le da una capacidad que un assembly normal cargado no tendría
ya.

## Qué hace el cuerpo generado de `Entry.Invoke`, y por qué no tiene dependencias

El starter generado lee a mano un único campo string `"name"` del JSON de
entrada (`IndexOf`/`Substring`, sin librería de JSON) y devuelve un saludo:

```csharp
public static byte[] Invoke(byte[] input)
{
    string json = Encoding.UTF8.GetString(input);
    string name = ReadStringField(json, "name") ?? "world";
    string output = "{\"message\":\"Hello, " + EscapeJson(name) + "!\"}";
    return Encoding.UTF8.GetBytes(output);
}
```

Esto es deliberadamente mínimo — sin objetos anidados, sin escapes unicode —
para que un plugin recién generado compile y corra sin nada más que el SDK
de .NET mismo, sin necesitar un restore de NuGet para que el starter
funcione. Reemplazá el cuerpo con tu lógica real; si la forma real de
entrada/salida de tu plugin es más rica que un puñado de campos planos,
agregá `Newtonsoft.Json` o `System.Text.Json` a `BillingRules.csproj` —
ambos funcionan bajo vmnet hoy para código real y común de grafos de
objetos JSON (ver los write-ups por paquete propios de
`docs/es/COMPATIBILITY.md` para lo que está verificado exactamente y qué
gaps siguen abiertos, ej. el
[issue #3](https://github.com/arturoeanton/go-vmnet/issues/3) para
deserialización `dynamic`/`ExpandoObject`).

`examples/plugin-demo` incluye en el repo una segunda versión, ya completa,
del mismo scaffold — `BillingRules/Entry.cs` ahí reemplaza el saludo del
starter con una regla de negocio real chica (una línea de impuesto plano
del 8%), mostrando de punta a punta el camino previsto de generar y luego
personalizar.

## Un bug real que encontró este template: `String.IndexOf(string, StringComparison)`

Compilar el propio starter `Entry.cs` de este template y correrlo de verdad
sacó a la luz un bug genuino y general del intérprete, no algo específico de
plugins: los natives `String.IndexOf`/`LastIndexOf` de vmnet
(`internal/bcl/system_string.go`) solo reciben una lista plana de
argumentos — sin metadata de firma por llamada — así que un argumento `int`
final siempre se trataba como índice de inicio. `IndexOf(value,
StringComparison.Ordinal)` pasa el valor crudo propio de
`StringComparison.Ordinal` (`4`) como ese argumento final, que antes se
leía en silencio, mal, como "empezar a buscar en el índice de rune 4",
salteando un match real anterior o directamente lanzando un
`ArgumentOutOfRangeException` falso en un string receptor corto.

Arreglado en `convertCharArgsForNative` de
`internal/interpreter/calls.go` (que ya pasaba el propio `paramTypeNames`
resuelto del sitio de llamada para una ambigüedad análoga `char`-vs-`int`,
Fase 3.40): una nueva tabla `stringComparisonSensitiveNatives` descarta
completamente el argumento final una vez que `paramTypeNames` dice que
realmente es un `System.StringComparison`, no un `int` — vmnet no tiene
soporte de cultura en ningún lado (`CultureInfo`, `StartsWith`, `Equals`,
... ya son todos solo-ordinal), así que no hay nada a lo que convertirlo.
Ver `TestCheapWins/IndexOf_with_StringComparison` (`vmnet_test.go`) para el
test de regresión.

### Cómo verificar

```bash
go build ./...
go test ./...
go test -run TestCheapWins -v .
dotnet new install ./templates/vmnet-plugin
dotnet new vmnet-plugin -n VerifyPlugin -o /tmp/verify-plugin
cd /tmp/verify-plugin && dotnet build -c Release
dotnet new uninstall ./templates/vmnet-plugin   # (desde la raíz del repo)
cd examples/plugin-demo/BillingRules && dotnet build -c Release && cd ..
go run .
```
