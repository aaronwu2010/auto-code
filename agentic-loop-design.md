# Agentic Loop 设计方案：让程序变成"会思考的执行者"

> 日期：2026-09-01
> 状态：设计阶段，待审批后实施
> 前置：planning / reflection / memory 三个核心包已完整实现（有接口、有实现、有测试通过），但引擎层接法仅为 metadata 注入的薄壳

---

## 一、现状诊断：积木都有，但没用起来

程序是一座**有脚手架没入住的建筑**——底层三个核心包的能力完整，但引擎层只把它们当"提示词注入器"用了一层薄壳：

| 模块 | 包级能力 | 引擎层接法 | 差距 |
|---|---|---|---|
| **Planning** | `ReActPlanner` 完整 ReAct 循环 + `TaskDecomposer` 启发式拆解 + `Plan/Task` 状态机（Pending→InProgress→Completed→Failed→Blocked） | `injectDecomposedPlan()` 只是把拆解步骤渲染成 `<system-reminder>` 文本 | **没有接管执行流**——模型可以无视 plan；没有状态机驱动的"子任务→完成→下一个"调度 |
| **Reflection** | `BaseReflector` + `BaseErrorAnalyzer`（错误分类+根因分析）+ `BaseSelfCorrector`（Suggest+Execute+Validate）+ `FileExperienceStore`（Save/Search/评分） | `reflectOnTurn()` 结束时存一条经验；`injectPendingLessons()` 下一轮取出来注入 | **没有错误自动修复**——tool 调用报错后直接返回给模型，没走 Classify→Remediate→Learn 链路；SelfCorrector 完全没接入 |
| **Memory** | `BaseShortTermMemory` + `BaseLongTermMemory` + `BaseMemoryConsolidator`（自动整理）+ Episodic/Semantic 分层 | `performActiveRecall()` 只做关键词召回 | **没有分层记忆**——短期/长期/语义/情景四种记忆都存在但没统一调度 |
| **Tools** | 35 个 tool | 模型自由选择 | **没有策略性选 tool**——每次把全部 35 个工具塞给模型，没有根据当前 plan/子任务/上下文过滤 |

---

## 二、我（AI Agent）自身的开发设计模式

我处理复杂任务时执行的是 **6 层嵌套循环**：

```
┌─────────────────────────────────────────────────────────────┐
│  L6 目标层   ┌─ Goal → Decompose → SubGoals[] → Replan ─┐ │
│              │                                          │ │
│  L5 验证层   │  ┌─ Verify → Pass/Fail → Refine ─┐      │ │
│              │  │                                 │      │ │
│  L4 修复层   │  │  ┌─ Error → Classify → AutoFix ─┐     │ │
│              │  │  │                                │     │ │
│  L3 记忆层   │  │  │  ┌─ Recall → Apply → Update ──┐    │ │
│              │  │  │  │                             │    │ │
│  L2 规划层   │  │  │  │  ┌─ Plan → Execute ─┐       │    │ │
│              │  │  │  │  │                  │       │    │ │
│  L1 感知层   │  │  │  │  │ Observe → Orient ─┘       │    │ │
│              │  │  │  │  └──────────────────────────┘    │ │
│              │  │  │  └─────────────────────────────────┘ │ │
│              │  │  └──────────────────────────────────────┘ │ │
│              │  └───────────────────────────────────────────┘ │
│              └─────────────────────────────────────────────────┘
└─────────────────────────────────────────────────────────────┘
```

每层的作用：

| 层 | 做什么 | 关键产物 |
|---|---|---|
| **L1 感知** | 读文件、看目录、理解用户输入 | Observation（结构化环境快照） |
| **L2 规划** | 把当前 Observation + 目标 + 经验 → 生成具体行动 | Plan（有序步骤） |
| **L3 记忆** | 主动检索相关历史经验（不是被动等下一轮） | RecallSet（当前任务的经验注入） |
| **L4 修复** | 执行出错 → 自动分类（网络/权限/语法/依赖...）→ 自动尝试修复 | CorrectedResult（修复后继续执行） |
| **L5 验证** | 执行完成 → 自动验证（编译过没过？测试过没过？输出对不对？） | VerificationResult（Pass/Fail/Partial） |
| **L6 目标** | 大目标 → 拆解子目标 → 每个子目标验证后 → 重新规划下一个 | GoalProgress（整体进度 + 剩余路径） |

