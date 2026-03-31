package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"smartplanner/backend/internal/ai"
	"smartplanner/backend/internal/config"
	"smartplanner/backend/internal/notify"
	"smartplanner/backend/internal/store"
	"smartplanner/backend/internal/ws"
)

type apiServer struct {
	cfg      config.Config
	store    *store.TaskStore
	teams    *store.TeamStore
	aiClient *ai.Client
	notifier *notify.Notifier
	hub      *ws.Hub
	db       *pgxpool.Pool
}

func main() {
	cfg := config.Load()
	ctx := context.Background()

	var redisClient *redis.Client
	if cfg.RedisAddr != "" {
		redisClient = redis.NewClient(&redis.Options{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
		if err := redisClient.Ping(ctx).Err(); err != nil {
			log.Printf("redis unavailable, continuing without pub/sub: %v", err)
			redisClient = nil
		}
	}

	var dbPool *pgxpool.Pool
	if cfg.PostgresURL != "" {
		pool, err := pgxpool.New(ctx, cfg.PostgresURL)
		if err != nil {
			log.Printf("postgres pool init failed: %v", err)
		} else {
			dbPool = pool
			if err := pool.Ping(ctx); err != nil {
				log.Printf("postgres ping failed at startup, readiness will retry: %v", err)
			}
		}
	}

	hub := ws.NewHub(redisClient)
	hub.StartRedisSubscriber(ctx)

	s := &apiServer{
		cfg:      cfg,
		store:    store.NewTaskStore(),
		teams:    store.NewTeamStore(),
		aiClient: ai.NewClient(cfg.OpenAIAPIKey, cfg.OpenAIModel, cfg.OpenAIBaseURL),
		notifier: notify.NewNotifier(cfg.SlackWebhookURL, cfg.DiscordWebhookURL),
		hub:      hub,
		db:       dbPool,
	}
	s.seedDemoData()

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: cfg.AllowedOrigins,
		AllowedMethods: []string{"GET", "POST", "PATCH", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type"},
		MaxAge:         300,
	}))

	r.Get("/health/live", s.live)
	r.Get("/health/ready", s.ready)

	r.Route("/api", func(r chi.Router) {
		r.Post("/auth/signup", s.authSignup)
		r.Post("/auth/login", s.authLogin)
		r.Post("/users/register", s.registerUser)
		r.Get("/teams/enrolled", s.enrolledTeams)
		r.Get("/admin/team-codes", s.ownerTeamCodes)
		r.Post("/teams/create", s.createTeam)
		r.Post("/teams/verify-code", s.verifyTeamCode)
		r.Get("/teams/{teamId}/members", s.teamMembers)
		r.Post("/teams/invite", s.inviteMember)
		r.Get("/teams/invitations", s.listInvitations)
		r.Post("/teams/invitations/{inviteId}/accept", s.acceptInvitation)
		r.Get("/teams/{teamId}/completion", s.teamCompletion)
		r.Get("/tasks", s.listTasks)
		r.Get("/metrics", s.metrics)
		r.Post("/tasks", s.createTask)
		r.Patch("/tasks/{id}", s.patchTask)
		r.Post("/tasks/{id}/generate-subtasks", s.generateSubtasks)
		r.Post("/nightly/summary", s.nightlySummary)
	})

	r.Get("/ws/crdt", s.crdtSocket)

	addr := ":" + cfg.Port
	log.Printf("smart planner api listening on %s", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatal(err)
	}
}

func (s *apiServer) live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "alive"})
}

func (s *apiServer) ready(w http.ResponseWriter, _ *http.Request) {
	if s.cfg.PostgresURL != "" && s.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "db-not-ready"})
		return
	}
	if s.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.db.Ping(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "db-unreachable"})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func requireTeamID(w http.ResponseWriter, r *http.Request) (string, bool) {
	teamID := strings.TrimSpace(r.URL.Query().Get("teamId"))
	if teamID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "teamId required"})
		return "", false
	}
	return teamID, true
}

func (s *apiServer) authSignup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Position string `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	user, err := s.teams.Signup(req.Name, req.Email, req.Password, req.Position)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	teams := s.teams.ListEnrolledTeams(user.ID)
	invites := s.teams.ListInvitations(user.ID)
	writeJSON(w, http.StatusCreated, map[string]any{"user": user, "teams": teams, "invitations": invites})
}

func (s *apiServer) authLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	user, err := s.teams.Login(req.Email, req.Password)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	teams := s.teams.ListEnrolledTeams(user.ID)
	invites := s.teams.ListInvitations(user.ID)
	writeJSON(w, http.StatusOK, map[string]any{"user": user, "teams": teams, "invitations": invites})
}

func (s *apiServer) registerUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Position string `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if strings.TrimSpace(req.Password) == "" {
		req.Password = "welcome123"
	}
	user, err := s.teams.Signup(req.Name, req.Email, req.Password, req.Position)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	teams := s.teams.ListEnrolledTeams(user.ID)
	writeJSON(w, http.StatusOK, map[string]any{"user": user, "teams": teams})
}

