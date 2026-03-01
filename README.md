# Gatehouse

A modern web application for monitoring a driveway camera, viewing detection events, and controlling two physical gates. Replaces an older PHP/jQuery stack with a Go backend and SvelteKit frontend in a single Docker image.

## What It Does

- **Live camera feeds** — WebRTC streams from two cameras (gate, gate-rear) via Frigate's go2rtc, with sub-second latency
- **Detection events** — Browse thumbnails and video clips from Frigate's ML-powered object detection (person, car, cat, etc.), filterable by date and camera
- **Gate control** — Open, hold, and release two physical gates (Wilson and Brigman) via an Arduino controller on the local network
- **Real-time updates** — New detection events and gate status changes are pushed instantly via WebSocket (backed by MQTT from Frigate)
- **Multi-user with permissions** — Admin and neighbor users with per-gate access control. Admins can manage users and delete events.
- **Responsive** — Desktop shows dual camera feeds side-by-side; mobile uses a tabbed camera view and compact gate controls

## Architecture

```
Browser ──WebRTC──► Frigate go2rtc (live video, direct peer connection)
Browser ──HTTP/WS──► Go backend ──► Frigate REST API (events, thumbnails, clips)
                                ──► Mosquitto MQTT (real-time event subscription)
                                ──► Arduino at 10.0.1.25 (gate control)
                                ──► SQLite (users & permissions)
```

The Go backend serves the SvelteKit static frontend and provides:
- REST API for events, gates, users, and auth
- WebSocket endpoint for real-time push
- WebRTC signaling proxy (SDP offer/answer relay to go2rtc)
- JWT authentication via httpOnly cookies

## Deployment

Gatehouse runs as a single container in Kubernetes. The CI pipeline (GitHub Actions) builds and pushes to `ghcr.io/hyperbolic2346/gatehouse`.

### Prerequisites

These services must be reachable from the Gatehouse pod:

| Service | Default Address | Purpose |
|---------|----------------|---------|
| Frigate | `frigate.home.svc.cluster.local:5000` | Event API, thumbnails, clips |
| go2rtc (Frigate) | `frigate.home.svc.cluster.local:8554` | WebRTC signaling |
| Mosquitto | `mosquitto.home.svc.cluster.local:8883` | MQTT event stream |
| Arduino | `10.0.1.25` | Gate controller |

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | HTTP listen port |
| `JWT_SECRET` | *(required)* | Secret key for signing JWT tokens |
| `FRIGATE_URL` | `http://frigate.home.svc.cluster.local:5000` | Frigate API base URL |
| `MQTT_BROKER` | `tcp://mosquitto.home.svc.cluster.local:8883` | MQTT broker URL |
| `ARDUINO_ADDR` | `http://10.0.1.25` | Arduino gate controller base URL |
| `DB_PATH` | `gatehouse.db` | Path to SQLite database file |
| `FRONTEND_DIR` | `../frontend/build` | Path to static frontend files |

### Kubernetes

Manifests are in `deploy/`. Apply with kustomize:

```bash
kubectl apply -k deploy/
```

The deployment expects:
- A `gatehouse-secret` Secret in the `gate` namespace with a `jwt-secret` key
- A `local-hostpath` StorageClass for the SQLite PVC (100Mi)
- An `external` Gateway in the `network` namespace for the HTTPRoute

The HTTPRoute uses `gatehouse.${SECRET_DOMAIN}` — substitute your domain via SOPS/Flux variable substitution.

### Docker (standalone)

```bash
docker run -d \
  -p 8080:8080 \
  -e JWT_SECRET=your-secret-here \
  -e FRIGATE_URL=http://your-frigate:5000 \
  -e MQTT_BROKER=tcp://your-mqtt:1883 \
  -e ARDUINO_ADDR=http://10.0.1.25 \
  -v gatehouse-data:/data \
  -e DB_PATH=/data/gatehouse.db \
  ghcr.io/hyperbolic2346/gatehouse:latest
```

## Default Login

On first startup with an empty database, a default admin user is created:

- **Username:** `knobby`
- **Password:** `changeme`

Change this password immediately via the admin panel at `/admin`.

## Pages

| Path | Description |
|------|-------------|
| `/` | Dashboard — live feeds, gate controls, event timeline |
| `/login` | Login page |
| `/admin` | User management (admin only) |

## API Endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `POST` | `/api/login` | No | Authenticate, returns JWT cookie |
| `POST` | `/api/logout` | Yes | Clear JWT cookie |
| `GET` | `/api/me` | Yes | Current user info |
| `GET` | `/api/events?date=YYYYMMDD&camera=gate` | Yes | List detection events |
| `DELETE` | `/api/events/:id` | Admin | Delete an event |
| `GET` | `/api/events/:id/thumbnail.jpg` | Yes | Event thumbnail (proxied from Frigate) |
| `GET` | `/api/events/:id/clip.mp4` | Yes | Event video clip (proxied from Frigate) |
| `POST` | `/api/webrtc/offer?camera=gate` | Yes | WebRTC SDP offer/answer relay |
| `GET` | `/api/gates` | Yes | Gate statuses (filtered by permission) |
| `POST` | `/api/gates/:id/open` | Yes | Open a gate |
| `POST` | `/api/gates/:id/hold` | Yes | Hold a gate open |
| `POST` | `/api/gates/:id/release` | Yes | Release a held gate |
| `GET` | `/api/users` | Admin | List all users |
| `POST` | `/api/users` | Admin | Create a user |
| `PUT` | `/api/users/:id` | Admin | Update a user |
| `DELETE` | `/api/users/:id` | Admin | Delete a user |
| `WS` | `/api/ws` | Yes | WebSocket for real-time events |

## WebSocket Messages

Messages pushed from server to client:

```json
{"type": "new_event", "data": {"id": "...", "camera": "gate", "label": "person", ...}}
{"type": "event_update", "data": {"id": "...", "end_time": 1234567890}}
{"type": "gate_status", "data": [{"name": "Wilson", "id": 0, "state": "CLOSED", ...}]}
{"type": "day_rollover", "data": {"date": "20260302"}}
```

## Gate Hardware

The Arduino gate controller at `10.0.1.25` exposes:

- `GET /gate` — JSON array of gate statuses
- `PUT /gate/{id}/open` — Momentary open
- `PUT /gate/{id}/hold` — Hold open until released
- `PUT /gate/{id}/release` — Release hold

Gate 0 = Wilson, Gate 1 = Brigman.

## User Permissions

| Field | Effect |
|-------|--------|
| `role: admin` | Full access: all gates, delete events, manage users |
| `role: user` | Access only permitted gates, no delete, no user management |
| `wilson_gate` | Can see and control Wilson gate |
| `brigman_gate` | Can see and control Brigman gate |
