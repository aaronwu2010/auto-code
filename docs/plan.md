# Auto-Code 核心业务逻辑补充计划

> 对照 c-code-2.1.88-main（TypeScript）代码库，逐步在 auto-code（Go）中补齐核心业务逻辑。
> 当前 Go 端共 33 个 .go 文件 / 115KB，框架骨架已搭建，但大量模块为空壳或仅存类型定义。

---

## 阶段一：工具层实现（Tools Layer）

**目标**：补齐 34 个工具目录的实现，使 QueryEngine 的工具调用链路完整可用。

**优先级**：最高 — 工具是整个系统的基石，没有工具实现则引擎无法运转。

### 1.1 文件操作工具（4个）

| 工具 | TS 参考 | Go 目录 | 说明 |
|------|---------|---------|------|
| FileReadTool | `tools/FileReadTool/` | `tools/fileread/` | 读取文件内容，支持 offset/limit |
| FileEditTool | `tools/FileEditTool/` | `tools/fileedit/` | 精确字符串替换编辑 |
| FileWriteTool | `tools/FileWriteTool/` | `tools/filewrite/` | 写入/创建文件 |
| GlobTool | `tools/GlobTool/` | `tools/glob/` | 文件模式匹配搜索 |

**实现要点**：
- 每个工具实现 `tools.Tool` 接口的所有方法（Name, Description, InputSchema, Call, Prompt, CheckPermissions 等）
- FileEditTool 需实现 oldString/newString 精确匹配替换逻辑，含多匹配检测
- FileReadTool 需支持行号偏移和截断输出
- 对照 TS 端的 `prompt.ts` 编写 Go 端的 Prompt() 方法

### 1.2 搜索与查询工具（3个）

| 工具 | TS 参考 | Go 目录 | 说明 |
|------|---------|---------|------|
| GrepTool | `tools/GrepTool/` | `tools/grep/` | 正则内容搜索 |
| WebSearchTool | `tools/WebSearchTool/` | `tools/websearch/` | Web 搜索 |
| WebFetchTool | `tools/WebFetchTool/` | `tools/webfetch/` | URL 内容获取 |

### 1.3 执行工具（3个）

| 工具 | TS 参考 | Go 目录 | 说明 |
|------|---------|---------|------|
| BashTool | `tools/BashTool/` | `tools/bash/` | Shell 命令执行，含超时/沙箱 |
| PowerShellTool | `tools/PowerShellTool/` | `tools/powershell/` | Windows PowerShell 执行 |
| REPLTool | `tools/REPLTool/` | `tools/repl/` | 交互式 REPL 会话 |

**实现要点**：
- BashTool 需实现进程管理（启动、输出流式读取、超时终止）
- 参考 TS 端 `BashTool/` 的沙箱安全限制逻辑

### 1.4 任务与协作工具（8个）

| 工具 | TS 参考 | Go 目录 | 说明 |
|------|---------|---------|------|
| TaskCreateTool | `tools/TaskCreateTool/` | `tools/task/` | 创建子任务 |
| TaskGetTool | `tools/TaskGetTool/` | `tools/task/` | 获取任务状态 |
| TaskListTool | `tools/TaskListTool/` | `tools/task/` | 列出所有任务 |
| TaskOutputTool | `tools/TaskOutputTool/` | `tools/task/` | 获取任务输出 |
| TaskStopTool | `tools/TaskStopTool/` | `tools/task/` | 停止任务 |
| TaskUpdateTool | `tools/TaskUpdateTool/` | `tools/task/` | 更新任务 |
| TeamCreateTool | `tools/TeamCreateTool/` | `tools/team/` | 创建团队 |
| TeamDeleteTool | `tools/TeamDeleteTool/` | `tools/team/` | 删除团队 |

**实现要点**：
- Task 系列工具共享 `internal/task/` 的 TaskState
- 需实现任务生命周期管理（创建→运行→完成/失败/停止）
- Team 工具需对接团队协作 API

### 1.5 其他工具（10个）

