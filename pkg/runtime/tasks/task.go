package tasks

import "time"

type TaskStatus string

const (
	TaskPending    TaskStatus = "pending"
	TaskInProgress TaskStatus = "in_progress"
	TaskCompleted  TaskStatus = "completed"
	TaskBlocked    TaskStatus = "blocked"
)

type Task struct {
	ID          string     `json:"id"`
	Subject     string     `json:"subject"`
	Description string     `json:"description"`
	ActiveForm  string     `json:"activeForm"`
	Status      TaskStatus `json:"status"`
	Owner       string     `json:"owner"`
	Blocks      []string   `json:"blocks"`
	BlockedBy   []string   `json:"blockedBy"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}
