namespace Vmnet.Fixtures
{
    public enum FakeLifetime
    {
        A = 0,
        B = 1,
    }

    // Fase 3.60 golden fixture: reproduces the exact overload-resolution
    // tie-break bug found running real Microsoft.Extensions.
    // DependencyInjection.Abstractions — its own ServiceDescriptor has a
    // private (Type serviceType, object serviceKey, ServiceLifetime
    // lifetime) constructor alongside a public (Type serviceType, Type
    // implementationType, ServiceLifetime lifetime) one (among others); a
    // null 2nd argument facing the (string, OverloadTieBreakKey,
    // FakeLifetime) candidate below scored higher than the correct
    // (string, object, FakeLifetime) one (assembly.go's
    // pickMethodOverload: runtime.KindNull's own scoreParamMatch
    // deliberately favors a concrete SigClass over SigObject, a fine
    // tie-break with no other signal, but wrong here since the call
    // site's OWN declared 2nd parameter type (object, unresolvable to a
    // name via paramTypeName — see its own doc comment) structurally
    // matches ONLY the (string, object, FakeLifetime) candidate, whose
    // own 2nd parameter is ALSO unresolvable-to-a-name the same way).
    //
    // OverloadTieBreakKey has to be a real, resolvable reference type
    // (a plain class, standing in for ServiceDescriptor's own real Type
    // parameter) — a primitive like System.String would NOT reproduce
    // the bug: paramTypeName never resolves System.String either (it's
    // SigString, not SigClass), so it wouldn't score any differently
    // from System.Object and the tie-break the fix targets wouldn't
    // actually trigger.
    public class OverloadTieBreakKey
    {
    }

    public class OverloadTieBreak
    {
        public string Tag;

        // The correct target: middle parameter is a bare `object`.
        private OverloadTieBreak(string name, object key, FakeLifetime mode)
        {
            Tag = "core:" + name + ":" + mode;
        }

        // A different 3-arg overload sharing param0/param2's exact shape,
        // but declaring param1 as a concrete, resolvable class — the
        // wrong tie-break winner before the Fase 3.60 fix.
        private OverloadTieBreak(string name, OverloadTieBreakKey key, FakeLifetime mode)
        {
            Tag = "wrong";
        }

        // The real entry point: its own `this(...)` call site statically
        // targets the (string, object, FakeLifetime) overload above (the
        // explicit (object) cast removes any compile-time ambiguity, same
        // as real ServiceDescriptor's own source does implicitly via its
        // parameter's actual declared type).
        public OverloadTieBreak(string name)
            : this(name, (object)null, FakeLifetime.A)
        {
        }

        public string GetTag()
        {
            return Tag;
        }
    }
}
