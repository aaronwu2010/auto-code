# 自动构建验证闭环设计文档

## 1. 状态总览

### 1.1 当前状态：**已实现，待增强**

auto-code 已经拥有完整的"编码 → 验证 → 修复 → 通过"闭环。核心组件全部就绪。

| 组件               | 状态    | 位置                                                                       |
| ---------------- | ----- | ------------------------------------------------------------------------ |
| VerificationGate | ✅ 已实现 | [verification\_gate.go](../internal/engine/query/verification_gate.go)   |
| ReActBridge 集成   | ✅ 已修复 | MarkFinalAnswer 返回 bool，query.go 根据返回值决定是否退出                             |
| 失败消息注入           | ✅ 已实现 | BuildGateFailureMessage() → `<verification-failure>` XML                 |
| 防重犯注入            | ✅ 已实现 | BuildPreCallContext() 把 lastGateFailure 注入下一轮                            |
| Prompt 引导（主动验证）  | ✅ 已实现 | [build\_verify.go](../internal/prompts/build_verify.go) — 各语言验证命令 + 实战经验 |
| **命令覆盖不全**       | ✅ 已修复 | PreCheck 机制 + 完善各语言命令链                                                   |

### 1.2 已修复的关键问题（对比上版设计）

| 问题                            | 修复前                         | 修复后                                                  |
| ----------------------------- | --------------------------- | ---------------------------------------------------- |
| 集成 Bug：MarkFinalAnswer 是 void | query.go 不管验证结果照样发 terminal | 返回 bool，query.go 根据结果决定是否退出                          |
| Go 缺少 gofmt                   | build + vet + test          | build + vet + gofmt -l + test                        |
| Java 只有 mvn compile           | 无法验证完整打包                    | mvn compile + mvn clean package -DskipTests          |
| Node 只有 npm test              | 不检查构建                       | npm run build + npm run lint + npm test（均带 PreCheck） |
| Generic 只有 make test          | 无默认构建                       | make build + make + make test                        |
| 无 PreCheck 机制                 | 跑不存在的命令会失败误报                | PreCheck 函数检测脚本/文件是否存在，不存在则跳过                        |

***

## 2. 架构设计

### 2.1 双轨验证架构

```
用户: "写一个 todo CRUD API"
    ↓
[Prompt 软引导] BuildVerifySection 告诉 LLM：
    "写完 Go 代码后主动跑 go build + go vet + gofmt -l"
    ↓
LLM 写完代码 → 主动跑 go build ./... → go vet ./...
    ↓
如果主动验证通过 → LLM 自信地说"完成了"
    ↓
如果主动验证失败 → LLM 在同轮里修复 → 再验证 → 通过 → 说"完成了"
    ↓
[代码硬强制] VerificationGate.Run() 在 MarkFinalAnswer 时触发
    → 跑完整验证链（build + vet + gofmt + test）
    → 通过 → 正常退出 ✅
    → 失败 → 注入 <verification-failure> → 强制继续 → LLM 修复 → 再验证
    ↓
最多 N 轮自动修复（由 queryLoop 的 max_turns 控制）
```

### 2.2 VerificationGate 命令链详解

各项目类型的验证命令**按依赖顺序排列**，build 失败会短路后续命令：

#### Go 项目（go.mod）

| 顺序 | 命令               | 超时  | 说明                       |
| -- | ---------------- | --- | ------------------------ |
| 1  | `go build ./...` | 60s | 编译检查，必须先过                |
| 2  | `go vet ./...`   | 30s | 静态分析（nil 解引用、格式串错、未使用变量） |
| 3  | `gofmt -l .`     | 15s | 格式检查（空输出=合规）             |
| 4  | `go test ./...`  | 60s | 单元测试                     |

#### Java 项目（pom.xml / build.gradle）

| 顺序 | 命令                              | PreCheck        | 说明          |
| -- | ------------------------------- | --------------- | ----------- |
| 1  | `mvn compile -q`                | pom.xml 存在      | Maven 编译    |
| 2  | `mvn clean package -DskipTests` | pom.xml 存在      | Maven 完整打包  |
| 3  | `gradle compileJava`            | build.gradle 存在 | Gradle 编译   |
| 4  | `gradle build -x test`          | build.gradle 存在 | Gradle 完整打包 |

#### Node.js 项目（package.json）

| 顺序 | 命令              | PreCheck                    | 说明                   |
| -- | --------------- | --------------------------- | -------------------- |
| 1  | `npm run build` | package.json 有 build script | TypeScript 编译 / 前端构建 |
| 2  | `npm run lint`  | package.json 有 lint script  | ESLint / TSLint      |
| 3  | `npm test`      | package.json 有 test script  | 单元测试                 |

#### Python 项目（pyproject.toml / requirements.txt / setup.py）

| 顺序 | 命令                         | PreCheck            | 说明                 |
| -- | -------------------------- | ------------------- | ------------------ |
| 1  | `python -c "import check"` | 始终跑                 | 验证能导入（语法正确 + 依赖齐全） |
| 2  | `pytest -x -q`             | test/ 或 tests/ 目录存在 | 单元测试               |

#### Rust 项目（Cargo.toml）

