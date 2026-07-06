# newtonsoft-json-demo

Parses and reads a real JSON document through the real, unmodified
`Newtonsoft.Json` 13.0.3 NuGet package — no dotnet SDK installed, no
compiled C# wrapper. It drives Newtonsoft's own "LINQ to JSON" DOM
(`JObject`/`JValue`, the real dynamic-style JSON tree API — no custom POCO
class definition needed) via `Assembly.Call`/`Instance.Call`, the same
no-wrapper pattern `examples/jint-nowrapper` and
`examples/system-text-json-demo` use, rather than
`JsonConvert.DeserializeObject<T>`, which would need a compiled C# type to
deserialize into.

```bash
go run .
```

Expected output:

```txt
vmnet:42
```

## What this closed, alongside a broad BCL hardening pass (Fase 3.43)

`docs/en/ROADMAP.md` pairs this demo with a general IL/BCL sweep, on the
standing principle that broader BCL coverage compounds in value across
every future package, not just the one currently being probed.

Newtonsoft.Json-specific fixes, each a real general bug rather than a
package-specific patch:

- A `KindObject` argument silently "matched" a `SigSZArray` parameter, and
  a numeric argument silently "matched" a `SigString` one — `JContainer.
  InsertItem` calls the virtual `ValidateToken(item, null)`, and since
  `JProperty` doesn't override it, the ancestor walk found `JToken`'s
  unrelated private static `ValidateToken(JToken, JTokenType[], bool)`
  (same arity) and accepted it. Both shapes are now hard-rejected as
  impossible, matching `KindObject`-vs-`SigValueType`'s existing rejection.
- `System.IO.StringReader`/`TextReader` — `JObject.Parse(string)` always
  goes through `new JsonTextReader(new StringReader(json))`.
- `System.Collections.ObjectModel.Collection<T>` and the 3-/4-arg
  `Array.IndexOf(array, value, startIndex[, count])` overloads, both
  reached by `JPropertyKeyedCollection`'s own base implementation.

General hardening from the same phase, found by systematic gap survey
rather than any one package's probe: `Convert.ToBase64String`/
`FromBase64String`, the remaining narrowing/widening `Convert.To*`
numeric conversions, `String.TrimStart`/`TrimEnd`/`PadLeft`/`PadRight`/
`Insert`/`Remove`, and the `Predicate<T>`/`Action<T>`/`Converter<T,
TOutput>`-taking `Array` statics (`Reverse`/`Fill`/`Find`/`FindLast`/
`FindIndex`/`FindAll`/`Exists`/`ForEach`/`TrueForAll`/`ConvertAll`/
`LastIndexOf`).

See `docs/en/COMPATIBILITY.md` for the full measured/verified state across
every package this project tracks.
