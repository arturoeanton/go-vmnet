using System;

namespace Vmnet.Fixtures
{
    // Fase 2 golden fixture: throw + managed exception propagation.
    public static class ExceptionTest
    {
        public static void Fail()
        {
            throw new InvalidOperationException("boom");
        }
    }
}
