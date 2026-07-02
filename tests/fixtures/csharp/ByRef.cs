namespace Vmnet.Fixtures
{
    // Fase 3.5 golden fixture: `out`/`ref` parameters — ldarga.s/ldloca.s
    // (address-of), the callee's stind.i4 writes through the pointer it
    // received as a plain argument, and the caller reading the local back
    // afterward. This was the single largest real-world blocker found
    // during Fase 3 certification (docs/ROADMAP.md).
    public static class ByRef
    {
        public static bool TryDouble(int input, out int result)
        {
            if (input < 0)
            {
                result = 0;
                return false;
            }
            result = input * 2;
            return true;
        }

        public static int CallTryDouble(int input)
        {
            int result;
            if (!TryDouble(input, out result))
                return -1;
            return result;
        }

        public static void Increment(ref int value)
        {
            value = value + 1;
        }

        public static int CallIncrementTwice(int start)
        {
            var x = start;
            Increment(ref x);
            Increment(ref x);
            return x;
        }
    }
}
