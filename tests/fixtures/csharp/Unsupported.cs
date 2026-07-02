namespace Vmnet.Fixtures
{
    // Fase 3 checker fixture: deliberately uses a feature vmnet doesn't
    // support yet, so the checker has a reproducible, offline "this
    // method is unsupported" case to test against, alongside the
    // real-world NuGet packages used for manual certification
    // (docs/ROADMAP.md). Repurposed twice already as vmnet's coverage
    // grew: System.Array (until Fase 3.5) then plain try/finally (until
    // Fase 3.10) both moved out once they became supported. Exception
    // filters (`catch (Foo) when (cond)`) are the current gap — Fase
    // 3.10 implements catch/finally/fault but explicitly not filter
    // clauses (see ir/builder.go's buildHandlers).
    public static class Unsupported
    {
        public static int FilterClause()
        {
            var x = 0;
            try
            {
                throw new System.ArgumentException("boom");
            }
            catch (System.ArgumentException) when (x == 0)
            {
                x = 2;
            }
            return x;
        }
    }
}
