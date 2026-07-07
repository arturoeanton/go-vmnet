namespace Vmnet.Fixtures
{
    public interface IKeyed
    {
        string GetName();
    }

    public class KeyedThing : IKeyed
    {
        private string _name;

        public KeyedThing(string name)
        {
            _name = name;
        }

        public string GetName()
        {
            return _name;
        }
    }

    // Fase 3.79 golden fixture: regresses ir.Call's own auto-dereference
    // of a KindRef receiver (internal/interpreter/eval.go) — mirrors
    // Jint.AstExtensions.GetKey<T>(this T property, Engine engine) where
    // T : IProperty, a generic method constrained to an interface,
    // calling an interface method on its own still-open T-typed
    // parameter. Real IL for `((IKeyed)item).GetName()` inside a method
    // generic over `T : IKeyed` emits `constrained. !!T` + `callvirt
    // IKeyed::GetName` — for a REFERENCE-typed T (any class implementing
    // IKeyed), the receiver arrives on the stack as a managed pointer
    // (T&, from the compiler's own `ldarga` ahead of `constrained.`) that
    // the real, JIT-honored constrained. prefix resolves by simple
    // dereference. vmnet drops constrained. as a Nop (ir/builder.go) —
    // harmless for a value-typed T (already a KindRef to a KindStruct,
    // handled directly), but not for this case: without dereferencing
    // first, GetName() ran with its own "this" still a raw managed
    // pointer instead of the real KeyedThing object, crashing with
    // "dereferencing a null managed pointer" the moment its own body
    // tried a plain field read. Found running real Jint/Esprima's ES6
    // class support — any class with at least one member hit this exact
    // shape via Jint.AstExtensions.GetKey<T>.
    public static class ConstrainedGenericTest
    {
        public static string CallThroughConstrainedGeneric<T>(T item) where T : IKeyed
        {
            return item.GetName();
        }

        public static string Run()
        {
            return CallThroughConstrainedGeneric(new KeyedThing("Rex"));
        }
    }
}
