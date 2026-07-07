using System;
using System.Collections;
using System.Collections.Generic;

namespace Vmnet.Fixtures
{
    // Fase 3.81 golden fixtures: three real, distinct generic-parameter-
    // forwarding shapes found getting CsvHelper's own AutoMap() reflection
    // path (examples/csvhelper-demo) to work end to end, none of which
    // ir.Call.MethodGenericArgs's existing "!!N" (method-level MVAR)
    // sentinel handling (Fase 3.60) covered on its own. NestedGenericSentinel
    // gained a fourth case in Fase 3.83 (DirectTypeofWrapped) closing the
    // one gap this file's own Fase 3.81 doc comment had left open.

    public class SentinelTarget
    {
    }

    public class SentinelWrapper<T>
    {
    }

    public static class ClassLevelSentinel
    {
        // A generic method whose own typeof(T) is read from ANOTHER
        // generic method call, forwarded as a CLASS-level generic
        // parameter — the exact shape CsvHelper's own compiler-generated
        // GetRecords<T>() iterator produces calling ValidateHeader<T>():
        // the iterator's MoveNext() is not itself generic, but is
        // declared on a generic class (the compiler-generated state
        // machine) that closes over T as a class-level parameter, so a
        // call from inside MoveNext() back out to another generic method
        // using that same T compiles to a MethodSpec instantiated with
        // "!0" (VAR), not "!!0" (MVAR) — previously resolved to an empty
        // type name (see ir.sigTypeListGenericArgNames's own doc comment
        // and eval.go's resolveForwardedGenericArgs).
        public static string NameOf<T>()
        {
            return typeof(T).FullName;
        }

        // The iterator: T here is the state machine's own CLASS-level
        // generic parameter — calling NameOf<T>() from inside MoveNext()
        // forwards it as "!0".
        public static IEnumerable<string> IterateNameOf<T>()
        {
            yield return NameOf<T>();
        }

        public static string IterateNameOfTargetCaller()
        {
            foreach (var name in IterateNameOf<SentinelTarget>())
            {
                return name;
            }
            return null;
        }
    }

    public static class NestedGenericSentinel
    {
        public static string NameOf<T>()
        {
            return typeof(T).FullName;
        }

        // A generic method's own still-open TWrapped forwarded not as a
        // bare type argument but NESTED inside a closed generic
        // instantiation of another type used to instantiate a DIFFERENT
        // generic method call — the exact shape CsvHelper's own
        // CsvContext.AutoMap<T>() uses: `ObjectResolver.Current.
        // Resolve<DefaultClassMap<T>>()`, a MethodSpec instantiated with
        // "DefaultClassMap`1[[!!0]]", not a bare "!!0" on its own. Here,
        // NameOf<SentinelWrapper<TWrapped>>() is a MethodSpec instantiated
        // with "SentinelWrapper`1[[!!0]]" — the identical shape. Previously
        // the "!!0" sentinel only survived when it was the ENTIRE argument
        // string, not nested one level inside a SigGenericInst — see
        // ir.sigTypeFullNameGenericArg and eval.go's
        // resolveForwardedGenericArgs (now a substring scan, not a
        // whole-string match).
        //
        public static string NameOfWrapped<TWrapped>()
        {
            return NameOf<SentinelWrapper<TWrapped>>();
        }

        public static string NameOfWrappedTargetCaller()
        {
            return NameOfWrapped<SentinelTarget>();
        }

        // A DIRECT `typeof(SentinelWrapper<TWrapped>)` ldtoken (as opposed
        // to forwarding into another generic METHOD call the way
        // NameOfWrapped above does) is a separate shape — a `ldtoken`
        // TypeSpec that's itself a SigGenericInst with TWrapped nested
        // inside, not a bare SigGenericParam, so neither
        // IsMethodGenericParam nor IsClassGenericParam gets set for it.
        // Fixed in Fase 3.83: resolveClosedTypeSpecName (internal/ir/
        // builder.go) now uses the same sigTypeFullNameGenericArg
        // NameOfWrapped's own MethodSpec case already used, and eval.go's
        // own ir.LoadTypeToken case resolves the resulting nested
        // sentinel via the same substring scan.
        public static string DirectTypeofWrapped<TWrapped>()
        {
            return typeof(SentinelWrapper<TWrapped>).FullName;
        }

        public static string DirectTypeofWrappedTargetCaller()
        {
            return DirectTypeofWrapped<SentinelTarget>();
        }
    }

    // A real, plugin-defined class implementing IEnumerable<string> via
    // its own compiler-generated iterator (yield return), NOT a
    // vmnet-native List<T>/array — the exact shape CsvHelper's own
    // MemberNameCollection is (CsvHelper.Configuration.
    // MemberNameCollection.cs, a plain List<string>-backed class with its
    // own `this[int]` indexer and a real `yield return this[i]`
    // GetEnumerator()).
    public class CustomStringCollection : IEnumerable<string>, IEnumerable
    {
        private readonly List<string> items = new List<string>();

        public void Add(string item)
        {
            items.Add(item);
        }

        public IEnumerator<string> GetEnumerator()
        {
            for (int i = 0; i < items.Count; i++)
            {
                yield return items[i];
            }
        }

        IEnumerator IEnumerable.GetEnumerator()
        {
            return items.GetEnumerator();
        }
    }

    public static class StringJoinOverCustomEnumerable
    {
        // System.String.Join(string, IEnumerable<string>) given a real
        // plugin IEnumerable<string> — bcl's own plain-native Join had no
        // Machine access to drive GetEnumerator/MoveNext/get_Current, so
        // it silently formatted the un-enumerated collection object
        // itself as one opaque placeholder string instead of joining its
        // real elements (Fase 3.81, found via CsvHelper's own
        // GetFieldIndex building a per-member cache key from
        // MemberNameCollection this exact way).
        public static string JoinTwo(string a, string b)
        {
            var coll = new CustomStringCollection();
            coll.Add(a);
            coll.Add(b);
            return string.Join("_", coll);
        }
    }

    public static class BooleanTryParseTest
    {
        // Boolean.TryParse(string, out bool) was simply missing (Fase
        // 3.81, found via CsvHelper's own BooleanConverter.
        // ConvertFromString, whose first attempt is a bare
        // bool.TryParse(text, out result)).
        public static string TryParseRoundTrip(string text)
        {
            bool ok = bool.TryParse(text, out bool result);
            return ok + ":" + result;
        }
    }
}
