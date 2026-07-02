namespace Vmnet.Fixtures
{
    // Fase 3 checker fixture: deliberately uses a feature vmnet doesn't
    // support yet (try/finally: leave/endfinally), so the checker has a
    // reproducible, offline "this method is unsupported" case to test
    // against, alongside the real-world NuGet packages used for manual
    // certification (docs/ROADMAP.md). System.Array moved here originally
    // but is now supported (Fase 3.5) — see Arrays.cs.
    public static class Unsupported
    {
        public static int TryFinally()
        {
            var x = 0;
            try
            {
                x = 1;
            }
            finally
            {
                x = 2;
            }
            return x;
        }
    }
}
