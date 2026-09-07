package prompts

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ProjectType 项目类型
type ProjectType string

const (
	ProjectWebFullstack ProjectType = "web_fullstack" // 前后端分离 Web 应用
	ProjectCLI          ProjectType = "cli"           // CLI 命令行工具
	ProjectLibrary      ProjectType = "library"       // 库/SDK
	ProjectService      ProjectType = "service"       // 后端服务/微服务
	ProjectScaffold     ProjectType = "scaffold"      // 脚手架/模板生成器
	ProjectMobile       ProjectType = "mobile"        // 移动端应用
	ProjectML           ProjectType = "ml"            // ML/数据科学项目
	ProjectUnknown      ProjectType = "unknown"
)

// DetectProjectType 从项目目录结构 + 构建文件推断项目类型
// 自行扫描 cwd 下的关键文件，无需外部传入 keyFiles
func DetectProjectType(cwd string) ProjectType {
	if cwd == "" {
		return ProjectUnknown
	}

	has := func(names ...string) bool {
		for _, n := range names {
			if _, err := os.Stat(filepath.Join(cwd, n)); err == nil {
				return true
			}
		}
		return false
	}

	// 检测子目录（frontend/, backend/, client/, server/）
	hasSubdir := func(names ...string) bool {
		for _, sub := range names {
			if info, err := os.Stat(filepath.Join(cwd, sub)); err == nil && info.IsDir() {
				return true
			}
		}
		return false
	}

	// Web 全栈：同时有前端和后端构建文件
	hasFrontend := has("package.json", "vite.config.ts", "vite.config.js", "webpack.config.js", "next.config.js", "nuxt.config.ts")
	hasBackend := has("go.mod", "requirements.txt", "pyproject.toml", "Cargo.toml", "pom.xml", "build.gradle", "Makefile")
	if hasFrontend && hasBackend {
		return ProjectWebFullstack
	}
	if hasSubdir("frontend", "backend", "client", "server", "web") {
		return ProjectWebFullstack
	}

	// CLI 工具：有 main.go/main.py + cobra/click/typer 依赖
	if has("cobra", "urfave/cli", "click", "typer", "commander", "cli") {
		return ProjectCLI
	}
	if has("main.go", "main.py", "main.rs") && hasSubdir("cmd", "bin") {
		return ProjectCLI
	}

	// 库/SDK：有 lib/ pkg/ 目录
	if hasSubdir("lib", "pkg", "sdk") && !hasSubdir("cmd", "bin") {
		return ProjectLibrary
	}

	// 脚手架/模板生成器
	if hasSubdir("template", "scaffold", "generator") {
		return ProjectScaffold
	}

	// ML/数据科学
	if hasSubdir("notebooks", "models", "training") {
		return ProjectML
	}

	// 后端服务：有 Dockerfile 或 docker-compose
	if has("docker-compose.yml", "docker-compose.yaml", "Dockerfile") {
		return ProjectService
	}
	// 有后端构建文件但没有前端 → 后端服务
	if hasBackend {
		return ProjectService
	}

	// 移动端
	if hasSubdir("android", "ios", "flutter") || has("android", "ios", "flutter", "react-native") {
		return ProjectMobile
	}

	return ProjectUnknown
}

// StackDeliveryChecklist 根据项目类型返回完整交付物 checklist
//
// 触发条件：
//   - 任务类型为 feature/build
//   - 项目类型 != ProjectUnknown
func StackDeliveryChecklist(taskType TaskType, projectType ProjectType, lang ProjectLang) string {
	if taskType != DynTaskFeature && taskType != DynTaskBuild {
		return ""
	}
	if projectType == ProjectUnknown {
		return ""
	}

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("[Stack Delivery Checklist] 检测到项目类型: %s\n\n", projectType))
	sb.WriteString("你的目标是交付一个**开箱即用**的完整项目。请对照以下清单检查并补齐所有必要文件：\n\n")

	switch projectType {
	case ProjectWebFullstack:
		sb.WriteString(webFullstackChecklist(lang))
	case ProjectCLI:
		sb.WriteString(cliChecklist(lang))
	case ProjectLibrary:
		sb.WriteString(libraryChecklist(lang))
	case ProjectService:
		sb.WriteString(serviceChecklist(lang))
	case ProjectScaffold:
		sb.WriteString(scaffoldChecklist(lang))
	case ProjectML:
		sb.WriteString(mlChecklist(lang))
	default:
		sb.WriteString(genericFullstackChecklist(lang))
	}

	sb.WriteString("\n---\n\n")
	sb.WriteString(`[Delivery Reminder] 交付完成前必须:
1. 运行 build 验证项目能成功编译
2. 运行 test 验证测试通过
3. 确保 README.md 包含运行说明
4. 如果创建了新配置文件，同步创建 .example 版本（不要提交真实密钥）`)

	return sb.String()
}

// webFullstackChecklist Web 全栈项目交付清单
func webFullstackChecklist(lang ProjectLang) string {
	return `[Web 全栈交付清单]

前端部分：
☐ package.json + 依赖声明
☐ index.html / main 入口文件
☐ 路由配置 + 页面组件
☐ API 调用层（封装 fetch/axios 请求）
☐ 组件库 + 样式（TailwindCSS / styled-components 等）

后端部分：
☐ 后端构建配置（go.mod / pyproject.toml / Cargo.toml / pom.xml）
☐ 路由/控制器（Router / Handler / Controller）
☐ 业务逻辑层（Service / Usecase）
☐ 数据访问层（Repository / DAO / Model）
☐ 中间件（认证 / 日志 / CORS）

跨层：
☐ 配置文件（config.json / settings.yaml + .example 版本）
☐ Dockerfile（前端 + 后端）
☐ docker-compose.yml（一键启动）
☐ Makefile / scripts/*.sh（构建、启动、测试脚本）
☐ README.md（项目说明 + 快速开始 + API 文档）
☐ .gitignore（覆盖 node_modules, .env, secrets）
☐ API 契约（OpenAPI/Swagger 或 docs/api.md）
☐ health endpoint（/health 用于健康检查）`
}

