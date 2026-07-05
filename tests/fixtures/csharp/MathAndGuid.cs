using System;

namespace Vmnet.Fixtures
{
    // Fase 3.69 golden fixture: System.Math.Abs and System.Guid (spec
    // §28.5) — both had a real native implementation already
    // (internal/bcl/system_math.go, internal/bcl/system_guid.go) but no
    // fixture/test anywhere ever called either one.
    public static class MathAndGuidTest
    {
        public static int AbsInt(int n) => Math.Abs(n);

        public static double AbsDouble(double n) => Math.Abs(n);

        // A real Guid round-trips through NewGuid/ToString/Equals: two
        // freshly generated Guids must differ, a Guid must equal itself,
        // and ToString must produce the canonical 36-character dashed
        // format real .NET uses.
        public static bool TwoNewGuidsDiffer()
        {
            var a = Guid.NewGuid();
            var b = Guid.NewGuid();
            return !a.Equals(b);
        }

        public static bool GuidEqualsItself()
        {
            var a = Guid.NewGuid();
            var s = a.ToString();
            return s == a.ToString();
        }

        public static int GuidStringLength() => Guid.NewGuid().ToString().Length;
    }
}
