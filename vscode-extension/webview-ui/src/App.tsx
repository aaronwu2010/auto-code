import { useState, useEffect, useRef, useCallback, useMemo } from "react";
import { marked } from "marked";
import DOMPurify from "dompurify";
import { api } from "./apiClient";
import type {
  Message,
  ContentBlock,
  AppStateSnapshot,
  OllamaConfig,
  OllamaHealth,
  ModelInfo,
  SDKMessage,
  StateChangeEvent,
} from "./types";

// 初始化 marked：GFM 风格 + 代码块围栏 + 宽松换行；不使用任何高亮器以避免体积
marked.setOptions({
  gfm: true,
  breaks: true,
});

/** 快速启发式判断字符串是否包含 Markdown 结构（标题/围栏/表/列表/引用/链接/行内代码块）。若不包含则回退到纯文本渲染，避免普通对话被加 `<p>` 标签。 */
function looksLikeMarkdown(text: string): boolean {
  if (!text) return false;
  const mdSignals = [
    /^\s{0,3}#{1,6}\s+\S/m,
    /^\s{0,3}```/m,
    /^\s{0,3}>\s?\S/m,
    /^\s{0,3}[-*+]\s+\S/m,
    /^\s{0,3}\d+\.\s+\S/m,
    /^\s{0,3}\|[^\n]+\|\s*$/m,
    /\[([^\]]+)\]\((https?:\/\/|\/|\.)[^)]*\)/,
    /(^|[^`])`[^`\n]+`([^`]|$)/,
    /^\s{0,3}\S[^\n]*\n\s{0,3}[-=]{2,}\s*$/m,
    /\*\*[^*\n]+\*\*|__[^_\n]+__/,
  ];
  return mdSignals.some((rx) => rx.test(text));
}

/** 统一 Markdown 渲染：marked -> DOMPurify -> React 注入。
 *  若文本不像 MD，则直接走纯文本换行，避免单行被包 p 标签引入行距。 */
