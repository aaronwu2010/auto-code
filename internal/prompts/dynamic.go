package prompts

import (
	"fmt"
	"strings"
)

// TaskType 任务类型（与 query 包中保持一致）
type TaskType string

const (
	DynTaskDebug       TaskType = "debug"
	DynTaskFeature     TaskType = "feature"
	DynTaskRefactor    TaskType = "refactor"
	DynTaskExplain     TaskType = "explain"
	DynTaskBuild       TaskType = "build"
	DynTaskPerformance TaskType = "performance"
	DynTaskUnknown     TaskType = "unknown"
)

// ProjectLang 项目语言
type ProjectLang string

const (
	LangGo         ProjectLang = "go"
	LangPython     ProjectLang = "python"
	LangTypeScript ProjectLang = "typescript"
	LangJavaScript ProjectLang = "javascript"
	LangJava       ProjectLang = "java"
	LangRust       ProjectLang = "rust"
	LangUnknown    ProjectLang = "unknown"
)

// DetectLangFromExt 从文件扩展名推断语言
func DetectLangFromExt(ext string) ProjectLang {
	switch strings.ToLower(ext) {
	case ".go":
		return LangGo
	case ".py":
		return LangPython
	case ".ts", ".tsx":
		return LangTypeScript
	case ".js", ".jsx":
		return LangJavaScript
	case ".java":
		return LangJava
	case ".rs":
		return LangRust
	default:
		return LangUnknown
	}
}

// DynamicPromptEngine 动态任务适配 Prompt（方案 2）
//
// 核心思想：不是用同一份静态 prompt 面对所有任务，而是根据
// 任务类型 + 项目语言 动态拼接针对性的行为指令。
//
// 注入方式：IsMeta=true 的 user message（不污染 system prompt，保持 system prompt 稳定短）
type DynamicPromptEngine struct {
	enabled bool
}

// NewDynamicPromptEngine 创建 DynamicPromptEngine
func NewDynamicPromptEngine(enabled bool) *DynamicPromptEngine {
	return &DynamicPromptEngine{enabled: enabled}
}

