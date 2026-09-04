# 完美交付能力设计文档

## 1. 问题诊断

### 1.1 现象
某些 AI agent（如 Cursor、Trae 等）在收到"参考 X 项目，在 Y 项目实现 Z 功能"的指令时，能一次性交付完整可用的项目：
- 服务端代码 + 客户端代码 + 配置文件 + 编译脚本 + 使用说明
- 开箱即编译、开箱即运行

而 auto-code 当前做不到这点——它能写出代码骨架，但经常遗漏关键交付物（如缺少构建脚本、配置模板、README、启动脚本等），或者只写了"主要代码"而忽略了配套设施。

### 1.2 根因分析

这种"完美交付"能力**不是大模型的固有能力**，而是 **Prompt 引导 + 代码流程约束** 共同作用的结果。

| 维度 | 大模型能力 | Prompt 提示词 | 代码流程 |
|------|-----------|--------------|---------|
| 理解"完整项目"的组成 | ✅ 模型知道前后端分离、配置分层 | ❌ auto-code 没有告诉模型"完整项目应该包含什么" | — |
| 参考项目分析 | ✅ 模型能读目录结构和代码 | ❌ auto-code 没有引导模型系统性分析参考项目 | — |
| 补齐遗漏交付物 | — | ❌ auto-code 只有"单语言编译检查"，没有"全栈交付清单" | ✅ 可通过 checklist 注入 + 强制执行 |
| 验证闭环（build/test） | — | ❌ auto-code 的 Doing tasks 里没有强约束"写完必须验证" | ✅ 可通过代码流程在任务结束前强制跑 build |
| 质量自检（read-back） | — | ❌ 没有让模型回读自己写的代码检查一致性 | ✅ 可通过 prompt 引导 |

**核心结论：auto-code 的缺失 90% 是 Prompt 设计问题，10% 是代码流程约束不足。**

### 1.3 auto-code 当前 prompt 的局限性

auto-code 现有的 `DynamicPromptEngine` + `ChecklistEngine` 架构是好的基础，但 `buildCompletenessChecklist` 只覆盖了：
- Go: go.mod + main.go + 正确 package
- Python: requirements.txt + main.py + __init__.py
- ...

**缺失的关键能力：**
1. **全栈交付清单**：不知道"一个完整的 Web 应用"还需要 config/、Dockerfile、docker-compose、Makefile、.env.example、scripts/
2. **参考项目分析引导**：当用户说"参考 X"时，没有引导模型系统性分析 X 的技术栈、目录结构、构建方式
3. **跨项目迁移 checklist**：从参考项目到新项目，需要适配哪些差异（目录结构、技术栈版本、API 版本）
4. **运行时验证闭环**：没有代码流程在生成完成后强制跑 build/test

---

## 2. 设计方案

### 2.1 整体架构

在现有 `DynamicPromptEngine` + `ChecklistEngine` 基础上，新增三层：

```
L1 任务类型层 (已有): debug / feature / refactor / ...
L2 语言特化层 (已有): Go / Python / JS / ...
L3 场景/风险层 (已有): REST API / command injection / ...
L4 全栈交付层 (新增): StackDeliveryChecklist — 识别全栈项目，补齐所有交付物
L5 参考迁移层 (新增): ReferenceMigrationGuide — 引导系统性分析参考项目 + 适配迁移
```

### 2.2 L4: StackDeliveryChecklist — 全栈交付清单

#### 2.2.1 触发条件
- 任务类型为 `feature` 或 `build`
- 项目目录下检测到**多技术栈共存**（如同时有 `package.json` 和 `go.mod`，或有 `frontend/` 和 `backend/` 子目录）

#### 2.2.2 全栈项目类型识别
| 项目类型 | 识别特征 | 必须包含 |
|---------|---------|---------|
| **Web 全栈** | 有 `frontend/` + `backend/` 或同时有 `package.json` + 后端构建文件 | 前端代码 + 后端代码 + API 契约 + 配置 + Dockerfile + README + 启动脚本 |
| **CLI 工具** | 有 `main.go` / `main.py` / `bin/` | 入口 + 子命令框架 + 配置解析 + README + 构建脚本 |
| **库/SDK** | 有 `lib/` / `pkg/` 或包声明 | 核心 API + 类型定义 + 示例 + 测试 + README |
| **服务（微服务）** | 有 `server/` / `cmd/` | 入口 + 路由/handler + 数据层 + 配置 + Dockerfile + health endpoint + README |
| **脚手架/模板** | 有 `template/` / `scaffold/` | 模板文件 + 生成器 + README + 使用示例 |

#### 2.2.3 通用交付物清单（按类型裁剪）

```
☐ 源代码（main + 核心模块）
☐ 构建配置（go.mod / package.json / pyproject.toml / Cargo.toml）
☐ 依赖声明 + 锁定文件（go.sum / package-lock.json / requirements.txt）
☐ 配置模板（config.example.yaml / .env.example / config.json.example）
☐ 启动/构建脚本（Makefile / scripts/*.sh / docker-compose.yml）
☐ Dockerfile（如涉及服务部署）
☐ README.md（项目说明 + 快速开始 + 目录结构 + 使用示例）
☐ API 文档（如涉及 HTTP 接口：OpenAPI / Swagger）
☐ .gitignore
☐ 测试文件（核心模块的单测）
```

#### 2.2.4 注入位置
通过现有的 `ChecklistEngine.BuildAll()` 注入，作为 `IsMeta` message，不污染 system prompt。

### 2.3 L5: ReferenceMigrationGuide — 参考项目迁移指南

#### 2.3.1 触发条件
- 用户消息中包含"参考"、"参照"、"基于"、"仿照" + 一个项目路径或 URL
- 或项目内存（memory）中存在参考项目信息

