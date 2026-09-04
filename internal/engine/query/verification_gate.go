package query

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ProjectType 项目类型
type ProjectType int

const (
	ProjectTypeUnknown ProjectType = iota
	ProjectTypeGo
	ProjectTypeNode
	ProjectTypePython
	ProjectTypeRust
	ProjectTypeJava
	ProjectTypeGeneric // 有 Makefile / build 脚本

	// 扩展主流语言/框架
	ProjectTypeCpp      // C/C++ (CMakeLists.txt)
	ProjectTypeCSharp   // C# / .NET (.csproj / .sln)
	ProjectTypePHP      // PHP (composer.json)
	ProjectTypeRuby     // Ruby (Gemfile)
	ProjectTypeSwift    // Swift (Package.swift)
	ProjectTypeScala    // Scala (build.sbt)
	ProjectTypeElixir   // Elixir (mix.exs)
	ProjectTypeHaskell  // Haskell (package.yaml / *.cabal)
	ProjectTypeZig      // Zig (build.zig)
	ProjectTypeDart     // Dart / Flutter (pubspec.yaml)
)

func (p ProjectType) String() string {
	switch p {
	case ProjectTypeGo:
		return "Go"
	case ProjectTypeNode:
		return "Node.js"
	case ProjectTypePython:
		return "Python"
	case ProjectTypeRust:
		return "Rust"
	case ProjectTypeJava:
		return "Java"
	case ProjectTypeGeneric:
		return "Generic"
	case ProjectTypeCpp:
		return "C/C++"
	case ProjectTypeCSharp:
		return "C#/.NET"
	case ProjectTypePHP:
		return "PHP"
	case ProjectTypeRuby:
		return "Ruby"
	case ProjectTypeSwift:
		return "Swift"
	case ProjectTypeScala:
		return "Scala"
	case ProjectTypeElixir:
		return "Elixir"
	case ProjectTypeHaskell:
		return "Haskell"
	case ProjectTypeZig:
		return "Zig"
	case ProjectTypeDart:
		return "Dart/Flutter"
	default:
		return "Unknown"
	}
}

// VerificationGate 验证门：标记完成前强制跑 build/test/vet
type VerificationGate struct {
	timeout time.Duration
	enabled bool
	// 缓存检测结果（避免每次都跑文件系统）
	cachedType ProjectType
	cachedOnce sync.Once
}

// NewVerificationGate 创建验证门
func NewVerificationGate(enabled bool) *VerificationGate {
	return &VerificationGate{
		timeout: 30 * time.Second,
		enabled: enabled,
	}
}

// VerificationCommand 单个验证命令
type VerificationCommand struct {
	Name    string        // 显示名 "go build"
	Cmd     string        // 命令名
	Args    []string      // 参数
	Timeout time.Duration // 单独超时
	// PreCheck 可选：运行前的预检查。返回 false 则跳过这条命令（不视为失败）。
	// 用于检测项目是否有 build script、是否有可导入的模块等。
	PreCheck func(cwd string) bool
}

// GateCheck 单个命令的结果
type GateCheck struct {
	Name     string
	Passed   bool
	Duration time.Duration
	Output   string // 截断后的输出
	Error    string // 如果失败，完整 stderr
}

// GateResult 整体验证结果
type GateResult struct {
	OverallPass        bool
	ProjectType        ProjectType
	Checks             []GateCheck
	FirstFailureName   string
	FirstFailureOutput string
	TotalDuration      time.Duration
	Skipped            bool
	SkipReason         string
}