| 工具 | TS 参考 | Go 目录 | 说明 |
|------|---------|---------|------|
| AskUserQuestionTool | `tools/AskUserQuestionTool/` | `tools/ask/` | 向用户提问 |
| BriefTool | `tools/BriefTool/` | `tools/brief/` | 简要输出模式 |
| ConfigTool | `tools/ConfigTool/` | `tools/config/` | 配置管理 |
| ScheduleCronTool | `tools/ScheduleCronTool/` | `tools/cron/` | 定时任务调度 |
| LSPTool | `tools/LSPTool/` | `tools/lsp/` | LSP 代码分析 |
| NotebookEditTool | `tools/NotebookEditTool/` | `tools/notebook/` | Notebook 编辑 |
| SendMessageTool | `tools/SendMessageTool/` | `tools/message/` | 跨任务消息传递 |
| SkillTool | `tools/SkillTool/` | `tools/skill/` | 技能调用 |
| SleepTool | `tools/SleepTool/` | `tools/sleep/` | 延时等待 |
| TodoWriteTool | `tools/TodoWriteTool/` | `tools/todo/` | 待办事项管理 |

### 1.6 MCP 相关工具（3个）

| 工具 | TS 参考 | Go 目录 | 说明 |
|------|---------|---------|------|
| MCPTool | `tools/MCPTool/` | `tools/mcp/` | MCP 服务器工具调用 |
| McpAuthTool | `tools/McpAuthTool/` | `tools/mcpauth/` | MCP 认证 |
| ReadMcpResourceTool | `tools/ReadMcpResourceTool/` + `tools/ListMcpResourcesTool/` | `tools/mcpresource/` | MCP 资源读取 |

### 1.7 模式切换工具（4个）

| 工具 | TS 参考 | Go 目录 | 说明 |
|------|---------|---------|------|
| EnterPlanModeTool | `tools/EnterPlanModeTool/` | `tools/planmode/` | 进入计划模式 |
| ExitPlanModeTool | `tools/ExitPlanModeTool/` | `tools/planmode/` | 退出计划模式 |
| EnterWorktreeTool | `tools/EnterWorktreeTool/` | `tools/worktree/` | 进入 Worktree |
| ExitWorktreeTool | `tools/ExitWorktreeTool/` | `tools/worktree/` | 退出 Worktree |

### 1.8 注册集成

- 在 `tools/registry/registry.go` 的初始化流程中注册所有工具
- 参考 TS 端 `tools.ts` 的注册逻辑

---

## 阶段二：命令层实现（Commands Layer）

**目标**：补齐 37 个命令目录的实现，使用户可通过 CLI 触发各项功能。

**优先级**：高 — 命令是用户交互的入口。

### 2.1 核心会话命令（6个）

| 命令 | TS 参考 | 说明 |
|------|---------|------|
| init | `commands/init.tsx` | 项目初始化 |
| resume | `commands/resume/` | 恢复会话 |
| session | `commands/session/` | 会话管理 |
| clear | `commands/clear/` | 清除上下文 |
| exit | `commands/exit/` | 退出 |
| help | `commands/help/` | 帮助信息 |

### 2.2 配置与权限命令（5个）

| 命令 | TS 参考 | 说明 |
|------|---------|------|
| config | `commands/config/` | 配置管理 |
| permissions | `commands/permissions/` | 权限管理 |
| model | `commands/model/` | 模型选择 |
| login | `commands/login/` | 登录 |
| logout | `commands/logout/` | 登出 |

### 2.3 工作流命令（6个）

| 命令 | TS 参考 | 说明 |
|------|---------|------|
| plan | `commands/plan/` | 计划模式 |
| compact | `commands/compact/` | 上下文压缩 |
| commit | `commands/commit.ts` | Git 提交 |
| diff | `commands/diff/` | 差异查看 |
| review | `commands/review.ts` | 代码审查 |
| tasks | `commands/tasks/` | 任务管理 |

### 2.4 集成命令（8个）

| 命令 | TS 参考 | 说明 |
|------|---------|------|
| mcp | `commands/mcp/` | MCP 服务器管理 |
| skills | `commands/skills/` | 技能管理 |
| plugin | `commands/plugin/` | 插件管理 |
| hooks | `commands/hooks/` | 钩子管理 |
| memory | `commands/memory/` | 记忆管理 |
| vim | `commands/vim/` | Vim 模式 |
| agents | `commands/agents/` | Agent 管理 |
| add-dir | `commands/add-dir/` | 添加工作目录 |

