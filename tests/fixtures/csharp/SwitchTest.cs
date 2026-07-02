namespace Vmnet.Fixtures
{
    // Fase 3.6 golden fixture: a dense, zero-based int switch compiles to
    // the `switch` opcode (a jump table) instead of a chain of branches.
    public static class SwitchTest
    {
        public static string DayName(int day)
        {
            switch (day)
            {
                case 0:
                    return "Sunday";
                case 1:
                    return "Monday";
                case 2:
                    return "Tuesday";
                case 3:
                    return "Wednesday";
                case 4:
                    return "Thursday";
                default:
                    return "Unknown";
            }
        }
    }
}
