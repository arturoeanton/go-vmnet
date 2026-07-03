using System;
using System.Collections.Generic;

namespace Vmnet.Fixtures
{
    // Fase 3.21 golden fixture: a third cheap-win BCL bundle, same
    // "measure, then bundle" pattern as Fase 3.6/3.13/3.18.
    public static class CheapWins3
    {
        public static string NotImplTest()
        {
            try
            {
                throw new NotImplementedException("nope");
            }
            catch (NotImplementedException e)
            {
                return e.Message;
            }
        }

        public static bool InfinityTest(double d)
        {
            return double.IsInfinity(d);
        }

        public static bool PosInfTest(double d)
        {
            return double.IsPositiveInfinity(d);
        }

        public static bool NegInfTest(double d)
        {
            return double.IsNegativeInfinity(d);
        }

        public static bool EndsWithTest(string s)
        {
            return s.EndsWith("lo");
        }

        public static double FloorTest(double d)
        {
            return Math.Floor(d);
        }

        public static int ListClearTest()
        {
            var xs = new List<int> { 1, 2, 3 };
            xs.Clear();
            return xs.Count;
        }

        public static int ParseTest()
        {
            return int.Parse("42");
        }

        public static int TryParseTest(string s)
        {
            int result;
            var ok = int.TryParse(s, out result);
            return ok ? result : -1;
        }

        public static int CompareToTest(int a, int b)
        {
            return a.CompareTo(b);
        }

        public static bool DictRemoveTest()
        {
            var d = new Dictionary<string, int>();
            d.Add("a", 1);
            var removed = d.Remove("a");
            return removed && d.Count == 0;
        }

        public static int DateTimeKindTest()
        {
            var dt = DateTime.UtcNow;
            return (int)dt.Kind;
        }

        public static string KeyValuePairCtorTest()
        {
            var kv = new KeyValuePair<string, int>("k", 42);
            return kv.Key + "=" + kv.Value;
        }

        // xs[1] via IList<T>::get_Item and ro.Count via
        // IReadOnlyCollection<T>::get_Count both need the Fase 3.13
        // interface-dispatch fallback — neither is registered under the
        // interface name directly, both redirect to List`1's own
        // get_Item/get_Count.
        public static int InterfaceCollectionTest()
        {
            var concrete = new List<int> { 10, 20, 30 };
            IList<int> xs = concrete;
            IReadOnlyCollection<int> ro = concrete;
            return xs[1] + ro.Count;
        }
    }
}
