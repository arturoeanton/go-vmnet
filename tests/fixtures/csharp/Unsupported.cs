namespace Vmnet.Fixtures
{
    // Fase 3 checker fixture: deliberately uses a feature vmnet doesn't
    // support yet, so the checker has a reproducible, offline "this
    // method is unsupported" case to test against, alongside the
    // real-world NuGet packages used for manual certification
    // (docs/ROADMAP.md). Repurposed three times now as vmnet's coverage
    // grew: System.Array (until Fase 3.5), plain try/finally (until Fase
    // 3.10), and exception filters/`catch (Foo) when (cond)` (until Fase
    // 3.51 — see docs/en/ROADMAP.md) all moved out once they became
    // supported. An indirect call through a C# 9+ function pointer
    // (`delegate*<...>`, compiling to a real `calli` instruction) is the
    // current gap — internal/ir/builder.go's opcode switch has no case
    // for `calli` at all, so it falls straight to the default
    // UnsupportedOpcodeError, the same "no native code generation, no
    // raw function-pointer indirection" boundary Reflection.Emit and
    // P/Invoke already sit outside of for this interpreter's architecture.
    public static unsafe class Unsupported
    {
        public static int FunctionPointerCall()
        {
            delegate*<int, int> fn = &Double;
            return fn(21);
        }

        static int Double(int x) => x * 2;
    }
}