### 2.5 诊断命令（6个）

| 命令 | TS 参考 | 说明 |
|------|---------|------|
| doctor | `commands/doctor/` | 环境诊断 |
| status | `commands/status/` | 状态查看 |
| cost | `commands/cost/` | 费用统计 |
| usage | `commands/usage/` | 用量统计 |
| feedback | `commands/feedback/` | 反馈 |
| upgrade | `commands/upgrade/` | 升级检查 |

### 2.6 其他命令（6个）

| 命令 | TS 参考 | 说明 |
|------|---------|------|
| context | `commands/context/` | 上下文管理 |
| effort | `commands/effort/` | 推理力度 |
| fast | `commands/fast/` | 快速模式 |
| files | `commands/files/` | 文件管理 |
| share | `commands/share/` | 会话分享 |
| theme | `commands/theme/` | 主题切换 |

### 2.7 命令注册框架

- 在 `internal/cli/` 实现命令注册与分发框架
- 参考 TS 端 `commands.ts` 的命令注册模式
- 每个命令实现统一的 `Command` 接口

---

## 阶段三：记忆系统（Memory System）

**目标**：实现完整的记忆提取、整合与同步体系。

**优先级**：高 — 记忆系统是智能体持续学习的关键能力。

### 3.1 记忆提取（ExtractMemories）

- 参考 TS: `services/extractMemories/extractMemories.ts`（615行）
- 在 `internal/memdir/` 中实现
- 功能：每次查询循环结束时，使用 forked subagent 从会话中提取持久化记忆
- 写入 `~/.claude/projects/<path>/memory/` 目录
- 需实现 `createAutoMemCanUseTool()` 工具白名单过滤

### 3.2 会话记忆（SessionMemory）

- 参考 TS: `services/SessionMemory/sessionMemory.ts`（495行）
- 新建 `internal/services/sessionmemory/`
- 功能：自动维护当前会话的 markdown 笔记
- 后台周期性运行 forked subagent 提取关键信息
- 不中断主对话流程

### 3.3 自动整合（AutoDream）

- 参考 TS: `services/autoDream/autoDream.ts`（324行）
- 新建 `internal/services/autodream/`
- 功能：后台记忆整合，时间门控 + 会话数量门控 + 分布式锁
- 当积累足够会话后自动触发 forked subagent 进行记忆合并
- 需实现 `consolidationPrompt` 和 `consolidationLock`

### 3.4 团队记忆同步（TeamMemorySync）

- 参考 TS: `services/teamMemorySync/index.ts`（1256行）
- 新建 `internal/services/teammemorysync/`
- 功能：基于 git remote hash 按仓库同步团队记忆
- 支持 pull（服务端覆盖本地）和 push（delta 上传）
- 需实现文件监视器（watcher）和密钥扫描（secretScanner）

### 3.5 记忆路径与扫描增强

- 扩展 `internal/memdir/memdir.go`
- 参考 TS: `memdir/paths.ts`, `memdir/memoryScan.ts`, `memdir/findRelevantMemories.ts`
- 实现 `getAutoMemPath()`, `isAutoMemoryEnabled()`, `isAutoMemPath()`
- 实现 `scanMemoryFiles()`, `formatMemoryManifest()`
- 实现 `findRelevantMemories()` 基于语义相关性查找记忆

---

## 阶段四：MCP 服务完整实现

**目标**：补齐 MCP 协议栈的连接管理、认证、传输层等核心实现。

**优先级**：高 — MCP 是工具扩展的核心机制。

### 4.1 MCP 连接管理器

- 参考 TS: `services/mcp/MCPConnectionManager.tsx`
- 扩展 `internal/mcp/server.go`
- 功能：MCP 服务器连接生命周期管理（发现、连接、断开、重连）
- 支持多 MCP 服务器并行连接
- 连接状态追踪与事件通知

### 4.2 MCP 认证

