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
		checks := []struct {
			file string
			pt   ProjectType
		}{
			{"go.mod", ProjectTypeGo},
			{"Cargo.toml", ProjectTypeRust},
			{"pom.xml", ProjectTypeJava},
			{"build.gradle", ProjectTypeJava},
			{"package.json", ProjectTypeNode},
			{"pyproject.toml", ProjectTypePython},
			{"requirements.txt", ProjectTypePython},
			{"setup.py", ProjectTypePython},
		}
		for _, c := range checks {
			if _, err := os.Stat(filepath.Join(cwd, c.file)); err == nil {
				g.cachedType = c.pt
				return
			}
		}
		if _, err := os.Stat(filepath.Join(cwd, "Makefile")); err == nil {
			g.cachedType = ProjectTypeGeneric
			return
		}
		g.cachedType = ProjectTypeUnknown
	})
	return g.cachedType
}

// getVerificationCommands 根据项目类型返回验证命令列表
func (g *VerificationGate) getVerificationCommands(pt ProjectType) []VerificationCommand {
	switch pt {
	case ProjectTypeGo:
		return []VerificationCommand{
			{Name: "go build", Cmd: "go", Args: []string{"build", "./..."}, Timeout: 60 * time.Second},
			{Name: "go vet", Cmd: "go", Args: []string{"vet", "./..."}, Timeout: 30 * time.Second},
			{Name: "go test", Cmd: "go", Args: []string{"test", "./..."}, Timeout: 60 * time.Second},
		}
	case ProjectTypeNode:
		return []VerificationCommand{
			{Name: "npm test", Cmd: "npm", Args: []string{"test"}, Timeout: 60 * time.Second},
		}
	case ProjectTypePython:
		return []VerificationCommand{
			{Name: "pytest", Cmd: "pytest", Args: []string{"-x", "-q"}, Timeout: 60 * time.Second},
		}
	case ProjectTypeRust:
		return []VerificationCommand{
			{Name: "cargo build", Cmd: "cargo", Args: []string{"build", "--quiet"}, Timeout: 120 * time.Second},
			{Name: "cargo test", Cmd: "cargo", Args: []string{"test", "--quiet"}, Timeout: 120 * time.Second},
		}
	default:
		return nil
	}
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
