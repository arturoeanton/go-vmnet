using System;

namespace Vmnet.Fixtures
{
    // Fase 3.17 golden fixture: System.Lazy<T> — a static field initialized
    // from a Func<T> factory (the compiler compiles this into the class's
    // .cctor calling Lazy`1::.ctor), lazy (factory not invoked until the
    // first .Value access), and cached (a second .Value access must not
    // re-invoke the factory — verified by counting real invocations, not
    // just checking the returned value happens to be consistent).
    public static class LazyTest
    {
        private static int _callCount = 0;

        private static readonly Lazy<int> _lazy = new Lazy<int>(() =>
        {
            _callCount++;
            return _callCount * 100;
        });

        public static int ValueTwiceAndCallCount()
        {
            var a = _lazy.Value;
            var b = _lazy.Value;
            return a * 1000 + b * 10 + _callCount;
        }

        public static bool IsValueCreatedBeforeAndAfterAccess()
        {
            var lazy = new Lazy<int>(() => 7);
            var before = lazy.IsValueCreated;
            _ = lazy.Value;
            var after = lazy.IsValueCreated;
            return !before && after;
        }
    }
}
