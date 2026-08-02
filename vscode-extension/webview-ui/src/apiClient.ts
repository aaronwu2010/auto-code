import type {
  AppStateSnapshot,
  OllamaConfig,
  OllamaHealth,
  ModelInfo,
  Message,
  SDKMessage,
  StateChangeEvent,
} from "./types";

/**
 * apiClient 封装 Webview <-> 扩展主进程的通信。
 *
 * 通信方向：
 *   请求：Webview postMessage {type:"request", id, method, params} -> 扩展主进程 -> Go server
 *   响应：扩展主进程 postMessage {type:"response", id, result|error} -> Webview
 *   事件：扩展主进程 postMessage {type:"event", event, data} -> Webview
 *   工作区：扩展主进程 postMessage {type:"workspace", dir} -> Webview
 */

type VsCodeApi = {
  postMessage(message: unknown): void;
  getState<T>(): T;
  setState<T>(state: T): void;
};

declare global {
  interface Window {
    acquireVsCodeApi(): VsCodeApi;
  }
}

let vscodeApi: VsCodeApi | undefined;
function getApi(): VsCodeApi {
  if (!vscodeApi) {
    vscodeApi = window.acquireVsCodeApi();
  }
  return vscodeApi;
}

const pending = new Map<string, {
  resolve: (value: unknown) => void;
  reject: (err: Error) => void;
}>();
let nextId = 1;

type EventHandler = (data: unknown) => void;
type WorkspaceHandler = (dir: string) => void;
const eventHandlers = new Map<string, Set<EventHandler>>();
const workspaceHandlers = new Set<WorkspaceHandler>();

// 全局监听一次 message 事件，分发响应与事件
if (typeof window !== "undefined") {
  window.addEventListener("message", (e: MessageEvent) => {
    const msg = e.data;
    if (!msg || typeof msg !== "object") return;

    if (msg.type === "response" && typeof msg.id === "string") {
      const entry = pending.get(msg.id);
      if (!entry) return;
      pending.delete(msg.id);
      if (msg.error) {
        entry.reject(new Error(String(msg.error)));
      } else {
        entry.resolve(msg.result);
      }
      return;
    }

    if (msg.type === "event" && typeof msg.event === "string") {
      const set = eventHandlers.get(msg.event);
      if (set) {
        set.forEach((h) => h(msg.data));
      }
      return;
    }

    if (msg.type === "workspace" && typeof msg.dir === "string") {
      workspaceHandlers.forEach((h) => h(msg.dir));
    }
  });
}

/** 发起一次请求，等待响应。 */
function request<T = unknown>(method: string, params?: unknown, timeoutMs = 30000): Promise<T> {
  const id = `webview-${nextId++}`;
  return new Promise<T>((resolve, reject) => {
    const timer = setTimeout(() => {
      pending.delete(id);
      reject(new Error(`请求超时: ${method}`));
    }, timeoutMs);

    pending.set(id, {
      resolve: (v) => {
        clearTimeout(timer);
        resolve(v as T);
      },
      reject: (err) => {
        clearTimeout(timer);
        reject(err);
      },
    });

    getApi().postMessage({ type: "request", id, method, params: params ?? {} });
  });
}

/** 监听一个事件。返回取消监听函数。 */
function onEvent(event: string, handler: EventHandler): () => void {
  let set = eventHandlers.get(event);
  if (!set) {
    set = new Set();
    eventHandlers.set(event, set);
  }
  set.add(handler);
  return () => set!.delete(handler);
}

/** 监听工作区目录变化。 */
function onWorkspaceChange(handler: WorkspaceHandler): () => void {
  workspaceHandlers.add(handler);
  return () => workspaceHandlers.delete(handler);
}

/** 请求当前工作区目录。 */
function requestWorkspace(): void {
  getApi().postMessage({ type: "getWorkspace" });
}

// ===== 类型化 API 表面（与 Go server 方法一一对应）=====

export const api = {
  // ===== 消息 =====
  sendMessage(prompt: string): Promise<{ success: boolean; error?: string; session_id?: string }> {
    return request("send_message", { prompt });
  },
  interrupt(): Promise<{ ok: boolean }> {
    return request("interrupt", {});
  },
  getMessages(): Promise<{ messages: Message[] }> {
    return request("get_messages", {});
  },

  // ===== 应用状态 =====
  getAppState(): Promise<AppStateSnapshot> {
    return request("get_app_state", {});
  },
  setModel(model: string): Promise<{ ok: boolean }> {
    return request("set_model", { model });
  },
  setThinking(enabled: boolean): Promise<{ ok: boolean }> {
    return request("set_thinking", { enabled });
  },
  setFastMode(enabled: boolean): Promise<{ ok: boolean }> {
    return request("set_fast_mode", { enabled });
  },
  setPermissionMode(mode: string): Promise<{ ok: boolean }> {
    return request("set_permission_mode", { mode });
  },
  getSessionId(): Promise<{ session_id: string }> {
    return request("get_session_id", {});
  },

  // ===== Ollama =====
  setOllamaConfig(cfg: OllamaConfig): Promise<{ ok: boolean }> {
    return request("set_ollama_config", cfg);
  },
  getOllamaConfig(): Promise<OllamaConfig> {
    return request("get_ollama_config", {});
  },
  listModels(): Promise<{ models: ModelInfo[]; error?: string }> {
    return request("list_models", {});
  },
  checkHealth(): Promise<OllamaHealth> {
    return request("check_health", {});
  },

  // ===== 工具 =====
  getAvailableTools(): Promise<
    Array<{ name: string; description: string; isReadOnly: boolean; isEnabled: boolean }>
  > {
    return request("get_available_tools", {});
  },
  refreshContext(): Promise<{ ok: boolean }> {
    return request("refresh_context", {});
  },

  // ===== 工作区 =====
  setWorkspace(dir: string): Promise<{ ok: boolean }> {
    return request("set_workspace", { dir });
  },
  getWorkspace(): Promise<{ dir: string }> {
    return request("get_workspace", {});
  },

  // ===== 事件订阅 =====
  onQueryMessage(handler: (data: SDKMessage) => void): () => void {
    return onEvent("query:message", handler as EventHandler);
  },
  onStateChange(handler: (data: StateChangeEvent) => void): () => void {
    return onEvent("state:change", handler as EventHandler);
  },
  onWorkspaceChange,

  // ===== 初始化 =====
  requestWorkspace,
};
