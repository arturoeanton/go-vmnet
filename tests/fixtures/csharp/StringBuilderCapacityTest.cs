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
}
