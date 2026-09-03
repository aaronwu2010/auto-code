package prompts

import (
	"regexp"
	"strings"

	"github.com/auto-code/auto-code/internal/types"
)

// RiskType 风险模式类型
type RiskType string

const (
	RiskCommandInjection RiskType = "command_injection"
	RiskSQLInjection     RiskType = "sql_injection"
	RiskSensitiveInfo    RiskType = "sensitive_info"
	RiskPathTraversal    RiskType = "path_traversal"
	RiskXSS              RiskType = "xss"
	RiskUnsafeUnmarshal  RiskType = "unsafe_unmarshal"
)

// DetectRisks 从用户 prompt + 历史消息中检测安全风险模式
func DetectRisks(prompt string, messages []types.Message) []RiskType {
	var toolResults []string
	for _, m := range messages {
		if m.Role == types.RoleTool {
			toolResults = append(toolResults, m.Content)
		}
	}
	return DetectRisksFromContent(prompt, strings.Join(toolResults, "\n"))
}

// DetectRisksFromToolResults 从工具结果中检测风险（Turn N 动态更新）
func DetectRisksFromToolResults(prompt string, recentToolResults []string) []RiskType {
	return DetectRisksFromContent(prompt, strings.Join(recentToolResults, "\n"))
}

func DetectRisksFromContent(prompt string, content string) []RiskType {
	all := prompt + "\n" + content
	var risks []RiskType

	// 命令注入风险：用户输入 → shell 执行
	commandInjectionPatterns := []string{
		`exec\.Command.*\+`,
		`exec\.CommandContext.*\+`,
		`bash.*-c.*\+`,
		`sh.*-c.*\+`,
		`os/exec`,
		`Runtime\.getRuntime\(\)\.exec`,
	}
	if matchAnyRegex(all, commandInjectionPatterns) {
		risks = append(risks, RiskCommandInjection)
	}

	// SQL 注入风险：字符串拼接 SQL
	sqlInjectionPatterns := []string{
		`fmt\.Sprintf.*(SELECT|INSERT|UPDATE|DELETE)`,
		`sql\.Query.*\+`,
		`db\.Query.*\+`,
		`SELECT.*\.\s*\+`,
		`\+.*\.\s*SELECT`,
		`Raw\(.*\+`,
	}
	if matchAnyRegex(all, sqlInjectionPatterns) {
		risks = append(risks, RiskSQLInjection)
	}

	// 敏感信息泄露：硬编码密钥
	sensitiveInfoPatterns := []string{
		`password.*=.*["'][^"']{6,}["']`,
		`secret.*=.*["'][^"']{8,}["']`,
		`api_key.*=.*["'][^"']{8,}["']`,
		`apiSecret.*=.*["'][^"']{8,}["']`,
		`AWS_SECRET`,
		`AKIA[0-9A-Z]{16}`,
	}
	if matchAnyRegex(all, sensitiveInfoPatterns) {
		risks = append(risks, RiskSensitiveInfo)
	}

	// 路径遍历风险：用户输入 → 文件路径拼接
	pathTraversalPatterns := []string{
		`os\.Open.*\+`,
		`ioutil\.ReadFile.*\+`,
		`os\.WriteFile.*\+`,
		`filepath\.Join.*\+.*Input`,
	}
	if matchAnyRegex(all, pathTraversalPatterns) {
		risks = append(risks, RiskPathTraversal)
	}

	// XSS 风险：模板未转义输出用户输入
	xssPatterns := []string{
		`template\.HTML.*\+`,
		`template\.JS.*\+`,
		`SafeHTML`,
		`innerHTML.*=.*\+`,
		`v-html.*=`,
	}
	if matchAnyRegex(all, xssPatterns) {
		risks = append(risks, RiskXSS)
	}

	// 不安全反序列化
	unsafeUnmarshalPatterns := []string{
		`gob\.NewDecoder.*Decode`,
		`yaml\.Unmarshal.*untrusted`,
		`pickle\.loads`,
		`ObjectInputStream`,
	}
	if matchAnyRegex(all, unsafeUnmarshalPatterns) {
		risks = append(risks, RiskUnsafeUnmarshal)
	}

	return risks
}

// RiskChecklistText 返回指定风险类型的 checklist 文本
func RiskChecklistText(risk RiskType) string {
	switch risk {
	case RiskCommandInjection:
		return `[Security: 命令注入风险]

检测到可能的命令注入风险。请检查:
- 用 exec.Command(name, args...) 不要用 exec.Command("sh", "-c", userInput)
- 对用户输入做白名单验证（只允许预期字符）
- 如果必须用 shell，对特殊字符做转义
- 绝对不要把用户输入直接拼到 shell 命令里`

	case RiskSQLInjection:
		return `[Security: SQL 注入风险]

检测到可能的 SQL 注入风险。请检查:
- 用 ? 占位符参数化查询: db.Query("SELECT * FROM users WHERE id = ?", id)
- 用 ORM（GORM/Ent）自动生成安全 SQL
- 绝对不要 fmt.Sprintf 拼 SQL 字符串
- 如果必须动态拼接表名/列名（无法用占位符），做严格的白名单验证`

	case RiskSensitiveInfo:
		return `[Security: 敏感信息泄露风险]

检测到可能的硬编码密钥。请检查:
- 密钥用环境变量: os.Getenv("DB_PASSWORD")
- 提供 .env.example 展示需要哪些 env
- .gitignore 里加 .env, *.pem, id_rsa
- 不要在日志/错误信息里打印密钥
- 已经提交过的密钥 → 立即轮换（CI/CD 上的 secret 管理）`

	case RiskPathTraversal:
		return `[Security: 路径遍历风险]

检测到可能的路径遍历（../）风险。请检查:
- 用 filepath.Clean 规范化后验证: 结果是否仍在允许目录内
- 不要直接 filepath.Join(projectDir, userInput)，要验证结果在 projectDir 下
- 或者用白名单验证路径组件（只允许字母数字_-）`

	case RiskXSS:
		return `[Security: XSS 风险]

检测到可能的 XSS 风险。请检查:
- 默认用 HTML 转义（Go html/template 默认转义，用 text/template 才不转）
- 不要用 template.HTML / SafeHTML 标记用户输入
- React/Vue 默认转义，dangerouslySetInnerHTML / v-html 要小心`

	case RiskUnsafeUnmarshal:
		return `[Security: 不安全反序列化]

检测到可能的不安全反序列化。请检查:
- gob/Java ObjectInputStream 只能反序列化自己产出的数据
- YAML/JSON 反序列化不受控输入时，用 schema 验证
- pickle.loads 只信任本地文件，不要信任网络数据`
	}
	return ""
}

// matchAnyRegex 尝试所有正则，匹配任一返回 true
func matchAnyRegex(s string, patterns []string) bool {
	for _, p := range patterns {
		re, err := regexp.Compile("(?i)" + p)
		if err != nil {
			continue // 跳过无效正则
		}
		if re.MatchString(s) {
			return true
		}
	}
	return false
}
