package bcl

import (
	"fmt"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// Common exception types fixtures throw and catch (Fase 3.10 adds real
// try/catch/finally — see internal/interpreter/exceptions.go; construction
// just needs to capture a type name and message, not a full object).
func init() {
	for _, name := range []string{
		"System.Exception",
		"System.InvalidOperationException",
		"System.ArgumentException",
		"System.ArgumentNullException",
		"System.ArgumentOutOfRangeException",
		"System.IndexOutOfRangeException",
		"System.NotSupportedException",
		"System.InvalidCastException",
		"System.FormatException",
		"System.OverflowException",
		"System.NotImplementedException",
		"System.IO.IOException",
		"System.IO.EndOfStreamException",
		// DocumentFormat.OpenXml.Framework's own OpenXmlPartRootElement.
		// LoadFromPart(part, stream) (Fase 3.41, found reading a real
		// .xlsx: /tmp/openxmlfw_ns20/DocumentFormat.OpenXml/
		// OpenXmlPartRootElement.cs:109-157) throws this both directly
		// (an unexpected root element name) and via a catch/rethrow
		// wrapper in OpenXmlPart.LoadDomTree<T> (loaddomtree.go's own doc
		// comment) — without it, even a real, well-formed .xlsx failed
		// with an unrelated "type System.IO.InvalidDataException not
		// found" the moment any part's root-element validation path ran
		// at all, real error or not.
		"System.IO.InvalidDataException",
		// System.Data.DataException/System.ApplicationException/System.
		// ObjectDisposedException (Fase 3.52) — Dapper's own
		// DisposedReader guard throws ObjectDisposedException, and
		// System.Data's own DataException is thrown by more than one real
		// ADO.NET-adjacent code path; all three share the same plain
		// (message)/(message, innerException) constructor shape as
		// System.Exception itself (ObjectDisposedException's real 2-string
		// overload is (objectName, message) rather than (message,
		// paramName), but no real caller here inspects ObjectName
		// specifically, only Message — same simplification
		// exceptionGetParamName's own doc comment already accepts for a
		// type outside argExceptionParamOrder).
		"System.Data.DataException",
		"System.ApplicationException",
		"System.ObjectDisposedException",
		// System.OperationCanceledException (Fase 3.53) — real
		// CancellationToken.ThrowIfCancellationRequested()
		// (system_cancellationtoken.go) throws exactly this when a
		// source it derives from has genuinely been Cancel()'d. Shares
		// the same plain (message)/(message, innerException) shape as
		// System.Exception itself; the real BCL's own third (message,
		// CancellationToken) overload isn't covered here — no real
		// corpus caller constructs one that way, only vmnet's own
		// ThrowIfCancellationRequested ever throws this type at all.
		"System.OperationCanceledException",
		// System.UnauthorizedAccessException/System.IO.
		// FileNotFoundException/System.IO.DirectoryNotFoundException (Fase
		// 3.59) — the new deny-by-default Permissions gate
		// (internal/interpreter/permissions.go) throws
		// UnauthorizedAccessException directly as a Go error rather than
		// through this ctor path, so registering it here isn't required
		// for that to work; it's registered anyway so a plugin that
		// explicitly does `new UnauthorizedAccessException("...")` (or
		// catches/rethrows one) compiles and behaves like any other
		// exception type here. FileNotFoundException/
		// DirectoryNotFoundException back the new System.IO.File/
		// Directory/FileInfo natives' own real "no such file/directory"
		// failures (system_io_file.go) the same way.
		"System.UnauthorizedAccessException",
		"System.IO.FileNotFoundException",
		"System.IO.DirectoryNotFoundException",
	} {
		registerCtor(name, newExceptionCtor(name))
		// A plugin's own exception subclass (`class MyException :
		// Exception { public MyException(string m) : base(m) {} }`)
		// chains to its base via a plain, non-virtual `call
		// System.Exception::.ctor(this, message)` — not `newobj` (only
		// the *exact* type gets newobj'd; the base call runs on the
		// already-allocated derived object). registerCtor above only
		// ever fires from newObj's LookupCtor branch, which handles
		// constructing the exact BCL type directly — this second
		// registration is what makes base-chaining from a subclass
		// resolve at all (Fase 3.13).
		register(name+"::.ctor", false, baseExceptionCtorInPlace(name))
	}
	register("System.Exception::get_Message", true, exceptionGetMessage)
	register("System.Exception::get_InnerException", true, exceptionGetInnerException)
	// Exception.ToString() is NOT plain Object.ToString() (which the
	// System.Object fallback in calls.go would otherwise supply once this
	// registration is gone — just the bare type name, with no message at
	// all): real .NET's override is "TypeName: Message" chained through
	// every InnerException via " ---> ", plus a stack trace this
	// interpreter has no frames to reconstruct. ManagedException.Error()
	// already builds exactly the first part (it exists so a Go caller's
	// err.Error() reads sensibly) — reused here verbatim rather than
	// duplicating the same TypeName/Message/Inner-chaining logic twice.
	register("System.Exception::ToString", true, exceptionToString)
	// Every exception carries a Data dictionary whether or not anything
	// ever populates it (real Exception.Data is never null) — see
	// ManagedException.Data's own doc comment for why it's lazily
	// allocated into the exception itself rather than built eagerly by
	// every one of the constructors above.
	register("System.Exception::get_Data", true, exceptionGetData)
	// ArgumentException/ArgumentNullException/ArgumentOutOfRangeException
	// all expose ParamName — "" for any other exception type, or for one
	// of these three constructed without it (see newExceptionCtor's own
	// per-type argument-shape handling for how it's captured).
	register("System.ArgumentException::get_ParamName", true, exceptionGetParamName)
	register("System.ArgumentNullException::get_ParamName", true, exceptionGetParamName)
	register("System.ArgumentOutOfRangeException::get_ParamName", true, exceptionGetParamName)

	// System.AggregateException: Task/Parallel's own multi-fault wrapper.
	// Not part of the shared newExceptionCtor loop above — its
	// constructor shape (a `params Exception[]`/IEnumerable<Exception>
	// argument, not a single (message, paramName)/(message, inner) pair)
	// and its own InnerExceptions/Flatten members are specific to it.
	registerCtor("System.AggregateException", aggregateExceptionCtor)
	register("System.AggregateException::get_InnerExceptions", true, aggregateExceptionGetInnerExceptions)
	register("System.AggregateException::Flatten", true, aggregateExceptionFlatten)
}

// innerExceptionArg finds a real Exception argument among ctorArgs (the
// `(string message, Exception innerException)` overload's 2nd parameter)
// — every real BCL/plugin exception is an Object whose Native is a
// *runtime.ManagedException, so that's what identifies one here rather
// than trusting a fixed argument position (some exception types have
// (message, paramName) with a second *string*, not an inner exception).
func innerExceptionArg(args []runtime.Value) *runtime.Object {
	for _, a := range args {
		if a.Kind == runtime.KindObject && a.Obj != nil {
			if _, ok := a.Obj.Native.(*runtime.ManagedException); ok {
				return a.Obj
			}
		}
	}
	return nil
}

// stringArgs collects every KindString argument, in call order — used to
// tell apart an exception constructor's various (string, string) overload
// shapes (message-then-paramName, or paramName-then-message — see
// newExceptionCtor's own doc comment for why these differ by exception
// type) without hardcoding a fixed argument index for either role.
func stringArgs(args []runtime.Value) []string {
	var out []string
	for _, a := range args {
		if a.Kind == runtime.KindString {
			out = append(out, a.Str)
		}
	}
	return out
}

// argExceptionParamOrder lists the exception types whose real ctor puts
// paramName BEFORE message when both strings are given — the opposite of
// ArgumentException's own (message, paramName) order, and of every other
// exception type's plain (message) or (message, innerException) shape.
// A real, well-known .NET API asymmetry (ArgumentException(message,
// paramName) vs. ArgumentNullException(paramName, message)/
// ArgumentOutOfRangeException(paramName, message)), not a typo here.
var argExceptionParamOrder = map[string]bool{
	"System.ArgumentNullException":       true,
	"System.ArgumentOutOfRangeException": true,
}

// defaultParamExceptionMessage matches the real BCL's own generated
// message text for the single-paramName constructor overload (no
// explicit message given) — used often enough in real code asserting on
// Message content that a generic placeholder would be a visible
// mismatch, unlike every other exception type's default message (a
// simple fixed string vmnet doesn't need to special-case at all since no
// real caller here inspects it).
func defaultParamExceptionMessage(typeName, paramName string) string {
	switch typeName {
	case "System.ArgumentNullException":
		return fmt.Sprintf("Value cannot be null. (Parameter '%s')", paramName)
	case "System.ArgumentOutOfRangeException":
		return fmt.Sprintf("Specified argument was out of the range of valid values. (Parameter '%s')", paramName)
	default:
		return ""
	}
}

// newExceptionCtor covers every fixed BCL exception type's real
// constructor overloads that take up to two strings (message/paramName,
// in whichever order argExceptionParamOrder says this type uses) plus an
// optional inner Exception — e.g. ArgumentException(message, paramName),
// ArgumentNullException(paramName), ArgumentNullException(paramName,
// message), or plain Exception(message)/Exception(message, inner). Every
// other registered type here (Exception, InvalidOperationException, ...)
// only ever has the plain (message) shape, so falls through
// argExceptionParamOrder's false case and behaves exactly as before.
func newExceptionCtor(typeName string) NativeCtor {
	return func(args []runtime.Value) (*runtime.Object, error) {
		strs := stringArgs(args)
		msg, paramName := "", ""
		switch {
		case argExceptionParamOrder[typeName] && len(strs) >= 1:
			paramName = strs[0]
			if len(strs) >= 2 {
				msg = strs[1]
			} else {
				msg = defaultParamExceptionMessage(typeName, paramName)
			}
		case len(strs) >= 1:
			msg = strs[0]
			if len(strs) >= 2 {
				paramName = strs[1]
			}
		}
		return &runtime.Object{Native: &runtime.ManagedException{TypeName: typeName, Message: msg, ParamName: paramName, Inner: innerExceptionArg(args)}}, nil
	}
}

// baseExceptionCtorInPlace backs "<name>::.ctor" as a plain (non-newobj)
// call — the shape a derived plugin class's constructor uses to chain to
// its base via `: base(message)`. Unlike newExceptionCtor's newobj-only
// path (which allocates a fresh Object), the receiver here already
// exists: newObj already allocated it as the *derived* plugin type
// (Obj.Type is the derived TypeDef, Fields holds the derived class's own
// fields), so this only adds Obj.Native alongside it — a deliberate,
// narrow exception to Object's normal "Type xor Native" rule (see
// runtime.Object's doc comment): ir.Throw requires Obj.Native to be a
// *runtime.ManagedException on ANY thrown object, plugin-declared or
// not, and vmnet has no real field layout for System.Exception to fold
// Message into Fields instead.
//
// TypeName is set to the receiver's actual *runtime.Type (the derived
// plugin class, e.g. "Vmnet.Fixtures.MyException"), not the fallback
// typeName this native is registered under ("System.Exception" etc.) —
// catch-matching (exceptionMatchesCatch, internal/interpreter/
// exceptions.go) needs the real most-derived name so `catch
// (MyException e)` matches; `catch (Exception e)` still works too, via
// nativeMatches walking the real TypeDef base chain once it can't find a
// hand-mapped BCL exception name (Fase 3.13).
func baseExceptionCtorInPlace(typeName string) Native {
	return func(args []runtime.Value) (runtime.Value, error) {
		if len(args) == 0 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
			return runtime.Value{}, fmt.Errorf("bcl: %s constructor called without a receiver", typeName)
		}
		msg := ""
		if len(args) > 1 && args[1].Kind == runtime.KindString {
			msg = args[1].Str
		}
		name := typeName
		if t := args[0].Obj.Type; t != nil {
			if t.Namespace != "" {
				name = t.Namespace + "." + t.Name
			} else {
				name = t.Name
			}
		}
		args[0].Obj.Native = &runtime.ManagedException{TypeName: name, Message: msg, Inner: innerExceptionArg(args[1:])}
		return runtime.Value{}, nil
	}
}

