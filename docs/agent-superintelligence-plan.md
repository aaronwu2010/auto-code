# Agent 超级智能化方案：让程序像我一样聪明

> 日期：2026-09-01
> 状态：设计完成，按 P0 → P1 → P2 顺序实施
> 前置：`agentic-loop-design.md` 的六层循环已全部实施完毕（L1-L6 + 阶段 1-5）

***

## 〇、为什么还需要这个文档？

前一个设计文档实现了六层 **ReAct 增强**——让 LLM 能追踪 Thought→Action→Observation、记住失败、调度子任务、自动修复。但那是**让 LLM 自己更会想**。

这份文档做的是**让程序更会执行**。我（AI agent）处理任务的优势不只是"想清楚"，而是有一套**执行纪律**——在我把控制权交给 LLM 推理之前，我会先主动探索环境、收集必要信息、按依赖排序子任务、强制验证结果。这套纪律不能靠 LLM 自己想出来。

### 核心洞察

```
普通 agent:     用户 prompt → 直接让 LLM 推理 → 执行 tool_calls → 循环
我 (AI agent):  [环境探索] → [结构化注入] → [让 LLM 推理] → [程序化执行] → [强制验证]
```

LLM 擅长**推理**（想清楚要做什么），但不擅长**执行纪律**（按正确顺序做对的事）。这套纪律必须由程序架构保证。

***

## 一、方案 P0：前置环境扫描（Pre-Execution Landscaping）

### 1.1 解决什么问题

用户说"修一下编译错误"，agent 直接把 prompt 扔给 LLM。但 LLM 此时是**盲人摸象**——它不知道项目里有哪些文件、哪些接口、哪些类型定义。

我处理任务的第一反应是：

```
1. Glob "**/*.go"     → 看项目结构
2. Grep "编译错误"    → 找用户说的错误在哪
3. Read 几个核心文件  → 看代码风格和结构
4. 把这些信息喂给 LLM  → LLM 才能做有根据的推理
```

**这 4 步我从来不会省略。** 但当前程序的 agent 完全没做——LLM 在信息真空里瞎猜。

### 1.2 与 MemoryOrchestrator 的关系

| 模块                            | 解决什么                         | 什么时候触发                                    |
| ----------------------------- | ---------------------------- | ----------------------------------------- |
| **MemoryOrchestrator**        | 召回跨 session 的经验（"上次遇到过类似问题"） | CallModel 前                               |
| **Pre-Execution Landscaping** | 探索当前 session 的环境（"现在项目是什么样"） | SubmitMessage 最开头，在 MemoryOrchestrator 之前 |

**两者互补**：一个解决"我从过去学到了什么"，一个解决"现在我面对的是什么"。

### 1.3 具体设计

```go
// 文件: internal/engine/query/pre_execution.go

// Landscaper 前置环境扫描器
// 触发时机: QueryEngine.SubmitMessage() 最开头
// 扫描目标: 当前工作目录 + 用户 prompt 里提到的关键词
type Landscaper struct {
    // 配置: 哪些目录/文件要跳过（node_modules, .git, vendor, dist 等）
    skipPatterns []*regexp.Regexp
    // 单文件最大读取字节数（防爆 token）
    maxFileReadBytes int
    // 最大注入 token 预算
    maxInjectTokens int
}

// LandscapingResult 扫描结果
type LandscapingResult struct {
    // 项目结构摘要（树状，只到 2 层 + 大文件列表）
    ProjectStructure string
    // 关键词搜索命中（用户 prompt 里提取的关键词在源码里的 grep 结果）
    KeywordHits []KeywordHit
    // 自动读取的关键文件（用户提到了 或 grep 命中最密集的）
    KeyFilesRead []FileRead
}

type KeywordHit struct {
    Keyword string
    File    string
    Line    int
    Snippet string
}

type FileRead struct {
    Path    string
    Content string // 截断后的
    Reason  string // 为什么读这个文件
}

// Run 执行扫描，返回一个 string 可以直接作为 meta message 注入 messages 最前面
func (l *Landscaper) Run(ctx context.Context, projectDir, userPrompt string) string
```

### 1.4 执行流程

