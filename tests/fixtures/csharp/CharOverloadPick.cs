namespace Vmnet.Fixtures
{
    // Fase 3.77 golden fixture: regresses assembly.go's paramTypeName
    // gaining SigChar/SigBoolean resolution — before this fix, a
    // candidate's own char- or bool-typed parameter never resolved to a
    // name at all (only SigClass/SigValueType/SigGenericInst did), so
    // the exact-match bonus that disambiguates same-arity overloads by
    // the call site's own resolved parameter type name could never fire
    // for this case, even though internal/ir/builder.go's
    // sigParamTypeNames already captured "System.Char"/"System.Boolean"
    // on the CALL SITE side. Every same-arity overload taking a
    // different primitive (char vs int vs uint) collapses to the exact
    // same runtime.KindI4 shape, so nothing else could tell them apart.
    //
    // Found running real Jint: JsString.Create has four same-arity
    // overloads (string, char, int, uint) — StringPrototype.CharAt's own
    // `JsString.Create(jsString[(int)num])` (a real `char` argument,
    // statically resolved to Create(char) by the compiler) kept losing
    // this tie to Create(int) (which converts the character's own
    // numeric code point to its decimal STRING form instead), so e.g.
    // 'abc'.charAt(1) returned "98" instead of "b".
    public static class CharOverloadPick
    {
        public static string MakeLabel(string value)
        {
            return "string:" + value;
        }

        public static string MakeLabel(char value)
        {
            return "char:" + value;
        }

        public static string MakeLabel(int value)
        {
            return "int:" + value;
        }

        public static string MakeLabel(uint value)
        {
            return "uint:" + value;
        }

        public static string Run()
        {
            string s = "abc";
            char c = s[1];
            return MakeLabel(c);
        }
    }
}