#### 2.3.2 分析流程 Prompt

当检测到"参考 X 项目"时，注入以下引导：

```
[Reference Migration] 检测到你需要参考另一个项目来实现功能。
请严格遵循以下 5 步分析流程：

Step 1 — 目录结构扫描
  → 用 Glob 列出参考项目的完整目录树
  → 识别：前端/后端/配置/测试/脚本 各层的位置
  → 特别注意：是否有 monorepo 结构（workspace / lerna / go.work）

Step 2 — 技术栈识别
  → 读取 package.json / go.mod / pyproject.toml / Cargo.toml
  → 确认：语言版本、核心框架、构建工具、测试框架、API 风格
  → 记录关键版本号（如 Node 18, Go 1.22, FastAPI 0.110）

Step 3 — 核心代码阅读
  → 找到入口文件（main.py / main.go / index.ts）
  → 顺着 import 链读核心模块，理解：
    - 路由/控制器 → 业务层 → 数据层 的分层
    - 依赖注入方式
    - 配置加载方式
    - 错误处理模式

Step 4 — 适配差异分析
  → 新项目与参考项目的差异：
    - 技术栈版本不同？（Node 16 vs 20, Python 3.10 vs 3.12）
    - 框架不同？（Express vs FastAPI, Gin vs Echo）
    - 目录结构不同？
    - API 接口需要适配？
  → 列出需要修改/适配的点

Step 5 — 生成交付物
  → 按 L4 全栈交付清单检查新项目
  → 用新项目的技术栈重新实现（不要复制参考项目的代码）
  → 适配参考项目的设计模式到新项目技术栈

重要：
- 理解模式 ≠ 复制代码。用新项目的技术栈和风格重写。
- 不要遗漏 README、配置模板、Dockerfile 等配套文件。
```

### 2.4 运行时验证闭环（代码流程）

仅靠 prompt 引导不够，需要代码层面的约束：

#### 2.4.1 任务结束前强制验证
在 `QueryEngine` 的 `SubmitMessage` 返回前，如果任务类型是 `feature` 或 `build`：
1. 自动检测项目语言
2. 自动运行对应的 build 命令（go build / npm run build / pip install + python -c "import xxx"）
3. 如果 build 失败，自动把编译错误追加到下一轮 user message，要求 agent 修复
4. 最多自动修复 2 次，超过则提示用户

#### 2.4.2 生成后自检 Prompt
在 agent 完成代码生成后，自动注入：
```
[Self-Check] 你刚写完代码。请执行以下自检：

1. 回读你创建的每个文件，检查：
   - import 是否正确
   - 函数签名是否与调用方匹配
   - 配置文件中的路径是否一致
2. 检查是否遗漏了必要的文件（参考 L4 交付清单）
3. 运行 build/test 验证
```

### 2.5 实施优先级

| 优先级 | 模块 | 原因 |
|-------|------|------|
| P0 | L4 StackDeliveryChecklist | 直接解决"缺文件"问题，覆盖 80% 场景 |
| P1 | L5 ReferenceMigrationGuide | 解决"参考项目"场景 |
| P2 | 运行时验证闭环 | 保障质量，但实现复杂度高 |
| P3 | 生成后自检 Prompt | Prompt 即可实现，成本最低 |

---

## 3. 实现计划

### 3.1 新增文件

| 文件 | 说明 |
|------|------|
| `internal/prompts/stack_delivery.go` | L4 全栈交付清单，定义项目类型识别 + 交付物 checklist |
| `internal/prompts/reference_migration.go` | L5 参考项目迁移指南 |
| `internal/prompts/self_check.go` | 生成后自检 prompt |

### 3.2 修改文件

| 文件 | 修改内容 |
|------|---------|
| `internal/prompts/checklist.go` | `BuildAll()` 方法追加 L4 + L5 层 |
| `internal/prompts/scene_detector.go` | 新增全栈项目类型检测逻辑 |
| `internal/prompts/builder.go` | 新增 L4/L5 构建入口 |
| `internal/engine/query/query.go` | 任务结束前注入自检 prompt |

### 3.3 代码流程修改

在 `query.go` 的消息处理循环中，当检测到任务完成信号（agent 不再调用 tool）且任务类型为 feature/build 时：
1. 检测项目目录下的构建文件（go.mod / package.json / pyproject.toml / Cargo.toml）
2. 运行对应 build 命令
3. 如果失败，将错误作为新的 user message 注入，要求 agent 修复
4. 最多 2 次自动修复循环

---

## 4. 风险与边界

### 4.1 过度生成
**风险**：agent 可能生成不需要的文件（如给纯 CLI 项目加 Dockerfile）。
**应对**：交付清单按项目类型裁剪，且只是"建议"而非"强制"，agent 可根据上下文判断是否需要。

### 4.2 参考项目代码泄露
**风险**：agent 可能复制参考项目的代码而非"理解模式后重写"。
**应对**：ReferenceMigrationGuide 明确要求"不要复制代码，理解模式后用新技术栈重写"。

### 4.3 自动验证循环死循环
**风险**：build 反复失败，agent 反复修复，陷入死循环。
**应对**：设置最多 2 次自动修复，超过则提示用户手动介入。

### 4.4 上下文窗口膨胀
**风险**：多层 checklist + 参考项目分析可能消耗大量 token。
**应对**：L4/L5 层只在触发条件满足时注入（feature/build + 全栈项目 + 参考关键字），不污染常规任务。

---

## 5. 可观测性

在 `Debug` 模式下，agent 输出的 IsMeta message 会显示完整的 checklist 内容，用户可以看到：
- 系统检测到了什么任务类型
- 触发了哪些 checklist
- 注入了哪些引导

这有助于用户理解 agent 的行为，也有助于排查问题。