func (s *apiServer) enrolledTeams(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.URL.Query().Get("userId"))
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "userId required"})
		return
	}
	writeJSON(w, http.StatusOK, s.teams.ListEnrolledTeams(userID))
}

func (s *apiServer) createTeam(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID      string `json:"userId"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Status      string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if strings.TrimSpace(req.UserID) == "" || strings.TrimSpace(req.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "userId and name required"})
		return
	}
	team := s.teams.CreateTeamWithOwner(req.UserID, req.Name, req.Description, req.Status)
	s.teams.AddMemberToTeam(req.UserID, team.ID)
	writeJSON(w, http.StatusCreated, team)
}

func (s *apiServer) ownerTeamCodes(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.URL.Query().Get("userId"))
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "userId required"})
		return
	}
	writeJSON(w, http.StatusOK, s.teams.OwnerTeamCodes(userID))
}

func (s *apiServer) inviteMember(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TeamID    string `json:"teamId"`
		InviterID string `json:"inviterId"`
		Email     string `json:"email"`
		Position  string `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	invite, err := s.teams.InviteMember(req.TeamID, req.InviterID, req.Email, req.Position)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, invite)
}

func (s *apiServer) listInvitations(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.URL.Query().Get("userId"))
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "userId required"})
		return
	}
	writeJSON(w, http.StatusOK, s.teams.ListInvitations(userID))
}

func (s *apiServer) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	inviteID := chi.URLParam(r, "inviteId")
	var req struct {
		UserID string `json:"userId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	team, err := s.teams.AcceptInvitation(req.UserID, inviteID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, team)
}

func (s *apiServer) verifyTeamCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"userId"`
		TeamID string `json:"teamId"`
		Code   string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	team, ok := s.teams.VerifyTeamCode(req.UserID, req.TeamID, req.Code)
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "invalid team code"})
		return
	}
	writeJSON(w, http.StatusOK, team)
}

func (s *apiServer) teamCompletion(w http.ResponseWriter, r *http.Request) {
	teamID := chi.URLParam(r, "teamId")
	if _, ok := s.teams.GetTeam(teamID); !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "team not found"})
		return
	}

	members := s.teams.MemberUsers(teamID)
	tasks := s.store.ListByTeam(teamID)

	type completion struct {
		Name           string  `json:"name"`
		Position       string  `json:"position"`
		TotalAssigned  int     `json:"totalAssigned"`
		Done           int     `json:"done"`
		CompletionRate float64 `json:"completionRate"`
	}

	out := make([]completion, 0, len(members))
	for _, member := range members {
		total := 0
		done := 0
		for _, task := range tasks {
			if strings.EqualFold(strings.TrimSpace(task.Assignee), strings.TrimSpace(member.Name)) {
				total++
				if strings.EqualFold(strings.TrimSpace(task.Status), "done") {
					done++
				}
			}
		}
		rate := 0.0
		if total > 0 {
			rate = (float64(done) / float64(total)) * 100
		}
		out = append(out, completion{
			Name:           member.Name,
			Position:       member.Position,
			TotalAssigned:  total,
			Done:           done,
			CompletionRate: rate,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})

	writeJSON(w, http.StatusOK, out)
}

func (s *apiServer) teamMembers(w http.ResponseWriter, r *http.Request) {
	teamID := chi.URLParam(r, "teamId")
	if _, ok := s.teams.GetTeam(teamID); !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "team not found"})
		return
	}
	writeJSON(w, http.StatusOK, s.teams.MemberUsers(teamID))
}

func (s *apiServer) listTasks(w http.ResponseWriter, r *http.Request) {
	teamID, ok := requireTeamID(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.store.ListByTeam(teamID))
}