```
SubmitMessage(prompt):
│
├─ 1. Landscaper.Run(ctx, cwd, prompt)          ← P0 新增
│   │
│   ├─ 1a. Glob 项目结构（跳过 node_modules/.git/vendor/dist）
│   │     → 生成 tree -L 2 风格的摘要
│   │
│   ├─ 1b. 从 prompt 提取关键词
│   │     → 动词 + 名词搭配（简单规则，零 LLM）
│   │     → "修编译错误" → ["build", "compile", "error", "fix"]
│   │     → "改 main.go 的 Foo" → ["main.go", "Foo"]
│   │
│   ├─ 1c. 对关键词 Grep
│   │     → 并发搜索，限流（防止扫大项目耗时过长）
│   │     → 取 top-20 命中
│   │
│   ├─ 1d. 自动 Read 关键文件
│   │     → 用户 prompt 里直接提到的文件
│   │     → grep 命中最密集的 top-3 源码文件
│   │     → 每个只读前 200 行或 8KB（防爆 token）
│   │
│   └─ 1e. 渲染成 meta message
│         → "[Project Landscape] 项目有 15 个 .go 文件，3 个包...\n"
│         → "[Project Structure] src/\n  main.go\n  utils/\n    helper.go\n"
│         → "[Keyword Hits] 'Foo' found in main.go:42, utils/helper.go:15..."
│         → "[Key File] main.go (用户提到)\n---\npackage main\nimport...\n..."
│
├─ 2. MemoryOrchestrator.Recall()               ← 已有（阶段 2）
├─ 3. 用户 prompt
└─ 4. query.Query() → queryLoop
```

### 1.5 安全考虑

| 风险            | 对策                                            |
| ------------- | --------------------------------------------- |
| **大项目扫太慢**    | 加超时（2s）、文件数上限（最多 200 个）、glob 层数上限（3 层）        |
| **二进制/大文件误读** | 按扩展名白名单（go/py/ts/js/java/c/rs/md/json/yaml）过滤 |
| **爆 token**   | 总注入量控制在 maxInjectTokens=2000 以内，超过则截断         |
| **扫描开销**      | 全部并发执行 + goroutine pool；扫描失败直接跳过，不影响主流程       |

### 1.6 降级

```go
// 在 Landscaper.Run() 里：
if !isLandscapingEnabled() {
    return ""
}
// 以及：
// Landscaper.Run 里的任何错误都不向上传播
// 失败 = 返回空串 = 和没开一样
```

### 1.7 预估工程量

| 文件                                            | 行数          | 职责                                            |
| --------------------------------------------- | ----------- | --------------------------------------------- |
| `internal/engine/query/pre_execution.go`      | \~200       | Landscaper 核心：Glob + 关键词提取 + Grep + Read + 渲染 |
| `internal/engine/query/pre_execution_test.go` | \~80        | 关键词提取规则测试                                     |
| `internal/engine/queryengine.go`              | \~10        | SubmitMessage 开头加 Landscaper.Run              |
| **合计**                                        | **\~290 行** | <br />                                        |

***

## 二、方案 P1：依赖感知子任务排序 + 验证门

### 2.1 解决什么问题

**问题 A**：当前 GoalTracker 能追踪子任务状态，但**不知道依赖关系**。如果 LLM 提议了：

```
st-1: 修复类型定义
st-2: 修复 interface 实现（依赖 st-1）
st-3: 修复 consumer 代码（依赖 st-2）
```

当前架构允许它们乱序执行。但实际上 st-2 不可能在 st-1 完成前做好。

**问题 B**：当前程序的"完成"定义是「模型说它完成了」。但我不会信模型说的——我必须跑 `go build && go vet && go test`，**全绿才算真的完成**。

### 2.2 具体设计

#### 2.2.1 依赖感知排序（扩展 GoalTracker）

```go
// 文件: internal/engine/query/goal_tracker.go （扩展）

type GoalSubtask struct {
    // ...现有字段...

    // 新增：依赖关系
    DependsOn []string `json:"depends_on"` // 子任务 ID 列表

    // 新增：推断得到的执行顺序
    Order int `json:"order"` // DAG 拓扑排序后的顺序
}

// TopologicalSort 按 DependsOn 对所有子任务做 Kahn 算法拓扑排序
// 返回排序后的子任务列表，以及可能存在的循环依赖（如果有）
func (gt *GoalTracker) TopologicalSort() ([]*GoalSubtask, error)

// MarkSubtaskCompleted 在依赖链上自动解锁下一个子任务
func (gt *GoalTracker) MarkSubtaskCompleted(id string) []*GoalSubtask
```

