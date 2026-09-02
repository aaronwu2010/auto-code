# Agent Intelligence Alignment Plan
## 让本地 Agent 达到 AI 助手同等智能水平

> 本方案从 **AI 助手自身的核心设计原理** 出发，识别本地项目的能力差距，制定分阶段改进计划。

---

## 一、AI 助手的核心智能设计原理

经过对自身工作模式的深度剖析，AI 助手之所以"聪明"，源于以下 **12 个核心设计原理**：

| # | 原理 | 描述 |
|---|------|------|
| R1 | **元认知（Self-Awareness）** | 能够意识到"我在思考"、"我不确定"、"我可能错了" |
| R2 | **结构化思维链（CoT）** | 复杂任务分步骤推理，每步有中间结论和验证 |
| R3 | **动态重规划（Dynamic Re-planning）** | 执行中发现问题实时调整方向，支持回退和策略切换 |
| R4 | **上下文选择（Context Selection）** | 从海量信息中提取最相关部分，优先级排序，动态裁剪 |
| R5 | **渐进式推理（Progressive Reasoning）** | 从简单假设出发逐步深入，假设-验证模式 |
| R6 | **主动探索（Proactive Exploration）** | 不确定时先搜索/阅读，预测所需信息并提前获取 |
| R7 | **自我纠错（Self-Correction）** | 发现矛盾/错误后立即修正，从错误中学习避免重犯 |
| R8 | **结果交叉验证（Cross-Validation）** | 对重要决策多角度验证，"我生成的代码会不会有 bug？" |
| R9 | **不确定性量化（Uncertainty）** | 对输出给出置信度，低置信度时主动寻求信息 |
| R10 | **反事实推理（Counterfactual）** | "如果用另一种方法会怎样？"评估多方案优劣 |
| R11 | **工作记忆管理（Working Memory）** | 复杂任务中动态管理"当前焦点"和"待办队列" |
| R12 | **深度执行-反思循环（Deep Reflect Loop）** | 执行一段 → 停下来深度反思 → 调整方向 → 继续 |

---

## 二、本地项目现状：能力盘点

### ✅ 已实现的能力

| 模块 | 文件 | 覆盖原理 |
|------|------|----------|
| 错误自动修复 (L4) | `error_handler.go` | R7 自我纠错 |
| 经验记忆 (L3) | `memory_orchestrator.go` | R4 上下文选择 |
| ReAct 渐进式追踪 (L2) | `react_bridge.go` | R2 结构化思维链 |
| 前置环境扫描 (P0) | `pre_execution.go` | R6 主动探索（被动版本）|
| 构建验证门 (P1) | `verification_gate.go` | R8 结果验证（单一维度）|
| 子任务依赖排序 (P1) | `goal_tracker.go` | R11 工作记忆（基础版）|
| 智能执行管道 (P2) | `pipeline.go` | R2 结构化执行 |
| 经验闭环 (P3) | `session_closer.go` | R7 自我纠错（跨 session）|
| 上下文压缩 | `compact/` | R4 上下文选择 |
| 感知层 | `perception/` | R4 上下文选择（输入特征）|

### ⚠️ 部分实现（有框架但深度不足）

| 能力 | 现状 | 缺失 |
|------|------|------|
| 动态重规划 | GoalTracker 能追踪状态，但执行中不会自动重规划 | 缺少"发现子任务失败后自动调整后续计划"的逻辑 |
| 结果验证 | VerificationGate 只做 build/test | 缺少 lint、类型检查、逻辑断言、多角度交叉验证 |
| 主动探索 | Landscaper 在 submit 前被动扫一次 | 缺少执行中"发现信息不足时主动搜索"的能力 |
| 经验提取 | SessionCloser 能提取经验 | 缺少更丰富的经验模式（成功链、反模式、通用教训）|
| 执行-反思循环 | ReActBridge 是轻量渐进式增强 | 缺少"执行 N 步后强制停下进行深度反思"的循环 |

### ❌ 完全缺失的关键能力

| 原理 | 对应模块 | 为什么重要 |
|------|----------|-----------|
| **R8 多角度交叉验证** | `validator/` (全新) | 单一 build/test 通过不代表代码正确。AI 助手会从语法、类型、逻辑、性能、安全多个角度审视输出 |
| **R9 不确定性驱动** | `uncertainty/` (全新) | 知道自己"不知道什么"比"知道什么"更重要。低置信度时主动搜索而非瞎编 |
| **R12 深度执行-反思循环** | `reflect_loop.go` (全新) | 当前 ReAct 每步都调 LLM 但反思很轻。需要"执行一段 → 深度反思 → 调整方向"的周期性循环 |
| **R3 执行中重规划** | `replanner.go` (全新) | 发现路径不对时能自动"擦掉后面几步"重新规划 |
| **R5 假设驱动探索** | `hypothesis.go` (全新) | 先提出几个假设，再有针对性地搜索验证，而非盲目遍历 |
| **R11 工作记忆动态管理** | `focus_manager.go` (全新) | 长任务中动态管理注意力焦点，避免"看漏关键信息" |