function MarkdownContent({ text, className = "" }: { text: string; className?: string }) {
  const html = useMemo(() => {
    const src = text ?? "";
    if (!looksLikeMarkdown(src)) {
      return null;
    }
    try {
      const raw = marked.parse(src, { async: false }) as string;
      return DOMPurify.sanitize(raw, {
        ADD_ATTR: ["target"],
      });
    } catch {
      return null;
    }
  }, [text]);

  if (html === null) {
    return (
      <pre
        className={`whitespace-pre-wrap break-words text-sm leading-relaxed ${className}`}
      >
        {text ?? ""}
      </pre>
    );
  }
  return (
    <div
      className={`markdown-body ${className}`}
      dangerouslySetInnerHTML={{ __html: html }}
    />
  );
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

  // 当前工作区目录（由扩展主进程注入）
  const [workspaceDir, setWorkspaceDir] = useState<string>("");
  const [streamingMessage, setStreamingMessage] = useState<Message | null>(null);
  const [isToolCalling, setIsToolCalling] = useState(false);
  const [phaseHint, setPhaseHint] = useState<string>("");

  const scrollToBottom = useCallback(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth", block: "end" });
  }, []);

  useEffect(() => {
    scrollToBottom();
  }, [messages, streamingMessage, isToolCalling, phaseHint, scrollToBottom]);

  // 每隔 120ms 强制滚动到底（针对流式追加时高频不 scroll 的兜底）
  useEffect(() => {
    if (!isLoading) return;
    const t = window.setInterval(scrollToBottom, 120);
    return () => window.clearInterval(t);
  }, [isLoading, scrollToBottom]);

  useEffect(() => {
    if (showSettings && models.length === 0 && !loadingModels) {
      loadModels();
    }
  }, [showSettings]);

  const loadConfig = async () => {
    try {
      const config = await api.getOllamaConfig();
      if (config) setOllamaConfig(config);
    } catch {
      /* ignore */
    }
  };

  const loadModels = async () => {
    setLoadingModels(true);
    setModelsError(null);
    try {
      const result = await api.listModels();
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
      const health = await api.checkHealth();
      if (health) {
        setOllamaHealth(health);
        if (health.connected) {
          setHealthCheckResult(
            `✓ 已连接到 ${health.base_url}，发现 ${health.available_models || 0} 个模型`
          );
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
      await api.setOllamaConfig(ollamaConfig);
      await checkHealth();
      await loadModels();
    } catch {
      /* ignore */
    }
  };

  useEffect(() => {
    const offQuery = api.onQueryMessage((msg: SDKMessage) => {
      try {
        if (msg.type === "stream_chunk" && msg.message) {
          setStreamingMessage((prev) => {
            // 只有在内容/思考变长（或首条）时才替换，避免覆盖"更长的版本"
            if (!prev) return msg.message!;
            const newContent = msg.message!.content ?? "";
            const oldContent = prev.content ?? "";
            const newThinking = msg.message!.thinking ?? "";
            const oldThinking = prev.thinking ?? "";
            if (
              newContent.length >= oldContent.length ||
              newThinking.length >= oldThinking.length
            ) {
              return msg.message!;
            }
            return prev;
          });
          setIsToolCalling(
            (msg.message.tool_calls && msg.message.tool_calls.length > 0) || isToolCalling
          );
          setPhaseHint("");
        } else if (msg.type === "tool_calls_start") {
          setIsToolCalling(true);
          setPhaseHint("正在调用工具（读取文件/执行命令）...");
        } else if (msg.type === "user" && msg.message) {
          setMessages((prev) => {
            const exists = prev.some((m) => m.id === msg.message!.id);
            if (exists) return prev;
            return [...prev, msg.message!];
          });
          setPhaseHint("已发送，模型正在思考...");
        } else if (msg.type === "assistant" && msg.message) {
          setStreamingMessage(null);
          setIsToolCalling(false);
          setPhaseHint("");
          setMessages((prev) => {
            const exists = prev.some((m) => m.id === msg.message!.id);
            if (exists) return prev;
            return [...prev, msg.message!];
          });
        } else if (msg.type === "system" && msg.message) {
          setMessages((prev) => {
            const exists = prev.some((m) => m.id === msg.message!.id);
            if (exists) return prev;
            return [...prev, msg.message!];
          });
        } else if (msg.type === "stream_event") {
          const d = msg.data as Record<string, unknown> | undefined;
          if (d && typeof d.model === "string") {
            setPhaseHint(`模型推理中 · ${String(d.model)}`);
          }
        } else if (msg.message) {
          setMessages((prev) => {
            const exists = prev.some((m) => m.id === msg.message!.id);
            if (exists) return prev;
            return [...prev, msg.message!];
          });
        }
        if (msg.type === "result" || msg.type === "error" || msg.type === "interrupted") {
          setIsLoading(false);
          setStreamingMessage(null);
          setIsToolCalling(false);
          setPhaseHint("");
        }
      } catch {
        /* ignore */
      }
    });

    const offState = api.onStateChange((event: StateChangeEvent) => {
      if (event.type === "status_update") {
        setStatusText(String(event.value ?? ""));
      }
      if (event.type === "processing_update") {
        setIsLoading(Boolean(event.value));
      }
    });

    const offWorkspace = api.onWorkspaceChange((dir: string) => {
      setWorkspaceDir(dir);
    });

    // 初始化加载
    (async () => {
      try {
        const state = await api.getAppState();
        if (state) setAppState(state);

        const msgs = await api.getMessages();
        if (msgs) setMessages(msgs.messages || []);

        await loadConfig();
        await checkHealth();
        await loadModels();

        // 主动请求当前工作区
        api.requestWorkspace();
      } catch {
        /* ignore */
      }
    })();

    return () => {
      offQuery();
      offState();
      offWorkspace();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleSubmit = async () => {
    if (!input.trim() || isLoading) return;

    const currentInput = input;
    setInput("");
    setIsLoading(true);
    setIsToolCalling(false);

    try {
      await api.sendMessage(currentInput);
    } catch {
      setIsLoading(false);
      setIsToolCalling(false);
    }
  };

  const handleInterrupt = () => {
    api.interrupt();
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
          <pre
            key={idx}
            className="whitespace-pre-wrap break-words text-sm leading-relaxed text-slate-200"
          >
            {block.text}
          </pre>
        );
      case "tool_use":
        return null;
      case "tool_result":
        return (
          <div
            key={idx}
            className={`rounded-lg px-3 py-2 my-1.5 text-sm ${
              block.is_error
                ? "bg-red-900/20 border border-red-800/40"
                : "bg-emerald-900/20 border border-emerald-800/40"
            }`}
          >
            <pre className="whitespace-pre-wrap break-words text-slate-300">
              {block.tool_output}
            </pre>
          </div>
        );
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
        {msg.thinking && !isUser && (
          <details className="bg-violet-900/10 border border-violet-800/30 rounded-lg px-3 py-2 my-1.5">
            <summary className="text-xs text-violet-400 cursor-pointer hover:text-violet-300 transition-colors select-none">
              💭 思考过程（{msg.thinking.length} 字）
            </summary>
            <pre className="whitespace-pre-wrap break-words text-xs text-slate-500 mt-2">
              {msg.thinking}
            </pre>
          </details>
        )}
        {msg.content_blocks && msg.content_blocks.length > 0
          ? msg.content_blocks.map((block, i) => renderContentBlock(block, i))
          : isUser
          ? (
            <pre className="whitespace-pre-wrap break-words text-sm leading-relaxed">
              {msg.content}
            </pre>
          )
          : (
            <MarkdownContent text={msg.content} />
          )}
      </div>
    );
  };

  // 简化路径显示（仅保留最后两层目录，避免 Header 过长）
  const displayWorkspace = workspaceDir
    ? workspaceDir.split(/[\\/]/).slice(-2).join("/") || workspaceDir
    : "";

  return (
    <div className="flex flex-col h-screen bg-gradient-to-br from-slate-950 via-slate-900 to-slate-950 text-slate-200 font-sans">
      {/* Header */}
      <div className="flex items-center justify-between px-6 py-3 border-b border-slate-800/50 bg-slate-900/80 backdrop-blur-sm">
        <div className="flex items-center gap-3 min-w-0">
          <div className="w-8 h-8 rounded-lg bg-gradient-to-br from-sky-500 to-indigo-600 flex items-center justify-center text-white font-bold text-sm shadow-lg flex-shrink-0">
            AC
          </div>
          <span className="text-base font-semibold bg-gradient-to-r from-sky-400 to-indigo-400 bg-clip-text text-transparent flex-shrink-0">
            Auto Code
          </span>
          {appState?.mainLoopModel && (
            <span className="text-xs text-slate-400 bg-slate-800/80 px-3 py-1 rounded-full border border-slate-700/50 flex-shrink-0 flex items-center gap-1.5">
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
            <span
              className={`text-xs px-3 py-1 rounded-full border flex items-center gap-1.5 flex-shrink-0 ${
                ollamaHealth.connected
                  ? "bg-emerald-900/30 text-emerald-400 border-emerald-800/40"
                  : "bg-red-900/30 text-red-400 border-red-800/40"
              }`}
            >
              <span
                className={`w-1.5 h-1.5 rounded-full ${
                  ollamaHealth.connected ? "bg-emerald-400 animate-pulse" : "bg-red-400"
                }`}
              ></span>
              {ollamaHealth.connected ? "已连接" : "未连接"}
              {ollamaHealth.is_local ? " · 本地" : " · 云端"}
            </span>
          )}
          {/* 当前工作区目录（替代原项目目录选择按钮） */}
          {displayWorkspace && (
            <span
              className="text-xs text-slate-400 bg-slate-800/60 px-3 py-1 rounded-full border border-slate-700/40 flex items-center gap-1.5 min-w-0"
              title={workspaceDir}
            >
              📁 <span className="truncate">{displayWorkspace}</span>
            </span>
          )}
          {appState?.thinkingEnabled && (
            <span className="text-xs text-violet-400 bg-violet-900/30 px-3 py-1 rounded-full border border-violet-800/40 flex-shrink-0">
              🧠 Thinking
            </span>
          )}
          {appState?.fastMode && (
            <span className="text-xs text-amber-400 bg-amber-900/30 px-3 py-1 rounded-full border border-amber-800/40 flex-shrink-0">
              ⚡ Fast
            </span>
          )}
        </div>
        <div className="flex items-center gap-3 flex-shrink-0">
          {statusText && <span className="text-xs text-slate-500">{statusText}</span>}
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
              <div
                className={`text-sm p-3 rounded-xl border mb-5 flex items-center gap-2 ${
                  ollamaHealth.connected
                    ? "bg-emerald-900/20 text-emerald-400 border-emerald-800/30"
                    : "bg-red-900/20 text-red-400 border-red-800/30"
                }`}
              >
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
                  onChange={(e) =>
                    setOllamaConfig({ ...ollamaConfig, base_url: e.target.value })
                  }
                  placeholder="http://localhost:11434/api"
                  className="w-full bg-slate-800/50 text-slate-200 border border-slate-700/50 rounded-xl px-4 py-2.5 text-sm outline-none focus:border-sky-600/50 focus:ring-2 focus:ring-sky-500/20 transition-all duration-200 placeholder-slate-600"
                />
              </div>
              <div>
                <label className="text-xs text-slate-400 block mb-2 font-medium">
                  API Key（可选）
                </label>
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
                  onChange={(e) =>
                    setOllamaConfig({ ...ollamaConfig, model: e.target.value })
                  }
                  className="flex-1 bg-slate-800/50 text-slate-200 border border-slate-700/50 rounded-xl px-4 py-2.5 text-sm outline-none focus:border-sky-600/50 focus:ring-2 focus:ring-sky-500/20 transition-all duration-200 cursor-pointer"
                >
                  <option value="">选择模型...</option>
                  {models.map((m) => (
                    <option key={m.name} value={m.name}>
                      {m.name} {m.size && `(${m.size})`}{" "}
                      {m.parameter_size && `- ${m.parameter_size}`}{" "}
                      {m.context_length ? `- ${m.context_length.toLocaleString()} tokens` : ""}
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
              {modelsError && <p className="text-xs text-red-400 mt-2">{modelsError}</p>}
              {models.length === 0 && !loadingModels && !modelsError && (
                <p className="text-xs text-slate-500 mt-2">
                  未找到模型，请确保 Ollama 服务正在运行，或手动输入模型名称
                </p>
              )}
            </div>

            <div className="mb-5">
              <label className="text-xs text-slate-400 block mb-2 font-medium">
                或手动输入模型名称
              </label>
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
              <div
                className={`text-sm mt-3 flex items-center gap-2 ${
                  healthCheckResult.includes("✓") ? "text-emerald-400" : "text-red-400"
                }`}
              >
                {healthCheckResult}
              </div>
            )}
          </div>
        </div>
      )}

      {/* 主内容区域：仅对话区（已移除右侧文件浏览器） */}
      <div className="flex flex-1 overflow-hidden">
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
                {workspaceDir && (
                  <div className="text-slate-600 text-xs mt-2" title={workspaceDir}>
                    当前目录：{workspaceDir}
                  </div>
                )}
              </div>
            )}
            {messages.map(renderMessage)}
            {(isLoading || streamingMessage || isToolCalling) && (
              <div className="mb-4 px-4 py-3 rounded-2xl max-w-[85%] bg-slate-800/60 border border-slate-700/50 text-slate-200 shadow-sm">
                <div className="flex items-center gap-2 text-[10px] mb-2 font-semibold uppercase tracking-wider text-slate-500">
                  <span>assistant</span>
                  {phaseHint && (
                    <span className="ml-auto text-[10px] text-slate-500 normal-case tracking-normal">
                      {phaseHint}
                    </span>
                  )}
                </div>
                {/* Thinking（流式：默认展开，实时追加） */}
                {streamingMessage?.thinking && (
                  <details
                    open
                    className="bg-violet-900/15 border border-violet-800/40 rounded-lg px-3 py-2 my-1.5"
                  >
                    <summary className="text-xs text-violet-400 cursor-pointer hover:text-violet-300 transition-colors select-none">
                      💭 思考过程（{streamingMessage.thinking.length} 字，实时追加）
                    </summary>
                    <pre className="whitespace-pre-wrap break-words text-xs text-slate-500 mt-2 max-h-64 overflow-y-auto">
                      {streamingMessage.thinking}
                      <span className="inline-block w-1.5 h-3 bg-violet-400 ml-0.5 align-middle animate-pulse rounded-sm" />
                    </pre>
                  </details>
                )}
                {/* Content（流式，Markdown 实时渲染；尾部追加光标；若无内容则显示 loading 点） */}
                {streamingMessage?.content ? (
                  <div className="relative">
                    <MarkdownContent text={streamingMessage.content} className="pr-3" />
                    <span
                      aria-hidden
                      className="inline-block w-2 h-4 bg-sky-400 ml-1 align-middle animate-pulse rounded-sm align-bottom"
                    />
                  </div>
                ) : (
                  <div className="text-sm leading-relaxed whitespace-pre-wrap break-words min-h-[1.6em]">
                    {!streamingMessage?.content && isToolCalling && !phaseHint ? (
                      <span className="flex items-center gap-2 text-amber-400">
                        <span className="flex gap-1">
                          <span
                            className="w-2 h-2 bg-amber-400 rounded-full animate-bounce"
                            style={{ animationDelay: "0ms" }}
                          ></span>
                          <span
                            className="w-2 h-2 bg-amber-400 rounded-full animate-bounce"
                            style={{ animationDelay: "150ms" }}
                          ></span>
                          <span
                            className="w-2 h-2 bg-amber-400 rounded-full animate-bounce"
                            style={{ animationDelay: "300ms" }}
                          ></span>
                        </span>
                        正在调用工具...
                      </span>
                    ) : (
                      <span className="inline-block w-2 h-4 bg-sky-400 ml-0.5 align-middle animate-pulse rounded-sm" />
                    )}
                  </div>
                )}
                {/* ToolCall 预览卡片（当 tool_calls 出现时即时展示） */}
                {streamingMessage?.tool_calls && streamingMessage.tool_calls.length > 0 && (
                  <div className="mt-2 space-y-1.5">
                    {streamingMessage.tool_calls.map((tc, i) => (
                      <div
                        key={i}
                        className="rounded-lg px-3 py-2 bg-amber-900/20 border border-amber-800/40 text-xs"
                      >
                        <div className="text-amber-400 font-medium mb-1 flex items-center gap-1.5">
                          🔧 {tc.name || `tool_${i}`}
                          {tc.id && <span className="text-[10px] text-slate-500">#{tc.id.slice(0, 8)}</span>}
                        </div>
                        {tc.input && (
                          <pre className="whitespace-pre-wrap break-words text-slate-400 max-h-40 overflow-y-auto">
                            {typeof tc.input === "string"
                              ? tc.input
                              : JSON.stringify(tc.input, null, 2)}
                          </pre>
                        )}
                      </div>
                    ))}
                  </div>
                )}
                {/* 没有 streamingMessage，但处于 tool_calls_start 或 phaseHint 的兜底显示 */}
                {!streamingMessage && (phaseHint || isToolCalling) && (
                  <div className="flex items-center gap-2 text-amber-400 text-sm pt-1">
                    <span className="flex gap-1">
                      <span className="w-2 h-2 bg-amber-400 rounded-full animate-bounce" style={{ animationDelay: "0ms" }}></span>
                      <span className="w-2 h-2 bg-amber-400 rounded-full animate-bounce" style={{ animationDelay: "150ms" }}></span>
                      <span className="w-2 h-2 bg-amber-400 rounded-full animate-bounce" style={{ animationDelay: "300ms" }}></span>
                    </span>
                    <span>{phaseHint || "正在处理..."}</span>
                  </div>
                )}
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
      </div>
    </div>
  );
}

export default App;
