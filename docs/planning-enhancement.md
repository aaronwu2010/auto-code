# 规划能力增强设计方案

## 1. 问题

Trae 每次处理编程任务时，会**先制定一个 Todo 计划**，然后按计划执行、追踪进度。auto-code 虽然有 `internal/planning/` 包，但规划能力**形同虚设**：

| 对比维度 | Trae 的规划 | auto-code 的规划 |
|---------|------------|-----------------|
| 谁做规划？ | **LLM 自己思考、自己拆解** | 启发式规则（关键词+句子切分） |
| 触发条件 | 用户要做多步骤任务 → 自动创建 Todo | `isSimpleTask` 判定 → 90% 任务被跳过 |
| 计划粒度 | 细粒度（每个 Edit/RunCommand 一个 step） | 粗粒度（一句话切一步） |
| 进度追踪 | ✅ TodoWrite 实时更新完成状态 | ❌ 注入 meta 消息后不追踪 |
| 执行流 | 多轮 ReAct + 随时调整 | 单轮执行流（plan 只注入一次） |
| 验证回路 | 每个 step 后验证（build/vet/test） | 只有最终的 VerificationGate |

## 2. auto-code 现有规划系统的问题

### 2.1 `isSimpleTask` 判定太松

```go
func (d *BaseTaskDecomposer) isSimpleTask(task *Task) bool {
    if len(task.Action) < 20 {
        return true  // 短任务 = 简单 → 跳过规划
    }
    keywords := d.identifyKeywords(task.Action)
    if len(keywords) == 0 {
        return true  // 没有中文连接词 = 简单 → 跳过规划
    }
    return false
}
```

**问题**：
- "用 Go 实现一个 WebSocket 聊天服务器" → 长度 22，无中文关键词 → **判定为简单任务，跳过规划**
- "修复 bug 并添加单元测试" → 长度 11，无中文关键词 → **判定为简单任务**
- 只有"我要创建一个网站，并且要实现登录功能，还要加上数据持久化，最后部署到服务器"这种超长中文才会触发规划

### 2.2 启发式拆解不调用 LLM

```go
func (d *BaseTaskDecomposer) heuristicDecompose(task *Task, ctx *PlanContext) (*TaskDecomposition, error) {
    // 只有两种策略：
    // 1. tryTemplateMatch — 预定义的模板匹配
    // 2. splitIntoSentences — 按句子切分
    // 3. decomposeByKeywords — 按中文连接词切分
}
```

**问题**：没有让 LLM 真正思考。拆解的步骤是机械的，可能错误拆解、遗漏关键依赖、顺序不对。

### 2.3 计划执行流不完整

```
auto-code 当前:
  SubmitMessage(prompt)
    → injectDecomposedPlan()  // 一次性注入 <system-reminder> plan
    → queryLoop()              // 单轮 ReAct
    → 模型自己决定按不按 plan 做 // plan 只是"提示"，不是"强制"
    → 完成

Trae 的做法:
  TodoWrite([todo list])      // LLM 自己创建
    → step 1: 做 A → 验证 → 标记 complete
    → step 2: 做 B → 验证 → 标记 complete
    → step 3: 做 C → 验证 → 遇到问题 → 调整 plan
    → 全部 complete
```

## 3. 设计方案

### 核心思路

**不让启发式算法替 LLM 做规划——让 LLM 自己做规划**。auto-code 的 planning 包保留，作为"计划追踪层"；而"思考层"交给 LLM。

### 3.1 Step 1: 新增 PlanTool（让 LLM 自己管理计划）

新增一个 **`Plan` 工具**，让 LLM 在对话中通过工具调用来创建/更新计划：

```go
// internal/tools/plan/plan.go

type PlanToolInput struct {
    Action      string `json:"action"`      // "create" | "update" | "complete" | "get"
    Description string `json:"description"` // 计划描述（create 时）
    Tasks       []Task `json:"tasks"`       // 任务列表（create/update 时）
    TaskID      string `json:"task_id"`     // 操作的任务 ID
    Status      string `json:"status"`      // "pending" | "in_progress" | "completed"
}

type Task struct {
    ID          string   `json:"id"`
    Title       string   `json:"title"`
    Description string   `json:"description"`
    DependsOn   []string `json:"depends_on,omitempty"`
    Status      string   `json:"status"`
    Priority    string   `json:"priority"` // "high" | "medium" | "low"
}
```

