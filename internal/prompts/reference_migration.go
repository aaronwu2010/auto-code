package prompts

import "strings"

// DetectReferencePrompt 从用户消息中检测是否包含"参考/参照"意图
// 触发 ReferenceMigrationGuide 的注入
func DetectReferencePrompt(prompt string) bool {
	keywords := []string{
		"参考", "参照", "基于", "仿照", "借鉴", "模仿",
		"reference", "based on", "modeled after", "inspired by",
		"like the", "similar to", "copy the",
	}
	lower := strings.ToLower(prompt)
	for _, kw := range keywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// ReferenceMigrationGuide 返回参考项目迁移的 5 步分析引导
//
// 触发条件：
//   - 用户消息包含"参考/参照/基于"等关键字
//   - 任务类型为 feature/build
func ReferenceMigrationGuide() string {
	return `[Reference Migration Guide] 检测到你需要参考另一个项目来实现功能。

请严格遵循以下 5 步分析流程，**每一步都要用工具实际执行**（不要猜测）：

═══════════════════════════════════════════════════
Step 1 — 目录结构扫描
═══════════════════════════════════════════════════
行动：用 Glob 列出参考项目的完整目录树，用 LS 查看关键子目录
目标：
  • 识别各层位置：前端 / 后端 / 配置 / 测试 / 脚本 / 文档
  • 判断是否 monorepo 结构（workspace / lerna / go.work / pnpm-workspace.yaml）
  • 标记参考项目的"入口点"和"核心模块"

═══════════════════════════════════════════════════
Step 2 — 技术栈识别
═══════════════════════════════════════════════════
行动：Read 参考项目的构建配置文件（package.json / go.mod / pyproject.toml / Cargo.toml / pom.xml）
目标：
  • 确认：语言版本、核心框架、构建工具、测试框架、ORM/数据库、API 风格
  • 记录关键版本号（如 Node 18, Go 1.22, FastAPI 0.110, Gin v1.10）
  • 识别依赖中的"重量级"库（如 React/Vue, FastAPI/Gin, Prisma/GORM）

═══════════════════════════════════════════════════
Step 3 — 核心代码阅读
═══════════════════════════════════════════════════
行动：Read 入口文件 → 顺着 import 链 Read 核心模块
目标：
  • 理解分层：路由/控制器 → 业务层 → 数据层
  • 理解模式：依赖注入方式、配置加载方式、错误处理模式、日志方式
  • 理解 API 契约：请求/响应格式、状态码约定、认证方式
  • 理解数据流：一个典型请求从入口到数据库的完整链路

═══════════════════════════════════════════════════
Step 4 — 适配差异分析
═══════════════════════════════════════════════════
行动：对比新项目与参考项目，列出差异点
目标：
  • 技术栈版本差异（Node 16 vs 20, Python 3.10 vs 3.12）
  • 框架差异（Express vs FastAPI, Gin vs Echo, Next.js vs Vite）
  • 目录结构差异
  • API 接口需要适配的点
  • 可以"照搬"的设计模式 vs 需要"重新实现"的具体代码

═══════════════════════════════════════════════════
Step 5 — 生成交付物
═══════════════════════════════════════════════════
行动：按 L4 全栈交付清单，用新项目技术栈重新实现
目标：
  • 理解模式 ≠ 复制代码。用新项目的技术栈和风格**重写**
  • 不要遗漏：README、配置模板、Dockerfile、启动脚本
  • 适配参考项目的设计模式到新项目技术栈（如：参考项目用 DI 容器 → 新项目也用 DI 容器，但用新项目的依赖注入库）
  • 最后跑 build + test 验证可编译

⚠️ 重要约束：
- 每一步都要**实际执行工具**，不要凭印象描述
- 不要复制粘贴参考项目的代码——理解模式后重写
- 如果参考项目和新项目技术栈不同，列出所有需要"翻译"的点
- 完成后必须跑 build 验证`
}
