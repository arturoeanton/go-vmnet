namespace Vmnet.Fixtures
{
    // Fase 3.5 golden fixture: static fields (ldsfld/stsfld) and a static
    // constructor (.cctor), lazily run on first static access — see
    // internal/interpreter/statics.go.
    public static class Statics
    {
        public static int Counter;
        public static int InitValue;

        static Statics()
        {
            InitValue = 42;
        }

        public static int IncrementAndGet()
        {
            Counter = Counter + 1;
            return Counter;
        }

        public static int GetInitValue()
        {
            return InitValue;
        }
    }
}
