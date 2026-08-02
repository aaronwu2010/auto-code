# 第一阶段：核心架构增强 - 详细任务分解表

> 项目: auto-code 架构改进
> 阶段: 第一阶段（核心架构增强）
> 周期: 4-6 周
> 更新时间: 2026-07-31

---

## 一、阶段目标

建立 AI Agent 的三大核心能力：
1. **感知能力**: 统一的输入处理和环境感知机制
2. **规划能力**: 智能的任务分解和计划管理
3. **反思能力**: 错误处理和持续学习机制

---

## 二、任务分解总览

| 模块 | 开发周期 | 代码文件数 | 预计工时 | 优先级 |
|------|---------|-----------|---------|--------|
| 感知层设计 | Week 1-2 | 5 | 80h | P0 |
| 规划模块 | Week 3-4 | 6 | 80h | P0 |
| 反思机制 | Week 5-6 | 5 | 80h | P1 |

**总计**: 240 工时（约 6 周，每周 40 小时）

---

## 三、Week 1-2: 感知层设计

### 3.1 任务清单

#### 任务组 1.1: 接口设计与数据结构 (Day 1-3)

| 任务ID | 任务名称 | 工时 | 依赖 | 交付物 | 验收标准 |
|--------|---------|------|------|--------|---------|
| P1-1.1.1 | 设计 Perception 接口定义 | 4h | - | `internal/perception/perception.go` | 接口定义清晰，包含 Process/Filter/Inject 方法 |
| P1-1.1.2 | 定义输入数据结构 (InputData) | 3h | P1-1.1.1 | `internal/perception/types.go` | 支持文本、图像、语音、结构化数据 |
| P1-1.1.3 | 定义上下文结构 (Context) | 3h | P1-1.1.1 | `internal/perception/context.go` | 包含用户信息、会话历史、环境变量 |
| P1-1.1.4 | 设计信号过滤器接口 | 2h | P1-1.1.1 | `internal/perception/signal_filter.go` | 支持规则过滤和智能过滤 |
| P1-1.1.5 | 编写接口单元测试 | 4h | P1-1.1.1-4 | `internal/perception/perception_test.go` | 测试覆盖率 > 90% |

**小计**: 16 工时

---

#### 任务组 1.2: 核心功能实现 (Day 4-7)

| 任务ID | 任务名称 | 工时 | 依赖 | 交付物 | 验收标准 |
|--------|---------|------|------|--------|---------|
| P1-1.2.1 | 实现输入处理器 (InputProcessor) | 8h | P1-1.1.2 | `internal/perception/input_processor.go` | 支持文本输入处理，性能 < 50ms |
| P1-1.2.2 | 实现多模态支持框架 | 8h | P1-1.2.1 | `internal/perception/multimodal.go` | 预留图像、语音接口，可扩展 |
| P1-1.2.3 | 实现上下文注入器 (ContextInjector) | 8h | P1-1.1.3 | `internal/perception/context_injector.go` | 自动注入用户信息、会话历史 |
| P1-1.2.4 | 实现信号过滤器 (SignalFilter) | 6h | P1-1.1.4 | `internal/perception/signal_filter.go` | 规则过滤准确率 > 95% |
| P1-1.2.5 | 编写功能单元测试 | 8h | P1-1.2.1-4 | `internal/perception/*_test.go` | 测试覆盖率 > 85% |

**小计**: 30 工时

---

#### 任务组 1.3: 集成与优化 (Day 8-10)

| 任务ID | 任务名称 | 工时 | 依赖 | 交付物 | 验收标准 |
|--------|---------|------|------|--------|---------|
| P1-1.3.1 | 与 engine/context/builder.go 集成 | 8h | P1-1.2.1-4 | 修改后的 builder.go | 集成后不影响现有功能 |
| P1-1.3.2 | 实现感知层管理器 (PerceptionManager) | 6h | P1-1.2.1-4 | `internal/perception/manager.go` | 统一管理所有感知组件 |
| P1-1.3.3 | 性能优化和缓存机制 | 6h | P1-1.3.2 | 性能优化代码 | 处理延迟 < 100ms |
| P1-1.3.4 | 编写集成测试 | 8h | P1-1.3.1-3 | `internal/perception/integration_test.go` | 端到端测试通过 |

