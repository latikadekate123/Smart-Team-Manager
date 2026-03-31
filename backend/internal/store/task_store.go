package store

import (
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"smartplanner/backend/internal/models"
)

type TaskStore struct {
	mu    sync.RWMutex
	tasks map[string]models.Task
}

type CreateTaskInput struct {
	TeamID        string
	Title         string
	Description   string
	Assignee      string
	Priority      string
	EstimateHours float64
	DueDate       *time.Time
}

func NewTaskStore() *TaskStore {
	return &TaskStore{tasks: make(map[string]models.Task)}
}

func (s *TaskStore) List() []models.Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]models.Task, 0, len(s.tasks))
	for _, task := range s.tasks {
		out = append(out, task)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

func (s *TaskStore) ListByTeam(teamID string) []models.Task {
	if teamID == "" {
		return []models.Task{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]models.Task, 0)
	for _, task := range s.tasks {
		if task.TeamID == teamID {
			out = append(out, task)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

func (s *TaskStore) Create(input CreateTaskInput) models.Task {
	now := time.Now().UTC()
	priority := input.Priority
	if priority == "" {
		priority = "medium"
	}
	task := models.Task{
		ID:            uuid.NewString(),
		TeamID:        input.TeamID,
		Title:         input.Title,
		Description:   input.Description,
		Subtasks:      []string{},
		Status:        "todo",
		Assignee:      input.Assignee,
		Priority:      priority,
		EstimateHours: input.EstimateHours,
		ProgressPct:   0,
		ProgressNote:  "",
		DueDate:       input.DueDate,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	s.mu.Lock()
	s.tasks[task.ID] = task
	s.mu.Unlock()
	return task
}

func (s *TaskStore) Update(task models.Task) {
	task.UpdatedAt = time.Now().UTC()
	s.mu.Lock()
	s.tasks[task.ID] = task
	s.mu.Unlock()
}

func (s *TaskStore) Get(id string) (models.Task, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	task, ok := s.tasks[id]
	return task, ok
}
