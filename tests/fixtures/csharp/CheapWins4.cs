using System;
using System.Collections.Generic;
using System.Globalization;
using System.Linq;

namespace Vmnet.Fixtures
{
    // Fase 3.23 golden fixture: a fourth cheap-win BCL bundle
    // (DateTimeOffset, DateTime operators, Double.TryParse,
    // Convert.ToInt64, Char.ToLowerInvariant, Int64.ToString,
    // ValueTuple, more LINQ, CultureInfo, IList) plus two real
    // correctness bugs this bundle exposed and fixed: the interface-
    // dispatch fallback (Fase 3.13) redirecting to a callee whose real
    // signature differs from the declared interface's (IList.Add
    // returns int, List`1.Add is void), and ldfld/stfld receiving a
    // struct receiver directly (not through a managed pointer) — a
    // shape fieldSlot never handled before.
    public static class CheapWins4
    {
        public static long DateTimeOffsetTest()
        {
            var dto = new DateTimeOffset(2024, 3, 15, 10, 0, 0, TimeSpan.Zero);
            return dto.UtcDateTime.Ticks;
        }

        public static double DateTimeSubtractTest()
        {
            var a = new DateTime(2024, 3, 16);
            var b = new DateTime(2024, 3, 15);
            var diff = a - b;
            return diff.TotalDays;
        }

        public static bool DateTimeEqualityTest()
        {
            var a = new DateTime(2024, 3, 15);
            var b = new DateTime(2024, 3, 15);
            return a == b;
        }

        public static bool DoubleTryParseTest(string s)
        {
            double result;
            return double.TryParse(s, out result);
        }

        public static long ConvertToInt64Test()
        {
            return Convert.ToInt64("123456789012");
        }

        public static char ToLowerInvariantTest(char c)
        {
            return char.ToLowerInvariant(c);
        }

        public static string Int64ToStringTest(long n)
        {
            return n.ToString();
        }

        // A struct field access without a managed pointer: Item1 goes
        // through ldloca+ldflda (an address), but Item2 — the second
        // field read in the same expression — compiles as a plain
        // ldloc+ldfld directly on the struct value, no address at all.
        // Confirmed against real IL, not assumed (Fase 3.23).
        public static string ValueTupleTest()
        {
            var t = (1, "hello");
            return t.Item1 + t.Item2;
        }

        public static int SelectManyTest()
        {
            var lists = new List<List<int>> { new List<int> { 1, 2 }, new List<int> { 3, 4 } };
            var flat = lists.SelectMany(x => x).ToList();
            var sum = 0;
            foreach (var x in flat)
            {
                sum += x;
            }
            return sum;
        }

        // Contains over an array source (not a concrete List<T>, which
        // has its own Contains taking precedence) — exercises the LINQ
        // registry's Contains, not just List<T>'s own.
        public static bool LinqContainsOverArrayTest()
        {
            var arr = new int[3];
            arr[0] = 1;
            arr[1] = 2;
            arr[2] = 3;
            return arr.Contains(2);
        }

        public static int TakeTest()
        {
            var xs = new List<int> { 1, 2, 3, 4, 5 };
            var taken = xs.Take(2).ToList();
            var sum = 0;
            foreach (var x in taken)
            {
                sum += x;
            }
            return sum;
        }

        public static string CultureNameTest()
        {
            return CultureInfo.CurrentCulture.Name;
        }

        // IList (non-generic) .Add returns int; List<T>.Add (what the
        // interface-dispatch fallback redirects to) is void — a real
        // signature mismatch the fallback must not crash on.
        public static int IListAddTest()
        {
            var concrete = new List<int>();
            System.Collections.IList xs = concrete;
            xs.Add(5);
            xs.Add(10);
            return concrete.Count;
        }
    }
}