**小计**: 28 工时

---

#### 任务组 1.4: 文档与交付 (Day 11-14)

| 任务ID | 任务名称 | 工时 | 依赖 | 交付物 | 验收标准 |
|--------|---------|------|------|--------|---------|
| P1-1.4.1 | 编写感知层使用文档 | 4h | P1-1.3.1-4 | `docs/perception-usage.md` | 文档清晰，包含示例代码 |
| P1-1.4.2 | 编写API文档 | 3h | P1-1.3.1-4 | `docs/perception-api.md` | API文档完整，包含参数说明 |
| P1-1.4.3 | 性能基准测试 | 3h | P1-1.3.3 | `internal/perception/benchmark_test.go` | 性能指标符合预期 |
| P1-1.4.4 | Code Review 和重构 | 4h | P1-1.4.1-3 | 优化后的代码 | 代码质量符合规范 |
| P1-1.4.5 | 准备演示和培训材料 | 2h | P1-1.4.1-4 | 演示PPT/视频 | 团队理解架构设计 |

**小计**: 16 工时

---

### 3.2 感知层交付物清单

**代码文件**:
```
internal/perception/
├── perception.go           # 主接口定义
├── types.go                # 数据结构定义
├── context.go              # 上下文结构
├── input_processor.go      # 输入处理器
├── multimodal.go           # 多模态支持
├── context_injector.go     # 上下文注入器
├── signal_filter.go        # 信号过滤器
├── manager.go              # 感知层管理器
├── perception_test.go      # 单元测试
├── integration_test.go     # 集成测试
└── benchmark_test.go       # 性能测试
```

**文档文件**:
```
docs/
├── perception-usage.md     # 使用文档
└── perception-api.md       # API文档
```

**修改文件**:
- `internal/engine/context/builder.go` - 集成感知层

---

### 3.3 感知层风险点

| 风险 | 影响 | 概率 | 应对措施 |
|------|------|------|---------|
| 多模态接口设计不完善 | 中 | 低 | 预留扩展点，先实现文本输入 |
| 与现有系统兼容性问题 | 高 | 中 | 增量集成，保持向后兼容 |
| 性能不达标 | 中 | 低 | 实现缓存机制，异步处理 |

---

## 四、Week 3-4: 规划模块

### 4.1 任务清单

#### 任务组 2.1: 接口设计 (Day 1-3)

| 任务ID | 任务名称 | 工时 | 依赖 | 交付物 | 验收标准 |
|--------|---------|------|------|--------|---------|
| P1-2.1.1 | 设计 Planner 接口 | 4h | - | `internal/planning/planner.go` | 支持多种规划策略 |
| P1-2.1.2 | 定义计划数据结构 (Plan) | 3h | P1-2.1.1 | `internal/planning/types.go` | 包含任务树、执行状态、依赖关系 |
| P1-2.1.3 | 定义任务分解接口 (TaskDecomposer) | 3h | P1-2.1.1 | `internal/planning/task_decomposer.go` | 支持启发式和LLM辅助分解 |
| P1-2.1.4 | 设计计划持久化接口 | 2h | P1-2.1.1 | `internal/planning/plan_repository.go` | 支持存储和恢复计划 |
| P1-2.1.5 | 编写接口单元测试 | 4h | P1-2.1.1-4 | `internal/planning/planner_test.go` | 测试覆盖率 > 90% |

**小计**: 16 工时

---

#### 任务组 2.2: 核心功能实现 (Day 4-7)

