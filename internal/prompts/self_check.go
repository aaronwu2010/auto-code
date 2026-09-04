package prompts

// SelfCheckPrompt 返回生成后自检的引导 prompt
//
// 触发时机：agent 完成代码生成后（在 query.go 中，当检测到 agent 连续 N 次输出无 tool call 且任务类型为 feature/build 时）
// 这是"运行时验证闭环"的 Prompt 层实现
func SelfCheckPrompt() string {
	return `[Self-Check & Verify] 你刚完成了代码生成。在结束任务之前，请执行以下自检：

**Step 1 — 回读自检**
用 Read 工具逐个读取你刚创建/修改的每个文件，检查：
  • import/use 是否正确（路径、包名）
  • 函数签名是否与调用方匹配（参数类型、返回值）
  • 配置文件中的路径/端口是否一致（前后端、docker-compose）
  • 类型断言是否安全（不要忽略类型转换的 error）

**Step 2 — 完整性检查**
对照 L4 Stack Delivery Checklist，检查是否遗漏了：
  • 构建/依赖配置文件（go.mod / package.json / pyproject.toml / Cargo.toml）
  • 配置模板（config.example.yaml / .env.example）
  • README.md（项目说明 + 运行方法）
  • 启动脚本（Makefile / docker-compose.yml）
  • .gitignore

**Step 3 — 运行验证（必须）**
用 Bash/PowerShell 运行项目的构建命令：
  • Go: go build ./... && go vet ./...
  • Python: pip install -r requirements.txt && python -c "import main; print('OK')"
  • Node: npm install && npm run build
  • Rust: cargo build && cargo clippy
  • Java: mvn compile

如果构建失败，读取错误信息，修复后重新构建。

**Step 4 — 交付总结**
验证通过后，输出任务完成总结：
  • 创建的文件列表（完整路径）
  • 修改的文件列表
  • 如何运行/测试的说明
  • 已知限制或 TODO`
}
