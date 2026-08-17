# 程序核心能力对齐优化计划

> 对照用户总结的核心能力（理解任务 / 拆分任务 / 执行任务 / 反馈用户），基于现有代码现状制定的优化方案。
> 制定时间: 2026-08-16
> 原则: 不重写已有半成品，以"接线 + 补齐"为主，保持代码简洁

---

## 一、现状与能力对照

| 用户总结的能力 | 现有代码 | 状态 | 关键证据 |
|---------------|---------|------|----------|
| 1. 分析理解用户输入 | `internal/perception/` 6 文件全实现，但全仓 0 引用；`SubmitMessage` 直接把 prompt 原样塞入 messages | **代码已写未接线** | `queryengine.go:397`；grep `internal/perception`=0 |
| 2. 制定计划项（独立子任务） | `internal/planning/` 8 文件全实现（含 `BaseTaskDecomposer`/`ReActPlanner`），但 0 引用；`defaultTaskExecutor` 是 `time.Sleep(10ms)` mock | **代码已写未接线** | grep `internal/planning`=0；`plan_executor.go:499` |
| 3. 理解先后顺序和依赖关系 | `planning/types.go:102` 的 `Task` 有 `Dependencies/Dependents/SubTasks/OnSuccess/OnFailure`；`BasePlanExecutor.orderTasksByDependencies` 已实现拓扑排序 | **模型已有，执行器是 mock** | `types.go:102`；`plan_executor.go:318` |
| 4. 执行子任务（URL/搜索/读写/总结/保存记忆） | `queryLoop` 的 LLM+工具 ReAct 循环完整；41 个工具注册；`memdir` 记忆已接入 | **已实现** | `query/query.go:320`；`registry.go:63` |
| 5. 按顺序调用，前任务输出→后任务输入 | **完全缺失**。`tools/task/TaskData` 无依赖字段；`planning.Task` 有依赖但无数据流注入机制 | **缺失** | `tools/task/task.go:54` 无 Dependencies |
| 6. 总结子任务完成情况汇报用户 | 流式 `SDKMessage` channel 已有；`OnPhaseChange` 状态栏已有；但无专门"子任务汇总汇报"环节，仅靠系统提示要求 LLM 自觉生成 final summary | **部分实现** | `queryengine.go:855`；`prompts/system.go:140` |

**核心判断**：工程骨架完整，智能层次停留在"单步 ReAct"。四个高级模块写了代码却悬空，任务间数据流和汇总汇报环节缺失。

---

## 二、优化总体思路

**不新增大模块，而是把已有半成品接线 + 补齐两个缺失机制**：

1. **接线**：让 `QueryEngine` 在主循环中调用 `perception` → `planning` → 现有 `queryLoop` → 汇总环节
2. **补齐**：任务间数据流注入机制 + 子任务汇总汇报环节
3. **统一**：合并 `planning.Task` 与 `tools/task/TaskData` 两套脱节的任务模型

**不改动**：工具系统、记忆系统、流式 I/O、上下文压缩——这些已完整。

---

## 三、分阶段实施计划

### 阶段 A：理解层接线（能力 1）

**目标**：用户输入经 `perception` 处理后再进引擎，建立"理解"显式阶段。

**改动点**：
- A1. `QueryEngine.SubmitMessage`（`queryengine.go:377`）在追加 user 消息前，调用 `perception.Manager.Process(prompt, ctx)`，得到结构化 `InputResult`（含意图、特征、置信度）
- A2. 将 `InputResult` 注入系统提示构建（`buildSystemPrompt` queryengine.go:459），让 LLM 看到意图分类
- A3. 简单输入（置信度高、单意图）可跳过 planning 直接进 `queryLoop`，避免过度工程

**验收**：多模态/结构化输入能被解析；纯文本聊天行为不变（向后兼容）。

**风险**：perception 增加延迟。应对：纯文本快速路径跳过 perception。

---

### 阶段 B：拆分 + 依赖 + 数据流（能力 2、3、5）

**目标**：复杂任务经 `planning` 分解为带依赖的子任务图，按拓扑顺序执行，前任务输出可注入后任务输入。

**改动点**：
- B1. **统一任务模型**：以 `planning.Task`（有依赖字段）为主，`tools/task/TaskData` 改为对 planning.Task 的轻量视图（或直接弃用 TaskData 的独立存储，task 工具操作 planning 仓库）
- B2. **对接真实分解器**：`BaseTaskDecomposer` 当前是模板+关键词分解（`task_decomposer.go:61`），需新增一个 `LLMTaskDecomposer`：调用 `QueryDeps.CallModel` 让 LLM 生成结构化子任务 JSON（含依赖声明）
- B3. **对接真实执行器**：`defaultTaskExecutor`（`plan_executor.go:499` 的 sleep mock）替换为 `QueryLoopTaskExecutor`：每个子任务起一个子 `queryLoop`（复用现有 `query.Query`），工具池用 `AssembleToolPool`
- B4. **数据流注入**（核心新增）：`planning.Task` 增加 `InputFromDeps []DepInput` 字段，描述"从依赖任务 X 的输出取字段 Y"。`PlanExecutor` 执行任务前，把依赖任务的 `Output` 按映射拼接到当前任务的 prompt 前缀
- B5. **触发条件**：在 `SubmitMessage` 中判断——若 perception 判定"多步骤意图"或 prompt 含计划关键词（"先...再...""分步"），则走 planning 路径；否则走原 queryLoop

