# Tunnel API - Coolify Deployment

## Quick Deploy with Coolify

1. **W Coolify, utwórz nowy projekt** i wybierz "Docker Compose"

2. **Źródło**: Podaj URL do repozytorium Git lub wgraj pliki

3. **Zmienne środowiskowe** (ustaw w Coolify Secrets):
   ```
   DATABASE_URL=postgres://tunnel:HASLO@postgres:5432/tunneldb?sslmode=disable
   JWT_SECRET=twoj-bardzo-dlugi-tajny-klucz-minimum-32-znaki
   FRP_TOKEN=token-z-twojego-frps-serwera
   POSTGRES_PASSWORD=bezpieczne-haslo-do-bazy
   DOMAIN=eu.makoto.com.pl
   ```

4. **Porty**:
   - `8080` → Tunnel API (HTTP)
   
5. **Domena**: Ustaw domenę np. `tunnel-api.makoto.com.pl` w Coolify

## Konfiguracja FRP Server

FRP Server musi działać osobno (nie w Docker). Upewnij się że:
- Port `7000` jest otwarty dla FRP control
- Porty `20000-30000` są otwarte dla tuneli
- Token w `frps.toml` = token w zmiennej `FRP_TOKEN`

## Zmienne środowiskowe

| Zmienna | Opis | Domyślna wartość |
|---------|------|------------------|
| `DATABASE_URL` | PostgreSQL connection string | `postgres://tunnel:tunnel@postgres:5432/tunneldb` |
| `JWT_SECRET` | Klucz do podpisywania JWT | ❗ WYMAGANE |
| `JWT_ACCESS_TTL` | Ważność access token (minuty) | `60` |
| `JWT_REFRESH_TTL` | Ważność refresh token (dni) | `7` |
| `FRP_SERVER_ADDR` | Adres FRP server | `0.0.0.0` |
| `FRP_SERVER_PORT` | Port FRP server | `7000` |
| `FRP_TOKEN` | Token autoryzacji FRP | ❗ WYMAGANE |
| `MIN_PORT` | Minimalny port dla tuneli | `20000` |
| `MAX_PORT` | Maksymalny port dla tuneli | `30000` |
| `MAX_TUNNELS` | Limit tuneli na użytkownika | `3` |
| `DOMAIN` | Domena dla subdomen | `eu.makoto.com.pl` |
| `REGION` | Region (do przyszłego użytku) | `eu` |

## Health Check

```bash
curl http://localhost:8080/health
```

## API Endpoints

- `POST /api/auth/register` - Rejestracja
- `POST /api/auth/login` - Logowanie
- `GET /api/tunnels` - Lista tuneli
- `POST /api/tunnels` - Utwórz tunel
- `POST /api/tunnels/:id/start` - Uruchom tunel
- `POST /api/tunnels/:id/stop` - Zatrzymaj tunel
