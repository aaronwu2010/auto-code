import { useState, useEffect, useRef, useCallback } from "react";
import {
  SendMessage,
  Interrupt,
  GetMessages,
  GetAppState,
  SetOllamaConfig,
  GetOllamaConfig,
  ListAvailableModels,
  CheckOllamaHealth,
  SelectProjectDirectory,
  ListDirectoryContents,
} from "../wailsjs/go/state/WailsBindings";
import { state, types } from "../wailsjs/go/models";

import { EventsOn, EventsOff } from "../wailsjs/runtime/runtime";

type Message = types.Message;
type ContentBlock = types.ContentBlock;
type AppStateSnapshot = state.AppStateSnapshot;
type OllamaConfig = state.OllamaConfigRequest;
type OllamaHealth = state.OllamaHealthResponse;
type ModelInfo = state.ModelInfoUI;
type FileInfo = state.FileInfo;

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

  const [projectDir, setProjectDir] = useState<string>("");
  const [files, setFiles] = useState<FileInfo[]>([]);
  const [selectedFile, setSelectedFile] = useState<string>("");
  const [loadingFiles, setLoadingFiles] = useState(false);

  const scrollToBottom = useCallback(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, []);

  useEffect(() => {
    scrollToBottom();
  }, [messages, scrollToBottom]);

  useEffect(() => {
    if (showSettings && models.length === 0 && !loadingModels) {
      loadModels();
    }
  }, [showSettings]);

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
      if (result) {
        if (result.models && result.models.length > 0) {
          setModels(result.models);
          setModelsError(null);
        } else if (result.error) {
          setModelsError(result.error);
          setModels([]);
        } else {
          setModelsError("未找到模型，请确保 Ollama 服务正在运行并已下载模型");
          setModels([]);
        }
      }
    } catch (err) {
      setModelsError("加载模型列表失败: " + String(err));
      setModels([]);
    }
    setLoadingModels(false);
  };

  const checkHealth = async () => {
    setHealthCheckResult("正在检查连接...");
    try {
      const health = await CheckOllamaHealth();
      if (health) {
        setOllamaHealth(health);
        if (health.connected) {
          setHealthCheckResult(`✓ 已连接到 ${health.base_url}，发现 ${health.available_models || 0} 个模型`);
        } else {
          setHealthCheckResult(`✗ 连接失败: ${health.error || "未知错误"}`);
        }
      } else {
        setHealthCheckResult("✗ 检查失败: 返回结果为空");
      }
    } catch (err) {
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

  const handleSelectDirectory = async () => {
    try {
      const dir = await SelectProjectDirectory();
      if (dir) {
        setProjectDir(dir);
        await loadFiles(dir);
      }
    } catch {}
  };

  const loadFiles = async (dir: string) => {
    if (!dir) return;
    setLoadingFiles(true);
    try {
      const fileList = await ListDirectoryContents(dir);
      setFiles(fileList || []);
    } catch {
      setFiles([]);
    }
    setLoadingFiles(false);
  };

  const handleFileClick = (file: FileInfo) => {
    if (file.is_dir) {
      setProjectDir(file.path);
      loadFiles(file.path);
    } else {
      setSelectedFile(file.path);
    }
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
        if (msg.type === "result" || msg.type === "error") {
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
      if (state) setAppState(state);

      const msgs = await GetMessages();
      if (msgs) setMessages(msgs.messages || []);

      await loadConfig();
      await checkHealth();
      await loadModels();
    } catch {}
  };

  const handleSubmit = async () => {
    if (!input.trim() || isLoading) return;

    const currentInput = input;
    setInput("");
    setIsLoading(true);

    try {
      await SendMessage({ prompt: currentInput });
    } catch {
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
          <pre key={idx} className="whitespace-pre-wrap break-words text-sm leading-relaxed">
            {block.text}
          </pre>
        );
      case "tool_use":
        return (
          <div
            key={idx}
            className="bg-[#1c2d44] border border-[#2a4a6f]/60 rounded-lg px-3 py-2 my-1.5 text-sm"
          >
            <span className="text-[#6cb6ff] font-semibold text-xs tracking-wide">
              {block.tool_name}
            </span>
            {block.tool_input && (
              <pre className="text-xs text-[#7a8a9a] mt-1.5 overflow-x-auto">
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
            className={`rounded-lg px-3 py-2 my-1.5 text-sm ${
              block.is_error
                ? "bg-[#2d1a1a] border border-[#5a2a2a]/60"
                : "bg-[#1a2d1a] border border-[#2a4a2a]/60"
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
            className="bg-[#1a1a2e]/60 border border-[#2a2a4a]/50 rounded-lg px-3 py-2 my-1.5"
          >
            <summary className="text-xs text-[#6a6a8a] cursor-pointer hover:text-[#8a8aaa] transition-colors">
              Thinking...
            </summary>
            <pre className="whitespace-pre-wrap break-words text-xs text-[#5a5a7a] mt-2">
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
        className={`mb-3 px-4 py-3 rounded-xl max-w-[92%] ${
          isUser
            ? "bg-[#1e3a5f]/70 ml-auto border border-[#2a5a8f]/30"
            : isSystem
            ? "bg-[#2a1a3e]/50 border border-[#3a2a5a]/30"
            : "bg-[#162440]/60 border border-[#2a3a5a]/30"
        }`}
      >
        <div className="text-[10px] text-[#6a7a8a] mb-1.5 font-semibold uppercase tracking-wider">
          {msg.role}
        </div>
        {msg.content_blocks && msg.content_blocks.length > 0
          ? msg.content_blocks.map((block, i) => renderContentBlock(block, i))
          : <pre className="whitespace-pre-wrap break-words text-sm leading-relaxed">{msg.content}</pre>}
      </div>
    );
  };

  return (
    <div className="flex flex-col h-screen bg-[#0e1525] text-[#d0d8e8] font-mono">
      {/* Header */}
      <div className="flex items-center justify-between px-5 py-2.5 border-b border-[#1e2d44] bg-[#111b2e]">
        <div className="flex items-center gap-3">
          <span className="text-sm font-bold text-[#6cb6ff] tracking-wide">Auto Code</span>
          {appState?.mainLoopModel && (
            <span className="text-[11px] text-[#8a9ab0] bg-[#1a2a40] px-2.5 py-0.5 rounded-md border border-[#2a3a50]/50">
              {appState.mainLoopModel}
            </span>
          )}
          {ollamaHealth && (
            <span className={`text-[11px] px-2.5 py-0.5 rounded-md border ${
              ollamaHealth.connected
                ? "bg-[#1a2e1a]/60 text-[#6bff6b] border-[#2a4a2a]/50"
                : "bg-[#2d1a1a]/60 text-[#ff6b6b] border-[#4a2a2a]/50"
            }`}>
              {ollamaHealth.connected ? "已连接" : "未连接"}
              {ollamaHealth.is_local ? " · 本地" : " · 云端"}
            </span>
          )}
          {appState?.thinkingEnabled && (
            <span className="text-[11px] text-[#a78bfa] bg-[#1e1530] px-2.5 py-0.5 rounded-md border border-[#2e2550]/50">
              Thinking
            </span>
          )}
          {appState?.fastMode && (
            <span className="text-[11px] text-[#fbbf24] bg-[#2a2010] px-2.5 py-0.5 rounded-md border border-[#3a3020]/50">
              Fast
            </span>
          )}
        </div>
        <div className="flex items-center gap-3">
          <button
            onClick={() => setShowSettings(!showSettings)}
            className="text-xs bg-[#1a2a40] text-[#6cb6ff] px-3 py-1.5 rounded-md hover:bg-[#243550] border border-[#2a3a50]/50 transition-colors"
          >
            设置
          </button>
          {statusText && (
            <span className="text-[11px] text-[#6a7a8a]">{statusText}</span>
          )}
          {isLoading && (
            <button
              onClick={handleInterrupt}
              className="text-xs bg-[#3a1a1a] text-[#ff6b6b] px-3 py-1.5 rounded-md hover:bg-[#4a2a2a] border border-[#5a2a2a]/50 transition-colors"
            >
              Stop
            </button>
          )}
        </div>
      </div>

      {/* 设置面板 */}
      {showSettings && (
        <div className="border-b border-[#1e2d44] bg-[#111b2e] p-5">
          <div className="max-w-2xl mx-auto space-y-4">
            <h2 className="text-sm font-bold text-[#6cb6ff] tracking-wide">Ollama 配置</h2>

            {ollamaHealth && (
              <div className={`text-xs p-2.5 rounded-lg border ${
                ollamaHealth.connected
                  ? "bg-[#1a2e1a]/40 text-[#6bff6b] border-[#2a4a2a]/50"
                  : "bg-[#2d1a1a]/40 text-[#ff6b6b] border-[#4a2a2a]/50"
              }`}>
                {ollamaHealth.connected
                  ? `✓ 已连接到 ${ollamaHealth.base_url}`
                  : `✗ 连接失败: ${ollamaHealth.error || "未知错误"}`}
              </div>
            )}

            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div>
                <label className="text-[11px] text-[#6a7a8a] block mb-1.5">Ollama URL</label>
                <input
                  type="text"
                  value={ollamaConfig.base_url}
                  onChange={(e) => setOllamaConfig({ ...ollamaConfig, base_url: e.target.value })}
                  placeholder="http://localhost:11434/api"
                  className="w-full bg-[#0e1525] text-[#d0d8e8] border border-[#1e2d44] rounded-lg px-3 py-2 text-sm outline-none focus:border-[#3a5a8a] transition-colors placeholder-[#3a4a5a]"
                />
              </div>

              <div>
                <label className="text-[11px] text-[#6a7a8a] block mb-1.5">API Key（可选）</label>
                <input
                  type="password"
                  value={ollamaConfig.api_key}
                  onChange={(e) => setOllamaConfig({ ...ollamaConfig, api_key: e.target.value })}
                  placeholder="留空使用本地模式"
                  className="w-full bg-[#0e1525] text-[#d0d8e8] border border-[#1e2d44] rounded-lg px-3 py-2 text-sm outline-none focus:border-[#3a5a8a] transition-colors placeholder-[#3a4a5a]"
                />
              </div>
            </div>

            <div>
              <label className="text-[11px] text-[#6a7a8a] block mb-1.5">选择模型</label>
              <div className="flex gap-2">
                <select
                  value={ollamaConfig.model}
                  onChange={(e) => setOllamaConfig({ ...ollamaConfig, model: e.target.value })}
                  className="flex-1 bg-[#0e1525] text-[#d0d8e8] border border-[#1e2d44] rounded-lg px-3 py-2 text-sm outline-none focus:border-[#3a5a8a] transition-colors"
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
                    loadModels();
                  }}
                  disabled={loadingModels}
                  className="bg-[#1a2a40] text-[#6cb6ff] px-4 py-2 rounded-lg hover:bg-[#243550] disabled:opacity-50 text-sm cursor-pointer border border-[#2a3a50]/50 transition-colors"
                >
                  {loadingModels ? "..." : "刷新"}
                </button>
              </div>
              {modelsError && (
                <p className="text-xs text-[#ff6b6b] mt-1.5">{modelsError}</p>
              )}
              {models.length === 0 && !loadingModels && !modelsError && (
                <p className="text-[11px] text-[#4a5a6a] mt-1.5">
                  未找到模型，请确保 Ollama 服务正在运行，或手动输入模型名称
                </p>
              )}
            </div>

            <div>
              <label className="text-[11px] text-[#6a7a8a] block mb-1.5">或手动输入模型名称</label>
              <input
                type="text"
                value={ollamaConfig.model}
                onChange={(e) => setOllamaConfig({ ...ollamaConfig, model: e.target.value })}
                placeholder="例如: llama3.2, qwen2.5, deepseek-coder"
                className="w-full bg-[#0e1525] text-[#d0d8e8] border border-[#1e2d44] rounded-lg px-3 py-2 text-sm outline-none focus:border-[#3a5a8a] transition-colors placeholder-[#3a4a5a]"
              />
            </div>

            <div className="flex gap-2 pt-1">
              <button
                type="button"
                onClick={saveConfig}
                className="bg-[#1a3a60] text-[#d0d8e8] px-5 py-2 rounded-lg hover:bg-[#244a70] text-sm border border-[#2a4a70]/50 transition-colors"
              >
                保存配置
              </button>
              <button
                type="button"
                onClick={(e) => {
                  e.preventDefault();
                  checkHealth();
                }}
                className="bg-[#1a2a40] text-[#6cb6ff] px-5 py-2 rounded-lg hover:bg-[#243550] text-sm border border-[#2a3a50]/50 transition-colors"
              >
                测试连接
              </button>
            </div>
            {healthCheckResult && (
              <div className={`text-sm mt-1 ${healthCheckResult.includes("✓") ? "text-[#6bff6b]" : "text-[#ff6b6b]"}`}>
                {healthCheckResult}
              </div>
            )}
          </div>
        </div>
      )}

      {/* 主内容区域 */}
      <div className="flex flex-1 overflow-hidden">
        {/* 左侧对话区域 */}
        <div className="flex flex-col flex-1">
          {/* Messages */}
          <div className="flex-1 overflow-y-auto px-6 py-4">
            {messages.length === 0 && (
              <div className="text-center text-[#3a4a5a] mt-24">
                <div className="text-3xl font-bold text-[#2a3a5a] mb-3 tracking-wide">Auto Code</div>
                <div className="text-sm">
                  输入消息开始对话
                </div>
              </div>
            )}
            {messages.map(renderMessage)}
            {isLoading && (
              <div className="text-[#6a7a8a] text-sm px-4 py-2 animate-pulse">
                处理中...
              </div>
            )}
            <div ref={messagesEndRef} />
          </div>

          {/* Input */}
          <div className="px-5 py-4 border-t border-[#1e2d44] bg-[#0c1320]">
            {projectDir && (
              <div className="flex items-center gap-2 mb-2.5 text-[11px] text-[#4a5a6a]">
                <span className="text-[#6a7a8a]">{projectDir}</span>
              </div>
            )}
            <div className="flex gap-3">
              <div className="flex-1 relative">
                <textarea
                  ref={inputRef}
                  value={input}
                  onChange={(e) => setInput(e.target.value)}
                  onKeyDown={handleKeyDown}
                  placeholder="输入消息... (Enter 发送, Shift+Enter 换行)"
                  rows={3}
                  className="w-full bg-[#111b2e] text-[#d0d8e8] border border-[#1e2d44] rounded-xl px-4 py-3 font-mono text-sm resize-none outline-none focus:border-[#3a5a8a] transition-colors placeholder-[#3a4a5a]"
                />
              </div>
              <div className="flex flex-col gap-2 justify-end">
                <button
                  type="button"
                  onClick={() => handleSubmit()}
                  disabled={isLoading || !input.trim()}
                  className="bg-[#1a3a60] text-[#d0d8e8] rounded-xl px-5 py-2.5 cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed hover:bg-[#244a70] text-sm border border-[#2a4a70]/50 transition-colors"
                >
                  发送
                </button>
                {isLoading && (
                  <button
                    onClick={handleInterrupt}
                    className="bg-[#3a1a1a] text-[#ff6b6b] rounded-xl px-5 py-2 cursor-pointer hover:bg-[#4a2a2a] text-xs border border-[#5a2a2a]/50 transition-colors"
                  >
                    取消
                  </button>
                )}
              </div>
            </div>
            <div className="flex items-center gap-2 mt-2.5">
              <button
                type="button"
                onClick={handleSelectDirectory}
                className="text-[11px] bg-[#1a2a40] text-[#6a8aaa] px-3 py-1 rounded-md hover:bg-[#243550] border border-[#2a3a50]/50 transition-colors"
              >
                选择目录
              </button>
              {!projectDir && (
                <span className="text-[11px] text-[#3a4a5a]">未选择项目目录</span>
              )}
            </div>
          </div>
        </div>

        {/* 右侧面板：文件资源管理器 */}
        <div className="w-64 flex flex-col bg-[#0c1320] overflow-hidden border-l border-[#1e2d44]">
          <div className="flex-1 flex flex-col overflow-hidden">
            <div className="px-4 py-3 border-b border-[#1e2d44] text-[11px] font-semibold text-[#6a8aaa] tracking-wide uppercase">
              文件资源管理器
            </div>
            <div className="flex-1 overflow-y-auto">
              {loadingFiles ? (
                <div className="text-[11px] text-[#4a5a6a] p-3">加载中...</div>
              ) : !projectDir ? (
                <div className="text-[11px] text-[#3a4a5a] p-3">请选择项目目录</div>
              ) : files.length === 0 ? (
                <div className="text-[11px] text-[#3a4a5a] p-3">目录为空</div>
              ) : (
                <div className="text-[11px] py-1">
                  {files.map((file, i) => (
                    <div
                      key={i}
                      onClick={() => handleFileClick(file)}
                      className={`px-3 py-1.5 cursor-pointer flex items-center gap-2 transition-colors ${
                        selectedFile === file.path
                          ? "bg-[#1a2a40] text-[#6cb6ff]"
                          : "text-[#8a9ab0] hover:bg-[#111b2e]"
                      }`}
                    >
                      <span className="text-[#4a5a6a]">{file.is_dir ? "▸" : "·"}</span>
                      <span className="truncate flex-1">{file.name}</span>
                      {!file.is_dir && file.size > 0 && (
                        <span className="text-[9px] text-[#3a4a5a]">
                          {file.size > 1024 * 1024
                            ? `${(file.size / 1024 / 1024).toFixed(1)}M`
                            : file.size > 1024
                            ? `${(file.size / 1024).toFixed(1)}K`
                            : `${file.size}B`}
                        </span>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

export default App;
