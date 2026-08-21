package background

import (
	"context"
	"sync"
	"time"
)

type TaskNavigationDirection int

const (
	NavigationUp   TaskNavigationDirection = iota
	NavigationDown
)

type BackgroundTask struct {
	ID          string
	Name        string
	Status      TaskStatus
	StartedAt   time.Time
	CompletedAt time.Time
	Output      string
	Error       string
}

type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCancelled TaskStatus = "cancelled"
)

type BackgroundTaskManager struct {
	mu        sync.RWMutex
	tasks     []*BackgroundTask
	cursorIdx int // 当前导航位置，-1 表示未选择
}

func NewBackgroundTaskManager() *BackgroundTaskManager {
	return &BackgroundTaskManager{cursorIdx: -1}
}

func (m *BackgroundTaskManager) AddTask(task *BackgroundTask) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks = append(m.tasks, task)
}

func (m *BackgroundTaskManager) GetTasks() []*BackgroundTask {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*BackgroundTask, len(m.tasks))
	copy(result, m.tasks)
	return result
}

func (m *BackgroundTaskManager) Navigate(dir TaskNavigationDirection) *BackgroundTask {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.tasks) == 0 {
		return nil
	}

	// 初始化 cursor
	if m.cursorIdx < 0 || m.cursorIdx >= len(m.tasks) {
		m.cursorIdx = len(m.tasks) - 1
		return m.tasks[m.cursorIdx]
	}

	switch dir {
	case NavigationUp:
		if m.cursorIdx > 0 {
			m.cursorIdx--
		}
	case NavigationDown:
		if m.cursorIdx < len(m.tasks)-1 {
			m.cursorIdx++
		}
	}
	return m.tasks[m.cursorIdx]
}

func (m *BackgroundTaskManager) UpdateTaskStatus(id string, status TaskStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, task := range m.tasks {
		if task.ID == id {
			task.Status = status
			if status == TaskStatusCompleted || status == TaskStatusFailed || status == TaskStatusCancelled {
				task.CompletedAt = time.Now()
			}
			return
		}
	}
}

type ScheduledTask struct {
	ID       string
	CronExpr string
	Command  string
	Enabled  bool
}

type ScheduledTaskManager struct {
	mu    sync.RWMutex
	tasks map[string]*ScheduledTask
}

func NewScheduledTaskManager() *ScheduledTaskManager {
	return &ScheduledTaskManager{
		tasks: make(map[string]*ScheduledTask),
	}
}

func (m *ScheduledTaskManager) AddTask(task *ScheduledTask) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[task.ID] = task
}

func (m *ScheduledTaskManager) RemoveTask(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tasks, id)
}

func (m *ScheduledTaskManager) GetTasks() []*ScheduledTask {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*ScheduledTask, 0, len(m.tasks))
	for _, task := range m.tasks {
		result = append(result, task)
	}
	return result
}

func (m *ScheduledTaskManager) RunPendingTasks(ctx context.Context) {
	m.mu.RLock()
	tasks := make([]*ScheduledTask, 0, len(m.tasks))
	for _, task := range m.tasks {
		if task.Enabled {
			tasks = append(tasks, task)
		}
	}
	m.mu.RUnlock()

	_ = tasks
}