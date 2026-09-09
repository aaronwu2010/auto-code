# 空会话兜底：显示对话失败原因

## 问题

当对话异常结束（API 错误、MaxTurns 中止、空响应持续、token 超限等）且最后一条 assistant 消息内容为空时，前端消息区呈现空白。用户不知道对话为何停止，体验差。

### 触发空页面的典型场景

| 场景 | 后端事件 | 最后一条 assistant 消息 |
|------|----------|------------------------|
| API Key 无效 / 网络不通 | `error` (subtype=api_error) | 无（直接报错） |
| MaxTurns 耗尽 | `result` (subtype=max_turns_reached) | 可能为空或不完整 |
| 模型持续返回空响应 | `result` (subtype=empty_response_persistent) | 空 |
| 连续 tool not found | `result` (subtype=max_consecutive_tool_not_found) | 空 |
| 连续无工具调用（空转） | `result` (subtype=max_consecutive_no_any_tool) | 空 |
| 超出预算 | `result` (subtype=max_budget_usd) | 可能为空 |
| 用户手动中断 | `result` (subtype=interrupted) | 可能为空 |

## 设计

### 后端（无需改动）

后端已经通过 SDKMessage 传递了足够的信息：
- `Type: "result"`, `Subtype: <reason>` — 正常/异常结束
- `Type: "error"`, `Subtype: <api_error|system_prompt_error|...>`, `Message: <error message>` — 错误

所有 reason 值列表：
- 正常结束：`completed`, `goal_complete`, `no_follow_up_needed`
- 异常中止：`max_turns_reached`, `max_turns`, `too_many_errors`, `max_consecutive_errors`, `too_stuck`, `max_consecutive_tool_not_found`, `max_consecutive_no_any_tool`, `empty_response_persistent`, `max_output_tokens`, `max_budget_usd`, `interrupted`

### 前端（App.tsx）

新增状态：
```typescript
// 最后一次会话结束信息，用于空页面兜底
const [sessionEndInfo, setSessionEndInfo] = useState<{
  type: 'result' | 'error';
  subtype?: string;
  errorMessage?: string;       // error 事件携带的 Message.content
  isAbnormal: boolean;         // 是否为异常结束（非 completed/goal_complete）
} | null>(null);
```

更新时机：
- 收到 `result` 事件 → 记录 subtype，判断是否异常（subtype 不在 completed/goal_complete/no_follow_up_needed 中）
- 收到 `error` 事件 → 记录 error 类型和错误消息
- 收到新的 `user` 消息 → 清空 sessionEndInfo

渲染逻辑：
- 在消息列表底部、`activityLog` 之后，渲染一个"会话结束状态"卡片
- 仅当以下条件同时满足时显示：
  1. `sessionEndInfo !== null`
  2. `isLoading === false`（对话已结束）
  3. 最后一条 assistant 消息内容为空（或没有 assistant 消息）

卡片样式：
- 正常结束：绿色 emoji + 简短文字（与现有 activityLog 风格一致）
- 异常中止/错误：红色/琥珀色边框 + 图标 + 中文原因 + （可选）错误详情折叠区

## 改动范围

仅前端 `frontend/src/App.tsx`，约 3 处改动：
1. 新增 `sessionEndInfo` state
2. 在 `query:message` 事件处理中更新该 state
3. 在 JSX 消息区渲染兜底卡片