// Run 执行验证，返回结果。
func (g *VerificationGate) Run(ctx context.Context, cwd string) *GateResult {
	start := time.Now()
	result := &GateResult{}

	if !g.enabled {
		result.Skipped = true
		result.SkipReason = "verification gate disabled"
		return result
	}

	if cwd == "" {
		result.Skipped = true
		result.SkipReason = "no project directory"
		return result
	}

	// 1. 检测项目类型
	projType := g.detectProjectType(cwd)
	result.ProjectType = projType

	// 2. 拿到验证命令
	commands := g.getVerificationCommands(projType)
	if len(commands) == 0 {
		result.Skipped = true
		result.SkipReason = fmt.Sprintf("no verification commands for %s project", projType)
		return result
	}

	// 3. 依次执行（串行，避免并发 build 冲突）
	gateCtx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	var firstFailure *GateCheck

	for _, cmd := range commands {
		select {
		case <-gateCtx.Done():
			cr := GateCheck{
				Name:     cmd.Name,
				Passed:   false,
				Duration: g.timeout,
				Error:    "verification gate global timeout",
			}
			result.Checks = append(result.Checks, cr)
			if firstFailure == nil {
				firstFailure = &cr
			}
			break
		default:
		}

		// PreCheck：如果定义了且返回 false，跳过这条命令（不加入 checks 也不视为失败）
		if cmd.PreCheck != nil && !cmd.PreCheck(cwd) {
			log.Printf("[VerificationGate] skipping %s: pre-check failed", cmd.Name)
			continue
		}

		cr := g.runCommand(gateCtx, cwd, cmd)
		result.Checks = append(result.Checks, cr)

		if !cr.Passed && firstFailure == nil {
			firstFailure = &cr
		}

		// build 失败就短路（vet/test 依赖 build 产物）
		if !cr.Passed && strings.Contains(strings.ToLower(cmd.Name), "build") {
			break
		}
	}

	// 4. 汇总
	result.TotalDuration = time.Since(start)
	if firstFailure == nil {
		result.OverallPass = true
	} else {
		result.OverallPass = false
		result.FirstFailureName = firstFailure.Name
		result.FirstFailureOutput = firstFailure.Error
		if result.FirstFailureOutput == "" {
			result.FirstFailureOutput = firstFailure.Output
		}
	}

	log.Printf("[VerificationGate] %s project: pass=%v, %d checks, %.0fms, first_fail=%s",
		projType, result.OverallPass, len(result.Checks), float64(result.TotalDuration)/float64(time.Millisecond), result.FirstFailureName)

	return result
}

// runCommand 执行单个命令
func (g *VerificationGate) runCommand(ctx context.Context, cwd string, cmd VerificationCommand) GateCheck {
	cr := GateCheck{Name: cmd.Name}
	start := time.Now()

	cmdTimeout := cmd.Timeout
	if cmdTimeout <= 0 {
		cmdTimeout = 15 * time.Second
	}

	execCtx, cancel := context.WithTimeout(ctx, cmdTimeout)
	defer cancel()

	execCmd := exec.CommandContext(execCtx, cmd.Cmd, cmd.Args...)
	execCmd.Dir = cwd
	var outBuf, errBuf strings.Builder
	execCmd.Stdout = &outBuf
	execCmd.Stderr = &errBuf

	err := execCmd.Run()
	cr.Duration = time.Since(start)

	stdout := outBuf.String()
	stderr := errBuf.String()

	// 截断输出
	maxOut := 2000
	if len(stdout) > maxOut {
		stdout = stdout[:maxOut] + "\n... [truncated]"
	}
	if len(stderr) > maxOut {
		stderr = stderr[:maxOut] + "\n... [truncated]"
	}
	cr.Output = stdout

	if err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			cr.Passed = false
			cr.Error = fmt.Sprintf("timeout after %v\n%s", cmdTimeout, stderr)
		} else {
			cr.Passed = false
			cr.Error = fmt.Sprintf("%s\n%s", err.Error(), stderr)
		}
	} else {
		cr.Passed = true
	}

	return cr
}

