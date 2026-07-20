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
	mu    sync.RWMutex
	tasks []*BackgroundTask
}

func NewBackgroundTaskManager() *BackgroundTaskManager {
	return &BackgroundTaskManager{}
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
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.tasks) == 0 {
		return nil
	}

	switch dir {
	case NavigationUp:
		return m.tasks[len(m.tasks)-1]
	case NavigationDown:
		return m.tasks[0]
	}
	return nil
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