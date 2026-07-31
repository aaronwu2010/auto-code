package perception

import (
	"time"
)

// Context 表示感知层的上下文信息
type Context struct {
	// 用户信息
	User        *UserContext        `json:"user,omitempty"`        // 用户上下文
	Session     *SessionContext     `json:"session,omitempty"`     // 会话上下文
	Environment *EnvironmentContext `json:"environment,omitempty"` // 环境上下文

	// 项目信息
	Project   *ProjectContext   `json:"project,omitempty"`   // 项目上下文
	Workspace *WorkspaceContext `json:"workspace,omitempty"` // 工作空间上下文

	// 历史信息
	History *HistoryContext `json:"history,omitempty"` // 历史上下文

	// 元数据
	Metadata  map[string]interface{} `json:"metadata,omitempty"` // 自定义元数据
	CreatedAt time.Time              `json:"created_at"`         // 创建时间
	UpdatedAt time.Time              `json:"updated_at"`         // 更新时间
}

// UserContext 用户上下文
type UserContext struct {
	ID          string                 `json:"id,omitempty"`          // 用户ID
	Name        string                 `json:"name,omitempty"`        // 用户名
	Email       string                 `json:"email,omitempty"`       // 邮箱
	Role        string                 `json:"role,omitempty"`        // 角色
	Preferences map[string]interface{} `json:"preferences,omitempty"` // 偏好设置

	// 权限信息
	Permissions []string `json:"permissions,omitempty"` // 权限列表
	Groups      []string `json:"groups,omitempty"`      // 用户组

	// 活动信息
	LastActive   time.Time `json:"last_active,omitempty"`   // 最后活动时间
	SessionCount int       `json:"session_count,omitempty"` // 会话计数
}

// SessionContext 会话上下文
type SessionContext struct {
	ID        string        `json:"id,omitempty"`         // 会话ID
	StartTime time.Time     `json:"start_time,omitempty"` // 开始时间
	EndTime   *time.Time    `json:"end_time,omitempty"`   // 结束时间
	Duration  time.Duration `json:"duration,omitempty"`   // 持续时间

	// 会话状态
	Status string `json:"status,omitempty"` // 状态
	Mode   string `json:"mode,omitempty"`   // 模式（交互、批处理等）

	// 会话统计
	MessageCount  int `json:"message_count,omitempty"`   // 消息计数
	ToolCallCount int `json:"tool_call_count,omitempty"` // 工具调用计数

	// 会话配置
	Config   map[string]interface{} `json:"config,omitempty"`   // 配置参数
	Settings map[string]interface{} `json:"settings,omitempty"` // 设置项
}

// EnvironmentContext 环境上下文
type EnvironmentContext struct {
	// 系统信息
	OS       string `json:"os,omitempty"`       // 操作系统
	Arch     string `json:"arch,omitempty"`     // 架构
	Hostname string `json:"hostname,omitempty"` // 主机名

	// 运行环境
	Runtime     string `json:"runtime,omitempty"`     // 运行时（Go版本）
	Version     string `json:"version,omitempty"`     // 应用版本
	Environment string `json:"environment,omitempty"` // 环境标识（dev/prod）

	// 资源信息
	CPU       int    `json:"cpu,omitempty"`        // CPU核心数
	Memory    uint64 `json:"memory,omitempty"`     // 内存大小
	DiskSpace uint64 `json:"disk_space,omitempty"` // 磁盘空间

	// 网络信息
	IPAddress string `json:"ip_address,omitempty"` // IP地址
	Port      int    `json:"port,omitempty"`       // 端口号

	// 环境变量
	Variables map[string]string `json:"variables,omitempty"` // 环境变量

	// 时间信息
	Timezone    string    `json:"timezone,omitempty"`     // 时区
	CurrentTime time.Time `json:"current_time,omitempty"` // 当前时间
}

// ProjectContext 项目上下文
type ProjectContext struct {
	ID   string `json:"id,omitempty"`   // 项目ID
	Name string `json:"name,omitempty"` // 项目名称
	Path string `json:"path,omitempty"` // 项目路径
	Type string `json:"type,omitempty"` // 项目类型

	// 项目配置
	Language  string `json:"language,omitempty"`   // 主要语言
	Framework string `json:"framework,omitempty"`  // 框架
	BuildTool string `json:"build_tool,omitempty"` // 构建工具

	// Git 信息
	Repository *GitContext `json:"repository,omitempty"` // Git上下文

	// 项目设置
	Config  map[string]interface{} `json:"config,omitempty"`  // 项目配置
	Exclude []string               `json:"exclude,omitempty"` // 排除目录
	Include []string               `json:"include,omitempty"` // 包含目录

	// 项目统计
	FileCount int   `json:"file_count,omitempty"` // 文件计数
	LineCount int   `json:"line_count,omitempty"` // 代码行数
	Size      int64 `json:"size,omitempty"`       // 项目大小
}

// GitContext Git上下文
type GitContext struct {
	Branch    string    `json:"branch,omitempty"`    // 分支名
	Commit    string    `json:"commit,omitempty"`    // 提交哈希
	Tag       string    `json:"tag,omitempty"`       // 标签
	Remote    string    `json:"remote,omitempty"`    // 远程地址
	Status    string    `json:"status,omitempty"`    // 状态（clean/dirty）
	Author    string    `json:"author,omitempty"`    // 作者
	Message   string    `json:"message,omitempty"`   // 提交消息
	Timestamp time.Time `json:"timestamp,omitempty"` // 提交时间
}