**关键设计原则：**
- 每层都有**结构化输入输出**（不是纯文本），可以被上层/下层程序化消费
- 每层失败都有**fallback**（修复层失败就跳过继续、验证层失败就标记 Partial）
- 所有跨层传递的信息都**带 metadata**（为什么这么决策、用了哪些经验）

---

## 三、方案设计：6 层循环接入

### 3.1 架构总图

```
用户输入
  │
  ▼
┌────────────────────────────────────────────────────────┐
│  QueryEngine.SubmitMessage()                           │
│                                                        │
│  ┌─ L6 目标层 ─────────────────────────────────┐       │
│  │ taskDecomposer.Decompose(userTask)           │       │
│  │ → Plan{Tasks[0..N], currentIdx, Status}      │       │
│  └──────────────────────────────────────────────┘       │
│  │                                                      │
│  ▼ 驱动                                                │
│  for each subTask in Plan (有状态机):                   │
│    │                                                    │
│    ┌─ L3 记忆层 ─────────────────────────────────┐       │
│    │ reflector.ApplyExperience(ctx, currentSubTask)│      │
│    │ memory.Retrieve(ctx, subTask.Query)         │       │
│    │ → LessonSet + MemoryContext                  │      │
│    └─────────────────────────────────────────────┘      │
│    │                                                    │
│    ▼                                                    │
│    ┌─ L2+L1 规划+感知 ───────────────────────────┐       │
│    │ reActPlanner.Run(goal=currentSubTask,       │       │
│    │     ThoughtGenerator=模型,                    │      │
│    │     ActionExecutor=ToolExecutor)             │      │
│    │ → ReActTrace{Thought, Action, Observation}   │      │
│    └──────────────────────────────────────────────┘      │
│    │                                                    │
│    ▼ 每步 tool 结果出来时                                │
│    ┌─ L4 修复层 ──────────────────────────────────┐       │
│    │ reflector.AnalyzeError(toolResult.err)       │      │
│    │ selfCorrector.Suggest(analysis)              │      │
│    │ if Retryable: 自动改参数重试一次              │      │
│    └─────────────────────────────────────────────┘      │
│    │                                                    │
│    ▼ 子任务完成后                                        │
│    ┌─ L5 验证层 ──────────────────────────────────┐       │
│    │ reflector.Evaluate(subTaskResult)            │      │
│    │ → PASS → 下一个子任务                         │      │
│    │ → FAIL → 标记 Plan.Status=Blocked, 重规划     │      │
│    └─────────────────────────────────────────────┘       │
│    │                                                    │
│    ▼ 整个 Plan 结束后                                    │
│  ┌─ L3b 记忆写入 ─────────────────────────────────┐       │
│  │ reflector.LearnFromExperience(整个 Trace)        │      │
│  │ memory.Consolidate(episodic→semantic)           │      │
│  └─────────────────────────────────────────────────┘     │
└────────────────────────────────────────────────────────┘
```

### 3.2 分阶段实施

#### 阶段 1：L4 错误自动修复（最小改动、最大见效）

**当前状态**：tool 调用出错 → 把错误原文返回给模型 → 模型自己想怎么修 → 重新生成 arguments → 再调一次（但经常犯同样错误）

**改成**：tool 出错时，引擎层先拦截 → 走 `ErrorAnalyzer.CategorizeError()` 分类：

