namespace Calculator
{
    // Slightly larger arithmetic/loop example (examples/calculator,
    // Fase 4 benchmark seed — docs/en/ROADMAP.md, spec.md Sec 32): two
    // independent, purely-arithmetic/loop-driven workloads with no BCL
    // surface beyond plain locals, so the only thing under test is
    // vmnet's own bytecode dispatch loop, not any interpreted BCL
    // method.
    public static class Bench
    {
        // CountPrimes counts the primes strictly below n via trial
        // division (O(n*sqrt(n)) comparisons/divisions) — the
        // "arithmetic loop" benchmark test named in spec.md Sec 32.2.
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

        // SumOfSquares sums i*i for i in [1, n] — a second, distinct
        // arithmetic/loop shape (single loop, multiply-accumulate, no
        // branch inside the loop body) alongside CountPrimes' nested
        // loop + modulo + branch shape.
        public static long SumOfSquares(int n)
        {
            long sum = 0;
            for (int i = 1; i <= n; i++)
            {
                sum += (long)i * i;
            }
            return sum;
        }
    }
}
