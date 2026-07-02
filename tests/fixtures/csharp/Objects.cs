namespace Vmnet.Fixtures
{
    // Fase 2 golden fixture: newobj, instance fields, auto-properties (callvirt on the getters/setters).
    public class Customer
    {
        public string Name { get; set; }
        public int Age { get; set; }
    }
}
