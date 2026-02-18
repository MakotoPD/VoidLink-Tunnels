![banner](https://i.imgur.com/JSBWYIt.png)

# VoidLink Tunnels — Backend Server

Backend service written in Go (Gin) for the VoidLink Tunnel System. Manages user accounts, authentication, and orchestrates tunnels via a **built-in custom TCP tunnel server** — no external FRP dependency required.

## Architecture

```
┌─────────────────────┐        HTTPS / REST API         ┌──────────────────────┐
│   VoidLink Client   │ ──────────────────────────────> │  Tunnel API  :8080   │
│   (Tauri Desktop)   │                                 │  (Gin HTTP)          │
│                     │    TCP control channel :7001    │                      │
│                     │ ──────────────────────────────> │  Tunnel Server       │
│                     │                                 │  (built-in Go TCP)   │
└─────────────────────┘                                 └──────────────────────┘
                                                                  │
                        Players connect directly                   │ routes traffic
                        ─────────────────────────                  ▼
                        Minecraft TCP  :25565          ┌──────────────────────┐
                        HTTP (Dynmap)  :80             │  Local Minecraft     │
                        UDP (voice)    :20000-30000    │  Server on client    │
                                                       └──────────────────────┘
```

The system consists of a single self-contained binary:
- **REST API** — user registration, login, 2FA, tunnel CRUD, password reset
- **Built-in tunnel server** — accepts client control connections, routes Minecraft TCP, HTTP and UDP traffic

No FRP, no external sidecar, no separate proxy process needed.

---

## What gets tunnelled

Each tunnel exposes three channels:

| Channel | Protocol | Public port | Purpose |
|---------|----------|-------------|---------|
| Minecraft | TCP | `25565` (shared, routed by subdomain) | Players connect to `subdomain.domain.com` |
| Web map | HTTP | `80` (shared) | Dynmap, BlueMap, etc. — `subdomain.domain.com` |
| Voice chat | UDP | Dedicated port from pool (`20000–30000`) | Simple Voice Chat, Plasmo Voice, etc. |

---

## 1. Installation: Docker (Recommended)

```bash
git clone https://github.com/MakotoPD/VoidLink-Tunnels.git
cd VoidLink-Tunnels
cp .env.example .env
# Edit .env — at minimum set JWT_SECRET and DATABASE_URL
docker-compose up -d --build
```

### Example `docker-compose.yml`

```yaml
version: '3.8'

services:
  api:
    build: .
    ports:
      - "8080:8080"    # REST API
      - "7001:7001"    # Tunnel control channel (TCP)
      - "25565:25565"  # Minecraft proxy
      - "80:80"        # HTTP proxy (Dynmap/BlueMap)
      - "20000-20100:20000-20100/udp"  # UDP voice pool (subset)
    env_file: .env
    depends_on:
      db:
        condition: service_healthy
    restart: unless-stopped

  db:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: tunnel
      POSTGRES_PASSWORD: tunnel
      POSTGRES_DB: tunneldb
    volumes:
      - db_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U tunnel"]
      interval: 5s
      timeout: 5s
      retries: 5

volumes:
  db_data:
```

---

## 2. Installation: Manual (No Docker)

### Prerequisites
- **Go 1.22+**
- **PostgreSQL** database

```bash
git clone https://github.com/MakotoPD/VoidLink-Tunnels.git
cd VoidLink-Tunnels
go mod download
go build -o tunnel-api ./cmd/server/main.go
cp .env.example .env
# Edit .env
./tunnel-api
```

On Windows:
```powershell
go build -o tunnel-api.exe ./cmd/server/main.go
.\tunnel-api.exe
```

---

## Configuration Variables

All variables can be set via `.env` file or environment variables.

| Variable | Description | Default |
|----------|-------------|---------|
| **Server** | | |
| `SERVER_PORT` | REST API port | `8080` |
| `SERVER_HOST` | Bind interface | `0.0.0.0` |
| `GIN_MODE` | Gin mode (`release`/`debug`) | `release` |
| **Database** | | |
| `DATABASE_URL` | PostgreSQL connection string | `postgres://tunnel:tunnel@localhost:5432/tunneldb` |
| **Security** | | |
| `JWT_SECRET` | Token signing secret | ❗ **REQUIRED** (min 32 chars) |
| `JWT_ACCESS_TTL` | Access token TTL (minutes) | `60` |
| `JWT_REFRESH_TTL` | Refresh token TTL (days) | `7` |
| **Tunnel Server** | | |
| `TUNNEL_PORT` | Client control connection port | `7001` |
| `MC_PROXY_PORT` | Shared Minecraft TCP listener | `25565` |
| `HTTP_PROXY_PORT` | Shared HTTP proxy listener | `80` |
| **Tunnels** | | |
| `MIN_PORT` | Start of UDP port pool | `20000` |
| `MAX_PORT` | End of UDP port pool | `30000` |
| `MAX_TUNNELS` | Max tunnels per user | `3` |
| `DOMAIN` | Base domain for subdomains | `eu.yourdomain.com` |
| `REGION` | Region identifier | `eu` |
| **SMTP (optional — password reset)** | | |
| `SMTP_HOST` | SMTP server host | — |
| `SMTP_PORT` | SMTP port | `587` |
| `SMTP_USER` | SMTP username | — |
| `SMTP_PASSWORD` | SMTP password | — |
| `SMTP_FROM` | Sender address | `noreply@yourdomain.com` |

---

## Reverse Proxy / HTTPS (nginx example)

The API runs on HTTP internally. Use nginx or Caddy for TLS termination.

```nginx
server {
    listen 443 ssl;
    server_name tunnel.yourdomain.com;

    ssl_certificate     /etc/letsencrypt/live/tunnel.yourdomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/tunnel.yourdomain.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # CORS — allow all origins (the VoidLink desktop app)
        add_header Access-Control-Allow-Origin  *;
        add_header Access-Control-Allow-Methods "GET, POST, PUT, DELETE, OPTIONS";
        add_header Access-Control-Allow-Headers "Authorization, Content-Type";

        if ($request_method = OPTIONS) {
            return 204;
        }
    }
}
```

> **Note:** The tunnel control port (`7001`) and Minecraft proxy (`25565`) are raw TCP — they must be exposed directly, not via HTTP reverse proxy.

---

## API Endpoints

### Public

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Health check |
| `GET` | `/ping` | Simple ping |
| `POST` | `/api/auth/register` | Register new account |
| `POST` | `/api/auth/login` | Login (returns access + refresh token) |
| `POST` | `/api/auth/refresh` | Refresh access token |
| `POST` | `/api/auth/logout` | Logout |
| `POST` | `/api/auth/forgot-password` | Request password reset email |
| `POST` | `/api/auth/reset-password` | Reset password with verification code |

### Protected (requires `Authorization: Bearer <token>`)

#### User

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/auth/me` | Get current user info |

#### Two-Factor Authentication

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/auth/2fa/setup` | Generate TOTP secret + QR code |
| `POST` | `/api/auth/2fa/verify` | Activate 2FA |
| `POST` | `/api/auth/2fa/disable` | Disable 2FA |

#### Tunnels

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/tunnels` | List my tunnels |
| `POST` | `/api/tunnels` | Create tunnel |
| `GET` | `/api/tunnels/:id` | Get tunnel details |
| `DELETE` | `/api/tunnels/:id` | Delete tunnel |
| `POST` | `/api/tunnels/:id/start` | Mark tunnel active + notify server |
| `POST` | `/api/tunnels/:id/stop` | Mark tunnel inactive |

---

## Tunnel Protocol

The built-in tunnel server uses a simple newline-delimited text protocol on port `7001`:

```
Client → Server:  AUTH <jwt_token> <tunnel_id>\n
Server → Client:  OK\n  |  ERROR <message>\n

Server → Client:  OPEN <conn_id> <local_port>\n   (new TCP connection to proxy)
Client → Server:  DATA <conn_id>\n                 (open data channel, then raw bytes)

Server → Client:  UDP_PKT <conn_id> <local_port> <hex_payload>\n
Client → Server:  UDP_REPLY <conn_id> <hex_payload>\n

Server ↔ Client:  PING\n / PONG\n                  (keepalive, every 30s)
```

The VoidLink desktop client (Tauri) implements this protocol natively in Rust — no external client binary needed.