func exceptionGetMessage(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return runtime.Value{}, fmt.Errorf("bcl: System.Exception.get_Message expects a receiver")
	}
	ex, ok := args[0].Obj.Native.(*runtime.ManagedException)
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: receiver is not an Exception")
	}
	return runtime.String(ex.Message), nil
}

func exceptionToString(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return runtime.Value{}, fmt.Errorf("bcl: System.Exception.ToString expects a receiver")
	}
	ex, ok := args[0].Obj.Native.(*runtime.ManagedException)
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: receiver is not an Exception")
	}
	return runtime.String(ex.Error()), nil
}

func exceptionGetInnerException(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return runtime.Value{}, fmt.Errorf("bcl: System.Exception.get_InnerException expects a receiver")
	}
	ex, ok := args[0].Obj.Native.(*runtime.ManagedException)
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: receiver is not an Exception")
	}
	if ex.Inner == nil {
		return runtime.Null(), nil
	}
	return runtime.ObjRef(ex.Inner), nil
}

// exceptionGetParamName backs ArgumentException/ArgumentNullException/
// ArgumentOutOfRangeException's own ParamName — "" for any other real
// exception type or ctor overload that never captured one (matching real
// ParamName's own null-becomes-empty-observable behavior isn't quite
// right — real ParamName is nullable and defaults to null, not "" — but
// no target-package caller found compares it against null specifically,
// only interpolates/concatenates it, where "" and null already print the
// same).
func exceptionGetParamName(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return runtime.Value{}, fmt.Errorf("bcl: ArgumentException.get_ParamName expects a receiver")
	}
	ex, ok := args[0].Obj.Native.(*runtime.ManagedException)
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: receiver is not an Exception")
	}
	return runtime.String(ex.ParamName), nil
}