// BuildTaskInstruction 根据任务类型构建行为指令
func (d *DynamicPromptEngine) BuildTaskInstruction(taskType TaskType, lang ProjectLang) string {
	if d == nil || !d.enabled {
		return ""
	}

	if taskType == DynTaskUnknown {
		return ""
	}

	var sb strings.Builder

	// 任务类型特化指令
	sb.WriteString(fmt.Sprintf("[Task Guidance] 检测到当前任务类型: %s\n\n", taskType))

	switch taskType {
	case DynTaskDebug:
		sb.WriteString(`你正在处理一个 **调试/排错** 任务。请严格遵循以下流程：

1. **先定位再动手**：优先用 Grep 搜 error/fail/panic/timeout 等关键词，或直接 Read 报错堆栈指向的文件
2. **复现根因**：用 Bash 跑 build/test，确认能否稳定复现问题
3. **最小修复**：只改导致 bug 的那几行，不做额外"优化"
4. **验证修复**：修复后必须重新跑 build/test，确认问题解决且没有引入新问题
5. **不要假设**：如果不确定根因，用 Grep/Read 收集更多信息，不要猜

特别注意：
- 不要一次性改一大堆文件
- 不要做"顺便"的重构
- 修复完成后简要说明根因和修复点`)

	case DynTaskFeature:
		sb.WriteString(`你正在实现一个 **新功能**。请遵循以下流程：

1. **理解现有代码**：先用 Grep/Glob 找到相关接口和数据结构，再 Read 关键文件理解设计
2. **规划改动范围**：列出来要改哪些文件，分别改什么
3. **风格一致**：保持与现有代码相同的命名风格、错误处理模式、导入组织
4. **小步快跑**：先写骨架，再填实现，每步都跑 build 验证
5. **边界处理**：考虑空输入、错误返回、类型断言失败等边界情况`)

	case DynTaskRefactor:
		sb.WriteString(`你正在进行 **重构**。重构的核心原则：

1. **行为不变**：重构前后的外部行为必须完全一致，测试应该仍然通过
2. **小步提交**：每次只做一件事（重命名、提取函数、简化逻辑），每步都跑测试
3. **建立基线**：重构前先跑一遍测试，确保有基线可以对比
4. **不要"顺便"加功能**：重构就是重构，不要在重构中加入新的业务逻辑
5. **提取 > 内联**：如果发现重复逻辑，提取出来但不要过度抽象`)

	case DynTaskExplain:
		sb.WriteString(`你正在 **解释/分析** 代码。请遵循以下方法：

1. **先看整体再看局部**：先用 Glob 看目录结构，再 Read 关键入口文件
2. **从调用链入手**：找到入口函数 → 顺着调用链 → 找到核心逻辑
3. **用代码说话**：引用具体的函数名、文件名、行号来辅助说明
4. **关联上下文**：提到的每个组件都要说明它和其他组件的关系
5. **不臆测**：没看过的代码不要假设它的行为`)

	case DynTaskBuild:
		sb.WriteString(`你正在 **构建/运行/安装**。请遵循以下流程：

1. **检查依赖**：先确认项目的构建系统（go.mod, package.json, requirements.txt 等）
2. **运行构建**：执行对应的构建命令（go build, npm run build, make 等）
3. **处理编译错误**：
   - 类型错误 → 检查导入路径和函数签名
   - 缺少依赖 → go mod tidy / npm install
   - 链接错误 → 检查 CGO / 外部库
4. **跑测试**：构建通过后必须跑测试，不能跳过
5. **记录环境信息**：如果构建失败，记录 Go/Node/Python 版本等`)

	case DynTaskPerformance:
		sb.WriteString(`你正在处理 **性能优化**。请遵循以下流程：

1. **先 profiling 找瓶颈**：不要盲目优化，先用内置工具找热点
   - Go: go test -bench ./... -benchmem
   - Python: python -m cProfile / py-spy
   - Node: node --inspect 或 clinic
2. **关注常见模式**：
   - N+1 查询（循环中 IO / 循环中 API 调用）
   - 资源未释放（file/connection/goroutine leak）
   - 锁竞争（mutex 粒度太大 / 长时间持锁）
   - 频繁内存分配（循环中 new/make）
3. **量化对比**：优化前后必须 benchmark 对比，用数据说话
4. **不要过早优化**：不要为了"看起来快"写复杂的代码，保持可读性
5. **最小改动原则**：只改确实慢的地方`)
	}

	sb.WriteString("\n\n")

	// 项目语言特化指令
	if lang != LangUnknown {
		langSection := d.buildLangInstruction(lang)
		if langSection != "" {
			sb.WriteString("---\n\n")
			sb.WriteString(langSection)
			sb.WriteString("\n")
		}
	}

	// 代码生成完整性 checklist
	// feature/build 任务类型 → 需要确保项目可编译运行 + README
	// explain 任务类型 → 设计方案应该输出到 design.md
	if taskType == DynTaskFeature || taskType == DynTaskBuild {
		if checklist := d.buildCompletenessChecklist(lang, true); checklist != "" {
			sb.WriteString("\n---\n\n")
			sb.WriteString(checklist)
			sb.WriteString("\n")
		}
	} else if taskType == DynTaskExplain {
		// 设计方案类 explain → 建议输出到 design.md
		sb.WriteString("\n---\n\n")
		sb.WriteString(`[Design Output] 如果你正在输出设计方案或架构分析:
- 将完整设计内容写入 design.md 文件
- 包含: 背景/目标、架构图（文字描述即可）、模块划分、数据流、关键接口、实施步骤
- 如果用户只是问一个简单问题（不需要完整设计文档），忽略这条`)
		sb.WriteString("\n")
	}

	return sb.String()
}

// buildLangInstruction 构建语言特化指令
func (d *DynamicPromptEngine) buildLangInstruction(lang ProjectLang) string {
	switch lang {
	case LangGo:
		return `[Language: Go] Go 项目注意事项:
- 错误处理: 始终检查 err != nil，不要忽略 error 返回值
- 清理: 用 defer 确保 Close/Unlock 的正确执行
- 并发: goroutine + channel 用于通信，mutex 用于保护共享状态
- 接口: 小接口优先（只暴露需要的方法），用 interface{} 而非 any 在导出 API
- 构建: 修改后跑 go build ./... 和 go vet ./...
- 测试: 跑 go test ./...`

	case LangPython:
		return `[Language: Python] Python 项目注意事项:
- 类型: 用 type hints (PEP 484)，dataclass 替代普通类
- 错误: try/except 要尽量具体，不要 bare except
- 资源: with 语句管理文件/连接，上下文管理器
- 异步: asyncio.gather 并发，注意事件循环
- 格式化: 用 f-string，不要用 % 或 .format
- 测试: pytest，修改后跑 pytest -v`

	case LangTypeScript, LangJavaScript:
		return `[Language: JS/TS] TypeScript/JavaScript 项目注意事项:
- 类型: TypeScript 项目中尽量用 strict 模式，避免 any
- 异步: Promise.all 并发，async/await 清晰
- 资源: 注意 EventListener 的清理，避免内存泄漏
- 模块化: 用 ES module (import/export)
- 构建: npm run build / tsc --noEmit 检查类型
- 测试: npm test / vitest / jest`

	case LangJava:
		return `[Language: Java] Java 项目注意事项:
- 异常: try-with-resources 自动关闭资源，异常要具体不要吞掉
- 并发: ExecutorService / CompletableFuture，synchronized / ReentrantLock
- 集合: 选择合适的数据结构，注意 hashCode/equals 契约
- Optional: 用 Optional 替代 null 检查
- 构建: mvn compile / mvn test 或 gradle build`

	case LangRust:
		return `[Language: Rust] Rust 项目注意事项:
- 所有权: 理解 ownership/borrowing/lifetime，不要滥用 clone
- 错误: Result<T, E> + ? 操作符，不要 unwrap 除非确定
- 并发: Arc<Mutex<T>> / std::sync::mpsc，注意 Send/Sync
- 安全: 不要轻易用 unsafe
- 构建: cargo build / cargo test / cargo clippy`
	}

	return ""
}