// detectProjectType 自动检测项目类型
func (g *VerificationGate) detectProjectType(cwd string) ProjectType {
	g.cachedOnce.Do(func() {
		// 优先级顺序：先检测专用构建文件，再 fallback 到 Makefile
		checks := []struct {
			file string
			pt   ProjectType
		}{
			// 原有 6 种
			{"go.mod", ProjectTypeGo},
			{"Cargo.toml", ProjectTypeRust},
			{"pom.xml", ProjectTypeJava},
			{"build.gradle", ProjectTypeJava},
			{"build.gradle.kts", ProjectTypeJava},
			{"package.json", ProjectTypeNode},
			{"pyproject.toml", ProjectTypePython},
			{"requirements.txt", ProjectTypePython},
			{"setup.py", ProjectTypePython},
			// 新增 10 种
			{"CMakeLists.txt", ProjectTypeCpp},
			{"composer.json", ProjectTypePHP},
			{"Gemfile", ProjectTypeRuby},
			{"Package.swift", ProjectTypeSwift},
			{"build.sbt", ProjectTypeScala},
			{"mix.exs", ProjectTypeElixir},
			{"package.yaml", ProjectTypeHaskell},
			{"build.zig", ProjectTypeZig},
			{"pubspec.yaml", ProjectTypeDart},
			// C# 特殊：检查 .csproj 或 .sln（通配符检测）
		}
		for _, c := range checks {
			if _, err := os.Stat(filepath.Join(cwd, c.file)); err == nil {
				g.cachedType = c.pt
				return
			}
		}
		// C# 检测：Glob 找 .csproj 或 .sln
		if matches, _ := filepath.Glob(filepath.Join(cwd, "*.csproj")); len(matches) > 0 {
			g.cachedType = ProjectTypeCSharp
			return
		}
		if matches, _ := filepath.Glob(filepath.Join(cwd, "*.sln")); len(matches) > 0 {
			g.cachedType = ProjectTypeCSharp
			return
		}
		// Haskell 备选：*.cabal 文件
		if matches, _ := filepath.Glob(filepath.Join(cwd, "*.cabal")); len(matches) > 0 {
			g.cachedType = ProjectTypeHaskell
			return
		}
		// Generic fallback：Makefile
		if _, err := os.Stat(filepath.Join(cwd, "Makefile")); err == nil {
			g.cachedType = ProjectTypeGeneric
			return
		}
		g.cachedType = ProjectTypeUnknown
	})
	return g.cachedType
}

