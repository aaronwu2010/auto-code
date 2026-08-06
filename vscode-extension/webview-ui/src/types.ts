// Webview 端共享类型定义。
// 与 Go 端 internal/server/bindings_adapter.go 的 JSON 输出一致。

export interface Message {
  id: string;
  role: "user" | "assistant" | "system" | "tool";
  content: string;
  content_blocks?: ContentBlock[];
  thinking?: string;
  model?: string;
  timestamp: number;
  is_meta?: boolean;
  uuid?: string;
  tool_calls?: ToolCallState[];
}

export interface ToolCallState {
  id?: string;
  name?: string;
  input?: string | Record<string, unknown>;
}

export interface ContentBlock {
  type: "text" | "tool_use" | "tool_result" | "image" | "thinking";
  text?: string;
  tool_use_id?: string;
  tool_name?: string;
  tool_input?: string | Record<string, unknown>;
  tool_output?: string;
  is_error?: boolean;
  thinking?: string;
}

export interface AppStateSnapshot {
  mainLoopModel: string;
  isProcessing: boolean;
  currentToolUse?: ToolUseState;
  statusLineText: string;
  remoteConnectionStatus: string;
  thinkingEnabled: boolean;
  fastMode: boolean;
  tasks: Record<string, TaskState>;
  todos: Record<string, TodoState>;
  mcp: MCPState;
  toolPermissionMode: string;
}

export interface ToolUseState {
  tool_name: string;
  tool_use_id: string;
  input?: unknown;
  status: string;
}

export interface TaskState {
  id: string;
  type: string;
  status: string;
  command?: string;
  output?: string;
  exit_code?: number;
  agent_id?: string;
}

export interface TodoState {
  id: string;
  content: string;
  status: string;
}

export interface MCPState {
  clients: MCPServerState[];
  tools: unknown[];
  commands: unknown[];
  resources: Record<string, unknown>;
}

export interface MCPServerState {
  name: string;
  status: string;
  type: string;
}

export interface OllamaConfig {
  base_url: string;
  api_key: string;
  model: string;
}

export interface OllamaHealth {
  connected: boolean;
  error?: string;
  is_local: boolean;
  base_url: string;
  model: string;
  available_models: number;
}

export interface ModelInfo {
  name: string;
  size?: string;
  family?: string;
  parameter_size?: string;
  quantization?: string;
  context_length?: number;
}

// SDK 消息（query:message 事件 data 字段）
export interface SDKMessage {
  type: string;
  subtype?: string;
  message?: Message;
  session_id?: string;
  data?: unknown;
}

export interface StateChangeEvent {
  type: string;
  key?: string;
  value?: unknown;
}
