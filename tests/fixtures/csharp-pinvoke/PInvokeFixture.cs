using System.Runtime.InteropServices;

namespace Vmnet.Fixtures.PInvoke
{
    // Fase 3.69 golden fixture: a real P/Invoke declaration (spec §28.6
    // "P/Invoke detection"). Deliberately a SEPARATE assembly from the
    // main Vmnet.Fixtures.dll — a real ImplMap table entry is an
    // assembly-wide checker finding (internal/checker/analyzer.go's
    // Analyze), which would otherwise break
    // TestAnalyze_OwnAssemblyIsCompatible's own "only Unsupported.
    // FunctionPointerCall is expected to be flagged" invariant for the
    // main fixture.
    public static class NativeCalls
    {
        [DllImport("libc")]
        public static extern int getpid();
    }
}
