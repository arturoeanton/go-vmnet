namespace Vmnet.Fixtures
{
    // Fase 3.14 golden fixture: reflection-lite — ldtoken (typeof(T)),
    // Object.GetType, System.Type equality/Name/FullName.
    public class Vehicle { }
    public class Car : Vehicle { }

    public static class ReflectionTest
    {
        public static bool TypeofEqualsGetType()
        {
            var c = new Car();
            return c.GetType() == typeof(Car);
        }

        public static bool GetTypeDoesNotMatchBaseType()
        {
            var c = new Car();
            return c.GetType() == typeof(Vehicle);
        }

        public static string TypeName()
        {
            var c = new Car();
            return c.GetType().Name;
        }

        public static string TypeFullName()
        {
            return typeof(Car).FullName;
        }

        public static bool TypeNotEquals()
        {
            return typeof(Car) != typeof(Vehicle);
        }
    }
}
