using System;

namespace Vmnet.Fixtures
{
    // Fase 3.10 golden fixture: real try/catch/finally.
    public static class TryCatch
    {
        public static int CatchByType(bool throwArgNull)
        {
            var x = 0;
            try
            {
                if (throwArgNull)
                {
                    throw new ArgumentNullException("p");
                }
                x = 1;
            }
            catch (ArgumentNullException)
            {
                x = 2;
            }
            return x;
        }

        // The catch clause for ArgumentException must also match an
        // ArgumentNullException thrown inside the try (real class
        // hierarchy walk, Fase 3.8), not just an exact type match.
        public static int CatchByBaseType()
        {
            try
            {
                throw new ArgumentNullException("p");
            }
            catch (ArgumentException)
            {
                return 42;
            }
        }

        public static string FinallyAlwaysRuns(bool doThrow)
        {
            var log = "";
            try
            {
                log += "try;";
                if (doThrow)
                {
                    throw new InvalidOperationException("boom");
                }
            }
            catch (InvalidOperationException)
            {
                log += "catch;";
            }
            finally
            {
                log += "finally;";
            }
            return log;
        }

        public static string FinallyRunsOnUncaughtException()
        {
            var log = "";
            try
            {
                try
                {
                    log += "inner-try;";
                    throw new NotSupportedException("nope");
                }
                finally
                {
                    log += "inner-finally;";
                }
            }
            catch (NotSupportedException)
            {
                log += "outer-catch;";
            }
            return log;
        }

        public static int FirstMatchingCatchWins(bool throwArgNull)
        {
            try
            {
                if (throwArgNull)
                {
                    throw new ArgumentNullException("p");
                }
                throw new InvalidOperationException("x");
            }
            catch (ArgumentNullException)
            {
                return 1;
            }
            catch (InvalidOperationException)
            {
                return 2;
            }
        }

        public static string Rethrow()
        {
            var log = "";
            try
            {
                try
                {
                    throw new InvalidOperationException("original");
                }
                catch (InvalidOperationException)
                {
                    log += "inner-catch;";
                    throw;
                }
            }
            catch (InvalidOperationException ex)
            {
                log += "outer-catch:" + ex.Message;
            }
            return log;
        }

        public static int UncaughtExceptionPropagates()
        {
            try
            {
                throw new NotSupportedException("propagates");
            }
            catch (ArgumentException)
            {
                // Wrong type — must not catch this.
                return -1;
            }
        }
    }

    // Fase 3.13 addition: a plugin's own exception subclass, chaining to
    // its base via `: base(message)` — a plain (non-virtual) `call
    // System.Exception::.ctor(this, message)`, not `newobj` (only the
    // exact BCL type gets newobj'd for a native exception ctor).
    public class CustomException : Exception
    {
        public CustomException(string message) : base(message) { }
    }

    public static class CustomExceptionTest
    {
        public static string CatchExact()
        {
            try
            {
                throw new CustomException("custom-boom");
            }
            catch (CustomException e)
            {
                return "exact:" + e.Message;
            }
        }

        // catch (Exception e) must also match a thrown CustomException —
        // real base-class walk, not just the exact declared catch type.
        public static string CatchBase()
        {
            try
            {
                throw new CustomException("custom-boom-2");
            }
            catch (Exception e)
            {
                return "base:" + e.Message;
            }
        }
    }
}