// buildCompletenessChecklist 构建代码生成完整性 checklist
// 确保 feature/build 任务生成的代码"开箱即编译运行"
//
// 核心理念：写完代码不等于完成——要检查"这个项目还缺什么才能独立编译运行"
// includeDocs=true 时追加 README.md 文档建议
func (d *DynamicPromptEngine) buildCompletenessChecklist(lang ProjectLang, includeDocs bool) string {
	// 构建配置（必须）+ README（建议）按语言组合
	var checklist string

	switch lang {
	case LangGo:
		checklist = `[Completeness Checklist] Go 项目可编译检查清单:

创建新项目时，必须确保以下文件齐全:
- ☑️ go.mod: 模块声明 + 依赖管理（go mod tidy 自动同步）
- ☑️ main.go: package main + func main() 入口函数
- ☑️ 正确的 package 声明（每个 .go 文件首行）
- ☑️ import 路径正确（标准库 / 第三方 / 项目内部）
- ☑️ 如果用了第三方包 → go mod tidy 后再构建

验证命令: go build ./... && go vet ./... && go test ./...`

	case LangPython:
		checklist = `[Completeness Checklist] Python 项目可运行检查清单:

创建新项目时，必须确保以下文件齐全:
- ☑️ requirements.txt 或 pyproject.toml: 依赖声明
- ☑️ main.py 或 app.py: 入口（if __name__ == "__main__"）
- ☑️ 正确的 import 语句（from xxx import yyy / import xxx）
- ☑️ __init__.py: 包目录下必须有

验证命令: pip install -r requirements.txt && python main.py`

	case LangJava:
		checklist = `[Completeness Checklist] Java 项目可编译检查清单:

创建新项目时，必须确保以下文件齐全:
- ☑️ pom.xml (Maven) 或 build.gradle (Gradle): 构建配置 + 依赖
- ☑️ 正确的 package 声明（必须与目录结构匹配）
- ☑️ public static void main(String[] args) 入口
- ☑️ 正确的 import 语句

验证命令: mvn compile && mvn test`

	case LangTypeScript, LangJavaScript:
		checklist = `[Completeness Checklist] JS/TS 项目可运行检查清单:

创建新项目时，必须确保以下文件齐全:
- ☑️ package.json: 依赖 + scripts（build, test, start）
- ☑️ tsconfig.json（TS 项目）: 编译配置
- ☑️ 入口文件 index.ts / app.ts
- ☑️ ES module import/export 正确

验证命令: npm install && npm run build && npm test`

	case LangRust:
		checklist = `[Completeness Checklist] Rust 项目可编译检查清单:

创建新项目时，必须确保以下文件齐全:
- ☑️ Cargo.toml: 包声明 + 依赖 + features
- ☑️ src/main.rs 或 src/lib.rs: 入口
- ☑️ 正确的 use 语句导入
- ☑️ mod 声明模块可见性

验证命令: cargo build && cargo test && cargo clippy`

	default:
		checklist = `[Completeness Checklist] 新项目完整性检查清单:

写完代码后，请主动检查:
1. 是否有正确的构建/依赖配置文件（Makefile / build.gradle / package.json / requirements.txt 等）
2. 是否有正确的入口文件
3. 所有 import/include 是否正确
4. 用对应的构建命令验证能否成功编译运行`
	}

	// 追加 README 文档建议
	if includeDocs {
		checklist += `

文档建议（强烈推荐但非强制）:
- 📄 README.md: 项目说明 + 运行方法 + 目录结构 + 使用示例
  包含: 项目概述、安装步骤、快速开始、核心功能、目录结构说明`
	}

	return checklist
}
