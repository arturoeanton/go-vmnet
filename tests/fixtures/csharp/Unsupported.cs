namespace Vmnet.Fixtures
{
    // Fase 3 checker fixture: deliberately uses a feature vmnet doesn't
    // support yet (System.Array: newarr/ldlen/ldelem/stelem), so the
    // checker has a reproducible, offline "this method is unsupported"
    // case to test against, alongside the real-world NuGet packages used
    // for manual certification (docs/ROADMAP.md, Fase 3).
    public static class Unsupported
    {
        public static int SumArray()
        {
            var xs = new int[] { 1, 2, 3 };
            var total = 0;
            for (var i = 0; i < xs.Length; i++)
                total += xs[i];
            return total;
        }
    }
}
