package task

import (
	"context"
	"time"

	"github.com/auto-code/auto-code/internal/types"
)

type TaskType string

const (
	TaskTypeLocalShell  TaskType = "local_shell"
	TaskTypeLocalAgent  TaskType = "local_agent"
	TaskTypeRemoteAgent TaskType = "remote_agent"
	TaskTypeTeammate    TaskType = "teammate"
	TaskTypeDream       TaskType = "dream"
)

type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusCompleted  TaskStatus = "completed"
	TaskStatusFailed     TaskStatus = "failed"
	TaskStatusStopped    TaskStatus = "stopped"
)

func IsTerminalTaskStatus(status TaskStatus) bool {
	return status == TaskStatusCompleted || status == TaskStatusFailed || status == TaskStatusStopped
}

type TaskState struct {
	TaskID      types.TaskID `json:"task_id"`
	Type        TaskType     `json:"type"`
	Status      TaskStatus   `json:"status"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	ActiveForm  string       `json:"active_form,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
	AgentID     types.AgentID `json:"agent_id,omitempty"`
	Metadata    any          `json:"metadata,omitempty"`
	BlockedBy   []types.TaskID `json:"blocked_by,omitempty"`
}

type Task interface {
	Type() TaskType
	State() *TaskState
	Execute(ctx context.Context) error
	Cancel() error
	Output(ctx context.Context) (string, error)
}

type TaskRegistry struct {
	tasks map[types.TaskID]Task
}

func NewTaskRegistry() *TaskRegistry {
	return &TaskRegistry{
		tasks: make(map[types.TaskID]Task),
	}
}

func (r *TaskRegistry) Register(task Task) {
	r.tasks[task.State().TaskID] = task
}

func (r *TaskRegistry) Get(taskID types.TaskID) Task {
	return r.tasks[taskID]
}

func (r *TaskRegistry) List() []Task {
	result := make([]Task, 0, len(r.tasks))
	for _, t := range r.tasks {
		result = append(result, t)
	}
	return result
}

func (r *TaskRegistry) Remove(taskID types.TaskID) {
	delete(r.tasks, taskID)
}