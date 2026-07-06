using System;
using System.Collections.Generic;
using System.Runtime.CompilerServices;

namespace Vmnet.Fixtures
{
    // Fase 3.13 golden fixture: a bundle of high-breadth, cheap-to-add
    // BCL natives found by the Fase 3.13 probe (String/Char predicates
    // and helpers, List<T>/Dictionary<K,V> extras) — bundled the same way
    // Fase 3.6's "switch + BCL barata de alto alcance" was.
    public static class CheapWins
    {
        public static string StringChecks(string s)
        {
            var result = "";
            result += string.IsNullOrEmpty(s) ? "1" : "0";
            result += string.IsNullOrWhiteSpace("   ") ? "1" : "0";
            result += s.StartsWith("He") ? "1" : "0";
            result += s.IndexOf("World").ToString();
            result += ";";
            result += s.Replace("World", "Go");
            result += ";";
            result += s.Trim();
            return result;
        }

        // IndexOfWithComparison exercises the real IndexOf(string,
        // StringComparison) overload — found broken while building the
        // vmnet-plugin template's own default Entry.cs: vmnet's IndexOf
        // native used to treat ANY trailing int argument as a start
        // index, so IndexOf(needle, StringComparison.Ordinal) (Ordinal's
        // own raw value, 4) got silently misread as "start searching at
        // rune index 4" instead of being recognized as a comparison-type
        // argument to ignore (see stringComparisonSensitiveNatives,
        // internal/interpreter/calls.go). Both calls below must agree.
        public static string IndexOfWithComparison(string s, string needle)
        {
            var plain = s.IndexOf(needle);
            var ordinal = s.IndexOf(needle, StringComparison.Ordinal);
            return plain + "|" + ordinal;
        }

        public static string SplitJoin(string csv)
        {
            var parts = csv.Split(',');
            return string.Join("|", parts);
        }

        public static string CharChecks()
        {
            var result = "";
            result += char.IsUpper('A') ? "1" : "0";
            result += char.IsDigit('5') ? "1" : "0";
            result += char.IsWhiteSpace(' ') ? "1" : "0";
            result += 'x'.ToString();
            return result;
        }

        public static string IntToString(int n)
        {
            return n.ToString();
        }

        public static int ListExtras()
        {
            var xs = new List<int>();
            xs.Add(1);
            xs.Add(2);
            xs.Add(3);
            xs[1] = 20;
            xs.AddRange(new List<int> { 4, 5 });
            var arr = xs.ToArray();
            var total = 0;
            foreach (var x in arr)
            {
                total += x;
            }
            return total * 10 + (xs.Contains(20) ? 1 : 0);
        }

        public static int DictTryGetValue()
        {
            var d = new Dictionary<string, int>();
            d.Add("a", 10);
            int found;
            int missing;
            var okFound = d.TryGetValue("a", out found);
            var okMissing = d.TryGetValue("z", out missing);
            return (okFound ? 1 : 0) * 100 + found * 10 + (okMissing ? 1 : 0);
        }

        // CharGetHashCode: found missing entirely (not a wrong-answer
        // bug, a genuine gap) building examples/jint-advanced-demo, whose
        // real Jint/Esprima source calls this from its own tokenizer's
        // character-class lookup tables. Real .NET's own exact bit
        // pattern isn't the contract — only that equal chars hash equal —
        // so this just checks that invariant, not a specific value.
        public static bool CharGetHashCode()
        {
            return 'x'.GetHashCode() == 'x'.GetHashCode() && 'x'.GetHashCode() != 'y'.GetHashCode();
        }

        // CharSurrogateChecks: also found missing building the same demo
        // (real Jint's own JSON.stringify checks every character position
        // for a UTF-16 surrogate pair while escaping a string). vmnet
        // strings store real Unicode code points (runes), never raw
        // UTF-16 surrogate halves, so both real .NET surrogate range
        // values used directly here (0xD800/0xDC00) are the only way to
        // exercise the real numeric-range check at all.
        public static string CharSurrogateChecks()
        {
            var result = "";
            result += char.IsSurrogate((char)0xD800) ? "1" : "0";
            result += char.IsSurrogate('x') ? "1" : "0";
            result += char.IsSurrogatePair((char)0xD800, (char)0xDC00) ? "1" : "0";
            result += char.IsSurrogatePair('x', 'y') ? "1" : "0";
            return result;
        }

        // RuntimeHelpersGetHashCode: found missing entirely building the
        // same demo (real Jint's own internal dictionary bookkeeping for
        // object/array literal construction calls this directly, to hash
        // by reference identity regardless of any GetHashCode() override
        // the receiver's own type might declare).
        public static bool RuntimeHelpersGetHashCode()
        {
            var o = new object();
            return RuntimeHelpers.GetHashCode(o) == RuntimeHelpers.GetHashCode(o);
        }

        // ListCapacity: found missing entirely building the same demo
        // (real Jint's own internal property storage uses a List<T> the
        // same way Esprima's ArrayList<T> does, and checks Capacity
        // before growing/serializing it). vmnet's own List<T> has no
        // separate capacity/count distinction (get_Capacity reports the
        // current Count itself, set_Capacity is a no-op) — this just
        // confirms both are callable and Capacity never reports less
        // than the real Count.
        public static int ListCapacity()
        {
            var xs = new List<int>();
            xs.Add(1);
            xs.Add(2);
            xs.Capacity = 10;
            return xs.Capacity >= xs.Count ? 1 : 0;
        }
    }
}
