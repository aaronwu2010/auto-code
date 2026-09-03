package prompts

import (
	"strings"

	"github.com/auto-code/auto-code/internal/types"
)

// SceneType 任务场景类型
type SceneType string

const (
	SceneRESTAPI       SceneType = "rest_api"
	SceneGraphQL       SceneType = "graphql"
	SceneGRPC          SceneType = "grpc"
	SceneGitCommit     SceneType = "git_commit"
	SceneCLI           SceneType = "cli_tool"
	SceneCrossPlatform SceneType = "cross_platform"
	SceneWebFrontend   SceneType = "web_frontend"
	SceneConfigFile    SceneType = "config_file"
	SceneDatabase      SceneType = "database"
	SceneTesting       SceneType = "testing"
	ScenePerformance   SceneType = "performance"
	SceneConcurrency   SceneType = "concurrency"
)

// DetectScenes 从用户 prompt + 历史消息中检测任务场景
// 纯关键词匹配，零 LLM 开销
func DetectScenes(prompt string, messages []types.Message) []SceneType {
	allContent := buildContentFromMessages(prompt, messages)
	return detectScenesFromString(allContent)
}

// DetectScenesFromToolResults 从最近的工具执行结果中补充检测场景
// 用于 Turn N 动态更新——Read 了代码后可能发现新场景
func DetectScenesFromToolResults(recentToolResults []string) []SceneType {
	content := strings.Join(recentToolResults, "\n")
	return detectScenesFromString(content)
}

func detectScenesFromString(content string) []SceneType {
	var scenes []SceneType
	has := func(keywords ...string) bool {
		for _, kw := range keywords {
			if strings.Contains(strings.ToLower(content), strings.ToLower(kw)) {
				return true
			}
		}
		return false
	}

	// REST API
	if has("rest", "api", "接口", "endpoint", "http 服务", "http server", "gin", "echo", "fiber", "mux router") {
		scenes = append(scenes, SceneRESTAPI)
	}

	// GraphQL
	if has("graphql", "gql") {
		scenes = append(scenes, SceneGraphQL)
	}

	// gRPC
	if has("grpc", "protobuf", ".proto") {
		scenes = append(scenes, SceneGRPC)
	}

	// Git Commit
	if has("git commit", "git push", "提交代码", "提交到 git", "commit message", "conventional commit") {
		scenes = append(scenes, SceneGitCommit)
	}

	// CLI 工具
	if has("命令行", "cli", "终端工具", "cobra", "urfave/cli", "flag", "subcommand") {
		scenes = append(scenes, SceneCLI)
	}

	// 跨平台
	if has("跨平台", "cross-platform", "windows + linux", "windows 和 linux", "darwin", "runtime.GOOS", "build tags") {
		scenes = append(scenes, SceneCrossPlatform)
	}

	// Web 前端
	if has("前端", "web 页面", "html", "css", "javascript", "react", "vue", "svelte", "dom", "browser") {
		scenes = append(scenes, SceneWebFrontend)
	}

	// 配置文件
	if has("配置文件", "config file", "yaml", "toml", "ini", ".env", "settings.json") {
		scenes = append(scenes, SceneConfigFile)
	}

	// 数据库
	if has("数据库", "database", "sql", "mysql", "postgresql", "sqlite", "mongodb", "redis", "gorm", "sqlx") {
		scenes = append(scenes, SceneDatabase)
	}

	// 测试
	if has("单元测试", "unit test", "integration test", "e2e", "jest", "pytest", "go test", "test case", "测试用例") {
		scenes = append(scenes, SceneTesting)
	}

	// 性能
	if has("性能优化", "performance", "benchmark", "profiling", "pprof", "latency", "throughput", "吞吐量") {
		scenes = append(scenes, ScenePerformance)
	}

	// 并发
	if has("并发", "concurrency", "goroutine", "channel", "mutex", "async", "await", "thread", "parallel") {
		scenes = append(scenes, SceneConcurrency)
	}

	return scenes
}