#### 2.2.2 自动推断依赖

规则（零 LLM，纯启发式）：

```
规则 1: 子任务描述里包含 "先/首先/第一步" → 优先级 0（最早执行）
规则 2: 子任务描述里包含 "再/然后/接着/第二步/第三步" → 按序号排序
规则 3: 子任务描述里包含 "验证/编译/测试/build/test" → 自动依赖所有其他子任务
规则 4: 如果子任务提到的文件路径包含相同前缀 → 隐含依赖（同目录下的类型定义先于使用）
规则 5: 如果完全没有可推断的依赖 → 保持原序
```

#### 2.2.3 验证门（Verification Gate）

```go
// 文件: internal/engine/query/verification_gate.go （新增）

// VerificationGate 标记"完成"前强制验证
// 核心设计：在 ReActBridge.MarkFinalAnswer() 里调用
type VerificationGate struct {
    // 项目类型自动检测（Go → go build/vet/test, Node → npm test, Python → pytest...）
    projectType ProjectTypeDetector

    // 预定义的验证命令（按项目类型）
    verificationCommands map[ProjectType][]VerificationCommand
}

type VerificationCommand struct {
    Name    string   // "go build"
    Cmd     string   // "go build ./..."
    Args    []string // 可选参数
    Timeout time.Duration
    // 失败了怎么办
    OnFailure FailureMode // Abort / RetryOnce / ContinueButWarn
}

// Run 执行所有验证命令，返回结果
type VerificationResult struct {
    OverallPass bool
    Checks      []CheckResult
    // 第一个失败的完整输出（自动喂回 LLM）
    FirstFailureOutput string
}

type CheckResult struct {
    Name   string
    Pass   bool
    Output string
    Cmd    string
}

// Run 在 ReActBridge 的 Hook 4 位置调用
// 如果验证失败 → 把错误作为 meta message 注入，不标记完成
func (g *VerificationGate) Run(ctx context.Context, cwd string) *VerificationResult
```

#### 2.2.4 验证失败自动反馈

```go
// 在 ReActBridge 里：
func (b *ReActBridge) MarkFinalAnswer(answer string) {
    // 原逻辑：b.trace.Complete(answer)

    // 新增：验证门
    if b.verificationGate != nil {
        result := b.verificationGate.Run(ctx, cwd)
        if !result.OverallPass {
            // 验证失败 → 不标记完成
            // 而是把错误注入 messages，让下一轮循环继续修
            b.injectVerificationFailure(result)
            log.Printf("[ReAct-Bridge] verification FAILED: %s", result.FirstFailureOutput[:200])
            // 不 return，让 queryLoop 继续下一轮
            b.mu.Lock()
            b.lastVerificationFailure = result
            b.mu.Unlock()
            return // ← 不 Complete，不 return 到 terminal
        }
    }

    b.trace.Complete(answer)
}
```

### 2.3 执行流程变化

```
之前:
  Hook 4: MarkFinalAnswer → 标记完成 → 输出结果 → return

现在:
  Hook 4: MarkFinalAnswer
    ├─ VerificationGate.Run()
    │   ├─ go build ./...  → pass?
    │   ├─ go vet ./...    → pass?
    │   └─ go test ./...   → pass?
    │
    ├─ 全 pass? → trace.Complete() → return
    └─ 有 fail? → injectVerificationFailure() → 下一轮继续修
```

### 2.4 降级

```go
// VerificationGate 自动检测：
// - 如果项目里没有 go.mod / package.json / requirements.txt → 跳过验证
// - 如果验证命令执行超时（>30s）→ 返回 warn 不 fail
// - 可以通过配置关闭：config.EnableVerificationGate = false
```

### 2.5 预估工程量

| 文件                                                | 行数          | 职责                       |
| ------------------------------------------------- | ----------- | ------------------------ |
| `internal/engine/query/verification_gate.go`      | \~150       | 验证门：项目类型检测 + 命令执行 + 结果解析 |
| `internal/engine/query/verification_gate_test.go` | \~60        | 项目类型检测测试                 |
| `internal/engine/query/goal_tracker.go`           | \~80        | 扩展：DAG 拓扑排序 + 自动推断依赖     |
| `internal/engine/query/react_bridge.go`           | \~30        | Hook 4 里加验证门调用           |
| **合计**                                            | **\~320 行** | <br />                   |