// WorkspaceContext 工作空间上下文
type WorkspaceContext struct {
	Path string `json:"path,omitempty"` // 工作空间路径
	Name string `json:"name,omitempty"` // 工作空间名称

	// 工作空间配置
	Config   map[string]interface{} `json:"config,omitempty"`   // 配置
	Settings map[string]interface{} `json:"settings,omitempty"` // 设置

	// 打开的文件
	OpenFiles  []string `json:"open_files,omitempty"`  // 打开的文件列表
	ActiveFile string   `json:"active_file,omitempty"` // 当前活动文件

	// 工作空间状态
	LastAccessed time.Time `json:"last_accessed,omitempty"` // 最后访问时间
	AccessCount  int       `json:"access_count,omitempty"`  // 访问计数
}

// HistoryContext 历史上下文
type HistoryContext struct {
	// 最近的消息
	RecentMessages []MessageRecord `json:"recent_messages,omitempty"` // 最近消息

	// 工具调用历史
	ToolCalls []ToolCallRecord `json:"tool_calls,omitempty"` // 工具调用记录

	// 决策历史
	Decisions []DecisionRecord `json:"decisions,omitempty"` // 决策记录

	// 错误历史
	Errors []ErrorRecord `json:"errors,omitempty"` // 错误记录

	// 统计信息
	TotalMessages  int `json:"total_messages,omitempty"`   // 总消息数
	TotalToolCalls int `json:"total_tool_calls,omitempty"` // 总工具调用数
	TotalErrors    int `json:"total_errors,omitempty"`     // 总错误数
}

// MessageRecord 消息记录
type MessageRecord struct {
	ID        string        `json:"id,omitempty"`
	Role      string        `json:"role,omitempty"`
	Content   string        `json:"content,omitempty"`
	Timestamp time.Time     `json:"timestamp,omitempty"`
	Duration  time.Duration `json:"duration,omitempty"`
}

// ToolCallRecord 工具调用记录
type ToolCallRecord struct {
	ID        string        `json:"id,omitempty"`
	ToolName  string        `json:"tool_name,omitempty"`
	Input     string        `json:"input,omitempty"`
	Output    string        `json:"output,omitempty"`
	Success   bool          `json:"success,omitempty"`
	Duration  time.Duration `json:"duration,omitempty"`
	Timestamp time.Time     `json:"timestamp,omitempty"`
}

// DecisionRecord 决策记录
type DecisionRecord struct {
	ID          string    `json:"id,omitempty"`
	Type        string    `json:"type,omitempty"`
	Description string    `json:"description,omitempty"`
	Rationale   string    `json:"rationale,omitempty"`
	Timestamp   time.Time `json:"timestamp,omitempty"`
}

// ErrorRecord 错误记录
type ErrorRecord struct {
	ID        string    `json:"id,omitempty"`
	Type      string    `json:"type,omitempty"`
	Message   string    `json:"message,omitempty"`
	Stack     string    `json:"stack,omitempty"`
	Recovered bool      `json:"recovered,omitempty"`
	Timestamp time.Time `json:"timestamp,omitempty"`
}

// NewContext 创建新的上下文
func NewContext() *Context {
	now := time.Now()
	return &Context{
		Metadata:  make(map[string]interface{}),
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// WithUser 设置用户上下文
func (c *Context) WithUser(user *UserContext) *Context {
	c.User = user
	c.UpdatedAt = time.Now()
	return c
}

// WithSession 设置会话上下文
func (c *Context) WithSession(session *SessionContext) *Context {
	c.Session = session
	c.UpdatedAt = time.Now()
	return c
}

// WithEnvironment 设置环境上下文
func (c *Context) WithEnvironment(env *EnvironmentContext) *Context {
	c.Environment = env
	c.UpdatedAt = time.Now()
	return c
}

// WithProject 设置项目上下文
func (c *Context) WithProject(project *ProjectContext) *Context {
	c.Project = project
	c.UpdatedAt = time.Now()
	return c
}

// Clone 克隆上下文
func (c *Context) Clone() *Context {
	clone := &Context{
		CreatedAt: c.CreatedAt,
		UpdatedAt: time.Now(),
	}

	if c.User != nil {
		clone.User = &UserContext{
			ID:          c.User.ID,
			Name:        c.User.Name,
			Email:       c.User.Email,
			Role:        c.User.Role,
			Preferences: copyMap(c.User.Preferences),
			Permissions: copyStringSlice(c.User.Permissions),
			Groups:      copyStringSlice(c.User.Groups),
		}
	}

	if c.Session != nil {
		clone.Session = &SessionContext{
			ID:           c.Session.ID,
			StartTime:    c.Session.StartTime,
			Duration:     c.Session.Duration,
			Status:       c.Session.Status,
			Mode:         c.Session.Mode,
			MessageCount: c.Session.MessageCount,
		}
	}

	if c.Environment != nil {
		clone.Environment = &EnvironmentContext{
			OS:          c.Environment.OS,
			Arch:        c.Environment.Arch,
			Hostname:    c.Environment.Hostname,
			Runtime:     c.Environment.Runtime,
			Version:     c.Environment.Version,
			Environment: c.Environment.Environment,
		}
	}

	if c.Project != nil {
		clone.Project = &ProjectContext{
			ID:        c.Project.ID,
			Name:      c.Project.Name,
			Path:      c.Project.Path,
			Type:      c.Project.Type,
			Language:  c.Project.Language,
			Framework: c.Project.Framework,
		}
	}

	clone.Metadata = copyMap(c.Metadata)

	return clone
}

// Helper functions
func copyMap(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return nil
	}
	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func copyStringSlice(src []string) []string {
	if src == nil {
		return nil
	}
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}
