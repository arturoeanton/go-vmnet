namespace Vmnet.Fixtures
{
    // Fase 3.77 golden fixture #1: reproduces the exact overload-
    // resolution bug found running real Jint (Function.SetFunctionName's
    // own `this.UnwrapJsValue(_nameDescriptor)` call, ObjectInstance.cs)
    // — a same-named INSTANCE method (1 real parameter) and STATIC
    // method (2 real parameters) declared on the same type, where the
    // instance call's own total argument count (receiver + 1) happens to
    // equal the static method's own parameter count (2). Without the
    // arity-aware guard in assembly.go's pickMethodOverload
    // (`len(sig.Params) == len(paramTypeNames)`), the call site's own
    // positional paramTypeNames — captured against the real, 1-param
    // instance target — got reused blindly against the static
    // candidate's own, unrelated 2-param signature: both candidates'
    // first parameter happened to share the exact declared type name, so
    // the static candidate won the same "+1000 exact call-site match"
    // bonus the real instance target earned, and its own second
    // parameter's small positive score then let it out-score the real
    // match by a couple of points.
    public class DifferentArityOverload
    {
        public string Tag;

        public DifferentArityOverload(string tag)
        {
            Tag = tag;
        }

        // The real target: an instance method, one explicit parameter.
        public string Unwrap(DifferentArityOverload other)
        {
            return "instance:" + Tag + ":" + other.Tag;
        }

        // An unrelated static method, same name, a DIFFERENT arity (two
        // explicit parameters) whose own first parameter's declared type
        // happens to match the instance overload's own first parameter
        // exactly — the coincidence the bug actually needs.
        public static string Unwrap(DifferentArityOverload other, DifferentArityOverload self)
        {
            return "static:" + other.Tag + ":" + self.Tag;
        }

        // Calls the instance overload the same way real C# compiles
        // `this.UnwrapJsValue(_nameDescriptor)` — a plain instance call,
        // unambiguous at compile time, but resolved by vmnet through the
        // same name-based machinery a callvirt's own ancestor walk uses.
        public string CallInstanceUnwrap(DifferentArityOverload other)
        {
            return Unwrap(other);
        }
    }

    // Fase 3.77 golden fixture #2: reproduces the missing "a reference
    // argument can never legitimately bind a numeric primitive
    // parameter" hard-shape check (assembly.go's hasHardShapeMismatch)
    // — found running real Jint: ArrayInstance's own `internal JsValue
    // Get(uint index)` coincidentally has the same arity as the call
    // site's real target, `ObjectInstance.Get(JsValue property)`
    // (inherited, not even among ArrayInstance's own
    // FindMethodDefCandidates results) — with no mismatch check catching
    // a JsValue argument (a real reference) being handed to a uint-typed
    // parameter, `Get(uint index)` "matched" with a low but positive
    // score instead of being rejected, and Machine.call's own ancestor
    // walk (calls.go) stopped there instead of continuing up to the real
    // virtual method.
    public class PrimitiveOverloadBase
    {
        public string Tag;

        public PrimitiveOverloadBase(string tag)
        {
            Tag = tag;
        }

        public virtual string Access(PrimitiveOverloadBase key)
        {
            return "base:" + Tag + ":" + key.Tag;
        }

        // `this` is statically typed as PrimitiveOverloadBase here, but
        // the concrete receiver at runtime may be
        // PrimitiveOverloadDerived — Access is virtual, so real C#
        // always emits `callvirt` for this call, forcing the exact
        // ancestor-walk-by-name resolution the bug needs to reproduce.
        public string CallAccessPolymorphically(PrimitiveOverloadBase key)
        {
            return Access(key);
        }
    }

    public class PrimitiveOverloadDerived : PrimitiveOverloadBase
    {
        public PrimitiveOverloadDerived(string tag)
            : base(tag)
        {
        }

        // Does NOT override Access(PrimitiveOverloadBase) — a totally
        // unrelated, same-named, same-arity method taking a primitive
        // instead. Without the fix, a PrimitiveOverloadBase argument
        // (a real reference) silently "matched" this int parameter.
        public int Access(int index)
        {
            return index * 2;
        }
    }

    // Fase 3.77 golden fixture #3: reproduces the "no candidate's arity
    // matched, fall back to rids[0] anyway" bug in pickMethodOverload's
    // own last-resort fallback — found the same way as fixture #2 above,
    // but needing TWO unrelated candidates on the concrete type (so
    // resolution goes through the multi-candidate scoring loop, not the
    // single-candidate fast path candidateMatchesArgs already guarded
    // correctly): one wrong arity, one right arity but a confirmed hard
    // shape mismatch. Before the fix, hasHardShapeMismatch correctly
    // rejected both, but the "no scored candidate won" fallback then
    // returned rids[0] (whichever one happens to sort first) anyway,
    // regardless of that rejection — so Machine.call's own ancestor walk
    // never got the "not found here" signal it needed to keep looking up
    // to the real match.
    public class MultiCandidateBase
    {
        public string Tag;

        public MultiCandidateBase(string tag)
        {
            Tag = tag;
        }

        public virtual string Get(MultiCandidateBase key, MultiCandidateBase key2)
        {
            return "base2:" + Tag + ":" + key.Tag + ":" + key2.Tag;
        }

        public string CallGetPolymorphically(MultiCandidateBase key, MultiCandidateBase key2)
        {
            return Get(key, key2);
        }
    }

    public class MultiCandidateDerived : MultiCandidateBase
    {
        public MultiCandidateDerived(string tag)
            : base(tag)
        {
        }

        // Wrong arity: one parameter, not two.
        public int Get(int index)
        {
            return index;
        }

        // Right arity, wrong shape: two parameters, but both primitive —
        // never a real match for two reference arguments.
        public int Get(int a, int b)
        {
            return a + b;
        }
    }

    public static class OverloadTieBreak2
    {
        public static string DifferentArityTest()
        {
            var a = new DifferentArityOverload("a");
            var b = new DifferentArityOverload("b");
            return a.CallInstanceUnwrap(b);
        }

        public static string PrimitiveShapeMismatchTest()
        {
            var derived = new PrimitiveOverloadDerived("d");
            var other = new PrimitiveOverloadBase("o");
            return derived.CallAccessPolymorphically(other);
        }

        public static string ConfirmedWrongFallbackTest()
        {
            var derived = new MultiCandidateDerived("d");
            var k1 = new MultiCandidateBase("k1");
            var k2 = new MultiCandidateBase("k2");
            return derived.CallGetPolymorphically(k1, k2);
        }
    }
}
