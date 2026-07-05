namespace Vmnet.Fixtures
{
    public class GenericTypeOfTarget
    {
    }

    // Fase 3.60 golden fixture: typeof(T) on a generic method's own,
    // still-open type parameter — the exact shape real Microsoft.
    // Extensions.DependencyInjection.AddSingleton<TService,
    // TImplementation>() uses, previously always resolving to an empty
    // Type (TypeFullName was meaningless at IR-build time, and nothing
    // resolved it at runtime either — see internal/interpreter/eval.go's
    // own ir.LoadTypeToken handling and Frame.MethodGenericArgs's doc
    // comment).
    public static class GenericTypeOf
    {
        public static string NameOf<T>()
        {
            return typeof(T).FullName;
        }

        // Forwards this method's own still-open T into ANOTHER generic
        // call (NameOf<T>()) rather than using it directly — a MethodSpec
        // instantiated with "!!0" (the caller's own generic parameter),
        // not a closed type, which methodSpecGenericArgNames must resolve
        // as a sentinel and eval.go's ir.Call case must substitute at
        // runtime from the calling frame's own MethodGenericArgs (Fase
        // 3.60) — the exact shape ServiceDescriptor.Singleton<TService,
        // TImplementation>() uses internally.
        public static string ForwardedNameOf<T>()
        {
            return NameOf<T>();
        }

        public static string NameOfTargetCaller()
        {
            return NameOf<GenericTypeOfTarget>();
        }

        public static string ForwardedNameOfTargetCaller()
        {
            return ForwardedNameOf<GenericTypeOfTarget>();
        }
    }
}
