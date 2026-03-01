# Contributing to Gatehouse

## Project Structure

```
gatehouse/
├── backend/                    # Go server
│   ├── cmd/gatehouse/main.go   # Entry point, config, lifecycle
│   └── internal/
│       ├── api/                # HTTP handlers (one file per resource)
│       │   ├── auth.go         # POST /api/login, GET /api/me, POST /api/logout
│       │   ├── events.go       # Frigate events: list, delete, thumbnail, clip, webrtc
│       │   ├── gates.go        # Arduino gate: status, open, hold, release
│       │   ├── users.go        # User CRUD (admin only)
│       │   ├── middleware.go   # JWT auth middleware, AdminOnly guard
│       │   └── helpers.go      # writeJSON / writeJSONError response helpers
│       ├── auth/jwt.go         # JWT sign/verify + bcrypt helpers
│       ├── db/sqlite.go        # SQLite schema, migration, User CRUD
│       ├── frigate/client.go   # HTTP client for Frigate REST API + go2rtc WebRTC
│       ├── gate/controller.go  # HTTP client for Arduino gate controller
│       ├── mqtt/subscriber.go  # MQTT subscriber for frigate/events topic
│       ├── ws/hub.go           # WebSocket hub (gorilla/websocket)
│       └── server/server.go    # HTTP router, static file serving, CORS
├── frontend/                   # SvelteKit (adapter-static)
│   └── src/
│       ├── routes/             # Pages: dashboard, login, admin
│       └── lib/
│           ├── api.ts          # Typed REST client for all API endpoints
│           ├── ws.ts           # WebSocket client with auto-reconnect
│           ├── stores/         # Svelte stores: auth, events, gates
│           └── components/     # UI components
├── deploy/                     # Kubernetes manifests (kustomize)
├── Dockerfile                  # Multi-stage: node build → go build → alpine
├── Makefile                    # dev/build/docker shortcuts
└── .github/workflows/build.yml # CI: lint, test, build, push Docker image
```

## Tech Stack

- **Backend:** Go 1.24, stdlib `net/http` (Go 1.22+ routing patterns), gorilla/websocket, paho.mqtt, modernc.org/sqlite (pure Go, no CGO)
- **Frontend:** SvelteKit 2 with adapter-static, Svelte 5 (runes), Tailwind CSS 4, TypeScript
- **Container:** Alpine-based, single binary + static files

## Development Setup

### Prerequisites