| 任务ID | 任务名称 | 工时 | 依赖 | 交付物 | 验收标准 |
|--------|---------|------|------|--------|---------|
| P1-2.2.1 | 实现任务分解器 | 10h | P1-2.1.3 | `internal/planning/task_decomposer.go` | 分解成功率 > 85% |
| P1-2.2.2 | 实现 ReAct 模式规划器 | 12h | P1-2.1.1 | `internal/planning/react_planner.go` | 支持边想边做模式 |
| P1-2.2.3 | 实现计划执行器 | 8h | P1-2.1.2 | `internal/planning/plan_executor.go` | 支持顺序和并行执行 |
| P1-2.2.4 | 实现计划持久化 | 6h | P1-2.1.4 | `internal/planning/plan_repository.go` | 支持JSON格式存储 |
| P1-2.2.5 | 编写功能单元测试 | 8h | P1-2.2.1-4 | `internal/planning/*_test.go` | 测试覆盖率 > 85% |

**小计**: 44 工时

---

#### 任务组 2.3: 重规划机制 (Day 8-11)

| 任务ID | 任务名称 | 工时 | 依赖 | 交付物 | 验收标准 |
|--------|---------|------|------|--------|---------|
| P1-2.3.1 | 设计重规划触发条件 | 4h | P1-2.2.1-4 | `internal/planning/replan_conditions.go` | 明确触发规则 |
| P1-2.3.2 | 实现重规划器 | 10h | P1-2.3.1 | `internal/planning/replanner.go` | 触发准确率 > 80% |
| P1-2.3.3 | 实现计划恢复机制 | 6h | P1-2.2.4 | `internal/planning/plan_recovery.go` | 支持从失败点恢复 |
| P1-2.3.4 | 与 tools/coordinator 集成 | 8h | P1-2.3.1-3 | 修改后的 coordinator.go | 集成后不影响现有功能 |
| P1-2.3.5 | 编写重规划测试 | 6h | P1-2.3.1-4 | `internal/planning/replan_test.go` | 覆盖各种异常场景 |

**小计**: 34 工时

---

#### 任务组 2.4: 测试与交付 (Day 12-14)

| 任务ID | 任务名称 | 工时 | 依赖 | 交付物 | 验收标准 |
|--------|---------|------|------|--------|---------|
| P1-2.4.1 | 编写集成测试 | 8h | P1-2.3.1-5 | `internal/planning/integration_test.go` | 端到端测试通过 |
| P1-2.4.2 | 性能优化 | 4h | P1-2.4.1 | 性能优化代码 | 规划生成 < 500ms |
| P1-2.4.3 | 编写使用文档 | 4h | P1-2.4.1 | `docs/planning-usage.md` | 文档清晰，包含示例 |
| P1-2.4.4 | Code Review | 3h | P1-2.4.1-3 | 优化后的代码 | 代码质量符合规范 |
| P1-2.4.5 | 准备演示材料 | 3h | P1-2.4.1-4 | 演示PPT/视频 | 团队理解规划逻辑 |

**小计**: 22 工时

---

### 4.2 规划模块交付物清单

**代码文件**:
```
internal/planning/
├── planner.go              # 规划器接口
├── types.go                # 数据结构
├── task_decomposer.go      # 任务分解器
├── react_planner.go        # ReAct规划器
├── plan_executor.go        # 计划执行器
├── replanner.go            # 重规划器
├── plan_repository.go      # 计划持久化
├── plan_recovery.go        # 计划恢复
├── replan_conditions.go    # 重规划条件
├── planner_test.go         # 单元测试
├── replan_test.go          # 重规划测试
└── integration_test.go     # 集成测试
```

**文档文件**:
```
docs/
└── planning-usage.md       # 使用文档
```

**修改文件**:
- `internal/tools/coordinator/coordinator.go` - 集成规划模块

---

### 4.3 规划模块风险点

| 风险 | 影响 | 概率 | 应对措施 |
|------|------|------|---------|
| 任务分解算法不准确 | 高 | 中 | 混合使用启发式和LLM辅助 |
| 重规划触发过于频繁 | 中 | 中 | 设置合理的阈值和冷却时间 |
| 计划持久化数据丢失 | 高 | 低 | 实现事务和备份机制 |

