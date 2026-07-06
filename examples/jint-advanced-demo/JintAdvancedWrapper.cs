using Jint;

namespace VmnetJintAdvancedDemo
{
    // JintAdvancedWrapper drives real Jint harder than a one-line "1 + 2":
    // var/let/const (including a single statement declaring three
    // variables at once), nested object/array literals with property and
    // index access, arithmetic/comparison/ternary/logical operators,
    // Math.* built-ins, real structured data passed in from the Go host,
    // a heavier computational loop, function declarations/closures/
    // recursion/arrow functions, array growth (push/sort/slice/reverse/
    // filter/reduce), and string methods (toUpperCase/trim/charAt/
    // indexOf) — all real, unmodified Jint 3.1.3 IL running inside
    // vmnet, no CGo, no dotnet runtime anywhere this Go binary actually
    // runs.
    //
    // What this still deliberately does NOT use, and why: building this
    // file's first drafts found three whole classes of real,
    // otherwise-idiomatic JavaScript that didn't work under this Jint
    // version running inside vmnet at all — Fase 3.77 root-caused and
    // fixed the two that blocked the most (function-object construction,
    // and an overload-resolution heuristic that misrouted several
    // internal Jint calls whenever a same-arity sibling method happened
    // to collapse to the same coarse runtime shape — see docs/en/
    // ROADMAP.md's Fase 3.77 entry for the full account, including the
    // `unbox` CIL opcode it also newly implements). One real gap remains,
    // narrower than first thought but still open: `.concat`/`.map` and
    // `JSON.stringify` on anything but a single-digit number both need a
    // multi-character `Span&lt;T&gt;` write, which needs the real `sizeof`
    // CIL opcode on an open generic type parameter — a per-instantiation
    // memory layout vmnet's type-erased Value model has no way to answer
    // at all today. `RunFunctionsArraysAndStrings` below sticks to the
    // methods that don't need it (`.filter`/`.reduce`/single-char
    // `.join(',')`, ...).
    //
    // Six other, narrower bugs found along the way ARE fixed
    // (String/RuntimeHelpers.GetHashCode, Char.IsSurrogate/
    // IsSurrogatePair, Nullable&lt;T&gt; defaulting to the wrong runtime
    // shape for a plugin-defined generic value type, two missing Array
    // natives, and List&lt;T&gt;.Capacity) — see the same ROADMAP file's
    // earlier Fase entries.
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

        // RunFunctionsArraysAndStrings exercises exactly the features
        // this file's own top comment used to list as "deliberately not
        // used, because they don't work yet" — named function
        // declarations, closures, recursion, and arrow functions; array
        // growth (push/sort/slice/reverse/filter/reduce); and string
        // methods (toUpperCase/trim/charAt/indexOf) — all fixed by three
        // real root-caused bugs in assembly.go's overload-resolution
        // heuristic and the `unbox` CIL opcode (see docs/en/ROADMAP.md's
        // Fase 3.77 entry for the full account).
        //
        // Two things this deliberately still avoids, and why: `.concat`/
        // `.map` and `JSON.stringify` on anything but a single-digit
        // number both still hit a genuinely different, unfixed gap — a
        // multi-character `Span<T>` write (a multi-char `.join` separator,
        // a two-digit number's own decimal formatting, ...) needs the real
        // `sizeof` CIL opcode on an open generic type parameter, which
        // vmnet's type-erased Value model has no per-instantiation memory
        // layout to answer at all. `.filter`/`.reduce`/single-char
        // `.join(',')` all take a different, unaffected code path — this
        // demo sticks to those.
        public static string RunFunctionsArraysAndStrings()
        {
            var engine = new Engine();
            var script = @"
                // Named function declaration, plain recursion.
                function fib(n) { return n < 2 ? n : fib(n - 1) + fib(n - 2); }

                // Closure: makeCounter's own `count` local outlives the
                // call that created it, captured by the function it
                // returns.
                function makeCounter() {
                    var count = 0;
                    return function () { count += 1; return count; };
                }
                var counter = makeCounter();
                counter();
                counter();

                // Arrow function.
                var doubleIt = (x) => x * 2;

                // Array growth: push onto a literal, sort in place, slice
                // a copy, reverse a copy, filter, reduce.
                var nums = [5, 3, 8, 1];
                nums.push(9);
                nums.sort();
                var sliced = nums.slice(1, 3);
                var reversed = nums.slice().reverse();
                var big = nums.filter(function (x) { return x > 3; });
                var total = nums.reduce(function (a, b) { return a + b; }, 0);

                // String methods.
                var text = 'Hello World';
                var upper = text.toUpperCase();
                var trimmed = '  padded  '.trim();
                var firstLetter = text.charAt(0);
                var whereWorld = text.indexOf('World');

                fib(10) + ',' + counter() + ',' + doubleIt(21) + ',' +
                    nums.join(',') + ',' + sliced.join(',') + ',' + reversed.join(',') + ',' +
                    big.join(',') + ',' + total + ',' +
                    upper + ',' + trimmed + ',' + firstLetter + ',' + whereWorld;
            ";
            var result = engine.Evaluate(script);
            return result.AsString();
        }
    }
}