// exceptionGetData lazily allocates ex.Data's backing dictionary on first
// access — see ManagedException.Data's own doc comment for why this is
// cached on the exception itself rather than rebuilt (and silently
// losing every previous write) on every single .Data access. Backed by
// the exact same nativeDict Hashtable already uses: Data's real static
// type is the non-generic System.Collections.IDictionary, which every
// Hashtable method here already implements against string-encoded keys
// (encodeDictKey), and Machine.call's virtual-dispatch ancestor walk
// (internal/interpreter/calls.go) resolves an IDictionary-declared call
// site against the receiver's real concrete NativeTypeName regardless —
// so tagging this dictionary "System.Collections.Hashtable" is enough
// for every IDictionary member a real caller here uses (indexer get/set,
// Count) to just work, with no separate IDictionary-specific
// registration needed.
func exceptionGetData(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return runtime.Value{}, fmt.Errorf("bcl: System.Exception.get_Data expects a receiver")
	}
	ex, ok := args[0].Obj.Native.(*runtime.ManagedException)
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: receiver is not an Exception")
	}
	if ex.Data == nil {
		ex.Data = &runtime.Object{Native: &nativeDict{m: map[string]dictEntry{}, typeName: "System.Collections.Hashtable"}}
	}
	return runtime.ObjRef(ex.Data), nil
}

