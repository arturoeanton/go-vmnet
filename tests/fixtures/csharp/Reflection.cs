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

        // Fase 3.16: Type.IsAssignableFrom, deferred out of Fase 3.14 as
        // needing Machine access (walking the real class hierarchy needs
        // Machine.ResolveType, unavailable to a plain bcl.Native).
        public static bool VehicleAssignableFromCar()
        {
            return typeof(Vehicle).IsAssignableFrom(typeof(Car));
        }

        public static bool CarNotAssignableFromVehicle()
        {
            return typeof(Car).IsAssignableFrom(typeof(Vehicle));
        }
    }
}
