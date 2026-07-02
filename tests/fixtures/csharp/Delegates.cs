using System;

namespace Vmnet.Fixtures
{
    // Fase 3.9 golden fixture: delegates (ldftn/newobj/Invoke) — a method
    // group conversion (static target, no receiver) and two closures
    // (capturing a parameter, and capturing+mutating a local — both lower
    // to a compiler-generated class with real fields, which vmnet's
    // existing object model already handles with no special support).
    // A local `delegate` declaration — real TypeDef extending
    // System.MulticastDelegate, resolved via isDelegateType's TypeDef
    // path rather than the well-known-BCL-prefix one.
    public delegate int IntTransform(int x);

    public static class Delegates
    {
        private static int Double(int x)
        {
            return x * 2;
        }

        public static int InvokeLocalDelegateType(int x)
        {
            IntTransform t = Double;
            return t(x);
        }

        public static int InvokeStaticFunc(int x)
        {
            Func<int, int> f = Double;
            return f(x);
        }

        public static int InvokeClosure(int factor, int x)
        {
            Func<int, int> f = n => n * factor;
            return f(x);
        }

        public static int InvokeMutatingClosure(int x)
        {
            int result = 0;
            Action<int> a = n => { result = n + 1; };
            a(x);
            return result;
        }
    }
}
