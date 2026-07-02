using System.Collections.Generic;

namespace Vmnet.Fixtures
{
    // Fase 2 golden fixture: generic List<T> construction and calls.
    public static class CollectionsTest
    {
        public static int Count()
        {
            var xs = new List<int>();
            xs.Add(1);
            xs.Add(2);
            return xs.Count;
        }
    }
}
