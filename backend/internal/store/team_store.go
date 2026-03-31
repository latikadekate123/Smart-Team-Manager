package store

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Position string `json:"position"`
	Password string `json:"-"`
}

type Team struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Code        string    `json:"code"`
	Status      string    `json:"status"`
	OwnerID     string    `json:"ownerId"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Invitation struct {
	ID        string    `json:"id"`
	TeamID    string    `json:"teamId"`
	TeamName  string    `json:"teamName"`
	Email     string    `json:"email"`
	Position  string    `json:"position"`
	Status    string    `json:"status"`
	InvitedBy string    `json:"invitedBy"`
	CreatedAt time.Time `json:"createdAt"`
}

type TeamStore struct {
	mu          sync.RWMutex
	users       map[string]User
	teams       map[string]Team
	memberships map[string]map[string]bool
	teamMembers map[string]map[string]bool
	emailIndex  map[string]string
	invitations map[string]Invitation
}

var emailRx = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

func NewTeamStore() *TeamStore {
	s := &TeamStore{
		users:       make(map[string]User),
		teams:       make(map[string]Team),
		memberships: make(map[string]map[string]bool),
		teamMembers: make(map[string]map[string]bool),
		emailIndex:  make(map[string]string),
		invitations: make(map[string]Invitation),
	}

	seedUser := User{
		ID:       uuid.NewString(),
		Name:     "Latika Dekate",
		Email:    "latikadekate16@gmail.com",
		Password: "mynewproject",
		Position: "Project Lead",
	}
	s.users[seedUser.ID] = seedUser
	s.emailIndex[seedUser.Email] = seedUser.ID
	s.memberships[seedUser.ID] = make(map[string]bool)

	teams := []Team{
		{
			ID:          uuid.NewString(),
			Name:        "Aurora Platform",
			Code:        "AURORA-909",
			Status:      "ongoing",
			OwnerID:     seedUser.ID,
			Description: "Core product engineering workspace",
			CreatedAt:   time.Now().UTC(),
		},
		{
			ID:          uuid.NewString(),
			Name:        "Payments Revamp",
			Code:        "PAY-221",
			Status:      "completed",
			OwnerID:     seedUser.ID,
			Description: "Completed billing modernization project",
			CreatedAt:   time.Now().UTC().Add(-72 * time.Hour),
		},
		{
			ID:          uuid.NewString(),
			Name:        "Legacy Sunset",
			Code:        "SUNSET-404",
			Status:      "closed",
			OwnerID:     seedUser.ID,
			Description: "Archived decommissioning initiative",
			CreatedAt:   time.Now().UTC().Add(-120 * time.Hour),
		},
	}

	for _, team := range teams {
		s.teams[team.ID] = team
		s.teamMembers[team.ID] = make(map[string]bool)
		s.memberships[seedUser.ID][team.ID] = true
		s.teamMembers[team.ID][seedUser.ID] = true
	}

	randomMembers := []User{
		{ID: uuid.NewString(), Name: "Riya Shah", Email: "riya.shah@demo.local", Password: "demo123", Position: "Backend Engineer"},
		{ID: uuid.NewString(), Name: "Karan Joshi", Email: "karan.joshi@demo.local", Password: "demo123", Position: "Frontend Engineer"},
		{ID: uuid.NewString(), Name: "Nikhil Rao", Email: "nikhil.rao@demo.local", Password: "demo123", Position: "QA Engineer"},
	}
	for _, member := range randomMembers {
		s.users[member.ID] = member
		s.emailIndex[member.Email] = member.ID
		s.memberships[member.ID] = make(map[string]bool)
		s.memberships[member.ID][teams[0].ID] = true
		s.teamMembers[teams[0].ID][member.ID] = true
	}

	return s
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func validateEmail(email string) bool {
	return emailRx.MatchString(email)
}

func publicUser(user User) User {
	user.Password = ""
	return user
}

func (s *TeamStore) Signup(name, email, password, position string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(name) == "" {
		return User{}, errors.New("name required")
	}
	normalizedEmail := normalizeEmail(email)
	if !validateEmail(normalizedEmail) {
		return User{}, errors.New("invalid email")
	}
	if len(strings.TrimSpace(password)) < 6 {
		return User{}, errors.New("password must be at least 6 characters")
	}
	if _, ok := s.emailIndex[normalizedEmail]; ok {
		return User{}, errors.New("account already exists")
	}

	defaultTeamID := s.firstTeamIDByName("Aurora Platform")

	user := User{
		ID:       uuid.NewString(),
		Name:     strings.TrimSpace(name),
		Email:    normalizedEmail,
		Password: strings.TrimSpace(password),
		Position: strings.TrimSpace(position),
	}
	s.users[user.ID] = user
	s.emailIndex[normalizedEmail] = user.ID
	s.memberships[user.ID] = make(map[string]bool)
	if defaultTeamID != "" {
		s.memberships[user.ID][defaultTeamID] = true
		s.teamMembers[defaultTeamID][user.ID] = true
	}

	for inviteID, invite := range s.invitations {
		if invite.Email == normalizedEmail && invite.Status == "pending" {
			invite.Status = "accepted"
			s.invitations[inviteID] = invite
			s.memberships[user.ID][invite.TeamID] = true
			if _, ok := s.teamMembers[invite.TeamID]; !ok {
				s.teamMembers[invite.TeamID] = make(map[string]bool)
			}
			s.teamMembers[invite.TeamID][user.ID] = true
		}
	}

	return publicUser(user), nil
}

func (s *TeamStore) Login(email, password string) (User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	normalizedEmail := normalizeEmail(email)
	if !validateEmail(normalizedEmail) {
		return User{}, errors.New("invalid email")
	}
	userID, ok := s.emailIndex[normalizedEmail]
	if !ok {
		return User{}, errors.New("account not found")
	}
	user := s.users[userID]
	if user.Password != strings.TrimSpace(password) {
		return User{}, errors.New("invalid credentials")
	}
	return publicUser(user), nil
}

func (s *TeamStore) firstTeamIDByName(name string) string {
	for id, team := range s.teams {
		if team.Name == name {
			return id
		}
	}
	return ""
}

func (s *TeamStore) ListEnrolledTeams(userID string) []Team {
	s.mu.RLock()
	defer s.mu.RUnlock()

	teamIDs := s.memberships[userID]
	out := make([]Team, 0, len(teamIDs))
	for teamID := range teamIDs {
		if team, ok := s.teams[teamID]; ok {
			out = append(out, team)
		}
	}
	return out
}

func (s *TeamStore) ListInvitations(userID string) []Invitation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, ok := s.users[userID]
	if !ok {
		return []Invitation{}
	}
	out := make([]Invitation, 0)
	for _, inv := range s.invitations {
		if inv.Email == user.Email && inv.Status == "pending" {
			out = append(out, inv)
		}
	}
	return out
}

func (s *TeamStore) InviteMember(teamID, inviterID, email, position string) (Invitation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	team, ok := s.teams[teamID]
	if !ok {
		return Invitation{}, errors.New("team not found")
	}
	normEmail := normalizeEmail(email)
	if !validateEmail(normEmail) {
		return Invitation{}, errors.New("invalid email")
	}
	invite := Invitation{
		ID:        uuid.NewString(),
		TeamID:    teamID,
		TeamName:  team.Name,
		Email:     normEmail,
		Position:  strings.TrimSpace(position),
		Status:    "pending",
		InvitedBy: inviterID,
		CreatedAt: time.Now().UTC(),
	}
	s.invitations[invite.ID] = invite
	return invite, nil
}

func (s *TeamStore) AcceptInvitation(userID, inviteID string) (Team, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	invite, ok := s.invitations[inviteID]
	if !ok || invite.Status != "pending" {
		return Team{}, errors.New("invitation not found")
	}
	user, ok := s.users[userID]
	if !ok {
		return Team{}, errors.New("user not found")
	}
	if user.Email != invite.Email {
		return Team{}, errors.New("invitation does not belong to this user")
	}
	invite.Status = "accepted"
	s.invitations[inviteID] = invite

	if _, ok := s.memberships[userID]; !ok {
		s.memberships[userID] = make(map[string]bool)
	}
	s.memberships[userID][invite.TeamID] = true
	if _, ok := s.teamMembers[invite.TeamID]; !ok {
		s.teamMembers[invite.TeamID] = make(map[string]bool)
	}
	s.teamMembers[invite.TeamID][userID] = true

	team := s.teams[invite.TeamID]
	return team, nil
}

func (s *TeamStore) VerifyTeamCode(userID, teamID, code string) (Team, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	team, ok := s.teams[teamID]
	if !ok {
		return Team{}, false
	}
	normalizedInput := strings.TrimSpace(strings.ToUpper(code))
	normalizedCode := strings.TrimSpace(strings.ToUpper(team.Code))
	if normalizedInput != normalizedCode {
		parts := strings.Split(normalizedCode, "-")
		suffix := parts[len(parts)-1]
		if normalizedInput != suffix {
			return Team{}, false
		}
	}
	if normalizedInput == "" {
		return Team{}, false
	}
	if _, ok := s.memberships[userID]; !ok {
		s.memberships[userID] = make(map[string]bool)
	}
	s.memberships[userID][teamID] = true
	if _, ok := s.teamMembers[teamID]; !ok {
		s.teamMembers[teamID] = make(map[string]bool)
	}
	s.teamMembers[teamID][userID] = true
	return team, true
}

func (s *TeamStore) GetTeam(teamID string) (Team, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	team, ok := s.teams[teamID]
	return team, ok
}

func (s *TeamStore) GetUser(userID string) (User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, ok := s.users[userID]
	if !ok {
		return User{}, false
	}
	return publicUser(user), true
}

func (s *TeamStore) UserByEmail(email string) (User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	userID, ok := s.emailIndex[normalizeEmail(email)]
	if !ok {
		return User{}, false
	}
	user := s.users[userID]
	return publicUser(user), true
}

func (s *TeamStore) IsTeamOwner(teamID, userID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	team, ok := s.teams[teamID]
	if !ok {
		return false
	}
	return team.OwnerID == userID
}

func (s *TeamStore) MemberUsers(teamID string) []User {
	s.mu.RLock()
	defer s.mu.RUnlock()

	memberMap := s.teamMembers[teamID]
	out := make([]User, 0, len(memberMap))
	for userID := range memberMap {
		if user, ok := s.users[userID]; ok {
			out = append(out, publicUser(user))
		}
	}
	return out
}

func (s *TeamStore) CreateTeam(name, description, status string) Team {

	return s.CreateTeamWithOwner("", name, description, status)
}

func (s *TeamStore) CreateTeamWithOwner(ownerID, name, description, status string) Team {
	s.mu.Lock()
	defer s.mu.Unlock()
	code := fmt.Sprintf("TEAM-%s", strings.ToUpper(uuid.NewString()[:6]))
	trimmedStatus := strings.ToLower(strings.TrimSpace(status))
	if trimmedStatus == "" {
		trimmedStatus = "ongoing"
	}
	team := Team{
		ID:          uuid.NewString(),
		Name:        strings.TrimSpace(name),
		Code:        code,
		Status:      trimmedStatus,
		OwnerID:     strings.TrimSpace(ownerID),
		Description: strings.TrimSpace(description),
		CreatedAt:   time.Now().UTC(),
	}
	s.teams[team.ID] = team
	s.teamMembers[team.ID] = make(map[string]bool)
	return team
}

func (s *TeamStore) AddMemberToTeam(userID, teamID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.memberships[userID]; !ok {
		s.memberships[userID] = make(map[string]bool)
	}
	s.memberships[userID][teamID] = true
	if _, ok := s.teamMembers[teamID]; !ok {
		s.teamMembers[teamID] = make(map[string]bool)
	}
	s.teamMembers[teamID][userID] = true
}

func (s *TeamStore) OwnerTeamCodes(userID string) map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string)
	for id, team := range s.teams {
		if team.OwnerID == userID {
			out[id] = team.Code
		}
	}
	return out
}
