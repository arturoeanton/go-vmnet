using Jint;

namespace VmnetJintAdvancedDemo
{
    // JintAdvancedWrapper drives real Jint harder than a one-line "1 + 2":
    // var/let/const (including a single statement declaring three
    // variables at once), nested object/array literals with property and
    // index access, arithmetic/comparison/ternary/logical operators,
    // Math.* built-ins, real structured data passed in from the Go host,
    // and a heavier computational loop — all real, unmodified Jint 3.1.3
    // IL running inside vmnet, no CGo, no dotnet runtime anywhere this
    // Go binary actually runs.
    //
    // What this deliberately does NOT use, and why: building this file's
    // own first drafts found that several whole classes of real,
    // otherwise-idiomatic JavaScript don't work yet under this Jint
    // version running inside vmnet — not narrow one-off gaps, but
    // consistent, reproducible walls hit from multiple different angles:
    //
    //   - Any JS `function` (named, anonymous, or arrow — even one never
    //     called) fails while Jint builds the function object itself,
    //     root-caused to a real field-aliasing bug in how vmnet
    //     constructs `Jint.Native.Function.ScriptFunction`.
    //   - Array growth (`.push`, `.slice`, `.sort`, `.concat`, `.reverse`
    //     on a copy) fails with "compare on mismatched value kinds",
    //     something in Jint's own internal array-storage growth path.
    //   - String methods (`.split`, `.trim`, `.charAt`, chained
    //     `.toUpperCase()`), template literals, and `JSON.stringify`
    //     all fail, two different ways — a JsValue field null-reference,
    //     and an unimplemented `sizeof` IL opcode inside
    //     `System.SpanHelpers.CopyTo` (a real, general gap: `sizeof` on
    //     an open generic type parameter needs a per-instantiation
    //     memory layout size vmnet's type-erased Value model doesn't
    //     track at all today).
    //
    // See docs/en/ROADMAP.md's own "found, not fixed" note for the full
    // account of all three, and this directory's own README for what
    // that means for real Jint usage today. Six other, narrower bugs
    // found along the way ARE fixed (String/RuntimeHelpers.GetHashCode,
    // Char.IsSurrogate/IsSurrogatePair, Nullable&lt;T&gt; defaulting to
    // the wrong runtime shape for a plugin-defined generic value type,
    // two missing Array natives, and List&lt;T&gt;.Capacity) — see the
    // same ROADMAP entry.
    public static class JintAdvancedWrapper
    {
        // RunSuite executes one script combining var/let/const (including
        // one statement declaring three variables at once — the exact
        // shape that found the Esprima ArrayList<Token>?/Array.Clear/
        // Array.Copy bugs below), nested object/array literals with
        // property and index access, arithmetic/ternary/logical
        // operators, and Math.* built-ins — then returns a single number
        // combining values pulled back out of the nested structure, so
        // only one plain value crosses the Go<->C# boundary.
        public static double RunSuite()
        {
            var engine = new Engine();
            var script = @"
                // var/let/const, including one statement declaring three
                // variables at once.
                var a = 1, b = 2, c = 3;
                let sum = a + b + c;
                const label = 'checkout';

                // A fixed array literal, indexed directly (not grown via
                // push/slice/sort/concat — see this file's own top
                // comment for why those specifically don't work yet).
                var nums = [5, 3, 8, 1, 9, 2, 7];
                var first = nums[0];
                var last = nums[nums.length - 1];
                var hasNine = nums[4] === 9;

                // Object literal, nested, with real string values (plain
                // string literals and `+` concatenation both work fine —
                // it's specifically string *methods* that don't yet).
                var order = {
                    label: label,
                    total: sum,
                    items: nums,
                    note: label + ' total ' + sum
                };

                // Math built-ins, plus comparison/ternary/logical
                // operators combining everything above.
                var geometry = {
                    hypotenuse: Math.sqrt(Math.pow(3, 2) + Math.pow(4, 2)),
                    rounded: Math.round(3.6),
                    biggest: Math.max(first, last),
                    smallest: Math.min(first, last)
                };
                var verdict = (order.total > 0 && hasNine) ? 1 : 0;

                order.total + geometry.hypotenuse + geometry.rounded +
                    geometry.biggest + geometry.smallest + verdict;
            ";
            var result = engine.Evaluate(script);
            return result.AsNumber();
        }

        // EvaluateWithData passes a real, host-supplied JSON document
        // into the engine — parsed as a plain JS expression
        // (`(` + jsonData + `)`, valid because JSON's own object/array
        // literal syntax is a subset of JS expression syntax, so this
        // needs no `JSON.parse` call at all) — then evaluates a second
        // expression that reaches into its nested structure. Proves
        // richer, structured data (not just a single number or string)
        // crosses the Go->Jint boundary and is directly usable as real
        // JS on the other side.
        public static double EvaluateWithData(string jsonData, string expression)
        {
            var engine = new Engine();
            var data = engine.Evaluate("(" + jsonData + ")");
            engine.SetValue("data", data);
            var result = engine.Evaluate(expression);
            return result.AsNumber();
        }

        // Loop runs a real, if modest, computational script (a
        // for-loop of `iterations` steps, each doing real arithmetic
        // through a `let` binding) — "intensive use" in the literal
        // sense, proving vmnet's own instruction/call-depth sandbox
        // doesn't choke on ordinary, heavier JavaScript.
        public static double Loop(int iterations)
        {
            var engine = new Engine();
            engine.SetValue("N", iterations);
            var result = engine.Evaluate(@"
                let sum = 0;
                for (let i = 0; i < N; i++) {
                    sum += i * i;
                }
                sum;
            ");
            return result.AsNumber();
        }
    }
}