// cliChecklist CLI 工具交付清单
func cliChecklist(lang ProjectLang) string {
	return `[CLI 工具交付清单]

核心：
☐ 入口文件（main.go / main.py / main.rs / index.ts）
☐ 子命令框架（cobra / click / urfave/cli / commander）
☐ 全局配置（--config, --verbose, --version, --help）
☐ 子命令注册 + handler 实现

辅助：
☐ 配置加载（YAML / TOML / INI + 默认值）
☐ 日志框架（debug/info/warn/error 级别）
☐ 退出码规范（0=成功, 1=通用错误, 2=用法错误）
☐ 信号处理（SIGINT/SIGTERM 优雅退出）
☐ 进度条/Spinner（长任务）

交付物：
☐ README.md（安装 + 用法 + 所有子命令说明）
☐ Makefile / scripts/build.sh（交叉编译脚本）
☐ .gitignore
☐ --help 示例输出
☐ --version 输出`
}

// libraryChecklist 库/SDK 交付清单
func libraryChecklist(lang ProjectLang) string {
	return `[库/SDK 交付清单]

API：
☐ 核心类型定义（interface / struct / class）
☐ 主 API 入口（NewXXXClient / createClient）
☐ 错误类型（自定义 error / error codes）
☐ 文档注释（每个导出类型和方法的 godoc/docstring）

实现：
☐ 接口 + 实现分离（便于 mock）
☐ 上下文支持（context.Context / AbortController）
☐ 超时/重试策略
☐ 连接池/资源管理（Close / cleanup）

质量：
☐ 单元测试（核心路径 + 边界情况）
☐ 类型安全（strict TS / generics / type hints）
☐ README.md（快速开始 + API 速查 + 示例）
☐ CHANGELOG.md（版本变更记录）`
}

// serviceChecklist 后端服务交付清单
func serviceChecklist(lang ProjectLang) string {
	return `[后端服务交付清单]

核心：
☐ 入口（main.go）+ 路由注册
☐ 中间件（CORS / 认证 / 日志 / 限流）
☐ 业务逻辑层（Service）
☐ 数据访问层（Repository + Model/Entity）
☐ 配置加载（环境变量 + 默认值 + 校验）

工程化：
☐ health endpoint（/health → DB 连接检查 + 版本号）
☐ 优雅关闭（SIGTERM → 停止接收请求 → 等待进行中请求 → 释放资源）
☐ Dockerfile（多阶段构建）
☐ docker-compose.yml（开发环境一键启动 + DB + Redis）
☐ Makefile（build / test / run / docker / migrate）
☐ .env.example（所有配置项 + 说明）

文档：
☐ README.md（架构概览 + 快速开始 + 配置说明）
☐ docs/api.md（OpenAPI/Swagger 或手写 API 文档）
☐ README 中的部署说明（Docker / 直接运行）`
}

// scaffoldChecklist 脚手架交付清单
func scaffoldChecklist(lang ProjectLang) string {
	return `[脚手架/模板生成器交付清单]

核心：
☐ 模板文件（template/ 目录结构）
☐ 模板变量注入（项目名 / 作者 / 版本等）
☐ 生成器入口（main / index.ts / cli.js）
☐ 交互式提问（prompt 模块）

交付物：
☐ README.md（安装 + 使用 + 模板变量说明）
☐ 示例输出（example/ 目录或截图）
☐ .gitignore
☐ CHANGELOG.md`
}

// mlChecklist ML/数据科学交付清单
func mlChecklist(lang ProjectLang) string {
	return `[ML/数据科学项目交付清单]

核心：
☐ 训练脚本（train.py）
☐ 推理/预测脚本（predict.py / serve.py）
☐ 模型定义（model.py）
☐ 数据加载器（dataset.py / dataloader.py）
☐ requirements.txt / pyproject.toml

工程化：
☐ 配置文件（config.yaml + .example）
☐ Dockerfile（训练环境 + 推理环境）
☐ Makefile / scripts/*.sh
☐ notebooks/（探索性分析 + 训练可视化）
☐ data/ 目录说明（.gitkeep + README）

文档：
☐ README.md（项目背景 + 模型架构 + 训练方法 + 使用说明）
☐ docs/ 模型文档（架构图 + 超参说明 + 评估指标）`
}

// genericFullstackChecklist 通用全栈交付清单（无法识别类型时的兜底）
func genericFullstackChecklist(lang ProjectLang) string {
	return `[通用交付清单]

核心代码：
☐ 入口文件（main / index / app）
☐ 核心业务逻辑
☐ 依赖声明（go.mod / package.json / requirements.txt / Cargo.toml）

工程化：
☐ 配置文件（config + .example 版本）
☐ .gitignore
☐ README.md（项目说明 + 运行方法 + 目录结构 + 使用示例）
☐ 构建脚本（Makefile / scripts/build.sh / npm scripts）

如果是 Web/服务类项目，额外需要：
☐ Dockerfile（如果涉及部署）
☐ docker-compose.yml（如果有多个服务）
☐ health endpoint

如果是库/SDK，额外需要：
☐ 单元测试
☐ API 文档注释`
}
