namespace Vmnet.Fixtures
{
    // Fase 3.5 golden fixture: newarr/ldlen/ldelem.i4/stelem.i4. Elements
    // are assigned individually (not via a `{ 1, 2, 3 }` literal) to avoid
    // the compiler's RuntimeHelpers.InitializeArray fast path (ldtoken +
    // a data blob), which is a separate, still-unsupported feature — see
    // Unsupported.cs.
    public static class Arrays
    {
        public static int SumArray()
        {
            var xs = new int[3];
            xs[0] = 1;
            xs[1] = 2;
            xs[2] = 3;

            var total = 0;
            for (var i = 0; i < xs.Length; i++)
                total += xs[i];
            return total;
        }
    }
}
