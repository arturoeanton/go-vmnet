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

        public static string DescribeGeneric<T>(T item)
        {
            return item.ToString();
        }

        public static string DescribePoint()
        {
            var p = new Point(5, 6);
            return DescribeGeneric(p);
        }
    }
}
