package bcl

import (
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/arturoeanton/go-vmnet/internal/runtime"
)

// System.Net.Http support (Fase 3.82) — the first real, host-visible
// network access this project has ever implemented, gated by
// Permissions.AllowNetwork (internal/interpreter/permissions.go's own
// gateNetwork), previously reserved but unenforced.
//
// Modeled narrowly, scoped to the exact real corpus need found:
// ClosedXML's own netstandard2.0 PolyfillExtensions shim implements the
// newer HttpClient.GetByteArrayAsync/GetStreamAsync/GetStringAsync
// convenience methods (not natively available pre-.NET Standard 2.1) by
// funneling all three through `client.GetAsync(uri)` then
// `response.EnsureSuccessStatusCode()` and one of
// `response.Content.ReadAsByteArrayAsync/ReadAsStreamAsync/
// ReadAsStringAsync()`. No POST/PUT, no request headers, no
// HttpRequestMessage construction — none of those appear anywhere in the
// certified corpus today, so none are implemented; a real caller needing
// them gets the ordinary "unsupported BCL method" error, not a silent
// wrong answer.
//
// Every request runs synchronously to completion before GetAsync
// returns, matching this project's own synchronous async model
// throughout (system_task.go's own doc comment) — a real compiler-
// generated async state machine's MoveNext() still runs start-to-finish
// in one call, needing no interpreter changes.
type nativeHttpClient struct {
	client *http.Client
}

// nativeHttpResponseMessage backs HttpResponseMessage — statusCode is the
// real numeric HTTP status (no distinct HttpStatusCode enum modeled;
// every caller found so far only checks IsSuccessStatusCode/
// EnsureSuccessStatusCode, matching this project's usual "plain int for
// a BCL enum with no TypeDef to resolve against" posture elsewhere).
type nativeHttpResponseMessage struct {
	statusCode int
	content    runtime.Value
}

// nativeHttpContent backs HttpContent — the real response body, read
// eagerly into memory the moment GetAsync's real HTTP round trip
// completes (there is no real streaming here: every ReadAs*Async below
// just hands back a view of the same already-buffered bytes).
type nativeHttpContent struct {
	body []byte
}

func init() {
	registerCtor("System.Net.Http.HttpClient", newHttpClientCtor)
	register("System.Net.Http.HttpClient::GetAsync", true, httpClientGetAsync)
	register("System.Net.Http.HttpClient::Dispose", false, httpClientDispose)

	register("System.Net.Http.HttpResponseMessage::get_StatusCode", true, httpResponseGetStatusCode)
	register("System.Net.Http.HttpResponseMessage::get_IsSuccessStatusCode", true, httpResponseGetIsSuccessStatusCode)
	register("System.Net.Http.HttpResponseMessage::get_Content", true, httpResponseGetContent)
	register("System.Net.Http.HttpResponseMessage::EnsureSuccessStatusCode", true, httpResponseEnsureSuccessStatusCode)
	register("System.Net.Http.HttpResponseMessage::Dispose", false, httpDisposeNoop)

	register("System.Net.Http.HttpContent::ReadAsStringAsync", true, httpContentReadAsStringAsync)
	register("System.Net.Http.HttpContent::ReadAsByteArrayAsync", true, httpContentReadAsByteArrayAsync)
	register("System.Net.Http.HttpContent::ReadAsStreamAsync", true, httpContentReadAsStreamAsync)
	register("System.Net.Http.HttpContent::Dispose", false, httpDisposeNoop)
}

func newHttpClientCtor(args []runtime.Value) (*runtime.Object, error) {
	return &runtime.Object{Native: &nativeHttpClient{
		client: &http.Client{Timeout: 100 * time.Second},
	}}, nil
}

func httpClientOf(v runtime.Value) (*nativeHttpClient, bool) {
	v = derefReceiver(v)
	if v.Kind != runtime.KindObject || v.Obj == nil {
		return nil, false
	}
	c, ok := v.Obj.Native.(*nativeHttpClient)
	return c, ok
}

