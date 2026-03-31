package models

import "time"

type Task struct {
	ID            string     `json:"id"`
	TeamID        string     `json:"teamId"`
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	Subtasks      []string   `json:"subtasks"`
	Status        string     `json:"status"`
	Assignee      string     `json:"assignee"`
	Priority      string     `json:"priority"`
	EstimateHours float64    `json:"estimateHours"`
	ProgressPct   float64    `json:"progressPct"`
	ProgressNote  string     `json:"progressNote"`
	DueDate       *time.Time `json:"dueDate"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}
