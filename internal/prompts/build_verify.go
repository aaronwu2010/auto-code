package prompts

// GetBuildVerifySection 返回"编码后自验证指南"段落
//
// 这是从实战经验中提炼的编码验证方法论。核心观点：
// 写完代码不叫完成，能编译、能通过静态检查、能通过测试才算完成。
//
// 和 VerificationGate（代码层面的硬强制）的关系：
// - VerificationGate 在 agent 说"完成"时强制跑 build/vet/test，失败则阻止退出
// - BuildVerifySection 告诉 LLM 在编码过程中/编码后**主动**跑验证，不要等到被 gate 卡住
// - 两者互补：prompt 引导"主动做"，gate 兜底"不做不行"
func GetBuildVerifySection() string {
	return `# Code Verification — 编码后自验证指南

## 什么时候跑验证？

**写完一批代码文件后，主动跑验证。不要等到全部写完才第一次编译。**

编码过程中插入验证点：
- 改了 import 或函数签名后 → 立刻跑编译（能发现类型不匹配、缺失 import）
- 写完一个模块的主要逻辑后 → 跑 build + vet
- 写完所有文件、准备告诉用户"完成了"之前 → 跑完整的 build + vet + test

## 各语言/框架的验证命令

### Go

  go build ./...        # 编译检查（最优先，必须先过）
  go vet ./...          # 静态分析：nil 解引用、printf 格式错、未使用变量等
  gofmt -l .            # 格式检查：列出需要格式化的文件（空输出=合规）
  go test ./...         # 单元测试（已有测试的情况下）

**经验**：
- go build 必须能过，vet 警告要修掉（常见 bug 源头）
- 如果项目没 go.mod，先 go mod init
- vendor 目录存在时用 go build -mod=vendor
- Windows 环境下 go build ./... 会扫描所有子目录，注意不要把 node_modules 等误包含

### Java (Maven)

  mvn compile -q                        # 快速编译（-q 安静模式）
  mvn clean package -DskipTests         # 完整打包（清理+编译+打包，跳过测试）
  mvn clean package                     # 完整打包+测试
  mvn test                              # 只跑测试

**经验**：
- compile 通过不代表 package 通过（打包阶段会处理资源文件、依赖合并）
- -DskipTests 在开发阶段省时间，但交付前必须跑完整 test
- 如果是多模块项目：mvn clean package -pl moduleName -am（只构建指定模块及其依赖）
- Gradle 项目把 mvn 换成 gradle：gradle build -x test

### Node.js / TypeScript

  npm run build           # TypeScript 编译 / 前端构建（如果有 build script）
  npm run lint            # ESLint / TSLint 检查（如果有）
  npm test                # 单元测试
  npx tsc --noEmit        # TypeScript 类型检查（不生成文件）

**经验**：
- TypeScript 项目必须跑 tsc --noEmit——类型错误是最常见的隐藏 bug
- build 脚本可能包含 webpack/vite/esbuild，比单纯 tsc 严格
- 如果 package.json 里没有 build script，说明可能不是构建型项目，跳过 build

### Python

  python -c "import sys; print(sys.version)"   # 确认 Python 版本
  python -c "import your_module"                # 验证模块能导入（语法正确）
  pytest -x -q                                  # 单元测试（-x 遇错停止，-q 安静模式）
  mypy .                                        # 类型检查（如果用了类型注解）
  ruff check .                                  # Lint 检查（现代 Python lint 工具）

**经验**：
- Python 没有编译步骤，但 import 检查能发现语法错误和缺失依赖
- import 失败 = 代码有问题（拼错模块名、依赖没装、循环导入）
- 如果是纯脚本项目（没有包结构），pytest 可能找不到测试，跳过即可
- venv/conda 环境先激活再跑，否则依赖不在 PATH 里

### Rust

  cargo check --quiet          # 只检查不生成二进制（比 build 快，日常开发首选）
  cargo build --quiet          # 完整编译
  cargo clippy --quiet         # Lint 检查（比 vet 更严格，警告必须修）
  cargo fmt -- --check         # 格式检查
  cargo test --quiet           # 单元测试

**经验**：
- cargo check 比 cargo build 快很多，日常写代码后先跑 check
- clippy 警告要修——Rust 编译器已经很严格了，clippy 更严格
- fmt 失败说明格式不对，cargo fmt 自动修复

### C/C++ (CMake)

  cmake --build . --config Debug      # 编译（自动找 CMakeLists.txt）
  cppcheck . --error-exitcode=1       # 静态分析（可选，需要安装）

**经验**：
- CMake 项目用 cmake --build 比直接 make 更通用（跨平台）
- Windows 上可以先 cmake -S . -B build -G "MinGW Makefiles" 再 build
- 没有 CMakeLists.txt 但有 Makefile → 跳过，走 Generic 类别

### C# / .NET

  dotnet build -v q                              # 编译（自动找 .csproj 或 .sln）
  dotnet format --verify-no-changes              # 格式检查
  dotnet test --no-build -v q                    # 单元测试

**经验**：
- dotnet build 自动扫描子目录，一个命令搞定整个解决方案
- -v q（quiet）减少输出，方便看错误
- Windows / macOS / Linux 命令完全一致

### PHP

  php -l .                               # 语法检查（对每个 .php 文件）
  composer validate --no-check-all       # composer.json 格式检查
  phpunit                                # 单元测试（需要 phpunit.xml）

**经验**：
- php -l 对每个文件单独检查，能发现所有语法错误
- composer validate 确保依赖声明正确
- Laravel/Symfony 等框架也用这套命令

### Ruby

  ruby -c .                               # 语法检查
  bundle exec rake test                   # 单元测试（需要 Rakefile）
  rubocop --format simple                 # Lint 检查（需要 .rubocop.yml）

**经验**：
- ruby -c 能检查语法但不执行代码
- 必须先 bundle install 才能跑 bundle exec
- Rails 项目：rails test（等价于 rake test）

### Swift (SPM)

  swift build                             # 编译
  swift test                              # 单元测试
  swift-format lint -r .                  # 格式检查（需要 .swift-format 配置）

**经验**：
- Package.swift 是 Swift Package Manager 标准入口
- 跨平台支持：macOS / Linux（Windows 有限支持）

### Scala (sbt)

  sbt compile            # 编译
  sbt test               # 单元测试
  scalafmt --check       # 格式检查

**经验**：
- sbt 首次启动慢（要下载依赖），后续会缓存
- build.sbt 是标准构建文件

### Elixir (mix)

  mix compile                       # 编译
  mix test                          # 单元测试
  mix format --check-formatted      # 格式检查
  mix credo                         # Lint 检查

**经验**：
- mix 是 Elixir 官方包管理器 + 构建工具，一个命令搞定一切
- mix test 会自动找 test/ 目录下的测试

### Haskell (stack / cabal)

  stack build              # 编译（优先 stack）
  stack test               # 单元测试
  cabal build              # 编译（没有 stack.yaml 时）
  cabal test               # 单元测试

**经验**：
- stack 比 cabal 更现代化，推荐优先使用
- stack.yaml 存在时自动走 stack，否则 fallback 到 cabal

### Zig

  zig build                    # 编译（自动运行 build.zig）
  zig build test               # 运行测试
  zig fmt --check .            # 格式检查

**经验**：
- build.zig 是 Zig 项目唯一的构建入口
- zig build 会自动发现和运行 build.zig 里定义的所有 step

### Dart / Flutter

  dart analyze                              # 静态分析（必须跑）
  dart test                                 # 单元测试
  dart format --set-exit-if-changed .       # 格式检查
  flutter analyze                           # Flutter 项目额外检查（需要 lib/main.dart）

**经验**：
- dart analyze 是 Dart 最核心的验证命令，能发现类型错误、未使用变量等
- Flutter 项目有 pubspec.yaml + lib/main.dart → 额外跑 flutter analyze

### 通用 Makefile 项目

  make          # 默认目标（通常是 build/all）
  make build    # 显式构建（如果有 build target）
  make test     # 测试
  make clean    # 清理构建产物

## 验证失败时的处理顺序

1. **读错误信息**：逐行读，提取文件路径、行号、错误类型
2. **定位根因**：Grep 搜报错的符号 → 读相关代码 → 找问题
3. **修复**：Edit 对应文件，只改必要的几行
4. **重新验证**：跑完 build → 跑 vet → 跑 test
5. **如果还失败**：读新的错误信息，回到 Step 1

**不要跳过错误信息直接"试试改别的地方"——错误信息是最精确的线索。**

## 实战 checklist

- [ ] 改完代码后，我跑编译了吗？
- [ ] 编译通过后，我跑 vet/lint 了吗？
- [ ] vet 警告我修了吗？（还是说"警告不影响"就跳过了）
- [ ] 有测试的话，我跑测试了吗？
- [ ] 我读了验证的输出了吗？（还是说看到绿色就跳过了）
- [ ] 如果有多个错误，我按从第一个开始顺序修了吗？（后面的错误可能是连锁反应）
- [ ] 修完后我重新跑了完整的验证链吗？（不要只跑 vet 不跑 build）

## 记住

- "我写了代码"不等于"代码能跑"
- "代码能编译"不等于"代码没有 bug"
- "vet/lint 有警告" = 需要修复，不是可以跳过
- **VerificationGate 会在你说"完成"时强制跑验证，你主动跑就不用被卡住多一轮**
- 最好的验证时机是"写完一段代码就跑"，而不是"全部写完才第一次编译"`
}
