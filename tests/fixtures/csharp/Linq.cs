using System.Collections.Generic;
using System.Linq;

namespace Vmnet.Fixtures
{
    // Fase 3.14+/3.15 golden fixture: System.Linq.Enumerable — eager,
    // Machine-aware natives (internal/interpreter/linq.go), not the CLR's
    // real lazy iterators. Chained calls (Where().Select().ToList()) and
    // an array source (not just List<T>) both need to work end to end.
    public static class LinqTest
    {
        public static int SumOfEvenDoubled()
        {
            var xs = new List<int> { 1, 2, 3, 4, 5, 6 };
            var result = xs.Where(x => x % 2 == 0).Select(x => x * 2).ToList();
            var sum = 0;
            foreach (var x in result)
            {
                sum += x;
            }
            return sum;
        }

        public static bool AnyOver10()
        {
            var xs = new List<int> { 1, 2, 3 };
            return xs.Any(x => x > 10);
        }

        public static bool AllPositive()
        {
            var xs = new List<int> { 1, 2, 3 };
            return xs.All(x => x > 0);
        }

        public static int FirstEven()
        {
            var xs = new List<int> { 1, 3, 4, 5 };
            return xs.FirstOrDefault(x => x % 2 == 0);
        }

        public static int ArraySelectSum()
        {
            var arr = new int[3];
            arr[0] = 1;
            arr[1] = 2;
            arr[2] = 3;
            var doubled = arr.Select(x => x * 2).ToArray();
            var sum = 0;
            foreach (var x in doubled)
            {
                sum += x;
            }
            return sum;
        }
    }
}
