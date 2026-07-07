using System.Text;

namespace Vmnet.Fixtures
{
    // Fase 3.79 golden fixture: regresses StringBuilder.set_Capacity
    // (internal/bcl/system_stringbuilder.go) — it wasn't registered at
    // all before this fix, so real code calling it fell through to a
    // nonsensical "real interpreted body" attempt (StringBuilder has no
    // real TypeDef/IL body anywhere for vmnet to run — it's native-only),
    // surfacing as a confusing "type System.Text.StringBuilder not
    // found" instead of running successfully. Found running real Jint/
    // Esprima: Esprima.Scanner.RegExpParser.ParsePattern sizes its own
    // reusable StringBuilder via `sb.Capacity = pattern.Length` before
    // every single regex literal it scans.
    public static class StringBuilderCapacityTest
    {
        public static string Run()
        {
            var sb = new StringBuilder();
            sb.Append("hi");
            sb.Capacity = 32;
            sb.Append("!");
            return sb.ToString();
        }
    }

    // Fase 3.79 golden fixture: regresses StringBuilder.ToString(int
    // startIndex, int length) — this overload used to ignore both
    // arguments and always return the whole buffer. Found running real
    // Jint/Esprima: Scanner.ScanRegExpBody's own `stringBuilder.ToString
    // (1, stringBuilder.Length - 2)` strips a scanned regex literal's own
    // leading/trailing `/` delimiters this way — without the fix, every
    // regex literal's own pattern kept both delimiters (`/a/` instead of
    // `a`), so `/a/.test('abc')` compiled a literal substring search for
    // the three characters "/a/" instead of the letter "a", silently
    // returning false for every regex literal, no exception at all.
    public static class StringBuilderSubstringTest
    {
        public static string Run()
        {
            var sb = new StringBuilder();
            sb.Append("/pattern/");
            return sb.ToString(1, sb.Length - 2);
        }
    }

    // Fase 3.80 golden fixture: regresses StringBuilder.Append(string
    // value, int startIndex, int count) — the real substring-append
    // overload silently did nothing at all (every other Append overload
    // collapses to "stringify the one value", but this one is a
    // genuinely different 3-real-argument shape that fell through
    // unhandled). Found running real Jint/Esprima: Scanner.RegExpParser.
    // ParsePattern's own `stringBuilder.Append(_pattern, index, 1 +
    // ((int)regExpGroupType >> 2))` appends a parenthesized group's own
    // opening delimiter(s) this way — every regex literal with a group
    // silently lost its own opening paren from the translated .NET
    // pattern (`(abc)` became the invalid `abc)`) — and the identical
    // call shape (`stringBuilder.Append(pattern, num, 2)`) also appends
    // `\d`/`\w`/`\s`/`\D`/`\W` backslash shorthand classes, which
    // vanished from the translated pattern entirely for the same reason.
    public static class StringBuilderAppendSubstringTest
    {
        public static string Run()
        {
            var sb = new StringBuilder();
            sb.Append('(');
            sb.Append("hello world", 6, 5);
            sb.Append(')');
            return sb.ToString();
        }
    }
}
