using System;
using Microsoft.Extensions.DependencyInjection;

namespace VmnetDiDemo
{
    // Real constructor injection, not just a trivial parameterless type:
    // Greeter depends on IClock, which the container must resolve and
    // pass in on its own — exactly the pattern every real ASP.NET Core/
    // worker-service Program.cs relies on, not a degenerate special case.
    public interface IClock
    {
        string Now();
    }

    public class FixedClock : IClock
    {
        public string Now() => "2026-01-01T00:00:00Z";
    }

    public interface IGreeter
    {
        string Greet(string name);
    }

    public class Greeter : IGreeter
    {
        private readonly IClock _clock;

        public Greeter(IClock clock)
        {
            _clock = clock;
        }

        public string Greet(string name) => "Hello, " + name + "! (at " + _clock.Now() + ")";
    }

    public static class Program
    {
        public static string Run(string name)
        {
            var services = new ServiceCollection();
            services.AddSingleton<IClock, FixedClock>();
            services.AddSingleton<IGreeter, Greeter>();
            using (var provider = services.BuildServiceProvider())
            {
                var greeter = provider.GetRequiredService<IGreeter>();
                return greeter.Greet(name);
            }
        }
    }
}
