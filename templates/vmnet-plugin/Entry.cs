using System;
using System.Text;

namespace PluginName
{
    // Entry is the one contract a vmnet plugin needs to expose: a public
    // static `byte[] X(byte[])` method — exactly what vmnet's own
    // Assembly.CallBytes/CallJSON already know how to call directly, no
    // interface, base class, or registration step on the Go side (see
    // https://github.com/arturoeanton/go-vmnet docs/en/plugin-sdk.md).
    //
    // From Go:
    //
    //   plugin, err := vm.LoadFile("bin/Release/netstandard2.0/PluginName.dll")
    //   out, err := plugin.CallBytes("PluginName.Entry", "Invoke", input)
    //
    // or, for automatic JSON marshaling on the Go side:
    //
    //   out, err := plugin.CallJSON("PluginName.Entry", "Invoke", map[string]any{"name": "Ada"})
    public static class Entry
    {
        // Invoke receives UTF-8 JSON bytes and returns UTF-8 JSON bytes.
        // This starter reads a single "name" string field and echoes it
        // back inside a greeting — replace the body with your real plugin
        // logic. It's kept dependency-free (no JSON library reference) on
        // purpose, so a freshly generated plugin builds and runs with
        // nothing beyond the .NET SDK. If your own plugin needs real JSON
        // object graphs, add Newtonsoft.Json or System.Text.Json to
        // PluginName.csproj — see this project's own
        // docs/en/COMPATIBILITY.md for what's verified working under
        // vmnet today.
        public static byte[] Invoke(byte[] input)
        {
            string json = Encoding.UTF8.GetString(input);
            string name = ReadStringField(json, "name");
            if (name == null)
            {
                name = "world";
            }
            string message = "Hello, " + name + "!";
            string output = "{\"message\":\"" + EscapeJson(message) + "\"}";
            return Encoding.UTF8.GetBytes(output);
        }

        // ReadStringField finds "field":"value" in a flat JSON object and
        // returns value, or null if field isn't present. This is
        // deliberately minimal (no nesting, no unicode escapes) — swap in
        // a real JSON library once your plugin's input shape grows beyond
        // what a starter template should hand-parse.
        private static string ReadStringField(string json, string field)
        {
            string needle = "\"" + field + "\"";
            int keyIndex = json.IndexOf(needle, StringComparison.Ordinal);
            if (keyIndex < 0)
            {
                return null;
            }
            int colonIndex = json.IndexOf(':', keyIndex + needle.Length);
            if (colonIndex < 0)
            {
                return null;
            }
            int firstQuote = json.IndexOf('"', colonIndex + 1);
            if (firstQuote < 0)
            {
                return null;
            }
            int secondQuote = json.IndexOf('"', firstQuote + 1);
            if (secondQuote < 0)
            {
                return null;
            }
            return json.Substring(firstQuote + 1, secondQuote - firstQuote - 1);
        }

        private static string EscapeJson(string value)
        {
            return value.Replace("\\", "\\\\").Replace("\"", "\\\"");
        }
    }
}
