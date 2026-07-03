using System;
using System.Collections.Generic;

namespace Vmnet.Fixtures
{
    // Fase 3.25 golden fixture: System.Type introspection basics — the
    // first slice of "deep reflection" (Type.IsGenericType/GetGenericType
    // Definition/GetGenericArguments/MakeGenericType, Nullable.
    // GetUnderlyingType, Type.IsValueType/IsEnum/IsInterface/BaseType/
    // GetInterfaces/GetType(string), Type.Assembly). Reuses TypeChecks.cs's
    // Animal/Dog/IShape hierarchy and Structs.cs's Point so BaseType/
    // GetInterfaces exercise a real plugin TypeDef, not just BCL names.
    public enum TrafficLight
    {
        Red,
        Yellow,
        Green
    }

    public static class Reflection2
    {
        public static bool ListIntIsGenericTest()
        {
            return typeof(List<int>).IsGenericType;
        }

        public static string ListIntGenericDefTest()
        {
            return typeof(List<int>).GetGenericTypeDefinition().FullName;
        }

        public static string ListIntGenericArgsTest()
        {
            var args = typeof(Dictionary<string, int>).GetGenericArguments();
            return args[0].FullName + "|" + args[1].FullName;
        }

        public static string MakeGenericListStringTest()
        {
            Type open = typeof(List<>);
            Type closed = open.MakeGenericType(typeof(string));
            return closed.FullName;
        }

        public static string NullableUnderlyingTest()
        {
            Type t = typeof(int?);
            var under = Nullable.GetUnderlyingType(t);
            return under.FullName;
        }

        public static bool NullableUnderlyingOfNonNullableTest()
        {
            var under = Nullable.GetUnderlyingType(typeof(int));
            return under == null;
        }

        public static bool IntIsValueTypeTest()
        {
            return typeof(int).IsValueType;
        }

        public static bool PointIsValueTypeTest()
        {
            return typeof(Point).IsValueType;
        }

        public static bool DogIsValueTypeTest()
        {
            return typeof(Dog).IsValueType;
        }

        public static bool TrafficLightIsEnumTest()
        {
            return typeof(TrafficLight).IsEnum;
        }

        public static bool DogIsEnumTest()
        {
            return typeof(Dog).IsEnum;
        }

        public static bool IDisposableIsInterfaceTest()
        {
            return typeof(IDisposable).IsInterface;
        }

        public static bool IShapeIsInterfaceTest()
        {
            return typeof(IShape).IsInterface;
        }

        public static bool DogIsInterfaceTest()
        {
            return typeof(Dog).IsInterface;
        }

        public static string DogBaseTypeTest()
        {
            return typeof(Dog).BaseType.FullName;
        }

        public static string PointBaseTypeTest()
        {
            return typeof(Point).BaseType.FullName;
        }

        public static string TrafficLightBaseTypeTest()
        {
            return typeof(TrafficLight).BaseType.FullName;
        }

        public static bool IShapeBaseTypeIsNullTest()
        {
            return typeof(IShape).BaseType == null;
        }

        public static string DogInterfacesTest()
        {
            var ifaces = typeof(Dog).GetInterfaces();
            var result = "";
            foreach (Type t in ifaces)
            {
                result += t.FullName + ";";
            }
            return result;
        }

        public static bool GetTypePluginTest()
        {
            Type t = Type.GetType("Vmnet.Fixtures.Animal");
            return t != null && t.FullName == "Vmnet.Fixtures.Animal";
        }

        public static bool GetTypeBclValueTypeTest()
        {
            Type t = Type.GetType("System.TimeSpan");
            return t != null && t.FullName == "System.TimeSpan";
        }

        public static bool GetTypeUnknownIsNullTest()
        {
            Type t = Type.GetType("Totally.Unknown.Type");
            return t == null;
        }

        public static bool AssemblyToStringNotEmptyTest()
        {
            var s = typeof(string).Assembly.ToString();
            return s != null && s.Length > 0;
        }
    }
}
