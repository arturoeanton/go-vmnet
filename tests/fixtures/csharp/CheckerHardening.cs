using System;
using System.Collections.Generic;

namespace Vmnet.Fixtures
{
    // Fase 3.74 golden fixture: real regression coverage for the natives
    // added while pushing ClosedXML/System.Text.Json over the checker's
    // own 97% bar (docs/en/COMPATIBILITY.md) — IReadOnlyDictionary`2
    // dispatch against a real Dictionary`2 receiver, ArraySegment`1,
    // Array.CopyTo, Exception.Source, KeyNotFoundException, and
    // List`1/Dictionary`2/HashSet`1's own IsReadOnly.
    public static class CheckerHardeningTest
    {
        public static int ReadOnlyDictionaryDispatch()
        {
            var dict = new Dictionary<string, int> { { "a", 1 }, { "b", 2 } };
            IReadOnlyDictionary<string, int> ro = dict;
            var sum = ro["a"] + ro["b"];
            if (ro.TryGetValue("a", out var v)) sum += v;
            if (ro.ContainsKey("b")) sum += 100;
            foreach (var k in ro.Keys) sum += k.Length;
            foreach (var val in ro.Values) sum += val;
            return sum;
        }

        public static int ArraySegmentRoundTrip()
        {
            var arr = new[] { 10, 20, 30, 40, 50 };
            var whole = new ArraySegment<int>(arr);
            var slice = new ArraySegment<int>(arr, 1, 3);
            return whole.Count + slice.Count + slice.Offset + slice.Array[slice.Offset];
        }

        public static int ArrayCopyTo()
        {
            var src = new[] { 1, 2, 3 };
            var dst = new int[5];
            src.CopyTo(dst, 2);
            return dst[2] + dst[3] + dst[4];
        }

        public static string ExceptionSourceRoundTrip()
        {
            var ex = new Exception("boom");
            ex.Source = "Vmnet.Fixtures";
            return ex.Source;
        }

        public static bool KeyNotFoundExceptionCatch()
        {
            var dict = new Dictionary<string, int>();
            try
            {
                if (!dict.ContainsKey("missing"))
                {
                    throw new KeyNotFoundException("missing key");
                }
                return false;
            }
            catch (KeyNotFoundException)
            {
                return true;
            }
        }

        public static bool CollectionsAreNotReadOnly()
        {
            ICollection<int> list = new List<int>();
            ICollection<KeyValuePair<string, int>> dict = new Dictionary<string, int>();
            ICollection<int> set = new HashSet<int>();
            return !list.IsReadOnly && !dict.IsReadOnly && !set.IsReadOnly;
        }

        // The four checks below all construct a real BCL value type
        // directly into a local variable (`var x = new Foo(...);`) — real
        // Roslyn compiles this to `ldloca` + `call instance .ctor`, NOT
        // `newobj` (confirmed via real IL disassembly), a genuinely
        // different call shape than the same constructor reached as a
        // standalone expression. Found auditing every registerValueTypeCtor
        // entry (internal/bcl) for a missing in-place counterpart after
        // discovering the gap while adding ArraySegment<T> support.
        public static bool GuidLocalAssign()
        {
            var g = new Guid("11111111-1111-1111-1111-111111111111");
            return g.ToString() == "11111111-1111-1111-1111-111111111111";
        }

        public static int ReadOnlySpanLocalAssign()
        {
            var arr = new[] { 'a', 'b', 'c' };
            var span = new ReadOnlySpan<char>(arr);
            return span.Length;
        }

        public static int SpanLocalAssign()
        {
            var arr = new[] { 1, 2, 3, 4 };
            var span = new Span<int>(arr, 1, 2);
            return span.Length;
        }

        public static bool CancellationTokenLocalAssign()
        {
            var token = new System.Threading.CancellationToken(true);
            return token.IsCancellationRequested;
        }
    }
}