**验收**：输入"先抓取 URL 内容，再总结，最后保存到 memory.md"→ 分解为 3 个子任务，WebFetch 输出注入 Summarize 输入，Summarize 输出注入 FileWrite。

**风险**：LLM 分解可能产出错误依赖图。应对：拓扑排序检测环 + 失败回退到单步 ReAct。

---

### 阶段 C：执行对接 + 反馈汇总（能力 4、6）

**目标**：子任务执行复用现有工具链；执行结束后引擎强制生成"子任务完成情况汇总"汇报用户。

**改动点**：
- C1. **子任务执行复用**：B3 的 `QueryLoopTaskExecutor` 已复用 `query.Query`，无需新写执行逻辑。子任务的工具结果通过现有 `processQueryOutput`（queryengine.go:855）流式回传，前端可见每步进度
- C2. **汇总汇报环节**（核心新增）：`PlanExecutor` 全部子任务完成后，调用一次 `CallModel` 生成汇总（输入=各子任务的 title/status/output 摘要），通过 `SDKMessage{Type:"result", Subtype:"plan_summary"}` 发到前端。这是引擎强制环节，不靠 LLM 自觉
- C3. **失败处理**：某子任务失败时，按 `Task.OnFailure` 决策（重试/跳过/中止/重规划）。重规划调用 `ReActPlanner.Replan`（已实现）
- C4. **状态栏联动**：`OnPhaseChange` 增加"计划阶段"状态（"分解任务中 N 个"/"执行子任务 X/N: title"/"汇总中"），前端可见计划进度

**验收**：执行完多步骤任务后，前端收到一条结构化汇总（含每步状态、产出文件、耗时）；某步失败能重规划或按策略继续。

**风险**：汇总多一次 LLM 调用增加成本。应对：汇总用小模型（接 model_routing，见下）。

---

## 四、配套补齐（低优先级，不阻塞主链路）

| 项 | 现状 | 建议 | 对应能力 |
|----|------|------|----------|
| `internal/reflection/` 接线 | 9 文件全实现 0 引用 | 子任务失败时触发 `Reflector.AnalyzeError`，经验存入 `memdir` | 能力 4 容错 |
| `internal/memory/` 分层记忆接线 | 全实现 0 引用（`memdir` 已接） | 评估是否替换 `memdir`，或先共存 | 能力 4 记忆 |
| `internal/model_routing/` | 缺失 | 新建：分解/汇总用小模型，主推理用大模型 | 成本优化 |
| `internal/tool_routing/` | 缺失 | 暂不建，工具选择继续靠 LLM（已有 ToolSearch） | — |

---

## 五、实施顺序与依赖

```
阶段 A (理解接线) ──┐
                     ├──> 阶段 B (拆分+依赖+数据流) ──> 阶段 C (执行对接+汇总)
                     │
纯文本快速路径保持 ──┘
```

- A、B 可并行设计，但 B 的触发条件依赖 A 的意图判定
- C 强依赖 B（没有计划就没有汇总）
- 每阶段独立可验收，可单独提交

---

## 六、关键决策点（需确认后再编码）

1. **改造 vs 新建**：是在 `QueryEngine.SubmitMessage` 内分支（简单/复杂两条路径），还是新建 `Pipeline` 结构包裹 QueryEngine？倾向前者，改动小。
2. **任务模型统一方向**：`planning.Task` 吞并 `tools/task/TaskData`，还是反向？倾向 planning.Task 为主（它有依赖字段）。
3. **汇总汇报是引擎强制还是 LLM 自觉**？倾向引擎强制（C2），保证用户总结的能力 6 必达。
4. **planning 触发条件**：靠 perception 意图判定，还是靠 prompt 关键词，还是用户显式 `/plan` 命令？倾向"perception 判定 + 显式命令兜底"。
5. **数据流注入的表达形式**：声明式（任务定义里写"从 task[2].output 取 content"）还是隐式（把所有依赖输出拼成上下文）？倾向声明式，可控。

---

## 七、与现有计划文档的关系

- `plan.md`（核心业务逻辑补充）：阶段一工具层基本完成，本计划不重复，聚焦其未覆盖的"能力管线"
- `architecture-improvement-plan.md`（架构改进）：本计划是其第 1、2 项（感知层、规划模块）的**落地集成方案**，并补齐其未提及的"数据流"和"汇总汇报"两个缺失机制

---

## 八、验收总标准

- [ ] 输入纯文本聊天，行为与现状一致（向后兼容）
- [ ] 输入多步骤任务，能分解为带依赖的子任务图并按序执行
- [ ] 前任务输出能作为后任务输入（如 WebFetch→Summarize→FileWrite）
- [ ] 执行结束有结构化汇总汇报（含每步状态/产出）
- [ ] 子任务失败能按策略处理（重试/跳过/重规划）
- [ ] 状态栏可见计划进度
- [ ] 无新增大模块，改动集中在 `queryengine.go` + `planning/` 接线 + 两个新机制

---

**变更记录**:
- 2026-08-16: 初始版本创建