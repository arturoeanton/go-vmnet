using System;
using System.Collections.Concurrent;
using System.Text.RegularExpressions;

namespace Vmnet.Fixtures
{
    // Fase 3.24 golden fixture: a fifth cheap-win BCL bundle —
    // ConcurrentDictionary<K,V> (both GetOrAdd overloads), Regex.Replace,
    // multicast Delegate.Combine/Remove, and foreach over a plain array
    // through the non-generic System.Array/IEnumerable path (a real
    // reference-type enumerator, not List<T>'s inlined struct one).
    public static class CheapWins5
    {
        public static int ConcurrentDictGetOrAddValueTest()
        {
            var d = new ConcurrentDictionary<string, int>();
            var v1 = d.GetOrAdd("a", 10);
            var v2 = d.GetOrAdd("a", 20);
            return v1 + v2;
        }

        public static int ConcurrentDictGetOrAddFactoryTest()
        {
            var calls = 0;
            var d = new ConcurrentDictionary<string, int>();
            Func<string, int> factory = key =>
            {
                calls++;
                return key.Length;
            };
            d.GetOrAdd("hello", factory);
            d.GetOrAdd("hello", factory);
            return d.GetOrAdd("hello", factory) * 100 + calls;
        }

        public static bool ConcurrentDictTryGetValueTest()
        {
            var d = new ConcurrentDictionary<string, int>();
            d.GetOrAdd("k", 42);
            int v;
            return d.TryGetValue("k", out v) && v == 42;
        }

        public static string RegexReplaceTest(string s)
        {
            return Regex.Replace(s, @"\d+", "#");
        }

        public static int DelegateCombineTest()
        {
            var total = 0;
            Action a1 = () => { total += 1; };
            Action a2 = () => { total += 10; };
            Action combined = (Action)Delegate.Combine(a1, a2);
            combined();
            return total;
        }

        public static int DelegateCombineThenRemoveTest()
        {
            var total = 0;
            Action a1 = () => { total += 1; };
            Action a2 = () => { total += 10; };
            Action combined = (Action)Delegate.Combine(a1, a2);
            Action reduced = (Action)Delegate.Remove(combined, a2);
            reduced();
            return total;
        }

        public static int ArrayForeachAsIEnumerableTest()
        {
            var arr = new int[4];
            arr[0] = 1;
            arr[1] = 2;
            arr[2] = 3;
            arr[3] = 4;
            Array xs = arr;
            var sum = 0;
            foreach (int x in xs)
            {
                sum += x;
            }
            return sum;
        }
    }
}
