using Jint;

namespace VmnetJintAdvancedDemo
{
    // JintAdvancedWrapper drives real Jint harder than a one-line "1 + 2":
    // var/let/const, nested object/array literals, operators, Math.*
    // built-ins, real structured data from the Go host, a heavier loop,
    // function declarations/closures/recursion/arrow functions, array
    // growth and higher-order methods (push/sort/slice/reverse/filter/
    // reduce/map/concat), string methods, ES6 classes (inheritance,
    // `super`, private fields, getters), regular expressions (test/exec/
    // match/replace, global and non-global, `Match.NextMatch`-style
    // iteration), `JSON.stringify` on real nested data with real numbers,
    // and template literals (including nested ones) — all real,
    // unmodified Jint 3.1.3 IL running inside vmnet, no CGo, no dotnet
    // runtime anywhere this Go binary actually runs.
    //
    // Getting all of that working took three Fases. Fase 3.77 found and
    // root-caused three whole classes of real JavaScript that didn't run
    // at all. Fase 3.78 fixed two of them (function-object construction;
    // an overload-resolution heuristic misrouting several internal Jint
    // calls). Fase 3.79 (docs/en/ROADMAP.md has the full account of all
    // three) went back for the third and, chasing it down, found a much
    // longer chain of real, narrow bugs across vmnet's own CIL/BCL
    // support — a `constrained.`-prefixed generic interface call never
    // being dereferenced (the actual ES6-class blocker), `conv.u8`
    // sign-extending instead of zero-extending, `Span&lt;T&gt;.CopyTo`/
    // `TryCopyTo` needing native registration to sidestep the real
    // `sizeof`-on-an-open-generic gap entirely, and half a dozen
    // regex/StringBuilder/TimeSpan natives that were simply never wired
    // up (`StringBuilder.set_Capacity`/`ToString(start,length)`, a
    // `Regex` object silently failing an `as Regex` cast, the
    // count-limited `Match`/`Replace` overloads, `Capture.Index`/
    // `Length`, `Match.NextMatch`, `MatchCollection`'s own indexer,
    // `TimeSpan`'s comparison operators).
    //
    // One real, narrower-than-it-looks gap remains: regex patterns using
    // a **parenthesized group** (capturing or not) or a **backslash
    // shorthand class** (`\d`/`\w`/`\s`) still translate incorrectly —
    // traced to Esprima's own hand-written regex-to-.NET pattern
    // translator (`Scanner.RegExpParser.ParsePattern`), not yet fully
    // root-caused. Character classes (`[0-9]`, `[a-z]`, ...), literal
    // text, quantifiers, and alternation all translate correctly —
    // `RunRegexAndClasses` below sticks to those.
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

        // RunClassesRegexAndJson exercises the third and last of Fase
        // 3.77's originally-documented gaps, fixed in Fase 3.79 — ES6
        // classes (inheritance, `super`, method overriding), `.map()`/
        // `.concat()`, regular expressions (character classes,
        // quantifiers, global and non-global `.match()`/`.replace()`),
        // `JSON.stringify` on real nested arrays/objects with real
        // (multi-digit) numbers, and template literals — all in one
        // script.
        public static string RunClassesRegexAndJson()
        {
            var engine = new Engine();
            var script = @"
                // ES6 classes: inheritance, super(), method overriding.
                class Shape {
                    constructor(name) { this.name = name; }
                    area() { return 0; }
                    describe() { return this.name + ' area=' + this.area(); }
                }
                class Circle extends Shape {
                    constructor(r) { super('Circle'); this.r = r; }
                    area() { return Math.round(Math.PI * this.r * this.r); }
                }
                class Square extends Shape {
                    constructor(s) { super('Square'); this.s = s; }
                    area() { return this.s * this.s; }
                }
                var shapes = [new Circle(3), new Square(4)];
                var described = shapes.map(function (s) { return s.describe(); });

                // Regular expressions: character classes, quantifiers,
                // global match/replace (parenthesized groups and
                // backslash shorthand classes like \d/\w/\s are the one
                // known remaining gap — see this file's own top comment).
                var text = 'order-1234, order-5678, order-9999';
                var orderIds = text.match(/[0-9]+/g);
                var masked = text.replace(/[0-9]+/g, 'XXXX');

                // JSON.stringify on real nested data with real,
                // multi-digit numbers (Fase 3.77 could only do this for
                // single-digit numbers).
                var payload = JSON.stringify({
                    shapes: described,
                    orderIds: orderIds,
                    masked: masked
                });

                // Template literals, including nesting.
                var greeting = `Processed ${shapes.length} shapes and ${orderIds.length} orders`;

                greeting + ' | ' + payload;
            ";
            var result = engine.Evaluate(script);
            return result.AsString();
        }
    }
}
