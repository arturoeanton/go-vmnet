using System.Collections.Generic;

namespace Vmnet.Fixtures
{
    // Fase 3.13 golden fixture: `foreach` over a collection accessed
    // through an interface-typed reference, instead of the concrete type
    // (Fase 3.11's fixture) — needs real interface-call dispatch, not
    // just isinst/castclass (Fase 3.8).
    public static class InterfaceForeachTest
    {
        public static int SumViaInterface()
        {
            var xs = new List<int>();
            xs.Add(10);
            xs.Add(20);
            xs.Add(30);
            IEnumerable<int> ie = xs;
            var total = 0;
            foreach (var x in ie)
            {
                total += x;
            }
            return total;
        }

        // A compiler-generated iterator (`yield return`): the method's own
        // declared return type is the interface IEnumerable<int>, so the
        // `foreach` below compiles its GetEnumerator/MoveNext/get_Current
        // call sites against the interface, never against the real
        // compiler-generated state-machine class — even though that class
        // is itself a real TypeDef in this very assembly.
        public static IEnumerable<int> CountTo(int n)
        {
            for (var i = 1; i <= n; i++)
            {
                yield return i;
            }
        }

        public static int SumCustomIterator()
        {
            var total = 0;
            foreach (var x in CountTo(4))
            {
                total += x;
            }
            return total;
        }
    }
}