func (s *apiServer) createTask(w http.ResponseWriter, r *http.Request) {
	teamID, ok := requireTeamID(w, r)
	if !ok {
		return
	}
	actorID := strings.TrimSpace(r.URL.Query().Get("userId"))
	if actorID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "userId required"})
		return
	}
	if !s.teams.IsTeamOwner(teamID, actorID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only team lead can assign tasks"})
		return
	}

	var req struct {
		Title         string  `json:"title"`
		Description   string  `json:"description"`
		Subtasks      []string `json:"subtasks"`
		Assignee      string  `json:"assignee"`
		Priority      string  `json:"priority"`
		EstimateHours float64 `json:"estimateHours"`
		DueDate       string  `json:"dueDate"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title required"})
		return
	}
	var dueDate *time.Time
	if trimmed := strings.TrimSpace(req.DueDate); trimmed != "" {
		parsed, err := time.Parse(time.RFC3339, trimmed)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "dueDate must be RFC3339"})
			return
		}
		u := parsed.UTC()
		dueDate = &u
	}

	task := s.store.Create(store.CreateTaskInput{
		TeamID:        teamID,
		Title:         req.Title,
		Description:   req.Description,
		Assignee:      strings.TrimSpace(req.Assignee),
		Priority:      strings.ToLower(strings.TrimSpace(req.Priority)),
		EstimateHours: req.EstimateHours,
		DueDate:       dueDate,
	})
	if req.Subtasks != nil {
		sanitized := make([]string, 0, len(req.Subtasks))
		for _, item := range req.Subtasks {
			trimmed := strings.TrimSpace(item)
			if trimmed != "" {
				sanitized = append(sanitized, trimmed)
			}
		}
		task.Subtasks = sanitized
		s.store.Update(task)
	}
	writeJSON(w, http.StatusCreated, task)
}

func (s *apiServer) patchTask(w http.ResponseWriter, r *http.Request) {
	teamID, ok := requireTeamID(w, r)
	if !ok {
		return
	}

	id := chi.URLParam(r, "id")
	task, ok := s.store.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	if task.TeamID != teamID {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "task belongs to a different team"})
		return
	}
	actorID := strings.TrimSpace(r.URL.Query().Get("userId"))
	if actorID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "userId required"})
		return
	}
	actorUser, actorExists := s.teams.GetUser(actorID)
	if !actorExists {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	isOwner := s.teams.IsTeamOwner(teamID, actorID)

	var req struct {
		Title         *string  `json:"title"`
		Description   *string  `json:"description"`
		Subtasks      []string `json:"subtasks"`
		Status        *string  `json:"status"`
		Assignee      *string  `json:"assignee"`
		Priority      *string  `json:"priority"`
		EstimateHours *float64 `json:"estimateHours"`
		ProgressPct   *float64 `json:"progressPct"`
		ProgressNote  *string  `json:"progressNote"`
		DueDate       *string  `json:"dueDate"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.Title != nil {
		task.Title = *req.Title
	}
	if req.Description != nil {
		task.Description = *req.Description
	}
	if req.Subtasks != nil {
		task.Subtasks = req.Subtasks
	}
	if req.Status != nil {
		task.Status = *req.Status
	}
	if req.Assignee != nil {
		if !isOwner {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "only team lead can reassign tasks"})
			return
		}
		task.Assignee = strings.TrimSpace(*req.Assignee)
	}
	if req.Priority != nil {
		if !isOwner {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "only team lead can change priority"})
			return
		}
		task.Priority = strings.ToLower(strings.TrimSpace(*req.Priority))
	}
	if req.EstimateHours != nil {
		if !isOwner {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "only team lead can edit estimates"})
			return
		}
		task.EstimateHours = *req.EstimateHours
	}
	if req.ProgressPct != nil || req.ProgressNote != nil {
		assigneeMatch := strings.EqualFold(strings.TrimSpace(task.Assignee), strings.TrimSpace(actorUser.Name)) ||
			strings.EqualFold(strings.TrimSpace(task.Assignee), strings.TrimSpace(actorUser.Email))
		if !isOwner && !assigneeMatch {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "only assignee or team lead can update progress"})
			return
		}
		if req.ProgressPct != nil {
			if *req.ProgressPct < 0 || *req.ProgressPct > 100 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "progressPct must be between 0 and 100"})
				return
			}
			task.ProgressPct = *req.ProgressPct
		}
		if req.ProgressNote != nil {
			task.ProgressNote = strings.TrimSpace(*req.ProgressNote)
		}
	}
	if req.DueDate != nil {
		if strings.TrimSpace(*req.DueDate) == "" {
			task.DueDate = nil
		} else {
			parsed, err := time.Parse(time.RFC3339, *req.DueDate)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "dueDate must be RFC3339"})
				return
			}
			u := parsed.UTC()
			task.DueDate = &u
		}
	}
	s.store.Update(task)
	writeJSON(w, http.StatusOK, task)
}

