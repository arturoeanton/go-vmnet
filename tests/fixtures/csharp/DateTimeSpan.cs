using System;

namespace Vmnet.Fixtures
{
    // Fase 3.12 golden fixture: System.DateTime and Span<T>/ReadOnlySpan<T>.
    public static class DateTimeSpanTest
    {
        public static int YearMonthDay()
        {
            var d = new DateTime(2024, 3, 15);
            return d.Year * 10000 + d.Month * 100 + d.Day;
        }

        public static int AddDaysAcrossMonth()
        {
            var d = new DateTime(2024, 1, 31);
            var next = d.AddDays(1);
            return next.Year * 10000 + next.Month * 100 + next.Day;
        }

        public static int CompareDates()
        {
            var a = new DateTime(2024, 1, 1);
            var b = new DateTime(2024, 6, 1);
            return a.CompareTo(b);
        }

        public static int SpanSum()
        {
            var xs = new int[5];
            xs[0] = 10;
            xs[1] = 20;
            xs[2] = 30;
            xs[3] = 40;
            xs[4] = 50;
            Span<int> span = xs.AsSpan();
            var slice = span.Slice(1, 3);
            var total = 0;
            for (var i = 0; i < slice.Length; i++)
            {
                total += slice[i];
            }
            return total;
        }

        public static string ReadOnlySpanSubstring()
        {
            string s = "Hello, World!";
            ReadOnlySpan<char> span = s.AsSpan();
            var slice = span.Slice(7, 5);
            return slice.ToString();
        }

        public static int SpanWriteThrough()
        {
            var xs = new int[3];
            Span<int> span = xs.AsSpan();
            span[0] = 100;
            span[1] = 200;
            span[2] = 300;
            return xs[0] + xs[1] + xs[2];
        }
    }
}
