using System.Text.RegularExpressions;

namespace Vmnet.Fixtures
{
    // Fase 3.20 golden fixture: System.Text.RegularExpressions, compiled
    // against Go's RE2 engine (no backreferences/lookaround — a
    // documented dialect difference, not a silent wrong answer).
    public static class RegexTest
    {
        public static bool IsMatchTest(string s)
        {
            return Regex.IsMatch(s, @"^\d+$");
        }

        // Confirmed against real IL: .Success/.Value on a Match instance
        // compile to callvirt Group::get_Success / Capture::get_Value —
        // Match (Capture -> Group -> Match) inherits both without
        // overriding either, it never declares its own get_Success/
        // get_Value.
        public static string MatchGroupTest(string s)
        {
            var m = Regex.Match(s, @"(\w+)@(\w+)\.com");
            if (!m.Success)
            {
                return "no-match";
            }
            return m.Groups[1].Value + "|" + m.Groups[2].Value;
        }

        public static string InstanceRegexTest(string s)
        {
            var re = new Regex(@"\d+");
            var m = re.Match(s);
            return m.Success ? m.Value : "no-match";
        }
    }
}