// sceneChecklistText 返回指定场景的 checklist 文本
func SceneChecklistText(scene SceneType) string {
	switch scene {
	case SceneRESTAPI:
		return `[Checklist: REST API 设计规范]

设计 REST API 时请遵循:
- 资源命名: 用复数名词 (/users, /orders, /users/{id}/orders)
- HTTP 方法: GET=查询, POST=创建, PUT=全量更新, PATCH=部分更新, DELETE=删除
- 状态码: 200=OK, 201=Created, 204=No Content, 400=Bad Request, 401=Unauthorized, 403=Forbidden, 404=Not Found, 409=Conflict, 500=Internal Error
- 错误格式: {"error": {"code": "NOT_FOUND", "message": "User not found"}}
- 版本化: /v1/users, /v2/users
- 分页: /users?page=1&size=20 或 /users?limit=20&offset=0
- 过滤/排序: /users?status=active&sort=created_at_desc`

	case SceneGraphQL:
		return `[Checklist: GraphQL 设计规范]

设计 GraphQL API 时请遵循:
- Schema-first: 先定义 schema.graphql，再实现 resolver
- 查询分层: Query 顶层只暴露顶级资源，嵌套资源用 GraphQL 自然嵌套
- Mutation 命名: camelCase（createUser, updateOrder）
- 错误处理: resolver 内不要 panic，返回 error + 自定义错误类型
- N+1 问题: 用 DataLoader 批量加载关联数据
- Schema 版本演进: 用 @deprecated 标记，不要直接删除字段`

	case SceneGRPC:
		return `[Checklist: gRPC 设计规范]

设计 gRPC API 时请遵循:
- proto3 语法: 不需要 required/optional（proto3 默认 optional）
- 命名: package 用 domain.v1，message CamelCase，service CamelCase，rpc PascalCase
- 错误: 用 google.rpc.Status 标准码（NOT_FOUND, ALREADY_EXISTS, INVALID_ARGUMENT）
- 流式: server streaming（大数据集）、client streaming（上传）、bidirectional（实时）
- 生成代码: protoc --go_out --go-grpc_out`

	case SceneGitCommit:
		return `[Checklist: Git Commit 规范]

执行 git commit / push 前:
- Conventional Commits: feat(scope): description / fix(scope): description / docs: / refactor: / chore:
- 提交前检查: git diff 查看改动 → 跑 build + test → 确认无临时调试代码
- 不要提交: 二进制文件、密钥（.env, *.pem）、IDE 配置、大的生成文件
- .gitignore 检查: 确认敏感文件在忽略列表里
- 分支策略: 开发分支 → PR → 主分支，不要直接 push main/master`

	case SceneCLI:
		return `[Checklist: CLI 工具设计]

编写命令行工具时请遵循:
- 框架选择: Go 用 cobra/urfave/cli, Python 用 click/typer, Node 用 commander
- 退出码: 0=成功, 1=通用错误, 2=用法错误, 64+ 特殊错误
- 信号处理: 捕获 SIGINT/SIGTERM 做优雅退出（关文件、取消 goroutine）
- --help 格式: 简短描述 + 用法示例 + 所有 flag 说明
- --version 输出程序版本号
- 错误输出到 stderr，数据输出到 stdout`

	case SceneCrossPlatform:
		return `[Checklist: 跨平台兼容性]

编写跨平台代码时注意:
- 路径: 用 filepath.Join() / filepath.Clean()，不要硬编码 / 或 \
- 换行符: 文件读写用 runtime.GOOS 判断是 \n 还是 \r\n
- 权限: os.Chmod 在 Windows 上部分支持，不要依赖
- 系统调用: Windows 不支持 fork()/signal()，用 os.StartProcess + os.Signal
- 构建标签: //go:build linux 或 //go:build windows
- Shell 差异: Windows 用 cmd/PowerShell，Linux 用 bash`

	case SceneWebFrontend:
		return `[Checklist: Web 前端开发]

编写 Web 前端时注意:
- HTML 语义化: <header><nav><main><section><footer><article>
- CSS: BEM 命名（.block__element--modifier）、避免 !important、用 flexbox/grid
- 响应式: media query 断点（320/768/1024/1440）、viewport meta
- 可访问性（a11y）: 图片有 alt、表单有 label、按钮有 aria-label、键盘可导航
- 性能: LCP < 2.5s、代码分割（import()）、图片懒加载、预连接关键域名`

	case SceneConfigFile:
		return `[Checklist: 配置文件设计]

设计配置文件时请遵循:
- 格式: YAML（常用）/ TOML（Go 友好）/ JSON（通用）
- 默认值: 所有字段有合理默认值
- 验证: schema 验证（jsonschema / 自定义校验函数）
- 环境变量覆盖: DB_PASSWORD 可以从 env 读
- 示例文件: config.example.yaml 展示所有字段
- 不要提交: config.yaml（实际密钥）→ 只提交 config.example.yaml
- 文档注释: 每个字段说明用途和取值范围`

	case SceneDatabase:
		return `[Checklist: 数据库操作]

编写数据库相关代码时注意:
- SQL 注入: 用 ? 占位符参数化查询，绝对不要 fmt.Sprintf 拼 SQL
- 错误处理: 每个 DB 操作检查 err，记录失败的 SQL 和参数
- 连接管理: 用完 Close() 或用 defer（sql.DB 会自动管理连接池）
- 事务: 多表操作用 tx.Rollback() 保护，不要部分提交
- 索引: 大表查询确保有索引（EXPLAIN 检查）
- N+1: 查询列表时避免循环中查关联表（JOIN / 批量查询）`

	case SceneTesting:
		return `[Checklist: 测试代码编写]

编写测试时注意:
- 覆盖: 核心逻辑 80%+，简单 getter/setter 可以不测
- 命名: TestFunctionName_scenario_expected（TestDivide_byZero_returnsError）
- table-driven tests: 多 case 用 []struct{input, expected}
- mock: 外部依赖用 mock/fake，不要连真实数据库/API
- 独立: 每个测试独立运行，不依赖执行顺序
- 边界: nil/empty/zero/negative/overflow 边界情况`

	case ScenePerformance:
		return `[Checklist: 性能敏感代码]

编写性能敏感代码时注意:
- 先 benchmark 再优化: go test -bench . -benchmem
- 热点分析: pprof（go tool pprof）找真正的瓶颈
- 内存: 避免循环中 new/make、预分配 slice 容量、sync.Pool 复用对象
- IO: 批量读写 > 循环单次读写、async/await 并发不阻塞
- 算法: 用标准库高效实现（sort.Slice / strings.Builder / sync.Map）
- 不要过早优化: 可读性 > 微优化，除非 benchmark 证明是瓶颈`

	case SceneConcurrency:
		return `[Checklist: 并发安全代码]

编写并发代码时注意:
- goroutine 泄漏: 用 context.Cancel() 或 WaitGroup 确保 goroutine 能退出
- 竞态条件: go test -race 跑一遍，检测数据竞争
- channel 关闭: 发送方关闭，接收方用 for range 消费
- mutex 粒度: 锁范围尽量小，避免长时间持锁
- 死锁: 不要反向嵌套加锁，channel 收发配对
- context 传递: 所有长操作都接受 context.Context 参数`
	}
	return ""
}

func buildContentFromMessages(prompt string, messages []types.Message) string {
	var parts []string
	if prompt != "" {
		parts = append(parts, prompt)
	}
	for _, m := range messages {
		if len(parts) > 20 {
			break // 限制长度
		}
		if m.Role == types.RoleUser || m.Role == types.RoleAssistant {
			parts = append(parts, m.Content)
		}
	}
	return strings.Join(parts, "\n")
}