func (s *apiServer) metrics(w http.ResponseWriter, r *http.Request) {
	teamID, ok := requireTeamID(w, r)
	if !ok {
		return
	}
	tasks := s.store.ListByTeam(teamID)
	now := time.Now().UTC()

	total := len(tasks)
	statusCounts := map[string]int{
		"todo":        0,
		"in-progress": 0,
		"blocked":     0,
		"done":        0,
	}
	overdue := 0
	dueSoon := 0
	workloadByAssignee := map[string]float64{}

	for _, task := range tasks {
		status := strings.ToLower(strings.TrimSpace(task.Status))
		if _, ok := statusCounts[status]; !ok {
			statusCounts[status] = 0
		}
		statusCounts[status]++

		if task.DueDate != nil && status != "done" {
			if task.DueDate.Before(now) {
				overdue++
			} else if task.DueDate.Before(now.Add(48 * time.Hour)) {
				dueSoon++
			}
		}

		if status != "done" && task.EstimateHours > 0 {
			assignee := strings.TrimSpace(task.Assignee)
			if assignee == "" {
				assignee = "unassigned"
			}
			workloadByAssignee[assignee] += task.EstimateHours
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"total":              total,
		"statusCounts":       statusCounts,
		"overdue":            overdue,
		"dueSoon":            dueSoon,
		"workloadByAssignee": workloadByAssignee,
	})
}

func (s *apiServer) generateSubtasks(w http.ResponseWriter, r *http.Request) {
	teamID, ok := requireTeamID(w, r)
	if !ok {
		return
	}

	id := chi.URLParam(r, "id")
	task, ok := s.store.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	if task.TeamID != teamID {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "task belongs to a different team"})
		return
	}
	subtasks, err := s.aiClient.GenerateSubtasks(task.Title, task.Description)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	task.Subtasks = subtasks
	s.store.Update(task)
	writeJSON(w, http.StatusOK, map[string]any{"id": task.ID, "subtasks": subtasks})
}

func (s *apiServer) nightlySummary(w http.ResponseWriter, _ *http.Request) {
	tasks := s.store.List()
	total := len(tasks)
	done := 0
	inProgress := 0
	for _, t := range tasks {
		switch strings.ToLower(t.Status) {
		case "done":
			done++
		case "in-progress":
			inProgress++
		}
	}
	overdue := 0
	for _, t := range tasks {
		if t.DueDate != nil && strings.ToLower(t.Status) != "done" && t.DueDate.Before(time.Now().UTC()) {
			overdue++
		}
	}
	message := fmt.Sprintf("Daily Smart Planner Summary: total=%d, done=%d, in-progress=%d, remaining=%d, overdue=%d", total, done, inProgress, total-done, overdue)
	if err := s.notifier.Broadcast(message); err != nil {
		log.Printf("nightly notify failed: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]string{"summary": message})
}

func (s *apiServer) seedDemoData() {
	user, err := s.teams.Login("latikadekate16@gmail.com", "mynewproject")
	if err != nil {
		return
	}
	teams := s.teams.ListEnrolledTeams(user.ID)
	for _, team := range teams {
		if len(s.store.ListByTeam(team.ID)) > 0 {
			continue
		}
		seed := []store.CreateTaskInput{
			{
				TeamID:        team.ID,
				Title:         "Architecture review and milestone plan",
				Description:   "",
				Assignee:      "Latika Dekate",
				Priority:      "high",
				EstimateHours: 8,
			},
			{
				TeamID:        team.ID,
				Title:         "API gateway hardening",
				Description:   "",
				Assignee:      "Riya Shah",
				Priority:      "critical",
				EstimateHours: 12,
			},
			{
				TeamID:        team.ID,
				Title:         "Frontend release checklist",
				Description:   "",
				Assignee:      "Karan Joshi",
				Priority:      "medium",
				EstimateHours: 6,
			},
		}
		for i, input := range seed {
			task := s.store.Create(input)
			if team.Status == "completed" || (team.Status == "ongoing" && i == 0) {
				done := "done"
				task.Status = done
				task.ProgressPct = 100
				task.ProgressNote = "Completed with review sign-off"
				s.store.Update(task)
			}
			if team.Status == "closed" {
				task.Status = "done"
				task.ProgressPct = 100
				task.ProgressNote = "Archived project output"
				s.store.Update(task)
			}
			if team.Status == "ongoing" && i == 1 {
				task.ProgressPct = 55
				task.ProgressNote = ""
				s.store.Update(task)
			}
		}
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (s *apiServer) crdtSocket(w http.ResponseWriter, r *http.Request) {
	boardID := strings.TrimSpace(r.URL.Query().Get("boardId"))
	if boardID == "" {
		boardID = "default"
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade failed: %v", err)
		return
	}
	s.hub.Register(boardID, conn)
	defer func() {
		s.hub.Unregister(boardID, conn)
		_ = conn.Close()
	}()

	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.BinaryMessage && messageType != websocket.TextMessage {
			continue
		}
		s.hub.PublishUpdate(context.Background(), boardID, payload, conn)
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