---

## 五、Week 5-6: 反思机制

### 5.1 任务清单

#### 任务组 3.1: 接口设计 (Day 1-3)

| 任务ID | 任务名称 | 工时 | 依赖 | 交付物 | 验收标准 |
|--------|---------|------|------|--------|---------|
| P1-3.1.1 | 设计 Reflector 接口 | 4h | - | `internal/reflection/reflector.go` | 定义反思触发点 |
| P1-3.1.2 | 定义评估结果结构 (EvaluationResult) | 3h | P1-3.1.1 | `internal/reflection/types.go` | 包含成功/失败/改进建议 |
| P1-3.1.3 | 定义经验数据结构 (Experience) | 3h | P1-3.1.1 | `internal/reflection/experience.go` | 包含错误类型、解决方案、效果评分 |
| P1-3.1.4 | 设计经验存储接口 | 2h | P1-3.1.3 | `internal/reflection/experience_store.go` | 支持检索和更新 |
| P1-3.1.5 | 编写接口单元测试 | 4h | P1-3.1.1-4 | `internal/reflection/reflector_test.go` | 测试覆盖率 > 90% |

**小计**: 16 工时

---

#### 任务组 3.2: 核心功能实现 (Day 4-7)

| 任务ID | 任务名称 | 工时 | 依赖 | 交付物 | 验收标准 |
|--------|---------|------|------|--------|---------|
| P1-3.2.1 | 实现结果评估器 | 8h | P1-3.1.2 | `internal/reflection/result_evaluator.go` | 评估准确率 > 75% |
| P1-3.2.2 | 实现错误分析器 | 8h | P1-3.1.2 | `internal/reflection/error_analyzer.go` | 支持根因定位 |
| P1-3.2.3 | 实现经验存储（基于文件） | 10h | P1-3.1.3-4 | `internal/reflection/experience_store.go` | 支持JSON存储 |
| P1-3.2.4 | 实现经验检索（相似度搜索） | 8h | P1-3.2.3 | `internal/reflection/experience_retrieval.go` | 检索延迟 < 300ms |
| P1-3.2.5 | 编写功能单元测试 | 8h | P1-3.2.1-4 | `internal/reflection/*_test.go` | 测试覆盖率 > 85% |

**小计**: 34 工时

---

#### 任务组 3.3: 自我修正机制 (Day 8-11)

| 任务ID | 任务名称 | 工时 | 依赖 | 交付物 | 验收标准 |
|--------|---------|------|------|--------|---------|
| P1-3.3.1 | 设计修正策略（重试、降级、替代） | 4h | P1-3.2.1-2 | `internal/reflection/correction_strategies.go` | 策略清晰可执行 |
| P1-3.3.2 | 实现自我修正器 | 10h | P1-3.3.1 | `internal/reflection/self_correction.go` | 修正成功率 > 60% |
| P1-3.3.3 | 实现经验应用机制 | 6h | P1-3.2.3-4 | `internal/reflection/experience_application.go` | 经验复用率 > 40% |
| P1-3.3.4 | 与 hooks 系统集成 | 8h | P1-3.3.1-3 | 修改后的 hooks 相关文件 | 集成后不影响现有功能 |
| P1-3.3.5 | 编写修正机制测试 | 6h | P1-3.3.1-4 | `internal/reflection/correction_test.go` | 覆盖各种修正场景 |

**小计**: 34 工时

---

#### 任务组 3.4: 测试与交付 (Day 12-14)

| 任务ID | 任务名称 | 工时 | 依赖 | 交付物 | 验收标准 |
|--------|---------|------|------|--------|---------|
| P1-3.4.1 | 编写集成测试 | 8h | P1-3.3.1-5 | `internal/reflection/integration_test.go` | 端到端测试通过 |
| P1-3.4.2 | 学习效果验证 | 4h | P1-3.4.1 | 验证报告 | 重复错误减少 > 30% |
| P1-3.4.3 | 编写使用文档 | 4h | P1-3.4.1 | `docs/reflection-usage.md` | 文档清晰，包含示例 |
| P1-3.4.4 | Code Review | 3h | P1-3.4.1-3 | 优化后的代码 | 代码质量符合规范 |
| P1-3.4.5 | 准备演示材料 | 3h | P1-3.4.1-4 | 演示PPT/视频 | 团队理解反思机制 |

