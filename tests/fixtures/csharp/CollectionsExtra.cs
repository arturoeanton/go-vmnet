using System;
using System.Collections.Generic;

namespace Vmnet.Fixtures
{
    // Fase 3.19 golden fixture: HashSet<T>, Stack<T>, TimeSpan.
    public static class CollectionsExtra
    {
        public static int HashSetTest()
        {
            var hs = new HashSet<int>();
            hs.Add(1);
            hs.Add(2);
            hs.Add(1);
            var sum = 0;
            foreach (var x in hs)
            {
                sum += x;
            }
            return sum * 10 + (hs.Contains(2) ? 1 : 0);
        }

        public static int StackTest()
        {
            var s = new Stack<int>();
            s.Push(1);
            s.Push(2);
            s.Push(3);
            var top = s.Pop();
            return top * 100 + s.Count;
        }

        public static int TimeSpanFromSecondsTest()
        {
            var ts = TimeSpan.FromSeconds(90);
            return ts.Minutes * 100 + ts.Seconds;
        }

        public static int TimeSpanCtorTest()
        {
            var ts = new TimeSpan(1, 2, 3);
            return ts.Hours * 10000 + ts.Minutes * 100 + ts.Seconds;
        }
    }
}