// aggregateExceptionCtor covers AggregateException's real (params
// Exception[])/(IEnumerable<Exception>) constructor shapes uniformly: a
// `new AggregateException("msg", ex1, ex2)` call site always collapses
// its trailing `params Exception[]` arguments into one real array by the
// time this native sees them (the compiler's job, not this native's), so
// scanning every argument's actual Kind — a lone Exception-typed
// argument, or an array of them — covers both real overloads without
// needing to tell them apart. Message defaults to the real BCL's own
// "One or more errors occurred." when none is given (the parameterless
// and IEnumerable-only overloads both use it).
func aggregateExceptionCtor(args []runtime.Value) (*runtime.Object, error) {
	msg := "One or more errors occurred."
	var inners []*runtime.Object
	collect := func(v runtime.Value) {
		if v.Kind == runtime.KindObject && v.Obj != nil {
			if _, ok := v.Obj.Native.(*runtime.ManagedException); ok {
				inners = append(inners, v.Obj)
			}
		}
	}
	for _, a := range args {
		switch a.Kind {
		case runtime.KindString:
			msg = a.Str
		case runtime.KindArray:
			if a.Arr != nil {
				for _, e := range a.Arr.Elems {
					collect(e)
				}
			}
		case runtime.KindObject:
			collect(a)
		}
	}
	var first *runtime.Object
	if len(inners) > 0 {
		first = inners[0]
	}
	return &runtime.Object{Native: &runtime.ManagedException{
		TypeName:        "System.AggregateException",
		Message:         msg,
		Inner:           first,
		InnerExceptions: inners,
	}}, nil
}

