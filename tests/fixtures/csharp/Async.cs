using System;
using System.Threading.Tasks;

namespace Vmnet.Fixtures
{
    // Fase 3.22 golden fixture: async/await, modeled as fully
    // synchronous — every Task any native produces is already completed
    // by construction (internal/bcl/system_task.go), so a real
    // compiler-generated state machine's MoveNext() runs start-to-finish
    // in a single call, needing no changes to the interpreter itself:
    // the state machine's own body is ordinary IL (fields, branches, a
    // real try/catch/finally region for exception routing), already
    // fully handled since Fase 1/3.10.
    public static class AsyncTest
    {
        public static async Task<int> ComputeAsync()
        {
            var a = await Task.FromResult(10);
            var b = await Task.FromResult(20);
            return a + b;
        }

        public static int RunSync()
        {
            return ComputeAsync().GetAwaiter().GetResult();
        }

        // An exception thrown inside an async method, after an await,
        // must still propagate out through GetAwaiter().GetResult() to
        // a synchronous catch — confirming SetException + the awaiter's
        // GetResult re-throw path both work, not just the happy path.
        public static async Task<int> ThrowingAsync()
        {
            await Task.FromResult(1);
            throw new InvalidOperationException("boom");
        }

        public static string RunThrowing()
        {
            try
            {
                ThrowingAsync().GetAwaiter().GetResult();
                return "no-throw";
            }
            catch (InvalidOperationException e)
            {
                return "caught:" + e.Message;
            }
        }

        public static async Task DoWorkAsync()
        {
            await Task.FromResult(1);
        }

        public static int RunVoid()
        {
            DoWorkAsync().GetAwaiter().GetResult();
            return 42;
        }

        // Awaiting another async method's own Task (not just
        // Task.FromResult) — confirms nested async call chains work,
        // not just the single-level case.
        public static async Task<int> NestedAwaitAsync()
        {
            var x = await ComputeAsync();
            return x * 2;
        }

        public static int RunNested()
        {
            return NestedAwaitAsync().GetAwaiter().GetResult();
        }
    }
}
