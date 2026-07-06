namespace Vmnet.Fixtures
{
    // Fase 3.77 golden fixture: regresses the plain `unbox` opcode
    // (spec §III.4.32) — distinct from `unbox.any`, which was already
    // handled as a Nop (vmnet's runtime.Value already stores a boxed
    // value type in its final, unboxed KindStruct shape, so unbox.any
    // needs no representation change at all). Plain `unbox` instead
    // pushes a managed pointer to the value type's own data, used when a
    // field is read directly off a cast-to-struct expression without
    // needing a full copy: `((UnboxPayload)boxed).Value` compiles to
    // `unbox UnboxPayload; ldfld UnboxPayload::Value` — before this fix,
    // vmnet had no translation for plain `unbox` at all and failed with
    // an unsupported-opcode error.
    //
    // Found running real Jint: Engine.GetValue(object value, bool
    // returnReferenceToPool)'s own `((Completion)value).Value` — Completion
    // is a real Jint struct, and this exact field-off-a-boxed-struct
    // pattern is how its own Value field is read.
    public struct UnboxPayload
    {
        public int Value;
    }

    public static class UnboxOpcodeTest
    {
        public static int Run()
        {
            object boxed = new UnboxPayload { Value = 42 };
            return ((UnboxPayload)boxed).Value;
        }
    }
}