**小计**: 22 工时

---

### 5.2 反思机制交付物清单

**代码文件**:
```
internal/reflection/
├── reflector.go            # 反思器接口
├── types.go                # 数据结构
├── result_evaluator.go     # 结果评估器
├── error_analyzer.go       # 错误分析器
├── experience.go           # 经验数据结构
├── experience_store.go     # 经验存储
├── experience_retrieval.go # 经验检索
├── experience_application.go # 经验应用
├── correction_strategies.go # 修正策略
├── self_correction.go      # 自我修正器
├── reflector_test.go       # 单元测试
├── correction_test.go      # 修正测试
└── integration_test.go     # 集成测试
```

**文档文件**:
```
docs/
└── reflection-usage.md     # 使用文档
```

**修改文件**:
- `internal/hooks/` 相关文件 - 集成反思机制

---

### 5.3 反思机制风险点

| 风险 | 影响 | 概率 | 应对措施 |
|------|------|------|---------|
| 经验存储占用空间过大 | 中 | 中 | 实现数据压缩和清理机制 |
| 自我修正效果不佳 | 中 | 中 | 提供人工干预接口 |
| 反思触发过于频繁影响性能 | 中 | 低 | 设置合理的触发阈值 |

---

## 六、阶段验收标准

### 6.1 功能验收

- [ ] 感知层支持文本输入处理，延迟 < 100ms
- [ ] 规划模块支持任务分解，成功率 > 85%
- [ ] 反思机制支持错误识别，准确率 > 75%
- [ ] 所有模块集成后不影响现有功能
- [ ] 单元测试覆盖率 > 85%

### 6.2 性能验收

- [ ] 感知层处理延迟 < 100ms
- [ ] 规划生成时间 < 500ms
- [ ] 记忆检索延迟 < 200ms
- [ ] 整体系统响应时间无明显增加

### 6.3 质量验收

- [ ] 代码符合项目规范
- [ ] 所有单元测试通过
- [ ] 所有集成测试通过
- [ ] Code Review 通过
- [ ] 文档完整清晰

---

## 七、资源需求

### 7.1 人员需求

- **开发工程师**: 1-2人（全职）
- **测试工程师**: 0.5人（兼职，参与测试设计）
- **架构师**: 0.25人（兼职，参与设计评审）

### 7.2 环境需求

- Go 1.21+ 开发环境
- 单元测试框架
- 性能测试工具
- 文档编写工具

### 7.3 工具需求

- Git 版本控制
- IDE（VSCode/GoLand）
- 测试覆盖率工具
- 性能分析工具

---

## 八、沟通计划

### 8.1 定期会议

- **每日站会**: 15分钟，同步进度和问题
- **每周评审**: 1小时，演示成果和收集反馈
- **阶段总结**: 2小时，全面验收和总结

### 8.2 汇报机制

- **日报**: 提交当日工作进度
- **周报**: 提交周度总结和下周计划
- **阶段报告**: 提交完整的阶段总结报告

---

## 九、下一步行动

### 9.1 立即行动（本周）

1. 组建开发团队，明确分工
2. 准备开发环境
3. 创建 Git 分支
4. 开始 P1-1.1.1 任务

### 9.2 准备工作

- [ ] 审查任务分解表
- [ ] 评估资源可用性
- [ ] 准备开发环境
- [ ] 设置项目管理工具
- [ ] 创建文档目录结构

---

**文档维护**:
- 更新频率: 每周更新进度
- 负责人: 项目经理/技术负责人
- 审核周期: 每周一次

**变更记录**:
- 2026-07-31: 初始版本创建