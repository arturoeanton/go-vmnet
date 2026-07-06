namespace Vmnet.Fixtures
{
    // Fase 3.77 golden fixture: reproduces the reverse direction of the
    // fixture #2 bug in OverloadTieBreak2.cs — there, a reference argument
    // wrongly matched a primitive-typed sibling; here, a PRIMITIVE
    // argument (a bool) wrongly matches a same-arity sibling declaring a
    // RESOLVABLE CLASS parameter in its place. assembly.go's
    // hasHardShapeMismatch had no check catching "a raw numeric Kind
    // (never boxed — CIL always emits an explicit `box` before handing a
    // primitive to a reference-typed parameter) can never legitimately
    // bind a SigClass parameter" until this fix.
    //
    // Found running real Jint: Engine.GetValue(object value, bool
    // returnReferenceToPool) — the real target for `engine.GetValue(obj3,
    // returnReferenceToPool: false)`
    // (JintMemberExpression.EvaluateInternal) — lost this exact tie to
    // the unrelated public Engine.GetValue(JsValue scope, JsValue
    // property) overload: the first parameter's assignable-subtype bonus
    // (a JsString argument is assignable to JsValue) outscored the
    // correct candidate, and nothing at all rejected the second
    // parameter — a raw `false` (KindI4) argument silently "matched" a
    // `JsValue property` parameter it could never actually satisfy.
    public class ShapeMismatchKey
    {
        public string Tag;

        public ShapeMismatchKey(string tag)
        {
            Tag = tag;
        }
    }

    public class ShapeMismatchBase
    {
        public string Tag;

        public ShapeMismatchBase(string tag)
        {
            Tag = tag;
        }

        public virtual string Lookup(ShapeMismatchKey key, bool flag)
        {
            return "base:" + Tag + ":" + key.Tag + ":" + flag;
        }

        // `this` is statically typed as ShapeMismatchBase here, but the
        // concrete receiver at runtime may be ShapeMismatchDerived —
        // Lookup is virtual, so real C# always emits `callvirt` for this
        // call, forcing the exact ancestor-walk-by-name resolution the
        // bug needs to reproduce.
        public string CallLookupPolymorphically(ShapeMismatchKey key, bool flag)
        {
            return Lookup(key, flag);
        }
    }

    public class ShapeMismatchDerived : ShapeMismatchBase
    {
        public ShapeMismatchDerived(string tag)
            : base(tag)
        {
        }

        // Does NOT override Lookup(ShapeMismatchKey, bool) — a totally
        // unrelated, same-named, same-arity method taking TWO reference
        // parameters instead of (reference, bool). Without the fix, the
        // `flag` bool argument silently "matched" this second
        // ShapeMismatchKey parameter.
        public string Lookup(ShapeMismatchKey a, ShapeMismatchKey b)
        {
            return "derived:" + a.Tag + ":" + b.Tag;
        }
    }

    public static class PrimitiveClassShapeMismatchTest
    {
        public static string Run()
        {
            var derived = new ShapeMismatchDerived("d");
            var key = new ShapeMismatchKey("k");
            return derived.CallLookupPolymorphically(key, true);
        }
    }
}