- Go 1.24+
- Node.js 22+
- (Optional) [mise](https://mise.jdx.dev/) — `mise install` will set up both from `mise.toml`

### Running Locally

Terminal 1 — backend:
```bash
cd backend
JWT_SECRET=dev-secret go run ./cmd/gatehouse
```

Terminal 2 — frontend:
```bash
cd frontend
npm install
npm run dev
```

The frontend dev server runs on `:5173` and proxies `/api/*` to the Go backend on `:8080` (configured in `vite.config.ts`). CORS is enabled in the Go server for `localhost:5173`.

Without Frigate/MQTT/Arduino available, the backend will start but log errors when trying to connect to those services. The UI will render but live feeds, events, and gate controls won't function.

### Building

```bash
# Full build (frontend + backend)
make build

# Docker image
make docker-build
```

## Backend Guide

### Routing

All routes are registered in `server/server.go` using Go 1.22+ pattern syntax:
```go
mux.Handle("POST /api/login", ...)
mux.Handle("GET /api/events/{id}/thumbnail.jpg", ...)
```

Path parameters are extracted with `r.PathValue("id")`. No external router library is used.

### Authentication Flow

1. `POST /api/login` validates credentials, issues a JWT in an httpOnly cookie named `token`
2. `AuthMiddleware` reads the cookie, validates the JWT, loads the user from SQLite, stores it in context
3. Handlers call `UserFromContext(r.Context())` to get the current user
4. `AdminOnly` middleware rejects non-admin users with 403

### Adding a New API Endpoint

1. Add the handler method to the appropriate file in `internal/api/` (or create a new file for a new resource)
2. Register the route in `server/server.go` with the correct HTTP method prefix and middleware chain
3. Use `writeJSON()` and `writeJSONError()` from `helpers.go` for responses
4. Use `r.PathValue("name")` for path parameters (Go 1.22+)

### Database

SQLite via `modernc.org/sqlite` (pure Go, no CGO). Schema is in `db/sqlite.go`. To add a migration:

1. Add the SQL to the `migrate()` function in `db/sqlite.go`
2. Use `CREATE TABLE IF NOT EXISTS` or `ALTER TABLE ... ADD COLUMN` patterns for forwards-compatibility
3. The database is a single file; path is set via `DB_PATH` env var

### External Service Clients

| Package | Service | Key Methods |
|---------|---------|-------------|
| `frigate/client.go` | Frigate NVR | `GetEvents`, `GetThumbnail`, `GetClip`, `DeleteEvent`, `ProxyWebRTCOffer` |
| `gate/controller.go` | Arduino | `GetStatus`, `Open`, `Hold`, `Release`, `StartPolling` |
| `mqtt/subscriber.go` | Mosquitto | `Start` (subscribes to `frigate/events`, broadcasts to WS hub) |

### WebSocket

`ws/hub.go` implements a broadcast-only WebSocket hub. Messages flow: MQTT → Subscriber → Hub → all connected clients. The hub also receives gate status changes from the polling loop and day-rollover events from main.go.

To broadcast a new message type:
```go
hub.BroadcastJSON(map[string]interface{}{
    "type": "your_message_type",
    "data": yourData,
})
```

## Frontend Guide

### Svelte 5 Runes

The frontend uses Svelte 5 with runes (`$state`, `$derived`, `$effect`, `$props`). Do not use the Svelte 4 reactive syntax (`$:`, `export let`).

### Stores

- `stores/auth.ts` — Current user state, login/logout, permission checks
- `stores/events.ts` — Event list for selected date, date/camera selection, WebSocket event handlers
- `stores/gates.ts` — Gate status array, updated by WebSocket

### API Client

`lib/api.ts` is a typed wrapper around `fetch`. All API calls go through it. It handles:
- JSON encoding/decoding
- Cookie-based auth (same-origin credentials)
- Redirect to `/login` on 401

### WebSocket Client

`lib/ws.ts` connects to `/api/ws`, auto-reconnects with exponential backoff, and dispatches messages to stores.

### Components

| Component | Purpose |
|-----------|---------|
| `LiveFeed.svelte` | WebRTC player for two cameras, responsive (side-by-side / tabbed) |
| `CameraToggle.svelte` | Mobile tab bar for switching cameras |
| `GateControl.svelte` | Gate status + action buttons, permission-gated |
| `EventList.svelte` | Grid of EventCards with clip modal |
| `EventCard.svelte` | Thumbnail, timestamp, label badge, admin delete button |
| `Calendar.svelte` | Date picker for browsing past days |
| `UserManager.svelte` | Admin user table with add/edit/delete |

### Adding a New Component

1. Create `frontend/src/lib/components/YourComponent.svelte`
2. Use `$props()` for inputs, `$state()` for local state
3. Import and use in the appropriate page (`routes/+page.svelte` or `routes/admin/+page.svelte`)

## CI/CD

GitHub Actions (`.github/workflows/build.yml`):
1. **backend** job — `go build`, `go test`, `go vet`
2. **frontend** job — `npm ci`, `svelte-check`, `npm run build`
3. **docker** job — Builds multi-stage Dockerfile, pushes to `ghcr.io/hyperbolic2346/gatehouse`

The Docker job only runs on pushes to main (not on PRs).

## Deployment

K8s manifests in `deploy/` are designed for use with Flux CD and SOPS variable substitution:
- `deployment.yaml` — Single replica, PVC for SQLite, env vars from Secret
- `service.yaml` — ClusterIP on port 80
- `httproute.yaml` — Routes `gatehouse.${SECRET_DOMAIN}` to the service
- `kustomization.yaml` — Ties them together

To create the required secret:
```bash
kubectl -n gate create secret generic gatehouse-secret --from-literal=jwt-secret=your-secret-here
```
