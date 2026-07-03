using System;

namespace Vmnet.Fixtures
{
    // Fase 3.26 golden fixture: System.Enum.GetValues/GetNames/IsDefined/
    // ToObject, backed by a new metadata Constant-table reader
    // (internal/metadata/constant.go). Reuses Reflection2.cs's
    // TrafficLight enum — a plugin-declared enum, since vmnet has no
    // member database for BCL-only enums like System.DayOfWeek.
    public static class Reflection3
    {
        public static string EnumGetValuesTest()
        {
            var result = "";
            foreach (TrafficLight t in Enum.GetValues(typeof(TrafficLight)))
            {
                result += (int)t;
            }
            return result;
        }

        public static string EnumGetNamesTest()
        {
            var result = "";
            foreach (string n in Enum.GetNames(typeof(TrafficLight)))
            {
                result += n + ";";
            }
            return result;
        }

        public static bool EnumIsDefinedByValueTest()
        {
            return Enum.IsDefined(typeof(TrafficLight), 1);
        }

        public static bool EnumIsDefinedByValueFalseTest()
        {
            return Enum.IsDefined(typeof(TrafficLight), 99);
        }

        public static bool EnumIsDefinedByNameTest()
        {
            return Enum.IsDefined(typeof(TrafficLight), "Green");
        }

        public static int EnumToObjectTest()
        {
            var obj = Enum.ToObject(typeof(TrafficLight), 2);
            return (int)obj;
        }
    }
}
