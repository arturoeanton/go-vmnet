package interpreter

import (
	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// System.Runtime.CompilerServices.Unsafe.SizeOf<T>() is a real,
// interpreted method (from the System.Runtime.CompilerServices.Unsafe
// shim package) whose entire body is just the IL `sizeof` instruction —
// an opcode ir/builder.go has never implemented (it needs to resolve a
// generic METHOD parameter operand, `!!0`, at the call site rather than
// once at IR-build time — the same shape as ir.Call.MethodGenericArgs
// already solves for FeatureCollectionBase::Get<TFeature>, see
// features.go). Rather than add a new opcode plus generic-param
// resolution just to run one instruction, this intercepts the method
// directly via genericMachineRegistry — used constantly by
// System.Memory's own SpanHelpers/Span/MemoryExtensions internals
// (Fase 3.40), so real code reaching this needs a real answer.
func init() {
	genericMachineRegistry["System.Runtime.CompilerServices.Unsafe::SizeOf"] = unsafeSizeOf
}

// primitiveSizeOf gives the real managed size in bytes for every
// primitive plus the handful of common blittable BCL value types
// (Decimal/Guid/DateTime/TimeSpan) found in real Span<T>-adjacent code
// (Fase 3.40) — assumes a 64-bit IntPtr/UIntPtr, matching every platform
// vmnet itself actually runs on.
var primitiveSizeOf = map[string]int32{
	"System.Boolean": 1, "System.SByte": 1, "System.Byte": 1,
	"System.Char": 2, "System.Int16": 2, "System.UInt16": 2,
	"System.Int32": 4, "System.UInt32": 4, "System.Single": 4,
	"System.Int64": 8, "System.UInt64": 8, "System.Double": 8,
	"System.IntPtr": 8, "System.UIntPtr": 8,
	"System.Decimal": 16, "System.Guid": 16,
	"System.DateTime": 8, "System.TimeSpan": 8,
}

func unsafeSizeOf(m *Machine, args []runtime.Value, methodGenericArgs []string, depth int, instrCount *int64) (runtime.Value, error) {
	if len(methodGenericArgs) < 1 || methodGenericArgs[0] == "" {
		return runtime.Int32(8), nil
	}
	open := bcl.GenericOpenName(methodGenericArgs[0])
	if size, ok := primitiveSizeOf[open]; ok {
		return runtime.Int32(size), nil
	}
	size := int32(8)
	if _, isEnum, _, _ := classifyTypeByName(m, methodGenericArgs[0]); isEnum {
		// A .NET enum's underlying type defaults to Int32 absent an
		// explicit `: byte`/`: long` base — the common case.
		size = 4
	}
	// A struct/class T with no tracked field-size layout (runtime.Type
	// only tracks field NAMES — see runtime/class.go's Fields doc
	// comment): 8 is a safe, common-case fallback (a single reference or
	// pointer-sized field), not an exact answer.
	return runtime.Int32(size), nil
}