---

## 三、实施方案（分 4 阶段）

### 阶段 1：多角度交叉验证器（R8 Cross-Validator）

**目标：** 让 agent 像 AI 助手一样，从多个维度审视自己的输出，而不是只看 build/test 是否通过。

**核心设计：**
```
CrossValidator
├── SyntaxValidator    (lint + compile + type-check)
├── LogicValidator     (单元测试 + 静态分析 + 规则检查)  
├── SecurityValidator  (OWASP Top 10 + 注入检测)
├── PerformanceValidator (N+1检测 + 资源泄漏)
└── ConsequenceValidator (diff影响范围 + 破坏性分析)
```

**集成点：**
- 在 ReActBridge.MarkFinalAnswer 之后、SessionCloser 之前调用
- 也作为 VerificationGate.Run 的补充（build/test 通过后追加多维度验证）
- 验证结果如果发现严重问题，触发自动修复循环

**文件：** `internal/engine/query/cross_validator.go`

---

### 阶段 2：不确定性感知引擎（R9 Uncertainty Engine）

**目标：** 让 agent 知道自己"不知道什么"。当置信度低时，主动搜索/验证而非瞎编。

**核心设计：**
```
UncertaintyEngine
├── ConfidenceScorer    (对每个 tool_call 结果打分: 0.0~1.0)
├── KnowledgeGapDetector (检测"回答里有未验证的断言")
├── ProactiveProber     (低置信度时自动追加 Read/Grep 验证)
└── AssertionValidator  (验证回答中的关键断言是否有证据)
```

**工作模式：**
1. LLM 生成工具调用或最终回答
2. UncertaintyEngine 评估置信度
3. **置信度 > 0.7** → 正常继续
4. **置信度 0.4~0.7** → 追加轻量验证（如快速 grep）
5. **置信度 < 0.4** → 强制主动搜索/阅读，向 LLM 注入新知识

**集成点：**
- 在每次 tool 执行结果返回后、注入回 messages 之前
- 在最终回答生成后、返回给用户之前
- 作为 ReActBridge.RecordObservation 的后置钩子

**文件：** `internal/engine/query/uncertainty.go`

---

### 阶段 3：深度执行-反思循环（R12 Deep Reflect Loop）

**目标：** 从"每步调 LLM"变为"执行一段 → 深度反思 → 调整方向 → 继续"，更接近人类的工作模式。

**核心设计：**
```
ReflectLoop
├── ReflectCycleConfig  
│   ├── MaxActionsPerCycle (默认 5)
│   ├── MinActionsForReflect (默认 3)  
│   ├── ReflectOnError (true: 出错立即反思)
│   └── ReflectOnMilestone (true: 达到里程碑反思)
├── Reflector  
│   ├── SummarizeWhatHappened (总结已做的事)
│   ├── DiagnoseProblems (诊断问题)
│   ├── SuggestAdjustments (建议调整)
│   └── ExtractLessons (提取教训)
└── CycleController (控制循环节奏)
```

**工作流程：**
```
queryLoop 每 5 个 action 后：
  1. 暂停正常迭代
  2. ReflectLoop.BuildReflectContext() → 把最近 5 步 trace + tool 结果打包
  3. 用 LLM 生成反思（独立 prompt，不污染正常对话）
  4. 反思结果：
     - "方向正确，继续" → 注入鼓励上下文，继续
     - "发现问题 X，应该改成 Y" → 注入修正提示 + 重规划 GoalTracker
     - "根本方向错了" → 注入根本性重新思考提示
  5. 继续正常迭代
```

**为什么重要：**
- 当前 queryLoop 每步都调 LLM，但反思很轻（只有防重犯）
- 深度反思让 agent 有机会"停下来喘口气，看看整体方向"
- 类比 AI 助手在回答复杂问题时会"让我整理一下思路"

**文件：** `internal/engine/query/reflect_loop.go`

---

### 阶段 4：执行中动态重规划器（R3 Runtime Replanner）

**目标：** 发现路径不对时自动"擦掉后面几步"重新规划。

**核心设计：**
```
RuntimeReplanner
├── PlanStateMachine (跟踪每个子任务的状态)
│   ├── Pending → Running → Done/Failed/Skipped
│   └── PredecessorFailed → Auto-Skip
├── FailureAnalyzer (分析失败是否可恢复)
│   ├── Recoverable (换个参数重试)
│   ├── Redirectable (换个方案做)  
│   └── Blocked (需要用户决策)
├── PlanPatcher (修改后续计划)
│   ├── Insert new subtask
│   ├── Replace blocked subtask
│   └── Mark subtasks as skipped
└── ProgressTracker (整体进度百分比)
```

**触发条件：**
- 子任务标记为 Failed 时
- CrossValidator 发现严重问题时
- ReflectLoop 反思建议调整时

