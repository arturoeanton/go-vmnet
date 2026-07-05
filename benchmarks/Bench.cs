using System.Collections.Generic;
using System.Text.Json;

namespace Vmnet.Benchmarks
{
    // Fase 4 benchmark suite (docs/en/ROADMAP.md; spec.md Sec 32.2) — the
    // six remaining workloads beyond "arithmetic loop" (already covered
    // by examples/calculator's own Bench.CountPrimes/SumOfSquares, the
    // original seed of this suite; benchmarks/main.go reports both
    // suites together for one combined picture). Every method loops
    // INSIDE C# and returns one final result — the Go harness times one
    // Assembly.Call per workload, so per-call dispatch overhead never
    // pollutes the "n iterations of real work" measurement, matching
    // Calculator.Bench's own convention.
    public static class Bench
    {
        // "arithmetic loop" (spec Sec 32.2) — identical algorithm to
        // examples/calculator's own Bench.CountPrimes/SumOfSquares (that
        // example remains the standalone "seed" demo; this copy makes
        // benchmarks/ itself fully self-contained, one binary covering
        // all seven spec Sec 32.2 workloads).
        public static long CountPrimes(int n)
        {
            long count = 0;
            for (int i = 2; i < n; i++)
            {
                bool isPrime = true;
                for (int d = 2; (long)d * d <= i; d++)
                {
                    if (i % d == 0)
                    {
                        isPrime = false;
                        break;
                    }
                }
                if (isPrime)
                {
                    count++;
                }
            }
            return count;
        }

        public static long SumOfSquares(int n)
        {
            long sum = 0;
            for (int i = 1; i <= n; i++)
            {
                sum += (long)i * i;
            }
            return sum;
        }

        // "string concat" (spec Sec 32.2): a loop of naive `+=`
        // concatenation — the real-world-common, quadratic-if-done-wrong
        // shape (each `+=` allocates a new string), not StringBuilder,
        // since that's what most real, unoptimized business code
        // actually writes.
        public static int StringConcat(int n)
        {
            var s = "";
            for (var i = 0; i < n; i++)
            {
                s += "x";
            }
            return s.Length;
        }

        // "object allocation" (spec Sec 32.2): n `newobj` + `ldfld`
        // round trips through a real reference-typed class (not a
        // struct — a struct wouldn't touch the heap the way spec Sec
        // 32.2's own "object allocation" intends).
        private sealed class Point
        {
            public int X;
            public int Y;
            public Point(int x, int y) { X = x; Y = y; }
        }

        public static long AllocateObjects(int n)
        {
            long sum = 0;
            for (var i = 0; i < n; i++)
            {
                var p = new Point(i, i + 1);
                sum += p.X + p.Y;
            }
            return sum;
        }

        // "List<T>.Add" (spec Sec 32.2): n sequential Adds into one
        // growing List<int>, returning Count so the result depends on
        // every single Add actually having happened.
        public static int ListAdd(int n)
        {
            var list = new List<int>();
            for (var i = 0; i < n; i++)
            {
                list.Add(i);
            }
            return list.Count;
        }

        // "Dictionary lookup" (spec Sec 32.2): build a Dictionary<int,
        // int> of n entries, then look up all n keys via TryGetValue,
        // summing the hits — both the write path (n inserts) and the
        // read path (n lookups) are real, measured work.
        public static long DictionaryLookup(int n)
        {
            var dict = new Dictionary<int, int>();
            for (var i = 0; i < n; i++)
            {
                dict[i] = i * 2;
            }
            long sum = 0;
            for (var i = 0; i < n; i++)
            {
                if (dict.TryGetValue(i, out var v))
                {
                    sum += v;
                }
            }
            return sum;
        }

        private sealed class Payload
        {
            public string Name { get; set; }
            public int Amount { get; set; }
            public bool Ok { get; set; }
        }

        // "JSON in/out" (spec Sec 32.2): n real JsonSerializer.Serialize
        // + JsonSerializer.Deserialize round trips through the real,
        // unmodified System.Text.Json NuGet package (the same one
        // examples/system-text-json-demo runs) — a representative real
        // BCL workload, not a hand-rolled toy encoder, so this measures
        // actual interpreted-BCL-call overhead at realistic depth
        // (reflection-driven serialization internally).
        public static long JsonRoundTrip(int n)
        {
            long sum = 0;
            for (var i = 0; i < n; i++)
            {
                var payload = new Payload { Name = "vmnet", Amount = i, Ok = i % 2 == 0 };
                var json = JsonSerializer.Serialize(payload);
                var back = JsonSerializer.Deserialize<Payload>(json);
                sum += back.Amount;
            }
            return sum;
        }

        // "rule engine call 10k times" (spec Sec 32.2): a small,
        // representative business-rule decision (discount tiers by
        // amount) — the Go harness calls THIS method itself 10,000
        // times (one Assembly.Call per iteration, not a C#-side loop),
        // deliberately the ONE benchmark in this suite that measures
        // real per-call round-trip overhead at realistic call volume,
        // matching spec Sec 32.3's own "method invoke overhead" metric
        // at a scale a real rule-engine-style host integration would
        // hit in practice.
        public static int EvalRule(int amount)
        {
            if (amount >= 1000) return 20;
            if (amount >= 500) return 10;
            if (amount >= 100) return 5;
            return 0;
        }
    }
}
