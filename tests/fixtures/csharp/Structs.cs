namespace Vmnet.Fixtures
{
    // Fase 3.7 golden fixture: value types (initobj/ldobj/stobj/
    // constrained.) and Nullable<T>.
    public struct Point
    {
        public int X;
        public int Y;

        public Point(int x, int y)
        {
            X = x;
            Y = y;
        }

        public int SumCoords()
        {
            return X + Y;
        }

        public void Scale(int factor)
        {
            X *= factor;
            Y *= factor;
        }
    }

    public static class Structs
    {
        public static int CreateAndSum()
        {
            var p = new Point(3, 4);
            return p.SumCoords();
        }

        public static int DefaultIsZero()
        {
            Point p = default;
            return p.SumCoords();
        }

        public static int CopySemantics()
        {
            var a = new Point(1, 1);
            var b = a;
            b.Scale(10);
            // a must be unaffected by mutating b: 1+1=2, b becomes 10+10=20.
            return a.SumCoords() * 100 + b.SumCoords();
        }

        public static bool HasValueTest(bool provide, int x)
        {
            int? n = provide ? (int?)x : null;
            return n.HasValue;
        }

        public static int GetValueOrDefaultTest(bool provide, int x)
        {
            int? n = provide ? (int?)x : null;
            return n.GetValueOrDefault();
        }

        // Fase 3.13: `int? n = 42;` (a non-conditional, direct assignment)
        // compiles to `ldloca`+`call .ctor` straight on the local's own
        // storage, not `newobj` — confirmed against real IL, the same
        // compiler shape already needing its own entry point for
        // System.DateTime (Fase 3.12) and plugin structs (this file,
        // Fase 3.7). HasValueTest/GetValueOrDefaultTest above never hit
        // this path: their ternary always goes through newobj instead.
        public static int DirectNullableAssignTest()
        {
            int? n = 42;
            return n.Value;
        }

        public static string DescribeGeneric<T>(T item)
        {
            return item.ToString();
        }

        public static string DescribePoint()
        {
            var p = new Point(5, 6);
            return DescribeGeneric(p);
        }

        // NullableOfPluginStructGetValueOrDefault: found broken building
        // examples/jint-advanced-demo — real Esprima.JavaScriptParser has
        // a field typed `Esprima.ArrayList<Token>?` (Nullable<T> wrapping
        // a plugin-defined GENERIC value type, not a numeric primitive),
        // and `.GetValueOrDefault()` on it, never yet assigned, used to
        // return a raw int32 where a real, zeroed struct was expected —
        // internal/bcl/system_nullable.go's own shared Nullable`1 native
        // hardcodes Int32(0) as its "value" field default, correct for
        // the dominant real case (int?/double?/bool?) but silently wrong
        // for any other T. `Point` here is non-generic, but the fix
        // (assembly.go's nullableValueTypeDefault, internal/interpreter/
        // structs.go's nullableDefaultFor) applies identically regardless
        // of whether T is itself generic — this is the simplest fixture
        // that exercises the same "T is a plugin struct" class of bug.
        public static int NullableOfPluginStructGetValueOrDefault()
        {
            Point? p = null;
            var got = p.GetValueOrDefault();
            return got.SumCoords();
        }
    }
}
