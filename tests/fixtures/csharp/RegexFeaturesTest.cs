using System.Text.RegularExpressions;

namespace Vmnet.Fixtures
{
    // Fase 3.79 golden fixtures: regress a chain of real bugs found while
    // getting real Jint/Esprima regex-literal support working end to end
    // — a `Regex` object's own `as Regex` cast (isAssignableTo's
    // nativeMatches fallback, internal/interpreter/typecheck.go, needed
    // *nativeRegex registered in internal/bcl/system_object.go's
    // NativeTypeName — without it, a just-constructed real Regex was
    // silently discarded as null the moment anything cast it back from
    // `object`), the count-limited Match/Replace overloads (Regex(string,
    // int beginning)/Replace(string, string, int count) — Jint's own
    // RegExpPrototype always calls these, never the bare 2-/3-arg ones),
    // Capture.Index/Length (RegExpPrototype.Exec's own result
    // construction), and Match.NextMatch() (the real, idiomatic
    // "iterate every match by hand" pattern String.Split(Regex)-style
    // code uses internally).
    public static class RegexFeaturesTest
    {
        // Regresses the `as Regex` cast: a Regex constructed, stored as
        // `object`, and cast back — exactly Esprima.RegExpParseResult's
        // own `_regexOrConversionError as Regex` shape.
        public static bool CastBackFromObject()
        {
            object boxed = new Regex("[0-9]+");
            Regex re = boxed as Regex;
            return re != null && re.IsMatch("abc123");
        }

        // Regresses the count-limited Match(string, int) overload —
        // finds the second occurrence by starting the search past the
        // first.
        public static string MatchWithBeginning()
        {
            var re = new Regex("[0-9]+");
            var first = re.Match("a1b22c333");
            var second = re.Match("a1b22c333", first.Index + first.Length);
            return second.Value;
        }

        // Regresses Capture.Index/Length (Match inherits both from
        // Capture).
        public static string MatchIndexAndLength()
        {
            var re = new Regex("[0-9]+");
            var m = re.Match("ab123cd");
            return m.Index + "," + m.Length;
        }

        // Regresses the count-limited Replace(string, string, int)
        // overload — replaces only the first occurrence, matching real
        // JS `.replace(regex, x)` (no `g` flag) semantics.
        public static string ReplaceFirstOnly()
        {
            var re = new Regex("[0-9]");
            return re.Replace("a1b2c3", "X", 1);
        }

        // Regresses Match.NextMatch() — walks every match by hand,
        // mirroring the real .NET regex-based String.Split(Regex)
        // algorithm.
        public static string WalkAllMatches()
        {
            var re = new Regex("[0-9]+");
            var sb = new System.Text.StringBuilder();
            var m = re.Match("a1bb22ccc333");
            while (m.Success)
            {
                if (sb.Length > 0) sb.Append(',');
                sb.Append(m.Value);
                m = m.NextMatch();
            }
            return sb.ToString();
        }

        // Regresses MatchCollection.get_Item — indexing the collection
        // directly (as opposed to a foreach) used to throw
        // unconditionally. Mirrors Jint's own RegExpPrototype global
        // `.match()` implementation.
        public static string IndexIntoMatchCollection()
        {
            var re = new Regex("[0-9]+");
            var matches = re.Matches("a1 bb22 ccc333");
            return matches[0].Value + "," + matches[1].Value + "," + matches[2].Value;
        }
    }
}