// getVerificationCommands 根据项目类型返回验证命令列表。
// 命令按依赖顺序排列：build/compile 在前，vet/lint/test 在后。
// build 失败会短路后续命令（见 Run() 里的 break 逻辑）。
//
// 覆盖 16 种语言/框架：Go, Node.js, Python, Rust, Java, C/C++, C#/.NET,
// PHP, Ruby, Swift, Scala, Elixir, Haskell, Zig, Dart/Flutter, Generic(Makefile)
func (g *VerificationGate) getVerificationCommands(pt ProjectType) []VerificationCommand {
	switch pt {
	// ========== 原有 6 种（增强）==========

	case ProjectTypeGo:
		return []VerificationCommand{
			{Name: "go build", Cmd: "go", Args: []string{"build", "./..."}, Timeout: 60 * time.Second},
			{Name: "go vet", Cmd: "go", Args: []string{"vet", "./..."}, Timeout: 30 * time.Second},
			{Name: "gofmt check", Cmd: "gofmt", Args: []string{"-l", "."}, Timeout: 15 * time.Second},
			{Name: "go test", Cmd: "go", Args: []string{"test", "./..."}, Timeout: 60 * time.Second},
		}

	case ProjectTypeNode:
		return []VerificationCommand{
			{Name: "npm run build", Cmd: "npm", Args: []string{"run", "build"}, Timeout: 60 * time.Second,
				PreCheck: func(cwd string) bool { return pkgHasScript(cwd, "build") }},
			// TypeScript 类型检查（独立于 build，更严格）
			{Name: "tsc --noEmit", Cmd: "npx", Args: []string{"tsc", "--noEmit"}, Timeout: 30 * time.Second,
				PreCheck: func(cwd string) bool { return hasFile(cwd, "tsconfig.json") }},
			{Name: "npm run lint", Cmd: "npm", Args: []string{"run", "lint"}, Timeout: 30 * time.Second,
				PreCheck: func(cwd string) bool { return pkgHasScript(cwd, "lint") }},
			{Name: "npm test", Cmd: "npm", Args: []string{"test"}, Timeout: 60 * time.Second,
				PreCheck: func(cwd string) bool { return pkgHasScript(cwd, "test") }},
		}

	case ProjectTypePython:
		return []VerificationCommand{
			// 1. 语法检查：python -m compileall（不依赖 import 成功）
			{Name: "python compileall", Cmd: "python", Args: []string{"-m", "compileall", "-q", "."}, Timeout: 30 * time.Second},
			// 2. ruff（现代 Python lint，比 flake8 快且覆盖广）
			{Name: "ruff check", Cmd: "ruff", Args: []string{"check", "."}, Timeout: 30 * time.Second,
				PreCheck: func(cwd string) bool { return hasFile(cwd, "pyproject.toml") || hasFile(cwd, "ruff.toml") }},
			// 3. mypy 类型检查（如果用了类型注解）
			{Name: "mypy", Cmd: "mypy", Args: []string{"."}, Timeout: 60 * time.Second,
				PreCheck: func(cwd string) bool { return hasFile(cwd, "mypy.ini") || hasFile(cwd, "pyproject.toml") }},
			// 4. pytest 单元测试
			{Name: "pytest", Cmd: "pytest", Args: []string{"-x", "-q"}, Timeout: 60 * time.Second,
				PreCheck: func(cwd string) bool { return hasFile(cwd, "test") || hasFile(cwd, "tests") }},
		}

	case ProjectTypeRust:
		return []VerificationCommand{
			// 1. cargo check（只检查不生成二进制，比 build 快）
			{Name: "cargo check", Cmd: "cargo", Args: []string{"check", "--quiet"}, Timeout: 120 * time.Second},
			// 2. cargo build（完整编译，验证产物）
			{Name: "cargo build", Cmd: "cargo", Args: []string{"build", "--quiet"}, Timeout: 120 * time.Second},
			// 3. clippy（Rust 的 vet，警告要修）
			{Name: "cargo clippy", Cmd: "cargo", Args: []string{"clippy", "--quiet"}, Timeout: 60 * time.Second,
				PreCheck: func(cwd string) bool { return hasFile(cwd, "Cargo.toml") }},
			// 4. fmt 格式检查
			{Name: "cargo fmt --check", Cmd: "cargo", Args: []string{"fmt", "--", "--check"}, Timeout: 30 * time.Second},
			// 5. 单元测试
			{Name: "cargo test", Cmd: "cargo", Args: []string{"test", "--quiet"}, Timeout: 120 * time.Second},
		}

	case ProjectTypeJava:
		return []VerificationCommand{
			// Maven 项目
			{Name: "mvn compile", Cmd: "mvn", Args: []string{"compile", "-q"}, Timeout: 120 * time.Second,
				PreCheck: func(cwd string) bool { return hasFile(cwd, "pom.xml") }},
			{Name: "mvn clean package -DskipTests", Cmd: "mvn", Args: []string{"clean", "package", "-DskipTests"}, Timeout: 180 * time.Second,
				PreCheck: func(cwd string) bool { return hasFile(cwd, "pom.xml") }},
			// Gradle 项目（Kotlin DSL 和 Groovy DSL）
			{Name: "gradle compileJava", Cmd: "gradle", Args: []string{"compileJava"}, Timeout: 120 * time.Second,
				PreCheck: func(cwd string) bool { return hasFile(cwd, "build.gradle") || hasFile(cwd, "build.gradle.kts") }},
			{Name: "gradle build -x test", Cmd: "gradle", Args: []string{"build", "-x", "test"}, Timeout: 180 * time.Second,
				PreCheck: func(cwd string) bool { return hasFile(cwd, "build.gradle") || hasFile(cwd, "build.gradle.kts") }},
		}

	case ProjectTypeGeneric:
		return []VerificationCommand{
			{Name: "make build", Cmd: "make", Args: []string{"build"}, Timeout: 60 * time.Second,
				PreCheck: func(cwd string) bool { return hasFile(cwd, "Makefile") }},
			{Name: "make", Cmd: "make", Args: []string{}, Timeout: 60 * time.Second,
				PreCheck: func(cwd string) bool { return hasFile(cwd, "Makefile") }},
			{Name: "make test", Cmd: "make", Args: []string{"test"}, Timeout: 60 * time.Second,
				PreCheck: func(cwd string) bool { return hasFile(cwd, "Makefile") }},
		}

	// ========== 新增 10 种 ==========

	// C/C++：CMake 是主流构建系统，也支持直接 make
	case ProjectTypeCpp:
		return []VerificationCommand{
			// 1. cmake 配置 + make 编译（检测到 CMakeLists.txt）
			{Name: "cmake + make", Cmd: "cmake",
				Args: []string{"--build", ".", "--config", "Debug"},
				Timeout: 180 * time.Second,
				PreCheck: func(cwd string) bool { return hasFile(cwd, "CMakeLists.txt") }},
			// 2. cppcheck（静态分析，可选）
			{Name: "cppcheck", Cmd: "cppcheck", Args: []string{".", "--error-exitcode=1"}, Timeout: 30 * time.Second,
				PreCheck: func(cwd string) bool { return hasFile(cwd, "CMakeLists.txt") }},
		}

	// C# / .NET：dotnet CLI 是跨平台标准
	case ProjectTypeCSharp:
		return []VerificationCommand{
			// 1. dotnet build — 编译（自动找 .csproj 或 .sln）
			{Name: "dotnet build", Cmd: "dotnet", Args: []string{"build", "-v", "q"}, Timeout: 120 * time.Second},
			// 2. dotnet format — 格式检查
			{Name: "dotnet format --verify-no-changes", Cmd: "dotnet",
				Args: []string{"format", "--verify-no-changes"},
				Timeout: 60 * time.Second},
			// 3. dotnet test — 单元测试（只跑包含测试的项目）
			{Name: "dotnet test", Cmd: "dotnet",
				Args: []string{"test", "--no-build", "-v", "q", "--filter", "FullyQualifiedName~Test"},
				Timeout: 120 * time.Second},
		}

	// PHP：composer 管理依赖，phpunit 测试
	case ProjectTypePHP:
		return []VerificationCommand{
			// 1. php -l — 语法检查（对每个 .php 文件）
			{Name: "php lint", Cmd: "php", Args: []string{"-l", "."}, Timeout: 30 * time.Second},
			// 2. composer validate — composer.json 格式检查
			{Name: "composer validate", Cmd: "composer", Args: []string{"validate", "--no-check-all"}, Timeout: 30 * time.Second,
				PreCheck: func(cwd string) bool { return hasFile(cwd, "composer.json") }},
			// 3. phpunit — 单元测试
			{Name: "phpunit", Cmd: "phpunit", Args: []string{}, Timeout: 60 * time.Second,
				PreCheck: func(cwd string) bool { return hasFile(cwd, "phpunit.xml") || hasFile(cwd, "phpunit.xml.dist") }},
		}

	// Ruby：bundler 管理依赖，默认 Rake 测试
	case ProjectTypeRuby:
		return []VerificationCommand{
			// 1. ruby -c — 语法检查
			{Name: "ruby syntax check", Cmd: "ruby", Args: []string{"-c", "."}, Timeout: 30 * time.Second},
			// 2. bundle exec rake test — 单元测试（如果有 Rakefile）
			{Name: "bundle exec rake test", Cmd: "bundle",
				Args: []string{"exec", "rake", "test"}, Timeout: 60 * time.Second,
				PreCheck: func(cwd string) bool {
					return hasFile(cwd, "Gemfile") && hasFile(cwd, "Rakefile")
				}},
			// 3. rubocop — Ruby Lint（如果有配置）
			{Name: "rubocop", Cmd: "rubocop", Args: []string{"--format", "simple"}, Timeout: 30 * time.Second,
				PreCheck: func(cwd string) bool { return hasFile(cwd, ".rubocop.yml") }},
		}

	// Swift：Swift Package Manager 是标准
	case ProjectTypeSwift:
		return []VerificationCommand{
			// 1. swift build — 编译
			{Name: "swift build", Cmd: "swift", Args: []string{"build"}, Timeout: 120 * time.Second},
			// 2. swift test — 单元测试
			{Name: "swift test", Cmd: "swift", Args: []string{"test"}, Timeout: 120 * time.Second},
			// 3. swift-format — 格式检查（如果有配置）
			{Name: "swift-format lint", Cmd: "swift-format", Args: []string{"lint", "-r", "."}, Timeout: 30 * time.Second,
				PreCheck: func(cwd string) bool { return hasFile(cwd, ".swift-format") || hasFile(cwd, ".swift-format.json") }},
		}

	// Scala：sbt 是主要构建工具
	case ProjectTypeScala:
		return []VerificationCommand{
			// 1. sbt compile — 编译
			{Name: "sbt compile", Cmd: "sbt", Args: []string{"compile"}, Timeout: 180 * time.Second},
			// 2. sbt test — 单元测试
			{Name: "sbt test", Cmd: "sbt", Args: []string{"test"}, Timeout: 180 * time.Second},
			// 3. scalafmt — 格式检查
			{Name: "scalafmt --check", Cmd: "scalafmt", Args: []string{"--check"}, Timeout: 30 * time.Second},
		}

	// Elixir：mix 是官方构建/包管理器
	case ProjectTypeElixir:
		return []VerificationCommand{
			// 1. mix compile — 编译
			{Name: "mix compile", Cmd: "mix", Args: []string{"compile"}, Timeout: 120 * time.Second},
			// 2. mix test — 单元测试
			{Name: "mix test", Cmd: "mix", Args: []string{"test"}, Timeout: 120 * time.Second},
			// 3. mix format --check-formatted — 格式检查
			{Name: "mix format", Cmd: "mix", Args: []string{"format", "--check-formatted"}, Timeout: 30 * time.Second},
			// 4. mix credo — Elixir Lint（可选）
			{Name: "mix credo", Cmd: "mix", Args: []string{"credo"}, Timeout: 60 * time.Second},
		}

	// Haskell：stack 或 cabal
	case ProjectTypeHaskell:
		return []VerificationCommand{
			// 优先 stack（更现代），fallback cabal
			{Name: "stack build", Cmd: "stack", Args: []string{"build"}, Timeout: 180 * time.Second,
				PreCheck: func(cwd string) bool { return hasFile(cwd, "stack.yaml") }},
			{Name: "stack test", Cmd: "stack", Args: []string{"test"}, Timeout: 180 * time.Second,
				PreCheck: func(cwd string) bool { return hasFile(cwd, "stack.yaml") }},
			{Name: "cabal build", Cmd: "cabal", Args: []string{"build"}, Timeout: 180 * time.Second,
				PreCheck: func(cwd string) bool {
					return !hasFile(cwd, "stack.yaml") && (hasFile(cwd, "package.yaml") || hasGlob(cwd, "*.cabal"))
				}},
			{Name: "cabal test", Cmd: "cabal", Args: []string{"test"}, Timeout: 180 * time.Second,
				PreCheck: func(cwd string) bool {
					return !hasFile(cwd, "stack.yaml") && (hasFile(cwd, "package.yaml") || hasGlob(cwd, "*.cabal"))
				}},
		}

	// Zig：build.zig 是标准
	case ProjectTypeZig:
		return []VerificationCommand{
			// 1. zig build — 编译（自动运行 build.zig）
			{Name: "zig build", Cmd: "zig", Args: []string{"build"}, Timeout: 120 * time.Second},
			// 2. zig build test — 运行测试
			{Name: "zig build test", Cmd: "zig", Args: []string{"build", "test"}, Timeout: 120 * time.Second},
			// 3. zig fmt --check — 格式检查
			{Name: "zig fmt check", Cmd: "zig", Args: []string{"fmt", "--check", "."}, Timeout: 30 * time.Second},
		}

	// Dart / Flutter：pubspec.yaml 是标准
	case ProjectTypeDart:
		return []VerificationCommand{
			// 判断是纯 Dart 还是 Flutter 项目
			// 1. pub get — 拉依赖（如果没拉过）
			{Name: "dart pub get", Cmd: "dart", Args: []string{"pub", "get"}, Timeout: 60 * time.Second,
				PreCheck: func(cwd string) bool { return hasFile(cwd, "pubspec.yaml") && !hasFile(cwd, "pubspec.lock") }},
			// 2. dart analyze — 静态分析（必须跑）
			{Name: "dart analyze", Cmd: "dart", Args: []string{"analyze"}, Timeout: 60 * time.Second},
			// 3. dart test — 单元测试
			{Name: "dart test", Cmd: "dart", Args: []string{"test"}, Timeout: 60 * time.Second},
			// 4. dart format --set-exit-if-changed — 格式检查
			{Name: "dart format check", Cmd: "dart", Args: []string{"format", "--set-exit-if-changed", "."}, Timeout: 30 * time.Second},
			// Flutter 项目额外跑 flutter analyze
			{Name: "flutter analyze", Cmd: "flutter", Args: []string{"analyze"}, Timeout: 120 * time.Second,
				PreCheck: func(cwd string) bool {
					return hasFile(cwd, "pubspec.yaml") && hasFile(cwd, "lib/main.dart")
				}},
		}

	default:
		return nil
	}
}

