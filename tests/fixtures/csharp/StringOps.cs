using System.Text;

namespace Vmnet.Fixtures
{
    // Fase 3.6 golden fixture: the BCL natives added alongside `switch` —
    // StringBuilder, String.Format/Substring/get_Chars/Equals.
    public static class StringOps
    {
        public static string BuildGreeting(string name)
        {
            var sb = new StringBuilder();
            sb.Append("Hello, ").Append(name).Append("!");
            return sb.ToString();
        }

        public static string FormatReport(string label, int count, double ratio)
        {
            return string.Format("{0}: {1} items ({2:F1}%)", label, count, ratio * 100);
        }

        public static string FirstThree(string input)
        {
            return input.Substring(0, 3);
        }

        public static char FirstChar(string input)
        {
            return input[0];
        }

        public static bool SameText(string a, string b)
        {
            return a.Equals(b);
        }
    }
}
