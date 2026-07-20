import { useState, useEffect, useRef, useCallback } from "react";
import type {
  Message,
  ContentBlock,
  AppStateSnapshot,
  SDKMessage,
  StateChangeEvent,
} from "./bindings/types";

declare global {
  interface Window {
    go: {
      main: {
        state: {
          WailsBindings: {
            SendMessage: (req: string) => Promise<string>;
            Interrupt: () => void;
            GetMessages: () => Promise<string>;
            GetAppState: () => Promise<string>;
            SetModel: (req: string) => void;
            SetPermissionMode: (req: string) => void;
            SetThinking: (req: string) => void;
            SetFastMode: (req: string) => void;
            GetAvailableTools: () => Promise<string>;
            GetSessionID: () => Promise<string>;
            RefreshContext: () => Promise<void>;
            AddTodo: (req: string) => void;
            UpdateTodoStatus: (id: string, status: string) => void;
            GetMCPStatus: () => Promise<string>;
          };
        };
      };
    };
    runtime: {
      EventsOn: (event: string, callback: (...args: unknown[]) => void) => void;
      EventsOff: (event: string) => void;
    };
  }
}

function App() {
  const [messages, setMessages] = useState<Message[]>([]);
  const [input, setInput] = useState("");
  const [isLoading, setIsLoading] = useState(false);
  const [appState, setAppState] = useState<AppStateSnapshot | null>(null);
  const [statusText, setStatusText] = useState("");
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);

  const scrollToBottom = useCallback(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, []);

  useEffect(() => {
    scrollToBottom();
  }, [messages, scrollToBottom]);

  useEffect(() => {
    if (window.runtime) {
      window.runtime.EventsOn("state:change", (data: unknown) => {
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

      window.runtime.EventsOn("query:message", (data: unknown) => {
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
    }

    loadInitialState();

    return () => {
      if (window.runtime) {
        window.runtime.EventsOff("state:change");
        window.runtime.EventsOff("query:message");
      }
    };
  }, []);

  const loadInitialState = async () => {
    try {
      if (!window.go?.main?.state?.WailsBindings) return;
      const stateStr = await window.go.main.state.WailsBindings.GetAppState();
      if (stateStr) {
        setAppState(JSON.parse(stateStr));
      }
      const msgsStr = await window.go.main.state.WailsBindings.GetMessages();
      if (msgsStr) {
        setMessages(JSON.parse(msgsStr));
      }
    } catch {}
  };

  const handleSubmit = async () => {
    if (!input.trim() || isLoading) return;
    if (!window.go?.main?.state?.WailsBindings) return;

    const userMsg: Message = {
      id: `user-${Date.now()}`,
      role: "user",
      content: [{ type: "text", text: input }],
      timestamp: Date.now(),
    };
    setMessages((prev) => [...prev, userMsg]);
    setInput("");
    setIsLoading(true);

    try {
      const req = JSON.stringify({ prompt: input });
      await window.go.main.state.WailsBindings.SendMessage(req);
    } catch (err) {
      setIsLoading(false);
    }
  };

  const handleInterrupt = () => {
    if (window.go?.main?.state?.WailsBindings) {
      window.go.main.state.WailsBindings.Interrupt();
    }
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
        {msg.content.map((block, i) => renderContentBlock(block, i))}
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
            rows={2}
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
