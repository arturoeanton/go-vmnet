package interpreter

import (
	"github.com/arturoeanton/go-vmnet/internal/bcl"
	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// defaultValueFor computes default(T) for initobj: a zero-valued struct
// for a value type, Null() for a reference type or anything vmnet can't
// resolve — typeFullName == "" (an unresolved generic type parameter, see
// ir.InitObj's doc comment) or a foreign value type vmnet doesn't model
// (no TypeDef in the loaded assembly and no native registration) both
// fall back the same way. This matches vmnet's existing stance on
// unmodeled BCL surface elsewhere (an unresolvable Call target isn't a
// build-time failure either — internal/interpreter/calls.go) rather than
// making initobj a stricter special case: a method whose struct local
// gets fully overwritten before any field read never actually needed a
// correct default in the first place.
func (m *Machine) defaultValueFor(typeFullName string) runtime.Value {
	if typeFullName == "" {
		return runtime.Null()
	}
	if def, ok := m.nullableDefaultFor(typeFullName); ok {
		return def
	}
	// A closed generic instantiation's own encoded name (e.g.
	// "System.ValueTuple`2[[System.Reflection.ConstructorInfo],
	// [System.Reflection.ParameterInfo[]]]", ir/builder.go's
	// sigTypeFullName encoding) — bcl.LookupValueType's own registry is
	// keyed by the OPEN name only ("System.ValueTuple`2"), same as every
	// other GenericOpenName-first caller (assembly.go's resolveMember is
	// the established precedent). Without this, a closed generic BCL
	// value type (found via Enumerable.FirstOrDefault<ValueTuple`2<...>>
	// on an empty sequence, Fase 3.66) fell all the way through to
	// Null() below — silently wrong for a real value-typed default,
	// later crashing as a NullReferenceException on the first field
	// read.
	typeFullName = bcl.GenericOpenName(typeFullName)
	// Primitive value types have no TypeDef anywhere (they're a bare Kind
	// on Value, not a runtime.Type) and aren't in bcl.LookupValueType
	// either (that registry is for struct-shaped BCL types like DateTime,
	// not primitives) — so without this they'd fall through to Null(),
	// silently wrong for e.g. `new int[n]` (found running real Jint:
	// StringDictionarySlim`1's `_buckets` is `int[]`, and AddKey reads
	// buckets[i] with no null check, exactly like any value-type array
	// element — see the NewArr seeding this feeds, Fase 3.27).
	if def, ok := primitiveDefaults[typeFullName]; ok {
		return def
	}
	if t, ok := bcl.LookupValueType(typeFullName); ok {
		return runtime.StructVal(runtime.NewStruct(t))
	}
	if m.ResolveType == nil {
		return runtime.Null()
	}
	t, err := m.ResolveType(typeFullName)
	if err != nil || !t.IsValueType {
		return runtime.Null()
	}
	// A real C# enum is always represented as its underlying primitive
	// directly on the CIL stack, never as a struct — see assembly.go's
	// valueTypeDefault, which needed the identical fix for the same
	// reason (Fase 3.27).
	if t.IsEnum {
		return runtime.Int32(0)
	}
	return runtime.StructVal(runtime.NewStruct(t))
}

// nullableDefaultFor computes default(Nullable<T>) — hasValue=false,
// value=default(T) — for the one case ir/builder.go's
// resolveTypeTokenOrGeneric encodes T into typeFullName for at all
// ("System.Nullable`1[[T]]", the same "Open[[Arg]]" convention
// SigTypeFullName uses elsewhere): bcl.LookupValueType's shared
// nullableType (internal/bcl/system_nullable.go) hardcodes Int32(0) as
// ITS OWN "value" field default, correct for the dominant real case
// (int?/double?/bool?) but silently wrong for any other T. Found running
// real Jint/Esprima: Esprima.JavaScriptParser's own
// `_parseVariableBindingParameters` field is `Esprima.ArrayList<Token>?`
// — a Nullable<T> wrapping a plugin-defined generic struct, not a BCL
// primitive — and `_parseVariableBindingParameters = null;` (compiling
// to `initobj Nullable<ArrayList<Token>>`) used to leave the "value"
// field a raw int32 where an ArrayList<Token> struct was expected,
// crashing several calls later with a NullReferenceException that
// looked completely unrelated to Nullable at first. Recurses through
// defaultValueFor itself for T's own default, so T can be anything this
// function already knows how to default (numeric, a BCL struct, or
// another plugin value type) — false for anything that isn't really a
// closed Nullable`1[[T]] encoding, in which case the caller falls back
// to its own existing GenericOpenName/bcl.LookupValueType path
// unchanged (the shared nullableType, correct for a numeric T).
func (m *Machine) nullableDefaultFor(typeFullName string) (runtime.Value, bool) {
	const openName = "System.Nullable`1"
	if bcl.GenericOpenName(typeFullName) != openName || typeFullName == openName {
		return runtime.Value{}, false
	}
	args := bcl.ClosedGenericArgs(typeFullName)
	if len(args) != 1 {
		return runtime.Value{}, false
	}
	t, ok := bcl.LookupValueType(openName)
	if !ok {
		return runtime.Value{}, false
	}
	s := runtime.NewStruct(t)
	s.Fields[0] = runtime.Bool(false)
	s.Fields[1] = m.defaultValueFor(args[0])
	return runtime.StructVal(s), true
}

// primitiveDefaults maps the CIL primitive value type names to their
// default(T) — see defaultValueFor's doc comment for why these can't go
// through the normal TypeDef/bcl.LookupValueType paths. bool/byte/sbyte/
// short/ushort/char all collapse to KindI4 on the CIL stack, matching
// every other primitive-as-int32 rule in vmnet (see runtime.Kind's doc
// comment); IntPtr/UIntPtr are excluded — vmnet doesn't model native int
// as a distinct kind.
var primitiveDefaults = map[string]runtime.Value{
	"System.Boolean": runtime.Int32(0),
	"System.Byte":    runtime.Int32(0),
	"System.SByte":   runtime.Int32(0),
	"System.Int16":   runtime.Int32(0),
	"System.UInt16":  runtime.Int32(0),
	"System.Char":    runtime.Int32(0),
	"System.Int32":   runtime.Int32(0),
	"System.UInt32":  runtime.Int32(0),
	"System.Int64":   runtime.Int64(0),
	"System.UInt64":  runtime.Int64(0),
	"System.Single":  runtime.Float32(0),
	"System.Double":  runtime.Float64(0),
}
