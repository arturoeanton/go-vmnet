using System.Collections;
using System.Collections.Generic;

namespace Vmnet.Fixtures
{
    // Fase 3.83 golden fixtures: three small, independent gaps found
    // auditing the same real corpus that drove Fase 3.81/3.82 — each
    // scoped narrow and fixed the same way its own sibling case already
    // was.

    // A real, plugin-defined class implementing IEnumerable<int> via its
    // own compiler-generated iterator (yield return), not a vmnet-native
    // List<T>/array — the same shape
    // GenericSentinelForwarding.cs's own CustomStringCollection is, just
    // int-valued (mirrors CsvHelper's own `new List<Product>(csv.
    // GetRecords<Product>())`, a real plugin iterator as the List<T>
    // constructor's own source).
    public class CustomIntCollection : IEnumerable<int>, IEnumerable
    {
        private readonly List<int> items = new List<int>();

        public void Add(int item)
        {
            items.Add(item);
        }

        public IEnumerator<int> GetEnumerator()
        {
            for (int i = 0; i < items.Count; i++)
            {
                yield return items[i];
            }
        }

        IEnumerator IEnumerable.GetEnumerator()
        {
            return items.GetEnumerator();
        }
    }

    public static class ListCtorFromEnumerable
    {
        // List<T>(IEnumerable<T> collection) given a real plugin iterator
        // — before Fase 3.83, the registered List`1 constructor never
        // even read its own arguments, silently building an empty list
        // regardless of what was passed.
        public static int FromCustomIterator()
        {
            var source = new CustomIntCollection();
            source.Add(10);
            source.Add(20);
            source.Add(30);
            var list = new List<int>(source);
            int sum = 0;
            foreach (var x in list)
            {
                sum += x;
            }
            return list.Count * 1000 + sum;
        }

        // The same gap applies to copying from an already-materialized
        // vmnet-native List<T> (the m.enumerateAll fast path, not just
        // the real-iterator slow path).
        public static int FromExistingList()
        {
            var source = new List<int>();
            source.Add(1);
            source.Add(2);
            var copy = new List<int>(source);
            return copy.Count * 100 + copy[0] * 10 + copy[1];
        }

        // The OTHER real 1-arg overload, List<T>(int capacity), must
        // still behave as a plain empty list with no elements at all —
        // a bare KindI4 argument is never mistaken for something to
        // enumerate.
        public static int WithCapacityStillEmpty()
        {
            var list = new List<int>(16);
            return list.Count;
        }

        // System.Collections.ArrayList shares the exact same native
        // backing (and the exact same pre-3.83 gap) as List<T>.
        public static int ArrayListFromExistingList()
        {
            var source = new List<int>();
            source.Add(7);
            source.Add(8);
            var arrayList = new ArrayList(source);
            return arrayList.Count * 100 + (int)arrayList[0] * 10 + (int)arrayList[1];
        }
    }

    public static class NarrowTryParseTest
    {
        // Int16.TryParse — Fase 3.83, found via CsvHelper's own
        // BooleanConverter.ConvertFromString falling back to
        // short.TryParse after a plain bool.TryParse fails.
        public static string Int16RoundTrip(string text)
        {
            bool ok = short.TryParse(text, out short result);
            return ok + ":" + result;
        }

        // Single.TryParse — Fase 3.83, found via CsvHelper's own
        // SingleConverter.ConvertFromString.
        public static string SingleRoundTrip(string text)
        {
            bool ok = float.TryParse(text, out float result);
            return ok + ":" + result;
        }
    }
}
