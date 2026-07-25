package godotconn

import (
	"encoding/json"

	"github.com/mrf/godot-stagehand/internal/gwp"
)

// Request is a JSON-RPC 2.0 request sent to the Godot addon.
type Request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response received from the Godot addon.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	return e.Message
}

// ErrorData is the payload the addon puts in a handler failure's `error.data`.
// It carries the machine-readable kind, the method and selector the call
// targeted, and whatever structured context the handler could supply — so a
// frontend never has to parse the human-readable message to react. See
// addons/stagehand/core/errors.gd and docs/error-model.md.
type ErrorData struct {
	ErrorCode string         `json:"error_code"`
	Method    string         `json:"method"`
	Selector  string         `json:"selector,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
}

// StructuredData decodes the addon's canonical `error.data` payload. It reports
// false when the error carries no data, or data this build does not recognise —
// an older addon, or a protocol-level fault such as a parse error, which the
// caller must still be able to report.
func (e *RPCError) StructuredData() (ErrorData, bool) {
	if e == nil || len(e.Data) == 0 {
		return ErrorData{}, false
	}
	var data ErrorData
	if err := json.Unmarshal(e.Data, &data); err != nil {
		return ErrorData{}, false
	}
	if data.ErrorCode == "" && data.Method == "" {
		return ErrorData{}, false
	}
	return data, true
}

// Failure adapts a JSON-RPC error object into the shared rendering type every
// frontend formats failures through. fallbackMethod is used when the addon did
// not name the method itself — a protocol-level fault, or an addon predating
// the canonical error model.
func (e *RPCError) Failure(fallbackMethod string) gwp.RemoteFailure {
	failure := gwp.RemoteFailure{Method: fallbackMethod, Message: e.Message}
	data, ok := e.StructuredData()
	if !ok {
		return failure
	}
	if data.Method != "" {
		failure.Method = data.Method
	}
	failure.Selector = data.Selector
	failure.Code = data.ErrorCode
	failure.Details = data.Details
	return failure
}

// Standard JSON-RPC 2.0 error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603

	CodeAuthenticationRequired = -32001
	CodeAuthenticationFailed   = -32002
	CodeUnsafeCapability       = -32003

	// Server-defined codes the addon reports for handler failures. They mirror
	// StagehandErrors.json_rpc_code in addons/stagehand/core/errors.gd; the
	// fine-grained kind travels as ErrorData.ErrorCode alongside them.

	// CodeTargetNotFound: the request was well formed but its target — a node,
	// property, method, or scene — does not exist.
	CodeTargetNotFound = -32004
	// CodeTimeout: a wait, poll, or capture gave up before its condition held.
	CodeTimeout = -32005
	// CodeHandlerError: any other addon-side handler failure.
	CodeHandlerError = -32006
)

func newRequest(id int64, method string, params any) *Request {
	return &Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
}
