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
    }
}
