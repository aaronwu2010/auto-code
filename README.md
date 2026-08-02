# auto-code

基于 Wails v2 构建的桌面应用。

## 环境要求

- Go 1.24+
- Node.js 16+
- Wails CLI v2

## 安装 Wails CLI

```bash
sudo apt install libgtk-3-dev libwebkit2gtk-4.0-dev

go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

## 开发模式

```bash
wails dev
```

## 构建生产版本

```bash
wails build
```

构建产物在 `build/bin/` 目录下。

## Ollama 配置

应用默认连接本地 Ollama 服务（`http://localhost:11434`）。如需对接 Ollama Cloud，通过环境变量配置：

### 环境变量

| 变量名 | 说明 | 默认值 |
|--------|------|--------|
| `OLLAMA_API_KEY` | Ollama Cloud API Key，设置后自动切换为 Cloud 模式 | 无（本地模式） |
| `OLLAMA_BASE_URL` | 自定义 API 地址 | 本地: `http://localhost:11434/api`，Cloud: `https://ollama.com/api` |
| `OLLAMA_MODEL` | 使用的模型名称 | `qwen3:latest` |

### 使用 Ollama Cloud

**Windows PowerShell：**

```powershell
$env:OLLAMA_API_KEY="你的API Key"
wails dev
```

**Linux / macOS：**

```bash
export OLLAMA_API_KEY="你的API Key"
wails dev
```

设置 `OLLAMA_API_KEY` 后，应用会自动切换到 Cloud 模式，使用 `https://ollama.com/api` 作为接口地址，并通过 Bearer Token 方式认证。

### 使用本地 Ollama

无需设置任何环境变量，确保本地已安装并运行 Ollama：

```bash
ollama serve
```

## 注意事项

**不要**使用 `go build` 直接编译，Wails 应用需要正确的 build tags，否则会报错：

```
Error: Wails applications will not build without the correct build tags.
```

必须通过 `wails build` 或 `wails dev` 命令构建。

## VS Code 插件

除 Wails 桌面应用外，本项目还提供一个 VS Code 插件（位于 [`vscode-extension/`](vscode-extension/)），复用同一套 Go 核心（QueryEngine、工具系统、上下文管理），通过 stdio NDJSON-RPC 与 Go 子进程通信。插件已移除文件浏览器与项目目录选择，改为自动读取 VS Code 工作区目录并在 Header 显示。

### 构建 .vsix 安装包

前置条件：Go 1.24+、Node.js 16+。

```powershell
# 1. 构建 Go 后端二进制（在仓库根目录）
go build -o vscode-extension\bin\auto-code-server.exe .\cmd\auto-code-server

# 2. 安装依赖并打包（在 vscode-extension 目录）
cd vscode-extension
npm install
cd webview-ui
npm install
cd ..
npm run vsce:package
```

构建产物：`vscode-extension\auto-code-<version>.vsix`（约 5.6 MB，内含 Go 二进制、扩展主进程、Webview 静态资源）。

### 安装 .vsix

1. 打开 VS Code → 扩展面板 → 右上角 `...` → **从 VSIX 安装...**
2. 选择 `vscode-extension\auto-code-<version>.vsix`
3. 重新加载窗口（`Ctrl+Shift+P` → `Developer: Reload Window`）

### 使用方法

1. 打开命令面板（`Ctrl+Shift+P` / macOS `Cmd+Shift+P`）。
2. 执行 `Auto Code: Open Chat` 打开对话面板。
3. 点击右上角 **⚙️ 设置**，填写 Ollama URL、API Key、模型，点击 **💾 保存配置** → **🔌 测试连接**。
4. 在输入框输入消息，`Enter` 发送，`Shift+Enter` 换行。

| 命令 | 说明 |
| --- | --- |
| `Auto Code: Open Chat` | 打开对话面板 |
| `Auto Code: Restart Server` | 重启 Go 后端进程（配置变更或异常时使用） |

### 配置项（settings.json）

| 配置键 | 说明 | 默认值 |
| --- | --- | --- |
| `auto-code.serverPath` | auto-code-server 可执行文件路径，留空使用扩展自带 | `""` |
| `auto-code.ollamaBaseUrl` | Ollama 服务地址 | `http://localhost:11434/api` |
| `auto-code.ollamaApiKey` | Ollama API Key（本地模式留空） | `""` |
| `auto-code.ollamaModel` | 默认模型名称 | `""` |

> 修改 `settings.json` 后需执行 `Auto Code: Restart Server` 生效。

更多细节见 [vscode-extension/README.md](vscode-extension/README.md)。