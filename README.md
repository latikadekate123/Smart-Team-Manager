# Smart Planner Demo

A team planning app with role-based workflow:

- Team lead creates and assigns tasks
- Members update progress on their own assigned tasks
- Team dashboard shows status and total progress

The app is container-first and easy to run locally.

## What It Does

- Secure team entry with team code verification
- Team-based workspace separation
- Lead-only task assignment
- Member-only task update flow
- Real-time collaborative description editing via WebSocket + Yjs
- AI subtask generation endpoint (optional API key)
- Health checks for backend

## Tech Stack

- Frontend: React + Vite
- Backend: Go + Chi + Gorilla WebSocket
- Data/Infra: In-memory stores, Redis fan-out, Postgres service
- Runtime: Docker Compose
- Deployment templates: Kubernetes manifests + Terraform starter

## Project Structure

- `frontend/` UI app
- `backend/` API + WebSocket service
- `k8s/` Kubernetes manifests
- `infra/terraform/` Terraform baseline

## Quick Start (Recommended)

From repository root:

```bash
docker compose up -d --build
```

Open:

- App: http://localhost:5173
- API: http://localhost:8080

To stop:

```bash
docker compose down
```

## Login (Demo)

Lead account:

- Email: `latikadekate16@gmail.com`
- Password: `mynewproject`

Member account:

- Email: `karan.joshi@demo.local`
- Password: `demo123`

## How Roles Work

- If you log in as `Project Lead` (and you are team owner), you get:
  - Task Assignment section
  - Full team task visibility
- If you log in as `Member`, you get:
  - Task Update section only
  - Only tasks assigned to you
