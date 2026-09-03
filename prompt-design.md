# PromptChecklistEngine — Agent 智能 Prompt 注入设计方案

> 日期：2026-09-03
> 目标：让本地 agent 具备和 AI 助手一样的"checklist 心智模型"——在不同场景下自动注入针对性的行为指导

---

## 1. 核心思想

### 1.1 什么是 Checklist 心智模型？

AI agent 在面对不同任务时，脑中会有一个"这类任务的完整 checklist"。比如：

- 用户说"写一个 Go 程序"→ 脑中自动浮现：go.mod + main.go + import + 构建验证
- 用户说"设计 REST API"→ 自动浮现：资源命名 + HTTP 方法 + 状态码 + 错误格式 + 版本化
- 用户说"写一个包含 SQL 查询的功能"→ 自动浮现：参数化查询 + 不要字符串拼接 + 错误处理

这些 checklist **不是硬编码规则**，而是 AI agent 对"完整交付物长什么样"的认知。通过 **动态 prompt 注入** 可以把这种认知移植到本地 agent。

### 1.2 为什么不用 Skill？

Skill 是**用户主动触发**的（"使用某某 skill"），而 checklist 是**agent 自动识别**的。两者互补：

| 机制 | 触发方式 | 用途 |
|------|---------|------|
| Skill | 用户主动调用 | 特定专业领域的深入指导 |
| **PromptChecklist** | agent 自动检测 | 通用场景的完整性保障 |

### 1.3 为什么不污染 System Prompt？

System Prompt 应该**稳定、短、通用**。Checklist 是**场景特定的**，放到 System Prompt 里会导致：
1. Token 浪费（每次都带一堆不相关的 checklist）
2. 指令冲突（debug 任务看到 build 相关的指令反而困惑）

正确做法：**IsMeta=true 的 user message** 动态注入。

---

## 2. 现有能力盘点

### 2.1 已有的 prompt 注入渠道

| 渠道 | 内容 | 位置 | 触发时机 |
|------|------|------|---------|
| **System Prompt** | 6 个固定段落 | `prompts/system.go` | 每次 CallModel |
| **DynamicPromptEngine** | 任务类型 + 语言 + Completeness Checklist | `prompts/dynamic.go` | Turn 0 |
| **Landscaper** | 项目目录结构扫描 | `queryengine.go` | Turn 0 |
| **GuardRail** | 已读文件状态 | `query.go` | 每轮 |
| **WorkingMemory** | 文件骨架 + 修改历史 | `query.go` | 每轮 |

### 2.2 DynamicPromptEngine 已实现的 checklist

| Checklist | 状态 | 触发 |
|-----------|------|------|
| 任务类型特化（6 种） | ✅ 已有 | 对应任务类型 |
| 语言特化（6 种） | ✅ 已有 | 检测到语言 |
| 语言完整性（go.mod/pom.xml） | ✅ 已有 | feature/build |
| README.md 建议 | ✅ 已有 | feature/build |
| design.md 建议 | ✅ 已有 | explain |

---

## 3. 缺失的 Checklist

### 3.1 L3 场景层（按任务场景触发）

| 场景 | 检测关键词 | Checklist 内容 |
|------|-----------|---------------|
| **REST API 设计** | api/接口/rest/endpoint/HTTP 服务 | RESTful 规范：资源命名（复数名词）、HTTP 方法语义、状态码约定、错误格式（{code, message}）、版本化（/v1/）、分页 |
| **Git Commit** | commit/提交/push/git | Conventional Commits（feat/fix/docs/refactor）、提交前检查（git diff/build/test）、不要提交二进制/密钥 |
| **CLI 工具** | 命令行/CLI/终端工具 | flag 解析（cobra/urfave）、退出码（0=成功,1=错误,2=用法）、SIGINT 信号处理、--help 格式 |
| **跨平台兼容** | 跨平台/windows+linux/macOS | 路径用 filepath.Join、换行符检测 runtime.GOOS、权限差异（chmod）、系统调用差异 |
| **Web 前端** | 前端/页面/UI/web | HTML 语义化、CSS BEM 组织、响应式（media query）、可访问性（aria label）、性能（LCP/FID） |
| **配置文件** | 配置/setting/yaml/json | schema 验证、默认值覆盖、env 变量覆盖、提供 .example 文件 |

### 3.2 L4 风险层（检测到危险模式时触发）

| 风险模式 | 检测方式 | Checklist 内容 |
|---------|---------|---------------|
| **命令注入** | prompt/代码包含 os/exec、exec.Command、shell 拼接用户输入 | 参数化 exec.Command（不要用 -c 拼字符串）、白名单验证、避免 eval |
| **SQL 注入** | prompt/代码包含 fmt.Sprintf 拼 SQL、SELECT + 字符串拼接 | 用 ? 占位符参数化查询、ORM 预编译、绝对不要拼 SQL |
| **敏感信息泄露** | prompt 包含 password/secret/key/token 硬编码 | 用 env 变量（os.Getenv）、提供 .env.example、gitignore .env、不要在日志里打密钥 |

