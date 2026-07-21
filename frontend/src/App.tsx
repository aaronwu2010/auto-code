import { useState, useEffect, useRef, useCallback } from "react";
// 导入 Wails 生成的绑定函数和类型
import {
  SendMessage,
  Interrupt,
  GetMessages,
  GetAppState,
  SetOllamaConfig,
  GetOllamaConfig,
  ListAvailableModels,
  CheckOllamaHealth,
} from "../wailsjs/go/state/WailsBindings";
import { state, types } from "../wailsjs/go/models";

// 导入 Wails 运行时
import { EventsOn, EventsOff } from "../wailsjs/runtime/runtime";

// 本地类型别名
type Message = types.Message;
type ContentBlock = types.ContentBlock;
type AppStateSnapshot = state.AppStateSnapshot;
type OllamaConfig = state.OllamaConfigRequest;
type OllamaHealth = state.OllamaHealthResponse;
type ModelInfo = state.ModelInfoUI;

interface SDKMessage {
  type: string;
  subtype?: string;
  message?: Message;
  session_id?: string;
  data?: unknown;
}

interface StateChangeEvent {
  type: string;
  key?: string;
  value?: unknown;
}

function App() {
  const [messages, setMessages] = useState<Message[]>([]);
  const [input, setInput] = useState("");
  const [isLoading, setIsLoading] = useState(false);
  const [appState, setAppState] = useState<AppStateSnapshot | null>(null);
  const [statusText, setStatusText] = useState("");
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);

  // 设置面板状态
  const [showSettings, setShowSettings] = useState(false);
  const [models, setModels] = useState<ModelInfo[]>([]);
  const [ollamaConfig, setOllamaConfig] = useState<OllamaConfig>({
    base_url: "http://localhost:11434/api",
    api_key: "",
    model: "",
  });
  const [ollamaHealth, setOllamaHealth] = useState<OllamaHealth | null>(null);
  const [loadingModels, setLoadingModels] = useState(false);
  const [modelsError, setModelsError] = useState<string | null>(null);
  const [healthCheckResult, setHealthCheckResult] = useState<string | null>(null);

  const scrollToBottom = useCallback(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, []);

  useEffect(() => {
    scrollToBottom();
  }, [messages, scrollToBottom]);

  // 当打开设置面板时自动加载模型列表
  useEffect(() => {
    if (showSettings && models.length === 0 && !loadingModels) {
      loadModels();
    }
  }, [showSettings]);

  // 加载配置和模型列表
  const loadConfig = async () => {
    try {
      const config = await GetOllamaConfig();
      if (config) {
        setOllamaConfig(config);
      }
    } catch {}
  };

  const loadModels = async () => {
    setLoadingModels(true);
    setModelsError(null);
    try {
      const result = await ListAvailableModels();
      console.log("ListAvailableModels result:", result);
      if (result) {
        if (result.models && result.models.length > 0) {
          console.log("Found models:", result.models);
          setModels(result.models);
          setModelsError(null);
        } else if (result.error) {
          console.log("Error from backend:", result.error);
          setModelsError(result.error);
          setModels([]);
        } else {
          setModelsError("未找到模型，请确保 Ollama 服务正在运行并已下载模型");
          setModels([]);
        }
      }
    } catch (err) {
      console.error("loadModels error:", err);
      setModelsError("加载模型列表失败: " + String(err));
      setModels([]);
    }
    setLoadingModels(false);
  };

  const checkHealth = async () => {
    console.log("checkHealth: 开始检查连接...");
    setHealthCheckResult("正在检查连接...");
    try {
      const health = await CheckOllamaHealth();
      console.log("checkHealth: 收到结果", health);
      if (health) {
        setOllamaHealth(health);
        if (health.connected) {
          const msg = `✓ 已连接到 ${health.base_url}，发现 ${health.available_models || 0} 个模型`;
          console.log(`checkHealth: ${msg}`);
          setHealthCheckResult(msg);
        } else {
          const msg = `✗ 连接失败: ${health.error || "未知错误"}`;
          console.log(`checkHealth: ${msg}`);
          setHealthCheckResult(msg);
        }
      } else {
        console.error("checkHealth: 返回结果为空");
        setHealthCheckResult("✗ 检查失败: 返回结果为空");
      }
    } catch (err) {
      console.error("checkHealth: 检查失败", err);
      setHealthCheckResult(`✗ 检查失败: ${String(err)}`);
    }
  };

  const saveConfig = async () => {
    try {
      await SetOllamaConfig(ollamaConfig);
      await checkHealth();
      await loadModels();
    } catch {}
  };

  useEffect(() => {
    EventsOn("state:change", (data: unknown) => {
      try {
        const event: StateChangeEvent =
          typeof data === "string" ? JSON.parse(data) : (data as StateChangeEvent);
        if (event.type === "status_update") {
          setStatusText(event.value as string);
        }
        if (event.type === "processing_update") {
          setIsLoading(event.value as boolean);
        }
      } catch {}
    });

    EventsOn("query:message", (data: unknown) => {
      try {
        const msg: SDKMessage =
          typeof data === "string" ? JSON.parse(data) : (data as SDKMessage);
        if (msg.message) {
          setMessages((prev) => {
            const exists = prev.some((m) => m.id === msg.message!.id);
            if (exists) return prev;
            return [...prev, msg.message!];
          });
        }
        if (msg.type === "result") {
          setIsLoading(false);
        }
        if (msg.type === "error") {
          setIsLoading(false);
        }
      } catch {}
    });

    loadInitialState();

    return () => {
      EventsOff("state:change");
      EventsOff("query:message");
    };
  }, []);

  const loadInitialState = async () => {
    try {
      const state = await GetAppState();
      if (state) {
        setAppState(state);
      }
      const msgs = await GetMessages();
      if (msgs) {
        setMessages(msgs.messages || []);
      }
      await loadConfig();
      await checkHealth();
      await loadModels();
    } catch {}
  };

  const handleSubmit = async () => {
    if (!input.trim() || isLoading) return;

    // 用户消息会通过事件从后端返回，这里不需要手动添加
    setInput("");
    setIsLoading(true);

    try {
      await SendMessage({ prompt: input });
    } catch (err) {
      setIsLoading(false);
    }
  };

  const handleInterrupt = () => {
    Interrupt();
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSubmit();
    }
  };

  const renderContentBlock = (block: ContentBlock, idx: number) => {
    switch (block.type) {
      case "text":
        return (
          <pre key={idx} className="whitespace-pre-wrap break-words text-sm">
            {block.text}
          </pre>
        );
      case "tool_use":
        return (
          <div
            key={idx}
            className="bg-[#1e3a5f] border border-[#2a4a6f] rounded px-3 py-2 my-1 text-sm"
          >
            <span className="text-[#6cb6ff] font-bold">
              {block.tool_name}
            </span>
            {block.tool_input && (
              <pre className="text-xs text-[#888] mt-1 overflow-x-auto">
                {typeof block.tool_input === "string"
                  ? block.tool_input
                  : JSON.stringify(block.tool_input, null, 2)}
              </pre>
            )}
          </div>
        );
      case "tool_result":
        return (
          <div
            key={idx}
            className={`rounded px-3 py-2 my-1 text-sm ${
              block.is_error
                ? "bg-[#3d1c1c] border border-[#5a2a2a]"
                : "bg-[#1a2e1a] border border-[#2a4a2a]"
            }`}
          >
            <pre className="whitespace-pre-wrap break-words">
              {block.tool_output}
            </pre>
          </div>
        );
      case "thinking":
        return (
          <details
            key={idx}
            className="bg-[#1a1a2e] border border-[#2a2a4a] rounded px-3 py-2 my-1"
          >
            <summary className="text-xs text-[#888] cursor-pointer">
              Thinking...
            </summary>
            <pre className="whitespace-pre-wrap break-words text-xs text-[#666] mt-2">
              {block.thinking}
            </pre>
          </details>
        );
      default:
        return null;
    }
  };

  const renderMessage = (msg: Message) => {
    const isUser = msg.role === "user";
    const isSystem = msg.role === "system";

    return (
      <div
        key={msg.id}
        className={`mb-3 px-3 py-2 rounded-lg max-w-[90%] ${
          isUser
            ? "bg-[#16213e] ml-auto"
            : isSystem
            ? "bg-[#2a1a2e] border border-[#3a2a4a]"
            : "bg-[#0f3460]"
        }`}
      >
        <div className="text-[11px] text-[#888] mb-1 font-bold uppercase">
          {msg.role}
        </div>
        {/* 渲染 content_blocks 或 content */}
        {msg.content_blocks && msg.content_blocks.length > 0
          ? msg.content_blocks.map((block, i) => renderContentBlock(block, i))
          : <pre className="whitespace-pre-wrap break-words text-sm">{msg.content}</pre>}
      </div>
    );
  };

  return (
    <div className="flex flex-col h-screen bg-[#1a1a2e] text-[#e0e0e0] font-mono">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-2 border-b border-[#2a2a4a] bg-[#16213e]">
        <div className="flex items-center gap-3">
          <span className="text-sm font-bold text-[#6cb6ff]">Auto Code</span>
          {appState?.mainLoopModel && (
            <span className="text-xs text-[#888] bg-[#0f3460] px-2 py-0.5 rounded">
              {appState.mainLoopModel}
            </span>
          )}
          {ollamaHealth && (
            <span className={`text-xs px-2 py-0.5 rounded ${
              ollamaHealth.connected
                ? "bg-[#1a2e1a] text-[#6bff6b]"
                : "bg-[#3d1c1c] text-[#ff6b6b]"
            }`}>
              {ollamaHealth.connected ? "已连接" : "未连接"}
              {ollamaHealth.is_local ? " (本地)" : " (云端)"}
            </span>
          )}
          {appState?.thinkingEnabled && (
            <span className="text-xs text-[#a78bfa] bg-[#2a1a3e] px-2 py-0.5 rounded">
              Thinking
            </span>
          )}
          {appState?.fastMode && (
            <span className="text-xs text-[#fbbf24] bg-[#3a2a0a] px-2 py-0.5 rounded">
              Fast
            </span>
          )}
        </div>
        <div className="flex items-center gap-2">
          {/* 设置按钮 */}
          <button
            onClick={() => setShowSettings(!showSettings)}
            className="text-xs bg-[#0f3460] text-[#6cb6ff] px-3 py-1 rounded hover:bg-[#1a4a80]"
          >
            ⚙️ 设置
          </button>
          {statusText && (
            <span className="text-xs text-[#888]">{statusText}</span>
          )}
          {isLoading && (
            <button
              onClick={handleInterrupt}
              className="text-xs bg-[#5a2a2a] text-[#ff6b6b] px-2 py-1 rounded hover:bg-[#6a3a3a]"
            >
              Stop
            </button>
          )}
        </div>
      </div>

      {/* 设置面板 */}
      {showSettings && (
        <div className="border-b border-[#2a2a4a] bg-[#16213e] p-4">
          <div className="max-w-2xl mx-auto space-y-4">
            <h2 className="text-sm font-bold text-[#6cb6ff] mb-3">Ollama 配置</h2>

            {/* 连接状态 */}
            {ollamaHealth && (
              <div className={`text-xs p-2 rounded ${
                ollamaHealth.connected
                  ? "bg-[#1a2e1a] text-[#6bff6b]"
                  : "bg-[#3d1c1c] text-[#ff6b6b]"
              }`}>
                {ollamaHealth.connected
                  ? `✓ 已连接到 ${ollamaHealth.base_url}`
                  : `✗ 连接失败: ${ollamaHealth.error || "未知错误"}`}
              </div>
            )}

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              {/* Ollama URL */}
              <div>
                <label className="text-xs text-[#888] block mb-1">Ollama URL</label>
                <input
                  type="text"
                  value={ollamaConfig.base_url}
                  onChange={(e) => setOllamaConfig({ ...ollamaConfig, base_url: e.target.value })}
                  placeholder="http://localhost:11434/api"
                  className="w-full bg-[#0f3460] text-[#e0e0e0] border border-[#2a4a6f] rounded px-3 py-2 text-sm outline-none focus:border-[#4a6a9a]"
                />
              </div>

              {/* API Key */}
              <div>
                <label className="text-xs text-[#888] block mb-1">API Key (可选，用于 Ollama Cloud)</label>
                <input
                  type="password"
                  value={ollamaConfig.api_key}
                  onChange={(e) => setOllamaConfig({ ...ollamaConfig, api_key: e.target.value })}
                  placeholder="留空使用本地模式"
                  className="w-full bg-[#0f3460] text-[#e0e0e0] border border-[#2a4a6f] rounded px-3 py-2 text-sm outline-none focus:border-[#4a6a9a]"
                />
              </div>
            </div>

            {/* 模型选择 */}
            <div>
              <label className="text-xs text-[#888] block mb-1">选择模型</label>
              <div className="flex gap-2">
                <select
                  value={ollamaConfig.model}
                  onChange={(e) => setOllamaConfig({ ...ollamaConfig, model: e.target.value })}
                  className="flex-1 bg-[#0f3460] text-[#e0e0e0] border border-[#2a4a6f] rounded px-3 py-2 text-sm outline-none focus:border-[#4a6a9a]"
                >
                  <option value="">选择模型...</option>
                  {models.map((m) => (
                    <option key={m.name} value={m.name}>
                      {m.name} {m.size && `(${m.size})`} {m.parameter_size && `- ${m.parameter_size}`}
                    </option>
                  ))}
                </select>
                <button
                  type="button"
                  onClick={(e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    console.log("Refresh button clicked");
                    loadModels();
                  }}
                  disabled={loadingModels}
                  className="bg-[#0f3460] text-[#6cb6ff] px-3 py-2 rounded hover:bg-[#1a4a80] disabled:opacity-50 text-sm cursor-pointer"
                >
                  {loadingModels ? "加载中..." : "刷新"}
                </button>
              </div>
              {modelsError && (
                <p className="text-xs text-[#ff6b6b] mt-1">{modelsError}</p>
              )}
              {models.length === 0 && !loadingModels && !modelsError && (
                <p className="text-xs text-[#888] mt-1">
                  未找到模型，请确保 Ollama 服务正在运行，或手动输入模型名称
                </p>
              )}
            </div>

            {/* 手动输入模型 */}
            <div>
              <label className="text-xs text-[#888] block mb-1">或手动输入模型名称</label>
              <input
                type="text"
                value={ollamaConfig.model}
                onChange={(e) => setOllamaConfig({ ...ollamaConfig, model: e.target.value })}
                placeholder="例如: llama3.2, qwen2.5, deepseek-coder"
                className="w-full bg-[#0f3460] text-[#e0e0e0] border border-[#2a4a6f] rounded px-3 py-2 text-sm outline-none focus:border-[#4a6a9a]"
              />
            </div>

            {/* 保存按钮 */}
            <div className="flex gap-2">
              <button
                type="button"
                onClick={saveConfig}
                className="bg-[#1a4a80] text-[#e0e0e0] px-4 py-2 rounded hover:bg-[#2a5a90] text-sm"
              >
                保存配置
              </button>
              <button
                type="button"
                onClick={(e) => {
                  e.preventDefault();
                  console.log("测试连接按钮被点击");
                  checkHealth();
                }}
                className="bg-[#0f3460] text-[#6cb6ff] px-4 py-2 rounded hover:bg-[#1a4a80] text-sm"
              >
                测试连接
              </button>
            </div>
            {healthCheckResult && (
              <div className={`text-sm mt-2 ${healthCheckResult.includes("✓") ? "text-green-400" : "text-red-400"}`}>
                {healthCheckResult}
              </div>
            )}
          </div>
        </div>
      )}

      {/* Messages */}
      <div className="flex-1 overflow-y-auto px-4 py-3">
        {messages.length === 0 && (
          <div className="text-center text-[#555] mt-20">
            <div className="text-4xl mb-4">Auto Code</div>
            <div className="text-sm">
              Type a message to start a conversation
            </div>
          </div>
        )}
        {messages.map(renderMessage)}
        {isLoading && (
          <div className="text-[#888] text-sm px-3 py-2 animate-pulse">
            Processing...
          </div>
        )}
        <div ref={messagesEndRef} />
      </div>

      {/* Input */}
      <div className="px-4 py-3 border-t border-[#2a2a4a]">
        <div className="flex gap-2">
          <textarea
            ref={inputRef}
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Type your message... (Enter to send, Shift+Enter for newline)"
            rows={4}
            className="flex-1 bg-[#16213e] text-[#e0e0e0] border border-[#2a2a4a] rounded-lg px-3 py-2 font-mono text-sm resize-none outline-none focus:border-[#4a6a9a] placeholder-[#555]"
          />
          <div className="flex flex-col gap-1">
            <button
              onClick={handleSubmit}
              disabled={isLoading || !input.trim()}
              className="bg-[#0f3460] text-[#e0e0e0] border-none rounded-lg px-4 py-2 cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed hover:bg-[#1a4a80] text-sm"
            >
              Send
            </button>
            {isLoading && (
              <button
                onClick={handleInterrupt}
                className="bg-[#5a2a2a] text-[#ff6b6b] border-none rounded-lg px-4 py-1 cursor-pointer hover:bg-[#6a3a3a] text-xs"
              >
                Cancel
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

export default App;