- 参考 TS: `services/mcp/auth.ts`, `services/mcp/oauthPort.ts`, `services/mcp/xaa.ts`, `services/mcp/xaaIdpLogin.ts`
- 新建 `internal/mcp/auth.go`
- 功能：OAuth 授权码流程、XAA IDP 登录
- 认证端口监听与回调

### 4.3 MCP 传输层

- 参考 TS: `services/mcp/InProcessTransport.ts`, `services/mcp/SdkControlTransport.ts`
- 新建 `internal/mcp/transport.go`
- 功能：进程内传输（stdio）、SDK 控制传输
- 消息序列化/反序列化

### 4.4 MCP Elicitation 处理

- 参考 TS: `services/mcp/elicitationHandler.ts`
- 新建 `internal/mcp/elicitation.go`
- 功能：MCP elicitation 交互处理（向用户请求额外信息）

### 4.5 MCP 频道权限与通知

- 参考 TS: `services/mcp/channelAllowlist.ts`, `services/mcp/channelPermissions.ts`, `services/mcp/channelNotification.ts`
- 新建 `internal/mcp/channel.go`
- 功能：频道白名单、权限控制、通知推送

### 4.6 MCP 官方注册表

- 参考 TS: `services/mcp/officialRegistry.ts`
- 新建 `internal/mcp/registry.go`
- 功能：官方 MCP 服务器注册表查询

### 4.7 MCP 工具集成

- 扩展 `internal/mcp/server.go`
- 实现 MCP 工具发现与动态注册
- 将 MCP 工具注入 ToolRegistry

---

## 阶段五：OAuth 认证与设置同步

**目标**：实现完整的 OAuth 认证流程和跨设备设置同步。

**优先级**：中 — 认证是远程服务接入的前提。

### 5.1 OAuth 客户端

- 参考 TS: `services/oauth/`（5个文件）
- 实现 `internal/auth/` 模块
- 功能：
  - OAuth 授权码流程（`client.ts`）
  - 本地授权码监听服务器（`auth-code-listener.ts`）
  - Token 加密存储（`crypto.ts`）
  - 用户 Profile 获取（`getOauthProfile.ts`）
  - Token 自动刷新

### 5.2 设置同步

- 参考 TS: `services/settingsSync/`（2个文件）
- 新建 `internal/services/settingssync/`
- 功能：跨设备设置同步（pull/push delta）
- ETag 追踪与冲突解决

### 5.3 远程托管设置

- 参考 TS: `services/remoteManagedSettings/`
- 新建 `internal/services/remotemanagedsettings/`
- 功能：服务端下发的托管设置策略
- 优先级高于本地设置

### 5.4 策略限制

- 参考 TS: `services/policyLimits/`
- 新建 `internal/services/policylimits/`
- 功能：组织级策略限制（token 预算、工具白名单等）

---

## 阶段六：Hooks 系统

**目标**：实现完整的事件钩子系统，支撑工具权限、IDE集成、语音等交互场景。

**优先级**：中 — Hooks 是解耦各子系统交互的关键机制。

### 6.1 Hooks 框架

- 在 `internal/hooks/` 实现核心框架
- 定义 Hook 接口：`PreToolUse`, `PostToolUse`, `PostSampling`, `OnStop` 等
- 实现 Hook 注册与生命周期管理
- 参考 TS 端 `utils/hooks/postSamplingHooks.ts` 的设计

### 6.2 工具权限 Hook

- 参考 TS: `hooks/useCanUseTool.tsx`, `hooks/toolPermission/`
- 实现 `internal/hooks/toolpermission/`
- 功能：工具调用前的权限检查拦截

### 6.3 后台任务 Hook

- 参考 TS: `hooks/useBackgroundTaskNavigation.ts`, `hooks/useScheduledTasks.ts`
- 实现 `internal/hooks/background/`
- 功能：后台任务调度与导航

### 6.4 远程会话 Hook

- 参考 TS: `hooks/useRemoteSession.ts`, `hooks/useSSHSession.ts`
- 实现 `internal/hooks/remote/`
- 功能：远程会话生命周期管理

---

## 阶段七：语音模式（Voice Mode）

**目标**：实现语音输入功能，包括音频采集和 STT 转写。

**优先级**：低 — 语音是增强体验但非核心路径。