func aggregateExceptionGetInnerExceptions(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return runtime.Value{}, fmt.Errorf("bcl: AggregateException.InnerExceptions expects a receiver")
	}
	ex, ok := args[0].Obj.Native.(*runtime.ManagedException)
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: receiver is not an AggregateException")
	}
	elems := make([]runtime.Value, len(ex.InnerExceptions))
	for i, o := range ex.InnerExceptions {
		elems[i] = runtime.ObjRef(o)
	}
	// A real ReadOnlyCollection<Exception> — NewListValue's List`1 shape
	// is a superset (foreach/Count/indexer all work identically), and no
	// real caller here mutates it back, so the missing read-only
	// enforcement is unobservable.
	return NewListValue(elems), nil
}

// flattenInnerExceptions implements Flatten()'s own recursive descent:
// any nested AggregateException among inners contributes its OWN (already
// flattened) inner exceptions instead of itself, exactly matching real
// AggregateException.Flatten's documented behavior of collapsing a whole
// tree of nested AggregateExceptions into one flat list with no
// AggregateException instances left inside it at all.
func flattenInnerExceptions(inners []*runtime.Object) []*runtime.Object {
	var out []*runtime.Object
	for _, o := range inners {
		if ex, ok := o.Native.(*runtime.ManagedException); ok && ex.TypeName == "System.AggregateException" {
			out = append(out, flattenInnerExceptions(ex.InnerExceptions)...)
			continue
		}
		out = append(out, o)
	}
	return out
}

func aggregateExceptionFlatten(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 || args[0].Kind != runtime.KindObject || args[0].Obj == nil {
		return runtime.Value{}, fmt.Errorf("bcl: AggregateException.Flatten expects a receiver")
	}
	ex, ok := args[0].Obj.Native.(*runtime.ManagedException)
	if !ok {
		return runtime.Value{}, fmt.Errorf("bcl: receiver is not an AggregateException")
	}
	flat := flattenInnerExceptions(ex.InnerExceptions)
	var first *runtime.Object
	if len(flat) > 0 {
		first = flat[0]
	}
	return runtime.ObjRef(&runtime.Object{Native: &runtime.ManagedException{
		TypeName:        "System.AggregateException",
		Message:         ex.Message,
		Inner:           first,
		InnerExceptions: flat,
	}}), nil
}
