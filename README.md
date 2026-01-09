# Tunnel API - VoidLink Backend

Backend service written in Go (Gin) for the VoidLink Tunnel System. This API manages user accounts, authenticates users, and orchestrates tunnel creation by communicating with an FRP (Fast Reverse Proxy) server.

## Architecture

The system consists of two main components:
1.  **Tunnel API (This Service)**: Handles user logic, database, and generates configs for clients.
2.  **FRP Server (External)**: The actual reverse proxy that tunnels traffic. You MUST have an FRP server running for this API to function correctly.

---

## 1. FRP Server Configuration (REQUIRED)

Before installing the API, you must set up an `frps` (FRP Server) instance on a server with a public IP.

1.  Download `frp` from [GitHub Releases](https://github.com/fatedier/frp/releases).
2.  Create a configuration file `frps.toml` on your server:

```toml
# /etc/frp/frps.toml

# Address to bind the server to (0.0.0.0 allows external connections)
bindAddr = "0.0.0.0"
bindPort = 7000

# Authentication token - MUST match FRP_TOKEN in the API config
auth.method = "token"
auth.token = "change-this-to-a-secure-random-token"

# Dashboard (Optional, for monitoring)
webServer.addr = "0.0.0.0"
webServer.port = 7500
webServer.user = "admin"
webServer.password = "admin-password"

# Allowed ports for user tunnels
allowPorts = [
  { start = 20000, end = 30000 }
]
```

3.  Run the FRP server:
    ```bash
    ./frps -c frps.toml
    ```

---

## 2. Installation: Docker (Recommended)

The easiest way to run the API is using Docker Compose.

1.  **Clone the repository**:
    ```bash
    git clone https://github.com/MakotoPD/minedash-backend.git
    cd minedash-backend
    ```

2.  **Create `.env` file**:
    ```bash
    cp .env.example .env
    ```
    Edit `.env` and fill in your details (Database URL, JWT Secret, FRP Token from step 1).

3.  **Run with Docker Compose**:
    ```bash
    docker-compose up -d --build
    ```

### Example `docker-compose.yml`

```yaml
version: '3.8'

services:
  api:
    build: .
    ports:
      - "8080:8080"
    environment:
      - SERVER_PORT=8080
      - DATABASE_URL=postgres://user:pass@db:5432/tunneldb?sslmode=disable
      - JWT_SECRET=secure-jwt-secret
      - FRP_TOKEN=your-frp-token-here
      - FRP_SERVER_ADDR=x.x.x.x
    depends_on:
      - db

  db:
    image: postgres:15-alpine
    environment:
      POSTGRES_USER: user
      POSTGRES_PASSWORD: pass
      POSTGRES_DB: tunneldb
    volumes:
      - db_data:/var/lib/postgresql/data

volumes:
  db_data:
```

---

## 3. Installation: Manual (No Docker)

If you prefer to run the binary directly on your system.

### Prerequisites
- **Go 1.22+** installed.
- **PostgreSQL** database running.

### Steps

1.  **Clone the repository**:
    ```bash
    git clone https://github.com/MakotoPD/minedash-backend.git
    cd minedash-backend
    ```

2.  **Download dependencies**:
    ```bash
    go mod download
    ```

3.  **Build the application**:
    ```bash
    go build -o tunnel-api cmd/server/main.go
    ```

4.  **Set Environment Variables**:
    You can use a `.env` file or export them in your shell.

    **Linux/MacOS**:
    ```bash
    export DATABASE_URL="postgres://user:pass@localhost:5432/tunneldb?sslmode=disable"
    export JWT_SECRET="your-secret"
    export FRP_TOKEN="your-frp-token"
    # ... any other variables
    ```

    **Windows (PowerShell)**:
    ```powershell
    $env:DATABASE_URL="postgres://user:pass@localhost:5432/tunneldb?sslmode=disable"
    $env:JWT_SECRET="your-secret"
    $env:FRP_TOKEN="your-frp-token"
    ```

5.  **Run the application**:
    ```bash
    ./tunnel-api
    ```
    (Or `.\tunnel-api.exe` on Windows)

---

## Configuration Variables

Configure these in your `.env` file or Docker environment.

| Variable | Description | Default / Example |
|---------|------|------------------|
| **Server** |
| `SERVER_PORT` | Port the API listens on | `8080` |
| `SERVER_HOST` | Host interface to bind to | `0.0.0.0` |
| `GIN_MODE` | Gin framework mode | `release` or `debug` |
| **Database** |
| `DATABASE_URL` | PostgreSQL connection string | `postgres://u:p@host:5432/db` |
| **Security** |
| `JWT_SECRET` | Secret key for signing tokens | ❗ **REQUIRED** (min 32 chars) |
| `JWT_ACCESS_TTL` | Access token validity (minutes) | `60` |
| `JWT_REFRESH_TTL` | Refresh token validity (days) | `7` |
| **FRP Integration** |
| `FRP_SERVER_ADDR` | Public IP/Domain of your FRP Server | `0.0.0.0` |
| `FRP_SERVER_PORT` | FRP Server Bind Port (Control Port) | `7000` |
| `FRP_TOKEN` | Auth Token (Must match `frps.toml`) | ❗ **REQUIRED** |
| **Tunnels Config** |
| `MIN_PORT` | Start of port range for allocation | `20000` |
| `MAX_PORT` | End of port range for allocation | `30000` |
| `MAX_TUNNELS` | Tunnels allowed per user | `3` |
| `DOMAIN` | Base domain for subdomains | `eu.makoto.com.pl` |
| `REGION` | Region identifier | `eu` |
| **SMTP / Mail** |
| `SMTP_HOST` | SMTP Host (e.g. Mailgun/SendGrid) | - |
| `SMTP_PORT` | SMTP Port | `587` |
| `SMTP_USER` | SMTP Username | - |
| `SMTP_PASSWORD`| SMTP Password | - |
| `SMTP_FROM` | Email sender address | `noreply@yourdomain.com` |

---

## API Endpoints

### Public
- `GET /health` - Service Health Check
- `GET /ping` - Simple Ping
- `POST /api/auth/register` - Register new account
- `POST /api/auth/login` - Login (returns Access & Refresh Token)
- `POST /api/auth/refresh` - Refresh token
- `POST /api/auth/logout` - Logout
- `POST /api/auth/forgot-password` - Request password reset
- `POST /api/auth/reset-password` - Reset password with code

### Protected (Requires `Authorization: Bearer <token>`)

#### User
- `GET /api/auth/me` - Get current user info

#### Two-Factor Authentication (2FA)
- `POST /api/auth/2fa/setup` - Generate TOTP secret
- `POST /api/auth/2fa/verify` - Activate 2FA
- `POST /api/auth/2fa/disable` - Disable 2FA

#### Tunnels
- `GET /api/tunnels` - List my tunnels
- `POST /api/tunnels` - Create tunnel
- `GET /api/tunnels/:id` - Get details
- `DELETE /api/tunnels/:id` - Delete tunnel
- `POST /api/tunnels/:id/start` - Start tunnel
- `POST /api/tunnels/:id/stop` - Stop tunnel
- `GET /api/tunnels/:id/config` - Get FRPC configuration
