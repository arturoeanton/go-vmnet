using System;
using System.Collections.Generic;
using System.Threading;

namespace Vmnet.Fixtures
{
    // Fase 3.18 golden fixture: a second cheap-win BCL bundle, same
    // "measure, then bundle" pattern as Fase 3.6/3.13.
    public static class CheapWins2
    {
        public static bool ContainsTest(string s)
        {
            return s.Contains("lo");
        }

        public static string NewLine()
        {
            return Environment.NewLine;
        }

        public static int ConvertToInt32Test()
        {
            return Convert.ToInt32("42");
        }

        public static string DoubleToStringTest(double d)
        {
            return d.ToString();
        }

        public static string StringFromChars()
        {
            char[] chars = new char[3];
            chars[0] = 'a';
            chars[1] = 'b';
            chars[2] = 'c';
            return new string(chars);
        }

        public static int ListRemoveAtInsert()
        {
            var xs = new List<int>();
            xs.Add(1);
            xs.Add(2);
            xs.Add(3);
            xs.Add(4);
            xs.RemoveAt(1);
            xs.Insert(0, 99);
            var sum = 0;
            foreach (var x in xs)
            {
                sum += x;
            }
            return sum;
        }

        public static int DictClear()
        {
            var d = new Dictionary<string, int>();
            d.Add("a", 1);
            d.Clear();
            return d.Count;
        }

        public static string FormatExceptionTest()
        {
            try
            {
                throw new FormatException("bad format");
            }
            catch (FormatException e)
            {
                return e.Message;
            }
        }

        public static int InterlockedTest()
        {
            var value = 5;
            Interlocked.CompareExchange(ref value, 10, 5);
            return value;
        }

        public static bool StringComparerOrdinalTest()
        {
            return StringComparer.Ordinal.Equals("abc", "abc");
        }
    }
}
