package bcl

import "github.com/arturoeanton/go-vmnet/internal/runtime"

// System.Runtime.CompilerServices.ConditionalWeakTable<TKey,TValue> (Fase
// 3.43, found reading a real .xlsx through ClosedXML 0.105.0's `new
// XLWorkbook(stream)`: ClosedXML's own formula-parse cache — real
// decompiled source, /tmp/closedxml_ns20/ClosedXML.Excel.CalcEngine/
// ExpressionCache.cs:14,22-27,34 — is a `ConditionalWeakTable<string,
// Formula>` used purely as a TryGetValue/Add memo of parsed formulas).
//
// Backed by the exact same nativeDict machinery Dictionary`2 already uses
// rather than anything weak: the "conditional weak" part of the real type
// exists only so cached entries don't outlive their keys — a pure memory
// optimization, unobservable in results, and vmnet has no GC hook to
// model it with anyway. Same documented "never collected is a safe
// over-approximation" posture nativeWeakReference (system_weakreference.
// go) already takes, for the same reason, for this same package's own
// WeakReference-based caches. Only the members ClosedXML's real cache
// touches (plus Remove, one line on the same shared native) are
// registered; nothing else in this loop's target packages constructs one
// (grepped the full decompiled ClosedXML + DocumentFormat.OpenXml
// surface: ExpressionCache is the only real use).
func init() {
	registerCtor("System.Runtime.CompilerServices.ConditionalWeakTable`2", func([]runtime.Value) (*runtime.Object, error) {
		return &runtime.Object{Native: &nativeDict{m: map[string]dictEntry{}, typeName: "System.Runtime.CompilerServices.ConditionalWeakTable`2"}}, nil
	})
	register("System.Runtime.CompilerServices.ConditionalWeakTable`2::TryGetValue", true, dictTryGetValue)
	register("System.Runtime.CompilerServices.ConditionalWeakTable`2::Add", false, dictAdd)
	register("System.Runtime.CompilerServices.ConditionalWeakTable`2::Remove", true, dictRemove)
}
