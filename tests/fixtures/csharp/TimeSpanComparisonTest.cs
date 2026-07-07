using System;

namespace Vmnet.Fixtures
{
    // Fase 3.79 golden fixture: regresses TimeSpan's comparison operators
    // (op_Equality/op_Inequality/op_GreaterThan/op_LessThan/.../
    // CompareTo) — none had a native registered at all before this fix.
    // TimeSpan has no real TypeDef anywhere (a synthetic vmnet value
    // type, internal/bcl/system_timespan.go's own doc comment), so real
    // IL comparing two TimeSpan values with `==`/`!=`/`<`/`>`/... has no
    // interpreted body to fall back to. Found running real Jint/Esprima:
    // System.Text.RegularExpressions.Regex's own MatchTimeout property
    // (a TimeSpan) gets compared with `==` somewhere in its own real,
    // interpreted static initialization/validation path.
    public static class TimeSpanComparisonTest
    {
        public static string Run()
        {
            var a = TimeSpan.FromSeconds(5);
            var b = TimeSpan.FromSeconds(5);
            var c = TimeSpan.FromSeconds(10);

            bool eq = a == b;
            bool neq = a != c;
            bool gt = c > a;
            bool lt = a < c;
            bool gte = a >= b;
            bool lte = a <= c;
            int cmp = a.CompareTo(c);

            return eq + "," + neq + "," + gt + "," + lt + "," + gte + "," + lte + "," + cmp;
        }
    }
}