***

## 三、方案 P2：智能执行管道（Smart Execution Pipeline）

### 3.1 解决什么问题

当前 agent 把 tool\_calls 当**单步执行**：每执行完一个 tool\_call，就回 LLM 等它想下一步。但**很多任务是固定模式的 pipeline**——编译失败 → 读错误 → 修代码 → 再编译，完全可以程序化，不需要每步都问 LLM。

我会做的事情：

1. **让 LLM 规划 pipeline**（一次性想完整执行链）
2. **程序自动执行 pipeline**（零 LLM 中间调用）
3. **每步的输出自动传给下一步的参数**（数据管道）
4. **最后统一把结果喂回 LLM**（总结）

### 3.2 具体设计

#### 3.2.1 Pipeline 数据结构

```go
// 文件: internal/engine/query/pipeline.go （新增）

// PipelineStep 管道中的一个步骤
type PipelineStep struct {
    // 显示名称（日志/调试用）
    Name string `json:"name"`
    // 要调用的 tool 名
    Tool string `json:"tool"`
    // 固定参数（简单情况）
    Args map[string]any `json:"args,omitempty"`
    // 动态参数（复杂情况：从之前步骤的结果里取）
    ArgsFrom []ArgFromSource `json:"args_from,omitempty"`
    // 失败时的行为
    OnFailure OnFailureAction `json:"on_failure,omitempty"`
}

// ArgFromSource 动态参数来源
type ArgFromSource struct {
    // 参数名
    Param string `json:"param"`
    // 从哪个 step 的结果里取（index 或 name）
    FromStep string `json:"from_step"`
    // 结果里的哪个字段（tool 返回的是 *ToolResult）
    FromField string `json:"from_field"`
    // 可选：结果处理函数（截断/正则提取/JSON path...）
    Transform string `json:"transform,omitempty"`
}

type OnFailureAction string

const (
    OnFailureAbort    OnFailureAction = "abort"     // 整个管道中止
    OnFailureContinue OnFailureAction = "continue"  // 继续下一步（失败的结果传空值）
    OnFailureRetry    OnFailureAction = "retry"     // 重试当前步骤 N 次
)

// PipelineSpec LLM 可以返回的管道规格
// 和 ToolCalls 并列的第二种响应格式
type PipelineSpec struct {
    ID     string          `json:"id"`
    Goal   string          `json:"goal"`   // 管道的目标（给 LLM 自己看的）
    Steps  []PipelineStep  `json:"steps"`
    // 成功条件：所有步骤 pass 还是只要最后一个 pass
    SuccessMode SuccessMode `json:"success_mode,omitempty"`
}

type SuccessMode string

const (
    SuccessAll    SuccessMode = "all"    // 所有步骤必须成功
    SuccessLast   SuccessMode = "last"   // 只要最后一个步骤成功
)
```

#### 3.2.2 预设 Pipeline（程序内置）

```go
// 程序内置的常用 pipeline——零 LLM 调用，纯确定性执行
var PresetPipelines = map[string]func() PipelineSpec{
    "go_build_fix": func() PipelineSpec {
        return PipelineSpec{
            ID: "go_build_fix",
            Goal: "Fix Go compilation errors",
            Steps: []PipelineStep{
                {
                    Name: "compile",
                    Tool: "bash",
                    Args: map[string]any{"command": "go build ./..."},
                },
                // ↑ 如果成功 → 跳过后续步骤
                // ↓ 如果失败 → 自动解析错误，跳到 fix 步骤
                {
                    Name: "analyze_errors",
                    Tool: "bash",
                    ArgsFrom: []ArgFromSource{
                        {Param: "command", FromStep: "compile", FromField: "stderr", Transform: "extract_file_lines"},
                    },
                    OnFailure: OnFailureAbort,
                },
                {
                    Name: "fix",
                    Tool: "edit_file",
                    ArgsFrom: []ArgFromSource{
                        {Param: "path", FromStep: "analyze_errors", FromField: "files"},
                        {Param: "replacements", FromStep: "analyze_errors", FromField: "fix_suggestions"},
                    },
                    OnFailure: OnFailureContinue,
                },
                {
                    Name: "verify",
                    Tool: "bash",
                    Args: map[string]any{"command": "go build ./..."},
                },
            },
            SuccessMode: SuccessLast,
        }
    },
}
```