// httpClientGetAsync performs a REAL, blocking GET request — gated by
// Permissions.AllowNetwork (internal/interpreter/calls.go's own tryCall,
// before this ever runs) — and returns an already-completed
// Task<HttpResponseMessage> (NewCompletedTask, system_task.go), or an
// already-faulted one (NewFaultedTask) wrapping a real
// HttpRequestException on any transport-level failure (DNS, connection
// refused, timeout, ...) — never a Go panic or a bare Go error escaping
// into interpreted code.
func httpClientGetAsync(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 || args[1].Kind != runtime.KindString {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ArgumentNullException", Message: "requestUri"}
	}
	c, ok := httpClientOf(args[0])
	if !ok {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ObjectDisposedException", Message: "HttpClient"}
	}
	resp, err := c.client.Get(args[1].Str)
	if err != nil {
		return NewFaultedTask(&runtime.ManagedException{TypeName: "System.Net.Http.HttpRequestException", Message: err.Error()}), nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return NewFaultedTask(&runtime.ManagedException{TypeName: "System.Net.Http.HttpRequestException", Message: err.Error()}), nil
	}
	msg := &nativeHttpResponseMessage{
		statusCode: resp.StatusCode,
		content:    runtime.ObjRef(&runtime.Object{Native: &nativeHttpContent{body: body}}),
	}
	return NewCompletedTask(runtime.ObjRef(&runtime.Object{Native: msg}), true), nil
}

func httpClientDispose(args []runtime.Value) (runtime.Value, error) {
	// Go's http.Client owns no real disposable resource of its own (its
	// Transport pools connections independently of the Client value) —
	// matching real .NET's own well-known "HttpClient is meant to be
	// reused, Dispose is close to a no-op for the default handler"
	// behavior closely enough for every real corpus caller found so far.
	return runtime.Value{}, nil
}

func httpResponseOf(v runtime.Value) (*nativeHttpResponseMessage, bool) {
	v = derefReceiver(v)
	if v.Kind != runtime.KindObject || v.Obj == nil {
		return nil, false
	}
	r, ok := v.Obj.Native.(*nativeHttpResponseMessage)
	return r, ok
}

func httpResponseGetStatusCode(args []runtime.Value) (runtime.Value, error) {
	r, ok := httpResponseOf(firstArg(args))
	if !ok {
		return runtime.Int32(0), nil
	}
	return runtime.Int32(int32(r.statusCode)), nil
}

func httpResponseGetIsSuccessStatusCode(args []runtime.Value) (runtime.Value, error) {
	r, ok := httpResponseOf(firstArg(args))
	if !ok {
		return runtime.Bool(false), nil
	}
	return runtime.Bool(r.statusCode >= 200 && r.statusCode <= 299), nil
}

func httpResponseGetContent(args []runtime.Value) (runtime.Value, error) {
	r, ok := httpResponseOf(firstArg(args))
	if !ok {
		return runtime.Null(), nil
	}
	return r.content, nil
}

// httpResponseEnsureSuccessStatusCode returns the receiver unchanged on a
// 2xx status (matching real .NET's own `EnsureSuccessStatusCode()`
// fluent-return signature), or throws a real HttpRequestException
// otherwise.
func httpResponseEnsureSuccessStatusCode(args []runtime.Value) (runtime.Value, error) {
	receiver := firstArg(args)
	r, ok := httpResponseOf(receiver)
	if !ok {
		return runtime.Value{}, &runtime.ManagedException{TypeName: "System.ObjectDisposedException", Message: "HttpResponseMessage"}
	}
	if r.statusCode < 200 || r.statusCode > 299 {
		return runtime.Value{}, &runtime.ManagedException{
			TypeName: "System.Net.Http.HttpRequestException",
			Message:  http.StatusText(r.statusCode) + " (" + strconv.Itoa(r.statusCode) + ")",
		}
	}
	return receiver, nil
}

func httpContentOf(v runtime.Value) (*nativeHttpContent, bool) {
	v = derefReceiver(v)
	if v.Kind != runtime.KindObject || v.Obj == nil {
		return nil, false
	}
	c, ok := v.Obj.Native.(*nativeHttpContent)
	return c, ok
}

func httpContentReadAsStringAsync(args []runtime.Value) (runtime.Value, error) {
	c, ok := httpContentOf(firstArg(args))
	if !ok {
		return NewCompletedTask(runtime.String(""), true), nil
	}
	return NewCompletedTask(runtime.String(string(c.body)), true), nil
}

func httpContentReadAsByteArrayAsync(args []runtime.Value) (runtime.Value, error) {
	c, ok := httpContentOf(firstArg(args))
	if !ok {
		return NewCompletedTask(bytesToArrayValue(nil), true), nil
	}
	return NewCompletedTask(bytesToArrayValue(c.body), true), nil
}

func httpContentReadAsStreamAsync(args []runtime.Value) (runtime.Value, error) {
	c, ok := httpContentOf(firstArg(args))
	if !ok {
		return NewCompletedTask(NewMemoryStreamValue(nil), true), nil
	}
	return NewCompletedTask(NewMemoryStreamValue(c.body), true), nil
}

func httpDisposeNoop(args []runtime.Value) (runtime.Value, error) {
	return runtime.Value{}, nil
}
