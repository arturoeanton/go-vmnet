package runtime

import "fmt"

// ManagedException is a thrown CIL exception object, surfaced to Go as a
// plain error (spec §18.4 ManagedExceptionError, simplified: Fase 2 only
// supports unhandled throw — see docs/en/ROADMAP.md, try/catch/finally are
// deferred).
type ManagedException struct {
	TypeName string
	Message  string
	// Inner is the real Exception object passed to a
	// `new SomeException(message, innerException)`-shaped constructor
	// (real .NET's Exception.InnerException) — nil when none was given.
	// Kept as the original *Object (not just its ManagedException) so
	// `.InnerException` returns something every other Exception member
	// (Message, GetType, ToString, ...) still works against transparently.
	Inner *Object

	// Object is the real *Object the `throw` opcode actually threw (set
	// there, once, the first time this exception is thrown — see
	// internal/interpreter/eval.go's ir.Throw case), i.e. the SAME object
	// newobj originally allocated: for a plugin exception subclass, that
	// object's own Type/Fields carry its extra members (`class
	// MyException : Exception { public int Code; }`) alongside this
	// ManagedException in its Native slot (see baseExceptionCtorInPlace's
	// doc comment, internal/bcl/system_exception.go, for why one Object
	// legitimately carries both). Nil for an exception a BCL native
	// constructed directly as a Go error (most FormatException/
	// ArgumentException faults raised deep inside a native without ever
	// going through a real `newobj`+`throw`) — those have no backing
	// Object at all. internal/interpreter/exceptions.go's exceptionValue
	// is the one place that reads this: a `catch (MyException e)` clause
	// needs `e` to be this original object (so `e.Code` resolves), not a
	// bare freshly-allocated wrapper that only knows Message/TypeName.
	Object *Object

	// Data backs the real Exception.Data property: an IDictionary every
	// exception instance carries whether or not anything ever populates
	// it (`ex.Data["key"] = value` is legal on ANY caught exception, not
	// just a custom subclass's own declared fields). nil until the first
	// System.Exception.get_Data call, which lazily allocates a real
	// Hashtable-shaped *Object into this slot and caches it here so a
	// later access sees the same dictionary (and its mutations), not a
	// fresh empty one every time — bcl.exceptionGetData (system_
	// exception.go) is the only thing that ever sets this; ManagedException
	// itself has no dictionary type of its own to avoid an import cycle
	// (bcl already imports runtime, not vice versa).
	Data *Object

	// ParamName backs ArgumentException/ArgumentNullException/
	// ArgumentOutOfRangeException's own ParamName property — their
	// (message, paramName) constructor overload's second parameter is a
	// plain string, not an inner Exception (see innerExceptionArg's own
	// doc comment, internal/bcl/system_exception.go, for how the two
	// (message, X) shapes are told apart at construction time). "" for
	// every other exception type, and for these three when that
	// constructor overload wasn't the one used.
	ParamName string

	// InnerExceptions is System.AggregateException's own real plural
	// fault list — set only by bcl.aggregateExceptionCtor/Flatten, nil for
	// every other exception type (which only ever has Inner, singular).
	// Inner itself is still set to InnerExceptions[0] when non-empty, so
	// the ordinary Exception.InnerException getter keeps working
	// transparently on an AggregateException too, matching real
	// AggregateException's own documented behavior of exposing its first
	// fault through the singular property as well.
	InnerExceptions []*Object
}

func (e *ManagedException) Error() string {
	msg := e.TypeName
	if e.Message != "" {
		msg = fmt.Sprintf("%s: %s", e.TypeName, e.Message)
	}
	if e.Inner != nil {
		if inner, ok := e.Inner.Native.(*ManagedException); ok {
			msg += " ---> " + inner.Error()
		}
	}
	return msg
}
