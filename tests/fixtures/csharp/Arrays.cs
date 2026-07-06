using System;

namespace Vmnet.Fixtures
{
    // Fase 3.5 golden fixture: newarr/ldlen/ldelem.i4/stelem.i4. Elements
    // are assigned individually (not via a `{ 1, 2, 3 }` literal) to avoid
    // the compiler's RuntimeHelpers.InitializeArray fast path (ldtoken +
    // a data blob), which is a separate, still-unsupported feature — see
    // Unsupported.cs.
    public static class Arrays
    {
        public static int SumArray()
        {
            var xs = new int[3];
            xs[0] = 1;
            xs[1] = 2;
            xs[2] = 3;

            var total = 0;
            for (var i = 0; i < xs.Length; i++)
                total += xs[i];
            return total;
        }

        // ArrayCopyThreeArg: found missing entirely building examples/
        // jint-advanced-demo — real Jint's own internal array/property-
        // storage growth logic calls the 3-arg Array.Copy(Array, Array,
        // int) shorthand (implicitly sourceIndex=destinationIndex=0), not
        // just the 5-arg form every other real caller found so far uses.
        public static int ArrayCopyThreeArg()
        {
            var src = new int[3];
            src[0] = 1;
            src[1] = 2;
            src[2] = 3;
            var dst = new int[3];
            Array.Copy(src, dst, 3);
            return dst[0] + dst[1] + dst[2];
        }

        // ArrayClearWholeAndRange: Array.Clear didn't exist at all before
        // — found via Esprima.ArrayList<T>.Clear()'s own real
        // `Array.Clear(_items, 0, _count);` call, building the same demo.
        // netstandard2.0 only exposes the (Array, int, int) overload (the
        // parameterless Clear(Array) shorthand arrived later, in real
        // .NET Core) — arrayClear (internal/bcl/system_array.go) still
        // supports both, since a newer TFM's assembly can use either.
        public static int ArrayClearWholeAndRange()
        {
            var whole = new int[3];
            whole[0] = 5;
            whole[1] = 6;
            whole[2] = 7;
            Array.Clear(whole, 0, whole.Length);
            var wholeSum = whole[0] + whole[1] + whole[2];

            var ranged = new int[4];
            ranged[0] = 1;
            ranged[1] = 2;
            ranged[2] = 3;
            ranged[3] = 4;
            Array.Clear(ranged, 1, 2);
            var rangedSum = ranged[0] + ranged[1] + ranged[2] + ranged[3];

            return wholeSum * 100 + rangedSum; // 0*100 + (1+0+0+4) = 5
        }
    }
}
