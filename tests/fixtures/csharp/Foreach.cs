using System.Collections.Generic;

namespace Vmnet.Fixtures
{
    // Fase 3.11 golden fixture: `foreach` over List<T>/Dictionary<K,V>.
    public static class ForeachTest
    {
        public static int SumList()
        {
            var xs = new List<int>();
            xs.Add(1);
            xs.Add(2);
            xs.Add(3);
            var total = 0;
            foreach (var x in xs)
            {
                total += x;
            }
            return total;
        }

        public static int SumDictionaryValues()
        {
            var d = new Dictionary<string, int>();
            d.Add("a", 10);
            d.Add("b", 20);
            d.Add("c", 30);
            var total = 0;
            foreach (var kv in d)
            {
                total += kv.Value;
            }
            return total;
        }

        public static int EqualityComparerDefaultEquals(int a, int b)
        {
            var cmp = EqualityComparer<int>.Default;
            return cmp.Equals(a, b) ? 1 : 0;
        }

        public static int MathMinMax(int a, int b)
        {
            return System.Math.Min(a, b) * 100 + System.Math.Max(a, b);
        }

        public static string JoinStrings()
        {
            var xs = new List<string>();
            xs.Add("a");
            xs.Add("b");
            xs.Add("c");
            return string.Join(",", xs);
        }
    }
}