#### 3.2.3 LLM 提议 Pipeline（ReActBridge 扩展）

```go
// ReActBridge.RecordThoughtAction 扩展：
// LLM 的 response 结构里，ToolCalls 和 PipelineSpec 是并列的两种选择
type LLMResponse struct {
    Text      string
    ToolCalls []types.ToolCall
    Pipeline  *PipelineSpec  // 新增
}

// 如果 LLM 返回 PipelineSpec：
// 1. 安全检查（无限循环检测，步数上限 ≤ 10）
// 2. ProgrammaticExecutor 自动执行
// 3. 每步输出自动传给下一步
// 4. 统一汇总成 observation 喂回 LLM
```

#### 3.2.4 ProgrammaticExecutor

```go
// PipelineExecutor 程序化执行器
type PipelineExecutor struct {
    tools       tools.ToolRegistry
    progressFn func(stepName string, result *tools.ToolResult, err error)
}

type PipelineResult struct {
    Pass   bool
    Steps  []StepResult  // 每个 step 的结果
    // 汇总输出（喂回 LLM 的 observation 内容）
    Summary string
}

type StepResult struct {
    Name     string
    Tool     string
    Args     map[string]any
    Success  bool
    Output   string  // 截断后的
    Error    string
    Duration time.Duration
}

// Run 执行整个管道
// prevResults: 之前步骤的结果（用于 ArgsFrom 引用）
func (e *PipelineExecutor) Run(ctx context.Context, spec PipelineSpec) PipelineResult
```

### 3.3 执行流程变化

```
之前:
  Turn 1: LLM → tool_call(bash: go build) → Error
  Turn 2: LLM → tool_call(edit_file: main.go)
  Turn 3: LLM → tool_call(bash: go build) → OK
  Turn 4: LLM 输出答案
  = 4 次 LLM 调用

现在 (Pipeline):
  Turn 1: LLM → PipelineSpec(go_build_fix)
    程序自动执行:
      Step 1: bash(go build) → Error
      Step 2: 自动解析错误
      Step 3: edit_file(main.go, fix)
      Step 4: bash(go build) → OK
    汇总所有结果 → 一条 observation
  Turn 2: LLM 看汇总 → 输出答案
  = 2 次 LLM 调用

优势:
  - LLM 调用数减半
  - 执行链不依赖 LLM 中途不犯错
  - 每步的参数来自上一步的结果，更准确
```

### 3.4 安全检查（防止 LLM 提议恶意 pipeline）

```go
func validatePipeline(spec PipelineSpec) error {
    // 1. 步数上限 ≤ 10
    if len(spec.Steps) > 10 {
        return fmt.Errorf("pipeline too long: %d steps", len(spec.Steps))
    }
    // 2. 禁止 tool 重复（防止无限循环）
    seen := make(map[string]int)
    for _, step := range spec.Steps {
        seen[step.Tool]++
        if seen[step.Tool] > 3 {
            return fmt.Errorf("tool %s repeated %d times", step.Tool, seen[step.Tool])
        }
    }
    // 3. 超时保护
    // 4. 每步的参数大小检查（防止读超大文件）
}
```

### 3.5 降级

```go
// 3 层降级保护：
// 1. 如果 LLM 返回 PipelineSpec 但验证不通过 → 当作普通 ToolCalls 处理
// 2. 如果 ProgrammaticExecutor.Run() 里某步失败 + OnFailure=Abort → 管道中止，返回已执行的结果
// 3. 如果 pipeline 执行总时间超过 30s → 中止，返回部分结果
```

### 3.6 预估工程量

| 文件                                              | 行数          | 职责                                                        |
| ----------------------------------------------- | ----------- | --------------------------------------------------------- |
| `internal/engine/query/pipeline.go`             | \~250       | PipelineStep/PipelineSpec/PipelineResult 类型定义             |
| `internal/engine/query/pipeline_executor.go`    | \~200       | ProgrammaticExecutor + ArgsFrom 动态解析 + 并发执行               |
| `internal/engine/query/pipeline_security.go`    | \~80        | validatePipeline 安全检查                                     |
| `internal/engine/query/predefined_pipelines.go` | \~150       | 预设 pipeline（go\_build\_fix、python\_test\_fix、npm\_fix...） |
| `internal/engine/query/react_bridge.go`         | \~50        | Hook 扩展：LLM 响应解析 → PipelineSpec                           |
| **合计**                                          | **\~730 行** | <br />                                                    |