**集成点：**
- 作为 GoalTracker 的扩展
- 在 ReActBridge.RecordObservation 中检测失败时触发
- ReflectLoop 反思结果返回"方向有问题"时触发

**文件：** `internal/engine/query/runtime_replanner.go`

---

## 四、集成架构图

```
用户输入
  │
  ▼
┌──────────────────────────────────────────────────────┐
│              SubmitMessage 入口                       │
│  ┌────────────┐  ┌────────────┐  ┌────────────────┐  │
│  │ Landscaper │  │ MemoryOrch. │  │ TaskDecomposer │  │
│  │ (P0 前置)  │  │ (L3 记忆)   │  │ (R3 初规划)    │  │
│  └────────────┘  └────────────┘  └────────────────┘  │
└───────────────────────┬──────────────────────────────┘
                        │
                        ▼
┌──────────────────────────────────────────────────────┐
│              queryLoop 主循环                          │
│                                                      │
│  ┌─────────────────────────────────────────────┐     │
│  │  正常迭代：CallModel → ToolExec → RecordObs │     │
│  └───────────┬─────────────────┬───────────────┘     │
│              │                 │                      │
│     每 tool 执行后         每 5 步 action 后           │
│              │                 │                      │
│  ┌───────────▼────┐  ┌────────▼──────────┐           │
│  │  Uncertainty   │  │   ReflectLoop     │           │
│  │  Engine (R9)   │  │   (R12 深度反思)  │           │
│  └───────────┬────┘  └────────┬──────────┘           │
│              │                │                      │
│   置信度 < 0.4?          方向有问题?                  │
│      ↓ Yes                  ↓ Yes                    │
│  主动探索追加    ┌────────────▼──────────┐           │
│  Read/Grep 验证  │  RuntimeReplanner     │           │
│      │          │  (R3 动态重规划)      │           │
│      └────┬─────┴────────────┬──────────┘           │
│           │                  │                        │
│           └──────────┬───────┘                        │
│                      ▼                                │
│            继续正常迭代 / 重规划后迭代                 │
└───────────────────────┬──────────────────────────────┘
                        │
                  最终回答/结束
                        │
          ┌─────────────┴───────────────┐
          ▼                             ▼
  ┌──────────────┐            ┌─────────────────┐
  │ CrossVal.    │            │ SessionCloser   │
  │ (R8 验证)    │            │ (经验闭环)      │
  └──────┬───────┘            └─────────────────┘
         │
  发现严重问题?
     ↓ Yes
  自动修复循环（回到 queryLoop 继续）
```

---

## 五、实施优先级

| 阶段 | 模块 | 预计代码量 | 理由 |
|------|------|-----------|------|
| **1** | CrossValidator | ~500 行 | 直接提升输出质量，从"能编译"到"正确" |
| **2** | UncertaintyEngine | ~400 行 | 减少瞎编，提升回答可信度 |
| **3** | ReflectLoop | ~450 行 | 让 agent 有"停下来思考"的能力 |
| **4** | RuntimeReplanner | ~350 行 | 执行中灵活调整，少走弯路 |

**总量：** ~1700 行新代码，全部可降级（nil 时跳过），不影响现有功能。

---

## 六、与现有代码的关系

### 新增模块（全部在 `internal/engine/query/` 下）
| 新文件 | 依赖现有 | 被谁依赖 |
|--------|----------|----------|
| `cross_validator.go` | `types.Message`, `reflection.ExperienceStore` | `react_bridge.go`, `verification_gate.go` |
| `uncertainty.go` | `types.ToolResult`, `reflection` | `query.go` (每次 tool 后), `react_bridge.go` |
| `reflect_loop.go` | `planning.ReActTrace`, `GoalTracker`, `QueryParams` | `query.go` (每 5 步 hook) |
| `runtime_replanner.go` | `GoalTracker`, `planning.TaskDecomposer` | `reflect_loop.go`, `react_bridge.go` |

### 现有模块需要的小幅修改
| 文件 | 修改 | 范围 |
|------|------|------|
| `queryengine.go` | 初始化新模块，传给 queryLoop | 构造函数 ~30 行 |
| `query.go` | 在 queryLoop 中插入新 hook 点 | 主循环 ~40 行 |
| `react_bridge.go` | 集成 CrossValidator/Uncertainty | 记录/标记方法 ~20 行 |
| `goal_tracker.go` | 暴露修改子任务的 API | 新增 3~4 个方法 |

---

## 七、设计原则

1. **全部可降级：** 每个新模块都有 nil 降级路径，queryLoop 零副作用
2. **零 API 开销时尽量零：** CrossValidator 的 lint/compile/type-check 用本地工具，不调 LLM
3. **LLM 反思独立 prompt：** ReflectLoop 的反思用独立 prompt，不污染正常对话上下文
4. **失败即跳过：** 所有外部命令（lint、type-check）失败时静默跳过，不中断主流程
5. **经验自动持久化：** CrossValidator/Uncertainty 的发现自动写入 ExperienceStore

---

*文档版本：v1.0 | 生成时间：2026-09-03*