| 错误类型 | 自动修复 |
|---|---|
| `RateLimitError` | 指数退避重试（已有 `retryConfig`） |
| `NetworkError` | 重试 1 次 + 提示网络问题 |
| `SyntaxError` (JSON 解析失败) | 从 `arguments` 里提取完整 JSON（去掉首尾干扰文本）→ 重试 |
| `PermissionError` (路径不在允许范围) | 提示换路径 → 不重试 |
| `FileNotFoundError` | 建议用 glob/grep 先找到文件 → 不重试 |
| `LLMResponseError` (tool_call 格式不对) | 提示模型"上次 tool_call 格式有问题，请确保..." → 重新生成 |

**改动范围**：`internal/engine/query/query.go` 的 `executeToolCall` + `queryLoop`。不改 planning/memory。新增一个 `errorClassifyAndFix()` 函数。

#### 阶段 2：L3 分层记忆统一调度

**当前状态**：`performActiveRecall()` 只做关键词召回，和 `reflector.ApplyExperience()` 并存但不统一。

**改成**：引擎层新增一个 `MemoryOrchestrator`：
```go
func (mo *MemoryOrchestrator) Recall(ctx, currentSubTask) MemoryContext {
  // 1. 短期记忆：当前 session 最近 10 条 tool_result
  // 2. 情景记忆：FileExperienceStore.Search(当前 subTask 关键词)
  // 3. 语义记忆：LongTermMemory.Retrieve(概念级召回)
  // 4. 经验：reflector.ApplyExperience(ReflectionContext)
  // → 去重、排序、截断到 token budget
}
```
所有记忆层的召回走同一个入口，统一渲染成 metadata 消息注入。

#### 阶段 3：L2 ReActPlanner 接管执行流

**这是最核心的一步。** 当前模型用 tool_calls 是"裸"的——模型自己决定"调不调 tool、调哪个、arguments 是什么"。`ReActPlanner` 已经实现了 Thought→Action→Observation 循环，但没接进来。

**接法**：
1. 模型先出 Thought（不暴露给用户）→ 判断下一步该做什么
2. Planner 从 Thought 中提取 Action + Params
3. ToolExecutor 执行 → Observation
4. Observation 喂回给模型 → 下一轮 Thought
5. 直到 Planner 判断 `isComplete()` 或 `shouldEarlyStop()`

**好处**：
- 自动追踪每一步的 Thought→Action→Observation 链路 → 完整的 `ReActTrace`
- Observation 失败可以让 Planner 决策重试/换路/放弃 → 程序化控制，不依赖模型"记得住之前犯过什么错"
- `ReActTrace` 直接喂给 Reflection 层 → 经验学习质量大幅提升

**降级策略**：如果模型连续 3 次 Thought 都无法形成有效 Action（比如模型本身不擅长推理格式），自动 fallback 到当前的裸 tool_calls 模式。

#### 阶段 4：L6 大目标状态机 + L5 自动验证

**当前状态**：`TaskDecomposer` 只是把任务拆成步骤文本，`Plan/Task` 状态机完全没用。

**接法**：
1. 用户输入 → `Decomposer.Decompose()` 生成 `Plan{Tasks[]}`，每个 Task 有明确的 `InputSchema`（输入是什么）、`SuccessCriteria`（怎么算成功）、`DependsOn`（依赖哪个 Task 先完成）
2. 引擎按 `Plan.UpdateProgress()` 驱动状态机：当前 Task = InProgress → L2 循环执行 → L5 `ResultEvaluator.Evaluate()` → PASS 则标记 Completed，触发下一个 Task
3. 如果某个 Task FAIL → 不是立即放弃，而是 L4 修复 → 重试；重试 2 次仍 FAIL → 标记 Blocked，触发 Replan

**验证层具体做什么**：
| Task 类型 | 自动验证 |
|---|---|
| "编译项目" | `go build` 退出码 |
| "跑测试" | `go test` 退出码 + PASS 行 |
| "读文件改内容" | 文件内容包含改动标志 |
| "grep 找什么" | 结果非空 |
| "代码重构" | 编译 + 测试 都过 |

#### 阶段 5：跨 session 经验学习闭环

**当前状态**：`FileExperienceStore` 有 Search/GetMostRelevant，但只在同一 session 内用。

