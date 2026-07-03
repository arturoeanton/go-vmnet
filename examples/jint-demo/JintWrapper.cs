using Jint;

namespace VmnetJintDemo
{
    public static class JintWrapper
    {
        public static string RunJs(string script)
        {
            var engine = new Engine();
            var result = engine.Evaluate(script);
            return result.ToString();
        }

        public static double AddNumbers(double a, double b)
        {
            var engine = new Engine();
            engine.SetValue("a", a);
            engine.SetValue("b", b);
            var result = engine.Evaluate("a + b");
            return result.AsNumber();
        }
    }
}
