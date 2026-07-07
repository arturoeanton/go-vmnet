using System;

namespace Vmnet.Fixtures
{
    // Fase 3.79 golden fixture: regresses evalConv's handling of the
    // plain `conv.u8` opcode (internal/interpreter/arithmetic.go) —
    // widening a value narrower than 64 bits to an unsigned 64-bit one
    // must zero-extend the source's own bit pattern, not sign-extend its
    // signed numeric value through the shared int64-widening path every
    // OTHER conv opcode here correctly uses. Found running real Jint
    // 3.1.3: String.prototype.split's own internal
    // SplitWithStringSeparator does `(uint)Math.Min(segmentCount,
    // (ulong)limit)`, where `limit` is `uint.MaxValue` (bit pattern
    // 0xFFFFFFFF, vmnet's own KindI4 representation -1) whenever split()
    // is called with no explicit limit argument (the overwhelmingly
    // common case) — sign-extending that to int64 produced -1 instead of
    // the correct 4294967295, so Math.Min silently picked "-1" as the
    // smaller value, corrupting the resulting array's own length to -1.
    public static class ConvU8Test
    {
        public static long MinOfCountAndMaxUintWidened(long segmentCount)
        {
            uint limit = uint.MaxValue;
            return (long)Math.Min((ulong)segmentCount, (ulong)limit);
        }

        public static ulong WidenMaxUint()
        {
            uint value = uint.MaxValue;
            return value;
        }
    }
}
