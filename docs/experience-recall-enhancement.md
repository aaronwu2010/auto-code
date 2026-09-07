# 经验召回 + 多轮会话增强设计方案

## 1. 重大发现：auto-code 已经实现了完整的经验召回系统！

### 1.1 已有的完整架构

```
┌────────────────────────────────────────────────────────────────────┐
│ auto-code 经验召回系统（已完整实现）                                │
│                                                                    │
│  写入路径：                                                         │
│  ┌─────────────────┐    ┌──────────────────┐    ┌─────────────┐   │
│  │ queryLoop 对话   │───→│ extractmemories   │───→│ Experience   │   │
│  │ （每轮完成）      │    │ （异步 forked agent）│    │ Store        │   │
│  └─────────────────┘    └──────────────────┘    │ (JSON 文件)   │   │
│                                                  └─────────────┘   │
│  召回路径：                                                         │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │ MemoryOrchestrator.Recall(prompt)                            │   │
│  │                                                             │   │
│  │ Layer 1: pendingLessons（上一轮反思，刚存的）                  │   │
│  │ Layer 2: LongTermMemory（语义记忆，概念级）                    │   │
│  │ Layer 3: reflector.ApplyExperience（ExperienceStore 全库搜索）  │   │
│  │                                                             │   │
│  │ → relevance(0.7) + recency(0.3) 排序                        │   │
│  │ → token budget 截断                                         │   │
│  │ → renderAsSystemReminder → <system-reminder> 注入 messages    │   │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                    │
│  多轮会话：                                                         │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │ queryengine.messages []Message 累积                           │   │
│  │ auto-compact 上下文压缩（context window 75% 触发）            │   │
│  │ MemoryOrchestrator 每次 SubmitMessage 自动 Recall             │   │
│  └─────────────────────────────────────────────────────────────┘   │
└────────────────────────────────────────────────────────────────────┘
```

### 1.2 已有组件清单

| 组件 | 文件 | 状态 |
|------|------|------|
| ExperienceStore 接口 | reflection/reflector.go | ✅ Save/Load/Search/Update/GetMostRelevant |
| FileExperienceStore | reflection/experience_store.go | ✅ JSON 文件持久化 + 关键词搜索 |
| BaseReflector | reflection/reflector_impl.go | ✅ Reflect + ApplyExperience + LearnFromExperience |
| BaseLongTermMemory | memory/long_term_memory.go | ✅ 语义记忆 + Retrieve |
| MemoryOrchestrator | engine/memory_orchestrator.go | ✅ 3 层联合召回 + token budget |
| SessionMemory | services/sessionmemory/ | ✅ 会话内记忆 + ShouldExtractMemory |
| ExtractMemories | services/extractmemories/ | ✅ 异步经验提取（forked agent） |
| ReflectionConfig | reflection/types.go | ✅ 可配置 |
| auto-compact | compact/ | ✅ context 压缩 |

## 2. 为什么用户感觉不到？

**问题不是"没有能力"——问题是"能力没被激活"。**

### 2.1 经验写入触发太苛刻

`ExtractMemories.shouldExtract()` 的条件链：

```go
func (e *ExtractMemories) shouldExtract(messages []types.Message) bool {
    if len(messages) < 2 { return false }           // 至少 2 条消息
    // 还要 hasSubstantiveContent（>20 chars）
    // 还要 !hasSensitiveKeywords（没密码/token 等）
    // 还要 没被禁用（IsAutoMemoryEnabled）
    // 还要 forkedAgentFn 已注册 ← 关键！
}
```

**核心问题：forkedAgentFn 可能没注册**。extractmemories 用"for a agent"来从对话中提取经验，如果没有注册 forked agent，经验就永远不会被写入。

### 2.2 没有 SessionEnd 强制保存

Session 结束时没有 SessionCloser 来确保经验落盘。如果 session 被强制终止，所有 pending 的 lessons 都丢失了。

### 2.3 经验召回的搜索质量

`GetMostRelevant()` 用**关键词匹配**（containsKeyword + extractKeywords），没有：
- 语义嵌入（embedding）
- TF-IDF 或 BM25 排序
- 质量评分 + 时间衰减 + 使用频率衰减

对于复杂的编程任务，关键词匹配可能找不到真正相关的经验。

### 2.4 用户看不到"经验被召回了"

经验被注入到 messages 里是 `<system-reminder>` meta 消息，前端可能不显示或用户没注意到。

## 3. 设计方案

### 核心思路：**不造新轮子，激活现有能力**

auto-code 的经验召回系统已经完整实现。需要做的是：
1. 确保 forkedAgentFn 被注册 → 经验能写入
2. 加 SessionEnd 强制保存 → 经验不丢
3. 增强搜索质量 → 召回更准
4. 让用户看到效果 → 可视化

### 3.1 Step 1: 经验写入激活（紧急）