| 顺序 | 命令                    | 说明   |
| -- | --------------------- | ---- |
| 1  | `cargo build --quiet` | 编译   |
| 2  | `cargo test --quiet`  | 单元测试 |

#### Generic 项目（Makefile）

| 顺序 | 命令           | PreCheck    | 说明   |
| -- | ------------ | ----------- | ---- |
| 1  | `make build` | Makefile 存在 | 显式构建 |
| 2  | `make`       | Makefile 存在 | 默认目标 |
| 3  | `make test`  | Makefile 存在 | 测试   |

### 2.3 PreCheck 机制

`VerificationCommand.PreCheck` 是一个可选函数，接收 cwd 返回 bool：

- 返回 `true` → 正常执行命令

- 返回 `false` → 跳过（不加入 checks 列表，不视为失败）

用途：

- Node.js：package.json 里没有 build script → 跳过 `npm run build`

- Python：没有 tests/ 目录 → 跳过 pytest

- Java：只有 pom.xml 没有 build.gradle → 只跑 Maven 命令

### 2.4 验证失败注入格式

```xml
<verification-failure>
<!-- 验证门未通过，请在最终回答前修复 -->
<!-- 项目类型: Go -->
<!-- 总耗时: 1500ms -->

[Verification Results]
  [PASS] go build (320ms)
  [FAIL] go vet (210ms)
    Error:
      pkg/engine/query/query.go:150:2: Printf format %d has arg wrong type
      pkg/engine/query/query.go:175:3: undefined: FooBar
</verification-failure>
```

LLM 在下一轮的 system prompt 里会看到这段 XML，知道哪个命令失败、什么错误。

***

## 3. Prompt 层面的经验传授

[build\_verify.go](../internal/prompts/build_verify.go) 把编码验证经验提炼成系统提示词，包含：

### 3.1 各语言/框架的完整验证命令链

Go（build → vet → gofmt → test）、Java（compile → clean package -DskipTests）、Node.js（build → lint → test）、Python（import check → pytest）、Rust（build → clippy → fmt）、Makefile（build → make → test）

### 3.2 编码过程中的验证时机

- 改了 import / 函数签名后 → 立刻跑编译

- 写完一个模块后 → 跑 build + vet

- 准备说"完成"之前 → 跑完整链

### 3.3 错误处理流程

读错误信息 → Grep 定位 → Edit 修复 → 重新验证

### 3.4 实战 checklist

7 个自检问题，每次完成后问自己。

***

## 4. 注入链路

### 4.1 代码层面（硬强制）

```
queryLoop 主循环
    ↓
LLM 输出最终回答（无 tool_calls）
    ↓
ReActBridge.MarkFinalAnswer(answer)
    ↓
VerificationGate.Run(ctx, projectDir)
    ↓
detectProjectType(cwd) → Go/Node/Python/Rust/Java/Generic
    ↓
getVerificationCommands(type) → 命令列表（含 PreCheck）
    ↓
依次执行（build 失败短路后续）
    ↓
整体结果：
    通过 → return true → query.go 发 terminal 退出
    失败 → return false → lastGateFailure = result → 注入失败消息 → 强制下一轮
```

### 4.2 Prompt 层面（软引导）

```
BuildSystemPrompt()
    ↓
注入顺序：
1. SimpleIntroSection
2. ToolUsageSection
3. SmartAgentSection          ← 意图理解 + 完整交付
4. DebugMethodologySection     ← 根因追踪方法论
5. BuildVerifySection          ← 编码后自验证（本方案）
6. GetSystemSection
7. GetDoingTasksSection
8. GetActionsSection
```

***

## 5. 未来增强方向（暂未实现）

| 方向                               | 说明                                          | 优先级 |
| -------------------------------- | ------------------------------------------- | --- |
| **增量验证**                         | 记录上次通过时的文件 hash，只有业务代码改动时跳过全量 vet/test      | 中   |
| **更智能的 Python import 检查**        | 当前命令是通用的，可以尝试自动发现可导入的模块                     | 低   |
| **npm run build 前置 npm install** | 如果 node\_modules 不存在，先跑 npm install         | 中   |
| **C/C++ 项目支持**                   | 检测 CMakeLists.txt / Makefile，跑 cmake + make | 低   |
| **验证失败后最多 N 轮自动修复**              | 避免死循环（当前依赖 max\_turns 兜底）                   | 中   |

***

## 6. 风险与缓解

| 风险                      | 影响                        | 缓解                                      |
| ----------------------- | ------------------------- | --------------------------------------- |
| 验证门超时（全局 30s / 单命令 60s） | 阻塞 queryLoop 最多 30s       | 超时后标记为失败让 LLM 修，不卡死                     |
| build 误判（项目还没写完，必然失败）   | LLM 收到无效失败提示              | 验证只在 MarkFinalAnswer 时触发（agent 说"完成"时）  |
| 死循环（验证一直失败）             | 依赖 max\_turns 兜底          | queryLoop 有 max\_turns 限制，超过后发 terminal |
| Generic 项目误跑 make       | 没有 Makefile 时 PreCheck 跳过 | detectProjectType 检查文件存在                |
| npm/yarn/pnpm 差异        | 命令名不同                     | 当前统一用 npm，yarn/pnpm 项目可能需要适配            |