// hasFile 检查 cwd 下是否存在指定文件名
func hasFile(cwd, name string) bool {
	_, err := os.Stat(filepath.Join(cwd, name))
	return err == nil
}

// hasGlob 检查 cwd 下是否存在匹配通配符的文件
func hasGlob(cwd, pattern string) bool {
	matches, err := filepath.Glob(filepath.Join(cwd, pattern))
	if err != nil {
		return false
	}
	return len(matches) > 0
}

// pkgHasScript 检查 Node.js 项目的 package.json 里是否有指定 script
func pkgHasScript(cwd, script string) bool {
	pkgPath := filepath.Join(cwd, "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return false
	}
	// 简单 JSON 解析（避免引入 encoding/json 循环依赖的风险）
	// 匹配 "scripts" 块里的 "script":
	scriptsMatch := strings.Contains(string(data), `"scripts"`) &&
		strings.Contains(string(data), `"`+script+`"`)
	return scriptsMatch
}

// BuildGateFailureMessage 把失败结果渲染成要注入 LLM 的消息
func BuildGateFailureMessage(r *GateResult) string {
	if r == nil || r.OverallPass || r.Skipped {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<verification-failure>\n")
	sb.WriteString("<!-- 验证门未通过，请在最终回答前修复 -->\n")
	sb.WriteString(fmt.Sprintf("<!-- 项目类型: %s -->\n", r.ProjectType))
	sb.WriteString(fmt.Sprintf("<!-- 总耗时: %.0fms -->\n", float64(r.TotalDuration)/float64(time.Millisecond)))

	sb.WriteString("\n[Verification Results]\n")
	for _, c := range r.Checks {
		status := "PASS"
		if !c.Passed {
			status = "FAIL"
		}
		sb.WriteString(fmt.Sprintf("  [%s] %s (%.0fms)\n", status, c.Name, float64(c.Duration)/float64(time.Millisecond)))
		if !c.Passed && c.Error != "" {
			sb.WriteString(fmt.Sprintf("    Error:\n%s\n", indentLines(c.Error, "      ")))
		}
	}

	sb.WriteString("</verification-failure>\n")
	return sb.String()
}

// indentLines 给多行文本加缩进
func indentLines(s, indent string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = indent + l
	}
	return strings.Join(lines, "\n")
}

// 确保 sort 包被使用
var _ = sort.Strings