**问题**：extractmemories 依赖 `RegisterForkedAgentFn()` 注册 forked agent，如果没注册，经验永远不会被写入。

**修复**：在 queryengine.go 的初始化里（或某个更早的地方）注册 forked agent。确保每个 session 结束/每轮对话后，有机会提取和保存经验。

具体改法：
- 在 queryengine 初始化时调用 `extractmemories.RegisterForkedAgentFn(...)`
- 用简化版的 LLM extraction（可以复用 auto-code 自己的 LLM 调用）
- 降低 `shouldExtract` 的触发阈值

### 3.2 Step 2: SessionEnd 强制保存（紧急）

**问题**：没有 SessionCloser。session 结束时 pendingLessons 在内存里，丢失。

**修复**：
- 在 QueryEngine 上加一个 `OnSessionEnd()` 方法
- session 结束时：把 pendingLessons 存到 ExperienceStore + 持久化 longTermMemory
- 注册到 queryengine 的 cleanup 流程

### 3.3 Step 3: 经验召回工具（增强）

**新增 ExperienceRecall 工具**——让 LLM 能**主动查询**经验库（而不是只被动等待 MemoryOrchestrator 注入）：

```go
// internal/tools/experiencerecall/experience_recall.go

type ExperienceRecallInput struct {
    Query   string `json:"query"`    // 描述你要解决的问题/遇到的困难
    MaxResults int  `json:"max_results"` // 最多返回几条
    FilterType string `json:"filter_type,omitempty"` // "success" | "failure" | "pattern" | ""(全部)
}

type ExperienceRecallOutput struct {
    Experiences []ExperienceHit `json:"experiences"`
    Count       int             `json:"count"`
    Summary     string          `json:"summary"`
}
```

**为什么需要这个工具？**

| 场景 | MemoryOrchestrator（被动） | ExperienceRecall（主动） |
|------|--------------------------|------------------------|
| 用户说"帮我写一个 Go 项目" | ✅ 自动召回 | LLM 可以主动搜"Go 项目经验" |
| LLM 遇到 build 错误 | ❌ 不会主动查 | ✅ 搜"build failure"相关经验 |
| LLM 用了一个没经验的技术 | ❌ 不会主动查 | ✅ 搜"gin"、"grpc"相关经验 |

**类比**：我自身的 ExperienceRecall 工具就是让我主动查历史经验。auto-code 需要同等能力。

### 3.4 Step 4: Prompt 引导（增强）

在 SmartAgentSection 或 AdaptiveExecutionSection 里加一句：

```
遇到以下情况时，先用 ExperienceRecall 工具查查有没有相关经验：
- 遇到 build error 或 编译错误
- 用了不熟悉的技术栈（gin/grpc/k8s 等）
- 之前尝试过失败的方向，要换方向
```

### 3.5 Step 5: 搜索质量增强（v2）

当前 GetMostRelevant 只做关键词匹配。增强方向：
- 加 **技术栈标签**（从 Experience.Keywords 里提取）
- 加 **时间衰减**（越近的经验权重越高）
- 加 **使用频率衰减**（用过很多次的经验权重降低）
- 可选：引入轻量 embedding（需要模型支持，v2 再说）

## 4. 实施计划

| Step | 改动 | 风险 | 优先级 |
|------|------|------|--------|
| 1 | 激活 forkedAgentFn + 降低 shouldExtract 阈值 | 低 | 🔴 紧急 |
| 2 | SessionEnd 强制保存经验 | 低 | 🔴 紧急 |
| 3 | 新增 ExperienceRecall 工具 | 低（新文件） | 🟡 高 |
| 4 | 注册 ExperienceRecall + 加 core tool | 低 | 🟡 高 |
| 5 | Prompt 引导 LLM 用 ExperienceRecall | 低 | 🟢 中 |
| 6 | 搜索质量增强（技术栈标签 + 时间衰减） | 中 | 🔵 v2 |

## 5. 验证方法

| 测试场景 | 预期 |
|---------|------|
| Session A：用 Go 实现 WebSocket 服务器 | Session 结束后经验被保存（JSON 文件落盘） |
| Session B：用户说"帮我写另一个 Go 项目" | ExperienceRecall 能查到 Session A 的 Go 经验 |
| Session B：LLM 遇到 build error | 主动调 ExperienceRecall 查"build failure"经验 |
| 经验库空时 ExperienceRecall 返回空 | 不报 error，返回空数组 |

## 6. 不做什么

- **不引入 embedding / vector DB** — 关键词匹配对编程经验召回已经够用，embedding 增加依赖
- **不做经验去重/合并** — 让 ExperienceStore 自己处理（当前已支持）
- **不修改 ExperienceStore 数据模型** — 复用现有 JSON schema
- **不做 web UI 展示经验** — 先让后端跑通
