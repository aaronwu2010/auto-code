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
  SetProjectDirectory,
  GetProjectDirectory,
  ListDirectoryContents,
  GetContextUsage,
} from "../wailsjs/go/state/WailsBindings";
import { state, types } from "../wailsjs/go/models";

import { EventsOn, EventsOff } from "../wailsjs/runtime/runtime";

type Message = types.Message;
type ContentBlock = types.ContentBlock;
type AppStateSnapshot = state.AppStateSnapshot;
type OllamaConfig = state.OllamaConfigRequest;
type OllamaHealth = state.OllamaHealthResponse;
type ModelInfo = state.ModelInfoUI;
type ContextUsage = types.ContextUsage;
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
  const [streamingMessage, setStreamingMessage] = useState<Message | null>(null);
  const [isToolCalling, setIsToolCalling] = useState(false);
  const [contextUsage, setContextUsage] = useState<ContextUsage | null>(null);
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number; filePath: string } | null>(null);

  const [currentToolUse, setCurrentToolUse] = useState<{
    tool_name: string;
    tool_use_id?: string;
    input?: unknown;
    status: string;
  } | null>(null);

  type ActivityPhase = "call_model" | "tool_start" | "tool_done" | "thinking";
  interface ActivityEntry {
    id: string;
    timestamp: number;
    phase: ActivityPhase;
    toolName?: string;
    status?: string;
    detail?: string;
  }
  const [activityLog, setActivityLog] = useState<ActivityEntry[]>([]);
  const sessionActivityRef = useRef<ActivityEntry[]>([]);

  const appendActivity = useCallback((entry: Omit<ActivityEntry, "id" | "timestamp">) => {
    const newEntry: ActivityEntry = {
      ...entry,
      id: Math.random().toString(36).slice(2) + Date.now().toString(36),
      timestamp: Date.now(),
    };
    sessionActivityRef.current = [...sessionActivityRef.current, newEntry].slice(-100);
    setActivityLog([...sessionActivityRef.current]);
  }, []);

  const resetSessionActivity = useCallback(() => {
    sessionActivityRef.current = [];
    setActivityLog([]);
  }, []);

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

  useEffect(() => {
    const handleGlobalClick = () => setContextMenu(null);
    window.addEventListener("click", handleGlobalClick);
    window.addEventListener("scroll", handleGlobalClick, true);
    return () => {
      window.removeEventListener("click", handleGlobalClick);
      window.removeEventListener("scroll", handleGlobalClick, true);
    };
  }, []);

  const insertPathToInput = (path: string) => {
    setInput((prev) => {
      const textarea = inputRef.current;
      const insertText = `\`${path}\``;
      if (textarea) {
        const start = textarea.selectionStart ?? prev.length;
        const end = textarea.selectionEnd ?? prev.length;
        const before = prev.slice(0, start);
        const after = prev.slice(end);
        const separator = before.length > 0 && !before.endsWith("\n") && !before.endsWith(" ") ? " " : "";
        const newValue = before + separator + insertText + after;
        requestAnimationFrame(() => {
          const pos = (before + separator + insertText).length;
          textarea.focus();
          textarea.setSelectionRange(pos, pos);
        });
        return newValue;
      }
      return prev + (prev.length > 0 ? " " : "") + insertText;
    });
    setContextMenu(null);
  };

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
        await SetProjectDirectory(dir);
        await loadFiles(dir);
      }
    } catch {}
  };

  const loadProjectDir = async () => {
    try {
      const savedDir = await GetProjectDirectory();
      if (savedDir) {
        setProjectDir(savedDir);
        await loadFiles(savedDir);
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
          const v = event.value as string;
          setStatusText(v);
          if (v && v.includes("思考")) {
            appendActivity({ phase: "call_model", detail: v });
          } else if (v && v.includes("完成")) {
            // done
          }
        }
        if (event.type === "processing_update") {
          const v = event.value as boolean;
          setIsLoading(v);
          if (v) {
            resetSessionActivity();
          }
        }
        if (event.type === "tool_use_update") {
          const v = event.value as { tool_name: string; status: string; input?: unknown } | null;
          setCurrentToolUse(v);
          if (v) {
            if (v.status === "running") {
              appendActivity({
                phase: "tool_start",
                toolName: v.tool_name,
                status: "running",
                detail: formatToolDetail(v.tool_name, v.input),
              });
              setIsToolCalling(true);
            } else if (v.status === "done" || v.status === "error") {
              appendActivity({
                phase: "tool_done",
                toolName: v.tool_name,
                status: v.status,
              });
            }
          }
        }
      } catch {}
    });

    EventsOn("query:message", (data: unknown) => {
      try {
        const msg: SDKMessage =
          typeof data === "string" ? JSON.parse(data) : (data as SDKMessage);
        if (msg.type === "stream_chunk" && msg.message) {
          setStreamingMessage(msg.message);
          setIsToolCalling(false);
        } else if (msg.type === "tool_calls_start") {
          setIsToolCalling(true);
        } else if (msg.type === "assistant" && msg.message) {
          setStreamingMessage(null);
          setIsToolCalling(false);
          setMessages((prev) => {
            const exists = prev.some((m) => m.id === msg.message!.id);
            if (exists) return prev;
            return [...prev, msg.message!];
          });
        } else if (msg.message && msg.message.role !== "tool") {
          setMessages((prev) => {
            const exists = prev.some((m) => m.id === msg.message!.id);
            if (exists) return prev;
            return [...prev, msg.message!];
          });
        }
        if (msg.type === "result" || msg.type === "error") {
          setIsLoading(false);
          setStreamingMessage(null);
          setIsToolCalling(false);
          setCurrentToolUse(null);
          setStatusText("");
          // 对话结束后刷新 token 占用
          GetContextUsage().then(setContextUsage).catch(() => {});
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
      await loadProjectDir();
      GetContextUsage().then(setContextUsage).catch(() => {});
    } catch {}
  };

  const handleSubmit = async () => {
    if (!input.trim() || isLoading) return;

    const currentInput = input;
    setInput("");
    setIsLoading(true);
    setIsToolCalling(false);

    try {
      await SendMessage({ prompt: currentInput });
    } catch {
      setIsLoading(false);
      setIsToolCalling(false);
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

  const formatToolDetail = (_toolName: string, input?: unknown): string => {
    if (!input) return "";
    try {
      const obj = typeof input === "string" ? JSON.parse(input) : input;
      const parts: string[] = [];
      for (const key of Object.keys(obj)) {
        const val = obj[key];
        let s = typeof val === "string" ? val : JSON.stringify(val);
        if (s.length > 60) s = s.slice(0, 60) + "…";
        parts.push(`${key}: ${s}`);
        if (parts.length >= 2) break;
      }
      return parts.join(" · ");
    } catch {
      return "";
    }
  };

  const getToolIcon = (toolName: string): string => {
    const n = toolName.toLowerCase();
    if (n.includes("read") || n.includes("file")) return "📄";
    if (n.includes("write")) return "✍️";
    if (n.includes("edit") || n.includes("patch")) return "🔧";
    if (n.includes("bash") || n.includes("shell") || n.includes("powershell")) return "🖥️";
    if (n.includes("search") || n.includes("grep") || n.includes("glob")) return "🔍";
    if (n.includes("list") || n.includes("dir")) return "📂";
    if (n.includes("agent") || n.includes("sub") || n.includes("delegate")) return "🧠";
    if (n.includes("http") || n.includes("fetch") || n.includes("web")) return "🌐";
    return "🛠️";
  };

  const renderActivityTimeline = (entries: ActivityEntry[]) => {
    if (!entries || entries.length === 0) return null;
    return (
      <div className="mt-3 border-t border-slate-700/40 pt-3 space-y-1.5">
        <div className="text-[10px] uppercase tracking-wider text-slate-500 mb-1.5 font-semibold">
          ⏱️ Agent 活动
        </div>
        {entries.slice(-12).map((e) => {
          const isRunning = e.status === "running" && e.phase !== "tool_done";
          const isError = e.status === "error";
          return (
            <div
              key={e.id}
              className={`flex items-start gap-2 text-xs py-0.5 ${
                isRunning ? "text-sky-300" : isError ? "text-red-300" : "text-slate-400"
              }`}
            >
              <span className="mt-0.5 shrink-0">
                {e.phase === "call_model" ? (
                  isRunning ? (
                    <span className="inline-flex gap-0.5">
                      <span className="w-1.5 h-1.5 bg-sky-400 rounded-full animate-bounce" style={{ animationDelay: '0ms' }}></span>
                      <span className="w-1.5 h-1.5 bg-sky-400 rounded-full animate-bounce" style={{ animationDelay: '100ms' }}></span>
                      <span className="w-1.5 h-1.5 bg-sky-400 rounded-full animate-bounce" style={{ animationDelay: '200ms' }}></span>
                    </span>
                  ) : (
                    "💭"
                  )
                ) : e.phase === "tool_start" ? (
                  isRunning ? (
                    <span className="inline-block w-3 h-3 rounded-full border-2 border-amber-400 border-t-transparent animate-spin"></span>
                  ) : (
                    "▶"
                  )
                ) : e.phase === "tool_done" ? (
                  isError ? "❌" : "✅"
                ) : "•"}
              </span>
              <span className="flex-1 break-all">
                {e.phase === "call_model" && (
                  <span>
                    <span className="font-semibold text-slate-300">思考中</span>
                    {e.detail && <span className="text-slate-500 ml-1">{e.detail}</span>}
                  </span>
                )}
                {e.phase === "tool_start" && (
                  <span>
                    <span className="inline-flex items-center gap-1">
                      <span>{getToolIcon(e.toolName || "")}</span>
                      <span className="font-semibold text-slate-300">{e.toolName}</span>
                    </span>
                    {isRunning && <span className="ml-1 text-amber-400">运行中…</span>}
                    {e.detail && (
                      <span className="ml-1.5 text-slate-500">({e.detail})</span>
                    )}
                  </span>
                )}
                {e.phase === "tool_done" && (
                  <span className="text-slate-500">
                    {getToolIcon(e.toolName || "")} {e.toolName}{" "}
                    {isError ? "失败" : "完成"}
                  </span>
                )}
              </span>
              <span className="text-slate-600 tabular-nums shrink-0 ml-1">
                {new Date(e.timestamp).toLocaleTimeString("zh-CN", { hour12: false })}
              </span>
            </div>
          );
        })}
      </div>
    );
  };

  const renderContentBlock = (block: ContentBlock, idx: number) => {
    switch (block.type) {
      case "text":
        return (
          <pre key={idx} className="whitespace-pre-wrap break-words text-sm leading-relaxed text-slate-200">
            {block.text}
          </pre>
        );
      case "tool_use":
        return null;
      case "tool_result":
        return null;
      case "thinking":
        return (
          <details
            key={idx}
            className="bg-violet-900/10 border border-violet-800/30 rounded-lg px-3 py-2 my-1.5"
          >
            <summary className="text-xs text-violet-400 cursor-pointer hover:text-violet-300 transition-colors select-none">
              💭 思考过程...
            </summary>
            <pre className="whitespace-pre-wrap break-words text-xs text-slate-500 mt-2">
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
    const isTool = msg.role === "tool";
    if (isTool) return null;

    return (
      <div
        key={msg.id}
        className={`mb-4 px-4 py-3 rounded-2xl max-w-[85%] shadow-sm ${
          isUser
            ? "bg-gradient-to-br from-sky-600 to-sky-700 ml-auto text-white"
            : isSystem
            ? "bg-violet-900/30 border border-violet-700/30 text-slate-200"
            : "bg-slate-800/60 border border-slate-700/50 text-slate-200"
        }`}
      >
        {!isUser && (
          <div className="text-[10px] mb-2 font-semibold uppercase tracking-wider text-slate-500">
            {msg.role}
          </div>
        )}
        {msg.content_blocks && msg.content_blocks.length > 0
          ? msg.content_blocks.map((block, i) => renderContentBlock(block, i))
          : <pre className="whitespace-pre-wrap break-words text-sm leading-relaxed">{msg.content}</pre>}
      </div>
    );
  };

  return (
    <div className="flex flex-col h-screen bg-gradient-to-br from-slate-950 via-slate-900 to-slate-950 text-slate-200 font-sans">
      {/* Header */}
      <div className="flex items-center justify-between px-6 py-3 border-b border-slate-800/50 bg-slate-900/80 backdrop-blur-sm">
        <div className="flex items-center gap-3">
          <div className="w-8 h-8 rounded-lg bg-gradient-to-br from-sky-500 to-indigo-600 flex items-center justify-center text-white font-bold text-sm shadow-lg">
            AC
          </div>
          <span className="text-base font-semibold bg-gradient-to-r from-sky-400 to-indigo-400 bg-clip-text text-transparent">
            Auto Code
          </span>
          {appState?.mainLoopModel && (
            <span className="text-xs text-slate-400 bg-slate-800/80 px-3 py-1 rounded-full border border-slate-700/50 flex items-center gap-1.5">
              {appState.mainLoopModel}
              {(() => {
                const m = models.find((m) => m.name === appState.mainLoopModel);
                return m?.context_length ? (
                  <span className="text-sky-400">· {(m.context_length / 1024).toFixed(0)}K ctx</span>
                ) : null;
              })()}
            </span>
          )}
          {ollamaHealth && (
            <span className={`text-xs px-3 py-1 rounded-full border flex items-center gap-1.5 ${
              ollamaHealth.connected
                ? "bg-emerald-900/30 text-emerald-400 border-emerald-800/40"
                : "bg-red-900/30 text-red-400 border-red-800/40"
            }`}>
              <span className={`w-1.5 h-1.5 rounded-full ${ollamaHealth.connected ? "bg-emerald-400 animate-pulse" : "bg-red-400"}`}></span>
              {ollamaHealth.connected ? "已连接" : "未连接"}
              {ollamaHealth.is_local ? " · 本地" : " · 云端"}
            </span>
          )}
          {appState?.thinkingEnabled && (
            <span className="text-xs text-violet-400 bg-violet-900/30 px-3 py-1 rounded-full border border-violet-800/40">
              🧠 Thinking
            </span>
          )}
          {appState?.fastMode && (
            <span className="text-xs text-amber-400 bg-amber-900/30 px-3 py-1 rounded-full border border-amber-800/40">
              ⚡ Fast
            </span>
          )}
          {contextUsage && contextUsage.context_length > 0 && (
            <span
              className={`text-xs px-2.5 py-1 rounded-full border flex items-center gap-1.5 ${
                contextUsage.usage_percent >= 80
                  ? "bg-red-900/30 text-red-400 border-red-800/40"
                  : contextUsage.usage_percent >= 50
                  ? "bg-amber-900/30 text-amber-400 border-amber-800/40"
                  : "bg-slate-800/60 text-slate-400 border-slate-700/40"
              }`}
              title={`System: ${contextUsage.system_tokens} | Messages: ${contextUsage.message_tokens} | Max: ${contextUsage.context_length}`}
            >
              <span className="w-12 h-1.5 rounded-full bg-slate-700 overflow-hidden">
                <span
                  className={`block h-full rounded-full ${
                    contextUsage.usage_percent >= 80
                      ? "bg-red-400"
                      : contextUsage.usage_percent >= 50
                      ? "bg-amber-400"
                      : "bg-sky-400"
                  }`}
                  style={{ width: `${Math.min(contextUsage.usage_percent, 100)}%` }}
                ></span>
              </span>
              {contextUsage.usage_percent}% · {(contextUsage.total_tokens / 1000).toFixed(1)}K/{(contextUsage.context_length / 1024).toFixed(0)}K
            </span>
          )}
        </div>
        <div className="flex items-center gap-3">
          {statusText && (
            <span className="text-xs text-slate-500">{statusText}</span>
          )}
          {isLoading && (
            <button
              onClick={handleInterrupt}
              className="text-xs bg-red-900/40 text-red-400 px-4 py-1.5 rounded-lg hover:bg-red-900/60 border border-red-800/40 transition-all duration-200 flex items-center gap-1.5"
            >
              ⏹ 停止
            </button>
          )}
          <button
            onClick={() => setShowSettings(!showSettings)}
            className={`text-xs px-4 py-1.5 rounded-lg transition-all duration-200 flex items-center gap-1.5 ${
              showSettings
                ? "bg-sky-600/30 text-sky-400 border border-sky-600/40"
                : "bg-slate-800/80 text-slate-400 hover:text-slate-300 border border-slate-700/50 hover:border-slate-600/50"
            }`}
          >
            ⚙️ 设置
          </button>
        </div>
      </div>

      {/* 设置面板 */}
      {showSettings && (
        <div className="border-b border-slate-800/50 bg-slate-900/50 backdrop-blur-sm p-6">
          <div className="max-w-3xl mx-auto">
            <h2 className="text-lg font-semibold bg-gradient-to-r from-sky-400 to-indigo-400 bg-clip-text text-transparent mb-5 flex items-center gap-2">
              <span className="text-xl">🔌</span> Ollama 配置
            </h2>

            {ollamaHealth && (
              <div className={`text-sm p-3 rounded-xl border mb-5 flex items-center gap-2 ${
                ollamaHealth.connected
                  ? "bg-emerald-900/20 text-emerald-400 border-emerald-800/30"
                  : "bg-red-900/20 text-red-400 border-red-800/30"
              }`}>
                <span className="text-lg">{ollamaHealth.connected ? "✅" : "❌"}</span>
                {ollamaHealth.connected
                  ? `已连接到 ${ollamaHealth.base_url}`
                  : `连接失败: ${ollamaHealth.error || "未知错误"}`}
              </div>
            )}

            <div className="grid grid-cols-1 md:grid-cols-2 gap-5 mb-5">
              <div>
                <label className="text-xs text-slate-400 block mb-2 font-medium">Ollama URL</label>
                <input
                  type="text"
                  value={ollamaConfig.base_url}
                  onChange={(e) => setOllamaConfig({ ...ollamaConfig, base_url: e.target.value })}
                  placeholder="http://localhost:11434/api"
                  className="w-full bg-slate-800/50 text-slate-200 border border-slate-700/50 rounded-xl px-4 py-2.5 text-sm outline-none focus:border-sky-600/50 focus:ring-2 focus:ring-sky-500/20 transition-all duration-200 placeholder-slate-600"
                />
              </div>

              <div>
                <label className="text-xs text-slate-400 block mb-2 font-medium">API Key（可选）</label>
                <input
                  type="password"
                  value={ollamaConfig.api_key}
                  onChange={(e) => setOllamaConfig({ ...ollamaConfig, api_key: e.target.value })}
                  placeholder="留空使用本地模式"
                  className="w-full bg-slate-800/50 text-slate-200 border border-slate-700/50 rounded-xl px-4 py-2.5 text-sm outline-none focus:border-sky-600/50 focus:ring-2 focus:ring-sky-500/20 transition-all duration-200 placeholder-slate-600"
                />
              </div>
            </div>

            <div className="mb-5">
              <label className="text-xs text-slate-400 block mb-2 font-medium">选择模型</label>
              <div className="flex gap-2">
                <select
                  value={ollamaConfig.model}
                  onChange={(e) => setOllamaConfig({ ...ollamaConfig, model: e.target.value })}
                  className="flex-1 bg-slate-800/50 text-slate-200 border border-slate-700/50 rounded-xl px-4 py-2.5 text-sm outline-none focus:border-sky-600/50 focus:ring-2 focus:ring-sky-500/20 transition-all duration-200 cursor-pointer"
                >
                  <option value="">选择模型...</option>
                  {models.map((m) => (
                    <option key={m.name} value={m.name}>
                      {m.name} {m.size && `(${m.size})`} {m.parameter_size && `- ${m.parameter_size}`} {m.context_length ? `- ${m.context_length.toLocaleString()} tokens` : ""}
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
                  className="bg-slate-800/80 text-slate-300 px-5 py-2.5 rounded-xl hover:bg-slate-700/80 disabled:opacity-50 text-sm cursor-pointer border border-slate-700/50 transition-all duration-200 flex items-center gap-1.5"
                >
                  {loadingModels ? "⟳" : "🔄"} {loadingModels ? "加载中" : "刷新"}
                </button>
              </div>
              {modelsError && (
                <p className="text-xs text-red-400 mt-2">{modelsError}</p>
              )}
              {models.length === 0 && !loadingModels && !modelsError && (
                <p className="text-xs text-slate-500 mt-2">
                  未找到模型，请确保 Ollama 服务正在运行，或手动输入模型名称
                </p>
              )}
            </div>

            <div className="mb-5">
              <label className="text-xs text-slate-400 block mb-2 font-medium">或手动输入模型名称</label>
              <input
                type="text"
                value={ollamaConfig.model}
                onChange={(e) => setOllamaConfig({ ...ollamaConfig, model: e.target.value })}
                placeholder="例如: llama3.2, qwen2.5, deepseek-coder"
                className="w-full bg-slate-800/50 text-slate-200 border border-slate-700/50 rounded-xl px-4 py-2.5 text-sm outline-none focus:border-sky-600/50 focus:ring-2 focus:ring-sky-500/20 transition-all duration-200 placeholder-slate-600"
              />
            </div>

            <div className="flex gap-3">
              <button
                type="button"
                onClick={saveConfig}
                className="bg-gradient-to-r from-sky-600 to-indigo-600 text-white px-6 py-2.5 rounded-xl hover:from-sky-500 hover:to-indigo-500 text-sm font-medium transition-all duration-200 shadow-lg shadow-sky-900/30 hover:shadow-sky-800/40 flex items-center gap-2"
              >
                💾 保存配置
              </button>
              <button
                type="button"
                onClick={(e) => {
                  e.preventDefault();
                  checkHealth();
                }}
                className="bg-slate-800/80 text-slate-300 px-6 py-2.5 rounded-xl hover:bg-slate-700/80 text-sm font-medium border border-slate-700/50 transition-all duration-200 flex items-center gap-2"
              >
                🔌 测试连接
              </button>
            </div>
            {healthCheckResult && (
              <div className={`text-sm mt-3 flex items-center gap-2 ${healthCheckResult.includes("✓") ? "text-emerald-400" : "text-red-400"}`}>
                {healthCheckResult}
              </div>
            )}
          </div>
        </div>
      )}

      {/* 主内容区域 */}
      <div className="flex flex-1 overflow-hidden">
        {/* 左侧对话区域 */}
        <div className="flex flex-col flex-1 min-w-0">
          {/* Messages */}
          <div className="flex-1 overflow-y-auto px-8 py-6">
            {messages.length === 0 && (
              <div className="text-center mt-32">
                <div className="w-20 h-20 mx-auto mb-6 rounded-2xl bg-gradient-to-br from-sky-500 to-indigo-600 flex items-center justify-center text-white text-3xl font-bold shadow-2xl shadow-sky-900/50">
                  AC
                </div>
                <div className="text-2xl font-bold bg-gradient-to-r from-sky-400 via-indigo-400 to-violet-400 bg-clip-text text-transparent mb-3">
                  Auto Code
                </div>
                <div className="text-slate-500 text-sm">
                  有什么可以帮你的吗？输入消息开始对话
                </div>
              </div>
            )}
            {messages.map(renderMessage)}
            {streamingMessage && (
              <div className="mb-4 px-4 py-3 rounded-2xl max-w-[85%] bg-slate-800/60 border border-slate-700/50 text-slate-200 shadow-sm">
                <div className="text-[10px] mb-2 font-semibold uppercase tracking-wider text-slate-500">
                  assistant
                </div>
                <div className="text-sm leading-relaxed whitespace-pre-wrap break-words">
                  {streamingMessage.content}
                  {!streamingMessage.content && (
                    <span className="inline-block w-2 h-4 bg-sky-400 ml-0.5 align-middle animate-pulse rounded-sm"></span>
                  )}
                </div>
                {streamingMessage.thinking && (
                  <details className="bg-violet-900/10 border border-violet-800/30 rounded-lg px-3 py-2 my-1.5">
                    <summary className="text-xs text-violet-400 cursor-pointer hover:text-violet-300 transition-colors select-none">
                      💭 思考过程...
                    </summary>
                    <pre className="whitespace-pre-wrap break-words text-xs text-slate-500 mt-2">
                      {streamingMessage.thinking}
                    </pre>
                  </details>
                )}
                {renderActivityTimeline(activityLog)}
              </div>
            )}
            {!streamingMessage && (isLoading || isToolCalling) && (
              <div className="mb-4 px-4 py-3 rounded-2xl max-w-[85%] bg-slate-800/60 border border-slate-700/50 text-slate-200 shadow-sm">
                <div className="text-[10px] mb-2 font-semibold uppercase tracking-wider text-slate-500">
                  assistant
                </div>
                <div className="flex items-center gap-2 text-sm text-slate-300 mb-1">
                  {currentToolUse ? (
                    <>
                      <span className="inline-block w-3 h-3 rounded-full border-2 border-amber-400 border-t-transparent animate-spin"></span>
                      <span className="font-semibold">
                        {getToolIcon(currentToolUse.tool_name)} {currentToolUse.tool_name}
                      </span>
                      <span className="text-amber-400">运行中…</span>
                      {formatToolDetail(currentToolUse.tool_name, currentToolUse.input) && (
                        <span className="text-slate-500 text-xs ml-1">
                          ({formatToolDetail(currentToolUse.tool_name, currentToolUse.input)})
                        </span>
                      )}
                    </>
                  ) : (
                    <>
                      <span className="flex gap-1">
                        <span className="w-2 h-2 bg-sky-400 rounded-full animate-bounce" style={{ animationDelay: '0ms' }}></span>
                        <span className="w-2 h-2 bg-sky-400 rounded-full animate-bounce" style={{ animationDelay: '150ms' }}></span>
                        <span className="w-2 h-2 bg-sky-400 rounded-full animate-bounce" style={{ animationDelay: '300ms' }}></span>
                      </span>
                      <span className="font-semibold text-slate-300">
                        {statusText || "正在思考中…"}
                      </span>
                    </>
                  )}
                </div>
                {renderActivityTimeline(activityLog)}
              </div>
            )}
            <div ref={messagesEndRef} />
          </div>

          {/* Input */}
          <div className="px-6 py-4 border-t border-slate-800/50 bg-slate-900/50 backdrop-blur-sm">
            <div className="flex gap-3 items-end">
              <div className="flex-1 relative">
                <textarea
                  ref={inputRef}
                  value={input}
                  onChange={(e) => setInput(e.target.value)}
                  onKeyDown={handleKeyDown}
                  placeholder="输入消息... (Enter 发送, Shift+Enter 换行)"
                  rows={3}
                  className="w-full bg-slate-800/50 text-slate-200 border border-slate-700/50 rounded-2xl px-5 py-3.5 text-sm resize-none outline-none focus:border-sky-600/50 focus:ring-2 focus:ring-sky-500/20 transition-all duration-200 placeholder-slate-600 font-sans"
                />
              </div>
              <div className="flex flex-col gap-2">
                {isLoading ? (
                  <button
                    type="button"
                    onClick={handleInterrupt}
                    className="bg-gradient-to-r from-red-600 to-red-700 text-white rounded-2xl px-6 py-3.5 cursor-pointer hover:from-red-500 hover:to-red-600 text-sm font-medium transition-all duration-200 shadow-lg shadow-red-900/30 hover:shadow-red-800/40 flex items-center gap-2 animate-pulse"
                  >
                    <span className="w-3 h-3 rounded-sm bg-white inline-block"></span>
                    停止
                  </button>
                ) : (
                  <button
                    type="button"
                    onClick={() => handleSubmit()}
                    disabled={!input.trim()}
                    className="bg-gradient-to-r from-sky-600 to-indigo-600 text-white rounded-2xl px-6 py-3.5 cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed hover:from-sky-500 hover:to-indigo-500 text-sm font-medium transition-all duration-200 shadow-lg shadow-sky-900/30 hover:shadow-sky-800/40 flex items-center gap-2"
                  >
                    发送 →
                  </button>
                )}
              </div>
            </div>
          </div>
        </div>

        {/* 右侧面板：文件资源管理器 */}
        <div className="w-72 flex flex-col bg-slate-900/30 overflow-hidden border-l border-slate-800/50">
          <div className="flex-1 flex flex-col overflow-hidden">
            <div className="px-5 py-4 border-b border-slate-800/50 text-xs font-semibold text-slate-400 tracking-wide uppercase flex items-center gap-2">
              📂 文件资源管理器
            </div>
            <div className="px-4 py-3 border-b border-slate-800/50 space-y-2">
              <button
                type="button"
                onClick={handleSelectDirectory}
                className="w-full text-xs bg-slate-800/80 text-slate-300 hover:text-white px-4 py-2 rounded-lg hover:bg-slate-700/80 border border-slate-700/50 transition-all duration-200 flex items-center justify-center gap-1.5"
              >
                📁 项目目录
              </button>
              {projectDir && (
                <div className="flex items-center gap-2 text-xs text-slate-500 px-1">
                  <span className="text-slate-400">📍</span>
                  <span className="truncate" title={projectDir}>{projectDir}</span>
                </div>
              )}
              {!projectDir && (
                <div className="text-xs text-slate-600 text-center">
                  未选择项目目录
                </div>
              )}
            </div>
            <div className="flex-1 overflow-y-auto py-2">
              {loadingFiles ? (
                <div className="text-xs text-slate-500 p-4">加载中...</div>
              ) : !projectDir ? (
                <div className="text-xs text-slate-600 p-4 text-center">
                  请选择项目目录
                </div>
              ) : files.length === 0 ? (
                <div className="text-xs text-slate-600 p-4 text-center">目录为空</div>
              ) : (
                <div className="text-xs">
                  {files.map((file, i) => (
                    <div
                      key={i}
                      onClick={() => handleFileClick(file)}
                      onContextMenu={(e) => {
                        e.preventDefault();
                        e.stopPropagation();
                        setContextMenu({ x: e.clientX, y: e.clientY, filePath: file.path });
                      }}
                      className={`px-4 py-2 cursor-pointer flex items-center gap-2.5 transition-all duration-150 group ${
                        selectedFile === file.path
                          ? "bg-sky-900/30 text-sky-400 border-l-2 border-sky-500"
                          : "text-slate-400 hover:bg-slate-800/50 hover:text-slate-300 border-l-2 border-transparent"
                      }`}
                    >
                      <span className={`text-base ${selectedFile === file.path ? "text-sky-400" : "text-slate-600 group-hover:text-slate-500"}`}>
                        {file.is_dir ? "📁" : "📄"}
                      </span>
                      <span className="truncate flex-1">{file.name}</span>
                      {!file.is_dir && file.size > 0 && (
                        <span className="text-[10px] text-slate-600">
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

      {contextMenu && (
        <div
          onClick={(e) => e.stopPropagation()}
          onContextMenu={(e) => {
            e.preventDefault();
            e.stopPropagation();
          }}
          className="fixed z-50 min-w-[180px] py-1.5 rounded-lg bg-slate-800 border border-slate-700 shadow-2xl shadow-black/40 text-sm overflow-hidden"
          style={{ left: contextMenu.x, top: contextMenu.y }}
        >
          <button
            type="button"
            onClick={() => insertPathToInput(contextMenu.filePath)}
            className="w-full px-4 py-2 text-left text-slate-300 hover:bg-sky-600/20 hover:text-sky-400 transition-colors flex items-center gap-2.5"
          >
            📎 添加路径到对话框
          </button>
          <button
            type="button"
            onClick={() => {
              const name = contextMenu.filePath.split(/[\\/]/).pop() || contextMenu.filePath;
              insertPathToInput(contextMenu.filePath);
              setInput((prev) => prev + `\n请帮我分析一下这个文件：\`${name}\``);
            }}
            className="w-full px-4 py-2 text-left text-slate-300 hover:bg-sky-600/20 hover:text-sky-400 transition-colors flex items-center gap-2.5"
          >
            💬 引用并分析此文件
          </button>
          <div className="my-1 border-t border-slate-700/60" />
          <button
            type="button"
            onClick={() => {
              navigator.clipboard?.writeText(contextMenu.filePath).catch(() => {});
              setContextMenu(null);
            }}
            className="w-full px-4 py-2 text-left text-slate-400 hover:bg-slate-700/50 hover:text-slate-200 transition-colors flex items-center gap-2.5"
          >
            📋 复制路径
          </button>
        </div>
      )}
    </div>
  );
}

export default App;
