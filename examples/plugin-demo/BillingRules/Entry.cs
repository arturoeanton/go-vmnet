using System;
using System.Globalization;
using System.Text;

namespace BillingRules
{
    // Entry is the one contract a vmnet plugin needs to expose: a public
    // static `byte[] X(byte[])` method — exactly what vmnet's own
    // Assembly.CallBytes/CallJSON already know how to call directly, no
    // interface, base class, or registration step on the Go side.
    //
    // This file started as `dotnet new vmnet-plugin -n BillingRules`'s own
    // generated starter (templates/vmnet-plugin/Entry.cs) — Invoke's body
    // below has been replaced with a small real business rule (an 8% flat
    // tax line), which is exactly the intended path: generate, then fill
    // in your own logic.
    public static class Entry
    {
        // Invoke computes an invoice line from {"customer":"...",
        // "amount": 100} and returns {"customer":"...", "amount":100,
        // "tax":8, "total":108}. Kept dependency-free (no JSON library
        // reference) on purpose, same rationale as the template's own
        // starter — see docs/en/plugin-sdk.md for when to reach for
        // Newtonsoft.Json/System.Text.Json instead.
        public static byte[] Invoke(byte[] input)
        {
            string json = Encoding.UTF8.GetString(input);
            string customer = ReadStringField(json, "customer") ?? "unknown";
            double amount = ReadNumberField(json, "amount") ?? 0;
            double tax = Math.Round(amount * 0.08, 2);
            double total = Math.Round(amount + tax, 2);

            string output = "{\"customer\":\"" + EscapeJson(customer) + "\","
                + "\"amount\":" + amount.ToString(CultureInfo.InvariantCulture) + ","
                + "\"tax\":" + tax.ToString(CultureInfo.InvariantCulture) + ","
                + "\"total\":" + total.ToString(CultureInfo.InvariantCulture) + "}";
            return Encoding.UTF8.GetBytes(output);
        }

        // ReadStringField finds "field":"value" in a flat JSON object and
        // returns value, or null if field isn't present. Deliberately
        // minimal (no nesting, no unicode escapes) — swap in a real JSON
        // library once your plugin's input shape grows beyond what a
        // starter template should hand-parse.
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

        // ReadNumberField finds "field":123.45 (a bare JSON number, no
        // quotes) and parses it, or returns null if field isn't present
        // or isn't a well-formed number.
        private static double? ReadNumberField(string json, string field)
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
            int i = colonIndex + 1;
            while (i < json.Length && json[i] == ' ')
            {
                i++;
            }
            int start = i;
            while (i < json.Length && (char.IsDigit(json[i]) || json[i] == '.' || json[i] == '-'))
            {
                i++;
            }
            if (i == start)
            {
                return null;
            }
            string numStr = json.Substring(start, i - start);
            double value;
            if (!double.TryParse(numStr, NumberStyles.Float, CultureInfo.InvariantCulture, out value))
            {
                return null;
            }
            return value;
        }

        private static string EscapeJson(string value)
        {
            return value.Replace("\\", "\\\\").Replace("\"", "\\\"");
        }
    }
}
