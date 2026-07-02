namespace Vmnet.Fixtures
{
    // Fase 1 golden fixture: branches and a loop.
    public static class Loops
    {
        public static int Sum(int n)
        {
            var total = 0;
            for (var i = 0; i <= n; i++)
                total += i;
            return total;
        }

        // Fase 2 sandbox fixture: a genuine infinite loop, used to prove
        // MaxInstructions actually stops a runaway plugin (docs/ROADMAP.md).
        public static int Runaway()
        {
            var i = 0;
            while (true)
            {
                i = i + 1;
            }
        }
    }
}
