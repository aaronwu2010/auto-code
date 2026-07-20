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