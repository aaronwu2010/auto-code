# Auto Code VS Code 插件

基于 [auto-code](../) Go 核心的 VS Code 插件版本，复用相同的 QueryEngine、工具系统和上下文管理。

## 使用方法

### 安装

#### 方式一：从 .vsix 安装（推荐给最终用户）

1. 获取 `auto-code-<version>.vsix` 文件（由发布流程产出，或在仓库根目录执行 `cd vscode-extension && npm run vsce:package` 自行打包）。
2. 打开 VS Code → 扩展面板 → 右上角 `...` → `从 VSIX 安装...` → 选择 `.vsix` 文件。
3. 重新加载窗口（`Ctrl+Shift+P` → `Developer: Reload Window`）。

#### 方式二：从源码构建安装（开发者）

```powershell
# 1. 构建 Go 后端（在仓库根目录）
go build -o vscode-extension\bin\auto-code-server.exe .\cmd\auto-code-server

# 2. 安装依赖并打包
cd vscode-extension
npm install
cd webview-ui && npm install && cd ..
npm run vsce:package      # 产出 auto-code-0.1.0.vsix

# 3. 按"方式一"安装产出的 .vsix
```

### 前置条件

- **Ollama 服务**：需要本地或远程运行的 Ollama 服务，并已拉取至少一个模型（如 `ollama pull qwen3:latest`）。
- **VS Code 版本**：≥ 1.85.0。
- **工作区**：建议打开一个文件夹作为工作区，插件的工具（文件读写、bash、grep 等）会以此目录为根目录。

### 首次配置

1. 打开命令面板（`Ctrl+Shift+P` / macOS `Cmd+Shift+P`）。
2. 执行 `Auto Code: Open Chat` 打开对话面板。
3. 点击右上角 **⚙️ 设置** 按钮，填写：
   - **Ollama URL**：如 `http://localhost:11434/api`（本地）或云端地址
   - **API Key**：本地模式留空，云端填入 API Key
   - **模型**：从下拉列表选择，或手动输入模型名
4. 点击 **💾 保存配置** → **🔌 测试连接**，确认连接成功。

### 日常使用

| 操作 | 入口 |
| --- | --- |
| 打开对话面板 | 命令面板 → `Auto Code: Open Chat` |
| 重启后端进程 | 命令面板 → `Auto Code: Restart Server`（用于配置变更或后端异常） |
| 发送消息 | 在输入框输入文本，`Enter` 发送，`Shift+Enter` 换行 |
| 中断推理 | 点击底部 **取消** 按钮，或 Header 的 **⏹ 停止** 按钮 |
| 查看后端日志 | VS Code 输出面板 → 选择 `Auto Code` 通道 |

### 配置项（settings.json）

可在 VS Code `settings.json` 中预置以下配置，避免每次在 UI 中手动填写：

| 配置键 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `auto-code.serverPath` | string | `""` | auto-code-server 可执行文件路径。留空则使用扩展自带的 `bin/auto-code-server` |
| `auto-code.ollamaBaseUrl` | string | `http://localhost:11434/api` | Ollama 服务地址 |
| `auto-code.ollamaApiKey` | string | `""` | Ollama API Key（本地模式留空） |
| `auto-code.ollamaModel` | string | `""` | 默认模型名称（留空使用服务端默认值） |

示例：

```json
{
  "auto-code.ollamaBaseUrl": "http://192.168.1.100:11434/api",
  "auto-code.ollamaModel": "qwen3:latest"
}
```

> 注意：`settings.json` 中的配置在插件启动时读取一次；修改后需执行 `Auto Code: Restart Server` 生效。

## 架构

```
┌─────────────────────┐     NDJSON-RPC      ┌──────────────────────┐
│  VS Code Webview    │  <-> Extension Host │  auto-code-server    │
│  (React + Tailwind) │     (postMessage)   │  (Go stdio server)   │
└─────────────────────┘                     └──────────────────────┘
                                                     │
                                                     ▼
                                            复用 internal/engine
                                            internal/state 等核心
```

- **Go 端**（`cmd/auto-code-server`）：通过 stdio 暴露 NDJSON-RPC 协议，复用 `QueryEngine`、`AppState`、`ContextBuilder` 等核心组件。
- **扩展主进程**（`src/extension.ts`）：启动/管理 Go 子进程，桥接 Webview 与 server。
- **Webview UI**（`webview-ui/`）：基于 React + Tailwind 的对话界面，复用现有前端样式，已移除文件浏览器，改为在 Header 显示当前工作区目录。

## 通信协议

每行一个 JSON 对象（NDJSON），行尾 `\n`。

- 请求：`{"id":"req-1","method":"send_message","params":{...}}`
- 响应：`{"id":"req-1","result":{...}}` / `{"id":"req-1","error":{"code":...,"message":...}}`
- 事件：`{"event":"query:message","data":{...}}`

## 开发

### 准备 Go 二进制

```powershell
# 在仓库根目录
go build -o vscode-extension\bin\auto-code-server.exe .\cmd\auto-code-server
```

### 安装依赖

```powershell
cd vscode-extension
npm install
cd webview-ui
npm install
```

### 构建

```powershell
cd vscode-extension
npm run compile        # 同时构建扩展与 Webview
```

### 调试

1. 用 VS Code 打开 `vscode-extension/` 目录。
2. 按 `F5` 启动扩展开发宿主（已配置 [`.vscode/launch.json`](.vscode/launch.json)，会自动运行 `npm run compile` 预构建任务）。
3. 在宿主窗口中执行命令 `Auto Code: Open Chat`。
4. 查看 `Auto Code` Output Channel 获取后端日志。

## 与原 Wails 版本的差异

- 移除"项目目录"选择按钮：改为自动读取 VS Code 工作区根目录，并在 Header 显示。
- 移除"文件资源管理器"右侧面板：直接复用 VS Code 自带的资源管理器。
- 通信层由 Wails Bindings 改为 NDJSON-RPC over stdio，便于跨进程复用 Go 核心。
