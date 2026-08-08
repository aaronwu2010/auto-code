package server

import "encoding/json"

// protocol.go 定义 stdio NDJSON-RPC 通信协议的类型。
//
// 协议说明：
//   - 每行一个 JSON 对象（NDJSON）。
//   - 请求/响应（带 id）：前端 <-> Go 双向配对。
//   - 事件（带 event，无 id）：Go -> 前端单向推送，用于流式消息与状态变更。

// Request 是前端发往 Go 端的请求。
type Request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response 是 Go 端对 Request 的响应。
type Response struct {
	ID     string      `json:"id"`
	Result interface{} `json:"result,omitempty"`
	Error  *RPCError   `json:"error,omitempty"`
}

// Event 是 Go 端主动推送的事件。
type Event struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data"`
}

// RPCError 描述一次方法调用失败。
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error 实现 error 接口。
func (e *RPCError) Error() string {
	return e.Message
}

// NewError 构造一个 RPCError。
func NewError(code int, msg string) *RPCError {
	return &RPCError{Code: code, Message: msg}
}

// 标准错误码。
const (
	CodeInternal       = -32603
	CodeInvalidParam   = -32602
	CodeMethodNotFound = -32601
)

// validMethods 列出所有已知的合法请求方法。
var validMethods = map[string]bool{
	"send_message":        true,
	"interrupt":           true,
	"get_messages":        true,
	"get_app_state":       true,
	"set_model":           true,
	"set_thinking":        true,
	"set_fast_mode":       true,
	"set_permission_mode": true,
	"set_ollama_config":   true,
	"get_ollama_config":   true,
	"list_models":         true,
	"get_context_usage":   true,
	"check_health":        true,
	"get_session_id":      true,
	"get_available_tools": true,
	"refresh_context":     true,
	"set_workspace":       true,
	"get_workspace":       true,
}

// ValidateMethod 检查请求方法是否为已知合法方法。
func ValidateMethod(method string) bool {
	return validMethods[method]
}