---

## 4. 架构设计

### 4.1 四层注入架构

```
用户输入 + 已读代码 + 工具结果
        │
        ▼
┌─────────────────────────────────────┐
│           Detector Layer            │
│  ┌──────────────┐  ┌─────────────┐  │
│  │ SceneDetector│  │ RiskDetector│  │  ← 纯关键词匹配，零 LLM 开销
│  └──────┬───────┘  └──────┬──────┘  │
└─────────┼─────────────────┼─────────┘
          │                 │
          ▼                 ▼
┌─────────────────────────────────────┐
│         Checklist Layer             │
│  ┌───────────────────────────────┐  │
│  │ L1 Task Checklist   (已有)     │  │  ← 任务类型特化指令
│  │ L2 Lang Checklist   (已有)     │  │  ← 编程语言注意事项
│  │ L3 Scene Checklists (新增)     │  │  ← REST API / Git / CLI 等
│  │ L4 Risk Checklists  (新增)     │  │  ← SQL 注入 / 命令注入 等
│  └───────────────────────────────┘  │
└──────────────────┬──────────────────┘
                   │
                   ▼
┌─────────────────────────────────────┐
│         Injection Layer             │
│  合并所有 checklist → IsMeta=true   │
│  user message → 注入到 messages     │
│  （不污染 System Prompt）           │
└─────────────────────────────────────┘
```

### 4.2 数据结构

```go
// SceneType 场景类型
type SceneType string
const (
    SceneRESTAPI     SceneType = "rest_api"
    SceneGitCommit   SceneType = "git_commit"
    SceneCLI         SceneType = "cli_tool"
    SceneCrossPlatform SceneType = "cross_platform"
    SceneWebFrontend SceneType = "web_frontend"
    SceneConfigFile  SceneType = "config_file"
)

// RiskType 风险类型
type RiskType string
const (
    RiskCommandInjection RiskType = "command_injection"
    RiskSQLInjection     RiskType = "sql_injection"
    RiskSensitiveInfo    RiskType = "sensitive_info"
)
```

### 4.3 注入时机

| Layer | 注入时机 | 说明 |
|-------|---------|------|
| L1-L2 (任务+语言) | Turn 0 | 已有，Turn 0 注入 DynamicPromptEngine |
| **L3 (场景)** | **Turn 0 + Turn N（动态更新）** | Turn 0 从用户 prompt 检测；Turn N 从 Read/Grep 结果补充检测 |
| **L4 (风险)** | **每轮 CallModel 前** | 从最近的 tool results 检测风险模式，动态追加 |

**动态更新的意义**：
- Turn 0 只看到用户 prompt → 可能漏掉场景
- Turn 3 Read 了 main.go → 发现里面有 exec.Command → 触发 RiskCommandInjection
- Turn 5 Grep 搜到了 SQL 拼接 → 追加 RiskSQLInjection checklist

---

## 5. 实施计划

### 步骤 1：Detector 实现（scene_detector.go + risk_detector.go）

- SceneDetector：6 个场景，纯关键词匹配
- RiskDetector：3 个风险模式，关键词 + 简单正则
- 输入：用户 prompt + 历史 messages 内容
- 输出：[]SceneType + []RiskType

### 步骤 2：Checklist 注册表实现（checklist.go）

- 每个 checklist 是一个独立函数，返回纯字符串
- BuildAll(taskType, lang, scenes, risks) → 合并所有触发的 checklist
- 零依赖，可单独测试

### 步骤 3：集成到 query.go

- State 新增 SceneDetector + RiskDetector 字段
- Turn 0：注入 L1-L3（任务 + 语言 + 场景）
- 每轮 CallModel 前：检测最近 tool results → 注入新发现的 L4 风险

### 步骤 4：验证

- go build / go vet / go test
- 手动测试：prompt 包含 "设计 REST API" → 确认 REST API checklist 出现
- 手动测试：prompt 包含 "os/exec 执行用户输入" → 确认命令注入 checklist 出现

---

## 6. 影响范围

| 组件 | 改动 | 风险 |
|------|------|------|
| prompts/checklist.go | **新增** ~150 行 | 🟢 零风险 |
| prompts/scene_detector.go | **新增** ~120 行 | 🟢 零风险 |
| prompts/risk_detector.go | **新增** ~80 行 | 🟢 零风险 |
| engine/query/query.go | **修改** +20 行 | 🟢 极低 |
| engine/query/query.go State | **修改** +2 字段 | 🟢 极低 |

**总计：~370 行新增代码，20 行修改。**

---

## 7. 设计原则

1. **零 LLM 额外开销**：纯规则匹配，不额外调 API
2. **失败即跳过**：任何 detector/checklist 返回空字符串 = 跳过，不影响主流程
3. **不污染 System Prompt**：全部走 IsMeta user message
4. **可测试、可插拔**：每个 checklist 独立函数，可单测，可按需增删
5. **与已有机制融合**：DynamicPromptEngine 已有 L1-L2，这轮只加 L3-L4