### 7.1 音频采集

- 参考 TS: `services/voice.ts`（525行）
- 实现 `internal/voice/capture.go`
- 功能：跨平台麦克风音频采集（macOS/Linux/Windows）
- 使用 CGo 或纯 Go 音频库（如 malgo）

### 7.2 STT 流式转写

- 参考 TS: `services/voiceStreamSTT.ts`（544行）
- 实现 `internal/voice/stt.go`
- 功能：WebSocket 连接 Anthropic voice_stream 端点
- JSON 控制消息 + 二进制音频帧协议
- 实时返回 TranscriptText / TranscriptEndpoint

### 7.3 语音关键词优化

- 参考 TS: `services/voiceKeyterms.ts`（106行）
- 实现 `internal/voice/keyterms.go`
- 功能：编码领域词汇提示，提升 STT 准确率
- 动态提取项目分支名、文件名等作为关键词

---

## 阶段八：辅助服务补齐

**目标**：补齐分析、压缩、迁移等辅助服务。

**优先级**：低 — 这些服务增强体验但非核心路径。

### 8.1 分析服务（Analytics）

- 参考 TS: `services/analytics/`（9个文件）
- 实现 `internal/analytics/`
- 功能：事件日志、DataDog 上报、GrowthBook 特性开关

### 8.2 上下文压缩（Compact）

- 参考 TS: `services/compact/`
- 实现 `internal/compact/`
- 功能：对话历史自动压缩，当前 `queryengine.go:382` 返回 "not implemented"
- 需实现 microcompact 和 autoCompact 两种策略

### 8.3 数据迁移（Migrations）

- 参考 TS: `migrations/`（11个文件）
- 实现 `internal/migrations/`
- 功能：版本升级时的配置/数据迁移

### 8.4 远程会话管理增强

- 扩展 `internal/remote/manager.go`
- 参考 TS: `remote/RemoteSessionManager.ts`, `remote/SessionsWebSocket.ts`, `remote/remotePermissionBridge.ts`
- 实现 WebSocket 会话管理、权限桥接

### 8.5 插件系统增强

- 扩展 `internal/plugins/loader.go`
- 参考 TS: `plugins/builtinPlugins.ts`, `plugins/bundled/`
- 实现内置插件注册和第三方插件加载

### 8.6 技能系统增强

- 扩展 `internal/skills/loader.go`
- 参考 TS: `skills/bundledSkills.ts`, `skills/bundled/`, `skills/mcpSkillBuilders.ts`
- 实现内置技能注册和 MCP 技能构建器

---

## 实施原则

1. **自底向上**：先实现工具层（阶段一），再实现命令层（阶段二），最后实现服务层
2. **对照实现**：每个模块严格对照 TS 端对应文件的功能和逻辑，确保行为一致
3. **接口先行**：先定义 Go 接口，再实现具体逻辑，保持可测试性
4. **增量验证**：每完成一个工具/命令，立即编写单元测试并集成到 registry
5. **优先核心路径**：文件操作工具 > 搜索工具 > 执行工具 > 任务工具 > 其他

---

## 当前状态汇总

| 模块 | TS 文件数 | Go .go 文件数 | 完成度 |
|------|-----------|---------------|--------|
| tools/ | 43 目录 | 1 (registry) | ~2% |
| commands/ | 101 目录 | 0 | 0% |
| services/ | 36 子目录 | 0 (无services目录) | 0% |
| hooks/ | 85 文件 | 0 | 0% |
| memdir/ | 8 文件 | 1 | ~15% |
| mcp/ | 23 文件 | 1 | ~5% |
| voice/ | 4 文件 | 0 | 0% |
| engine/ | - | 3 | ~40% |
| api/ | - | 3 | ~50% |
| state/ | - | 2 | ~60% |
| types/ | - | 5 | ~70% |
| bridge/ | - | 1 | ~30% |
| coordinator/ | - | 1 | ~40% |

---

## 未实现模块审计报告（2026-07-21）

### 高优先级（影响核心功能可用性）