**改成**：
1. 每次完整 Plan 结束后，`LearnFromExperience()` 把 **整个 ReActTrace + VerificationResult** 打包成一条 Experience 存下来
2. Experience 带 `Effectiveness` 评分（Verification 的 PASS/FAIL 自动打分）
3. 下一个新 session 开始时，自动从 ExperienceStore 拉 `top-K relevant` 经验 → 作为系统 prompt 的一部分注入
4. 定期（每周/每 100 次 experience）跑 `MemoryConsolidator` → 把高频模式提炼成 SemanticMemory（"当遇到 X 时，Y 方法有效"）

### 3.3 不做什么（明确的边界）

| 不做 | 为什么 |
|---|---|
| **Multi-Agent 协作**（CEO/PM/Dev 等角色） | 当前 35 个 tool 足够覆盖单 agent 能力，多 agent 会引入复杂度爆发；先把单 agent 的 agentic loop 做好 |
| **全新的 tool 设计** | 35 个 tool 覆盖了 file/exec/web/plan/mcp/agent，够用；L2+L4 做好后，tool 本身的能力不变但**使用方式**会变聪明 |
| **全新的后端路由**（比如"简单 query 用快模型、复杂 plan 用强模型"） | 属于成本优化，可以作为阶段 6；当前先保证正确性 |
| **替换当前 query 主循环** | 用 **wrapper/decorator** 模式包在当前 `queryLoop` 外面（阶段 3 提过降级策略），不重写 |

### 3.4 改动量估算

| 阶段 | 新增文件 | 改现有文件 | 测试工作量 |
|---|---|---|---|
| **阶段 1** (L4 修复) | 0 | `query.go` + `reflection` 包接入 | 中（加 error 分类测试） |
| **阶段 2** (L3 记忆调度) | 1（`memory_orchestrator.go`） | `queryengine.go` 接入 | 小（已有 memory 包测试） |
| **阶段 3** (L2 ReAct 接管) | 1（`react_adapter.go`） | `query.go` 主循环重构 | 大（ReAct 循环 + tool 桥接 + 降级路径） |
| **阶段 4** (L6 状态机) | 0 | `queryengine.go` + `planning` 包接入 | 中（Plan 状态机驱动测试） |
| **阶段 5** (跨 session) | 0 | `queryengine.go` turn 结束 hook | 小（experience persist 测试已存在） |

**总改动量约 1500-2000 行**（主要是 ReAct adapter 和 error 分类），不会碰 UI、不会碰 API 层、不会碰 tool 定义。

---

## 四、核心设计决策背后的思考

**Q: 为什么用 wrapper/decorator 包在 queryLoop 外面，而不是重写 queryLoop？**
A: 因为 queryLoop 已经跑通了 35 个 tool + 三种后端 + 流式处理 + tool_calls 聚合，这些是**资产**不是**技术债**。ReAct 循环相当于"更聪明的 driver"，驱动的还是同一辆车。降级路径必须存在——模型推理格式不稳定时自动 fallback。

**Q: 为什么 ReActPlanner 是 Thought→Action→Observation，而不是直接让模型出 tool_call？**
A: 裸 tool_calls 时，模型**同时做**了 "想清楚该做什么 + 选择哪个 tool + 填 arguments" 三件事。ReAct 把它们解耦——Thought 专注"想清楚"，Planner 专注"选 tool + 填参数"，Observation 专注"看结果"。这样每一步都可程序化控制和验证，错误也更容易定位（是 Thought 错了？还是参数填错了？还是 tool 本身出问题了？）。

**Q: 经验怎么存？存一条完整 Trace 还是只存 lessons？**
A: 存**完整 Trace + 提炼的 Lessons**。完整 Trace 用于 Search（"上次处理类似任务时我做了什么"），提炼的 Lessons 用于 ApplyExperience（"上次遇到同样错误时，X 修复方法有效"）。这是 Reflection 包已经有的 Experience 结构（Goal/Action/Result/LessonsLearned/FailureReasons/SuccessFactors），直接用。

