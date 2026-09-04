# 自动构建验证闭环设计文档

## 1. 可行性评估

### 1.1 结论：**完全可行**，且大部分基础设施已存在

auto-code 已经有 `VerificationGate`（[verification\_gate.go](file:///d:/auto-code/internal/engine/query/verification_gate.go)），能自动检测项目类型并跑 build/test/vet。问题出在**集成层面**——有 bug 导致验证失败不能阻止退出，以及覆盖不全。

### 1.2 现有能力盘点

| 组件                | 状态   | 说明                                                         |
| ----------------- | ---- | ---------------------------------------------------------- |
| VerificationGate  | ✅ 已有 | 检测 Go/Node/Python/Rust/Java/Generic，跑 build+vet+test       |
| ReActBridge 集成    | ✅ 已有 | MarkFinalAnswer 里调用 gate.Run()                             |
| 失败消息注入            | ✅ 已有 | BuildGateFailureMessage() 渲染成 `<verification-failure>` XML |
| 防重犯注入             | ✅ 已有 | BuildPreCallContext() 把 lastGateFailure 注入下一轮              |
| **集成 Bug**        | ❌ 阻断 | 验证失败时 MarkFinalAnswer return 了，但 query.go 照样发 terminal 退出  |
| **增量检查**          | ❌ 缺失 | 每次都跑全量 build，不区分"改了哪些文件"                                   |
| **Node.js build** | ❌ 缺失 | 只有 `npm test`，没有 `npm run build`                           |
| **Python build**  | ❌ 缺失 | 只有 `pytest`，没有 `python -c "import xxx"` 验证导入               |
| **通用项目**          | ❌ 缺失 | 有 Makefile 但没定义命令                                          |

### 1.3 核心集成 Bug 详解

```
query.go L1338-1348:
  if !needsFollowUp {
    state.ReActBridge.MarkFinalAnswer(assistantBuffer.Content)  // ← 里面跑 gate.Run()
    if stopReason != "" {
      ch <- QueryOutput{Type: "terminal", ...}   // ← 不管 gate 成功失败，照样退出！
      return
    }
  }

react_bridge.go L226-232:
  if !result.Skipped && !result.OverallPass {
    b.lastGateFailure = result   // ← 存了失败结果
    return                       // ← 但调用方已经在走 terminal 了
  }
```

**问题本质**：MarkFinalAnswer 是 void 函数，调用方无法知道验证是否失败。验证失败时需要一个**返回值**或**状态标记**来阻止 terminal 发送。

***

## 2. 修复方案

### 2.1 Bug 修复：验证失败时强制继续

**方案**：让 `MarkFinalAnswer` 返回 bool 表示验证是否通过，query.go 根据返回值决定是否发送 terminal。

```go
// react_bridge.go
func (b *ReActBridge) MarkFinalAnswer(answer string) bool {
    // ...
    if gate != nil && cwd != "" {
        result := gate.Run(context.Background(), cwd)
        if !result.Skipped && !result.OverallPass {
            b.lastGateFailure = result
            return false  // 验证失败 → 不应结束
        }
    }
    b.trace.Complete(...)
    return true  // 验证通过 或 跳过 → 可以结束
}
```

```go
// query.go L1338-1348
if !needsFollowUp {
    shouldComplete := true
    if state.ReActBridge != nil && assistantBuffer != nil {
        shouldComplete = state.ReActBridge.MarkFinalAnswer(assistantBuffer.Content)
    }
    if shouldComplete {
        ch <- QueryOutput{Type: "terminal", ...}
        return
    }
    // 验证失败 → 注入失败消息，让主循环继续
    needsFollowUp = true
}
```

### 2.2 扩展 VerificationGate 覆盖

| 项目类型    | 当前                 | 增加                                         |
| ------- | ------------------ | ------------------------------------------ |
| Go      | build + vet + test | 不变                                         |
| Node.js | test               | + `npm run build`（如有 build script）         |
| Python  | pytest             | + `python -c "import main"` 验证导入           |
| Rust    | build + test       | 不变                                         |
| Java    | —                  | + `mvn compile test` 或 `gradle build test` |
| Generic | —                  | + `make test` / `make build`               |

### 2.3 增量检查（优化项）

当前每次都跑全量 build，对于大项目很慢。可以：

1. 记录上次验证通过时的文件 hash 摘要
2. 如果只有业务代码改动（没有改 go.mod/package.json 等构建文件），跳过全量 vet/test，只跑 build
3. 这个优化不影响正确性，只影响速度

***

## 3. 实施计划

| 步骤 | 文件                    | 说明                                                               |
| -- | --------------------- | ---------------------------------------------------------------- |
| 1  | react\_bridge.go      | MarkFinalAnswer 改为返回 bool；新增 HasUnresolvedGateFailure()          |
| 2  | query.go L1338-1358   | 根据 MarkFinalAnswer 返回值决定是否退出；验证失败时注入失败消息 + 强制 needsFollowUp=true |
| 3  | verification\_gate.go | 扩展 Node/Python/Java/Generic 的验证命令                                |
| 4  | query.go L1247 附近     | 空 response 重试后，也跑一次 gate（如果之前没跑过）                                |

***

## 4. 风险评估

| 风险                          | 影响                      | 缓解                                            |
| --------------------------- | ----------------------- | --------------------------------------------- |
| 验证门超时                       | 阻塞 queryLoop 30s        | 已有 timeout 机制，且超时后标记为失败让 LLM 修                |
| build 误判（项目还没写完，build 必然失败） | LLM 收到无效的失败提示           | 验证只在 MarkFinalAnswer 时触发（agent 说"完成"时），不是每轮都跑 |
| 死循环（验证一直失败，LLM 一直修）         | 最多 2 次自动修复，超过则 terminal | 用已有的 MaxOutputTokensRecoveryCount 类似机制        |
| Generic 项目误跑 make           | 没有 Makefile 时 skip      | detectProjectType 会检查文件是否存在                   |