| 模块 | 文件 | 问题 | 影响 |
|------|------|------|------|
| auth/types.go | `internal/auth/types.go` | `currentTimestampMs()` 返回 0 | token 永远被认为已过期，OAuth 认证失效 |
| AskTool | `internal/tools/ask/ask.go` | 无处理器，返回占位字符串 | AI 无法向用户提问交互 |
| SkillTool | `internal/tools/skill/skill.go` | skillHandlers 为空，永远 "not found" | AI 无法调用任何技能 |
| CronTool | `internal/tools/cron/cron.go` | 仅存 sync.Map，无调度器 goroutine | 定时任务完全不工作 |
| REPL 工具 | `internal/tools/repl/repl.go` | Python/Node 执行均返回 "not yet implemented" | AI 无法执行代码片段验证 |
| compact 压缩 | `internal/compact/` | 无 LLM 摘要，仅过滤空消息/截断 | 长对话无法智能压缩，上下文窗口浪费 |

### 中优先级（影响扩展功能）

| 模块 | 文件 | 问题 | 影响 |
|------|------|------|------|
| LSPTool | `internal/tools/lsp/lsp.go` | 不连接 LSP 服务器 | AI 无法做定义跳转/引用查找 |
| McpAuthTool | `internal/tools/mcpauth/mcpauth.go` | 假 OAuth 流程 | MCP 服务器认证不可用 |
| McpResourceTool | `internal/tools/mcpresource/mcpresource.go` | 不读取 MCP 资源 | MCP 资源发现不可用 |
| MCP InProcessTransport | `internal/mcp/transport.go` | 回环 mock，不与真实服务器通信 | MCP 协议端到端不可用 |
| AgentTool | `internal/tools/agent/agent.go` | 不启动子代理 | 多代理协作不可用 |
| MonitorTool | `internal/tools/monitor/monitor.go` | 不监控进程 | 无法追踪长时间运行任务 |
| WebBrowserTool | `internal/tools/webbrowser/webbrowser.go` | 不打开浏览器 | AI 无法访问网页 |
| WorktreeTool | `internal/tools/worktree/worktree.go` | 不操作 git worktree | 并行开发工作流不可用 |
| NotebookEditTool | `internal/tools/notebook/notebook.go` | 不修改 .ipynb | Jupyter 工作流不可用 |
| BriefTool | `internal/tools/brief/brief.go` | SetAppState 返回未修改的 prev | brief 模式切换无效 |
| commands/ 28个 | `internal/commands/` | 大部分为 stub | CLI 命令几乎不可用 |
| hooks/remote/ | `internal/hooks/remote/` | SSH/Remote 连接为 mock | 远程开发不可用 |

### 低优先级（辅助功能/体验优化）

| 模块 | 文件 | 问题 | 影响 |
|------|------|------|------|
| SnipTool | `internal/tools/snip/snip.go` | 不持久化代码片段 | 代码片段管理不可用 |
| Voice/STT | `internal/voice/stt.go` | 全部 mock，不发送音频 | 语音模式不可用 |
| Bridge.GetWork() | `internal/bridge/bridge.go` | 始终返回错误 | 远程任务分配不可用 |
| GetTotalCost() | `internal/engine/queryengine.go` | 始终返回 0 | 无法追踪 API 费用 |
| fmtRemoteID() | `internal/remote/websocket.go` | 返回空字符串 | 远程会话 ID 显示为空 |
| analytics/ | `internal/analytics/` | 无实际上报 | 无使用统计 |
| 前端 16 个空目录 | `frontend/src/components/` 等 | 无文件 | UI 不可扩展 |
| plugins/loader | `internal/plugins/loader.go` | LoadAllPluginsCacheOnly 返回 nil | 插件加载不可用 |
| skills/loader | `internal/skills/loader.go` | LoadSkillDirSkills 返回空切片 | 自定义技能目录加载不可用 |
| migrations 2/6 | `internal/migrations/migrations.go` | 空实现 | 旧配置迁移可能缺失 |

### 实施顺序

1. `auth/types.go 时间戳 bug` → 2. `AskTool 处理器` → 3. `SkillTool 注册` → 4. `compact LLM 摘要` → 5. `REPL 执行` → 6. `CronTool 调度器` → 7-16. 中优先级 → 17-26. 低优先级