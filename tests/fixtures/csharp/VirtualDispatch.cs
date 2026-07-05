namespace Vmnet.Fixtures
{
    // Fase 3.69 golden fixture: real `virtual`/`override` dispatch through
    // a base-typed reference — vmnet's own real corpus (FluentValidation,
    // AutoMapper, ...) exercises this constantly via `callvirt` against an
    // ancestor-walked target, but the shared fixture assembly had no first-
    // party regression test for it at all (spec §28.4 "virtual call").
    public class Beast
    {
        public virtual string Speak() => "...";

        // A second virtual method, called BY a non-virtual method on the
        // same class — proves dispatch happens through the real object's
        // own runtime type even when reached indirectly, not just at a
        // direct call site.
        public string Describe() => "A " + Kind() + " says " + Speak();
        public virtual string Kind() => "animal";
    }

    public class Wolf : Beast
    {
        public override string Speak() => "Woof";
        public override string Kind() => "dog";
    }

    public class Lion : Beast
    {
        public override string Speak() => "Meow";
        // Kind() deliberately NOT overridden: must fall back to Beast's
        // own implementation ("animal"), proving the ancestor walk finds
        // an inherited, non-overridden virtual method correctly too.
    }

    public static class VirtualDispatchTest
    {
        public static string SpeakThroughBaseRef(Beast a) => a.Speak();

        public static string DescribeThroughBaseRef(Beast a) => a.Describe();

        // Returns a joined string rather than a string[] — vmnet's public
        // Go API (value.go's fromRuntime) always represents a returned
        // array as a raw byte[] regardless of its real element type, a
        // separate, pre-existing, documented simplification unrelated to
        // dispatch itself.
        public static string AllSpeak()
        {
            Beast[] zoo = { new Wolf(), new Lion(), new Beast() };
            var joined = "";
            for (var i = 0; i < zoo.Length; i++)
            {
                if (i > 0) joined += ",";
                joined += zoo[i].Speak();
            }
            return joined;
        }
    }

    // Fase 3.69 golden fixture: box/unbox.any round-trip (spec §28.4
    // "boxing/unboxing") — a value type stored through an `object`-typed
    // local/parameter, then unboxed back to its own real value type.
    public static class BoxingTest
    {
        public static object BoxInt(int n) => n;

        public static int RoundTripInt(int n)
        {
            object boxed = n;
            return (int)boxed;
        }

        public static int RoundTripZero() => RoundTripInt(0);

        public static bool BoxedIntEqualsUnboxed(int n)
        {
            object boxed = n;
            return n == (int)boxed;
        }

        // A boxed value passed through a generic method constrained only
        // by `object` (System.Collections.Generic.List<object>), then
        // unboxed back — a shape real serialization/validation libraries
        // use constantly (see docs/en/ROADMAP.md, Fase 3.68's own
        // FluentValidation MessageFormatter finding, a narrower, still-
        // documented gap in this exact area for a boxed zero specifically
        // reaching a null-conditional check).
        public static int RoundTripThroughList(int n)
        {
            var list = new System.Collections.Generic.List<object> { n };
            return (int)list[0];
        }
    }
}
