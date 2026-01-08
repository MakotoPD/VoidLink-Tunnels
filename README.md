# Tunnel API - VoidLink Backend

Backend service written in Go (Gin) for the VoidLink Tunnel System.

## Quick Deploy with Coolify

1. **In Coolify, create a new project** and select "Docker Compose".
2. **Source**: Provide the Git repository URL or upload files directly.
3. **Environment Variables** (set in Coolify Secrets):

```bash
# Database
DATABASE_URL=postgres://tunnel:PASSWORD@postgres:5432/tunneldb?sslmode=disable
POSTGRES_PASSWORD=your-secure-db-password

# Security
JWT_SECRET=your-very-long-secret-key-min-32-chars
FRP_TOKEN=token-from-your-frps-server

# Domain configuration
DOMAIN=eu.makoto.com.pl

# SMTP (Required for password reset)
SMTP_HOST=smtp.mailgun.org
SMTP_PORT=587
SMTP_USER=postmaster@your-domain.com
SMTP_PASSWORD=your-smtp-key
SMTP_FROM=noreply@your-domain.com (optional)
```

4. **Ports**:
   - `8080` → Tunnel API (HTTP)
   
5. **Domain**: Set the domain, e.g., `tunnel-api.makoto.com.pl` in Coolify.

## FRP Server Configuration

The FRP Server must run separately (not within the API Docker container, unless you use a custom image). Ensure that:
- Port `7000` is open for FRP control.
- Ports `20000-30000` are open for tunnels (TCP/UDP).
- The token in `frps.toml` matches the `FRP_TOKEN` variable.

## Environment Variables

| Variable | Description | Default Value |
|---------|------|------------------|
| **Server** |
| `SERVER_PORT` | API Port | `8080` |
| `SERVER_HOST` | API Host | `0.0.0.0` |
| `GIN_MODE` | Gin Mode (debug/release) | `release` (if set) |
| **Database** |
| `DATABASE_URL` | PostgreSQL connection string | `postgres://tunnel:tunnel@postgres:5432/tunneldb...` |
| **Security** |
| `JWT_SECRET` | JWT Signing Key | ❗ **REQUIRED** |
| `JWT_ACCESS_TTL` | Access token validity (minutes) | `60` |
| `JWT_REFRESH_TTL` | Refresh token validity (days) | `7` |
| **FRP Integration** |
| `FRP_SERVER_ADDR` | Public address of FRP server | `0.0.0.0` |
| `FRP_SERVER_PORT` | FRP Control Port | `7000` |
| `FRP_TOKEN` | FRP Authorization Token | ❗ **REQUIRED** |
| **Tunnels Config** |
| `MIN_PORT` | Minimum tunnel port | `20000` |
| `MAX_PORT` | Maximum tunnel port | `30000` |
| `MAX_TUNNELS` | Tunnels per user limit | `3` |
| `DOMAIN` | Main domain for subdomains | `eu.makoto.com.pl` |
| `REGION` | Region (for display) | `eu` |
| **SMTP / Mail** |
| `SMTP_HOST` | SMTP Server Host | - |
| `SMTP_PORT` | SMTP Server Port | `587` |
| `SMTP_USER` | SMTP Username | - |
| `SMTP_PASSWORD`| SMTP Password | - |
| `SMTP_FROM` | Sender Address | `noreply@makoto.com.pl` |

## API Endpoints

### Public
- `GET /health` - Service Health Check
- `GET /ping` - Simple Ping
- `POST /api/auth/register` - Register new account
- `POST /api/auth/login` - Login (returns Access & Refresh Token)
- `POST /api/auth/refresh` - Refresh token
- `POST /api/auth/logout` - Logout
- `POST /api/auth/forgot-password` - Request password reset (sends email)
- `POST /api/auth/reset-password` - Set new password using code

### Protected (Requires Bearer Token)

#### User
- `GET /api/auth/me` - Get current user details

#### Two-Factor Authentication (2FA)
- `POST /api/auth/2fa/setup` - Generate TOTP secret (returns QR code)
- `POST /api/auth/2fa/verify` - Verify code and enable 2FA
- `POST /api/auth/2fa/disable` - Disable 2FA

#### Tunnels
- `GET /api/tunnels` - List user tunnels
- `POST /api/tunnels` - Create new tunnel
- `GET /api/tunnels/:id` - Get tunnel details
- `DELETE /api/tunnels/:id` - Delete tunnel
- `POST /api/tunnels/:id/start` - Start tunnel (generates client config)
- `POST /api/tunnels/:id/stop` - Stop tunnel (releases port)
- `GET /api/tunnels/:id/config` - Get FRPC configuration for tunnel