**PlanTool 的作用**：
- 让 LLM 自己在对话开始时调用 `Plan action="create"` 创建计划
- 每完成一步调用 `Plan action="complete" task_id="step-2"`
- 进度状态会注入到后续的 system prompt 里（"你当前正在执行 step-3/5"）
- 如果 LLM 没有调用 PlanTool，不强制（避免干扰简单任务）

### 3.2 Step 2: 强制复杂任务先规划

在 SubmitMessage 里加一个 **pre-plan 判断**，但这次**不是启发式**，而是让 LLM 做判断：

```
SubmitMessage(prompt):
  1. 轻量启发式判断（保守）:
     - prompt 长度 > 150 字符 或 包含 "实现"/"创建"/"修复"/"添加"/"重写" 关键词
     → 是 → "这是一个复杂任务"
  2. 如果是复杂任务 → 注入规划提示:
     "<system-reminder>这是一个复杂的编程任务。请先用 Plan 工具创建一个执行计划，
      列出所有步骤、依赖关系和验证方法，然后按计划逐步执行。</system-reminder>"
  3. 如果是简单任务 → 正常流程，不强制规划
```

### 3.3 Step 3: 计划进度注入 system prompt

每轮对话前，把当前计划的进度状态注入 system prompt：

```
// System prompt 追加:
当前执行计划进度:
  [✅] 1. 分析项目结构和现有代码
  [✅] 2. 实现核心功能模块
  [🔄] 3. 添加单元测试 ← 当前步骤
  [⬜] 4. 更新文档
  [⬜] 5. 验证和构建
```

这样 LLM 每一轮都知道自己做到哪了、还有什么没做。

### 3.4 Step 4: 替换启发式 Decompose

保留 `injectDecomposedPlan` 作为**保底方案**，但主要依赖 LLM 自己的 PlanTool。当 LLM 创建了自己的计划时，跳过启发式拆解。

### 3.5 PlanTool vs 现有 planning 包的关系

| 组件 | 职责 |
|------|------|
| `PlanTool`（新增） | 给 LLM 的工具接口，让 LLM 创建/更新计划 |
| `planning.Plan` / `planning.Task`（现有） | 数据模型 |
| `planning.BaseTaskDecomposer`（现有） | 保底方案——LLM 不规划时用启发式 |
| `injectDecomposedPlan`（现有） | 把计划注入 messages |

## 4. 实施计划

| Step | 改动 | 风险 | 优先级 |
|------|------|------|--------|
| 1 | 新增 `internal/tools/plan/plan.go` — PlanTool 实现 | 低（新文件，不影响现有） | 🔴 紧急 |
| 2 | 注册 PlanTool 到引擎（类似其他工具的注册方式） | 低 | 🔴 紧急 |
| 3 | Prompt 增强 — SmartAgentSection 里加"复杂任务先规划"指引 | 低 | 🔴 紧急 |
| 4 | 计划进度注入 system prompt | 中（需要改 QueryEngine 消息拼接） | 🟡 高 |
| 5 | 增强 injectDecomposedPlan — 更聪明的启发式 | 中 | 🟢 中 |

## 5. 验证方法

| 场景 | 预期 |
|------|------|
| "实现一个 WebSocket 聊天服务器" | ✅ LLM 自动调用 Plan 创建计划 |
| "修复 XXX 函数里的 nil pointer bug" | ✅ 简单任务，不强制规划 |
| "用 gin 写一个完整 todo CRUD API + Dockerfile + README" | ✅ LLM 先规划 → 按步骤执行 |
| "用 Go 实现分布式锁 + 单元测试 + 压测脚本" | ✅ LLM 自动规划 3-5 个 step |
| 中途出错需要调整 | ✅ LLM 更新 Plan 加新 step |

## 6. 不做什么

- **不替换 planning 包的底层数据模型** — 复用现有 Plan/Task 类型
- **不强制所有任务都规划** — 简单任务（"hello world"、"改个变量名"）不干扰
- **不做 plan 自动执行** — 执行权在 LLM，PlanTool 只是追踪器
- **不引入新的 LLM 调用** — 规划和执行在同一个 queryLoop 里完成
