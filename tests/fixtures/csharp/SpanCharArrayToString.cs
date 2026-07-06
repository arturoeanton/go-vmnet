using System;

namespace Vmnet.Fixtures
{
    // Fase 3.77 golden fixture: regresses spanToStringValue's (internal/
    // bcl/system_span.go) handling of an array-backed Span<char> — found
    // running real Jint 3.1.3: its own internal, vendored
    // System.Text.ValueStringBuilder (used by Array.prototype.join, among
    // others) backs its `_chars` field with exactly this shape (a real
    // `char[]` wrapped in a Span<char>, not a slice of an existing
    // string), and its own ToString() is `_chars.Slice(0, _pos).ToString
    // ()`. Before the fix, spanToStringValue only handled string-backed
    // spans, so this always fell through to the generic "unhelpful CLR
    // default" fallback, which for an array-backed value prints the raw
    // backing array (vmnet's own "<array[N]>" debug placeholder) instead
    // of the real joined string.
    public static class SpanCharArrayToString
    {
        public static string Run()
        {
            char[] chars = new char[] { 'h', 'i', '!', 'x', 'x' };
            Span<char> span = new Span<char>(chars, 0, 3);
            return span.ToString();
        }
    }
}
