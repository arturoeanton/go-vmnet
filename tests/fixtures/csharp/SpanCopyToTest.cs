using System;

namespace Vmnet.Fixtures
{
    // Fase 3.79 golden fixture: regresses Span<T>/ReadOnlySpan<T>.CopyTo/
    // TryCopyTo (internal/bcl/system_span.go) — natively registered
    // because the real interpreted body (System.SpanHelpers.CopyTo<T>)
    // does real unsafe pointer-address arithmetic (Unsafe.ByteOffset) to
    // detect an overlapping copy, plus a raw `sizeof` on the still-open
    // generic type parameter T just to reach that logic at all — a CIL
    // opcode vmnet never implemented. Found running real Jint 3.1.3: its
    // own internally vendored System.Text.ValueStringBuilder (used by
    // Array.prototype.join/.concat()/.map()/JSON.stringify's own number
    // formatting) failed with "unsupported opcode sizeof" the moment it
    // needed to grow past its initial inline buffer — i.e. almost
    // immediately for any of those.
    public static class SpanCopyToTest
    {
        public static string Run()
        {
            char[] src = new char[] { 'h', 'i', '!', 'x', 'x' };
            char[] dstBuf = new char[5];
            Span<char> srcSpan = new Span<char>(src, 0, 3);
            Span<char> dstSpan = new Span<char>(dstBuf);
            srcSpan.CopyTo(dstSpan);
            return new string(dstBuf, 0, 3);
        }

        public static bool TryRunTooShort()
        {
            char[] src = new char[] { 'a', 'b', 'c' };
            char[] dstBuf = new char[1];
            Span<char> srcSpan = new Span<char>(src);
            Span<char> dstSpan = new Span<char>(dstBuf);
            return srcSpan.TryCopyTo(dstSpan);
        }

        public static bool TryRunFits()
        {
            char[] src = new char[] { 'a', 'b' };
            char[] dstBuf = new char[4];
            Span<char> srcSpan = new Span<char>(src);
            Span<char> dstSpan = new Span<char>(dstBuf);
            return srcSpan.TryCopyTo(dstSpan);
        }
    }
}
