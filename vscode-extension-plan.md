# VS Code 插件开发计划

> 评估时间: 2026-08-02
> 项目: auto-code
> 目标: 在保持现有 Wails 桌面程序不变的前提下，新增一个 VS Code 插件，复用现有 Go 核心代码

---

## 一、可行性评估

### 结论：✅ 可行

### 评估依据

#### 1. Go 核心代码高度可复用

现有架构已经把核心逻辑与前端展现层解耦：

- **核心引擎** [internal/engine/queryengine.go](file:///d:/auto-code/internal/engine/queryengine.go) 提供 `QueryEngine` 类型，暴露 `SubmitMessage`、`Interrupt`、`GetMessages`、`SetOllamaConfig`、`SetModel`、`GetSessionID` 等方法，方法签名与表现层无关。
- **API 表面层** [internal/state/bindings.go](file:///d:/auto-code/internal/state/bindings.go) 中的 `WailsBindings` 虽然名字带 "Wails"，但本质上是一组与 UI 无关的业务 API（SendMessage / GetAppState / SetOllamaConfig / ListAvailableModels / CheckOllamaHealth 等），只是把 SDKMessage 通过 `wailsRuntime.EventsEmit` 推送到前端。
- **CLI 入口** [cmd/auto-code-cli/main.go](file:///d:/auto-code/cmd/auto-code-cli/main.go) 已经证明 QueryEngine 可以脱离 Wails 在纯 Go 进程内运行。这说明核心代码不依赖 Wails 运行时。
- **工具系统** [internal/tools/](file:///d:/auto-code/internal/tools) 包含 fileedit / fileread / filewrite / bash / grep / glob 等工具，全部以纯 Go 实现，可在任意宿主进程内运行。

#### 2. 前端 UI 可参考且可裁剪

现有 React 前端 [frontend/src/App.tsx](file:///d:/auto-code/frontend/src/App.tsx) 模块清晰：

- 顶部 Header（Logo、模型名、Ollama 连接状态、Thinking/Fast 徽章、设置按钮、停止按钮）
- Ollama 配置面板（URL、API Key、模型选择、连接测试）
- 左侧对话区（消息列表 + 流式渲染 + 思考折叠 + 工具结果展示）
- 右侧文件浏览器（项目目录选择 + 文件列表）  ← 用户要求移除
- 底部输入框

用户要求：**去掉项目目录树、去掉资源目录，仅显示当前目录**。这与 VS Code 插件场景天然契合——VS Code 自带资源管理器，插件无需重复实现文件浏览；"当前目录" 即 VS Code 工作区根目录，可直接通过 `vscode.workspace.workspaceFolders` 获取。

#### 3. 唯一缺口：缺少一个 stdio/HTTP 服务端入口

现有代码没有任何 HTTP/stdio 服务端：
- Wails 程序通过 Wails IPC 通信
- CLI 通过进程内函数调用
- [internal/mcp/](file:///d:/auto-code/internal/mcp) 是 MCP **客户端**，不是服务端

因此需要在 `cmd/` 下新增一个 `auto-code-server` 入口，把 QueryEngine 包装为 stdio JSON-RPC 服务，让 VS Code 插件以子进程方式启动它。这是 VS Code 插件对接外部后端的标准模式（与 LSP、Pylance 等同构）。

---

## 二、架构设计

### 总体架构

```
┌──────────────────────────────────────────────────────────────┐
│  VS Code Extension Host (Node.js / TypeScript)               │
│                                                              │
│  ┌────────────────────┐    ┌──────────────────────────────┐  │
│  │  extension.ts      │    │  Webview (React + Vite)      │  │
│  │  - 激活/命令注册    │◄──►│  - 复用现有 App.tsx 样式     │  │
│  │  - 子进程管理       │    │  - 移除文件浏览器            │  │
│  │  - JSON-RPC 客户端  │    │  - Header 显示工作区目录     │  │
│  │  - 工作区目录注入   │    │  - 保留对话/设置/输入框      │  │
│  └─────────┬──────────┘    └──────────────────────────────┘  │
│            │ stdin/stdout (NDJSON-RPC)                        │
└────────────┼─────────────────────────────────────────────────┘
             │
             ▼
┌──────────────────────────────────────────────────────────────┐
│  auto-code-server (Go 子进程)                                │
│                                                              │
│  cmd/auto-code-server/main.go  ← 新增（约 200 行）           │
│    │                                                         │
│    ▼                                                         │
│  internal/server/stdio_server.go  ← 新增（约 300 行）        │
│    │  复用 WailsBindings 风格的 API 表面                     │
│    │  把 EventsEmit 替换为 stdout NDJSON 推送                │
│    ▼                                                         │
│  internal/engine/QueryEngine  (现有，零改动)                 │
│  internal/state/AppState     (现有，零改动)                  │
│  internal/tools/*            (现有，零改动)                  │
│  internal/api/*              (现有，零改动)                  │
└──────────────────────────────────────────────────────────────┘
```

### 通信协议：NDJSON-RPC over stdio

选择 NDJSON（每行一个 JSON）而非 JSON-RPC 2.0 批量，原因：
- 流式 SDKMessage 推送天然适合行分隔
- 实现简单，调试方便（可 tail -f 日志）
- 与 LSP 的 Header/Content-Length 相比，对 Go 实现更友好

**请求/响应**（前端 → Go）：
```json
{"id":"req-1","method":"send_message","params":{"prompt":"你好"}}
{"id":"req-2","method":"set_ollama_config","params":{"base_url":"...","api_key":"","model":"qwen3:latest"}}
```

**响应**（Go → 前端）：
```json
{"id":"req-1","result":{"success":true,"session_id":"abc"}}
```

**事件流**（Go → 前端，无 id）：
```json
{"event":"query:message","data":{"type":"stream_chunk","message":{...}}}
{"event":"query:message","data":{"type":"assistant","message":{...}}}
{"event":"query:message","data":{"type":"result","subtype":"end_turn"}}
{"event":"state:change","data":{"type":"processing_update","value":true}}
```

### 方法清单（与 WailsBindings 一一对应）

| 方法               | 对应 WailsBindings 方法            | 说明                       |
| ------------------ | --------------------------------- | ------------------------- |
| `send_message`     | SendMessage                       | 触发流式回复，事件通过推送  |
| `interrupt`        | Interrupt                         | 中断当前推理               |
| `get_messages`     | GetMessages                       | 拉取历史消息               |
| `get_app_state`    | GetAppState                       | 拉取应用状态快照           |
| `set_ollama_config`| SetOllamaConfig                   | 设置 Ollama 配置           |
| `get_ollama_config`| GetOllamaConfig                   | 获取 Ollama 配置           |
| `list_models`      | ListAvailableModels               | 列出可用模型               |
| `check_health`     | CheckOllamaHealth                 | 检查 Ollama 连接           |
| `set_model`        | SetModel                          | 切换模型                   |
| `set_thinking`     | SetThinking                       | 切换思考模式               |
| `set_fast_mode`    | SetFastMode                       | 切换快速模式               |
| `get_session_id`   | GetSessionID                      | 获取当前会话 ID            |
| `refresh_context`  | RefreshContext                    | 刷新 git 状态/记忆         |
| `get_available_tools` | GetAvailableTools               | 列出可用工具（可选）       |

---

## 三、UI 改造方案

### 现有 UI 模块拆分

| 模块               | 现状                          | 插件处理                                   |
| ------------------ | ----------------------------- | ------------------------------------------ |
| 顶部 Header        | Logo / 模型 / 健康 / 徽章     | **保留**，新增工作区目录显示                |
| Ollama 设置面板    | URL / API Key / 模型选择      | **保留**，样式直接复用                      |
| 对话区             | 消息流 / 流式 / 工具结果 / 思考 | **保留**，完全复用                          |
| 右侧文件浏览器     | 项目目录选择 + 文件列表       | **删除**（VS Code 自带资源管理器）          |
| 底部输入框         | 文本输入 + Enter 发送         | **保留**                                   |
| 项目目录选择按钮   | 调用 SelectProjectDirectory   | **删除**，改由 `vscode.workspace` 注入     |

### Header 新增"当前目录"展示

```
┌─────────────────────────────────────────────────────────────┐
│ [AC] Auto Code  [qwen3:latest]  [● 已连接 · 本地]            │
│      📁 d:\auto-code                          [⚙️ 设置]      │
└─────────────────────────────────────────────────────────────┘
```

工作区目录来源：
- 插件激活时通过 `vscode.workspace.workspaceFolders[0].uri.fsPath` 获取
- 通过 `send_message` 之前的 `set_workspace` 方法（新增）注入 Go 端的 `engineConfig.CWD`
- 工作区切换时（`vscode.workspace.onDidChangeWorkspaceFolders`）重新注入

### 工作区目录如何传递给 Go

Go 端 QueryEngine 的 `CWD` 字段决定工具（fileedit / bash / grep 等）的工作根目录。流程：

1. 插件启动 Go 子进程时，通过命令行参数 `--cwd <workspace>` 或环境变量 `AUTO_CODE_CWD=<workspace>` 传入
2. Go 端 `cmd/auto-code-server/main.go` 读取该参数，传给 `QueryEngineConfig.CWD`
3. 工作区切换时，通过 `set_workspace` 方法热更新（需要 QueryEngine 支持运行时改 CWD，目前未支持，**作为可选增强**）

---

## 四、目录结构

```
d:\auto-code\
├── cmd/                              # 现有
│   ├── auto-code-cli/                # 现有 CLI
│   ├── auto-code-mcp/                # 现有 MCP 客户端
│   └── auto-code-server/             # 【新增】stdio 服务端入口
│       └── main.go
├── internal/                         # 现有，全部复用
│   ├── server/                       # 【新增】stdio 服务端实现
│   │   ├── stdio_server.go           # NDJSON-RPC 编解码 + 方法分发
│   │   ├── protocol.go               # 请求/响应/事件类型定义
│   │   └── bindings_adapter.go       # 复用 WailsBindings 风格，事件改 stdout 推送
│   ├── engine/                       # 现有，零改动
│   ├── state/                        # 现有，零改动
│   ├── tools/                        # 现有，零改动
│   └── ...
├── frontend/                         # 现有 Wails 前端，保持不动
├── vscode-extension/                 # 【新增】VS Code 插件工程
│   ├── package.json                  # 插件清单（engine、activation、commands）
│   ├── tsconfig.json
│   ├── esbuild.config.mjs            # 打分扩展主进程
│   ├── README.md
│   ├── media/
│   │   └── icon.png
│   ├── src/                          # 扩展主进程（Node.js）
│   │   ├── extension.ts              # 激活入口、命令注册
│   │   ├── serverClient.ts           # 启动/管理 Go 子进程、NDJSON-RPC 客户端
│   │   ├── webviewPanel.ts           # Webview 面板管理
│   │   └── workspace.ts              # 工作区目录监听
│   ├── webview-ui/                   # Webview 内 UI（React + Vite）
│   │   ├── package.json
│   │   ├── vite.config.ts
│   │   ├── tsconfig.json
│   │   ├── index.html
│   │   └── src/
│   │       ├── main.tsx              # 渲染入口
│   │       ├── App.tsx               # 从 frontend/src/App.tsx 裁剪而来
│   │       ├── apiClient.ts          # 通过 acquireVsCodeApi 与扩展主进程通信
│   │       ├── index.css             # 复用 Tailwind 配置
│   │       └── components/
│   │           ├── Header.tsx        # 顶部条（含工作区目录显示）
│   │           ├── SettingsPanel.tsx # Ollama 配置面板
│   │           ├── MessageList.tsx   # 消息列表
│   │           └── InputBox.tsx      # 输入框
│   │       # 注意：不再包含 FileBrowser / ProjectDirSelector
│   └── .vscodeignore
├── main.go                           # 现有 Wails 入口，保持不动
├── wails.json                        # 现有
└── go.mod                            # 现有
```

---

## 五、实施计划

### 阶段 0：可行性评估（本文档） ✅

### 阶段 1：Go 端 stdio 服务端（核心改造）

**目标**：新建 `auto-code-server`，暴露与 WailsBindings 等价的 API over stdio。

**任务清单**：
- [ ] 新建 `internal/server/protocol.go`：定义 `Request`、`Response`、`Event` 结构体
- [ ] 新建 `internal/server/stdio_server.go`：实现 NDJSON 行编解码、读写循环、方法分发表
- [ ] 新建 `internal/server/bindings_adapter.go`：复用 AppState + QueryEngine，把 `wailsRuntime.EventsEmit` 替换为 stdout 推送
- [ ] 新建 `cmd/auto-code-server/main.go`：解析 `--cwd` 参数，初始化 QueryEngine + AppState + StdioServer，阻塞运行
- [ ] 新增 `make server` 或 `go build -o bin/auto-code-server ./cmd/auto-code-server` 构建脚本
- [ ] 单元测试：协议编解码、方法分发、事件推送

**验收标准**：
- 命令行 `echo '{"id":"1","method":"check_health","params":{}}' | ./bin/auto-code-server` 能返回健康检查结果
- `send_message` 后能持续收到 `query:message` 事件直到 `result`

### 阶段 2：VS Code 插件骨架

**目标**：搭建插件工程，能启动 Go 子进程并完成一次完整对话。

**任务清单**：
- [ ] `vscode-extension/` 工程初始化（package.json、tsconfig、esbuild）
- [ ] 实现 `src/serverClient.ts`：子进程生命周期管理 + NDJSON-RPC 客户端 + 请求/事件 Promise 化
- [ ] 实现 `src/extension.ts`：激活事件、命令注册（`auto-code.openChat`）、子进程随插件生命周期启停
- [ ] 实现 `src/webviewPanel.ts`：Webview 创建、资源加载、消息桥接（webview ↔ extension host）
- [ ] 工作区目录获取与注入：`vscode.workspace.workspaceFolders` → 命令行参数 `--cwd`
- [ ] 最小可运行验证：打开命令面板 → `Auto Code: Open Chat` → 输入消息 → 收到回复

**验收标准**：
- 在 VS Code 中能打开 Auto Code 聊天面板
- 能与 Go 后端完成一次完整对话（含流式输出）

### 阶段 3：Webview UI 移植与裁剪

**目标**：把现有 React UI 移植到 Webview，按需求裁剪。

**任务清单**：
- [ ] `webview-ui/` 工程初始化（Vite + React + TypeScript + Tailwind）
- [ ] 复制 `frontend/src/App.tsx` 到 `webview-ui/src/App.tsx`
- [ ] **删除**：`SelectProjectDirectory` / `SetProjectDirectory` / `GetProjectDirectory` / `ListDirectoryContents` 相关代码与 UI
- [ ] **删除**：右侧文件浏览器面板整块 JSX
- [ ] **替换**：所有 Wails 绑定调用（`SendMessage`、`EventsOn` 等）改为通过 `acquireVsCodeApi().postMessage` 与扩展主进程通信
- [ ] **新增**：Header 中显示当前工作区目录（由扩展主进程注入）
- [ ] 实现 `apiClient.ts`：封装 `postMessage` 为 Promise，对接 `serverClient` 的方法
- [ ] 构建集成：`npm run build` 产出 Webview 静态资源，被扩展主进程加载
- [ ] VS Code 主题适配（可选）：暗色主题与现有 Slate 配色基本一致

**验收标准**：
- UI 视觉与现有 Wails 程序高度一致（Header / 设置 / 对话 / 输入）
- 无文件浏览器、无项目目录选择按钮
- Header 显示当前 VS Code 工作区目录
- 流式输出、思考折叠、工具结果展示正常

### 阶段 4：集成完善与打包

**目标**：插件可日常使用，可打包发布。

**任务清单**：
- [ ] Ollama 健康检查自动重连
- [ ] 子进程崩溃自动重启
- [ ] 输出日志到 VS Code Output Channel（`Auto Code`）
- [ ] 配置项迁移：插件 `settings.json` 配置 Ollama URL/Model，同步到 Go 端
- [ ] 打包脚本：`vsce package` 前先 `go build` 产出对应平台的 `auto-code-server` 二进制
- [ ] 跨平台二进制分发（windows / darwin / linux）
- [ ] README 编写

**验收标准**：
- `.vsix` 安装后可正常使用
- 多次开关面板、切换工作区不崩溃

---

## 六、复用与改动清单

### Go 端：零侵入 + 少量新增

| 模块                          | 处理方式                                   |
| ----------------------------- | ------------------------------------------ |
| `internal/engine/*`           | 零改动，直接复用                            |
| `internal/state/AppState`     | 零改动，直接复用                            |
| `internal/state/bindings.go`  | 零改动（保留给 Wails 用）                   |
| `internal/tools/*`            | 零改动，直接复用                            |
| `internal/api/*`              | 零改动，直接复用                            |
| `internal/perception/*`       | 零改动，直接复用                            |
| `internal/planning/*`         | 零改动，直接复用                            |
| `internal/reflection/*`       | 零改动，直接复用                            |
| `internal/mcp/*`              | 零改动，直接复用                            |
| `internal/server/*`           | **新增** stdio 服务端                       |
| `cmd/auto-code-server/*`      | **新增** 入口                               |

### 前端：移植 + 裁剪

| 文件                              | 处理方式                                   |
| --------------------------------- | ------------------------------------------ |
| `frontend/src/App.tsx`            | 复制到 `webview-ui/src/App.tsx` 后裁剪      |
| `frontend/src/index.css`          | 复制（Tailwind 入口）                       |
| `frontend/tailwind.config.js`     | 复制                                       |
| `frontend/package.json`           | 参考新建（去掉 Wails 依赖，加 vite 中间件） |
| Wails 绑定调用（`SendMessage` 等） | **替换**为 `postMessage` 通信               |
| `EventsOn` / `EventsOff`          | **替换**为 `message` 事件监听               |
| 文件浏览器相关代码                | **删除**                                    |
| 项目目录选择相关代码              | **删除**                                    |

### VS Code 插件：全新

全部为新增文件，详见"目录结构"。

---

## 七、风险与对策

| 风险                                       | 等级   | 对策                                                       |
| ------------------------------------------ | ------ | ---------------------------------------------------------- |
| Go 子进程在 Windows 路径含中文/空格时启动失败 | 中     | 启动时用 `shell: false` + 参数数组，路径加引号；测试覆盖    |
| Webview CSP 限制导致 Tailwind/字体加载失败  | 中     | 使用 `localResourceRoots` + `nonce`；资源打包进扩展         |
| 流式输出在 Webview 中卡顿                   | 低     | 使用 `postMessage` 批量转发；React 端用 `useReducer` 合并   |
| 工作区切换时 Go 端 CWD 无法热更新           | 中     | 阶段 1 不支持；阶段 4 为 QueryEngine 增设 `SetCWD` 方法     |
| Go 二进制跨平台分发体积大                   | 低     | 每平台单独打包；可选 UPX 压缩                              |
| 与 Wails 版本配置文件冲突                   | 低     | 插件使用独立的 `~/.auto-code/vscode-config.json`           |
| `wailsRuntime.EventsEmit` 在 server 模式下不可用 | 已规避 | server 端不依赖 Wails runtime，直接写 stdout               |

---

## 八、关键技术点说明

### 1. Go 子进程启动方式

```typescript
// src/serverClient.ts 示意
import { spawn } from 'child_process';
import * as path from 'path';

const serverPath = path.join(context.extensionPath, 'bin', 'auto-code-server');
const cwd = vscode.workspace.workspaceFolders?.[0].uri.fsPath;
const child = spawn(serverPath, ['--cwd', cwd], { shell: false });
```

### 2. NDJSON 读写（Go 端示意）

```go
// internal/server/stdio_server.go 示意
scanner := bufio.NewScanner(os.Stdin)
scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
for scanner.Scan() {
    line := scanner.Bytes()
    var req Request
    if err := json.Unmarshal(line, &req); err != nil { continue }
    go s.handleRequest(req)  // 响应写回 stdout（加锁）
}
```

### 3. Webview 与扩展主进程通信

```typescript
// webview 内
const vscode = acquireVsCodeApi();
vscode.postMessage({ id: 'req-1', method: 'send_message', params: { prompt } });
window.addEventListener('message', (e) => {
  const msg = e.data;
  // 分发响应 / 事件
});
```

```typescript
// 扩展主进程内
panel.webview.onDidReceiveMessage(async (msg) => {
  if (msg.method) {
    const result = await serverClient.request(msg.method, msg.params);
    panel.webview.postMessage({ id: msg.id, result });
  }
});
```

---

## 九、待用户确认事项

在开始编码前，请确认以下方案选择：

1. **通信协议**：采用 NDJSON-RPC over stdio（推荐），还是本地 HTTP + SSE？
2. **Webview UI 技术栈**：沿用 React + Vite + Tailwind（推荐，最大化复用），还是改用原生 VS Code Webview UI Toolkit？
3. **Go 二进制分发**：随扩展打包每平台二进制（推荐），还是要求用户本地 `go build`？
4. **配置存储**：插件独立配置文件（推荐），还是直接复用 `~/.auto-code/config.json`？
5. **CWD 热更新**：阶段 1 是否需要支持工作区切换时热更新 CWD（需要小改 QueryEngine），还是先要求重启面板？
6. **目录命名**：`vscode-extension/` 是否合适？是否需要更短的名字如 `vscode/`？

---

## 十、总结

本方案在**零侵入现有 Go 核心**的前提下，通过新增一个 stdio 服务端入口 + 一个 VS Code 插件工程，实现 Go 代码的完整复用。UI 沿用现有 React 实现，按需求裁剪掉文件浏览器与项目目录选择，新增工作区目录显示。整体改动量可控（Go 端约 500 行新增，TS 端为标准插件工程），技术风险可控。

等待用户确认上述方案与待决事项后，即可进入阶段 1 编码。
