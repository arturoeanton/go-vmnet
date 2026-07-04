using Calculator;

// Standalone console entry point used ONLY for examples/calculator's
// optional native-CoreCLR timing comparison. main.go shells out to this
// project's own build output (never `dotnet run`, so build overhead
// never pollutes the timing). Calculator.csproj itself is a
// netstandard2.0 class library (matching every other vmnet-loaded
// wrapper in examples/) and can't be launched directly, so this project
// ProjectReferences it — the exact same Bench.CountPrimes/SumOfSquares
// code the vmnet run interprets is what CoreCLR runs here too, never a
// hand-duplicated copy that could drift out of sync.
if (args.Length != 2 || !int.TryParse(args[0], out var n) || !int.TryParse(args[1], out var m))
{
    Console.Error.WriteLine("usage: coreclr <primeBound> <squareBound>");
    return 1;
}

Console.WriteLine($"primes={Bench.CountPrimes(n)}");
Console.WriteLine($"squares={Bench.SumOfSquares(m)}");
return 0;