***

## 四、三个方案的关系

```
                    ┌─────────────────────┐
                    │   SubmitMessage     │
                    │  (用户 prompt)      │
                    └─────────┬───────────┘
                              │
              ┌───────────────┼───────────────┐
              │               │               │
     ┌────────▼────────┐ ┌────▼─────┐ ┌──────▼──────┐
     │   P0 Landscaper │ │ Memory   │ │  Reflector  │
     │  (环境探索)      │ │ Orchestr │ │  (跨session)│
     └────────┬────────┘ └────┬─────┘ └──────┬──────┘
              │               │               │
              └───────────────┼───────────────┘
                              │
                    ┌─────────▼─────────┐
                    │   queryLoop       │
                    │  ┌─ CallModel ─┐  │
                    │  │ LLM 推理    │  │
                    │  └──────┬───────┘  │
                    │         │          │
                    │    ┌────┴────┐      │
                    │    │         │      │
                    │ ┌──▼──┐ ┌───▼───────┐
                    │ │Tool │ │ Pipeline  │  ← P2: LLM 可选返回 pipeline
                    │ │Calls│ │(程序化执行)│
                    │ └──┬──┘ └─────┬─────┘
                    │    │          │      │
                    │    └────┬─────┘      │
                    │         │            │
                    │   ┌─────▼──────┐     │
                    │   │ Tool Exec  │     │
                    │   │ (P1: 验证门)│ ← P1: 每步自动验证
                    │   └─────┬──────┘     │
                    │         │            │
                    │   [循环 or 结束]     │
                    └─────────────────────┘
                              │
                    ┌─────────▼──────────┐
                    │  P1 验证门 (Gate)  │  ← 标记完成前强制验证
                    │  go build/test/vet  │
                    └─────────┬──────────┘
                              │ pass?
                         ┌────┴────┐
                    pass │         │ fail
                         ▼         ▼
                   完成(✓)    不完成 → 注入失败 → 下一轮继续修
```

***

## 五、实施顺序与依赖

```
P0 Landscaper  ────── 无依赖 ────── 先做（投入产出比最高）
      │
      ▼
P1 Gate + DAG ────── 依赖 GoalTracker（已在阶段 3 做了扩展点）
      │
      ▼
P2 Pipeline  ──────── 依赖 P1 的验证结果（预设 pipeline 需要验证门）
```

每一步做完后，全量 build + vet + test 确保不退化。

***

## 六、不做什么

| 不做                          | 原因                               |
| --------------------------- | -------------------------------- |
| **让 LLM 直接修改代码并执行（无验证）**    | 太危险，安全边界模糊                       |
| **自动修复所有编译错误**              | 自动化修复的质量不稳定，LLM 仍然需要参与决策         |
| **Pipeline 里执行复杂 shell 脚本** | Pipeline 只做"工具调用链编排"，复杂逻辑留给 LLM  |
| **Landscaper 扫描所有目录**       | 大项目会超时，严格限制白名单                   |
| **验证门强制执行所有项目**             | 非 Go 项目需要启发式检测，失败时 gracefully 跳过 |

***

## 七、风险与缓解

| 风险                  | 影响       | 缓解                                   |
| ------------------- | -------- | ------------------------------------ |
| Landscaper 扫描耗时过长   | 用户等待时间增加 | 硬超时 2s + 最大文件数 200                   |
| Pipeline 提议被 LLM 滥用 | 执行链爆炸    | 步数上限 + tool 重复上限 + 安全验证              |
| 验证门频繁触发，用户觉得烦       | 用户体验     | 可配置关闭 + 项目类型自动跳过                     |
| 引入新 bug             | 程序不可靠    | 每步全量 build+vet+test                  |
| 与现有六层循环冲突           | 行为不确定    | P0/P1/P2 都是**增强**而非替换，所有新模块 nil-safe |

***

## 八、总预估工程量

| 方案            | 文件数    | 总行数        | 阶段     |
| ------------- | ------ | ---------- | ------ |
| P0 Landscaper | 3      | \~290      | 1      |
| P1 DAG + Gate | 4      | \~320      | 2      |
| P2 Pipeline   | 5      | \~730      | 3      |
| **合计**        | **12** | **\~1340** | <br /> |

***

> ✅ 设计完成，准备按 P0 → P1 → P2 顺序实施