**Q: 阶段 1 为什么先做 L4 错误修复？**
A: ROI 最高。当前最痛的点就是 tool 出错后模型反复犯同样错误（因为没结构化反馈）。L4 只需要在 `executeToolCall` 返回错误时加一个 if 分支，不碰主循环。改完立刻见效——编译报错自动重试、JSON 格式错自动提取、网络错自动退避。这是立竿见影的一步，能验证整个架构的正确性。

---

## 五、前置包能力速查

### planning 包（已实现）
- `ReActPlanner` — ThoughtGenerator / ActionExecutor / ObservationCollector 接口 + 完整 Run 循环
- `BaseTaskDecomposer` — Decompose / CanDecompose / EstimateComplexity，带启发式拆解和模板匹配
- `Plan` / `Task` / `TaskStep` / `TaskDecomposition` — 状态机（Pending→InProgress→Completed→Failed→Blocked）
- `DefaultReActConfig` / `DefaultPlannerConfig` — 配置

### reflection 包（已实现）
- `BaseReflector` — Reflect / Evaluate / AnalyzeError / LearnFromExperience / ApplyExperience
- `BaseErrorAnalyzer` — CategorizeError / AssessSeverity / identifyRootCause / suggestImmediateActions
- `BaseResultEvaluator` — evaluateCorrectness / evaluateEfficiency / evaluateCompleteness / evaluateQuality
- `BaseSelfCorrector` — Suggest / Execute / Validate
- `FileExperienceStore` — Save / Load / Search / GetMostRelevant / Export / Import
- `Experience` 结构 — Goal / Action / Result / LessonsLearned / FailureReasons / SuccessFactors / Effectiveness
- `ReflectionContext` / `ErrorInfo` / `ExperienceQuery` / `CorrectionAction`

### memory 包（已实现）
- `BaseShortTermMemory` — 容量限制 + decay + eviction
- `BaseLongTermMemory` — 持久化 + 关键词 + 概念级召回
- `BaseMemoryConsolidator` — 自动整理 episodic → semantic
- `MemoryItem` / `MemoryQuery` / `MemorySearchResult` / `MemoryConfig`
- `MemoryManager` / `EpisodicMemory` / `SemanticMemory` 接口

---

## 六、总结

**核心理念**：程序已经有了完整的 agentic 基础设施——planning（ReActPlanner）、reflection（ErrorAnalyzer + SelfCorrector + ExperienceStore）、memory（ShortTerm + LongTerm + Consolidator）。当前只是接法太保守（metadata 注入）。设计方案的核心是**让这些模块接管执行流**，而不是继续只做"提示词增强"。

实施顺序：**先修错（L4）→ 再统一记忆（L3）→ 再接管执行（L2）→ 再大目标驱动（L6）→ 最后跨 session 学习（L5+L3b）**。每步独立可交付、可降级。

---

## 七、相关文件索引

| 文件 | 说明 |
|---|---|
| `internal/engine/queryengine.go` | 引擎主入口，阶段 2/4/5 的主要改动点 |
| `internal/engine/query/query.go` | queryLoop 主循环 + StreamingToolExecutor + executeToolCall，阶段 1/3 的主要改动点 |
| `internal/api/openai.go` | OpenAI SSE tool_calls 解析，阶段 0 已修（增量聚合） |
| `internal/api/localai.go` | LocalAI SSE tool_calls 解析，阶段 0 已修（增量聚合） |
| `internal/api/client.go` | Ollama NDJSON 解析 + StreamMessage + mergeToolCallDeltas helper |
| `internal/types/message.go` | ToolCall 结构（Index + Arguments string），阶段 0 已改 |
| `internal/planning/` | ReActPlanner + TaskDecomposer + Plan/Task 状态机，已完整实现 |
| `internal/reflection/` | Reflector + ErrorAnalyzer + SelfCorrector + FileExperienceStore，已完整实现 |
| `internal/memory/` | ShortTerm + LongTerm + MemoryConsolidator，已完整实现 |
| `internal/tools/` | 35 个 tool 目录，不动 |
